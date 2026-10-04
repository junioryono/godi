// Package echo provides godi integration for the Echo web framework
// (github.com/labstack/echo/v4). For Echo v5, use
// github.com/junioryono/godi/echov5/v5.
//
// This package provides middleware for creating request-scoped containers
// and type-safe handler wrappers for resolving controllers.
//
// Example usage:
//
//	provider, _ := collection.Build()
//
//	e := echo.New()
//	e.Use(godiecho.ScopeMiddleware(provider))
//
//	e.POST("/login", godiecho.Handle(AuthController.Login))
//	e.GET("/users/:id", godiecho.Handle(UserController.GetByID))
//
// Default handlers log the cause with log/slog (see WithLogger and
// WithHandlerLogger) and respond with a generic 500 Internal Server Error;
// they never write internal error text to the client.
package echo

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/junioryono/godi/v5"
	"github.com/labstack/echo/v4"
)

// Config holds the configuration for the scope middleware.
type Config struct {
	// ErrorHandler is called when scope creation fails. It also handles
	// middleware failures unless MiddlewareErrorHandler is set.
	// If nil, a default handler that logs the error and returns a generic
	// 500 Internal Server Error is used.
	ErrorHandler func(echo.Context, error) error

	// MiddlewareErrorHandler is called when a function registered with
	// WithMiddleware returns an error. If nil, ErrorHandler is used.
	MiddlewareErrorHandler func(echo.Context, error) error

	// CloseErrorHandler is called when scope closing fails.
	// If nil, errors are logged.
	CloseErrorHandler func(error)

	// Middlewares are functions that run after scope creation.
	// They can be used to initialize request context, set user data, etc.
	Middlewares []func(godi.Scope, echo.Context) error

	// Logger is used by the default handlers. If nil, slog.Default() is used.
	Logger *slog.Logger

	// ErrorPassthrough returns downstream and middleware errors to the outer
	// middleware chain instead of dispatching them through Echo's
	// HTTPErrorHandler while the scope is alive. See WithErrorPassthrough.
	ErrorPassthrough bool
}

// Option configures the scope middleware.
type Option func(*Config)

// WithErrorHandler sets the error handler for scope creation failures. Unless
// WithMiddlewareErrorHandler is also used, it handles middleware failures too.
func WithErrorHandler(h func(echo.Context, error) error) Option {
	return func(c *Config) {
		if h != nil {
			c.ErrorHandler = h
		}
	}
}

// WithMiddlewareErrorHandler sets the error handler for failures returned by
// functions registered with WithMiddleware, such as authentication checks
// that respond 401 or 403. Scope creation failures still go to ErrorHandler.
func WithMiddlewareErrorHandler(h func(echo.Context, error) error) Option {
	return func(c *Config) {
		if h != nil {
			c.MiddlewareErrorHandler = h
		}
	}
}

// WithCloseErrorHandler sets the error handler for scope close failures.
func WithCloseErrorHandler(h func(error)) Option {
	return func(c *Config) {
		if h != nil {
			c.CloseErrorHandler = h
		}
	}
}

// WithMiddleware adds a middleware function that runs after scope creation.
// Multiple middlewares are executed in the order they are added.
func WithMiddleware(mw func(godi.Scope, echo.Context) error) Option {
	return func(c *Config) {
		if mw != nil {
			c.Middlewares = append(c.Middlewares, mw)
		}
	}
}

// WithLogger sets the logger used by the middleware's default handlers.
// A nil logger keeps the default, slog.Default().
func WithLogger(l *slog.Logger) Option {
	return func(c *Config) {
		if l != nil {
			c.Logger = l
		}
	}
}

// WithErrorPassthrough controls what happens to errors returned by the
// handler chain and by WithMiddleware functions.
//
// By default (false) the middleware renders them through Echo's
// HTTPErrorHandler while the request scope is still alive, then returns nil,
// so middleware registered before ScopeMiddleware never sees them.
//
// When enabled, the error is returned to the outer middleware chain instead,
// and Echo renders it after ScopeMiddleware returns. The scope is closed by
// then: an HTTPErrorHandler or outer middleware cannot resolve scoped
// services, and scoped resources have already been disposed.
func WithErrorPassthrough(enabled bool) Option {
	return func(c *Config) {
		c.ErrorPassthrough = enabled
	}
}

func (c *Config) logger() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}

func (c *Config) defaultErrorHandler(_ echo.Context, err error) error {
	c.logger().Error("failed to set up request scope", "error", err)
	return echo.NewHTTPError(http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
}

func (c *Config) defaultCloseErrorHandler(err error) {
	c.logger().Error("failed to close scope", "error", err)
}

func defaultConfig() *Config {
	c := &Config{}
	c.ErrorHandler = c.defaultErrorHandler
	c.CloseErrorHandler = c.defaultCloseErrorHandler
	return c
}

func normalizeConfig(c *Config) {
	if c.ErrorHandler == nil {
		c.ErrorHandler = c.defaultErrorHandler
	}
	if c.MiddlewareErrorHandler == nil {
		c.MiddlewareErrorHandler = c.ErrorHandler
	}
	if c.CloseErrorHandler == nil {
		c.CloseErrorHandler = c.defaultCloseErrorHandler
	}
	// Copy while filtering nils: reslicing in place would mutate a
	// caller-owned slice assigned via a custom option.
	middlewares := make([]func(godi.Scope, echo.Context) error, 0, len(c.Middlewares))
	for _, middleware := range c.Middlewares {
		if middleware != nil {
			middlewares = append(middlewares, middleware)
		}
	}
	c.Middlewares = middlewares
}

// ScopeMiddleware creates an Echo middleware that creates a request-scoped
// container for each request. The scope is attached to the request context
// and can be retrieved using godi.FromContext.
//
// The scope is automatically closed when the request completes.
//
// Errors are consumed by default: errors returned by the handler chain or by
// WithMiddleware functions are dispatched through Echo's HTTPErrorHandler
// while the scope is alive, then this middleware returns nil to prevent
// duplicate handling. Middleware that must inspect returned errors, including
// panic recovery, must therefore run inside this middleware (registered after
// it). Use WithErrorPassthrough(true) to return errors to outer middleware
// instead, at the cost of rendering them after the scope has closed.
//
// Example:
//
//	e := echo.New()
//	e.Use(godiecho.ScopeMiddleware(provider))
func ScopeMiddleware(provider godi.Provider, opts ...Option) echo.MiddlewareFunc {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	normalizeConfig(cfg)

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			scope, err := provider.CreateScope(c.Request().Context())
			if err != nil {
				return cfg.ErrorHandler(c, err)
			}

			defer func() {
				if err := scope.Close(); err != nil {
					cfg.CloseErrorHandler(err)
				}
			}()

			// Attach scope to request context
			c.SetRequest(c.Request().WithContext(scope.Context()))

			// Run middlewares
			for _, mw := range cfg.Middlewares {
				if err := mw(scope, c); err != nil {
					return cfg.dispatchError(c, cfg.MiddlewareErrorHandler(c, err))
				}
			}

			return cfg.dispatchError(c, next(c))
		}
	}
}

func (c *Config) dispatchError(ctx echo.Context, err error) error {
	if err == nil || c.ErrorPassthrough {
		return err
	}
	// Render while request-scoped services are still alive, then consume the
	// error so Echo does not invoke the same handler again after scope teardown.
	ctx.Echo().HTTPErrorHandler(err, ctx)
	return nil
}

// HandlerConfig holds configuration for the Handle wrapper.
type HandlerConfig struct {
	// PanicRecovery enables panic recovery in the handler.
	PanicRecovery bool

	// PanicHandler is called when a panic occurs (if PanicRecovery is true).
	// If nil, a default handler that logs the panic value and stack trace and
	// returns a generic 500 Internal Server Error is used.
	PanicHandler func(echo.Context, any) error

	// ScopeErrorHandler is called when scope retrieval fails.
	ScopeErrorHandler func(echo.Context, error) error

	// ResolutionErrorHandler is called when service resolution fails.
	ResolutionErrorHandler func(echo.Context, error) error

	// Logger is used by the default handlers. If nil, slog.Default() is used.
	Logger *slog.Logger
}

// HandlerOption configures the Handle wrapper.
type HandlerOption func(*HandlerConfig)

// WithPanicRecovery enables or disables panic recovery in the handler.
func WithPanicRecovery(enabled bool) HandlerOption {
	return func(c *HandlerConfig) {
		c.PanicRecovery = enabled
	}
}

// WithPanicHandler sets the handler for panics.
func WithPanicHandler(h func(echo.Context, any) error) HandlerOption {
	return func(c *HandlerConfig) {
		if h != nil {
			c.PanicHandler = h
		}
	}
}

// WithScopeErrorHandler sets the error handler for scope retrieval failures.
func WithScopeErrorHandler(h func(echo.Context, error) error) HandlerOption {
	return func(c *HandlerConfig) {
		if h != nil {
			c.ScopeErrorHandler = h
		}
	}
}

// WithResolutionErrorHandler sets the error handler for service resolution failures.
func WithResolutionErrorHandler(h func(echo.Context, error) error) HandlerOption {
	return func(c *HandlerConfig) {
		if h != nil {
			c.ResolutionErrorHandler = h
		}
	}
}

// WithHandlerLogger sets the logger used by Handle's default handlers.
// A nil logger keeps the default, slog.Default().
func WithHandlerLogger(l *slog.Logger) HandlerOption {
	return func(c *HandlerConfig) {
		if l != nil {
			c.Logger = l
		}
	}
}

func (c *HandlerConfig) logger() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}

func (c *HandlerConfig) defaultPanicHandler(_ echo.Context, v any) error {
	c.logger().Error("panic in handler", "panic", v, "stack", string(debug.Stack()))
	return echo.NewHTTPError(http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
}

func (c *HandlerConfig) defaultScopeErrorHandler(_ echo.Context, err error) error {
	c.logger().Error("failed to get scope from context", "error", err)
	return echo.NewHTTPError(http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
}

func (c *HandlerConfig) defaultResolutionErrorHandler(_ echo.Context, err error) error {
	c.logger().Error("failed to resolve controller", "error", err)
	return echo.NewHTTPError(http.StatusInternalServerError, http.StatusText(http.StatusInternalServerError))
}

func defaultHandlerConfig() *HandlerConfig {
	c := &HandlerConfig{PanicRecovery: false}
	c.PanicHandler = c.defaultPanicHandler
	c.ScopeErrorHandler = c.defaultScopeErrorHandler
	c.ResolutionErrorHandler = c.defaultResolutionErrorHandler
	return c
}

func normalizeHandlerConfig(c *HandlerConfig) {
	if c.PanicHandler == nil {
		c.PanicHandler = c.defaultPanicHandler
	}
	if c.ScopeErrorHandler == nil {
		c.ScopeErrorHandler = c.defaultScopeErrorHandler
	}
	if c.ResolutionErrorHandler == nil {
		c.ResolutionErrorHandler = c.defaultResolutionErrorHandler
	}
}

// Handle wraps a controller method for type-safe resolution from the request scope.
// The controller type T is resolved from the scope attached to the request context.
//
// The method signature should be: func(T, echo.Context) error
//
// Example:
//
//	type UserController interface {
//	    GetByID(echo.Context) error
//	}
//
//	e.GET("/users/:id", godiecho.Handle(UserController.GetByID))
func Handle[T any](method func(T, echo.Context) error, opts ...HandlerOption) echo.HandlerFunc {
	cfg := defaultHandlerConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	normalizeHandlerConfig(cfg)

	return func(c echo.Context) (err error) {
		if cfg.PanicRecovery {
			defer func() {
				if v := recover(); v != nil {
					// http.ErrAbortHandler is the net/http contract for aborting a
					// response mid-write; suppressing it would send a bogus 500 on
					// a deliberately aborted connection.
					if v == http.ErrAbortHandler { //nolint:errorlint // sentinel panic value, compared by identity
						panic(v)
					}
					err = cfg.PanicHandler(c, v)
				}
			}()
		}

		scope, scopeErr := godi.FromContext(c.Request().Context())
		if scopeErr != nil {
			return cfg.ScopeErrorHandler(c, scopeErr)
		}

		controller, resolveErr := godi.Resolve[T](scope)
		if resolveErr != nil {
			return cfg.ResolutionErrorHandler(c, resolveErr)
		}

		return method(controller, c)
	}
}
