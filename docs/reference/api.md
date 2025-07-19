# API Reference

This reference covers all public APIs in the godi package.

## Core Types

### ServiceCollection

`ServiceCollection` is the builder for configuring services before creating a provider.

```go
type ServiceCollection interface {
    // Build the provider
    BuildServiceProvider() (ServiceProvider, error)
    BuildServiceProviderWithOptions(options *ServiceProviderOptions) (ServiceProvider, error)

    // Register services
    AddSingleton(constructor interface{}, opts ...ProvideOption) error
    AddScoped(constructor interface{}, opts ...ProvideOption) error

    // Advanced registration
    Decorate(decorator interface{}, opts ...DecorateOption) error
    Replace(lifetime ServiceLifetime, constructor interface{}, opts ...ProvideOption) error
    RemoveAll(serviceType reflect.Type) error
    Clear()

    // Modules
    AddModules(modules ...func(ServiceCollection) error) error

    // Inspection
    ToSlice() []*serviceDescriptor
    Count() int
    Contains(serviceType reflect.Type) bool
    ContainsKeyed(serviceType reflect.Type, key interface{}) bool
}
```

#### Creating a ServiceCollection

```go
collection := godi.NewServiceCollection()
```

### ServiceProvider

`ServiceProvider` is the main dependency injection container.

```go
type ServiceProvider interface {
    // Service resolution
    Resolve(serviceType reflect.Type) (interface{}, error)
    ResolveKeyed(serviceType reflect.Type, serviceKey interface{}) (interface{}, error)

    // Service inspection
    IsService(serviceType reflect.Type) bool
    IsKeyedService(serviceType reflect.Type, serviceKey interface{}) bool

    // Scoping
    GetRootScope() Scope
    CreateScope(ctx context.Context) Scope

    // Function invocation
    Invoke(function interface{}) error

    // Lifecycle
    IsDisposed() bool
    Close() error
}
```

### Scope

`Scope` represents a service resolution boundary.

```go
type Scope interface {
    // Identity
    ID() string
    Context() context.Context

    // Hierarchy
    ServiceProvider() ServiceProvider
    IsRootScope() bool
    GetRootScope() Scope
    Parent() Scope

    // Lifecycle
    Close() error
}
```

### ServiceLifetime

```go
type ServiceLifetime int

const (
    Singleton ServiceLifetime = iota  // One instance for entire app
    Scoped                           // One instance per scope
)
```

## Registration Functions

### Basic Registration

```go
// Register a singleton service
err := collection.AddSingleton(NewLogger)

// Register a scoped service
err := collection.AddScoped(NewRepository)
```

### Keyed Registration

```go
// Register with a key
err := collection.AddSingleton(NewRedisCache, godi.Name("redis"))
err := collection.AddSingleton(NewMemoryCache, godi.Name("memory"))
```

### Group Registration

```go
// Register in a group
err := collection.AddSingleton(NewUserHandler, godi.Group("handlers"))
err := collection.AddSingleton(NewOrderHandler, godi.Group("handlers"))
```

### Interface Registration

```go
// Register as specific interfaces
err := collection.AddSingleton(NewPostgresDB, godi.As(new(Reader), new(Writer)))
```

## Resolution Functions

### Generic Resolution Helpers

```go
// Resolve[T] - Type-safe resolution
logger, err := godi.Resolve[Logger](provider)
service, err := godi.Resolve[*UserService](provider)

// ResolveKeyed[T] - Type-safe keyed resolution
cache, err := godi.ResolveKeyed[Cache](provider, "redis")
```

### Direct Resolution

```go
// Using reflection types
loggerType := reflect.TypeOf((*Logger)(nil)).Elem()
service, err := provider.Resolve(loggerType)

// Keyed resolution
service, err := provider.ResolveKeyed(loggerType, "primary")
```

## Dependency Injection

### Constructor Injection

```go
// Simple constructor
func NewService(dep1 Dependency1, dep2 Dependency2) *Service {
    return &Service{dep1: dep1, dep2: dep2}
}

// Constructor with error
func NewDatabase(config *Config) (*Database, error) {
    db, err := sql.Open("postgres", config.DatabaseURL)
    if err != nil {
        return nil, err
    }
    return &Database{db: db}, nil
}
```

### Parameter Objects (In)

```go
type ServiceParams struct {
    godi.In

    DB       *sql.DB
    Logger   Logger           `optional:"true"`
    Cache    Cache            `name:"redis"`
    Handlers []http.Handler   `group:"routes"`
}

func NewService(params ServiceParams) *Service {
    return &Service{
        db:       params.DB,
        logger:   params.Logger,
        cache:    params.Cache,
        handlers: params.Handlers,
    }
}
```

### Result Objects (Out)

```go
type ServiceResults struct {
    godi.Out

    UserService  *UserService
    AdminService *AdminService  `name:"admin"`
    Handler      http.Handler   `group:"routes"`
}

func NewServices(db *sql.DB) ServiceResults {
    return ServiceResults{
        UserService:  newUserService(db),
        AdminService: newAdminService(db),
        Handler:      newAPIHandler(),
    }
}
```

## Modules

```go
// Define a module
var DatabaseModule = godi.Module("database",
    godi.AddSingleton(NewDatabaseConnection),
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewOrderRepository),
)

// Use modules
err := collection.AddModules(DatabaseModule, CacheModule, AppModule)
```

## Service Provider Options

```go
options := &godi.ServiceProviderOptions{
    // Validate all services can be created
    ValidateOnBuild: true,

    // Resolution callbacks
    OnServiceResolved: func(serviceType reflect.Type, instance interface{}, duration time.Duration) {
        log.Printf("Resolved %s in %v", serviceType, duration)
    },

    OnServiceError: func(serviceType reflect.Type, err error) {
        log.Printf("Failed to resolve %s: %v", serviceType, err)
    },

    // Timeout for service resolution
    ResolutionTimeout: 5 * time.Second,

    // Recovery options
    RecoverFromPanics: true,

    // Defer cycle detection
    DeferAcyclicVerification: false,
}

provider, err := collection.BuildServiceProviderWithOptions(options)
```

## Decorators

```go
// Define a decorator
func LoggingDecorator(service UserService, logger Logger) UserService {
    return &loggingUserService{
        inner:  service,
        logger: logger,
    }
}

// Register decorator
err := collection.Decorate(LoggingDecorator)
```

## Disposal

### Disposable Interface

```go
type Disposable interface {
    Close() error
}

type DisposableWithContext interface {
    Close(ctx context.Context) error
}
```

Services implementing these interfaces are automatically disposed when their scope closes.

## Error Handling

### Error Types

```go
// Check error types
if godi.IsNotFound(err) {
    // Service not registered
}

if godi.IsCircularDependency(err) {
    // Circular dependency detected
}

if godi.IsDisposed(err) {
    // Provider or scope disposed
}

if godi.IsTimeout(err) {
    // Resolution timeout
}
```

### Common Errors

- `ErrServiceNotFound` - Service type not registered
- `ErrScopeDisposed` - Scope has been disposed
- `ErrProviderDisposed` - Provider has been disposed
- `ErrNilConstructor` - Constructor is nil
- `ErrConstructorNotFunction` - Constructor is not a function

## Invoke Function

```go
// Invoke a function with dependency injection
err := provider.Invoke(func(logger Logger, db *Database) error {
    logger.Log("Database connected")
    return db.Ping()
})
```

## Default Provider

```go
// Set a default provider
godi.SetDefaultServiceProvider(provider)

// Get the default provider
provider := godi.DefaultServiceProvider()
```

## Complete Example

```go
package main

import (
    "context"
    "log"
    "github.com/junioryono/godi"
)

func main() {
    // Create collection
    collection := godi.NewServiceCollection()

    // Register services
    collection.AddSingleton(NewConfig)
    collection.AddSingleton(NewLogger)
    collection.AddSingleton(NewDatabase)
    collection.AddScoped(NewUserRepository)
    collection.AddScoped(NewUserService)

    // Build provider
    provider, err := collection.BuildServiceProvider()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // Create scope
    scope := provider.CreateScope(context.Background())
    defer scope.Close()

    // Resolve service
    userService, err := godi.Resolve[*UserService](scope.ServiceProvider())
    if err != nil {
        log.Fatal(err)
    }

    // Use service
    user, err := userService.GetUser(123)
    if err != nil {
        log.Fatal(err)
    }

    log.Printf("User: %+v", user)
}
```
