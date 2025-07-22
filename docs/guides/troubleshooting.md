# Troubleshooting & Performance

Common issues and how to fix them, plus performance tips.

## Common Issues

### 1. "Service Not Found" Error

**Problem**: `failed to resolve service: no service registered for type X`

**Solutions**:

```go
// ❌ Forgot to register
service, err := godi.Resolve[UserService](provider)
// Error: no service registered

// ✅ Register the service
var AppModule = godi.NewModule("app",
    godi.AddScoped(NewUserService), // Add this!
)

// ❌ Wrong type
services.AddScoped(func() *UserService { }) // Registered as *UserService
service, err := godi.Resolve[UserService](provider) // Looking for UserService

// ✅ Match the types
services.AddScoped(func() UserService { }) // Return interface
// OR
service, err := godi.Resolve[*UserService](provider) // Request pointer
```

### 2. "Circular Dependency" Error

**Problem**: `circular dependency detected: A -> B -> C -> A`

**Solution**: Break the cycle with lazy loading or provider injection:

```go
// ❌ Circular dependency
type ServiceA struct {
    b ServiceB
}

type ServiceB struct {
    a ServiceA // Circular!
}

// ✅ Solution 1: Use provider
type ServiceA struct {
    provider godi.ServiceProvider
}

func (a *ServiceA) GetB() ServiceB {
    b, _ := godi.Resolve[ServiceB](a.provider)
    return b
}

// ✅ Solution 2: Use interfaces to break cycle
type AInterface interface {
    DoA()
}

type BInterface interface {
    DoB()
}

type ServiceA struct {
    b BInterface // Interface, not concrete
}

type ServiceB struct {
    // Don't reference A here
}
```

### 3. "Scope Already Disposed" Error

**Problem**: Using a scope after closing it

**Solution**: Check your defer statements:

```go
// ❌ Using scope after close
func BadHandler(provider godi.ServiceProvider) {
    scope := provider.CreateScope(ctx)
    scope.Close()

    service, _ := godi.Resolve[Service](scope) // Error!
}

// ✅ Use defer
func GoodHandler(provider godi.ServiceProvider) {
    scope := provider.CreateScope(ctx)
    defer scope.Close() // Closes AFTER function returns

    service, _ := godi.Resolve[Service](scope) // Works!
}
```

### 4. "Captive Dependency" (Singleton using Scoped)

**Problem**: Singleton holds reference to scoped service

**Symptoms**:

- First request works
- Subsequent requests use stale data
- Data leaks between users

**Solution**:

```go
// ❌ Singleton captures scoped
type CacheService struct {
    userContext UserContext // Scoped!
}

func NewCacheService(ctx UserContext) *CacheService {
    return &CacheService{userContext: ctx}
}

// ✅ Use provider to get scoped services
type CacheService struct {
    provider godi.ServiceProvider
}

func NewCacheService(provider godi.ServiceProvider) *CacheService {
    return &CacheService{provider: provider}
}

func (c *CacheService) GetUserData(scope godi.Scope) interface{} {
    ctx, _ := godi.Resolve[UserContext](scope)
    // Use ctx for this request only
}
```

## Performance Issues

### Slow Startup

**Problem**: Application takes too long to start

**Diagnosis**:

```go
options := &godi.ServiceProviderOptions{
    OnServiceResolved: func(serviceType reflect.Type, instance interface{}, duration time.Duration) {
        if duration > 100*time.Millisecond {
            log.Printf("SLOW: %s took %v", serviceType, duration)
        }
    },
}
```

**Solutions**:

1. **Lazy initialization for expensive services**:

```go
// ❌ Connects immediately
func NewSlowService() *SlowService {
    // Takes 5 seconds to connect
    conn := establishConnection()
    return &SlowService{conn: conn}
}

// ✅ Connect on first use
func NewSlowService() *SlowService {
    return &SlowService{
        // Don't connect yet
    }
}

func (s *SlowService) Connect() error {
    if s.conn == nil {
        s.conn = establishConnection()
    }
    return nil
}
```

2. **Parallel initialization**:

```go
// Initialize independent services in parallel
var wg sync.WaitGroup
var dbErr, cacheErr error

wg.Add(2)
go func() {
    defer wg.Done()
    _, dbErr = godi.Resolve[Database](provider)
}()

go func() {
    defer wg.Done()
    _, cacheErr = godi.Resolve[Cache](provider)
}()
```
