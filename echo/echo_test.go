package echo

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/junioryono/godi/v4"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
)

// Test types
type testService struct {
	ID    string
	Value int
}

type testController struct {
	Service *testService
}

func newTestController(svc *testService) *testController {
	return &testController{Service: svc}
}

func (c *testController) GetValue(ctx echo.Context) error {
	return ctx.String(http.StatusOK, c.Service.ID)
}

func (c *testController) Panic(ctx echo.Context) error {
	panic("test panic")
}

func TestScopeMiddleware(t *testing.T) {
	t.Run("creates scope and attaches to context", func(t *testing.T) {
		collection := godi.NewCollection()
		collection.AddScoped(func() *testService {
			return &testService{ID: "scoped", Value: 42}
		})

		provider, err := collection.Build()
		assert.NoError(t, err)
		defer provider.Close()

		var resolvedService *testService

		e := echo.New()
		e.Use(ScopeMiddleware(provider))
		e.GET("/test", func(c echo.Context) error {
			scope, err := godi.FromContext(c.Request().Context())
			assert.NoError(t, err)

			resolvedService, err = godi.Resolve[*testService](scope)
			assert.NoError(t, err)

			return c.NoContent(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.NotNil(t, resolvedService)
		assert.Equal(t, "scoped", resolvedService.ID)
	})

	t.Run("calls error handler on scope creation failure", func(t *testing.T) {
		errorHandlerCalled := false

		collection := godi.NewCollection()
		provider, err := collection.Build()
		assert.NoError(t, err)
		provider.Close() // Close provider to cause scope creation failure

		e := echo.New()
		e.Use(ScopeMiddleware(provider,
			WithErrorHandler(func(c echo.Context, err error) error {
				errorHandlerCalled = true
				return c.NoContent(http.StatusServiceUnavailable)
			}),
		))
		e.GET("/test", func(c echo.Context) error {
			return c.NoContent(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.True(t, errorHandlerCalled)
		assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	})

	t.Run("runs middlewares in order", func(t *testing.T) {
		var mwOrder []int

		collection := godi.NewCollection()
		collection.AddScoped(func() *testService {
			return &testService{ID: "test", Value: 1}
		})

		provider, err := collection.Build()
		assert.NoError(t, err)
		defer provider.Close()

		e := echo.New()
		e.Use(ScopeMiddleware(provider,
			WithMiddleware(func(scope godi.Scope, c echo.Context) error {
				mwOrder = append(mwOrder, 1)
				return nil
			}),
			WithMiddleware(func(scope godi.Scope, c echo.Context) error {
				mwOrder = append(mwOrder, 2)
				return nil
			}),
		))
		e.GET("/test", func(c echo.Context) error {
			return c.NoContent(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, []int{1, 2}, mwOrder)
	})

	t.Run("calls error handler when middleware fails", func(t *testing.T) {
		errorHandlerCalled := false
		expectedErr := errors.New("middleware failed")

		collection := godi.NewCollection()
		collection.AddScoped(func() *testService {
			return &testService{ID: "test", Value: 1}
		})

		provider, err := collection.Build()
		assert.NoError(t, err)
		defer provider.Close()

		e := echo.New()
		e.Use(ScopeMiddleware(provider,
			WithMiddleware(func(scope godi.Scope, c echo.Context) error {
				return expectedErr
			}),
			WithErrorHandler(func(c echo.Context, err error) error {
				errorHandlerCalled = true
				assert.Equal(t, expectedErr, err)
				return c.NoContent(http.StatusBadRequest)
			}),
		))
		e.GET("/test", func(c echo.Context) error {
			return c.NoContent(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.True(t, errorHandlerCalled)
		assert.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestHandle(t *testing.T) {
	t.Run("resolves controller and calls method", func(t *testing.T) {
		collection := godi.NewCollection()
		collection.AddScoped(func() *testService {
			return &testService{ID: "handled", Value: 100}
		})
		collection.AddScoped(newTestController)

		provider, err := collection.Build()
		assert.NoError(t, err)
		defer provider.Close()

		e := echo.New()
		e.Use(ScopeMiddleware(provider))
		e.GET("/value", Handle((*testController).GetValue))

		req := httptest.NewRequest(http.MethodGet, "/value", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		body, _ := io.ReadAll(rec.Body)
		assert.Equal(t, "handled", string(body))
	})

	t.Run("calls scope error handler when no scope", func(t *testing.T) {
		errorHandlerCalled := false

		e := echo.New()
		e.GET("/value", Handle((*testController).GetValue,
			WithScopeErrorHandler(func(c echo.Context, err error) error {
				errorHandlerCalled = true
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": "no scope"})
			}),
		))

		req := httptest.NewRequest(http.MethodGet, "/value", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.True(t, errorHandlerCalled)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("calls resolution error handler when service not found", func(t *testing.T) {
		errorHandlerCalled := false

		collection := godi.NewCollection()
		collection.AddScoped(func() *testService {
			return &testService{ID: "test", Value: 1}
		})

		provider, err := collection.Build()
		assert.NoError(t, err)
		defer provider.Close()

		e := echo.New()
		e.Use(ScopeMiddleware(provider))
		e.GET("/value", Handle((*testController).GetValue,
			WithResolutionErrorHandler(func(c echo.Context, err error) error {
				errorHandlerCalled = true
				return c.NoContent(http.StatusNotFound)
			}),
		))

		req := httptest.NewRequest(http.MethodGet, "/value", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.True(t, errorHandlerCalled)
		assert.Equal(t, http.StatusNotFound, rec.Code)
	})

	t.Run("recovers from panic when enabled", func(t *testing.T) {
		panicHandlerCalled := false

		collection := godi.NewCollection()
		collection.AddScoped(func() *testService {
			return &testService{ID: "test", Value: 1}
		})
		collection.AddScoped(newTestController)

		provider, err := collection.Build()
		assert.NoError(t, err)
		defer provider.Close()

		e := echo.New()
		e.Use(ScopeMiddleware(provider))
		e.GET("/panic", Handle((*testController).Panic,
			WithPanicRecovery(true),
			WithPanicHandler(func(c echo.Context, v any) error {
				panicHandlerCalled = true
				assert.Equal(t, "test panic", v)
				return c.NoContent(http.StatusInternalServerError)
			}),
		))

		req := httptest.NewRequest(http.MethodGet, "/panic", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.True(t, panicHandlerCalled)
		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("does not recover from panic when disabled", func(t *testing.T) {
		collection := godi.NewCollection()
		collection.AddScoped(func() *testService {
			return &testService{ID: "test", Value: 1}
		})
		collection.AddScoped(newTestController)

		provider, err := collection.Build()
		assert.NoError(t, err)
		defer provider.Close()

		e := echo.New()
		e.Use(ScopeMiddleware(provider))
		e.GET("/panic", Handle((*testController).Panic,
			WithPanicRecovery(false),
		))

		req := httptest.NewRequest(http.MethodGet, "/panic", nil)
		rec := httptest.NewRecorder()

		assert.Panics(t, func() {
			e.ServeHTTP(rec, req)
		})
	})
}

func TestDefaultConfig(t *testing.T) {
	t.Run("default error handler returns HTTPError", func(t *testing.T) {
		cfg := defaultConfig()

		e := echo.New()
		e.GET("/test", func(c echo.Context) error {
			return cfg.ErrorHandler(c, errors.New("test error"))
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})
}

func TestDefaultHandlerConfig(t *testing.T) {
	t.Run("panic recovery disabled by default", func(t *testing.T) {
		cfg := defaultHandlerConfig()
		assert.False(t, cfg.PanicRecovery)
	})
}

func TestIntegration(t *testing.T) {
	t.Run("full request lifecycle", func(t *testing.T) {
		requestValues := make(map[string]string)

		collection := godi.NewCollection()
		collection.AddScoped(func() *testService {
			return &testService{ID: "integration", Value: 999}
		})
		collection.AddScoped(newTestController)

		provider, err := collection.Build()
		assert.NoError(t, err)
		defer provider.Close()

		e := echo.New()
		e.Use(ScopeMiddleware(provider,
			WithMiddleware(func(scope godi.Scope, c echo.Context) error {
				requestValues["initialized"] = "true"
				return nil
			}),
		))
		e.GET("/test", Handle(func(ctrl *testController, c echo.Context) error {
			requestValues["service_id"] = ctrl.Service.ID
			return c.String(http.StatusOK, "OK")
		}))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		e.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "true", requestValues["initialized"])
		assert.Equal(t, "integration", requestValues["service_id"])
	})
}
