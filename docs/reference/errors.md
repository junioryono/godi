# Error Reference

Guide to understanding and handling godi errors.

## Error Types

godi v4 uses typed errors for better error handling:

```go
service, err := godi.Resolve[*UserService](provider)
if err != nil {
    var resErr *godi.ResolutionError
    if errors.As(err, &resErr) {
        log.Printf("Failed to resolve %v: %v",
            resErr.ServiceType, resErr.Cause)
    }
}
```

## Common Errors

### ResolutionError

**When**: Service cannot be resolved

```go
type ResolutionError struct {
    ServiceType reflect.Type
    ServiceKey  any
    Cause       error
}
```

**Common Causes**:

- Service not registered
- Wrong type requested
- Provider/scope disposed

**Solution**:

```go
// Check if service is registered
var AppModule = godi.NewModule("app",
    godi.AddScoped(NewUserService),  // Add missing service
)

// Check type matches
service, err := godi.Resolve[*UserService](provider)  // Use pointer if registered as pointer
```

### CircularDependencyError

**When**: Services depend on each other in a circle

```go
type CircularDependencyError struct {
    Node graph.NodeKey
}
```

**Example**:

```go
// A depends on B, B depends on A
type ServiceA struct { b *ServiceB }
type ServiceB struct { a *ServiceA }
```

**Solutions**:

1. Break the cycle with interfaces:

```go
type ServiceAInterface interface {
    DoA()
}

type ServiceB struct {
    // Don't depend on concrete type
}

type ServiceA struct {
    b *ServiceB
}
```

2. Use provider pattern:

```go
type ServiceA struct {
    provider godi.Provider
}

func (a *ServiceA) GetB() *ServiceB {
    b, _ := godi.Resolve[*ServiceB](a.provider)
    return b
}
```

### LifetimeConflictError

**When**: Invalid lifetime dependencies

```go
type LifetimeConflictError struct {
    ServiceType reflect.Type
    Current     Lifetime
    Requested   Lifetime
}
```

**Rule**: Singleton and Transient cannot depend on Scoped

**Example**:

```go
// ❌ Bad - Singleton depending on Scoped
func NewSingletonService(scoped *ScopedService) *SingletonService

// ✅ Good - Make both the same lifetime
func NewScopedService(scoped *ScopedService) *ScopedService
```

### Disposed Errors

**When**: Using provider/scope after closing

```go
// ErrProviderDisposed
provider.Close()
service, err := godi.Resolve[*Service](provider)  // Error!

// ErrScopeDisposed
scope.Close()
service, err := godi.Resolve[*Service](scope)  // Error!
```

**Solution**: Always use defer for cleanup

```go
scope, _ := provider.CreateScope(ctx)
defer scope.Close()  // Closes after function returns
service, _ := godi.Resolve[*Service](scope)  // Works!
```

### Constructor Errors

**When**: Invalid constructor function

```go
// ErrConstructorNil
collection.AddSingleton(nil)  // Error!

// ErrConstructorNoReturn
func NewService() {  // No return value
    // Error!
}

// ErrConstructorReturnedNil
func NewService() *Service {
    return nil  // Error!
}
```

**Solution**: Valid constructor patterns

```go
// Single return
func NewService() *Service { }

// With error
func NewService() (*Service, error) { }

// Multiple returns
func NewServices() (*Service1, *Service2) { }

// With parameter object
func NewService(params struct{ godi.In; DB Database }) *Service { }

// With result object
func NewServices() struct{ godi.Out; S1 *Service1; S2 *Service2 } { }
```

## Error Handling Patterns

### Basic Error Checking

```go
service, err := godi.Resolve[*Service](provider)
if err != nil {
    log.Printf("Failed to resolve service: %v", err)
    return err
}
```

### Type-Specific Handling

```go
service, err := godi.Resolve[*Service](provider)
if err != nil {
    var resErr *godi.ResolutionError
    if errors.As(err, &resErr) {
        if errors.Is(resErr.Cause, godi.ErrServiceNotFound) {
            // Service not registered
            log.Printf("Service %v not found", resErr.ServiceType)
        }
    }

    var circErr *godi.CircularDependencyError
    if errors.As(err, &circErr) {
        // Circular dependency detected
        log.Printf("Circular dependency: %v", circErr)
    }

    return err
}
```

### Graceful Degradation

```go
// Try primary service, fall back to secondary
primary, err := godi.ResolveKeyed[Cache](provider, "redis")
if err != nil {
    log.Printf("Redis cache unavailable, using memory cache")
    cache, _ := godi.ResolveKeyed[Cache](provider, "memory")
    return cache
}
return primary
```

## Build-Time Validation

Most errors are caught when building:

```go
provider, err := collection.Build()
if err != nil {
    var buildErr *godi.BuildError
    if errors.As(err, &buildErr) {
        log.Printf("Build failed in %s phase: %s",
            buildErr.Phase, buildErr.Details)
    }

    // Common build errors:
    // - Circular dependencies
    // - Lifetime conflicts
    // - Missing dependencies
    // - Invalid constructors

    return nil, err
}
```

## Sentinel Errors

Pre-defined error values:

```go
var (
    // Service resolution
    ErrServiceNotFound         // Service not registered
    ErrServiceTypeNil          // nil type passed
    ErrServiceKeyNil           // nil key for keyed service

    // Lifecycle
    ErrProviderDisposed        // Provider has been closed
    ErrScopeDisposed           // Scope has been closed

    // Constructor
    ErrConstructorNil          // nil constructor function
    ErrConstructorNoReturn     // Constructor returns nothing
    ErrConstructorReturnedNil  // Constructor returned nil

    // Validation
    ErrGroupNameEmpty          // Empty group name
    ErrSingletonNotInitialized // Singleton creation failed
    ErrDescriptorNil           // nil descriptor
)
```

Use with `errors.Is`:

```go
if errors.Is(err, godi.ErrServiceNotFound) {
    // Handle missing service
}
```

## Debugging Tips

### 1. Check Registration

```go
// Verify service is registered
if !collection.Contains(reflect.TypeOf((*UserService)(nil)).Elem()) {
    log.Println("UserService not registered!")
}
```

### 2. Check Lifetime Dependencies

```go
// Singleton → Singleton ✅
// Singleton → Scoped ❌
// Scoped → Scoped ✅
// Scoped → Singleton ✅
```

### 3. Check Circular Dependencies

```go
// Look for A → B → C → A patterns in your constructors
```

### 4. Enable Detailed Logging

```go
// Log all resolutions
type LoggingProvider struct {
    godi.Provider
}

func (l LoggingProvider) Get(t reflect.Type) (any, error) {
    log.Printf("Resolving %v", t)
    return l.Provider.Get(t)
}
```

## Common Scenarios

### Service Not Found

**Error**: `unable to resolve UserService: service not found`

**Check**:

1. Is the service registered in a module?
2. Is the module added to the collection?
3. Is the type correct (pointer vs value)?

### Circular Dependency

**Error**: `circular dependency detected: ServiceA → ServiceB → ServiceA`

**Fix**:

1. Redesign to remove circular dependency
2. Use interfaces to break the cycle
3. Use lazy resolution with provider

### Scope Disposed

**Error**: `scope has been disposed`

**Fix**:

1. Use `defer scope.Close()` immediately after creating
2. Don't store scopes - create as needed
3. Check for concurrent access

### Lifetime Conflict

**Error**: `service Database already registered as Singleton, cannot register as Scoped`

**Fix**:

1. Use consistent lifetimes for the same type
2. Use keyed services for different lifetimes
3. Review your lifetime strategy

## Summary

Most godi errors fall into these categories:

1. **Registration** - Service not registered or wrong type
2. **Lifetime** - Invalid lifetime dependencies
3. **Circular** - Services depending on each other
4. **Disposal** - Using closed scopes/providers
5. **Constructor** - Invalid constructor functions

The typed error system in v4 makes it easy to handle specific error cases and provide meaningful error messages to users.
