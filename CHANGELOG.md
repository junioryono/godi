# Changelog

Notable changes to godi. Release notes for each version are also generated from
commit messages on GitHub.

## Unreleased

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
