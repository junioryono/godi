# godi - Type-Safe Dependency Injection for Go

[![GoDoc](https://pkg.go.dev/badge/github.com/junioryono/godi/v4)](https://pkg.go.dev/github.com/junioryono/godi/v4)
[![Github release](https://img.shields.io/github/release/junioryono/godi.svg)](https://github.com/junioryono/godi/releases)
[![Build Status](https://github.com/junioryono/godi/actions/workflows/test.yml/badge.svg)](https://github.com/junioryono/godi/actions/workflows/test.yml)
[![Coverage Status](https://codecov.io/gh/junioryono/godi/branch/main/graph/badge.svg)](https://codecov.io/gh/junioryono/godi)
[![Go Report Card](https://goreportcard.com/badge/github.com/junioryono/godi)](https://goreportcard.com/report/github.com/junioryono/godi)
[![License](https://img.shields.io/github/license/junioryono/godi)](LICENSE)

**godi** makes dependency injection in Go effortless. Your dependencies evolve, your wiring just works.

## Why godi?

**The Problem**: Manual dependency wiring in Go gets complex fast. You need to create services in the right order, pass dependencies through multiple layers, and wire everything correctly in main(). Testing requires recreating this entire chain with mocks.

**The Solution**: godi automatically resolves your entire dependency graph. Define what each service needs, godi figures out the rest.

### See the Difference

**Without godi** - Manual wiring gets complex:

```go
func main() {
    // You must create everything in the right order
    config := loadConfig()
    logger := newLogger(config)
    db := newDatabase(config, logger)
    cache := newCache(config, logger)

    // Wire up repositories
    userRepo := newUserRepo(db, logger)
    orderRepo := newOrderRepo(db, logger)

    // Wire up services (getting messy...)
    emailService := newEmailService(config, logger)
    userService := newUserService(userRepo, emailService, cache, logger)
    orderService := newOrderService(orderRepo, userService, emailService, cache, logger)

    // Wire up handlers (this is getting out of hand!)
    userHandler := newUserHandler(userService, logger)
    orderHandler := newOrderHandler(orderService, userService, logger)

    // Forgot something? Added a dependency? Start over...
    // Want to test? Recreate this entire chain with mocks!
}
```

**With godi** - Let the framework handle it:

```go
func main() {
    services := godi.NewCollection()

    // Register in any order - godi figures out dependencies
    services.AddSingleton(loadConfig)
    services.AddSingleton(newLogger)
    services.AddSingleton(newDatabase)
    services.AddSingleton(newCache)
    services.AddScoped(newUserRepo)
    services.AddScoped(newOrderRepo)
    services.AddScoped(newEmailService)
    services.AddScoped(newUserService)
    services.AddScoped(newOrderService)
    services.AddScoped(newUserHandler)
    services.AddScoped(newOrderHandler)

    provider, _ := services.Build()

    // That's it! godi handles all the wiring
    handler, _ := godi.Resolve[*OrderHandler](provider)
    // handler has everything it needs, transitively resolved
}

// Adding a new dependency? Just update the constructor:
func newOrderService(
    repo *OrderRepo,
    userSvc *UserService,
    emailSvc *EmailService,
    cache Cache,
    logger Logger,
    metrics *Metrics,  // NEW: Just added this
) *OrderService {
    // godi automatically injects metrics everywhere this is used
}
```

## Installation

```bash
go get github.com/junioryono/godi/v4
```

**Requirements:** Go 1.21+ (uses generics for type safety)

## Quick Start

```go
package main

import (
    "fmt"
    "github.com/junioryono/godi/v4"
)

// 1. Define your services - just regular Go types
type Logger struct{}
func (l *Logger) Log(msg string) { fmt.Println(msg) }
func NewLogger() *Logger { return &Logger{} }

type Database struct { logger *Logger }
func NewDatabase(logger *Logger) *Database {
    return &Database{logger: logger}
}

type UserService struct {
    db *Database
    logger *Logger
}
func NewUserService(db *Database, logger *Logger) *UserService {
    return &UserService{db: db, logger: logger}
}

func main() {
    // 2. Register your services - order doesn't matter!
    services := godi.NewCollection()
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewDatabase)
    services.AddScoped(NewUserService)

    // 3. Build the container
    provider, _ := services.Build()
    defer provider.Close()

    // 4. Use your services - dependencies are automatically resolved!
    userService, _ := godi.Resolve[*UserService](provider)
    userService.logger.Log("Ready to go!")
}
```

## Key Features

### 🎯 Three Service Lifetimes

Control exactly when instances are created:

```go
services.AddSingleton(NewDatabase)    // One instance for entire app
services.AddScoped(NewUserService)    // New instance per request/scope
services.AddTransient(NewRequestID)   // New instance every time
```

**When to use each:**

- **Singleton**: Shared state (database, cache, connection pools)
- **Scoped**: Request context (user session, transaction, unit of work)
- **Transient**: Unique values (IDs, timestamps, temporary objects)

### 🔄 Request Scoping for Web Apps

Perfect isolation for concurrent requests:

```go
http.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
    // Create isolated scope for this request
    scope, _ := provider.CreateScope(r.Context())
    defer scope.Close()

    // Each request gets its own instances of scoped services
    userRepo, _ := godi.Resolve[*UserRepository](scope)
    // This repo instance is unique to this request
})
```

### 📦 Modular Organization

Keep large apps maintainable with modules:

```go
// database/module.go
var DatabaseModule = godi.NewModule("database",
    godi.AddSingleton(NewConnection),
    godi.AddScoped(NewTransaction),
)

// user/module.go
var UserModule = godi.NewModule("user",
    DatabaseModule,  // Dependencies are explicit
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewUserService),
)

// main.go
services := godi.NewCollection()
services.AddModules(UserModule)  // Includes everything needed
```

### 🧪 Testing Made Simple

Swap implementations effortlessly:

```go
// In production
services.AddSingleton(NewPostgresDB)

// In tests - just swap the implementation
services.AddSingleton(func() Database {
    return &MockDatabase{
        users: []User{{ID: 1, Name: "Test"}},
    }
})
// Your entire app now uses the mock!
```

### 🔌 Interface-Based Services

Register implementations for interfaces:

```go
type Cache interface {
    Get(key string) (any, error)
    Set(key string, value any) error
}

type RedisCache struct { /* ... */ }

// Register concrete type as interface
services.AddSingleton(NewRedisCache, godi.As(new(Cache)))

// Resolve by interface
cache, _ := godi.Resolve[Cache](provider)
```

### 🏷️ Named Services

Support multiple implementations of the same type:

```go
// Register different databases
services.AddSingleton(NewPrimaryDB, godi.Name("primary"))
services.AddSingleton(NewReplicaDB, godi.Name("replica"))

// Resolve the one you need
primary, _ := godi.ResolveKeyed[Database](provider, "primary")
replica, _ := godi.ResolveKeyed[Database](provider, "replica")
```

### 🚀 Zero Magic

- **No code generation** - Everything happens at runtime
- **No struct tags** - Your types stay clean
- **Full IDE support** - Go's type system does the work
- **Compile-time safety** - Generics catch errors early

## Real-World Example

Here's how godi shines in a typical web application:

```go
// Define your service graph
func main() {
    services := godi.NewCollection()

    // Infrastructure (Singletons - shared across requests)
    services.AddSingleton(config.Load)
    services.AddSingleton(database.NewConnection)
    services.AddSingleton(cache.NewRedis)
    services.AddSingleton(logger.New)

    // Business Logic (Scoped - per request)
    services.AddScoped(repository.NewUserRepo)
    services.AddScoped(repository.NewOrderRepo)
    services.AddScoped(service.NewUserService)
    services.AddScoped(service.NewOrderService)

    // Utilities (Transient - always new)
    services.AddTransient(utils.NewRequestID)
    services.AddTransient(utils.NewTimer)

    provider, _ := services.Build()
    defer provider.Close()

    // Your HTTP handlers stay clean
    http.HandleFunc("/api/users", makeHandler(provider, handleUsers))
    http.ListenAndServe(":8080", nil)
}

func makeHandler(provider godi.Provider, fn HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Each request gets its own scope
        scope, _ := provider.CreateScope(r.Context())
        defer scope.Close()

        fn(w, r, scope)
    }
}

func handleUsers(w http.ResponseWriter, r *http.Request, scope godi.Scope) {
    // Dependencies are injected and scoped to this request
    userService, _ := godi.Resolve[*service.UserService](scope)
    users, _ := userService.GetAll()
    json.NewEncoder(w).Encode(users)
}
```

## Advanced Features

### Parameter Objects (In)

Simplify constructors with many dependencies:

```go
type ServiceParams struct {
    godi.In  // Must be embedded

    DB       *Database
    Cache    Cache           `optional:"true"`
    Logger   Logger          `name:"app"`
    Handlers []http.Handler  `group:"routes"`
}

func NewService(params ServiceParams) *Service {
    // All fields are automatically injected
    return &Service{
        db:       params.DB,
        cache:    params.Cache,  // nil if not registered
        handlers: params.Handlers,
    }
}
```

### Result Objects (Out)

Register multiple services from one constructor:

```go
type Services struct {
    godi.Out  // Must be embedded

    UserService  *UserService
    AdminService *AdminService  `name:"admin"`
    APIHandler   http.Handler   `group:"routes"`
}

func NewServices(db *Database) Services {
    user := &UserService{db: db}
    admin := &AdminService{db: db}

    return Services{
        UserService:  user,
        AdminService: admin,
        APIHandler:   NewAPIHandler(user, admin),
    }
}

// All three services are registered with one call
services.AddSingleton(NewServices)
```

## Framework Integration

godi works seamlessly with any Go framework:

- **net/http**: [Standard Library Guide](docs/guides/web-apps-http.md)
- **Gin**: [Gin Framework Guide](docs/guides/web-apps-gin.md)
- **Echo**: [Echo Framework Guide](docs/guides/web-apps-echo.md)
- **Fiber**: [Fiber Framework Guide](docs/guides/web-apps-fiber.md)
- **Gorilla/Mux**: [Mux Router Guide](docs/guides/web-apps-mux.md)
- **gRPC**: [gRPC Services Guide](docs/guides/grpc.md)

## How It Compares

| Feature            | godi | wire | fx  | dig |
| ------------------ | ---- | ---- | --- | --- |
| Runtime DI         | ✅   | ❌   | ✅  | ✅  |
| No Code Generation | ✅   | ❌   | ✅  | ✅  |
| Request Scoping    | ✅   | ❌   | ⚠️  | ✅  |
| Modules            | ✅   | ❌   | ✅  | ❌  |
| Generic Support    | ✅   | ✅   | ✅  | ❌  |
| Simple API         | ✅   | ⚠️   | ❌  | ✅  |
| IDE Autocomplete   | ✅   | ⚠️   | ✅  | ❌  |

## When to Use godi

✅ **Use godi when:**

- Building web APIs or microservices
- You need request-scoped isolation
- Testing is important (easy mocking)
- Your app has 5+ services
- Dependencies change frequently
- Multiple team members work on the codebase

❌ **Skip godi when:**

- Building simple CLI tools
- Your app has < 5 services
- You prefer compile-time wiring (use wire instead)
- Zero runtime overhead is critical

## Common Patterns & Solutions

### Circular Dependencies

```go
// ❌ Problem: A needs B, B needs A
type ServiceA struct { b *ServiceB }
type ServiceB struct { a *ServiceA }

// ✅ Solution: Use interfaces or lazy loading
type ServiceA struct { provider godi.Provider }
func (a *ServiceA) GetB() *ServiceB {
    b, _ := godi.Resolve[*ServiceB](a.provider)
    return b
}
```

### Lifetime Conflicts

```go
// ❌ Problem: Singleton can't depend on Scoped
services.AddSingleton(NewCache)  // Cache needs Repository
services.AddScoped(NewRepository)

// ✅ Solution: Align lifetimes
services.AddScoped(NewCache)  // Both are now scoped
// OR
services.AddSingleton(NewRepository)  // Both are now singleton
```

## Documentation

- 📖 [Full API Documentation](https://pkg.go.dev/github.com/junioryono/godi/v4)
- 🚀 [Getting Started Guide](docs/getting-started.md)
- 🧪 [Testing Guide](docs/guides/testing.md)
- 🏗️ [Advanced Patterns](docs/guides/advanced.md)
- 💡 [Examples](examples/)

## Get Help

- 💬 [GitHub Discussions](https://github.com/junioryono/godi/discussions) - Ask questions
- 🐛 [GitHub Issues](https://github.com/junioryono/godi/issues) - Report bugs
- 📖 [FAQ](docs/reference/faq.md) - Common questions

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License - see [LICENSE](LICENSE) for details.
