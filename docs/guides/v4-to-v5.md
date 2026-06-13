# Migrating from v4 to v5

godi v5 is a breaking release. This guide lists every breaking change and the
mechanical fix for each. Most upgrades are a find-and-replace plus a single
build-error sweep.

A v4 program keeps working on v4 forever — v5 lives at a different import path,
so nothing updates until you opt in.

## At a glance

| Change | Action |
| --- | --- |
| Import path `/v4` → `/v5` | Find-and-replace across your module |
| Go 1.26 minimum | Upgrade your toolchain |
| `Add*` no longer return errors | Check `Build()` (or `Err()`) instead |
| Typed errors are now pointers | Match with `errors.AsType[*godi.XxxError]` |
| `Collection.ToSlice()` returns `[]ServiceInfo` | Use the read-only view fields |
| `CircularDependencyError` fields are strings | Read `.Node` / `.Path` as strings |
| `Remove` removes keyed + grouped too | Use `RemoveKeyed` for surgical removal |
| `optional:"true"` propagates construction failures | Fix the failing constructor, or make it non-optional |

---

## 1. Import path

The root module moves to `/v5`. Each framework integration is its own module
with the major version at the **end** of its path (`/<framework>/v5`), the
layout Go requires for a multi-module repository:

```go
// Before
import "github.com/junioryono/godi/v4"
import godigin "github.com/junioryono/godi/v4/gin"

// After
import "github.com/junioryono/godi/v5"
import godigin "github.com/junioryono/godi/gin/v5"
```

```sh
# Rewrite integration imports first (godi/v4/<fw> -> godi/<fw>/v5), then the
# root import (godi/v4 -> godi/v5). Order matters.
grep -rl 'junioryono/godi/v4' . | xargs sed -i '' -E 's#junioryono/godi/v4/(http|chi|echo|fiber|gin|huma)#junioryono/godi/\1/v5#g'
grep -rl 'junioryono/godi/v4' . | xargs sed -i '' 's#junioryono/godi/v4#junioryono/godi/v5#g'
go get github.com/junioryono/godi/v5@latest
go mod tidy
```

> Integration import paths are `github.com/junioryono/godi/<framework>/v5`
> (e.g. `.../gin/v5`), **not** `.../v5/gin`.

## 2. Go 1.26 minimum

v5 uses `errors.AsType`, `context.AfterFunc`, and other Go 1.26 features. Bump
your `go.mod`:

```
go 1.26.0
```

## 3. `Add*` methods no longer return errors

This is the largest change. `AddSingleton`, `AddScoped`, `AddTransient`, and
`AddModules` now return nothing. Registration errors are recorded and reported
together by `Build()`.

### Before

```go
c := godi.NewCollection()
if err := c.AddSingleton(NewLogger); err != nil {
    return err
}
if err := c.AddScoped(NewDatabase); err != nil {
    return err
}
```

### After

```go
c := godi.NewCollection()
c.AddSingleton(NewLogger)
c.AddScoped(NewDatabase)

provider, err := c.Build() // every registration error surfaces here
if err != nil {
    return err
}
```

`Build` returns a `*godi.BuildError` with `Phase == "registration"` whose cause
joins all recorded errors (via `errors.Join`), so `errors.Is` / `errors.As`
still match individual causes. Errors recorded inside a module are attributed
with the module name.

If you need to inspect registration errors *before* building, call
`Collection.Err()`:

```go
c.AddSingleton(NewLogger)
if err := c.Err(); err != nil {
    // handle recorded registration errors early
}
```

Module builder functions (`godi.AddSingleton(...)` inside `godi.NewModule`) are
unchanged in shape — they still compose into a `ModuleOption`.

## 4. Typed errors are now pointers

Every typed error is returned as a pointer. Update any error matching that used
a value target. With Go 1.26, prefer the generic `errors.AsType`:

```go
// Before
var resErr godi.ResolutionError
if errors.As(err, &resErr) { ... }

// After (Go 1.26)
if resErr, ok := errors.AsType[*godi.ResolutionError](err); ok { ... }

// After (errors.As, still valid)
var resErr *godi.ResolutionError
if errors.As(err, &resErr) { ... }
```

This applies to all godi error types, including `*godi.LifetimeError` and
`*godi.ModuleError`, which were the last value-returning errors in v4. Sentinel
checks are unchanged: `errors.Is(err, godi.ErrServiceNotFound)` still works.

## 5. `Collection.ToSlice()` returns `[]ServiceInfo`

`ToSlice` previously returned the internal `[]*Descriptor` (shared, mutable
pointers exposing internal wiring). It now returns a read-only snapshot:

```go
type ServiceInfo struct {
    ServiceType reflect.Type
    Key         any       // nil if not keyed
    Group       string    // "" if not grouped
    Lifetime    godi.Lifetime
}
```

```go
// Before
for _, d := range c.ToSlice() {
    fmt.Println(d.Type, d.Lifetime)
}

// After
for _, info := range c.ToSlice() {
    fmt.Println(info.ServiceType, info.Lifetime)
}
```

The `Descriptor` type is no longer exported. If you depended on inspecting its
other fields (`Constructor`, `Dependencies`, etc.), those were internal wiring
and have no replacement; open an issue describing your use case.

## 6. `CircularDependencyError` exposes strings

The cycle is now reported with human-readable type names instead of an internal
node-key type:

```go
// Before: Node graph.NodeKey, Path []graph.NodeKey
// After:
type CircularDependencyError struct {
    Node string
    Path []string
}
```

The rendered error message (`err.Error()`) is unchanged.

## 7. Behavioral changes

These compile without changes but behave differently. All three align the code
with what the v4 docs already promised.

- **`Remove[T]()` / `Collection.Remove`** now removes *all* registrations of a
  type — unkeyed, keyed, and grouped. Use `RemoveKeyed[T](key)` to remove a
  single keyed registration.

- **`optional:"true"`** only forgives a dependency that is *not registered*. If
  the dependency is registered but its constructor fails, that error now
  propagates instead of silently injecting `nil`. Fix the failing constructor,
  or drop `optional` if you intended the failure to be ignored.

- **Stricter registration validation.** Several constructor shapes that v4
  silently accepted (and then failed to resolve) are now rejected at
  registration and reported by `Build`: a non-final `error` return, malformed
  `Out` structs, a `group` tag on a non-slice `In` field, an `In` struct mixed
  with other parameters, a field tagged with both `name` and `group`, and
  `godi.As` combined with a result-object / multi-return / void constructor.
  If `Build` newly fails, the constructor was already broken; the error message
  names the fix.

## 8. Framework integration changes

If you use the framework integration packages, two helpers were removed:

- **`godihttp.Wrap` is removed.** Use `Handle` — it returns an
  `http.HandlerFunc`, which already satisfies `http.Handler`, so it works
  anywhere `Wrap` did.

  ```go
  // Before
  handler := godihttp.Wrap(fn)
  // After
  handler := godihttp.Handle(fn)
  ```

- **`godifiber.FromContext` is removed.** Fiber now attaches the scope to the
  request's `UserContext` (the same context-based access the other
  integrations use, and what Huma needs), instead of `Locals`. Retrieve it
  with the standard helper:

  ```go
  // Before
  scope := godifiber.FromContext(c)
  // After
  scope, err := godi.FromContext(c.UserContext())
  ```

The `gin`, `chi`, `echo`, and `net/http` integrations are otherwise
source-compatible (beyond the `/v5` import path).

New in v5: a [Huma](https://github.com/danielgtaylor/huma) integration
(`github.com/junioryono/godi/huma/v5`) for typed, OpenAPI-backed APIs.

---

## Verifying your upgrade

```sh
go build ./...
go test ./...
```

If it compiles and your `Build()` call returns no error, you're done. If you
hit a breaking change not covered here, please
[open an issue](https://github.com/junioryono/godi/issues).
