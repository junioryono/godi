// Package gin provides godi integration for the Gin web framework.
//
// This package provides middleware for creating request-scoped containers
// and type-safe handler wrappers for resolving controllers.
//
// Example usage:
//
//	provider, _ := collection.Build()
//
//	g := gin.New()
//	g.Use(godigin.ScopeMiddleware(provider))
//
//	g.POST("/login", godigin.Handle(AuthController.Login))
//	g.GET("/users/:id", godigin.Handle(UserController.GetByID))
//
// Default handlers log the cause with log/slog (see WithLogger and
// WithHandlerLogger) and respond with a generic 500 Internal Server Error;
// they never write internal error text to the client.
package gin

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"github.com/junioryono/godi/v6"
)

// Config holds the configuration for the scope middleware.
type Config struct {
	// ErrorHandler is called when scope creation fails. It also handles
	// middleware failures unless MiddlewareErrorHandler is set.
	// If nil, a default handler that logs the error and returns a generic
	// 500 Internal Server Error is used.
	ErrorHandler func(*gin.Context, error)

	// MiddlewareErrorHandler is called when a function registered with
	// WithMiddleware returns an error. If nil, ErrorHandler is used.
	MiddlewareErrorHandler func(*gin.Context, error)

	// CloseErrorHandler is called when scope closing fails.
	// If nil, errors are logged.
	CloseErrorHandler func(error)

	// Middlewares are functions that run after scope creation.
	// They can be used to initialize request context, set user claims, etc.
	Middlewares []func(godi.Scope, *gin.Context) error

	// Logger is used by the default handlers. If nil, slog.Default() is used.
	Logger *slog.Logger
}

// Option configures the scope middleware.
type Option func(*Config)

// WithErrorHandler sets the error handler for scope creation failures. Unless
// WithMiddlewareErrorHandler is also used, it handles middleware failures too.
func WithErrorHandler(h func(*gin.Context, error)) Option {
	return func(c *Config) {
		if h != nil {
			c.ErrorHandler = h
		}
	}
}

// WithMiddlewareErrorHandler sets the error handler for failures returned by
// functions registered with WithMiddleware, such as authentication checks
// that respond 401 or 403. Scope creation failures still go to ErrorHandler.
func WithMiddlewareErrorHandler(h func(*gin.Context, error)) Option {
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
//
// Example:
//
//	godigin.ScopeMiddleware(provider,
//	    godigin.WithMiddleware(func(scope godi.Scope, c *gin.Context) error {
//	        reqCtx := godi.MustResolve[*request.Context](scope)
//	        reqCtx.SetGinContext(c)
//	        return nil
//	    }),
//	)
func WithMiddleware(mw func(godi.Scope, *gin.Context) error) Option {
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

func (c *Config) logger() *slog.Logger {
	if c.Logger != nil {
		return c.Logger
	}
	return slog.Default()
}

func (c *Config) defaultErrorHandler(ctx *gin.Context, err error) {
	c.logger().Error("failed to set up request scope", "error", err)
	ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
		"error": "Internal Server Error",
	})
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
	middlewares := make([]func(godi.Scope, *gin.Context) error, 0, len(c.Middlewares))
	for _, middleware := range c.Middlewares {
		if middleware != nil {
			middlewares = append(middlewares, middleware)
		}
	}
	c.Middlewares = middlewares
}

// ScopeMiddleware creates a gin.HandlerFunc that creates a request-scoped
// container for each request. The scope is attached to the request context
// and can be retrieved using godi.FromContext.
//
// The scope is automatically closed when the request completes. Terminal
// errors (scope creation or configured middleware failures) abort the handler
// chain before the configured ErrorHandler or MiddlewareErrorHandler renders
// the response, so later handlers never run without a scope. Error handlers
// run while the scope is still alive.
//
// Example:
//
//	g := gin.New()
//	g.Use(godigin.ScopeMiddleware(provider))
func ScopeMiddleware(provider godi.Provider, opts ...Option) gin.HandlerFunc {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	normalizeConfig(cfg)

	return func(c *gin.Context) {
		scope, err := provider.CreateScope(c.Request.Context())
		if err != nil {
			c.Abort()
			cfg.ErrorHandler(c, err)
			return
		}

		defer func() {
			if err := scope.Close(); err != nil {
				cfg.CloseErrorHandler(err)
			}
		}()

		// Attach scope to request context
		c.Request = c.Request.WithContext(scope.Context())

		// Run middlewares
		for _, mw := range cfg.Middlewares {
			if err := mw(scope, c); err != nil {
				c.Abort()
				cfg.MiddlewareErrorHandler(c, err)
				return
			}
		}

		c.Next()
	}
}

// HandlerConfig holds configuration for the Handle wrapper.
type HandlerConfig struct {
	// PanicRecovery enables panic recovery in the handler.
	// If true, panics are caught and handled by PanicHandler.
	PanicRecovery bool

	// PanicHandler is called when a panic occurs (if PanicRecovery is true).
	// If nil, a default handler that logs the panic value and stack trace and
	// returns a generic 500 Internal Server Error is used.
	PanicHandler func(*gin.Context, any)

	// ScopeErrorHandler is called when scope retrieval fails.
	// If nil, a default handler returning 500 Internal Server Error is used.
	ScopeErrorHandler func(*gin.Context, error)

	// ResolutionErrorHandler is called when service resolution fails.
	// If nil, a default handler returning 500 Internal Server Error is used.
	ResolutionErrorHandler func(*gin.Context, error)

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

// WithPanicHandler sets the handler for panics (requires WithPanicRecovery(true)).
func WithPanicHandler(h func(*gin.Context, any)) HandlerOption {
	return func(c *HandlerConfig) {
		if h != nil {
			c.PanicHandler = h
		}
	}
}

// WithScopeErrorHandler sets the error handler for scope retrieval failures.
func WithScopeErrorHandler(h func(*gin.Context, error)) HandlerOption {
	return func(c *HandlerConfig) {
		if h != nil {
			c.ScopeErrorHandler = h
		}
	}
}

// WithResolutionErrorHandler sets the error handler for service resolution failures.
func WithResolutionErrorHandler(h func(*gin.Context, error)) HandlerOption {
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

func (c *HandlerConfig) defaultPanicHandler(ctx *gin.Context, v any) {
	c.logger().Error("panic in handler", "panic", v, "stack", string(debug.Stack()))
	ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
		"error": "Internal Server Error",
	})
}

func (c *HandlerConfig) defaultScopeErrorHandler(ctx *gin.Context, err error) {
	c.logger().Error("failed to get scope from context", "error", err)
	ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
		"error": "Internal Server Error",
	})
}

func (c *HandlerConfig) defaultResolutionErrorHandler(ctx *gin.Context, err error) {
	c.logger().Error("failed to resolve controller", "error", err)
	ctx.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
		"error": "Internal Server Error",
	})
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
// The method signature should be: func(T, *gin.Context)
//
// Example:
//
//	type UserController interface {
//	    GetByID(*gin.Context)
//	}
//
//	g.GET("/users/:id", godigin.Handle(UserController.GetByID))
func Handle[T any](method func(T, *gin.Context), opts ...HandlerOption) gin.HandlerFunc {
	cfg := defaultHandlerConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	normalizeHandlerConfig(cfg)

	return func(c *gin.Context) {
		if cfg.PanicRecovery {
			defer func() {
				if r := recover(); r != nil {
					// http.ErrAbortHandler is the net/http contract for aborting a
					// response mid-write; suppressing it would send a bogus 500 on
					// a deliberately aborted connection.
					if r == http.ErrAbortHandler { //nolint:errorlint // sentinel panic value, compared by identity
						panic(r)
					}
					c.Abort()
					cfg.PanicHandler(c, r)
				}
			}()
		}

		scope, err := godi.FromContext(c.Request.Context())
		if err != nil {
			c.Abort()
			cfg.ScopeErrorHandler(c, err)
			return
		}

		controller, err := godi.Resolve[T](scope)
		if err != nil {
			c.Abort()
			cfg.ResolutionErrorHandler(c, err)
			return
		}

		method(controller, c)
	}
}
