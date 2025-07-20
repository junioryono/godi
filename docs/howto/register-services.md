# Registering Services

Service registration is how you tell godi about your services, their constructors, and their lifetimes. This guide covers all the ways to register services.

## Basic Registration

### AddSingleton

Registers a service that will have a single instance throughout the application lifetime.

```go
// Register by constructor function
services.AddSingleton(NewLogger)

// Register with explicit type
services.AddSingleton(func() Logger {
    return &FileLogger{path: "/var/log/app.log"}
})

// Register instance directly
logger := &ConsoleLogger{}
services.AddSingleton(func() Logger { return logger })
```

### AddScoped

Registers a service that will have one instance per scope.

```go
// Typical for repository pattern
services.AddScoped(NewUserRepository)

// With dependencies
services.AddScoped(func(db *Database, logger Logger) UserRepository {
    return &SqlUserRepository{db: db, logger: logger}
})
```

## Constructor Patterns

### Simple Constructor

The most common pattern - a function that returns a service:

```go
func NewUserService(repo UserRepository, logger Logger) *UserService {
    return &UserService{
        repo:   repo,
        logger: logger,
    }
}

services.AddScoped(NewUserService)
```

### Constructor with Error

Constructors can return an error as the second value:

```go
func NewDatabase(config *Config) (*Database, error) {
    db, err := sql.Open("postgres", config.DatabaseURL)
    if err != nil {
        return nil, fmt.Errorf("failed to connect: %w", err)
    }

    if err := db.Ping(); err != nil {
        return nil, fmt.Errorf("failed to ping: %w", err)
    }

    return &Database{db: db}, nil
}

services.AddSingleton(NewDatabase)
```

### Interface Registration

Always register services by their interface when possible:

```go
// Good: Register by interface
func NewFileLogger(config *Config) Logger {  // Returns interface
    return &FileLogger{path: config.LogPath}
}

// Avoid: Register by concrete type
func NewFileLogger(config *Config) *FileLogger {  // Returns concrete type
    return &FileLogger{path: config.LogPath}
}
```

## Keyed Services

Register multiple implementations of the same interface using keys:

```go
// Register different cache implementations
services.AddSingleton(
    func() Cache { return &RedisCache{} },
    godi.Name("redis"),
)

services.AddSingleton(
    func() Cache { return &MemoryCache{} },
    godi.Name("memory"),
)

// Register named databases
services.AddSingleton(
    func(config *Config) Database {
        return NewPostgresDB(config.PrimaryDB)
    },
    godi.Name("primary"),
)

services.AddSingleton(
    func(config *Config) Database {
        return NewPostgresDB(config.ReadReplicaDB)
    },
    godi.Name("replica"),
)
```

## Service Groups

Register multiple services that will be collected into a slice:

```go
// Register HTTP handlers
services.AddSingleton(NewUserHandler, godi.Group("handlers"))
services.AddSingleton(NewOrderHandler, godi.Group("handlers"))
services.AddSingleton(NewProductHandler, godi.Group("handlers"))

// Consume the group
type Router struct {
    godi.In
    Handlers []http.Handler `group:"handlers"`
}

func NewRouter(params Router) *mux.Router {
    router := mux.NewRouter()
    for _, handler := range params.Handlers {
        handler.RegisterRoutes(router)
    }
    return router
}
```

## Instance Registration

Sometimes you need to register an existing instance:

```go
// Configuration loaded from file
config := loadConfigFromFile("config.yaml")
services.AddSingleton(func() *Config { return config })

// Third-party client
redisClient := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
})
services.AddSingleton(func() *redis.Client { return redisClient })
```

## Conditional Registration

Register services based on configuration or environment:

```go
// Based on environment
if os.Getenv("ENV") == "production" {
    services.AddSingleton(NewS3Storage)
} else {
    services.AddSingleton(NewLocalStorage)
}

// Based on configuration
if config.EnableCache {
    services.AddSingleton(NewRedisCache)
} else {
    services.AddSingleton(NewNoOpCache)
}

// Feature flags
if config.Features.EmailNotifications {
    services.AddScoped(NewEmailService)
    services.AddScoped(NewNotificationService)
}
```

## Generic Services

Register generic services with type constraints:

```go
// Generic repository
type Repository[T any] interface {
    GetByID(id string) (*T, error)
    Save(entity *T) error
}

// Concrete implementation
type MongoRepository[T any] struct {
    collection *mongo.Collection
}

// Register for specific types
services.AddScoped(func(db *mongo.Database) Repository[User] {
    return &MongoRepository[User]{
        collection: db.Collection("users"),
    }
})

services.AddScoped(func(db *mongo.Database) Repository[Order] {
    return &MongoRepository[Order]{
        collection: db.Collection("orders"),
    }
})
```

## Registration Options

### Service Replacement

Replace an existing service registration:

```go
// Initial registration
services.AddSingleton(NewFileLogger)

// Replace with different implementation
services.Replace(godi.Singleton, NewConsoleLogger)
```

### Remove Services

Remove service registrations:

```go
// Remove all registrations of a type
services.RemoveAll(reflect.TypeOf((*Logger)(nil)).Elem())

// Clear all registrations
services.Clear()
```

## Factory Pattern

Use factories for complex construction logic:

```go
// Factory interface
type ServiceFactory interface {
    CreateService(name string) (Service, error)
}

// Factory implementation
type DefaultServiceFactory struct {
    logger Logger
    config *Config
}

func NewServiceFactory(logger Logger, config *Config) ServiceFactory {
    return &DefaultServiceFactory{
        logger: logger,
        config: config,
    }
}

func (f *DefaultServiceFactory) CreateService(name string) (Service, error) {
    switch name {
    case "email":
        return NewEmailService(f.logger, f.config.SMTP), nil
    case "sms":
        return NewSMSService(f.logger, f.config.Twilio), nil
    default:
        return nil, fmt.Errorf("unknown service: %s", name)
    }
}

// Register the factory
services.AddSingleton(NewServiceFactory)
```

## Best Practices

### 1. Register by Interface

```go
// Good
func NewService() ServiceInterface { }

// Avoid
func NewService() *ServiceImpl { }
```

### 2. Use Appropriate Lifetimes

```go
// Singletons: Stateless, shared resources
services.AddSingleton(NewLogger)
services.AddSingleton(NewConfiguration)

// Scoped: Request-specific, stateful
services.AddScoped(NewRepository)
services.AddScoped(NewUnitOfWork)
```

### 3. Validate Early

```go
// Validate during registration
services.AddSingleton(func(config *Config) (Database, error) {
    if config.DatabaseURL == "" {
        return nil, errors.New("database URL required")
    }
    return NewDatabase(config)
})
```

### 4. Document Dependencies

```go
// NewUserService creates a user service
// Dependencies:
// - UserRepository: for data access
// - Logger: for logging operations
// - EmailService: for sending notifications
func NewUserService(
    repo UserRepository,
    logger Logger,
    email EmailService,
) *UserService {
    return &UserService{
        repo:   repo,
        logger: logger,
        email:  email,
    }
}
```

## Common Patterns

### Options Pattern

```go
type ServerOptions struct {
    Port     int
    TLS      bool
    CertFile string
    KeyFile  string
}

func NewServer(logger Logger, opts ServerOptions) *Server {
    return &Server{
        logger:  logger,
        options: opts,
    }
}

// Register with options
services.AddSingleton(func(logger Logger) *Server {
    return NewServer(logger, ServerOptions{
        Port: 8080,
        TLS:  false,
    })
})
```

### Multi-Stage Initialization

```go
// Stage 1: Create instance
func NewDatabaseConnection(config *Config) *DatabaseConnection {
    return &DatabaseConnection{
        url: config.DatabaseURL,
    }
}

// Stage 2: Initialize
func (db *DatabaseConnection) Initialize() error {
    return db.connect()
}

// Register with initialization
services.AddSingleton(func(config *Config) (*DatabaseConnection, error) {
    db := NewDatabaseConnection(config)
    if err := db.Initialize(); err != nil {
        return nil, err
    }
    return db, nil
})
```

## Next Steps

- Learn about [Using Scopes](use-scopes.md)
- Explore [Keyed Services](keyed-services.md) in detail
- Understand [Service Groups](service-groups.md)
- Master [Modules](modules.md) for organization
