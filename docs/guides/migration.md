# Migration Guide

This guide helps you migrate to godi from other dependency injection approaches.

## From Manual Dependency Injection

If you're manually wiring dependencies in `main()`:

### Before

```go
func main() {
    // Manual wiring
    config := NewConfig()
    logger := NewLogger(config)
    db := NewDatabase(config, logger)
    userRepo := NewUserRepository(db, logger)
    orderRepo := NewOrderRepository(db, logger)
    userService := NewUserService(userRepo, logger)
    orderService := NewOrderService(orderRepo, userService, logger)
    authService := NewAuthService(userService, logger)
    userController := NewUserController(userService, authService)
    orderController := NewOrderController(orderService, authService)

    // Use services
    http.Handle("/users", userController)
    http.Handle("/orders", orderController)
}
```

### After

```go
func main() {
    services := godi.NewCollection()

    // Register constructors - godi figures out the order
    services.AddSingleton(NewConfig)
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewDatabase)
    services.AddScoped(NewUserRepository)
    services.AddScoped(NewOrderRepository)
    services.AddScoped(NewUserService)
    services.AddScoped(NewOrderService)
    services.AddScoped(NewAuthService)
    services.AddScoped(NewUserController)
    services.AddScoped(NewOrderController)

    provider, _ := services.Build()
    defer provider.Close()

    // Use with HTTP
    mux := http.NewServeMux()
    mux.HandleFunc("/users", godihttp.Handle((*UserController).Handle))
    mux.HandleFunc("/orders", godihttp.Handle((*OrderController).Handle))
    handler := godihttp.ScopeMiddleware(provider)(mux)
}
```

### Benefits

- **No ordering required** - Register in any order
- **Request isolation** - Scoped services per request
- **Automatic cleanup** - Resources disposed properly
- **Validation** - Circular dependencies caught at build time

## From Uber Fx

### Fx Concepts → godi Concepts

| Uber Fx        | godi                                           |
| -------------- | ---------------------------------------------- |
| `fx.Provide`   | `services.AddSingleton/AddScoped/AddTransient` |
| `fx.Invoke`    | Resolve after Build                            |
| `fx.Module`    | `godi.Module`                                  |
| `fx.In`        | `godi.In`                                      |
| `fx.Out`       | `godi.Out`                                     |
| `fx.Lifecycle` | `Close()` method                               |

### Before (Fx)

```go
func main() {
    fx.New(
        fx.Provide(NewLogger),
        fx.Provide(NewDatabase),
        fx.Provide(NewUserService),
        fx.Invoke(func(s *UserService) {
            // Use service
        }),
    ).Run()
}
```

### After (godi)

```go
func main() {
    services := godi.NewCollection()
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewDatabase)
    services.AddScoped(NewUserService)

    provider, _ := services.Build()
    defer provider.Close()

    userService := godi.MustResolve[*UserService](provider)
    // Use service
}
```

### Lifecycle Hooks

```go
// Fx
fx.Lifecycle.Append(fx.Hook{
    OnStart: func(ctx context.Context) error { ... },
    OnStop: func(ctx context.Context) error { ... },
})

// godi - implement Close() for cleanup
type Database struct {
    conn *sql.DB
}

func (d *Database) Close() error {
    return d.conn.Close()
}

// Called automatically when provider.Close() is called
```

## From samber/do

### do Concepts → godi Concepts

| samber/do             | godi                    |
| --------------------- | ----------------------- |
| `do.Provide`          | `services.AddSingleton` |
| `do.ProvideTransient` | `services.AddTransient` |
| `do.Invoke`           | `godi.MustResolve`      |
| `do.Injector`         | `godi.Provider`         |

### Before (do)

```go
func main() {
    injector := do.New()

    do.Provide(injector, NewLogger)
    do.Provide(injector, NewDatabase)

    db := do.MustInvoke[*Database](injector)
}
```

### After (godi)

```go
func main() {
    services := godi.NewCollection()
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewDatabase)

    provider, _ := services.Build()
    defer provider.Close()

    db := godi.MustResolve[*Database](provider)
}
```

### Adding Scopes

godi adds scoped lifetime that do doesn't have:

```go
// godi: per-request services
services.AddScoped(NewRequestContext)
services.AddScoped(NewUserService)

scope, _ := provider.CreateScope(ctx)
defer scope.Close()

// Services isolated per scope
userService := godi.MustResolve[*UserService](scope)
```

## From Google Wire

Wire uses code generation; godi is runtime-based.

### Wire Concepts → godi Concepts

| Wire               | godi                           |
| ------------------ | ------------------------------ |
| Provider functions | Constructor functions          |
| `wire.NewSet`      | `godi.Module` or registrations |
| `wire.Build`       | `services.Build()`             |
| `wire.Bind`        | `godi.As[Interface]()`         |

### Before (Wire)

```go
// wire.go
func InitializeApp() (*App, error) {
    wire.Build(
        NewLogger,
        NewDatabase,
        NewUserService,
        NewApp,
    )
    return nil, nil
}

// Generated code creates App
```

### After (godi)

```go
func main() {
    services := godi.NewCollection()
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewDatabase)
    services.AddScoped(NewUserService)
    services.AddScoped(NewApp)

    provider, _ := services.Build()
    defer provider.Close()

    app := godi.MustResolve[*App](provider)
}
```

### Interface Binding

```go
// Wire
wire.Bind(new(Logger), new(*ConsoleLogger))

// godi
services.AddSingleton(NewConsoleLogger, godi.As[Logger]())
```

## Gradual Migration

You don't have to migrate everything at once:

### Step 1: Add godi for New Code

```go
// Keep existing manual wiring
oldDB := NewDatabase(config)

// Use godi for new features
services := godi.NewCollection()
services.AddSingleton(func() *Database { return oldDB }) // Bridge
services.AddScoped(NewNewFeatureService)

provider, _ := services.Build()
```

### Step 2: Move Services Incrementally

```go
// Move one service at a time
services.AddSingleton(NewLogger)  // Moved from manual
services.AddSingleton(NewDatabase)  // Moved from manual
services.AddSingleton(func() *OldService {
    return NewOldService(manualDeps)  // Still using old wiring
})
```

### Step 3: Full Migration

```go
// Eventually, everything through godi
services := godi.NewCollection()
services.AddModule(infrastructure.Module())
services.AddModule(users.Module())
services.AddModule(orders.Module())
```

## Common Migration Issues

### Constructor Signature Changes

```go
// Old: factory with no DI
func NewUserService() *UserService {
    logger := log.New(os.Stdout, "", 0)
    db := connectDB()
    return &UserService{logger, db}
}

// New: constructor with injected dependencies
func NewUserService(logger *Logger, db *Database) *UserService {
    return &UserService{logger, db}
}
```

### Global Variables

```go
// Old: global singleton
var globalDB *Database

func init() {
    globalDB = connectDB()
}

// New: inject through constructor
type UserService struct {
    db *Database  // Injected
}

func NewUserService(db *Database) *UserService {
    return &UserService{db: db}
}
```

### Testing After Migration

```go
// Old: hard to test, uses globals
func TestOld(t *testing.T) {
    globalDB = mockDB  // Mutate global (dangerous)
    // ...
}

// New: easy to test with mocks
func TestNew(t *testing.T) {
    services := godi.NewCollection()
    services.AddSingleton(func() *Database { return mockDB })
    services.AddScoped(NewUserService)

    provider, _ := services.Build()
    // Clean, isolated test
}
```

---

**Need more help?** Check the [API reference](https://pkg.go.dev/github.com/junioryono/godi/v4) or open an issue on [GitHub](https://github.com/junioryono/godi/issues).
