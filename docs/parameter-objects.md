# Parameter Objects (In)

Simplify complex constructors by using parameter objects with automatic dependency injection.

## Understanding Parameter Objects

Parameter objects let you group constructor dependencies into a struct, making constructors cleaner and more maintainable:

```go
// Without parameter objects - messy
func NewOrderService(
    db Database,
    cache Cache,
    logger Logger,
    emailer EmailService,
    payment PaymentGateway,
    inventory InventoryService,
    shipping ShippingService,
) OrderService { }

// With parameter objects - clean
type OrderServiceParams struct {
    godi.In

    DB        Database
    Cache     Cache
    Logger    Logger
    Emailer   EmailService
    Payment   PaymentGateway
    Inventory InventoryService
    Shipping  ShippingService
}

func NewOrderService(params OrderServiceParams) OrderService {
    return &orderService{
        db:        params.DB,
        cache:     params.Cache,
        logger:    params.Logger,
        emailer:   params.Emailer,
        payment:   params.Payment,
        inventory: params.Inventory,
        shipping:  params.Shipping,
    }
}
```

## Basic Usage

### Creating Parameter Objects

```go
// 1. Define struct with embedded godi.In
type ServiceParams struct {
    godi.In  // Must be embedded anonymously

    Database Database
    Logger   Logger
    Config   *Config
}

// 2. Use in constructor
func NewService(params ServiceParams) Service {
    return &service{
        db:     params.Database,
        logger: params.Logger,
        config: params.Config,
    }
}

// 3. Register normally - godi handles the rest
services.AddSingleton(NewService)
```

### Field Tags

Parameter objects support special field tags:

```go
type ServiceParams struct {
    godi.In

    // Required fields (default)
    Database Database
    Logger   Logger

    // Optional field
    Cache Cache `optional:"true"`

    // Named/keyed service
    PrimaryDB Database `name:"primary"`

    // Group of services
    Middlewares []Middleware `group:"middleware"`
}
```

## Optional Dependencies

### Handling Optional Fields

```go
type ServiceParams struct {
    godi.In

    // Required dependencies
    DB     Database
    Logger Logger

    // Optional dependencies
    Cache     Cache     `optional:"true"`
    Metrics   Metrics   `optional:"true"`
    Telemetry Telemetry `optional:"true"`
}

func NewService(params ServiceParams) Service {
    svc := &service{
        db:     params.DB,
        logger: params.Logger,
    }

    // Check if optional dependencies are available
    if params.Cache != nil {
        svc.cache = params.Cache
        svc.logger.Info("Cache enabled")
    }

    if params.Metrics != nil {
        svc.metrics = params.Metrics
        svc.logger.Info("Metrics enabled")
    }

    if params.Telemetry != nil {
        svc.telemetry = params.Telemetry
        svc.logger.Info("Telemetry enabled")
    }

    return svc
}
```

## Named Dependencies

### Using Keyed Services

```go
type RepositoryParams struct {
    godi.In

    // Named database connections
    PrimaryDB   Database `name:"primary"`
    ReplicaDB   Database `name:"replica"`
    AnalyticsDB Database `name:"analytics"`

    // Named caches
    RedisCache  Cache `name:"redis"`
    MemoryCache Cache `name:"memory"`
}

func NewRepository(params RepositoryParams) Repository {
    return &repository{
        writer:    params.PrimaryDB,
        reader:    params.ReplicaDB,
        analytics: params.AnalyticsDB,
        hotCache:  params.MemoryCache,
        coldCache: params.RedisCache,
    }
}
```

## Group Dependencies

### Injecting Service Groups

```go
type ApplicationParams struct {
    godi.In

    // Single services
    Logger Logger
    Config *Config

    // Groups of services
    Middlewares []Middleware   `group:"middleware"`
    Validators  []Validator    `group:"validators"`
    Handlers    []EventHandler `group:"handlers"`
}

func NewApplication(params ApplicationParams) Application {
    app := &application{
        logger:     params.Logger,
        config:     params.Config,
        validators: params.Validators,
    }

    // Apply all middleware
    for _, mw := range params.Middlewares {
        app.Use(mw)
    }

    // Register all handlers
    for _, handler := range params.Handlers {
        app.RegisterHandler(handler)
    }

    return app
}
```

## Complex Examples

### Mixed Dependencies

```go
type ComplexServiceParams struct {
    godi.In

    // Regular dependencies
    Logger Logger
    Config *Config

    // Named dependencies
    PrimaryDB Database `name:"primary"`
    ReplicaDB Database `name:"replica"`

    // Optional named dependency
    CacheDB Database `name:"cache" optional:"true"`

    // Group of services
    Validators []Validator `group:"validators"`

    // Optional group
    Plugins []Plugin `group:"plugins" optional:"true"`
}

func NewComplexService(params ComplexServiceParams) ComplexService {
    svc := &complexService{
        logger:     params.Logger,
        config:     params.Config,
        primary:    params.PrimaryDB,
        replica:    params.ReplicaDB,
        validators: params.Validators,
    }

    if params.CacheDB != nil {
        svc.cache = params.CacheDB
    }

    if len(params.Plugins) > 0 {
        svc.plugins = params.Plugins
    }

    return svc
}
```

### Nested Services

```go
type DatabaseParams struct {
    godi.In

    Config     *DatabaseConfig
    Logger     Logger
    Monitoring Monitoring `optional:"true"`
}

type CacheParams struct {
    godi.In

    Config  *CacheConfig
    Logger  Logger
    Metrics Metrics `optional:"true"`
}

type ServiceParams struct {
    godi.In

    // Can include other services that also use parameter objects
    DB    *Database // Created with DatabaseParams
    Cache *Cache    // Created with CacheParams
}

func NewDatabase(params DatabaseParams) Database {
    // ...
}

func NewCache(params CacheParams) Cache {
    // ...
}

func NewService(params ServiceParams) Service {
    // Both DB and Cache are properly injected
    return &service{
        db:    params.DB,
        cache: params.Cache,
    }
}
```

## Benefits

### 1. Cleaner Constructors

```go
// Before: 10+ parameters
func NewService(db Database, cache Cache, logger Logger,
    config Config, auth Auth, mailer Mailer, queue Queue,
    storage Storage, metrics Metrics, tracer Tracer) Service {
    // ...
}

// After: 1 parameter object
func NewService(params ServiceParams) Service {
    // ...
}
```

### 2. Easier Refactoring

```go
// Adding a new dependency only requires updating the struct
type ServiceParams struct {
    godi.In

    Database Database
    Logger   Logger
    Cache    Cache
    // Easy to add new field:
    Metrics  Metrics // New dependency
}

// Constructor signature doesn't change
func NewService(params ServiceParams) Service {
    // ...
}
```

### 3. Self-Documenting

```go
type EmailServiceParams struct {
    godi.In

    // Core dependencies
    SMTPClient   SMTPClient
    TemplateEngine TemplateEngine

    // Optional features
    RateLimiter RateLimiter `optional:"true"`
    Analytics   Analytics   `optional:"true"`

    // Configuration
    Config *EmailConfig
}

// Clear what the service needs
func NewEmailService(params EmailServiceParams) EmailService {
    // ...
}
```

## Testing with Parameter Objects

### Creating Test Parameters

```go
func TestService(t *testing.T) {
    // Manually create parameter object for testing
    params := ServiceParams{
        Database: &MockDatabase{},
        Logger:   &TestLogger{},
        Cache:    &MemoryCache{},
    }

    service := NewService(params)

    // Test service...
}
```

### With Test Provider

```go
func TestServiceIntegration(t *testing.T) {
    services := godi.NewCollection()

    // Register test dependencies
    services.AddSingleton(NewMockDatabase)
    services.AddSingleton(NewTestLogger)
    services.AddSingleton(NewMemoryCache)

    // Register service with parameter object
    services.AddScoped(NewService)

    provider, _ := services.Build()

    // godi handles parameter object injection
    service := godi.MustResolve[Service](provider)

    // Test service...
}
```

## Best Practices

1. **Always embed godi.In anonymously** - Not as a named field
2. **Use descriptive field names** - They document dependencies
3. **Group related fields** - Use comments to organize
4. **Consider optional fields** - Not everything needs to be required
5. **Keep parameter objects focused** - One per service

### Good Structure

```go
type ServiceParams struct {
    godi.In

    // Core dependencies
    Database Database
    Logger   Logger

    // External services
    EmailClient   EmailClient
    PaymentClient PaymentClient

    // Optional features
    Cache   Cache   `optional:"true"`
    Metrics Metrics `optional:"true"`

    // Configuration
    Config *ServiceConfig
}
```

## Common Pitfalls

### Named Embedding

```go
// ❌ Wrong - named field
type BadParams struct {
    In godi.In  // This won't work!
    Database Database
}

// ✅ Correct - anonymous embedding
type GoodParams struct {
    godi.In     // Anonymous embedding
    Database Database
}
```

### Unexported Fields

```go
// ❌ Unexported fields won't be injected
type BadParams struct {
    godi.In
    database Database  // lowercase - won't work!
}

// ✅ Exported fields are injected
type GoodParams struct {
    godi.In
    Database Database  // Uppercase - works!
}
```

### Pointer vs Value

```go
type ServiceParams struct {
    godi.In

    // Both work - choose based on your needs
    Config    Config     // Value
    Database  *Database  // Pointer
    Interface Logger     // Interface
}
```

## Next Steps

- Explore [Result Objects](result-objects.md)
- Learn about [Interface Registration](interface-registration.md)
