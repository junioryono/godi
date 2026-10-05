# Migrating from v5 to v6

godi v6 is a breaking release at a new import path
(`github.com/junioryono/godi/v6`), so a v5 program keeps working until you
opt in. Most of the changes fail loudly, at compile time or at `Build`. One
does not: read [Instances are not disposed](#instances-are-not-disposed)
first.

## Checklist

1. Replace the import paths: `github.com/junioryono/godi/v5` with
   `github.com/junioryono/godi/v6`, and `github.com/junioryono/godi/<integration>/v5`
   with `.../<integration>/v6`. The Echo and Fiber integrations now target
   Echo v5 and Fiber v3 (see [Integrations](#v6-integrations)).
2. Close the values you register as instances yourself.
3. Build, and fix what the compiler and `Build` report, using the sections
   below.

(instances-are-not-disposed)=

## Instances are not disposed

v5 closed every disposable value it handed out, including values passed to
`AddSingleton(value)`. v6 disposes only what its constructors create: a value
you created belongs to you, as in Microsoft.Extensions.DependencyInjection.

```go
// v5: godi closed db when the provider closed.
services.AddSingleton(db)

// v6: you close it ...
defer db.Close()
services.AddSingleton(db)

// ... or hand it to godi with a constructor.
services.AddSingleton(func() *sql.DB { return db })
```

`godi.NoDispose()` remains for constructors whose values you manage.

Two related rules:

- Results of decorators applied to an instance are not disposed either. A
  wrapper that passes `Close` through would otherwise close your value.
  Decorate a constructor instead if a decorator opens resources of its own.
- `godi.Start` no longer starts instances. godi doesn't stop them, so their
  whole lifecycle is yours.

## Scopes are validated by default

Resolving a scoped service from the root provider, directly or through
transients, fails with `godi.ErrScopeRequired`, and the root scope no longer
runs scoped initializers. Create a scope:

```go
scope, err := provider.CreateScope(ctx)
if err != nil {
    return err
}
defer scope.Close()
svc := godi.MustResolve[*RequestService](scope)
```

An application that deliberately uses the root as a scope can opt out with
`services.Build(godi.WithScopeValidation(false))`.

## One Build with options

`BuildWithContext`, `BuildWithOptions` and `ProviderOptions` are replaced by
options to `Build`:

| v5 | v6 |
| --- | --- |
| `BuildWithContext(ctx)` | `Build(godi.WithContext(ctx))` |
| `ProviderOptions{BuildTimeout: d}` | `godi.WithBuildTimeout(d)` |
| `ProviderOptions{Context: ctx}` | `godi.WithContext(ctx)` |
| `ProviderOptions{Observer: o}` | `godi.WithObserver(o)` |
| `ProviderOptions{ValidateScopes: true}` | the default; `godi.WithScopeValidation(false)` opts out |

## Constructors receive Resolver and ScopeFactory

Constructors, decorators and `Invoke` functions can no longer depend on
`godi.Provider` or `godi.Scope`, which let them close the container and
locate any service. `Build` reports such a dependency. Depend on:

- `godi.Resolver` to resolve services. It resolves from the scope running the
  constructor and detects runtime cycles.
- `godi.ScopeFactory` to create scopes, for example in a background worker.
  The scopes are children of the constructor's scope and are closed with it.

```go
// v5
func NewManager(p godi.Provider) *Manager { ... godi.MustResolveKeyed[DB](p, "primary") ... }

// v6
func NewManager(r godi.Resolver) *Manager { ... godi.MustResolveKeyed[DB](r, "primary") ... }
```

Neither view leads back to the container:

- An injected `Resolver` refuses to resolve `godi.Provider` or `godi.Scope`.
- The `context.Context` injected into a constructor keeps the scope's values
  but not the scope, so `godi.FromContext` and `godi.ResolveFromContext` fail
  inside constructors. Use the injected `Resolver`.
- An injected `ScopeFactory` refuses to create scopes until its constructor
  returns. The new scope's initializers could need the constructor's own
  output. Store it and create scopes later.

`Scope.Provider()` is removed. `godi.Invoke`, `godi.IsService` and
`godi.IsKeyedService` take a `godi.Resolver` (a Provider or Scope still
works; `IsService` reports false for Resolver implementations godi didn't
create).

## Names are the only keys

`godi.Key(any)` is removed: register with `godi.Name(string)`, which struct
tags (`name:"..."`) can also express. Keyed lookups take a name:

| v5 | v6 |
| --- | --- |
| `godi.Key(RegionEU)` | `godi.Name(string(RegionEU))` |
| `GetKeyed(t, key any)` | `GetKeyed(t, name string)` |
| `ResolveKeyed[T](r, key any)` | `ResolveKeyed[T](r, name string)` |
| `ContainsKeyed`, `RemoveKeyed`, `RemoveKeyed[T]` with `any` | the same with `string` |
| `ErrServiceKeyNil` | `ErrServiceKeyEmpty` |

The key fields of `ServiceInfo`, `DependencyInfo`, `ConstructedEvent`,
`ResolutionError` and `MissingDependencyError` are names (`""` when unnamed).
An empty name names no service: `ContainsKeyed` reports false and
`RemoveKeyed` removes nothing (in v5 they meant the unnamed registration).

## godi.Name names every output

On a constructor with several non-error returns, `godi.Name` now keys every
return, as `godi.Group` adds every return to the group. A dependency on an
unnamed return of such a constructor fails `Build` with a missing dependency:
resolve it by name, or use a result object (`godi.Out`) to name outputs
individually.

## Errors

- Every error type has pointer receivers: only `*godi.ResolutionError` (not
  `godi.ResolutionError`) implements `error`. A value-typed `errors.As` target
  now panics, and `go vet` reports it; use `errors.AsType[*godi.ResolutionError]`
  or a pointer target.
- `TimeoutError` and `GraphOperationError` are removed (godi never returned
  them). `ErrDescriptorNil`, `ErrSingletonNotInitialized` and
  `ReflectionAnalysisError` are no longer exported.
- `BuildError.Phase` is a `godi.BuildPhase` and `DisposalError.Context` a
  `godi.DisposalContext`; compare them with the `Phase*` and
  `DisposalProvider`/`DisposalScope` constants.
- `ResolutionError.ServiceNotFound()` is removed: match
  `errors.Is(err, godi.ErrServiceNotFound)`.

## Sealed interfaces

`Collection`, `Provider` and `Scope` can no longer be implemented outside
godi, so new methods can be added in minor releases. Test doubles should
implement `godi.Resolver`. A `Collection` embedded in your own type keeps
working with `Validate`, `Decorate`, `Replace*` and `TryAdd*`.

`TypeKey` and `GroupKey` are no longer exported.

(v6-integrations)=

## Integrations

- `github.com/junioryono/godi/echo/v6` integrates **Echo v5**
  (`github.com/labstack/echo/v5`). It was `.../echov5/v5` in godi v5; the Echo
  v4 integration is retired. See
  [Migrating from Echo v4](#migrating-from-echo-v4).
- `github.com/junioryono/godi/fiber/v6` integrates **Fiber v3**
  (`github.com/gofiber/fiber/v3`). It was `.../fiberv3/v5`; the Fiber v2
  integration is retired. See
  [Migrating from Fiber v2](#migrating-from-fiber-v2).

The net/http, Chi, Gin and Huma integrations change only their import paths.
