# Quick Start - Learn godi in 5 Minutes

Start simple, add features as you need them. Here's godi from zero to hero.

## Install

```bash
go get github.com/junioryono/godi/v3
```

## Level 1: Basic DI (Start Here!)

The simplest possible example:

```go
package main

import (
    "fmt"
    "github.com/junioryono/godi/v3"
)

// Your service
type Greeter struct {
    name string
}

func NewGreeter() *Greeter {
    return &Greeter{name: "World"}
}

func (g *Greeter) Greet() string {
    return fmt.Sprintf("Hello, %s!", g.name)
}

// Module to organize services
var AppModule = godi.NewModule("app",
    godi.AddSingleton(NewGreeter),
)

func main() {
    // Setup
    services := godi.NewServiceCollection()
    services.AddModules(AppModule)

    provider, _ := services.BuildServiceProvider()
    defer provider.Close()

    // Use
    greeter, _ := godi.Resolve[*Greeter](provider)
    fmt.Println(greeter.Greet()) // Hello, World!
}
```

**That's it!** You're using dependency injection.

## Level 2: Multiple Services

Real apps have multiple services that depend on each other:

```go
// Logger service
type Logger struct{}

func NewLogger() *Logger {
    return &Logger{}
}

func (l *Logger) Log(msg string) {
    fmt.Printf("[LOG] %s\n", msg)
}

// Database service (depends on Logger)
type Database struct {
    logger *Logger
}

func NewDatabase(logger *Logger) *Database {
    logger.Log("Database connected")
    return &Database{logger: logger}
}

// Module with multiple services
var AppModule = godi.NewModule("app",
    godi.AddSingleton(NewLogger),
    godi.AddSingleton(NewDatabase),
)

func main() {
    services := godi.NewServiceCollection()
    services.AddModules(AppModule)

    provider, _ := services.BuildServiceProvider()
    defer provider.Close()

    // godi automatically injects Logger into Database!
    db, _ := godi.Resolve[*Database](provider)
    // Output: [LOG] Database connected
}
```

**Key insight**: You never manually created the Logger for Database. godi did it!

## Level 3: Web Apps with Scopes

For web apps, you want each request to have its own instances:

```go
// User service (scoped - new instance per request)
type UserService struct {
    db     *Database
    logger *Logger
}

func NewUserService(db *Database, logger *Logger) *UserService {
    return &UserService{db: db, logger: logger}
}

func (s *UserService) GetUser(id int) string {
    s.logger.Log(fmt.Sprintf("Getting user %d", id))
    return fmt.Sprintf("User-%d", id)
}

// Modules can depend on other modules
var CoreModule = godi.NewModule("core",
    godi.AddSingleton(NewLogger),
    godi.AddSingleton(NewDatabase),
)

var WebModule = godi.NewModule("web",
    CoreModule, // Include core services
    godi.AddScoped(NewUserService), // Scoped for requests!
)

// HTTP handler
func handleRequest(provider godi.ServiceProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Create scope for this request
        scope := provider.CreateScope(r.Context())
        defer scope.Close()

        // Get service for this request
        userService, _ := godi.Resolve[*UserService](scope)

        user := userService.GetUser(1)
        fmt.Fprint(w, user)
    }
}
```

**Why scopes?** Each request gets its own UserService. Perfect for request-specific data!

## Level 4: Testing with Mocks

Testing is where DI really shines:

```go
// Define interfaces for mocking
type Database interface {
    Query(sql string) []string
}

type Logger interface {
    Log(msg string)
}

// Real implementations
type SQLDatabase struct{}
func (d *SQLDatabase) Query(sql string) []string {
    // Real database query
    return []string{"real", "data"}
}

// Test implementations
type MockDatabase struct{}
func (d *MockDatabase) Query(sql string) []string {
    return []string{"test", "data"}
}

type MockLogger struct{}
func (l *MockLogger) Log(msg string) {
    // Silent in tests
}

// Test module with mocks
var TestModule = godi.NewModule("test",
    godi.AddSingleton(func() Database { return &MockDatabase{} }),
    godi.AddSingleton(func() Logger { return &MockLogger{} }),
    godi.AddScoped(NewUserService),
)

func TestUserService(t *testing.T) {
    services := godi.NewServiceCollection()
    services.AddModules(TestModule) // Use test module!

    provider, _ := services.BuildServiceProvider()
    defer provider.Close()

    userService, _ := godi.Resolve[*UserService](provider)
    // Test with mocks - no real database needed!
}
```

## Level 5: Module Organization

As your app grows, organize with multiple modules:

```go
// features/user/module.go
var UserModule = godi.NewModule("user",
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewUserService),
    godi.AddScoped(NewUserHandler),
)

// features/auth/module.go
var AuthModule = godi.NewModule("auth",
    godi.AddSingleton(NewTokenService),
    godi.AddScoped(NewAuthService),
    godi.AddScoped(NewAuthMiddleware),
)

// infrastructure/module.go
var InfraModule = godi.NewModule("infra",
    godi.AddSingleton(NewLogger),
    godi.AddSingleton(NewDatabase),
    godi.AddSingleton(NewCache),
)

// main.go
var AppModule = godi.NewModule("app",
    InfraModule,
    UserModule,
    AuthModule,
)

func main() {
    services := godi.NewServiceCollection()
    services.AddModules(AppModule) // Everything wired!
    // ...
}
```

## Common Patterns

### Pattern 1: Configuration

```go
type Config struct {
    DatabaseURL string
    Port        int
}

func NewConfig() *Config {
    return &Config{
        DatabaseURL: os.Getenv("DATABASE_URL"),
        Port:        8080,
    }
}

var ConfigModule = godi.NewModule("config",
    godi.AddSingleton(NewConfig),
)
```

### Pattern 2: Interfaces for Flexibility

```go
type Cache interface {
    Get(key string) (any, bool)
    Set(key string, value any)
}

// Easy to swap implementations
var DevModule = godi.NewModule("dev",
    godi.AddSingleton(func() Cache { return NewMemoryCache() }),
)

var ProdModule = godi.NewModule("prod",
    godi.AddSingleton(func() Cache { return NewRedisCache() }),
)
```

### Pattern 3: Groups of Services

```go
// Register multiple validators
var ValidatorModule = godi.NewModule("validators",
    godi.AddScoped(NewEmailValidator, godi.Group("validators")),
    godi.AddScoped(NewPhoneValidator, godi.Group("validators")),
    godi.AddScoped(NewAddressValidator, godi.Group("validators")),
)

// Use all validators
type ValidationService struct {
    validators []Validator
}

func NewValidationService(in struct {
    godi.In
    Validators []Validator `group:"validators"`
}) *ValidationService {
    return &ValidationService{validators: in.Validators}
}
```

## Cheat Sheet

```go
// 1. Create modules (group related services)
var MyModule = godi.NewModule("name",
    godi.AddSingleton(NewService),  // One instance
    godi.AddScoped(NewService),      // Per-request instance
)

// 2. Include other modules
var AppModule = godi.NewModule("app",
    CoreModule,    // Include dependencies
    MyModule,
)

// 3. Setup container
services := godi.NewServiceCollection()
services.AddModules(AppModule)
provider, _ := services.BuildServiceProvider()
defer provider.Close()

// 4. Use services
service, _ := godi.Resolve[*MyService](provider)

// 5. For web requests
scope := provider.CreateScope(ctx)
defer scope.Close()
service, _ := godi.Resolve[*MyService](scope)
```

## What's Next?

You now know 90% of what you need! For more:

- **[Full Tutorial](getting-started.md)** - Build a complete API
- **[Module Patterns](../howto/modules.md)** - Advanced organization
- **[Testing Guide](testing.md)** - Test like a pro

**Remember**: Start simple with basic modules, add features as you need them!
