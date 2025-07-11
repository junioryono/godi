# Comparison with Other DI Solutions

This guide compares godi with other dependency injection solutions in the Go ecosystem, helping you choose the right tool for your project.

## Comparison Table

| Feature                | godi                          | wire               | fx             | dig        | Manual DI       |
| ---------------------- | ----------------------------- | ------------------ | -------------- | ---------- | --------------- |
| **Type Safety**        | ✅ Compile-time with generics | ✅ Code generation | ✅ Runtime     | ✅ Runtime | ✅ Compile-time |
| **Runtime/Compile**    | Runtime                       | Compile-time       | Runtime        | Runtime    | Manual          |
| **Scoped Lifetimes**   | ✅ Full support               | ❌ No              | ✅ Via modules | ❌ No      | Manual          |
| **Service Lifetimes**  | Singleton, Scoped, Transient  | Singleton only     | Singleton      | Singleton  | Manual          |
| **Learning Curve**     | Low (familiar API)            | Medium             | High           | Medium     | Low             |
| **Boilerplate**        | Minimal                       | Some               | Moderate       | Minimal    | High            |
| **Testing**            | Excellent                     | Good               | Good           | Good       | Depends         |
| **Performance**        | Good                          | Excellent          | Good           | Good       | Excellent       |
| **Microsoft DI Style** | ✅ Yes                        | ❌ No              | ❌ No          | ❌ No      | ❌ No           |

## Detailed Comparisons

### godi vs wire (Google)

**Wire** uses code generation to create dependency injection code at compile time.

```go
// Wire
//+build wireinject

func InitializeApp() (*App, error) {
    wire.Build(NewConfig, NewDatabase, NewService, NewApp)
    return nil, nil // Wire will generate this
}
```

```go
// godi
services := godi.NewServiceCollection()
services.AddSingleton(NewConfig)
services.AddSingleton(NewDatabase)
services.AddScoped(NewService)
services.AddScoped(NewApp)

provider, _ := services.BuildServiceProvider()
app, _ := godi.Resolve[*App](provider)
```

**Key Differences:**

- **Wire** generates code at compile time (faster runtime)
- **godi** resolves at runtime (more flexible)
- **Wire** requires build tags and code generation step
- **godi** works without any build tools
- **Wire** only supports singleton lifetime
- **godi** supports singleton, scoped, and transient lifetimes

**When to use Wire:**

- You want zero runtime overhead
- You prefer compile-time verification
- You don't need scoped services
- You're okay with code generation

**When to use godi:**

- You need scoped services (e.g., for web requests)
- You want Microsoft-style DI
- You prefer runtime flexibility
- You want to avoid code generation

### godi vs fx (Uber)

**Fx** is a full application framework with dependency injection.

```go
// Fx
app := fx.New(
    fx.Provide(
        NewConfig,
        NewDatabase,
        NewService,
        NewHTTPServer,
    ),
    fx.Invoke(func(*http.Server) {}),
)
app.Run()
```

```go
// godi
services := godi.NewServiceCollection()
services.AddSingleton(NewConfig)
services.AddSingleton(NewDatabase)
services.AddScoped(NewService)
services.AddSingleton(NewHTTPServer)

provider, _ := services.BuildServiceProvider()
server, _ := godi.Resolve[*http.Server](provider)
server.ListenAndServe()
```

**Key Differences:**

- **Fx** is a full framework with lifecycle management
- **godi** is just dependency injection
- **Fx** has built-in app lifecycle (start/stop hooks)
- **godi** requires manual lifecycle management
- **Fx** has a steeper learning curve
- **godi** has familiar API (especially for .NET developers)

**When to use Fx:**

- You want a full application framework
- You need complex lifecycle management
- You're building microservices
- You want built-in logging and metrics

**When to use godi:**

- You want just dependency injection
- You prefer Microsoft-style API
- You need scoped services
- You want to keep things simple

### godi vs dig (Uber)

**Dig** is the underlying DI container used by fx (and godi!).

```go
// Dig
container := dig.New()
container.Provide(NewConfig)
container.Provide(NewDatabase)
container.Provide(NewService)

err := container.Invoke(func(s *Service) {
    // Use service
})
```

```go
// godi
services := godi.NewServiceCollection()
services.AddSingleton(NewConfig)
services.AddSingleton(NewDatabase)
services.AddScoped(NewService)

provider, _ := services.BuildServiceProvider()
service, _ := godi.Resolve[*Service](provider)
```

**Key Differences:**

- **godi** is built on top of dig
- **godi** adds service lifetimes (scoped, transient)
- **godi** provides Microsoft-style API
- **dig** is lower level
- **godi** adds automatic disposal
- **godi** provides type-safe generics

**When to use Dig:**

- You want maximum control
- You only need singleton services
- You prefer minimal abstraction
- You're building your own DI framework

**When to use godi:**

- You want service lifetimes
- You need scoped services
- You prefer higher-level API
- You want automatic disposal

### godi vs Manual DI

**Manual DI** means wiring dependencies yourself.

```go
// Manual DI
func main() {
    config := NewConfig()
    logger := NewLogger(config)
    db := NewDatabase(config, logger)
    cache := NewCache(config)
    userRepo := NewUserRepository(db, logger)
    userService := NewUserService(userRepo, cache, logger)
    handler := NewHandler(userService, logger)

    // Manual cleanup
    defer db.Close()
    defer cache.Close()

    http.ListenAndServe(":8080", handler)
}
```

```go
// godi
func main() {
    services := godi.NewServiceCollection()
    services.AddSingleton(NewConfig)
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewDatabase)
    services.AddSingleton(NewCache)
    services.AddScoped(NewUserRepository)
    services.AddScoped(NewUserService)
    services.AddScoped(NewHandler)

    provider, _ := services.BuildServiceProvider()
    defer provider.Close() // Automatic cleanup

    handler, _ := godi.Resolve[*Handler](provider)
    http.ListenAndServe(":8080", handler)
}
```

**Key Differences:**

- **Manual** requires explicit wiring
- **godi** wires automatically
- **Manual** is faster (no overhead)
- **godi** is more maintainable
- **Manual** requires manual cleanup
- **godi** has automatic disposal

**When to use Manual DI:**

- Very small applications
- Performance-critical code
- You want full control
- Simple dependency graphs

**When to use godi:**

- Medium to large applications
- Complex dependency graphs
- You want automatic wiring
- You need service lifetimes

## Feature-by-Feature Comparison

### Service Lifetimes

| Framework  | Singleton | Scoped | Transient |
| ---------- | --------- | ------ | --------- |
| **godi**   | ✅        | ✅     | ✅        |
| **wire**   | ✅        | ❌     | ❌        |
| **fx**     | ✅        | ⚠️\*   | ❌        |
| **dig**    | ✅        | ⚠️\*   | ❌        |
| **Manual** | ✅        | ✅     | ✅        |

\* Possible with workarounds but not built-in

### Developer Experience

| Framework  | Setup Complexity | API Familiarity  | Documentation | Error Messages |
| ---------- | ---------------- | ---------------- | ------------- | -------------- |
| **godi**   | Low              | High (.NET-like) | Good          | Clear          |
| **wire**   | Medium           | Medium           | Excellent     | Compile-time   |
| **fx**     | High             | Low              | Good          | Detailed       |
| **dig**    | Low              | Medium           | Good          | Technical      |
| **Manual** | None             | N/A              | N/A           | Your own       |

### Testing Support

| Framework  | Mock Injection | Test Isolation | Setup Speed  |
| ---------- | -------------- | -------------- | ------------ |
| **godi**   | Excellent      | Excellent      | Fast         |
| **wire**   | Good           | Good           | Requires gen |
| **fx**     | Good           | Good           | Moderate     |
| **dig**    | Good           | Moderate       | Fast         |
| **Manual** | Depends        | Depends        | Fast         |

### Use Cases

| Use Case                     | Best Choice | Why                              |
| ---------------------------- | ----------- | -------------------------------- |
| Web API with request scoping | **godi**    | Built-in scoped lifetime support |
| CLI tool                     | **wire**    | Zero runtime overhead            |
| Microservice                 | **fx**      | Full framework with lifecycle    |
| Library                      | **Manual**  | No dependencies                  |
| Enterprise app               | **godi**    | Familiar patterns, full features |
| Performance critical         | **wire**    | Compile-time resolution          |

## Code Examples Side-by-Side

### Basic Setup

```go
// godi
collection := godi.NewServiceCollection()
collection.AddSingleton(NewService)
provider, _ := collection.BuildServiceProvider()

// wire
wire.Build(NewService)

// fx
fx.New(fx.Provide(NewService))

// dig
container := dig.New()
container.Provide(NewService)

// manual
service := NewService()
```

### With Dependencies

```go
// godi
collection.AddSingleton(NewDatabase)
collection.AddSingleton(NewLogger)
collection.AddScoped(NewUserService) // Auto-wires Database and Logger

// wire
wire.Build(NewDatabase, NewLogger, NewUserService)

// fx
fx.Provide(NewDatabase, NewLogger, NewUserService)

// dig
container.Provide(NewDatabase)
container.Provide(NewLogger)
container.Provide(NewUserService)

// manual
db := NewDatabase()
logger := NewLogger()
userService := NewUserService(db, logger)
```

### Testing

```go
// godi
testCollection := godi.NewServiceCollection()
testCollection.AddSingleton(func() Database { return &MockDB{} })
testCollection.AddScoped(NewUserService)

// wire
// Requires separate injector for tests

// fx
fx.New(
    fx.Provide(func() Database { return &MockDB{} }),
    fx.Provide(NewUserService),
)

// dig
container.Provide(func() Database { return &MockDB{} })
container.Provide(NewUserService)

// manual
mockDB := &MockDB{}
userService := NewUserService(mockDB, mockLogger)
```

## Decision Matrix

Choose **godi** if:

- ✅ You need scoped services (web apps)
- ✅ You want Microsoft-style DI
- ✅ You prefer runtime flexibility
- ✅ You want automatic disposal
- ✅ You need transient services
- ✅ You want familiar patterns

Choose **wire** if:

- ✅ You want zero runtime overhead
- ✅ You prefer compile-time safety
- ✅ You only need singletons
- ✅ You're building CLIs or tools
- ✅ You're okay with code generation

Choose **fx** if:

- ✅ You want a full framework
- ✅ You need lifecycle management
- ✅ You're building microservices
- ✅ You want built-in observability
- ✅ You need module system

Choose **dig** if:

- ✅ You want low-level control
- ✅ You're building a framework
- ✅ You only need singletons
- ✅ You want minimal abstraction

Choose **manual DI** if:

- ✅ Your app is very small
- ✅ You have simple dependencies
- ✅ You want zero overhead
- ✅ You want full control

## Migration Effort

| From → To     | Effort | Main Changes                   |
| ------------- | ------ | ------------------------------ |
| Manual → godi | Low    | Add service registration       |
| wire → godi   | Medium | Remove code gen, add lifetimes |
| fx → godi     | Medium | Remove lifecycle hooks         |
| dig → godi    | Low    | Add service collection         |
| godi → wire   | High   | Add code gen, remove scopes    |
| godi → fx     | Medium | Add lifecycle management       |

## Conclusion

**godi** fills a unique niche in the Go DI ecosystem:

- **Familiar API** for developers coming from .NET
- **Full lifetime support** including scoped and transient
- **Runtime flexibility** without code generation
- **Built on proven foundation** (Uber's dig)
- **Excellent for web applications** with request scoping

Choose the right tool for your specific needs, but if you want the most feature-complete DI solution with a familiar API, godi is an excellent choice.
