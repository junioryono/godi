# Contributing to godi

Contributions should keep the public API small, preserve lifecycle guarantees, and include tests for behavioral changes.

## Development Setup

Requirements:

- A supported Go release (see [Go Version Policy](#go-version-policy)); CI's primary toolchain is pinned in [`.go-version`](.go-version)
- Python 3.13 for the documentation build
- GNU Make, Bash, and `zip` (used by `make published-check`)

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
[`scripts/modules.txt`](scripts/modules.txt); Make, the release scripts, and CI
read that file rather than maintaining separate module lists. Adding a module is
a one-line change there plus its Dependabot entry (`make dependency-check`
enforces the match).

## Repository Layout

The root package `godi` has one file per concept:

| File | Contents |
| --- | --- |
| `collection.go` | `Collection`, `Build`, `Validate`, build-time validation |
| `module.go` | `ModuleOption`, `NewModule`, `Add*`, `Remove*`, `Replace*`, `TryAdd*` |
| `options.go` | registration options: `Name`, `Key`, `Group`, `As`, `Lazy`, `NoDispose`, `Instance` |
| `decorate.go` | `Decorate` |
| `provider.go`, `scope.go` | the built container: `Provider`, `ProviderOptions`, `Scope`, `FromContext` |
| `resolve.go` | `Resolver`, `Resolve*`, `MustResolve*`, `ResolveFromContext`, `Invoke`, `IsService` |
| `lifecycle.go` | disposal (`Disposable`, `ContextCloser`, `Shutdowner`, `Shutdown`), `Start`, `HealthCheck` |
| `errors.go` | error types and `Explain` |
| `observer.go` | `Observer` and its events |
| `describe.go` | `Describe`, `WriteDOT` |
| `descriptor.go`, `lifetime.go`, `inout.go` | registration descriptors, `Lifetime`, `In`/`Out` |

`internal/reflection` analyzes constructors and `internal/graph` holds the
dependency graph. Each integration (`http`, `chi`, `echo`, `fiber`, `gin`,
`huma`) is its own module.

Two modules are never released:

- `integrationtests` holds tests that need several integrations at once. It
  is a separate module so that no integration's `go.mod` depends on the
  others' routers.
- `benchmarks` compares godi with other DI libraries, keeping their
  dependencies out of the root module.

## Verification Commands

```bash
make verify           # inventory, release floors, formatting, tidy, build, vet, race tests, lint
make verify-ci        # reproduce all required CI checks, including slower checks
make test-cover       # enforce the coverage floor for all runtime modules
make docs             # build Sphinx docs with warnings treated as errors
make published-check  # test the candidate release set without local replace directives
make security         # run pinned gosec and govulncheck tools
make vulncheck        # run only govulncheck across every module
make benchmark        # emit raw results plus reproducibility metadata
make tool-updates     # report pinned tools and the Go toolchain with newer releases
```

Run `make verify` during development. Before opening or updating a pull request, run `make verify-ci`; it composes the same coverage, docs, candidate-release, and security targets required by CI. CI additionally runs root tests on Linux, macOS, and Windows, and module tests on every supported Go release.

## Making Changes

- Use `gofmt` and standard Go conventions.
- Add focused tests for new behavior, failure paths, and concurrency where relevant.
- Keep test files paired with the source file they cover: tests for `scope.go` belong in `scope_test.go`. Place a test next to the file that implements the behavior rather than creating a new test file named after a theme. Fixtures live with the area they model: the service fixtures (`TService`, …) in `collection_test.go`, `TDisposable` in `lifecycle_test.go`, `BuildProvider` in `provider_test.go`. The exceptions are cross-cutting suites: `model_test.go` (model-based invariants), `fuzz_registration_test.go`, `example_test.go`, `benchmark_test.go`, and `main_test.go` (goroutine-leak checks).
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

Useful scopes include core packages (`provider`, `collection`, `module`, `lifetime`, `descriptor`, `errors`, `inout`, `scope`, `resolver`), repository concerns (`deps`, `docs`, `benchmarks`, `release`, `security`), and integrations (`http`, `chi`, `echo`, `echov5`, `fiber`, `fiberv3`, `gin`, `huma`, or `integrations` for changes that span several).

Examples:

```text
feat(provider): add constructor diagnostics
fix(gin): close request scope after panic
docs: clarify keyed registrations
ci(release): make tag publication atomic
```

PR descriptions should explain the behavior change, motivation, related issue, and verification performed. Breaking changes must use `!` in the title or a `BREAKING CHANGE:` footer.

## Go Version Policy

godi supports the two most recent Go minor releases, matching the
[Go release policy](https://go.dev/doc/devel/release#policy). Use the latest
patch release of either; older patches may carry standard-library
vulnerabilities.

- The `go` directive in every `go.mod` names the oldest supported minor
  (`make module-check` keeps all modules on the same directive).
- [`.go-version`](.go-version) pins the newest stable Go release. Every
  single-toolchain CI job uses it.
- Module tests run on both: the oldest supported minor at its latest patch and
  the pinned newest release ([`scripts/go-matrix.sh`](scripts/go-matrix.sh)).

When a new Go minor is released, bump `.go-version`. Once the oldest minor
leaves Go's support window, raise the `go` directive of every module in its own
pull request and call it out in the release notes, because it raises the
minimum Go version for users.

## Release Process

Releases are maintainer-operated. The exact target version is entered manually;
Conventional Commit titles organize release notes but do not choose the version
automatically. The target must be the next patch, minor, or major version.

All released modules (the core and every `integration` in
`scripts/modules.txt`) are tagged together at one commit with one version, and
every module requires the godi modules released with it: an integration tagged
`vX.Y.Z` requires core `vX.Y.Z`. Upgrading any integration therefore upgrades
the core to at least the same release, and an integration may use a core API
added in the same release.

1. **Prepare.** On a branch from `main`, run:

   ```bash
   make prepare-release VERSION=vX.Y.Z
   make verify
   ```

   `scripts/prepare-release.sh` raises every `require github.com/junioryono/godi/...`
   line in every module (the core floor of each integration, and the
   integration requirements of the test module) to `vX.Y.Z` and runs
   `go mod tidy`. That version is not published yet; local builds still work
   because each godi requirement is paired with a directory `replace`, which
   needs neither a download nor a `go.sum` entry. Open a
   `chore(release): prepare vX.Y.Z` pull request and merge it. Its CI includes
   the **Candidate Release Set** job (`make published-check`): it serves this
   commit's modules at `vX.Y.Z` from a local module proxy and tests the
   integration and test modules with every `replace` removed, exactly as
   consumers will resolve the tags.
2. **Tag.** Run the `Tag` workflow on `main` with version `vX.Y.Z`. It refuses
   to tag unless every release floor equals `vX.Y.Z`
   (`scripts/check-release-floors.sh`), runs the complete test workflow,
   validates module paths, and pushes the root and integration tags
   atomically. A major release is rejected until all module paths have been
   migrated to the new major suffix, and it requires explicit `MAJOR`
   confirmation.
3. **Publish.** The `Tag` workflow then creates the GitHub release
   (`release.yml`, which validates the tag set and its floors again) and runs
   the **Release Smoke Test** (`release-smoke.yml`): a clean module with no
   `replace` directives requires every released module at `vX.Y.Z`, checks
   that exactly that version of each is selected, and builds and runs a small
   program. It retries while proxy.golang.org catches up. To re-check a
   release, dispatch `Release Smoke Test` (optionally with `goproxy: direct`)
   or run `make release-smoke VERSION=vX.Y.Z`.

Dry runs:

```bash
make floor-check                          # floors agree with each other
scripts/check-release-floors.sh vX.Y.Z    # floors equal vX.Y.Z
GITHUB_REF=refs/heads/main RELEASE_DRY_RUN=true scripts/release-tags.sh vX.Y.Z
```

Do not create release tags or release branches manually. Re-running an already
completed tag workflow is safe when the complete tag set points at the same
commit. Dependabot ignores godi modules; their versions change only through
`prepare-release`.

## CI and Security Automation

| Workflow | Trigger | Purpose |
| --- | --- | --- |
| `Test` | pull requests, `main`, releases | build, vet, and race tests on each supported Go; lint; coverage; docs; candidate release set; gosec; govulncheck |
| `Dependency Review` | pull requests | block dependency changes with high-severity advisories |
| `CodeQL` | pull requests, `main`, weekly | static analysis of every Go module |
| `Scorecard` | `main`, weekly | OpenSSF Scorecard supply-chain checks |
| `Scheduled Security Scan` | weekly | govulncheck of every module on each supported Go; `pip-audit` of `docs/requirements.txt` |
| `Tool Updates` | weekly | opens an issue when a tool pinned in the `Makefile` or `.go-version` has a newer release |
| `Benchmarks` | nightly, manual | raw results plus a `benchstat` comparison with the previous run |
| `Tag`, `Release`, `Release Smoke Test` | manual | the release process above |

Workflows declare least-privilege `permissions` and pin every action to a
commit SHA (`make workflow-check`). Benchmarks do not gate pull requests:
shared runners are too noisy for a pass/fail threshold. Read the nightly run's
job summary, or dispatch the workflow on a branch, and compare
`benchmark-results` artifacts with `benchstat`.

### Tooling Updates

Dependabot updates Go modules, documentation dependencies, and GitHub Actions.
It cannot see the `*_VERSION` tool pins in the `Makefile` or `.go-version`;
`make tool-updates` and the weekly `Tool Updates` workflow report those. Bump
the pin, run `make tools verify`, and open a `chore(deps)` pull request.

Some integration modules build on another one (`chi` is a facade over `http`). Such a module develops against the sibling source through a `replace => ../<module>` directive, and because integrations are tagged in lockstep it must require the sibling at the version being released. Before dispatching the `Tag` workflow for `vX.Y.Z`, merge a commit that runs `go mod edit -require=github.com/junioryono/godi/http/v5@vX.Y.Z` in `chi` (and likewise for any other cross-integration requirement); the workflow refuses to tag otherwise. `scripts/sibling-requires.sh` lists these requirements.

## Reporting Security Issues

Do not disclose suspected vulnerabilities in a public issue. Follow [SECURITY.md](SECURITY.md) to report them privately.

Participation in this project is governed by [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md).
