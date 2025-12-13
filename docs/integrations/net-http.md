# net/http Integration

Complete guide for using godi with Go's standard `net/http` package.

## Installation

```bash
go get github.com/junioryono/godi/v4
go get github.com/junioryono/godi/v4/http
```

## Quick Start

```go
package main

import (
    "encoding/json"
    "net/http"

    "github.com/junioryono/godi/v4"
    godihttp "github.com/junioryono/godi/v4/http"
)

type UserController struct{}

func NewUserController() *UserController {
    return &UserController{}
}

func (c *UserController) List(w http.ResponseWriter, r *http.Request) {
    json.NewEncoder(w).Encode([]string{"alice", "bob"})
}

func main() {
    services := godi.NewCollection()
    services.AddScoped(NewUserController)

    provider, _ := services.Build()
    defer provider.Close()

    mux := http.NewServeMux()
    mux.HandleFunc("GET /users", godihttp.Handle((*UserController).List))

    handler := godihttp.ScopeMiddleware(provider)(mux)
    http.ListenAndServe(":8080", handler)
}
```

## ScopeMiddleware

Creates a request scope for each HTTP request.

```go
mux := http.NewServeMux()
// ... register handlers ...

handler := godihttp.ScopeMiddleware(provider)(mux)
http.ListenAndServe(":8080", handler)
```

### Configuration Options

```go
handler := godihttp.ScopeMiddleware(provider,
    // Custom error handler for scope creation failures
    godihttp.WithErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
        http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
    }),

    // Custom handler for scope close errors
    godihttp.WithCloseErrorHandler(func(err error) {
        log.Printf("Scope close error: %v", err)
    }),

    // Middleware that runs after scope creation
    godihttp.WithMiddleware(func(scope godi.Scope, r *http.Request) error {
        reqCtx := godi.MustResolve[*RequestContext](scope)
        reqCtx.UserID = r.Header.Get("X-User-ID")
        return nil
    }),
)(mux)
```

## Handle

Wraps a controller method for type-safe resolution.

```go
type UserController interface {
    List(http.ResponseWriter, *http.Request)
    GetByID(http.ResponseWriter, *http.Request)
    Create(http.ResponseWriter, *http.Request)
}

mux.HandleFunc("GET /users", godihttp.Handle(UserController.List))
mux.HandleFunc("GET /users/{id}", godihttp.Handle(UserController.GetByID))
mux.HandleFunc("POST /users", godihttp.Handle(UserController.Create))
```

### Handler Options

```go
mux.HandleFunc("GET /users", godihttp.Handle(UserController.List,
    // Enable panic recovery
    godihttp.WithPanicRecovery(true),

    // Custom panic handler
    godihttp.WithPanicHandler(func(w http.ResponseWriter, r *http.Request, v any) {
        log.Printf("Panic: %v", v)
        http.Error(w, "Unexpected error", http.StatusInternalServerError)
    }),

    // Custom scope error handler
    godihttp.WithScopeErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
        http.Error(w, "Session error", http.StatusInternalServerError)
    }),

    // Custom resolution error handler
    godihttp.WithResolutionErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
        http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
    }),
))
```

## Complete Example

```go
package main

import (
    "context"
    "encoding/json"
    "log"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/google/uuid"
    "github.com/junioryono/godi/v4"
    godihttp "github.com/junioryono/godi/v4/http"
)

// === Services ===

type Logger struct{}

func NewLogger() *Logger { return &Logger{} }

func (l *Logger) Info(msg string, args ...any) {
    log.Printf("[INFO] "+msg, args...)
}

type RequestContext struct {
    ID        string
    UserID    string
    StartTime time.Time
}

func NewRequestContext() *RequestContext {
    return &RequestContext{
        ID:        uuid.New().String()[:8],
        StartTime: time.Now(),
    }
}

type User struct {
    ID   int    `json:"id"`
    Name string `json:"name"`
}

type UserService struct {
    reqCtx *RequestContext
    logger *Logger
}

func NewUserService(reqCtx *RequestContext, logger *Logger) *UserService {
    return &UserService{reqCtx: reqCtx, logger: logger}
}

func (s *UserService) GetAll() []User {
    s.logger.Info("[%s] Fetching all users", s.reqCtx.ID)
    return []User{{ID: 1, Name: "Alice"}, {ID: 2, Name: "Bob"}}
}

// === Controllers ===

type UserController struct {
    service *UserService
    reqCtx  *RequestContext
}

func NewUserController(service *UserService, reqCtx *RequestContext) *UserController {
    return &UserController{service: service, reqCtx: reqCtx}
}

func (c *UserController) List(w http.ResponseWriter, r *http.Request) {
    users := c.service.GetAll()
    w.Header().Set("Content-Type", "application/json")
    w.Header().Set("X-Request-ID", c.reqCtx.ID)
    json.NewEncoder(w).Encode(map[string]any{
        "users":    users,
        "duration": time.Since(c.reqCtx.StartTime).String(),
    })
}

func (c *UserController) GetByID(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    w.Header().Set("Content-Type", "application/json")
    w.Header().Set("X-Request-ID", c.reqCtx.ID)
    json.NewEncoder(w).Encode(User{ID: 1, Name: "User " + id})
}

// === Main ===

func main() {
    // Register services
    services := godi.NewCollection()
    services.AddSingleton(NewLogger)
    services.AddScoped(NewRequestContext)
    services.AddScoped(NewUserService)
    services.AddScoped(NewUserController)

    // Build provider
    provider, err := services.Build()
    if err != nil {
        log.Fatalf("Failed to build provider: %v", err)
    }
    defer provider.Close()

    // Get logger
    logger := godi.MustResolve[*Logger](provider)

    // Create mux
    mux := http.NewServeMux()

    // Routes
    mux.HandleFunc("GET /users", godihttp.Handle((*UserController).List))
    mux.HandleFunc("GET /users/{id}", godihttp.Handle((*UserController).GetByID))

    // Health check (no DI needed)
    mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("OK"))
    })

    // Wrap with scope middleware
    handler := godihttp.ScopeMiddleware(provider,
        godihttp.WithMiddleware(func(scope godi.Scope, r *http.Request) error {
            reqCtx := godi.MustResolve[*RequestContext](scope)
            reqCtx.UserID = r.Header.Get("X-User-ID")
            return nil
        }),
    )(mux)

    // Create server
    server := &http.Server{
        Addr:         ":8080",
        Handler:      handler,
        ReadTimeout:  15 * time.Second,
        WriteTimeout: 15 * time.Second,
        IdleTimeout:  60 * time.Second,
    }

    // Graceful shutdown
    go func() {
        sigChan := make(chan os.Signal, 1)
        signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
        <-sigChan

        logger.Info("Shutting down server...")
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        server.Shutdown(ctx)
    }()

    // Start server
    logger.Info("Server starting on :8080")
    if err := server.ListenAndServe(); err != http.ErrServerClosed {
        log.Fatalf("Server error: %v", err)
    }
}
```

## Wrap Helper

Alternative to `Handle` for creating `http.Handler`:

```go
handler := godihttp.Wrap(func(ctrl *UserController, w http.ResponseWriter, r *http.Request) {
    ctrl.List(w, r)
})

mux.Handle("GET /users", handler)
```

## Accessing URL Parameters (Go 1.22+)

Use `r.PathValue()` for path parameters:

```go
func (c *UserController) GetByID(w http.ResponseWriter, r *http.Request) {
    id := r.PathValue("id")
    // ...
}
```

## Accessing Scope Manually

```go
mux.HandleFunc("GET /custom", func(w http.ResponseWriter, r *http.Request) {
    scope, err := godi.FromContext(r.Context())
    if err != nil {
        http.Error(w, "No scope", http.StatusInternalServerError)
        return
    }

    service := godi.MustResolve[*UserService](scope)
    users := service.GetAll()
    json.NewEncoder(w).Encode(users)
})
```

## Middleware Chaining

Compose with other middleware:

```go
func loggingMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        log.Printf("%s %s", r.Method, r.URL.Path)
        next.ServeHTTP(w, r)
    })
}

func authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        token := r.Header.Get("Authorization")
        if token == "" {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }
        next.ServeHTTP(w, r)
    })
}

// Compose middleware
handler := loggingMiddleware(
    godihttp.ScopeMiddleware(provider)(
        authMiddleware(mux),
    ),
)
```

## Subrouting

Create subrouters with different middleware:

```go
// Public routes
publicMux := http.NewServeMux()
publicMux.HandleFunc("GET /health", healthHandler)

// API routes with DI
apiMux := http.NewServeMux()
apiMux.HandleFunc("GET /users", godihttp.Handle((*UserController).List))
apiMux.HandleFunc("GET /users/{id}", godihttp.Handle((*UserController).GetByID))

// Compose
mainMux := http.NewServeMux()
mainMux.Handle("/", publicMux)
mainMux.Handle("/api/", http.StripPrefix("/api",
    godihttp.ScopeMiddleware(provider)(apiMux),
))

http.ListenAndServe(":8080", mainMux)
```

## JSON Response Helper

Create a helper for consistent JSON responses:

```go
func writeJSON(w http.ResponseWriter, status int, data any) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(status)
    json.NewEncoder(w).Encode(data)
}

func (c *UserController) List(w http.ResponseWriter, r *http.Request) {
    users := c.service.GetAll()
    writeJSON(w, http.StatusOK, map[string]any{
        "users": users,
    })
}
```

---

**See also:** [Gin Integration](gin.md) | [Chi Integration](chi.md) | [Echo Integration](echo.md) | [Fiber Integration](fiber.md)
