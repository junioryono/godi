# Error Handling Reference

This guide covers all error types and error handling patterns in godi.

## Error Categories

godi errors fall into several categories:

1. **Resolution Errors** - Service not found or cannot be resolved
2. **Lifecycle Errors** - Disposed providers or scopes
3. **Registration Errors** - Invalid service registration
4. **Validation Errors** - Invalid constructors or configurations
5. **Circular Dependency Errors** - Circular references between services

## Common Error Values

### Service Resolution Errors

```go
var (
    // Service not registered
    ErrServiceNotFound = errors.New("service not found")

    // Invalid service type (nil)
    ErrInvalidServiceType = errors.New("invalid service type")

    // Service key is nil
    ErrServiceKeyNil = errors.New("service key cannot be nil")
)
```

### Lifecycle Errors

```go
var (
    // Generic disposed error
    ErrDisposed = errors.New("disposed")

    // Scope has been disposed
    ErrScopeDisposed = errors.New("scope has been disposed")

    // Provider has been disposed
    ErrProviderDisposed = errors.New("service provider has been disposed")
)
```

### Constructor Errors

```go
var (
    // Constructor is nil
    ErrNilConstructor = errors.New("constructor cannot be nil")

    // Constructor is not a function
    ErrConstructorNotFunction = errors.New("constructor must be a function")

    // Constructor doesn't return a value
    ErrConstructorNoReturn = errors.New("constructor must return at least one value")

    // Constructor returns too many values
    ErrConstructorTooManyReturns = errors.New("constructor must return at most 2 values")

    // Second return must be error
    ErrConstructorInvalidSecondReturn = errors.New("constructor's second return value must be error")
)
```

## Typed Errors

### ResolutionError

Wraps errors that occur during service resolution:

```go
type ResolutionError struct {
    ServiceType reflect.Type
    ServiceKey  interface{} // nil for non-keyed services
    Cause       error
}

// Example usage
service, err := provider.Resolve(serviceType)
if err != nil {
    var resErr ResolutionError
    if errors.As(err, &resErr) {
        log.Printf("Failed to resolve %s: %v",
            resErr.ServiceType, resErr.Cause)
    }
}
```

### CircularDependencyError

Indicates circular dependencies in service registration:

```go
type CircularDependencyError struct {
    ServiceType reflect.Type
    Chain       []reflect.Type // Dependency chain if available
    DigError    error          // Underlying dig error
}

// Example
provider, err := collection.BuildServiceProvider()
if err != nil {
    var circErr CircularDependencyError
    if errors.As(err, &circErr) {
        log.Printf("Circular dependency: %s", err)
        // Output: Circular dependency detected: A -> B -> C -> A
    }
}
```

### LifetimeConflictError

Service registered with conflicting lifetimes:

```go
type LifetimeConflictError struct {
    ServiceType reflect.Type
    Current     ServiceLifetime
    Requested   ServiceLifetime
}

// Example
collection.AddSingleton(NewLogger)
err := collection.AddScoped(NewLogger) // Error: already registered as Singleton
```

### TimeoutError

Service resolution timeout:

```go
type TimeoutError struct {
    ServiceType reflect.Type
    Timeout     time.Duration
}

// Configure timeout
options := &ServiceProviderOptions{
    ResolutionTimeout: 5 * time.Second,
}
```

## Error Checking Functions

### IsNotFound

Check if a service was not found:

```go
func IsNotFound(err error) bool

// Usage
service, err := provider.Resolve(serviceType)
if godi.IsNotFound(err) {
    // Service not registered
    log.Printf("Service %s not registered", serviceType)
}
```

### IsCircularDependency

Check for circular dependencies:

```go
func IsCircularDependency(err error) bool

// Usage
provider, err := collection.BuildServiceProvider()
if godi.IsCircularDependency(err) {
    // Fix circular dependency
    log.Fatal("Circular dependency detected")
}
```

### IsDisposed

Check if provider/scope is disposed:

```go
func IsDisposed(err error) bool

// Usage
service, err := provider.Resolve(serviceType)
if godi.IsDisposed(err) {
    // Provider or scope was disposed
    log.Println("Cannot resolve from disposed provider")
}
```

### IsTimeout

Check for timeout errors:

```go
func IsTimeout(err error) bool

// Usage
service, err := provider.Resolve(serviceType)
if godi.IsTimeout(err) {
    // Resolution timed out
    log.Printf("Resolution timed out")
}
```

## Error Handling Patterns

### Basic Error Handling

```go
// Simple error check
service, err := godi.Resolve[UserService](provider)
if err != nil {
    return fmt.Errorf("failed to resolve user service: %w", err)
}

// Type-specific handling
service, err := godi.Resolve[UserService](provider)
if err != nil {
    if godi.IsNotFound(err) {
        // Register the service or use a default
        return ErrServiceUnavailable
    }
    if godi.IsDisposed(err) {
        // Provider was disposed
        return ErrSystemShutdown
    }
    // Other errors
    return err
}
```

### Registration Error Handling

```go
// Handle registration errors
err := collection.AddSingleton(constructor)
if err != nil {
    var lifetimeErr LifetimeConflictError
    if errors.As(err, &lifetimeErr) {
        // Service already registered with different lifetime
        log.Printf("Service %s already registered as %s",
            lifetimeErr.ServiceType, lifetimeErr.Current)
    }

    var alreadyErr AlreadyRegisteredError
    if errors.As(err, &alreadyErr) {
        // Use Replace instead
        collection.Replace(Singleton, constructor)
    }
}
```

### Build Error Handling

```go
provider, err := collection.BuildServiceProvider()
if err != nil {
    // Check for specific build errors
    if godi.IsCircularDependency(err) {
        log.Fatal("Fix circular dependencies:", err)
    }

    var regErr RegistrationError
    if errors.As(err, &regErr) {
        log.Printf("Registration failed for %s: %v",
            regErr.ServiceType, regErr.Cause)
    }

    return nil, fmt.Errorf("failed to build provider: %w", err)
}
```

### Graceful Degradation

```go
// Try primary service, fall back to secondary
func GetCache(provider godi.ServiceProvider) Cache {
    // Try Redis cache
    cache, err := godi.ResolveKeyed[Cache](provider, "redis")
    if err == nil {
        return cache
    }

    // Fall back to memory cache
    cache, err = godi.ResolveKeyed[Cache](provider, "memory")
    if err == nil {
        return cache
    }

    // Last resort - no cache
    return &NoOpCache{}
}
```

## Custom Error Types

### Creating Custom Errors

```go
// Service-specific error
type ServiceInitError struct {
    Service string
    Reason  string
}

func (e ServiceInitError) Error() string {
    return fmt.Sprintf("failed to initialize %s: %s", e.Service, e.Reason)
}

// In constructor
func NewDatabase(config *Config) (*Database, error) {
    if config.ConnectionString == "" {
        return nil, ServiceInitError{
            Service: "Database",
            Reason:  "connection string is empty",
        }
    }
    // ...
}
```

### Wrapping Errors

```go
func NewService(dep Dependency) (*Service, error) {
    if err := dep.Validate(); err != nil {
        return nil, fmt.Errorf("dependency validation failed: %w", err)
    }

    service := &Service{dep: dep}
    if err := service.initialize(); err != nil {
        return nil, fmt.Errorf("service initialization failed: %w", err)
    }

    return service, nil
}
```

## Error Recovery

### Panic Recovery

```go
options := &ServiceProviderOptions{
    RecoverFromPanics: true,
}

// Constructor that might panic
func NewRiskyService() *RiskyService {
    if someCondition {
        panic("unexpected condition")
    }
    return &RiskyService{}
}
```

### Validation Options

```go
// Validate all services during build
options := &ServiceProviderOptions{
    ValidateOnBuild: true, // Catch errors early
}

provider, err := collection.BuildServiceProviderWithOptions(options)
if err != nil {
    // All services validated, error indicates real problem
    log.Fatal("Invalid service configuration:", err)
}
```

## Testing Error Scenarios

### Testing Not Found Errors

```go
func TestServiceNotFound(t *testing.T) {
    collection := godi.NewServiceCollection()
    provider, _ := collection.BuildServiceProvider()

    _, err := godi.Resolve[UnregisteredService](provider)

    assert.Error(t, err)
    assert.True(t, godi.IsNotFound(err))
}
```

### Testing Circular Dependencies

```go
func TestCircularDependency(t *testing.T) {
    collection := godi.NewServiceCollection()

    // A depends on B
    collection.AddSingleton(func(b B) A { return A{b: b} })

    // B depends on A (circular!)
    collection.AddSingleton(func(a A) B { return B{a: a} })

    _, err := collection.BuildServiceProvider()

    assert.Error(t, err)
    assert.True(t, godi.IsCircularDependency(err))
}
```

### Testing Disposal Errors

```go
func TestDisposedProvider(t *testing.T) {
    collection := godi.NewServiceCollection()
    collection.AddSingleton(NewService)

    provider, _ := collection.BuildServiceProvider()
    provider.Close()

    _, err := godi.Resolve[Service](provider)

    assert.Error(t, err)
    assert.True(t, godi.IsDisposed(err))
}
```

## Best Practices

### 1. Always Check Errors

```go
// ❌ Bad
service, _ := godi.Resolve[Service](provider)

// ✅ Good
service, err := godi.Resolve[Service](provider)
if err != nil {
    return fmt.Errorf("failed to resolve service: %w", err)
}
```

### 2. Use Error Context

```go
// ❌ Bad
if err != nil {
    return err
}

// ✅ Good
if err != nil {
    return fmt.Errorf("in UserController.GetUser: %w", err)
}
```

### 3. Handle Specific Errors

```go
// ✅ Good - specific handling
if godi.IsNotFound(err) {
    // Specific action for missing service
} else if godi.IsDisposed(err) {
    // Specific action for disposed provider
} else {
    // Generic error handling
}
```

### 4. Fail Fast on Configuration

```go
// ✅ Good - validate during startup
options := &ServiceProviderOptions{
    ValidateOnBuild: true,
}

provider, err := collection.BuildServiceProviderWithOptions(options)
if err != nil {
    log.Fatal("Service configuration error:", err)
}
```

## Summary

- Use error checking functions (`IsNotFound`, `IsCircularDependency`, etc.)
- Handle specific error types appropriately
- Wrap errors with context
- Validate early with `ValidateOnBuild`
- Test error scenarios
- Fail fast on configuration errors
- Provide graceful degradation where appropriate
