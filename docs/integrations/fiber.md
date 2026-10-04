# Fiber Integration

Complete guide for using godi with the [Fiber](https://github.com/gofiber/fiber) web framework, version 2 (`github.com/gofiber/fiber/v2`).
For Fiber v3, see the [Fiber v3 integration](fiber-v3.md).

## Installation

```bash
go get github.com/junioryono/godi/v5
go get github.com/junioryono/godi/fiber/v5
```

## Quick Start

```go
package main

import (
    "github.com/gofiber/fiber/v2"
    "github.com/junioryono/godi/v5"
    godifiber "github.com/junioryono/godi/fiber/v5"
)

type UserController struct{}

func NewUserController() *UserController {
    return &UserController{}
}

func (c *UserController) List(ctx *fiber.Ctx) error {
    return ctx.JSON([]string{"alice", "bob"})
}

func main() {
    services := godi.NewCollection()
    services.AddScoped(NewUserController)

    provider, _ := services.Build()
    defer provider.Close()

    app := fiber.New()
    app.Use(godifiber.ScopeMiddleware(provider))
    app.Get("/users", godifiber.Handle((*UserController).List))

    app.Listen(":8080")
}
```

## ScopeMiddleware

Creates a request scope for each HTTP request. The scope is attached to the request's `UserContext`, which Fiber resets between requests.

```go
app := fiber.New()
app.Use(godifiber.ScopeMiddleware(provider))
```

```{important}
The scope middleware **consumes errors**. It dispatches downstream errors
(and `WithMiddleware` errors) through Fiber's configured `ErrorHandler`
before closing the request scope, then returns `nil` to avoid a second
dispatch. Handlers registered *before* `ScopeMiddleware` never see the error.
```

Register middleware that needs the returned error, including panic recovery,
*after* the scope middleware so it runs inside the scope:

```go
app.Use(godifiber.ScopeMiddleware(provider))
app.Use(logger.New())
app.Use(recover.New())
```

To return errors to outer handlers instead, use
`godifiber.WithErrorPassthrough(true)`. Fiber then renders the error after the
scope has closed, so the error handler cannot use request-scoped services.
See the [integration contract](#echo-and-fiber-errors-are-consumed-inside-the-scope).

When the response body is a stream (`c.SendStream`), the scope stays open
until fasthttp has written the response, because the stream is usually a
scoped resource.

### Configuration Options

```go
app.Use(godifiber.ScopeMiddleware(provider,
    // Custom error handler for scope creation failures
    godifiber.WithErrorHandler(func(c *fiber.Ctx, err error) error {
        return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
            "error": "Service unavailable",
        })
    }),

    // Custom error handler for WithMiddleware failures
    // (defaults to the error handler above)
    godifiber.WithMiddlewareErrorHandler(func(c *fiber.Ctx, err error) error {
        return fiber.ErrUnauthorized
    }),

    // Logger for the default handlers (defaults to slog.Default())
    godifiber.WithLogger(logger),

    // Return errors to outer handlers instead of consuming them;
    // Fiber then renders them after the scope closes (default false)
    godifiber.WithErrorPassthrough(false),

    // Custom handler for scope close errors
    godifiber.WithCloseErrorHandler(func(err error) {
        log.Printf("Scope close error: %v", err)
    }),

    // Middleware that runs after scope creation
    godifiber.WithMiddleware(func(scope godi.Scope, c *fiber.Ctx) error {
        reqCtx := godi.MustResolve[*RequestContext](scope)
        reqCtx.UserID = c.Get("X-User-ID")
        return nil
    }),
))
```

## Handle

Wraps a controller method for type-safe resolution.

```go
type UserController interface {
    List(*fiber.Ctx) error
    GetByID(*fiber.Ctx) error
    Create(*fiber.Ctx) error
}

app.Get("/users", godifiber.Handle(UserController.List))
app.Get("/users/:id", godifiber.Handle(UserController.GetByID))
app.Post("/users", godifiber.Handle(UserController.Create))
```

### Handler Options

```go
app.Get("/users", godifiber.Handle(UserController.List,
    // Enable panic recovery
    godifiber.WithPanicRecovery(true),

    // Custom panic handler
    godifiber.WithPanicHandler(func(c *fiber.Ctx, v any) error {
        log.Printf("Panic: %v", v)
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "error": "Unexpected error",
        })
    }),

    // Custom scope error handler
    godifiber.WithScopeErrorHandler(func(c *fiber.Ctx, err error) error {
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "error": "Session error",
        })
    }),

    // Custom resolution error handler
    godifiber.WithResolutionErrorHandler(func(c *fiber.Ctx, err error) error {
        return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
            "error": "Service unavailable",
        })
    }),

    // Logger for the default handlers (defaults to slog.Default())
    godifiber.WithHandlerLogger(logger),
))
```

Default handlers log the cause with `log/slog` (the panic handler also logs
the stack trace) and respond with a generic 500; they never send internal
error text to the client.

## Complete Example

```go
package main

import (
    "log"
    "time"

    "github.com/gofiber/fiber/v2"
    "github.com/gofiber/fiber/v2/middleware/logger"
    "github.com/gofiber/fiber/v2/middleware/recover"
    "github.com/google/uuid"
    "github.com/junioryono/godi/v5"
    godifiber "github.com/junioryono/godi/fiber/v5"
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

func (c *UserController) List(ctx *fiber.Ctx) error {
    users := c.service.GetAll()
    ctx.Set("X-Request-ID", c.reqCtx.ID)
    return ctx.JSON(fiber.Map{
        "users":    users,
        "duration": time.Since(c.reqCtx.StartTime).String(),
    })
}

func (c *UserController) GetByID(ctx *fiber.Ctx) error {
    id := ctx.Params("id")
    ctx.Set("X-Request-ID", c.reqCtx.ID)
    return ctx.JSON(User{ID: 1, Name: "User " + id})
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

    // Create Fiber app
    app := fiber.New(fiber.Config{
        ErrorHandler: func(c *fiber.Ctx, err error) error {
            return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
                "error": "internal server error",
            })
        },
    })

    // The scope wraps error observation and recovery so they run before close.
    app.Use(godifiber.ScopeMiddleware(provider,
        godifiber.WithMiddleware(func(scope godi.Scope, c *fiber.Ctx) error {
            reqCtx := godi.MustResolve[*RequestContext](scope)
            reqCtx.UserID = c.Get("X-User-ID")
            return nil
        }),
    ))
    app.Use(logger.New())
    app.Use(recover.New())

    // Routes
    app.Get("/users", godifiber.Handle((*UserController).List))
    app.Get("/users/:id", godifiber.Handle((*UserController).GetByID))

    // Health check
    app.Get("/health", func(c *fiber.Ctx) error {
        return c.SendString("OK")
    })

    // Start server
    log.Println("Server starting on :8080")
    app.Listen(":8080")
}
```

## Route Groups

Use with Fiber route groups:

```go
api := app.Group("/api/v1")

users := api.Group("/users")
users.Get("/", godifiber.Handle((*UserController).List))
users.Get("/:id", godifiber.Handle((*UserController).GetByID))
users.Post("/", godifiber.Handle((*UserController).Create))

orders := api.Group("/orders")
orders.Get("/", godifiber.Handle((*OrderController).List))
orders.Post("/", godifiber.Handle((*OrderController).Create))
```

## FromContext Helper

Fiber attaches the scope to the request's `UserContext`. Retrieve it with
`godi.FromContext`:

```go
app.Get("/custom", func(c *fiber.Ctx) error {
    scope, err := godi.FromContext(c.UserContext())
    if err != nil {
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "error": "No scope",
        })
    }

    service := godi.MustResolve[*UserService](scope)
    users := service.GetAll()
    return c.JSON(users)
})
```

## Accessing URL Parameters

Use Fiber's parameter methods in your controllers:

```go
func (c *UserController) GetByID(ctx *fiber.Ctx) error {
    id := ctx.Params("id")
    // ...
}
```

## Request Body Parsing

Combine godi with Fiber's body parser:

```go
type CreateUserRequest struct {
    Name  string `json:"name"`
    Email string `json:"email"`
}

func (c *UserController) Create(ctx *fiber.Ctx) error {
    var req CreateUserRequest
    if err := ctx.BodyParser(&req); err != nil {
        return ctx.Status(fiber.StatusBadRequest).JSON(fiber.Map{
            "error": "Invalid request",
        })
    }

    user := c.service.Create(req.Name, req.Email)
    return ctx.Status(fiber.StatusCreated).JSON(user)
}
```

## Important: Fiber's Context Handling

Fiber reuses `*fiber.Ctx` between requests for performance. The godi integration handles this correctly by attaching the scope to the request's `UserContext`, which Fiber resets between requests.

```go
// Retrieve the scope the same way as every other integration:
scope, err := godi.FromContext(c.UserContext())
```

---

**See also:** [Integration contract](#integration-contract) | [Fiber v3 Integration](fiber-v3.md) | [Gin Integration](gin.md) | [Chi Integration](chi.md) | [Echo Integration](echo.md) | [net/http Integration](net-http.md)
