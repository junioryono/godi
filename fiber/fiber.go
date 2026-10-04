// Package fiber provides godi integration for Fiber v3
// (github.com/gofiber/fiber/v3).
//
// This package provides middleware for creating request-scoped containers
// and type-safe handler wrappers for resolving controllers. Handlers take
// the fiber.Ctx interface. The scope is attached to the request context set with Ctx.SetContext, so it is
// retrieved with godi.FromContext(c.Context()).
//
// Example usage:
//
//	provider, _ := collection.Build()
//
//	app := fiber.New()
//	app.Use(godifiber.ScopeMiddleware(provider))
//
//	app.Post("/login", godifiber.Handle(AuthController.Login))
//	app.Get("/users/:id", godifiber.Handle(UserController.GetByID))
//
// Default handlers log the cause with log/slog (see WithLogger and
// WithHandlerLogger) and respond with a generic 500 Internal Server Error;
// they never write internal error text to the client.
package fiber

import (
	"log/slog"
	"runtime/debug"
	"sync"

	"github.com/gofiber/fiber/v3"
	"github.com/junioryono/godi/v6"
)

// Config holds the configuration for the scope middleware.
type Config struct {
	// ErrorHandler is called when scope creation fails. It also handles
	// middleware failures unless MiddlewareErrorHandler is set.
	// If nil, a default handler that logs the error and returns a generic
	// 500 Internal Server Error is used.
	ErrorHandler func(fiber.Ctx, error) error

	// MiddlewareErrorHandler is called when a function registered with
	// WithMiddleware returns an error. If nil, ErrorHandler is used.
	MiddlewareErrorHandler func(fiber.Ctx, error) error

	// CloseErrorHandler is called when scope closing fails.
	// If nil, errors are logged.
	CloseErrorHandler func(error)

	// Middlewares are functions that run after scope creation.
	// They can be used to initialize request context, set user data, etc.
	Middlewares []func(godi.Scope, fiber.Ctx) error

	// Logger is used by the default handlers. If nil, slog.Default() is used.
	Logger *slog.Logger

	// ErrorPassthrough returns downstream and middleware errors to the outer
	// handler chain instead of dispatching them through Fiber's ErrorHandler
	// while the scope is alive. See WithErrorPassthrough.
	ErrorPassthrough bool
}

// Option configures the scope middleware.
type Option func(*Config)

// WithErrorHandler sets the error handler for scope creation failures. Unless
// WithMiddlewareErrorHandler is also used, it handles middleware failures too.
func WithErrorHandler(h func(fiber.Ctx, error) error) Option {
	return func(c *Config) {
		if h != nil {
			c.ErrorHandler = h
		}
	}
}

// WithMiddlewareErrorHandler sets the error handler for failures returned by
// functions registered with WithMiddleware, such as authentication checks
// that respond 401 or 403. Scope creation failures still go to ErrorHandler.
func WithMiddlewareErrorHandler(h func(fiber.Ctx, error) error) Option {
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
func WithMiddleware(mw func(godi.Scope, fiber.Ctx) error) Option {
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
// By default (false) the middleware renders them through Fiber's ErrorHandler
// while the request scope is still alive, then returns nil, so handlers
// registered before ScopeMiddleware never see them.
//
// When enabled, the error is returned to the outer handler chain instead, and
// Fiber renders it after ScopeMiddleware returns. The scope is closed by then
// (unless the response is streamed): an ErrorHandler or outer middleware
// cannot resolve scoped services, and scoped resources have already been
// disposed.
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

func internalServerError(ctx fiber.Ctx) error {
	return ctx.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
		"error": "Internal Server Error",
	})
}

func (c *Config) defaultErrorHandler(ctx fiber.Ctx, err error) error {
	c.logger().Error("failed to set up request scope", "error", err)
	return internalServerError(ctx)
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
	middlewares := make([]func(godi.Scope, fiber.Ctx) error, 0, len(c.Middlewares))
	for _, middleware := range c.Middlewares {
		if middleware != nil {
			middlewares = append(middlewares, middleware)
		}
	}
	c.Middlewares = middlewares
}

// ScopeMiddleware creates a Fiber middleware that creates a request-scoped
// container for each request. The scope's context becomes the request context
// (Ctx.SetContext) and can be retrieved with godi.FromContext(c.Context()).
//
// The scope is automatically closed when the request completes, or, for a
// streamed response body, once fasthttp has written the response.
//
// Errors are consumed by default: errors returned by the handler chain or by
// WithMiddleware functions are dispatched through Fiber's ErrorHandler while
// the scope is alive, then this middleware returns nil to prevent duplicate
// handling. Middleware that must inspect returned errors, including panic
// recovery, must therefore run inside this middleware (registered after it).
// Use WithErrorPassthrough(true) to return errors to outer handlers instead,
// at the cost of rendering them after the scope has closed.
//
// Example:
//
//	app := fiber.New()
//	app.Use(godifiber.ScopeMiddleware(provider))
func ScopeMiddleware(provider godi.Provider, opts ...Option) fiber.Handler {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	normalizeConfig(cfg)

	return func(c fiber.Ctx) error {
		scope, err := provider.CreateScope(c.Context())
		if err != nil {
			return cfg.ErrorHandler(c, err)
		}

		closeScope := func() {
			if closeErr := scope.Close(); closeErr != nil {
				cfg.CloseErrorHandler(closeErr)
			}
		}

		// Close via defer so the scope is released even when a handler panics.
		// A streamed response body (c.SendStream, c.SendStreamWriter) is
		// written by fasthttp only after the handler chain returns, and it is
		// typically a scoped resource, so then the scope is kept open until
		// the response is done.
		defer func() {
			if c.Response().IsBodyStream() {
				deferScopeClose(c, closeScope)
				return
			}
			closeScope()
		}()

		// Attach the scope's context as the request context so it is
		// reachable via godi.FromContext(c.Context()) — the same context-based
		// access the other integrations use — and so it propagates to
		// frameworks layered on top (e.g. Huma). Fiber clears it when the
		// pooled Ctx is released.
		c.SetContext(scope.Context())

		// Run middlewares
		for _, mw := range cfg.Middlewares {
			if err := mw(scope, c); err != nil {
				return cfg.dispatchError(c, cfg.MiddlewareErrorHandler(c, err))
			}
		}

		// Execute handler chain
		return cfg.dispatchError(c, c.Next())
	}
}

// scopeCloserKey is the request user-value key of a deferred scope close.
type scopeCloserKey struct{}

// scopeCloser closes a request scope when fasthttp releases the request.
type scopeCloser struct {
	once  sync.Once
	close func()
}

func (s *scopeCloser) Close() error {
	s.once.Do(s.close)
	return nil
}

// deferScopeClose closes the scope once fasthttp has written the response.
// Fiber v3 still runs on fasthttp, which closes request user values that
// implement io.Closer when it resets the request: after the response
// (including a body stream) has been written, or when the connection fails.
func deferScopeClose(c fiber.Ctx, closeScope func()) {
	c.RequestCtx().SetUserValue(scopeCloserKey{}, &scopeCloser{close: closeScope})
}

func (c *Config) dispatchError(ctx fiber.Ctx, err error) error {
	if err == nil || c.ErrorPassthrough {
		return err
	}
	// Render while request-scoped services are still alive, then consume the
	// error so Fiber does not invoke the same handler again after scope teardown.
	if handlerErr := ctx.App().ErrorHandler(ctx, err); handlerErr != nil {
		// Match Fiber's native fallback: a configured error-handler failure is
		// rendered as a generic 500, never as the handler's internal error text.
		c.logger().Error("fiber error handler failed", "error", handlerErr)
		_ = fiber.DefaultErrorHandler(ctx, fiber.ErrInternalServerError)
	}
	return nil
}

// HandlerConfig holds configuration for the Handle wrapper.
type HandlerConfig struct {
	// PanicRecovery enables panic recovery in the handler.
	PanicRecovery bool

	// PanicHandler is called when a panic occurs (if PanicRecovery is true).
	// If nil, a default handler that logs the panic value and stack trace and
	// returns a generic 500 Internal Server Error is used.
	PanicHandler func(fiber.Ctx, any) error

	// ScopeErrorHandler is called when scope retrieval fails.
	ScopeErrorHandler func(fiber.Ctx, error) error

	// ResolutionErrorHandler is called when service resolution fails.
	ResolutionErrorHandler func(fiber.Ctx, error) error

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
func WithPanicHandler(h func(fiber.Ctx, any) error) HandlerOption {
	return func(c *HandlerConfig) {
		if h != nil {
			c.PanicHandler = h
		}
	}
}

// WithScopeErrorHandler sets the error handler for scope retrieval failures.
func WithScopeErrorHandler(h func(fiber.Ctx, error) error) HandlerOption {
	return func(c *HandlerConfig) {
		if h != nil {
			c.ScopeErrorHandler = h
		}
	}
}

// WithResolutionErrorHandler sets the error handler for service resolution failures.
func WithResolutionErrorHandler(h func(fiber.Ctx, error) error) HandlerOption {
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

func (c *HandlerConfig) defaultPanicHandler(ctx fiber.Ctx, v any) error {
	c.logger().Error("panic in handler", "panic", v, "stack", string(debug.Stack()))
	return internalServerError(ctx)
}

func (c *HandlerConfig) defaultScopeErrorHandler(ctx fiber.Ctx, err error) error {
	c.logger().Error("failed to get scope from context", "error", err)
	return internalServerError(ctx)
}

func (c *HandlerConfig) defaultResolutionErrorHandler(ctx fiber.Ctx, err error) error {
	c.logger().Error("failed to resolve controller", "error", err)
	return internalServerError(ctx)
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
// The controller type T is resolved from the scope on the request context.
//
// The method signature should be: func(T, fiber.Ctx) error
//
// Example:
//
//	type UserController interface {
//	    GetByID(fiber.Ctx) error
//	}
//
//	app.Get("/users/:id", godifiber.Handle(UserController.GetByID))
func Handle[T any](method func(T, fiber.Ctx) error, opts ...HandlerOption) fiber.Handler {
	cfg := defaultHandlerConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	normalizeHandlerConfig(cfg)

	return func(c fiber.Ctx) (err error) {
		if cfg.PanicRecovery {
			defer func() {
				if v := recover(); v != nil {
					err = cfg.PanicHandler(c, v)
				}
			}()
		}

		scope, scopeErr := godi.FromContext(c.Context())
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
