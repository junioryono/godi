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
	"github.com/junioryono/godi/v4"
)

// scopeKey is the key used to store the scope in fiber.Ctx.Locals
const scopeKey = "godi_scope"

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
		c.ErrorHandler = h
	}
}

// WithCloseErrorHandler sets the error handler for scope close failures.
func WithCloseErrorHandler(h func(error)) Option {
	return func(c *Config) {
		c.CloseErrorHandler = h
	}
}

// WithMiddleware adds a middleware function that runs after scope creation.
// Multiple middlewares are executed in the order they are added.
func WithMiddleware(mw func(godi.Scope, *fiber.Ctx) error) Option {
	return func(c *Config) {
		c.Middlewares = append(c.Middlewares, mw)
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

// ScopeMiddleware creates a Fiber middleware that creates a request-scoped
// container for each request. The scope is stored in fiber.Ctx.Locals
// and attached to the UserContext.
//
// The scope is automatically closed when the request completes.
//
// Example:
//
//	app := fiber.New()
//	app.Use(godifiber.ScopeMiddleware(provider))
func ScopeMiddleware(provider godi.Provider, opts ...Option) fiber.Handler {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	return func(c *fiber.Ctx) error {
		scope, err := provider.CreateScope(c.UserContext())
		if err != nil {
			return cfg.ErrorHandler(c, err)
		}

		// Store scope in context and locals
		c.SetUserContext(scope.Context())
		c.Locals(scopeKey, scope)

		// Run middlewares
		for _, mw := range cfg.Middlewares {
			if err := mw(scope, c); err != nil {
				scope.Close()
				return cfg.ErrorHandler(c, err)
			}
		}

		// Execute handler chain
		err = c.Next()

		// Close scope after request completes
		if closeErr := scope.Close(); closeErr != nil {
			cfg.CloseErrorHandler(closeErr)
		}

		return err
	}
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
		c.PanicHandler = h
	}
}

// WithScopeErrorHandler sets the error handler for scope retrieval failures.
func WithScopeErrorHandler(h func(*fiber.Ctx, error) error) HandlerOption {
	return func(c *HandlerConfig) {
		c.ScopeErrorHandler = h
	}
}

// WithResolutionErrorHandler sets the error handler for service resolution failures.
func WithResolutionErrorHandler(h func(*fiber.Ctx, error) error) HandlerOption {
	return func(c *HandlerConfig) {
		c.ResolutionErrorHandler = h
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

// Handle wraps a controller method for type-safe resolution from the request scope.
// The controller type T is resolved from the scope stored in fiber.Ctx.Locals.
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
		opt(cfg)
	}

	return func(c *fiber.Ctx) (err error) {
		if cfg.PanicRecovery {
			defer func() {
				if v := recover(); v != nil {
					err = cfg.PanicHandler(c, v)
				}
			}()
		}

		// Get scope from locals
		scopeVal := c.Locals(scopeKey)
		if scopeVal == nil {
			return cfg.ScopeErrorHandler(c, godi.ErrScopeDisposed)
		}

		scope, ok := scopeVal.(godi.Scope)
		if !ok {
			return cfg.ScopeErrorHandler(c, godi.ErrScopeDisposed)
		}

		controller, resolveErr := godi.Resolve[T](scope)
		if resolveErr != nil {
			return cfg.ResolutionErrorHandler(c, resolveErr)
		}

		return method(controller, c)
	}
}

// FromContext retrieves the scope from fiber.Ctx.Locals.
// This is useful when you need to resolve services manually.
//
// Example:
//
//	scope := godifiber.FromContext(c)
//	userService := godi.MustResolve[*UserService](scope)
func FromContext(c *fiber.Ctx) godi.Scope {
	scopeVal := c.Locals(scopeKey)
	if scopeVal == nil {
		return nil
	}

	scope, ok := scopeVal.(godi.Scope)
	if !ok {
		return nil
	}

	return scope
}
