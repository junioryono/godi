# Service Lifetimes Reference

Service lifetimes control when instances are created and how long they live. Understanding lifetimes is crucial for building efficient and correct applications.

## Overview

godi supports two service lifetimes:

| Lifetime      | Instance Creation    | Instance Sharing    | Disposal             |
| ------------- | -------------------- | ------------------- | -------------------- |
| **Singleton** | Once per application | Shared globally     | When provider closes |
| **Scoped**    | Once per scope       | Shared within scope | When scope closes    |

## Singleton Services

Singleton services are created once and shared throughout the application lifetime.

### Characteristics

- **One instance** for the entire application
- Created on first request (lazy initialization)
- Thread-safe instance sharing
- Disposed when the root provider is closed
- Cannot depend on scoped services

### When to Use

- **Stateless services**: Loggers, configuration, metrics collectors
- **Expensive resources**: Database connections, HTTP clients
- **Shared state**: Caches, connection pools
- **Application-wide services**: Background workers, schedulers

### Example

```go
// Good singleton examples
collection.AddSingleton(NewLogger)           // Stateless
collection.AddSingleton(NewConfiguration)    // Immutable
collection.AddSingleton(NewDatabasePool)     // Shared resource
collection.AddSingleton(NewMetricsCollector) // Thread-safe

// Bad singleton examples
collection.AddSingleton(NewHttpContext)      // ❌ Request-specific
collection.AddSingleton(NewTransaction)      // ❌ Should be scoped
```

### Implementation Details

```go
type Cache struct {
    mu    sync.RWMutex
    items map[string]interface{}
}

func NewCache() *Cache {
    return &Cache{
        items: make(map[string]interface{}),
    }
}

// Thread-safe methods required for singletons
func (c *Cache) Get(key string) (interface{}, bool) {
    c.mu.RLock()
    defer c.mu.RUnlock()

    val, ok := c.items[key]
    return val, ok
}

func (c *Cache) Set(key string, value interface{}) {
    c.mu.Lock()
    defer c.mu.Unlock()

    c.items[key] = value
}
```

## Scoped Services

Scoped services are created once per scope and shared within that scope.

### Characteristics

- **One instance per scope**
- Created when first requested in a scope
- Shared by all services within the same scope
- Disposed when the scope is closed
- Can depend on singleton or other scoped services

### When to Use

- **Request-specific services**: HTTP context, request ID, user context
- **Unit of work patterns**: Database transactions, batch operations
- **Temporary state**: Request caches, operation context
- **Resource isolation**: Per-request database connections

### Example

```go
// Good scoped examples
collection.AddScoped(NewRequestContext)   // HTTP request context
collection.AddScoped(NewUnitOfWork)       // Database transaction
collection.AddScoped(NewUserContext)      // Authenticated user
collection.AddScoped(NewRequestLogger)    // Request-scoped logger

// Web request handling
func HandleRequest(provider godi.ServiceProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Create scope for this request
        scope := provider.CreateScope(r.Context())
        defer scope.Close()

        // All services share the same UnitOfWork in this scope
        service, _ := godi.Resolve[*OrderService](scope.ServiceProvider())
        service.CreateOrder(order) // Uses scoped UnitOfWork
    }
}
```

### Scope Hierarchy

```go
// Root scope (provider level)
provider, _ := collection.BuildServiceProvider()

// Request scope
requestScope := provider.CreateScope(ctx)
defer requestScope.Close()

// Nested scope (e.g., for batch processing)
batchScope := requestScope.ServiceProvider().CreateScope(ctx)
defer batchScope.Close()
```

## Lifetime Compatibility

### Dependency Rules

1. **Singleton** can depend on:

   - ✅ Other singletons
   - ❌ Scoped services (causes captive dependency)

2. **Scoped** can depend on:

   - ✅ Singletons
   - ✅ Other scoped services

### Captive Dependencies

A captive dependency occurs when a service with a longer lifetime holds a reference to a service with a shorter lifetime:

```go
// ❌ BAD: Singleton holding scoped
type SingletonService struct {
    scopedDb ScopedDatabase // Will capture first scope's instance!
}

// ✅ GOOD: Use a factory or service provider
type SingletonService struct {
    provider godi.ServiceProvider
}

func (s *SingletonService) DoWork(ctx context.Context) {
    scope := s.provider.CreateScope(ctx)
    defer scope.Close()

    db, _ := godi.Resolve[ScopedDatabase](scope.ServiceProvider())
    // Use db within scope
}
```

## Disposal Order

Services are disposed in reverse order of creation (LIFO):

```go
scope := provider.CreateScope(ctx)

// Creation order:
// 1. Logger (singleton - not disposed with scope)
// 2. Database (scoped)
// 3. Repository (scoped)
// 4. Service (scoped)

scope.Close()

// Disposal order:
// 1. Service
// 2. Repository
// 3. Database
// (Logger remains - disposed with provider)
```

## Best Practices

### Choose the Right Lifetime

```go
// Stateless, thread-safe → Singleton
collection.AddSingleton(NewLogger)
collection.AddSingleton(NewConfiguration)

// Request/operation specific → Scoped
collection.AddScoped(NewDbContext)
collection.AddScoped(NewRequestContext)
```

### Avoid Common Pitfalls

1. **Don't capture scoped in singleton**

   ```go
   // ❌ Wrong
   func NewCache(db Database) *Cache {
       return &Cache{db: db} // If db is scoped, this is wrong
   }

   // ✅ Correct
   func NewCache(provider ServiceProvider) *Cache {
       return &Cache{provider: provider}
   }
   ```

## Testing Considerations

Different lifetimes require different testing approaches:

```go
// Singleton - mock once
collection.AddSingleton(func() Logger {
    return &MockLogger{}
})

// Scoped - mock per test scope
func TestWithScope(t *testing.T) {
    provider, _ := collection.BuildServiceProvider()

    scope := provider.CreateScope(context.Background())
    defer scope.Close()

    // Test with scoped mocks
}
```

## Performance Implications

| Lifetime  | Creation Cost      | Memory Usage | Caching   |
| --------- | ------------------ | ------------ | --------- |
| Singleton | Once (low)         | Constant     | Yes       |
| Scoped    | Per scope (medium) | Per scope    | Per scope |

### Optimization Tips

1. **Use singleton for expensive resources**
2. **Use scoped for request-bound state**

## Summary

- **Singleton**: Application-wide, shared, thread-safe services
- **Scoped**: Per-operation services with shared state within scope

Choose lifetimes based on:

- State requirements
- Resource cost
- Sharing needs
- Thread safety
- Disposal requirements
