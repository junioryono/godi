# godi - Dependency Injection with Service Lifetimes for Go

[![GoDoc](https://pkg.go.dev/badge/github.com/junioryono/godi/v4)](https://pkg.go.dev/github.com/junioryono/godi/v4)
[![Github release](https://img.shields.io/github/release/junioryono/godi.svg)](https://github.com/junioryono/godi/releases)
[![Build Status](https://github.com/junioryono/godi/actions/workflows/test.yml/badge.svg)](https://github.com/junioryono/godi/actions/workflows/test.yml)
[![Coverage Status](https://codecov.io/gh/junioryono/godi/branch/main/graph/badge.svg)](https://codecov.io/gh/junioryono/godi)
[![Go Report Card](https://goreportcard.com/badge/github.com/junioryono/godi)](https://goreportcard.com/report/github.com/junioryono/godi)
[![License](https://img.shields.io/github/license/junioryono/godi)](LICENSE)

A sophisticated dependency injection container for Go with service lifetimes, type safety, and automatic dependency resolution.

## Installation

```bash
go get github.com/junioryono/godi/v4
```

Requires Go 1.21+

## Features

### Core Architecture

- **Three Service Lifetimes**: Singleton (application-wide), Scoped (per request/scope), and Transient (always new)
- **Type-Safe Resolution**: Compile-time type safety using Go generics
- **Automatic Dependency Graph**: Analyzes constructors and resolves entire dependency chains automatically
- **Build-Time Validation**: Circular dependency detection and lifetime validation before runtime

### Advanced Capabilities

- **Keyed Services**: Register multiple implementations of the same type with unique keys
- **Service Groups**: Collect multiple services under a group name for batch resolution
- **Parameter Objects (`In`)**: Simplify complex constructors with automatic field injection
- **Result Objects (`Out`)**: Register multiple services from a single constructor
- **Multi-Return Constructors**: Functions returning multiple services are automatically handled
- **Interface Registration**: Register concrete types as their interfaces with the `As` option

### Request Isolation & Scoping

- **Scoped Resolution**: Perfect for web applications with concurrent requests
- **Context Integration**: Retrieve scopes from `context.Context` for deep call stacks
- **Automatic Cleanup**: Resources implementing `Disposable` are automatically cleaned up

### Organization & Safety

- **Module System**: Group related service registrations for better organization
- **Thread-Safe**: Fully concurrent-safe resolution and caching
- **Rich Error Types**: Detailed error information for debugging dependency issues
- **Zero Code Generation**: Pure runtime dependency injection without build steps

## Quick Start

```go
// 1. Create a service collection
services := godi.NewCollection()

// 2. Register your services with appropriate lifetimes
services.AddSingleton(NewLogger)
services.AddSingleton(NewDatabase)
services.AddScoped(NewUserService)
services.AddTransient(NewCalculatorService)

// 3. Build the provider
provider, _ := services.Build()
defer provider.Close()

// 4. Resolve services with type safety
userService := godi.MustResolve[UserService](provider)
```

## Documentation

- [Getting Started Guide](docs/getting-started.md)
- [API Documentation](https://pkg.go.dev/github.com/junioryono/godi/v4)
- [Examples](examples/)
- [Web Framework Integration](docs/guides/)
- [Testing Guide](docs/guides/testing.md)
- [Advanced Patterns](docs/guides/advanced.md)

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License - see [LICENSE](LICENSE)
