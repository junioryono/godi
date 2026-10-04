# Fiber v3 Integration

Complete guide for using godi with [Fiber](https://github.com/gofiber/fiber) v3 (`github.com/gofiber/fiber/v3`).
For Fiber v2, see the [Fiber integration](fiber.md).

The Fiber v3 integration has the same API as the Fiber v2 one: `ScopeMiddleware`,
`Handle`, and the same options. The differences follow Fiber v3 itself:

- Handlers and options take the `fiber.Ctx` interface instead of `*fiber.Ctx`.
- The scope is attached with `Ctx.SetContext`, so it is retrieved with
  `godi.FromContext(c.Context())` (Fiber v2 used `c.UserContext()`).

The module path's `/v5` suffix is godi's major version; the `fiberv3` directory
names the Fiber version.

## Installation

```bash
go get github.com/junioryono/godi/v5
go get github.com/junioryono/godi/fiberv3/v5
```

## Quick Start

```go
package main

import (
    "github.com/gofiber/fiber/v3"
    "github.com/junioryono/godi/v5"
    godifiber "github.com/junioryono/godi/fiberv3/v5"
)

type UserController struct{}

func NewUserController() *UserController {
    return &UserController{}
}

func (c *UserController) List(ctx fiber.Ctx) error {
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

Creates a request scope for each HTTP request and sets its context as the
request context (`c.Context()`). Fiber clears it when the pooled `Ctx` is
released, so a scope never leaks into the next request.

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

### Streamed responses

Fiber writes a streamed body (`c.SendStream`, `c.SendStreamWriter`) after the
handler chain returns, and the stream is usually a scoped resource. When the
response is a body stream, the middleware keeps the scope open until fasthttp
has written the response, then closes it. Fiber v3 still runs on fasthttp,
which closes request user values that implement `io.Closer` once the
response is written; the middleware registers the scope close as one.

```go
app.Get("/export", func(c fiber.Ctx) error {
    scope, _ := godi.FromContext(c.Context())
    report := godi.MustResolve[*ReportStream](scope) // scoped io.Reader
    return c.SendStream(report)                      // scope closes after the write
})
```

### Configuration Options

```go
app.Use(godifiber.ScopeMiddleware(provider,
    // Custom error handler for scope creation failures
    godifiber.WithErrorHandler(func(c fiber.Ctx, err error) error {
        return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
            "error": "Service unavailable",
        })
    }),

    // Custom error handler for WithMiddleware failures
    // (defaults to the error handler above)
    godifiber.WithMiddlewareErrorHandler(func(c fiber.Ctx, err error) error {
        return fiber.ErrUnauthorized
    }),

    // Custom handler for scope close errors
    godifiber.WithCloseErrorHandler(func(err error) {
        log.Printf("Scope close error: %v", err)
    }),

    // Middleware that runs after scope creation
    godifiber.WithMiddleware(func(scope godi.Scope, c fiber.Ctx) error {
        reqCtx := godi.MustResolve[*RequestContext](scope)
        reqCtx.UserID = c.Get("X-User-ID")
        return nil
    }),

    // Logger for the default handlers (defaults to slog.Default())
    godifiber.WithLogger(logger),

    // Return errors to outer handlers instead of consuming them;
    // Fiber then renders them after the scope closes (default false)
    godifiber.WithErrorPassthrough(false),
))
```

## Handle

Wraps a controller method for type-safe resolution.

```go
type UserController interface {
    List(fiber.Ctx) error
    GetByID(fiber.Ctx) error
    Create(fiber.Ctx) error
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
    godifiber.WithPanicHandler(func(c fiber.Ctx, v any) error {
        log.Printf("Panic: %v", v)
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "error": "Unexpected error",
        })
    }),

    // Custom scope error handler
    godifiber.WithScopeErrorHandler(func(c fiber.Ctx, err error) error {
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "error": "Session error",
        })
    }),

    // Custom resolution error handler
    godifiber.WithResolutionErrorHandler(func(c fiber.Ctx, err error) error {
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

## Error Handling

Fiber's own `DefaultErrorHandler` writes `err.Error()` for plain errors. If
handlers can return internal errors, configure an `ErrorHandler` that maps
them to a generic response. With the default (consuming) mode it runs while
the request scope is alive:

```go
app := fiber.New(fiber.Config{
    ErrorHandler: func(c fiber.Ctx, err error) error {
        if fiberErr, ok := errors.AsType[*fiber.Error](err); ok {
            return c.Status(fiberErr.Code).JSON(fiber.Map{"error": fiberErr.Message})
        }
        if scope, scopeErr := godi.FromContext(c.Context()); scopeErr == nil {
            godi.MustResolve[*RequestLogger](scope).Error("request failed", "error", err)
        }
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "error": "internal server error",
        })
    },
})
```

If the configured `ErrorHandler` itself fails, the middleware logs that
failure and renders a generic 500.

## Accessing Scope Manually

```go
app.Get("/custom", func(c fiber.Ctx) error {
    scope, err := godi.FromContext(c.Context())
    if err != nil {
        return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
            "error": "No scope",
        })
    }

    service := godi.MustResolve[*UserService](scope)
    return c.JSON(service.GetAll())
})
```

`fiber.Ctx` itself implements `context.Context`, but its values are fasthttp
user values, not the request context: pass `c.Context()` to
`godi.FromContext`, not `c`.

## Migrating from the Fiber v2 integration

1. Replace the imports: `github.com/gofiber/fiber/v2` with `github.com/gofiber/fiber/v3`, and
   `github.com/junioryono/godi/fiber/v5` with `github.com/junioryono/godi/fiberv3/v5`.
2. Change `*fiber.Ctx` to `fiber.Ctx` in controllers and option callbacks.
3. Replace `godi.FromContext(c.UserContext())` with `godi.FromContext(c.Context())`.

Option names and behavior are unchanged.

---

**See also:** [Integration contract](#integration-contract) | [Fiber v2 Integration](fiber.md) | [Gin Integration](gin.md) | [Chi Integration](chi.md) | [Echo v5 Integration](echo-v5.md) | [net/http Integration](net-http.md)
