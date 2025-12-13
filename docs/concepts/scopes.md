# Scopes and Isolation

Scopes provide isolation between different execution contexts. In web applications, each HTTP request gets its own scope with isolated services.

## What is a Scope?

A scope is a container for scoped and transient services. When you resolve a scoped service within a scope, you get the same instance. Different scopes get different instances.

```
┌────────────────────────────────────────────────────────────────┐
│                          Provider                              │
│   ┌────────────────────────────────────────────────────────┐   │
│   │                    Singletons                          │   │
│   │   Logger, DatabasePool, Config (shared everywhere)     │   │
│   └────────────────────────────────────────────────────────┘   │
│                                                                │
│   ┌────────────────┐  ┌────────────────┐  ┌────────────────┐   │
│   │    Scope 1     │  │    Scope 2     │  │    Scope 3     │   │
│   │                │  │                │  │                │   │
│   │  RequestCtx A  │  │  RequestCtx B  │  │  RequestCtx C  │   │
│   │  UserSession A │  │  UserSession B │  │  UserSession C │   │
│   │  Transaction A │  │  Transaction B │  │  Transaction C │   │
│   │                │  │                │  │                │   │
│   │  (isolated)    │  │  (isolated)    │  │  (isolated)    │   │
│   └────────────────┘  └────────────────┘  └────────────────┘   │
└────────────────────────────────────────────────────────────────┘
```

## Creating Scopes

```go
// From a provider
scope, err := provider.CreateScope(ctx)
if err != nil {
    return err
}
defer scope.Close()

// Resolve services from the scope
userService := godi.MustResolve[*UserService](scope)
```

## Scope Behavior

### Scoped Services: Same Within Scope

```go
services.AddScoped(NewRequestContext)

scope, _ := provider.CreateScope(ctx)
defer scope.Close()

// Same instance within scope
ctx1 := godi.MustResolve[*RequestContext](scope)
ctx2 := godi.MustResolve[*RequestContext](scope)
// ctx1 == ctx2 ✓

// Different scope = different instance
scope2, _ := provider.CreateScope(ctx)
defer scope2.Close()
ctx3 := godi.MustResolve[*RequestContext](scope2)
// ctx1 == ctx3 ✗
```

### Singletons: Same Everywhere

```go
services.AddSingleton(NewLogger)

scope1, _ := provider.CreateScope(ctx)
scope2, _ := provider.CreateScope(ctx)

logger1 := godi.MustResolve[*Logger](scope1)
logger2 := godi.MustResolve[*Logger](scope2)
// logger1 == logger2 ✓ (singletons shared across scopes)
```

### Transients: Always New

```go
services.AddTransient(NewTempFile)

scope, _ := provider.CreateScope(ctx)

file1 := godi.MustResolve[*TempFile](scope)
file2 := godi.MustResolve[*TempFile](scope)
// file1 == file2 ✗ (new every time)
```

## Scopes in Web Applications

In web applications, create a scope per HTTP request:

```go
func Handler(provider godi.Provider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Create scope for this request
        scope, err := provider.CreateScope(r.Context())
        if err != nil {
            http.Error(w, "Internal Error", 500)
            return
        }
        defer scope.Close()

        // All services resolved here share the same scope
        userService := godi.MustResolve[*UserService](scope)
        authService := godi.MustResolve[*AuthService](scope)

        // Both services see the same RequestContext (if scoped)
        // ...
    }
}
```

## Context Integration

godi stores the scope in `context.Context`, making it accessible throughout your call stack:

```go
// Create scope with context
scope, _ := provider.CreateScope(r.Context())

// The scope is now in scope.Context()
ctx := scope.Context()

// Later, retrieve scope from context
scope, err := godi.FromContext(ctx)
if err != nil {
    // No scope in context
}
```

This is useful for deep call stacks:

```go
func HandleRequest(w http.ResponseWriter, r *http.Request) {
    scope, _ := provider.CreateScope(r.Context())
    defer scope.Close()

    // Pass the scope's context down
    processUser(scope.Context())
}

func processUser(ctx context.Context) {
    // Retrieve scope from context
    scope, _ := godi.FromContext(ctx)
    userService := godi.MustResolve[*UserService](scope)
    // ...
}
```

## Scope Cleanup

When a scope closes, all scoped and transient services created within it are disposed:

```go
type Transaction struct {
    db *sql.DB
    tx *sql.Tx
}

func (t *Transaction) Close() error {
    // Commit or rollback
    return t.tx.Commit()
}

services.AddScoped(NewTransaction)

scope, _ := provider.CreateScope(ctx)
tx := godi.MustResolve[*Transaction](scope)

// ... do work ...

scope.Close() // Transaction.Close() called automatically
```

Disposal order is reverse creation order:

```
Created:  A → B → C
Disposed: C → B → A
```

## Framework Integration

godi's framework integrations handle scope creation automatically:

```go
// Gin
g := gin.New()
g.Use(godigin.ScopeMiddleware(provider))

// Chi
r := chi.NewRouter()
r.Use(godichi.ScopeMiddleware(provider))

// Echo
e := echo.New()
e.Use(godiecho.ScopeMiddleware(provider))

// Fiber
app := fiber.New()
app.Use(godifiber.ScopeMiddleware(provider))

// net/http
handler := godihttp.ScopeMiddleware(provider)(mux)
```

Each middleware:

1. Creates a scope when request starts
2. Attaches scope to request context
3. Closes scope when request ends

## Advanced: Nested Scopes

Scopes can be nested for complex scenarios:

```go
scope1, _ := provider.CreateScope(ctx)
defer scope1.Close()

// Nested scope
scope2, _ := scope1.CreateScope(context.Background())
defer scope2.Close()

// scope2 can access singletons from provider
// scope2 has its own scoped service instances
```

## Common Patterns

### Request-Per-Scope

```go
type RequestContext struct {
    ID        string
    UserID    string
    StartTime time.Time
}

func NewRequestContext() *RequestContext {
    return &RequestContext{
        ID:        uuid.New().String(),
        StartTime: time.Now(),
    }
}

// All services in the request share the same RequestContext
services.AddScoped(NewRequestContext)
services.AddScoped(NewUserService)    // Uses RequestContext
services.AddScoped(NewOrderService)   // Uses same RequestContext
```

### Database Transaction Per Request

```go
type Transaction struct {
    db *sql.DB
    tx *sql.Tx
}

func NewTransaction(pool *DatabasePool) (*Transaction, error) {
    tx, err := pool.Begin()
    if err != nil {
        return nil, err
    }
    return &Transaction{db: pool.DB(), tx: tx}, nil
}

func (t *Transaction) Close() error {
    return t.tx.Commit()
}

services.AddSingleton(NewDatabasePool)  // Shared pool
services.AddScoped(NewTransaction)       // Per-request transaction
services.AddScoped(NewUserRepository)    // Uses transaction
services.AddScoped(NewOrderRepository)   // Uses same transaction
```

---

**Next:** Learn about [organizing with modules](modules.md)
