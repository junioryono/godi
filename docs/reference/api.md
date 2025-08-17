# API Reference

Complete reference for all godi types and functions.

## Collection

Container for service registrations.

### Creating a Collection

```go
collection := godi.NewCollection()
```

### Registering Services

```go
// Register with lifetime
err := collection.AddSingleton(constructor, options...)
err := collection.AddScoped(constructor, options...)
err := collection.AddTransient(constructor, options...)

// Add modules
err := collection.AddModules(module1, module2, ...)
```

### Building Provider

```go
// Build with defaults
provider, err := collection.Build()

// Build with options
options := &godi.ProviderOptions{
    BuildTimeout: 30 * time.Second,
}
provider, err := collection.BuildWithOptions(options)
```

### Query Methods

```go
// Check if service exists
exists := collection.Contains(reflect.TypeOf((*T)(nil)).Elem())
exists := collection.ContainsKeyed(type, key)

// Get count
count := collection.Count()

// Get all descriptors
descriptors := collection.ToSlice()

// Remove services
collection.Remove(reflect.TypeOf((*T)(nil)).Elem())
collection.RemoveKeyed(type, key)
```

## Provider

Main dependency injection container.

### Resolution Methods (Generic)

```go
// Resolve by type
service, err := godi.Resolve[T](provider)
service, err := godi.Resolve[T](scope)

// Resolve by key
service, err := godi.ResolveKeyed[T](provider, "key")

// Resolve group
services, err := godi.ResolveGroup[T](provider, "group")

// Must variants (panic on error)
service := godi.MustResolve[T](provider)
service := godi.MustResolveKeyed[T](provider, "key")
services := godi.MustResolveGroup[T](provider, "group")
```

### Resolution Methods (Reflection)

```go
// Not recommended - use generic methods above
service, err := provider.Get(reflect.TypeOf((*T)(nil)).Elem())
service, err := provider.GetKeyed(type, key)
services, err := provider.GetGroup(type, "group")
```

### Scope Management

```go
// Create scope
scope, err := provider.CreateScope(context.Background())

// Provider info
id := provider.ID()

// Cleanup
err := provider.Close()
```

## Scope

Isolated service lifetime boundary.

```go
// Scope implements Provider interface
service, err := godi.Resolve[T](scope)

// Get context and provider
ctx := scope.Context()
provider := scope.Provider()

// Create nested scope
nestedScope, err := scope.CreateScope(ctx)

// Cleanup
err := scope.Close()

// Get scope from context
scope, ok := godi.FromContext(ctx)
```

## Modules

Organize related services.

### Creating Modules

```go
var MyModule = godi.NewModule("name",
    godi.AddSingleton(constructor),
    godi.AddScoped(constructor),
    godi.AddTransient(constructor),
    OtherModule,  // Include other modules
)
```

### Module Builders

```go
// These return ModuleOption for use in NewModule
godi.AddSingleton(constructor, opts...)
godi.AddScoped(constructor, opts...)
godi.AddTransient(constructor, opts...)
```

## Registration Options

### Name Option

Register named/keyed services:

```go
godi.AddSingleton(NewService, godi.Name("primary"))

// Resolve with key
service, _ := godi.ResolveKeyed[Service](provider, "primary")
```

### Group Option

Add services to groups:

```go
godi.AddSingleton(NewValidator, godi.Group("validators"))

// Resolve group
validators, _ := godi.ResolveGroup[Validator](provider, "validators")
```

### As Option

Register as interface:

```go
godi.AddSingleton(NewRedisCache, godi.As(new(Cache)))

// Resolve as interface
cache, _ := godi.Resolve[Cache](provider)
```

### Combining Options

```go
godi.AddSingleton(NewService,
    godi.Name("primary"),
    godi.As(new(IService)),
)
```

## Parameter Objects

### Input Parameters (In)

```go
type ServiceParams struct {
    godi.In

    // Required dependency
    DB Database

    // Optional dependency
    Cache Cache `optional:"true"`

    // Named dependency
    Primary Database `name:"primary"`

    // Group dependency
    Validators []Validator `group:"validators"`
}

func NewService(params ServiceParams) *Service {
    // Use params.DB, params.Cache, etc.
}
```

### Output Parameters (Out)

```go
type ServiceBundle struct {
    godi.Out

    // Register as UserService
    UserService UserService

    // Register with name
    OrderService OrderService `name:"orders"`

    // Add to group
    AuthHandler http.Handler `group:"handlers"`
}

func NewServices(db Database) ServiceBundle {
    return ServiceBundle{
        UserService:  &userService{db},
        OrderService: &orderService{db},
        AuthHandler:  &authHandler{},
    }
}
```

## Disposal Interface

```go
// Implement for automatic cleanup
type Disposable interface {
    Close() error
}

// Called automatically when scope/provider closes
```

## Provider Options

```go
type ProviderOptions struct {
    // Timeout for building provider
    BuildTimeout time.Duration
}

options := &godi.ProviderOptions{
    BuildTimeout: 30 * time.Second,
}

provider, err := collection.BuildWithOptions(options)
```

## Lifetimes

```go
const (
    Singleton Lifetime = iota  // One instance forever
    Scoped                     // One instance per scope
    Transient                  // New instance every time
)
```

## Error Types

```go
// Resolution error
type ResolutionError struct {
    ServiceType reflect.Type
    ServiceKey  any
    Cause       error
}

// Circular dependency
type CircularDependencyError struct {
    Node graph.NodeKey
}

// Lifetime conflict
type LifetimeConflictError struct {
    ServiceType reflect.Type
    Current     Lifetime
    Requested   Lifetime
}

// Validation error
type ValidationError struct {
    ServiceType reflect.Type
    Cause       error
}

// Build error
type BuildError struct {
    Phase   string
    Details string
    Cause   error
}

// Disposal error
type DisposalError struct {
    Context string
    Errors  []error
}
```

## Sentinel Errors

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
    ErrSingletonNotInitialized // Singleton not created
)
```

## Complete Example

```go
package main

import (
    "context"
    "github.com/junioryono/godi/v4"
)

// Services
type Logger struct{}
func NewLogger() *Logger { return &Logger{} }

type Database struct{ logger *Logger }
func NewDatabase(logger *Logger) *Database {
    return &Database{logger: logger}
}

type UserService struct{ db *Database }
func NewUserService(db *Database) *UserService {
    return &UserService{db: db}
}

func main() {
    // Create module
    appModule := godi.NewModule("app",
        godi.AddSingleton(NewLogger),
        godi.AddSingleton(NewDatabase),
        godi.AddScoped(NewUserService),
    )

    // Build provider
    collection := godi.NewCollection()
    collection.AddModules(appModule)

    provider, err := collection.Build()
    if err != nil {
        panic(err)
    }
    defer provider.Close()

    // Create scope
    scope, err := provider.CreateScope(context.Background())
    if err != nil {
        panic(err)
    }
    defer scope.Close()

    // Resolve service
    service, err := godi.Resolve[*UserService](scope)
    if err != nil {
        panic(err)
    }

    // Use service
    service.DoWork()
}
```
