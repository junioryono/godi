# Decorators

A decorator wraps a registered service with cross-cutting behavior — logging,
metrics, caching, retries, authorization — without re-registering it or
changing its consumers.

```go
services.AddSingleton(NewPostgresStore, godi.As[Store]())

services.AddModules(
    godi.Decorate(func(next Store, log *slog.Logger) Store {
        return &loggingStore{next: next, log: log}
    }),
)

store := godi.MustResolve[Store](provider) // *loggingStore wrapping *PostgresStore
```

## Rules

- **Target.** The decorator's first parameter selects the decorated service
  type. Add `godi.Name("key")` to decorate a keyed registration, or
  `godi.Group("name")` to decorate every member of a group.
- **Dependencies.** The remaining parameters are injected like constructor
  parameters, and validated at `Build` (missing dependencies, cycles, lifetime
  conflicts).
- **Errors.** A decorator may return `(T, error)`; an error fails the
  resolution.
- **Lifetime.** The decorated service keeps its lifetime: the decorator runs
  once per singleton, once per scope for scoped services, and on every
  resolution of a transient.
- **Order.** Several decorators of one service apply in registration order;
  the first is innermost.
- **Disposal.** A decorator's result that is itself disposable (has `Close`
  or `Shutdown`) owns the value it wraps: godi closes only the outermost
  disposable layer, and that layer should close what it wraps. A wrapper that
  is not disposable leaves the wrapped value to godi. Either way each value is
  closed exactly once.
- **Lifecycle hooks.** `godi.Start` and `godi.HealthCheck` act on the
  constructed service, not on decorators' results.
- **Restrictions.** It is a `Build` (and `Validate`) error if a decorator
  matches no registration, or if it depends — directly or through other
  services — on another output of the decorated service's own constructor,
  which is still being produced when the decorator runs.
- **Root-resolved transients.** A decorated transient resolved directly from
  the provider is owned by the caller, like any such transient: close the
  value you receive, so make wrappers of disposable transients disposable.

## Example: metrics around every handler

```go
services.AddSingleton(NewCreateUser, godi.Group("handlers"))
services.AddSingleton(NewDeleteUser, godi.Group("handlers"))

services.AddModules(
    godi.Decorate(func(next Handler, m *Metrics) Handler {
        return &timedHandler{next: next, metrics: m}
    }, godi.Group("handlers")),
)
```
