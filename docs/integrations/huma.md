# Huma Integration

Guide for using godi with the [Huma](https://github.com/danielgtaylor/huma) REST API framework.

Huma is router-agnostic: it runs on top of an adapter (`humago`, `humachi`,
`humagin`, `humaecho`, `humafiber`) and propagates the underlying request's
`context.Context` to your operation handlers. godi builds on that — the request
scope is created by the **router's** `ScopeMiddleware`, and Huma carries it
through, so `godi.FromContext` works inside Huma handlers.

So the `godi/huma/v5` package provides only the Huma-specific piece: a type-safe
controller wrapper for `huma.Register`. Scope creation comes from the router
integration (`godigin`, `godichi`, `godihttp`, `godiecho`, `godifiber`).

## Installation

```bash
go get github.com/junioryono/godi/v5
go get github.com/junioryono/godi/huma/v5
# plus the router integration you mount Huma on, e.g.:
go get github.com/junioryono/godi/gin/v5
```

## Quick Start

```go
package main

import (
    "context"
    "net/http"

    "github.com/danielgtaylor/huma/v2"
    "github.com/danielgtaylor/huma/v2/adapters/humagin"
    "github.com/gin-gonic/gin"
    "github.com/junioryono/godi/v5"
    godigin "github.com/junioryono/godi/gin/v5"
    godihuma "github.com/junioryono/godi/huma/v5"
)

type GreetInput struct {
    Name string `path:"name"`
}

type GreetOutput struct {
    Body struct {
        Message string `json:"message"`
    }
}

type UserController struct{}

func NewUserController() *UserController { return &UserController{} }

func (c *UserController) Greet(ctx context.Context, in *GreetInput) (*GreetOutput, error) {
    out := &GreetOutput{}
    out.Body.Message = "hello " + in.Name
    return out, nil
}

func main() {
    services := godi.NewCollection()
    services.AddScoped(NewUserController)

    provider, _ := services.Build()
    defer provider.Close()

    g := gin.New()
    // 1. The router middleware owns the request scope.
    g.Use(godigin.ScopeMiddleware(provider))

    // 2. Mount Huma on the router.
    api := humagin.New(g, huma.DefaultConfig("My API", "1.0.0"))

    // 3. Register operations against DI-resolved controllers.
    huma.Register(api, huma.Operation{
        OperationID: "greet",
        Method:      http.MethodGet,
        Path:        "/greet/{name}",
    }, godihuma.Handle((*UserController).Greet))

    g.Run(":8080")
}
```

## How `Handle` works

`godihuma.Handle` adapts a controller method to Huma's registration signature.
Pass the method as a **method expression** so the receiver becomes the first
parameter:

```go
huma.Register(api, op, godihuma.Handle((*UserController).Greet))
```

For each request it:

1. Reads the scope from the request context (`godi.FromContext(ctx)`).
2. Resolves the controller `C` from that scope.
3. Calls `method(controller, ctx, in)`.

A failure to resolve the scope or controller returns a generic 500 by default.
For controller errors, `huma.StatusError` values are preserved and unexpected
plain errors are logged server-side and sanitized to a generic 500.

## Resolving services directly

Inside a handler the `ctx` argument is the request context, and inside Huma
middleware you have `humaCtx.Context()`. Use the standard godi helpers — there
is no Huma-specific accessor:

```go
func (c *UserController) Greet(ctx context.Context, in *GreetInput) (*GreetOutput, error) {
    scope, err := godi.FromContext(ctx)
    if err != nil {
        return nil, err
    }
    svc, err := godi.Resolve[*UserService](scope)
    // ...
}
```

## Mapping domain errors

To translate domain errors into HTTP responses for every wrapped handler, pass
`WithErrorMapper`:

```go
mapErr := func(err error) error {
    if errors.Is(err, sql.ErrNoRows) {
        return huma.Error404NotFound("not found")
    }
    return err
}

huma.Register(api, op, godihuma.Handle((*UserController).Greet, godihuma.WithErrorMapper(mapErr)))
```

Only mapped `huma.StatusError` values are sent to clients. A mapper result that
is still a plain error is treated as internal, logged, and replaced with a
generic 500 response.

## Huma-level middleware

Auth, logging, and other per-operation middleware use Huma's **native**
`api.UseMiddleware` — godi does not wrap it. The scope is already on the
context, so middleware can resolve services too:

```go
api.UseMiddleware(func(ctx huma.Context, next func(huma.Context)) {
    scope, err := godi.FromContext(ctx.Context())
    if err != nil {
        huma.WriteErr(api, ctx, http.StatusInternalServerError, "internal error", err)
        return
    }
    // ... inspect/resolve services, then continue or short-circuit
    next(ctx)
})
```
