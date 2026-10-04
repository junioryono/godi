// Package http provides godi integration for the standard net/http package.
//
// This package provides middleware for creating request-scoped containers
// and type-safe handler wrappers for resolving controllers.
//
// Example usage:
//
//	provider, _ := collection.Build()
//
//	mux := http.NewServeMux()
//	handler := godihttp.ScopeMiddleware(provider)(mux)
//
//	mux.HandleFunc("/login", godihttp.Handle(AuthController.Login))
//	mux.HandleFunc("/users/", godihttp.Handle(UserController.GetByID))
//
//	http.ListenAndServe(":8080", handler)
//
// Default handlers log the cause with log/slog (see WithLogger and
// WithHandlerLogger) and respond with a generic 500 Internal Server Error;
// they never write internal error text to the client.
package http

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/junioryono/godi/v6"
)

// Config holds the configuration for the scope middleware.
type Config struct {
	// ErrorHandler is called when scope creation fails. It also handles
	// middleware failures unless MiddlewareErrorHandler is set.
	// If nil, a default handler that logs the error and returns a generic
	// 500 Internal Server Error is used.
	ErrorHandler func(http.ResponseWriter, *http.Request, error)

	// MiddlewareErrorHandler is called when a function registered with
	// WithMiddleware returns an error. If nil, ErrorHandler is used.
	MiddlewareErrorHandler func(http.ResponseWriter, *http.Request, error)

	// CloseErrorHandler is called when scope closing fails.
	// If nil, errors are logged.
	CloseErrorHandler func(error)

	// Middlewares are functions that run after scope creation.
	// They can be used to initialize request context, set user data, etc.
	Middlewares []func(godi.Scope, *http.Request) error

	// Logger is used by the default handlers. If nil, slog.Default() is used.
	Logger *slog.Logger
}

// Option configures the scope middleware.
type Option func(*Config)

// WithErrorHandler sets the error handler for scope creation failures. Unless
// WithMiddlewareErrorHandler is also used, it handles middleware failures too.
func WithErrorHandler(h func(http.ResponseWriter, *http.Request, error)) Option {
	return func(c *Config) {
		if h != nil {
			c.ErrorHandler = h
		}
	}
}

// WithMiddlewareErrorHandler sets the error handler for failures returned by
// functions registered with WithMiddleware, such as authentication checks
// that respond 401 or 403. Scope creation failures still go to ErrorHandler.
func WithMiddlewareErrorHandler(h func(http.ResponseWriter, *http.Request, error)) Option {
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
func WithMiddleware(mw func(godi.Scope, *http.Request) error) Option {
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

func (c *Config) defaultErrorHandler(w http.ResponseWriter, _ *http.Request, err error) {
	c.logger().Error("failed to set up request scope", "error", err)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
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
	middlewares := make([]func(godi.Scope, *http.Request) error, 0, len(c.Middlewares))
	for _, middleware := range c.Middlewares {
		if middleware != nil {
			middlewares = append(middlewares, middleware)
		}
	}
	c.Middlewares = middlewares
}

// ScopeMiddleware creates a middleware that creates a request-scoped
// container for each request. The scope is attached to the request context
// and can be retrieved using godi.FromContext.
//
// The scope is automatically closed when the request completes.
//
// Example:
//
//	mux := http.NewServeMux()
//	handler := godihttp.ScopeMiddleware(provider)(mux)
//	http.ListenAndServe(":8080", handler)
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
					cfg.MiddlewareErrorHandler(w, r, err)
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
	// If nil, a default handler that logs the panic value and stack trace and
	// returns a generic 500 Internal Server Error is used.
	PanicHandler func(http.ResponseWriter, *http.Request, any)

	// ScopeErrorHandler is called when scope retrieval fails.
	ScopeErrorHandler func(http.ResponseWriter, *http.Request, error)

	// ResolutionErrorHandler is called when service resolution fails.
	ResolutionErrorHandler func(http.ResponseWriter, *http.Request, error)

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

func (c *HandlerConfig) defaultPanicHandler(w http.ResponseWriter, _ *http.Request, v any) {
	c.logger().Error("panic in handler", "panic", v, "stack", string(debug.Stack()))
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func (c *HandlerConfig) defaultScopeErrorHandler(w http.ResponseWriter, _ *http.Request, err error) {
	c.logger().Error("failed to get scope from context", "error", err)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}

func (c *HandlerConfig) defaultResolutionErrorHandler(w http.ResponseWriter, _ *http.Request, err error) {
	c.logger().Error("failed to resolve controller", "error", err)
	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
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
// The method signature should be: func(T, http.ResponseWriter, *http.Request)
//
// Example:
//
//	type UserController interface {
//	    GetByID(http.ResponseWriter, *http.Request)
//	}
//
//	mux.HandleFunc("/users/", godihttp.Handle(UserController.GetByID))
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
