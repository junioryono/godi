// Package chi provides godi integration for the Chi router.
//
// Chi uses standard net/http handlers and middleware, so this integration
// mirrors the net/http one (github.com/junioryono/godi/http/v5): using godihttp
// with a Chi router works identically. This package exists for discoverability.
//
// Example usage:
//
//	provider, _ := collection.Build()
//
//	r := chi.NewRouter()
//	r.Use(godichi.ScopeMiddleware(provider))
//
//	r.Post("/login", godichi.Handle(AuthController.Login))
//	r.Get("/users/{id}", godichi.Handle(UserController.GetByID))
package chi

import (
	"log/slog"
	"net/http"

	"github.com/junioryono/godi/v5"
)

// Config holds the configuration for the scope middleware.
type Config struct {
	// ErrorHandler is called when scope creation fails.
	// If nil, a default handler returning 500 Internal Server Error is used.
	ErrorHandler func(http.ResponseWriter, *http.Request, error)

	// CloseErrorHandler is called when scope closing fails.
	// If nil, errors are logged using slog.
	CloseErrorHandler func(error)

	// Middlewares are functions that run after scope creation.
	// They can be used to initialize request context, set user data, etc.
	Middlewares []func(godi.Scope, *http.Request) error
}

// Option configures the scope middleware.
type Option func(*Config)

// WithErrorHandler sets the error handler for scope creation failures.
func WithErrorHandler(h func(http.ResponseWriter, *http.Request, error)) Option {
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
func WithMiddleware(mw func(godi.Scope, *http.Request) error) Option {
	return func(c *Config) {
		if mw != nil {
			c.Middlewares = append(c.Middlewares, mw)
		}
	}
}

func defaultConfig() *Config {
	return &Config{
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
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
	middlewares := make([]func(godi.Scope, *http.Request) error, 0, len(c.Middlewares))
	for _, middleware := range c.Middlewares {
		if middleware != nil {
			middlewares = append(middlewares, middleware)
		}
	}
	c.Middlewares = middlewares
}

// ScopeMiddleware creates a Chi middleware that creates a request-scoped
// container for each request. The scope is attached to the request context
// and can be retrieved using godi.FromContext.
//
// The scope is automatically closed when the request completes.
//
// Example:
//
//	r := chi.NewRouter()
//	r.Use(godichi.ScopeMiddleware(provider))
func ScopeMiddleware(provider godi.Provider, opts ...Option) func(http.Handler) http.Handler {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	normalizeConfig(cfg)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, err := provider.CreateScope(r.Context())
			if err != nil {
				cfg.ErrorHandler(w, r, err)
				return
			}

			defer func() {
				if err := scope.Close(); err != nil {
					cfg.CloseErrorHandler(err)
				}
			}()

			// Attach scope to request context
			r = r.WithContext(scope.Context())

			// Run middlewares
			for _, mw := range cfg.Middlewares {
				if err := mw(scope, r); err != nil {
					cfg.ErrorHandler(w, r, err)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// HandlerConfig holds configuration for the Handle wrapper.
type HandlerConfig struct {
	// PanicRecovery enables panic recovery in the handler.
	PanicRecovery bool

	// PanicHandler is called when a panic occurs (if PanicRecovery is true).
	PanicHandler func(http.ResponseWriter, *http.Request, any)

	// ScopeErrorHandler is called when scope retrieval fails.
	ScopeErrorHandler func(http.ResponseWriter, *http.Request, error)

	// ResolutionErrorHandler is called when service resolution fails.
	ResolutionErrorHandler func(http.ResponseWriter, *http.Request, error)
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
func WithPanicHandler(h func(http.ResponseWriter, *http.Request, any)) HandlerOption {
	return func(c *HandlerConfig) {
		if h != nil {
			c.PanicHandler = h
		}
	}
}

// WithScopeErrorHandler sets the error handler for scope retrieval failures.
func WithScopeErrorHandler(h func(http.ResponseWriter, *http.Request, error)) HandlerOption {
	return func(c *HandlerConfig) {
		if h != nil {
			c.ScopeErrorHandler = h
		}
	}
}

// WithResolutionErrorHandler sets the error handler for service resolution failures.
func WithResolutionErrorHandler(h func(http.ResponseWriter, *http.Request, error)) HandlerOption {
	return func(c *HandlerConfig) {
		if h != nil {
			c.ResolutionErrorHandler = h
		}
	}
}

func defaultHandlerConfig() *HandlerConfig {
	return &HandlerConfig{
		PanicRecovery: false,
		PanicHandler: func(w http.ResponseWriter, r *http.Request, v any) {
			slog.Error("panic in handler", "panic", v)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		},
		ScopeErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("failed to get scope from context", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
		},
		ResolutionErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			slog.Error("failed to resolve controller", "error", err)
			http.Error(w, "Internal Server Error", http.StatusInternalServerError)
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
// The controller type T is resolved from the scope attached to the request context.
//
// The method signature should be: func(T, http.ResponseWriter, *http.Request)
//
// Example:
//
//	type UserController interface {
//	    GetByID(http.ResponseWriter, *http.Request)
//	}
//
//	r.Get("/users/{id}", godichi.Handle(UserController.GetByID))
func Handle[T any](method func(T, http.ResponseWriter, *http.Request), opts ...HandlerOption) http.HandlerFunc {
	cfg := defaultHandlerConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	normalizeHandlerConfig(cfg)

	return func(w http.ResponseWriter, r *http.Request) {
		if cfg.PanicRecovery {
			defer func() {
				if v := recover(); v != nil {
					// http.ErrAbortHandler is the net/http contract for aborting a
					// response mid-write; suppressing it would send a bogus 500 on
					// a deliberately aborted connection.
					if v == http.ErrAbortHandler { //nolint:errorlint // sentinel panic value, compared by identity
						panic(v)
					}
					cfg.PanicHandler(w, r, v)
				}
			}()
		}

		scope, err := godi.FromContext(r.Context())
		if err != nil {
			cfg.ScopeErrorHandler(w, r, err)
			return
		}

		controller, err := godi.Resolve[T](scope)
		if err != nil {
			cfg.ResolutionErrorHandler(w, r, err)
			return
		}

		method(controller, w, r)
	}
}
