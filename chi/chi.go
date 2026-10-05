// Package chi provides godi integration for the Chi router.
//
// Chi uses standard net/http handlers and middleware, so this package is a
// thin facade over the net/http integration
// (github.com/junioryono/godi/http/v6): its types are aliases of the godihttp
// types and its functions delegate to godihttp. Using either package with a
// Chi router behaves identically; this one exists for discoverability.
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
//
// Default handlers log the cause with log/slog (see WithLogger and
// WithHandlerLogger) and respond with a generic 500 Internal Server Error;
// they never write internal error text to the client.
package chi

import (
	"log/slog"
	"net/http"

	godihttp "github.com/junioryono/godi/http/v6"
	"github.com/junioryono/godi/v6"
)

// Config holds the configuration for the scope middleware.
// See godihttp.Config.
type Config = godihttp.Config

// Option configures the scope middleware.
type Option = godihttp.Option

// HandlerConfig holds configuration for the Handle wrapper.
// See godihttp.HandlerConfig.
type HandlerConfig = godihttp.HandlerConfig

// HandlerOption configures the Handle wrapper.
type HandlerOption = godihttp.HandlerOption

// WithErrorHandler sets the error handler for scope creation failures. Unless
// WithMiddlewareErrorHandler is also used, it handles middleware failures too.
func WithErrorHandler(h func(http.ResponseWriter, *http.Request, error)) Option {
	return godihttp.WithErrorHandler(h)
}

// WithMiddlewareErrorHandler sets the error handler for failures returned by
// functions registered with WithMiddleware, such as authentication checks
// that respond 401 or 403. Scope creation failures still go to ErrorHandler.
func WithMiddlewareErrorHandler(h func(http.ResponseWriter, *http.Request, error)) Option {
	return godihttp.WithMiddlewareErrorHandler(h)
}

// WithCloseErrorHandler sets the error handler for scope close failures.
func WithCloseErrorHandler(h func(error)) Option {
	return godihttp.WithCloseErrorHandler(h)
}

// WithMiddleware adds a middleware function that runs after scope creation.
// Multiple middlewares are executed in the order they are added.
func WithMiddleware(mw func(godi.Scope, *http.Request) error) Option {
	return godihttp.WithMiddleware(mw)
}

// WithLogger sets the logger used by the middleware's default handlers.
// A nil logger keeps the default, slog.Default().
func WithLogger(l *slog.Logger) Option {
	return godihttp.WithLogger(l)
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
	return godihttp.ScopeMiddleware(provider, opts...)
}

// WithPanicRecovery enables or disables panic recovery in the handler.
func WithPanicRecovery(enabled bool) HandlerOption {
	return godihttp.WithPanicRecovery(enabled)
}

// WithPanicHandler sets the handler for panics.
func WithPanicHandler(h func(http.ResponseWriter, *http.Request, any)) HandlerOption {
	return godihttp.WithPanicHandler(h)
}

// WithScopeErrorHandler sets the error handler for scope retrieval failures.
func WithScopeErrorHandler(h func(http.ResponseWriter, *http.Request, error)) HandlerOption {
	return godihttp.WithScopeErrorHandler(h)
}

// WithResolutionErrorHandler sets the error handler for service resolution failures.
func WithResolutionErrorHandler(h func(http.ResponseWriter, *http.Request, error)) HandlerOption {
	return godihttp.WithResolutionErrorHandler(h)
}

// WithHandlerLogger sets the logger used by Handle's default handlers.
// A nil logger keeps the default, slog.Default().
func WithHandlerLogger(l *slog.Logger) HandlerOption {
	return godihttp.WithHandlerLogger(l)
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
	return godihttp.Handle(method, opts...)
}
