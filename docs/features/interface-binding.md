# Interface Binding

Register concrete types to satisfy interfaces.

## The Problem

You have a concrete type but want to resolve by interface:

```go
type consoleLogger struct{}
func (c *consoleLogger) Log(msg string) { fmt.Println(msg) }

type Logger interface {
    Log(string)
}

// Register concrete
services.AddSingleton(NewConsoleLogger)  // Returns *consoleLogger

// Want to resolve by interface
logger := godi.MustResolve[Logger](provider)  // Error: Logger not registered
```

## The Solution: As Option

Use `godi.As[T]()` to register a concrete type as an interface:

```go
services.AddSingleton(NewConsoleLogger, godi.As[Logger]())

// Now resolvable by interface
logger := godi.MustResolve[Logger](provider)
```

## Basic Usage

```go
// Interface
type Cache interface {
    Get(key string) (any, bool)
    Set(key string, value any)
}

// Concrete implementation
type redisCache struct {
    client *redis.Client
}

func NewRedisCache(config *Config) *redisCache {
    return &redisCache{
        client: redis.NewClient(&redis.Options{Addr: config.RedisAddr}),
    }
}

// Register as interface
services.AddSingleton(NewRedisCache, godi.As[Cache]())

// Resolve by interface
cache := godi.MustResolve[Cache](provider)
```

## Multiple Interfaces

A type can implement and be registered as multiple interfaces:

```go
type userStore struct {
    db *sql.DB
}

// Implements multiple interfaces
type UserReader interface { GetUser(id int) *User }
type UserWriter interface { SaveUser(user *User) error }
type UserRepository interface {
    GetUser(id int) *User
    SaveUser(user *User) error
}

services.AddSingleton(NewUserStore,
    godi.As[UserReader](),
    godi.As[UserWriter](),
    godi.As[UserRepository](),
)

// Resolve by any interface
reader := godi.MustResolve[UserReader](provider)
writer := godi.MustResolve[UserWriter](provider)
repo := godi.MustResolve[UserRepository](provider)
// All return the same *userStore instance (singleton)
```

## With Keys and Groups

Combine with other options:

```go
// Named interface
services.AddSingleton(NewFileLogger,
    godi.Name("file"),
    godi.As[Logger](),
)

// Resolve by key and interface
fileLogger := godi.MustResolveKeyed[Logger](provider, "file")

// Interface in group
services.AddSingleton(NewEmailValidator,
    godi.Group("validators"),
    godi.As[Validator](),
)

validators := godi.MustResolveGroup[Validator](provider, "validators")
```

## Use Cases

### Swappable Implementations

```go
// Production
services.AddSingleton(NewProductionEmailer, godi.As[Emailer]())

// Testing
services.AddSingleton(NewMockEmailer, godi.As[Emailer]())

// Code uses interface
type NotificationService struct {
    emailer Emailer  // Interface
}
```

### Repository Pattern

```go
type UserRepository interface {
    FindByID(id int) (*User, error)
    FindByEmail(email string) (*User, error)
    Save(user *User) error
}

// PostgreSQL implementation
type postgresUserRepository struct {
    db *sql.DB
}

func NewUserRepository(db *sql.DB) *postgresUserRepository {
    return &postgresUserRepository{db: db}
}

// Register implementation as interface
services.AddScoped(NewUserRepository, godi.As[UserRepository]())

// Service depends on interface
type UserService struct {
    repo UserRepository
}

func NewUserService(repo UserRepository) *UserService {
    return &UserService{repo: repo}
}
```

### Dependency Inversion

```go
// Domain layer defines interface
type OrderPlacer interface {
    PlaceOrder(order *Order) error
}

// Infrastructure implements it
type stripeOrderPlacer struct {
    client *stripe.Client
}

func NewStripeOrderPlacer(config *Config) *stripeOrderPlacer {
    return &stripeOrderPlacer{
        client: stripe.NewClient(config.StripeKey),
    }
}

// Register infrastructure as domain interface
services.AddSingleton(NewStripeOrderPlacer, godi.As[OrderPlacer]())

// Domain service uses interface
type CheckoutService struct {
    placer OrderPlacer
}
```

## With Parameter Objects

Reference interfaces in parameter objects:

```go
type ServiceParams struct {
    godi.In

    Logger Logger     // Interface
    Cache  Cache      // Interface
    Repo   Repository // Interface
}

func NewService(params ServiceParams) *Service {
    return &Service{
        logger: params.Logger,
        cache:  params.Cache,
        repo:   params.Repo,
    }
}
```

## Testing

Easy to swap implementations for testing:

```go
// Production setup
func ProductionModule() godi.Module {
    return func(services *godi.ServiceCollection) {
        services.AddSingleton(NewProductionDB, godi.As[Database]())
        services.AddSingleton(NewProductionCache, godi.As[Cache]())
    }
}

// Test setup
func TestModule() godi.Module {
    return func(services *godi.ServiceCollection) {
        services.AddSingleton(NewMockDB, godi.As[Database]())
        services.AddSingleton(NewMockCache, godi.As[Cache]())
    }
}

// In tests
services := godi.NewCollection()
services.AddModule(TestModule())  // Use mocks
```

## Common Mistakes

### Resolving Concrete When Registered as Interface

```go
services.AddSingleton(NewConsoleLogger, godi.As[Logger]())

// Error: *consoleLogger not registered directly
logger := godi.MustResolve[*consoleLogger](provider)

// Correct: resolve by interface
logger := godi.MustResolve[Logger](provider)
```

### Forgetting As Option

```go
// Only registers *consoleLogger
services.AddSingleton(NewConsoleLogger)

// Error: Logger interface not registered
logger := godi.MustResolve[Logger](provider)
```

---

**See also:** [Keyed Services](keyed-services.md) | [Parameter Objects](parameter-objects.md)
