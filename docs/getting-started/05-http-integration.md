# HTTP Integration

godi shines in web applications where each request needs its own isolated scope. This tutorial uses the standard library, but godi has integrations for Gin, Chi, Echo, and Fiber too.

## The Pattern

```
Request arrives → Create scope → Handle request → Close scope
```

Each request gets fresh scoped services while sharing singletons like database connections.

## Basic Setup

```go
package main

import (
    "encoding/json"
    "log"
    "net/http"

    "github.com/junioryono/godi/v4"
    godihttp "github.com/junioryono/godi/v4/http"
)

// Services
type Logger struct{}

func (l *Logger) Log(msg string) {
    log.Println(msg)
}

func NewLogger() *Logger {
    return &Logger{}
}

type UserService struct {
    logger *Logger
}

func NewUserService(logger *Logger) *UserService {
    return &UserService{logger: logger}
}

func (u *UserService) GetUsers(w http.ResponseWriter, r *http.Request) {
    u.logger.Log("Fetching users")
    json.NewEncoder(w).Encode([]string{"alice", "bob"})
}

func main() {
    // Register services
    services := godi.NewCollection()
    services.AddSingleton(NewLogger)
    services.AddScoped(NewUserService)

    // Build provider
    provider, err := services.Build()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // Setup routes
    mux := http.NewServeMux()
    mux.HandleFunc("GET /users", godihttp.Handle((*UserService).GetUsers))

    // Wrap with scope middleware
    handler := godihttp.ScopeMiddleware(provider)(mux)

    log.Println("Server starting on :8080")
    http.ListenAndServe(":8080", handler)
}
```

## What's Happening

```
┌────────────────────────────────────────────────────────────────┐
│  GET /users                                                     │
├────────────────────────────────────────────────────────────────┤
│  1. ScopeMiddleware creates a new scope                        │
│     scope := provider.CreateScope(r.Context())                 │
│                                                                │
│  2. Handle resolves UserService from scope                     │
│     userService := godi.Resolve[*UserService](scope)           │
│
│  3. UserService depends on Logger (singleton)                  │
│     - Logger already exists, reused                            │
│                                                                │
│  4. Handler method is called                                   │
│     userService.GetUsers(w, r)                                 │
│                                                                │
│  5. Scope is closed (scoped services disposed)                 │
│     scope.Close()                                              │
└────────────────────────────────────────────────────────────────┘
```

## Request-Scoped Data

Use scoped services for per-request state:

```go
// RequestContext holds per-request data
type RequestContext struct {
    RequestID string
    UserID    string
}

func NewRequestContext() *RequestContext {
    return &RequestContext{
        RequestID: uuid.New().String(),
    }
}

// UserService uses the request context
type UserService struct {
    ctx    *RequestContext
    logger *Logger
}

func NewUserService(ctx *RequestContext, logger *Logger) *UserService {
    return &UserService{ctx: ctx, logger: logger}
}

func (u *UserService) GetUsers(w http.ResponseWriter, r *http.Request) {
    u.logger.Log(fmt.Sprintf("[%s] Fetching users", u.ctx.RequestID))
    // ...
}

func main() {
    services := godi.NewCollection()
    services.AddSingleton(NewLogger)
    services.AddScoped(NewRequestContext)  // Fresh per request
    services.AddScoped(NewUserService)
    // ...
}
```

## Setting Request Data

Use middleware to populate request context:

```go
handler := godihttp.ScopeMiddleware(provider,
    godihttp.WithMiddleware(func(scope godi.Scope, r *http.Request) error {
        ctx := godi.MustResolve[*RequestContext](scope)
        ctx.UserID = r.Header.Get("X-User-ID")
        return nil
    }),
)(mux)
```

## Complete Example

```go
package main

import (
    "encoding/json"
    "fmt"
    "log"
    "net/http"
    "time"

    "github.com/google/uuid"
    "github.com/junioryono/godi/v4"
    godihttp "github.com/junioryono/godi/v4/http"
)

// === Services ===

type Logger struct{}

func NewLogger() *Logger { return &Logger{} }

func (l *Logger) Log(reqID, msg string) {
    log.Printf("[%s] %s", reqID, msg)
}

type RequestContext struct {
    ID        string
    StartTime time.Time
    UserID    string
}

func NewRequestContext() *RequestContext {
    return &RequestContext{
        ID:        uuid.New().String()[:8],
        StartTime: time.Now(),
    }
}

type UserService struct {
    ctx    *RequestContext
    logger *Logger
}

func NewUserService(ctx *RequestContext, logger *Logger) *UserService {
    return &UserService{ctx: ctx, logger: logger}
}

func (u *UserService) GetUsers(w http.ResponseWriter, r *http.Request) {
    u.logger.Log(u.ctx.ID, "Fetching all users")
    json.NewEncoder(w).Encode(map[string]any{
        "request_id": u.ctx.ID,
        "users":      []string{"alice", "bob", "charlie"},
        "duration":   time.Since(u.ctx.StartTime).String(),
    })
}

func (u *UserService) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
    if u.ctx.UserID == "" {
        http.Error(w, "unauthorized", http.StatusUnauthorized)
        return
    }
    u.logger.Log(u.ctx.ID, fmt.Sprintf("Fetching user: %s", u.ctx.UserID))
    json.NewEncoder(w).Encode(map[string]any{
        "request_id": u.ctx.ID,
        "user_id":    u.ctx.UserID,
    })
}

// === Main ===

func main() {
    services := godi.NewCollection()
    services.AddSingleton(NewLogger)
    services.AddScoped(NewRequestContext)
    services.AddScoped(NewUserService)

    provider, err := services.Build()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    mux := http.NewServeMux()
    mux.HandleFunc("GET /users", godihttp.Handle((*UserService).GetUsers))
    mux.HandleFunc("GET /me", godihttp.Handle((*UserService).GetCurrentUser))

    // Middleware to set user from header
    handler := godihttp.ScopeMiddleware(provider,
        godihttp.WithMiddleware(func(scope godi.Scope, r *http.Request) error {
            ctx := godi.MustResolve[*RequestContext](scope)
            ctx.UserID = r.Header.Get("X-User-ID")
            return nil
        }),
    )(mux)

    log.Println("Server starting on :8080")
    log.Fatal(http.ListenAndServe(":8080", handler))
}
```

Test it:

```bash
# Get all users
curl http://localhost:8080/users

# Get current user (with auth header)
curl -H "X-User-ID: alice" http://localhost:8080/me

# Get current user (no auth)
curl http://localhost:8080/me
```

## Framework Integrations

godi has dedicated integrations for popular frameworks:

| Framework | Package                               | Docs                                                |
| --------- | ------------------------------------- | --------------------------------------------------- |
| Gin       | `github.com/junioryono/godi/v4/gin`   | [Gin Integration](../integrations/gin.md)           |
| Chi       | `github.com/junioryono/godi/v4/chi`   | [Chi Integration](../integrations/chi.md)           |
| Echo      | `github.com/junioryono/godi/v4/echo`  | [Echo Integration](../integrations/echo.md)         |
| Fiber     | `github.com/junioryono/godi/v4/fiber` | [Fiber Integration](../integrations/fiber.md)       |
| net/http  | `github.com/junioryono/godi/v4/http`  | [net/http Integration](../integrations/net-http.md) |

Each integration provides:

- **ScopeMiddleware** - Creates request scopes automatically
- **Handle** - Type-safe controller resolution
- **Custom error handlers** - Control error responses

---

**Next:** [Where to go from here](06-next-steps.md)
