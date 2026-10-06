package scripts_test

import (
	"bufio"
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

// lockScript returns a command running with-check-lock.sh with args.
func lockScript(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	return exec.CommandContext(t.Context(), "./with-check-lock.sh", args...)
}

// reap kills and waits for cmd if the test ended before waiting for it, so a
// failed test leaves no process and no os/exec goroutine behind.
func reap(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	})
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
	// has said it is waiting, so load cannot let the second find the lock free.
	// The first run gives up on its own after 120s, longer than every wait below
	// put together, only so that a failed test leaves nothing running for long.
	t.Run("ASecondRunWaitsForTheFirst", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)

		dir := t.TempDir()
		lock := filepath.Join(dir, "gate.lock")
		trace := filepath.Join(dir, "trace")
		release := filepath.Join(dir, "release")
		seen := filepath.Join(dir, "seen")

		first := lockScript(t, lock, "sh", "-c", `echo held > "$1"
i=0
while [ ! -e "$2" ] && [ "$i" -lt 2400 ]; do sleep 0.05; i=$((i + 1)); done
echo done >> "$1"`, "first", trace, release)
		require.NoError(t, first.Start())
		reap(t, first)

		// The first run holds the lock once its command has written.
		require.Eventually(t, func() bool {
			_, err := os.Stat(trace)
			return err == nil
		}, 10*time.Second, 20*time.Millisecond, "the first run never started its command")

		second := lockScript(t, lock, "sh", "-c", `cat "$1" > "$2"`, "second", trace, seen)
		stderr, err := second.StderrPipe()
		require.NoError(t, err)
		require.NoError(t, second.Start())
		reap(t, second)

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
	// later gate. The next run must find the lock free while the leftover is
	// still alive, which the test checks after the run rather than assumes.
	t.Run("ALeftoverChildDoesNotKeepTheLock", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)

		dir := t.TempDir()
		lock := filepath.Join(dir, "gate.lock")
		pidFile := filepath.Join(dir, "leftover.pid")

		out, err := lockScript(t, lock, "sh", "-c",
			`sleep 60 > /dev/null 2>&1 & echo $! > "$1"; exit 0`, "first", pidFile).CombinedOutput()
		require.NoError(t, err, "a run that leaves a child behind: %s", out)

		raw, err := os.ReadFile(pidFile)
		require.NoError(t, err)
		pid := strings.TrimSpace(string(raw))
		t.Cleanup(func() { _ = exec.Command("kill", pid).Run() })

		out, err = lockScript(t, lock, "true").CombinedOutput()
		require.NoError(t, err, "the next run: %s", out)
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
