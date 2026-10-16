//go:build unix

package scripts_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// verifyRepo is a throwaway checkout shaped like godi's: a root module with a
// package and a scripts/ test package, and an integration module in http/,
// committed and recorded as origin/main, with a working branch checked out.
func verifyRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	write := func(rel, body string) { writeRepoFile(t, dir, rel, body) }
	write("go.mod", "module github.com/junioryono/godi/v6\n\ngo 1.26\n")
	write("godi.go", "package godi\n")
	write("internal/graph/graph.go", "package graph\n")
	write("internal/graph/testdata/cycle.txt", "a b\n")
	write("internal/graph/testdata/fixture.go", "package fixture\n")
	write("scripts/scripts_test.go", "package scripts_test\n")
	write("Makefile", "verify:\n")
	write("README.md", "godi\n")
	write("http/go.mod", "module github.com/junioryono/godi/http/v6\n\ngo 1.26\n")
	write("http/http.go", "package http\n")

	verifyGit(t, dir, "init", "-q", "-b", "main")
	verifyGit(t, dir, "add", ".")
	verifyGit(t, dir, "commit", "-q", "-m", "base")
	verifyGit(t, dir, "update-ref", "refs/remotes/origin/main", "HEAD")
	verifyGit(t, dir, "checkout", "-q", "-b", "junior-oct15-work")
	return dir
}

// writeRepoFile writes body to the slash-separated path rel in dir.
func writeRepoFile(t *testing.T, dir, rel, body string) {
	t.Helper()

	path := filepath.Join(dir, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(body), 0o644))
}

func verifyGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(gitlessEnv(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v: %s", args, out)
}

// gitlessEnv is this process's environment without GIT_ variables, which could
// point a fixture's git at another repository.
func gitlessEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			env = append(env, kv)
		}
	}
	return env
}

// runVerify runs verify-changed.sh in dir with stand-ins for go and
// golangci-lint, which append each call, its directory and GOMAXPROCS to a log,
// and exit with FAKE_GO_STATUS (go) or 0. It returns the exit status, the
// output and the log.
func runVerify(t *testing.T, dir string, env ...string) (status int, out, calls string) {
	t.Helper()

	log := filepath.Join(t.TempDir(), "calls")
	record := `echo "$(basename "$PWD") $(basename "$0") $* GOMAXPROCS=$GOMAXPROCS" >> "$VERIFY_CALLS"` + "\n"
	path := fakeTools(t, map[string]string{
		"go":            "#!/bin/sh\n" + record + "exit ${FAKE_GO_STATUS:-0}\n",
		"golangci-lint": "#!/bin/sh\n" + record,
	})
	script, err := filepath.Abs("verify-changed.sh")
	require.NoError(t, err)

	cmd := exec.CommandContext(t.Context(), script)
	cmd.Dir = dir
	cmd.Env = append(gitlessEnv(), path, "VERIFY_CALLS="+log, "GOLANGCI_LINT=", "GOMAXPROCS=")
	cmd.Env = append(cmd.Env, env...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		status = exitCode(t, err)
	}
	logged, readErr := os.ReadFile(log)
	if readErr != nil && !os.IsNotExist(readErr) {
		require.NoError(t, readErr)
	}
	return status, string(output), string(logged)
}

func TestVerifyChanged(t *testing.T) {
	t.Parallel()
	if !lookPath("gofmt") {
		t.Skip("gofmt is not installed")
	}

	// What changed in each module is built, and its changed packages are
	// vetted, tested without -race at -p 2 under GOMAXPROCS=4, and linted with
	// --allow-serial-runners, each from its own module's root; nothing else is.
	t.Run("ChecksTheChangedPackagesOfEachModule", func(t *testing.T) {
		t.Parallel()

		dir := verifyRepo(t)
		writeRepoFile(t, dir, "internal/graph/graph.go", "package graph\n\nconst A = 1\n")
		writeRepoFile(t, dir, "http/new.go", "package http\n") // untracked

		status, out, calls := runVerify(t, dir)
		require.Equal(t, 0, status, out)
		// Modules in the order of their first changed path: http/ sorts first.
		assert.Equal(t, strings.Join([]string{
			"http go build ./... GOMAXPROCS=4",
			"http go vet . GOMAXPROCS=4",
			"http go test -p 2 . GOMAXPROCS=4",
			"http golangci-lint run --allow-serial-runners . GOMAXPROCS=4",
			"root go build ./... GOMAXPROCS=4",
			"root go vet ./internal/graph GOMAXPROCS=4",
			"root go test -p 2 ./internal/graph GOMAXPROCS=4",
			"root golangci-lint run --allow-serial-runners ./internal/graph GOMAXPROCS=4",
		}, "\n")+"\n", strings.ReplaceAll(calls, filepath.Base(dir)+" ", "root "), out)
	})

	// A committed change counts, a testdata file selects the package above it
	// (a Go file there too: the go tool ignores testdata), and the Makefile
	// selects scripts/, whose tests read it.
	t.Run("FollowsCommitsTestdataAndTheMakefile", func(t *testing.T) {
		t.Parallel()

		dir := verifyRepo(t)
		writeRepoFile(t, dir, "internal/graph/testdata/cycle.txt", "b a\n")
		writeRepoFile(t, dir, "internal/graph/testdata/fixture.go", "package fixture\n\nconst A = 1\n")
		verifyGit(t, dir, "commit", "-q", "-am", "work")
		writeRepoFile(t, dir, "Makefile", "verify:\n\t@true\n")

		status, out, calls := runVerify(t, dir)
		require.Equal(t, 0, status, out)
		assert.Contains(t, calls, " go test -p 2 ./scripts ./internal/graph GOMAXPROCS=4", out)
		assert.NotContains(t, calls, "-race")
	})

	// A file only a person reads selects no package.
	t.Run("ADocumentSelectsNothing", func(t *testing.T) {
		t.Parallel()

		dir := verifyRepo(t)
		writeRepoFile(t, dir, "README.md", "godi, again\n")

		status, out, calls := runVerify(t, dir)
		require.Equal(t, 0, status, out)
		assert.Empty(t, calls, out)
	})

	t.Run("NothingChanged", func(t *testing.T) {
		t.Parallel()

		status, out, calls := runVerify(t, verifyRepo(t))
		require.Equal(t, 0, status, out)
		assert.Contains(t, out, "nothing changed")
		assert.Empty(t, calls)
	})

	// A failure in one step fails the run, and the steps after it still run.
	t.Run("AFailingStepFailsTheRun", func(t *testing.T) {
		t.Parallel()

		dir := verifyRepo(t)
		writeRepoFile(t, dir, "godi.go", "package godi\n\nconst A = 1\n")

		status, out, calls := runVerify(t, dir, "FAKE_GO_STATUS=1")
		assert.Equal(t, 1, status, out)
		assert.Contains(t, out, "FAILED: the root module: build or tidy the root module: go vet the root module: go test")
		assert.Contains(t, calls, "golangci-lint run --allow-serial-runners .", "lint still runs after a failing test")
	})

	t.Run("AnUnformattedFileFails", func(t *testing.T) {
		t.Parallel()

		dir := verifyRepo(t)
		writeRepoFile(t, dir, "godi.go", "package godi\nconst  A=1\n")

		status, out, _ := runVerify(t, dir)
		assert.Equal(t, 1, status, out)
		assert.Contains(t, out, "need gofmt -s:\ngodi.go")
	})

	// A merge base that cannot be found is could-not-tell, never "nothing
	// changed".
	t.Run("NoMergeBaseCannotTell", func(t *testing.T) {
		t.Parallel()

		status, out, calls := runVerify(t, verifyRepo(t), "VERIFY_BASE=origin/nowhere")
		assert.Equal(t, 2, status, out)
		assert.Contains(t, out, "could not tell what changed")
		assert.Empty(t, calls)
	})
}
