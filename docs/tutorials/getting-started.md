# Getting Started with godi

Let's build a real web API in 10 minutes. We'll create a user service with authentication to show you how godi makes your life easier.

## Why Dependency Injection?

**The Problem**: Your app is growing. Every time you add a dependency, you update 20 files. Testing requires complex setup. Sound familiar?

**The Solution**: godi wires everything automatically. Change a constructor once, godi handles the rest.

## Installation

```bash
go get github.com/junioryono/godi/v4
```

## Your First App: User API

Let's build a real API with users and authentication. We'll use modules to keep things organized.

### Step 1: Define Your Services

Create these files in your project:

**services/logger.go**

```go
package services

import "log"

type Logger interface {
    Info(msg string)
    Error(msg string)
}

type ConsoleLogger struct{}

func NewLogger() Logger {
    return &ConsoleLogger{}
}

func (l *ConsoleLogger) Info(msg string) {
    log.Printf("[INFO] %s", msg)
}

func (l *ConsoleLogger) Error(msg string) {
    log.Printf("[ERROR] %s", msg)
}
```

**services/database.go**

```go
package services

type Database struct {
    logger Logger
}

func NewDatabase(logger Logger) *Database {
    logger.Info("Database connected")
    return &Database{logger: logger}
}

func (db *Database) Query(sql string) []map[string]any {
    db.logger.Info("Executing: " + sql)
    // Simulate database query
    return []map[string]any{
        {"id": 1, "name": "Alice"},
        {"id": 2, "name": "Bob"},
    }
}
```

**services/user.go**

```go
package services

import "fmt"

type UserService struct {
    db     *Database
    logger Logger
}

func NewUserService(db *Database, logger Logger) *UserService {
    return &UserService{db: db, logger: logger}
}

func (s *UserService) GetUser(id int) (string, error) {
    s.logger.Info(fmt.Sprintf("Getting user %d", id))
    users := s.db.Query(fmt.Sprintf("SELECT * FROM users WHERE id = %d", id))

    if len(users) > 0 {
        return users[0]["name"].(string), nil
    }
    return "", fmt.Errorf("user not found")
}

func (s *UserService) CreateUser(name string) error {
    s.logger.Info(fmt.Sprintf("Creating user: %s", name))
    // In real app, insert into database
    return nil
}
```

### Step 2: Create Modules

This is where godi shines. Create **modules/modules.go**:

```go
package modules

import (
    "github.com/junioryono/godi/v4"
    "myapp/services"
)

// CoreModule - Infrastructure services that rarely change
var CoreModule = godi.NewModule("core",
    godi.AddSingleton(services.NewLogger),
    godi.AddSingleton(services.NewDatabase),
)

// UserModule - All user-related services
var UserModule = godi.NewModule("user",
    CoreModule, // Depends on core services
    godi.AddScoped(services.NewUserService),
)

// AppModule - Your complete application
var AppModule = godi.NewModule("app",
    UserModule,
    // Add more modules as your app grows!
)
```

### Step 3: Wire Everything in main.go

```go
package main

import (
    "fmt"
    "log"
    "net/http"

    "github.com/junioryono/godi/v4"
    "myapp/modules"
    "myapp/services"
)

func main() {
    // Create DI container
    collection := godi.NewCollection()

    // Add your app module (includes everything!)
    if err := collection.AddModules(modules.AppModule); err != nil {
        log.Fatal(err)
    }

    // Build the provider
    provider, err := collection.Build()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // Set up HTTP handlers
    http.HandleFunc("/users/", handleUser(provider))

    log.Println("Server starting on :8080")
    log.Fatal(http.ListenAndServe(":8080", nil))
}

func handleUser(provider godi.ServiceProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Create a scope for this request
        scope := provider.CreateScope(r.Context())
        defer scope.Close()

        // Get services for this request
        userService, err := godi.Resolve[*services.UserService](scope)
        if err != nil {
            http.Error(w, "Service error", http.StatusInternalServerError)
            return
        }

        // Use the service
        user, err := userService.GetUser(1)
        if err != nil {
            http.Error(w, "User not found", http.StatusNotFound)
            return
        }

        fmt.Fprintf(w, "User: %s\n", user)
    }
}
```

### Step 4: Run Your App

```bash
go run .

# In another terminal:
curl http://localhost:8080/users/
# Output: User: Alice
```

## What Just Happened?

1. **We defined services** - Just regular Go types
2. **We created modules** - Grouped related services
3. **We used scopes** - Each request gets fresh instances
4. **godi wired everything** - No manual dependency management!

## Adding Features is Easy

Want to add caching? Just update the module:

```go
// services/cache.go
type Cache interface {
    Get(key string) (any, bool)
    Set(key string, value any)
}

type MemoryCache struct {
    data map[string]any
}

func NewCache() Cache {
    return &MemoryCache{data: make(map[string]any)}
}

// Update UserService constructor
func NewUserService(db *Database, logger Logger, cache Cache) *UserService {
    return &UserService{db: db, logger: logger, cache: cache}
}

// Update CoreModule
var CoreModule = godi.NewModule("core",
    godi.AddSingleton(services.NewLogger),
    godi.AddSingleton(services.NewDatabase),
    godi.AddSingleton(services.NewCache), // Just add this!
)
```

That's it! godi automatically provides the cache to UserService. No need to update main.go or anywhere else.

## Testing is a Breeze

Create **services/user_test.go**:

```go
package services_test

import (
    "testing"
    "github.com/junioryono/godi/v4"
    "myapp/services"
)

// Mock implementations
type MockDatabase struct{}

func (m *MockDatabase) Query(sql string) []map[string]any {
    return []map[string]any{
        {"id": 1, "name": "Test User"},
    }
}

type MockLogger struct{}
func (m *MockLogger) Info(msg string) {}
func (m *MockLogger) Error(msg string) {}

func TestUserService(t *testing.T) {
    // Create test module with mocks
    testModule := godi.NewModule("test",
        godi.AddSingleton(func() services.Logger { return &MockLogger{} }),
        godi.AddSingleton(func() *services.Database { return &MockDatabase{} }),
        godi.AddScoped(services.NewUserService),
    )

    // Set up DI
    collection := godi.NewCollection()
    collection.AddModules(testModule)

    provider, _ := collection.Build()
    defer provider.Close()

    // Test your service
    userService, _ := godi.Resolve[*services.UserService](provider)

    user, err := userService.GetUser(1)
    if err != nil {
        t.Fatal(err)
    }

    if user != "Test User" {
        t.Errorf("Expected Test User, got %s", user)
    }
}
```

## Key Concepts

### 1. Services

Your regular Go types - no magic, no framework dependencies.

### 2. Modules

Groups of related services. Makes your code organized and reusable.

### 3. Lifetimes

- **Singleton**: One instance for the entire app (database, logger)
- **Scoped**: New instance per request (user service, repositories)

### 4. Scopes

Isolate each request. Perfect for web apps!

## Next Steps

✅ You've learned the basics! Here's what to explore next:

1. **[Web Application Tutorial](web-application.md)** - Build a complete REST API
2. **[Testing Guide](testing.md)** - Write better tests with DI
3. **[Module Patterns](../howto/modules.md)** - Advanced module techniques

## Tips for Success

1. **Start with modules** - Even for small apps, they keep things organized
2. **Use scopes for web apps** - Each request should be isolated
3. **Keep constructors simple** - Just dependency injection, no logic
4. **Test with mocks** - Swap real services for test doubles

Remember: godi is just a tool to wire your dependencies. Your code stays clean, testable, and Go-idiomatic!
