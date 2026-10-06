package scripts_test

import (
	"bytes"
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

// requireLockScript skips where with-check-lock.sh cannot run (Windows has no
// sh) or where the machine has neither lock tool, except under CI on Linux or
// macOS, where a skip would report success for a script nothing exercised.
func requireLockScript(t *testing.T, needLockTool bool) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("with-check-lock.sh is a POSIX shell script")
	}

	if !needLockTool {
		return
	}

	for _, tool := range []string{"lockf", "flock"} {
		if _, err := exec.LookPath(tool); err == nil {
			return
		}
	}

	if os.Getenv("CI") != "" {
		t.Fatal("neither lockf nor flock is installed under CI, so with-check-lock.sh would go untested")
	}

	t.Skip("neither lockf nor flock is installed")
}

// exitCode returns the exit status err reports, failing the test if err is not
// a process exiting with one.
func exitCode(t *testing.T, err error) int {
	t.Helper()

	var exitErr *exec.ExitError
	require.True(t, errors.As(err, &exitErr), "want a non-zero exit status, got %v", err)

	return exitErr.ExitCode()
}

func TestWithCheckLock(t *testing.T) {
	t.Parallel()

	// The command's exit status is the script's, so `make verify` failing under
	// the lock still fails.
	t.Run("ReturnsTheCommandsStatus", func(t *testing.T) {
		t.Parallel()
		requireLockScript(t, true)

		err := exec.CommandContext(t.Context(), "./with-check-lock.sh",
			filepath.Join(t.TempDir(), "gate.lock"), "sh", "-c", "exit 3").Run()

		assert.Equal(t, 3, exitCode(t, err))
	})

	// A second run waits for the first, says so, and starts only once the first
	// has finished: the second command copies the file the first writes on its
	// way out, and would see only the first line if the two overlapped.
	t.Run("ASecondRunWaitsForTheFirst", func(t *testing.T) {
		t.Parallel()
		requireLockScript(t, true)

		dir := t.TempDir()
		lock := filepath.Join(dir, "gate.lock")
		trace := filepath.Join(dir, "trace")
		seen := filepath.Join(dir, "seen")

		first := exec.CommandContext(t.Context(), "./with-check-lock.sh", lock,
			"sh", "-c", `echo held > "$1"; sleep 1; echo done >> "$1"`, "first", trace)
		require.NoError(t, first.Start())
		t.Cleanup(func() { _ = first.Wait() })

		// The first run holds the lock once its command has written.
		require.Eventually(t, func() bool {
			_, err := os.Stat(trace)
			return err == nil
		}, 10*time.Second, 20*time.Millisecond, "the first run never started its command")

		var stderr bytes.Buffer
		second := exec.CommandContext(t.Context(), "./with-check-lock.sh", lock,
			"sh", "-c", `cat "$1" > "$2"`, "second", trace, seen)
		second.Stderr = &stderr
		require.NoError(t, second.Run(), "the second run: %s", stderr.String())

		got, err := os.ReadFile(seen)
		require.NoError(t, err)
		assert.Equal(t, "held\ndone\n", string(got),
			"the second run's command must see the first run's whole trace; the two overlapped")

		// One line, the script's own: the probe's lock tool says "already
		// locked" in its own words, and a waiting run should not print it.
		lines := strings.Split(strings.TrimSpace(stderr.String()), "\n")
		if assert.Len(t, lines, 1, "the second run's stderr: %q", stderr.String()) {
			assert.Contains(t, lines[0], "waiting for it to finish")
		}
	})

	// A call without a command is refused rather than run as a lock with
	// nothing in it.
	t.Run("RefusesACallWithoutACommand", func(t *testing.T) {
		t.Parallel()
		requireLockScript(t, false)

		err := exec.CommandContext(t.Context(), "./with-check-lock.sh",
			filepath.Join(t.TempDir(), "gate.lock")).Run()

		assert.Equal(t, 2, exitCode(t, err))
	})
}
