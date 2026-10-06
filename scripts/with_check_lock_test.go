package scripts_test

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireShell skips on Windows, where with-check-lock.sh, a POSIX shell script,
// cannot run; the gate it serves needs make and bash there anyway.
func requireShell(t *testing.T) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("with-check-lock.sh is a POSIX shell script")
	}
}

// requireLockTool skips where the machine has no lock tool the script can use,
// except under CI, where a skip would report success for a script nothing
// exercised. It asks the tool the script would choose to take a lock the way the
// script does, because an installed flock too old for -E makes the script fall
// back to running unlocked, which is right for the script and would fail the
// contention test. It returns that probe, which exits 75 when lock is held.
func requireLockTool(t *testing.T) (probe func(lock string) []string) {
	t.Helper()
	requireShell(t)

	switch {
	case lookPath("lockf"):
		probe = func(lock string) []string { return []string{"lockf", "-k", "-t", "0", lock, "true"} }
	case lookPath("flock"):
		probe = func(lock string) []string { return []string{"flock", "-n", "-E", "75", lock, "true"} }
	}

	var err error
	if probe == nil {
		err = errors.New("neither lockf nor flock is installed")
	} else {
		argv := probe(filepath.Join(t.TempDir(), "probe.lock"))
		err = exec.CommandContext(t.Context(), argv[0], argv[1:]...).Run()
	}
	if err == nil {
		return probe
	}

	if os.Getenv("CI") != "" {
		t.Fatalf("no usable lock tool under CI (%v), so with-check-lock.sh would go untested", err)
	}
	t.Skipf("no usable lock tool: %v", err)
	return nil
}

func lookPath(tool string) bool {
	_, err := exec.LookPath(tool)
	return err == nil
}

// exitCode returns the exit status err reports, failing the test if err is not
// a process exiting with one.
func exitCode(t *testing.T, err error) int {
	t.Helper()

	var exitErr *exec.ExitError
	require.True(t, errors.As(err, &exitErr), "want a non-zero exit status, got %v", err)
	return exitErr.ExitCode()
}

// lockScript returns a command running with-check-lock.sh with args, by its
// absolute path so that a test may run it in another directory.
func lockScript(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	script, err := filepath.Abs("with-check-lock.sh")
	require.NoError(t, err)
	return exec.CommandContext(t.Context(), script, args...)
}

// holdUntilReleased is shell that writes "held" to $1, then waits until $2
// exists. It never gives up and succeeds: past its two-minute watchdog, or once
// $1 is gone because the test's temporary directory was removed, it exits 9, so
// a test whose release never came fails rather than seeing the lock come free.
const holdUntilReleased = `echo held > "$1"
i=0
while [ ! -e "$2" ]; do
	i=$((i + 1))
	if [ "$i" -gt 2400 ] || [ ! -e "$1" ]; then exit 9; fi
	sleep 0.05
done
`

// releaseAndReap releases a holder, when release is not empty, and reaps cmd if
// the test ends before it does, so no holder outlives its test. The test's
// context is cancelled first, which kills the lock tool; the release ends the
// shell the lock tool was running.
func releaseAndReap(t *testing.T, cmd *exec.Cmd, release string) {
	t.Helper()
	t.Cleanup(func() {
		if cmd.ProcessState != nil {
			return
		}
		if release != "" {
			_ = os.WriteFile(release, nil, 0o600)
		}
		_ = cmd.Wait()
	})
}

// waitForFile polls until path exists, failing with msg after ten seconds.
func waitForFile(t *testing.T, path, msg string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, err := os.Stat(path)
		return err == nil
	}, 10*time.Second, 20*time.Millisecond, msg)
}

// makeEnv is the environment for a make the test runs: this process's, without
// anything an enclosing make or the caller set that would change the Makefile's
// own defaults, plus extra.
func makeEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "MAKEFLAGS", "MFLAGS", "MAKELEVEL", "MAKEOVERRIDES", "GATE_LOCK", "GATE_SUBMAKE", "XDG_CACHE_HOME":
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

func TestWithCheckLock(t *testing.T) {
	t.Parallel()

	// The command's exit status is the script's, so `make verify` failing under
	// the lock still fails.
	t.Run("ReturnsTheCommandsStatus", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)

		err := lockScript(t, filepath.Join(t.TempDir(), "gate.lock"), "sh", "-c", "exit 3").Run()
		assert.Equal(t, 3, exitCode(t, err))
	})

	// A second run waits for the first, says so in one line of its own, and
	// starts only once the first has finished. The first run holds the lock
	// until the test releases it, and the test releases it only after the second
	// has said it is waiting, so no amount of load can let the second find the
	// lock free: the timeouts bound a failure and decide nothing.
	t.Run("ASecondRunWaitsForTheFirst", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)

		dir := t.TempDir()
		lock := filepath.Join(dir, "gate.lock")
		trace := filepath.Join(dir, "trace")
		release := filepath.Join(dir, "release")
		seen := filepath.Join(dir, "seen")

		first := lockScript(t, lock, "sh", "-c", holdUntilReleased+`echo done >> "$1"`, "first", trace, release)
		require.NoError(t, first.Start())
		releaseAndReap(t, first, release)
		waitForFile(t, trace, "the first run never started its command")

		second := lockScript(t, lock, "sh", "-c", `cat "$1" > "$2"`, "second", trace, seen)
		stderr, err := second.StderrPipe()
		require.NoError(t, err)
		require.NoError(t, second.Start())
		releaseAndReap(t, second, "")

		// Both channels are buffered for the one value each ever carries, so the
		// reader never blocks on a test that stopped listening.
		firstLine := make(chan string, 1)
		allLines := make(chan []string, 1)
		go func() {
			var lines []string
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				if len(lines) == 0 {
					firstLine <- scanner.Text()
				}
				lines = append(lines, scanner.Text())
			}
			close(firstLine)
			allLines <- lines
		}()

		select {
		case _, ok := <-firstLine:
			require.True(t, ok, "the second run ended without saying it was waiting")
		case <-time.After(30 * time.Second):
			require.FailNow(t, "the second run said nothing in 30s")
		}

		// Saying it waits is not waiting: the second run's command must still
		// not have started a second later, while the first still holds the lock.
		time.Sleep(time.Second)
		_, err = os.Stat(seen)
		require.ErrorIs(t, err, os.ErrNotExist, "the second run's command started while the first still held the lock")

		require.NoError(t, os.WriteFile(release, nil, 0o600), "release the first run")

		var said []string
		select {
		case said = <-allLines:
		case <-time.After(30 * time.Second):
			require.FailNow(t, "the second run's stderr stayed open 30s after the first was released")
		}
		require.NoError(t, second.Wait(), "the second run: %q", said)
		require.NoError(t, first.Wait(), "the first run")

		got, err := os.ReadFile(seen)
		require.NoError(t, err)
		assert.Equal(t, "held\ndone\n", string(got),
			"the second run's command must see the first run's whole trace; the two overlapped")

		// One line, the script's own: the probe's lock tool says "already
		// locked" in its own words, and a waiting run should not print it.
		if assert.Len(t, said, 1, "the second run's stderr: %q", said) {
			assert.Contains(t, said[0], "waiting for it to finish")
		}
	})

	// A process the command leaves behind does not keep the lock. util-linux
	// flock passes the locked descriptor to the command and so to anything it
	// starts, unless told -o; a daemon a test left running would then hold every
	// later gate. The child says it is up and stays up until released, and the
	// test checks it was still alive when the next run finished, so a slow
	// machine cannot let the child expire first and the run pass vacuously.
	t.Run("ALeftoverChildDoesNotKeepTheLock", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)

		dir := t.TempDir()
		lock := filepath.Join(dir, "gate.lock")
		up := filepath.Join(dir, "up")
		release := filepath.Join(dir, "release")
		pidFile := filepath.Join(dir, "leftover.pid")

		// Released however the test ends, before anything below can fail.
		t.Cleanup(func() { _ = os.WriteFile(release, nil, 0o600) })
		out, err := lockScript(t, lock, "sh", "-c", `sh -c "$1" leftover "$2" "$3" > /dev/null 2>&1 &
echo $! > "$4"`, "leaver", holdUntilReleased, up, release, pidFile).CombinedOutput()
		require.NoError(t, err, "a run that leaves a child behind: %s", out)
		waitForFile(t, up, "the leftover child never started")

		raw, err := os.ReadFile(pidFile)
		require.NoError(t, err)
		pid := strings.TrimSpace(string(raw))

		// Bounded on its own: a lock the child kept would make this wait.
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
		defer cancel()
		out, err = exec.CommandContext(ctx, "./with-check-lock.sh", lock, "true").CombinedOutput()
		require.NoError(t, err, "the next run, with the leftover child still alive: %s", out)

		require.NoError(t, exec.CommandContext(t.Context(), "kill", "-0", pid).Run(),
			"the leftover child had exited before the next run finished, so the run proves nothing")
		assert.NotContains(t, string(out), "waiting",
			"the next run waited for a lock only a leftover child held")
	})

	// A lock path holding spaces is one path: its directories are made and the
	// command runs holding the lock there, not at the pieces a word split would
	// give. The command asks the lock tool itself whether that path is held.
	t.Run("HoldsAPathWithSpaces", func(t *testing.T) {
		t.Parallel()
		probe := requireLockTool(t)

		lock := filepath.Join(t.TempDir(), "a cache", "dev gate.lock")
		args := append([]string{lock, "sh", "-c", `"$@"; test "$?" -eq 75`, "held"}, probe(lock)...)

		out, err := lockScript(t, args...).CombinedOutput()
		assert.NoError(t, err, "the command did not find %q held while it ran: %s", lock, out)
	})

	// An empty lock path is the one every repository's gate shares, worked out
	// in the shell so an XDG_CACHE_HOME holding spaces stays one path, and a
	// relative XDG_CACHE_HOME is not used: it would give every checkout a lock of
	// its own. The command asks the lock tool whether the expected path is held.
	t.Run("AnEmptyLockPathIsTheSharedOne", func(t *testing.T) {
		t.Parallel()
		probe := requireLockTool(t)

		dir := t.TempDir()
		home := filepath.Join(dir, "home")
		for name, tc := range map[string]struct{ xdg, want string }{
			"absolute with spaces": {filepath.Join(dir, "a cache"), filepath.Join(dir, "a cache", "dev-gate.lock")},
			"relative":             {"relative-cache", filepath.Join(home, ".cache", "dev-gate.lock")},
		} {
			args := append([]string{"", "sh", "-c", `"$@"; test "$?" -eq 75`, "held"}, probe(tc.want)...)
			cmd := lockScript(t, args...)
			// In the temporary directory, so a relative path taken by mistake
			// lands there and not in the repository.
			cmd.Dir = dir
			cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CACHE_HOME="+tc.xdg)

			out, err := cmd.CombinedOutput()
			assert.NoError(t, err, "%s: the command did not find %s held while it ran: %s", name, tc.want, out)
		}
	})

	// `make verify` hands the script the shared path whole. With MAKE=echo, so
	// that GATE_SUBMAKE is echo, the recipe runs the real script around an echo
	// instead of the gate: this is the Makefile's own line, with an
	// XDG_CACHE_HOME holding spaces and none of the caller's make settings.
	t.Run("MakeVerifyTakesTheSharedLock", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)
		if !lookPath("make") {
			if os.Getenv("CI") != "" {
				t.Fatal("make is not installed under CI, so the gate's own lock line would go untested")
			}
			t.Skip("make is not installed")
		}

		dir := t.TempDir()
		xdg := filepath.Join(dir, "a cache")
		cmd := exec.CommandContext(t.Context(), "make", "-C", "..", "--no-print-directory", "MAKE=echo", "verify")
		// HOME too, so that a lock path taken by mistake is never the machine's
		// shared one, which another gate may be holding.
		cmd.Env = makeEnv("HOME="+filepath.Join(dir, "home"), "XDG_CACHE_HOME="+xdg)

		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "make verify with MAKE=echo: %s", out)
		assert.Contains(t, string(out), "verify-unlocked", "the recipe did not run its steps' target under the script")
		_, err = os.Stat(filepath.Join(xdg, "dev-gate.lock"))
		assert.NoError(t, err, "make verify did not take the shared lock under an XDG_CACHE_HOME with spaces: %s", out)
	})

	// A lock that cannot be made is a courtesy lost, not a gate: the command
	// still runs, its status is still the script's, and the run says it is
	// unlocked.
	t.Run("RunsWhenTheLockCannotBeMade", func(t *testing.T) {
		t.Parallel()
		requireShell(t)

		blocker := filepath.Join(t.TempDir(), "file")
		require.NoError(t, os.WriteFile(blocker, nil, 0o600), "a file where a directory must go")

		out, err := lockScript(t, filepath.Join(blocker, "sub", "gate.lock"), "sh", "-c", "exit 4").CombinedOutput()
		assert.Equal(t, 4, exitCode(t, err), "output: %s", out)
		assert.Contains(t, string(out), "not serialised with others")
	})

	// A call without a command is refused rather than run as a lock with
	// nothing in it.
	t.Run("RefusesACallWithoutACommand", func(t *testing.T) {
		t.Parallel()
		requireShell(t)

		err := lockScript(t, filepath.Join(t.TempDir(), "gate.lock")).Run()
		assert.Equal(t, 2, exitCode(t, err))
	})
}
