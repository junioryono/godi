# Echo Integration

Complete guide for using godi with the [Echo](https://github.com/labstack/echo) web framework.

## Installation

```bash
go get github.com/junioryono/godi/v4
go get github.com/junioryono/godi/v4/echo
```

## Quick Start

```go
package main

import (
    "net/http"

    "github.com/labstack/echo/v4"
    "github.com/junioryono/godi/v4"
    godiecho "github.com/junioryono/godi/v4/echo"
)

type UserController struct{}

func NewUserController() *UserController {
    return &UserController{}
}

func (c *UserController) List(ctx echo.Context) error {
    return ctx.JSON(http.StatusOK, []string{"alice", "bob"})
}

func main() {
    services := godi.NewCollection()
    services.AddScoped(NewUserController)

    provider, _ := services.Build()
    defer provider.Close()

    e := echo.New()
    e.Use(godiecho.ScopeMiddleware(provider))
    e.GET("/users", godiecho.Handle((*UserController).List))

    e.Start(":8080")
}
```

## ScopeMiddleware

Creates a request scope for each HTTP request.

```go
e := echo.New()
e.Use(godiecho.ScopeMiddleware(provider))
```

### Configuration Options

```go
e.Use(godiecho.ScopeMiddleware(provider,
    // Custom error handler for scope creation failures
    godiecho.WithErrorHandler(func(c echo.Context, err error) error {
        return echo.NewHTTPError(http.StatusServiceUnavailable, "Service unavailable")
    }),

    // Custom handler for scope close errors
    godiecho.WithCloseErrorHandler(func(err error) {
        log.Printf("Scope close error: %v", err)
    }),

    // Middleware that runs after scope creation
    godiecho.WithMiddleware(func(scope godi.Scope, c echo.Context) error {
        reqCtx := godi.MustResolve[*RequestContext](scope)
        reqCtx.UserID = c.Request().Header.Get("X-User-ID")
        return nil
    }),
))
```

## Handle

Wraps a controller method for type-safe resolution.

```go
type UserController interface {
    List(echo.Context) error
    GetByID(echo.Context) error
    Create(echo.Context) error
}

e.GET("/users", godiecho.Handle(UserController.List))
e.GET("/users/:id", godiecho.Handle(UserController.GetByID))
e.POST("/users", godiecho.Handle(UserController.Create))
```

### Handler Options

```go
e.GET("/users", godiecho.Handle(UserController.List,
    // Enable panic recovery
    godiecho.WithPanicRecovery(true),

    // Custom panic handler
    godiecho.WithPanicHandler(func(c echo.Context, v any) error {
        log.Printf("Panic: %v", v)
        return echo.NewHTTPError(http.StatusInternalServerError, "Unexpected error")
    }),

    // Custom scope error handler
    godiecho.WithScopeErrorHandler(func(c echo.Context, err error) error {
        return echo.NewHTTPError(http.StatusInternalServerError, "Session error")
    }),

    // Custom resolution error handler
    godiecho.WithResolutionErrorHandler(func(c echo.Context, err error) error {
        return echo.NewHTTPError(http.StatusServiceUnavailable, "Service unavailable")
    }),
))
```

## Complete Example

```go
package main

import (
    "log"
    "net/http"
    "time"

    "github.com/labstack/echo/v4"
    "github.com/labstack/echo/v4/middleware"
    "github.com/google/uuid"
    "github.com/junioryono/godi/v4"
    godiecho "github.com/junioryono/godi/v4/echo"
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

func (c *UserController) List(ctx echo.Context) error {
    users := c.service.GetAll()
    ctx.Response().Header().Set("X-Request-ID", c.reqCtx.ID)
    return ctx.JSON(http.StatusOK, map[string]any{
        "users":    users,
        "duration": time.Since(c.reqCtx.StartTime).String(),
    })
}

func (c *UserController) GetByID(ctx echo.Context) error {
    id := ctx.Param("id")
    ctx.Response().Header().Set("X-Request-ID", c.reqCtx.ID)
    return ctx.JSON(http.StatusOK, User{ID: 1, Name: "User " + id})
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

    // Create Echo instance
    e := echo.New()

    // Echo middleware
    e.Use(middleware.Logger())
    e.Use(middleware.Recover())

    // godi scope middleware
    e.Use(godiecho.ScopeMiddleware(provider,
        godiecho.WithMiddleware(func(scope godi.Scope, c echo.Context) error {
            reqCtx := godi.MustResolve[*RequestContext](scope)
            reqCtx.UserID = c.Request().Header.Get("X-User-ID")
            return nil
        }),
    ))

    // Routes
    e.GET("/users", godiecho.Handle((*UserController).List))
    e.GET("/users/:id", godiecho.Handle((*UserController).GetByID))

    // Health check
    e.GET("/health", func(c echo.Context) error {
        return c.String(http.StatusOK, "OK")
    })

    // Start server
    log.Println("Server starting on :8080")
    e.Start(":8080")
}
```

## Route Groups

Use with Echo route groups:

```go
api := e.Group("/api/v1")

users := api.Group("/users")
users.GET("", godiecho.Handle((*UserController).List))
users.GET("/:id", godiecho.Handle((*UserController).GetByID))
users.POST("", godiecho.Handle((*UserController).Create))

orders := api.Group("/orders")
orders.GET("", godiecho.Handle((*OrderController).List))
orders.POST("", godiecho.Handle((*OrderController).Create))
```

## Accessing URL Parameters

Use Echo's parameter methods in your controllers:

```go
func (c *UserController) GetByID(ctx echo.Context) error {
    id := ctx.Param("id")
    // ...
}
```

## Accessing Scope Manually

```go
e.GET("/custom", func(c echo.Context) error {
    scope, err := godi.FromContext(c.Request().Context())
    if err != nil {
        return echo.NewHTTPError(http.StatusInternalServerError, "No scope")
    }

    service := godi.MustResolve[*UserService](scope)
    users := service.GetAll()
    return c.JSON(http.StatusOK, users)
})
```

## Request Binding

Combine godi with Echo's request binding:

```go
type CreateUserRequest struct {
    Name  string `json:"name" validate:"required"`
    Email string `json:"email" validate:"required,email"`
}

func (c *UserController) Create(ctx echo.Context) error {
    var req CreateUserRequest
    if err := ctx.Bind(&req); err != nil {
        return echo.NewHTTPError(http.StatusBadRequest, "Invalid request")
    }

    user := c.service.Create(req.Name, req.Email)
    return ctx.JSON(http.StatusCreated, user)
}
```

## Error Handling

Echo uses `echo.HTTPError` for error responses:

```go
godiecho.WithResolutionErrorHandler(func(c echo.Context, err error) error {
    return echo.NewHTTPError(http.StatusServiceUnavailable, map[string]any{
        "error":   "Service temporarily unavailable",
        "details": err.Error(),
    })
})
```

---

**See also:** [Gin Integration](gin.md) | [Chi Integration](chi.md) | [Fiber Integration](fiber.md) | [net/http Integration](net-http.md)
