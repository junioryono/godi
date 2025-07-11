# Performance Guide

This guide covers performance considerations and optimization techniques when using godi.

## Performance Overview

godi is built on top of Uber's dig, which is highly optimized. However, understanding the performance characteristics helps you make better design decisions.

### Performance Characteristics

| Operation            | Performance | Notes                            |
| -------------------- | ----------- | -------------------------------- |
| Service Registration | O(1)        | One-time at startup              |
| Provider Build       | O(n)        | Once at startup, validates graph |
| Singleton Resolution | O(1)\*      | Cached after first creation      |
| Scoped Resolution    | O(1)\*      | Cached within scope              |
| Transient Resolution | O(d)        | d = dependency depth             |
| Scope Creation       | O(s)        | s = scoped services count        |

\* After initial creation

## Resolution Performance

### Singleton Services

Singletons are the most performant - created once and cached:

```go
// First resolution - constructs the service
logger, _ := godi.Resolve[Logger](provider) // ~100μs

// Subsequent resolutions - returns cached instance
logger2, _ := godi.Resolve[Logger](provider) // ~1μs
```

### Scoped Services

Scoped services are cached within their scope:

```go
scope := provider.CreateScope(ctx)

// First resolution in scope - constructs the service
repo, _ := godi.Resolve[Repository](scope.ServiceProvider()) // ~50μs

// Subsequent resolutions in same scope - cached
repo2, _ := godi.Resolve[Repository](scope.ServiceProvider()) // ~1μs

// Different scope - new instance
scope2 := provider.CreateScope(ctx)
repo3, _ := godi.Resolve[Repository](scope2.ServiceProvider()) // ~50μs
```

### Transient Services

Transients have the highest overhead - created every time:

```go
// Every resolution creates a new instance
cmd1, _ := godi.Resolve[Command](provider) // ~30μs
cmd2, _ := godi.Resolve[Command](provider) // ~30μs (not cached)
```

## Optimization Techniques

### 1. Cache Service Resolution

For hot paths, resolve services once:

```go
// ❌ Bad - resolves on every request
func (h *Handler) HandleRequest(w http.ResponseWriter, r *http.Request) {
    service, _ := godi.Resolve[UserService](h.scope.ServiceProvider())
    service.ProcessRequest(r)
}

// ✅ Good - resolve once in constructor
type Handler struct {
    userService UserService
}

func NewHandler(userService UserService) *Handler {
    return &Handler{userService: userService}
}
```

### 2. Minimize Transient Usage

Use transients only when necessary:

```go
// ❌ Bad - transient for stateless service
collection.AddTransient(NewLogger) // Creates new logger each time

// ✅ Good - singleton for stateless service
collection.AddSingleton(NewLogger) // Reuses same instance

// ✅ Good - transient for stateful objects
collection.AddTransient(NewEmailMessage) // Unique state per message
```

### 3. Optimize Scope Usage

Create scopes at the right granularity:

```go
// ❌ Bad - scope for each small operation
for _, item := range items {
    scope := provider.CreateScope(ctx) // Overhead
    processItem(scope, item)
    scope.Close()
}

// ✅ Good - batch operations in scope
scope := provider.CreateScope(ctx)
defer scope.Close()

processor, _ := godi.Resolve[BatchProcessor](scope.ServiceProvider())
processor.ProcessItems(items)
```

### 4. Lazy Resolution

Defer expensive service resolution:

```go
type Service struct {
    provider godi.ServiceProvider
    expensive ExpensiveService // Don't resolve in constructor
    once sync.Once
}

func (s *Service) getExpensive() ExpensiveService {
    s.once.Do(func() {
        s.expensive, _ = godi.Resolve[ExpensiveService](s.provider)
    })
    return s.expensive
}

func (s *Service) ProcessIfNeeded(condition bool) {
    if !condition {
        return // Expensive service never created
    }

    expensive := s.getExpensive()
    expensive.Process()
}
```

## Benchmarking

### Basic Benchmark

```go
func BenchmarkServiceResolution(b *testing.B) {
    collection := godi.NewServiceCollection()
    collection.AddSingleton(NewConfig)
    collection.AddSingleton(NewLogger)
    collection.AddScoped(NewRepository)
    collection.AddTransient(NewCommand)

    provider, _ := collection.BuildServiceProvider()
    defer provider.Close()

    b.Run("Singleton", func(b *testing.B) {
        b.ResetTimer()
        for i := 0; i < b.N; i++ {
            _, _ = godi.Resolve[*Config](provider)
        }
    })

    b.Run("Scoped", func(b *testing.B) {
        scope := provider.CreateScope(context.Background())
        defer scope.Close()

        b.ResetTimer()
        for i := 0; i < b.N; i++ {
            _, _ = godi.Resolve[*Repository](scope.ServiceProvider())
        }
    })

    b.Run("Transient", func(b *testing.B) {
        b.ResetTimer()
        for i := 0; i < b.N; i++ {
            _, _ = godi.Resolve[*Command](provider)
        }
    })
}
```

### Concurrent Benchmark

```go
func BenchmarkConcurrentResolution(b *testing.B) {
    collection := godi.NewServiceCollection()
    collection.AddSingleton(NewThreadSafeService)

    provider, _ := collection.BuildServiceProvider()
    defer provider.Close()

    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            _, _ = godi.Resolve[ThreadSafeService](provider)
        }
    })
}
```

## Memory Optimization

### 1. Dispose Scopes Promptly

```go
// ❌ Bad - scope lives too long
func ProcessRequests(provider godi.ServiceProvider, requests []Request) {
    scope := provider.CreateScope(context.Background())
    defer scope.Close()

    for _, req := range requests {
        // All request instances accumulate in scope
        processRequest(scope, req)
    }
}

// ✅ Good - scope per request
func ProcessRequests(provider godi.ServiceProvider, requests []Request) {
    for _, req := range requests {
        func() {
            scope := provider.CreateScope(context.Background())
            defer scope.Close()

            processRequest(scope, req)
        }() // Scope disposed after each request
    }
}
```

### 2. Avoid Service Leaks

```go
// ❌ Bad - holds references to scoped services
type BadSingleton struct {
    cache map[string]ScopedService // Leaks scoped services!
}

// ✅ Good - stores only data
type GoodSingleton struct {
    cache map[string]ServiceData // Only data, not services
}
```

### 3. Pool Expensive Objects

```go
// Object pool for expensive transients
type PooledService struct {
    pool sync.Pool
}

func NewPooledService() *PooledService {
    return &PooledService{
        pool: sync.Pool{
            New: func() interface{} {
                return &ExpensiveObject{}
            },
        },
    }
}

func (s *PooledService) GetObject() *ExpensiveObject {
    return s.pool.Get().(*ExpensiveObject)
}

func (s *PooledService) ReturnObject(obj *ExpensiveObject) {
    obj.Reset() // Clear state
    s.pool.Put(obj)
}
```

## Provider Options for Performance

### Validation Timing

```go
// Development - validate everything upfront
devOptions := &godi.ServiceProviderOptions{
    ValidateOnBuild: true, // Slower startup, catches errors early
}

// Production - defer validation
prodOptions := &godi.ServiceProviderOptions{
    ValidateOnBuild: false,              // Faster startup
    DeferAcyclicVerification: true,      // Defer cycle detection
}
```

### Resolution Monitoring

```go
options := &godi.ServiceProviderOptions{
    OnServiceResolved: func(serviceType reflect.Type, instance interface{}, duration time.Duration) {
        if duration > 100*time.Millisecond {
            log.Warnf("Slow resolution: %s took %v", serviceType, duration)
            metrics.RecordSlowResolution(serviceType, duration)
        }
    },
}
```

## Common Performance Pitfalls

### 1. Excessive Transient Usage

```go
// ❌ Bad - transient for everything
collection.AddTransient(NewLogger)        // Wasteful
collection.AddTransient(NewConfig)        // Wasteful
collection.AddTransient(NewMetrics)       // Wasteful

// ✅ Good - appropriate lifetimes
collection.AddSingleton(NewLogger)        // Shared, stateless
collection.AddSingleton(NewConfig)        // Shared, immutable
collection.AddSingleton(NewMetrics)       // Shared, thread-safe
collection.AddTransient(NewCommand)       // Unique state needed
```

### 2. Deep Dependency Chains

```go
// ❌ Bad - deep nesting
A depends on B
B depends on C
C depends on D
D depends on E
...

// ✅ Good - flatter structure
A depends on B, C
B depends on D
C depends on D
```

### 3. Scope Explosion

```go
// ❌ Bad - nested scopes
scope1 := provider.CreateScope(ctx)
scope2 := scope1.ServiceProvider().CreateScope(ctx)
scope3 := scope2.ServiceProvider().CreateScope(ctx)

// ✅ Good - single scope level
scope := provider.CreateScope(ctx)
```

## Performance Best Practices

### 1. Profile Before Optimizing

```go
import _ "net/http/pprof"

func main() {
    // Enable profiling
    go func() {
        log.Println(http.ListenAndServe("localhost:6060", nil))
    }()

    // Your application
}

// Profile: go tool pprof http://localhost:6060/debug/pprof/profile
```

### 2. Measure Resolution Times

```go
func measureResolution[T any](provider godi.ServiceProvider, name string) (T, error) {
    start := time.Now()
    service, err := godi.Resolve[T](provider)
    duration := time.Since(start)

    log.Printf("Resolution of %s took %v", name, duration)
    return service, err
}
```

### 3. Use Appropriate Lifetimes

| Service Type        | Recommended Lifetime | Reason                     |
| ------------------- | -------------------- | -------------------------- |
| Stateless           | Singleton            | No overhead, safe to share |
| Database Connection | Singleton            | Expensive to create        |
| HTTP Client         | Singleton            | Connection pooling         |
| Request Context     | Scoped               | Request-specific           |
| Transaction         | Scoped               | Must not be shared         |
| Command/Query       | Transient            | Unique parameters          |

### 4. Batch Operations

```go
// ❌ Bad - multiple resolutions
func ProcessItems(provider godi.ServiceProvider, items []Item) {
    for _, item := range items {
        service, _ := godi.Resolve[ItemService](provider)
        service.Process(item)
    }
}

// ✅ Good - single resolution
func ProcessItems(provider godi.ServiceProvider, items []Item) {
    service, _ := godi.Resolve[ItemService](provider)
    service.ProcessBatch(items)
}
```

## Optimization Checklist

- [ ] Use singletons for stateless services
- [ ] Cache service resolutions in hot paths
- [ ] Minimize transient usage
- [ ] Dispose scopes promptly
- [ ] Avoid deep dependency chains
- [ ] Profile before optimizing
- [ ] Monitor resolution times in production
- [ ] Use connection pooling for external resources
- [ ] Batch operations when possible
- [ ] Avoid holding references to scoped services

## Conclusion

godi is designed for performance, but following these guidelines ensures optimal performance:

1. **Choose appropriate lifetimes** - Singleton > Scoped > Transient
2. **Cache resolutions** - Don't resolve in loops
3. **Manage scopes carefully** - Create at the right granularity
4. **Monitor in production** - Track slow resolutions
5. **Profile when needed** - Measure before optimizing

With proper usage, godi adds minimal overhead while providing significant architectural benefits.
