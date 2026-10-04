package gin

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
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

func serve(engine *gin.Engine) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/test", http.NoBody))
	return rec
}

func engineWith(middleware, handler gin.HandlerFunc) *gin.Engine {
	engine := gin.New()
	if middleware != nil {
		engine.Use(middleware)
	}
	engine.GET("/test", handler)
	return engine
}

func ok(c *gin.Context) { c.Status(http.StatusOK) }

func TestContractDefaultScopeErrorIsLoggedAndGeneric(t *testing.T) {
	logger, logs := newCapturingLogger()

	rec := serve(engineWith(ScopeMiddleware(closedProvider(t), WithLogger(logger)), ok))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, logs.String(), godi.ErrProviderDisposed.Error())
	assert.NotContains(t, rec.Body.String(), godi.ErrProviderDisposed.Error())
}

func TestContractDefaultMiddlewareErrorIsLoggedAndGeneric(t *testing.T) {
	logger, logs := newCapturingLogger()

	rec := serve(engineWith(ScopeMiddleware(openProvider(t),
		WithLogger(logger),
		WithMiddleware(func(godi.Scope, *gin.Context) error { return errors.New(internalDetail) }),
	), ok))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, logs.String(), internalDetail)
	assert.NotContains(t, rec.Body.String(), internalDetail)
}

func TestContractMiddlewareErrorHandler(t *testing.T) {
	unauthorized := func(c *gin.Context, _ error) { c.AbortWithStatus(http.StatusUnauthorized) }
	failingAuth := WithMiddleware(func(godi.Scope, *gin.Context) error { return errors.New("bad token") })

	t.Run("handles middleware errors", func(t *testing.T) {
		handlerCalled := false
		rec := serve(engineWith(ScopeMiddleware(openProvider(t), failingAuth, WithMiddlewareErrorHandler(unauthorized)),
			func(c *gin.Context) { handlerCalled = true; c.Status(http.StatusOK) }))
		assert.Equal(t, http.StatusUnauthorized, rec.Code)
		assert.False(t, handlerCalled)
	})

	t.Run("does not handle scope creation errors", func(t *testing.T) {
		logger, _ := newCapturingLogger()
		rec := serve(engineWith(ScopeMiddleware(closedProvider(t), WithLogger(logger), WithMiddlewareErrorHandler(unauthorized)), ok))
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("defaults to the configured error handler", func(t *testing.T) {
		rec := serve(engineWith(ScopeMiddleware(openProvider(t), failingAuth,
			WithErrorHandler(func(c *gin.Context, _ error) { c.AbortWithStatus(http.StatusTeapot) }),
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

	serve(engineWith(ScopeMiddleware(closedProvider(t)), ok))

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

	rec := serve(engineWith(ScopeMiddleware(openProvider(t)),
		Handle((*testController).Panic, WithPanicRecovery(true), WithHandlerLogger(logger))))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, logs.String(), "test panic")
	assert.Contains(t, logs.String(), "runtime/debug.Stack")
	assert.NotContains(t, rec.Body.String(), "test panic")
}

func TestContractDefaultResolutionErrorIsLoggedAndGeneric(t *testing.T) {
	logger, logs := newCapturingLogger()
	collection := godi.NewCollection()
	provider, err := collection.Build()
	assert.NoError(t, err)
	defer provider.Close()

	rec := serve(engineWith(ScopeMiddleware(provider), Handle((*testController).GetValue, WithHandlerLogger(logger))))

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, logs.String(), "testController")
	assert.NotContains(t, rec.Body.String(), "testController")
}
