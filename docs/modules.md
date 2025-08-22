# Modules

Organize your services into logical groups for better maintainability and reusability.

## What are Modules?

Modules are a way to group related service registrations together. They help you:

- Organize large applications
- Share service configurations
- Enable/disable features
- Improve code reusability

```go
// Define a module
var DatabaseModule = godi.NewModule("database",
    godi.AddSingleton(NewDatabaseConfig),
    godi.AddSingleton(NewConnectionPool),
    godi.AddScoped(NewTransaction),
)

// Use the module
services := godi.NewCollection()
services.AddModules(DatabaseModule)
```

## Creating Modules

### Basic Module

```go
package auth

import "github.com/junioryono/godi/v4"

var Module = godi.NewModule("auth",
    godi.AddSingleton(NewPasswordHasher),
    godi.AddSingleton(NewJWTService),
    godi.AddScoped(NewAuthService),
    godi.AddScoped(NewUserValidator),
)
```

### Module with Configuration

```go
package database

type Options struct {
    Host     string
    Port     int
    Database string
    MaxConns int
}

func NewModule(opts Options) godi.ModuleOption {
    return godi.NewModule("database",
        // Register config
        godi.AddSingleton(func() *Config {
            return &Config{
                Host:     opts.Host,
                Port:     opts.Port,
                Database: opts.Database,
                MaxConns: opts.MaxConns,
            }
        }),

        // Register services
        godi.AddSingleton(NewConnectionPool),
        godi.AddScoped(NewQueryBuilder),
    )
}

// Usage
services.AddModules(database.NewModule(database.Options{
    Host:     "localhost",
    Port:     5432,
    Database: "myapp",
    MaxConns: 25,
}))
```

## Module Organization

### Layer-Based Structure

```
app/
├── infrastructure/
│   ├── module.go
│   ├── config.go
│   ├── database.go
│   └── cache.go
├── repositories/
│   ├── module.go
│   ├── user_repository.go
│   └── order_repository.go
├── services/
│   ├── module.go
│   ├── user_service.go
│   └── order_service.go
└── main.go
```

**Infrastructure Module:**

```go
package infrastructure

var Module = godi.NewModule("infrastructure",
    godi.AddSingleton(NewConfig),
    godi.AddSingleton(NewLogger),
    godi.AddSingleton(NewDatabase),
    godi.AddSingleton(NewCache),
)
```

**Repository Module:**

```go
package repositories

var Module = godi.NewModule("repositories",
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewOrderRepository),
    godi.AddScoped(NewProductRepository),
)
```

**Service Module:**

```go
package services

var Module = godi.NewModule("services",
    godi.AddScoped(NewUserService),
    godi.AddScoped(NewOrderService),
    godi.AddScoped(NewEmailService),
)
```

**Main Application:**

```go
package main

func main() {
    services := godi.NewCollection()

    // Add all modules
    services.AddModules(
        infrastructure.Module,
        repositories.Module,
        services.Module,
    )

    provider, _ := services.Build()
    defer provider.Close()

    // Start application
}
```

### Feature-Based Structure

```
app/
├── users/
│   ├── module.go
│   ├── user.go
│   ├── repository.go
│   └── service.go
├── orders/
│   ├── module.go
│   ├── order.go
│   ├── repository.go
│   └── service.go
└── main.go
```

**User Module:**

```go
package users

var Module = godi.NewModule("users",
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewUserService),
    godi.AddScoped(NewUserValidator),
)
```

**Order Module:**

```go
package orders

var Module = godi.NewModule("orders",
    godi.AddScoped(NewOrderRepository),
    godi.AddScoped(NewOrderService),
    godi.AddScoped(NewOrderProcessor),
)
```

## Composing Modules

### Nested Modules

```go
// Core module contains shared services
var CoreModule = godi.NewModule("core",
    godi.AddSingleton(NewLogger),
    godi.AddSingleton(NewConfig),
    godi.AddSingleton(NewMetrics),
)

// Database module
var DatabaseModule = godi.NewModule("database",
    godi.AddSingleton(NewConnectionPool),
    godi.AddScoped(NewTransaction),
)

// Application module combines others
var AppModule = godi.NewModule("app",
    CoreModule,      // Include core
    DatabaseModule,  // Include database
    godi.AddScoped(NewAppService),
)
```

### Conditional Modules

```go
func GetModules(config *Config) []godi.ModuleOption {
    modules := []godi.ModuleOption{
        CoreModule,
        DatabaseModule,
    }

    // Add cache if enabled
    if config.CacheEnabled {
        modules = append(modules, CacheModule)
    }

    // Add auth based on type
    switch config.AuthType {
    case "oauth":
        modules = append(modules, OAuthModule)
    case "basic":
        modules = append(modules, BasicAuthModule)
    }

    // Feature flags
    if config.Features.BetaAPI {
        modules = append(modules, BetaAPIModule)
    }

    return modules
}

// Usage
services.AddModules(GetModules(config)...)
```

## Module Dependencies

### Explicit Dependencies

```go
// Shared module - no dependencies
var SharedModule = godi.NewModule("shared",
    godi.AddSingleton(NewLogger),
    godi.AddSingleton(NewConfig),
)

// Database module - depends on shared
var DatabaseModule = godi.NewModule("database",
    godi.AddSingleton(NewDatabase), // Needs Logger from shared
)

// App module - depends on both
var AppModule = godi.NewModule("app",
    godi.AddScoped(NewAppService), // Needs Database and Logger
)

// Register in order
services.AddModules(
    SharedModule,   // First
    DatabaseModule, // Second
    AppModule,      // Third
)
```

## Testing with Modules

### Test Module

```go
// Production module
var ProductionModule = godi.NewModule("production",
    godi.AddSingleton(NewPostgresDB, godi.As[Database]()),
    godi.AddSingleton(NewRedisCache, godi.As[Cache]()),
)

// Test module with mocks
var TestModule = godi.NewModule("test",
    godi.AddSingleton(NewMockDB, godi.As[Database]()),
    godi.AddSingleton(NewMemoryCache, godi.As[Cache]()),
)

// Use in tests
func setupTestProvider() godi.Provider {
    services := godi.NewCollection()
    services.AddModules(
        TestModule,  // Use test module instead
        AppModule,
    )
    provider, _ := services.Build()
    return provider
}
```

### Module Isolation

```go
func TestUserModule(t *testing.T) {
    services := godi.NewCollection()

    // Add only required dependencies
    services.AddSingleton(NewMockDatabase)
    services.AddSingleton(NewTestLogger)

    // Add module under test
    services.AddModules(users.Module)

    provider, _ := services.Build()
    defer provider.Close()

    // Test module in isolation
    userService := godi.MustResolve[UserService](provider)
    // ... test userService
}
```

## Real-World Example

### E-commerce Application

```go
// Infrastructure module
var InfrastructureModule = godi.NewModule("infrastructure",
    godi.AddSingleton(NewConfig),
    godi.AddSingleton(NewLogger),
    godi.AddSingleton(NewDatabase),
    godi.AddSingleton(NewCache),
    godi.AddSingleton(NewMessageQueue),
)

// Auth module
var AuthModule = godi.NewModule("auth",
    godi.AddSingleton(NewJWTService),
    godi.AddScoped(NewAuthMiddleware),
    godi.AddScoped(NewSessionManager),
)

// Product catalog module
var CatalogModule = godi.NewModule("catalog",
    godi.AddScoped(NewProductRepository),
    godi.AddScoped(NewCategoryRepository),
    godi.AddScoped(NewCatalogService),
)

// Shopping cart module
var CartModule = godi.NewModule("cart",
    godi.AddScoped(NewCartRepository),
    godi.AddScoped(NewCartService),
    godi.AddTransient(NewCartCalculator),
)

// Order module
var OrderModule = godi.NewModule("orders",
    godi.AddScoped(NewOrderRepository),
    godi.AddScoped(NewOrderService),
    godi.AddScoped(NewPaymentProcessor),
    godi.AddScoped(NewShippingService),
)

// Main application
func main() {
    services := godi.NewCollection()

    services.AddModules(
        InfrastructureModule,
        AuthModule,
        CatalogModule,
        CartModule,
        OrderModule,
    )

    provider, _ := services.Build()
    defer provider.Close()

    // Start HTTP server
    server := NewServer(provider)
    server.Start(":8080")
}
```

## Best Practices

1. **Group related services** - Keep modules focused
2. **Use clear naming** - Module names should describe their purpose
3. **Document dependencies** - Make it clear what each module needs
4. **Keep modules small** - Easier to test and understand
5. **Avoid circular dependencies** - Modules shouldn't depend on each other circularly
6. **Test modules independently** - Each module should be testable in isolation

## Module Patterns

### Plugin System

```go
type Plugin interface {
    Name() string
    Initialize() error
}

func LoadPlugins(dir string) []godi.ModuleOption {
    var modules []godi.ModuleOption

    files, _ := os.ReadDir(dir)
    for _, file := range files {
        if strings.HasSuffix(file.Name(), ".so") {
            plugin := loadPlugin(file.Name())
            module := godi.NewModule(plugin.Name(),
                godi.AddSingleton(func() Plugin { return plugin }),
            )
            modules = append(modules, module)
        }
    }

    return modules
}
```

### Environment-Specific Modules

```go
func GetEnvironmentModules(env string) []godi.ModuleOption {
    common := []godi.ModuleOption{
        CoreModule,
        BusinessModule,
    }

    switch env {
    case "development":
        return append(common,
            DevDatabaseModule,
            MockPaymentModule,
            DebugModule,
        )
    case "staging":
        return append(common,
            StagingDatabaseModule,
            TestPaymentModule,
            MonitoringModule,
        )
    case "production":
        return append(common,
            ProductionDatabaseModule,
            RealPaymentModule,
            MonitoringModule,
            AlertingModule,
        )
    }

    return common
}
```

## Next Steps

- Explore [Keyed Services](keyed-services.md)
- Learn about [Service Groups](service-groups.md)
- Understand [Parameter Objects](parameter-objects.md)
