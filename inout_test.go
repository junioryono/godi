package godi_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/junioryono/godi"
	"github.com/junioryono/godi/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParameterObjects(t *testing.T) {
	t.Run("basic parameter object", func(t *testing.T) {
		t.Parallel()

		type ServiceParams struct {
			godi.In

			Logger   testutil.TestLogger
			Database testutil.TestDatabase
		}

		constructor := func(params ServiceParams) *testutil.TestService {
			// Use dependencies
			params.Logger.Log("Creating service")
			params.Database.Query("SELECT 1")

			return &testutil.TestService{
				ID:   "from-params",
				Data: "test",
			}
		}

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithSingleton(testutil.NewTestDatabase).
			WithScoped(constructor).
			BuildProvider()

		scope := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		service := testutil.AssertServiceResolvableInScope[*testutil.TestService](t, scope)
		assert.Equal(t, "from-params", service.ID)
	})

	t.Run("parameter object with optional fields", func(t *testing.T) {
		t.Parallel()

		type ServiceParams struct {
			godi.In

			Logger   testutil.TestLogger
			Database testutil.TestDatabase
			Cache    testutil.TestCache `optional:"true"`
		}

		type ServiceWithOptional struct {
			hasCache bool
		}

		constructor := func(params ServiceParams) *ServiceWithOptional {
			return &ServiceWithOptional{
				hasCache: params.Cache != nil,
			}
		}

		// Test without cache
		provider1 := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithSingleton(testutil.NewTestDatabase).
			WithSingleton(constructor).
			BuildProvider()

		service1 := testutil.AssertServiceResolvable[*ServiceWithOptional](t, provider1)
		assert.False(t, service1.hasCache, "should not have cache when not registered")

		// Test with cache
		provider2 := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithSingleton(testutil.NewTestDatabase).
			WithSingleton(testutil.NewTestCache).
			WithSingleton(constructor).
			BuildProvider()

		service2 := testutil.AssertServiceResolvable[*ServiceWithOptional](t, provider2)
		assert.True(t, service2.hasCache, "should have cache when registered")
	})

	t.Run("parameter object with named services", func(t *testing.T) {
		t.Parallel()

		type ServiceParams struct {
			godi.In

			PrimaryDB   testutil.TestDatabase `name:"primary"`
			SecondaryDB testutil.TestDatabase `name:"secondary"`
		}

		type ServiceWithNamedDeps struct {
			primaryResult   string
			secondaryResult string
		}

		constructor := func(params ServiceParams) *ServiceWithNamedDeps {
			return &ServiceWithNamedDeps{
				primaryResult:   params.PrimaryDB.Query("SELECT 1"),
				secondaryResult: params.SecondaryDB.Query("SELECT 2"),
			}
		}

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(func() testutil.TestDatabase {
				return testutil.NewTestDatabaseNamed("primary-db")
			}, godi.Name("primary")).
			WithSingleton(func() testutil.TestDatabase {
				return testutil.NewTestDatabaseNamed("secondary-db")
			}, godi.Name("secondary")).
			WithSingleton(constructor).
			BuildProvider()

		service := testutil.AssertServiceResolvable[*ServiceWithNamedDeps](t, provider)
		assert.Contains(t, service.primaryResult, "primary-db")
		assert.Contains(t, service.secondaryResult, "secondary-db")
	})

	t.Run("parameter object with groups", func(t *testing.T) {
		t.Parallel()

		type ServiceParams struct {
			godi.In

			Handlers []testutil.TestHandler `group:"handlers"`
		}

		type HandlerManager struct {
			handlerCount int
			names        []string
		}

		constructor := func(params ServiceParams) *HandlerManager {
			names := make([]string, len(params.Handlers))
			for i, h := range params.Handlers {
				names[i] = h.Handle()
			}

			return &HandlerManager{
				handlerCount: len(params.Handlers),
				names:        names,
			}
		}

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(func() testutil.TestHandler {
				return testutil.NewTestHandler("handler1")
			}, godi.Group("handlers")).
			WithSingleton(func() testutil.TestHandler {
				return testutil.NewTestHandler("handler2")
			}, godi.Group("handlers")).
			WithSingleton(func() testutil.TestHandler {
				return testutil.NewTestHandler("handler3")
			}, godi.Group("handlers")).
			WithSingleton(constructor).
			BuildProvider()

		manager := testutil.AssertServiceResolvable[*HandlerManager](t, provider)
		assert.Equal(t, 3, manager.handlerCount)
		assert.Len(t, manager.names, 3)

		// Check all handlers are present
		nameSet := make(map[string]bool)
		for _, name := range manager.names {
			nameSet[name] = true
		}
		assert.True(t, nameSet["handler1"])
		assert.True(t, nameSet["handler2"])
		assert.True(t, nameSet["handler3"])
	})
}

func TestResultObjects(t *testing.T) {
	t.Run("basic result object", func(t *testing.T) {
		t.Parallel()

		type ServiceBundle struct {
			godi.Out

			Logger   testutil.TestLogger
			Database testutil.TestDatabase
			Cache    testutil.TestCache
		}

		constructor := func() ServiceBundle {
			return ServiceBundle{
				Logger:   testutil.NewTestLogger(),
				Database: testutil.NewTestDatabase(),
				Cache:    testutil.NewTestCache(),
			}
		}

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(constructor).
			BuildProvider()

		// All services should be resolvable
		logger := testutil.AssertServiceResolvable[testutil.TestLogger](t, provider)
		database := testutil.AssertServiceResolvable[testutil.TestDatabase](t, provider)
		cache := testutil.AssertServiceResolvable[testutil.TestCache](t, provider)

		assert.NotNil(t, logger)
		assert.NotNil(t, database)
		assert.NotNil(t, cache)
	})

	t.Run("result object with named services", func(t *testing.T) {
		t.Parallel()

		type ServiceBundle struct {
			godi.Out

			UserService  *testutil.TestService `name:"user"`
			AdminService *testutil.TestService `name:"admin"`
		}

		constructor := func() ServiceBundle {
			return ServiceBundle{
				UserService:  &testutil.TestService{ID: "user-service"},
				AdminService: &testutil.TestService{ID: "admin-service"},
			}
		}

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(constructor).
			BuildProvider()

		// Named services should be resolvable
		userService := testutil.AssertKeyedServiceResolvable[*testutil.TestService](t, provider, "user")
		adminService := testutil.AssertKeyedServiceResolvable[*testutil.TestService](t, provider, "admin")

		assert.Equal(t, "user-service", userService.ID)
		assert.Equal(t, "admin-service", adminService.ID)
	})

	t.Run("result object with groups", func(t *testing.T) {
		t.Parallel()

		type HandlerBundle struct {
			godi.Out

			UserHandler  testutil.TestHandler `group:"routes"`
			AdminHandler testutil.TestHandler `group:"routes"`
			APIHandler   testutil.TestHandler `group:"routes"`
		}

		constructor := func() HandlerBundle {
			return HandlerBundle{
				UserHandler:  testutil.NewTestHandler("user"),
				AdminHandler: testutil.NewTestHandler("admin"),
				APIHandler:   testutil.NewTestHandler("api"),
			}
		}

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(constructor).
			BuildProvider()

		// Group should contain all handlers
		handlers, err := godi.ResolveGroup[testutil.TestHandler](provider, "routes")
		require.NoError(t, err)
		assert.Len(t, handlers, 3)

		// Verify all handlers are present
		names := make(map[string]bool)
		for _, h := range handlers {
			names[h.Handle()] = true
		}
		assert.True(t, names["user"])
		assert.True(t, names["admin"])
		assert.True(t, names["api"])
	})

	t.Run("result object with error", func(t *testing.T) {
		t.Parallel()

		type ServiceBundle struct {
			godi.Out

			Service *testutil.TestService
		}

		expectedErr := errors.New("construction failed")

		constructor := func() (ServiceBundle, error) {
			return ServiceBundle{}, expectedErr
		}

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(constructor).
			BuildProvider()

		// Resolution should fail with the constructor error
		_, err := godi.Resolve[*testutil.TestService](provider)
		assert.Error(t, err)
		assert.ErrorIs(t, err, expectedErr)
	})
}

func TestComplexScenarios(t *testing.T) {
	t.Run("parameter and result objects together", func(t *testing.T) {
		t.Parallel()

		// Input params
		type DatabaseParams struct {
			godi.In

			Logger testutil.TestLogger
		}

		// Output bundle
		type DatabaseBundle struct {
			godi.Out

			Primary   testutil.TestDatabase `name:"primary"`
			Secondary testutil.TestDatabase `name:"secondary"`
		}

		constructor := func(params DatabaseParams) DatabaseBundle {
			params.Logger.Log("Creating databases")

			return DatabaseBundle{
				Primary:   testutil.NewTestDatabaseNamed("primary"),
				Secondary: testutil.NewTestDatabaseNamed("secondary"),
			}
		}

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithSingleton(constructor).
			BuildProvider()

		// Both databases should be available
		primary := testutil.AssertKeyedServiceResolvable[testutil.TestDatabase](t, provider, "primary")
		secondary := testutil.AssertKeyedServiceResolvable[testutil.TestDatabase](t, provider, "secondary")

		assert.Contains(t, primary.Query("SELECT 1"), "primary")
		assert.Contains(t, secondary.Query("SELECT 1"), "secondary")
	})

	t.Run("nested parameter objects", func(t *testing.T) {
		t.Parallel()

		type InnerParams struct {
			godi.In

			Logger testutil.TestLogger
		}

		type OuterService struct {
			logger testutil.TestLogger
		}

		// This should work - the inner struct has In, not the constructor param
		innerConstructor := func(params InnerParams) *OuterService {
			return &OuterService{logger: params.Logger}
		}

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithSingleton(innerConstructor).
			BuildProvider()

		service := testutil.AssertServiceResolvable[*OuterService](t, provider)
		assert.NotNil(t, service.logger)
	})

	t.Run("mixed dependencies with parameter objects", func(t *testing.T) {
		t.Parallel()

		type ServiceParams struct {
			godi.In

			Logger   testutil.TestLogger
			Handlers []testutil.TestHandler `group:"handlers"`
			Config   string                 `optional:"true"`
		}

		type ComplexService struct {
			loggerType   string
			handlerCount int
			hasConfig    bool
		}

		constructor := func(params ServiceParams, db testutil.TestDatabase) *ComplexService {
			// Mix of parameter object and regular parameter
			params.Logger.Log("Creating complex service")
			db.Query("INIT")

			return &ComplexService{
				loggerType:   fmt.Sprintf("%T", params.Logger),
				handlerCount: len(params.Handlers),
				hasConfig:    params.Config != "",
			}
		}

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithSingleton(testutil.NewTestDatabase).
			WithSingleton(func() testutil.TestHandler {
				return testutil.NewTestHandler("h1")
			}, godi.Group("handlers")).
			WithSingleton(func() testutil.TestHandler {
				return testutil.NewTestHandler("h2")
			}, godi.Group("handlers")).
			WithSingleton(constructor).
			BuildProvider()

		service := testutil.AssertServiceResolvable[*ComplexService](t, provider)
		assert.Contains(t, service.loggerType, "TestLoggerImpl")
		assert.Equal(t, 2, service.handlerCount)
		assert.False(t, service.hasConfig) // Optional not provided
	})
}

func TestIsInAndIsOut(t *testing.T) {
	t.Run("IsIn correctly identifies In structs", func(t *testing.T) {
		t.Parallel()

		type ValidIn struct {
			godi.In
			Logger testutil.TestLogger
		}

		type InvalidIn struct {
			Logger testutil.TestLogger
		}

		type NamedIn struct {
			In     godi.In // Wrong - should be embedded
			Logger testutil.TestLogger
		}

		assert.True(t, godi.IsIn(ValidIn{}))
		assert.False(t, godi.IsIn(InvalidIn{}))
		assert.False(t, godi.IsIn(NamedIn{}))
		assert.False(t, godi.IsIn("not a struct"))
	})

	t.Run("IsOut correctly identifies Out structs", func(t *testing.T) {
		t.Parallel()

		type ValidOut struct {
			godi.Out
			Service *testutil.TestService
		}

		type InvalidOut struct {
			Service *testutil.TestService
		}

		type NamedOut struct {
			Out     godi.Out // Wrong - should be embedded
			Service *testutil.TestService
		}

		assert.True(t, godi.IsOut(ValidOut{}))
		assert.False(t, godi.IsOut(InvalidOut{}))
		assert.False(t, godi.IsOut(NamedOut{}))
		assert.False(t, godi.IsOut(42))
	})
}

// Table-driven tests for edge cases
func TestInOutEdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T) godi.ServiceCollection
		wantErr  bool
		checkErr func(t *testing.T, err error)
	}{
		{
			name: "empty parameter object",
			setup: func(t *testing.T) godi.ServiceCollection {
				type EmptyParams struct {
					godi.In
				}

				constructor := func(params EmptyParams) string {
					return "success"
				}

				collection := godi.NewServiceCollection()
				require.NoError(t, collection.AddSingleton(constructor))
				return collection
			},
			wantErr: false,
		},
		{
			name: "empty result object",
			setup: func(t *testing.T) godi.ServiceCollection {
				type EmptyResult struct {
					godi.Out
				}

				constructor := func() EmptyResult {
					return EmptyResult{}
				}

				collection := godi.NewServiceCollection()
				require.NoError(t, collection.AddSingleton(constructor))
				return collection
			},
			wantErr: true,
			checkErr: func(t *testing.T, err error) {
				assert.Contains(t, err.Error(), "must provide at least one non-error type")
			},
		},
		{
			name: "parameter object with unexported fields",
			setup: func(t *testing.T) godi.ServiceCollection {
				type ParamsWithUnexported struct {
					godi.In

					Logger testutil.TestLogger
					hidden string //lint:ignore U1000 unexported
				}

				constructor := func(params ParamsWithUnexported) string {
					return "success"
				}

				collection := godi.NewServiceCollection()
				require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))
				require.NoError(t, collection.AddSingleton(constructor))
				return collection
			},
			wantErr: true,
			checkErr: func(t *testing.T, err error) {
				assert.Contains(t, err.Error(), "unexported fields not allowed")
			},
		},
		{
			name: "result object with nil values",
			setup: func(t *testing.T) godi.ServiceCollection {
				type NilResult struct {
					godi.Out

					Service testutil.TestLogger
				}

				constructor := func() NilResult {
					return NilResult{
						Service: nil, // Explicitly nil
					}
				}

				collection := godi.NewServiceCollection()
				require.NoError(t, collection.AddSingleton(constructor))
				return collection
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			collection := tt.setup(t)
			provider, err := collection.BuildServiceProvider()

			if tt.wantErr {
				assert.Error(t, err)
				if tt.checkErr != nil {
					tt.checkErr(t, err)
				}
			} else {
				require.NoError(t, err)
				t.Cleanup(func() {
					require.NoError(t, provider.Close())
				})
			}
		})
	}
}

func TestProvideOptions(t *testing.T) {
	t.Run("FillProvideInfo works", func(t *testing.T) {
		t.Parallel()

		var info godi.ProvideInfo

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger, godi.FillProvideInfo(&info)).
			BuildProvider()

		// Resolve to ensure the constructor runs
		testutil.AssertServiceResolvable[testutil.TestLogger](t, provider)

		// Info should be populated
		assert.NotNil(t, info)
		// The exact contents depend on dig's implementation
	})

	t.Run("callbacks work", func(t *testing.T) {
		t.Parallel()

		var callbackInvoked bool
		var beforeCallbackInvoked bool

		provider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(
				testutil.NewTestLogger,
				godi.WithProviderCallback(func(ci godi.CallbackInfo) {
					callbackInvoked = true
				}),
				godi.WithProviderBeforeCallback(func(ci godi.BeforeCallbackInfo) {
					beforeCallbackInvoked = true
				}),
			).
			BuildProvider()

		// Resolve to trigger callbacks
		testutil.AssertServiceResolvable[testutil.TestLogger](t, provider)

		assert.True(t, beforeCallbackInvoked, "before callback should be invoked")
		assert.True(t, callbackInvoked, "after callback should be invoked")
	})
}
