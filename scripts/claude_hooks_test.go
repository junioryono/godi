//go:build unix

package scripts_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requirePython skips where python3 is not installed, except under CI, where the
// hook would go untested.
func requirePython(t *testing.T) {
	t.Helper()

	if lookPath("python3") {
		return
	}
	if os.Getenv("CI") != "" {
		t.Fatal("python3 is not installed under CI, so the Claude Code hook would go untested")
	}
	t.Skip("python3 is not installed")
}

// runGuard runs .claude/hooks/guard.py with payload on stdin, in an environment
// without CI or CLAUDE_FULL_CHECKS unless env sets them, and returns its exit
// status and what it printed to stderr.
func runGuard(t *testing.T, payload []byte, env ...string) (status int, stderr string) {
	t.Helper()

	cmd := exec.CommandContext(t.Context(), "python3", filepath.Join("..", ".claude", "hooks", "guard.py"))
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); name != "CI" && name != "CLAUDE_FULL_CHECKS" {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	cmd.Env = append(cmd.Env, env...)
	cmd.Stdin = bytes.NewReader(payload)
	var said bytes.Buffer
	cmd.Stderr = &said

	err := cmd.Run()
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return 0, said.String()
	case errors.As(err, &exitErr):
		return exitErr.ExitCode(), said.String()
	default:
		t.Fatalf("run the hook: %v", err)
		return 0, ""
	}
}

// bashPayload is the PreToolUse input Claude Code hands the hook for a Bash call.
func bashPayload(t *testing.T, command, cwd string) []byte {
	t.Helper()

	payload, err := json.Marshal(map[string]any{
		"hook_event_name": "PreToolUse",
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": command},
		"cwd":             cwd,
	})
	require.NoError(t, err)
	return payload
}

// TestClaudeGuardHook runs the committed PreToolUse hook on the command lines it
// must refuse and on those it must let through, in the checkout itself, so the
// module roots it reads are godi's own.
func TestClaudeGuardHook(t *testing.T) {
	t.Parallel()
	requirePython(t)

	root, err := filepath.Abs("..")
	require.NoError(t, err)

	const gate, worktree = "make verify", ".claude/worktrees/"
	// checkout is a main checkout's path for the worktree cases: this test may
	// itself run in a worktree under .claude/worktrees/, where ../x is allowed.
	const checkout = "/srv/godi"

	for _, tc := range []struct {
		name    string
		command string
		dir     string   // relative to the checkout's root, or absolute
		env     []string // the hook's own environment
		refuses string   // what the refusal says; empty when the command passes
	}{
		// The full gate and the whole-tree make targets.
		{name: "full gate", command: "make verify-full", refuses: gate},
		{name: "full gate without the lock", command: "make verify-full-unlocked", refuses: gate},
		{name: "CI gate", command: "make verify-ci", refuses: gate},
		{name: "race tests of every module", command: "make test", refuses: gate},
		{name: "lint of every module", command: "make lint", refuses: gate},
		{name: "gate with jobs and a directory", command: "make -j 4 -C . verify-full", refuses: gate},
		{name: "gate behind an assignment", command: "make TEST_PARALLEL=2 verify-ci", refuses: gate},
		{name: "gate in a later command", command: "git status && make verify-full", refuses: gate},
		{name: "gate with its output redirected", command: "make verify-full > gate.log 2>&1", refuses: gate},
		{name: "a prefix does not lift the refusal", command: "CLAUDE_FULL_CHECKS=1 make verify-full", refuses: gate},
		{name: "env does not lift the refusal", command: "env CLAUDE_FULL_CHECKS=1 go test ./...", refuses: gate},

		// go test over a module, or with -race across packages.
		{name: "every package of the root module", command: "go test ./...", refuses: gate},
		{name: "every package with the race detector", command: "go test -race ./...", refuses: gate},
		{name: "every package behind wrappers", command: "GOMAXPROCS=4 nice -n 10 go test -count=1 ./...", refuses: gate},
		{name: "an integration module from its root", command: "cd http && go test ./...", refuses: gate},
		{name: "an integration module through -C", command: "go -C chi test ./...", refuses: gate},
		{name: "an integration module in a subshell", command: "(cd echo && go test -race -count=1 ./...)", refuses: gate},
		{name: "the all pattern", command: "go test all", refuses: gate},
		{name: "a module by import path", command: "go test github.com/junioryono/godi/v6/...", refuses: gate},
		{name: "an integration module by import path", command: "go test github.com/junioryono/godi/gin/v6/...", refuses: gate},
		{name: "race across two packages", command: "go test -race . ./internal/graph", refuses: gate},
		{name: "race across a pattern", command: "go test -race ./internal/...", refuses: gate},
		{name: "inside sh -c", command: "bash -c 'go test ./...'", refuses: gate},
		{name: "every module's tests", command: "nice -n 10 scripts/for-each-module.sh go test -race ./...", refuses: gate},

		// golangci-lint over a module, on a file, or without its lock.
		{name: "lint with no package", command: "golangci-lint run", refuses: gate},
		{name: "lint of the root module", command: "golangci-lint run --allow-serial-runners ./...", refuses: gate},
		{name: "the pinned lint of a module", command: "cd fiber && ../.tools/bin/golangci-lint-v2.14.0 run ./...", refuses: gate},
		{name: "every module's lint", command: "scripts/for-each-module.sh golangci-lint run --allow-serial-runners .", refuses: gate},
		{name: "lint of a file", command: "golangci-lint run --allow-serial-runners scope.go", refuses: "never files"},
		{name: "lint without its lock", command: "golangci-lint run --allow-parallel-runners .", refuses: "--allow-serial-runners"},

		// Worktrees outside .claude/worktrees/.
		{name: "worktree beside the repository", command: "git worktree add ../godi-work -b junior-oct15-x origin/main", dir: checkout, refuses: worktree},
		{name: "worktree in /tmp", command: "git worktree add /tmp/godi-work origin/main", refuses: worktree},
		{name: "worktree through -C", command: "git -C .. worktree add elsewhere", dir: checkout + "/http", refuses: worktree},
		{name: "full checks do not lift the worktree rule", command: "git worktree add ../w", dir: checkout, env: []string{"CLAUDE_FULL_CHECKS=1"}, refuses: worktree},

		// What a session runs while working.
		{name: "the targeted check", command: "make verify"},
		{name: "the default goal", command: "make"},
		{name: "a dry run of the full gate", command: "make -n verify-full"},
		{name: "the docs", command: "make docs"},
		{name: "the root package", command: "go test ."},
		{name: "one package with the race detector", command: "go test -race -run 'TestScope/closes' ."},
		{name: "a subtree without the race detector", command: "go test ./internal/..."},
		{name: "two packages", command: "GOMAXPROCS=4 go test -p 2 -count=1 ./internal/graph ./internal/reflection"},
		{name: "every package below a module root", command: "go test ./...", dir: "internal"},
		{name: "an integration module's package", command: "go -C http test -run TestScope ."},
		{name: "build", command: "go build ./..."},
		{name: "vet", command: "go vet ./..."},
		{name: "lint of packages", command: "golangci-lint run --allow-serial-runners . ./internal/graph"},
		{name: "the lint version", command: "golangci-lint version"},
		{name: "a worktree under .claude/worktrees", command: "git worktree add .claude/worktrees/x -b junior-oct15-x origin/main", dir: checkout},
		{name: "a worktree in a scratchpad", command: "git worktree add /private/tmp/claude-501/p/s/scratchpad/wt origin/main"},
		{name: "the worktree list", command: "git worktree list"},
		{name: "a gate named in an argument", command: "echo 'go test ./...' && grep -n 'make verify-full' Makefile"},
		{name: "a gate in a heredoc", command: "cat > notes.txt <<'EOF'\ngo test -race ./...\nmake verify-full\nEOF"},
		{name: "a gate in a comment", command: "go build ./... # then go test ./..."},
		{name: "a line it cannot parse", command: "go test \"./..."},

		// The hook's own environment.
		{name: "full checks lift the gate", command: "make verify-full", env: []string{"CLAUDE_FULL_CHECKS=1"}},
		{name: "full checks lift go test", command: "go test -race ./...", env: []string{"CLAUDE_FULL_CHECKS=1"}},
		{name: "full checks must be 1", command: "make verify-full", env: []string{"CLAUDE_FULL_CHECKS=true"}, refuses: gate},
		{name: "CI is exempt from the gates", command: "make verify-ci", env: []string{"CI=true"}},
		{name: "CI is exempt from the worktree rule", command: "git worktree add ../w", dir: checkout, env: []string{"CI=true"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := tc.dir
			if !filepath.IsAbs(dir) {
				dir = filepath.Join(root, dir)
			}
			status, stderr := runGuard(t, bashPayload(t, tc.command, dir), tc.env...)
			if tc.refuses == "" {
				assert.Equal(t, 0, status, "%q must pass; the hook said: %s", tc.command, stderr)
				return
			}
			assert.Equal(t, 2, status, "%q must be refused with exit 2; the hook said: %s", tc.command, stderr)
			assert.Contains(t, stderr, tc.refuses, "%q: the refusal must say what to do instead", tc.command)
		})
	}

	// What is not a Bash call the hook can read passes.
	for name, payload := range map[string]string{
		"not JSON":          "{",
		"not an object":     "[]",
		"no command":        `{"tool_name":"Bash","tool_input":{},"cwd":"/"}`,
		"a command no text": `{"tool_name":"Bash","tool_input":{"command":7},"cwd":"/"}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			status, stderr := runGuard(t, []byte(payload))
			assert.Equal(t, 0, status, "the hook must let through what it cannot read; it said: %s", stderr)
		})
	}
}

// TestClaudeSettings checks the committed settings wire the hook and turn the
// per-session gopls plugin off.
func TestClaudeSettings(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("..", ".claude", "settings.json"))
	require.NoError(t, err)
	var settings struct {
		EnabledPlugins map[string]bool `json:"enabledPlugins"`
		Hooks          map[string][]struct {
			Matcher string `json:"matcher"`
			Hooks   []struct {
				Command string `json:"command"`
			} `json:"hooks"`
		} `json:"hooks"`
	}
	require.NoError(t, json.Unmarshal(raw, &settings))

	enabled, set := settings.EnabledPlugins["gopls-lsp@claude-plugins-official"]
	assert.True(t, set && !enabled, "the gopls plugin must be set to false")

	var wired bool
	for _, entry := range settings.Hooks["PreToolUse"] {
		for _, hook := range entry.Hooks {
			if entry.Matcher == "Bash" && strings.Contains(hook.Command, ".claude/hooks/guard.py") {
				wired = true
			}
		}
	}
	assert.True(t, wired, "a PreToolUse hook on Bash must run .claude/hooks/guard.py")
}
