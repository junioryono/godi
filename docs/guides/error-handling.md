# Debugging godi Errors

When something goes wrong, godi provides detailed error messages to help you fix the issue. This guide covers common errors and how to resolve them.

## Build-Time Errors

These errors occur when calling `services.Build()`. `Build` wraps each one in a
`build failed during <phase> phase: ...` prefix; the samples below show the full
message for a program whose types live in package `main`. Each message is one
line; `godi.Explain(err)` adds the detail shown under some samples (see
[Get the Full Explanation](#get-the-full-explanation)).

### Registration Errors

The `Add*` methods do not return errors. Invalid registrations (nil
constructors, duplicates, bad tags, ...) are recorded and reported all at
once when you call `Build()`:

```
build failed during registration phase: one or more service registrations failed: constructor cannot be nil
```

Register everything first, then handle the single error from `Build()`. Use
`Collection.Err()` if you need to inspect recorded errors before building.

### Circular Dependency Detected

```
build failed during validation phase: dependency graph validation failed: circular dependency detected: *main.UserService -> *main.AuthService -> *main.UserService
```

`godi.Explain(err)` draws the cycle and suggests fixes:

```
    *main.UserService
      ↓
    *main.AuthService
      ↓
    *main.UserService (cycle)

To resolve this:
  • Use an interface to break the dependency
  • Use a factory function for lazy initialization
  • Restructure to remove the circular relationship
```

The cycle can be reported starting from any service in it. Match it with
`errors.As(err, &cycleErr)` for a `*godi.CircularDependencyError`; its `Path`
field holds the chain of services.

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
build failed during validation phase: missing dependencies: *main.UserRepository requires *main.DatabasePool (not registered) [constructor main.NewUserRepository (repository.go:12)]
```

**What it means:** A constructor needs a type that wasn't registered.

**When it surfaces:** at `Build()`, for every lifetime. `Build` checks each
constructor's dependencies and reports all missing ones together as
`*godi.MissingDependencyError` values (`errors.Is(err, godi.ErrServiceNotFound)`
matches). Optional (`optional:"true"`) and group dependencies are exempt: they
may legitimately be absent or empty.

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
build failed during validation phase: lifetime validation failed: lifetime conflict: *main.Cache (Singleton) cannot depend on *main.RequestContext (Scoped)
```

`godi.Explain(err)` adds:

```
Singleton services are created once and live for the application lifetime.
Scoped services are created per-scope and may have different values in different scopes.
A singleton depending on a scoped service would capture a single scope's value,
which is almost certainly not what you want.

To resolve this:
  • Change *main.Cache to Scoped lifetime
  • Change *main.RequestContext to Singleton lifetime
  • Pass *main.RequestContext to *main.Cache's methods per call instead of holding it
```

The typed error is `*godi.LifetimeConflictError`.

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

For a singleton, the constructor runs during `Build`:

```
build failed during singleton-creation phase: failed to initialize singletons: failed to resolve *main.Database: constructor main.NewDatabase (database.go:20) failed: connection refused
```

For a scoped or transient service, it runs at resolution, and the error is
returned by `Resolve` (a `*godi.ConstructorInvocationError`; it does not match
`godi.ErrServiceNotFound`):

```
constructor main.NewDatabase (database.go:20) failed: connection refused
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
service not found: *main.UnknownService
```

**What it means:** You're trying to resolve a type that wasn't registered.
(A registered service whose constructor failed reports the constructor's
error instead, as shown under Constructor Error above.)

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
scope has been disposed
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

### Circular Resolution at Runtime

```
constructor main.NewService (service.go:15) failed: circular dependency detected: *main.Service -> *main.Service
```

**What it means:** A constructor resolved itself, directly or through other
constructors, via the `godi.Resolver` it was injected with. `Build` can't see
these dynamic resolutions, so the cycle is reported when it happens (a
`*godi.CircularDependencyError`) instead of deadlocking.

The injected `Resolver` resolves from the scope running the constructor and
attributes its resolutions to that constructor. After the constructor returns,
a stored `Resolver` resolves without that attribution. (A constructor's
injected `context.Context` carries no scope, so `godi.FromContext` fails
there: use the `Resolver`.)

**How to fix:** Break the cycle: take the dependency as a constructor
parameter, or resolve lazily after construction.

### No Scope in Context

```
failed to resolve godi.Scope: no scope found in context
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

    // Check for "not registered" with the sentinel
    if errors.Is(err, godi.ErrServiceNotFound) {
        log.Print("service is not registered")
    }

    // Or extract the typed error for details (Go 1.26+)
    if resErr, ok := errors.AsType[*godi.ResolutionError](err); ok {
        log.Printf("failed to resolve: %s", resErr.ServiceType)
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

(get-the-full-explanation)=

### 4. Get the Full Explanation

Each godi error's message is a single line without stack traces, safe for
logs. (An aggregate — several validation failures joined together, or a
`DisposalError` with several failures — lists one per line.)
`godi.Explain(err)` adds the detail: remediation hints, "did you mean"
suggestions, the dependency cycle drawn out, and the stack trace of a
constructor panic. It walks any wrapped or joined error, so it is the reliable
entry point; `fmt.Printf("%+v", err)` gives the same output only when `err` is
itself a godi error, not one wrapped with `fmt.Errorf`.

```go
if _, err := services.Build(); err != nil {
    log.Fatal(godi.Explain(err))
}
```

Constructor failures name the function and its source location, for example
`constructor users.NewService (service.go:42) failed: ...`, and types are
package-qualified (`*db.Config`).

A runnable `Explain` of a lifetime conflict is the [`ExampleExplain`](https://pkg.go.dev/github.com/junioryono/godi/v6#example-Explain) example in the package documentation (`example_test.go`), verified by `go test`.

### 5. Observe Construction and Disposal

`godi.WithObserver` receives an event for every constructor call and
every disposal, with durations and errors — including cleanup failures of
values produced after their scope closed, which have no caller to return an
error to. Set the callbacks you need:

```go
provider, err := services.Build(godi.WithObserver(godi.Observer{
    Constructed: func(e *godi.ConstructedEvent) {
        slog.Debug("constructed", "service", e.ServiceType, "scope", e.ScopeID,
            "took", e.Duration, "err", e.Err)
    },
    Disposed: func(e *godi.DisposedEvent) {
        if e.Err != nil {
            slog.Warn("cleanup failed", "type", e.Type, "err", e.Err)
        }
    },
}))
```

### 6. Inspect the Graph

`godi.Describe(provider)` lists every registration with its lifetime,
constructor location and dependencies (including decorators'), without
constructing anything. `godi.WriteDOT` renders it for Graphviz:

```go
godi.WriteDOT(os.Stdout, godi.Describe(provider)) // go run . | dot -Tsvg > graph.svg
```

### 7. Log Scope Creation

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
