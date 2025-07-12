# Why Use Dependency Injection?

Let's be honest - Go developers are skeptical of dependency injection. "It's too complex!" "Go is simple!" "I can wire things manually!"

They're not wrong. But here's what changes when your app grows...

## The Problem: A Real Example

You start with a simple app:

```go
func main() {
    logger := log.New(os.Stdout, "", log.LstdFlags)
    db := openDatabase()

    userRepo := &UserRepository{db: db, logger: logger}
    emailService := &EmailService{logger: logger}
    userService := &UserService{repo: userRepo, email: emailService, logger: logger}

    handler := &Handler{userService: userService, logger: logger}
    http.ListenAndServe(":8080", handler)
}
```

Looks fine, right? Now your boss says: "We need to add caching."

## The Cascade of Changes

You add a cache parameter to UserRepository:

```go
type UserRepository struct {
    db     *sql.DB
    logger Logger
    cache  Cache  // NEW!
}
```

Now you have to update EVERYWHERE that creates a UserRepository:

```go
// main.go
cache := createCache()  // Add this
userRepo := &UserRepository{db: db, logger: logger, cache: cache}  // Update this

// user_test.go - Update 15 test files
repo := &UserRepository{db: mockDB, logger: testLogger, cache: mockCache}

// integration_test.go - Update integration tests
repo := &UserRepository{db: testDB, logger: logger, cache: testCache}

// benchmarks_test.go - Update benchmarks too
repo := &UserRepository{db: benchDB, logger: perfLogger, cache: benchCache}
```

**You just wanted to add caching, but you touched 20 files!**

## The DI Solution

With godi, you change exactly ONE place:

```go
// Just update the constructor
func NewUserRepository(db *sql.DB, logger Logger, cache Cache) *UserRepository {
    return &UserRepository{db: db, logger: logger, cache: cache}
}

// That's it. Seriously. godi handles the rest.
```

## Real-World Benefits

### 1. Testing Becomes Trivial

**Without DI:**

```go
func TestUserService(t *testing.T) {
    // Oh no, I need to create the entire dependency tree!
    logger := &MockLogger{}
    db := createTestDB()
    cache := &MockCache{}
    emailClient := &MockEmailClient{}

    userRepo := &UserRepository{db: db, logger: logger, cache: cache}
    emailService := &EmailService{client: emailClient, logger: logger}
    userService := &UserService{repo: userRepo, email: emailService, logger: logger}

    // Finally can test...
}
```

**With DI:**

```go
func TestUserService(t *testing.T) {
    services := godi.NewServiceCollection()

    // Just register mocks
    services.AddSingleton(func() UserRepository { return &MockUserRepository{} })
    services.AddSingleton(func() EmailService { return &MockEmailService{} })
    services.AddScoped(NewUserService)

    provider, _ := services.BuildServiceProvider()
    userService, _ := godi.Resolve[*UserService](provider)

    // Test away!
}
```

### 2. Request Isolation in Web Apps

**Without DI:**

```go
// Dangerous! Shared transaction across requests
var globalTx *sql.Tx

func HandleRequest(w http.ResponseWriter, r *http.Request) {
    tx, _ := db.Begin()
    globalTx = tx  // Race condition!

    // ... do work ...

    tx.Commit()
}
```

**With DI:**

```go
func HandleRequest(provider godi.ServiceProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Each request gets its own scope
        scope := provider.CreateScope(r.Context())
        defer scope.Close()

        // Automatic transaction per request!
        service, _ := godi.Resolve[*UserService](scope.ServiceProvider())
        service.CreateUser(...)  // Uses this request's transaction
    }
}
```

### 3. Clean Architectural Boundaries

**Without DI:**

```go
// Everything knows about everything
type UserService struct {
    db       *sql.DB        // Knows about database
    logger   *log.Logger    // Knows about logging
    smtp     *smtp.Client   // Knows about email
    redis    *redis.Client  // Knows about caching
    config   *Config        // Knows about config
}
```

**With DI:**

```go
// Clean interfaces
type UserService struct {
    repo    UserRepository  // Just interfaces!
    email   EmailSender
    cache   Cache
    logger  Logger
}

// Easy to swap implementations
services.AddSingleton(func() EmailSender {
    if config.Dev {
        return &MockEmailSender{}
    }
    return &SMTPEmailSender{}
})
```

### 4. Resource Management

**Without DI:**

```go
func main() {
    logger := createLogger()
    defer logger.Close()  // Don't forget!

    db := createDB()
    defer db.Close()  // Don't forget!

    cache := createCache()
    defer cache.Close()  // Don't forget!

    queue := createQueue()
    defer queue.Close()  // Getting error-prone...

    // ... 20 more resources
}
```

**With DI:**

```go
func main() {
    services := godi.NewServiceCollection()
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewDatabase)
    services.AddSingleton(NewCache)
    services.AddSingleton(NewQueue)

    provider, _ := services.BuildServiceProvider()
    defer provider.Close()  // Closes EVERYTHING in the right order!
}
```

## Common Concerns Addressed

### "But I like Go's simplicity!"

godi IS simple. Look at this:

```go
// 1. Say what you have
services.AddSingleton(NewLogger)
services.AddScoped(NewUserService)

// 2. Get what you need
userService, _ := godi.Resolve[*UserService](provider)

// That's it. No magic, no reflection, no struct tags.
```

### "I don't want a framework!"

godi isn't a framework. It's a container. Your services don't know about godi:

```go
// This is just a normal Go function
func NewUserService(repo UserRepository, logger Logger) *UserService {
    return &UserService{repo: repo, logger: logger}
}

// No imports from godi, no base classes, no annotations
```

### "It's overkill for small apps!"

True! If your entire app is 200 lines, you don't need DI. But when you have:

- Multiple services
- Unit tests
- Integration tests
- Different environments
- Team members

...DI pays for itself quickly.

## The Real Magic: Examples

### Adding Multi-Tenancy

Without DI: Rewrite half your app to pass tenant context everywhere.

With DI: Add one scoped service:

```go
services.AddScoped(NewTenantContext)
// Now every service in that scope has access to the tenant!
```

### Adding Request Tracing

Without DI: Add traceID parameter to 50 functions.

With DI: Add one scoped service:

```go
services.AddScoped(NewRequestTracing)
// Every service automatically includes trace IDs in logs!
```

### Switching Databases

Without DI: Find and update every place that creates connections.

With DI: Change one line:

```go
// From
services.AddSingleton(NewMySQLDatabase)
// To
services.AddSingleton(NewPostgresDatabase)
```

## When You Really Need DI

You **need** DI when:

- ✅ Adding a dependency means updating 10+ files
- ✅ Testing requires complex setup
- ✅ You have request-scoped state (transactions, user context)
- ✅ Different environments need different implementations
- ✅ You're copying setup code between tests

You **don't need** DI when:

- ❌ Your app is a single file
- ❌ You have no tests
- ❌ You never change dependencies
- ❌ It's a simple CLI tool

## Summary: The 80/20 of DI

**80% of DI value comes from these 20% of features:**

1. **Automatic Wiring** - Change constructors, not callers
2. **Easy Testing** - Swap implementations trivially
3. **Request Scoping** - Isolate operations from each other
4. **Lifecycle Management** - Automatic cleanup

That's it. Not complex. Not magic. Just solving real problems.

**Ready to try it?** Start with the [Getting Started Tutorial](../tutorials/getting-started.md). You'll be productive in 10 minutes.
