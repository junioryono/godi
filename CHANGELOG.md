# Changelog

Notable changes to godi. Release notes for each version are also generated from
commit messages on GitHub.

## v6.1.0 (2026-10-05)

### Upgrade notes

- **`WithScopeValidation` and `ErrScopeRequired` are removed; the root provider
  is always a scope**, as in v5 and in Microsoft.Extensions.DependencyInjection
  without `ValidateScopes`. A scoped service resolved from the provider (or
  through a singleton's injected `Resolver`) is the root scope's instance,
  cached until the provider closes, and the root scope runs scoped
  initializers during `Build`, so a failing scoped initializer fails `Build`.
  Delete `WithScopeValidation(...)` from `Build` calls. Removing an API in a
  minor release is deliberate: v6.0.0 was a day old with no importers outside
  its author's projects.
- Singletons still cannot depend on scoped services, directly or through
  transients: `Build` rejects that whatever the scope.

## v6.0.0 (2026-10-04)

A major release at the import path `github.com/junioryono/godi/v6`. See the
[v5 → v6 guide](docs/guides/v5-to-v6.md) for before/after examples.

### Upgrade notes (behavior changes)

- **Values registered as instances (`AddSingleton(value)`, `godi.Instance`)
  are no longer disposed**: the caller that created them closes them. This is
  the one silent change. Register a constructor that returns the value to hand
  it to godi.
- **Scope validation is on by default**: resolving a scoped service from the
  root provider fails with `ErrScopeRequired`, and the root scope runs no
  scoped initializers. `WithScopeValidation(false)` opts out.
- **Constructors receive `godi.Resolver` and the new `godi.ScopeFactory`**;
  `Provider` and `Scope` parameters are rejected at registration, and
  `Scope.Provider()` is removed. An injected `Resolver` refuses `Provider` and
  `Scope`; a constructor's context carries no scope; an injected
  `ScopeFactory` cannot create scopes until its constructor returns.
- **`godi.Start` skips instances**, which godi neither starts nor disposes.
- **An empty name names no service** in `ContainsKeyed` and `RemoveKeyed`.
- **`godi.Name` keys every output** of a multi-return constructor.
- **Error types have pointer receivers**; a value-typed `errors.As` target
  panics.
- **`echo` and `fiber` integrate Echo v5 and Fiber v3** (formerly `echov5`
  and `fiberv3`); the Echo v4 and Fiber v2 integrations are retired.

### Changed

- One `Build(opts ...BuildOption)` with `WithContext`, `WithBuildTimeout`,
  `WithObserver` and `WithScopeValidation` replaces `BuildWithContext`,
  `BuildWithOptions` and `ProviderOptions`.
- Names are the only service keys: `godi.Key` is removed, and keyed APIs and
  report fields use `string` names. `ErrServiceKeyNil` is `ErrServiceKeyEmpty`.
- `Collection`, `Provider` and `Scope` are sealed. `Invoke`, `IsService` and
  `IsKeyedService` take a `Resolver`.
- `BuildError.Phase` is a `BuildPhase` and `DisposalError.Context` a
  `DisposalContext`.

### Removed

- `TimeoutError`, `GraphOperationError`, `ProviderOptions`, `godi.Key`,
  `Scope.Provider()`, `ResolutionError.ServiceNotFound()`.
- Exported internals: `TypeKey`, `GroupKey`, `ErrDescriptorNil`,
  `ErrSingletonNotInitialized`, `ReflectionAnalysisError`.

### Internal

- The outputs of one constructor share a registration record, which is their
  construction identity.
- Dead code removed from `internal/graph` and `internal/reflection`;
  `internal/reflection` takes the not-found policy as an option instead of
  defining `ErrServiceNotFound`.

## v5.2.0 (2026-10-04)

### Upgrade notes (behavior changes)

Review these when upgrading from v5.1:

- **Cancelling a scope's context no longer closes the scope.** v5.1 closed a
  scope from another goroutine when the context passed to `CreateScope` was
  cancelled — for HTTP requests, on client disconnect, disposing transactions
  and files under a handler that was still running. Cancellation is now only a
  signal; the scope's owner closes it (the HTTP integrations `defer
  scope.Close()`). Code that relied on cancellation to release scopes must
  close them, e.g. `context.AfterFunc(ctx, func() { _ = scope.Close() })`;
  unclosed scopes stay alive until the provider closes.
- **Missing dependencies fail `Build` for every lifetime**, not only for
  singletons (`MissingDependencyError`, matching `ErrServiceNotFound`).
- **Transients resolved directly from the provider are owned by the caller**
  and no longer retained until shutdown. Transients a singleton (or a
  root-level scoped service) depends on are still disposed by the provider.
- **A scope no longer disposes values it only borrows** (e.g. a scoped service
  returning a singleton).
- **Resources implementing only `Shutdown(ctx)` or `Close(ctx)` are now
  disposed.** Mark values the application shuts down itself with
  `godi.NoDispose()`.
- **Transient → scoped dependencies are allowed**; a singleton capturing a
  scoped service through transients is still rejected.
- **Error messages changed**: package-qualified types, constructor names and
  locations, one line per error with detail in `godi.Explain`; a construction
  failure reads `failed to resolve …` instead of `service not found …`. Match
  errors with `errors.Is`/`errors.As`, not message text.
- **`MustResolve*` panic with an `error`** (wrapping the cause) instead of a
  `string`.
- **`Resolve`/`ResolveKeyed`/`ResolveGroup` take a `godi.Resolver`** (every
  `Provider` and `Scope` is one).
- Previously broken registrations are now rejected: a struct error return,
  `(error, error)`, an `In` field with both `name` and `group`, nil instance
  values, and reserved (`context.Context`, `Provider`, `Scope`), channel,
  unsafe-pointer or error types as a later return value or a `godi.Out`
  field.
- Integrations log scope-creation and middleware failures (they were silent)
  and include stacks in panic logs; responses are unchanged. `godichi` types
  are aliases of `godihttp`'s.

### Added

- `godi.Shutdown(ctx, …)`, `ContextCloser`, `Shutdowner`, `NoDispose`.
- `ProviderOptions.ValidateScopes`, `ProviderOptions.Context`,
  `ProviderOptions.Observer`.
- `Decorate`, `ReplaceSingleton/Scoped/Transient`,
  `TryAddSingleton/Scoped/Transient`, `Validate`, `Lazy`, `Invoke`,
  `IsService`, `IsKeyedService`, `Start`/`Starter`,
  `HealthCheck`/`HealthChecker`.
- `Explain`, `Describe`/`ServiceDescription`, `WriteDOT`, build phase
  constants.
- `Resolver`, `ResolveFromContext`, `Instance`, `Key`.
- Integrations for Echo v5 (`echov5`) and Fiber v3 (`fiberv3`);
  `WithLogger`, `WithMiddlewareErrorHandler`, `WithErrorPassthrough`.

### Fixed

- Runtime dependency cycles through an injected `Scope`, `Provider` or
  `context.Context` (including across goroutines) report a
  `CircularDependencyError` instead of deadlocking or overflowing the stack.
- Disposal order, ownership and leaks across singletons, scopes, decorators,
  result objects and removed registrations; nil `godi.Out` fields; build order
  is deterministic. See the pull requests linked from #61 for details.

### Deprecated

- `TimeoutError` (never returned), `TypeKey`, `GroupKey`. See #61 for the v6
  plan.
