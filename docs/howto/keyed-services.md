# Keyed Services

Keyed services allow you to register multiple implementations of the same interface, distinguishing them with unique keys. This is essential when you need different implementations for different scenarios.

## When to Use Keyed Services

Use keyed services when you have:

- Multiple database connections (primary/replica)
- Different cache implementations (Redis/Memcached)
- Environment-specific services (dev/staging/prod)
- Feature flags requiring different implementations
- Multiple payment gateways or notification channels

## Basic Registration

Register services with the `Name` option:

```go
// Register multiple cache implementations
services.AddSingleton(
    func() Cache { return NewRedisCache("redis://primary:6379") },
    godi.Name("primary"),
)

services.AddSingleton(
    func() Cache { return NewRedisCache("redis://cache:6379") },
    godi.Name("cache"),
)

services.AddSingleton(
    func() Cache { return NewMemoryCache() },
    godi.Name("memory"),
)
```

## Resolving Keyed Services

Use `ResolveKeyed` to get a specific implementation:

```go
// Resolve specific cache
primaryCache, err := godi.ResolveKeyed[Cache](provider, "primary")
if err != nil {
    log.Fatal("Failed to resolve primary cache:", err)
}

// Use in a service
func (s *DataService) GetUser(ctx context.Context, id string) (*User, error) {
    // Try primary cache first
    cache, _ := godi.ResolveKeyed[Cache](s.provider, "primary")
    if user, found := cache.Get(id); found {
        return user.(*User), nil
    }

    // Fallback to database
    // ...
}
```

## Database Example

Common pattern for read replicas:

```go
// Database connections
services.AddSingleton(
    func(config *Config) (Database, error) {
        return NewPostgresDB(config.PrimaryDB)
    },
    godi.Name("primary"),
)

services.AddSingleton(
    func(config *Config) (Database, error) {
        return NewPostgresDB(config.ReadReplicaDB)
    },
    godi.Name("replica"),
)

// Repository using different databases
type UserRepository struct {
    primaryDB Database
    replicaDB Database
}

func NewUserRepository(provider godi.ServiceProvider) (*UserRepository, error) {
    primary, err := godi.ResolveKeyed[Database](provider, "primary")
    if err != nil {
        return nil, err
    }

    replica, err := godi.ResolveKeyed[Database](provider, "replica")
    if err != nil {
        return nil, err
    }

    return &UserRepository{
        primaryDB: primary,
        replicaDB: replica,
    }, nil
}

func (r *UserRepository) Create(user *User) error {
    // Writes go to primary
    return r.primaryDB.Insert(user)
}

func (r *UserRepository) GetByID(id string) (*User, error) {
    // Reads go to replica
    return r.replicaDB.FindOne(id)
}
```

## Notification Channels

Multiple notification services:

```go
// Email service implementations
services.AddSingleton(
    func(config *Config) EmailService {
        return NewSMTPService(config.SMTP)
    },
    godi.Name("smtp"),
)

services.AddSingleton(
    func(config *Config) EmailService {
        return NewSendGridService(config.SendGridAPIKey)
    },
    godi.Name("sendgrid"),
)

services.AddSingleton(
    func() EmailService {
        return NewMockEmailService()
    },
    godi.Name("mock"),
)

// Notification service that chooses based on config
type NotificationService struct {
    provider godi.ServiceProvider
    config   *Config
}

func (s *NotificationService) SendEmail(to, subject, body string) error {
    // Choose service based on configuration
    serviceName := s.config.EmailProvider // "smtp", "sendgrid", or "mock"

    emailService, err := godi.ResolveKeyed[EmailService](s.provider, serviceName)
    if err != nil {
        return fmt.Errorf("email provider %s not found: %w", serviceName, err)
    }

    return emailService.Send(to, subject, body)
}
```

## Payment Gateways

Handle multiple payment providers:

```go
// Payment gateway implementations
services.AddSingleton(
    func(config *Config) PaymentGateway {
        return NewStripeGateway(config.StripeKey)
    },
    godi.Name("stripe"),
)

services.AddSingleton(
    func(config *Config) PaymentGateway {
        return NewPayPalGateway(config.PayPalClientID, config.PayPalSecret)
    },
    godi.Name("paypal"),
)

services.AddSingleton(
    func(config *Config) PaymentGateway {
        return NewSquareGateway(config.SquareAccessToken)
    },
    godi.Name("square"),
)

// Payment processor that routes to correct gateway
type PaymentProcessor struct {
    provider godi.ServiceProvider
}

func (p *PaymentProcessor) ProcessPayment(
    amount float64,
    currency string,
    gateway string,
) (*PaymentResult, error) {
    paymentGateway, err := godi.ResolveKeyed[PaymentGateway](p.provider, gateway)
    if err != nil {
        return nil, fmt.Errorf("payment gateway %s not available", gateway)
    }

    return paymentGateway.Charge(amount, currency)
}
```

## Environment-Based Services

Different implementations per environment:

```go
func ConfigureServices(services godi.ServiceCollection, env string) {
    switch env {
    case "production":
        services.AddSingleton(
            func() Logger { return NewCloudLogger() },
            godi.Name("logger"),
        )
        services.AddSingleton(
            func() Metrics { return NewDatadogMetrics() },
            godi.Name("metrics"),
        )

    case "development":
        services.AddSingleton(
            func() Logger { return NewConsoleLogger() },
            godi.Name("logger"),
        )
        services.AddSingleton(
            func() Metrics { return NewNoOpMetrics() },
            godi.Name("metrics"),
        )
    }
}

// Usage remains the same across environments
logger, _ := godi.ResolveKeyed[Logger](provider, "logger")
metrics, _ := godi.ResolveKeyed[Metrics](provider, "metrics")
```

## Feature Flags

Toggle features with keyed services:

```go
// Feature implementations
services.AddSingleton(
    func() SearchEngine { return NewElasticsearchEngine() },
    godi.Name("search-v2"),
)

services.AddSingleton(
    func() SearchEngine { return NewSQLSearchEngine() },
    godi.Name("search-v1"),
)

// Feature flag service
type FeatureService struct {
    provider godi.ServiceProvider
    flags    FeatureFlags
}

func (f *FeatureService) GetSearchEngine() (SearchEngine, error) {
    engineKey := "search-v1" // default
    if f.flags.IsEnabled("new-search") {
        engineKey = "search-v2"
    }

    return godi.ResolveKeyed[SearchEngine](f.provider, engineKey)
}
```

## Storage Backends

Multiple storage options:

```go
// Storage implementations
services.AddSingleton(
    func() Storage { return NewS3Storage() },
    godi.Name("s3"),
)

services.AddSingleton(
    func() Storage { return NewGCSStorage() },
    godi.Name("gcs"),
)

services.AddSingleton(
    func() Storage { return NewLocalStorage() },
    godi.Name("local"),
)

// File service with configurable storage
type FileService struct {
    storages map[string]Storage
}

func NewFileService(provider godi.ServiceProvider) (*FileService, error) {
    // Pre-resolve all storage backends
    storages := make(map[string]Storage)

    for _, name := range []string{"s3", "gcs", "local"} {
        storage, err := godi.ResolveKeyed[Storage](provider, name)
        if err == nil {
            storages[name] = storage
        }
    }

    return &FileService{storages: storages}, nil
}

func (s *FileService) SaveFile(file []byte, backend string) error {
    storage, ok := s.storages[backend]
    if !ok {
        return fmt.Errorf("storage backend %s not available", backend)
    }

    return storage.Save(file)
}
```

## Injecting Keyed Services

Use struct tags in parameter objects:

```go
// Parameter object with keyed dependencies
type ServiceParams struct {
    godi.In

    PrimaryDB   Database `name:"primary"`
    ReplicaDB   Database `name:"replica"`
    CacheRedis  Cache    `name:"redis"`
    CacheMemory Cache    `name:"memory"`
}

func NewComplexService(params ServiceParams) *ComplexService {
    return &ComplexService{
        primaryDB:   params.PrimaryDB,
        replicaDB:   params.ReplicaDB,
        cacheRedis:  params.CacheRedis,
        cacheMemory: params.CacheMemory,
    }
}
```

## Dynamic Key Resolution

Resolve services with dynamic keys:

```go
type MultiTenantService struct {
    provider godi.ServiceProvider
}

func (s *MultiTenantService) GetTenantDB(tenantID string) (Database, error) {
    // Each tenant has their own database
    dbKey := fmt.Sprintf("tenant-%s", tenantID)

    db, err := godi.ResolveKeyed[Database](s.provider, dbKey)
    if err != nil {
        // Fallback to default
        return godi.ResolveKeyed[Database](s.provider, "default")
    }

    return db, nil
}

// Register tenant databases dynamically
func RegisterTenantDatabases(services godi.ServiceCollection, tenants []Tenant) {
    for _, tenant := range tenants {
        t := tenant // capture loop variable
        services.AddSingleton(
            func() Database {
                return NewPostgresDB(t.DatabaseURL)
            },
            godi.Name(fmt.Sprintf("tenant-%s", t.ID)),
        )
    }
}
```

## Testing with Keyed Services

Easy to mock specific implementations:

```go
func TestPaymentProcessing(t *testing.T) {
    services := godi.NewServiceCollection()

    // Register mock gateways
    services.AddSingleton(
        func() PaymentGateway {
            return &MockGateway{
                chargeFunc: func(amount float64, currency string) (*PaymentResult, error) {
                    return &PaymentResult{Success: true}, nil
                },
            }
        },
        godi.Name("stripe"),
    )

    services.AddSingleton(
        func() PaymentGateway {
            return &MockGateway{
                chargeFunc: func(amount float64, currency string) (*PaymentResult, error) {
                    return nil, errors.New("paypal unavailable")
                },
            }
        },
        godi.Name("paypal"),
    )

    provider, _ := services.BuildServiceProvider()
    defer provider.Close()

    processor := &PaymentProcessor{provider: provider}

    // Test successful payment
    result, err := processor.ProcessPayment(100, "USD", "stripe")
    assert.NoError(t, err)
    assert.True(t, result.Success)

    // Test failed payment
    _, err = processor.ProcessPayment(100, "USD", "paypal")
    assert.Error(t, err)
}
```

## Best Practices

### 1. Use Constants for Keys

```go
const (
    DBPrimary  = "primary"
    DBReplica  = "replica"
    DBAnalytics = "analytics"
)

services.AddSingleton(NewPrimaryDB, godi.Name(DBPrimary))
```

### 2. Document Available Keys

```go
// CacheService provides access to different cache implementations.
// Available keys:
// - "redis": Redis-based cache (production)
// - "memory": In-memory cache (development)
// - "distributed": Hazelcast distributed cache
type CacheService interface {
    Get(key string, cacheType string) (interface{}, error)
}
```

### 3. Provide Fallbacks

```go
func ResolveWithFallback[T any](provider godi.ServiceProvider, keys ...string) (T, error) {
    var zero T

    for _, key := range keys {
        service, err := godi.ResolveKeyed[T](provider, key)
        if err == nil {
            return service, nil
        }
    }

    return zero, fmt.Errorf("no service found for keys: %v", keys)
}

// Usage
cache, err := ResolveWithFallback[Cache](provider, "redis", "memory", "noop")
```

### 4. Validate at Startup

```go
func ValidateKeyedServices(provider godi.ServiceProvider) error {
    requiredKeys := map[string]reflect.Type{
        "primary": reflect.TypeOf((*Database)(nil)).Elem(),
        "replica": reflect.TypeOf((*Database)(nil)).Elem(),
        "redis":   reflect.TypeOf((*Cache)(nil)).Elem(),
    }

    for key, serviceType := range requiredKeys {
        if !provider.IsKeyedService(serviceType, key) {
            return fmt.Errorf("required keyed service not found: %s[%s]",
                serviceType, key)
        }
    }

    return nil
}
```

## Summary

Keyed services provide flexibility for:

- Multiple implementations of the same interface
- Environment-specific configurations
- Feature toggling
- Multi-tenancy
- A/B testing

They maintain type safety while allowing runtime selection of implementations, making your application more flexible and testable.
