# Comparing godi to Other DI Solutions

Quick comparison to help you choose the right tool.

## godi vs Manual Dependency Injection

**Manual DI**: Wire everything yourself

```go
// Manual DI
func main() {
    config := NewConfig()
    logger := NewLogger(config)
    db := NewDatabase(config, logger)
    cache := NewCache(config, logger)
    userRepo := NewUserRepository(db, cache, logger)
    userService := NewUserService(userRepo, logger)
    handler := NewHandler(userService, logger)

    // Manual cleanup
    defer db.Close()
    defer cache.Close()
}
```

**godi**: Automatic wiring

```go
// godi with modules
var AppModule = godi.NewModule("app",
    godi.AddSingleton(NewConfig),
    godi.AddSingleton(NewLogger),
    godi.AddSingleton(NewDatabase),
    godi.AddSingleton(NewCache),
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewUserService),
    godi.AddScoped(NewHandler),
)

func main() {
    services := godi.NewCollection()
    services.AddModules(AppModule)

    provider, _ := services.Build()
    defer provider.Close() // Automatic cleanup!

    handler, _ := godi.Resolve[*Handler](provider)
}
```

**When to use manual DI:**

- Very small apps (< 10 services)
- Simple CLI tools
- Learning projects

**When to use godi:**

- Web applications
- Apps with many services
- When you need testing
- Request-scoped isolation

## godi vs wire (Google)

**wire**: Compile-time code generation

```go
// wire.go
//+build wireinject

func InitializeApp() (*App, error) {
    wire.Build(
        NewConfig,
        NewDatabase,
        NewService,
        NewApp,
    )
    return nil, nil
}

// Run: wire gen ./...
// Generates: wire_gen.go with all wiring code
```

**godi**: Runtime dependency injection with modules

```go
// No code generation needed!
var AppModule = godi.NewModule("app",
    godi.AddSingleton(NewConfig),
    godi.AddSingleton(NewDatabase),
    godi.AddScoped(NewService),
    godi.AddScoped(NewApp),
)

// Use immediately
services := godi.NewCollection()
services.AddModules(AppModule)
provider, _ := services.Build()
app, _ := godi.Resolve[*App](provider)
```

**Key differences:**

| Feature         | wire                   | godi           |
| --------------- | ---------------------- | -------------- |
| When            | Compile time           | Runtime        |
| Setup           | Requires wire tool     | Just import    |
| Scoped services | ❌ No                  | ✅ Yes         |
| Speed           | Faster (no reflection) | Fast enough    |
| Flexibility     | Less (compile time)    | More (runtime) |
| Learning curve  | Steeper                | Easier         |

**Choose wire if:**

- You want zero runtime overhead
- You don't need scoped services
- You're OK with code generation

**Choose godi if:**

- You need scoped services (web apps)
- You want runtime flexibility
- You prefer no build tools

## godi vs fx (Uber)

**fx**: Full application framework

```go
// fx - application lifecycle
app := fx.New(
    fx.Provide(
        NewConfig,
        NewDatabase,
        NewServer,
    ),
    fx.Invoke(func(s *Server) {
        // Automatically called on start
    }),
)

app.Run() // Blocks and manages lifecycle
```

**godi**: Just dependency injection

```go
// godi - you control the app
var AppModule = godi.NewModule("app",
    godi.AddSingleton(NewConfig),
    godi.AddSingleton(NewDatabase),
    godi.AddSingleton(NewServer),
)

func main() {
    services := godi.NewCollection()
    services.AddModules(AppModule)

    provider, _ := services.Build()
    defer provider.Close()

    server, _ := godi.Resolve[*Server](provider)
    server.Run() // You control when/how
}
```

**Key differences:**

| Feature   | fx               | godi                |
| --------- | ---------------- | ------------------- |
| Scope     | Full framework   | Just DI             |
| Lifecycle | Managed          | Manual              |
| Hooks     | Start/Stop hooks | Constructor/Dispose |
| Learning  | More complex     | Simpler             |
| Control   | Less             | More                |

**Choose fx if:**

- You want a full framework
- You need complex lifecycle management
- Building microservices

**Choose godi if:**

- You just want dependency injection
- You prefer manual control
- You like .NET-style DI

## Quick Decision Guide

```
Need DI for Go?
    │
    ├─ Very small app?
    │   └─ Use Manual DI
    │
    ├─ Need compile-time safety + zero overhead?
    │   └─ Use wire
    │
    ├─ Need full application framework?
    │   └─ Use fx
    │
    └─ Need scoped services + simple API?
        └─ Use godi ✓
```

## Feature Comparison

| Feature             | Manual | wire | fx   | godi |
| ------------------- | ------ | ---- | ---- | ---- |
| No setup            | ✅     | ❌   | ❌   | ✅   |
| Type safe           | ✅     | ✅   | ⚠️   | ✅   |
| Scoped services     | ❌     | ❌   | ⚠️   | ✅   |
| Runtime flexibility | ❌     | ❌   | ✅   | ✅   |
| Zero overhead       | ✅     | ✅   | ❌   | ❌   |
| Testing support     | ⚠️     | ✅   | ✅   | ✅   |
| Modules             | ❌     | ❌   | ✅   | ✅   |
| Auto disposal       | ❌     | ❌   | ✅   | ✅   |
| Learning curve      | Easy   | Hard | Hard | Easy |

## For .NET Developers

If you're coming from .NET, godi will feel very familiar:

**.NET**:

```csharp
services.AddSingleton<ILogger, Logger>();
services.AddScoped<IUserService, UserService>();
var provider = services.Build();
var service = provider.GetService<IUserService>();
```

**godi**:

```go
services.AddSingleton(NewLogger)
services.AddScoped(NewUserService)
provider, _ := services.Build()
service, _ := godi.Resolve[UserService](provider)
```

Same concepts, Go syntax!

## Summary

- **Manual DI**: For tiny apps
- **wire**: For compile-time DI without scopes
- **fx**: For full application frameworks
- **godi**: For runtime DI with scopes (perfect for web apps)

Choose godi when you want simple, flexible dependency injection with support for request-scoped services!
