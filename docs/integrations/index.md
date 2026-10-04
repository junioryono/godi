# Integrations

Each router integration (net/http, Chi, Echo, Fiber, Gin) creates and closes
request scopes around its framework's handler lifecycle. The Huma integration
resolves controllers from the scope that the underlying router's scope
middleware creates.

- [net/http](net-http.md)
- [Chi](chi.md)
- [Echo v4](echo.md)
- [Echo v5](echo-v5.md)
- [Fiber v2](fiber.md)
- [Fiber v3](fiber-v3.md)
- [Gin](gin.md)
- [Huma](huma.md)

| Framework | Module                                  | Package name | Retrieve the scope with                     |
| --------- | --------------------------------------- | ------------ | ------------------------------------------- |
| net/http  | `github.com/junioryono/godi/http/v6`    | `http`       | `godi.FromContext(r.Context())`             |
| Chi       | `github.com/junioryono/godi/chi/v6`     | `chi`        | `godi.FromContext(r.Context())`             |
| Gin       | `github.com/junioryono/godi/gin/v6`     | `gin`        | `godi.FromContext(c.Request.Context())`     |
| Echo v4   | `github.com/junioryono/godi/echo/v6`    | `echo`       | `godi.FromContext(c.Request().Context())`   |
| Echo v5   | `github.com/junioryono/godi/echov5/v6`  | `echov5`     | `godi.FromContext(c.Request().Context())`   |
| Fiber v2  | `github.com/junioryono/godi/fiber/v6`   | `fiber`      | `godi.FromContext(c.UserContext())`         |
| Fiber v3  | `github.com/junioryono/godi/fiberv3/v6` | `fiberv3`    | `godi.FromContext(c.Context())`             |
| Huma      | `github.com/junioryono/godi/huma/v6`    | `huma`       | `godi.FromContext(ctx)` in the handler      |

The trailing `/v5` in every module path is godi's major version. `echov5/v5`
is godi v5's integration for Echo v5, and `fiberv3/v5` is the one for Fiber v3.
The Chi module is a thin facade over the net/http module: its types are
aliases of the `godihttp` types.

(integration-contract)=

## Integration Contract

Every router integration (net/http, Chi, Gin, Echo v4/v5, Fiber v2/v3)
provides the same two pieces and follows the same rules. A shared conformance
suite (`integrationtests/conformance_test.go`) runs these rules against every
adapter.

**`ScopeMiddleware(provider, opts...)`** creates one scope per request, attaches
it to the request context, runs any `WithMiddleware` functions, calls the next
handler, and closes the scope after the handler returns. Scopes are isolated
between requests.

**`Handle(method, opts...)`** resolves the controller from the request scope
and calls the method with it.

### Error handling

| Failure                            | Handler (option)                                         | Default                                    |
| ---------------------------------- | -------------------------------------------------------- | ------------------------------------------ |
| Scope creation fails               | `ErrorHandler` (`WithErrorHandler`)                      | log the cause, generic 500                 |
| A `WithMiddleware` function fails  | `MiddlewareErrorHandler` (`WithMiddlewareErrorHandler`)  | the `ErrorHandler`                         |
| Scope close fails                  | `CloseErrorHandler` (`WithCloseErrorHandler`)            | log the cause                              |
| No scope in `Handle`               | `ScopeErrorHandler` (`WithScopeErrorHandler`)            | log the cause, generic 500                 |
| Controller resolution fails        | `ResolutionErrorHandler` (`WithResolutionErrorHandler`)  | log the cause, generic 500                 |
| Handler panics (opt-in)            | `PanicHandler` (`WithPanicRecovery`, `WithPanicHandler`) | log the panic value and stack, generic 500 |

1. **Default handlers log, then respond generically.** Every default handler
   logs the underlying cause with `log/slog` at error level, then renders a
   generic 500 Internal Server Error.
2. **Middleware errors have their own handler.** `WithMiddlewareErrorHandler`
   handles errors returned by `WithMiddleware` functions, so an authentication
   step can respond 401 or 403 without changing how scope-creation failures
   are rendered. If it is not set, middleware errors go to the `ErrorHandler`,
   as before.
3. **The logger is configurable.** `WithLogger(*slog.Logger)` sets the logger
   for `ScopeMiddleware`'s default handlers, and `WithHandlerLogger(*slog.Logger)`
   sets it for `Handle`'s default handlers (two names because Go has no
   overloading: they configure different option types). Both default to
   `slog.Default()`, looked up when a handler runs.
4. **Panics are logged with their stack.** With `WithPanicRecovery(true)`, the
   default panic handler logs the panic value and `debug.Stack()`. The net/http,
   Chi, Gin and Echo integrations re-panic `http.ErrAbortHandler`, which
   net/http uses to abort a response deliberately.
5. **No default handler writes internal error text to the client.** Causes go
   to the log only. Huma additionally sanitizes controller errors: anything
   that is not a `huma.StatusError` becomes a generic 500.

```go
authenticate := func(scope godi.Scope, r *http.Request) error {
    session := godi.MustResolve[*Session](scope)
    return session.Authenticate(r.Header.Get("Authorization"))
}

handler := godihttp.ScopeMiddleware(provider,
    godihttp.WithLogger(logger),
    godihttp.WithMiddleware(authenticate),
    godihttp.WithMiddlewareErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
        http.Error(w, "Unauthorized", http.StatusUnauthorized)
    }),
)(mux)
```

(echo-and-fiber-errors-are-consumed-inside-the-scope)=

### Echo and Fiber: errors are consumed inside the scope

```{important}
In the Echo (v4 and v5) and Fiber (v2 and v3) integrations, `ScopeMiddleware`
**consumes** errors by default. When the handler chain or a `WithMiddleware`
function returns an error, the middleware dispatches it through the
framework's error handler (Echo's `HTTPErrorHandler`, Fiber's `ErrorHandler`)
while the request scope is still alive, then returns `nil`.

Two consequences:

- Your error handler can resolve request-scoped services (a request logger, a
  request ID) because the scope has not closed yet.
- Middleware registered **before** `ScopeMiddleware` never sees the error. Register
  middleware that inspects returned errors, including panic recovery and
  request logging, **after** `ScopeMiddleware` so it runs inside the scope.
```

To return errors to outer middleware instead, opt in with
`WithErrorPassthrough(true)`:

```go
e.Use(errorReporting)                    // now sees the returned error
e.Use(godiecho.ScopeMiddleware(provider, // returns errors instead of consuming them
    godiecho.WithErrorPassthrough(true),
))
```

The trade-off: with passthrough, the framework renders the error after
`ScopeMiddleware` has returned, so **the request scope is already closed when
the error is rendered**. An error handler or outer middleware cannot resolve
scoped services, and scoped resources (database transactions, request
loggers) have already been disposed. Use passthrough only when the code that
handles the error does not need the scope.

Fiber has one more rule: when the response body is a stream (`SendStream`),
the scope stays open until fasthttp has written the response, because the
stream is usually a scoped resource.

The net/http, Chi and Gin integrations do not need this rule: their handlers
write the response directly, so errors never travel back through the scope
middleware.
