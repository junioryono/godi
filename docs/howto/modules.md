# How to Use Modules

Modules are the best way to organize your godi services. Think of them as packages of related functionality.

## Basic Module

Start simple - group related services:

```go
// email/module.go
package email

import "github.com/junioryono/godi/v3"

var Module = godi.NewModule("email",
    godi.AddSingleton(NewSMTPClient),
    godi.AddScoped(NewEmailService),
    godi.AddScoped(NewEmailValidator),
)
```

Use it:

```go
// main.go
services := godi.NewCollection()
services.AddModules(email.Module)

provider, _ := services.Build()
```

## Module Dependencies

Modules can include other modules:

```go
// core/module.go
var CoreModule = godi.NewModule("core",
    godi.AddSingleton(NewConfig),
    godi.AddSingleton(NewLogger),
)

// database/module.go
var DatabaseModule = godi.NewModule("database",
    CoreModule, // Depends on core!
    godi.AddSingleton(NewDatabaseConnection),
    godi.AddScoped(NewTransaction),
)

// user/module.go
var UserModule = godi.NewModule("user",
    DatabaseModule, // Depends on database (which includes core)
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewUserService),
)
```

## Real-World Examples

### Example 1: Web API Module Structure

```go
// infrastructure/logger/module.go
var LoggerModule = godi.NewModule("logger",
    godi.AddSingleton(NewLogConfig),
    godi.AddSingleton(NewLogger),
)

// infrastructure/database/module.go
var DatabaseModule = godi.NewModule("database",
    LoggerModule,
    godi.AddSingleton(NewDatabaseConfig),
    godi.AddSingleton(NewDatabaseConnection),
    godi.AddScoped(NewTransaction),
)

// infrastructure/cache/module.go
var CacheModule = godi.NewModule("cache",
    LoggerModule,
    godi.AddSingleton(NewRedisConfig),
    godi.AddSingleton(NewRedisClient),
)

// features/user/module.go
var UserModule = godi.NewModule("user",
    DatabaseModule,
    CacheModule,
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewUserService),
    godi.AddScoped(NewUserHandler),
)

// features/auth/module.go
var AuthModule = godi.NewModule("auth",
    UserModule, // Auth depends on users
    godi.AddSingleton(NewJWTService),
    godi.AddScoped(NewAuthService),
    godi.AddScoped(NewAuthMiddleware),
)

// app/module.go
var AppModule = godi.NewModule("app",
    UserModule,
    AuthModule,
    // Add more feature modules as needed
)
```

### Example 2: Environment-Specific Modules

```go
// environments/development.go
var DevelopmentModule = godi.NewModule("dev",
    godi.AddSingleton(func() Database {
        return NewSQLiteDatabase(":memory:")
    }),
    godi.AddSingleton(func() Cache {
        return NewMemoryCache()
    }),
    godi.AddSingleton(func() EmailClient {
        return NewMockEmailClient()
    }),
)

// environments/production.go
var ProductionModule = godi.NewModule("prod",
    godi.AddSingleton(func() Database {
        return NewPostgresDatabase(os.Getenv("DATABASE_URL"))
    }),
    godi.AddSingleton(func() Cache {
        return NewRedisCache(os.Getenv("REDIS_URL"))
    }),
    godi.AddSingleton(func() EmailClient {
        return NewSendGridClient(os.Getenv("SENDGRID_KEY"))
    }),
)

// main.go
func main() {
    // Base module with business logic
    baseModule := godi.NewModule("base",
        UserModule,
        AuthModule,
        OrderModule,
    )

    // Choose environment
    var envModule godi.ModuleOption
    if os.Getenv("ENV") == "production" {
        envModule = ProductionModule
    } else {
        envModule = DevelopmentModule
    }

    // Combine modules
    appModule := godi.NewModule("app",
        envModule,
        baseModule,
    )

    // Build and run
    services := godi.NewCollection()
    services.AddModules(appModule)
    provider, _ := services.Build()
}
```

### Example 3: Plugin System with Modules

```go
// plugin/interface.go
type Plugin interface {
    Name() string
    Initialize() error
}

// plugins/analytics/module.go
var AnalyticsModule = godi.NewModule("analytics-plugin",
    godi.AddSingleton(NewAnalyticsClient),
    godi.AddSingleton(func(client *AnalyticsClient) Plugin {
        return &AnalyticsPlugin{client: client}
    }, godi.Group("plugins")),
)

// plugins/monitoring/module.go
var MonitoringModule = godi.NewModule("monitoring-plugin",
    godi.AddSingleton(NewMetricsCollector),
    godi.AddSingleton(func(collector *MetricsCollector) Plugin {
        return &MonitoringPlugin{collector: collector}
    }, godi.Group("plugins")),
)

// app/plugin_manager.go
type PluginManager struct {
    plugins []Plugin
}

func NewPluginManager(in struct {
    godi.In
    Plugins []Plugin `group:"plugins"`
}) *PluginManager {
    return &PluginManager{plugins: in.Plugins}
}

// main.go
var AppModule = godi.NewModule("app",
    CoreModule,
    AnalyticsModule,
    MonitoringModule,
    godi.AddSingleton(NewPluginManager),
)
```

## Advanced Patterns

### Dynamic Module Configuration

```go
func DatabaseModule(config DatabaseConfig) godi.ModuleOption {
    return godi.NewModule("database",
        godi.AddSingleton(func() *DatabaseConfig {
            return &config
        }),
        godi.AddSingleton(NewDatabaseConnection),
    )
}

// Use with configuration
dbConfig := DatabaseConfig{
    Host:     "localhost",
    Port:     5432,
    Database: "myapp",
}

appModule := godi.NewModule("app",
    DatabaseModule(dbConfig),
    UserModule,
)
```

### Conditional Module Registration

```go
func AppModule(features FeatureFlags) godi.ModuleOption {
    modules := []godi.ModuleOption{
        CoreModule,
        DatabaseModule,
    }

    if features.UsersEnabled {
        modules = append(modules, UserModule)
    }

    if features.BillingEnabled {
        modules = append(modules, BillingModule)
    }

    if features.AnalyticsEnabled {
        modules = append(modules, AnalyticsModule)
    }

    return godi.NewModule("app", modules...)
}
```

### Testing with Module Overrides

```go
// Create test module that replaces services
var TestOverridesModule = godi.NewModule("test-overrides",
    godi.AddSingleton(func() Database {
        return &MockDatabase{
            users: []User{
                {ID: "1", Name: "Test User"},
            },
        }
    }),
    godi.AddSingleton(func() EmailClient {
        return &MockEmailClient{
            sentEmails: []Email{},
        }
    }),
)

func TestUserService(t *testing.T) {
    // Combine production module with test overrides
    testModule := godi.NewModule("test",
        UserModule,        // Production user logic
        TestOverridesModule, // Test implementations
    )

    services := godi.NewCollection()
    services.AddModules(testModule)

    provider, _ := services.Build()
    // Test with mocked dependencies
}
```

## Best Practices

### 1. One Module Per Package

```go
// ✅ Good: user/module.go
package user

var Module = godi.NewModule("user",
    godi.AddScoped(NewRepository),
    godi.AddScoped(NewService),
    godi.AddScoped(NewHandler),
)
```

### 2. Clear Module Dependencies

```go
// ✅ Good: Explicit dependencies
var OrderModule = godi.NewModule("order",
    UserModule,     // Orders need users
    InventoryModule, // Orders need inventory
    PaymentModule,   // Orders need payment
    godi.AddScoped(NewOrderService),
)
```

### 3. Module Naming Conventions

```go
// ✅ Good: Clear, consistent names
var UserModule = godi.NewModule("user", ...)
var AuthModule = godi.NewModule("auth", ...)
var CoreModule = godi.NewModule("core", ...)

// ❌ Bad: Inconsistent or unclear
var UsrMod = godi.NewModule("usr", ...)
var Authentication = godi.NewModule("authentication", ...)
var BasicStuff = godi.NewModule("basic", ...)
```

### 4. Document Module Purpose

```go
// Package user provides user management functionality.
//
// The UserModule includes:
// - User repository for database operations
// - User service for business logic
// - User handler for HTTP endpoints
// - User validator for input validation
//
// Dependencies:
// - DatabaseModule (for database connection)
// - CacheModule (for caching user data)
var UserModule = godi.NewModule("user",
    DatabaseModule,
    CacheModule,
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewUserService),
    godi.AddScoped(NewUserHandler),
    godi.AddScoped(NewUserValidator),
)
```

## Common Patterns

### Feature Toggle Module

```go
type Features struct {
    NewCheckout bool
    BetaSearch  bool
}

func FeatureModule(features Features) godi.ModuleOption {
    return godi.NewModule("features",
        godi.AddSingleton(func() Features { return features }),
    )
}

// In services
func NewSearchService(features Features, /* other deps */) *SearchService {
    if features.BetaSearch {
        return &BetaSearchService{/* ... */}
    }
    return &SearchService{/* ... */}
}
```

### Multi-Tenant Module

```go
var TenantModule = godi.NewModule("tenant",
    godi.AddScoped(func(ctx context.Context) *TenantContext {
        tenantID := ctx.Value("tenantID").(string)
        return &TenantContext{TenantID: tenantID}
    }),
)

// Services automatically get tenant context
func NewUserRepository(db *Database, tenant *TenantContext) *UserRepository {
    return &UserRepository{
        db:       db,
        tenantID: tenant.TenantID,
    }
}
```

## Summary

Modules are powerful because they:

- **Organize** your code into logical units
- **Encapsulate** dependencies
- **Enable** reuse across projects
- **Simplify** testing with easy overrides
- **Scale** from simple apps to complex systems

Start with simple modules and add complexity as needed. The goal is clarity and maintainability!
