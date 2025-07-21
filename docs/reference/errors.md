# Error Reference

Quick reference for common godi errors and their solutions.

## Common Errors

### ServiceNotFoundError

**Message**: `no service registered for type: X`

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

**Message**: `circular dependency detected: A -> B -> C -> A`

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

### ScopeDisposedError

**Message**: `scope has been disposed`

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

**Message**: `service X already registered as Singleton, cannot register as Scoped`

**Cause**: Registering same type with different lifetimes.

**Solution**:

```go
// ❌ Problem
godi.AddSingleton(NewLogger)
godi.AddScoped(NewLogger) // Error!

// ✅ Solution - Use consistent lifetime
godi.AddSingleton(NewLogger)
// OR use Replace
services.Replace(godi.Scoped, NewLogger)
```

### ConstructorError

**Message**: Various constructor-related errors

**Common Issues**:

```go
// ❌ No return value
func NewService() { } // Must return something

// ❌ Only returns error
func NewService() error { } // Must return (Service, error)

// ❌ Too many returns
func NewService() (S1, S2, S3) { } // Max 2 returns

// ✅ Correct patterns
func NewService() Service { }
func NewService() (Service, error) { }
```

## Error Checking Helpers

```go
// Check if service not found
if godi.IsNotFound(err) {
    // Handle missing service
}

// Check for circular dependency
if godi.IsCircularDependency(err) {
    // Fix dependency cycle
}

// Check if disposed
if godi.IsDisposed(err) {
    // Scope or provider was closed
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

Most errors are caught at build time when using:

```go
options := &godi.ServiceProviderOptions{
    ValidateOnBuild: true,
}
```
