# godi v4 → v5 Migration

v5 is a breaking release. A v4 program keeps working on v4 — v5 lives at a new
import path, so nothing changes until you opt in. Most upgrades are a
find-and-replace plus one build-error sweep.

The full guide with before/after examples lives at
[docs/guides/v4-to-v5.md](docs/guides/v4-to-v5.md).

## Breaking changes

| Change | Action |
| --- | --- |
| Import path `/v4` → `/v5` | Find-and-replace across your module |
| Go 1.26 minimum | Set `go 1.26.0` in your `go.mod` |
| `AddSingleton`/`AddScoped`/`AddTransient`/`AddModules` no longer return errors | Check `Build()` (or `Collection.Err()`) instead |
| Typed errors are now pointers | Match with `errors.AsType[*godi.XxxError]` (Go 1.26) or a pointer target |
| `Collection.ToSlice()` returns `[]ServiceInfo` (read-only) | Use `info.ServiceType` / `.Key` / `.Group` / `.Lifetime`; `Descriptor` is no longer exported |
| `CircularDependencyError.Node`/`.Path` are now `string`/`[]string` | Read them as strings (`Error()` output unchanged) |
| `Remove[T]()` removes keyed + grouped registrations too | Use `RemoveKeyed[T](key)` for surgical removal |
| `optional:"true"` propagates construction failures | Only *unregistered* deps are forgiven; fix the failing constructor |
| Stricter registration validation | Previously-broken constructor shapes now fail at `Build` with a descriptive error |

## The one that touches the most code

`Add*` methods no longer return errors. Register freely, then check `Build`:

```go
c := godi.NewCollection()
c.AddSingleton(NewLogger)
c.AddScoped(NewDatabase)

provider, err := c.Build() // all registration errors surface here
if err != nil {
    return err
}
```

`Build` returns a `*godi.BuildError` (`Phase == "registration"`) whose cause
joins every recorded error via `errors.Join`, so `errors.Is`/`errors.As` still
match individual causes, and module errors carry the module name.

## Quick path-migration

```sh
go get github.com/junioryono/godi/v5@latest
grep -rl 'junioryono/godi/v4' . | xargs sed -i '' 's#junioryono/godi/v4#junioryono/godi/v5#g'
go mod tidy && go build ./...
```
