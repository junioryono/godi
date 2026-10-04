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
- **Disposal.** Both the original value and the decorator's result are
  disposed, the decorator's result first.
- **Matching.** It is a `Build` error if a decorator matches no registration.

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
