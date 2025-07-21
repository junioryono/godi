# API Reference

Quick reference for all godi types and functions.

## Core Types

### ServiceCollection

Container for service registrations.

```go
// Create
collection := godi.NewServiceCollection()

// Register services
collection.AddSingleton(constructor, options...)
collection.AddScoped(constructor, options...)

// Add modules
err := collection.AddModules(module1, module2)

// Build provider
provider, err := collection.BuildServiceProvider()
provider, err := collection.BuildServiceProviderWithOptions(options)

// Other methods
count := collection.Count()
collection.Clear()
```

### ServiceProvider

Resolves services and creates scopes.

```go
// Resolve services
service, err := godi.Resolve[T](provider)
service, err := godi.ResolveKeyed[T](provider, key)

// Direct resolution (avoid - use generic helpers)
err := provider.Resolve(reflect.TypeOf((*T)(nil)).Elem())
err := provider.ResolveKeyed(type, key)

// Create scopes
scope := provider.CreateScope(context)

// Check registration
exists := provider.IsService(reflect.TypeOf((*T)(nil)).Elem())
exists := provider.IsKeyedService(type, key)

// Cleanup
err := provider.Close()
disposed := provider.IsDisposed()
```

### Scope

Isolated service lifetime boundary.

```go
// Get scoped provider
scopedProvider := scope.ServiceProvider()

// Create nested scope
nestedScope := scope.ServiceProvider().CreateScope(ctx)

// Cleanup
err := scope.Close()
disposed := scope.IsDisposed()
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
// Use with parameter objects to get []Validator

// Both
godi.AddSingleton(NewService,
    godi.Name("primary"),
    godi.Group("services"))
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

## Disposal Interfaces

Services can implement these for cleanup:

```go
// Simple disposal
type Disposable interface {
    Close() error
}

// Context-aware disposal
type DisposableWithContext interface {
    Close(ctx context.Context) error
}
```

## Provider Options

Configure provider behavior:

```go
options := &godi.ServiceProviderOptions{
    // Validate all services on build
    ValidateOnBuild: true,

    // Resolution callbacks
    OnServiceResolved: func(
        serviceType reflect.Type,
        instance interface{},
        duration time.Duration,
    ) {
        log.Printf("Resolved %s in %v", serviceType, duration)
    },

    OnServiceError: func(
        serviceType reflect.Type,
        err error,
    ) {
        log.Printf("Failed to resolve %s: %v", serviceType, err)
    },

    // Resolution timeout
    ResolutionTimeout: 30 * time.Second,
}

provider, err := collection.BuildServiceProviderWithOptions(options)
```

## Service Lifetimes

```go
const (
    Singleton ServiceLifetime = iota // One instance forever
    Scoped                          // One instance per scope
)
```

## Error Types

```go
// Check error types
if godi.IsNotFound(err) { }
if godi.IsCircularDependency(err) { }
if godi.IsDisposed(err) { }
if godi.IsTimeout(err) { }

// Common errors
var (
    ErrServiceNotFound      // Service not registered
    ErrCircularDependency   // A -> B -> A
    ErrScopeDisposed        // Using closed scope
    ErrProviderDisposed     // Using closed provider
    ErrLifetimeConflict     // Same type, different lifetimes
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
service, err := godi.Resolve[*UserService](scope.ServiceProvider())
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
    services := godi.NewServiceCollection()
    services.AddModules(AppModule)

    provider, err := services.BuildServiceProvider()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // Resolve and use
    userService, _ := godi.Resolve[*UserService](provider)
    userService.DoWork()
}
```
