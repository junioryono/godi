# AddScoped vs Modules: When to Use What

One of the most common questions: "Should I use `services.AddScoped(...)` or create a module?" Let's make this simple.

## Start Simple: Direct Registration

For most apps, start with direct registration. It's clear, straightforward, and easy to understand:

```go
func main() {
    services := godi.NewServiceCollection()

    // Just list what you need
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewDatabase)
    services.AddScoped(NewUserRepository)
    services.AddScoped(NewUserService)
    services.AddScoped(NewAuthService)

    provider, _ := services.BuildServiceProvider()
    // ... use your app
}
```

**Use direct registration when:**

- Your app has < 20 services
- Everything is in one package
- You're just getting started
- The setup fits in one screen

## When You Need Modules

Modules are just a way to group related registrations together. Think of them as "setup functions" that you can reuse.

### Example: Your App Grows

When your main.go starts looking like this, it's time for modules:

```go
func main() {
    services := godi.NewServiceCollection()

    // Database stuff
    services.AddSingleton(NewDatabaseConfig)
    services.AddSingleton(NewDatabaseConnection)
    services.AddSingleton(NewMigrationRunner)
    services.AddScoped(NewTransaction)
    services.AddScoped(NewUserRepository)
    services.AddScoped(NewOrderRepository)
    services.AddScoped(NewProductRepository)

    // Auth stuff
    services.AddSingleton(NewJWTConfig)
    services.AddSingleton(NewPasswordHasher)
    services.AddScoped(NewAuthService)
    services.AddScoped(NewTokenService)
    services.AddScoped(NewPermissionService)

    // Email stuff
    services.AddSingleton(NewEmailConfig)
    services.AddSingleton(NewSMTPClient)
    services.AddTransient(NewEmailMessage)
    services.AddScoped(NewEmailService)

    // ... 50 more lines
}
```

### The Module Solution

Break it into logical groups:

```go
// database/module.go
package database

var Module = godi.Module("database",
    godi.AddSingleton(NewDatabaseConfig),
    godi.AddSingleton(NewDatabaseConnection),
    godi.AddSingleton(NewMigrationRunner),
    godi.AddScoped(NewTransaction),
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewOrderRepository),
    godi.AddScoped(NewProductRepository),
)

// auth/module.go
package auth

var Module = godi.Module("auth",
    godi.AddSingleton(NewJWTConfig),
    godi.AddSingleton(NewPasswordHasher),
    godi.AddScoped(NewAuthService),
    godi.AddScoped(NewTokenService),
    godi.AddScoped(NewPermissionService),
)

// email/module.go
package email

var Module = godi.Module("email",
    godi.AddSingleton(NewEmailConfig),
    godi.AddSingleton(NewSMTPClient),
    godi.AddTransient(NewEmailMessage),
    godi.AddScoped(NewEmailService),
)

// main.go - Now it's clean!
func main() {
    services := godi.NewServiceCollection()

    services.AddModules(
        database.Module,
        auth.Module,
        email.Module,
    )

    provider, _ := services.BuildServiceProvider()
}
```

## Real-World Module Examples

### Module Example 1: Feature Modules

When you have distinct features:

```go
// features/user/module.go
var UserModule = godi.Module("user",
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewUserService),
    godi.AddScoped(NewUserController),
    godi.AddScoped(NewUserValidator),
)

// features/billing/module.go
var BillingModule = godi.Module("billing",
    godi.AddSingleton(NewStripeClient),
    godi.AddScoped(NewInvoiceRepository),
    godi.AddScoped(NewPaymentService),
    godi.AddScoped(NewSubscriptionService),
)

// features/notifications/module.go
var NotificationModule = godi.Module("notifications",
    godi.AddSingleton(NewEmailClient),
    godi.AddSingleton(NewSMSClient),
    godi.AddScoped(NewNotificationService),
    godi.AddTransient(NewNotificationMessage),
)
```

### Module Example 2: Environment-Specific Modules

Different setups for different environments:

```go
// infrastructure/development.go
var DevelopmentModule = godi.Module("dev",
    godi.AddSingleton(func() Database {
        return NewSQLiteDatabase("dev.db")
    }),
    godi.AddSingleton(func() Cache {
        return NewMemoryCache()
    }),
    godi.AddSingleton(func() EmailClient {
        return NewMockEmailClient()
    }),
)

// infrastructure/production.go
var ProductionModule = godi.Module("prod",
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
    services := godi.NewServiceCollection()

    // Core services always needed
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewConfig)

    // Environment-specific module
    if os.Getenv("ENV") == "production" {
        services.AddModules(ProductionModule)
    } else {
        services.AddModules(DevelopmentModule)
    }

    // Feature modules
    services.AddModules(
        UserModule,
        BillingModule,
        NotificationModule,
    )
}
```

### Module Example 3: Shared Libraries

When you have common services used across projects:

```go
// In shared library: github.com/mycompany/shared/observability
var ObservabilityModule = godi.Module("observability",
    godi.AddSingleton(NewLogger),
    godi.AddSingleton(NewMetricsCollector),
    godi.AddSingleton(NewTracer),
    godi.AddScoped(NewRequestLogger),
    godi.AddScoped(NewRequestTracer),
)

// In your app
import "github.com/mycompany/shared/observability"

func main() {
    services := godi.NewServiceCollection()

    // Use shared module
    services.AddModules(observability.ObservabilityModule)

    // Add app-specific services
    services.AddScoped(NewUserService)
    // ...
}
```

## Module Best Practices

### 1. One Module Per Package

```
project/
├── auth/
│   ├── service.go
│   ├── repository.go
│   └── module.go      # auth.Module
├── user/
│   ├── service.go
│   ├── repository.go
│   └── module.go      # user.Module
└── main.go
```

### 2. Module Dependencies

```go
var DatabaseModule = godi.Module("database",
    godi.AddSingleton(NewConnection),
    godi.AddScoped(NewTransaction),
)

var UserModule = godi.Module("user",
    godi.AddModule(DatabaseModule), // Depends on database
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewUserService),
)
```

### 3. Keep Modules Focused

```go
// ✅ Good - focused module
var AuthModule = godi.Module("auth",
    godi.AddSingleton(NewJWTService),
    godi.AddScoped(NewAuthService),
    godi.AddScoped(NewPermissionService),
)

// ❌ Bad - kitchen sink module
var UtilModule = godi.Module("util",
    godi.AddSingleton(NewLogger),      // Should be in ObservabilityModule
    godi.AddSingleton(NewDatabase),    // Should be in DatabaseModule
    godi.AddScoped(NewEmailService),   // Should be in EmailModule
)
```

## Decision Guide

### Use Direct Registration When:

- ✅ Small applications (< 20 services)
- ✅ Prototyping or getting started
- ✅ All code in one package
- ✅ Simple scripts or tools

### Use Modules When:

- ✅ Services are organized in packages
- ✅ You want to reuse configurations
- ✅ Different environments need different setups
- ✅ You're building a library others will use
- ✅ Your main.go is getting too long

### Mix Both!

```go
func main() {
    services := godi.NewServiceCollection()

    // Use modules for organized features
    services.AddModules(
        database.Module,
        auth.Module,
    )

    // Direct registration for app-specific stuff
    services.AddSingleton(NewAppConfig)
    services.AddScoped(NewMainController)

    provider, _ := services.BuildServiceProvider()
}
```

## Summary

**Start simple with direct registration.** When you find yourself:

- Scrolling through a long list of registrations
- Copying registration code between projects
- Wanting different setups for different environments

...then it's time to use modules.

Modules are just a way to say "here's a group of related services that go together." They're not required - they're a convenience for when your app grows.
