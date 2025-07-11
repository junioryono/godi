# Why Dependency Injection?

Dependency Injection (DI) is a design pattern that has transformed how developers build maintainable, testable, and scalable applications. While Go's simplicity is one of its greatest strengths, as applications grow, managing dependencies becomes increasingly complex.

## The Problem: Dependency Management at Scale

Consider a typical web application without DI:

```go
func main() {
    // Manual dependency wiring
    config := loadConfig()
    logger := log.New(os.Stdout, "", log.LstdFlags)

    db, err := sql.Open("postgres", config.DatabaseURL)
    if err != nil {
        logger.Fatal(err)
    }
    defer db.Close()

    cache := redis.NewClient(&redis.Options{
        Addr: config.RedisURL,
    })
    defer cache.Close()

    emailClient := email.NewClient(config.SMTPHost, config.SMTPPort)

    userRepo := repository.NewUserRepository(db, logger)
    authService := service.NewAuthService(userRepo, emailClient, logger)
    userService := service.NewUserService(userRepo, cache, logger)

    handlers := api.NewHandlers(authService, userService, logger)

    // ... more services and wiring
}
```

### Problems with this approach:

1. **Constructor Changes Cascade** - Add a parameter to `NewUserRepository`, and you must update every place it's constructed
2. **Testing is Difficult** - Creating a service for testing requires constructing all its dependencies
3. **No Lifecycle Management** - Manual cleanup with multiple `defer` statements
4. **Hidden Dependencies** - Hard to see the full dependency graph
5. **Boilerplate Explosion** - As the app grows, main() becomes unwieldy

## The Solution: Dependency Injection with godi

Here's the same application with godi:

```go
func main() {
    // Define services
    services := godi.NewServiceCollection()

    // Register infrastructure
    services.AddSingleton(loadConfig)
    services.AddSingleton(newLogger)
    services.AddSingleton(newDatabase)
    services.AddSingleton(newCache)
    services.AddSingleton(newEmailClient)

    // Register repositories
    services.AddScoped(repository.NewUserRepository)

    // Register services
    services.AddScoped(service.NewAuthService)
    services.AddScoped(service.NewUserService)

    // Register handlers
    services.AddScoped(api.NewHandlers)

    // Build the container
    provider, err := services.BuildServiceProvider()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close() // Automatic cleanup!

    // Start the application
    handlers, _ := godi.Resolve[*api.Handlers](provider)
    http.ListenAndServe(":8080", handlers)
}
```

## Key Benefits

### 1. Never Touch Constructors Again

When you add a new dependency, you only change two places:

- The constructor that needs it
- The service registration

Everyone else gets the updated dependencies automatically!

```go
// Before: Add metrics to UserService
func NewUserService(repo UserRepository, cache Cache, logger Logger) *UserService

// After: Just add the parameter
func NewUserService(repo UserRepository, cache Cache, logger Logger, metrics Metrics) *UserService

// That's it! No need to update callers
```

### 2. Request Scoping for Web Applications

One of godi's killer features is request scoping:

```go
func middleware(provider godi.ServiceProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Create a scope for this request
        scope := provider.CreateScope(r.Context())
        defer scope.Close()

        // All services resolved in this scope share the same instances
        // Perfect for request-scoped database transactions!

        handler, _ := godi.Resolve[*MyHandler](scope.ServiceProvider())
        handler.ServeHTTP(w, r)
    }
}
```

### 3. Testability Built-In

Testing becomes trivial with DI:

```go
func TestUserService(t *testing.T) {
    services := godi.NewServiceCollection()

    // Register mocks
    services.AddSingleton(func() UserRepository {
        return &MockUserRepository{
            users: []User{{ID: 1, Name: "Test"}},
        }
    })
    services.AddSingleton(func() Cache { return &MockCache{} })
    services.AddSingleton(func() Logger { return &MockLogger{} })

    provider, _ := services.BuildServiceProvider()
    defer provider.Close()

    // Test with real service but mock dependencies
    userService, _ := godi.Resolve[*UserService](provider)
    user, err := userService.GetUser(1)

    assert.NoError(t, err)
    assert.Equal(t, "Test", user.Name)
}
```

### 4. Automatic Resource Management

godi automatically handles cleanup for services implementing `Disposable`:

```go
type DatabaseConnection struct {
    db *sql.DB
}

func (d *DatabaseConnection) Close() error {
    return d.db.Close()
}

// When the provider is closed, all disposable services are cleaned up
// in reverse order of creation (LIFO)
```

### 5. Clear Dependency Graph

With godi, dependencies are explicit and centralized:

```go
// Easy to see what depends on what
services.AddSingleton(NewLogger)                    // No dependencies
services.AddSingleton(NewDatabase)                  // Depends on: Config, Logger
services.AddScoped(NewUserRepository)               // Depends on: Database, Logger
services.AddScoped(NewUserService)                  // Depends on: UserRepository, Cache, Logger
```

## Real-World Benefits

### Large Team Development

- **Parallel Development**: Teams can work on different services without coordination
- **Clear Contracts**: Interfaces define boundaries between team responsibilities
- **Easy Integration**: New services plug in without modifying existing code

### Microservices

- **Consistent Pattern**: Same DI pattern across all services
- **Easy Testing**: Test services in isolation
- **Configuration Management**: Centralized service configuration

### Growing Applications

- **Add Features**: New services integrate seamlessly
- **Refactor Safely**: Change implementations without touching consumers
- **Scale Gradually**: Start simple, add complexity as needed

## Common Concerns Addressed

### "But Go is simple! This adds complexity!"

godi embraces Go's simplicity:

- Uses standard Go functions as constructors
- No reflection magic or struct tags
- Clear, explicit registration
- Type-safe with compile-time checking

### "I can just pass dependencies manually"

True for small apps, but consider:

- A service with 10 dependencies, each with 5 dependencies
- That's 50 manual wirings to maintain
- Add one dependency, update 50 places
- With DI: update 1 place

### "What about performance?"

- Service resolution is optimized and cached
- Overhead is negligible compared to benefits
- Most resolution happens at startup
- Runtime performance identical to manual wiring

## Conclusion

Dependency Injection with godi isn't about adding complexity—it's about managing complexity that already exists in your application. It provides structure and patterns that make large Go applications maintainable, testable, and enjoyable to work with.

Ready to get started? Check out our [Getting Started Tutorial](../tutorials/getting-started.md)!
