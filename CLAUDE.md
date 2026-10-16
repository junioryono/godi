# godi

A Go dependency-injection library: the core module at the root, one module per web-framework integration (`http`, `chi`, `echo`, `fiber`, `gin`, `huma`), and the unreleased `integrationtests` and `benchmarks` modules. `scripts/modules.txt` lists them. [CONTRIBUTING.md](CONTRIBUTING.md) holds the layout, the commands, the PR conventions (Conventional Commit titles) and the release process.

## Check what you changed; CI runs the rest

Verify what you changed, locally and narrowly; CI runs the full gate on every push. Full gates run again and again from several sessions on one shared Mac are what starved it into a kernel panic on 2026-10-15, and CI runs them anyway.

- Run `make verify`. It reads the change against the merge base with `origin/main` (plus staged, unstaged and untracked files) and runs gofmt on the changed files, the build, and `go vet`, `go test` without `-race` (`GOMAXPROCS=4`, `-p 2`) and golangci-lint on the changed packages of each module touched.
- For a new or changed test, run it by name: `go test -run 'TestName/subtest' ./pkg`. `-race` is fine on one package with `-run`.
- Lint packages, never files, always with `--allow-serial-runners`: `golangci-lint run --allow-serial-runners ./internal/graph`. A file alone is type-checked without the rest of its package.
- Do not run locally: `make verify-full`, `make verify-ci`, `make test`, `make lint` or the other whole-tree targets, `go test ./...` at a module root, `-race` across several packages, or golangci-lint over a whole module. Read CI instead: `gh pr checks <n> --watch`, then `gh run view <id> --log-failed`.
- A hook in `.claude/settings.json` (`.claude/hooks/guard.py`) refuses those commands. `CLAUDE_FULL_CHECKS=1` in the environment Claude Code is started with lifts it, for a person who wants the full gate here; writing it in front of a command does not.
- Subagents follow the same rules: they read this file, not the conversation that started them.

## Worktrees

Make worktrees directly under the main checkout's `.claude/worktrees/`, never beside the repository in `../`, never inside another worktree and never in `/tmp`: from the main checkout, `git worktree add .claude/worktrees/<name> -b <branch> origin/main`. The hook refuses any other place.

## Finding code

Search with grep or ripgrep. When exact navigation matters, run gopls once for the answer instead of keeping a language server running (the per-session gopls plugin is off in `.claude/settings.json`; each session's gopls held 0.3 to 2.6 GB):

```bash
gopls definition scope.go:242:24
gopls references scope.go:242:24
gopls implementation lifecycle.go:18:6
gopls check scope.go
```
