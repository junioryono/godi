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
package gin

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/junioryono/godi/v4"
)

// Config holds the configuration for the scope middleware.
type Config struct {
	// ErrorHandler is called when scope creation fails.
	// If nil, a default handler returning 500 Internal Server Error is used.
	ErrorHandler func(*gin.Context, error)

	// CloseErrorHandler is called when scope closing fails.
	// If nil, errors are logged using slog.
	CloseErrorHandler func(error)

	// Middlewares are functions that run after scope creation.
	// They can be used to initialize request context, set user claims, etc.
	Middlewares []func(godi.Scope, *gin.Context) error
}

// Option configures the scope middleware.
type Option func(*Config)

// WithErrorHandler sets the error handler for scope creation failures.
func WithErrorHandler(h func(*gin.Context, error)) Option {
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
		c.Middlewares = append(c.Middlewares, mw)
	}
}

func defaultConfig() *Config {
	return &Config{
		ErrorHandler: func(c *gin.Context, err error) {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": "Internal Server Error",
			})
		},
		CloseErrorHandler: func(err error) {
			slog.Error("failed to close scope", "error", err)
		},
		Middlewares: nil,
	}
}

// ScopeMiddleware creates a gin.HandlerFunc that creates a request-scoped
// container for each request. The scope is attached to the request context
// and can be retrieved using godi.FromContext.
//
// The scope is automatically closed when the request completes.
//
// Example:
//
//	g := gin.New()
//	g.Use(godigin.ScopeMiddleware(provider))
func ScopeMiddleware(provider godi.Provider, opts ...Option) gin.HandlerFunc {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	return func(c *gin.Context) {
		scope, err := provider.CreateScope(c.Request.Context())
		if err != nil {
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
				cfg.ErrorHandler(c, err)
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
	// If nil, a default handler returning 500 Internal Server Error is used.
	PanicHandler func(*gin.Context, any)

	// ScopeErrorHandler is called when scope retrieval fails.
	// If nil, a default handler returning 500 Internal Server Error is used.
	ScopeErrorHandler func(*gin.Context, error)

	// ResolutionErrorHandler is called when service resolution fails.
	// If nil, a default handler returning 500 Internal Server Error is used.
	ResolutionErrorHandler func(*gin.Context, error)
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
		c.PanicHandler = h
	}
}

// WithScopeErrorHandler sets the error handler for scope retrieval failures.
func WithScopeErrorHandler(h func(*gin.Context, error)) HandlerOption {
	return func(c *HandlerConfig) {
		c.ScopeErrorHandler = h
	}
}

// WithResolutionErrorHandler sets the error handler for service resolution failures.
func WithResolutionErrorHandler(h func(*gin.Context, error)) HandlerOption {
	return func(c *HandlerConfig) {
		c.ResolutionErrorHandler = h
	}
}

func defaultHandlerConfig() *HandlerConfig {
	return &HandlerConfig{
		PanicRecovery: false,
		PanicHandler: func(c *gin.Context, r any) {
			slog.Error("panic in handler", "panic", r)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": "Internal Server Error",
			})
		},
		ScopeErrorHandler: func(c *gin.Context, err error) {
			slog.Error("failed to get scope from context", "error", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": "Internal Server Error",
			})
		},
		ResolutionErrorHandler: func(c *gin.Context, err error) {
			slog.Error("failed to resolve controller", "error", err)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": "Internal Server Error",
			})
		},
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
		opt(cfg)
	}

	return func(c *gin.Context) {
		if cfg.PanicRecovery {
			defer func() {
				if r := recover(); r != nil {
					cfg.PanicHandler(c, r)
				}
			}()
		}

		scope, err := godi.FromContext(c.Request.Context())
		if err != nil {
			cfg.ScopeErrorHandler(c, err)
			return
		}

		controller, err := godi.Resolve[T](scope)
		if err != nil {
			cfg.ResolutionErrorHandler(c, err)
			return
		}

		method(controller, c)
	}
}
