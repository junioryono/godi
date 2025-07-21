# Simple Registration vs Modules

When should you use modules? Always! But let's understand why.

## The Evolution of Your Code

### Day 1: Simple Registration

When you first start, you might think this is fine:

```go
func main() {
    services := godi.NewServiceCollection()

    // Just a few services...
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewConfig)
    services.AddSingleton(NewDatabase)
    services.AddScoped(NewUserService)

    provider, _ := services.BuildServiceProvider()
    // ...
}
```

### Week 2: Growing Pains

Now you have more services:

```go
func main() {
    services := godi.NewServiceCollection()

    // Getting messy...
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewConfig)
    services.AddSingleton(NewDatabase)
    services.AddSingleton(NewCache)
    services.AddSingleton(NewEmailClient)
    services.AddSingleton(NewSMSClient)
    services.AddScoped(NewUserRepository)
    services.AddScoped(NewOrderRepository)
    services.AddScoped(NewProductRepository)
    services.AddScoped(NewUserService)
    services.AddScoped(NewOrderService)
    services.AddScoped(NewNotificationService)
    services.AddScoped(NewAuthService)

    // Where does what belong?
    // What depends on what?
    // How do I test parts of this?

    provider, _ := services.BuildServiceProvider()
}
```

### The Module Solution

Organize from the start with modules:

```go
// modules/core.go
var CoreModule = godi.NewModule("core",
    godi.AddSingleton(NewLogger),
    godi.AddSingleton(NewConfig),
)

// modules/data.go
var DataModule = godi.NewModule("data",
    CoreModule, // Depends on core
    godi.AddSingleton(NewDatabase),
    godi.AddSingleton(NewCache),
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewOrderRepository),
    godi.AddScoped(NewProductRepository),
)

// modules/notifications.go
var NotificationModule = godi.NewModule("notifications",
    CoreModule, // Also needs logging
    godi.AddSingleton(NewEmailClient),
    godi.AddSingleton(NewSMSClient),
    godi.AddScoped(NewNotificationService),
)

// modules/business.go
var BusinessModule = godi.NewModule("business",
    DataModule,
    NotificationModule,
    godi.AddScoped(NewUserService),
    godi.AddScoped(NewOrderService),
    godi.AddScoped(NewAuthService),
)

// main.go - Clean and simple!
func main() {
    services := godi.NewServiceCollection()
    services.AddModules(BusinessModule) // Includes everything!

    provider, _ := services.BuildServiceProvider()
}
```

## Why Always Use Modules?

### 1. Organization

Without modules:

```go
// 50 lines of service registrations
// No clear structure
// Hard to find anything
```

With modules:

```go
// Clear structure
CoreModule       // Config, logging
DataModule       // Database, repositories
BusinessModule   // Services, use cases
WebModule        // HTTP handlers, middleware
```

### 2. Dependencies Are Clear

```go
var OrderModule = godi.NewModule("orders",
    UserModule,      // Orders need users
    InventoryModule, // Orders need inventory
    PaymentModule,   // Orders need payment

    godi.AddScoped(NewOrderService),
    godi.AddScoped(NewOrderRepository),
)

// Dependencies are explicit!
```

### 3. Testing Is Easier

```go
// Test just the order functionality
func TestOrderService(t *testing.T) {
    testModule := godi.NewModule("test",
        // Mock dependencies
        MockUserModule,
        MockInventoryModule,
        MockPaymentModule,

        // Real order services
        godi.AddScoped(NewOrderService),
    )

    // Test in isolation!
}
```

### 4. Reusability

```go
// auth/module.go - Reusable auth module
var AuthModule = godi.NewModule("auth",
    godi.AddSingleton(NewJWTService),
    godi.AddScoped(NewAuthService),
    godi.AddScoped(NewPermissionService),
)

// Use in different apps
// app1/main.go
services.AddModules(AuthModule, App1Module)

// app2/main.go
services.AddModules(AuthModule, App2Module)
```

### 5. Environment Switching

```go
// environments/dev.go
var DevModule = godi.NewModule("dev",
    godi.AddSingleton(func() Database {
        return NewSQLiteDatabase(":memory:")
    }),
    godi.AddSingleton(func() EmailClient {
        return NewMockEmailClient()
    }),
)

// environments/prod.go
var ProdModule = godi.NewModule("prod",
    godi.AddSingleton(func() Database {
        return NewPostgresDatabase(os.Getenv("DATABASE_URL"))
    }),
    godi.AddSingleton(func() EmailClient {
        return NewSendGridClient(os.Getenv("SENDGRID_KEY"))
    }),
)

// main.go
func main() {
    services := godi.NewServiceCollection()

    // Core business logic
    services.AddModules(BusinessModule)

    // Environment-specific
    if os.Getenv("ENV") == "production" {
        services.AddModules(ProdModule)
    } else {
        services.AddModules(DevModule)
    }
}
```

## Module Best Practices

### Start with Modules from Day 1

Even for small apps:

```go
// Even tiny apps benefit from modules
var AppModule = godi.NewModule("app",
    godi.AddSingleton(NewConfig),
    godi.AddSingleton(NewService),
)

func main() {
    services := godi.NewServiceCollection()
    services.AddModules(AppModule) // Clean from the start
}
```

### One Module Per Feature/Layer

```go
// ✅ Good - Clear, focused modules
var UserModule = godi.NewModule("user", ...)
var OrderModule = godi.NewModule("order", ...)
var PaymentModule = godi.NewModule("payment", ...)

// ❌ Bad - Everything in one module
var AppModule = godi.NewModule("app",
    // 100 services here...
)
```

### Module Naming

```go
// ✅ Good names
var DatabaseModule = ...      // Infrastructure
var UserFeatureModule = ...    // Feature
var AuthenticationModule = ... // Capability

// ❌ Vague names
var StuffModule = ...
var Module1 = ...
var MiscModule = ...
```

## Migration Path

If you started with simple registration:

### Step 1: Group by Purpose

```go
// Before: main.go has everything
services.AddSingleton(NewLogger)
services.AddSingleton(NewDatabase)
services.AddScoped(NewUserService)
// ... 50 more lines

// After: Organized modules
var CoreModule = godi.NewModule("core",
    godi.AddSingleton(NewLogger),
)

var DataModule = godi.NewModule("data",
    CoreModule,
    godi.AddSingleton(NewDatabase),
)

var UserModule = godi.NewModule("user",
    DataModule,
    godi.AddScoped(NewUserService),
)
```

### Step 2: Extract to Files

```go
// modules/core.go
package modules

var CoreModule = godi.NewModule("core", ...)

// modules/data.go
package modules

var DataModule = godi.NewModule("data", ...)

// main.go
import "myapp/modules"

services.AddModules(modules.AppModule)
```

## Summary

**Always use modules because they:**

- Keep code organized
- Make dependencies explicit
- Enable easy testing
- Support reusability
- Allow environment switching

**Start with modules from day 1** - even tiny apps benefit from the organization.

The question isn't "should I use modules?" but "how should I organize my modules?"
