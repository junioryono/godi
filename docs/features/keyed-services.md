# Keyed Services

Register and resolve multiple implementations of the same type using keys.

## The Problem

You need multiple database connections, cache implementations, or payment gateways - all of the same type:

```go
// Can't do this - same type registered twice
services.AddSingleton(NewPrimaryDB)   // Database
services.AddSingleton(NewReplicaDB)   // Also Database - conflict!
```

## The Solution: Keys

```go
services.AddSingleton(NewPrimaryDB, godi.Name("primary"))
services.AddSingleton(NewReplicaDB, godi.Name("replica"))

// Resolve by key
primary := godi.MustResolveKeyed[Database](provider, "primary")
replica := godi.MustResolveKeyed[Database](provider, "replica")
```

## Registration

Use `godi.Name()` to assign a key:

```go
// Database connections
services.AddSingleton(NewPrimaryDB, godi.Name("primary"))
services.AddSingleton(NewReplicaDB, godi.Name("replica"))
services.AddSingleton(NewAnalyticsDB, godi.Name("analytics"))

// Cache implementations
services.AddSingleton(NewRedisCache, godi.Name("redis"))
services.AddSingleton(NewMemoryCache, godi.Name("memory"))
```

## Resolution

```go
// By key
primary := godi.MustResolveKeyed[Database](provider, "primary")
replica := godi.MustResolveKeyed[Database](provider, "replica")

// With error handling
analytics, err := godi.ResolveKeyed[Database](provider, "analytics")
if err != nil {
    // Key not found or resolution error
}
```

## Use Cases

### Multiple Database Connections

```go
type DatabaseManager struct {
    primary   Database
    replica   Database
    analytics Database
}

func NewDatabaseManager(provider godi.Provider) *DatabaseManager {
    return &DatabaseManager{
        primary:   godi.MustResolveKeyed[Database](provider, "primary"),
        replica:   godi.MustResolveKeyed[Database](provider, "replica"),
        analytics: godi.MustResolveKeyed[Database](provider, "analytics"),
    }
}

func (m *DatabaseManager) Read(query string) Result {
    return m.replica.Query(query)  // Use replica for reads
}

func (m *DatabaseManager) Write(query string) Result {
    return m.primary.Exec(query)  // Use primary for writes
}
```

### Strategy Pattern

```go
type PaymentStrategy interface {
    Process(amount float64) error
}

// Register strategies
services.AddSingleton(NewStripeStrategy, godi.Name("stripe"), godi.As[PaymentStrategy]())
services.AddSingleton(NewPayPalStrategy, godi.Name("paypal"), godi.As[PaymentStrategy]())
services.AddSingleton(NewSquareStrategy, godi.Name("square"), godi.As[PaymentStrategy]())

// Select at runtime
func (s *PaymentService) Process(method string, amount float64) error {
    strategy := godi.MustResolveKeyed[PaymentStrategy](s.provider, method)
    return strategy.Process(amount)
}
```

### Environment-Specific Services

```go
func RegisterEmailService(services *godi.ServiceCollection, env string) {
    switch env {
    case "development":
        services.AddSingleton(NewMockEmailer, godi.Name("email"))
    case "staging":
        services.AddSingleton(NewSandboxEmailer, godi.Name("email"))
    case "production":
        services.AddSingleton(NewSESEmailer, godi.Name("email"))
    }
}

// Same resolution everywhere
emailer := godi.MustResolveKeyed[Emailer](provider, "email")
```

## With Parameter Objects

Use the `name` tag to inject keyed services:

```go
type ServiceParams struct {
    godi.In

    PrimaryDB   Database `name:"primary"`
    ReplicaDB   Database `name:"replica"`
    RedisCache  Cache    `name:"redis"`
    MemoryCache Cache    `name:"memory"`
}

func NewService(params ServiceParams) *Service {
    return &Service{
        primary: params.PrimaryDB,
        replica: params.ReplicaDB,
        redis:   params.RedisCache,
        memory:  params.MemoryCache,
    }
}
```

## Best Practices

### Use Constants for Keys

```go
const (
    PrimaryDB   = "primary"
    ReplicaDB   = "replica"
    RedisCache  = "redis"
    MemoryCache = "memory"
)

// Registration
services.AddSingleton(NewPrimaryDB, godi.Name(PrimaryDB))

// Resolution
db := godi.MustResolveKeyed[Database](provider, PrimaryDB)
```

### Fallback Pattern

```go
func GetCache(provider godi.Provider) Cache {
    // Try primary
    cache, err := godi.ResolveKeyed[Cache](provider, "redis")
    if err == nil {
        return cache
    }

    // Fallback
    cache, err = godi.ResolveKeyed[Cache](provider, "memory")
    if err == nil {
        return cache
    }

    // Default
    return NewDefaultCache()
}
```

## Common Mistakes

### Duplicate Keys

```go
// Error: same key twice
services.AddSingleton(NewServiceA, godi.Name("service"))
services.AddSingleton(NewServiceB, godi.Name("service"))  // Conflict!

// Correct: unique keys
services.AddSingleton(NewServiceA, godi.Name("serviceA"))
services.AddSingleton(NewServiceB, godi.Name("serviceB"))
```

### Wrong Type

```go
services.AddSingleton(NewLogger, godi.Name("logger"))

// Error: wrong type
cache := godi.MustResolveKeyed[Cache](provider, "logger")  // Panic!

// Correct: matching type
logger := godi.MustResolveKeyed[Logger](provider, "logger")
```

---

**See also:** [Service Groups](service-groups.md) | [Parameter Objects](parameter-objects.md)
