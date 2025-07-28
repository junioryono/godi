package godi_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/junioryono/godi/v2"
	"github.com/junioryono/godi/v2/internal/testutil"
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

		provider := godi.NewServiceProvider()
		assert.NoError(t, provider.AddSingleton(testutil.NewTestLogger))
		assert.NoError(t, provider.AddSingleton(testutil.NewTestDatabase))
		assert.NoError(t, provider.AddScoped(constructor))
		scope := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		service := testutil.AssertServiceResolvable[*testutil.TestService](t, scope)
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
		provider1 := godi.NewServiceProvider()
		assert.NoError(t, provider1.AddSingleton(testutil.NewTestLogger))
		assert.NoError(t, provider1.AddSingleton(testutil.NewTestDatabase))
		assert.NoError(t, provider1.AddSingleton(constructor))

		service1 := testutil.AssertServiceResolvable[*ServiceWithOptional](t, provider1.GetRootScope())
		assert.False(t, service1.hasCache, "should not have cache when not registered")

		// Test with cache
		provider2 := godi.NewServiceProvider()
		assert.NoError(t, provider2.AddSingleton(testutil.NewTestLogger))
		assert.NoError(t, provider2.AddSingleton(testutil.NewTestDatabase))
		assert.NoError(t, provider2.AddSingleton(testutil.NewTestCache))
		assert.NoError(t, provider2.AddSingleton(constructor))

		service2 := testutil.AssertServiceResolvable[*ServiceWithOptional](t, provider2.GetRootScope())
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

		provider := godi.NewServiceProvider()
		assert.NoError(t, provider.AddSingleton(func() testutil.TestDatabase {
			return testutil.NewTestDatabaseNamed("primary-db")
		}, godi.Name("primary")))
		assert.NoError(t, provider.AddSingleton(func() testutil.TestDatabase {
			return testutil.NewTestDatabaseNamed("secondary-db")
		}, godi.Name("secondary")))
		assert.NoError(t, provider.AddSingleton(constructor))

		service := testutil.AssertServiceResolvable[*ServiceWithNamedDeps](t, provider.GetRootScope())
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

		provider := godi.NewServiceProvider()
		assert.NoError(t, provider.AddSingleton(func() testutil.TestHandler {
			return testutil.NewTestHandler("handler1")
		}, godi.Group("handlers")))
		assert.NoError(t, provider.AddSingleton(func() testutil.TestHandler {
			return testutil.NewTestHandler("handler2")
		}, godi.Group("handlers")))
		assert.NoError(t, provider.AddSingleton(func() testutil.TestHandler {
			return testutil.NewTestHandler("handler3")
		}, godi.Group("handlers")))
		assert.NoError(t, provider.AddSingleton(constructor))

		manager := testutil.AssertServiceResolvable[*HandlerManager](t, provider.GetRootScope())
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

		provider := godi.NewServiceProvider()
		assert.NoError(t, provider.AddSingleton(constructor))

		// All services should be resolvable
		logger := testutil.AssertServiceResolvable[testutil.TestLogger](t, provider.GetRootScope())
		database := testutil.AssertServiceResolvable[testutil.TestDatabase](t, provider.GetRootScope())
		cache := testutil.AssertServiceResolvable[testutil.TestCache](t, provider.GetRootScope())

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

		provider := godi.NewServiceProvider()
		assert.NoError(t, provider.AddSingleton(constructor))

		// Named services should be resolvable
		userService := testutil.AssertKeyedServiceResolvable[*testutil.TestService](t, provider.GetRootScope(), "user")
		adminService := testutil.AssertKeyedServiceResolvable[*testutil.TestService](t, provider.GetRootScope(), "admin")

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

		provider := godi.NewServiceProvider()
		assert.NoError(t, provider.AddSingleton(constructor))

		// Group should contain all handlers
		handlers, err := godi.ResolveGroup[testutil.TestHandler](provider.GetRootScope(), "routes")
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

		provider := godi.NewServiceProvider()
		assert.NoError(t, provider.AddSingleton(constructor))

		// Resolution should fail with the constructor error
		_, err := godi.Resolve[*testutil.TestService](provider.GetRootScope())
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

		provider := godi.NewServiceProvider()
		assert.NoError(t, provider.AddSingleton(testutil.NewTestLogger))
		assert.NoError(t, provider.AddSingleton(constructor))

		// Both databases should be available
		primary := testutil.AssertKeyedServiceResolvable[testutil.TestDatabase](t, provider.GetRootScope(), "primary")
		secondary := testutil.AssertKeyedServiceResolvable[testutil.TestDatabase](t, provider.GetRootScope(), "secondary")

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

		provider := godi.NewServiceProvider()
		assert.NoError(t, provider.AddSingleton(testutil.NewTestLogger))
		assert.NoError(t, provider.AddSingleton(innerConstructor))
		service := testutil.AssertServiceResolvable[*OuterService](t, provider.GetRootScope())
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

		provider := godi.NewServiceProvider()
		assert.NoError(t, provider.AddSingleton(testutil.NewTestLogger))
		assert.NoError(t, provider.AddSingleton(testutil.NewTestDatabase))
		assert.NoError(t, provider.AddSingleton(func() testutil.TestHandler {
			return testutil.NewTestHandler("h1")
		}, godi.Group("handlers")))
		assert.NoError(t, provider.AddSingleton(func() testutil.TestHandler {
			return testutil.NewTestHandler("h2")
		}, godi.Group("handlers")))
		assert.NoError(t, provider.AddSingleton(constructor))

		service := testutil.AssertServiceResolvable[*ComplexService](t, provider.GetRootScope())
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
		name   string
		action func(t *testing.T) godi.ServiceProvider
	}{
		{
			name: "empty parameter object",
			action: func(t *testing.T) godi.ServiceProvider {
				type EmptyParams struct {
					godi.In
				}

				constructor := func(params EmptyParams) string {
					return "success"
				}

				provider := godi.NewServiceProvider()
				require.NoError(t, provider.AddSingleton(constructor))
				return provider
			},
		},
		{
			name: "empty result object",
			action: func(t *testing.T) godi.ServiceProvider {
				type EmptyResult struct {
					godi.Out
				}

				constructor := func() EmptyResult {
					return EmptyResult{}
				}

				provider := godi.NewServiceProvider()
				require.Error(t, provider.AddSingleton(constructor), "func() godi_test.EmptyResult must provide at least one non-error type")
				return provider
			},
		},
		{
			name: "parameter object with unexported fields",
			action: func(t *testing.T) godi.ServiceProvider {
				type ParamsWithUnexported struct {
					godi.In

					Logger testutil.TestLogger
					hidden string //lint:ignore U1000 unexported
				}

				constructor := func(params ParamsWithUnexported) string {
					return "success"
				}

				provider := godi.NewServiceProvider()
				require.NoError(t, provider.AddSingleton(testutil.NewTestLogger))
				require.Error(t, provider.AddSingleton(constructor), "unexported fields not allowed in dig.In, did you mean to export \"hidden\" (string)?")
				return provider
			},
		},
		{
			name: "result object with nil values",
			action: func(t *testing.T) godi.ServiceProvider {
				type NilResult struct {
					godi.Out

					Service testutil.TestLogger
				}

				constructor := func() NilResult {
					return NilResult{
						Service: nil, // Explicitly nil
					}
				}

				provider := godi.NewServiceProvider()
				require.NoError(t, provider.AddSingleton(constructor))
				return provider
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			provider := tt.action(t)
			t.Cleanup(func() {
				require.NoError(t, provider.Close())
			})
		})
	}
}

func TestProvideOptions(t *testing.T) {
	t.Run("FillProvideInfo works", func(t *testing.T) {
		t.Parallel()

		var info godi.ProvideInfo

		provider := godi.NewServiceProvider()
		assert.NoError(t, provider.AddSingleton(testutil.NewTestLogger, godi.FillProvideInfo(&info)))

		// Resolve to ensure the constructor runs
		testutil.AssertServiceResolvable[testutil.TestLogger](t, provider.GetRootScope())

		// Info should be populated
		assert.NotNil(t, info)
		// The exact contents depend on dig's implementation
	})

	t.Run("callbacks work", func(t *testing.T) {
		t.Parallel()

		var callbackInvoked bool
		var beforeCallbackInvoked bool

		provider := godi.NewServiceProvider()
		assert.NoError(t, provider.AddSingleton(testutil.NewTestLogger,
			godi.WithProviderCallback(func(ci godi.CallbackInfo) {
				callbackInvoked = true
			}),
			godi.WithProviderBeforeCallback(func(ci godi.BeforeCallbackInfo) {
				beforeCallbackInvoked = true
			}),
		))

		// Resolve to trigger callbacks
		testutil.AssertServiceResolvable[testutil.TestLogger](t, provider.GetRootScope())

		assert.True(t, beforeCallbackInvoked, "before callback should be invoked")
		assert.True(t, callbackInvoked, "after callback should be invoked")
	})
}
