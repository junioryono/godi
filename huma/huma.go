// Package huma provides godi integration for the Huma REST API framework
// (github.com/danielgtaylor/huma).
//
// Huma is router-agnostic: it runs on top of an adapter (humago, humachi,
// humagin, humaecho, humafiber) and propagates the underlying request's
// context.Context to operation handlers and middleware. godi relies on that:
// the request scope is created by the *router's* godi ScopeMiddleware
// (godihttp, godichi, godigin, godiecho, godifiber), and Huma carries it
// through, so godi.FromContext works inside Huma handlers.
//
// This package therefore provides only the Huma-specific piece — a type-safe
// controller wrapper for huma.Register. To resolve services directly inside a
// handler or middleware, use the standard godi helpers on the request context:
//
//	scope, err := godi.FromContext(ctx) // ctx is the handler's context, or humaCtx.Context()
//	svc, err := godi.Resolve[*UserService](scope)
//
// Example wiring (Gin shown; any adapter works the same way):
//
//	g := gin.New()
//	g.Use(godigin.ScopeMiddleware(provider))
//	api := humagin.New(g, huma.DefaultConfig("My API", "1.0.0"))
//
//	huma.Register(api, huma.Operation{
//	    OperationID: "get-user",
//	    Method:      http.MethodGet,
//	    Path:        "/users/{id}",
//	}, godihuma.Handle((*UserController).GetByID))
package huma

import (
	"context"
	"errors"
	"log/slog"

	"github.com/danielgtaylor/huma/v2"
	"github.com/junioryono/godi/v5"
)

// HandlerConfig holds configuration for the Handle wrapper.
type HandlerConfig struct {
	// ResolutionErrorHandler maps a failure to obtain the request scope or
	// resolve the controller into the error returned to Huma. If nil, a
	// 500 Internal Server Error is returned.
	ResolutionErrorHandler func(error) error

	// ErrorMapper maps the error returned by the controller method before it
	// is handed back to Huma. huma.StatusError values are preserved and other
	// mapped errors become a generic 500. Use it to translate domain errors into
	// huma.StatusError values; plain internal errors are never exposed.
	ErrorMapper func(error) error
}

// HandlerOption configures the Handle wrapper.
type HandlerOption func(*HandlerConfig)

// WithResolutionErrorHandler sets the handler invoked when the request scope
// or the controller cannot be resolved. The returned error is sent to Huma.
func WithResolutionErrorHandler(h func(error) error) HandlerOption {
	return func(c *HandlerConfig) {
		if h != nil {
			c.ResolutionErrorHandler = h
		}
	}
}

// WithErrorMapper sets a function that maps the controller method's returned
// error before it is handed back to Huma. This is the place to translate
// domain errors into huma.StatusError values (e.g. sql.ErrNoRows -> 404).
// Plain mapped errors are sanitized to a generic 500 response.
func WithErrorMapper(m func(error) error) HandlerOption {
	return func(c *HandlerConfig) {
		if m != nil {
			c.ErrorMapper = m
		}
	}
}

func defaultHandlerConfig() *HandlerConfig {
	return &HandlerConfig{
		ResolutionErrorHandler: func(err error) error {
			slog.Error("failed to resolve request controller", "error", err)
			return huma.Error500InternalServerError("internal server error")
		},
		ErrorMapper: sanitizeControllerError,
	}
}

func normalizeHandlerConfig(c *HandlerConfig) {
	defaults := defaultHandlerConfig()
	if c.ResolutionErrorHandler == nil {
		c.ResolutionErrorHandler = defaults.ResolutionErrorHandler
	}
	if c.ErrorMapper == nil {
		c.ErrorMapper = defaults.ErrorMapper
	}
}

func sanitizeControllerError(err error) error {
	if err == nil {
		return nil
	}
	var statusErr huma.StatusError
	if errors.As(err, &statusErr) {
		if _, ok := statusErr.(huma.HeadersError); ok {
			return statusErr
		}

		var headersErr huma.HeadersError
		if errors.As(err, &headersErr) {
			return huma.ErrorWithHeaders(statusErr, headersErr.GetHeaders().Clone())
		}

		// Return the status error itself so an outer wrapper cannot add internal
		// context to the client-visible message.
		return statusErr
	}
	slog.Error("unexpected error in handler", "error", err)
	return huma.Error500InternalServerError("internal server error")
}

// Handle adapts a controller method for registration with huma.Register.
//
// The controller C is resolved from the request scope on each request, then
// the method is invoked with the request context and typed input. C is
// typically an interface or pointer type registered with the container; pass
// the method as a method expression so its receiver becomes the first
// parameter:
//
//	huma.Register(api, op, godihuma.Handle((*UserController).GetByID))
//
// where
//
//	func (c *UserController) GetByID(ctx context.Context, in *GetInput) (*GetOutput, error)
//
// On a failure to resolve the scope or controller, the ResolutionErrorHandler
// is used (default: 500). The controller's returned error is passed through
// ErrorMapper (default: preserve status errors and sanitize unexpected errors).
func Handle[C, I, O any](
	method func(C, context.Context, *I) (*O, error),
	opts ...HandlerOption,
) func(context.Context, *I) (*O, error) {
	cfg := defaultHandlerConfig()
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	normalizeHandlerConfig(cfg)

	return func(ctx context.Context, in *I) (*O, error) {
		scope, err := godi.FromContext(ctx)
		if err != nil {
			return nil, cfg.ResolutionErrorHandler(err)
		}

		controller, err := godi.Resolve[C](scope)
		if err != nil {
			return nil, cfg.ResolutionErrorHandler(err)
		}

		out, err := method(controller, ctx, in)
		if err != nil {
			return nil, sanitizeControllerError(cfg.ErrorMapper(err))
		}

		return out, nil
	}
}
