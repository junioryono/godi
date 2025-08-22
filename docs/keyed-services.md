# Keyed Services

Register and resolve multiple implementations of the same interface using keys.

## Understanding Keyed Services

Keyed services allow you to register multiple implementations of the same type and resolve them by a unique key:

```go
// Register multiple cache implementations
services.AddSingleton(NewRedisCache, godi.Name("redis"))
services.AddSingleton(NewMemoryCache, godi.Name("memory"))
services.AddSingleton(NewDiskCache, godi.Name("disk"))

// Resolve specific implementation
redisCache := godi.MustResolveKeyed[Cache](provider, "redis")
memoryCache := godi.MustResolveKeyed[Cache](provider, "memory")
```

## Registration with Keys

### Named Services

Use `godi.Name()` to assign a key to a service:

```go
// Database connections
func NewPrimaryDB(config *Config) Database {
    return connectDB(config.PrimaryHost)
}

func NewReplicaDB(config *Config) Database {
    return connectDB(config.ReplicaHost)
}

func NewAnalyticsDB(config *Config) Database {
    return connectDB(config.AnalyticsHost)
}

// Register with names
services.AddSingleton(NewPrimaryDB, godi.Name("primary"))
services.AddSingleton(NewReplicaDB, godi.Name("replica"))
services.AddSingleton(NewAnalyticsDB, godi.Name("analytics"))
```

### Resolution

```go
// Resolve by key
primary := godi.MustResolveKeyed[Database](provider, "primary")
replica := godi.MustResolveKeyed[Database](provider, "replica")

// With error handling
analytics, err := godi.ResolveKeyed[Database](provider, "analytics")
if err != nil {
    // Handle error
}
```

## Common Use Cases

### Multiple Database Connections

```go
type DatabaseManager interface {
    Read(query string) Result
    Write(query string) Result
    RunAnalytics(query string) Result
}

type databaseManager struct {
    primary   Database
    replica   Database
    analytics Database
}

func NewDatabaseManager(provider godi.Provider) DatabaseManager {
    return &databaseManager{
        primary:   godi.MustResolveKeyed[Database](provider, "primary"),
        replica:   godi.MustResolveKeyed[Database](provider, "replica"),
        analytics: godi.MustResolveKeyed[Database](provider, "analytics"),
    }
}

func (m *databaseManager) Read(query string) Result {
    // Use replica for reads
    return m.replica.Query(query)
}

func (m *databaseManager) Write(query string) Result {
    // Use primary for writes
    return m.primary.Exec(query)
}

func (m *databaseManager) RunAnalytics(query string) Result {
    // Use analytics DB for heavy queries
    return m.analytics.Query(query)
}
```

### Strategy Pattern

```go
// Payment strategies
type PaymentStrategy interface {
    ProcessPayment(amount float64) (*Transaction, error)
    ValidateCard(card *Card) error
}

// Implementations
type StripeStrategy struct { client *stripe.Client }
type PayPalStrategy struct { client *paypal.Client }
type SquareStrategy struct { client *square.Client }

type PaymentStrategyKey string

const (
    StripeStrategyKey PaymentStrategyKey = "stripe"
    PayPalStrategyKey PaymentStrategyKey = "paypal"
    SquareStrategyKey PaymentStrategyKey = "square"
)

// Register strategies
services.AddSingleton(NewStripeStrategy, godi.Name(StripeStrategyKey), godi.As[PaymentStrategy]())
services.AddSingleton(NewPayPalStrategy, godi.Name(PayPalStrategyKey), godi.As[PaymentStrategy]())
services.AddSingleton(NewSquareStrategy, godi.Name(SquareStrategyKey), godi.As[PaymentStrategy]())

// Payment service selects strategy
type PaymentService struct {
    provider godi.Provider
}

func (s *PaymentService) ProcessPayment(method StripeStrategyKey, amount float64) (*Transaction, error) {
    strategy := godi.MustResolveKeyed[PaymentStrategy](s.provider, string(method))
    return strategy.ProcessPayment(amount)
}
```

### Environment-Specific Services

```go
// Different implementations per environment
func RegisterServices(services godi.Collection, env string) {
    switch env {
    case "development":
        services.AddSingleton(NewMockEmailSender, godi.Name("email"), godi.As[EmailSender]())
    case "staging":
        services.AddSingleton(NewSandboxEmailSender, godi.Name("email"), godi.As[EmailSender]())
    case "production":
        services.AddSingleton(NewSESEmailSender, godi.Name("email"), godi.As[EmailSender]())
    }
}

// Always resolve the same way
emailSender := godi.MustResolveKeyed[EmailSender](provider, "email")
```

## Advanced Patterns

### Dynamic Key Resolution

```go
type CacheManager struct {
    provider godi.Provider
    config   *Config
}

func (m *CacheManager) GetCache() Cache {
    // Dynamically determine which cache to use
    cacheType := m.config.GetCacheType() // Returns "redis", "memory", etc.
    return godi.MustResolveKeyed[Cache](m.provider, cacheType)
}
```

### Fallback Pattern

```go
func GetCacheWithFallback(provider godi.Provider) Cache {
    // Try primary cache
    cache, err := godi.ResolveKeyed[Cache](provider, "redis")
    if err == nil {
        return cache
    }

    // Fallback to memory cache
    cache, err = godi.ResolveKeyed[Cache](provider, "memory")
    if err == nil {
        return cache
    }

    // Last resort - create default
    return NewDefaultCache()
}
```

### Factory with Keys

```go
type RepositoryFactory struct {
    provider godi.Provider
}

func (f *RepositoryFactory) GetRepository(entity string) Repository {
    // Map entity types to repository keys
    keyMap := map[string]string{
        "user":    "userRepo",
        "order":   "orderRepo",
        "product": "productRepo",
    }

    key, ok := keyMap[entity]
    if !ok {
        return NewGenericRepository()
    }

    return godi.MustResolveKeyed[Repository](f.provider, key)
}
```

## With Parameter Objects

### Using Named Dependencies in Parameter Objects

```go
type ServiceParams struct {
    godi.In

    PrimaryDB   Database `name:"primary"`
    ReplicaDB   Database `name:"replica"`
    RedisCache  Cache    `name:"redis"`
    MemoryCache Cache    `name:"memory"`
}

func NewComplexService(params ServiceParams) ComplexService {
    return &complexService{
        primary: params.PrimaryDB,
        replica: params.ReplicaDB,
        redis:   params.RedisCache,
        memory:  params.MemoryCache,
    }
}

// godi automatically resolves the named dependencies
services.AddScoped(NewComplexService)
```

## Multi-Tenant Applications

### Tenant-Specific Services

```go
// Register tenant-specific databases
for _, tenant := range tenants {
    services.AddSingleton(
        func(t Tenant) func() Database {
            return func() Database {
                return NewTenantDatabase(t.ConnectionString)
            }
        }(tenant),
        godi.Name(tenant.ID),
    )
}

// Resolve for specific tenant
func GetTenantDatabase(provider godi.Provider, tenantID string) Database {
    return godi.MustResolveKeyed[Database](provider, tenantID)
}
```

### Request-Scoped Tenant Resolution

```go
type TenantService struct {
    provider godi.Provider
}

func (s *TenantService) GetDatabase(ctx context.Context) Database {
    // Extract tenant from context
    tenantID := GetTenantID(ctx)

    // Resolve tenant-specific database
    return godi.MustResolveKeyed[Database](s.provider, tenantID)
}
```

## Testing with Keyed Services

### Mock Specific Implementations

```go
func TestPaymentService(t *testing.T) {
    services := godi.NewCollection()

    // Register mock for specific payment method
    services.AddSingleton(NewMockStripeStrategy, godi.Name("stripe"), godi.As[PaymentStrategy]())

    provider, _ := services.Build()

    paymentService := NewPaymentService(provider)

    // Test stripe payment
    tx, err := paymentService.ProcessPayment("stripe", 100.00)
    assert.NoError(t, err)
    assert.NotNil(t, tx)
}
```

### Override Specific Keys

```go
func SetupTestProvider(overrides map[string]any) godi.Provider {
    services := godi.NewCollection()

    // Register defaults
    services.AddSingleton(NewDefaultLogger, godi.Name("logger"))
    services.AddSingleton(NewDefaultCache, godi.Name("cache"))

    // Override specific keys for testing
    for key, service := range overrides {
        services.AddSingleton(service, godi.Name(key))
    }

    provider, err := services.Build()
    if err != nil {
        panic(err)
    }

    return provider
}

// Usage in test
provider := SetupTestProvider(map[string]any{
    "cache": NewMockCache(), // Override cache with mock
})
```

## Best Practices

1. **Use descriptive keys** - Make keys self-documenting
2. **Document available keys** - List all possible keys in comments
3. **Handle missing keys gracefully** - Provide fallbacks when appropriate
4. **Validate keys at build time** - Fail fast for invalid configurations
5. **Keep keys consistent** - Use constants for repeated keys

### Key Constants

```go
// Define constants for keys
const (
    PrimaryDB   = "primary"
    ReplicaDB   = "replica"
    RedisCache  = "redis"
    MemoryCache = "memory"
)

// Use constants in registration
services.AddSingleton(NewPrimaryDB, godi.Name(PrimaryDB))
services.AddSingleton(NewRedisCache, godi.Name(RedisCache))

// Use constants in resolution
primary := godi.MustResolveKeyed[Database](provider, PrimaryDB)
cache := godi.MustResolveKeyed[Cache](provider, RedisCache)
```

## Common Pitfalls

### Key Conflicts

```go
// ❌ Don't register same key twice
services.AddSingleton(NewServiceA, godi.Name("service"))
services.AddSingleton(NewServiceB, godi.Name("service")) // Error!

// ✅ Use unique keys
services.AddSingleton(NewServiceA, godi.Name("serviceA"))
services.AddSingleton(NewServiceB, godi.Name("serviceB"))
```

### Type Mismatches

```go
// ❌ Wrong type assertion
services.AddSingleton(NewLogger, godi.Name("logger"))
cache := godi.MustResolveKeyed[Cache](provider, "logger") // Panic!

// ✅ Correct type
logger := godi.MustResolveKeyed[Logger](provider, "logger")
```

## Next Steps

- Explore [Service Groups](service-groups.md)
- Learn about [Parameter Objects](parameter-objects.md)
- Understand [Result Objects](result-objects.md)
