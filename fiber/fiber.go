// Package fiber provides godi integration for the Fiber web framework.
//
// This package provides middleware for creating request-scoped containers
// and type-safe handler wrappers for resolving controllers.
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
package fiber

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"
	"github.com/junioryono/godi/v5"
)

// Config holds the configuration for the scope middleware.
type Config struct {
	// ErrorHandler is called when scope creation fails.
	// If nil, the error is returned.
	ErrorHandler func(*fiber.Ctx, error) error

	// CloseErrorHandler is called when scope closing fails.
	// If nil, errors are logged using slog.
	CloseErrorHandler func(error)

	// Middlewares are functions that run after scope creation.
	// They can be used to initialize request context, set user data, etc.
	Middlewares []func(godi.Scope, *fiber.Ctx) error
}

// Option configures the scope middleware.
type Option func(*Config)

// WithErrorHandler sets the error handler for scope creation failures.
func WithErrorHandler(h func(*fiber.Ctx, error) error) Option {
	return func(c *Config) {
		if h != nil {
			c.ErrorHandler = h
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
func WithMiddleware(mw func(godi.Scope, *fiber.Ctx) error) Option {
	return func(c *Config) {
		if mw != nil {
			c.Middlewares = append(c.Middlewares, mw)
		}
	}
}

func defaultConfig() *Config {
	return &Config{
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Internal Server Error",
			})
		},
		CloseErrorHandler: func(err error) {
			slog.Error("failed to close scope", "error", err)
		},
		Middlewares: nil,
	}
}

func normalizeConfig(c *Config) {
	defaults := defaultConfig()
	if c.ErrorHandler == nil {
		c.ErrorHandler = defaults.ErrorHandler
	}
	if c.CloseErrorHandler == nil {
		c.CloseErrorHandler = defaults.CloseErrorHandler
	}
	// Copy while filtering nils: reslicing in place would mutate a
	// caller-owned slice assigned via a custom option.
	middlewares := make([]func(godi.Scope, *fiber.Ctx) error, 0, len(c.Middlewares))
	for _, middleware := range c.Middlewares {
		if middleware != nil {
			middlewares = append(middlewares, middleware)
		}
	}
	c.Middlewares = middlewares
}

// ScopeMiddleware creates a Fiber middleware that creates a request-scoped
// container for each request. The scope is attached to the request's
// UserContext and can be retrieved with godi.FromContext(c.UserContext()).
//
// The scope is automatically closed when the request completes.
// Downstream errors are dispatched through Fiber's ErrorHandler while the
// scope is alive, then consumed to prevent duplicate handling. Middleware that
// must inspect returned errors, including panic recovery, must run inside this
// middleware in the request chain.
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

	return func(c *fiber.Ctx) error {
		scope, err := provider.CreateScope(c.UserContext())
		if err != nil {
			return cfg.ErrorHandler(c, err)
		}

		// Close via defer so the scope is released even when a handler panics.
		defer func() {
			if closeErr := scope.Close(); closeErr != nil {
				cfg.CloseErrorHandler(closeErr)
			}
		}()

		// Attach the scope's context as the request's UserContext so it is
		// reachable via godi.FromContext(c.UserContext()) — the same
		// context-based access the other integrations use — and so it
		// propagates to frameworks layered on top (e.g. Huma).
		c.SetUserContext(scope.Context())

		// Run middlewares
		for _, mw := range cfg.Middlewares {
			if err := mw(scope, c); err != nil {
				return dispatchError(c, cfg.ErrorHandler(c, err))
			}
		}

		// Execute handler chain
		return dispatchError(c, c.Next())
	}
}

func dispatchError(c *fiber.Ctx, err error) error {
	if err == nil {
		return nil
	}
	// Render while request-scoped services are still alive, then consume the
	// error so Fiber does not invoke the same handler again after scope teardown.
	if handlerErr := c.App().ErrorHandler(c, err); handlerErr != nil {
		// Match Fiber's native fallback: a configured error-handler failure is
		// rendered as a generic 500, never as the handler's internal error text.
		_ = fiber.DefaultErrorHandler(c, fiber.ErrInternalServerError)
	}
	return nil
}

// HandlerConfig holds configuration for the Handle wrapper.
type HandlerConfig struct {
	// PanicRecovery enables panic recovery in the handler.
	PanicRecovery bool

	// PanicHandler is called when a panic occurs (if PanicRecovery is true).
	PanicHandler func(*fiber.Ctx, any) error

	// ScopeErrorHandler is called when scope retrieval fails.
	ScopeErrorHandler func(*fiber.Ctx, error) error

	// ResolutionErrorHandler is called when service resolution fails.
	ResolutionErrorHandler func(*fiber.Ctx, error) error
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
func WithPanicHandler(h func(*fiber.Ctx, any) error) HandlerOption {
	return func(c *HandlerConfig) {
		if h != nil {
			c.PanicHandler = h
		}
	}
}

// WithScopeErrorHandler sets the error handler for scope retrieval failures.
func WithScopeErrorHandler(h func(*fiber.Ctx, error) error) HandlerOption {
	return func(c *HandlerConfig) {
		if h != nil {
			c.ScopeErrorHandler = h
		}
	}
}

// WithResolutionErrorHandler sets the error handler for service resolution failures.
func WithResolutionErrorHandler(h func(*fiber.Ctx, error) error) HandlerOption {
	return func(c *HandlerConfig) {
		if h != nil {
			c.ResolutionErrorHandler = h
		}
	}
}

func defaultHandlerConfig() *HandlerConfig {
	return &HandlerConfig{
		PanicRecovery: false,
		PanicHandler: func(c *fiber.Ctx, v any) error {
			slog.Error("panic in handler", "panic", v)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Internal Server Error",
			})
		},
		ScopeErrorHandler: func(c *fiber.Ctx, err error) error {
			slog.Error("failed to get scope from context", "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Internal Server Error",
			})
		},
		ResolutionErrorHandler: func(c *fiber.Ctx, err error) error {
			slog.Error("failed to resolve controller", "error", err)
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Internal Server Error",
			})
		},
	}
}

func normalizeHandlerConfig(c *HandlerConfig) {
	defaults := defaultHandlerConfig()
	if c.PanicHandler == nil {
		c.PanicHandler = defaults.PanicHandler
	}
	if c.ScopeErrorHandler == nil {
		c.ScopeErrorHandler = defaults.ScopeErrorHandler
	}
	if c.ResolutionErrorHandler == nil {
		c.ResolutionErrorHandler = defaults.ResolutionErrorHandler
	}
}

// Handle wraps a controller method for type-safe resolution from the request scope.
// The controller type T is resolved from the scope on the request's UserContext.
//
// The method signature should be: func(T, *fiber.Ctx) error
//
// Example:
//
//	type UserController interface {
//	    GetByID(*fiber.Ctx) error
//	}
//
//	app.Get("/users/:id", godifiber.Handle(UserController.GetByID))
func Handle[T any](method func(T, *fiber.Ctx) error, opts ...HandlerOption) fiber.Handler {
	cfg := defaultHandlerConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	normalizeHandlerConfig(cfg)

	return func(c *fiber.Ctx) (err error) {
		if cfg.PanicRecovery {
			defer func() {
				if v := recover(); v != nil {
					err = cfg.PanicHandler(c, v)
				}
			}()
		}

		scope, scopeErr := godi.FromContext(c.UserContext())
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
