package huma_test

import (
	"context"
	"errors"
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

// scopedContext builds a provider, opens a scope, and returns the scope's
// context.Context — exactly what a router's ScopeMiddleware sets as the
// request context and Huma propagates to handlers.
func scopedContext(t *testing.T) (context.Context, func()) {
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

func TestHandle_DefaultErrorMapperPassesThrough(t *testing.T) {
	ctx, cleanup := scopedContext(t)
	defer cleanup()

	h := godihuma.Handle((*userController).Fail)
	_, err := h(ctx, &greetInput{Name: "x"})
	assert.ErrorIs(t, err, errBoom)
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

	srv := httptest.NewServer(handler)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/greet/world")
	require.NoError(t, err)
	defer resp.Body.Close()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHandle_NilOptionsKeepDefaults(t *testing.T) {
	ctx, cleanup := scopedContext(t)
	defer cleanup()

	// Passing nil options must not overwrite the defaults (the field docs
	// promise nil-tolerant behavior) — and must not panic at request time.
	h := godihuma.Handle((*userController).Fail,
		godihuma.WithErrorMapper(nil),
		godihuma.WithResolutionErrorHandler(nil))

	require.NotPanics(t, func() {
		_, err := h(ctx, &greetInput{Name: "x"})
		// default ErrorMapper is identity, so the method's error passes through
		assert.ErrorIs(t, err, errBoom)
	})

	// nil resolution handler keeps the default 500 on a missing scope.
	require.NotPanics(t, func() {
		_, err := h(context.Background(), &greetInput{Name: "x"})
		var se huma.StatusError
		require.ErrorAs(t, err, &se)
		assert.Equal(t, http.StatusInternalServerError, se.GetStatus())
	})
}
