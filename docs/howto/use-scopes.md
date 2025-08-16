# Using Scopes

Scopes are essential for web applications. They isolate each request, ensuring data doesn't leak between users.

## What are Scopes?

A scope is a boundary for service instances. Services with `Scoped` lifetime get new instances per scope.

```go
// One database connection for the app
godi.AddSingleton(NewDatabase)

// New transaction per request
godi.AddScoped(NewTransaction)
```

## Basic Usage

### Creating and Using Scopes

```go
func HandleRequest(provider godi.ServiceProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Create scope for this request
        scope := provider.CreateScope(r.Context())
        defer scope.Close() // Always clean up!

        // Get services for this request
        service, err := godi.Resolve[UserService](scope)
        if err != nil {
            http.Error(w, "Service error", 500)
            return
        }

        // Use the service
        service.ProcessRequest()
    }
}
```

## Real-World Example: Web API

Here's a complete example showing scopes in action:

```go
// Models
type RequestContext struct {
    RequestID string
    UserID    string
    StartTime time.Time
}

// Scoped service - new instance per request
func NewRequestContext(ctx context.Context) *RequestContext {
    return &RequestContext{
        RequestID: ctx.Value("requestID").(string),
        UserID:    ctx.Value("userID").(string),
        StartTime: time.Now(),
    }
}

// Transaction - also scoped
type Transaction struct {
    tx *sql.Tx
    ctx *RequestContext
}

func NewTransaction(db *Database, ctx *RequestContext) (*Transaction, error) {
    tx, err := db.Begin()
    if err != nil {
        return nil, err
    }

    return &Transaction{
        tx: tx,
        ctx: ctx,
    }, nil
}

func (t *Transaction) Commit() error {
    duration := time.Since(t.ctx.StartTime)
    log.Printf("[%s] Transaction committed after %v", t.ctx.RequestID, duration)
    return t.tx.Commit()
}

// Repository using the transaction
type UserRepository struct {
    tx *Transaction
}

func NewUserRepository(tx *Transaction) *UserRepository {
    return &UserRepository{tx: tx}
}

func (r *UserRepository) SaveUser(user *User) error {
    log.Printf("[%s] Saving user %s", r.tx.ctx.RequestID, user.ID)
    // Use transaction for this request
    _, err := r.tx.tx.Exec("INSERT INTO users ...")
    return err
}
```

### Module Setup

```go
var WebModule = godi.NewModule("web",
    // Singleton - shared
    godi.AddSingleton(NewDatabase),
    godi.AddSingleton(NewLogger),

    // Scoped - per request
    godi.AddScoped(NewRequestContext),
    godi.AddScoped(NewTransaction),
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewUserService),
)
```

### HTTP Handler

```go
func CreateUserHandler(provider godi.ServiceProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Add request data to context
        ctx := context.WithValue(r.Context(), "requestID", generateID())
        ctx = context.WithValue(ctx, "userID", getUserID(r))

        // Create scope with context
        scope := provider.CreateScope(ctx)
        defer scope.Close()

        // Get service - it has access to request context!
        service, err := godi.Resolve[*UserService](scope)
        if err != nil {
            http.Error(w, "Service error", 500)
            return
        }

        // Process request
        var req CreateUserRequest
        json.NewDecoder(r.Body).Decode(&req)

        user, err := service.CreateUser(req)
        if err != nil {
            http.Error(w, err.Error(), 400)
            return
        }

        json.NewEncoder(w).Encode(user)

        // When scope closes:
        // 1. Transaction commits/rollbacks
        // 2. Resources are cleaned up
        // 3. Metrics are recorded
    }
}
```

## Automatic Disposal

When a scope closes, it automatically disposes services that implement these interfaces:

```go
// Simple disposal
type Disposable interface {
    Close() error
}

// Disposal with context (for graceful shutdown)
type DisposableWithContext interface {
    Close(ctx context.Context) error
}
```

### Disposal Example

```go
// File handler that needs cleanup
type FileProcessor struct {
    file *os.File
    ctx  *RequestContext
}

func NewFileProcessor(ctx *RequestContext) (*FileProcessor, error) {
    file, err := os.Create(fmt.Sprintf("upload_%s.tmp", ctx.RequestID))
    if err != nil {
        return nil, err
    }

    return &FileProcessor{
        file: file,
        ctx:  ctx,
    }, nil
}

// Implements Disposable
func (f *FileProcessor) Close() error {
    log.Printf("[%s] Cleaning up file", f.ctx.RequestID)
    f.file.Close()
    return os.Remove(f.file.Name())
}

// Database transaction with disposal
type Transaction struct {
    tx        *sql.Tx
    committed bool
}

func (t *Transaction) Close() error {
    if !t.committed {
        return t.tx.Rollback() // Auto-rollback if not committed
    }
    return nil
}

func (t *Transaction) Commit() error {
    t.committed = true
    return t.tx.Commit()
}
```

### Disposal Order

Services are disposed in reverse order of creation (LIFO):

```go
// Creation order:
// 1. Logger (singleton - not disposed with scope)
// 2. Database connection (scoped)
// 3. Transaction (scoped)
// 4. Repository (scoped)
// 5. Service (scoped)

scope.Close()

// Disposal order:
// 1. Service
// 2. Repository
// 3. Transaction (auto-rollback if not committed)
// 4. Database connection
// (Logger remains - disposed with provider)
```

### Context-Aware Disposal

For graceful shutdown with timeouts:

```go
type GracefulService struct {
    workers []*Worker
}

func (s *GracefulService) Close(ctx context.Context) error {
    log.Println("Starting graceful shutdown...")

    done := make(chan error, 1)
    go func() {
        // Stop all workers
        for _, w := range s.workers {
            w.Stop()
        }
        done <- nil
    }()

    select {
    case err := <-done:
        log.Println("Graceful shutdown complete")
        return err
    case <-ctx.Done():
        log.Println("Shutdown timeout - forcing close")
        return ctx.Err()
    }
}
```

## Scope Isolation

Each scope has its own instances:

```go
func TestScopeIsolation(t *testing.T) {
    module := godi.NewModule("test",
        godi.AddScoped(func() *Counter {
            return &Counter{value: 0}
        }),
    )

    services := godi.NewCollection()
    services.AddModules(module)
    provider, _ := services.Build()

    // Request 1
    scope1 := provider.CreateScope(context.Background())
    counter1, _ := godi.Resolve[*Counter](scope1.ServiceProvider())
    counter1.Increment() // value = 1

    // Request 2 - different instance!
    scope2 := provider.CreateScope(context.Background())
    counter2, _ := godi.Resolve[*Counter](scope2.ServiceProvider())
    counter2.Increment() // value = 1 (not 2!)

    // Different instances
    assert.NotSame(t, counter1, counter2)

    scope1.Close()
    scope2.Close()
}
```

## Advanced Patterns

### Middleware with Scopes

```go
func DIMiddleware(provider godi.ServiceProvider) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Create scope
            scope := provider.CreateScope(r.Context())
            defer scope.Close()

            // Add scope to request context
            ctx := context.WithValue(r.Context(), "scope", scope)
            r = r.WithContext(ctx)

            next.ServeHTTP(w, r)
        })
    }
}

// In handlers
func MyHandler(w http.ResponseWriter, r *http.Request) {
    scope := r.Context().Value("scope").(godi.Scope)
    service, _ := godi.Resolve[MyService](scope)
    // Use service...
}
```

### Unit of Work Pattern

```go
type UnitOfWork struct {
    tx         *sql.Tx
    committed  bool
    repositories map[string]any
}

func NewUnitOfWork(db *Database) (*UnitOfWork, error) {
    tx, err := db.Begin()
    if err != nil {
        return nil, err
    }

    return &UnitOfWork{
        tx: tx,
        repositories: make(map[string]any),
    }, nil
}

func (u *UnitOfWork) UserRepository() *UserRepository {
    if repo, ok := u.repositories["user"]; ok {
        return repo.(*UserRepository)
    }

    repo := &UserRepository{tx: u.tx}
    u.repositories["user"] = repo
    return repo
}

func (u *UnitOfWork) Commit() error {
    u.committed = true
    return u.tx.Commit()
}

// Automatic disposal
func (u *UnitOfWork) Close() error {
    if !u.committed {
        return u.tx.Rollback()
    }
    return nil
}
```

## Best Practices

### 1. Always Close Scopes

```go
// ✅ Good
scope := provider.CreateScope(ctx)
defer scope.Close()

// ❌ Bad
scope := provider.CreateScope(ctx)
// Missing close - memory leak!
```

### 2. One Scope Per Request

```go
// ✅ Good - one scope per HTTP request
func Handler(provider godi.ServiceProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        scope := provider.CreateScope(r.Context())
        defer scope.Close()
        // Handle entire request with this scope
    }
}

// ❌ Bad - multiple scopes per request
func BadHandler(provider godi.ServiceProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Don't create multiple scopes!
        scope1 := provider.CreateScope(r.Context())
        service1, _ := godi.Resolve[Service1](scope1.ServiceProvider())
        scope1.Close()

        scope2 := provider.CreateScope(r.Context())
        service2, _ := godi.Resolve[Service2](scope2.ServiceProvider())
        scope2.Close()
    }
}
```

### 3. Pass Context Through Scopes

```go
// Add request metadata
ctx := context.WithValue(r.Context(), "requestID", uuid.New())
ctx = context.WithValue(ctx, "userID", getUserID(r))

// Create scope with enriched context
scope := provider.CreateScope(ctx)

// Services can access context
func NewAuditLogger(ctx context.Context) *AuditLogger {
    return &AuditLogger{
        requestID: ctx.Value("requestID").(string),
        userID:    ctx.Value("userID").(string),
    }
}
```

### 4. Implement Disposal for Resources

```go
// ✅ Good - cleanup resources
type ResourceManager struct {
    resources []io.Closer
}

func (r *ResourceManager) Close() error {
    var errs []error
    for _, res := range r.resources {
        if err := res.Close(); err != nil {
            errs = append(errs, err)
        }
    }
    return errors.Join(errs...)
}

// ❌ Bad - leaking resources
type BadService struct {
    file *os.File
    conn net.Conn
    // No Close method - resources leak!
}
```

## Common Use Cases

### 1. Database Transactions

Each request gets its own transaction that commits/rollbacks with the scope.

### 2. Request Logging

Track all operations within a request with consistent request ID.

### 3. User Context

Ensure user permissions and identity are consistent throughout request.

### 4. Resource Cleanup

Automatically close files, connections, or other resources when request ends.

### 5. Metrics Collection

Measure request duration and collect metrics when scope closes.

## Summary

Scopes are powerful for:

- **Isolating requests** - Each user gets their own instances
- **Managing transactions** - Automatic commit/rollback
- **Resource cleanup** - Guaranteed disposal
- **Request context** - Consistent data throughout request

Remember: Always create a scope for each operation and close it when done!
