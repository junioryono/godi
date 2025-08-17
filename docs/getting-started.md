# Getting Started

Get up and running with godi in 5 minutes.

## Installation

```bash
go get github.com/junioryono/godi/v4
```

Requirements: Go 1.21 or later

## Your First App

Let's build something real - a simple blog API with users and posts. This shows you exactly how godi makes your life easier.

### Step 1: Define Your Services

Create a new file `main.go`:

```go
package main

import (
    "fmt"
    "log"
    "github.com/junioryono/godi/v4"
)

// A simple logger service
type Logger struct{}

func NewLogger() *Logger {
    return &Logger{}
}

func (l *Logger) Info(msg string) {
    log.Printf("[INFO] %s\n", msg)
}

// A database service that needs the logger
type Database struct {
    logger *Logger
}

func NewDatabase(logger *Logger) *Database {
    logger.Info("Database connected")
    return &Database{logger: logger}
}

func (db *Database) Query(sql string) []string {
    db.logger.Info("Executing: " + sql)
    return []string{"user1", "user2"}
}

// A user service that needs both
type UserService struct {
    db     *Database
    logger *Logger
}

func NewUserService(db *Database, logger *Logger) *UserService {
    return &UserService{db: db, logger: logger}
}

func (s *UserService) GetUsers() []string {
    s.logger.Info("Getting all users")
    return s.db.Query("SELECT * FROM users")
}
```

### Step 2: Wire Everything with godi

Add this to your main function:

```go
func main() {
    // Create a collection to register services
    collection := godi.NewCollection()

    // Register your services with their lifetimes
    collection.AddSingleton(NewLogger)      // One logger for the whole app
    collection.AddSingleton(NewDatabase)    // One database connection
    collection.AddScoped(NewUserService)    // New service per request

    // Build the provider
    provider, err := collection.Build()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // Use your services - godi handles all the wiring!
    userService, err := godi.Resolve[*UserService](provider)
    if err != nil {
        log.Fatal(err)
    }

    users := userService.GetUsers()
    fmt.Println("Users:", users)
}
```

### Step 3: Run It

```bash
go run main.go

# Output:
# [INFO] Database connected
# [INFO] Getting all users
# [INFO] Executing: SELECT * FROM users
# Users: [user1 user2]
```

🎉 **That's it!** You never had to manually create the Logger or Database - godi handled all the dependencies for you.

## Why This Matters

### Without godi

```go
func main() {
    // You have to wire everything manually
    logger := NewLogger()
    database := NewDatabase(logger)
    userService := NewUserService(database, logger)

    // And when you add a new dependency...
    cache := NewCache(logger)  // Now you need to update
    database := NewDatabase(logger, cache)  // this line
    userService := NewUserService(database, logger, cache)  // and this line
    // ... and every test that creates these services
}
```

### With godi

```go
// Just update the constructor
func NewDatabase(logger *Logger, cache *Cache) *Database {
    return &Database{logger: logger, cache: cache}
}

// That's it! godi handles injecting the cache everywhere
```

## Using Modules (Recommended)

As your app grows, organize services with modules:

```go
package main

import (
    "github.com/junioryono/godi/v4"
)

// Group related services
var CoreModule = godi.NewModule("core",
    godi.AddSingleton(NewLogger),
    godi.AddSingleton(NewDatabase),
)

var UserModule = godi.NewModule("user",
    CoreModule,  // Include dependencies
    godi.AddScoped(NewUserService),
)

func main() {
    collection := godi.NewCollection()
    collection.AddModules(UserModule)  // Clean!

    provider, _ := collection.Build()
    defer provider.Close()

    // Use as before
    userService, _ := godi.Resolve[*UserService](provider)
    users := userService.GetUsers()
}
```

## For Web Applications

If you're building a web app, use scopes to isolate each request:

```go
func handleRequest(provider godi.Provider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Create a scope for this request
        scope, err := provider.CreateScope(r.Context())
        if err != nil {
            http.Error(w, "Internal error", 500)
            return
        }
        defer scope.Close()

        // Each request gets its own instances of scoped services
        userService, _ := godi.Resolve[*UserService](scope)

        users := userService.GetUsers()
        json.NewEncoder(w).Encode(users)
    }
}
```

## Testing is Easy

With godi, testing becomes trivial:

```go
func TestUserService(t *testing.T) {
    // Create test module with mocks
    testModule := godi.NewModule("test",
        godi.AddSingleton(func() *Logger {
            return &MockLogger{}  // Use a mock
        }),
        godi.AddSingleton(func() *Database {
            return &MockDatabase{  // Use a mock
                users: []string{"test-user"},
            }
        }),
        godi.AddScoped(NewUserService),  // Real service with mock dependencies
    )

    collection := godi.NewCollection()
    collection.AddModules(testModule)

    provider, _ := collection.Build()
    defer provider.Close()

    // Test with mocks - no real database needed!
    service, _ := godi.Resolve[*UserService](provider)
    users := service.GetUsers()

    assert.Equal(t, []string{"test-user"}, users)
}
```

## Next Steps

Now that you have the basics:

1. **[Core Concepts](core-concepts.md)** - Understand services, lifetimes, and scopes (10 min)
2. **[Web Applications](guides/web-apps.md)** - Build REST APIs with request isolation
3. **[Testing Guide](guides/testing.md)** - Write better tests with mocks
4. **[Modules Guide](guides/modules.md)** - Organize larger applications

## Quick Tips

✅ **DO:**

- Start simple - add complexity only when needed
- Use modules to organize related services
- Use scopes for web requests
- Keep constructors simple - just dependency injection

❌ **DON'T:**

- Put business logic in constructors
- Forget to close scopes/providers
- Use singletons for request-specific data
- Over-complicate - godi is meant to be simple!

Remember: godi is just a tool to wire your dependencies. Your code stays clean, testable, and Go-idiomatic!
