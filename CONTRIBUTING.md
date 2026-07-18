# Contributing to godi

Contributions should keep the public API small, preserve lifecycle guarantees, and include tests for behavioral changes.

## Development Setup

Requirements:

- Go 1.26.5 or later (older 1.26 patch releases contain known standard-library vulnerabilities)
- Python 3.13 for the documentation build
- GNU Make and Bash

Clone your fork, install the pinned Go development tools, and run the same verification entry point used by CI:

```bash
git clone https://github.com/your-username/godi.git
cd godi
make tools
make verify
```

`make tools` installs version-pinned binaries under `.tools/bin`; Make invokes
them directly, so the Go workspace's `bin` directory does not need to be on
`PATH`.

The repository contains multiple Go modules. Their authoritative list is
[`scripts/modules.txt`](scripts/modules.txt); Make and CI use that file rather than maintaining separate module lists.

## Verification Commands

```bash
make verify           # inventory, formatting, tidy, build, vet, race tests, lint
make verify-ci        # reproduce all required CI checks, including slower checks
make test-cover       # enforce the coverage floor for all runtime modules
make docs             # build Sphinx docs with warnings treated as errors
make published-check  # test integrations without local replace directives
make security         # run pinned gosec and govulncheck tools
make benchmark        # emit raw results plus reproducibility metadata
```

Run `make verify` during development. Before opening or updating a pull request, run `make verify-ci`; it composes the same coverage, docs, published-graph, security, and benchmark targets required by CI. CI additionally runs root tests on Linux, macOS, and Windows.

## Making Changes

- Use `gofmt` and standard Go conventions.
- Add focused tests for new behavior, failure paths, and concurrency where relevant.
- Keep test files paired with the source file they cover: tests for `scope.go` belong in `scope_test.go`. Place a test next to the file that implements the behavior rather than creating a new test file named after a theme. Shared helpers and fixtures live in `testutil_test.go`.
- Update Go documentation and user guides when behavior or public APIs change.
- Keep unrelated refactors out of feature and bug-fix pull requests.
- Do not edit generated benchmark results into the README. CI publishes raw results for comparison with `benchstat`.

Documentation examples under `docs/examples` are compiled as part of the root module. Prefer referencing those examples over duplicating large snippets that can drift.

## Pull Requests

PR titles use Conventional Commit form because the squash-merge title feeds release notes:

```text
type(optional-scope): imperative description
```

Allowed types are `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`.

Useful scopes include core packages (`provider`, `collection`, `module`, `lifetime`, `descriptor`, `errors`, `inout`, `scope`, `resolver`), repository concerns (`deps`, `docs`, `benchmarks`, `release`, `security`), and integrations (`http`, `chi`, `echo`, `fiber`, `gin`, `huma`).

Examples:

```text
feat(provider): add constructor diagnostics
fix(gin): close request scope after panic
docs: clarify keyed registrations
ci(release): make tag publication atomic
```

PR descriptions should explain the behavior change, motivation, related issue, and verification performed. Breaking changes must use `!` in the title or a `BREAKING CHANGE:` footer.

## Release Process

Releases are maintainer-operated through the `Tag` workflow. The exact target version is entered manually; Conventional Commit titles organize release notes but do not choose the version automatically. The target must be the next patch, minor, or major version.

The workflow only accepts `main`, runs the complete reusable test workflow, validates module paths, and pushes the root and integration tags atomically. A major release is rejected until all Go module paths have been migrated to the new major suffix, and it requires explicit `MAJOR` confirmation.

Do not create release tags or release branches manually. Re-running an already completed tag workflow is safe when the complete tag set points at the same commit.

## Reporting Security Issues

Do not disclose suspected vulnerabilities in a public issue. Follow [SECURITY.md](SECURITY.md) to report them privately.

Participation in this project is governed by [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
