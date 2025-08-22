# Result Objects (Out)

Register multiple services from a single constructor using result objects.

## Understanding Result Objects

Result objects allow a single constructor to register multiple services at once:

```go
type ServicesResult struct {
    godi.Out

    UserService  UserService
    OrderService OrderService
    EmailService EmailService
}

func NewServices(db Database, logger Logger) ServicesResult {
    return ServicesResult{
        UserService:  &userService{db: db, logger: logger},
        OrderService: &orderService{db: db, logger: logger},
        EmailService: &emailService{logger: logger},
    }
}

// All three services are registered
services.AddSingleton(NewServices)

// Resolve each individually
userSvc := godi.MustResolve[UserService](provider)
orderSvc := godi.MustResolve[OrderService](provider)
emailSvc := godi.MustResolve[EmailService](provider)
```

## Basic Usage

### Creating Result Objects

```go
// 1. Define struct with embedded godi.Out
type RepositoriesResult struct {
    godi.Out  // Must be embedded anonymously

    UserRepo    UserRepository
    OrderRepo   OrderRepository
    ProductRepo ProductRepository
}

// 2. Constructor returns the result struct
func NewRepositories(db Database) RepositoriesResult {
    return RepositoriesResult{
        UserRepo:    &userRepository{db: db},
        OrderRepo:   &orderRepository{db: db},
        ProductRepo: &productRepository{db: db},
    }
}

// 3. Register once - all services available
services.AddScoped(NewRepositories)
```

### Field Tags

Result objects support field tags for customization:

```go
type ServicesResult struct {
    godi.Out

    // Regular service
    Logger Logger

    // Named service
    PrimaryDB Database `name:"primary"`

    // Added to group
    Handler http.Handler `group:"handlers"`
}
```

## Common Use Cases

### Database Connections

```go
type DatabasesResult struct {
    godi.Out

    Primary   Database `name:"primary"`
    Replica   Database `name:"replica"`
    Analytics Database `name:"analytics"`
    Cache     Database `name:"cache"`
}

func NewDatabases(config *Config) DatabasesResult {
    return DatabasesResult{
        Primary:   connectDB(config.PrimaryURL),
        Replica:   connectDB(config.ReplicaURL),
        Analytics: connectDB(config.AnalyticsURL),
        Cache:     connectDB(config.CacheURL),
    }
}

// Single registration provides all databases
services.AddSingleton(NewDatabases)

// Use named resolution
primary := godi.MustResolveKeyed[Database](provider, "primary")
replica := godi.MustResolveKeyed[Database](provider, "replica")
```

### Application Bootstrap

```go
type CoreServicesResult struct {
    godi.Out

    Logger    Logger
    Config    *Config
    Metrics   MetricsCollector
    Telemetry TelemetryService
    Health    HealthChecker
}

func InitializeCoreServices() (CoreServicesResult, error) {
    // Load config first
    config, err := LoadConfig()
    if err != nil {
        return CoreServicesResult{}, err
    }

    // Setup logger
    logger := NewLogger(config.LogLevel)

    // Initialize other core services
    return CoreServicesResult{
        Logger:    logger,
        Config:    config,
        Metrics:   NewMetrics(logger),
        Telemetry: NewTelemetry(config, logger),
        Health:    NewHealthChecker(logger),
    }, nil
}

// Bootstrap application with one call
services.AddSingleton(InitializeCoreServices)
```

### Repository Factory

```go
type RepositoriesResult struct {
    godi.Out

    Users    UserRepository
    Orders   OrderRepository
    Products ProductRepository
    Reviews  ReviewRepository
}

func NewRepositories(db Database, cache Cache) RepositoriesResult {
    // Share common dependencies
    baseRepo := &BaseRepository{db: db, cache: cache}

    return RepositoriesResult{
        Users:    &userRepository{base: baseRepo},
        Orders:   &orderRepository{base: baseRepo},
        Products: &productRepository{base: baseRepo},
        Reviews:  &reviewRepository{base: baseRepo},
    }
}
```

## With Groups

### Multiple Handlers

```go
type HandlersResult struct {
    godi.Out

    UserHandler    http.Handler `group:"handlers"`
    OrderHandler   http.Handler `group:"handlers"`
    ProductHandler http.Handler `group:"handlers"`
    AdminHandler   http.Handler `group:"handlers"`
}

func NewHandlers(db Database, logger Logger) HandlersResult {
    return HandlersResult{
        UserHandler:    NewUserHandler(db, logger),
        OrderHandler:   NewOrderHandler(db, logger),
        ProductHandler: NewProductHandler(db, logger),
        AdminHandler:   NewAdminHandler(db, logger),
    }
}

// All handlers added to group
services.AddSingleton(NewHandlers)

// Resolve all handlers
handlers := godi.MustResolveGroup[http.Handler](provider, "handlers")
for _, handler := range handlers {
    router.Handle(handler)
}
```

### Middleware Collection

```go
type MiddlewareResult struct {
    godi.Out

    Auth        Middleware `group:"middleware"`
    RateLimit   Middleware `group:"middleware"`
    Logging     Middleware `group:"middleware"`
    Metrics     Middleware `group:"middleware"`
    Recovery    Middleware `group:"middleware"`
}

func NewMiddleware(config *Config, logger Logger) MiddlewareResult {
    return MiddlewareResult{
        Auth:      NewAuthMiddleware(config.AuthSettings),
        RateLimit: NewRateLimitMiddleware(config.RateLimit),
        Logging:   NewLoggingMiddleware(logger),
        Metrics:   NewMetricsMiddleware(),
        Recovery:  NewRecoveryMiddleware(logger),
    }
}
```

## Advanced Patterns

### Conditional Services

```go
type ServicesResult struct {
    godi.Out

    BaseService    BaseService
    PremiumService PremiumService `optional:"true"`
    AdminService   AdminService   `optional:"true"`
}

func NewServices(config *Config) ServicesResult {
    result := ServicesResult{
        BaseService: NewBaseService(),
    }

    // Conditionally include services
    if config.PremiumEnabled {
        result.PremiumService = NewPremiumService()
    }

    if config.AdminEnabled {
        result.AdminService = NewAdminService()
    }

    return result
}
```

### Mixed Registration

```go
type MixedResult struct {
    godi.Out

    // Different registration strategies
    Logger       Logger               // Regular service
    PrimaryDB    Database `name:"primary"`   // Named service
    ReplicaDB    Database `name:"replica"`   // Named service
    AuthHandler  Handler  `group:"handlers"` // Group member
    AdminHandler Handler  `group:"handlers"` // Group member
}

func NewMixedServices(config *Config) MixedResult {
    return MixedResult{
        Logger:       NewLogger(config.LogLevel),
        PrimaryDB:    NewDatabase(config.PrimaryURL),
        ReplicaDB:    NewDatabase(config.ReplicaURL),
        AuthHandler:  NewAuthHandler(),
        AdminHandler: NewAdminHandler(),
    }
}
```

### Service Initialization

```go
type InitializedServicesResult struct {
    godi.Out

    Cache    Cache
    Database Database
    Queue    Queue
}

func InitializeServices(config *Config) (InitializedServicesResult, error) {
    // Initialize cache
    cache, err := NewCache(config.CacheURL)
    if err != nil {
        return InitializedServicesResult{}, fmt.Errorf("cache init: %w", err)
    }

    // Initialize database
    db, err := NewDatabase(config.DatabaseURL)
    if err != nil {
        cache.Close()
        return InitializedServicesResult{}, fmt.Errorf("database init: %w", err)
    }

    // Initialize queue
    queue, err := NewQueue(config.QueueURL)
    if err != nil {
        cache.Close()
        db.Close()
        return InitializedServicesResult{}, fmt.Errorf("queue init: %w", err)
    }

    return InitializedServicesResult{
        Cache:    cache,
        Database: db,
        Queue:    queue,
    }, nil
}
```

## Combining with Parameter Objects

### Full Example

```go
// Parameter object for dependencies
type FactoryParams struct {
    godi.In

    Database Database
    Cache    Cache
    Logger   Logger
    Config   *Config
}

// Result object for services
type ServicesResult struct {
    godi.Out

    UserService    UserService
    OrderService   OrderService
    ProductService ProductService
    EmailService   EmailService
}

// Constructor uses params and returns result
func NewServices(params FactoryParams) ServicesResult {
    return ServicesResult{
        UserService: &userService{
            db:     params.Database,
            cache:  params.Cache,
            logger: params.Logger,
        },
        OrderService: &orderService{
            db:     params.Database,
            logger: params.Logger,
            config: params.Config,
        },
        ProductService: &productService{
            db:    params.Database,
            cache: params.Cache,
        },
        EmailService: &emailService{
            logger: params.Logger,
            config: params.Config,
        },
    }
}

// Single registration
services.AddSingleton(NewServices)
```

## Testing with Result Objects

### Mock Services

```go
type MockServicesResult struct {
    godi.Out

    UserService  UserService
    OrderService OrderService
    EmailService EmailService
}

func NewMockServices() MockServicesResult {
    return MockServicesResult{
        UserService:  &MockUserService{},
        OrderService: &MockOrderService{},
        EmailService: &MockEmailService{},
    }
}

func TestWithMocks(t *testing.T) {
    services := godi.NewCollection()
    services.AddSingleton(NewMockServices)

    provider, _ := services.Build()

    // All mocks available
    userSvc := godi.MustResolve[UserService](provider)
    assert.IsType(t, &MockUserService{}, userSvc)
}
```

### Partial Mocking

```go
func NewTestServices(mockEmail bool) ServicesResult {
    result := ServicesResult{
        UserService:  NewRealUserService(),
        OrderService: NewRealOrderService(),
    }

    if mockEmail {
        result.EmailService = &MockEmailService{}
    } else {
        result.EmailService = NewRealEmailService()
    }

    return result
}
```

## Best Practices

1. **Group related services** - Result objects should contain related services
2. **Share dependencies** - Use result objects when services share dependencies
3. **Keep it reasonable** - Don't put too many services in one result
4. **Document the grouping** - Explain why services are grouped together
5. **Consider initialization order** - Services in result are created together

### Good Grouping

```go
// ✅ Related services that share dependencies
type RepositoriesResult struct {
    godi.Out

    UserRepo    UserRepository
    OrderRepo   OrderRepository
    ProductRepo ProductRepository
}

// ✅ Services that initialize together
type InfrastructureResult struct {
    godi.Out

    Database Database
    Cache    Cache
    Queue    Queue
}

// ❌ Unrelated services
type BadResult struct {
    godi.Out

    Logger       Logger
    EmailService EmailService
    Calculator   Calculator  // Unrelated!
}
```

## Common Pitfalls

### Named Embedding

```go
// ❌ Wrong - named field
type BadResult struct {
    Out godi.Out  // Won't work!
    Service Service
}

// ✅ Correct - anonymous embedding
type GoodResult struct {
    godi.Out     // Anonymous
    Service Service
}
```

### Nil Fields

```go
// ⚠️ Nil fields won't be registered
type ServicesResult struct {
    godi.Out

    Service1 Service1
    Service2 Service2
}

func NewServices(includeService2 bool) ServicesResult {
    result := ServicesResult{
        Service1: NewService1(),
    }

    if includeService2 {
        result.Service2 = NewService2()
    }
    // If includeService2 is false, Service2 won't be registered

    return result
}
```

### Circular Dependencies

```go
// ❌ Can still create circular dependencies
type ServicesResult struct {
    godi.Out

    ServiceA ServiceA
    ServiceB ServiceB
}

func NewServices() ServicesResult {
    a := &serviceA{}
    b := &serviceB{}

    // Circular reference
    a.B = b
    b.A = a

    return ServicesResult{
        ServiceA: a,
        ServiceB: b,
    }
}
```

## Next Steps

- Learn about [Interface Registration](interface-registration.md)
- Review the [API Reference](https://pkg.go.dev/github.com/junioryono/godi/v4)
