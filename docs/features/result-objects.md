# Result Objects

Register multiple services from a single constructor.

## The Problem

One constructor creates multiple related services:

```go
// Creates both a Database and a HealthChecker
func NewDatabaseConnection(config *Config) (*Database, *HealthChecker) {
    db := connectDB(config)
    health := &HealthChecker{db: db}
    return db, health
}
```

godi registers each non-error return of a multi-return constructor as its own
service, and every return shares the registration's options: `godi.Name` keys
every return with the name, and `godi.Group` adds every return to the group. To
give each output its own name or group, use a result object.

## The Solution: Result Objects

Use `godi.Out` to return multiple services:

```go
type DatabaseResult struct {
    godi.Out

    Database      *Database
    HealthChecker *HealthChecker
}

func NewDatabaseConnection(config *Config) DatabaseResult {
    db := connectDB(config)
    return DatabaseResult{
        Database:      db,
        HealthChecker: &HealthChecker{db: db},
    }
}

// Register once, get both services
services.AddSingleton(NewDatabaseConnection)

// Resolve each separately
db := godi.MustResolve[*Database](provider)
health := godi.MustResolve[*HealthChecker](provider)
```

## Basic Usage

```go
// 1. Define result struct with embedded godi.Out
type Result struct {
    godi.Out  // Must be embedded anonymously

    Service1 *Service1
    Service2 *Service2
    Service3 *Service3
}

// 2. Return from constructor
func NewServices(deps Dependencies) Result {
    return Result{
        Service1: NewService1(deps),
        Service2: NewService2(deps),
        Service3: NewService3(deps),
    }
}

// 3. Register once
services.AddSingleton(NewServices)

// 4. Resolve individually
s1 := godi.MustResolve[*Service1](provider)
s2 := godi.MustResolve[*Service2](provider)
s3 := godi.MustResolve[*Service3](provider)
```

## Field Tags

### Named Services

```go
type CacheResult struct {
    godi.Out

    RedisCache  Cache `name:"redis"`
    MemoryCache Cache `name:"memory"`
}

func NewCaches(config *Config) CacheResult {
    return CacheResult{
        RedisCache:  NewRedisCache(config.RedisURL),
        MemoryCache: NewMemoryCache(config.CacheSize),
    }
}

// Resolve by name
redis := godi.MustResolveKeyed[Cache](provider, "redis")
memory := godi.MustResolveKeyed[Cache](provider, "memory")
```

### Group Membership

```go
type ValidatorResult struct {
    godi.Out

    EmailValidator   Validator `group:"validators"`
    PhoneValidator   Validator `group:"validators"`
    AddressValidator Validator `group:"validators"`
}

func NewValidators() ValidatorResult {
    return ValidatorResult{
        EmailValidator:   &EmailValidator{},
        PhoneValidator:   &PhoneValidator{},
        AddressValidator: &AddressValidator{},
    }
}

// Resolve as group
validators := godi.MustResolveGroup[Validator](provider, "validators")
```

### Interface Binding

There is no `as` tag, and `godi.As` cannot be combined with a result object.
A field is registered under its declared type, so declare the field with the
interface type to provide an interface:

```go
type RepositoryResult struct {
    godi.Out

    UserRepo  UserRepository  // registered as UserRepository
    OrderRepo OrderRepository // registered as OrderRepository
}

func NewRepositories(db *Database) RepositoryResult {
    return RepositoryResult{
        UserRepo:  &postgresUserRepository{db: db},
        OrderRepo: &postgresOrderRepository{db: db},
    }
}
```

### Skipping a Field

A field tagged `inject:"-"` is not registered:

```go
type Result struct {
    godi.Out

    Service *Service
    Debug   *DebugInfo `inject:"-"` // not registered
}
```

## Use Cases

### Database with Health Checker

```go
type DatabaseResult struct {
    godi.Out

    DB          *Database
    Health      *HealthChecker
    Migrations  *MigrationRunner
}

func NewDatabase(config *Config, logger *Logger) (DatabaseResult, error) {
    db, err := sql.Open("postgres", config.DatabaseURL)
    if err != nil {
        return DatabaseResult{}, err
    }

    return DatabaseResult{
        DB:         &Database{db},
        Health:     &HealthChecker{db},
        Migrations: &MigrationRunner{db, logger},
    }, nil
}
```

### Cache Layer

```go
type CacheResult struct {
    godi.Out

    LocalCache  Cache `name:"local"`
    RemoteCache Cache `name:"remote"`
    TieredCache Cache `name:"tiered"`
}

func NewCacheLayer(config *Config) CacheResult {
    local := NewMemoryCache(config.LocalCacheSize)
    remote := NewRedisCache(config.RedisURL)
    tiered := NewTieredCache(local, remote)

    return CacheResult{
        LocalCache:  local,
        RemoteCache: remote,
        TieredCache: tiered,
    }
}
```

### HTTP Client Suite

```go
type HTTPClientResult struct {
    godi.Out

    DefaultClient  *http.Client `name:"default"`
    TimeoutClient  *http.Client `name:"timeout"`
    RetryingClient *http.Client `name:"retrying"`
}

func NewHTTPClients(config *Config) HTTPClientResult {
    return HTTPClientResult{
        DefaultClient:  &http.Client{},
        TimeoutClient:  &http.Client{Timeout: config.HTTPTimeout},
        RetryingClient: NewRetryingClient(config.MaxRetries),
    }
}
```

## Combining In and Out

Use both parameter and result objects:

```go
type ServiceParams struct {
    godi.In

    Config   *Config
    Logger   *Logger
    Database *Database
}

type ServiceResult struct {
    godi.Out

    UserService  *UserService
    OrderService *OrderService
    AdminService *AdminService
}

func NewServices(params ServiceParams) ServiceResult {
    return ServiceResult{
        UserService:  NewUserService(params.Database, params.Logger),
        OrderService: NewOrderService(params.Database, params.Logger),
        AdminService: NewAdminService(params.Database, params.Logger, params.Config),
    }
}
```

## With Errors

Result objects work with error returns:

```go
func NewDatabaseConnection(config *Config) (DatabaseResult, error) {
    db, err := connectDB(config)
    if err != nil {
        return DatabaseResult{}, err
    }

    return DatabaseResult{
        Database:      db,
        HealthChecker: &HealthChecker{db: db},
    }, nil
}
```

## Common Mistakes

### Named Embedding

```go
// Wrong
type BadResult struct {
    Out godi.Out  // Named - won't work
    Service *Service
}

// Correct
type GoodResult struct {
    godi.Out  // Anonymous
    Service *Service
}
```

### Optional Outputs (nil fields)

A field left nil is "not provided" by that construction. The constructor still
runs only once, and the other fields are cached as usual:

- Resolving the missing service fails with `godi.ErrServiceNotFound`, so an
  `optional:"true"` dependency on it receives its zero value.
- A nil group field is skipped: the group just has one fewer member.

```go
type Clients struct {
    godi.Out
    Primary *Client
    Replica *Client `name:"replica"` // nil when no replica is configured
}
```

### Unexported Fields

```go
// Wrong
type BadResult struct {
    godi.Out
    service *Service  // lowercase - not registered
}

// Correct
type GoodResult struct {
    godi.Out
    Service *Service  // Uppercase - registered
}
```

---

**See also:** [Parameter Objects](parameter-objects.md) | [Interface Binding](interface-binding.md)
