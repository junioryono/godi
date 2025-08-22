# Service Registration

Learn how to register services with godi's collection.

## Basic Registration

### Constructor Functions

The most common way to register services is with constructor functions:

```go
// Simple constructor
func NewLogger() Logger {
    return &logger{level: "INFO"}
}

// Constructor with dependencies
func NewUserService(db Database, logger Logger) UserService {
    return &userService{
        db:     db,
        logger: logger,
    }
}

// Register with collection
services := godi.NewCollection()
services.AddSingleton(NewLogger)
services.AddSingleton(NewDatabase)
services.AddScoped(NewUserService)
```

### Instance Registration

You can also register existing instances:

```go
// Create instance
config := &Config{
    DatabaseURL: "postgres://localhost:5432",
    APIKey:      "secret",
}

// Register instance
services.AddSingleton(config)

// Later resolve it
cfg := godi.MustResolve[*Config](provider)
```

## Constructor Patterns

### Interface Return Types

Best practice: return interfaces from constructors:

```go
// Define interface
type Logger interface {
    Log(message string)
    Error(err error)
}

// Implementation
type fileLogger struct {
    file *os.File
}

// Constructor returns interface
func NewLogger() Logger { // Returns interface, not *fileLogger
    return &fileLogger{
        file: openLogFile(),
    }
}

services.AddSingleton(NewLogger)
```

### Multiple Return Values

Constructors can return multiple values:

```go
// Constructor with error return
func NewDatabase(config *Config) (Database, error) {
    db, err := sql.Open("postgres", config.DatabaseURL)
    if err != nil {
        return nil, err
    }
    return &database{db: db}, nil
}

// godi handles the error
services.AddSingleton(NewDatabase)
```

### Multiple Services from One Constructor

Return multiple services from a single constructor:

```go
func NewServices(db Database) (UserService, OrderService) {
    userSvc := &userService{db: db}
    orderSvc := &orderService{db: db}
    return userSvc, orderSvc
}

// Both services are registered
services.AddSingleton(NewServices)

// Resolve each individually
userService := godi.MustResolve[UserService](provider)
orderService := godi.MustResolve[OrderService](provider)
```

## Registration Options

### Named Services (Keyed)

Register multiple implementations of the same interface:

```go
// Different cache implementations
func NewRedisCache(config *Config) Cache {
    return &redisCache{addr: config.RedisAddr}
}

func NewMemoryCache() Cache {
    return &memoryCache{data: make(map[string]any)}
}

// Register with names
services.AddSingleton(NewRedisCache, godi.Name("redis"))
services.AddSingleton(NewMemoryCache, godi.Name("memory"))

// Resolve by name
redisCache := godi.MustResolveKeyed[Cache](provider, "redis")
memCache := godi.MustResolveKeyed[Cache](provider, "memory")
```

### Interface Registration (As)

Register a concrete type as its interface:

```go
type Reader interface { Read([]byte) (int, error) }
type Writer interface { Write([]byte) (int, error) }

type Buffer struct {
    data []byte
}

func NewBuffer() *Buffer {
    return &Buffer{data: make([]byte, 0)}
}

// Register as multiple interfaces
services.AddSingleton(NewBuffer, godi.As[Reader](), godi.As[Writer]())

// Resolve as interfaces
reader := godi.MustResolve[Reader](provider)
writer := godi.MustResolve[Writer](provider)
// Both reader and writer point to the same Buffer instance
```

## Lifetime Methods

Each lifetime has its own registration method:

```go
// Singleton - one instance for entire application
services.AddSingleton(NewLogger)
services.AddSingleton(NewDatabase)
services.AddSingleton(NewCache)

// Scoped - one instance per scope
services.AddScoped(NewRequestContext)
services.AddScoped(NewUnitOfWork)
services.AddScoped(NewTransaction)

// Transient - new instance every time
services.AddTransient(NewTempFileHandler)
services.AddTransient(NewRandomGenerator)
services.AddTransient(NewBuilder)
```

## Validation

### Build-Time Validation

godi validates your registrations when building the provider:

```go
services := godi.NewCollection()
services.AddSingleton(NewServiceA)
services.AddScoped(NewServiceB)

provider, err := services.Build()
if err != nil {
    // Possible errors:
    // - Circular dependency
    // - Lifetime violation
    // - Missing dependency
    fmt.Printf("Build failed: %v\n", err)
}
```

### Common Registration Errors

```go
// ❌ Circular dependency
func NewServiceA(b ServiceB) ServiceA { return &serviceA{b: b} }
func NewServiceB(a ServiceA) ServiceB { return &serviceB{a: a} }
services.AddSingleton(NewServiceA)
services.AddSingleton(NewServiceB)
// Error: Circular dependency detected

// ❌ Lifetime violation
func NewSingleton(scoped ScopedService) SingletonService {
    return &singletonService{scoped: scoped}
}
services.AddSingleton(NewSingleton)
services.AddScoped(NewScopedService)
// Error: Singleton cannot depend on Scoped

// ❌ Missing dependency
func NewService(missing MissingDependency) Service {
    return &service{dep: missing}
}
services.AddSingleton(NewService)
// Error: No service registered for type MissingDependency
```

## Advanced Registration

### Conditional Registration

Register services based on configuration:

```go
config := LoadConfig()

services := godi.NewCollection()
services.AddSingleton(NewLogger)

// Register database based on config
if config.UsePostgres {
    services.AddSingleton(NewPostgresDB, godi.As[Database]())
} else {
    services.AddSingleton(NewSQLiteDB, godi.As[Database]())
}

// Register cache based on environment
if config.Environment == "production" {
    services.AddSingleton(NewRedisCache, godi.As[Cache]())
} else {
    services.AddSingleton(NewMemoryCache, godi.As[Cache]())
}
```

### Factory Pattern

Register factories that create services:

```go
// Factory function type
type ServiceFactory func(id string) Service

// Factory constructor
func NewServiceFactory(db Database) ServiceFactory {
    return func(id string) Service {
        return &service{
            id: id,
            db: db,
        }
    }
}

// Register factory
services.AddSingleton(NewServiceFactory)

// Use factory
factory := godi.MustResolve[ServiceFactory](provider)
service1 := factory("service-1")
service2 := factory("service-2")
```

### Generic Services

Register generic services with type parameters:

```go
// Generic repository
type Repository[T any] interface {
    Get(id string) (T, error)
    Save(entity T) error
}

type repository[T any] struct {
    db Database
}

func NewUserRepository(db Database) Repository[User] {
    return &repository[User]{db: db}
}

func NewOrderRepository(db Database) Repository[Order] {
    return &repository[Order]{db: db}
}

// Register typed repositories
services.AddScoped(NewUserRepository)
services.AddScoped(NewOrderRepository)

// Resolve with correct types
userRepo := godi.MustResolve[Repository[User]](provider)
orderRepo := godi.MustResolve[Repository[Order]](provider)
```

## Best Practices

1. **Return interfaces** from constructors, not concrete types
2. **Keep constructors simple** - just dependency injection and basic setup
3. **Validate early** - let godi catch issues at build time
4. **Use appropriate lifetimes** - don't make everything singleton
5. **Name similar services** - use `godi.Name()` for multiple implementations
6. **Document dependencies** - make constructor parameters clear

## Common Patterns

### Repository Pattern

```go
// Repository interface
type UserRepository interface {
    GetByID(id string) (*User, error)
    Save(user *User) error
}

// Implementation
type postgresUserRepo struct {
    db *sql.DB
}

func NewUserRepository(db Database) UserRepository {
    return &postgresUserRepo{db: db.Connection()}
}

services.AddScoped(NewUserRepository)
```

### Service Layer Pattern

```go
type UserService interface {
    CreateUser(email, password string) (*User, error)
    AuthenticateUser(email, password string) (*User, error)
}

type userService struct {
    repo   UserRepository
    hasher PasswordHasher
    logger Logger
}

func NewUserService(
    repo UserRepository,
    hasher PasswordHasher,
    logger Logger,
) UserService {
    return &userService{
        repo:   repo,
        hasher: hasher,
        logger: logger,
    }
}

services.AddScoped(NewUserService)
```

## Next Steps

- Learn about [Dependency Resolution](dependency-resolution.md)
- Explore [Scopes & Isolation](scopes-isolation.md)
- Understand [Resource Management](resource-management.md)
