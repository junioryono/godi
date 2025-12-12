package http

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

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

func (c *testController) GetValue(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(c.Service.ID))
}

func (c *testController) Panic(w http.ResponseWriter, r *http.Request) {
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

		handler := ScopeMiddleware(provider)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			scope, err := godi.FromContext(r.Context())
			assert.NoError(t, err)

			resolvedService, err = godi.Resolve[*testService](scope)
			assert.NoError(t, err)

			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.NotNil(t, resolvedService)
		assert.Equal(t, "scoped", resolvedService.ID)
	})

	t.Run("scope is closed after request", func(t *testing.T) {
		closeCalled := false

		collection := godi.NewCollection()
		collection.AddScoped(func() *testService {
			return &testService{ID: "test", Value: 1}
		})

		provider, err := collection.Build()
		assert.NoError(t, err)
		defer provider.Close()

		handler := ScopeMiddleware(provider,
			WithCloseErrorHandler(func(err error) {
				closeCalled = true
			}),
		)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		// Scope close is successful, so error handler is not called
		assert.False(t, closeCalled)
	})

	t.Run("calls error handler on scope creation failure", func(t *testing.T) {
		errorHandlerCalled := false
		var capturedError error

		collection := godi.NewCollection()
		provider, err := collection.Build()
		assert.NoError(t, err)
		provider.Close() // Close provider to cause scope creation failure

		handler := ScopeMiddleware(provider,
			WithErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
				errorHandlerCalled = true
				capturedError = err
				w.WriteHeader(http.StatusServiceUnavailable)
			}),
		)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.True(t, errorHandlerCalled)
		assert.Error(t, capturedError)
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

		handler := ScopeMiddleware(provider,
			WithMiddleware(func(scope godi.Scope, r *http.Request) error {
				mwOrder = append(mwOrder, 1)
				return nil
			}),
			WithMiddleware(func(scope godi.Scope, r *http.Request) error {
				mwOrder = append(mwOrder, 2)
				return nil
			}),
			WithMiddleware(func(scope godi.Scope, r *http.Request) error {
				mwOrder = append(mwOrder, 3)
				return nil
			}),
		)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, []int{1, 2, 3}, mwOrder)
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

		handler := ScopeMiddleware(provider,
			WithMiddleware(func(scope godi.Scope, r *http.Request) error {
				return expectedErr
			}),
			WithErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
				errorHandlerCalled = true
				assert.Equal(t, expectedErr, err)
				w.WriteHeader(http.StatusBadRequest)
			}),
		)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

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

		mux := http.NewServeMux()
		mux.HandleFunc("/value", Handle((*testController).GetValue))

		handler := ScopeMiddleware(provider)(mux)

		req := httptest.NewRequest(http.MethodGet, "/value", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		body, _ := io.ReadAll(rec.Body)
		assert.Equal(t, "handled", string(body))
	})

	t.Run("calls scope error handler when no scope", func(t *testing.T) {
		errorHandlerCalled := false

		handler := Handle((*testController).GetValue,
			WithScopeErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
				errorHandlerCalled = true
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("no scope"))
			}),
		)

		req := httptest.NewRequest(http.MethodGet, "/value", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.True(t, errorHandlerCalled)
		body, _ := io.ReadAll(rec.Body)
		assert.Contains(t, string(body), "no scope")
	})

	t.Run("calls resolution error handler when service not found", func(t *testing.T) {
		errorHandlerCalled := false

		collection := godi.NewCollection()
		// Don't register testController
		collection.AddScoped(func() *testService {
			return &testService{ID: "test", Value: 1}
		})

		provider, err := collection.Build()
		assert.NoError(t, err)
		defer provider.Close()

		handler := ScopeMiddleware(provider)(Handle((*testController).GetValue,
			WithResolutionErrorHandler(func(w http.ResponseWriter, r *http.Request, err error) {
				errorHandlerCalled = true
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte("service not found"))
			}),
		))

		req := httptest.NewRequest(http.MethodGet, "/value", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.True(t, errorHandlerCalled)
		body, _ := io.ReadAll(rec.Body)
		assert.Contains(t, string(body), "service not found")
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

		handler := ScopeMiddleware(provider)(Handle((*testController).Panic,
			WithPanicRecovery(true),
			WithPanicHandler(func(w http.ResponseWriter, r *http.Request, v any) {
				panicHandlerCalled = true
				assert.Equal(t, "test panic", v)
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("recovered"))
			}),
		))

		req := httptest.NewRequest(http.MethodGet, "/panic", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

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

		handler := ScopeMiddleware(provider)(Handle((*testController).Panic,
			WithPanicRecovery(false),
		))

		req := httptest.NewRequest(http.MethodGet, "/panic", nil)
		rec := httptest.NewRecorder()

		assert.Panics(t, func() {
			handler.ServeHTTP(rec, req)
		})
	})
}

func TestWrap(t *testing.T) {
	t.Run("wraps function as handler", func(t *testing.T) {
		collection := godi.NewCollection()
		collection.AddScoped(func() *testService {
			return &testService{ID: "wrapped", Value: 200}
		})
		collection.AddScoped(newTestController)

		provider, err := collection.Build()
		assert.NoError(t, err)
		defer provider.Close()

		handler := ScopeMiddleware(provider)(Wrap(func(ctrl *testController, w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("wrapped: " + ctrl.Service.ID))
		}))

		req := httptest.NewRequest(http.MethodGet, "/wrapped", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		assert.Equal(t, http.StatusOK, rec.Code)
		body, _ := io.ReadAll(rec.Body)
		assert.Equal(t, "wrapped: wrapped", string(body))
	})
}

func TestDefaultConfig(t *testing.T) {
	t.Run("default error handler returns 500", func(t *testing.T) {
		cfg := defaultConfig()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		cfg.ErrorHandler(rec, req, errors.New("test error"))

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("default close error handler logs error", func(t *testing.T) {
		cfg := defaultConfig()
		// Just ensure it doesn't panic
		cfg.CloseErrorHandler(errors.New("close error"))
	})
}

func TestDefaultHandlerConfig(t *testing.T) {
	t.Run("default panic handler returns 500", func(t *testing.T) {
		cfg := defaultHandlerConfig()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		cfg.PanicHandler(rec, req, "panic value")

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("default scope error handler returns 500", func(t *testing.T) {
		cfg := defaultHandlerConfig()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		cfg.ScopeErrorHandler(rec, req, errors.New("scope error"))

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

	t.Run("default resolution error handler returns 500", func(t *testing.T) {
		cfg := defaultHandlerConfig()

		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/test", nil)

		cfg.ResolutionErrorHandler(rec, req, errors.New("resolution error"))

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
	})

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

		mux := http.NewServeMux()
		mux.HandleFunc("/test", Handle(func(ctrl *testController, w http.ResponseWriter, r *http.Request) {
			requestValues["service_id"] = ctrl.Service.ID
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("OK"))
		}))

		handler := ScopeMiddleware(provider,
			WithMiddleware(func(scope godi.Scope, r *http.Request) error {
				requestValues["initialized"] = "true"
				return nil
			}),
		)(mux)

		// First request
		req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req1)

		assert.Equal(t, http.StatusOK, rec1.Code)
		assert.Equal(t, "true", requestValues["initialized"])
		assert.Equal(t, "integration", requestValues["service_id"])

		// Second request gets fresh scope
		requestValues = make(map[string]string)
		req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)

		assert.Equal(t, http.StatusOK, rec2.Code)
		assert.Equal(t, "true", requestValues["initialized"])
	})
}
