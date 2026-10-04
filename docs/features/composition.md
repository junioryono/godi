# Composing and Testing Registrations

## Replace

Swap a registration — typically a real dependency for a fake in tests:

```go
services := godi.NewCollection()
services.AddModules(app.Module) // registers the real *sql.DB, Mailer, ...

services.AddModules(
    godi.ReplaceSingleton(newFakeMailer, godi.As[Mailer]()),
    godi.ReplaceSingleton(newTestDB),
)
```

`ReplaceSingleton`, `ReplaceScoped` and `ReplaceTransient` remove the existing
registrations of the new service's type (and name with `godi.Name`, or
interfaces with `godi.As`) and register the replacement. Unlike `Remove`, a
replacement that matches nothing — for example one ordered before the original
registration — is an error, not a silent no-op.

Only the matching outputs are replaced. If the original registration was a
multi-return or `godi.Out` constructor that also provides other services, that
constructor still runs for them.

## TryAdd

Register a default only if the application has not registered one — the usual
pattern for libraries:

```go
var Module = godi.NewModule("mylib",
    godi.TryAddSingleton(defaultClock, godi.As[Clock]()),
    godi.AddSingleton(NewService),
)
```

## Shared modules

A module value is applied to a collection at most once, so modules can include
the modules they depend on:

```go
var Logging = godi.NewModule("logging", godi.AddSingleton(NewLogger))
var Users = godi.NewModule("users", Logging, godi.AddScoped(NewUserService))
var Orders = godi.NewModule("orders", Logging, godi.AddScoped(NewOrderService))

services.AddModules(Users, Orders) // Logging is registered once
```

## Validate without building

`Build` constructs every singleton, which can open databases or start work.
`godi.Validate` runs every check `Build` makes — registration errors, missing
dependencies, cycles, lifetime conflicts, unmatched decorators — without
constructing anything, so a wiring test needs no infrastructure:

```go
func TestWiring(t *testing.T) {
    services := godi.NewCollection()
    services.AddModules(app.Module)
    if err := godi.Validate(services); err != nil {
        t.Fatal(err)
    }
}
```

## Lazy singletons

Singletons are created at `Build`. Mark expensive ones that not every run needs
with `godi.Lazy()` to create them on first resolution instead:

```go
services.AddSingleton(NewSearchIndex, godi.Lazy())
```

A lazy singleton is still validated at `Build`. It is constructed once, even
under concurrent first resolutions; a failed construction is not cached, so the
next resolution retries.

## Invoke

Run a function with its parameters resolved from a provider or scope — for
migrations, route registration, or one-off jobs:

```go
err := godi.Invoke(provider, func(db *sql.DB, log *slog.Logger) error {
    return migrate(db, log)
})
```

The function may take a `godi.In` parameter object and return nothing or an
`error`. Use `godi.IsService(p, t)` and `godi.IsKeyedService(p, t, key)` to
check whether a type can be resolved without constructing anything.

## Start and health checks

Singletons implementing `godi.Starter` (`Start(ctx) error`) are started by
`godi.Start`, in creation order (dependencies first); stop them with
`godi.Shutdown`, which disposes in reverse order. Singletons implementing
`godi.HealthChecker` (`HealthCheck(ctx) error`) are checked concurrently by
`godi.HealthCheck`:

```go
provider, _ := services.Build()
if err := godi.Start(ctx, provider); err != nil {
    log.Fatal(err)
}

http.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
    if err := godi.HealthCheck(r.Context(), provider); err != nil {
        http.Error(w, "unhealthy", http.StatusServiceUnavailable)
    }
})

<-ctx.Done()
shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
_ = godi.Shutdown(shutdownCtx, provider)
```

Neither creates services: lazy singletons that have not been resolved are
skipped.
