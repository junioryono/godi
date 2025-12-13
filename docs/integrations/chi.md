# Chi Integration

Complete guide for using godi with the [Chi](https://github.com/go-chi/chi) router.

## Installation

```bash
go get github.com/junioryono/godi/v4
go get github.com/junioryono/godi/v4/chi
```

## Quick Start

```go
package main

import (
    "encoding/json"
    "net/http"

    "github.com/go-chi/chi/v5"
    "github.com/junioryono/godi/v4"
    godichi "github.com/junioryono/godi/v4/chi"
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

    r := chi.NewRouter()
    r.Use(godichi.ScopeMiddleware(provider))
    r.Get("/users", godichi.Handle((*UserController).List))

    http.ListenAndServe(":8080", r)
}
```

## ScopeMiddleware

Creates a request scope for each HTTP request.

```go
r := chi.NewRouter()
r.Use(godichi.ScopeMiddleware(provider))
```

### Configuration Options

```go
r.Use(godichi.ScopeMiddleware(provider,
    // Custom error handler for scope creation failures
    godichi.WithErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
        http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
    }),

    // Custom handler for scope close errors
    godichi.WithCloseErrorHandler(func(err error) {
        log.Printf("Scope close error: %v", err)
    }),

    // Middleware that runs after scope creation
    godichi.WithMiddleware(func(scope godi.Scope, r *http.Request) error {
        reqCtx := godi.MustResolve[*RequestContext](scope)
        reqCtx.UserID = r.Header.Get("X-User-ID")
        return nil
    }),
))
```

## Handle

Wraps a controller method for type-safe resolution.

```go
type UserController interface {
    List(http.ResponseWriter, *http.Request)
    GetByID(http.ResponseWriter, *http.Request)
    Create(http.ResponseWriter, *http.Request)
}

r.Get("/users", godichi.Handle(UserController.List))
r.Get("/users/{id}", godichi.Handle(UserController.GetByID))
r.Post("/users", godichi.Handle(UserController.Create))
```

### Handler Options

```go
r.Get("/users", godichi.Handle(UserController.List,
    // Enable panic recovery
    godichi.WithPanicRecovery(true),

    // Custom panic handler
    godichi.WithPanicHandler(func(w http.ResponseWriter, r *http.Request, v any) {
        log.Printf("Panic: %v", v)
        http.Error(w, "Unexpected error", http.StatusInternalServerError)
    }),

    // Custom scope error handler
    godichi.WithScopeErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
        http.Error(w, "Session error", http.StatusInternalServerError)
    }),

    // Custom resolution error handler
    godichi.WithResolutionErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
        http.Error(w, "Service unavailable", http.StatusServiceUnavailable)
    }),
))
```

## Complete Example

```go
package main

import (
    "encoding/json"
    "log"
    "net/http"
    "time"

    "github.com/go-chi/chi/v5"
    "github.com/go-chi/chi/v5/middleware"
    "github.com/google/uuid"
    "github.com/junioryono/godi/v4"
    godichi "github.com/junioryono/godi/v4/chi"
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
    id := chi.URLParam(r, "id")
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

    // Create router
    r := chi.NewRouter()

    // Chi middleware
    r.Use(middleware.Logger)
    r.Use(middleware.Recoverer)

    // godi scope middleware
    r.Use(godichi.ScopeMiddleware(provider,
        godichi.WithMiddleware(func(scope godi.Scope, req *http.Request) error {
            reqCtx := godi.MustResolve[*RequestContext](scope)
            reqCtx.UserID = req.Header.Get("X-User-ID")
            return nil
        }),
    ))

    // Routes
    r.Get("/users", godichi.Handle((*UserController).List))
    r.Get("/users/{id}", godichi.Handle((*UserController).GetByID))

    // Health check
    r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("OK"))
    })

    // Start server
    log.Println("Server starting on :8080")
    http.ListenAndServe(":8080", r)
}
```

## Route Groups

Use with Chi route groups:

```go
r.Route("/api/v1", func(r chi.Router) {
    r.Route("/users", func(r chi.Router) {
        r.Get("/", godichi.Handle((*UserController).List))
        r.Get("/{id}", godichi.Handle((*UserController).GetByID))
        r.Post("/", godichi.Handle((*UserController).Create))
    })

    r.Route("/orders", func(r chi.Router) {
        r.Get("/", godichi.Handle((*OrderController).List))
        r.Post("/", godichi.Handle((*OrderController).Create))
    })
})
```

## Accessing URL Parameters

Use Chi's URL parameter functions in your controllers:

```go
func (c *UserController) GetByID(w http.ResponseWriter, r *http.Request) {
    id := chi.URLParam(r, "id")
    // ...
}
```

## Accessing Scope Manually

```go
r.Get("/custom", func(w http.ResponseWriter, r *http.Request) {
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

## Subrouters

Apply different middleware to subrouters:

```go
r := chi.NewRouter()
r.Use(godichi.ScopeMiddleware(provider))

// Public routes
r.Group(func(r chi.Router) {
    r.Get("/health", healthHandler)
    r.Get("/public", publicHandler)
})

// Authenticated routes
r.Group(func(r chi.Router) {
    r.Use(authMiddleware)
    r.Get("/users", godichi.Handle((*UserController).List))
})
```

---

**See also:** [Gin Integration](gin.md) | [Echo Integration](echo.md) | [Fiber Integration](fiber.md) | [net/http Integration](net-http.md)
