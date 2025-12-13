# Modules

Modules help organize large applications by grouping related service registrations. Think of them as packages for your DI configuration.

## Why Modules?

Without modules, all registrations end up in one place:

```go
// main.go - gets messy fast
services := godi.NewCollection()
services.AddSingleton(NewLogger)
services.AddSingleton(NewConfig)
services.AddSingleton(NewDatabasePool)
services.AddSingleton(NewRedisClient)
services.AddScoped(NewUserRepository)
services.AddScoped(NewOrderRepository)
services.AddScoped(NewProductRepository)
services.AddScoped(NewUserService)
services.AddScoped(NewOrderService)
services.AddScoped(NewPaymentService)
services.AddScoped(NewNotificationService)
// ... 50 more lines
```

With modules, you organize by domain:

```go
// main.go - clean and organized
services := godi.NewCollection()
services.AddModules(
    infrastructure.Module,
    users.Module,
    orders.Module,
    payments.Module,
)
```

## Creating Modules

Use `godi.NewModule` to create a module with a name and service registrations:

```go
// users/module.go
package users

import "github.com/junioryono/godi/v4"

var Module = godi.NewModule("users",
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewUserService),
    godi.AddScoped(NewUserController),
)
```

## Module Organization

A typical application structure:

```
myapp/
├── main.go
├── infrastructure/
│   ├── module.go         # Database, logging, config
│   ├── database.go
│   ├── logger.go
│   └── config.go
├── users/
│   ├── module.go         # User-related services
│   ├── repository.go
│   ├── service.go
│   └── controller.go
├── orders/
│   ├── module.go         # Order-related services
│   ├── repository.go
│   ├── service.go
│   └── controller.go
└── payments/
    ├── module.go         # Payment-related services
    ├── gateway.go
    └── service.go
```

## Example: Infrastructure Module

```go
// infrastructure/module.go
package infrastructure

import "github.com/junioryono/godi/v4"

var Module = godi.NewModule("infrastructure",
    godi.AddSingleton(NewConfig),       // Configuration - load once, share everywhere
    godi.AddSingleton(NewLogger),       // Logging - singleton, thread-safe
    godi.AddSingleton(NewDatabasePool), // Database pool - singleton, manages connections
    godi.AddSingleton(NewRedisClient),  // Redis - singleton, connection pool
)
```

```go
// infrastructure/config.go
package infrastructure

type Config struct {
    DatabaseURL string
    RedisURL    string
    Debug       bool
}

func NewConfig() *Config {
    return &Config{
        DatabaseURL: os.Getenv("DATABASE_URL"),
        RedisURL:    os.Getenv("REDIS_URL"),
        Debug:       os.Getenv("DEBUG") == "true",
    }
}
```

## Example: Domain Module

```go
// users/module.go
package users

import "github.com/junioryono/godi/v4"

var Module = godi.NewModule("users",
    godi.AddScoped(NewUserRepository),  // Repository - scoped, uses transaction
    godi.AddScoped(NewUserService),     // Service - scoped, business logic
    godi.AddScoped(NewUserController),  // Controller - scoped, HTTP handlers
)
```

```go
// users/service.go
package users

type UserService struct {
    repo   *UserRepository
    logger *infrastructure.Logger
}

func NewUserService(repo *UserRepository, logger *infrastructure.Logger) *UserService {
    return &UserService{repo: repo, logger: logger}
}
```

## Composing Modules

Main.go stays clean:

```go
// main.go
package main

import (
    "github.com/junioryono/godi/v4"
    "myapp/infrastructure"
    "myapp/users"
    "myapp/orders"
    "myapp/payments"
)

func main() {
    services := godi.NewCollection()

    // Add all modules
    services.AddModules(
        infrastructure.Module,
        users.Module,
        orders.Module,
        payments.Module,
    )

    provider, err := services.Build()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // Start application
    // ...
}
```

## Module Dependencies

Modules can depend on services from other modules:

```go
// orders/service.go
package orders

type OrderService struct {
    orderRepo   *OrderRepository
    userService *users.UserService  // From users module
    logger      *infrastructure.Logger  // From infrastructure module
}

func NewOrderService(
    orderRepo *OrderRepository,
    userService *users.UserService,
    logger *infrastructure.Logger,
) *OrderService {
    return &OrderService{
        orderRepo:   orderRepo,
        userService: userService,
        logger:      logger,
    }
}
```

godi resolves cross-module dependencies automatically. Registration order of modules doesn't matter.

## Conditional Modules

Enable modules based on configuration:

```go
func main() {
    services := godi.NewCollection()
    services.AddModules(infrastructure.Module)

    cfg := loadConfig()

    if cfg.EnableUsers {
        services.AddModules(users.Module)
    }

    if cfg.EnablePayments {
        services.AddModules(payments.Module)
    }

    // ...
}
```

## Testing with Modules

Replace modules for testing:

```go
// users/module_test.go
var TestModule = godi.NewModule("users-test",
    godi.AddScoped(NewMockUserRepository), // Use mock repository
    godi.AddScoped(NewUserService),
)

func TestUserService(t *testing.T) {
    services := godi.NewCollection()
    services.AddModules(TestModule)

    provider, _ := services.Build()
    defer provider.Close()

    // Test with mock
    // ...
}
```

## Best Practices

### 1. One Module Per Domain

```
users/module.go      # User-related services
orders/module.go     # Order-related services
payments/module.go   # Payment-related services
```

### 2. Infrastructure in Its Own Module

```go
// infrastructure/module.go
var Module = godi.NewModule("infrastructure",
    godi.AddSingleton(NewConfig),
    godi.AddSingleton(NewLogger),
    godi.AddSingleton(NewDatabasePool),
)
```

### 3. Keep Modules Focused

```go
// Good: focused on one area
var UserModule = godi.NewModule("users", ...)
var OrderModule = godi.NewModule("orders", ...)

// Bad: kitchen sink module
var EverythingModule = godi.NewModule("everything", ...)
```

### 4. Document Cross-Module Dependencies

```go
// orders/module.go
package orders

// Module registers order-related services.
// Requires: infrastructure.Module, users.Module
var Module = godi.NewModule("orders",
    godi.AddScoped(NewOrderRepository),
    godi.AddScoped(NewOrderService),
)
```

---

**Next:** Explore [framework integrations](../integrations/) or [advanced features](../features/)
