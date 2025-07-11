# Parameter Objects

Parameter objects (using `godi.In` and `godi.Out`) provide a structured way to handle multiple dependencies and return values in your constructors.

## Understanding Parameter Objects

Parameter objects solve several problems:

- Constructor functions with many parameters
- Optional dependencies
- Grouped dependencies
- Named/keyed dependencies
- Multiple return values from a single constructor

## Input Parameter Objects (In)

### Basic Usage

Instead of multiple parameters:

```go
// Without parameter objects - gets unwieldy
func NewOrderService(
    db *sql.DB,
    logger Logger,
    cache Cache,
    emailer EmailService,
    payment PaymentGateway,
    inventory InventoryService,
) *OrderService {
    return &OrderService{
        db:        db,
        logger:    logger,
        cache:     cache,
        emailer:   emailer,
        payment:   payment,
        inventory: inventory,
    }
}
```

Use a parameter object:

```go
// With parameter object - cleaner and extensible
type OrderServiceParams struct {
    godi.In

    DB        *sql.DB
    Logger    Logger
    Cache     Cache
    Emailer   EmailService
    Payment   PaymentGateway
    Inventory InventoryService
}

func NewOrderService(params OrderServiceParams) *OrderService {
    return &OrderService{
        db:        params.DB,
        logger:    params.Logger,
        cache:     params.Cache,
        emailer:   params.Emailer,
        payment:   params.Payment,
        inventory: params.Inventory,
    }
}
```

### Optional Dependencies

Mark dependencies as optional:

```go
type ServiceParams struct {
    godi.In

    DB     *sql.DB
    Logger Logger          `optional:"true"`
    Cache  Cache           `optional:"true"`
    Tracer Tracer          `optional:"true"`
}

func NewService(params ServiceParams) *Service {
    svc := &Service{
        db: params.DB,
    }

    // Use default logger if not provided
    if params.Logger != nil {
        svc.logger = params.Logger
    } else {
        svc.logger = NewDefaultLogger()
    }

    // Cache is truly optional
    svc.cache = params.Cache // might be nil

    return svc
}
```

### Named Dependencies

Use named services with parameter objects:

```go
type DatabaseParams struct {
    godi.In

    Primary   Database `name:"primary"`
    Replica   Database `name:"replica"`
    Analytics Database `name:"analytics" optional:"true"`
}

func NewRepository(params DatabaseParams) *Repository {
    repo := &Repository{
        primary: params.Primary,
        replica: params.Replica,
    }

    // Analytics DB is optional
    if params.Analytics != nil {
        repo.analytics = params.Analytics
    }

    return repo
}
```

### Groups in Parameters

Collect multiple services of the same type:

```go
type ApplicationParams struct {
    godi.In

    Config     *Config
    Logger     Logger
    Handlers   []http.Handler   `group:"routes"`
    Middleware []Middleware     `group:"middleware"`
    Validators []Validator      `group:"validators"`
}

func NewApplication(params ApplicationParams) *Application {
    app := &Application{
        config: params.Config,
        logger: params.Logger,
    }

    // Register all handlers
    for _, handler := range params.Handlers {
        app.RegisterHandler(handler)
    }

    // Apply all middleware
    for _, mw := range params.Middleware {
        app.Use(mw)
    }

    // Register validators
    for _, v := range params.Validators {
        app.AddValidator(v)
    }

    return app
}
```

## Output Result Objects (Out)

### Basic Usage

Return multiple services from one constructor:

```go
type RepositoryResults struct {
    godi.Out

    UserRepo    UserRepository
    OrderRepo   OrderRepository
    ProductRepo ProductRepository
}

func NewRepositories(db *sql.DB, logger Logger) RepositoryResults {
    return RepositoryResults{
        UserRepo:    NewUserRepository(db, logger),
        OrderRepo:   NewOrderRepository(db, logger),
        ProductRepo: NewProductRepository(db, logger),
    }
}

// Register once, get three services
collection.AddSingleton(NewRepositories)
```

### Named Results

Provide multiple implementations:

```go
type CacheResults struct {
    godi.Out

    Redis   Cache `name:"redis"`
    Memory  Cache `name:"memory"`
    Default Cache // Unnamed, available as regular Cache
}

func NewCaches(config *Config) CacheResults {
    return CacheResults{
        Redis:   NewRedisCache(config.RedisURL),
        Memory:  NewMemoryCache(),
        Default: NewRedisCache(config.RedisURL), // Same as Redis
    }
}
```

### Group Results

Add multiple services to a group:

```go
type HandlerResults struct {
    godi.Out

    UserHandler    http.Handler `group:"routes"`
    OrderHandler   http.Handler `group:"routes"`
    ProductHandler http.Handler `group:"routes"`
    HealthHandler  http.Handler `group:"routes"`
}

func NewHandlers(services *Services) HandlerResults {
    return HandlerResults{
        UserHandler:    NewUserHandler(services.UserService),
        OrderHandler:   NewOrderHandler(services.OrderService),
        ProductHandler: NewProductHandler(services.ProductService),
        HealthHandler:  NewHealthHandler(),
    }
}
```

## Advanced Patterns

### Nested Parameter Objects

```go
type DatabaseConfig struct {
    ConnectionString string
    MaxConnections   int
}

type CacheConfig struct {
    RedisURL string
    TTL      time.Duration
}

type ServiceConfig struct {
    godi.In

    Database DatabaseConfig
    Cache    CacheConfig
    Logger   Logger
}

func NewService(config ServiceConfig) *Service {
    // Use nested configuration
    db := connectDB(config.Database.ConnectionString)
    cache := connectCache(config.Cache.RedisURL)

    return &Service{
        db:     db,
        cache:  cache,
        logger: config.Logger,
    }
}
```

### Combining In and Out

```go
// Input parameters
type ServiceParams struct {
    godi.In

    DB     *sql.DB
    Logger Logger
    Config *Config
}

// Output results
type ServiceResults struct {
    godi.Out

    UserService    *UserService
    OrderService   *OrderService    `name:"orders"`
    AdminService   *AdminService    `name:"admin"`
    PublicHandler  http.Handler     `group:"public-routes"`
    AdminHandler   http.Handler     `group:"admin-routes"`
}

// Constructor using both
func NewServices(params ServiceParams) ServiceResults {
    userSvc := newUserService(params.DB, params.Logger)
    orderSvc := newOrderService(params.DB, params.Logger)
    adminSvc := newAdminService(params.DB, params.Logger, params.Config)

    return ServiceResults{
        UserService:   userSvc,
        OrderService:  orderSvc,
        AdminService:  adminSvc,
        PublicHandler: newPublicAPI(userSvc, orderSvc),
        AdminHandler:  newAdminAPI(adminSvc),
    }
}
```

### Factory Pattern with Parameters

```go
type FactoryParams struct {
    godi.In

    Config    *Config
    Logger    Logger
    Providers map[string]Provider `group:"providers"`
}

type FactoryResult struct {
    godi.Out

    Factory        ServiceFactory
    DefaultService Service
}

func NewServiceFactory(params FactoryParams) FactoryResult {
    factory := &serviceFactory{
        config:    params.Config,
        logger:    params.Logger,
        providers: params.Providers,
    }

    // Create default service
    defaultService := factory.CreateService("default")

    return FactoryResult{
        Factory:        factory,
        DefaultService: defaultService,
    }
}
```

## Best Practices

### 1. When to Use Parameter Objects

Use parameter objects when:

- Constructor has more than 3-4 parameters
- You need optional dependencies
- You need named or grouped dependencies
- Parameters are likely to grow over time

```go
// ✅ Good candidate for parameter object
func NewService(db *sql.DB, cache Cache, logger Logger,
    emailer EmailService, config *Config, metrics Metrics) *Service

// ✅ Simplified with parameter object
func NewService(params ServiceParams) *Service
```

### 2. Naming Conventions

```go
// Input parameter types: add "Params" suffix
type UserServiceParams struct {
    godi.In
    // ...
}

// Output result types: add "Result" or "Results" suffix
type RepositoryResults struct {
    godi.Out
    // ...
}
```

### 3. Field Documentation

```go
type ServiceParams struct {
    godi.In

    // DB is the primary database connection (required)
    DB *sql.DB

    // Logger is used for structured logging (optional)
    // If not provided, a default console logger will be used
    Logger Logger `optional:"true"`

    // Cache is used for performance optimization (optional)
    // If not provided, caching will be disabled
    Cache Cache `optional:"true"`

    // Handlers are HTTP handlers to be registered (group)
    // All handlers in the "routes" group will be included
    Handlers []http.Handler `group:"routes"`
}
```

### 4. Validation

```go
type ServiceParams struct {
    godi.In

    Config *Config
    DB     *sql.DB
}

func NewService(params ServiceParams) (*Service, error) {
    // Validate required fields
    if params.Config == nil {
        return nil, errors.New("config is required")
    }

    if params.Config.APIKey == "" {
        return nil, errors.New("API key is required")
    }

    if params.DB == nil {
        return nil, errors.New("database is required")
    }

    return &Service{
        config: params.Config,
        db:     params.DB,
    }, nil
}
```

### 5. Avoid Overuse

```go
// ❌ Overkill for simple cases
type LoggerParams struct {
    godi.In

    Config *Config
}

func NewLogger(params LoggerParams) Logger {
    return &logger{level: params.Config.LogLevel}
}

// ✅ Simple is better
func NewLogger(config *Config) Logger {
    return &logger{level: config.LogLevel}
}
```

## Testing with Parameter Objects

### Mock Specific Fields

```go
func TestServiceWithMocks(t *testing.T) {
    collection := godi.NewServiceCollection()

    // Register mocks
    collection.AddSingleton(func() *sql.DB { return mockDB })
    collection.AddSingleton(func() Logger { return mockLogger })
    collection.AddSingleton(func() Cache { return nil }) // Optional

    // Register service that uses parameter object
    collection.AddScoped(NewService)

    provider, _ := collection.BuildServiceProvider()
    service, _ := godi.Resolve[*Service](provider)

    // Test with mocked dependencies
}
```

### Test Helpers

```go
func createTestParams() ServiceParams {
    return ServiceParams{
        DB:     createTestDB(),
        Logger: NewTestLogger(),
        Cache:  NewTestCache(),
    }
}

func TestServiceBehavior(t *testing.T) {
    params := createTestParams()

    // Override specific fields for test
    params.Cache = nil // Test without cache

    service := NewService(params)
    // ... test service behavior
}
```

## Common Patterns

### Configuration Parameters

```go
type AppParams struct {
    godi.In

    // Configuration
    Config     *Config
    Secrets    *Secrets         `optional:"true"`
    FeatureFlags *FeatureFlags  `optional:"true"`

    // Infrastructure
    DB         *sql.DB
    Cache      Cache            `optional:"true"`
    Queue      Queue            `optional:"true"`

    // Services
    Logger     Logger
    Metrics    Metrics          `optional:"true"`
    Tracer     Tracer           `optional:"true"`
}
```

### Multi-Database Parameters

```go
type DatabaseParams struct {
    godi.In

    // Different databases for different purposes
    UserDB      Database `name:"users"`
    OrderDB     Database `name:"orders"`
    AnalyticsDB Database `name:"analytics" optional:"true"`

    // Connection pooling
    MaxConns int `optional:"true"`
}
```

### Plugin Parameters

```go
type PluginParams struct {
    godi.In

    // Core services available to plugins
    Logger   Logger
    Config   *Config
    EventBus EventBus

    // All registered plugins
    Plugins []Plugin `group:"plugins"`
}
```

## Summary

Parameter objects provide:

- **Clean constructors** with many dependencies
- **Optional dependencies** with `optional:"true"`
- **Named dependencies** for multiple implementations
- **Groups** for collections of services
- **Multiple returns** with result objects

Use them to keep your dependency injection code maintainable and extensible as your application grows.
