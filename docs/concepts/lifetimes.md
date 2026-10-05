# Service Lifetimes

When should a database connection be shared? When should a request context be unique? Lifetimes answer these questions.

## Visual Overview

```
┌─────────────────────────────────────────────────────────────────┐
│ Application Lifetime                                            │
│ ┌─────────────────────────────────────────────────────────────┐ │
│ │                        SINGLETON                            │ │
│ │   Logger, Database Pool, Config, HTTP Client                │ │
│ │   Created once at startup, shared everywhere                │ │
│ └─────────────────────────────────────────────────────────────┘ │
│                                                                 │
│ ┌──────────────┐  ┌──────────────┐  ┌──────────────┐            │
│ │  Request 1   │  │  Request 2   │  │  Request 3   │            │
│ │              │  │              │  │              │            │
│ │   SCOPED     │  │   SCOPED     │  │   SCOPED     │            │
│ │  UserSession │  │  UserSession │  │  UserSession │            │
│ │  Transaction │  │  Transaction │  │  Transaction │            │
│ │              │  │              │  │              │            │
│ │  TRANSIENT   │  │  TRANSIENT   │  │  TRANSIENT   │            │
│ │  new each    │  │  new each    │  │  new each    │            │
│ │  resolution  │  │  resolution  │  │  resolution  │            │
│ └──────────────┘  └──────────────┘  └──────────────┘            │
└─────────────────────────────────────────────────────────────────┘
```

## Singleton

**One instance for the entire application.**

```go
services.AddSingleton(NewDatabasePool)

// Same instance everywhere
db1 := godi.MustResolve[*DatabasePool](provider)
db2 := godi.MustResolve[*DatabasePool](provider)
// db1 == db2 ✓
```

### When to Use Singleton

- Database connection pools
- Configuration objects
- Loggers
- HTTP clients
- Caches
- Any shared, thread-safe resource

### Singleton Lifecycle

```
┌──────────────────────────────────────────────────────────┐
│  services.Build()                                        │
│       │                                                  │
│       ▼                                                  │
│  Constructor Called (eagerly, at build) ──▶ Cached       │
│       │                                                  │
│       ▼                                                  │
│  Every Resolution ──▶ Return Cached Instance             │
│       │                                                  │
│       ▼                                                  │
│  provider.Close() ──▶ Dispose (if implements Close())    │
└──────────────────────────────────────────────────────────┘
```

Singletons are created **eagerly when you call `Build()`**, in dependency
order. A failing singleton constructor fails the build, not the first
resolution.

## Scoped

**One instance per scope. Different scopes get different instances.**

```go
services.AddScoped(NewRequestContext)

// Create a scope
scope, _ := provider.CreateScope(ctx)
defer scope.Close()

// Same within scope
ctx1 := godi.MustResolve[*RequestContext](scope)
ctx2 := godi.MustResolve[*RequestContext](scope)
// ctx1 == ctx2 ✓

// Different scope = different instance
scope2, _ := provider.CreateScope(ctx)
defer scope2.Close()
ctx3 := godi.MustResolve[*RequestContext](scope2)
// ctx1 == ctx3 ✗
```

### When to Use Scoped

- Request context
- Database transactions
- User sessions
- Per-request caches
- Unit of work patterns

### Scoped Lifecycle

```
┌──────────────────────────────────────────────────────────┐
│  provider.CreateScope(ctx)                               │
│       │                                                  │
│       ▼                                                  │
│  First Resolution in Scope ──▶ Constructor ──▶ Cached    │
│       │                                                  │
│       ▼                                                  │
│  More Resolutions in Scope ──▶ Return Cached             │
│       │                                                  │
│       ▼                                                  │
│  scope.Close() ──▶ Dispose All Scoped Services           │
└──────────────────────────────────────────────────────────┘
```

## Transient

**New instance every single time.**

```go
services.AddTransient(NewEmailBuilder)

// Always new
builder1 := godi.MustResolve[*EmailBuilder](provider)
builder2 := godi.MustResolve[*EmailBuilder](provider)
// builder1 == builder2 ✗
```

### When to Use Transient

- Builders
- Temporary objects
- Stateful utilities that shouldn't be shared
- Unique instances

### Transient Lifecycle

```
┌──────────────────────────────────────────────────────────┐
│  Each Resolution                                         │
│       │                                                  │
│       ▼                                                  │
│  Constructor Called ──▶ New Instance Returned            │
│       │                                                  │
│       ▼                                                  │
│  scope.Close() ──▶ Dispose (if tracked and disposable)   │
└──────────────────────────────────────────────────────────┘
```

## The Golden Rule

**A singleton must never hold a scoped service.**

Scoped and transient services can depend on anything. A singleton cannot depend
on a scoped service — directly, or through a chain of transients — and godi
rejects that at build time. Resolved from the root provider, a scoped service
is the root scope's single instance, so resolve per-request services from a
scope you create (see below).

### Valid Dependencies

```go
// ✓ Scoped depending on Singleton
services.AddSingleton(NewLogger)
services.AddScoped(func(logger *Logger) *UserService {
    return &UserService{logger: logger}
})

// ✓ Transient depending on Singleton
services.AddSingleton(NewLogger)
services.AddTransient(func(logger *Logger) *TempService {
    return &TempService{logger: logger}
})

// ✓ Scoped depending on Scoped
services.AddScoped(NewRequestContext)
services.AddScoped(func(ctx *RequestContext) *Handler {
    return &Handler{ctx: ctx}
})

// ✓ Singleton depending on Transient
// Allowed: the transient is created once at build and captured
// by the singleton for its whole lifetime (so it must be safe for
// concurrent use, like the singleton itself).
services.AddTransient(NewIDGenerator)
services.AddSingleton(func(gen *IDGenerator) *Storage {
    return &Storage{gen: gen}
})

// ✓ Transient depending on Scoped
// A fresh handler per use, sharing the request's unit of work.
// Resolve it from the request scope.
services.AddScoped(NewUnitOfWork)
services.AddTransient(func(uow *UnitOfWork) *CreateOrderHandler {
    return &CreateOrderHandler{uow: uow}
})
```

### Invalid Dependencies

```go
// ✗ Singleton depending on Scoped
services.AddScoped(NewRequestContext)
services.AddSingleton(func(ctx *RequestContext) *Cache {
    return &Cache{ctx: ctx}  // Build error!
})
// Why? The singleton lives forever, but the scoped service
// is destroyed when the scope closes. The singleton would
// hold a dangling reference.

// ✗ Singleton reaching Scoped through a Transient
services.AddScoped(NewRequestContext)
services.AddTransient(func(ctx *RequestContext) *Handler {
    return &Handler{ctx: ctx}
})
services.AddSingleton(func(h *Handler) *Router {
    return &Router{h: h}  // Build error! (via *Handler)
})
// Why? The singleton is built once, so its transient Handler
// (and the scoped value inside it) would be captured forever.
```

(scoped-services-from-the-root)=

### Scoped Services and the Root Provider

The provider has a root scope of its own. A scoped service resolved from the
provider (or through a singleton's injected `godi.Resolver`) belongs to that
root scope: it is created once, cached until the provider closes, and disposed
with it. The root scope runs scoped initializers during `Build`.

That suits work that lasts as long as the application, such as a CLI command or
a worker's own state. For a unit of work, such as a request or a job, create a
scope, so it gets its own instances and they are disposed when it ends:

```go
provider, err := services.Build()

cmd := godi.MustResolve[*Command](provider)  // the root scope's instance

scope, _ := provider.CreateScope(ctx)
defer scope.Close()
req := godi.MustResolve[*RequestContext](scope)  // this request's instance
```

Transients that need scoped services share the instances of the scope they are
resolved from.

## Performance Considerations

### Memory Usage

```go
// Singleton: 1 instance total
services.AddSingleton(NewHeavyService) // 100MB
// Total: 100MB

// Scoped: 1 instance per active scope
services.AddScoped(NewHeavyService) // 100MB each
// 10 concurrent requests = 1GB

// Transient: 1 instance per resolution
services.AddTransient(NewHeavyService) // 100MB each
// Can grow unbounded!
```

### Creation Cost

```go
// Singleton: Paid once
services.AddSingleton(NewExpensiveService) // 5 seconds
// Total cost: 5 seconds

// Scoped: Paid per scope
services.AddScoped(NewExpensiveService) // 5 seconds
// Per request cost: 5 seconds

// Transient: Paid every time
services.AddTransient(NewExpensiveService) // 5 seconds
// Every resolution: 5 seconds
```

## Quick Reference

| Lifetime  | Created    | Shared       | Disposed         | Best For                      |
| --------- | ---------- | ------------ | ---------------- | ----------------------------- |
| Singleton | Once       | App-wide     | provider.Close() | DB pools, config, loggers     |
| Scoped    | Per scope  | Within scope | scope.Close()    | Request context, transactions |
| Transient | Every time | Never        | scope.Close() ¹  | Builders, temp objects        |

¹ When resolved from a scope. A transient resolved directly from the provider
is owned by the caller, who must close it (see
[Resource Cleanup](../features/resource-cleanup.md)).

## Common Patterns

### Web Application

```go
// Singletons - shared infrastructure
services.AddSingleton(NewLogger)
services.AddSingleton(NewDatabasePool)
services.AddSingleton(NewRedisClient)
services.AddSingleton(NewHTTPClient)

// Scoped - per-request state
services.AddScoped(NewRequestContext)
services.AddScoped(NewTransaction)
services.AddScoped(NewUserSession)

// Transient - utilities
services.AddTransient(NewQueryBuilder)
services.AddTransient(NewEmailBuilder)
```

### Background Worker

```go
// Singletons - shared
services.AddSingleton(NewJobQueue)
services.AddSingleton(NewMetrics)

// Scoped - per-job
services.AddScoped(NewJobContext)
services.AddScoped(NewJobLogger)

// Transient - utilities
services.AddTransient(NewRetryHandler)
```

---

**Next:** Learn about [scopes and request isolation](scopes.md)
