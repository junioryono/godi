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
defer provider.Close()

// Scoped services are resolved from a scope (typically one per request)
scope, err := provider.CreateScope(context.Background())
if err != nil {
    log.Fatal(err)
}
defer scope.Close()

users := godi.MustResolve[*UserService](scope)
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

godi supports the two most recent Go minor releases, like Go itself, and CI
tests both. See the [Go version policy](CONTRIBUTING.md#go-version-policy).

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

type Logger struct{}

func NewLogger() *Logger { return &Logger{} }

type UserController struct {
    logger *Logger // Injected automatically
}

func NewUserController(logger *Logger) *UserController {
    return &UserController{logger: logger}
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

| Framework | Package                                 | Install                                        |
| --------- | --------------------------------------- | ---------------------------------------------- |
| net/http  | `github.com/junioryono/godi/http/v5`    | `go get github.com/junioryono/godi/http/v5`    |
| Gin       | `github.com/junioryono/godi/gin/v5`     | `go get github.com/junioryono/godi/gin/v5`     |
| Chi       | `github.com/junioryono/godi/chi/v5`     | `go get github.com/junioryono/godi/chi/v5`     |
| Echo v4   | `github.com/junioryono/godi/echo/v5`    | `go get github.com/junioryono/godi/echo/v5`    |
| Echo v5   | `github.com/junioryono/godi/echov5/v5`  | `go get github.com/junioryono/godi/echov5/v5`  |
| Fiber v2  | `github.com/junioryono/godi/fiber/v5`   | `go get github.com/junioryono/godi/fiber/v5`   |
| Fiber v3  | `github.com/junioryono/godi/fiberv3/v5` | `go get github.com/junioryono/godi/fiberv3/v5` |
| Huma      | `github.com/junioryono/godi/huma/v5`    | `go get github.com/junioryono/godi/huma/v5`    |

The trailing `/v5` is godi's major version, not the framework's: `echov5/v5`
is godi v5's integration for Echo v5.

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

Common errors caught at build time (each is wrapped in a `build failed during ... phase:` prefix):

- **Circular dependencies** - a multi-line message that draws the cycle:

  ```text
  circular dependency detected:

      *main.A
        ↓
      *main.B
        ↓
      *main.A
        ↓
      *main.A (cycle)

  To resolve this:
    • Use an interface to break the dependency
    • ...
  ```

- **Missing dependencies** - `missing dependencies: *UserService requires *Database (not registered)`
- **Lifetime conflicts** - `lifetime conflict: *Cache (Singleton) cannot depend on *RequestContext (Scoped)`, followed by an explanation and suggested fixes
- **Singleton constructor failures** - singletons are created during `Build`, so their errors surface here too: `build failed during singleton-creation phase: failed to initialize singletons: failed to resolve *Database: failed to invoke func() (*main.Database, error) with parameters []: constructor error: connection refused`

Use `errors.As` with `*godi.CircularDependencyError`, `*godi.MissingDependencyError` or `*godi.LifetimeConflictError` to handle a specific case.

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

How godi differs from other Go DI libraries, as of 2026-10 (google/wire v0.7.0,
uber-go/fx v1.24.0 on dig v1.19.0, samber/do v2.1.0):

|                    | godi                                                                                                 | google/wire                                                                              | uber-go/fx                                                                                           | samber/do v2                                                                                         |
| ------------------ | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| Approach           | Runtime container (reflection, generic resolve helpers)                                              | Compile-time code generation; no runtime container                                       | Runtime container (reflection, built on dig)                                                         | Runtime container (generics)                                                                         |
| Lifetimes          | Singleton, scoped, transient                                                                         | None; the generated injector calls each provider once per injector call                  | One instance per container; no transient                                                             | Lazy or eager singletons, transient                                                                  |
| Scopes             | Scoped services get one instance per scope (e.g. per request), closed with the scope                 | None                                                                                     | `fx.Module`/`fx.Private` and dig scopes control visibility; no per-scope instances or scope disposal | Nested scopes (`injector.Scope`) control visibility; services registered in a child scope live in it |
| Graph validation   | At `Build`: cycles, missing dependencies, lifetime conflicts; singletons are constructed             | At code generation                                                                       | `fx.New` runs invokes; `fx.ValidateApp` checks for missing dependencies without running constructors | None up front; missing services and cycles are reported on invoke                                    |
| Startup / shutdown | Singletons are created at `Build`; `Close() error` methods run on `scope.Close()`/`provider.Close()` | Providers can return a cleanup `func()`; the injector returns a combined cleanup         | `fx.Lifecycle` `OnStart`/`OnStop` hooks                                                              | `Shutdown`/`ShutdownWithContext` call `Shutdowner` implementations (not for transients)              |
| Health checks      | No                                                                                                   | No                                                                                       | No                                                                                                   | Yes (`HealthCheck`, `Healthchecker`)                                                                 |
| Names and groups   | `godi.Name`, `godi.Group`, `godi.In`/`godi.Out` tags                                                 | No; use distinct types                                                                   | Yes (`name`/`group` tags, `fx.ResultTags`)                                                           | Named services; no groups                                                                            |
| HTTP integration   | Scope middleware for net/http, Chi, Echo, Fiber, Gin; Huma handlers                                  | No                                                                                       | No                                                                                                   | No                                                                                                   |
| Status             | Active                                                                                               | Archived 2025-08-25 ([goforj/wire](https://github.com/goforj/wire) is a maintained fork) | Maintained                                                                                           | Maintained                                                                                           |

Sources: [wire README](https://github.com/google/wire) and
[guide](https://github.com/google/wire/blob/main/docs/guide.md);
[fx package docs](https://pkg.go.dev/go.uber.org/fx) and
[lifecycle](https://uber-go.github.io/fx/lifecycle.html);
[dig package docs](https://pkg.go.dev/go.uber.org/dig);
[do docs](https://do.samber.dev/docs/container/scope) on
[transients](https://do.samber.dev/docs/service-registration/transient-loading),
[health checks](https://do.samber.dev/docs/service-lifecycle/healthchecker) and
[shutdown](https://do.samber.dev/docs/service-lifecycle/shutdowner).
Corrections are welcome.

## Performance

The repository includes package-level benchmarks and a separate comparison suite for
[dig](https://github.com/uber-go/dig) and [do](https://github.com/samber/do). Dependency
versions are locked in [benchmarks/go.mod](benchmarks/go.mod), and every run records the
commit, timestamp, Go version, OS, and architecture alongside the raw results.

```bash
make benchmark
```

Benchmark results depend on the machine, toolchain, and system load. A nightly CI
workflow publishes the raw `benchmark-results` artifact and compares it with the
previous run using [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat);
compare repeated samples instead of treating a single run as a stable product claim.
The libraries do not expose identical operations (dig, for example, has no
resolve-by-type API), so the [comparison source](benchmarks/comparison_test.go)
documents what each benchmark measures.

## Documentation

**[Full Documentation](https://godi.readthedocs.io)**

- [Getting Started](https://godi.readthedocs.io/en/latest/getting-started/) - 5-minute tutorial
- [Core Concepts](https://godi.readthedocs.io/en/latest/concepts/) - Lifetimes, scopes, modules
- [Features](https://godi.readthedocs.io/en/latest/features/) - Keyed services, groups, parameter objects
- [Integrations](https://godi.readthedocs.io/en/latest/integrations/) - Gin, Chi, Echo (v4 and v5), Fiber (v2 and v3), net/http, Huma
- [Guides](https://godi.readthedocs.io/en/latest/guides/) - Web apps, testing, error handling
- [API Reference](https://pkg.go.dev/github.com/junioryono/godi/v5)
- [Executable Quick Start](docs/examples/quickstart/main.go)

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License - see [LICENSE](LICENSE) for details.
