# Core Concepts

Understanding these core concepts will help you use godi effectively.

## Dependency Injection

Dependency injection (DI) is a pattern where objects receive their dependencies rather than creating them. godi automates this process:

```go
// Without DI - tight coupling
type Service struct {
    logger *Logger
}

func NewService() *Service {
    return &Service{
        logger: NewLogger(), // Creates its own dependency
    }
}

// With DI - loose coupling
type Service struct {
    logger Logger
}

func NewService(logger Logger) *Service {
    return &Service{
        logger: logger, // Receives dependency
    }
}
```

## Services and Constructors

### What is a Service?

A service is any type that provides functionality in your application:

```go
type Logger interface { Log(string) }
type Database interface { Query(string) Result }
type EmailSender interface { Send(Email) error }
```

### What is a Constructor?

A constructor is a function that creates a service. godi calls these automatically:

```go
// Simple constructor
func NewLogger() Logger {
    return &logger{}
}

// Constructor with dependencies
func NewEmailService(logger Logger, smtp SMTPClient) EmailService {
    return &emailService{
        logger: logger,
        smtp:   smtp,
    }
}
```

## The Service Collection

The collection is where you register all your services:

```go
services := godi.NewCollection()

// Register services
services.AddSingleton(NewLogger)
services.AddSingleton(NewDatabase)
services.AddScoped(NewEmailService)
```

## The Provider

The provider is built from the collection and manages service creation:

```go
// Build provider from collection
provider, err := services.Build()
if err != nil {
    // Handle build errors (circular dependencies, etc.)
}
defer provider.Close()

// Use provider to resolve services
logger := godi.MustResolve[Logger](provider)
```

## Service Lifetimes

Every service has a lifetime that determines when it's created:

### Singleton

Created once, shared everywhere:

```go
services.AddSingleton(NewDatabaseConnection)
// Same instance returned every time
```

### Scoped

Created once per scope (e.g., per HTTP request):

```go
services.AddScoped(NewRequestContext)
// Different instance for each scope
```

### Transient

Created fresh every time:

```go
services.AddTransient(NewTempFileHandler)
// New instance on every resolution
```

## Dependency Resolution

godi automatically builds a dependency graph and creates services in the correct order:

```go
// godi sees this dependency chain:
// UserService → Database → Logger

func NewLogger() Logger { }
func NewDatabase(logger Logger) Database { }
func NewUserService(db Database) UserService { }

services.AddSingleton(NewLogger)
services.AddSingleton(NewDatabase)
services.AddSingleton(NewUserService)

// godi creates them in order:
// 1. Logger (no dependencies)
// 2. Database (needs Logger)
// 3. UserService (needs Database)
```

## Scopes

Scopes provide isolation between different contexts (like HTTP requests):

```go
// In an HTTP handler
func Handler(provider godi.Provider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Create isolated scope for this request
        scope, _ := provider.CreateScope(r.Context())
        defer scope.Close()

        // Services resolved in this scope are isolated
        service := godi.MustResolve[MyService](scope)
    }
}
```

## Type Safety with Generics

godi uses Go generics for compile-time type safety:

```go
// Type-safe resolution
logger := godi.MustResolve[Logger](provider)
db := godi.MustResolve[Database](provider)

// Compile error if type doesn't match
// user := godi.MustResolve[string](provider) // Error!
```

## Resource Cleanup

Services implementing `Disposable` are automatically cleaned up:

```go
type FileHandler struct {
    file *os.File
}

func (f *FileHandler) Close() error {
    return f.file.Close()
}

// Automatically closed when scope/provider closes
```

## Complete Example

Putting it all together:

```go
package main

import (
    "fmt"
    "github.com/junioryono/godi/v4"
)

// Define services
type Config interface { GetDBURL() string }
type Logger interface { Log(string) }
type Database interface { Connect() error }
type UserService interface { GetUser(int) string }

// Constructors
func NewConfig() Config { return &config{url: "localhost:5432"} }
func NewLogger() Logger { return &logger{} }
func NewDatabase(cfg Config, log Logger) Database {
    return &database{url: cfg.GetDBURL(), logger: log}
}
func NewUserService(db Database, log Logger) UserService {
    return &userService{db: db, logger: log}
}

func main() {
    // Setup DI
    services := godi.NewCollection()
    services.AddSingleton(NewConfig)
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewDatabase)
    services.AddScoped(NewUserService)

    // Build and use
    provider, _ := services.Build()
    defer provider.Close()

    userService := godi.MustResolve[UserService](provider)
    fmt.Println(userService.GetUser(1))
}
```

## Next Steps

Now that you understand the core concepts:

- Learn about [Service Lifetimes](service-lifetimes.md) in detail
- Explore [Service Registration](service-registration.md) options
- Understand [Dependency Resolution](dependency-resolution.md)
