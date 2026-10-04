# Echo v5 Integration

Complete guide for using godi with [Echo](https://github.com/labstack/echo) v5 (`github.com/labstack/echo/v5`).
For Echo v4, see the [Echo integration](echo.md).

The Echo v5 integration has the same API as the Echo v4 one: `ScopeMiddleware`,
`Handle`, and the same options. The differences follow Echo v5 itself:

- Handlers and options take `*echo.Context` (a struct) instead of `echo.Context` (an interface).
- Errors are dispatched with Echo v5's `HTTPErrorHandler` signature, `func(c *echo.Context, err error)`.

The module path's `/v5` suffix is godi's major version; the `echov5` directory
names the Echo version.

## Installation

```bash
go get github.com/junioryono/godi/v6
go get github.com/junioryono/godi/echov5/v6
```

## Quick Start

```go
package main

import (
    "net/http"

    "github.com/junioryono/godi/v6"
    godiecho "github.com/junioryono/godi/echov5/v6"
    "github.com/labstack/echo/v5"
)

type UserController struct{}

func NewUserController() *UserController {
    return &UserController{}
}

func (c *UserController) List(ctx *echo.Context) error {
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

Creates a request scope for each HTTP request and attaches it to the request
context.

```go
e := echo.New()
e.Use(godiecho.ScopeMiddleware(provider))
```

```{important}
The scope middleware **consumes errors**. It dispatches downstream errors
(and `WithMiddleware` errors) through Echo's configured `HTTPErrorHandler`
before closing the request scope, then returns `nil` to avoid a second
dispatch. Middleware registered *before* `ScopeMiddleware` never sees the
error.
```

Register middleware that needs the returned error, including panic recovery
and request logging, *after* the scope middleware so it runs inside the scope:

```go
e.Use(godiecho.ScopeMiddleware(provider))
e.Use(middleware.RequestLogger())
e.Use(middleware.Recover())
```

To return errors to outer middleware instead, use
`godiecho.WithErrorPassthrough(true)`. Echo then renders the error after the
scope has closed, so the error handler cannot use request-scoped services.
See the [integration contract](#echo-and-fiber-errors-are-consumed-inside-the-scope).

### Configuration Options

```go
e.Use(godiecho.ScopeMiddleware(provider,
    // Custom error handler for scope creation failures
    godiecho.WithErrorHandler(func(c *echo.Context, err error) error {
        return echo.NewHTTPError(http.StatusServiceUnavailable, "Service unavailable")
    }),

    // Custom error handler for WithMiddleware failures
    // (defaults to the error handler above)
    godiecho.WithMiddlewareErrorHandler(func(c *echo.Context, err error) error {
        return echo.ErrUnauthorized
    }),

    // Custom handler for scope close errors
    godiecho.WithCloseErrorHandler(func(err error) {
        log.Printf("Scope close error: %v", err)
    }),

    // Middleware that runs after scope creation
    godiecho.WithMiddleware(func(scope godi.Scope, c *echo.Context) error {
        reqCtx := godi.MustResolve[*RequestContext](scope)
        reqCtx.UserID = c.Request().Header.Get("X-User-ID")
        return nil
    }),

    // Logger for the default handlers (defaults to slog.Default());
    // Echo v5's own logger is a *slog.Logger
    godiecho.WithLogger(e.Logger),

    // Return errors to outer middleware instead of consuming them;
    // Echo then renders them after the scope closes (default false)
    godiecho.WithErrorPassthrough(false),
))
```

## Handle

Wraps a controller method for type-safe resolution.

```go
type UserController interface {
    List(*echo.Context) error
    GetByID(*echo.Context) error
    Create(*echo.Context) error
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
    godiecho.WithPanicHandler(func(c *echo.Context, v any) error {
        log.Printf("Panic: %v", v)
        return echo.NewHTTPError(http.StatusInternalServerError, "Unexpected error")
    }),

    // Custom scope error handler
    godiecho.WithScopeErrorHandler(func(c *echo.Context, err error) error {
        return echo.NewHTTPError(http.StatusInternalServerError, "Session error")
    }),

    // Custom resolution error handler
    godiecho.WithResolutionErrorHandler(func(c *echo.Context, err error) error {
        return echo.NewHTTPError(http.StatusServiceUnavailable, "Service unavailable")
    }),

    // Logger for the default handlers (defaults to slog.Default())
    godiecho.WithHandlerLogger(logger),
))
```

The default panic handler logs the panic value and stack trace, then returns
a generic 500. A panic with `http.ErrAbortHandler` is re-panicked.

## Error Handling

Default handlers log the cause with `log/slog` and return a generic 500
`*echo.HTTPError`; they never send internal error text to the client.

In Echo v5 the error handler receives the context first, and the predefined
errors such as `echo.ErrUnauthorized` are not `*echo.HTTPError` values. Use
`echo.StatusCode(err)` to read a status from any error:

```go
e.HTTPErrorHandler = func(c *echo.Context, err error) {
    // Runs while the request scope is alive (the default), so scoped
    // services can be resolved here.
    if scope, scopeErr := godi.FromContext(c.Request().Context()); scopeErr == nil {
        godi.MustResolve[*RequestLogger](scope).Error("request failed", "error", err)
    }
    echo.DefaultHTTPErrorHandler(false)(c, err)
}
```

Authentication in a `WithMiddleware` function can respond 401 without
changing how scope-creation failures are rendered:

```go
e.Use(godiecho.ScopeMiddleware(provider,
    godiecho.WithMiddleware(func(scope godi.Scope, c *echo.Context) error {
        return godi.MustResolve[*Session](scope).Authenticate(c.Request())
    }),
    godiecho.WithMiddlewareErrorHandler(func(c *echo.Context, err error) error {
        return echo.ErrUnauthorized
    }),
))
```

## Accessing Scope Manually

```go
e.GET("/custom", func(c *echo.Context) error {
    scope, err := godi.FromContext(c.Request().Context())
    if err != nil {
        return echo.NewHTTPError(http.StatusInternalServerError, "No scope")
    }

    service := godi.MustResolve[*UserService](scope)
    return c.JSON(http.StatusOK, service.GetAll())
})
```

## Migrating from the Echo v4 integration

1. Replace the imports: `github.com/labstack/echo/v4` with `github.com/labstack/echo/v5`, and
   `github.com/junioryono/godi/echo/v6` with `github.com/junioryono/godi/echov5/v6`.
2. Change `echo.Context` to `*echo.Context` in controllers and option callbacks.
3. Swap the parameters of a custom `HTTPErrorHandler` to `func(c *echo.Context, err error)`.

Option names and behavior are unchanged.

---

**See also:** [Integration contract](#integration-contract) | [Echo v4 Integration](echo.md) | [Gin Integration](gin.md) | [Chi Integration](chi.md) | [Fiber v3 Integration](fiber-v3.md) | [net/http Integration](net-http.md)
