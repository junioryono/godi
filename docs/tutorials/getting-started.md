# Getting Started with godi

Let's build something real - a web API that needs database connections, logging, and user sessions. This tutorial will show you why dependency injection makes your life easier.

## Why Use Dependency Injection?

Imagine you're building a web app. Without DI, you might write:

```go
func main() {
    // Manual setup - everything depends on everything else
    logger := NewLogger()
    db := NewDatabase(logger)
    userRepo := NewUserRepository(db, logger)
    authService := NewAuthService(userRepo, logger)
    handler := NewHandler(authService, logger)

    // What if you need to add email service to authService?
    // You'd have to update EVERY place that creates authService!
}
```

With godi, you just describe what you need:

```go
func main() {
    services := godi.NewServiceCollection()

    // Tell godi about your services
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewDatabase)
    services.AddScoped(NewUserRepository)
    services.AddScoped(NewAuthService)

    // godi figures out the wiring for you!
    provider, _ := services.BuildServiceProvider()

    // Get what you need
    handler, _ := godi.Resolve[*Handler](provider)
}
```

## Your First App: A Simple API

Let's build a real API with users and sessions. Create a new project:

```bash
mkdir my-api && cd my-api
go mod init my-api
go get github.com/junioryono/godi
```

### Step 1: Define Your Services

Create `main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "sync"
    "time"

    "github.com/junioryono/godi"
)

// Logger - everyone needs logging
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

// Database - shared connection
type Database struct {
    logger Logger
    // In real app: *sql.DB
}

func NewDatabase(logger Logger) *Database {
    logger.Info("Connecting to database...")
    return &Database{logger: logger}
}

// UserRepository - data access
type UserRepository struct {
    db     *Database
    logger Logger
}

func NewUserRepository(db *Database, logger Logger) *UserRepository {
    return &UserRepository{db: db, logger: logger}
}

func (r *UserRepository) GetUser(id string) string {
    r.logger.Info(fmt.Sprintf("Getting user %s", id))
    return fmt.Sprintf("User-%s", id)
}

// AuthService - business logic
type AuthService struct {
    repo   *UserRepository
    logger Logger
}

func NewAuthService(repo *UserRepository, logger Logger) *AuthService {
    return &AuthService{repo: repo, logger: logger}
}

func (s *AuthService) Login(userID string) string {
    user := s.repo.GetUser(userID)
    s.logger.Info(fmt.Sprintf("User %s logged in", user))
    return fmt.Sprintf("session-for-%s", user)
}
```

### Step 2: Wire Everything with godi

Add to your `main.go`:

```go
func main() {
    // Create service collection
    services := godi.NewServiceCollection()

    // Register services - order doesn't matter!
    services.AddSingleton(NewLogger)        // One logger for entire app
    services.AddSingleton(NewDatabase)      // One DB connection
    services.AddScoped(NewUserRepository)   // New repo per request
    services.AddScoped(NewAuthService)      // New service per request

    // Build the container
    provider, err := services.BuildServiceProvider()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // Simulate handling requests
    simulateRequests(provider)
}

func simulateRequests(provider godi.ServiceProvider) {
    // Simulate 3 concurrent requests
    var wg sync.WaitGroup

    for i := 1; i <= 3; i++ {
        wg.Add(1)
        go func(requestID int) {
            defer wg.Done()

            // Each request gets its own scope
            scope := provider.CreateScope(context.Background())
            defer scope.Close()

            // Get the auth service - godi injects all dependencies!
            authService, _ := godi.Resolve[*AuthService](scope)

            // Use the service
            session := authService.Login(fmt.Sprintf("user-%d", requestID))
            fmt.Printf("Request %d: %s\n", requestID, session)
        }(i)
    }

    wg.Wait()
}
```

Run it:

```bash
go run main.go
```

You'll see the logger and database are created once (singleton), but each request gets its own service instances (scoped).

## Understanding Service Lifetimes

### Singleton - Shared Across Everything

Use for:

- Loggers
- Database connections
- Configuration
- Caches

```go
services.AddSingleton(NewLogger)     // Created once, shared by all
services.AddSingleton(NewDatabase)   // One connection pool
```

### Scoped - One Per Request/Operation

Use for:

- Database transactions
- Request context
- User sessions
- Unit of work

```go
services.AddScoped(NewUserRepository)  // Fresh instance per request
services.AddScoped(NewAuthService)     // Isolated from other requests
```

## Real Example: Why Scoped Services Matter

Here's a real scenario showing why scoped services are powerful:

```go
// Session holds user info for current request
type Session struct {
    UserID    string
    UserName  string
    StartTime time.Time
}

func NewSession() *Session {
    return &Session{
        StartTime: time.Now(),
    }
}

// AuditLogger logs with session context
type AuditLogger struct {
    session *Session
    logger  Logger
}

func NewAuditLogger(session *Session, logger Logger) *AuditLogger {
    return &AuditLogger{session: session, logger: logger}
}

func (a *AuditLogger) LogAction(action string) {
    a.logger.Info(fmt.Sprintf("[User: %s] %s (session time: %v)",
        a.session.UserName, action, time.Since(a.session.StartTime)))
}

// UserService uses the audit logger
type UserServiceV2 struct {
    audit *AuditLogger
}

func NewUserServiceV2(audit *AuditLogger) *UserServiceV2 {
    return &UserServiceV2{audit: audit}
}

func (s *UserServiceV2) UpdateProfile(name string) {
    s.audit.LogAction(fmt.Sprintf("Updated profile to %s", name))
}

// Usage
func handleRequest(provider godi.ServiceProvider, userID, userName string) {
    // Create scope for this request
    scope := provider.CreateScope(context.Background())
    defer scope.Close()

    // Get session and populate it
    session, _ := godi.Resolve[*Session](scope)
    session.UserID = userID
    session.UserName = userName

    // Get service - it automatically has access to this request's session!
    userService, _ := godi.Resolve[*UserServiceV2](scope)
    userService.UpdateProfile("New Name")

    // The audit log shows: [User: John] Updated profile to New Name (session time: 50ms)
}
```

The magic: Every service in this request's scope automatically shares the same Session instance!

## Next Steps

Now you understand the basics:

1. **Services** are just regular Go types
2. **Constructors** are functions that godi calls
3. **Lifetimes** control when instances are created
4. **Scopes** isolate requests from each other

Ready for more? Check out:

- [Building a Web API](web-application.md) - Real HTTP server with DI
- [Using Modules](../howto/modules.md) - Organize services into groups
- [Testing with DI](testing.md) - Mock services easily

## Key Takeaways

✅ **Start simple** - You don't need every feature right away
✅ **Use scopes for requests** - Each request gets isolated instances
✅ **Let godi wire dependencies** - Just describe what you need
✅ **Test easily** - Swap real services for mocks

Remember: The goal is to write less boilerplate and focus on your business logic!
