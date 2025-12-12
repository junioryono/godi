// Package echo provides godi integration for the Echo web framework.
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
package echo

import (
	"log/slog"
	"net/http"

	"github.com/junioryono/godi/v4"
	"github.com/labstack/echo/v4"
)

// Config holds the configuration for the scope middleware.
type Config struct {
	// ErrorHandler is called when scope creation fails.
	// If nil, the error is returned (Echo's default error handling).
	ErrorHandler func(echo.Context, error) error

	// CloseErrorHandler is called when scope closing fails.
	// If nil, errors are logged using slog.
	CloseErrorHandler func(error)

	// Middlewares are functions that run after scope creation.
	// They can be used to initialize request context, set user data, etc.
	Middlewares []func(godi.Scope, echo.Context) error
}

// Option configures the scope middleware.
type Option func(*Config)

// WithErrorHandler sets the error handler for scope creation failures.
func WithErrorHandler(h func(echo.Context, error) error) Option {
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
func WithMiddleware(mw func(godi.Scope, echo.Context) error) Option {
	return func(c *Config) {
		c.Middlewares = append(c.Middlewares, mw)
	}
}

func defaultConfig() *Config {
	return &Config{
		ErrorHandler: func(c echo.Context, err error) error {
			return echo.NewHTTPError(http.StatusInternalServerError, "Internal Server Error")
		},
		CloseErrorHandler: func(err error) {
			slog.Error("failed to close scope", "error", err)
		},
		Middlewares: nil,
	}
}

// ScopeMiddleware creates an Echo middleware that creates a request-scoped
// container for each request. The scope is attached to the request context
// and can be retrieved using godi.FromContext.
//
// The scope is automatically closed when the request completes.
//
// Example:
//
//	e := echo.New()
//	e.Use(godiecho.ScopeMiddleware(provider))
func ScopeMiddleware(provider godi.Provider, opts ...Option) echo.MiddlewareFunc {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

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
					return cfg.ErrorHandler(c, err)
				}
			}

			return next(c)
		}
	}
}

// HandlerConfig holds configuration for the Handle wrapper.
type HandlerConfig struct {
	// PanicRecovery enables panic recovery in the handler.
	PanicRecovery bool

	// PanicHandler is called when a panic occurs (if PanicRecovery is true).
	PanicHandler func(echo.Context, any) error

	// ScopeErrorHandler is called when scope retrieval fails.
	ScopeErrorHandler func(echo.Context, error) error

	// ResolutionErrorHandler is called when service resolution fails.
	ResolutionErrorHandler func(echo.Context, error) error
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
		c.PanicHandler = h
	}
}

// WithScopeErrorHandler sets the error handler for scope retrieval failures.
func WithScopeErrorHandler(h func(echo.Context, error) error) HandlerOption {
	return func(c *HandlerConfig) {
		c.ScopeErrorHandler = h
	}
}

// WithResolutionErrorHandler sets the error handler for service resolution failures.
func WithResolutionErrorHandler(h func(echo.Context, error) error) HandlerOption {
	return func(c *HandlerConfig) {
		c.ResolutionErrorHandler = h
	}
}

func defaultHandlerConfig() *HandlerConfig {
	return &HandlerConfig{
		PanicRecovery: false,
		PanicHandler: func(c echo.Context, v any) error {
			slog.Error("panic in handler", "panic", v)
			return echo.NewHTTPError(http.StatusInternalServerError, "Internal Server Error")
		},
		ScopeErrorHandler: func(c echo.Context, err error) error {
			slog.Error("failed to get scope from context", "error", err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Internal Server Error")
		},
		ResolutionErrorHandler: func(c echo.Context, err error) error {
			slog.Error("failed to resolve controller", "error", err)
			return echo.NewHTTPError(http.StatusInternalServerError, "Internal Server Error")
		},
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
		opt(cfg)
	}

	return func(c echo.Context) (err error) {
		if cfg.PanicRecovery {
			defer func() {
				if v := recover(); v != nil {
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
