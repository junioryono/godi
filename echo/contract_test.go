package echo

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/junioryono/godi/v5"
	"github.com/labstack/echo/v4"
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

func serve(e *echo.Echo) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/test", http.NoBody))
	return rec
}

func echoWith(middleware echo.MiddlewareFunc, handler echo.HandlerFunc) *echo.Echo {
	e := echo.New()
	if middleware != nil {
		e.Use(middleware)
	}
	e.GET("/test", handler)
	return e
}

func ok(c echo.Context) error { return c.NoContent(http.StatusOK) }

func TestContractDefaultScopeErrorIsLoggedAndGeneric(t *testing.T) {
	logger, logs := newCapturingLogger()

	rec := serve(echoWith(ScopeMiddleware(closedProvider(t), WithLogger(logger)), ok))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, logs.String(), godi.ErrProviderDisposed.Error())
	assert.NotContains(t, rec.Body.String(), godi.ErrProviderDisposed.Error())
}

func TestContractDefaultMiddlewareErrorIsLoggedAndGeneric(t *testing.T) {
	logger, logs := newCapturingLogger()

	rec := serve(echoWith(ScopeMiddleware(openProvider(t),
		WithLogger(logger),
		WithMiddleware(func(godi.Scope, echo.Context) error { return errors.New(internalDetail) }),
	), ok))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, logs.String(), internalDetail)
	assert.NotContains(t, rec.Body.String(), internalDetail)
}

func TestContractMiddlewareErrorHandler(t *testing.T) {
	unauthorized := func(echo.Context, error) error { return echo.NewHTTPError(http.StatusUnauthorized, "Unauthorized") }
	failingAuth := WithMiddleware(func(godi.Scope, echo.Context) error { return errors.New("bad token") })

	t.Run("handles middleware errors", func(t *testing.T) {
		rec := serve(echoWith(ScopeMiddleware(openProvider(t), failingAuth, WithMiddlewareErrorHandler(unauthorized)), ok))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
	})

	t.Run("does not handle scope creation errors", func(t *testing.T) {
		logger, _ := newCapturingLogger()
		rec := serve(echoWith(ScopeMiddleware(closedProvider(t), WithLogger(logger), WithMiddlewareErrorHandler(unauthorized)), ok))
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("defaults to the configured error handler", func(t *testing.T) {
		rec := serve(echoWith(ScopeMiddleware(openProvider(t), failingAuth,
			WithErrorHandler(func(c echo.Context, _ error) error { return c.NoContent(http.StatusTeapot) }),
		), ok))
		assert.Equal(t, http.StatusTeapot, rec.Code)
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

	serve(echoWith(ScopeMiddleware(closedProvider(t)), ok))

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

	rec := serve(echoWith(ScopeMiddleware(openProvider(t)),
		Handle((*testController).Panic, WithPanicRecovery(true), WithHandlerLogger(logger))))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, logs.String(), "test panic")
	assert.Contains(t, logs.String(), "runtime/debug.Stack")
	assert.NotContains(t, rec.Body.String(), "test panic")
}

func TestContractPanicRecoveryRepanicsErrAbortHandler(t *testing.T) {
	handler := Handle(func(*testController, echo.Context) error { panic(http.ErrAbortHandler) }, WithPanicRecovery(true))

	assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
		serve(echoWith(ScopeMiddleware(openProvider(t)), handler))
	})
}

func TestContractDefaultResolutionErrorIsLoggedAndGeneric(t *testing.T) {
	logger, logs := newCapturingLogger()
	provider, err := godi.NewCollection().Build()
	assert.NoError(t, err)
	defer provider.Close()

	rec := serve(echoWith(ScopeMiddleware(provider), Handle((*testController).GetValue, WithHandlerLogger(logger))))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, logs.String(), "testController")
	assert.NotContains(t, rec.Body.String(), "testController")
}

// recordingMiddleware records the error the rest of the chain returns to it.
func recordingMiddleware(seen *[]error) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			err := next(c)
			*seen = append(*seen, err)
			return err
		}
	}
}

func TestContractErrorPassthrough(t *testing.T) {
	handlerErr := echo.NewHTTPError(http.StatusConflict, "conflict")

	t.Run("consumes downstream errors by default", func(t *testing.T) {
		var seen []error
		errorHandlerCalls := 0
		e := echo.New()
		e.HTTPErrorHandler = func(err error, c echo.Context) {
			errorHandlerCalls++
			e.DefaultHTTPErrorHandler(err, c)
		}
		e.Use(recordingMiddleware(&seen), ScopeMiddleware(openProvider(t)))
		e.GET("/test", func(echo.Context) error { return handlerErr })

		rec := serve(e)

		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Equal(t, []error{nil}, seen, "outer middleware must not see a consumed error")
		assert.Equal(t, 1, errorHandlerCalls)
	})

	t.Run("returns downstream errors to outer middleware when enabled", func(t *testing.T) {
		var seen []error
		errorHandlerCalls := 0
		var scopeAliveDuringRender bool
		e := echo.New()
		e.HTTPErrorHandler = func(err error, c echo.Context) {
			errorHandlerCalls++
			scope, scopeErr := godi.FromContext(c.Request().Context())
			if scopeErr == nil {
				_, resolveErr := godi.Resolve[*testService](scope)
				scopeAliveDuringRender = resolveErr == nil
			}
			e.DefaultHTTPErrorHandler(err, c)
		}
		e.Use(recordingMiddleware(&seen), ScopeMiddleware(openProvider(t), WithErrorPassthrough(true)))
		e.GET("/test", func(echo.Context) error { return handlerErr })

		rec := serve(e)

		assert.Equal(t, http.StatusConflict, rec.Code)
		assert.Equal(t, []error{handlerErr}, seen)
		assert.Equal(t, 1, errorHandlerCalls)
		assert.False(t, scopeAliveDuringRender, "with passthrough, rendering happens after the scope closes")
	})

	t.Run("returns middleware errors to outer middleware when enabled", func(t *testing.T) {
		var seen []error
		e := echoWith(nil, ok)
		e.Use(recordingMiddleware(&seen), ScopeMiddleware(openProvider(t),
			WithErrorPassthrough(true),
			WithMiddleware(func(godi.Scope, echo.Context) error { return errors.New("bad token") }),
			WithMiddlewareErrorHandler(func(echo.Context, error) error { return echo.ErrUnauthorized }),
		))

		rec := serve(e)

		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.Equal(t, []error{echo.ErrUnauthorized}, seen)
	})
}
