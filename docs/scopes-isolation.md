# Scopes & Isolation

Scopes provide isolated contexts for service resolution, perfect for handling concurrent requests in web applications.

## Understanding Scopes

### What is a Scope?

A scope is an isolated container for scoped services. Each scope:

- Has its own instances of scoped services
- Shares singleton instances with all other scopes
- Can create child scopes for hierarchical isolation
- Automatically cleans up resources when closed

```go
// Create a scope
scope, err := provider.CreateScope(context.Background())
defer scope.Close() // Always close scopes

// Scoped services are isolated
service := godi.MustResolve[MyService](scope)
```

### Scope Hierarchy

```go
// Root provider (contains singletons)
provider, _ := services.Build()

// Parent scope
parentScope, _ := provider.CreateScope(context.Background())
defer parentScope.Close()

// Child scope
childScope, _ := parentScope.CreateScope(context.Background())
defer childScope.Close()

// Service resolution hierarchy:
// 1. Check child scope cache
// 2. Check parent scope cache
// 3. Check provider (singletons)
// 4. Create new instance
```

## Web Application Patterns

### HTTP Request Isolation

Each HTTP request gets its own scope:

```go
func ScopeMiddleware(provider godi.Provider) http.HandlerFunc {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Create scope for this request
            scope, _ := provider.CreateScope(r.Context())
            defer scope.Close()

            // Add scope to context
            ctx := scope.Context()
            r = r.WithContext(ctx)

            next.ServeHTTP(w, r)
        })
    }
}
```

### Request Context Pattern

Track request-specific data:

```go
type RequestContext interface {
    RequestID() string
    UserID() string
    StartTime() time.Time
}

type requestContext struct {
    requestID string
    userID    string
    startTime time.Time
}

func NewRequestContext() RequestContext {
    return &requestContext{
        requestID: uuid.New().String(),
        startTime: time.Now(),
    }
}

// Register as scoped
services.AddScoped(NewRequestContext)

// Each request gets its own context
func Handler(w http.ResponseWriter, r *http.Request) {
    scope, _ := godi.FromContext(r.Context())
    ctx := godi.MustResolve[RequestContext](scope)

    log.Printf("Request %s started at %v",
        ctx.RequestID(), ctx.StartTime())
}
```

## Concurrent Request Handling

### Isolation in Action

```go
func DemoHandler(provider godi.Provider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Each request gets isolated scope
        scope, _ := provider.CreateScope(r.Context())
        defer scope.Close()

        // Get request-specific context
        ctx := godi.MustResolve[RequestContext](scope)

        // Get shared singleton
        logger := godi.MustResolve[Logger](scope)

        // Log with request context
        logger.Info("Processing request", "id", ctx.RequestID())

        // Services in this scope see the same context
        userService := godi.MustResolve[UserService](scope)
        orderService := godi.MustResolve[OrderService](scope)

        // Both services work with same request context
        user := userService.GetCurrentUser()   // Uses ctx
        orders := orderService.GetUserOrders() // Also uses ctx
    }
}
```

### Thread Safety

Scopes are thread-safe for concurrent requests:

```go
func TestConcurrentRequests(t *testing.T) {
    provider, _ := setupProvider()

    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()

            // Each goroutine gets its own scope
            scope, _ := provider.CreateScope(context.Background())
            defer scope.Close()

            // Isolated request context
            ctx := godi.MustResolve[RequestContext](scope)

            // Process independently
            processRequest(scope, id)
        }(i)
    }
    wg.Wait()
}
```

## Context Integration

### Passing Scopes Through Context

```go
// Store scope in context
func WithScope(ctx context.Context, scope godi.Scope) context.Context {
    return context.WithValue(ctx, scopeKey{}, scope)
}

// Retrieve scope from context
func GetScope(ctx context.Context) (godi.Scope, error) {
    return godi.FromContext(ctx)
}

// Use in deep call stacks
func DeepBusinessLogic(ctx context.Context) error {
    scope, err := godi.FromContext(ctx)
    if err != nil {
        return err
    }

    service := godi.MustResolve[MyService](scope)
    return service.DoWork()
}
```

### Context Cancellation

Scopes respect context cancellation:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

scope, _ := provider.CreateScope(ctx)
// Scope automatically closes when context is cancelled

go func() {
    <-ctx.Done()
    // Scope is automatically closed
}()
```

## Database Transactions

### Transaction per Scope Pattern

```go
type Transaction interface {
    Commit() error
    Rollback() error
    Exec(query string, args ...any) error
}

type transaction struct {
    tx *sql.Tx
}

func NewTransaction(db Database) (Transaction, error) {
    tx, err := db.Begin()
    if err != nil {
        return nil, err
    }
    return &transaction{tx: tx}, nil
}

func (t *transaction) Close() error {
    // Rollback if not committed
    return t.tx.Rollback()
}

// Register as scoped
services.AddScoped(NewTransaction)

// Use in handler
func CreateUserHandler(w http.ResponseWriter, r *http.Request) {
    scope, _ := godi.FromContext(r.Context())
    tx := godi.MustResolve[Transaction](scope)

    // All services in scope use same transaction
    userRepo := godi.MustResolve[UserRepository](scope)
    err := userRepo.Create(user) // Uses tx

    if err != nil {
        tx.Rollback()
        return
    }

    tx.Commit()
}
```

## Unit of Work Pattern

```go
type UnitOfWork interface {
    UserRepository() UserRepository
    OrderRepository() OrderRepository
    Complete() error
}

type unitOfWork struct {
    tx       *sql.Tx
    userRepo UserRepository
    orderRepo OrderRepository
}

func NewUnitOfWork(db Database) (UnitOfWork, error) {
    tx, err := db.Begin()
    if err != nil {
        return nil, err
    }

    return &unitOfWork{
        tx:        tx,
        userRepo:  NewUserRepository(tx),
        orderRepo: NewOrderRepository(tx),
    }, nil
}

func (u *unitOfWork) Complete() error {
    return u.tx.Commit()
}

func (u *unitOfWork) Close() error {
    return u.tx.Rollback()
}

// Register as scoped
services.AddScoped(NewUnitOfWork)

// Use in business logic
func ProcessOrder(scope godi.Scope) error {
    uow := godi.MustResolve[UnitOfWork](scope)

    user, _ := uow.UserRepository().GetByID(userID)
    order, _ := uow.OrderRepository().Create(orderData)

    return uow.Complete() // Commit all changes
}
```

## Resource Cleanup

### Automatic Disposal

Resources are automatically cleaned up when scope closes:

```go
type TempFileManager struct {
    files []string
}

func NewTempFileManager() *TempFileManager {
    return &TempFileManager{
        files: make([]string, 0),
    }
}

func (t *TempFileManager) CreateTempFile() string {
    file, _ := os.CreateTemp("", "temp")
    t.files = append(t.files, file.Name())
    return file.Name()
}

func (t *TempFileManager) Close() error {
    // Clean up all temp files
    for _, file := range t.files {
        os.Remove(file)
    }
    return nil
}

// Automatic cleanup
func Handler(scope godi.Scope) {
    manager := godi.MustResolve[*TempFileManager](scope)

    tempFile := manager.CreateTempFile()
    // Use temp file...

    // When scope closes, all temp files are deleted
}
```

### Cleanup Order

Resources are disposed in reverse order of creation:

```go
// Creation order: A -> B -> C
// Disposal order: C -> B -> A

scope, _ := provider.CreateScope(ctx)
a := godi.MustResolve[ServiceA](scope) // Created first
b := godi.MustResolve[ServiceB](scope) // Created second
c := godi.MustResolve[ServiceC](scope) // Created third

scope.Close()
// Closes in order: C, B, A
```

## Testing with Scopes

### Isolated Test Cases

```go
func TestUserService(t *testing.T) {
    provider := setupTestProvider()

    t.Run("create user", func(t *testing.T) {
        // Isolated scope for this test
        scope, _ := provider.CreateScope(context.Background())
        defer scope.Close()

        service := godi.MustResolve[UserService](scope)
        user, err := service.CreateUser("test@example.com")
        assert.NoError(t, err)
    })

    t.Run("delete user", func(t *testing.T) {
        // Different scope - isolated from previous test
        scope, _ := provider.CreateScope(context.Background())
        defer scope.Close()

        service := godi.MustResolve[UserService](scope)
        err := service.DeleteUser("123")
        assert.NoError(t, err)
    })
}
```

### Mock Injection

```go
func TestWithMocks(t *testing.T) {
    services := godi.NewCollection()

    // Register mocks as scoped
    services.AddScoped(func() Database {
        return &MockDatabase{
            users: make(map[string]*User),
        }
    })

    provider, _ := services.Build()

    // Each test gets fresh mocks
    scope, _ := provider.CreateScope(context.Background())
    defer scope.Close()

    db := godi.MustResolve[Database](scope)
    // db is a fresh mock for this test
}
```

## Best Practices

1. **Always close scopes** - Use `defer scope.Close()`
2. **One scope per request** - Isolate concurrent requests
3. **Pass through context** - Use context for deep call stacks
4. **Implement Disposable** - For automatic cleanup
5. **Test with scopes** - Isolate test cases
6. **Avoid scope in singletons** - Singletons shouldn't hold scopes

## Common Pitfalls

### Forgetting to Close Scopes

```go
// ❌ Memory leak!
func BadHandler(provider godi.Provider) {
    scope, _ := provider.CreateScope(context.Background())
    service := godi.MustResolve[MyService](scope)
    // Scope never closed - resources leak!
}

// ✅ Always close
func GoodHandler(provider godi.Provider) {
    scope, _ := provider.CreateScope(context.Background())
    defer scope.Close() // Always do this
    service := godi.MustResolve[MyService](scope)
}
```

### Storing Scopes in Singletons

```go
// ❌ Don't store scopes in singletons
type BadCache struct {
    scope godi.Scope // Wrong!
}

// ✅ Store provider instead
type GoodCache struct {
    provider godi.Provider // Correct
}

func (c *GoodCache) GetItem(ctx context.Context, key string) any {
    scope, _ := godi.FromContext(ctx)
    // Use scope from context
}
```

## Next Steps

- Learn about [Resource Management](resource-management.md)
- Explore [Modules](modules.md)
- Understand [Keyed Services](keyed-services.md)
