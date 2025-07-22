package godi_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/junioryono/godi/v2"
	"github.com/junioryono/godi/v2/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScope_Creation(t *testing.T) {
	t.Run("creates scope with context", func(t *testing.T) {
		t.Parallel()

		provider := testutil.CreateProviderWithBasicServices(t)
		type testKeyType struct{}
		var testKey = testKeyType{}
		ctx := context.WithValue(context.Background(), testKey, "test-value")

		scope := provider.CreateScope(ctx)
		assert.NotNil(t, scope)
		assert.False(t, scope.IsDisposed())
		assert.NotEmpty(t, scope.ID())

		scopeCtx := scope.Context()
		assert.Equal(t, "test-value", scopeCtx.Value(testKey))

		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})
	})

	t.Run("creates scope with nil context", func(t *testing.T) {
		t.Parallel()

		provider := testutil.CreateProviderWithBasicServices(t)

		scope := provider.CreateScope(context.Background())
		assert.NotNil(t, scope)
		assert.NotNil(t, scope.Context())

		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})
	})

	t.Run("nested scope creation", func(t *testing.T) {
		t.Parallel()

		provider := testutil.CreateProviderWithBasicServices(t)

		scope1 := provider.CreateScope(context.Background())
		scope2 := scope1.CreateScope(context.Background())
		scope3 := scope2.CreateScope(context.Background())

		// Verify hierarchy
		assert.False(t, scope1.IsRootScope())
		assert.NotNil(t, scope1.Parent())
		assert.Equal(t, provider.GetRootScope().ID(), scope1.Parent().ID())

		assert.False(t, scope2.IsRootScope())
		assert.NotNil(t, scope2.Parent())
		assert.Equal(t, scope1.ID(), scope2.Parent().ID())

		assert.False(t, scope3.IsRootScope())
		assert.NotNil(t, scope3.Parent())
		assert.Equal(t, scope2.ID(), scope3.Parent().ID())

		// Clean up in reverse order
		require.NoError(t, scope3.Close())
		require.NoError(t, scope2.Close())
		require.NoError(t, scope1.Close())
	})

	t.Run("root scope identification", func(t *testing.T) {
		t.Parallel()

		provider := testutil.CreateProviderWithBasicServices(t)
		rootScope := provider.GetRootScope()

		assert.True(t, rootScope.IsRootScope())
		assert.Nil(t, rootScope.Parent())
		assert.Equal(t, rootScope.ID(), rootScope.GetRootScope().ID())

		// Child scope should not be root
		childScope := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, childScope.Close())
		})

		assert.False(t, childScope.IsRootScope())
		assert.Equal(t, rootScope.ID(), childScope.GetRootScope().ID())
	})
}

func TestScope_ScopedServices(t *testing.T) {
	t.Run("scoped services are unique per scope", func(t *testing.T) {
		t.Parallel()

		provider := testutil.NewServiceCollectionBuilder(t).
			WithScoped(testutil.NewTestService).
			BuildProvider()

		scope1 := provider.CreateScope(context.Background())
		scope2 := provider.CreateScope(context.Background())

		t.Cleanup(func() {
			require.NoError(t, scope1.Close())
			require.NoError(t, scope2.Close())
		})

		// Resolve in different scopes
		service1 := testutil.AssertServiceResolvableInScope[*testutil.TestService](t, scope1)
		service2 := testutil.AssertServiceResolvableInScope[*testutil.TestService](t, scope2)

		// Should be different instances
		testutil.AssertDifferentInstances(t, service1, service2)
		assert.NotEqual(t, service1.ID, service2.ID)
	})

	t.Run("scoped services are same within scope", func(t *testing.T) {
		t.Parallel()

		provider := testutil.NewServiceCollectionBuilder(t).
			WithScoped(testutil.NewTestService).
			BuildProvider()

		scope := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		// Multiple resolutions in same scope
		service1 := testutil.AssertServiceResolvableInScope[*testutil.TestService](t, scope)
		service2 := testutil.AssertServiceResolvableInScope[*testutil.TestService](t, scope)

		// Should be same instance
		testutil.AssertSameInstance(t, service1, service2)
	})

	t.Run("scoped service can depend on singleton", func(t *testing.T) {
		t.Parallel()

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithSingleton(testutil.NewTestDatabase).
			WithSingleton(testutil.NewTestCache).
			WithScoped(testutil.NewTestServiceWithDeps).
			BuildProvider()

		scope := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		service := testutil.AssertServiceResolvableInScope[*testutil.TestServiceWithDeps](t, scope)
		assert.NotNil(t, service.Logger)
		assert.NotNil(t, service.Database)
		assert.NotNil(t, service.Cache)
	})

	t.Run("context is available in scoped constructors", func(t *testing.T) {
		t.Parallel()

		type ContextAwareService struct {
			RequestID string
		}

		// Define a custom type for context key
		type requestIDKeyType struct{}
		var requestIDKey = requestIDKeyType{}

		constructor := func(ctx context.Context) *ContextAwareService {
			requestID, _ := ctx.Value(requestIDKey).(string)
			return &ContextAwareService{RequestID: requestID}
		}

		provider := testutil.NewServiceCollectionBuilder(t).
			WithScoped(constructor).
			BuildProvider()

		ctx := context.WithValue(context.Background(), requestIDKey, "test-123")
		scope := provider.CreateScope(ctx)
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		service := testutil.AssertServiceResolvableInScope[*ContextAwareService](t, scope)
		assert.Equal(t, "test-123", service.RequestID)
	})
}

func TestScope_Disposal(t *testing.T) {
	t.Run("disposes scoped services", func(t *testing.T) {
		t.Parallel()

		disposable := testutil.NewTestDisposable()

		provider := testutil.NewServiceCollectionBuilder(t).
			WithScoped(func() *testutil.TestDisposable {
				return disposable
			}).
			BuildProvider()

		scope := provider.CreateScope(context.Background())

		// Resolve to create instance
		d := testutil.AssertServiceResolvableInScope[*testutil.TestDisposable](t, scope)
		assert.False(t, d.IsDisposed())

		// Close scope
		err := scope.Close()
		require.NoError(t, err)

		// Service should be disposed
		assert.True(t, disposable.IsDisposed())
		assert.True(t, scope.IsDisposed())
	})

	t.Run("disposes in LIFO order", func(t *testing.T) {
		t.Parallel()

		var disposalOrder []string
		mu := sync.Mutex{}

		createDisposable := func(name string) func() godi.Disposable {
			return func() godi.Disposable {
				return testutil.CloserFunc(func() error {
					mu.Lock()
					disposalOrder = append(disposalOrder, name)
					mu.Unlock()
					return nil
				})
			}
		}

		provider := testutil.NewServiceCollectionBuilder(t).
			WithScoped(createDisposable("first"), godi.Name("first")).
			WithScoped(createDisposable("second"), godi.Name("second")).
			WithScoped(createDisposable("third"), godi.Name("third")).
			BuildProvider()

		scope := provider.CreateScope(context.Background())

		testutil.AssertKeyedServiceResolvable[godi.Disposable](t, scope, "second")
		testutil.AssertKeyedServiceResolvable[godi.Disposable](t, scope, "first")
		testutil.AssertKeyedServiceResolvable[godi.Disposable](t, scope, "third")

		require.NoError(t, scope.Close())

		// Should dispose in reverse order of resolution
		assert.Equal(t, []string{"third", "first", "second"}, disposalOrder)
	})

	t.Run("handles disposal errors", func(t *testing.T) {
		t.Parallel()

		expectedErr1 := errors.New("disposal error 1")
		expectedErr2 := errors.New("disposal error 2")

		// Create unique types for each disposable
		type Disposable1 struct{ *testutil.TestDisposable }
		type Disposable2 struct{ *testutil.TestDisposable }

		provider := testutil.NewServiceCollectionBuilder(t).
			WithScoped(func() *Disposable1 {
				return &Disposable1{testutil.NewTestDisposableWithError(expectedErr1)}
			}).
			WithScoped(func() *Disposable2 {
				return &Disposable2{testutil.NewTestDisposableWithError(expectedErr2)}
			}).
			BuildProvider()

		scope := provider.CreateScope(context.Background())

		// Resolve to create instances
		testutil.AssertServiceResolvableInScope[*Disposable1](t, scope)
		testutil.AssertServiceResolvableInScope[*Disposable2](t, scope)

		// Close should return joined errors
		err := scope.Close()
		assert.Error(t, err)
	})

	t.Run("context-aware disposal", func(t *testing.T) {
		t.Parallel()

		contextDisposable := testutil.NewTestContextDisposable()
		contextDisposable.SetDisposeTime(50 * time.Millisecond)

		provider := testutil.NewServiceCollectionBuilder(t).
			WithScoped(func() *testutil.TestContextDisposable {
				return contextDisposable
			}).
			BuildProvider()

		// Create scope with timeout context
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()

		scope := provider.CreateScope(ctx)

		// Resolve to create instance
		testutil.AssertServiceResolvableInScope[*testutil.TestContextDisposable](t, scope)

		// Close should respect context
		err := scope.Close()
		assert.NoError(t, err)
		assert.True(t, contextDisposable.WasDisposedWithContext())
	})

	t.Run("prevents operations after disposal", func(t *testing.T) {
		t.Parallel()

		provider := testutil.CreateProviderWithCompleteServices(t)
		scope := provider.CreateScope(context.Background())

		require.NoError(t, scope.Close())

		testutil.AssertScopeDisposed(t, scope)
	})

	t.Run("idempotent close", func(t *testing.T) {
		t.Parallel()

		provider := testutil.CreateProviderWithBasicServices(t)
		scope := provider.CreateScope(context.Background())

		err1 := scope.Close()
		err2 := scope.Close()

		assert.NoError(t, err1)
		assert.NoError(t, err2)
	})
}

func TestScope_ServiceResolution(t *testing.T) {
	t.Run("resolves all service types in scope", func(t *testing.T) {
		t.Parallel()

		provider := testutil.CreateProviderWithCompleteServices(t)
		scope := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		// Test regular resolution
		logger := testutil.AssertServiceResolvableInScope[testutil.TestLogger](t, scope)
		assert.NotNil(t, logger)

		// Test keyed resolution
		keyedProvider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger, godi.Name("primary")).
			BuildProvider()

		keyedScope := keyedProvider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, keyedScope.Close())
		})

		keyedLogger := testutil.AssertKeyedServiceResolvable[testutil.TestLogger](t, keyedScope, "primary")
		assert.NotNil(t, keyedLogger)

		// Test group resolution
		groupProvider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(func() testutil.TestHandler {
				return testutil.NewTestHandler("h1")
			}, godi.Group("handlers")).
			WithSingleton(func() testutil.TestHandler {
				return testutil.NewTestHandler("h2")
			}, godi.Group("handlers")).
			BuildProvider()

		groupScope := groupProvider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, groupScope.Close())
		})

		handlers, err := godi.ResolveGroup[testutil.TestHandler](groupScope, "handlers")
		require.NoError(t, err)
		assert.Len(t, handlers, 2)
	})

	t.Run("scope inherits provider service checks", func(t *testing.T) {
		t.Parallel()

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithSingleton(testutil.NewTestDatabase, godi.Name("primary")).
			BuildProvider()

		scope := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		// Should have same service availability
		loggerType := reflect.TypeOf((*testutil.TestLogger)(nil)).Elem()
		dbType := reflect.TypeOf((*testutil.TestDatabase)(nil)).Elem()

		assert.True(t, scope.IsService(loggerType))
		assert.False(t, scope.IsService(dbType))
		assert.True(t, scope.IsKeyedService(dbType, "primary"))
		assert.False(t, scope.IsKeyedService(dbType, "secondary"))
	})

	t.Run("invoke works in scope", func(t *testing.T) {
		t.Parallel()

		provider := testutil.CreateProviderWithCompleteServices(t)
		scope := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		var invoked bool
		err := scope.Invoke(func(logger testutil.TestLogger, service *testutil.TestServiceWithDeps) {
			invoked = true
			assert.NotNil(t, logger)
			assert.NotNil(t, service)
		})

		require.NoError(t, err)
		assert.True(t, invoked)
	})
}

func TestScope_Concurrency(t *testing.T) {
	t.Run("concurrent resolution in scope", func(t *testing.T) {
		t.Parallel()

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithScoped(testutil.NewTestService).
			BuildProvider()

		scope := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		const goroutines = 50
		var wg sync.WaitGroup
		wg.Add(goroutines)

		services := make([]*testutil.TestService, goroutines)
		errors := make([]error, goroutines)

		for i := 0; i < goroutines; i++ {
			go func(idx int) {
				defer wg.Done()
				svc, err := godi.Resolve[*testutil.TestService](scope)
				services[idx] = svc
				errors[idx] = err
			}(i)
		}

		wg.Wait()

		// All should succeed
		for i, err := range errors {
			assert.NoError(t, err, "goroutine %d failed", i)
		}

		// All should be same instance (scoped)
		firstService := services[0]
		for i, svc := range services {
			testutil.AssertSameInstance(t, firstService, svc, "service %d differs", i)
		}
	})

	t.Run("concurrent scope creation and disposal", func(t *testing.T) {
		t.Parallel()

		provider := testutil.NewServiceCollectionBuilder(t).
			WithScoped(testutil.NewTestDisposable).
			BuildProvider()

		const iterations = 20
		var wg sync.WaitGroup

		for i := 0; i < iterations; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()

				scope := provider.CreateScope(context.Background())

				// Resolve service
				d := testutil.AssertServiceResolvableInScope[*testutil.TestDisposable](t, scope)
				assert.False(t, d.IsDisposed())

				// Close scope
				err := scope.Close()
				assert.NoError(t, err)
				assert.True(t, d.IsDisposed())
			}()
		}

		wg.Wait()
	})
}

func TestScope_LifetimeValidation(t *testing.T) {
	t.Run("singleton cannot depend on scoped - caught at resolution", func(t *testing.T) {
		t.Parallel()

		type SingletonService struct {
			Scoped *testutil.TestService
		}

		provider := testutil.NewServiceCollectionBuilder(t).
			WithScoped(testutil.NewTestService).
			WithSingleton(func(scoped *testutil.TestService) *SingletonService {
				return &SingletonService{Scoped: scoped}
			}).
			BuildProvider()

		// Should fail when trying to resolve singleton that depends on scoped
		_, err := godi.Resolve[*SingletonService](provider)
		assert.Error(t, err)
		// The error might be "not found" because scoped services aren't available at root
		assert.True(t, godi.IsNotFound(err))
	})

	t.Run("scoped services isolated between scopes", func(t *testing.T) {
		t.Parallel()

		var instances []*testutil.TestService
		mu := sync.Mutex{}

		constructor := func() *testutil.TestService {
			svc := testutil.NewTestService()
			mu.Lock()
			instances = append(instances, svc)
			mu.Unlock()
			return svc
		}

		provider := testutil.NewServiceCollectionBuilder(t).
			WithScoped(constructor).
			BuildProvider()

		// Create multiple scopes
		for i := 0; i < 3; i++ {
			scope := provider.CreateScope(context.Background())
			svc := testutil.AssertServiceResolvableInScope[*testutil.TestService](t, scope)
			assert.NotNil(t, svc)
			require.NoError(t, scope.Close())
		}

		// Should have created 3 different instances
		assert.Len(t, instances, 3)

		// All should be different
		for i := 0; i < len(instances); i++ {
			for j := i + 1; j < len(instances); j++ {
				assert.NotEqual(t, instances[i].ID, instances[j].ID)
			}
		}
	})
}

func TestScope_FromContext(t *testing.T) {
	t.Run("retrieves scope from context", func(t *testing.T) {
		t.Parallel()

		provider := testutil.CreateProviderWithBasicServices(t)
		scope := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		// Get context from scope
		ctx := scope.Context()

		// Should be able to retrieve scope
		retrievedScope, err := godi.ScopeFromContext(ctx)
		require.NoError(t, err)
		assert.Equal(t, scope.ID(), retrievedScope.ID())
	})

	t.Run("returns error for missing scope", func(t *testing.T) {
		t.Parallel()

		ctx := context.Background()
		_, err := godi.ScopeFromContext(ctx)
		assert.ErrorIs(t, err, godi.ErrScopeNotInContext)
	})

	t.Run("returns error for disposed scope", func(t *testing.T) {
		t.Parallel()

		provider := testutil.CreateProviderWithBasicServices(t)
		scope := provider.CreateScope(context.Background())
		ctx := scope.Context()

		// Close scope
		require.NoError(t, scope.Close())

		// Should return error
		_, err := godi.ScopeFromContext(ctx)
		assert.ErrorIs(t, err, godi.ErrScopeDisposed)
	})
}

func TestScope_EdgeCases(t *testing.T) {
	t.Run("deeply nested scopes", func(t *testing.T) {
		t.Parallel()

		provider := testutil.CreateProviderWithBasicServices(t)

		// Create deeply nested scopes
		scopes := make([]godi.Scope, 10)
		currentScope := provider.GetRootScope()

		for i := 0; i < 10; i++ {
			newScope := currentScope.CreateScope(context.Background())
			scopes[i] = newScope
			currentScope = newScope
		}

		// Verify all work
		for i, scope := range scopes {
			logger := testutil.AssertServiceResolvableInScope[testutil.TestLogger](t, scope)
			assert.NotNil(t, logger, "scope %d", i)
		}

		// Clean up in reverse order
		for i := len(scopes) - 1; i >= 0; i-- {
			require.NoError(t, scopes[i].Close())
		}
	})

	t.Run("scope with canceled context", func(t *testing.T) {
		t.Parallel()

		// Track if disposal happened
		var disposed bool
		mu := sync.Mutex{}

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithScoped(func() godi.Disposable {
				return testutil.CloserFunc(func() error {
					mu.Lock()
					disposed = true
					mu.Unlock()
					return nil
				})
			}).
			BuildProvider()

		ctx, cancel := context.WithCancel(context.Background())
		scope := provider.CreateScope(ctx)

		// Resolve the disposable service to ensure it's created
		disposable := testutil.AssertServiceResolvableInScope[godi.Disposable](t, scope)
		assert.NotNil(t, disposable)

		// Also resolve logger to verify normal services work
		logger := testutil.AssertServiceResolvableInScope[testutil.TestLogger](t, scope)
		assert.NotNil(t, logger)

		// Cancel context
		cancel()

		// Context should be canceled
		select {
		case <-scope.Context().Done():
			// Expected
		default:
			t.Fatal("context should be canceled")
		}

		// Close scope - should still dispose services even with canceled context
		err := scope.Close()
		require.NoError(t, err)

		// Verify disposal happened
		mu.Lock()
		assert.True(t, disposed, "service should be disposed even with canceled context")
		mu.Unlock()
	})

	t.Run("scope with timeout context and slow disposal", func(t *testing.T) {
		t.Parallel()

		slowDisposable := testutil.NewTestContextDisposable()
		slowDisposable.SetDisposeTime(100 * time.Millisecond)

		provider := testutil.NewServiceCollectionBuilder(t).
			WithScoped(func() *testutil.TestContextDisposable {
				return slowDisposable
			}).
			BuildProvider()

		// Create scope with short timeout
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
		defer cancel()

		scope := provider.CreateScope(ctx)

		// Resolve to create instance
		testutil.AssertServiceResolvableInScope[*testutil.TestContextDisposable](t, scope)

		// Wait for context to timeout
		<-ctx.Done()

		// Close should handle timeout during disposal
		err := scope.Close()
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})
}

// Table-driven tests for scope scenarios
func TestScope_Scenarios(t *testing.T) {
	type RequestScoped struct {
		ID        string
		Timestamp time.Time
	}

	scenarios := []testutil.TestScenario{
		{
			Name: "web request simulation",
			Setup: func(t *testing.T) godi.ServiceProvider {

				return testutil.NewServiceCollectionBuilder(t).
					WithSingleton(testutil.NewTestLogger).
					WithScoped(func() *RequestScoped {
						return &RequestScoped{
							ID:        "req-" + time.Now().Format("150405"),
							Timestamp: time.Now(),
						}
					}).
					BuildProvider()
			},
			Validate: func(t *testing.T, provider godi.ServiceProvider) {
				// Simulate multiple concurrent requests
				var wg sync.WaitGroup

				type requestIDKeyType struct{}
				var requestIDKey = requestIDKeyType{}

				for i := 0; i < 5; i++ {
					wg.Add(1)
					go func(reqNum int) {
						defer wg.Done()

						// Create request scope
						ctx := context.WithValue(context.Background(), requestIDKey, reqNum)
						scope := provider.CreateScope(ctx)
						defer scope.Close()

						// Services in same request should be same
						svc1 := testutil.AssertServiceResolvableInScope[*RequestScoped](t, scope)
						svc2 := testutil.AssertServiceResolvableInScope[*RequestScoped](t, scope)

						assert.Equal(t, svc1.ID, svc2.ID)
					}(i)
				}
				wg.Wait()
			},
		},
		{
			Name: "background job with scoped resources",
			Setup: func(t *testing.T) godi.ServiceProvider {
				return testutil.CreateProviderWithCompleteServices(t)
			},
			Validate: func(t *testing.T, provider godi.ServiceProvider) {
				// Simulate background job
				jobCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
				defer cancel()

				scope := provider.CreateScope(jobCtx)
				defer scope.Close()

				// Use services
				service := testutil.AssertServiceResolvableInScope[*testutil.TestServiceWithDeps](t, scope)
				assert.NotNil(t, service)

				// Simulate work
				service.Logger.Log("Job started")
				time.Sleep(10 * time.Millisecond)
				service.Logger.Log("Job completed")
			},
		},
	}

	testutil.RunTestScenarios(t, scenarios)
}

func TestContextRegistrationConflict(t *testing.T) {
	t.Run("user-registered context should not conflict with scope context", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// User registers their own context
		userContext := context.WithValue(context.Background(), ctxKeyRequestID{}, "user-value")
		err := collection.AddSingleton(func() context.Context {
			return userContext
		})
		require.NoError(t, err)

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Creating a scope should not panic
		scopeCtx := context.WithValue(context.Background(), ctxKeyRequestID{}, "scope-value")
		scope := provider.CreateScope(scopeCtx)
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		// Resolve context in scope
		ctx, err := godi.Resolve[context.Context](scope)
		require.NoError(t, err)

		// The resolved context should be the wrapped scope context
		// It should contain both the scope information and the original context value
		assert.Equal(t, "scope-value", ctx.Value(ctxKeyRequestID{}))

		// Verify it's the same context as scope.Context()
		assert.Equal(t, scope.Context(), ctx)
	})
}
