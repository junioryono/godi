# Parameter Objects

Simplify complex constructors with automatic field injection.

## The Problem

Constructors with many dependencies get unwieldy:

```go
func NewOrderService(
    db Database,
    cache Cache,
    logger Logger,
    emailer EmailService,
    payment PaymentGateway,
    inventory InventoryService,
    shipping ShippingService,
) *OrderService {
    // ...
}
```

## The Solution: Parameter Objects

Group dependencies into a struct with `godi.In`:

```go
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

func NewOrderService(params OrderServiceParams) *OrderService {
    return &OrderService{
        db:        params.DB,
        cache:     params.Cache,
        logger:    params.Logger,
        // ...
    }
}
```

## Basic Usage

```go
// 1. Define struct with embedded godi.In
type ServiceParams struct {
    godi.In  // Must be embedded anonymously

    Database Database
    Logger   Logger
    Config   *Config
}

// 2. Use in constructor
func NewService(params ServiceParams) *Service {
    return &Service{
        db:     params.Database,
        logger: params.Logger,
        config: params.Config,
    }
}

// 3. Register normally
services.AddSingleton(NewService)
```

godi automatically creates the parameter object and fills in the fields.

## Field Tags

### Optional Dependencies

```go
type ServiceParams struct {
    godi.In

    // Required (default)
    Database Database
    Logger   Logger

    // Optional - nil if not registered
    Cache   Cache   `optional:"true"`
    Metrics Metrics `optional:"true"`
}

func NewService(params ServiceParams) *Service {
    svc := &Service{
        db:     params.Database,
        logger: params.Logger,
    }

    if params.Cache != nil {
        svc.cache = params.Cache
    }

    return svc
}
```

### Named Dependencies

```go
type ServiceParams struct {
    godi.In

    PrimaryDB   Database `name:"primary"`
    ReplicaDB   Database `name:"replica"`
    RedisCache  Cache    `name:"redis"`
}
```

### Group Dependencies

```go
type ServiceParams struct {
    godi.In

    Validators  []Validator  `group:"validators"`
    Middlewares []Middleware `group:"middleware"`
}
```

### Combining Tags

```go
type ServiceParams struct {
    godi.In

    // Required named dependency
    PrimaryDB Database `name:"primary"`

    // Optional named dependency
    CacheDB Database `name:"cache" optional:"true"`

    // Optional group
    Plugins []Plugin `group:"plugins" optional:"true"`
}
```

## Benefits

### 1. Cleaner Signatures

```go
// Before: 10 parameters
func NewService(a, b, c, d, e, f, g, h, i, j SomeType) *Service

// After: 1 parameter object
func NewService(params ServiceParams) *Service
```

### 2. Easier Refactoring

```go
// Adding a dependency: just add a field
type ServiceParams struct {
    godi.In

    Database Database
    Logger   Logger
    Cache    Cache
    Metrics  Metrics  // New - no signature change!
}
```

### 3. Self-Documenting

```go
type EmailServiceParams struct {
    godi.In

    // Core dependencies
    SMTPClient     SMTPClient
    TemplateEngine TemplateEngine

    // Optional features
    RateLimiter RateLimiter `optional:"true"`
    Analytics   Analytics   `optional:"true"`

    // Configuration
    Config *EmailConfig
}
```

## Testing

### Direct Construction

```go
func TestService(t *testing.T) {
    params := ServiceParams{
        Database: &MockDatabase{},
        Logger:   &TestLogger{},
        Cache:    &MemoryCache{},
    }

    service := NewService(params)
    // Test service...
}
```

### With Provider

```go
func TestServiceIntegration(t *testing.T) {
    services := godi.NewCollection()
    services.AddSingleton(NewMockDatabase)
    services.AddSingleton(NewTestLogger)
    services.AddScoped(NewService)

    provider, _ := services.Build()

    service := godi.MustResolve[*Service](provider)
    // Test...
}
```

## Common Mistakes

### Named Embedding

```go
// Wrong: named field
type BadParams struct {
    In godi.In  // Won't work!
    Database Database
}

// Correct: anonymous
type GoodParams struct {
    godi.In  // Anonymous embedding
    Database Database
}
```

### Unexported Fields

```go
// Wrong: unexported
type BadParams struct {
    godi.In
    database Database  // lowercase - not injected
}

// Correct: exported
type GoodParams struct {
    godi.In
    Database Database  // Uppercase - injected
}
```

---

**See also:** [Result Objects](result-objects.md) | [Keyed Services](keyed-services.md)
