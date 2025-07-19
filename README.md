# godi - Modern Dependency Injection for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/junioryono/godi.svg)](https://pkg.go.dev/github.com/junioryono/godi)
[![Go Report Card](https://goreportcard.com/badge/github.com/junioryono/godi)](https://goreportcard.com/report/github.com/junioryono/godi)
[![License](https://img.shields.io/github/license/junioryono/godi)](LICENSE)

`godi` is a modern, type-safe dependency injection framework for Go that brings the power and familiarity of Microsoft's dependency injection patterns to the Go ecosystem. Built on top of [Uber's dig](https://github.com/uber-go/dig), it provides a more intuitive API with proper lifecycle management.

## Features

- 🚀 **Simple, intuitive API** inspired by Microsoft's DI container
- 🔄 **Two service lifetimes**: Singleton and Scoped
- 🎯 **Type-safe** service resolution with generic helpers
- 🏗️ **Constructor injection** with automatic dependency resolution
- 🔌 **Modular design** with support for service modules
- 🎨 **Service decoration** for extending functionality
- 🔑 **Keyed services** for multiple implementations
- 👥 **Service groups** for collections of services
- 🧹 **Automatic disposal** with context-aware cleanup
- 🔍 **Built-in service validation** and cycle detection
- 💡 **Parameter and result objects** for complex dependencies
- 🧵 **Thread-safe** operations

## Installation

```bash
go get github.com/junioryono/godi
```

## Quick Start

```go
package main

import (
    "context"
    "log"

    "github.com/junioryono/godi"
)

// Define your service interfaces
type Logger interface {
    Log(message string)
}

type Database interface {
    Query(sql string) string
}

// Implement your services
type ConsoleLogger struct{}

func (l *ConsoleLogger) Log(message string) {
    log.Println(message)
}

type SQLDatabase struct {
    logger Logger
}

func (db *SQLDatabase) Query(sql string) string {
    db.logger.Log("Executing query: " + sql)
    return "query result"
}

// Define constructors
func NewLogger() Logger {
    return &ConsoleLogger{}
}

func NewDatabase(logger Logger) Database {
    return &SQLDatabase{logger: logger}
}

func main() {
    // Create service collection
    services := godi.NewServiceCollection()

    // Register services
    services.AddSingleton(NewLogger)
    services.AddScoped(NewDatabase)

    // Build service provider
    provider, err := services.BuildServiceProvider()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // Resolve services
    logger, err := godi.Resolve[Logger](provider)
    if err != nil {
        log.Fatal(err)
    }

    logger.Log("Application started!")

    // Create a scope for request handling
    scope := provider.CreateScope(context.Background())
    defer scope.Close()

    // Resolve scoped services
    db, err := godi.Resolve[Database](scope.ServiceProvider())
    if err != nil {
        log.Fatal(err)
    }

    result := db.Query("SELECT * FROM users")
    logger.Log("Query result: " + result)
}
```

## Service Lifetimes

### Singleton

Created once and shared across the entire application lifetime.

```go
services.AddSingleton(NewLogger)
```

### Scoped

Created once per scope. Ideal for per-request services in web applications.

```go
services.AddScoped(NewRepository)
```

## Advanced Features

### Keyed Services

Register multiple implementations of the same interface:

```go
services.AddSingleton(NewFileLogger, godi.Name("file"))
services.AddSingleton(NewConsoleLogger, godi.Name("console"))

// Resolve specific implementation
fileLogger, err := godi.ResolveKeyed[Logger](provider, "file")
```

### Service Groups

Collect multiple services of the same type:

```go
services.AddSingleton(NewUserHandler, godi.Group("handlers"))
services.AddSingleton(NewOrderHandler, godi.Group("handlers"))

// Consume all handlers
type App struct {
    godi.In
    Handlers []Handler `group:"handlers"`
}
```

### Modules

Organize related services into reusable modules:

```go
var DatabaseModule = godi.Module("database",
    godi.AddSingleton(NewDatabaseConnection),
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewOrderRepository),
)

var CacheModule = godi.Module("cache",
    godi.AddSingleton(NewRedisCache),
    godi.AddSingleton(NewCacheMetrics),
)

// Use modules
services.AddModules(DatabaseModule, CacheModule)
```

### Service Decoration

Enhance services with additional functionality:

```go
// Original service
services.AddSingleton(NewLogger)

// Decorate with metrics
services.Decorate(func(logger Logger) Logger {
    return &MetricsLogger{wrapped: logger}
})
```

### Parameter Objects

Use parameter objects for complex constructors:

```go
type ServiceParams struct {
    godi.In

    Logger   Logger
    Database Database
    Cache    Cache `optional:"true"`
    Config   Config
}

func NewService(params ServiceParams) *Service {
    return &Service{
        logger: params.Logger,
        db:     params.Database,
        cache:  params.Cache, // May be nil if not registered
        config: params.Config,
    }
}
```

### Result Objects

Register multiple services from a single constructor:

```go
type ServiceBundle struct {
    godi.Out

    UserService  *UserService
    OrderService *OrderService
    AuthService  *AuthService  `name:"auth"`
    Middleware   Middleware    `group:"middleware"`
}

func NewServices(db Database) ServiceBundle {
    return ServiceBundle{
        UserService:  &UserService{db: db},
        OrderService: &OrderService{db: db},
        AuthService:  &AuthService{db: db},
        Middleware:   &AuthMiddleware{},
    }
}
```

### Context Injection

Scoped services can receive the scope's context:

```go
func NewRequestHandler(ctx context.Context, logger Logger) *RequestHandler {
    requestID := ctx.Value("requestID").(string)
    return &RequestHandler{
        logger:    logger,
        requestID: requestID,
    }
}
```

### Disposal

Services implementing `Disposable` or `DisposableWithContext` are automatically cleaned up:

```go
type DatabaseConnection struct {
    conn *sql.DB
}

func (dc *DatabaseConnection) Close() error {
    return dc.conn.Close()
}

// Or with context for graceful shutdown
func (dc *DatabaseConnection) Close(ctx context.Context) error {
    done := make(chan error, 1)
    go func() {
        done <- dc.conn.Close()
    }()

    select {
    case err := <-done:
        return err
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

## Service Provider Options

Configure provider behavior:

```go
options := &godi.ServiceProviderOptions{
    ValidateOnBuild: true,  // Validate all services can be created
    ResolutionTimeout: 30 * time.Second,
    OnServiceResolved: func(serviceType reflect.Type, instance interface{}, duration time.Duration) {
        log.Printf("Resolved %v in %v", serviceType, duration)
    },
    OnServiceError: func(serviceType reflect.Type, err error) {
        log.Printf("Failed to resolve %v: %v", serviceType, err)
    },
}

provider, err := services.BuildServiceProviderWithOptions(options)
```

## Best Practices

1. **Use interfaces** for your services to enable testing and flexibility
2. **Register services in order** of their dependencies (though godi handles this automatically)
3. **Prefer constructor injection** over property injection
4. **Use scoped services** for request-specific data in web applications
5. **Always close scopes** to ensure proper cleanup
6. **Use modules** to organize related services
7. **Validate on build** in development to catch issues early

## Web Framework Integration

Example with a web framework:

```go
func middlewareWithDI(provider godi.ServiceProvider) gin.HandlerFunc {
    return func(c *gin.Context) {
        // Create a scope for this request
        ctx := context.WithValue(c.Request.Context(), "requestID", c.GetHeader("X-Request-ID"))
        scope := provider.CreateScope(ctx)
        defer scope.Close()

        // Store scoped provider in context
        c.Set("provider", scope.ServiceProvider())
        c.Next()
    }
}

func handleRequest(c *gin.Context) {
    provider := c.MustGet("provider").(godi.ServiceProvider)

    // Resolve scoped services
    service, err := godi.Resolve[UserService](provider)
    if err != nil {
        c.JSON(500, gin.H{"error": err.Error()})
        return
    }

    // Use service...
}
```

## Testing

Mock services for testing:

```go
func TestUserService(t *testing.T) {
    services := godi.NewServiceCollection()

    // Register mock implementations
    services.AddSingleton(func() Logger {
        return &MockLogger{}
    })
    services.AddSingleton(func() Database {
        return &MockDatabase{
            users: []User{{ID: 1, Name: "Test User"}},
        }
    })
    services.AddScoped(NewUserService)

    provider, err := services.BuildServiceProvider()
    require.NoError(t, err)
    defer provider.Close()

    userService, err := godi.Resolve[UserService](provider)
    require.NoError(t, err)

    user, err := userService.GetUser(1)
    assert.NoError(t, err)
    assert.Equal(t, "Test User", user.Name)
}
```

## Performance

`godi` is built on top of [dig](https://github.com/uber-go/dig), which uses reflection for service resolution. While this has some overhead compared to manual dependency injection, the benefits of automatic wiring and lifecycle management often outweigh the performance costs. Service resolution is optimized with caching after the first resolution.

For performance-critical paths, consider:

- Resolving services once and reusing them
- Using singleton services where appropriate

## Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## Acknowledgments

- Built on top of [Uber's dig](https://github.com/uber-go/dig)
- Inspired by [Microsoft's Dependency Injection](https://docs.microsoft.com/en-us/aspnet/core/fundamentals/dependency-injection)
- Thanks to all [contributors](https://github.com/junioryono/godi/graphs/contributors)

## Related Projects

- [dig](https://github.com/uber-go/dig) - The underlying DI framework
- [wire](https://github.com/google/wire) - Google's compile-time dependency injection
- [fx](https://github.com/uber-go/fx) - Uber's application framework built on dig
