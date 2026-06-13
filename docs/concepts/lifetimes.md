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

**Only scoped services may depend on scoped services.**

Scoped services can depend on anything. Singletons and transients cannot
depend on scoped services — godi rejects both at build time.

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
// by the singleton for its whole lifetime.
services.AddTransient(NewIDGenerator)
services.AddSingleton(func(gen *IDGenerator) *Storage {
    return &Storage{gen: gen}
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

// ✗ Transient depending on Scoped
services.AddScoped(NewRequestContext)
services.AddTransient(func(ctx *RequestContext) *Handler {
    return &Handler{ctx: ctx}  // Build error!
})
// Why? A transient can be resolved from the root provider or
// outlive the scope it was created in, so it could hold a
// reference to a disposed scoped service.
```

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
| Transient | Every time | Never        | scope.Close()    | Builders, temp objects        |

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
