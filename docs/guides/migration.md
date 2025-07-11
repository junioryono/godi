# Migration Guide

This guide helps you migrate to godi from other dependency injection solutions or manual dependency management.

## Migrating from Manual Dependency Management

### Before: Manual Wiring

```go
func main() {
    // Manual dependency creation
    config := loadConfig()
    logger := log.New(os.Stdout, "[APP] ", log.LstdFlags)

    db, err := sql.Open("postgres", config.DatabaseURL)
    if err != nil {
        logger.Fatal(err)
    }
    defer db.Close()

    cache := redis.NewClient(&redis.Options{
        Addr: config.RedisURL,
    })
    defer cache.Close()

    // Manual injection
    userRepo := repository.NewUserRepository(db, logger)
    emailService := email.NewService(config.SMTPHost, config.SMTPPort, logger)
    userService := service.NewUserService(userRepo, emailService, cache, logger)
    authService := service.NewAuthService(userRepo, logger, config.JWTSecret)

    // More manual wiring...
    handler := api.NewHandler(userService, authService, logger)

    // Start server
    http.ListenAndServe(":8080", handler)
}
```

### After: With godi

```go
func main() {
    // Configure services
    collection := godi.NewServiceCollection()

    // Register services - order doesn't matter!
    collection.AddSingleton(loadConfig)
    collection.AddSingleton(newLogger)
    collection.AddSingleton(newDatabase)
    collection.AddSingleton(newCache)
    collection.AddSingleton(email.NewService)
    collection.AddScoped(repository.NewUserRepository)
    collection.AddScoped(service.NewUserService)
    collection.AddScoped(service.NewAuthService)
    collection.AddScoped(api.NewHandler)

    // Build provider
    provider, err := collection.BuildServiceProvider()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close() // Automatic cleanup!

    // Resolve and start
    handler, _ := godi.Resolve[*api.Handler](provider)
    http.ListenAndServe(":8080", handler)
}
```

### Migration Steps

1. **Identify your services and their dependencies**
2. **Create constructor functions** that accept dependencies as parameters
3. **Register services** with appropriate lifetimes
4. **Replace manual wiring** with service resolution
5. **Remove manual cleanup** - godi handles disposal

## Migrating from Google Wire

### Wire Approach

```go
// wire.go
//+build wireinject

package main

import "github.com/google/wire"

func InitializeApp() (*App, error) {
    wire.Build(
        NewConfig,
        NewLogger,
        NewDatabase,
        NewRepository,
        NewService,
        NewApp,
    )
    return nil, nil
}
```

### godi Approach

```go
// main.go - no code generation needed!
package main

func InitializeApp() (*App, error) {
    collection := godi.NewServiceCollection()

    // Register services
    collection.AddSingleton(NewConfig)
    collection.AddSingleton(NewLogger)
    collection.AddSingleton(NewDatabase)
    collection.AddScoped(NewRepository)
    collection.AddScoped(NewService)
    collection.AddScoped(NewApp)

    // Build provider
    provider, err := collection.BuildServiceProvider()
    if err != nil {
        return nil, err
    }

    // Resolve app
    return godi.Resolve[*App](provider)
}
```

### Key Differences

| Feature            | Wire          | godi         |
| ------------------ | ------------- | ------------ |
| When               | Compile-time  | Runtime      |
| Code Generation    | Required      | Not needed   |
| Build Tags         | Required      | Not needed   |
| Scoped Services    | Limited       | Full support |
| Groups             | Via providers | Built-in     |
| Runtime Resolution | No            | Yes          |

### Migration Steps

1. **Remove wire.go files** and build tags
2. **Convert providers** to service registrations
3. **Replace wire.Build** with ServiceCollection
4. **Update build process** - no more wire generation
5. **Add lifetime management** (scoped, transient)

## Migrating from Uber Fx

### Fx Approach

```go
package main

import "go.uber.org/fx"

func main() {
    app := fx.New(
        fx.Provide(
            NewConfig,
            NewLogger,
            NewDatabase,
            NewRepository,
            NewService,
            NewHTTPServer,
        ),
        fx.Invoke(startServer),
    )

    app.Run()
}

func startServer(server *HTTPServer, lifecycle fx.Lifecycle) {
    lifecycle.Append(fx.Hook{
        OnStart: func(ctx context.Context) error {
            go server.Start()
            return nil
        },
        OnStop: func(ctx context.Context) error {
            return server.Stop(ctx)
        },
    })
}
```

### godi Approach

```go
package main

func main() {
    collection := godi.NewServiceCollection()

    // Register services
    collection.AddSingleton(NewConfig)
    collection.AddSingleton(NewLogger)
    collection.AddSingleton(NewDatabase)
    collection.AddScoped(NewRepository)
    collection.AddScoped(NewService)
    collection.AddSingleton(NewHTTPServer)

    // Build provider
    provider, err := collection.BuildServiceProvider()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // Start server
    server, _ := godi.Resolve[*HTTPServer](provider)
    if err := server.Start(); err != nil {
        log.Fatal(err)
    }

    // Wait for shutdown
    waitForShutdown()
}
```

### Key Differences

| Feature               | Fx           | godi                 |
| --------------------- | ------------ | -------------------- |
| Application Lifecycle | Built-in     | Manual               |
| Module System         | fx.Module    | godi.Module          |
| Parameter Objects     | fx.In/fx.Out | godi.In/godi.Out     |
| Logging               | Structured   | Your choice          |
| Hooks                 | fx.Hook      | Disposable interface |

### Migration Steps

1. **Replace fx.App** with ServiceProvider
2. **Convert fx.Provide** to Add\* methods
3. **Handle lifecycle manually** or use Disposable
4. **Update parameter objects** from fx.In to godi.In
5. **Convert modules** to godi modules

## Migrating from Microsoft.Extensions.DependencyInjection

### .NET Approach

```csharp
var services = new ServiceCollection();

// Register services
services.AddSingleton<IConfiguration, Configuration>();
services.AddSingleton<ILogger, Logger>();
services.AddScoped<IUserRepository, UserRepository>();
services.AddTransient<IEmailService, EmailService>();

// Build provider
var provider = services.BuildServiceProvider();

// Resolve
var userService = provider.GetService<IUserService>();
```

### godi Approach (Very Similar!)

```go
collection := godi.NewServiceCollection()

// Register services
collection.AddSingleton(NewConfiguration)
collection.AddSingleton(NewLogger)
collection.AddScoped(NewUserRepository)
collection.AddTransient(NewEmailService)

// Build provider
provider, err := collection.BuildServiceProvider()

// Resolve
userService, err := godi.Resolve[UserService](provider)
```

### Familiar Concepts

- Same lifetime semantics (Singleton, Scoped, Transient)
- ServiceCollection and ServiceProvider
- Scopes for request handling
- Similar API design

### Migration Steps

1. **Port service registrations** - syntax is very similar
2. **Convert interfaces** to Go interfaces
3. **Update resolution** to use generics
4. **Handle errors** explicitly (Go style)

## Migrating from Spring/Java DI

### Spring Approach

```java
@Component
public class UserService {
    @Autowired
    private UserRepository repository;

    @Autowired
    private EmailService emailService;
}

@Configuration
public class AppConfig {
    @Bean
    @Scope("singleton")
    public Logger logger() {
        return new Logger();
    }
}
```

### godi Approach

```go
// No annotations - explicit registration
type UserService struct {
    repository   UserRepository
    emailService EmailService
}

func NewUserService(repo UserRepository, email EmailService) *UserService {
    return &UserService{
        repository:   repo,
        emailService: email,
    }
}

// Configuration
collection := godi.NewServiceCollection()
collection.AddSingleton(NewLogger)
collection.AddScoped(NewUserRepository)
collection.AddScoped(NewEmailService)
collection.AddScoped(NewUserService)
```

### Key Differences

- No annotations/reflection magic
- Explicit registration
- Compile-time safety
- No classpath scanning
- Manual configuration

## Common Migration Patterns

### 1. Converting Singletons

```go
// Before: Global variable
var (
    globalLogger *Logger
    loggerOnce   sync.Once
)

func GetLogger() *Logger {
    loggerOnce.Do(func() {
        globalLogger = NewLogger()
    })
    return globalLogger
}

// After: DI Singleton
collection.AddSingleton(NewLogger)
```

### 2. Converting Factory Functions

```go
// Before: Factory function
func CreateService(env string) Service {
    switch env {
    case "prod":
        return NewProdService()
    default:
        return NewDevService()
    }
}

// After: Conditional registration
if config.Environment == "prod" {
    collection.AddSingleton(NewProdService)
} else {
    collection.AddSingleton(NewDevService)
}
```

### 3. Converting Init Functions

```go
// Before: Init functions
func init() {
    setupLogger()
    connectDatabase()
    initializeCache()
}

// After: Constructors with dependencies
func NewApplication(logger Logger, db Database, cache Cache) *Application {
    return &Application{
        logger: logger,
        db:     db,
        cache:  cache,
    }
}
```

## Testing Migration

### Before: Complex Setup

```go
func TestUserService(t *testing.T) {
    // Manual test setup
    logger := &MockLogger{}
    db := &MockDatabase{}
    cache := &MockCache{}
    repo := repository.NewUserRepository(db, logger)
    emailSvc := &MockEmailService{}

    service := service.NewUserService(repo, emailSvc, cache, logger)
    // ... test
}
```

### After: Clean DI

```go
func TestUserService(t *testing.T) {
    collection := godi.NewServiceCollection()

    // Register mocks
    collection.AddSingleton(func() Logger { return &MockLogger{} })
    collection.AddSingleton(func() Database { return &MockDatabase{} })
    collection.AddSingleton(func() Cache { return &MockCache{} })
    collection.AddSingleton(func() EmailService { return &MockEmailService{} })

    // Register real services
    collection.AddScoped(repository.NewUserRepository)
    collection.AddScoped(service.NewUserService)

    provider, _ := collection.BuildServiceProvider()
    defer provider.Close()

    service, _ := godi.Resolve[*service.UserService](provider)
    // ... test
}
```

## Migration Checklist

- [ ] Identify all services and dependencies
- [ ] Create constructor functions
- [ ] Choose appropriate lifetimes
- [ ] Register services in modules
- [ ] Replace manual wiring with DI
- [ ] Update tests to use DI
- [ ] Remove global state
- [ ] Add disposal for resources
- [ ] Validate the container
- [ ] Performance test if needed

## Benefits After Migration

1. **Less Boilerplate** - No more manual wiring
2. **Better Testing** - Easy mock injection
3. **Clear Dependencies** - Explicit constructors
4. **Automatic Cleanup** - Disposal management
5. **Flexible Architecture** - Easy to refactor
6. **Type Safety** - Compile-time checks

## Getting Help

- Check examples in the repository
- Review the tutorials
- Ask on GitHub Discussions
- File issues for migration problems

Remember: Migration can be gradual - start with one module and expand!
