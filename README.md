# godi

[![Go Reference](https://pkg.go.dev/badge/github.com/junioryono/godi/v4.svg)](https://pkg.go.dev/github.com/junioryono/godi/v4)
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

provider, _ := services.Build()
user := godi.MustResolve[*UserService](provider)
```

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
go get github.com/junioryono/godi/v4
```

Requires **Go 1.21+**. Zero external dependencies.

## Quick Start

```go
package main

import (
    "fmt"
    "github.com/junioryono/godi/v4"
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

    // 2. Build the container
    provider, _ := services.Build()
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
services.AddScoped(NewTransaction)       // Fresh transaction per request
services.AddTransient(NewQueryBuilder)   // New builder every resolution
```

## HTTP Integration

godi shines in web applications where each request needs isolated services:

```go
package main

import (
    "net/http"
    "github.com/junioryono/godi/v4"
    godihttp "github.com/junioryono/godi/v4/http"
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

    provider, _ := services.Build()
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
| net/http  | `github.com/junioryono/godi/v4/http`  | `go get github.com/junioryono/godi/v4/http`  |
| Gin       | `github.com/junioryono/godi/v4/gin`   | `go get github.com/junioryono/godi/v4/gin`   |
| Chi       | `github.com/junioryono/godi/v4/chi`   | `go get github.com/junioryono/godi/v4/chi`   |
| Echo      | `github.com/junioryono/godi/v4/echo`  | `go get github.com/junioryono/godi/v4/echo`  |
| Fiber     | `github.com/junioryono/godi/v4/fiber` | `go get github.com/junioryono/godi/v4/fiber` |

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
func Module() godi.Module {
    return func(services *godi.ServiceCollection) {
        services.AddScoped(NewUserRepository)
        services.AddScoped(NewUserService)
    }
}

// main.go
services.AddModule(infrastructure.Module())
services.AddModule(users.Module())
services.AddModule(orders.Module())
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

Benchmarks comparing godi with [dig](https://github.com/uber-go/dig) (Uber's DI, powers Fx) and [do](https://github.com/samber/do) (samber's DI).

Run on Apple M2 Max. [Source code](benchmarks/comparison_test.go).

### Singleton Resolution (cached)

| Library | ns/op | B/op | allocs/op | vs godi |
| ------- | ----: | ---: | --------: | ------: |
| godi    |    53 |    0 |         0 |      1x |
| do      |   191 |  224 |         6 |    3.6x |
| dig     |   798 |  736 |        20 |     15x |

godi's lock-free singleton cache delivers **zero allocations** after warmup.

### Concurrent Resolution

| Library | ns/op | B/op | allocs/op | vs godi |
| ------- | ----: | ---: | --------: | ------: |
| godi    |     6 |    0 |         0 |      1x |
| do      |   310 |  224 |         6 |     52x |
| dig     |   336 |  736 |        20 |     56x |

Under high concurrency, godi is **50x+ faster** with zero contention.

### Transient Resolution (new instance each time)

| Library | ns/op | B/op | allocs/op |
| ------- | ----: | ---: | --------: |
| godi    |   174 |   40 |         2 |
| do      |   185 |  208 |         7 |

### First Resolution (cold start)

| Library |  ns/op |   B/op | allocs/op |
| ------- | -----: | -----: | --------: |
| dig     | 32,148 | 37,658 |       549 |
| godi    | 34,448 | 49,394 |       349 |
| do      | 44,960 | 30,500 |       351 |

Cold start performance is similar across libraries. godi optimizes for the hot path.

<details>
<summary>Run benchmarks yourself</summary>

```bash
cd benchmarks && go test -bench=. -benchmem
```

</details>

## Documentation

**[Full Documentation](https://godi.readthedocs.io)**

- [Getting Started](https://godi.readthedocs.io/en/latest/getting-started/) - 5-minute tutorial
- [Core Concepts](https://godi.readthedocs.io/en/latest/concepts/) - Lifetimes, scopes, modules
- [Features](https://godi.readthedocs.io/en/latest/features/) - Keyed services, groups, parameter objects
- [Integrations](https://godi.readthedocs.io/en/latest/integrations/) - Gin, Chi, Echo, Fiber, net/http
- [Guides](https://godi.readthedocs.io/en/latest/guides/) - Web apps, testing, error handling
- [API Reference](https://pkg.go.dev/github.com/junioryono/godi/v4)

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## License

MIT License - see [LICENSE](LICENSE) for details.
