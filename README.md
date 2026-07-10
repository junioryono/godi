# godi

[![Go Reference](https://pkg.go.dev/badge/github.com/junioryono/godi/v5.svg)](https://pkg.go.dev/github.com/junioryono/godi/v5)
[![Go Report Card](https://goreportcard.com/badge/github.com/junioryono/godi)](https://goreportcard.com/report/github.com/junioryono/godi)
[![Build Status](https://github.com/junioryono/godi/actions/workflows/test.yml/badge.svg)](https://github.com/junioryono/godi/actions/workflows/test.yml)
[![Coverage](https://codecov.io/gh/junioryono/godi/branch/main/graph/badge.svg)](https://codecov.io/gh/junioryono/godi)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

**Dependency injection for Go with service lifetimes.** godi automatically wires your application, manages service lifetimes, and handles cleanup - so you can focus on business logic.

```go
services := godi.NewCollection()
services.AddSingleton(NewDatabase)     // One instance, shared everywhere
services.AddScoped(NewUserService)     // One instance per request
services.AddTransient(NewEmailBuilder) // New instance every time

provider, err := services.Build()
if err != nil {
    log.Fatal(err)
}

user := godi.MustResolve[*UserService](provider)
```

## Contents

- [Why godi?](#why-godi)
- [Installation](#installation)
- [Quick Start](#quick-start)
- [Service Lifetimes](#service-lifetimes)
- [HTTP Integration](#http-integration)
- [Features](#features)
- [Error Handling](#error-handling)
- [Testing](#testing)
- [Comparison](#comparison)
- [Performance](#performance)
- [Documentation](#documentation)

## Why godi?

**The problem:** As applications grow, manually wiring dependencies becomes painful. Constructor parameters multiply, initialization order matters, and per-request isolation requires careful scope management.

```go
// Manual wiring - gets messy fast
config := NewConfig()
logger := NewLogger(config)
db := NewDatabase(config, logger)
cache := NewCache(config, logger)
userRepo := NewUserRepository(db, cache, logger)
orderRepo := NewOrderRepository(db, cache, logger)
userService := NewUserService(userRepo, logger)
orderService := NewOrderService(orderRepo, userService, logger)
// ... 20 more lines
```

**The solution:** Register constructors. godi figures out the rest.

```go
// godi - register in any order, resolve anything
services := godi.NewCollection()
services.AddSingleton(NewConfig)
services.AddSingleton(NewLogger)
services.AddSingleton(NewDatabase)
services.AddScoped(NewUserService)
// ... godi handles the wiring
```

## Installation

```bash
go get github.com/junioryono/godi/v5
```

Requires **Go 1.26+**. Zero external dependencies.

> **Upgrading from v4?** See the [v4 → v5 migration guide](MIGRATION.md) — v5
> is a breaking release at a new import path.

## Quick Start

```go
package main

import (
    "fmt"
    "log"

    "github.com/junioryono/godi/v5"
)

type Logger struct{}
func (l *Logger) Log(msg string) { fmt.Println(msg) }
func NewLogger() *Logger { return &Logger{} }

type UserService struct {
    logger *Logger
}
func NewUserService(logger *Logger) *UserService {
    return &UserService{logger: logger}
}
func (s *UserService) Greet() { s.logger.Log("Hello from UserService!") }

func main() {
    // 1. Register services
    services := godi.NewCollection()
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewUserService)

    // 2. Build the container — registration errors surface here
    provider, err := services.Build()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // 3. Resolve and use - dependencies wired automatically
    users := godi.MustResolve[*UserService](provider)
    users.Greet() // Hello from UserService!
}
```

## Service Lifetimes

godi provides three lifetimes to control when instances are created:

```
┌──────────────────────────────────────────────────────────────────┐
│  Application                                                     │
│  ┌────────────────────────────────────────────────────────────┐  │
│  │ SINGLETON: Database, Logger, Config                        │  │
│  │ Created once, shared everywhere                            │  │
│  └────────────────────────────────────────────────────────────┘  │
│                                                                  │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐            │
│  │  Request 1   │  │  Request 2   │  │  Request 3   │            │
│  │   SCOPED:    │  │   SCOPED:    │  │   SCOPED:    │            │
│  │  Transaction │  │  Transaction │  │  Transaction │            │
│  │  UserSession │  │  UserSession │  │  UserSession │            │
│  └──────────────┘  └──────────────┘  └──────────────┘            │
└──────────────────────────────────────────────────────────────────┘
```

| Lifetime    | Created        | Shared       | Use Case                      |
| ----------- | -------------- | ------------ | ----------------------------- |
| `Singleton` | Once           | App-wide     | Database pools, config        |
| `Scoped`    | Once per scope | Within scope | Request context, transactions |
| `Transient` | Every time     | Never        | Builders, temp objects        |

```go
services.AddSingleton(NewDatabasePool)  // One pool for the whole app
services.AddScoped(NewTransaction)      // Fresh transaction per request
services.AddTransient(NewQueryBuilder)  // New builder every resolution
```

## HTTP Integration

godi shines in web applications where each request needs isolated services:

```go
package main

import (
    "log"
    "net/http"

    "github.com/junioryono/godi/v5"
    godihttp "github.com/junioryono/godi/http/v5"
)

type UserController struct {
    // Dependencies injected automatically
}

func (c *UserController) List(w http.ResponseWriter, r *http.Request) {
    w.Write([]byte(`["alice", "bob"]`))
}

func main() {
    services := godi.NewCollection()
    services.AddSingleton(NewLogger)
    services.AddScoped(NewUserController)

    provider, err := services.Build()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    mux := http.NewServeMux()
    mux.HandleFunc("GET /users", godihttp.Handle((*UserController).List))

    // ScopeMiddleware creates a fresh scope per request
    handler := godihttp.ScopeMiddleware(provider)(mux)
    http.ListenAndServe(":8080", handler)
}
```

### Framework Support

| Framework | Package                               | Install                                      |
| --------- | ------------------------------------- | -------------------------------------------- |
| net/http  | `github.com/junioryono/godi/http/v5`  | `go get github.com/junioryono/godi/http/v5`  |
| Gin       | `github.com/junioryono/godi/gin/v5`   | `go get github.com/junioryono/godi/gin/v5`   |
| Chi       | `github.com/junioryono/godi/chi/v5`   | `go get github.com/junioryono/godi/chi/v5`   |
| Echo      | `github.com/junioryono/godi/echo/v5`  | `go get github.com/junioryono/godi/echo/v5`  |
| Fiber     | `github.com/junioryono/godi/fiber/v5` | `go get github.com/junioryono/godi/fiber/v5` |
| Huma      | `github.com/junioryono/godi/huma/v5`  | `go get github.com/junioryono/godi/huma/v5`  |

Huma runs on top of a router, so pair `godi/huma/v5` with the matching router
integration above — the router middleware owns the request scope, and Huma
propagates it to your typed operation handlers.

## Features

### Interface Binding

Register concrete types as interfaces for easy testing and swapping:

```go
services.AddSingleton(NewConsoleLogger, godi.As[Logger]())

// Resolve by interface
logger := godi.MustResolve[Logger](provider)
```

### Keyed Services

Multiple implementations of the same type:

```go
services.AddSingleton(NewPrimaryDB, godi.Name("primary"))
services.AddSingleton(NewReplicaDB, godi.Name("replica"))

primary := godi.MustResolveKeyed[Database](provider, "primary")
replica := godi.MustResolveKeyed[Database](provider, "replica")
```

### Service Groups

Collect related services for batch operations:

```go
services.AddSingleton(NewEmailValidator, godi.Group("validators"))
services.AddSingleton(NewPhoneValidator, godi.Group("validators"))

validators := godi.MustResolveGroup[Validator](provider, "validators")
for _, v := range validators {
    v.Validate(input)
}
```

### Parameter Objects

Simplify constructors with many dependencies:

```go
type ServiceParams struct {
    godi.In
    DB      Database
    Cache   Cache
    Logger  Logger
    Metrics Metrics `optional:"true"`
}

func NewService(params ServiceParams) *Service {
    return &Service{db: params.DB, cache: params.Cache}
}
```

### Result Objects

Register multiple services from one constructor:

```go
type InfraResult struct {
    godi.Out
    DB     *Database
    Cache  *Cache
    Health *HealthChecker
}

func NewInfra(cfg *Config) InfraResult {
    db := connectDB(cfg)
    return InfraResult{
        DB:     db,
        Cache:  NewCache(cfg),
        Health: NewHealthChecker(db),
    }
}

// One registration, three services
services.AddSingleton(NewInfra)
```

### Modules

Organize large applications:

```go
// users/module.go
var Module = godi.NewModule("users",
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewUserService),
)

// main.go
services.AddModules(
    infrastructure.Module,
    users.Module,
    orders.Module,
)
```

### Automatic Cleanup

Services implementing `Close() error` are cleaned up automatically:

```go
func (d *Database) Close() error {
    return d.conn.Close()
}

provider.Close() // Database.Close() called automatically
```

## Error Handling

godi validates at build time to catch problems early:

```go
provider, err := services.Build()
if err != nil {
    // Circular dependency? Missing service? Lifetime conflict?
    // The error message tells you exactly what's wrong
    log.Fatal(err)
}
```

Common errors caught at build time:

- **Circular dependencies** - `*A -> *B -> *A`
- **Missing dependencies** - `*UserService requires *Database (not registered)`
- **Lifetime conflicts** - `singleton *Cache cannot depend on scoped *RequestContext`

## Testing

Replace implementations for testing:

```go
func TestUserService(t *testing.T) {
    services := godi.NewCollection()
    services.AddSingleton(func() Database { return &MockDB{} })
    services.AddScoped(NewUserService)

    provider, _ := services.Build()
    defer provider.Close()

    scope, _ := provider.CreateScope(context.Background())
    defer scope.Close()

    svc := godi.MustResolve[*UserService](scope)
    // Test with mock database
}
```

## Comparison

| Feature                    | godi | Wire | Fx  | do  |
| -------------------------- | ---- | ---- | --- | --- |
| No code generation         | Yes  | No   | Yes | Yes |
| Service lifetimes          | Yes  | No   | No  | No  |
| Scoped services            | Yes  | No   | No  | No  |
| Build-time validation      | Yes  | Yes  | No  | No  |
| HTTP framework integration | Yes  | No   | No  | No  |
| Parameter objects          | Yes  | No   | Yes | No  |
| Automatic cleanup          | Yes  | No   | Yes | Yes |

## Performance

The repository includes package-level benchmarks and a separate comparison suite for
[dig](https://github.com/uber-go/dig) and [do](https://github.com/samber/do). Dependency
versions are locked in [benchmarks/go.mod](benchmarks/go.mod), and every run records the
commit, timestamp, Go version, OS, and architecture alongside the raw results.

```bash
make benchmark
```

Benchmark results depend on the machine, toolchain, and system load. CI publishes the
raw `benchmark-results` artifact for each run; compare repeated samples with
[`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) instead of treating a
single run as a stable product claim. See the [comparison source](benchmarks/comparison_test.go)
for the exact workloads.

## Documentation

**[Full Documentation](https://godi.readthedocs.io)**

- [Getting Started](https://godi.readthedocs.io/en/latest/getting-started/) - 5-minute tutorial
- [Core Concepts](https://godi.readthedocs.io/en/latest/concepts/) - Lifetimes, scopes, modules
- [Features](https://godi.readthedocs.io/en/latest/features/) - Keyed services, groups, parameter objects
- [Integrations](https://godi.readthedocs.io/en/latest/integrations/) - Gin, Chi, Echo, Fiber, net/http, Huma
- [Guides](https://godi.readthedocs.io/en/latest/guides/) - Web apps, testing, error handling
- [API Reference](https://pkg.go.dev/github.com/junioryono/godi/v5)
- [Executable Quick Start](docs/examples/quickstart/main.go)

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License - see [LICENSE](LICENSE) for details.
