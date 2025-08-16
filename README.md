# godi - Dependency Injection for Go

[![Go Reference](https://pkg.go.dev/badge/github.com/junioryono/godi.svg)](https://pkg.go.dev/github.com/junioryono/godi)
[![Go Report Card](https://goreportcard.com/badge/github.com/junioryono/godi)](https://goreportcard.com/report/github.com/junioryono/godi)
[![License](https://img.shields.io/github/license/junioryono/godi)](LICENSE)

**godi** makes dependency injection in Go simple and familiar. If you've used .NET's DI container, you'll feel right at home.

## Quick Start

```go
package main

import (
    "fmt"
    "github.com/junioryono/godi/v3"
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

### 🎯 Two Service Lifetimes

- **Singleton**: One instance for the entire app (databases, loggers)
- **Scoped**: New instance per request/operation (web handlers, transactions)

```go
services.AddSingleton(NewDatabase)    // Shared across app
services.AddScoped(NewUserService)    // Fresh per request
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
go get github.com/junioryono/godi/v3
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

    provider, _ := services.Build()
    defer provider.Close()

    // Use with your web framework
    http.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
        // Create a scope for this request
        scope := provider.CreateScope(r.Context())
        defer scope.Close()

        // Services are isolated per request
        repo, _ := godi.Resolve[*UserRepository](scope)
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

### Interfaces for Flexibility

```go
type Cache interface {
    Get(key string) (string, error)
    Set(key string, value string) error
}

// Easy to swap implementations
services.AddSingleton(func() Cache {
    if config.UseRedis {
        return NewRedisCache()
    }
    return NewMemoryCache()
})
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
