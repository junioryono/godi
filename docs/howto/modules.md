# Modules

Modules provide a way to organize and group related service registrations. They promote code organization, reusability, and maintainability in larger applications.

## What are Modules?

A module is a function that configures a set of related services. Modules can:

- Group related services together
- Be reused across different applications
- Depend on other modules
- Encapsulate configuration logic

## Creating a Module

### Basic Module

```go
var DatabaseModule = godi.Module("database",
    godi.AddSingleton(NewDatabaseConfig),
    godi.AddSingleton(NewDatabaseConnection),
    godi.AddScoped(NewUnitOfWork),
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewOrderRepository),
)
```

### Module with Dependencies

```go
var LoggingModule = godi.Module("logging",
    godi.AddSingleton(NewLogConfig),
    godi.AddSingleton(NewLogger),
)

var DatabaseModule = godi.Module("database",
    godi.AddModule(LoggingModule), // Depend on logging
    godi.AddSingleton(NewDatabaseConnection),
    godi.AddScoped(NewRepository),
)
```

## Using Modules

### Single Module

```go
func main() {
    collection := godi.NewServiceCollection()

    // Add a module
    err := collection.AddModules(DatabaseModule)
    if err != nil {
        log.Fatal(err)
    }

    provider, _ := collection.BuildServiceProvider()
    defer provider.Close()
}
```

### Multiple Modules

```go
func main() {
    collection := godi.NewServiceCollection()

    // Add multiple modules
    err := collection.AddModules(
        CoreModule,
        DatabaseModule,
        CacheModule,
        APIModule,
    )
    if err != nil {
        log.Fatal(err)
    }

    provider, _ := collection.BuildServiceProvider()
    defer provider.Close()
}
```

## Module Patterns

### Layered Architecture

```go
// Core layer - no dependencies
var CoreModule = godi.Module("core",
    godi.AddSingleton(NewConfig),
    godi.AddSingleton(NewLogger),
    godi.AddSingleton(NewMetrics),
)

// Infrastructure layer - depends on core
var InfrastructureModule = godi.Module("infrastructure",
    godi.AddModule(CoreModule),
    godi.AddSingleton(NewDatabase),
    godi.AddSingleton(NewCache),
    godi.AddSingleton(NewMessageQueue),
)

// Domain layer - depends on infrastructure
var DomainModule = godi.Module("domain",
    godi.AddModule(InfrastructureModule),
    godi.AddScoped(NewUserService),
    godi.AddScoped(NewOrderService),
    godi.AddScoped(NewProductService),
)

// API layer - depends on domain
var APIModule = godi.Module("api",
    godi.AddModule(DomainModule),
    godi.AddScoped(NewUserController),
    godi.AddScoped(NewOrderController),
    godi.AddScoped(NewProductController),
)
```

### Feature Modules

```go
// User feature module
var UserFeatureModule = godi.Module("user-feature",
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewUserService),
    godi.AddScoped(NewUserController),
    godi.AddSingleton(NewUserValidator),
)

// Order feature module
var OrderFeatureModule = godi.Module("order-feature",
    godi.AddScoped(NewOrderRepository),
    godi.AddScoped(NewOrderService),
    godi.AddScoped(NewOrderController),
    godi.AddScoped(NewPaymentGateway),
)

// Combine features
var ApplicationModule = godi.Module("application",
    godi.AddModule(CoreModule),
    godi.AddModule(UserFeatureModule),
    godi.AddModule(OrderFeatureModule),
)
```

### Environment-Specific Modules

```go
// Development module
var DevelopmentModule = godi.Module("development",
    godi.AddSingleton(func() Cache { return NewMemoryCache() }),
    godi.AddSingleton(func() Database { return NewSQLiteDB() }),
    godi.AddSingleton(func() EmailService { return NewMockEmailService() }),
)

// Production module
var ProductionModule = godi.Module("production",
    godi.AddSingleton(func() Cache { return NewRedisCache() }),
    godi.AddSingleton(func() Database { return NewPostgresDB() }),
    godi.AddSingleton(func() EmailService { return NewSMTPEmailService() }),
)

// Select module based on environment
func GetEnvironmentModule(env string) func(ServiceCollection) error {
    switch env {
    case "production":
        return ProductionModule
    case "development":
        return DevelopmentModule
    default:
        return DevelopmentModule
    }
}
```

## Advanced Module Techniques

### Module with Configuration

```go
func DatabaseModuleWithConfig(dbConfig DatabaseConfig) func(godi.ServiceCollection) error {
    return godi.Module("database",
        godi.AddSingleton(func() *DatabaseConfig { return &dbConfig }),
        godi.AddSingleton(NewDatabaseConnection),
        godi.AddScoped(NewRepository),
    )
}

// Usage
dbConfig := DatabaseConfig{
    Host:     "localhost",
    Port:     5432,
    Database: "myapp",
}

collection.AddModules(DatabaseModuleWithConfig(dbConfig))
```

### Conditional Registration in Modules

```go
func APIModuleWithFeatures(features FeatureFlags) func(godi.ServiceCollection) error {
    return func(collection godi.ServiceCollection) error {
        // Always register core API services
        collection.AddScoped(NewUserController)
        collection.AddScoped(NewProductController)

        // Conditionally register features
        if features.OrdersEnabled {
            collection.AddScoped(NewOrderController)
            collection.AddScoped(NewOrderService)
        }

        if features.AnalyticsEnabled {
            collection.AddScoped(NewAnalyticsController)
            collection.AddSingleton(NewAnalyticsService)
        }

        if features.AdminEnabled {
            collection.AddScoped(NewAdminController)
            collection.AddScoped(NewAdminService)
        }

        return nil
    }
}
```

### Module Composition

```go
// Base modules
var LoggingModule = godi.Module("logging",
    godi.AddSingleton(NewLogger),
)

var MetricsModule = godi.Module("metrics",
    godi.AddSingleton(NewMetricsCollector),
)

var TracingModule = godi.Module("tracing",
    godi.AddSingleton(NewTracer),
)

// Composite module
var ObservabilityModule = godi.Module("observability",
    godi.AddModule(LoggingModule),
    godi.AddModule(MetricsModule),
    godi.AddModule(TracingModule),
    godi.AddSingleton(NewObservabilityService),
)
```

## Testing with Modules

### Test Module

```go
var TestModule = godi.Module("test",
    godi.AddSingleton(func() Database { return NewInMemoryDB() }),
    godi.AddSingleton(func() Cache { return NewMockCache() }),
    godi.AddSingleton(func() EmailService { return NewMockEmailService() }),
)

func TestUserService(t *testing.T) {
    collection := godi.NewServiceCollection()

    // Use test module instead of production modules
    collection.AddModules(
        TestModule,
        UserFeatureModule,
    )

    provider, _ := collection.BuildServiceProvider()
    defer provider.Close()

    // Test with mocked dependencies
    userService, _ := godi.Resolve[*UserService](provider)
    // ... run tests
}
```

### Module Override Pattern

```go
func TestWithOverrides(t *testing.T) {
    collection := godi.NewServiceCollection()

    // Add production module
    collection.AddModules(ProductionModule)

    // Override specific services for testing
    collection.Replace(godi.Singleton, func() EmailService {
        return &MockEmailService{
            shouldFail: true, // Test error cases
        }
    })

    provider, _ := collection.BuildServiceProvider()
    // ... run tests
}
```

## Module Organization

### Directory Structure

```
internal/
├── modules/
│   ├── core.go
│   ├── infrastructure.go
│   ├── domain.go
│   └── api.go
├── services/
│   ├── user/
│   ├── order/
│   └── product/
└── main.go
```

### Module File Example

```go
// internal/modules/infrastructure.go
package modules

import (
    "github.com/junioryono/godi"
    "myapp/internal/infrastructure/cache"
    "myapp/internal/infrastructure/database"
    "myapp/internal/infrastructure/messaging"
)

var InfrastructureModule = godi.Module("infrastructure",
    // Database
    godi.AddSingleton(database.NewConfig),
    godi.AddSingleton(database.NewConnection),
    godi.AddScoped(database.NewTransaction),

    // Cache
    godi.AddSingleton(cache.NewRedisConfig),
    godi.AddSingleton(cache.NewRedisClient),

    // Messaging
    godi.AddSingleton(messaging.NewRabbitMQConfig),
    godi.AddSingleton(messaging.NewPublisher),
    godi.AddSingleton(messaging.NewSubscriber),
)
```

## Best Practices

### 1. Single Responsibility

Each module should have a single, clear purpose:

```go
// ✅ Good - focused modules
var AuthModule = godi.Module("auth", ...)
var PaymentModule = godi.Module("payment", ...)

// ❌ Bad - mixed concerns
var UtilityModule = godi.Module("utility",
    godi.AddSingleton(NewAuth),
    godi.AddSingleton(NewPayment),
    godi.AddSingleton(NewEmail),
)
```

### 2. Clear Dependencies

Make module dependencies explicit:

```go
// ✅ Good - clear dependency chain
var AppModule = godi.Module("app",
    godi.AddModule(CoreModule),      // Explicit dependency
    godi.AddModule(DatabaseModule),  // Explicit dependency
    godi.AddScoped(NewAppService),
)
```

### 3. Avoid Circular Dependencies

```go
// ❌ Bad - circular dependency
var ModuleA = godi.Module("A",
    godi.AddModule(ModuleB), // A depends on B
)

var ModuleB = godi.Module("B",
    godi.AddModule(ModuleA), // B depends on A - circular!
)
```

### 4. Document Module Purpose

```go
// Package auth provides authentication and authorization services.
//
// The AuthModule includes:
// - JWT token generation and validation
// - User authentication service
// - Permission checking service
// - Password hashing utilities
//
// Dependencies:
// - CoreModule (for logging and configuration)
// - DatabaseModule (for user storage)
var AuthModule = godi.Module("auth",
    // ... registrations
)
```

### 5. Test Modules Independently

```go
func TestAuthModule(t *testing.T) {
    collection := godi.NewServiceCollection()

    // Test module can be loaded
    err := collection.AddModules(AuthModule)
    assert.NoError(t, err)

    // Test module provides expected services
    provider, _ := collection.BuildServiceProvider()

    _, err = godi.Resolve[AuthService](provider)
    assert.NoError(t, err)
}
```

## Module Patterns for Common Scenarios

### Web Application Module

```go
var WebModule = godi.Module("web",
    // Core web services
    godi.AddSingleton(NewRouter),
    godi.AddSingleton(NewMiddlewareChain),

    // Request-scoped services
    godi.AddScoped(NewRequestContext),
    godi.AddScoped(NewRequestLogger),

    // Controllers as groups
    godi.AddScoped(NewUserController, godi.Group("controllers")),
    godi.AddScoped(NewProductController, godi.Group("controllers")),

    // Middleware as groups
    godi.AddSingleton(NewAuthMiddleware, godi.Group("middleware")),
    godi.AddSingleton(NewLoggingMiddleware, godi.Group("middleware")),
)
```

### Background Jobs Module

```go
var JobsModule = godi.Module("jobs",
    // Job infrastructure
    godi.AddSingleton(NewJobScheduler),
    godi.AddSingleton(NewJobQueue),
    godi.AddScoped(NewJobExecutor),

    // Job handlers as groups
    godi.AddSingleton(NewEmailJob, godi.Group("jobs")),
    godi.AddSingleton(NewReportJob, godi.Group("jobs")),
    godi.AddSingleton(NewCleanupJob, godi.Group("jobs")),
)
```

### Plugin System Module

```go
// Plugin interface
type Plugin interface {
    Name() string
    Initialize(app *Application) error
}

// Create module that loads plugins
func PluginModule(pluginDir string) func(godi.ServiceCollection) error {
    return func(collection godi.ServiceCollection) error {
        // Scan plugin directory
        plugins, err := loadPlugins(pluginDir)
        if err != nil {
            return err
        }

        // Register each plugin
        for _, plugin := range plugins {
            p := plugin // capture
            collection.AddSingleton(func() Plugin { return p },
                godi.Group("plugins"))
        }

        return nil
    }
}
```

## Summary

Modules provide powerful organization capabilities:

- **Group** related services together
- **Reuse** common configurations
- **Compose** complex applications from simple parts
- **Test** components in isolation
- **Manage** dependencies explicitly

Use modules to keep your dependency injection configuration clean, maintainable, and testable as your application grows.
