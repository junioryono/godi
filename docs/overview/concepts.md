# Core Concepts

Understanding these core concepts will help you get the most out of godi.

## Service

A **service** is any type that provides functionality to other parts of your application. In godi, services are typically interfaces or structs.

```go
// Service interface
type Logger interface {
    Log(message string)
}

// Service implementation
type ConsoleLogger struct {
    prefix string
}

func (l *ConsoleLogger) Log(message string) {
    fmt.Printf("[%s] %s\n", l.prefix, message)
}
```

## Constructor

A **constructor** is a function that creates a service. godi uses these functions to instantiate services with their dependencies automatically injected.

```go
// Constructor function
func NewConsoleLogger(config *Config) Logger {
    return &ConsoleLogger{
        prefix: config.LogPrefix,
    }
}

// Constructor with multiple dependencies
func NewUserService(db Database, logger Logger, cache Cache) *UserService {
    return &UserService{
        db:     db,
        logger: logger,
        cache:  cache,
    }
}
```

## Service Collection

The **ServiceCollection** is where you register all your services and their constructors. It's the blueprint for your dependency injection container.

```go
services := godi.NewServiceCollection()

// Register services with their constructors
services.AddSingleton(NewConsoleLogger)
services.AddScoped(NewUserService)
services.AddTransient(NewEmailService)
```

## Service Provider

The **ServiceProvider** is the built container that can create instances of your services. It's created from a ServiceCollection and manages the lifecycle of all services.

```go
// Build the provider from the collection
provider, err := services.BuildServiceProvider()
if err != nil {
    log.Fatal(err)
}
defer provider.Close()

// Resolve services from the provider
logger, err := godi.Resolve[Logger](provider)
```

## Service Lifetimes

godi supports three service lifetimes that control when instances are created and how they're shared:

### Singleton

**Singleton** services are created once and shared across the entire application lifetime.

```go
services.AddSingleton(NewDatabaseConnection)
// Same instance returned every time
```

Use for:

- Database connections
- Configuration
- Loggers
- Cache clients
- Shared resources

### Scoped

**Scoped** services are created once per scope and shared within that scope.

```go
services.AddScoped(NewRepository)
// New instance per scope, shared within scope
```

Use for:

- Database repositories (share transaction)
- Request-specific services
- Unit of work patterns
- Services that hold request state

### Transient

**Transient** services are created every time they're requested.

```go
services.AddTransient(NewEmailMessage)
// New instance every time
```

Use for:

- Stateless operations
- Lightweight objects
- Services with mutable state
- One-time operations

## Scopes

A **scope** creates a boundary for service lifetime. Scoped services live within a scope and are disposed when the scope ends.

```go
// Create a scope for a web request
func handleRequest(provider godi.ServiceProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Create scope for this request
        scope := provider.CreateScope(r.Context())
        defer scope.Close() // Cleanup scoped services

        // Services resolved here share scoped instances
        repo, _ := godi.Resolve[UserRepository](scope.ServiceProvider())
        service, _ := godi.Resolve[UserService](scope.ServiceProvider())

        // Both get the same repository instance
    }
}
```

## Dependency Resolution

godi automatically resolves dependencies by examining constructor parameters:

```go
// godi sees this needs Config and Logger
func NewDatabase(config *Config, logger Logger) Database {
    // ...
}

// When resolving Database, godi will:
// 1. Check if Config is registered ✓
// 2. Check if Logger is registered ✓
// 3. Create/retrieve Config instance
// 4. Create/retrieve Logger instance
// 5. Call NewDatabase with both
// 6. Return the Database instance
```

## Type Safety

godi leverages Go's type system and generics for compile-time safety:

```go
// Type-safe resolution with generics
logger, err := godi.Resolve[Logger](provider)
// Compiler ensures logger is of type Logger

// Also works with concrete types
service, err := godi.Resolve[*UserService](provider)
```

## Disposal and Cleanup

Services can implement `Disposable` for automatic cleanup:

```go
type Disposable interface {
    Close() error
}

type DatabaseConnection struct {
    db *sql.DB
}

func (d *DatabaseConnection) Close() error {
    return d.db.Close()
}

// Automatically called when provider/scope closes
```

## Keyed Services

Register multiple implementations of the same interface using keys:

```go
// Register different cache implementations
services.AddSingleton(NewRedisCache, godi.Name("redis"))
services.AddSingleton(NewMemoryCache, godi.Name("memory"))

// Resolve specific implementation
redisCache, _ := godi.ResolveKeyed[Cache](provider, "redis")
```

## Service Groups

Collect multiple services of the same type:

```go
// Register multiple handlers
services.AddSingleton(NewUserHandler, godi.Group("handlers"))
services.AddSingleton(NewOrderHandler, godi.Group("handlers"))
services.AddSingleton(NewProductHandler, godi.Group("handlers"))

// Consume all handlers
type App struct {
    godi.In
    Handlers []Handler `group:"handlers"`
}
```

## Modules

Organize related services into reusable modules:

```go
var DatabaseModule = godi.Module("database",
    godi.AddSingleton(NewDatabaseConfig),
    godi.AddSingleton(NewDatabaseConnection),
    godi.AddScoped(NewTransaction),
)

// Use the module
services.AddModules(DatabaseModule)
```

## Best Practices

1. **Use Interfaces**: Define services as interfaces for flexibility
2. **Constructor Injection**: Inject dependencies through constructors
3. **Appropriate Lifetimes**: Choose the right lifetime for each service
4. **Scoped for Requests**: Use scoped services in web applications
5. **Dispose Resources**: Implement Disposable for cleanup
6. **Modular Design**: Group related services in modules

## Next Steps

Now that you understand the core concepts:

- Try the [Getting Started Tutorial](../tutorials/getting-started.md)
- Learn about [Service Registration](../howto/register-services.md)
- Explore [Scopes in Detail](../howto/use-scopes.md)
