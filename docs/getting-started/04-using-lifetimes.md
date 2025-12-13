# Using Lifetimes

When should a database connection be shared? When should a request context be unique? Lifetimes answer these questions.

## The Three Lifetimes

```
┌────────────────────────────────────────────────────────┐
│  Application Lifetime                                  │
│  ┌───────────────────────────────────────────────────┐ │
│  │ SINGLETON: Logger, Database, Config               │ │
│  │ Created once, shared everywhere                   │ │
│  └───────────────────────────────────────────────────┘ │
│                                                        │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  │
│  │  Request 1   │  │  Request 2   │  │  Request 3   │  │
│  │              │  │              │  │              │  │
│  │ SCOPED:      │  │ SCOPED:      │  │ SCOPED:      │  │
│  │ UserSession  │  │ UserSession  │  │ UserSession  │  │
│  │ Transaction  │  │ Transaction  │  │ Transaction  │  │
│  │              │  │              │  │              │  │
│  │ TRANSIENT:   │  │ TRANSIENT:   │  │ TRANSIENT:   │  │
│  │ new instance │  │ new instance │  │ new instance │  │
│  │ every time   │  │ every time   │  │ every time   │  │
│  └──────────────┘  └──────────────┘  └──────────────┘  │
└────────────────────────────────────────────────────────┘
```

## Singleton - One Instance Forever

Created once when first requested. Shared by everyone.

```go
services.AddSingleton(NewDatabasePool)

// Same instance everywhere
db1 := godi.MustResolve[*DatabasePool](provider)
db2 := godi.MustResolve[*DatabasePool](provider)
// db1 == db2 ✓
```

**Use for:** Database connections, configuration, loggers, HTTP clients, caches

## Scoped - One Instance Per Scope

Created once per scope. Different scopes get different instances.

```go
services.AddScoped(NewRequestContext)

// Create a scope (typically per HTTP request)
scope1, _ := provider.CreateScope(ctx)
defer scope1.Close()

// Same within scope
ctx1 := godi.MustResolve[*RequestContext](scope1)
ctx2 := godi.MustResolve[*RequestContext](scope1)
// ctx1 == ctx2 ✓

// Different scope = different instance
scope2, _ := provider.CreateScope(ctx)
defer scope2.Close()
ctx3 := godi.MustResolve[*RequestContext](scope2)
// ctx1 == ctx3 ✗
```

**Use for:** Request context, database transactions, user sessions, per-request caches

## Transient - New Instance Every Time

Created fresh on every resolution.

```go
services.AddTransient(NewEmailBuilder)

// Always new
builder1 := godi.MustResolve[*EmailBuilder](provider)
builder2 := godi.MustResolve[*EmailBuilder](provider)
// builder1 == builder2 ✗
```

**Use for:** Builders, temporary objects, stateful utilities

## Complete Example

```go
package main

import (
    "context"
    "fmt"
    "log"
    "github.com/junioryono/godi/v4"
)

// Singleton - shared everywhere
type Logger struct {
    id int
}
var loggerCount = 0
func NewLogger() *Logger {
    loggerCount++
    return &Logger{id: loggerCount}
}

// Scoped - one per scope
type RequestID struct {
    value int
}
var requestCount = 0
func NewRequestID() *RequestID {
    requestCount++
    return &RequestID{value: requestCount}
}

// Transient - always new
type TempFile struct {
    name string
}
var fileCount = 0
func NewTempFile() *TempFile {
    fileCount++
    return &TempFile{name: fmt.Sprintf("temp_%d.txt", fileCount)}
}

func main() {
    services := godi.NewCollection()
    services.AddSingleton(NewLogger)
    services.AddScoped(NewRequestID)
    services.AddTransient(NewTempFile)

    provider, err := services.Build()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // Simulate two HTTP requests
    for i := 1; i <= 2; i++ {
        fmt.Printf("\n--- Request %d ---\n", i)

        scope, _ := provider.CreateScope(context.Background())

        // Singleton: same logger
        logger := godi.MustResolve[*Logger](scope)
        fmt.Printf("Logger ID: %d\n", logger.id)

        // Scoped: same within request
        reqID1 := godi.MustResolve[*RequestID](scope)
        reqID2 := godi.MustResolve[*RequestID](scope)
        fmt.Printf("RequestID (same scope): %d == %d? %v\n",
            reqID1.value, reqID2.value, reqID1 == reqID2)

        // Transient: different every time
        file1 := godi.MustResolve[*TempFile](scope)
        file2 := godi.MustResolve[*TempFile](scope)
        fmt.Printf("TempFile: %s, %s\n", file1.name, file2.name)

        scope.Close()
    }
}
```

Output:

```
--- Request 1 ---
Logger ID: 1
RequestID (same scope): 1 == 1? true
TempFile: temp_1.txt, temp_2.txt

--- Request 2 ---
Logger ID: 1
RequestID (same scope): 2 == 2? true
TempFile: temp_3.txt, temp_4.txt
```

## The Golden Rule

**A service can only depend on services with the same or longer lifetime.**

```go
// ✓ OK: Scoped can depend on Singleton
services.AddSingleton(NewLogger)
services.AddScoped(func(logger *Logger) *UserService {
    return &UserService{logger: logger}
})

// ✗ ERROR: Singleton cannot depend on Scoped
services.AddScoped(NewRequestContext)
services.AddSingleton(func(ctx *RequestContext) *Cache {  // Build error!
    return &Cache{ctx: ctx}
})
```

Why? A singleton lives forever, but scoped services are destroyed when the scope closes. The singleton would hold a reference to something that no longer exists.

## Quick Reference

| Lifetime  | Created    | Shared       | Destroyed        | Use Case                      |
| --------- | ---------- | ------------ | ---------------- | ----------------------------- |
| Singleton | Once       | App-wide     | Provider.Close() | DB pools, config              |
| Scoped    | Per scope  | Within scope | Scope.Close()    | Request context, transactions |
| Transient | Every time | Never        | Scope.Close()    | Builders, temp objects        |

---

**Next:** [Build a web application](05-http-integration.md)
