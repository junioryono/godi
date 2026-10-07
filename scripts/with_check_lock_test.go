//go:build unix

// with-check-lock.sh is a POSIX shell script, and these tests hold its runs in
// process groups of their own, so they build only where both exist; the gate it
// serves needs make and bash anyway.

package scripts_test

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireLockTool skips where the machine has no lock tool the script can use,
// except under CI, where a skip would report success for a script nothing
// exercised. It asks the tool the script would choose to take a lock the way the
// script does, because an installed flock too old for -E makes the script fall
// back to running unlocked, which is right for the script and would fail the
// contention test. It returns that probe, which exits 75 when lock is held.
func requireLockTool(t *testing.T) (probe func(lock string) []string) {
	t.Helper()

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

// requireMake skips where make is not installed, except under CI, where the
// gate's own lock line would go untested.
func requireMake(t *testing.T) {
	t.Helper()

	if lookPath("make") {
		return
	}
	if os.Getenv("CI") != "" {
		t.Fatal("make is not installed under CI, so the gate's own lock line would go untested")
	}
	t.Skip("make is not installed")
}

func lookPath(tool string) bool {
	_, err := exec.LookPath(tool)
	return err == nil
}

// exitCode returns the exit status err reports, failing the test if err is not
// a process exiting with one. A process killed by a signal reports -1.
func exitCode(t *testing.T, err error) int {
	t.Helper()

	var exitErr *exec.ExitError
	require.True(t, errors.As(err, &exitErr), "want a non-zero exit status, got %v", err)
	return exitErr.ExitCode()
}

// scriptPath is with-check-lock.sh by its absolute path, so that a test may run
// it in another directory.
func scriptPath(t *testing.T) string {
	t.Helper()
	script, err := filepath.Abs("with-check-lock.sh")
	require.NoError(t, err)
	return script
}

// lockScript returns a command running with-check-lock.sh with args in
// scriptEnv's environment.
func lockScript(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), scriptPath(t), args...)
	cmd.Env = scriptEnv()
	return cmd
}

// boundedLockScript is lockScript for a run whose failure is a wait that never
// ends: it runs in a process group of its own, killed whole after twenty
// seconds or when the test ends.
func boundedLockScript(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, scriptPath(t), args...)
	cmd.Env = scriptEnv()
	inGroup(cmd)
	return cmd
}

// inGroup starts cmd in a process group of its own and has its cancellation
// kill the whole group, so a lock tool, a shell or a sub-make it started cannot
// outlive the test.
func inGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	// A killed group's pipes close with it; this bounds a straggler holding one.
	cmd.WaitDelay = 2 * time.Second
}

// scriptEnv is this process's environment, without the variable naming the
// locks an enclosing gate holds (this may run inside `make verify`, and a test's
// run of the script must not take that gate's lock for its ancestor's), plus
// extra, which replaces a variable of the same name.
func scriptEnv(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name == "DEV_GATE_LOCK_HELD" || setIn(extra, name) {
			continue
		}
		env = append(env, kv)
	}
	return append(env, extra...)
}

// setIn reports whether one of kvs assigns name.
func setIn(kvs []string, name string) bool {
	for _, kv := range kvs {
		if n, _, _ := strings.Cut(kv, "="); n == name {
			return true
		}
	}
	return false
}

// makeEnv is scriptEnv for a make the test runs, without anything an enclosing
// make or the caller set that would change the Makefile's own defaults, unless
// extra sets it.
func makeEnv(extra ...string) []string {
	var env []string
	for _, kv := range scriptEnv(extra...) {
		name, _, _ := strings.Cut(kv, "=")
		switch name {
		case "MAKEFLAGS", "MFLAGS", "MAKELEVEL", "MAKEOVERRIDES", "GATE_LOCK", "GATE_SUBMAKE", "XDG_CACHE_HOME":
			if !setIn(extra, name) {
				continue
			}
		}
		env = append(env, kv)
	}
	return env
}

// forkSafeWriteFile writes an executable a test is about to run.
//
// Under syscall.ForkLock, which every os/exec start holds while it creates its
// child: a fork by another parallel test while this file is open for writing
// inherits the descriptor, and exec of the file then fails with ETXTBSY ("text
// file busy") until that child execs (Go issue 22315). A shell that meets it on a
// shebang interpreter reports exit 126 and fails the test for nothing.
func forkSafeWriteFile(name string, data []byte, perm os.FileMode) error {
	syscall.ForkLock.Lock()
	defer syscall.ForkLock.Unlock()
	return os.WriteFile(name, data, perm)
}

// fakeTools writes each script in fakes as an executable of that name in a
// directory of its own and returns a PATH that finds them first.
func fakeTools(t *testing.T, fakes map[string]string) string {
	t.Helper()
	bin := t.TempDir()
	for name, script := range fakes {
		require.NoError(t, forkSafeWriteFile(filepath.Join(bin, name), []byte(script), 0o700), "write the fake %s", name)
	}
	return "PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH")
}

// holdUntilReleased is shell that writes its pid to $1.pid and "held" to $1,
// then waits until $2 exists. It never gives up and succeeds: past its
// two-minute watchdog, or once $1 is gone because the test's temporary directory
// was removed, it exits 9, so a test whose release never came fails rather than
// seeing the lock come free.
const holdUntilReleased = `echo $$ > "$1.pid"
echo held > "$1"
i=0
while [ ! -e "$2" ]; do
	i=$((i + 1))
	if [ "$i" -gt 2400 ] || [ ! -e "$1" ]; then exit 9; fi
	sleep 0.05
done
`

// holderContext is the context a lock holder runs under: not the test's, which
// is cancelled before cleanup runs and would kill the lock tool and orphan the
// holder's shell, but one cleanup cancels only after releaseAndReap has
// released the holder and given it ten seconds to finish.
func holderContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithCancel(context.WithoutCancel(t.Context()))
	t.Cleanup(cancel)
	return ctx
}

// startHolder starts a run of the script that holds lock, writes "held" to
// trace, waits until release exists and then appends "done" to trace. It
// returns once the holder's command has started. The holder runs in a process
// group of its own and is released and reaped however the test ends.
func startHolder(t *testing.T, lock, trace, release string) *exec.Cmd {
	t.Helper()
	holder := exec.CommandContext(holderContext(t), scriptPath(t), lock, "sh", "-c",
		holdUntilReleased+`echo done >> "$1"`, "holder", trace, release)
	holder.Env = scriptEnv()
	inGroup(holder)
	require.NoError(t, holder.Start(), "start the holder")
	releaseAndReap(t, holder, release)
	waitForFile(t, trace, "the holder never started its command")
	return holder
}

// releaseAndReap releases a holder and reaps it if the test ends before it
// does, so no holder outlives its test. Registered after holderContext's
// cancel, it runs before it. One still running ten seconds after its release
// is killed with its whole group, and the test fails.
func releaseAndReap(t *testing.T, cmd *exec.Cmd, release string) {
	t.Helper()
	t.Cleanup(func() {
		if cmd.ProcessState != nil {
			return
		}
		if err := os.WriteFile(release, nil, 0o600); err != nil {
			t.Errorf("release the holder: %v", err)
		}

		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Errorf("the holder did not finish within 10s of its release, and was killed")
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
			<-done
		}
	})
}

// recordTakes puts a stand-in for the lock tool the script will choose in front
// of PATH: it appends a line to the returned file for every blocking take (any
// call that does not begin as the probe's does: -k -t 0 or -n -E 75; a command
// under the lock may carry a probe of its own) and then runs the real tool, so a
// test can wait until a run is waiting in the lock rather than until it has
// said it will. It returns the run's environment entries; the stand-in reads
// both paths from them, so no path is ever quoted into its source.
func recordTakes(t *testing.T) (env []string, takes string) {
	t.Helper()

	tool := "lockf"
	if !lookPath(tool) {
		tool = "flock"
	}
	toolPath, err := exec.LookPath(tool)
	require.NoError(t, err)

	takes = filepath.Join(t.TempDir(), "takes")
	path := fakeTools(t, map[string]string{
		tool: "#!/bin/sh\ncase \"$1 $2 $3\" in \"-k -t 0\" | \"-n -E 75\") ;; *) echo take >> \"$TEST_LOCK_TAKES\" ;; esac\n" +
			"exec \"$TEST_LOCK_TOOL\" \"$@\"\n",
	})
	return []string{path, "TEST_LOCK_TAKES=" + takes, "TEST_LOCK_TOOL=" + toolPath}, takes
}

// awaitLeftoverExit waits for the process whose pid is in pidFile to exit after
// its release, so no process outlives its test. One still alive ten seconds
// later is killed with its process group, and the test fails.
func awaitLeftoverExit(t *testing.T, pidFile string, group int) {
	t.Helper()

	raw, err := os.ReadFile(pidFile)
	if err != nil {
		return // it never started
	}
	var pid int
	if _, err := fmt.Sscan(string(raw), &pid); err != nil {
		t.Errorf("%s holds %q, not a pid", pidFile, raw)
		return
	}
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if syscall.Kill(pid, 0) != nil {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	_ = syscall.Kill(-group, syscall.SIGKILL)
	t.Errorf("the leftover %d was still running ten seconds after its release, and was killed", pid)
}

// shellQuote quotes s as one word for sh.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// waitForFile polls until path exists, failing with msg after ten seconds.
func waitForFile(t *testing.T, path, msg string) {
	t.Helper()
	require.Eventually(t, func() bool {
		_, err := os.Stat(path)
		return err == nil
	}, 10*time.Second, 20*time.Millisecond, msg)
}

// runOnceUnlocked runs a command that records its runs and exits 5 under the
// script, with TMPDIR at tmp (a directory of the test's own when empty) and
// extra in its environment, and requires that the command ran exactly once,
// with its own status, and that the run said once that it went unlocked. It
// returns what the run printed.
func runOnceUnlocked(t *testing.T, tmp string, extra ...string) string {
	t.Helper()

	dir := t.TempDir()
	runs := filepath.Join(dir, "runs")
	if tmp == "" {
		tmp = dir
	}

	cmd := lockScript(t, filepath.Join(dir, "gate.lock"), "sh", "-c", `echo ran >> "$1"; exit 5`, "command", runs)
	cmd.Env = scriptEnv(append(extra, "TMPDIR="+tmp)...)

	out, err := cmd.CombinedOutput()
	assert.Equal(t, 5, exitCode(t, err), "the command's own status; output: %s", out)
	got, err := os.ReadFile(runs)
	require.NoError(t, err, "the command never ran: %s", out)
	assert.Equal(t, "ran\n", string(got), "the command must run exactly once")
	assert.Contains(t, string(out), "not serialised with others", "the run must say it went unlocked")
	assert.Equal(t, 1, strings.Count(string(out), "with-check-lock: "), "the run must say why once: %s", out)
	return string(out)
}

func TestWithCheckLock(t *testing.T) {
	t.Parallel()

	// The command's exit status is the script's, so `make verify` failing under
	// the lock still fails, and a command that failed is not run again unlocked.
	t.Run("ReturnsTheCommandsStatus", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)

		dir := t.TempDir()
		runs := filepath.Join(dir, "runs")

		err := lockScript(t, filepath.Join(dir, "gate.lock"), "sh", "-c", `echo ran >> "$1"; exit 3`, "failing", runs).Run()
		assert.Equal(t, 3, exitCode(t, err))
		got, err := os.ReadFile(runs)
		require.NoError(t, err)
		assert.Equal(t, "ran\n", string(got), "the failing command must run once")
	})

	// A second run waits for the first, says so in one line of its own, and
	// starts only once the first has finished, holding the lock itself. The first
	// run holds the lock until the test releases it, and the test releases it
	// only after the second is waiting in the lock tool (a stand-in records each
	// blocking take, because the message is printed before the take), so no
	// amount of load can let the second find the lock free: the timeouts bound a
	// failure and decide nothing. The second's command reads the first's trace
	// and then asks the lock tool whether its lock is held: a run that started
	// before the first finished reads half a trace, and one that started after it
	// without the lock finds the lock free, so skipping the lock fails after any
	// delay.
	t.Run("ASecondRunWaitsForTheFirst", func(t *testing.T) {
		t.Parallel()
		probe := requireLockTool(t)

		dir := t.TempDir()
		lock := filepath.Join(dir, "gate.lock")
		trace := filepath.Join(dir, "trace")
		release := filepath.Join(dir, "release")
		seen := filepath.Join(dir, "seen")

		first := startHolder(t, lock, trace, release)
		recorder, takes := recordTakes(t)

		ctx, cancel := context.WithCancel(t.Context())
		second := exec.CommandContext(ctx, scriptPath(t), append([]string{lock, "sh", "-c",
			`trace=$1 seen=$2; shift 2; cat "$trace" > "$seen"; "$@" 2>/dev/null; [ "$?" -eq 75 ] || exit 7`,
			"second", trace, seen}, probe(lock)...)...)
		second.Env = scriptEnv(recorder...)
		inGroup(second)
		stderr, err := second.StderrPipe()
		require.NoError(t, err)
		require.NoError(t, second.Start())
		// Reaped on every path out, a failure below included: the group kill ends
		// it, and with it the lock tool that would otherwise start its command once
		// the holder is released.
		t.Cleanup(func() {
			cancel()
			_ = second.Wait()
		})

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

		// Saying it waits is not waiting: the second run must be waiting in the
		// lock tool, its command not started, before the first is released.
		waitForFile(t, takes, "the second run said it was waiting and never asked for the lock")
		_, err = os.Stat(seen)
		require.ErrorIs(t, err, os.ErrNotExist, "the second run's command started while the first still held the lock")

		require.NoError(t, os.WriteFile(release, nil, 0o600), "release the first run")

		var said []string
		select {
		case said = <-allLines:
		case <-time.After(30 * time.Second):
			require.FailNow(t, "the second run's stderr stayed open 30s after the first was released")
		}
		require.NoError(t, second.Wait(), "the second run, which exits 7 if it ran without the lock: %q", said)
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

	// A run inside a run holding the same lock runs under it: a gate that runs
	// another gate would otherwise wait for an ancestor that cannot finish until
	// it does. The inner command asks the lock tool whether the lock is held, so
	// the outer run must have taken it.
	t.Run("ANestedRunRunsUnderItsAncestorsLock", func(t *testing.T) {
		t.Parallel()
		probe := requireLockTool(t)

		lock := filepath.Join(t.TempDir(), "gate.lock")
		args := append([]string{lock, scriptPath(t), lock, "sh", "-c",
			`echo inner; "$@" 2>/dev/null; [ "$?" -eq 75 ] || exit 6; exit 7`, "inner"}, probe(lock)...)

		out, err := boundedLockScript(t, args...).CombinedOutput()
		assert.Equal(t, 7, exitCode(t, err), "the inner command's status under its ancestor's held lock; output: %s", out)
		assert.Contains(t, string(out), "running under it")
	})

	// A run inside another lock inside its own still runs under its own: a holds
	// A, b inside it holds B, and c inside b asks for A again. Every lock an
	// ancestor holds is handed down, not only the nearest.
	t.Run("ARunNestedThroughAnotherLockRunsUnderItsOwn", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)

		dir := t.TempDir()
		a, b := filepath.Join(dir, "a.lock"), filepath.Join(dir, "b.lock")

		out, err := boundedLockScript(t, a, scriptPath(t), b, scriptPath(t), a,
			"sh", "-c", "echo innermost; exit 7").CombinedOutput()
		assert.Equal(t, 7, exitCode(t, err), "the innermost command's status; output: %s", out)
		assert.Contains(t, string(out), "innermost")
	})

	// A lock named by a run that is not an ancestor is not held for this one: a
	// process a gate left behind, still carrying what that gate handed it, waits
	// like anyone else once another run holds the lock. Here the entry names the
	// holder itself, which this run is not inside.
	t.Run("ALockHeldOutsideThisRunsAncestryIsWaitedFor", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)

		dir := t.TempDir()
		lock := filepath.Join(dir, "gate.lock")
		trace := filepath.Join(dir, "trace")
		release := filepath.Join(dir, "release")
		seen := filepath.Join(dir, "seen")

		holder := startHolder(t, lock, trace, release)
		recorder, takes := recordTakes(t)

		stale := boundedLockScript(t, lock, "sh", "-c", `cat "$1" > "$2"`, "stale", trace, seen)
		stale.Env = scriptEnv(append(recorder, fmt.Sprintf("DEV_GATE_LOCK_HELD=%d:%s", holder.Process.Pid, lock))...)
		stderr, err := stale.StderrPipe()
		require.NoError(t, err)
		require.NoError(t, stale.Start())
		t.Cleanup(func() { _ = stale.Wait() })

		line, err := bufio.NewReader(stderr).ReadString('\n')
		require.NoError(t, err, "the run said nothing before it ended")
		assert.Contains(t, line, "waiting for it to finish", "a run outside the holder's ancestry must wait")

		waitForFile(t, takes, "the run said it was waiting and never asked for the lock")
		require.NoError(t, os.WriteFile(release, nil, 0o600), "release the holder")
		require.NoError(t, stale.Wait(), "the run, once the holder let go")

		// And its command started only once the holder had finished, the holder
		// having been released only after this run was waiting in the lock tool.
		got, err := os.ReadFile(seen)
		require.NoError(t, err)
		assert.Equal(t, "held\ndone\n", string(got), "the run's command must see the holder's whole trace")
	})

	// One relative path in two directories is two locks: a run holding
	// gate.lock in one directory does not hold gate.lock in another, so a run
	// there takes its own.
	t.Run("ARelativeLockPathIsItsOwnDirectorysLock", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)

		first, second := t.TempDir(), t.TempDir()

		cmd := boundedLockScript(t, "gate.lock", "sh", "-c", `cd "$1" && exec "$2" gate.lock true`, "outer", second, scriptPath(t))
		cmd.Dir = first
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "a run inside a run, each with gate.lock in its own directory: %s", out)
		assert.NotContains(t, string(out), "running under it", "the inner run took the outer directory's gate.lock for its own")
		for _, dir := range []string{first, second} {
			assert.FileExists(t, filepath.Join(dir, "gate.lock"), "no lock was taken in %s", dir)
		}
	})

	// A process tree that cannot be read is not an answer: a nested run whose
	// ancestry ps cannot report goes ahead unlocked and says so, rather than read
	// "could not tell" as "not an ancestor" and wait forever on the run it is
	// inside.
	t.Run("ANestedRunThatCannotReadItsAncestryRunsUnlocked", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)

		lock := filepath.Join(t.TempDir(), "gate.lock")
		cmd := boundedLockScript(t, lock, scriptPath(t), lock, "sh", "-c", "echo inner; exit 7")
		cmd.Env = scriptEnv(fakeTools(t, map[string]string{"ps": "#!/bin/sh\nexit 1\n"}))

		out, err := cmd.CombinedOutput()
		assert.Equal(t, 7, exitCode(t, err), "the inner command's status; output: %s", out)
		assert.Contains(t, string(out), "could not tell")
	})

	// A take that fails after the probe succeeded still runs the command,
	// unlocked and saying so, with the command's own status. The lock tool here
	// answers the probe (its only call with -t) and refuses every other take
	// before any command runs, as lockf refuses a file it cannot open. The script
	// prefers lockf, so the fake stands in on Linux too.
	t.Run("AFailedTakeAfterTheProbeStillRunsTheCommand", func(t *testing.T) {
		t.Parallel()

		runOnceUnlocked(t, "", fakeTools(t, map[string]string{
			"lockf": "#!/bin/sh\ncase \" $* \" in *\" -t \"*) exit 0 ;; esac\necho \"lockf: cannot open\" >&2\nexit 71\n",
		}))
	})

	// A marker that cannot be removed runs the command once, unlocked: under the
	// lock the command runs only after its marker is gone, so a removal that
	// failed leaves the marker, the take reads as failed, and the fallback is the
	// command's one run. Without that order a failing command would run twice.
	// This lockf takes nothing and runs its command; this rm removes nothing.
	t.Run("AMarkerThatCannotBeRemovedRunsTheCommandOnce", func(t *testing.T) {
		t.Parallel()

		runOnceUnlocked(t, "", fakeTools(t, map[string]string{
			"lockf": "#!/bin/sh\ncase \" $* \" in *\" -t \"*) exit 0 ;; esac\nshift 2\nexec \"$@\"\n",
			"rm":    "#!/bin/sh\nexit 1\n",
		}))
	})

	// No private directory for the marker is a courtesy lost: without one a
	// failed take could not be told from a failed command, so the command runs
	// unlocked, once, and says why. A run that went on without one would put its
	// marker at /started, which only root can write and every run would share.
	t.Run("ARunWithNoPrivateDirectoryRunsUnlockedOnce", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)

		out := runOnceUnlocked(t, filepath.Join(t.TempDir(), "absent", "tmp"))
		assert.Contains(t, out, "could not make a private directory")
	})

	// A lock path with a newline cannot be one line of the list handed down to
	// nested runs, so the run goes ahead unlocked rather than hold a lock a
	// nested run could not recognise.
	t.Run("ALockPathWithANewlineRunsUnlocked", func(t *testing.T) {
		t.Parallel()

		// One file name with a newline in it, appended rather than joined so it
		// stays one path element.
		out, err := lockScript(t, t.TempDir()+"/gate\nlock", "sh", "-c", "exit 8").CombinedOutput()
		assert.Equal(t, 8, exitCode(t, err), "the command's own status; output: %s", out)
		assert.Contains(t, string(out), "holds a newline")
	})

	// Something other than a file where the lock goes is not opened: a FIFO's
	// open waits for a writer, so the probe would hang and the command never run.
	// The run goes ahead unlocked and says so.
	t.Run("ALockPathThatIsAFIFORunsUnlocked", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)

		lock := filepath.Join(t.TempDir(), "gate.lock")
		require.NoError(t, syscall.Mkfifo(lock, 0o600), "make a FIFO where the lock goes")

		out, err := boundedLockScript(t, lock, "sh", "-c", "exit 9").CombinedOutput()
		assert.Equal(t, 9, exitCode(t, err), "the command's own status; output: %s", out)
		assert.Contains(t, string(out), "not a regular file")
	})

	// An inherited errexit neither stops the script nor is taken from the
	// command. bash takes errexit from an exported SHELLOPTS, so a status the
	// script reads as an answer would end it (measured on macOS's /bin/sh,
	// 2026-10-06: exit 1, the command never run), and any option the script sets
	// rewrites the SHELLOPTS the command inherits: a `set +e` took the command's
	// errexit away, and a `set -u` gave it nounset. The command under the lock
	// must behave exactly as it does run directly, through an unset expansion and
	// a failure. dash ignores SHELLOPTS, so on Linux both runs simply agree.
	t.Run("AnInheritedErrexitReachesTheCommandUnchanged", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)

		dir := t.TempDir()
		command := []string{"sh", "-c",
			`echo ran >> "$1"; unset optional; echo "optional=$optional" >> "$1"; false; echo "carried on" >> "$1"`, "command"}

		run := func(name string, argv ...string) (code int, wrote string) {
			runs := filepath.Join(dir, name)
			cmd := exec.CommandContext(t.Context(), argv[0], append(argv[1:], runs)...)
			cmd.Env = scriptEnv("SHELLOPTS=errexit")

			out, err := cmd.CombinedOutput()
			if err != nil {
				code = exitCode(t, err)
			}
			got, err := os.ReadFile(runs)
			require.NoError(t, err, "%s: the command never ran: %s", name, out)
			return code, string(got)
		}

		directCode, direct := run("direct", command...)
		lockedCode, locked := run("locked", append([]string{scriptPath(t), filepath.Join(dir, "gate.lock")}, command...)...)
		assert.Equal(t, directCode, lockedCode, "the exit status under SHELLOPTS=errexit, run directly and under the lock")
		assert.Equal(t, direct, locked, "what the command did under SHELLOPTS=errexit, run directly and under the lock")
	})

	// A command ended by a signal reports the shell's 128+n under the lock, as it
	// does run directly: macOS lockf reports any signalled child as 70, so the
	// command runs as the child of a shell that exits with its status.
	t.Run("ACommandEndedByASignalReportsItsOwnStatus", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)

		out, err := lockScript(t, filepath.Join(t.TempDir(), "gate.lock"), "sh", "-c", `kill -TERM $$`).CombinedOutput()
		assert.Equal(t, 128+int(syscall.SIGTERM), exitCode(t, err), "a command ended by SIGTERM; output: %s", out)
	})

	// The same with dash as the shell the script runs under the lock (dash
	// stands first on PATH as sh). macOS's /bin/dash runs a subshell that is the
	// last command of `sh -c` in place, so `(exec "$@")` alone replaced the
	// waiting shell and lockf reported the command as 70 (measured 2026-10-07: 70
	// without the trailing exit, 143 with it). Debian's and Ubuntu's dash 0.5.12
	// fork it, so there both lines pass.
	t.Run("ACommandEndedByASignalReportsItsOwnStatusUnderDash", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)

		dash, err := exec.LookPath("dash")
		if err != nil {
			t.Skip("dash is not installed")
		}
		bin := t.TempDir()
		require.NoError(t, os.Symlink(dash, filepath.Join(bin, "sh")), "put dash on PATH as sh")

		cmd := lockScript(t, filepath.Join(t.TempDir(), "gate.lock"), "sh", "-c", `kill -TERM $$`)
		cmd.Env = scriptEnv("PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"))
		out, err := cmd.CombinedOutput()
		assert.Equal(t, 128+int(syscall.SIGTERM), exitCode(t, err), "a command ended by SIGTERM under dash; output: %s", out)
	})

	// And the same under a lock tool that reports a signalled child as 70, with
	// dash as sh, on every platform: the stand-in is this test binary acting as
	// macOS lockf does (see TestFakeLockfProcess), so the rule is held wherever
	// util-linux flock, which reports 128+n itself, would hide it. It fails the
	// line without the trailing exit only under a dash that runs the subshell in
	// place, as macOS's does; Debian's and Ubuntu's dash 0.5.12 fork it (measured
	// 2026-10-07). dash is required under CI, where a skip would report success
	// for the case nothing ran.
	t.Run("ASignalledCommandIsNotReportedAsTheLockToolsFailure", func(t *testing.T) {
		t.Parallel()

		dash, err := exec.LookPath("dash")
		if err != nil {
			if os.Getenv("CI") != "" {
				t.Fatalf("no dash under CI (%v), so the case dash breaks would go untested", err)
			}
			t.Skipf("no dash: %v", err)
		}
		self, err := os.Executable()
		require.NoError(t, err, "find this test binary")

		bin := t.TempDir()
		require.NoError(t, forkSafeWriteFile(filepath.Join(bin, "lockf"),
			[]byte("#!/bin/sh\nexec \"$FAKE_LOCKF_BINARY\" -test.run='^TestFakeLockfProcess$' -- \"$@\"\n"), 0o700),
			"write the stand-in lockf")
		require.NoError(t, os.Symlink(dash, filepath.Join(bin, "sh")), "put dash on PATH as sh")

		cmd := boundedLockScript(t, filepath.Join(t.TempDir(), "gate.lock"), "sh", "-c", `kill -TERM $$`)
		cmd.Env = scriptEnv("PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
			"FAKE_LOCKF=1", "FAKE_LOCKF_BINARY="+self)
		out, err := cmd.CombinedOutput()
		assert.Equal(t, 128+int(syscall.SIGTERM), exitCode(t, err),
			"a command ended by SIGTERM under a lock tool reporting signalled children as 70; output: %s", out)
	})

	// The command is the program it names, not a shell builtin of that name:
	// under the lock it runs from a shell, and `test` there would be the builtin
	// while the unlocked fallback runs the program on PATH.
	t.Run("ALockedCommandIsTheProgramOnPath", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)

		dir := t.TempDir()
		runs := filepath.Join(dir, "runs")
		path := fakeTools(t, map[string]string{"test": "#!/bin/sh\necho ran >> \"$TEST_LOCK_RUNS\"\nexit 7\n"})

		cmd := lockScript(t, filepath.Join(dir, "gate.lock"), "test", "x")
		cmd.Env = scriptEnv(path, "TEST_LOCK_RUNS="+runs)
		out, err := cmd.CombinedOutput()
		assert.Equal(t, 7, exitCode(t, err), "`test x` under the lock must be the program's status; output: %s", out)
		got, err := os.ReadFile(runs)
		require.NoError(t, err, "the program on PATH never ran")
		assert.Equal(t, "ran\n", string(got), "the program on PATH must run once")
	})

	// A diagnostic naming a path prints the path: echo in dash, and in macOS's
	// /bin/sh, reads a backslash sequence as an escape, and \c ends the output
	// there.
	t.Run("ADiagnosticPrintsAPathWithABackslashWhole", func(t *testing.T) {
		t.Parallel()

		// One file name holding a backslash, appended rather than joined so it
		// stays one path element.
		blocker := t.TempDir() + `/cut\cshort`
		require.NoError(t, os.WriteFile(blocker, nil, 0o600), "a file where a directory must go")

		out, err := lockScript(t, filepath.Join(blocker, "sub", "gate.lock"), "true").CombinedOutput()
		require.NoError(t, err, "a run beside a lock that cannot be made: %s", out)
		assert.Contains(t, string(out), filepath.Join(blocker, "sub")+", so this run is not serialised",
			"the diagnostic must carry the path whole")
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

		leaver := lockScript(t, lock, "sh", "-c", `sh -c "$1" leftover "$2" "$3" > /dev/null 2>&1 &`,
			"leaver", holdUntilReleased, up, release)
		inGroup(leaver)
		// Released however the test ends, before anything below can fail, and
		// waited for, so the test leaves no process behind; the child stays in
		// the leaver's process group.
		t.Cleanup(func() {
			_ = os.WriteFile(release, nil, 0o600)
			if leaver.Process != nil {
				awaitLeftoverExit(t, up+".pid", leaver.Process.Pid)
			}
		})
		out, err := leaver.CombinedOutput()
		require.NoError(t, err, "a run that leaves a child behind: %s", out)
		waitForFile(t, up, "the leftover child never started")

		raw, err := os.ReadFile(up + ".pid")
		require.NoError(t, err)
		var pid int
		_, err = fmt.Sscan(string(raw), &pid)
		require.NoError(t, err, "the leftover child's pid")

		// Bounded on its own: a lock the child kept would make this wait.
		out, err = boundedLockScript(t, lock, "true").CombinedOutput()
		require.NoError(t, err, "the next run, with the leftover child still alive: %s", out)

		require.NoError(t, syscall.Kill(pid, 0),
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
			cmd.Env = scriptEnv("HOME="+home, "XDG_CACHE_HOME="+tc.xdg)

			out, err := cmd.CombinedOutput()
			assert.NoError(t, err, "%s: the command did not find %s held while it ran: %s", name, tc.want, out)
		}
	})

	// No place for the shared lock is a courtesy lost, not a gate: with neither
	// an absolute XDG_CACHE_HOME nor HOME, the command still runs, its status is
	// the script's, and the run says why it is unlocked. A run that went on
	// without HOME would lock /.cache/dev-gate.lock, which only root can make.
	t.Run("RunsWithNoHome", func(t *testing.T) {
		t.Parallel()

		cmd := lockScript(t, "", "sh", "-c", "exit 6")
		cmd.Env = nil
		for _, kv := range scriptEnv() {
			if name, _, _ := strings.Cut(kv, "="); name != "HOME" && name != "XDG_CACHE_HOME" {
				cmd.Env = append(cmd.Env, kv)
			}
		}

		out, err := cmd.CombinedOutput()
		assert.Equal(t, 6, exitCode(t, err), "the command's own status; output: %s", out)
		assert.Contains(t, string(out), "nor HOME is set, so this run is not serialised with others")
	})

	// `make verify` and `make verify-ci` hold the shared lock, at the path whole,
	// while their steps run. MAKE, and so GATE_SUBMAKE, is a helper that asks the
	// lock tool whether the expected path is held and fails if it is not, so the
	// recipe runs the real script around the helper instead of the gate: this is
	// the Makefile's own line, with an XDG_CACHE_HOME holding spaces and none of
	// the caller's make settings. HOME is temporary too, so that a lock path
	// taken by mistake is never the machine's shared one.
	t.Run("MakeVerifyHoldsTheSharedLock", func(t *testing.T) {
		t.Parallel()
		probe := requireLockTool(t)
		requireMake(t)

		dir := t.TempDir()
		xdg := filepath.Join(dir, "a cache")
		lock := filepath.Join(xdg, "dev-gate.lock")

		var quoted []string
		for _, arg := range probe(lock) {
			quoted = append(quoted, shellQuote(arg))
		}
		helper := filepath.Join(dir, "submake")
		require.NoError(t, forkSafeWriteFile(helper, []byte("#!/bin/sh\n"+strings.Join(quoted, " ")+
			"\n[ \"$?\" -eq 75 ] || { echo \"the shared lock was not held\" >&2; exit 7; }\necho \"held: $*\"\n"), 0o700))

		for _, target := range []string{"verify", "verify-ci"} {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			cmd := exec.CommandContext(ctx, "make", "-C", "..", "--no-print-directory", "MAKE=sh "+shellQuote(helper), target)
			cmd.Env = makeEnv("HOME="+filepath.Join(dir, "home"), "XDG_CACHE_HOME="+xdg)
			inGroup(cmd)

			out, err := cmd.CombinedOutput()
			cancel()
			require.NoError(t, err, "make %s: %s", target, out)
			assert.Contains(t, string(out), "held: --no-print-directory "+target+"-unlocked",
				"make %s did not run its steps' target holding the shared lock", target)
		}
	})

	// A dry run takes no lock: `make -n verify` waits for nothing while another
	// gate holds the lock. When the Makefile can see the -n it lists the steps.
	// GNU Make 3.81 keeps an inherited MAKEFLAGS=e and hides the command line's
	// -n from the Makefile, which then chooses the locked recipe; that recipe
	// reaches make through GATE_SUBMAKE, which make does not treat as recursive,
	// so it is printed and never run. (GNU Make 4.x shows the Makefile the -n in
	// both cases.)
	t.Run("ADryRunTakesNoLock", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)
		requireMake(t)

		dir := t.TempDir()
		lock := filepath.Join(dir, "held.lock")
		startHolder(t, lock, filepath.Join(dir, "trace"), filepath.Join(dir, "release"))

		for name, tc := range map[string]struct {
			env        []string
			listsSteps bool
		}{
			"seen by the Makefile":     {listsSteps: true},
			"hidden from the Makefile": {env: []string{"MAKEFLAGS=e"}},
		} {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			cmd := exec.CommandContext(ctx, "make", "-C", "..", "-n", "verify", "GATE_LOCK="+lock)
			cmd.Env = makeEnv(tc.env...)
			// Killing make alone would leave the script and the lock tool, its
			// grandchildren, waiting for the lock and starting the steps once the
			// holder is released.
			inGroup(cmd)

			out, err := cmd.CombinedOutput()
			cancel()
			require.NoError(t, err, "%s: make -n verify while the lock is held: %s", name, out)
			assert.NotContains(t, string(out), "waiting", "%s: make -n verify waited for the lock", name)
			if tc.listsSteps {
				assert.Contains(t, string(out), "scripts/for-each-module.sh go test -race",
					"%s: make -n verify did not list the steps", name)
			}
		}
	})

	// A real run takes the lock whatever is on the command line, and hands the
	// command line's overrides to the steps. With MAKE="make -n" the recipe runs
	// the real script around a sub-make that prints its steps instead of running
	// them, so what is asserted is the race test line the steps would run: the
	// overrides reach it locally, and under CI the Makefile forces both caps empty
	// whatever the command line says. GATE_LOCK comes last and holds a "t": on
	// GNU Make 3.81 a command line of only assignments puts the last of them first
	// in MAKEFLAGS, with no flags. Run from the root rather than with -C, which
	// adds its own flag to MAKEFLAGS and would hide that shape.
	t.Run("ARealRunWithOverridesTakesTheLock", func(t *testing.T) {
		t.Parallel()
		requireLockTool(t)
		requireMake(t)

		goTest := regexp.MustCompile(`(?m)^(.*)scripts/for-each-module\.sh go test -race(.*) \./\.\.\.$`)
		for name, tc := range map[string]struct{ ci, nice, parallel string }{
			"local": {ci: "", nice: "nice -n 10 ", parallel: " -p 2"},
			"CI":    {ci: "true", nice: "", parallel: ""},
		} {
			lock := filepath.Join(t.TempDir(), "tmp", "override.lock")

			ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
			cmd := exec.CommandContext(ctx, "make", "NICE=nice -n 10", "TEST_PARALLEL=2", "MAKE=make -n", "GATE_LOCK="+lock, "verify")
			cmd.Dir = ".."
			cmd.Env = makeEnv("CI=" + tc.ci)
			inGroup(cmd)

			out, err := cmd.CombinedOutput()
			cancel()
			require.NoError(t, err, "%s: make verify with overrides: %s", name, out)
			assert.FileExists(t, lock, "%s: make verify with overrides on the command line did not take the lock", name)

			m := goTest.FindStringSubmatch(string(out))
			require.NotNil(t, m, "%s: the sub-make printed no race test line: %s", name, out)
			assert.Equal(t, tc.nice, m[1], "%s: what runs the race tests, in %q", name, m[0])
			assert.Equal(t, tc.parallel, m[2], "%s: the race tests' -p, in %q", name, m[0])
		}
	})

	// A lock that cannot be made is a courtesy lost, not a gate: the command
	// still runs, its status is still the script's, and the run says it is
	// unlocked.
	t.Run("RunsWhenTheLockCannotBeMade", func(t *testing.T) {
		t.Parallel()

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

		err := lockScript(t, filepath.Join(t.TempDir(), "gate.lock")).Run()
		assert.Equal(t, 2, exitCode(t, err))
	})
}

// TestFakeLockfProcess is not a test of its own. Run with FAKE_LOCKF=1 it is a
// lock tool shaped like macOS lockf (`lockf [-k] [-t secs] file command...`)
// that locks nothing, runs the command as its child and, as lockf does, exits
// 70 when that child was ended by a signal and with its status otherwise.
func TestFakeLockfProcess(t *testing.T) {
	if os.Getenv("FAKE_LOCKF") != "1" {
		t.Skip("the stand-in lockf, run only by TestWithCheckLock/ASignalledCommandIsNotReportedAsTheLockToolsFailure")
	}

	args := os.Args
	for i, arg := range args {
		if arg == "--" {
			args = args[i+1:]
			break
		}
	}
	for len(args) > 0 && strings.HasPrefix(args[0], "-") {
		if args[0] == "-t" {
			args = args[1:]
		}
		args = args[1:]
	}
	if len(args) < 2 {
		fmt.Fprintln(os.Stderr, "fake lockf: usage: lockf [-k] [-t secs] file command...")
		os.Exit(64)
	}

	child := exec.CommandContext(t.Context(), args[1], args[2:]...)
	child.Stdin, child.Stdout, child.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := child.Run()
	if err == nil {
		os.Exit(0)
	}
	// A child that never started has no state to read: that is the diagnostic
	// and 71, as lockf answers a command it cannot run.
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		fmt.Fprintln(os.Stderr, "fake lockf:", err)
		os.Exit(71)
	}
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		os.Exit(70)
	}
	os.Exit(exitErr.ExitCode())
}
