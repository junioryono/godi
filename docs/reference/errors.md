# Error Reference

Quick reference for common godi errors and their solutions.

## Error Types

godi v4 uses typed errors for better error handling:

```go
// Example error handling
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

**Type**: `ResolutionError`
**Message**: `unable to resolve Type: cause`

**Cause**: Trying to resolve a service that wasn't registered.

**Solution**:

```go
// ❌ Problem
service, err := godi.Resolve[UserService](provider)
// Error: no service registered for type: UserService

// ✅ Solution - Register the service
var AppModule = godi.NewModule("app",
    godi.AddScoped(NewUserService), // Add this!
)
```

### CircularDependencyError

**Type**: `CircularDependencyError`
**Message**: `circular dependency detected: Node`

**Cause**: Services depend on each other in a circle.

**Solution**:

```go
// ❌ Problem
type A struct { b B }
type B struct { a A } // Circular!

// ✅ Solution 1 - Use interfaces
type A struct { b BInterface }
type B struct { /* no ref to A */ }

// ✅ Solution 2 - Use provider
type A struct { provider godi.ServiceProvider }
// Resolve B when needed
```

### Disposed Errors

**Message**: `scope has been disposed` or `service provider has been disposed`

**Cause**: Using a scope after closing it.

**Solution**:

```go
// ❌ Problem
scope.Close()
service, _ := godi.Resolve[Service](scope) // Error!

// ✅ Solution - Use defer
defer scope.Close()
service, _ := godi.Resolve[Service](scope) // Works!
```

### LifetimeConflictError

**Type**: `LifetimeConflictError`
**Message**: `service Type already registered as Singleton, cannot register as Scoped`

**Cause**: Invalid lifetime dependencies (e.g., Singleton depending on Scoped).

**Solution**:

```go
// ❌ Problem - Singleton can't depend on Scoped
func NewSingleton(scoped *ScopedService) *Singleton { }

// ✅ Solution - Make both the same lifetime
godi.AddScoped(NewSingleton) // Change to scoped
// OR
godi.AddSingleton(NewScopedService) // Change dependency to singleton
```

### Constructor Errors

**Common Issues**:

```go
// ❌ No return value
func NewService() { } // ErrConstructorNoReturn

// ❌ Returns nil
func NewService() *Service { 
    return nil // ErrConstructorReturnedNil
}

// ✅ Correct patterns
func NewService() *Service { }
func NewService() (*Service, error) { }
func NewService() (Service1, Service2) { } // Multi-return
func NewService() (Service1, Service2, error) { } // With error
```

## Error Handling Patterns

```go
// Using errors.As for typed errors
service, err := godi.Resolve[*Service](provider)
if err != nil {
    var resErr *godi.ResolutionError
    if errors.As(err, &resErr) {
        if errors.Is(resErr.Cause, godi.ErrServiceNotFound) {
            // Service not registered
        }
    }
    
    var circErr *godi.CircularDependencyError
    if errors.As(err, &circErr) {
        // Handle circular dependency
    }
    
    var lifetimeErr *godi.LifetimeConflictError
    if errors.As(err, &lifetimeErr) {
        // Handle lifetime conflict
    }
}
```

## Quick Diagnosis

| Error               | Check This                          |
| ------------------- | ----------------------------------- |
| Service not found   | Is it registered in a module?       |
| Circular dependency | Do services reference each other?   |
| Scope disposed      | Are you using defer for Close()?    |
| Lifetime conflict   | Same type with different lifetimes? |
| Constructor error   | Does it return a value?             |

## Build-Time Validation

Most errors are caught when building the provider:

```go
provider, err := collection.Build()
if err != nil {
    // Handle build errors:
    // - Circular dependencies
    // - Lifetime conflicts
    // - Missing dependencies
    log.Fatal("Build failed:", err)
}
```

## Sentinel Errors

Common pre-defined errors:

```go
var (
    ErrServiceNotFound         // Service not registered
    ErrServiceTypeNil          // nil type passed
    ErrServiceKeyNil           // nil key passed
    ErrProviderDisposed        // Provider closed
    ErrScopeDisposed           // Scope closed
    ErrConstructorNil          // nil constructor
    ErrConstructorNoReturn     // No return values
    ErrConstructorReturnedNil  // Returned nil
    ErrGroupNameEmpty          // Empty group name
    ErrSingletonNotInitialized // Singleton not built
)
```
