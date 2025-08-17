# API Reference

Quick reference for all godi types and functions.

## Core Types

### Collection

Container for service registrations.

```go
// Create
collection := godi.NewCollection()

// Register services
collection.AddSingleton(constructor, options...)
collection.AddScoped(constructor, options...)
collection.AddTransient(constructor, options...)

// Add modules
err := collection.AddModules(module1, module2)

// Build provider
provider, err := collection.Build()
provider, err := collection.BuildWithOptions(options)

// Query methods
count := collection.Count()
exists := collection.HasService(reflect.TypeOf((*T)(nil)).Elem())
exists := collection.HasKeyedService(type, key)

// Remove services
collection.Remove(reflect.TypeOf((*T)(nil)).Elem())
collection.RemoveKeyed(type, key)

// Get all descriptors
descriptors := collection.ToSlice()
```

### Provider

Resolves services and creates scopes.

```go
// Generic helpers (recommended)
service, err := godi.Resolve[T](provider)
service, err := godi.ResolveKeyed[T](provider, key)
services, err := godi.ResolveGroup[T](provider, "group")

// Must variants (panic on error)
service := godi.MustResolve[T](provider)
service := godi.MustResolveKeyed[T](provider, key)
services := godi.MustResolveGroup[T](provider, "group")

// Direct resolution (use generic helpers instead)
service, err := provider.Get(reflect.TypeOf((*T)(nil)).Elem())
service, err := provider.GetKeyed(type, key)
services, err := provider.GetGroup(type, "group")

// Create scopes
scope, err := provider.CreateScope(context)

// Provider info
id := provider.ID()

// Cleanup
err := provider.Close()
```

### Scope

Isolated service lifetime boundary.

```go
// Scope implements Provider interface
service, err := godi.Resolve[T](scope)
service, err := scope.Get(reflect.TypeOf((*T)(nil)).Elem())

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

Organize service registrations.

```go
// Create module
var MyModule = godi.NewModule("name",
    godi.AddSingleton(constructor),
    godi.AddScoped(constructor),
    OtherModule, // Include other modules
)

// Module builder functions
godi.AddSingleton(constructor, opts...)
godi.AddScoped(constructor, opts...)
godi.AddTransient(constructor, opts...)
godi.AddDecorator(decorator, opts...)
```

## Registration Options

### Lifetime Options

```go
// Named services
godi.AddSingleton(NewService, godi.Name("primary"))
service, _ := godi.ResolveKeyed[Service](provider, "primary")

// Service groups
godi.AddSingleton(NewValidator, godi.Group("validators"))
validators, _ := godi.ResolveGroup[Validator](provider, "validators")

// Register as interface
godi.AddSingleton(NewRedisCache, godi.As(new(Cache)))
cache, _ := godi.Resolve[Cache](provider)

// Combine options
godi.AddSingleton(NewService,
    godi.Name("primary"),
    godi.As(new(IService)))
```

### Decorators

Wrap services with additional behavior.

```go
// Define decorator
func LoggingDecorator(service Service, logger Logger) Service {
    return &loggingService{inner: service, logger: logger}
}

// Register
collection.Decorate(LoggingDecorator)
```

## Parameter Objects

### Input Parameters (In)

```go
type ServiceParams struct {
    godi.In

    DB       Database
    Logger   Logger
    Cache    Cache    `optional:"true"`
    Primary  Database `name:"primary"`
    Handlers []Handler `group:"handlers"`
}

func NewService(params ServiceParams) *Service {
    // Use params.DB, params.Logger, etc.
}
```

### Output Parameters (Out)

```go
type ServiceBundle struct {
    godi.Out

    UserService  UserService
    OrderService OrderService  `name:"orders"`
    AuthHandler  http.Handler  `group:"handlers"`
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

Services can implement this for cleanup:

```go
// Disposable interface for resources that need cleanup
type Disposable interface {
    Close() error
}

// Services implementing Disposable are automatically
// cleaned up when their scope/provider is closed
```

## Provider Options

Configure provider behavior:

```go
options := &godi.ProviderOptions{
    // Build timeout
    BuildTimeout: 30 * time.Second,
}

provider, err := collection.BuildWithOptions(options)
```

## Service Lifetimes

```go
const (
    Singleton Lifetime = iota // One instance forever
    Scoped                    // One instance per scope
    Transient                 // New instance every time
)
```

## Error Types

```go
// Typed errors for rich context
type ResolutionError struct {
    ServiceType reflect.Type
    ServiceKey  any
    Cause       error
}

type CircularDependencyError struct {
    Node graph.NodeKey
}

type LifetimeConflictError struct {
    ServiceType reflect.Type
    Current     Lifetime
    Requested   Lifetime
}

type ValidationError struct {
    ServiceType reflect.Type
    Cause       error
}

// Error checking
var resErr *godi.ResolutionError
if errors.As(err, &resErr) {
    // Handle resolution error
}

// Common sentinel errors
var (
    ErrServiceNotFound         // Service not registered
    ErrServiceTypeNil          // Service type is nil
    ErrServiceKeyNil           // Service key is nil
    ErrProviderDisposed        // Provider has been disposed
    ErrScopeDisposed           // Scope has been disposed
    ErrConstructorNil          // Constructor is nil
    ErrConstructorNoReturn     // Constructor has no returns
    ErrConstructorReturnedNil  // Constructor returned nil
    ErrSingletonNotInitialized // Singleton not created at build
)
```

## Generic Helpers

Type-safe resolution helpers:

```go
// Resolve by type
service, err := godi.Resolve[*UserService](provider)

// Resolve by key
service, err := godi.ResolveKeyed[Database](provider, "primary")

// From scope
service, err := godi.Resolve[*UserService](scope)
```

## Complete Example

```go
// Define module
var AppModule = godi.NewModule("app",
    // Singletons
    godi.AddSingleton(NewConfig),
    godi.AddSingleton(NewLogger),
    godi.AddSingleton(NewDatabase, godi.Name("primary")),

    // Scoped
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewUserService),

    // Groups
    godi.AddSingleton(NewEmailValidator, godi.Group("validators")),
    godi.AddSingleton(NewPhoneValidator, godi.Group("validators")),

    // Decorators
    godi.AddDecorator(LoggingDecorator),
)

// Use it
func main() {
    services := godi.NewCollection()
    services.AddModules(AppModule)

    provider, err := services.Build()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // Resolve and use
    userService, _ := godi.Resolve[*UserService](provider)
    userService.DoWork()
}
```
