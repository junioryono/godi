# Keyed Services

Keyed services let you register multiple implementations of the same interface and choose which one to use.

## When to Use Keyed Services

Use keyed services when you have:

- Multiple database connections (primary, replica)
- Different implementations for different scenarios
- Feature flags or environment-specific services
- Multiple external API clients

## Basic Example

```go
// Define interface
type Logger interface {
    Log(message string)
}

// Multiple implementations
type FileLogger struct {
    filename string
}

func NewFileLogger() Logger {
    return &FileLogger{filename: "app.log"}
}

type ConsoleLogger struct{}

func NewConsoleLogger() Logger {
    return &ConsoleLogger{}
}

// Register with keys
var LoggingModule = godi.NewModule("logging",
    godi.AddSingleton(NewFileLogger, godi.Name("file")),
    godi.AddSingleton(NewConsoleLogger, godi.Name("console")),
    godi.AddSingleton(NewCloudLogger, godi.Name("cloud")),
)

// Use specific implementation
func main() {
    services := godi.NewCollection()
    services.AddModules(LoggingModule)

    provider, _ := services.Build()
    defer provider.Close()

    // Get specific logger
    fileLogger, _ := godi.ResolveKeyed[Logger](provider, "file")
    fileLogger.Log("Writing to file")

    consoleLogger, _ := godi.ResolveKeyed[Logger](provider, "console")
    consoleLogger.Log("Writing to console")
}
```

## Real-World Example: Multiple Databases

```go
// Database interface
type Database interface {
    Query(sql string) ([]Row, error)
    Execute(sql string) error
}

// Different database connections
func NewPrimaryDB(config *Config) Database {
    return &PostgresDB{
        connString: config.PrimaryDB,
        maxConns:   100,
    }
}

func NewReplicaDB(config *Config) Database {
    return &PostgresDB{
        connString: config.ReplicaDB,
        maxConns:   50,
        readOnly:   true,
    }
}

func NewAnalyticsDB(config *Config) Database {
    return &ClickhouseDB{
        connString: config.AnalyticsDB,
    }
}

// Module with keyed databases
var DatabaseModule = godi.NewModule("database",
    godi.AddSingleton(NewConfig),
    godi.AddSingleton(NewPrimaryDB, godi.Name("primary")),
    godi.AddSingleton(NewReplicaDB, godi.Name("replica")),
    godi.AddSingleton(NewAnalyticsDB, godi.Name("analytics")),
)

// Repository using specific database
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

func (r *UserRepository) CreateUser(user *User) error {
    // Write to primary
    return r.primaryDB.Execute("INSERT INTO users...")
}

func (r *UserRepository) GetUser(id string) (*User, error) {
    // Read from replica
    rows, err := r.replicaDB.Query("SELECT * FROM users WHERE id = ?")
    // ...
}
```

## Using Parameter Objects

For cleaner code, use parameter objects with named dependencies:

```go
type RepositoryDeps struct {
    godi.In

    Primary   Database `name:"primary"`
    Replica   Database `name:"replica"`
    Analytics Database `name:"analytics" optional:"true"`
    Logger    Logger
}

func NewUserRepository(deps RepositoryDeps) *UserRepository {
    repo := &UserRepository{
        primary: deps.Primary,
        replica: deps.Replica,
        logger:  deps.Logger,
    }

    // Analytics is optional
    if deps.Analytics != nil {
        repo.analytics = deps.Analytics
    }

    return repo
}
```

## Environment-Based Selection

Choose implementations based on environment:

```go
var CacheModule = godi.NewModule("cache",
    godi.AddSingleton(func() Cache {
        switch os.Getenv("CACHE_TYPE") {
        case "redis":
            return NewRedisCache()
        case "memcached":
            return NewMemcachedCache()
        default:
            return NewMemoryCache()
        }
    }),
)

// Or use keyed services
var CacheModule = godi.NewModule("cache",
    godi.AddSingleton(NewRedisCache, godi.Name("redis")),
    godi.AddSingleton(NewMemcachedCache, godi.Name("memcached")),
    godi.AddSingleton(NewMemoryCache, godi.Name("memory")),

    // Default cache
    godi.AddSingleton(func(provider godi.ServiceProvider) Cache {
        cacheType := os.Getenv("CACHE_TYPE")
        if cacheType == "" {
            cacheType = "memory"
        }

        cache, err := godi.ResolveKeyed[Cache](provider, cacheType)
        if err != nil {
            // Fallback to memory
            cache, _ = godi.ResolveKeyed[Cache](provider, "memory")
        }
        return cache
    }),
)
```

## Feature Flags Pattern

Use keyed services for feature toggles:

```go
// Payment processors
var PaymentModule = godi.NewModule("payment",
    godi.AddSingleton(NewStripeProcessor, godi.Name("stripe")),
    godi.AddSingleton(NewPayPalProcessor, godi.Name("paypal")),
    godi.AddSingleton(NewBraintreeProcessor, godi.Name("braintree")),

    // Feature flag based selection
    godi.AddScoped(func(provider godi.ServiceProvider, config *Config) PaymentProcessor {
        // Check feature flags
        if config.Features.UseNewPaymentProvider {
            processor, _ := godi.ResolveKeyed[PaymentProcessor](provider, "braintree")
            return processor
        }

        // Default
        processor, _ := godi.ResolveKeyed[PaymentProcessor](provider, "stripe")
        return processor
    }),
)
```

## Testing with Keyed Services

Easy to mock specific implementations:

```go
var TestPaymentModule = godi.NewModule("test-payment",
    godi.AddSingleton(func() PaymentProcessor {
        return &MockPaymentProcessor{
            shouldSucceed: true,
        }
    }, godi.Name("stripe")),

    godi.AddSingleton(func() PaymentProcessor {
        return &MockPaymentProcessor{
            shouldSucceed: false, // Test failures
        }
    }, godi.Name("failing")),
)

func TestPaymentFlow(t *testing.T) {
    services := godi.NewCollection()
    services.AddModules(TestPaymentModule)

    provider, _ := services.Build()

    // Test success case
    successProcessor, _ := godi.ResolveKeyed[PaymentProcessor](provider, "stripe")
    assert.NoError(t, successProcessor.Process(payment))

    // Test failure case
    failProcessor, _ := godi.ResolveKeyed[PaymentProcessor](provider, "failing")
    assert.Error(t, failProcessor.Process(payment))
}
```

## Best Practices

### 1. Use Clear, Descriptive Keys

```go
// ✅ Good keys
godi.Name("primary-db")
godi.Name("replica-db")
godi.Name("analytics-db")

// ❌ Vague keys
godi.Name("db1")
godi.Name("db2")
godi.Name("db3")
```

### 2. Document Available Keys

```go
// Package database provides database connections.
//
// Available keys:
// - "primary": Read-write connection to primary database
// - "replica": Read-only connection to replica
// - "analytics": Connection to analytics database (ClickHouse)
var DatabaseModule = godi.NewModule("database",
    // ...
)
```

### 3. Provide Defaults When Possible

```go
var NotificationModule = godi.NewModule("notification",
    // Keyed implementations
    godi.AddSingleton(NewEmailNotifier, godi.Name("email")),
    godi.AddSingleton(NewSMSNotifier, godi.Name("sms")),
    godi.AddSingleton(NewPushNotifier, godi.Name("push")),

    // Default notifier (not keyed)
    godi.AddSingleton(func() Notifier {
        return NewEmailNotifier() // Email as default
    }),
)
```

### 4. Consider Using Enums for Keys

```go
type CacheType string

const (
    CacheTypeRedis     CacheType = "redis"
    CacheTypeMemcached CacheType = "memcached"
    CacheTypeMemory    CacheType = "memory"
)

// Use enum values as keys
godi.AddSingleton(NewRedisCache, godi.Name(string(CacheTypeRedis)))

// Resolve with type safety
cache, _ := godi.ResolveKeyed[Cache](provider, string(CacheTypeRedis))
```

## When NOT to Use Keyed Services

Don't use keyed services for:

- Simple feature toggles (use configuration instead)
- Services that should be composed (use decorators)
- When you need ALL implementations (use service groups)

## Summary

Keyed services are perfect for:

- Multiple implementations of the same interface
- Environment-specific services
- Feature flags and A/B testing
- Multiple external service connections

Use descriptive keys and provide good documentation for available options!
