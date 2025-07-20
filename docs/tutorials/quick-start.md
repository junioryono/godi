# Quick Start: From Simple to Advanced

This guide shows you how to progressively adopt godi features as your needs grow.

## Level 1: Just the Basics (5 minutes)

Start with the absolute minimum:

```go
package main

import (
    "fmt"
    "github.com/junioryono/godi"
)

// Your types
type Logger struct{}
func (l *Logger) Log(msg string) { fmt.Println(msg) }

type Database struct {
    logger *Logger
}

type UserService struct {
    db     *Database
    logger *Logger
}

// Constructors
func NewLogger() *Logger {
    return &Logger{}
}

func NewDatabase(logger *Logger) *Database {
    return &Database{logger: logger}
}

func NewUserService(db *Database, logger *Logger) *UserService {
    return &UserService{db: db, logger: logger}
}

func main() {
    // Setup
    services := godi.NewServiceCollection()
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewDatabase)
    services.AddSingleton(NewUserService)

    // Use
    provider, _ := services.BuildServiceProvider()
    defer provider.Close()

    userService, _ := godi.Resolve[*UserService](provider)
    userService.logger.Log("It works!")
}
```

**That's it!** You're using DI. Everything else builds on this.

## Level 2: Add Scopes for Web Apps (10 minutes)

When building web APIs, use scopes for request isolation:

```go
// Add a request-scoped service
type RequestContext struct {
    UserID    string
    RequestID string
}

func NewRequestContext() *RequestContext {
    return &RequestContext{
        RequestID: uuid.NewString(),
    }
}

// Update registration
services.AddSingleton(NewLogger)
services.AddSingleton(NewDatabase)
services.AddScoped(NewRequestContext)  // One per request!
services.AddScoped(NewUserService)      // One per request!

// Handle HTTP requests
func HandleRequest(provider godi.ServiceProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Create request scope
        scope := provider.CreateScope(r.Context())
        defer scope.Close()

        // Set request info
        ctx, _ := godi.Resolve[*RequestContext](scope.ServiceProvider())
        ctx.UserID = getUserID(r)

        // Get service - it has access to request context!
        service, _ := godi.Resolve[*UserService](scope.ServiceProvider())
        service.DoSomething()
    }
}
```

**Why scopes?** Each request gets its own instances. No data leaks between requests!

## Level 3: Add Interfaces for Testing (15 minutes)

Make testing easy with interfaces:

```go
// Define interfaces
type Logger interface {
    Log(string)
}

type Database interface {
    Query(string) ([]Row, error)
}

type UserRepository interface {
    GetUser(id string) (*User, error)
}

// Implementations
type ConsoleLogger struct{}
func (l *ConsoleLogger) Log(msg string) { fmt.Println(msg) }

type PostgresDB struct { conn *sql.DB }
func (d *PostgresDB) Query(sql string) ([]Row, error) { /* ... */ }

// Easy testing
func TestUserService(t *testing.T) {
    services := godi.NewServiceCollection()

    // Use mocks
    services.AddSingleton(func() Logger {
        return &MockLogger{t: t}
    })
    services.AddSingleton(func() Database {
        return &MockDB{
            users: []User{{ID: "123", Name: "Test"}},
        }
    })
    services.AddScoped(NewUserService)

    provider, _ := services.BuildServiceProvider()
    service, _ := godi.Resolve[UserService](provider)

    // Test with mocks!
    user, err := service.GetUser("123")
    assert.NoError(t, err)
    assert.Equal(t, "Test", user.Name)
}
```

**Why interfaces?** Swap real implementations for test doubles easily.

## Level 4: Organize with Modules (20 minutes)

When your app grows, organize with modules:

```go
// auth/module.go
package auth

var Module = godi.Module("auth",
    godi.AddSingleton(NewPasswordHasher),
    godi.AddSingleton(NewJWTService),
    godi.AddScoped(NewAuthService),
)

// database/module.go
package database

var Module = godi.Module("database",
    godi.AddSingleton(NewConnection),
    godi.AddScoped(NewTransaction),
    godi.AddScoped(NewUserRepository),
)

// main.go - clean and organized!
func main() {
    services := godi.NewServiceCollection()

    services.AddModules(
        config.Module,
        database.Module,
        auth.Module,
        api.Module,
    )

    provider, _ := services.BuildServiceProvider()
    // ...
}
```

**Why modules?** Keep related services together. Reuse across projects.

## Level 5: Advanced Patterns (as needed)

### Keyed Services (Multiple Implementations)

```go
// Register variants
services.AddSingleton(NewFileLogger, godi.Name("file"))
services.AddSingleton(NewConsoleLogger, godi.Name("console"))

// Use specific one
logger, _ := godi.ResolveKeyed[Logger](provider, "file")
```

### Service Groups (Collections)

```go
// Register multiple
services.AddSingleton(NewUserValidator, godi.Group("validators"))
services.AddSingleton(NewEmailValidator, godi.Group("validators"))

// Get all
type App struct {
    godi.In
    Validators []Validator `group:"validators"`
}
```

### Decorators (Wrap Services)

```go
// Add behavior
func LoggingDecorator(service UserService, logger Logger) UserService {
    return &loggingUserService{inner: service, logger: logger}
}

services.Decorate(LoggingDecorator)
```

## Decision Tree: What Do I Need?

```
Start Here
    │
    ├─ Building a small CLI tool?
    │   └─ Use Level 1 (Basic DI)
    │
    ├─ Building a web API?
    │   └─ Use Level 2 (Add Scopes)
    │
    ├─ Need unit tests?
    │   └─ Use Level 3 (Add Interfaces)
    │
    ├─ App has 20+ services?
    │   └─ Use Level 4 (Add Modules)
    │
    └─ Need multiple implementations or advanced patterns?
        └─ Use Level 5 (Advanced features)
```

## Common Progression Path

Most projects follow this path:

1. **Week 1**: Basic DI for wiring (Level 1)
2. **Week 2**: Add scopes for web requests (Level 2)
3. **Week 3**: Add interfaces for testing (Level 3)
4. **Month 2**: Organize with modules (Level 4)
5. **Month 3+**: Add advanced features as needed (Level 5)

## Key Principle: Start Simple

❌ **Don't** start with every feature:

```go
// Too much too soon!
services.AddModules(
    CoreModule,
    WithDecoration(LoggingDecorator),
    WithKeyedServices("primary", "secondary"),
    WithGroups("validators", "handlers"),
)
```

✅ **Do** start simple and evolve:

```go
// Day 1
services.AddSingleton(NewLogger)
services.AddSingleton(NewDatabase)

// Week 2 - add scopes
services.AddScoped(NewUserService)

// Month 2 - add modules
services.AddModules(DatabaseModule)
```

## Summary

1. **Start with basic DI** - It's just functions and types
2. **Add scopes** when you build web apps
3. **Add interfaces** when you need tests
4. **Add modules** when organization matters
5. **Add advanced features** when you actually need them

The beauty of godi: You can start simple and grow as your needs grow. No big rewrites, just progressive enhancement.

**Ready?** Copy the Level 1 example and start coding!
