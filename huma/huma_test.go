package huma_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	godihuma "github.com/junioryono/godi/huma/v5"
	"github.com/junioryono/godi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- test services / controller ---

type greeter struct{ prefix string }

func newGreeter() *greeter { return &greeter{prefix: "hello"} }

type userController struct {
	g *greeter
}

func newUserController(g *greeter) *userController { return &userController{g: g} }

type greetInput struct {
	Name string `path:"name"`
}

type greetOutput struct {
	Body struct {
		Message string `json:"message"`
	}
}

func (c *userController) Greet(_ context.Context, in *greetInput) (*greetOutput, error) {
	out := &greetOutput{}
	out.Body.Message = c.g.prefix + " " + in.Name
	return out, nil
}

var errBoom = errors.New("boom")

func (c *userController) Fail(_ context.Context, _ *greetInput) (*greetOutput, error) {
	return nil, errBoom
}

func (c *userController) NotFound(_ context.Context, _ *greetInput) (*greetOutput, error) {
	return nil, huma.Error404NotFound("user not found")
}

// scopedContext builds a provider, opens a scope, and returns the scope's
// context.Context — exactly what a router's ScopeMiddleware sets as the
// request context and Huma propagates to handlers.
func scopedContext(t *testing.T) (ctx context.Context, cleanup func()) {
	t.Helper()
	c := godi.NewCollection()
	c.AddSingleton(newGreeter)
	c.AddScoped(newUserController)

	p, err := c.Build()
	require.NoError(t, err)

	scope, err := p.CreateScope(context.Background())
	require.NoError(t, err)

	return scope.Context(), func() {
		_ = scope.Close()
		_ = p.Close()
	}
}

// --- unit tests: Handle wrapper called directly with a scope context ---

func TestHandle_ResolvesControllerAndInvokes(t *testing.T) {
	ctx, cleanup := scopedContext(t)
	defer cleanup()

	h := godihuma.Handle((*userController).Greet)
	out, err := h(ctx, &greetInput{Name: "world"})

	require.NoError(t, err)
	require.NotNil(t, out)
	assert.Equal(t, "hello world", out.Body.Message)
}

func TestHandle_NoScopeReturnsResolutionError(t *testing.T) {
	h := godihuma.Handle((*userController).Greet)

	// A bare context with no scope — simulates the router scope middleware
	// not being installed.
	_, err := h(context.Background(), &greetInput{Name: "x"})
	require.Error(t, err)

	var se huma.StatusError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, http.StatusInternalServerError, se.GetStatus())
}

func TestHandle_CustomResolutionErrorHandler(t *testing.T) {
	sentinel := errors.New("custom resolution failure")
	h := godihuma.Handle((*userController).Greet,
		godihuma.WithResolutionErrorHandler(func(error) error { return sentinel }))

	_, err := h(context.Background(), &greetInput{Name: "x"})
	assert.ErrorIs(t, err, sentinel)
}

func TestHandle_ErrorMapperTransformsMethodError(t *testing.T) {
	ctx, cleanup := scopedContext(t)
	defer cleanup()

	h := godihuma.Handle((*userController).Fail,
		godihuma.WithErrorMapper(func(err error) error {
			if errors.Is(err, errBoom) {
				return huma.Error404NotFound("not found")
			}
			return err
		}))

	_, err := h(ctx, &greetInput{Name: "x"})
	require.Error(t, err)

	var se huma.StatusError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, http.StatusNotFound, se.GetStatus())
}

func TestHandle_DefaultErrorMapperSanitizesUnexpectedError(t *testing.T) {
	ctx, cleanup := scopedContext(t)
	defer cleanup()

	h := godihuma.Handle((*userController).Fail)
	_, err := h(ctx, &greetInput{Name: "x"})
	require.Error(t, err)
	assert.NotErrorIs(t, err, errBoom)

	var se huma.StatusError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, http.StatusInternalServerError, se.GetStatus())
	model, ok := se.(*huma.ErrorModel)
	require.True(t, ok, "sanitized error should be huma's default *huma.ErrorModel, got %T", se)
	assert.Empty(t, model.Errors)
}

func TestHandle_DefaultErrorMapperPreservesStatusError(t *testing.T) {
	ctx, cleanup := scopedContext(t)
	defer cleanup()

	h := godihuma.Handle((*userController).NotFound)
	_, err := h(ctx, &greetInput{Name: "x"})
	require.Error(t, err)

	var se huma.StatusError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, http.StatusNotFound, se.GetStatus())
	assert.Equal(t, "user not found", err.Error())
}

func TestHandle_CustomErrorMapperSanitizesPlainResult(t *testing.T) {
	ctx, cleanup := scopedContext(t)
	defer cleanup()

	mapped := errors.New("database-password=correct-horse-battery-staple")
	h := godihuma.Handle((*userController).Fail,
		godihuma.WithErrorMapper(func(error) error { return mapped }))
	_, err := h(ctx, &greetInput{Name: "x"})
	assert.NotErrorIs(t, err, mapped)

	var se huma.StatusError
	require.ErrorAs(t, err, &se)
	assert.Equal(t, http.StatusInternalServerError, se.GetStatus())
	model, ok := se.(*huma.ErrorModel)
	require.True(t, ok, "sanitized error should be huma's default *huma.ErrorModel, got %T", se)
	assert.Empty(t, model.Errors)
}

func TestHandle_ErrorMapperStripsInternalStatusErrorWrapper(t *testing.T) {
	ctx, cleanup := scopedContext(t)
	defer cleanup()

	const secret = "database-password=correct-horse-battery-staple"
	h := godihuma.Handle((*userController).Fail,
		godihuma.WithErrorMapper(func(error) error {
			return fmt.Errorf("%s: %w", secret, huma.Error404NotFound("not found"))
		}))
	_, err := h(ctx, &greetInput{Name: "x"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), secret)

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, http.StatusNotFound, statusErr.GetStatus())
}

func TestHandle_ErrorMapperPreservesStatusErrorHeaders(t *testing.T) {
	ctx, cleanup := scopedContext(t)
	defer cleanup()

	expectedHeaders := http.Header{
		"Retry-After":      {"120"},
		"WWW-Authenticate": {`Bearer realm="api"`},
	}
	h := godihuma.Handle((*userController).Fail,
		godihuma.WithErrorMapper(func(error) error {
			statusErr := huma.Error429TooManyRequests("slow down")
			return fmt.Errorf("internal context: %w", huma.ErrorWithHeaders(statusErr, expectedHeaders))
		}))

	_, err := h(ctx, &greetInput{Name: "x"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "internal context")

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, http.StatusTooManyRequests, statusErr.GetStatus())

	var headersErr huma.HeadersError
	require.ErrorAs(t, err, &headersErr)
	assert.Equal(t, expectedHeaders, headersErr.GetHeaders())
}

// statusWithWrappedHeaders is a StatusError whose chain (not its own type)
// carries a HeadersError, mimicking a domain error wrapping
// huma.ErrorWithHeaders.
type statusWithWrappedHeaders struct{ inner error }

func (e *statusWithWrappedHeaders) Error() string  { return e.inner.Error() }
func (e *statusWithWrappedHeaders) GetStatus() int { return http.StatusTooManyRequests }
func (e *statusWithWrappedHeaders) Unwrap() error  { return e.inner }

func TestHandle_StatusErrorCarryingHeadersIsNotMutated(t *testing.T) {
	ctx, cleanup := scopedContext(t)
	defer cleanup()

	// A package-level sentinel error reused across requests: sanitization
	// must not merge cloned headers back into its map on every request.
	sentinel := &statusWithWrappedHeaders{
		inner: huma.ErrorWithHeaders(huma.Error429TooManyRequests("slow down"), http.Header{
			"Retry-After": {"30"},
		}),
	}
	h := godihuma.Handle((*userController).Fail,
		godihuma.WithErrorMapper(func(error) error { return sentinel }))

	for range 3 {
		_, err := h(ctx, &greetInput{Name: "x"})
		require.Error(t, err)

		var headersErr huma.HeadersError
		require.ErrorAs(t, err, &headersErr)
		assert.Equal(t, []string{"30"}, headersErr.GetHeaders()["Retry-After"],
			"sentinel headers must not accumulate across requests")
	}
}

func TestHandle_PlainErrorHeadersSurviveSanitization(t *testing.T) {
	ctx, cleanup := scopedContext(t)
	defer cleanup()

	h := godihuma.Handle((*userController).Fail,
		godihuma.WithErrorMapper(func(error) error {
			return huma.ErrorWithHeaders(errors.New("internal detail"), http.Header{
				"Retry-After": {"30"},
			})
		}))

	_, err := h(ctx, &greetInput{Name: "x"})
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "internal detail")

	var statusErr huma.StatusError
	require.ErrorAs(t, err, &statusErr)
	assert.Equal(t, http.StatusInternalServerError, statusErr.GetStatus())

	var headersErr huma.HeadersError
	require.ErrorAs(t, err, &headersErr)
	assert.Equal(t, []string{"30"}, headersErr.GetHeaders()["Retry-After"])
}

// --- end-to-end: real Huma (humago) propagates the scope context to handlers ---

func TestEndToEnd_ScopePropagatesThroughHuma(t *testing.T) {
	c := godi.NewCollection()
	c.AddSingleton(newGreeter)
	c.AddScoped(newUserController)
	p, err := c.Build()
	require.NoError(t, err)
	defer p.Close()

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test", "1.0.0"))

	huma.Register(api, huma.Operation{
		OperationID: "greet",
		Method:      http.MethodGet,
		Path:        "/greet/{name}",
	}, godihuma.Handle((*userController).Greet))

	// Minimal inline scope middleware (what godihttp.ScopeMiddleware does):
	// create a scope per request and attach scope.Context() to the request.
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope, scopeErr := p.CreateScope(r.Context())
		require.NoError(t, scopeErr)
		defer scope.Close()
		mux.ServeHTTP(w, r.WithContext(scope.Context()))
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/greet/world", http.NoBody)
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "hello world")
}

func TestEndToEnd_DefaultErrorsDoNotDiscloseInternals(t *testing.T) {
	t.Run("resolution failure", func(t *testing.T) {
		mux := http.NewServeMux()
		api := humago.New(mux, huma.DefaultConfig("Test", "1.0.0"))
		huma.Register(api, huma.Operation{
			OperationID: "missing-scope",
			Method:      http.MethodGet,
			Path:        "/greet/{name}",
		}, godihuma.Handle((*userController).Greet))

		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/greet/world", http.NoBody))
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.NotContains(t, rec.Body.String(), "no scope found")
		assert.NotContains(t, rec.Body.String(), "godi.Scope")
	})

	t.Run("unexpected controller error", func(t *testing.T) {
		c := godi.NewCollection()
		c.AddSingleton(newGreeter)
		c.AddScoped(newUserController)
		p, err := c.Build()
		require.NoError(t, err)
		defer p.Close()

		mux := http.NewServeMux()
		api := humago.New(mux, huma.DefaultConfig("Test", "1.0.0"))
		huma.Register(api, huma.Operation{
			OperationID: "unexpected-error",
			Method:      http.MethodGet,
			Path:        "/fail/{name}",
		}, godihuma.Handle((*userController).Fail))

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, scopeErr := p.CreateScope(r.Context())
			require.NoError(t, scopeErr)
			defer scope.Close()
			mux.ServeHTTP(w, r.WithContext(scope.Context()))
		})

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/fail/world", http.NoBody))
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		assert.NotContains(t, rec.Body.String(), errBoom.Error())
	})
}

func TestHandle_NilOptionsKeepDefaults(t *testing.T) {
	ctx, cleanup := scopedContext(t)
	defer cleanup()
	require.NotPanics(t, func() { godihuma.Handle((*userController).Fail, nil) })

	var normalizedCfg *godihuma.HandlerConfig
	godihuma.Handle((*userController).Fail, func(cfg *godihuma.HandlerConfig) {
		cfg.ResolutionErrorHandler = nil
		cfg.ErrorMapper = nil
		normalizedCfg = cfg
	})
	require.NotNil(t, normalizedCfg.ResolutionErrorHandler)
	require.NotNil(t, normalizedCfg.ErrorMapper)

	// Passing nil options must not overwrite the defaults (the field docs
	// promise nil-tolerant behavior) — and must not panic at request time.
	h := godihuma.Handle((*userController).Fail,
		godihuma.WithErrorMapper(nil),
		godihuma.WithResolutionErrorHandler(nil))

	require.NotPanics(t, func() {
		_, err := h(ctx, &greetInput{Name: "x"})
		var se huma.StatusError
		require.ErrorAs(t, err, &se)
		assert.Equal(t, http.StatusInternalServerError, se.GetStatus())
	})

	// nil resolution handler keeps the default 500 on a missing scope.
	require.NotPanics(t, func() {
		_, err := h(context.Background(), &greetInput{Name: "x"})
		var se huma.StatusError
		require.ErrorAs(t, err, &se)
		assert.Equal(t, http.StatusInternalServerError, se.GetStatus())
	})
}
