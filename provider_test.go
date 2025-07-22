package godi_test

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/junioryono/godi/v2"
	"github.com/junioryono/godi/v2/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceProvider_Creation(t *testing.T) {
	t.Run("creates provider from empty collection", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()

		require.NoError(t, err)
		assert.NotNil(t, provider)
		assert.False(t, provider.IsDisposed())

		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})
	})

	t.Run("creates provider with services", func(t *testing.T) {
		t.Parallel()

		builder := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithScoped(testutil.NewTestService)

		provider := builder.BuildProvider()

		assert.NotNil(t, provider)
		assert.True(t, provider.IsService(reflect.TypeOf((*testutil.TestLogger)(nil)).Elem()))
		assert.True(t, provider.IsService(reflect.TypeOf((*testutil.TestService)(nil))))
	})

	t.Run("validates services on build when requested", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add service with missing dependency
		require.NoError(t, collection.AddSingleton(
			func(missing testutil.TestCache) testutil.TestLogger {
				return testutil.NewTestLogger()
			},
		))

		options := &godi.ServiceProviderOptions{
			ValidateOnBuild: true,
		}

		_, err := collection.BuildServiceProviderWithOptions(options)
		assert.Error(t, err)
		assert.True(t, godi.IsNotFound(err))
	})

	t.Run("accepts custom options", func(t *testing.T) {
		t.Parallel()

		var resolved []reflect.Type
		var resolutionErrors []error

		options := &godi.ServiceProviderOptions{
			OnServiceResolved: func(serviceType reflect.Type, instance interface{}, duration time.Duration) {
				resolved = append(resolved, serviceType)
			},
			OnServiceError: func(serviceType reflect.Type, err error) {
				resolutionErrors = append(resolutionErrors, err)
			},
			ResolutionTimeout: 5 * time.Second,
		}

		builder := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger)

		provider, err := builder.Build().BuildServiceProviderWithOptions(options)
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Resolve a service
		logger := testutil.AssertServiceResolvable[testutil.TestLogger](t, provider)
		assert.NotNil(t, logger)

		// Check callback was called
		assert.Len(t, resolved, 1)
		assert.Equal(t, reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(), resolved[0])

		// Try to resolve missing service
		_, err = provider.Resolve(reflect.TypeOf((*testutil.TestCache)(nil)).Elem())
		assert.Error(t, err)

		// Check error callback was called
		assert.Len(t, resolutionErrors, 1)
	})
}

func TestServiceProvider_Resolution(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T) godi.ServiceProvider
		resolve  func(provider godi.ServiceProvider) (interface{}, error)
		validate func(t *testing.T, result interface{}, err error)
		wantErr  bool
	}{
		{
			name: "resolves singleton service",
			setup: func(t *testing.T) godi.ServiceProvider {
				return testutil.NewServiceCollectionBuilder(t).
					WithSingleton(testutil.NewTestLogger).
					BuildProvider()
			},
			resolve: func(provider godi.ServiceProvider) (interface{}, error) {
				return godi.Resolve[testutil.TestLogger](provider)
			},
			validate: func(t *testing.T, result interface{}, err error) {
				assert.NotNil(t, result)
				logger, ok := result.(testutil.TestLogger)
				assert.True(t, ok)
				assert.NotNil(t, logger)
			},
		},
		{
			name: "resolves scoped service from root scope",
			setup: func(t *testing.T) godi.ServiceProvider {
				return testutil.NewServiceCollectionBuilder(t).
					WithScoped(testutil.NewTestService).
					BuildProvider()
			},
			resolve: func(provider godi.ServiceProvider) (interface{}, error) {
				return godi.Resolve[*testutil.TestService](provider)
			},
			validate: func(t *testing.T, result interface{}, err error) {
				assert.NotNil(t, result)
				service, ok := result.(*testutil.TestService)
				assert.True(t, ok)
				assert.NotEmpty(t, service.ID)
			},
		},
		{
			name: "resolves service with dependencies",
			setup: func(t *testing.T) godi.ServiceProvider {
				return testutil.NewServiceCollectionBuilder(t).
					WithSingleton(testutil.NewTestLogger).
					WithSingleton(testutil.NewTestDatabase).
					WithSingleton(testutil.NewTestCache).
					WithScoped(testutil.NewTestServiceWithDeps).
					BuildProvider()
			},
			resolve: func(provider godi.ServiceProvider) (interface{}, error) {
				return godi.Resolve[*testutil.TestServiceWithDeps](provider)
			},
			validate: func(t *testing.T, result interface{}, err error) {
				assert.NotNil(t, result)
				service, ok := result.(*testutil.TestServiceWithDeps)
				assert.True(t, ok)
				assert.NotNil(t, service.Logger)
				assert.NotNil(t, service.Database)
				assert.NotNil(t, service.Cache)
			},
		},
		{
			name: "resolves keyed service",
			setup: func(t *testing.T) godi.ServiceProvider {
				return testutil.NewServiceCollectionBuilder(t).
					WithSingleton(testutil.NewTestLogger, godi.Name("primary")).
					WithSingleton(testutil.NewTestLogger, godi.Name("secondary")).
					BuildProvider()
			},
			resolve: func(provider godi.ServiceProvider) (interface{}, error) {
				return godi.ResolveKeyed[testutil.TestLogger](provider, "primary")
			},
			validate: func(t *testing.T, result interface{}, err error) {
				assert.NotNil(t, result)
				assert.IsType(t, &testutil.TestLoggerImpl{}, result)
			},
		},
		{
			name: "resolves group services",
			setup: func(t *testing.T) godi.ServiceProvider {
				return testutil.NewServiceCollectionBuilder(t).
					WithSingleton(func() testutil.TestHandler {
						return testutil.NewTestHandler("handler1")
					}, godi.Group("handlers")).
					WithSingleton(func() testutil.TestHandler {
						return testutil.NewTestHandler("handler2")
					}, godi.Group("handlers")).
					WithSingleton(func() testutil.TestHandler {
						return testutil.NewTestHandler("handler3")
					}, godi.Group("handlers")).
					BuildProvider()
			},
			resolve: func(provider godi.ServiceProvider) (interface{}, error) {
				return godi.ResolveGroup[testutil.TestHandler](provider, "handlers")
			},
			validate: func(t *testing.T, result interface{}, err error) {
				handlers, ok := result.([]testutil.TestHandler)
				assert.True(t, ok)
				assert.Len(t, handlers, 3)

				// Collect handler names
				names := make(map[string]bool)
				for _, h := range handlers {
					names[h.Handle()] = true
				}

				assert.True(t, names["handler1"])
				assert.True(t, names["handler2"])
				assert.True(t, names["handler3"])
			},
		},
		{
			name: "fails to resolve missing service",
			setup: func(t *testing.T) godi.ServiceProvider {
				return testutil.NewServiceCollectionBuilder(t).BuildProvider()
			},
			resolve: func(provider godi.ServiceProvider) (interface{}, error) {
				return godi.Resolve[testutil.TestLogger](provider)
			},
			validate: func(t *testing.T, result interface{}, err error) {
				assert.Nil(t, result)
				assert.Error(t, err)
				assert.True(t, godi.IsNotFound(err))
			},
			wantErr: true,
		},
		{
			name: "fails to resolve missing keyed service",
			setup: func(t *testing.T) godi.ServiceProvider {
				return testutil.NewServiceCollectionBuilder(t).
					WithSingleton(testutil.NewTestLogger, godi.Name("primary")).
					BuildProvider()
			},
			resolve: func(provider godi.ServiceProvider) (interface{}, error) {
				return godi.ResolveKeyed[testutil.TestLogger](provider, "secondary")
			},
			validate: func(t *testing.T, result interface{}, err error) {
				assert.Nil(t, result)
				assert.Error(t, err)
				assert.True(t, godi.IsNotFound(err))
			},
			wantErr: true,
		},
		{
			name: "returns empty slice for missing group",
			setup: func(t *testing.T) godi.ServiceProvider {
				return testutil.NewServiceCollectionBuilder(t).BuildProvider()
			},
			resolve: func(provider godi.ServiceProvider) (interface{}, error) {
				return godi.ResolveGroup[testutil.TestHandler](provider, "handlers")
			},
			validate: func(t *testing.T, result interface{}, err error) {
				handlers, ok := result.([]testutil.TestHandler)
				assert.True(t, ok)
				assert.Empty(t, handlers)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			provider := tt.setup(t)
			result, err := tt.resolve(provider)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tt.validate != nil {
				tt.validate(t, result, err)
			}
		})
	}
}

func TestServiceProvider_SingletonBehavior(t *testing.T) {
	t.Run("returns same instance for multiple resolutions", func(t *testing.T) {
		t.Parallel()

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			BuildProvider()

		logger1 := testutil.AssertServiceResolvable[testutil.TestLogger](t, provider)
		logger2 := testutil.AssertServiceResolvable[testutil.TestLogger](t, provider)

		testutil.AssertSameInstance(t, logger1, logger2)
	})

	t.Run("returns same instance across scopes", func(t *testing.T) {
		t.Parallel()

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			BuildProvider()

		logger1 := testutil.AssertServiceResolvable[testutil.TestLogger](t, provider)

		scope := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		logger2 := testutil.AssertServiceResolvable[testutil.TestLogger](t, scope)

		testutil.AssertSameInstance(t, logger1, logger2)
	})
}

func TestServiceProvider_IsService(t *testing.T) {
	t.Run("correctly identifies registered services", func(t *testing.T) {
		t.Parallel()

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithScoped(testutil.NewTestService).
			BuildProvider()

		assert.True(t, provider.IsService(reflect.TypeOf((*testutil.TestLogger)(nil)).Elem()))
		assert.True(t, provider.IsService(reflect.TypeOf((*testutil.TestService)(nil))))
		assert.False(t, provider.IsService(reflect.TypeOf((*testutil.TestCache)(nil)).Elem()))
	})

	t.Run("identifies keyed services", func(t *testing.T) {
		t.Parallel()

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger, godi.Name("primary")).
			BuildProvider()

		loggerType := reflect.TypeOf((*testutil.TestLogger)(nil)).Elem()

		assert.True(t, provider.IsKeyedService(loggerType, "primary"))
		assert.False(t, provider.IsKeyedService(loggerType, "secondary"))
		assert.False(t, provider.IsService(loggerType)) // Not registered as non-keyed
	})
}

func TestServiceProvider_Invoke(t *testing.T) {
	t.Run("invokes function with dependencies", func(t *testing.T) {
		t.Parallel()

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithSingleton(testutil.NewTestDatabase).
			BuildProvider()

		var invokedLogger testutil.TestLogger
		var invokedDB testutil.TestDatabase

		err := provider.Invoke(func(logger testutil.TestLogger, db testutil.TestDatabase) {
			invokedLogger = logger
			invokedDB = db
		})

		require.NoError(t, err)
		assert.NotNil(t, invokedLogger)
		assert.NotNil(t, invokedDB)
	})

	t.Run("invokes function returning error", func(t *testing.T) {
		t.Parallel()

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			BuildProvider()

		expectedErr := errors.New("invoke error")

		err := provider.Invoke(func(logger testutil.TestLogger) error {
			assert.NotNil(t, logger)
			return expectedErr
		})

		assert.ErrorIs(t, err, expectedErr)
	})

	t.Run("fails with missing dependency", func(t *testing.T) {
		t.Parallel()

		provider := testutil.NewServiceCollectionBuilder(t).BuildProvider()

		err := provider.Invoke(func(logger testutil.TestLogger) {
			t.Fatal("should not be called")
		})

		assert.Error(t, err)
		assert.True(t, godi.IsNotFound(err))
	})
}

func TestServiceProvider_Disposal(t *testing.T) {
	t.Run("disposes provider and services", func(t *testing.T) {
		t.Parallel()

		disposable := testutil.NewTestDisposable()

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(func() *testutil.TestDisposable {
				return disposable
			}).
			BuildProvider()

		// Resolve to create instance
		d := testutil.AssertServiceResolvable[*testutil.TestDisposable](t, provider)
		assert.False(t, d.IsDisposed())

		// Close provider
		err := provider.Close()
		require.NoError(t, err)

		// Check disposal
		assert.True(t, provider.IsDisposed())
		assert.True(t, disposable.IsDisposed())
	})

	t.Run("prevents operations after disposal", func(t *testing.T) {
		t.Parallel()

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			BuildProvider()

		require.NoError(t, provider.Close())

		testutil.AssertProviderDisposed(t, provider)
	})

	t.Run("handles disposal errors", func(t *testing.T) {
		t.Parallel()

		expectedErr := errors.New("disposal error")
		disposable := testutil.NewTestDisposableWithError(expectedErr)

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(func() *testutil.TestDisposable {
				return disposable
			}).
			BuildProvider()

		// Resolve to create instance
		testutil.AssertServiceResolvable[*testutil.TestDisposable](t, provider)

		// Close should return the error
		err := provider.Close()
		assert.ErrorIs(t, err, expectedErr)
	})

	t.Run("disposes multiple services in LIFO order", func(t *testing.T) {
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
			WithSingleton(createDisposable("first"), godi.Name("first")).
			WithSingleton(createDisposable("second"), godi.Name("second")).
			WithSingleton(createDisposable("third"), godi.Name("third")).
			BuildProvider()

		// Resolve all services to create instances
		testutil.AssertKeyedServiceResolvable[godi.Disposable](t, provider, "second")
		testutil.AssertKeyedServiceResolvable[godi.Disposable](t, provider, "first")
		testutil.AssertKeyedServiceResolvable[godi.Disposable](t, provider, "third")

		require.NoError(t, provider.Close())

		// Verify LIFO order
		assert.Equal(t, []string{"third", "first", "second"}, disposalOrder)
	})

	t.Run("idempotent close", func(t *testing.T) {
		t.Parallel()

		provider := testutil.NewServiceCollectionBuilder(t).BuildProvider()

		err1 := provider.Close()
		err2 := provider.Close()

		assert.NoError(t, err1)
		assert.NoError(t, err2)
	})
}

func TestServiceProvider_CircularDependency(t *testing.T) {
	t.Run("detects direct circular dependency", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()
		require.NoError(t, collection.AddSingleton(testutil.NewCircularServiceA))
		require.NoError(t, collection.AddSingleton(testutil.NewCircularServiceB))

		provider, err := collection.BuildServiceProvider()

		// Circular dependency might be caught at build time or resolution time
		if err != nil {
			testutil.AssertCircularDependency(t, err)
			return
		}

		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Try to resolve - should fail
		_, err = godi.Resolve[*testutil.CircularServiceA](provider)
		testutil.AssertCircularDependency(t, err)
	})

	t.Run("detects circular dependency with validation", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()
		require.NoError(t, collection.AddSingleton(testutil.NewCircularServiceA))
		require.NoError(t, collection.AddSingleton(testutil.NewCircularServiceB))

		options := &godi.ServiceProviderOptions{
			ValidateOnBuild: true,
		}

		_, err := collection.BuildServiceProviderWithOptions(options)
		assert.Error(t, err)
		testutil.AssertCircularDependency(t, err)
	})
}

func TestServiceProvider_Concurrency(t *testing.T) {
	t.Run("concurrent resolution is safe", func(t *testing.T) {
		t.Parallel()

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithSingleton(testutil.NewTestDatabase).
			WithScoped(testutil.NewTestService).
			BuildProvider()

		const goroutines = 100
		var wg sync.WaitGroup
		wg.Add(goroutines)

		errors := make([]error, goroutines)
		services := make([]interface{}, goroutines)

		for i := 0; i < goroutines; i++ {
			go func(idx int) {
				defer wg.Done()

				// Mix of different service resolutions
				switch idx % 3 {
				case 0:
					svc, err := godi.Resolve[testutil.TestLogger](provider)
					services[idx] = svc
					errors[idx] = err
				case 1:
					svc, err := godi.Resolve[testutil.TestDatabase](provider)
					services[idx] = svc
					errors[idx] = err
				case 2:
					svc, err := godi.Resolve[*testutil.TestService](provider)
					services[idx] = svc
					errors[idx] = err
				}
			}(i)
		}

		wg.Wait()

		// Check all resolutions succeeded
		for i, err := range errors {
			assert.NoError(t, err, "goroutine %d failed", i)
			assert.NotNil(t, services[i], "goroutine %d got nil service", i)
		}

		// Verify singleton instances are the same
		var firstLogger testutil.TestLogger
		var firstDB testutil.TestDatabase

		for i, svc := range services {
			switch i % 3 {
			case 0:
				if firstLogger == nil {
					firstLogger = svc.(testutil.TestLogger)
				} else {
					testutil.AssertSameInstance(t, firstLogger, svc)
				}
			case 1:
				if firstDB == nil {
					firstDB = svc.(testutil.TestDatabase)
				} else {
					testutil.AssertSameInstance(t, firstDB, svc)
				}
			}
		}
	})

	t.Run("concurrent scope creation is safe", func(t *testing.T) {
		t.Parallel()

		provider := testutil.NewServiceCollectionBuilder(t).
			WithScoped(testutil.NewTestService).
			BuildProvider()

		const goroutines = 50
		var wg sync.WaitGroup
		wg.Add(goroutines)

		scopes := make([]godi.Scope, goroutines)
		errors := make([]error, goroutines)

		for i := 0; i < goroutines; i++ {
			go func(idx int) {
				defer wg.Done()

				scope := provider.CreateScope(context.Background())
				scopes[idx] = scope

				// Resolve service in scope
				_, err := godi.Resolve[*testutil.TestService](scope)
				errors[idx] = err
			}(i)
		}

		wg.Wait()

		// Check all operations succeeded
		for i, err := range errors {
			assert.NoError(t, err, "goroutine %d failed", i)
			assert.NotNil(t, scopes[i], "goroutine %d got nil scope", i)
		}

		// Clean up scopes
		for _, scope := range scopes {
			require.NoError(t, scope.Close())
		}
	})
}

func TestServiceProvider_ResultObjects(t *testing.T) {
	t.Run("handles result objects correctly", func(t *testing.T) {
		t.Parallel()

		constructor := func() testutil.TestServiceResult {
			return testutil.TestServiceResult{
				Service:  testutil.NewTestService(),
				Logger:   testutil.NewTestLogger(),
				Database: testutil.NewTestDatabase(),
			}
		}

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(constructor).
			BuildProvider()

		// All services from result should be resolvable
		service := testutil.AssertServiceResolvable[*testutil.TestService](t, provider)
		logger := testutil.AssertKeyedServiceResolvable[testutil.TestLogger](t, provider, "service")
		databases := testutil.AssertGroupServiceResolvable[testutil.TestDatabase](t, provider, "databases")

		assert.NotNil(t, service)
		assert.NotNil(t, logger)
		assert.Len(t, databases, 1)
	})
}

func TestServiceProvider_ProviderCallback(t *testing.T) {
	t.Run("provider callbacks are invoked for singleton services", func(t *testing.T) {
		t.Parallel()

		var callbackInvoked bool

		collection := godi.NewServiceCollection()
		require.NoError(t, collection.AddSingleton(
			testutil.NewTestLogger,
			godi.WithProviderCallback(func(ci godi.CallbackInfo) {
				callbackInvoked = true
			}),
		))

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Resolve to trigger callback
		logger := testutil.AssertServiceResolvable[testutil.TestLogger](t, provider)
		assert.NotNil(t, logger)
		assert.True(t, callbackInvoked, "callback should have been invoked")
	})

	t.Run("provider callbacks are invoked for scoped services", func(t *testing.T) {
		t.Parallel()

		var callbackInvoked bool

		collection := godi.NewServiceCollection()
		require.NoError(t, collection.AddScoped(
			testutil.NewTestService,
			godi.WithProviderCallback(func(ci godi.CallbackInfo) {
				callbackInvoked = true
			}),
		))

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		scope := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		// Resolve to trigger callback
		service := testutil.AssertServiceResolvable[*testutil.TestService](t, scope)
		assert.NotNil(t, service)
		assert.True(t, callbackInvoked, "callback should have been invoked")
	})
}

func TestServiceProvider_ResolutionTimeout(t *testing.T) {
	t.Run("respects resolution timeout", func(t *testing.T) {
		t.Parallel()

		// Create a slow constructor
		slowConstructor := func() testutil.TestLogger {
			time.Sleep(100 * time.Millisecond)
			return testutil.NewTestLogger()
		}

		options := &godi.ServiceProviderOptions{
			ResolutionTimeout: 10 * time.Millisecond,
		}

		collection := godi.NewServiceCollection()
		require.NoError(t, collection.AddSingleton(slowConstructor))

		provider, err := collection.BuildServiceProviderWithOptions(options)
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Resolution should timeout
		_, err = godi.Resolve[testutil.TestLogger](provider)
		testutil.AssertTimeout(t, err)
	})
}

func TestServiceProvider_DryRun(t *testing.T) {
	t.Run("dry run mode skips actual construction", func(t *testing.T) {
		t.Parallel()

		var constructed bool
		constructor := func() testutil.TestLogger {
			constructed = true
			return testutil.NewTestLogger()
		}

		options := &godi.ServiceProviderOptions{
			DryRun: true,
		}

		collection := godi.NewServiceCollection()
		require.NoError(t, collection.AddSingleton(constructor))

		provider, err := collection.BuildServiceProviderWithOptions(options)
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Try to resolve - in dry run mode this might fail or return nil
		_, _ = godi.Resolve[testutil.TestLogger](provider)

		assert.False(t, constructed, "constructor should not be called in dry run mode")
	})
}

func TestServiceProvider_MemoryManagement(t *testing.T) {
	t.Run("no goroutine leaks", func(t *testing.T) {
		t.Parallel()

		initialGoroutines := runtime.NumGoroutine()

		// Create and dispose multiple providers
		for i := 0; i < 10; i++ {
			provider := testutil.NewServiceCollectionBuilder(t).
				WithSingleton(testutil.NewTestLogger).
				WithScoped(testutil.NewTestService).
				BuildProvider()

			// Create some scopes
			for j := 0; j < 5; j++ {
				scope := provider.CreateScope(context.Background())
				testutil.AssertServiceResolvable[*testutil.TestService](t, scope)
				require.NoError(t, scope.Close())
			}

			require.NoError(t, provider.Close())
		}

		// Give goroutines time to finish
		time.Sleep(100 * time.Millisecond)

		// Check goroutine count
		finalGoroutines := runtime.NumGoroutine()
		assert.LessOrEqual(t, finalGoroutines, initialGoroutines+2,
			"possible goroutine leak: started with %d, ended with %d",
			initialGoroutines, finalGoroutines)
	})
}

// Table-driven test for error scenarios
func TestServiceProvider_ErrorScenarios(t *testing.T) {
	errorCases := []testutil.ErrorTestCase{
		{
			Name: "nil service type",
			Setup: func(t *testing.T) godi.ServiceProvider {
				return testutil.NewServiceCollectionBuilder(t).BuildProvider()
			},
			Action: func(provider godi.ServiceProvider) error {
				_, err := provider.Resolve(nil)
				return err
			},
			WantError: godi.ErrInvalidServiceType,
		},
		{
			Name: "nil service key",
			Setup: func(t *testing.T) godi.ServiceProvider {
				return testutil.NewServiceCollectionBuilder(t).
					WithSingleton(testutil.NewTestLogger, godi.Name("test")).
					BuildProvider()
			},
			Action: func(provider godi.ServiceProvider) error {
				_, err := provider.ResolveKeyed(
					reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
					nil,
				)
				return err
			},
			WantError: godi.ErrServiceKeyNil,
		},
		{
			Name: "empty group name",
			Setup: func(t *testing.T) godi.ServiceProvider {
				return testutil.NewServiceCollectionBuilder(t).BuildProvider()
			},
			Action: func(provider godi.ServiceProvider) error {
				_, err := provider.ResolveGroup(
					reflect.TypeOf((*testutil.TestHandler)(nil)).Elem(),
					"",
				)
				return err
			},
			CheckErr: func(t *testing.T, err error) {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "group name cannot be empty")
			},
		},
	}

	testutil.RunErrorTestCases(t, errorCases)
}

// Example of advanced scenario testing
func TestServiceProvider_AdvancedScenarios(t *testing.T) {
	t.Run("complex dependency graph", func(t *testing.T) {
		t.Parallel()

		// Create a complex service graph
		type ServiceA struct{ Name string }
		type ServiceB struct{ A *ServiceA }
		type ServiceC struct{ B *ServiceB }
		type ServiceD struct {
			A *ServiceA
			C *ServiceC
		}

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(func() *ServiceA { return &ServiceA{Name: "A"} }).
			WithSingleton(func(a *ServiceA) *ServiceB { return &ServiceB{A: a} }).
			WithSingleton(func(b *ServiceB) *ServiceC { return &ServiceC{B: b} }).
			WithSingleton(func(a *ServiceA, c *ServiceC) *ServiceD {
				return &ServiceD{A: a, C: c}
			}).
			BuildProvider()

		// Resolve the most complex service
		d := testutil.AssertServiceResolvable[*ServiceD](t, provider)

		// Verify the entire graph
		assert.Equal(t, "A", d.A.Name)
		assert.Equal(t, "A", d.C.B.A.Name)
		testutil.AssertSameInstance(t, d.A, d.C.B.A) // Should be same singleton instance
	})

	t.Run("parameter objects with optional dependencies", func(t *testing.T) {
		t.Parallel()

		// Service with optional dependency
		type ServiceWithOptional struct {
			Logger testutil.TestLogger
			Cache  testutil.TestCache
		}

		constructor := func(params testutil.TestServiceParams) *ServiceWithOptional {
			return &ServiceWithOptional{
				Logger: params.Logger,
				Cache:  params.Cache, // Optional
			}
		}

		// Build without cache
		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithSingleton(testutil.NewTestDatabase).
			WithSingleton(constructor).
			BuildProvider()

		service := testutil.AssertServiceResolvable[*ServiceWithOptional](t, provider)
		assert.NotNil(t, service.Logger)
		assert.Nil(t, service.Cache) // Optional dependency not registered
	})
}

// Test types for As option tests
type TestRepository interface {
	Get() string
}

type testUserRepository struct{}

func (u *testUserRepository) Get() string { return "user" }

type TestReader interface {
	Read() string
}

type TestWriter interface {
	Write(string)
}

type testDatabase struct{ data string }

func (d *testDatabase) Read() string   { return d.data }
func (d *testDatabase) Write(s string) { d.data = s }

// Test types for Group+As combination tests
type GroupAsTestInterface interface {
	GetName() string
}

type groupAsImpl1 struct{ name string }
type groupAsImpl2 struct{ name string }

func (g *groupAsImpl1) GetName() string { return g.name }
func (g *groupAsImpl2) GetName() string { return g.name }

// Test types for exact bug reproduction
type TestController interface {
	RegisterRoutes() string
}

type testGraphQLController struct{ id string }
type testHealthController struct{ id string }
type testOAuthController struct{ id string }
type testTebexController struct{ id string }

func (g *testGraphQLController) RegisterRoutes() string { return g.id }
func (h *testHealthController) RegisterRoutes() string  { return h.id }
func (o *testOAuthController) RegisterRoutes() string   { return o.id }
func (t *testTebexController) RegisterRoutes() string   { return t.id }

// Test types for group without As
type SimpleHandler interface {
	Handle() string
}

type simpleHandlerImpl struct{ name string }

func (h *simpleHandlerImpl) Handle() string { return h.name }

// TestGroupOption verifies that Group option works correctly by itself
func TestGroupOption(t *testing.T) {
	t.Run("group registration and resolution", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Register multiple services in the same group
		require.NoError(t, collection.AddSingleton(
			func() testutil.TestHandler { return testutil.NewTestHandler("handler1") },
			godi.Group("handlers"),
		))
		require.NoError(t, collection.AddSingleton(
			func() testutil.TestHandler { return testutil.NewTestHandler("handler2") },
			godi.Group("handlers"),
		))
		require.NoError(t, collection.AddSingleton(
			func() testutil.TestHandler { return testutil.NewTestHandler("handler3") },
			godi.Group("handlers"),
		))

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Resolve the group
		handlers, err := godi.ResolveGroup[testutil.TestHandler](provider, "handlers")
		require.NoError(t, err)
		assert.Len(t, handlers, 3)

		// Verify all handlers are present
		handlerNames := make(map[string]bool)
		for _, h := range handlers {
			handlerNames[h.Handle()] = true
		}
		assert.True(t, handlerNames["handler1"])
		assert.True(t, handlerNames["handler2"])
		assert.True(t, handlerNames["handler3"])
	})

	t.Run("empty group returns empty slice", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Should return empty slice, not error
		handlers, err := godi.ResolveGroup[testutil.TestHandler](provider, "nonexistent")
		require.NoError(t, err)
		assert.Empty(t, handlers)
	})
}

// TestAsOption verifies that As option works correctly by itself
func TestAsOption(t *testing.T) {
	t.Run("as interface registration", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Register with As option
		require.NoError(t, collection.AddSingleton(
			func() *testUserRepository { return &testUserRepository{} },
			godi.As((*TestRepository)(nil)),
		))

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Should be able to resolve as interface
		repo, err := godi.Resolve[TestRepository](provider)
		require.NoError(t, err)
		assert.Equal(t, "user", repo.Get())

		// Should NOT be able to resolve as concrete type when using As
		// (unless also registered separately)
		_, err = godi.Resolve[*testUserRepository](provider)
		assert.Error(t, err)
	})

	t.Run("as multiple interfaces", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Register as multiple interfaces
		require.NoError(t, collection.AddSingleton(
			func() *testDatabase { return &testDatabase{data: "initial"} },
			godi.As((*TestReader)(nil), (*TestWriter)(nil)),
		))

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Should resolve as both interfaces
		reader, err := godi.Resolve[TestReader](provider)
		require.NoError(t, err)
		assert.Equal(t, "initial", reader.Read())

		writer, err := godi.Resolve[TestWriter](provider)
		require.NoError(t, err)
		writer.Write("updated")

		// Both should be the same instance (singleton)
		reader2, _ := godi.Resolve[TestReader](provider)
		assert.Equal(t, "updated", reader2.Read())
	})
}

// TestGroupWithAsOption tests the combination that's causing the bug
func TestGroupWithAsOption(t *testing.T) {
	t.Run("group with As option should not create duplicates", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Register services with both As and Group options
		require.NoError(t, collection.AddSingleton(
			func() *groupAsImpl1 { return &groupAsImpl1{name: "impl1"} },
			godi.As((*GroupAsTestInterface)(nil)),
			godi.Group("test-group"),
		))
		require.NoError(t, collection.AddSingleton(
			func() *groupAsImpl2 { return &groupAsImpl2{name: "impl2"} },
			godi.As((*GroupAsTestInterface)(nil)),
			godi.Group("test-group"),
		))

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Resolve the group
		group, err := godi.ResolveGroup[GroupAsTestInterface](provider, "test-group")
		require.NoError(t, err)

		// Should return exactly 2 services, not 4
		assert.Len(t, group, 2, "Expected 2 services in group, but got %d", len(group))

		// Verify no duplicates by checking names
		names := make(map[string]bool)
		for _, svc := range group {
			name := svc.GetName()
			assert.False(t, names[name], "Duplicate service with name %s in group", name)
			names[name] = true
		}

		// Verify we got both implementations
		assert.True(t, names["impl1"], "Missing impl1 in group")
		assert.True(t, names["impl2"], "Missing impl2 in group")
	})

	t.Run("exact reproduction of user's bug", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Register exactly as user does
		require.NoError(t, collection.AddSingleton(
			func() *testGraphQLController { return &testGraphQLController{id: "graphql"} },
			godi.As(new(TestController)),
			godi.Group("routes"),
		))
		require.NoError(t, collection.AddSingleton(
			func() *testHealthController { return &testHealthController{id: "health"} },
			godi.As(new(TestController)),
			godi.Group("routes"),
		))
		require.NoError(t, collection.AddSingleton(
			func() *testOAuthController { return &testOAuthController{id: "oauth"} },
			godi.As(new(TestController)),
			godi.Group("routes"),
		))
		require.NoError(t, collection.AddSingleton(
			func() *testTebexController { return &testTebexController{id: "tebex"} },
			godi.As(new(TestController)),
			godi.Group("routes"),
		))

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Resolve the group
		controllers, err := godi.ResolveGroup[TestController](provider, "routes")
		require.NoError(t, err)

		// Should return exactly 4 controllers
		assert.Len(t, controllers, 4, "Expected 4 controllers in group, but got %d", len(controllers))

		// Track which controllers we've seen
		seen := make(map[string]int)
		for _, c := range controllers {
			id := c.RegisterRoutes()
			seen[id]++
		}

		// Each controller should appear exactly once
		assert.Equal(t, 1, seen["graphql"], "graphql controller appeared %d times", seen["graphql"])
		assert.Equal(t, 1, seen["health"], "health controller appeared %d times", seen["health"])
		assert.Equal(t, 1, seen["oauth"], "oauth controller appeared %d times", seen["oauth"])
		assert.Equal(t, 1, seen["tebex"], "tebex controller appeared %d times", seen["tebex"])

		// Log what we actually got if test fails
		if len(controllers) != 4 {
			t.Logf("Controllers returned: %v", seen)
		}
	})
}

// TestGroupWithoutAsOption verifies group functionality without As
func TestGroupWithoutAsOption(t *testing.T) {
	t.Parallel()

	collection := godi.NewServiceCollection()

	// Register services that already return the interface type
	require.NoError(t, collection.AddSingleton(
		func() SimpleHandler {
			return &simpleHandlerImpl{name: "handler1"}
		},
		godi.Group("handlers"),
	))
	require.NoError(t, collection.AddSingleton(
		func() SimpleHandler {
			return &simpleHandlerImpl{name: "handler2"}
		},
		godi.Group("handlers"),
	))

	provider, err := collection.BuildServiceProvider()
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, provider.Close())
	})

	handlers, err := godi.ResolveGroup[SimpleHandler](provider, "handlers")
	require.NoError(t, err)
	assert.Len(t, handlers, 2)
}
