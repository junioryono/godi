package fiber

import (
	"bytes"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/junioryono/godi/v6"
	"github.com/stretchr/testify/assert"
)

// internalDetail is error text that must reach the log but never the client.
const internalDetail = "db password=hunter2"

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newCapturingLogger() (*slog.Logger, *syncBuffer) {
	buf := &syncBuffer{}
	return slog.New(slog.NewTextHandler(buf, nil)), buf
}

func closedProvider(t *testing.T) godi.Provider {
	t.Helper()
	provider, err := godi.NewCollection().Build()
	assert.NoError(t, err)
	assert.NoError(t, provider.Close())
	return provider
}

func openProvider(t *testing.T) godi.Provider {
	t.Helper()
	collection := godi.NewCollection()
	collection.AddScoped(func() *testService { return &testService{ID: "contract"} })
	collection.AddScoped(newTestController)
	provider, err := collection.Build()
	assert.NoError(t, err)
	t.Cleanup(func() { _ = provider.Close() })
	return provider
}

type response struct {
	status int
	body   string
}

func serve(t *testing.T, app *fiber.App) response {
	t.Helper()
	resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/test", http.NoBody))
	if !assert.NoError(t, err) {
		return response{}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	assert.NoError(t, err)
	return response{status: resp.StatusCode, body: string(body)}
}

func appWith(middleware, handler fiber.Handler) *fiber.App {
	app := fiber.New()
	if middleware != nil {
		app.Use(middleware)
	}
	app.Get("/test", handler)
	return app
}

func ok(c *fiber.Ctx) error { return c.SendStatus(http.StatusOK) }

func TestContractDefaultScopeErrorIsLoggedAndGeneric(t *testing.T) {
	logger, logs := newCapturingLogger()

	resp := serve(t, appWith(ScopeMiddleware(closedProvider(t), WithLogger(logger)), ok))

	assert.Equal(t, http.StatusInternalServerError, resp.status)
	assert.Contains(t, logs.String(), godi.ErrProviderDisposed.Error())
	assert.NotContains(t, resp.body, godi.ErrProviderDisposed.Error())
}

func TestContractDefaultMiddlewareErrorIsLoggedAndGeneric(t *testing.T) {
	logger, logs := newCapturingLogger()

	resp := serve(t, appWith(ScopeMiddleware(openProvider(t),
		WithLogger(logger),
		WithMiddleware(func(godi.Scope, *fiber.Ctx) error { return errors.New(internalDetail) }),
	), ok))

	assert.Equal(t, http.StatusInternalServerError, resp.status)
	assert.Contains(t, logs.String(), internalDetail)
	assert.NotContains(t, resp.body, internalDetail)
}

func TestContractMiddlewareErrorHandler(t *testing.T) {
	unauthorized := func(c *fiber.Ctx, _ error) error { return c.SendStatus(http.StatusUnauthorized) }
	failingAuth := WithMiddleware(func(godi.Scope, *fiber.Ctx) error { return errors.New("bad token") })

	t.Run("handles middleware errors", func(t *testing.T) {
		resp := serve(t, appWith(ScopeMiddleware(openProvider(t), failingAuth, WithMiddlewareErrorHandler(unauthorized)), ok))
		assert.Equal(t, http.StatusUnauthorized, resp.status)
	})

	t.Run("does not handle scope creation errors", func(t *testing.T) {
		logger, _ := newCapturingLogger()
		resp := serve(t, appWith(ScopeMiddleware(closedProvider(t), WithLogger(logger), WithMiddlewareErrorHandler(unauthorized)), ok))
		assert.Equal(t, http.StatusInternalServerError, resp.status)
	})

	t.Run("defaults to the configured error handler", func(t *testing.T) {
		resp := serve(t, appWith(ScopeMiddleware(openProvider(t), failingAuth,
			WithErrorHandler(func(c *fiber.Ctx, _ error) error { return c.SendStatus(http.StatusTeapot) }),
		), ok))
		assert.Equal(t, http.StatusTeapot, resp.status)
	})

	t.Run("nil keeps the default", func(t *testing.T) {
		cfg := defaultConfig()
		WithMiddlewareErrorHandler(nil)(cfg)
		normalizeConfig(cfg)
		assert.NotNil(t, cfg.MiddlewareErrorHandler)
	})
}

func TestContractDefaultLoggerIsSlogDefault(t *testing.T) {
	logger, logs := newCapturingLogger()
	previous := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(previous) })

	serve(t, appWith(ScopeMiddleware(closedProvider(t)), ok))

	assert.Contains(t, logs.String(), godi.ErrProviderDisposed.Error())
}

func TestContractWithLoggerNilKeepsDefault(t *testing.T) {
	cfg := defaultConfig()
	WithLogger(nil)(cfg)
	assert.Nil(t, cfg.Logger)

	handlerCfg := defaultHandlerConfig()
	WithHandlerLogger(nil)(handlerCfg)
	assert.Nil(t, handlerCfg.Logger)
}

func TestContractDefaultCloseErrorUsesLogger(t *testing.T) {
	logger, logs := newCapturingLogger()
	cfg := defaultConfig()
	WithLogger(logger)(cfg)

	cfg.CloseErrorHandler(errors.New("close failed"))

	assert.Contains(t, logs.String(), "close failed")
}

func TestContractDefaultPanicHandlerLogsStack(t *testing.T) {
	logger, logs := newCapturingLogger()

	resp := serve(t, appWith(ScopeMiddleware(openProvider(t)),
		Handle((*testController).Panic, WithPanicRecovery(true), WithHandlerLogger(logger))))

	assert.Equal(t, http.StatusInternalServerError, resp.status)
	assert.Contains(t, logs.String(), "test panic")
	assert.Contains(t, logs.String(), "runtime/debug.Stack")
	assert.NotContains(t, resp.body, "test panic")
}

func TestContractDefaultResolutionErrorIsLoggedAndGeneric(t *testing.T) {
	logger, logs := newCapturingLogger()
	provider, err := godi.NewCollection().Build()
	assert.NoError(t, err)
	defer provider.Close()

	resp := serve(t, appWith(ScopeMiddleware(provider), Handle((*testController).GetValue, WithHandlerLogger(logger))))

	assert.Equal(t, http.StatusInternalServerError, resp.status)
	assert.Contains(t, logs.String(), "testController")
	assert.NotContains(t, resp.body, "testController")
}

// recordingMiddleware records the error the rest of the chain returns to it.
func recordingMiddleware(seen *[]error) fiber.Handler {
	return func(c *fiber.Ctx) error {
		err := c.Next()
		*seen = append(*seen, err)
		return err
	}
}

func TestContractErrorPassthrough(t *testing.T) {
	handlerErr := fiber.NewError(http.StatusConflict, "conflict")

	t.Run("consumes downstream errors by default", func(t *testing.T) {
		var seen []error
		errorHandlerCalls := 0
		app := fiber.New(fiber.Config{ErrorHandler: func(c *fiber.Ctx, err error) error {
			errorHandlerCalls++
			return fiber.DefaultErrorHandler(c, err)
		}})
		app.Use(recordingMiddleware(&seen), ScopeMiddleware(openProvider(t)))
		app.Get("/test", func(*fiber.Ctx) error { return handlerErr })

		resp := serve(t, app)

		assert.Equal(t, http.StatusConflict, resp.status)
		assert.Equal(t, []error{nil}, seen, "outer middleware must not see a consumed error")
		assert.Equal(t, 1, errorHandlerCalls)
	})

	t.Run("returns downstream errors to outer middleware when enabled", func(t *testing.T) {
		var seen []error
		errorHandlerCalls := 0
		var scopeAliveDuringRender bool
		app := fiber.New(fiber.Config{ErrorHandler: func(c *fiber.Ctx, err error) error {
			errorHandlerCalls++
			scope, scopeErr := godi.FromContext(c.UserContext())
			if scopeErr == nil {
				_, resolveErr := godi.Resolve[*testService](scope)
				scopeAliveDuringRender = resolveErr == nil
			}
			return fiber.DefaultErrorHandler(c, err)
		}})
		app.Use(recordingMiddleware(&seen), ScopeMiddleware(openProvider(t), WithErrorPassthrough(true)))
		app.Get("/test", func(*fiber.Ctx) error { return handlerErr })

		resp := serve(t, app)

		assert.Equal(t, http.StatusConflict, resp.status)
		assert.Equal(t, []error{handlerErr}, seen)
		assert.Equal(t, 1, errorHandlerCalls)
		assert.False(t, scopeAliveDuringRender, "with passthrough, rendering happens after the scope closes")
	})

	t.Run("returns middleware errors to outer middleware when enabled", func(t *testing.T) {
		var seen []error
		app := fiber.New()
		app.Use(recordingMiddleware(&seen), ScopeMiddleware(openProvider(t),
			WithErrorPassthrough(true),
			WithMiddleware(func(godi.Scope, *fiber.Ctx) error { return errors.New("bad token") }),
			WithMiddlewareErrorHandler(func(*fiber.Ctx, error) error { return fiber.ErrUnauthorized }),
		))
		app.Get("/test", ok)

		resp := serve(t, app)

		assert.Equal(t, http.StatusUnauthorized, resp.status)
		assert.Equal(t, []error{fiber.ErrUnauthorized}, seen)
	})
}
