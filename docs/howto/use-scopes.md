# Understanding Scoped Services

Scoped services are one of godi's most powerful features, but they can be confusing at first. Let's explore real scenarios where they shine.

## What Are Scoped Services?

Think of a scope as a "bubble" that exists for the duration of an operation:

- In a web app: one scope per HTTP request
- In a CLI tool: one scope per command execution
- In a background job: one scope per job

All scoped services within that bubble share the same instances.

## Real Example 1: Database Transactions

Here's why scoped services are perfect for database transactions:

```go
// Without scoped services - manual transaction passing 😱
func BadCreateOrder(db *sql.DB, userID string, items []Item) error {
    tx, err := db.Begin()
    if err != nil {
        return err
    }

    // Have to pass tx to EVERY function!
    user, err := getUserWithTx(tx, userID)
    if err != nil {
        tx.Rollback()
        return err
    }

    order, err := createOrderWithTx(tx, user)
    if err != nil {
        tx.Rollback()
        return err
    }

    err = updateInventoryWithTx(tx, items)
    if err != nil {
        tx.Rollback()
        return err
    }

    return tx.Commit()
}

// With scoped services - automatic transaction sharing! 🎉
type Transaction struct {
    tx *sql.Tx
}

func NewTransaction(db *Database) (*Transaction, error) {
    tx, err := db.Begin()
    return &Transaction{tx: tx}, err
}

func (t *Transaction) Close() error {
    if t.tx != nil {
        return t.tx.Rollback() // Rollback if not committed
    }
    return nil
}

type UserRepository struct {
    tx *Transaction // Injected automatically!
}

func NewUserRepository(tx *Transaction) *UserRepository {
    return &UserRepository{tx: tx}
}

type OrderService struct {
    userRepo *UserRepository
    tx       *Transaction
}

func NewOrderService(userRepo *UserRepository, tx *Transaction) *OrderService {
    return &OrderService{userRepo: userRepo, tx: tx}
}

func (s *OrderService) CreateOrder(userID string, items []Item) error {
    // No need to pass transaction - everyone in this scope shares it!
    user, err := s.userRepo.GetUser(userID)
    if err != nil {
        return err // Transaction auto-rollbacks when scope closes
    }

    // Create order...
    // Update inventory...

    return s.tx.Commit() // Explicitly commit
}

// Usage
func HandleCreateOrder(provider godi.ServiceProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Create scope for this request
        scope := provider.CreateScope(r.Context())
        defer scope.Close() // Auto-rollback if not committed!

        // Get service - transaction is automatically injected
        orderService, _ := godi.Resolve[*OrderService](scope)

        err := orderService.CreateOrder(userID, items)
        if err != nil {
            // Transaction automatically rolled back when scope closes
            http.Error(w, err.Error(), 500)
            return
        }

        // Success - transaction was committed
        w.WriteHeader(200)
    }
}
```

## Real Example 2: Request Context & User Info

Track user actions throughout a request without passing user info everywhere:

```go
// RequestContext holds info for current request
type RequestContext struct {
    RequestID string
    UserID    string
    UserEmail string
    StartTime time.Time
    TraceID   string
}

func NewRequestContext() *RequestContext {
    return &RequestContext{
        RequestID: uuid.NewString(),
        StartTime: time.Now(),
    }
}

// Logger that automatically includes request context
type RequestLogger struct {
    ctx    *RequestContext
    logger Logger
}

func NewRequestLogger(ctx *RequestContext, logger Logger) *RequestLogger {
    return &RequestLogger{ctx: ctx, logger: logger}
}

func (l *RequestLogger) Info(message string) {
    l.logger.Info(fmt.Sprintf("[ReqID: %s, User: %s] %s",
        l.ctx.RequestID, l.ctx.UserEmail, message))
}

// Services automatically get request-aware logger
type ProductService struct {
    logger *RequestLogger
    repo   *ProductRepository
}

func NewProductService(logger *RequestLogger, repo *ProductRepository) *ProductService {
    return &ProductService{logger: logger, repo: repo}
}

func (s *ProductService) GetProduct(id string) (*Product, error) {
    // This log automatically includes request ID and user!
    s.logger.Info(fmt.Sprintf("Getting product %s", id))
    return s.repo.FindByID(id)
}

// Middleware sets up the context
func AuthMiddleware(provider godi.ServiceProvider) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Create scope for request
            scope := provider.CreateScope(r.Context())
            defer scope.Close()

            // Get request context
            ctx, _ := godi.Resolve[*RequestContext](scope)

            // Populate from auth token
            token := r.Header.Get("Authorization")
            user := validateToken(token)
            ctx.UserID = user.ID
            ctx.UserEmail = user.Email

            // Now ALL services in this request automatically have user info!
            next.ServeHTTP(w, r)
        })
    }
}
```

## Real Example 3: Multi-Tenant Applications

Perfect for SaaS apps where each request belongs to a different tenant:

```go
// TenantContext holds current tenant info
type TenantContext struct {
    TenantID     string
    TenantName   string
    DatabaseName string
    Features     []string
}

func NewTenantContext() *TenantContext {
    return &TenantContext{}
}

// TenantRepository uses tenant-specific database
type TenantRepository struct {
    ctx *TenantContext
    db  *Database
}

func NewTenantRepository(ctx *TenantContext, db *Database) *TenantRepository {
    return &TenantRepository{ctx: ctx, db: db}
}

func (r *TenantRepository) GetConnection() *sql.DB {
    // Connect to tenant-specific database
    return r.db.GetConnection(r.ctx.DatabaseName)
}

// Feature flag service
type FeatureService struct {
    ctx *TenantContext
}

func NewFeatureService(ctx *TenantContext) *FeatureService {
    return &FeatureService{ctx: ctx}
}

func (s *FeatureService) IsEnabled(feature string) bool {
    // Check if tenant has this feature
    for _, f := range s.ctx.Features {
        if f == feature {
            return true
        }
    }
    return false
}

// Business logic automatically respects tenant context
type BillingService struct {
    repo     *TenantRepository
    features *FeatureService
    logger   *RequestLogger
}

func NewBillingService(repo *TenantRepository, features *FeatureService, logger *RequestLogger) *BillingService {
    return &BillingService{repo: repo, features: features, logger: logger}
}

func (s *BillingService) GenerateInvoice() (*Invoice, error) {
    if !s.features.IsEnabled("advanced-billing") {
        return nil, errors.New("Advanced billing not enabled for tenant")
    }

    // Automatically uses tenant-specific database!
    conn := s.repo.GetConnection()
    // Generate invoice...
}
```

## Real Example 4: Performance Monitoring

Track performance metrics for each request:

```go
// RequestMetrics collects stats for current request
type RequestMetrics struct {
    DatabaseQueries int
    CacheHits      int
    CacheMisses    int
    StartTime      time.Time
}

func NewRequestMetrics() *RequestMetrics {
    return &RequestMetrics{StartTime: time.Now()}
}

func (m *RequestMetrics) RecordQuery() {
    m.DatabaseQueries++
}

func (m *RequestMetrics) RecordCacheHit() {
    m.CacheHits++
}

func (m *RequestMetrics) RecordCacheMiss() {
    m.CacheMisses++
}

func (m *RequestMetrics) Duration() time.Duration {
    return time.Since(m.StartTime)
}

// Repository automatically tracks queries
type MetricsAwareRepository struct {
    db      *Database
    metrics *RequestMetrics
}

func NewMetricsAwareRepository(db *Database, metrics *RequestMetrics) *MetricsAwareRepository {
    return &MetricsAwareRepository{db: db, metrics: metrics}
}

func (r *MetricsAwareRepository) FindUser(id string) (*User, error) {
    r.metrics.RecordQuery() // Automatic tracking!
    return r.db.Query("SELECT * FROM users WHERE id = ?", id)
}

// At the end of request, report metrics
func MetricsMiddleware(provider godi.ServiceProvider) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            scope := provider.CreateScope(r.Context())
            defer func() {
                // Get metrics before scope closes
                metrics, _ := godi.Resolve[*RequestMetrics](scope)

                log.Printf("Request stats - Queries: %d, Cache hits: %d, Duration: %v",
                    metrics.DatabaseQueries,
                    metrics.CacheHits,
                    metrics.Duration())

                scope.Close()
            }()

            next.ServeHTTP(w, r)
        })
    }
}
```

## When to Use Each Lifetime

### Use Singleton When:

- Service has no state OR is thread-safe
- Expensive to create (database connections)
- Shared across entire application
- Examples: Loggers, Config, Connection Pools

### Use Scoped When:

- Service holds request-specific state
- Need isolation between operations
- Managing transactions or units of work
- Examples: Request Context, User Session, Transaction, Tenant Context

## Common Patterns

### Pattern 1: Request Pipeline

```go
services.AddScoped(NewRequestContext)     // Request info
services.AddScoped(NewTransaction)        // Database transaction
services.AddScoped(NewRequestLogger)      // Contextual logging
services.AddScoped(NewRequestMetrics)     // Performance tracking
services.AddScoped(NewTenantContext)      // Multi-tenancy
```

### Pattern 2: Shared State in Scope

```go
// All these share the same RequestContext within a scope
services.AddScoped(NewAuditService)       // Uses RequestContext
services.AddScoped(NewSecurityService)    // Uses RequestContext
services.AddScoped(NewNotificationService) // Uses RequestContext
```

### Pattern 3: Scope Hierarchies

```go
// HTTP Request Scope
//   └── Background Job Scope (triggered by request)
//       └── Batch Processing Scope (for each batch)

requestScope := provider.CreateScope(ctx)
jobScope := requestScope.CreateScope(ctx)
batchScope := jobScope.CreateScope(ctx)
```

## Summary

Scoped services solve real problems:

✅ **Automatic transaction management** - No more passing tx everywhere
✅ **Request context propagation** - User info available everywhere
✅ **Multi-tenancy isolation** - Each request in its own bubble
✅ **Performance tracking** - Metrics collected automatically
✅ **Clean code** - No manual wiring of request-specific data

The key insight: **Scoped services let you share state within an operation while keeping operations isolated from each other.**
