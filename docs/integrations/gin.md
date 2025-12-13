# Gin Integration

Complete guide for using godi with the [Gin](https://github.com/gin-gonic/gin) web framework.

## Installation

```bash
go get github.com/junioryono/godi/v4
go get github.com/junioryono/godi/v4/gin
```

## Quick Start

```go
package main

import (
    "github.com/gin-gonic/gin"
    "github.com/junioryono/godi/v4"
    godigin "github.com/junioryono/godi/v4/gin"
)

type UserController struct{}

func NewUserController() *UserController {
    return &UserController{}
}

func (c *UserController) List(ctx *gin.Context) {
    ctx.JSON(200, gin.H{"users": []string{"alice", "bob"}})
}

func main() {
    services := godi.NewCollection()
    services.AddScoped(NewUserController)

    provider, _ := services.Build()
    defer provider.Close()

    g := gin.Default()
    g.Use(godigin.ScopeMiddleware(provider))
    g.GET("/users", godigin.Handle((*UserController).List))

    g.Run(":8080")
}
```

## ScopeMiddleware

Creates a request scope for each HTTP request. The scope is automatically closed when the request completes.

```go
g := gin.New()
g.Use(godigin.ScopeMiddleware(provider))
```

### Configuration Options

```go
g.Use(godigin.ScopeMiddleware(provider,
    // Custom error handler for scope creation failures
    godigin.WithErrorHandler(func(c *gin.Context, err error) {
        c.AbortWithStatusJSON(500, gin.H{"error": "Service unavailable"})
    }),

    // Custom handler for scope close errors
    godigin.WithCloseErrorHandler(func(err error) {
        log.Printf("Scope close error: %v", err)
    }),

    // Middleware that runs after scope creation
    godigin.WithMiddleware(func(scope godi.Scope, c *gin.Context) error {
        // Initialize request context
        reqCtx := godi.MustResolve[*RequestContext](scope)
        reqCtx.UserID = c.GetHeader("X-User-ID")
        return nil
    }),
))
```

## Handle

Wraps a controller method for type-safe resolution from the request scope.

```go
type UserController interface {
    List(*gin.Context)
    GetByID(*gin.Context)
    Create(*gin.Context)
}

g.GET("/users", godigin.Handle(UserController.List))
g.GET("/users/:id", godigin.Handle(UserController.GetByID))
g.POST("/users", godigin.Handle(UserController.Create))
```

### Handler Options

```go
g.GET("/users", godigin.Handle(UserController.List,
    // Enable panic recovery
    godigin.WithPanicRecovery(true),

    // Custom panic handler
    godigin.WithPanicHandler(func(c *gin.Context, recovered any) {
        log.Printf("Panic: %v", recovered)
        c.AbortWithStatusJSON(500, gin.H{"error": "Unexpected error"})
    }),

    // Custom scope error handler
    godigin.WithScopeErrorHandler(func(c *gin.Context, err error) {
        c.AbortWithStatusJSON(500, gin.H{"error": "Session error"})
    }),

    // Custom resolution error handler
    godigin.WithResolutionErrorHandler(func(c *gin.Context, err error) {
        c.AbortWithStatusJSON(500, gin.H{"error": "Service unavailable"})
    }),
))
```

## Complete Example

A production-ready setup with multiple controllers:

```go
package main

import (
    "log"
    "net/http"
    "strconv"
    "time"

    "github.com/gin-gonic/gin"
    "github.com/google/uuid"
    "github.com/junioryono/godi/v4"
    godigin "github.com/junioryono/godi/v4/gin"
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

func (s *UserService) GetByID(id int) *User {
    s.logger.Info("[%s] Fetching user %d", s.reqCtx.ID, id)
    return &User{ID: id, Name: "User " + strconv.Itoa(id)}
}

// === Controllers ===

type UserController struct {
    service *UserService
    reqCtx  *RequestContext
}

func NewUserController(service *UserService, reqCtx *RequestContext) *UserController {
    return &UserController{service: service, reqCtx: reqCtx}
}

func (c *UserController) List(ctx *gin.Context) {
    users := c.service.GetAll()
    ctx.Header("X-Request-ID", c.reqCtx.ID)
    ctx.JSON(http.StatusOK, gin.H{
        "users":    users,
        "duration": time.Since(c.reqCtx.StartTime).String(),
    })
}

func (c *UserController) GetByID(ctx *gin.Context) {
    id, _ := strconv.Atoi(ctx.Param("id"))
    user := c.service.GetByID(id)
    ctx.Header("X-Request-ID", c.reqCtx.ID)
    ctx.JSON(http.StatusOK, user)
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
    g := gin.New()
    g.Use(gin.Recovery())

    // Add scope middleware with initialization
    g.Use(godigin.ScopeMiddleware(provider,
        godigin.WithMiddleware(func(scope godi.Scope, c *gin.Context) error {
            reqCtx := godi.MustResolve[*RequestContext](scope)
            reqCtx.UserID = c.GetHeader("X-User-ID")
            return nil
        }),
    ))

    // Routes
    g.GET("/users", godigin.Handle((*UserController).List))
    g.GET("/users/:id", godigin.Handle((*UserController).GetByID))

    // Health check (no DI needed)
    g.GET("/health", func(c *gin.Context) {
        c.JSON(http.StatusOK, gin.H{"status": "ok"})
    })

    // Start server
    log.Println("Server starting on :8080")
    g.Run(":8080")
}
```

## Accessing Scope Manually

If you need to resolve services manually within a handler:

```go
g.GET("/custom", func(c *gin.Context) {
    scope, err := godi.FromContext(c.Request.Context())
    if err != nil {
        c.AbortWithStatusJSON(500, gin.H{"error": "No scope"})
        return
    }

    service := godi.MustResolve[*UserService](scope)
    users := service.GetAll()
    c.JSON(200, users)
})
```

## Route Groups

Use with Gin route groups:

```go
api := g.Group("/api/v1")
{
    users := api.Group("/users")
    {
        users.GET("", godigin.Handle((*UserController).List))
        users.GET("/:id", godigin.Handle((*UserController).GetByID))
        users.POST("", godigin.Handle((*UserController).Create))
    }

    orders := api.Group("/orders")
    {
        orders.GET("", godigin.Handle((*OrderController).List))
        orders.POST("", godigin.Handle((*OrderController).Create))
    }
}
```

## Error Responses

Default error responses return JSON:

```json
{
  "error": "Internal Server Error"
}
```

Customize with error handlers:

```go
godigin.WithResolutionErrorHandler(func(c *gin.Context, err error) {
    c.AbortWithStatusJSON(503, gin.H{
        "error":   "Service temporarily unavailable",
        "details": err.Error(),
    })
})
```

---

**See also:** [Chi Integration](chi.md) | [Echo Integration](echo.md) | [Fiber Integration](fiber.md) | [net/http Integration](net-http.md)
