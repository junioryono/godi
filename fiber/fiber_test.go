package fiber

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/junioryono/godi/v4"
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

func (c *testController) GetValue(ctx *fiber.Ctx) error {
	return ctx.SendString(c.Service.ID)
}

func (c *testController) Panic(ctx *fiber.Ctx) error {
	panic("test panic")
}

func TestScopeMiddleware(t *testing.T) {
	t.Run("creates scope and stores in locals", func(t *testing.T) {
		collection := godi.NewCollection()
		collection.AddScoped(func() *testService {
			return &testService{ID: "scoped", Value: 42}
		})

		provider, err := collection.Build()
		assert.NoError(t, err)
		defer provider.Close()

		var resolvedService *testService

		app := fiber.New()
		app.Use(ScopeMiddleware(provider))
		app.Get("/test", func(c *fiber.Ctx) error {
			scope := FromContext(c)
			assert.NotNil(t, scope)

			resolvedService, err = godi.Resolve[*testService](scope)
			assert.NoError(t, err)

			return c.SendStatus(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		resp, err := app.Test(req)
		assert.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.NotNil(t, resolvedService)
		assert.Equal(t, "scoped", resolvedService.ID)
	})

	t.Run("scope also available from context", func(t *testing.T) {
		collection := godi.NewCollection()
		collection.AddScoped(func() *testService {
			return &testService{ID: "context-scoped", Value: 100}
		})

		provider, err := collection.Build()
		assert.NoError(t, err)
		defer provider.Close()

		var resolvedService *testService

		app := fiber.New()
		app.Use(ScopeMiddleware(provider))
		app.Get("/test", func(c *fiber.Ctx) error {
			scope, err := godi.FromContext(c.UserContext())
			assert.NoError(t, err)

			resolvedService, err = godi.Resolve[*testService](scope)
			assert.NoError(t, err)

			return c.SendStatus(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		resp, err := app.Test(req)
		assert.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "context-scoped", resolvedService.ID)
	})

	t.Run("calls error handler on scope creation failure", func(t *testing.T) {
		errorHandlerCalled := false

		collection := godi.NewCollection()
		provider, err := collection.Build()
		assert.NoError(t, err)
		provider.Close() // Close provider to cause scope creation failure

		app := fiber.New()
		app.Use(ScopeMiddleware(provider,
			WithErrorHandler(func(c *fiber.Ctx, err error) error {
				errorHandlerCalled = true
				return c.SendStatus(http.StatusServiceUnavailable)
			}),
		))
		app.Get("/test", func(c *fiber.Ctx) error {
			return c.SendStatus(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		resp, err := app.Test(req)
		assert.NoError(t, err)
		defer resp.Body.Close()

		assert.True(t, errorHandlerCalled)
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
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

		app := fiber.New()
		app.Use(ScopeMiddleware(provider,
			WithMiddleware(func(scope godi.Scope, c *fiber.Ctx) error {
				mwOrder = append(mwOrder, 1)
				return nil
			}),
			WithMiddleware(func(scope godi.Scope, c *fiber.Ctx) error {
				mwOrder = append(mwOrder, 2)
				return nil
			}),
		))
		app.Get("/test", func(c *fiber.Ctx) error {
			return c.SendStatus(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		resp, err := app.Test(req)
		assert.NoError(t, err)
		defer resp.Body.Close()

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

		app := fiber.New()
		app.Use(ScopeMiddleware(provider,
			WithMiddleware(func(scope godi.Scope, c *fiber.Ctx) error {
				return expectedErr
			}),
			WithErrorHandler(func(c *fiber.Ctx, err error) error {
				errorHandlerCalled = true
				assert.Equal(t, expectedErr, err)
				return c.SendStatus(http.StatusBadRequest)
			}),
		))
		app.Get("/test", func(c *fiber.Ctx) error {
			return c.SendStatus(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		resp, err := app.Test(req)
		assert.NoError(t, err)
		defer resp.Body.Close()

		assert.True(t, errorHandlerCalled)
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
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

		app := fiber.New()
		app.Use(ScopeMiddleware(provider))
		app.Get("/value", Handle((*testController).GetValue))

		req := httptest.NewRequest(http.MethodGet, "/value", nil)
		resp, err := app.Test(req)
		assert.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		body, _ := io.ReadAll(resp.Body)
		assert.Equal(t, "handled", string(body))
	})

	t.Run("calls scope error handler when no scope", func(t *testing.T) {
		errorHandlerCalled := false

		app := fiber.New()
		app.Get("/value", Handle((*testController).GetValue,
			WithScopeErrorHandler(func(c *fiber.Ctx, err error) error {
				errorHandlerCalled = true
				return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": "no scope"})
			}),
		))

		req := httptest.NewRequest(http.MethodGet, "/value", nil)
		resp, err := app.Test(req)
		assert.NoError(t, err)
		defer resp.Body.Close()

		assert.True(t, errorHandlerCalled)
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
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

		app := fiber.New()
		app.Use(ScopeMiddleware(provider))
		app.Get("/value", Handle((*testController).GetValue,
			WithResolutionErrorHandler(func(c *fiber.Ctx, err error) error {
				errorHandlerCalled = true
				return c.SendStatus(http.StatusNotFound)
			}),
		))

		req := httptest.NewRequest(http.MethodGet, "/value", nil)
		resp, err := app.Test(req)
		assert.NoError(t, err)
		defer resp.Body.Close()

		assert.True(t, errorHandlerCalled)
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
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

		app := fiber.New()
		app.Use(ScopeMiddleware(provider))
		app.Get("/panic", Handle((*testController).Panic,
			WithPanicRecovery(true),
			WithPanicHandler(func(c *fiber.Ctx, v any) error {
				panicHandlerCalled = true
				assert.Equal(t, "test panic", v)
				return c.SendStatus(http.StatusInternalServerError)
			}),
		))

		req := httptest.NewRequest(http.MethodGet, "/panic", nil)
		resp, err := app.Test(req)
		assert.NoError(t, err)
		defer resp.Body.Close()

		assert.True(t, panicHandlerCalled)
		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	})
}

func TestFromContext(t *testing.T) {
	t.Run("returns nil when no scope", func(t *testing.T) {
		app := fiber.New()
		app.Get("/test", func(c *fiber.Ctx) error {
			scope := FromContext(c)
			assert.Nil(t, scope)
			return c.SendStatus(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		resp, err := app.Test(req)
		assert.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("returns scope when present", func(t *testing.T) {
		collection := godi.NewCollection()
		collection.AddScoped(func() *testService {
			return &testService{ID: "test", Value: 1}
		})

		provider, err := collection.Build()
		assert.NoError(t, err)
		defer provider.Close()

		var scopeFound bool

		app := fiber.New()
		app.Use(ScopeMiddleware(provider))
		app.Get("/test", func(c *fiber.Ctx) error {
			scope := FromContext(c)
			scopeFound = scope != nil
			return c.SendStatus(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		resp, err := app.Test(req)
		assert.NoError(t, err)
		defer resp.Body.Close()

		assert.True(t, scopeFound)
	})
}

func TestDefaultConfig(t *testing.T) {
	t.Run("default error handler returns JSON error", func(t *testing.T) {
		cfg := defaultConfig()

		app := fiber.New()
		app.Get("/test", func(c *fiber.Ctx) error {
			return cfg.ErrorHandler(c, errors.New("test error"))
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		resp, err := app.Test(req)
		assert.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusInternalServerError, resp.StatusCode)
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

		app := fiber.New()
		app.Use(ScopeMiddleware(provider,
			WithMiddleware(func(scope godi.Scope, c *fiber.Ctx) error {
				requestValues["initialized"] = "true"
				return nil
			}),
		))
		app.Get("/test", Handle(func(ctrl *testController, c *fiber.Ctx) error {
			requestValues["service_id"] = ctrl.Service.ID
			return c.SendString("OK")
		}))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		resp, err := app.Test(req)
		assert.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "true", requestValues["initialized"])
		assert.Equal(t, "integration", requestValues["service_id"])
	})
}
