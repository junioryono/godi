# Troubleshooting Guide

This guide helps you diagnose and fix common issues when using godi.

## Common Issues

### Service Not Found

**Error Message:**

```
unable to resolve service of type *main.UserService: service not found
```

**Causes and Solutions:**

1. **Service not registered**

   ```go
   // ❌ Forgot to register
   provider, _ := collection.BuildServiceProvider()
   service, err := godi.Resolve[UserService](provider) // Error!

   // ✅ Register the service
   collection.AddScoped(NewUserService)
   provider, _ := collection.BuildServiceProvider()
   service, err := godi.Resolve[UserService](provider) // Works!
   ```

2. **Wrong type requested**

   ```go
   // Registered as interface
   collection.AddScoped(func() UserService { return &userService{} })

   // ❌ Requesting concrete type
   service, err := godi.Resolve[*userService](provider) // Error!

   // ✅ Request interface type
   service, err := godi.Resolve[UserService](provider) // Works!
   ```

3. **Keyed service without key**

   ```go
   // Registered with key
   collection.AddSingleton(NewRedisCache, godi.Name("redis"))

   // ❌ Resolving without key
   cache, err := godi.Resolve[Cache](provider) // Error!

   // ✅ Use keyed resolution
   cache, err := godi.ResolveKeyed[Cache](provider, "redis") // Works!
   ```

### Circular Dependencies

**Error Message:**

```
circular dependency detected: A -> B -> C -> A
```

**Solution:**

1. **Break the cycle with interfaces**

   ```go
   // ❌ Circular dependency
   type ServiceA struct {
       b *ServiceB
   }

   type ServiceB struct {
       a *ServiceA // Circular!
   }

   // ✅ Use interface or event bus
   type ServiceA struct {
       events EventBus
   }

   type ServiceB struct {
       events EventBus
   }
   ```

2. **Use lazy resolution**

   ```go
   type ServiceA struct {
       provider godi.ServiceProvider
   }

   func (a *ServiceA) GetB() ServiceB {
       b, _ := godi.Resolve[ServiceB](a.provider)
       return b
   }
   ```

### Disposed Provider/Scope

**Error Message:**

```
service provider has been disposed
```

**Common Causes:**

1. **Using provider after Close()**

   ```go
   provider, _ := collection.BuildServiceProvider()
   provider.Close()

   // ❌ Using after close
   service, err := godi.Resolve[Service](provider) // Error!
   ```

2. **Storing and using old scopes**

   ```go
   var globalScope godi.Scope

   func BadHandler(provider godi.ServiceProvider) {
       scope := provider.CreateScope(ctx)
       globalScope = scope // ❌ Storing scope
       scope.Close()
   }

   func LaterUse() {
       // ❌ Using closed scope
       service, _ := godi.Resolve[Service](globalScope.ServiceProvider())
   }
   ```

### Lifetime Conflicts

**Error Message:**

```
service Logger already registered as Singleton, cannot register as Scoped
```

**Solution:**

```go
// ❌ Conflicting lifetimes
collection.AddSingleton(NewLogger)
collection.AddScoped(NewLogger) // Error!

// ✅ Option 1: Use consistent lifetime
collection.AddSingleton(NewLogger)

// ✅ Option 2: Use Replace
collection.AddSingleton(NewLogger)
collection.Replace(godi.Scoped, NewLogger)

// ✅ Option 3: Use different types
collection.AddSingleton(NewGlobalLogger)
collection.AddScoped(NewRequestLogger)
```

### Missing Dependencies

**Error Message:**

```
missing type: *main.Database
```

**Diagnosis:**

```go
// Check what depends on Database
func NewUserRepository(db *Database) *UserRepository {
    // This requires Database to be registered
}

// ✅ Register all dependencies
collection.AddSingleton(NewDatabase)
collection.AddScoped(NewUserRepository)
```

### Constructor Errors

**Error Message:**

```
constructor must be a function
```

**Common Mistakes:**

1. **Passing instance instead of constructor**

   ```go
   // ❌ Passing instance
   logger := NewLogger()
   collection.AddSingleton(logger) // Error!

   // ✅ Pass constructor
   collection.AddSingleton(NewLogger)

   // ✅ Or wrap instance
   collection.AddSingleton(func() Logger { return logger })
   ```

2. **Constructor returns nothing**

   ```go
   // ❌ No return value
   func InitializeService() {
       // setup code
   }
   collection.AddSingleton(InitializeService) // Error!

   // ✅ Return the service
   func NewService() *Service {
       return &Service{}
   }
   ```

## Debugging Techniques

### 1. Enable Validation

```go
options := &godi.ServiceProviderOptions{
    ValidateOnBuild: true, // Catch errors early
}

provider, err := collection.BuildServiceProviderWithOptions(options)
if err != nil {
    log.Printf("Validation error: %v", err)
}
```

### 2. Add Resolution Logging

```go
options := &godi.ServiceProviderOptions{
    OnServiceResolved: func(serviceType reflect.Type, instance interface{}, duration time.Duration) {
        log.Printf("Resolved %s in %v", serviceType, duration)
    },
    OnServiceError: func(serviceType reflect.Type, err error) {
        log.Printf("Failed to resolve %s: %v", serviceType, err)
    },
}
```

### 3. Check Service Registration

```go
// Debug helper
func debugRegistrations(collection godi.ServiceCollection) {
    descriptors := collection.ToSlice()

    log.Println("Registered services:")
    for _, desc := range descriptors {
        log.Printf("- %s (%s)", desc.ServiceType, desc.Lifetime)
    }
}
```

### 4. Trace Dependency Chain

```go
// When debugging "missing type" errors
func traceDependencies() {
    // Start from the failing service
    // UserController -> UserService -> UserRepository -> Database

    log.Println("Dependency chain:")
    log.Println("UserController requires: UserService")
    log.Println("UserService requires: UserRepository, Logger")
    log.Println("UserRepository requires: Database")
    log.Println("Database requires: Config")
}
```

## Performance Issues

### Slow Resolution

**Symptoms:**

- Application startup is slow
- Request handling is sluggish

**Diagnosis:**

```go
options := &godi.ServiceProviderOptions{
    OnServiceResolved: func(serviceType reflect.Type, instance interface{}, duration time.Duration) {
        if duration > 100*time.Millisecond {
            log.Printf("SLOW: %s took %v", serviceType, duration)
        }
    },
}
```

**Solutions:**

1. Check for expensive constructors
2. Use singletons for expensive services
3. Implement lazy initialization
4. Profile the application

### Memory Leaks

**Symptoms:**

- Memory usage grows over time
- Scoped services not being garbage collected

**Common Causes:**

1. **Not closing scopes**

   ```go
   // ❌ Leak - scope never closed
   func LeakyHandler(provider godi.ServiceProvider) {
       scope := provider.CreateScope(ctx)
       // Missing: defer scope.Close()
   }
   ```

2. **Singleton holding scoped references**
   ```go
   // ❌ Singleton captures scoped service
   type Cache struct {
       services []ScopedService // Prevents GC
   }
   ```

## Testing Issues

### Mocks Not Being Used

**Problem:** Real services used instead of mocks

**Solution:**

```go
// ✅ Register mocks before real services
collection.AddSingleton(func() Database { return &MockDatabase{} })
collection.AddScoped(NewUserRepository) // Uses mock database

// ❌ Wrong order
collection.AddScoped(NewUserRepository) // Might be cached
collection.AddSingleton(func() Database { return &MockDatabase{} })
```

### Test Isolation

**Problem:** Tests affecting each other

**Solution:**

```go
func TestIsolated(t *testing.T) {
    // Create fresh container for each test
    collection := godi.NewServiceCollection()
    // Register services
    provider, _ := collection.BuildServiceProvider()
    defer provider.Close()

    // Test runs in isolation
}
```

## Error Patterns

### Check Error Types

```go
service, err := godi.Resolve[Service](provider)
if err != nil {
    switch {
    case godi.IsNotFound(err):
        log.Println("Service not registered")

    case godi.IsCircularDependency(err):
        log.Println("Fix circular dependency")

    case godi.IsDisposed(err):
        log.Println("Provider/scope disposed")

    default:
        log.Printf("Unknown error: %v", err)
    }
}
```

### Wrap Errors with Context

```go
func NewService(db Database) (*Service, error) {
    if err := db.Ping(); err != nil {
        return nil, fmt.Errorf("database connection failed: %w", err)
    }

    return &Service{db: db}, nil
}
```

## Prevention Strategies

### 1. Consistent Registration Pattern

```go
// main.go or composition root
func configureServices(collection godi.ServiceCollection) error {
    // Infrastructure
    if err := collection.AddModules(InfrastructureModule); err != nil {
        return fmt.Errorf("infrastructure module: %w", err)
    }

    // Business logic
    if err := collection.AddModules(DomainModule); err != nil {
        return fmt.Errorf("domain module: %w", err)
    }

    // API
    if err := collection.AddModules(APIModule); err != nil {
        return fmt.Errorf("api module: %w", err)
    }

    return nil
}
```

### 2. Validate Early

```go
func main() {
    collection := godi.NewServiceCollection()

    if err := configureServices(collection); err != nil {
        log.Fatal("Failed to configure services:", err)
    }

    options := &godi.ServiceProviderOptions{
        ValidateOnBuild: true,
    }

    provider, err := collection.BuildServiceProviderWithOptions(options)
    if err != nil {
        log.Fatal("Failed to build provider:", err)
    }
    defer provider.Close()
}
```

### 3. Document Dependencies

```go
// Package user provides user management services.
//
// Dependencies:
// - Database (singleton)
// - Logger (singleton)
// - EmailService (singleton)
// - CacheService (singleton, optional)
//
// Exports:
// - UserService (scoped)
// - UserRepository (scoped)
var UserModule = godi.Module("user", /* ... */)
```

## Getting Help

If you're still stuck:

1. **Check the examples** in the repository
2. **Review the error messages** - they often contain the solution
3. **Enable debug logging** to trace the issue
4. **Create a minimal reproduction** of the problem
5. **Ask on GitHub Discussions** with:
   - The error message
   - Relevant code snippets
   - What you've tried
   - godi version

## Quick Reference

| Error               | Likely Cause              | Solution                           |
| ------------------- | ------------------------- | ---------------------------------- |
| Service not found   | Not registered            | Add service registration           |
| Circular dependency | A->B->A                   | Break cycle with interface         |
| Disposed error      | Using closed scope        | Create new scope                   |
| Missing type        | Dependency not registered | Register all dependencies          |
| Constructor error   | Wrong function signature  | Check constructor requirements     |
| Lifetime conflict   | Multiple registrations    | Use consistent lifetime or Replace |
