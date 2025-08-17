# godi - Type-Safe Dependency Injection for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/junioryono/godi/v4.svg)](https://pkg.go.dev/github.com/junioryono/godi/v4)
[![Go Report Card](https://goreportcard.com/badge/github.com/junioryono/godi)](https://goreportcard.com/report/github.com/junioryono/godi)
[![License](https://img.shields.io/github/license/junioryono/godi)](LICENSE)

**godi** brings type-safe, zero-magic dependency injection to Go. Wire your dependencies once, change your constructors freely.

## Quick Start

```go
package main

import (
    "fmt"
    "github.com/junioryono/godi/v4"
)

// 1. Define your service
type Greeter struct {
    name string
}

func NewGreeter() *Greeter {
    return &Greeter{name: "World"}
}

func (g *Greeter) Greet() string {
    return fmt.Sprintf("Hello, %s!", g.name)
}

func main() {
    // 2. Register your services
    services := godi.NewCollection()
    services.AddSingleton(NewGreeter)

    // 3. Build the container
    provider, _ := services.Build()
    defer provider.Close()

    // 4. Use your service
    greeter, _ := godi.Resolve[*Greeter](provider)
    fmt.Println(greeter.Greet()) // Output: Hello, World!
}
```

## Why godi?

**Problem**: Your Go app is growing. Adding a new dependency to a service means updating every place that creates it. Testing requires complex setup. Sound familiar?

**Solution**: godi automatically wires your dependencies. Change a constructor in one place, godi handles the rest.

### Real Example: Adding a Logger

Without godi:

```go
// You have to update EVERY file that creates a UserService
userService := NewUserService()                    // Before
userService := NewUserService(logger)              // After - updating 20+ files!
```

With godi:

```go
// Just update the constructor - godi handles the rest
func NewUserService(logger Logger) *UserService {  // godi injects the logger
    return &UserService{logger: logger}
}
```

## Core Features

### 🎯 Three Service Lifetimes

- **Singleton**: One instance for the entire app (databases, loggers)
- **Scoped**: New instance per scope/request (repositories, handlers)
- **Transient**: New instance every time (unique IDs, stateless operations)

```go
services.AddSingleton(NewDatabase)    // Shared across app
services.AddScoped(NewUserService)    // Fresh per request
services.AddTransient(NewRequestID)   // New every time
```

### 🧪 Testing Made Easy

Swap real services with mocks instantly:

```go
// In tests
services.AddSingleton(func() Database {
    return &MockDatabase{
        users: []User{{ID: 1, Name: "Test User"}},
    }
})

// Your code uses the mock automatically!
```

### 🔌 Zero Magic

- Your services are just normal Go types
- No struct tags, no code generation
- Full IDE support with Go generics

## Installation

```bash
go get github.com/junioryono/godi/v4
```

## Common Patterns

### Web Application

```go
func main() {
    services := godi.NewCollection()

    // Register services
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewDatabase)
    services.AddScoped(NewUserRepository)
    services.AddTransient(NewRequestID)

    provider, _ := services.Build()
    defer provider.Close()

    // Use with your web framework
    http.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
        // Create a scope for this request
        scope, _ := provider.CreateScope(r.Context())
        defer scope.Close()

        // Services are resolved with proper lifetime
        repo, _ := godi.Resolve[*UserRepository](scope)
        reqID, _ := godi.Resolve[*RequestID](scope)
        // ... handle request
    })
}
```

### Service Dependencies

```go
// godi automatically injects dependencies
func NewUserService(repo UserRepository, logger Logger) *UserService {
    return &UserService{
        repo:   repo,
        logger: logger,
    }
}

// Just register - godi figures out the order
services.AddSingleton(NewLogger)
services.AddScoped(NewUserRepository)
services.AddScoped(NewUserService)
```

### Interface Registration with As()

```go
type Cache interface {
    Get(key string) (any, error)
    Set(key string, value any) error
}

type RedisCache struct { /* ... */ }

func NewRedisCache() *RedisCache {
    return &RedisCache{}
}

// Register concrete type as interface
services.AddSingleton(NewRedisCache, godi.As(new(Cache)))

// Resolve by interface
cache, _ := godi.Resolve[Cache](provider)
```

## Documentation

- 📖 [Full Documentation](https://github.com/junioryono/godi/wiki)
- 🚀 [Getting Started Guide](https://github.com/junioryono/godi/wiki/Getting-Started)
- 🧪 [Testing Guide](https://github.com/junioryono/godi/wiki/Testing)
- 🏗️ [Advanced Patterns](https://github.com/junioryono/godi/wiki/Advanced-Patterns)

## When to Use godi

✅ **Use godi when:**

- You're building web APIs or services
- You need request-scoped isolation
- You want easy unit testing
- You're tired of manual dependency wiring

❌ **Skip godi when:**

- Building simple CLI tools
- Your app has < 5 services
- You prefer manual control

## Contributing

We welcome contributions! See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License - see [LICENSE](LICENSE) for details.
