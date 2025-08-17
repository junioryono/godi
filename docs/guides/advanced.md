# Advanced Features

This guide covers advanced godi features for specific use cases. Most applications won't need these, but they're powerful when you do.

## Keyed Services

Register multiple implementations of the same interface:

```go
// Multiple database connections
var DatabaseModule = godi.NewModule("databases",
    godi.AddSingleton(NewPrimaryDB, godi.Name("primary")),
    godi.AddSingleton(NewReplicaDB, godi.Name("replica")),
    godi.AddSingleton(NewAnalyticsDB, godi.Name("analytics")),
)

// Use specific implementation
func main() {
    collection := godi.NewCollection()
    collection.AddModules(DatabaseModule)
    provider, _ := collection.Build()

    // Get specific database
    primary, _ := godi.ResolveKeyed[Database](provider, "primary")
    replica, _ := godi.ResolveKeyed[Database](provider, "replica")

    // Use different databases for different operations
    primary.Execute("INSERT INTO users...")  // Writes go to primary
    replica.Query("SELECT * FROM users...")  // Reads from replica
}
```

### With Parameter Objects

```go
type RepositoryParams struct {
    godi.In

    Primary   Database `name:"primary"`
    Replica   Database `name:"replica"`
    Analytics Database `name:"analytics" optional:"true"`
}

func NewUserRepository(params RepositoryParams) *UserRepository {
    return &UserRepository{
        primary:   params.Primary,
        replica:   params.Replica,
        analytics: params.Analytics,  // Might be nil
    }
}
```

## Service Groups

Collect multiple services of the same type:

```go
// Register validators
var ValidationModule = godi.NewModule("validation",
    godi.AddSingleton(NewEmailValidator, godi.Group("validators")),
    godi.AddSingleton(NewPhoneValidator, godi.Group("validators")),
    godi.AddSingleton(NewAddressValidator, godi.Group("validators")),
)

// Use all validators
type ValidationService struct {
    validators []Validator
}

func NewValidationService(params struct {
    godi.In
    Validators []Validator `group:"validators"`
}) *ValidationService {
    return &ValidationService{
        validators: params.Validators,
    }
}

func (v *ValidationService) ValidateAll(data any) error {
    for _, validator := range v.validators {
        if err := validator.Validate(data); err != nil {
            return err
        }
    }
    return nil
}
```

### Real Example: Middleware Chain

```go
// HTTP middleware
type Middleware interface {
    Wrap(http.Handler) http.Handler
}

// Register middleware
var MiddlewareModule = godi.NewModule("middleware",
    godi.AddScoped(NewLoggingMiddleware, godi.Group("middleware")),
    godi.AddScoped(NewAuthMiddleware, godi.Group("middleware")),
    godi.AddScoped(NewRateLimitMiddleware, godi.Group("middleware")),
)

// Build middleware chain
type Server struct {
    middleware []Middleware
    handler    http.Handler
}

func NewServer(params struct {
    godi.In
    Middleware []Middleware `group:"middleware"`
}) *Server {
    return &Server{
        middleware: params.Middleware,
    }
}

func (s *Server) Start() {
    handler := s.handler

    // Apply middleware in reverse order
    for i := len(s.middleware) - 1; i >= 0; i-- {
        handler = s.middleware[i].Wrap(handler)
    }

    http.ListenAndServe(":8080", handler)
}
```

## Parameter Objects (In/Out)

### Input Parameters (godi.In)

Simplify constructors with many dependencies:

```go
// Instead of this:
func NewUserService(
    db Database,
    cache Cache,
    logger Logger,
    emailService EmailService,
    smsService SMSService,
    config *Config,
) *UserService { }

// Use this:
type UserServiceParams struct {
    godi.In

    DB           Database
    Cache        Cache       `optional:"true"`
    Logger       Logger
    EmailService EmailService
    SMSService   SMSService  `optional:"true"`
    Config       *Config
}

func NewUserService(params UserServiceParams) *UserService {
    svc := &UserService{
        db:           params.DB,
        logger:       params.Logger,
        emailService: params.EmailService,
        config:       params.Config,
    }

    // Optional dependencies
    if params.Cache != nil {
        svc.cache = params.Cache
    }

    if params.SMSService != nil {
        svc.smsService = params.SMSService
    }

    return svc
}
```

### Output Parameters (godi.Out)

Register multiple services from one constructor:

```go
type RepositoryBundle struct {
    godi.Out

    UserRepo    UserRepository
    ProductRepo ProductRepository
    OrderRepo   OrderRepository   `name:"orders"`
}

func NewRepositories(db Database) RepositoryBundle {
    return RepositoryBundle{
        UserRepo:    &userRepository{db: db},
        ProductRepo: &productRepository{db: db},
        OrderRepo:   &orderRepository{db: db},
    }
}

// Register once, get three services!
var DataModule = godi.NewModule("data",
    godi.AddSingleton(NewDatabase),
    godi.AddScoped(NewRepositories),  // Registers all three repos
)
```

## Resource Disposal

Services that implement `Disposable` are automatically cleaned up:

```go
type Disposable interface {
    Close() error
}

// Example: Database connection
type DatabaseConnection struct {
    conn *sql.DB
}

func NewDatabaseConnection(config *Config) (*DatabaseConnection, error) {
    conn, err := sql.Open("postgres", config.DSN)
    if err != nil {
        return nil, err
    }
    return &DatabaseConnection{conn: conn}, nil
}

// Implement Disposable
func (db *DatabaseConnection) Close() error {
    if db.conn != nil {
        return db.conn.Close()
    }
    return nil
}

// Automatically closed when provider/scope closes!
```

### Disposal Order

Services are disposed in reverse order of creation:

```go
// Creation order:
// 1. Logger
// 2. Database
// 3. Cache
// 4. Service

// Disposal order (when scope closes):
// 1. Service
// 2. Cache
// 3. Database
// 4. Logger
```

### Scoped Disposal Example

```go
type Transaction struct {
    tx        *sql.Tx
    committed bool
}

func NewTransaction(db *Database) (*Transaction, error) {
    tx, err := db.Begin()
    if err != nil {
        return nil, err
    }
    return &Transaction{tx: tx}, nil
}

func (t *Transaction) Commit() error {
    t.committed = true
    return t.tx.Commit()
}

// Auto-rollback if not committed
func (t *Transaction) Close() error {
    if !t.committed && t.tx != nil {
        return t.tx.Rollback()
    }
    return nil
}

// Transaction automatically rolls back when scope closes!
```

## Register As Interface

Register concrete types as their interfaces:

```go
type Cache interface {
    Get(key string) (any, bool)
    Set(key string, value any)
}

type RedisCache struct {
    client *redis.Client
}

func NewRedisCache() *RedisCache {
    return &RedisCache{}
}

func (r *RedisCache) Get(key string) (any, bool) { /* ... */ }
func (r *RedisCache) Set(key string, value any) { /* ... */ }

// Register as interface
var CacheModule = godi.NewModule("cache",
    godi.AddSingleton(NewRedisCache, godi.As(new(Cache))),
)

// Resolve as interface
cache, _ := godi.Resolve[Cache](provider)  // Returns RedisCache as Cache
```

## Mixed Lifetime Groups

Groups can contain services with different lifetimes:

```go
var ProcessorModule = godi.NewModule("processors",
    // Different lifetimes in same group
    godi.AddSingleton(NewMetricsProcessor, godi.Group("processors")),
    godi.AddScoped(NewRequestProcessor, godi.Group("processors")),
    godi.AddTransient(NewTempProcessor, godi.Group("processors")),
)

// When resolved:
// - MetricsProcessor: same instance always (singleton)
// - RequestProcessor: same instance per scope (scoped)
// - TempProcessor: new instance every time (transient)
```

## Advanced Patterns

### Factory Pattern

```go
type ConnectionFactory struct {
    config *Config
}

func NewConnectionFactory(config *Config) *ConnectionFactory {
    return &ConnectionFactory{config: config}
}

func (f *ConnectionFactory) CreateConnection(database string) (Connection, error) {
    switch database {
    case "users":
        return NewConnection(f.config.UsersDB)
    case "orders":
        return NewConnection(f.config.OrdersDB)
    default:
        return nil, errors.New("unknown database")
    }
}
```

### Lazy Loading

```go
type LazyService struct {
    initOnce sync.Once
    provider godi.Provider
    service  *ExpensiveService
    err      error
}

func NewLazyService(provider godi.Provider) *LazyService {
    return &LazyService{provider: provider}
}

func (l *LazyService) Get() (*ExpensiveService, error) {
    l.initOnce.Do(func() {
        // Only create when first needed
        l.service, l.err = godi.Resolve[*ExpensiveService](l.provider)
    })
    return l.service, l.err
}
```

### Context Enrichment

```go
// Add request metadata to context
func EnrichContext(ctx context.Context, r *http.Request) context.Context {
    ctx = context.WithValue(ctx, "requestID", generateID())
    ctx = context.WithValue(ctx, "userID", getUserID(r))
    ctx = context.WithValue(ctx, "traceID", getTraceID(r))
    return ctx
}

// Services can access context values
func NewAuditLog(ctx context.Context) *AuditLog {
    return &AuditLog{
        requestID: ctx.Value("requestID").(string),
        userID:    ctx.Value("userID").(string),
        traceID:   ctx.Value("traceID").(string),
    }
}
```

## Performance Considerations

### Singleton vs Scoped

- **Singleton**: Created once at startup, fastest resolution
- **Scoped**: Created once per scope, cached within scope
- **Transient**: Created every time, slowest but ensures uniqueness

### Minimizing Allocations

```go
// Use pointer receivers for large structs
type LargeService struct {
    data [1000]byte
}

func (s *LargeService) DoWork() { }  // Pointer receiver

// Pool transient objects
var bufferPool = sync.Pool{
    New: func() any {
        return new(bytes.Buffer)
    },
}

func NewBuffer() *bytes.Buffer {
    return bufferPool.Get().(*bytes.Buffer)
}
```

## When to Use Advanced Features

- **Keyed Services**: Multiple implementations (databases, APIs)
- **Service Groups**: Plugin systems, middleware chains
- **Parameter Objects**: Constructors with 4+ parameters
- **Disposal**: Resources that need cleanup (files, connections)
- **As Interface**: Decouple from concrete types

## Summary

These advanced features are powerful but not always necessary. Start simple and add complexity only when you need it. The beauty of godi is that these features are there when you need them, but don't get in the way when you don't!
