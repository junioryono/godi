# Service Lifetimes

Service lifetimes determine when instances are created and how long they live. Understanding lifetimes is crucial for building efficient applications.

## The Three Lifetimes

### Singleton - Application Lifetime

Created once, shared everywhere throughout the application.

```go
func NewDatabaseConnection(config Config) DatabaseConnection {
    // This expensive connection is created only once
    conn, _ := sql.Open("postgres", config.DatabaseURL)
    return &databaseConnection{conn: conn}
}

services.AddSingleton(NewDatabaseConnection)

// Later in your app
db1 := godi.MustResolve[DatabaseConnection](provider) // Created
db2 := godi.MustResolve[DatabaseConnection](provider) // Same instance
// db1 == db2 (true)
```

**Use Singleton for:**

- Database connections
- Configuration objects
- Loggers
- Cache instances
- HTTP clients
- Any shared, thread-safe resource

### Scoped - Request Lifetime

Created once per scope, shared within that scope.

```go
func NewRequestContext() *RequestContext {
    return &RequestContext{
        ID:        uuid.New().String(),
        StartTime: time.Now(),
    }
}

services.AddScoped(NewRequestContext)

// In HTTP handler
func Handler(provider godi.Provider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        scope, _ := provider.CreateScope(r.Context())
        defer scope.Close()

        ctx1 := godi.MustResolve[*RequestContext](scope) // Created
        ctx2 := godi.MustResolve[*RequestContext](scope) // Same instance
        // ctx1 == ctx2 (true) - same within scope

        // Different scope = different instance
        scope2, _ := provider.CreateScope(r.Context())
        defer scope2.Close()
        ctx3 := godi.MustResolve[*RequestContext](scope2) // New instance
        // ctx1 == ctx3 (false)
    }
}
```

**Use Scoped for:**

- HTTP request context
- Database transactions
- User sessions
- Unit of work patterns
- Request-specific caches

### Transient - Always New

Created fresh every time it's requested.

```go
func NewTempFileHandler() TempFileHandler {
    file, _ := os.CreateTemp("", "temp")
    return &tempFileHandler{file: file}
}

services.AddTransient(NewTempFileHandler)

// Each resolution creates a new instance
handler1 := godi.MustResolve[TempFileHandler](provider)
handler2 := godi.MustResolve[TempFileHandler](provider)
// handler1 != handler2 (different instances)
```

**Use Transient for:**

- Temporary objects
- Builders
- Unique instances
- Stateful helpers
- Objects that shouldn't be shared

## Lifetime Rules

### The Golden Rule

**A service can only depend on services with the same or longer lifetime.**

```go
// ✅ Valid: Scoped depending on Singleton
func NewUserService(db Database) UserService { // Database is Singleton
    return &userService{db: db}
}
services.AddScoped(NewUserService)    // Scoped
services.AddSingleton(NewDatabase)    // Singleton

// ❌ Invalid: Singleton depending on Scoped
func NewCache(ctx *RequestContext) Cache { // RequestContext is Scoped
    return &cache{context: ctx}
}
services.AddSingleton(NewCache)        // Error at build time!
services.AddScoped(NewRequestContext)  // Scoped
```

## Practical Examples

### Web Application Pattern

```go
// Singleton - shared resources
services.AddSingleton(NewLogger)
services.AddSingleton(NewDatabasePool)
services.AddSingleton(NewRedisCache)
services.AddSingleton(NewEmailClient)

// Scoped - per request
services.AddScoped(NewRequestContext)
services.AddScoped(NewDatabaseTransaction)
services.AddScoped(NewUserSession)

// Transient - always new
services.AddTransient(NewEmailBuilder)
services.AddTransient(NewQueryBuilder)
```

### Background Job Pattern

```go
// Singleton - shared
services.AddSingleton(NewJobQueue)
services.AddSingleton(NewMetricsCollector)

// Scoped - per job execution
services.AddScoped(NewJobContext)
services.AddScoped(NewJobLogger)

// Transient - utilities
services.AddTransient(NewRetryHandler)
```

## Lifetime and Performance

### Memory Usage

```go
// Singleton: 1 instance total
services.AddSingleton(NewHeavyService) // 100MB
// Total memory: 100MB

// Scoped: 1 instance per active scope
services.AddScoped(NewHeavyService) // 100MB per request
// 10 concurrent requests = 1GB

// Transient: 1 instance per resolution
services.AddTransient(NewHeavyService) // 100MB per use
// Can grow unbounded!
```

### Creation Cost

```go
// Singleton: Created once at startup
services.AddSingleton(NewExpensiveService) // 5 second setup
// Cost: 5 seconds total

// Scoped: Created per scope
services.AddScoped(NewExpensiveService) // 5 second setup
// Cost: 5 seconds per request!

// Transient: Created every time
services.AddTransient(NewExpensiveService) // 5 second setup
// Cost: 5 seconds per resolution!
```

## Disposal and Cleanup

Services implementing `Disposable` are cleaned up based on lifetime:

```go
type DatabaseConnection struct {
    conn *sql.DB
}

func (d *DatabaseConnection) Close() error {
    return d.conn.Close()
}

// Singleton: Closed when provider closes
services.AddSingleton(NewDatabaseConnection)
provider.Close() // Disposes all singletons

// Scoped: Closed when scope closes
services.AddScoped(NewTransaction)
scope.Close() // Disposes all scoped services

// Transient: Closed when scope closes (if tracked)
services.AddTransient(NewTempFile)
scope.Close() // Disposes transients created in this scope
```

## Common Mistakes

### 1. Wrong Lifetime for Database Connections

```go
// ❌ Don't make connections transient
services.AddTransient(NewDatabaseConnection)
// Creates new connection every time - connection pool exhaustion!

// ✅ Use singleton for connection pools
services.AddSingleton(NewDatabasePool)
```

### 2. Caching Request Data in Singletons

```go
// ❌ Don't store request data in singletons
type Cache struct {
    userID string // Wrong! Shared across all requests
}
services.AddSingleton(NewCache)

// ✅ Use scoped for request-specific data
type RequestCache struct {
    userID string // Correct - isolated per request
}
services.AddScoped(NewRequestCache)
```

### 3. Not Considering Thread Safety

```go
// ❌ Mutable singleton without synchronization
type Counter struct {
    count int // Race condition!
}
services.AddSingleton(NewCounter)

// ✅ Thread-safe singleton
type Counter struct {
    mu    sync.Mutex
    count int
}
func (c *Counter) Increment() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.count++
}
```

## Best Practices

1. **Start with Singleton** for shared, thread-safe services
2. **Use Scoped** for request/operation-specific state
3. **Reserve Transient** for stateless utilities or unique instances
4. **Consider memory** impact when choosing lifetimes
5. **Validate at build time** - godi will catch lifetime violations
6. **Implement Disposable** for resources needing cleanup

## Next Steps

- Learn about [Service Registration](service-registration.md)
- Understand [Dependency Resolution](dependency-resolution.md)
- Explore [Scopes & Isolation](scopes-isolation.md)
