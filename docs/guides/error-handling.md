# Debugging godi Errors

When something goes wrong, godi provides detailed error messages to help you fix the issue. This guide covers common errors and how to resolve them.

## Build-Time Errors

These errors occur when calling `services.Build()`.

### Circular Dependency Detected

```
Error: circular dependency detected: *UserService -> *AuthService -> *UserService
```

**What it means:** Service A needs B, but B needs A (directly or indirectly).

**How to fix:**

1. **Identify the cycle** - The error message shows the dependency chain
2. **Break the cycle** with one of these approaches:

```go
// Problem: circular dependency
type UserService struct {
    auth *AuthService
}
type AuthService struct {
    users *UserService  // Cycle!
}

// Solution 1: Use interface
type UserProvider interface {
    GetUser(id int) *User
}

type AuthService struct {
    users UserProvider  // Interface breaks the cycle
}

// Solution 2: Restructure - extract shared functionality
type TokenValidator struct{}

type UserService struct {
    validator *TokenValidator
}
type AuthService struct {
    validator *TokenValidator  // Both depend on shared service
}

// Solution 3: Method injection instead of constructor
type AuthService struct{}

func (a *AuthService) ValidateWithUser(users *UserService, token string) bool {
    // Pass UserService when needed, not at construction
}
```

### Missing Dependency

```
Error: no registration found for type *DatabasePool required by *UserRepository
```

**What it means:** A constructor needs a type that wasn't registered.

**How to fix:**

```go
// Problem: forgot to register DatabasePool
services.AddScoped(NewUserRepository)  // Needs *DatabasePool

// Solution: register the missing dependency
services.AddSingleton(NewDatabasePool)
services.AddScoped(NewUserRepository)
```

### Lifetime Conflict

```
Error: singleton *Cache cannot depend on scoped *RequestContext
```

**What it means:** A longer-lived service depends on a shorter-lived one.

**How to fix:**

```go
// Problem: singleton holding scoped reference
services.AddScoped(NewRequestContext)
services.AddSingleton(func(ctx *RequestContext) *Cache {
    return &Cache{ctx: ctx}  // Error!
})

// Solution 1: Make Cache scoped too
services.AddScoped(func(ctx *RequestContext) *Cache {
    return &Cache{ctx: ctx}
})

// Solution 2: Remove the dependency
services.AddSingleton(func() *Cache {
    return &Cache{}  // Don't need RequestContext
})

// Solution 3: Access context through scope at runtime
type Cache struct {
    provider godi.Provider
}

func (c *Cache) DoSomething(ctx context.Context) {
    scope, _ := godi.FromContext(ctx)
    reqCtx := godi.MustResolve[*RequestContext](scope)
    // Use reqCtx
}
```

### Constructor Error

```
Error: failed to create *Database: connection refused
```

**What it means:** A constructor returned an error.

**How to fix:**

```go
// Constructors can return errors
func NewDatabase(cfg *Config) (*Database, error) {
    db, err := sql.Open("postgres", cfg.URL)
    if err != nil {
        return nil, err  // This error bubbles up
    }
    return &Database{db}, nil
}

// Fix the underlying issue (database not running, wrong URL, etc.)
// Or add better error handling:
func NewDatabase(cfg *Config) (*Database, error) {
    db, err := sql.Open("postgres", cfg.URL)
    if err != nil {
        return nil, fmt.Errorf("failed to connect to database at %s: %w",
            cfg.URL, err)
    }
    return &Database{db}, nil
}
```

## Runtime Errors

These errors occur when resolving services.

### Service Not Found

```
Error: no registration found for type *UnknownService
```

**What it means:** You're trying to resolve a type that wasn't registered.

**How to fix:**

```go
// Check your registration
services.AddScoped(NewUserService)  // Registers *UserService

// Make sure you're resolving the right type
user := godi.MustResolve[*UserService](provider)  // Correct
user := godi.MustResolve[UserService](provider)   // Wrong! (no pointer)
```

### Scope Disposed

```
Error: scope has been disposed
```

**What it means:** You're trying to use a scope after calling `Close()`.

**How to fix:**

```go
// Problem: using scope after close
scope, _ := provider.CreateScope(ctx)
scope.Close()
service := godi.MustResolve[*UserService](scope)  // Error!

// Solution: keep scope open while using it
scope, _ := provider.CreateScope(ctx)
defer scope.Close()  // Close AFTER you're done
service := godi.MustResolve[*UserService](scope)
service.DoWork()
```

### No Scope in Context

```
Error: no scope found in context
```

**What it means:** You're calling `godi.FromContext` but no scope was attached.

**How to fix:**

```go
// Problem: no scope middleware
mux.HandleFunc("/users", godihttp.Handle((*UserController).List))
// No ScopeMiddleware wrapping!

// Solution: wrap with scope middleware
handler := godihttp.ScopeMiddleware(provider)(mux)

// Or create scope manually in handler
func handler(w http.ResponseWriter, r *http.Request) {
    scope, err := provider.CreateScope(r.Context())
    if err != nil {
        http.Error(w, "Internal Error", 500)
        return
    }
    defer scope.Close()

    // Attach to context if needed downstream
    r = r.WithContext(scope.Context())
    // ...
}
```

## Debugging Tips

### 1. Use `Resolve` Instead of `MustResolve`

```go
// MustResolve panics on error
service := godi.MustResolve[*UserService](provider)

// Resolve returns error for inspection
service, err := godi.Resolve[*UserService](provider)
if err != nil {
    log.Printf("Resolution failed: %v", err)
    // Inspect error type
    var notFound *godi.ServiceNotFoundError
    if errors.As(err, &notFound) {
        log.Printf("Missing service: %s", notFound.ServiceType)
    }
}
```

### 2. Check Registrations

```go
// Before build, verify what's registered
services := godi.NewCollection()
services.AddSingleton(NewLogger)
services.AddScoped(NewUserService)

// Build with error handling
provider, err := services.Build()
if err != nil {
    // The error message lists the problem
    log.Fatalf("Build failed: %v", err)
}
```

### 3. Validate Dependencies Early

```go
// In main(), build provider early to catch issues at startup
func main() {
    services := godi.NewCollection()
    // ... register services ...

    provider, err := services.Build()
    if err != nil {
        log.Fatalf("DI setup failed: %v", err)
    }
    defer provider.Close()

    // Application only starts if DI is valid
    runServer(provider)
}
```

### 4. Log Resolution for Debugging

```go
// Add logging middleware
handler := godihttp.ScopeMiddleware(provider,
    godihttp.WithMiddleware(func(scope godi.Scope, r *http.Request) error {
        log.Printf("Scope created for %s %s", r.Method, r.URL.Path)
        return nil
    }),
)(mux)
```

## Common Mistakes

### Wrong Type in Generic Parameter

```go
// Interface vs concrete type
services.AddSingleton(func() Logger { return &consoleLogger{} })

godi.MustResolve[Logger](provider)        // Correct
godi.MustResolve[*consoleLogger](provider) // Error: not registered

// Pointer vs value
services.AddSingleton(func() *UserService { ... })

godi.MustResolve[*UserService](provider)  // Correct
godi.MustResolve[UserService](provider)   // Error: no pointer
```

### Forgetting to Close Scopes

```go
// Memory leak: scope never closed
func handler(provider godi.Provider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        scope, _ := provider.CreateScope(r.Context())
        // Missing: defer scope.Close()

        service := godi.MustResolve[*UserService](scope)
        service.Handle(w, r)
        // Scope resources leak!
    }
}
```

### Registering Instance Instead of Constructor

```go
// Wrong: registering an instance
logger := NewLogger()
services.AddSingleton(func() *Logger { return logger })
// This works but defeats the purpose - dependencies aren't injected

// Right: register the constructor
services.AddSingleton(NewLogger)
```

---

**Next:** See the [migration guide](migration.md) for moving from other DI libraries
