# Dependency Resolution

Understanding how godi resolves dependencies helps you build better applications.

## How Resolution Works

### The Dependency Graph

When you call `Build()`, godi:

1. **Analyzes** all registered constructors
2. **Builds** a dependency graph
3. **Validates** for cycles and lifetime violations
4. **Sorts** services in dependency order
5. **Creates** singletons at build time

```go
// Given these registrations:
services.AddSingleton(NewLogger)        // No dependencies
services.AddSingleton(NewConfig)        // No dependencies
services.AddSingleton(NewDatabase)      // Needs Config, Logger
services.AddSingleton(NewCache)         // Needs Config
services.AddScoped(NewUserRepository)   // Needs Database, Cache
services.AddScoped(NewUserService)      // Needs UserRepository, Logger

// godi creates this dependency order:
// 1. Logger, Config (no dependencies, can be parallel)
// 2. Database, Cache (depend on level 1)
// 3. UserRepository (depends on level 2)
// 4. UserService (depends on level 3)
```

### Resolution Process

When you request a service:

```go
userService := godi.MustResolve[UserService](provider)
```

godi follows this process:

1. **Check cache** - Is it already created?
2. **Find descriptor** - Get the service registration
3. **Resolve dependencies** - Recursively resolve each dependency
4. **Create instance** - Call the constructor
5. **Cache if needed** - Based on lifetime
6. **Return instance**

## Resolution Methods

### Basic Resolution

```go
// Resolve with error handling
service, err := godi.Resolve[MyService](provider)
if err != nil {
    // Handle error - service not found, etc.
}

// Resolve or panic
service := godi.MustResolve[MyService](provider)
```

### Keyed Resolution

```go
// Resolve named service
cache := godi.MustResolveKeyed[Cache](provider, "redis")

// With error handling
cache, err := godi.ResolveKeyed[Cache](provider, "redis")
```

### Group Resolution

```go
// Resolve all services in a group
handlers := godi.MustResolveGroup[Handler](provider, "http")

// With error handling
handlers, err := godi.ResolveGroup[Handler](provider, "http")
```

## Automatic Dependency Injection

### Constructor Analysis

godi automatically analyzes constructor parameters:

```go
func NewUserService(
    db Database,           // godi will inject Database
    cache Cache,           // godi will inject Cache
    logger Logger,         // godi will inject Logger
    config *AppConfig,     // godi will inject *AppConfig
) UserService {
    return &userService{
        db:     db,
        cache:  cache,
        logger: logger,
        config: config,
    }
}

// Just register it - godi handles the rest
services.AddScoped(NewUserService)
```

### Recursive Resolution

Dependencies are resolved recursively:

```go
// A depends on B
// B depends on C
// C depends on D

func NewA(b B) A { return &a{b: b} }
func NewB(c C) B { return &b{c: c} }
func NewC(d D) C { return &c{d: d} }
func NewD() D { return &d{} }

// When you resolve A:
a := godi.MustResolve[A](provider)

// godi automatically:
// 1. Sees A needs B
// 2. Sees B needs C
// 3. Sees C needs D
// 4. Creates D (no dependencies)
// 5. Creates C with D
// 6. Creates B with C
// 7. Creates A with B
```

## Circular Dependencies

### Detection

godi detects circular dependencies at build time:

```go
func NewServiceA(b ServiceB) ServiceA {
    return &serviceA{b: b}
}

func NewServiceB(a ServiceA) ServiceB {
    return &serviceB{a: a}
}

services.AddSingleton(NewServiceA)
services.AddSingleton(NewServiceB)

provider, err := services.Build()
// Error: Circular dependency detected: ServiceA -> ServiceB -> ServiceA
```

### Breaking Circles

#### Option 1: Lazy Resolution with Provider

```go
type ServiceA interface {
    DoWork()
}

type serviceA struct {
    provider godi.Provider
}

func NewServiceA(provider godi.Provider) ServiceA {
    return &serviceA{provider: provider}
}

func (a *serviceA) DoWork() {
    // Resolve B only when needed
    b := godi.MustResolve[ServiceB](a.provider)
    b.Process()
}
```

#### Option 2: Setter Injection

```go
type ServiceA interface {
    Process()
}

type serviceA struct {
    b ServiceB
}

func NewServiceA() ServiceA {
    return &serviceA{}
}

func (a *serviceA) SetServiceB(b ServiceB) {
    a.b = b
}

// After resolution
a := godi.MustResolve[ServiceA](provider)
b := godi.MustResolve[ServiceB](provider)
a.SetServiceB(b)
```

#### Option 3: Refactor Design

```go
// Extract shared functionality
type SharedService interface {
    SharedMethod()
}

func NewSharedService() SharedService {
    return &sharedService{}
}

func NewServiceA(shared SharedService) ServiceA {
    return &serviceA{shared: shared}
}

func NewServiceB(shared SharedService) ServiceB {
    return &serviceB{shared: shared}
}
```

## Lifetime Validation

### The Problem

Singletons can't depend on scoped services:

```go
// ❌ This will fail at build time
func NewSingletonCache(ctx RequestContext) Cache { // RequestContext is scoped
    return &cache{context: ctx}
}

services.AddSingleton(NewSingletonCache)
services.AddScoped(NewRequestContext)

provider, err := services.Build()
// Error: Singleton service Cache cannot depend on Scoped service RequestContext
```

### Why It Matters

```go
// If this were allowed:

// Request 1
scope1 := provider.CreateScope(ctx)
cache := godi.MustResolve[Cache](scope1) // Singleton created with scope1's context

// Request 2
scope2 := provider.CreateScope(ctx)
cache2 := godi.MustResolve[Cache](scope2) // Same singleton, still has scope1's context!
// Bug: cache is using wrong request context
```

### Valid Dependency Chains

```go
// ✅ Singleton -> Singleton
services.AddSingleton(NewLogger)
services.AddSingleton(NewDatabase) // Can depend on Logger

// ✅ Scoped -> Singleton
services.AddSingleton(NewDatabase)
services.AddScoped(NewRepository) // Can depend on Database

// ✅ Scoped -> Scoped
services.AddScoped(NewRequestContext)
services.AddScoped(NewUserService) // Can depend on RequestContext

// ✅ Transient -> Any
services.AddTransient(NewTempHandler) // Can depend on anything
```

## Resolution Context

### Using Scopes

```go
// Root provider resolution
logger := godi.MustResolve[Logger](provider) // From root

// Scoped resolution
scope, _ := provider.CreateScope(context.Background())
defer scope.Close()

service := godi.MustResolve[MyService](scope) // From scope
```

### Context Integration

```go
// Store scope in context
func Middleware(provider godi.Provider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        scope, _ := provider.CreateScope(r.Context())
        defer scope.Close()

        // Pass scope through context
        r = r.WithContext(scope.Context())

        // ... handle request
    }
}

// Retrieve scope from context
func DeepFunction(ctx context.Context) {
    scope, _ := godi.FromContext(ctx)
    service := godi.MustResolve[MyService](scope)
}
```

## Performance Considerations

### Caching Strategy

```go
// Singleton: Resolved once, cached forever
// Cost: O(1) after first resolution
logger := godi.MustResolve[Logger](provider)

// Scoped: Resolved once per scope, cached in scope
// Cost: O(1) within same scope
ctx := godi.MustResolve[RequestContext](scope)

// Transient: Never cached, always created
// Cost: O(n) where n is dependency depth
temp := godi.MustResolve[TempHandler](provider)
```

### Build-Time Optimization

```go
// Singletons are created at build time
services.AddSingleton(NewExpensiveService) // 5 second initialization

start := time.Now()
provider, _ := services.Build() // Takes 5 seconds
fmt.Printf("Build took: %v\n", time.Since(start))

// Resolution is instant
start = time.Now()
service := godi.MustResolve[ExpensiveService](provider) // Instant
fmt.Printf("Resolution took: %v\n", time.Since(start))
```

## Error Handling

### Resolution Errors

```go
// Service not found
service, err := godi.Resolve[UnknownService](provider)
if err != nil {
    // err: service not found
}

// Keyed service not found
cache, err := godi.ResolveKeyed[Cache](provider, "unknown")
if err != nil {
    // err: keyed service 'unknown' not found
}

// Constructor error
func NewDatabase() (Database, error) {
    return nil, errors.New("connection failed")
}
services.AddSingleton(NewDatabase)
provider, err := services.Build()
// err: failed to create singleton: connection failed
```

## Best Practices

1. **Let godi handle dependencies** - Don't manually wire services
2. **Validate at build time** - Catch issues early
3. **Use appropriate lifetimes** - Prevent lifetime violations
4. **Avoid circular dependencies** - Refactor if detected
5. **Cache expensive services** - Use singleton or scoped
6. **Handle resolution errors** - Don't always use MustResolve

## Next Steps

- Explore [Scopes & Isolation](scopes-isolation.md)
- Learn about [Resource Management](resource-management.md)
- Understand [Modules](modules.md)
