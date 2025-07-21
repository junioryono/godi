package godi_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/junioryono/godi/v2"
	"github.com/junioryono/godi/v2/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceCollection_Creation(t *testing.T) {
	t.Run("creates empty collection", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		assert.NotNil(t, collection)
		assert.Equal(t, 0, collection.Count())
		assert.Empty(t, collection.ToSlice())
	})
}

func TestServiceCollection_AddSingleton(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T) godi.ServiceCollection
		construct interface{}
		opts      []godi.ProvideOption
		wantErr   assert.ErrorAssertionFunc
		validate  func(t *testing.T, collection godi.ServiceCollection)
	}{
		{
			name: "adds singleton service successfully",
			setup: func(t *testing.T) godi.ServiceCollection {
				return godi.NewServiceCollection()
			},
			construct: testutil.NewTestLogger,
			wantErr:   assert.NoError,
			validate: func(t *testing.T, collection godi.ServiceCollection) {
				assert.Equal(t, 1, collection.Count())
				assert.True(t, collection.Contains(reflect.TypeOf((*testutil.TestLogger)(nil)).Elem()))
			},
		},
		{
			name: "rejects nil constructor",
			setup: func(t *testing.T) godi.ServiceCollection {
				return godi.NewServiceCollection()
			},
			construct: nil,
			wantErr:   assert.Error,
			validate: func(t *testing.T, collection godi.ServiceCollection) {
				assert.Equal(t, 0, collection.Count())
			},
		},
		{
			name: "rejects duplicate registration",
			setup: func(t *testing.T) godi.ServiceCollection {
				collection := godi.NewServiceCollection()
				require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))
				return collection
			},
			construct: testutil.NewTestLogger,
			wantErr:   assert.Error,
			validate: func(t *testing.T, collection godi.ServiceCollection) {
				assert.Equal(t, 1, collection.Count())
			},
		},
		{
			name: "accepts keyed services",
			setup: func(t *testing.T) godi.ServiceCollection {
				return godi.NewServiceCollection()
			},
			construct: testutil.NewTestLogger,
			opts:      []godi.ProvideOption{godi.Name("primary")},
			wantErr:   assert.NoError,
			validate: func(t *testing.T, collection godi.ServiceCollection) {
				assert.Equal(t, 1, collection.Count())
				assert.True(t, collection.ContainsKeyed(
					reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
					"primary",
				))
			},
		},
		{
			name: "accepts multiple keyed services of same type",
			setup: func(t *testing.T) godi.ServiceCollection {
				collection := godi.NewServiceCollection()
				require.NoError(t, collection.AddSingleton(
					testutil.NewTestLogger,
					godi.Name("primary"),
				))
				return collection
			},
			construct: testutil.NewTestLogger,
			opts:      []godi.ProvideOption{godi.Name("secondary")},
			wantErr:   assert.NoError,
			validate: func(t *testing.T, collection godi.ServiceCollection) {
				assert.Equal(t, 2, collection.Count())
				assert.True(t, collection.ContainsKeyed(
					reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
					"primary",
				))
				assert.True(t, collection.ContainsKeyed(
					reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
					"secondary",
				))
			},
		},
		{
			name: "accepts group registration",
			setup: func(t *testing.T) godi.ServiceCollection {
				return godi.NewServiceCollection()
			},
			construct: func() testutil.TestHandler {
				return testutil.NewTestHandler("handler1")
			},
			opts:    []godi.ProvideOption{godi.Group("handlers")},
			wantErr: assert.NoError,
			validate: func(t *testing.T, collection godi.ServiceCollection) {
				assert.Equal(t, 1, collection.Count())
			},
		},
		{
			name: "accepts instance registration",
			setup: func(t *testing.T) godi.ServiceCollection {
				return godi.NewServiceCollection()
			},
			construct: &testutil.TestLoggerImpl{},
			wantErr:   assert.NoError,
			validate: func(t *testing.T, collection godi.ServiceCollection) {
				assert.Equal(t, 1, collection.Count())
				assert.True(t, collection.Contains(reflect.TypeOf((*testutil.TestLoggerImpl)(nil))))
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			collection := tt.setup(t)
			err := collection.AddSingleton(tt.construct, tt.opts...)
			tt.wantErr(t, err)

			if tt.validate != nil {
				tt.validate(t, collection)
			}
		})
	}
}

func TestServiceCollection_AddScoped(t *testing.T) {
	t.Run("adds scoped service", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		err := collection.AddScoped(testutil.NewTestService)
		require.NoError(t, err)

		descriptors := collection.ToSlice()
		require.Len(t, descriptors, 1)
		assert.Equal(t, godi.Scoped, descriptors[0].Lifetime)
	})

	t.Run("rejects same type with different lifetime", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add as singleton first
		require.NoError(t, collection.AddSingleton(testutil.NewTestService))

		// Try to add as scoped
		err := collection.AddScoped(testutil.NewTestService)
		assert.Error(t, err)

		var lifetimeErr godi.LifetimeConflictError
		assert.ErrorAs(t, err, &lifetimeErr)
	})
}

func TestServiceCollection_Decorate(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(t *testing.T) godi.ServiceCollection
		decorator interface{}
		wantErr   assert.ErrorAssertionFunc
		validate  func(t *testing.T, collection godi.ServiceCollection)
	}{
		{
			name: "decorates service successfully",
			setup: func(t *testing.T) godi.ServiceCollection {
				collection := godi.NewServiceCollection()
				require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))
				return collection
			},
			decorator: func(logger testutil.TestLogger) testutil.TestLogger {
				return &testutil.TestLoggerImpl{}
			},
			wantErr: assert.NoError,
			validate: func(t *testing.T, collection godi.ServiceCollection) {
				assert.Equal(t, 2, collection.Count()) // Original + decorator
			},
		},
		{
			name: "rejects nil decorator",
			setup: func(t *testing.T) godi.ServiceCollection {
				return godi.NewServiceCollection()
			},
			decorator: nil,
			wantErr:   assert.Error,
		},
		{
			name: "rejects non-function decorator",
			setup: func(t *testing.T) godi.ServiceCollection {
				return godi.NewServiceCollection()
			},
			decorator: "not a function",
			wantErr:   assert.Error,
		},
		{
			name: "rejects decorator with no parameters",
			setup: func(t *testing.T) godi.ServiceCollection {
				return godi.NewServiceCollection()
			},
			decorator: func() {},
			wantErr:   assert.Error,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			collection := tt.setup(t)
			err := collection.Decorate(tt.decorator)
			tt.wantErr(t, err)

			if tt.validate != nil {
				tt.validate(t, collection)
			}
		})
	}
}

func TestServiceCollection_Replace(t *testing.T) {
	t.Run("replaces existing service", func(t *testing.T) {
		t.Parallel()

		builder := testutil.NewServiceCollectionBuilder(t)

		// Add initial service
		builder.WithSingleton(func() testutil.TestLogger {
			logger := testutil.NewTestLogger()
			logger.Log("original")
			return logger
		})

		collection := builder.Build()
		require.Equal(t, 1, collection.Count())

		// Replace it
		err := collection.Replace(godi.Singleton, func() testutil.TestLogger {
			logger := testutil.NewTestLogger()
			logger.Log("replaced")
			return logger
		})
		require.NoError(t, err)

		assert.Equal(t, 1, collection.Count())

		// Verify replacement works
		provider := builder.BuildProvider()
		logger := testutil.AssertServiceResolvable[testutil.TestLogger](t, provider)

		logs := logger.GetLogs()
		require.Len(t, logs, 1)
		assert.Equal(t, "replaced", logs[0])
	})

	t.Run("changes lifetime when replacing", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add as singleton
		require.NoError(t, collection.AddSingleton(testutil.NewTestService))

		// Replace as scoped
		err := collection.Replace(godi.Scoped, testutil.NewTestService)
		require.NoError(t, err)

		descriptors := collection.ToSlice()
		require.Len(t, descriptors, 1)
		assert.Equal(t, godi.Scoped, descriptors[0].Lifetime)
	})
}

func TestServiceCollection_RemoveAll(t *testing.T) {
	t.Run("removes all services of type", func(t *testing.T) {
		t.Parallel()

		builder := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithSingleton(testutil.NewTestDatabase)

		collection := builder.Build()
		require.Equal(t, 2, collection.Count())

		// Remove TestLogger
		err := collection.RemoveAll(reflect.TypeOf((*testutil.TestLogger)(nil)).Elem())
		require.NoError(t, err)

		assert.Equal(t, 1, collection.Count())
		assert.False(t, collection.Contains(reflect.TypeOf((*testutil.TestLogger)(nil)).Elem()))
		assert.True(t, collection.Contains(reflect.TypeOf((*testutil.TestDatabase)(nil)).Elem()))
	})

	t.Run("removes keyed services", func(t *testing.T) {
		t.Parallel()

		builder := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger, godi.Name("primary")).
			WithSingleton(testutil.NewTestLogger, godi.Name("secondary"))

		collection := builder.Build()
		require.Equal(t, 2, collection.Count())

		// Remove all TestLogger services
		err := collection.RemoveAll(reflect.TypeOf((*testutil.TestLogger)(nil)).Elem())
		require.NoError(t, err)

		assert.Equal(t, 0, collection.Count())
		assert.False(t, collection.ContainsKeyed(
			reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
			"primary",
		))
		assert.False(t, collection.ContainsKeyed(
			reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
			"secondary",
		))
	})
}

func TestServiceCollection_Clear(t *testing.T) {
	t.Run("clears all services", func(t *testing.T) {
		t.Parallel()

		builder := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithScoped(testutil.NewTestService)

		collection := builder.Build()
		require.GreaterOrEqual(t, collection.Count(), 2)

		collection.Clear()

		assert.Equal(t, 0, collection.Count())
		assert.Empty(t, collection.ToSlice())
	})

	t.Run("can add services after clear", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add and clear
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))
		collection.Clear()

		// Should be able to add again
		err := collection.AddSingleton(testutil.NewTestLogger)
		assert.NoError(t, err)
		assert.Equal(t, 1, collection.Count())
	})
}

func TestServiceCollection_BuildServiceProvider(t *testing.T) {
	t.Run("builds provider with services", func(t *testing.T) {
		t.Parallel()

		builder := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithSingleton(testutil.NewTestDatabase).
			WithScoped(testutil.NewTestServiceWithDeps)

		provider := builder.BuildProvider()

		assert.NotNil(t, provider)
		assert.False(t, provider.IsDisposed())

		// Verify services are registered
		assert.True(t, provider.IsService(reflect.TypeOf((*testutil.TestLogger)(nil)).Elem()))
		assert.True(t, provider.IsService(reflect.TypeOf((*testutil.TestDatabase)(nil)).Elem()))
		assert.True(t, provider.IsService(reflect.TypeOf((*testutil.TestServiceWithDeps)(nil))))
	})

	t.Run("builds empty provider", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()

		require.NoError(t, err)
		assert.NotNil(t, provider)

		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})
	})

	t.Run("prevents building twice", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Try to build again
		_, err = collection.BuildServiceProvider()
		assert.Error(t, err)
		assert.ErrorIs(t, err, godi.ErrCollectionBuilt)
	})

	t.Run("validates on build when requested", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add a service with missing dependency
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
	})
}

func TestServiceCollection_ModificationAfterBuild(t *testing.T) {
	t.Run("prevents modification after build", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// All modification methods should fail
		err = collection.AddSingleton(testutil.NewTestLogger)
		assert.ErrorIs(t, err, godi.ErrCollectionBuilt)

		err = collection.AddScoped(testutil.NewTestDatabase)
		assert.ErrorIs(t, err, godi.ErrCollectionBuilt)

		err = collection.Decorate(func(l testutil.TestLogger) testutil.TestLogger { return l })
		assert.ErrorIs(t, err, godi.ErrCollectionBuilt)

		err = collection.Replace(godi.Singleton, testutil.NewTestLogger)
		assert.ErrorIs(t, err, godi.ErrCollectionBuilt)

		err = collection.RemoveAll(reflect.TypeOf((*testutil.TestLogger)(nil)).Elem())
		assert.ErrorIs(t, err, godi.ErrCollectionBuilt)
	})
}

func TestServiceCollection_ResultObjects(t *testing.T) {
	t.Run("accepts result object constructors", func(t *testing.T) {
		t.Parallel()

		constructor := func() testutil.TestServiceResult {
			return testutil.TestServiceResult{
				Service:  testutil.NewTestService(),
				Logger:   testutil.NewTestLogger(),
				Database: testutil.NewTestDatabase(),
			}
		}

		collection := godi.NewServiceCollection()
		err := collection.AddSingleton(constructor)

		require.NoError(t, err)
		assert.Equal(t, 1, collection.Count())
	})
}

func TestServiceCollection_ParameterObjects(t *testing.T) {
	t.Run("handles parameter objects", func(t *testing.T) {
		t.Parallel()

		// Constructor using parameter object
		constructor := func(params testutil.TestServiceParams) *testutil.TestService {
			return &testutil.TestService{
				ID:   "from-params",
				Data: "test",
			}
		}

		builder := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			WithSingleton(testutil.NewTestDatabase).
			WithScoped(constructor)

		provider := builder.BuildProvider()
		scope := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		service := testutil.AssertServiceResolvableInScope[*testutil.TestService](t, scope)
		assert.Equal(t, "from-params", service.ID)
	})
}

func TestServiceCollection_AddModules(t *testing.T) {
	t.Run("applies modules successfully", func(t *testing.T) {
		t.Parallel()

		loggerModule := godi.NewModule("logger",
			godi.AddSingleton(testutil.NewTestLogger),
		)

		databaseModule := godi.NewModule("database",
			godi.AddSingleton(testutil.NewTestDatabase),
			godi.AddSingleton(testutil.NewTestCache),
		)

		collection := godi.NewServiceCollection()
		err := collection.AddModules(loggerModule, databaseModule)

		require.NoError(t, err)
		assert.Equal(t, 3, collection.Count())
		assert.True(t, collection.Contains(reflect.TypeOf((*testutil.TestLogger)(nil)).Elem()))
		assert.True(t, collection.Contains(reflect.TypeOf((*testutil.TestDatabase)(nil)).Elem()))
		assert.True(t, collection.Contains(reflect.TypeOf((*testutil.TestCache)(nil)).Elem()))
	})

	t.Run("module error includes module name", func(t *testing.T) {
		t.Parallel()

		errorModule := godi.NewModule("problematic",
			func(s godi.ServiceCollection) error {
				return testutil.ErrIntentional
			},
		)

		collection := godi.NewServiceCollection()
		err := collection.AddModules(errorModule)

		assert.Error(t, err)
		assert.ErrorIs(t, err, testutil.ErrIntentional)

		var moduleErr godi.ModuleError
		assert.ErrorAs(t, err, &moduleErr)
		assert.Equal(t, "problematic", moduleErr.Module)
	})
}

func TestServiceCollection_ExtendedCoverage(t *testing.T) {
	t.Run("instance registration with various types", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Register different instance types
		instances := []interface{}{
			"string instance",
			42,
			3.14,
			true,
			[]string{"a", "b", "c"},
			map[string]int{"one": 1, "two": 2},
			&testutil.TestLoggerImpl{},
		}

		for i, instance := range instances {
			err := collection.AddSingleton(instance)
			assert.NoError(t, err, "failed to add instance %d: %v", i, instance)
		}

		assert.Equal(t, len(instances), collection.Count())
	})

	t.Run("constructor with dig.Out validation", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Valid result object
		validConstructor := func() testutil.TestServiceResult {
			return testutil.TestServiceResult{
				Service:  testutil.NewTestService(),
				Logger:   testutil.NewTestLogger(),
				Database: testutil.NewTestDatabase(),
			}
		}

		err := collection.AddSingleton(validConstructor)
		assert.NoError(t, err)
	})

	t.Run("invalid lifetime value", func(t *testing.T) {
		invalidLifetime := godi.ServiceLifetime(999)
		assert.False(t, invalidLifetime.IsValid(), "Expected invalid lifetime to be false")
	})

	t.Run("multiple options extraction", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Test Name extraction
		err := collection.AddSingleton(testutil.NewTestLogger, godi.Name("primary"))
		require.NoError(t, err)

		// Test Group extraction
		err = collection.AddSingleton(func() testutil.TestHandler {
			return testutil.NewTestHandler("h1")
		}, godi.Group("handlers"))
		require.NoError(t, err)

		// Test As extraction
		err = collection.AddSingleton(
			testutil.NewTestDatabase,
			godi.As(new(interface{})),
		)
		require.NoError(t, err)

		// Test multiple options
		err = collection.AddSingleton(
			func() testutil.TestHandler {
				return testutil.NewTestHandler("h2")
			},
			godi.Name("secondary"),
			godi.Group("handlers"),
		)
		require.NoError(t, err)

		assert.Equal(t, 4, collection.Count())
	})

	t.Run("nil option in list", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		err := collection.AddSingleton(
			testutil.NewTestCache,
			godi.Name("cache"),
			nil, // nil option should be skipped
			godi.Group("caches"),
		)
		assert.NoError(t, err)
	})

	t.Run("callback options", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()
		var callbackInvoked bool

		err := collection.AddSingleton(
			func() string { return "test" },
			godi.WithProviderCallback(func(ci godi.CallbackInfo) {
				callbackInvoked = true
			}),
		)
		require.NoError(t, err)

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Resolve to trigger callback
		_, _ = godi.Resolve[string](provider)
		assert.True(t, callbackInvoked)
	})

	t.Run("FillProvideInfo option", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()
		var info godi.ProvideInfo

		err := collection.AddSingleton(
			func() int { return 42 },
			godi.FillProvideInfo(&info),
		)
		assert.NoError(t, err)
	})

	t.Run("removeByTypeInternal coverage", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add multiple services of different types
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))
		require.NoError(t, collection.AddSingleton(testutil.NewTestDatabase))
		require.NoError(t, collection.AddSingleton(testutil.NewTestCache))

		// Add keyed services
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger, godi.Name("logger1")))
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger, godi.Name("logger2")))

		// Add group services
		require.NoError(t, collection.AddSingleton(func() testutil.TestHandler {
			return testutil.NewTestHandler("h1")
		}, godi.Group("handlers")))

		assert.Equal(t, 6, collection.Count())

		// Remove all logger services (including keyed)
		loggerType := reflect.TypeOf((*testutil.TestLogger)(nil)).Elem()
		err := collection.RemoveAll(loggerType)
		require.NoError(t, err)

		// Verify removal
		assert.False(t, collection.Contains(loggerType))
		assert.False(t, collection.ContainsKeyed(loggerType, "logger1"))
		assert.False(t, collection.ContainsKeyed(loggerType, "logger2"))
		assert.Equal(t, 3, collection.Count())

		// Other types should remain
		assert.True(t, collection.Contains(reflect.TypeOf((*testutil.TestDatabase)(nil)).Elem()))
		assert.True(t, collection.Contains(reflect.TypeOf((*testutil.TestCache)(nil)).Elem()))
	})

	t.Run("lifetime conflict with keyed services", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add non-keyed singleton
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))

		// Should be able to add keyed service with different lifetime
		err := collection.AddScoped(testutil.NewTestLogger, godi.Name("scoped"))
		assert.NoError(t, err)

		// But not non-keyed with different lifetime
		err = collection.AddScoped(testutil.NewTestLogger)
		assert.Error(t, err)
		var lifetimeErr godi.LifetimeConflictError
		assert.ErrorAs(t, err, &lifetimeErr)
	})

	t.Run("group services don't conflict", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add to group as singleton
		require.NoError(t, collection.AddSingleton(
			func() testutil.TestHandler { return testutil.NewTestHandler("h1") },
			godi.Group("handlers"),
		))

		// Should be able to add same type to group with same lifetime
		err := collection.AddSingleton(
			func() testutil.TestHandler { return testutil.NewTestHandler("h2") },
			godi.Group("handlers"),
		)
		assert.NoError(t, err)
	})

	t.Run("decorators don't conflict with lifetimes", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()
		require.NoError(t, collection.AddSingleton(testutil.NewTestDatabase))

		// Decorator should work regardless of lifetime tracking
		err := collection.Decorate(func(db testutil.TestDatabase) testutil.TestDatabase {
			return db
		})
		assert.NoError(t, err)
	})

	t.Run("Clear with built collection", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))

		// Build the collection
		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Clear should reset built flag
		collection.Clear()
		assert.Equal(t, 0, collection.Count())

		// Should be able to add services again
		err = collection.AddSingleton(testutil.NewTestDatabase)
		assert.NoError(t, err)
	})

	t.Run("complex validation scenario", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Constructor that returns only error
		errorOnly := func() error { return nil }
		err := collection.AddSingleton(errorOnly)
		assert.Error(t, err)
		// The error message includes the full text
		assert.Contains(t, err.Error(), "constructor must be a function that returns at least one value")

		// Valid constructor with error
		validWithError := func() (testutil.TestLogger, error) {
			return testutil.NewTestLogger(), nil
		}
		err = collection.AddSingleton(validWithError)
		assert.NoError(t, err)
	})

	t.Run("validation catches missing dependencies", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add service with missing dependency
		require.NoError(t, collection.AddSingleton(
			func(missing *testutil.TestService) testutil.TestLogger {
				return testutil.NewTestLogger()
			},
		))

		opts := &godi.ServiceProviderOptions{
			ValidateOnBuild: true,
		}

		_, err := collection.BuildServiceProviderWithOptions(opts)
		assert.Error(t, err)
		assert.True(t, godi.IsNotFound(err))
	})

	t.Run("validation catches circular dependencies", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Create circular dependency
		require.NoError(t, collection.AddSingleton(testutil.NewCircularServiceA))
		require.NoError(t, collection.AddSingleton(testutil.NewCircularServiceB))

		opts := &godi.ServiceProviderOptions{
			ValidateOnBuild:          true,
			DeferAcyclicVerification: false,
		}

		_, err := collection.BuildServiceProviderWithOptions(opts)
		assert.Error(t, err)
		assert.True(t, godi.IsCircularDependency(err))
	})

	t.Run("dry run mode", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		constructorCalled := false
		require.NoError(t, collection.AddSingleton(func() testutil.TestLogger {
			constructorCalled = true
			return testutil.NewTestLogger()
		}))

		opts := &godi.ServiceProviderOptions{
			DryRun: true,
		}

		provider, err := collection.BuildServiceProviderWithOptions(opts)
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Constructor should not be called in dry run mode
		assert.False(t, constructorCalled)
	})

	t.Run("ToSlice immutability", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))

		// Get slice
		slice1 := collection.ToSlice()
		originalLen := len(slice1)

		//lint:ignore SA4006 Appending should not change the original slice
		slice1 = append(slice1, nil)

		// Original collection should be unchanged
		slice2 := collection.ToSlice()
		assert.Equal(t, originalLen, len(slice2))
		assert.Equal(t, originalLen, collection.Count())
	})

	t.Run("complex option string parsing", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Test edge cases in option string parsing
		// Some of these might fail depending on dig's validation
		complexNames := []string{
			`testname`,             // Normal name
			`test-with-dash`,       // With dash
			`test_with_underscore`, // With underscore
		}

		for i, name := range complexNames {
			err := collection.AddSingleton(
				func() string { return fmt.Sprintf("test-%d", i) },
				godi.Name(name),
			)
			require.NoError(t, err, "failed to add service with name %q", name)
		}

		// Build provider to verify
		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Verify services are registered
		for i, name := range complexNames {
			assert.True(t, provider.IsKeyedService(reflect.TypeOf(""), name))

			s, err := godi.ResolveKeyed[string](provider, name)
			require.NoError(t, err)
			assert.Equal(t, fmt.Sprintf("test-%d", i), s)
		}

		// Test invalid names separately
		invalidNames := []string{
			`test"with"quotes`,
			`test'with'single`,
			`test with spaces`,
			``, // empty name
		}

		for _, name := range invalidNames {
			collection2 := godi.NewServiceCollection()
			err := collection2.AddSingleton(
				func() string { return "test" },
				godi.Name(name),
			)
			// These might fail - that's expected
			_ = err
		}
	})
}

func TestServiceCollection_ErrorPropagation(t *testing.T) {
	t.Run("module error details", func(t *testing.T) {
		t.Parallel()

		expectedErr := errors.New("module init failed")

		failingModule := godi.NewModule("failing",
			godi.AddSingleton(testutil.NewTestLogger),
			func(s godi.ServiceCollection) error {
				return expectedErr
			},
			godi.AddSingleton(testutil.NewTestDatabase), // Should not be reached
		)

		collection := godi.NewServiceCollection()
		err := collection.AddModules(failingModule)

		assert.Error(t, err)
		var moduleErr godi.ModuleError
		assert.ErrorAs(t, err, &moduleErr)
		assert.Equal(t, "failing", moduleErr.Module)
		assert.ErrorIs(t, err, expectedErr)

		// Only first service should be registered
		assert.Equal(t, 1, collection.Count())
	})

	t.Run("nested module errors", func(t *testing.T) {
		t.Parallel()

		innerModule := godi.NewModule("inner",
			func(s godi.ServiceCollection) error {
				return godi.ErrNilConstructor
			},
		)

		outerModule := godi.NewModule("outer",
			godi.AddSingleton(testutil.NewTestLogger),
			innerModule,
		)

		collection := godi.NewServiceCollection()
		err := collection.AddModules(outerModule)

		assert.Error(t, err)

		// Should have nested module errors
		var outerErr godi.ModuleError
		assert.ErrorAs(t, err, &outerErr)
		assert.Equal(t, "outer", outerErr.Module)

		var innerErr godi.ModuleError
		assert.ErrorAs(t, outerErr.Cause, &innerErr)
		assert.Equal(t, "inner", innerErr.Module)
		assert.ErrorIs(t, err, godi.ErrNilConstructor)
	})

	t.Run("validation error details", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add invalid service - this should fail immediately
		err := collection.AddSingleton(func() {}) // No return
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "constructor must return at least one value")

		// Try with a different validation error that happens during build
		collection2 := godi.NewServiceCollection()

		// This constructor is valid but has missing dependency
		require.NoError(t, collection2.AddSingleton(func(missing *testutil.TestCache) testutil.TestLogger {
			return testutil.NewTestLogger()
		}))

		opts := &godi.ServiceProviderOptions{
			ValidateOnBuild: true,
		}

		_, err = collection2.BuildServiceProviderWithOptions(opts)
		assert.Error(t, err)
		// This might be a different error type (not found rather than validation)
		assert.True(t, godi.IsNotFound(err))
	})
}

func TestServiceCollection_Replace_Extended(t *testing.T) {
	t.Run("replace with keyed services present", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add non-keyed and keyed services
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger, godi.Name("keyed")))

		// Replace should only affect non-keyed
		err := collection.Replace(godi.Scoped, testutil.NewTestLogger)
		require.NoError(t, err)

		// After building provider, we can check keyed service
		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Keyed service should still exist
		assert.True(t, provider.IsKeyedService(
			reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
			"keyed",
		))
	})

	t.Run("replace non-existent service", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Replace when service doesn't exist should add it
		err := collection.Replace(godi.Singleton, testutil.NewTestLogger)
		require.NoError(t, err)

		assert.Equal(t, 1, collection.Count())
		assert.True(t, collection.Contains(reflect.TypeOf((*testutil.TestLogger)(nil)).Elem()))
	})

	t.Run("replace with result object", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		constructor := func() testutil.TestServiceResult {
			return testutil.TestServiceResult{
				Service:  testutil.NewTestService(),
				Logger:   testutil.NewTestLogger(),
				Database: testutil.NewTestDatabase(),
			}
		}

		// Replace with result object should fail
		err := collection.Replace(godi.Singleton, constructor)
		assert.ErrorIs(t, err, godi.ErrReplaceResultObject)
	})

	t.Run("replace with non-function constructor", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// This should fail validation
		err := collection.Replace(godi.Singleton, "not a function")
		assert.Error(t, err)
	})

	t.Run("replace non-keyed preserves keyed services", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add non-keyed and multiple keyed services
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger, godi.Name("primary")))
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger, godi.Name("secondary")))

		// Replace non-keyed service with scoped
		err := collection.Replace(godi.Scoped, testutil.NewTestLogger)
		require.NoError(t, err)

		// Build provider to verify
		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Non-keyed service should exist as scoped
		loggerType := reflect.TypeOf((*testutil.TestLogger)(nil)).Elem()
		assert.True(t, provider.IsService(loggerType))

		// Keyed services should still exist
		assert.True(t, provider.IsKeyedService(loggerType, "primary"))
		assert.True(t, provider.IsKeyedService(loggerType, "secondary"))

		// Verify we can resolve all of them
		logger := testutil.AssertServiceResolvable[testutil.TestLogger](t, provider)
		assert.NotNil(t, logger)

		primary := testutil.AssertKeyedServiceResolvable[testutil.TestLogger](t, provider, "primary")
		assert.NotNil(t, primary)

		secondary := testutil.AssertKeyedServiceResolvable[testutil.TestLogger](t, provider, "secondary")
		assert.NotNil(t, secondary)
	})

	t.Run("replace specific keyed service", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add non-keyed and keyed services
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))
		require.NoError(t, collection.AddSingleton(func() testutil.TestLogger {
			logger := testutil.NewTestLogger()
			logger.Log("primary")
			return logger
		}, godi.Name("primary")))
		require.NoError(t, collection.AddSingleton(func() testutil.TestLogger {
			logger := testutil.NewTestLogger()
			logger.Log("secondary")
			return logger
		}, godi.Name("secondary")))

		// Replace only the "primary" keyed service
		err := collection.Replace(godi.Scoped, func() testutil.TestLogger {
			logger := testutil.NewTestLogger()
			logger.Log("primary-replaced")
			return logger
		}, godi.Name("primary"))
		require.NoError(t, err)

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Non-keyed should still exist
		assert.True(t, provider.IsService(reflect.TypeOf((*testutil.TestLogger)(nil)).Elem()))

		// Primary should exist (replaced)
		assert.True(t, provider.IsKeyedService(reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(), "primary"))

		// Secondary should still exist (not replaced)
		assert.True(t, provider.IsKeyedService(reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(), "secondary"))

		// Verify the primary was actually replaced
		scope := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		primary := testutil.AssertKeyedServiceResolvable[testutil.TestLogger](t, scope, "primary")
		assert.Contains(t, primary.GetLogs(), "primary-replaced")

		secondary := testutil.AssertKeyedServiceResolvable[testutil.TestLogger](t, provider, "secondary")
		assert.Contains(t, secondary.GetLogs(), "secondary")
	})

	t.Run("replace preserves group services", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add non-keyed and group services
		require.NoError(t, collection.AddSingleton(func() testutil.TestHandler {
			return testutil.NewTestHandler("non-keyed")
		}))
		require.NoError(t, collection.AddSingleton(func() testutil.TestHandler {
			return testutil.NewTestHandler("handler1")
		}, godi.Group("handlers")))
		require.NoError(t, collection.AddSingleton(func() testutil.TestHandler {
			return testutil.NewTestHandler("handler2")
		}, godi.Group("handlers")))

		// Replace non-keyed service
		err := collection.Replace(godi.Scoped, func() testutil.TestHandler {
			return testutil.NewTestHandler("replaced")
		})
		require.NoError(t, err)

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Group services should still exist
		handlers, err := godi.ResolveGroup[testutil.TestHandler](provider, "handlers")
		require.NoError(t, err)
		assert.Len(t, handlers, 2)

		// Verify group handlers are unchanged
		handlerNames := make(map[string]bool)
		for _, h := range handlers {
			handlerNames[h.Handle()] = true
		}
		assert.True(t, handlerNames["handler1"])
		assert.True(t, handlerNames["handler2"])
		assert.False(t, handlerNames["replaced"]) // Should not be in group

		// Non-keyed service should be replaced
		scope := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		handler := testutil.AssertServiceResolvableInScope[testutil.TestHandler](t, scope)
		assert.Equal(t, "replaced", handler.Handle())
	})

	t.Run("replace with mixed registrations", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add various combinations
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger, godi.Name("key1")))
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger, godi.Name("key2")))
		require.NoError(t, collection.AddSingleton(func() testutil.TestLogger {
			return testutil.NewTestLogger()
		}, godi.Group("loggers")))
		require.NoError(t, collection.AddSingleton(func() testutil.TestLogger {
			return testutil.NewTestLogger()
		}, godi.Group("loggers")))

		// Count before replace
		assert.Equal(t, 5, collection.Count())

		// Replace non-keyed
		err := collection.Replace(godi.Scoped, testutil.NewTestLogger)
		require.NoError(t, err)

		// Should still have 5 services (1 replaced + 2 keyed + 2 group)
		assert.Equal(t, 5, collection.Count())

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Verify all services exist
		loggerType := reflect.TypeOf((*testutil.TestLogger)(nil)).Elem()
		assert.True(t, provider.IsService(loggerType))
		assert.True(t, provider.IsKeyedService(loggerType, "key1"))
		assert.True(t, provider.IsKeyedService(loggerType, "key2"))

		loggers, err := godi.ResolveGroup[testutil.TestLogger](provider, "loggers")
		require.NoError(t, err)
		assert.Len(t, loggers, 2)
	})

	t.Run("replace keyed service does not affect non-keyed", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add both non-keyed and keyed
		require.NoError(t, collection.AddSingleton(func() testutil.TestDatabase {
			return testutil.NewTestDatabaseNamed("non-keyed")
		}))
		require.NoError(t, collection.AddSingleton(func() testutil.TestDatabase {
			return testutil.NewTestDatabaseNamed("keyed-db")
		}, godi.Name("primary")))

		// Replace only keyed service
		err := collection.Replace(godi.Scoped, func() testutil.TestDatabase {
			return testutil.NewTestDatabaseNamed("keyed-replaced")
		}, godi.Name("primary"))
		require.NoError(t, err)

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Non-keyed should be unchanged
		nonKeyed := testutil.AssertServiceResolvable[testutil.TestDatabase](t, provider)
		assert.Contains(t, nonKeyed.Query("test"), "non-keyed")

		// Keyed should be replaced and scoped
		scope := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		keyed := testutil.AssertKeyedServiceResolvable[testutil.TestDatabase](t, scope, "primary")
		assert.Contains(t, keyed.Query("test"), "keyed-replaced")
	})

	t.Run("replace non-existent keyed service adds it", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add only non-keyed
		require.NoError(t, collection.AddSingleton(testutil.NewTestCache))

		// Replace with a key that doesn't exist should add it
		err := collection.Replace(godi.Singleton, testutil.NewTestCache, godi.Name("memory"))
		require.NoError(t, err)

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Both should exist
		cacheType := reflect.TypeOf((*testutil.TestCache)(nil)).Elem()
		assert.True(t, provider.IsService(cacheType))
		assert.True(t, provider.IsKeyedService(cacheType, "memory"))

		// Can resolve both
		cache1 := testutil.AssertServiceResolvable[testutil.TestCache](t, provider)
		cache2 := testutil.AssertKeyedServiceResolvable[testutil.TestCache](t, provider, "memory")
		assert.NotNil(t, cache1)
		assert.NotNil(t, cache2)
	})

	t.Run("replace with lifetime change for keyed service", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add keyed services with singleton lifetime
		require.NoError(t, collection.AddSingleton(testutil.NewTestService, godi.Name("service1")))
		require.NoError(t, collection.AddSingleton(testutil.NewTestService, godi.Name("service2")))

		// Replace one to scoped
		err := collection.Replace(godi.Scoped, testutil.NewTestService, godi.Name("service1"))
		require.NoError(t, err)

		// Build provider
		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Create two scopes
		scope1 := provider.CreateScope(context.Background())
		scope2 := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, scope1.Close())
			require.NoError(t, scope2.Close())
		})

		// service1 should be scoped (different instances per scope)
		svc1_scope1 := testutil.AssertKeyedServiceResolvable[*testutil.TestService](t, scope1, "service1")
		svc1_scope2 := testutil.AssertKeyedServiceResolvable[*testutil.TestService](t, scope2, "service1")
		assert.NotEqual(t, svc1_scope1.ID, svc1_scope2.ID) // Different instances

		// service2 should still be singleton (same instance across scopes)
		svc2_scope1 := testutil.AssertKeyedServiceResolvable[*testutil.TestService](t, scope1, "service2")
		svc2_scope2 := testutil.AssertKeyedServiceResolvable[*testutil.TestService](t, scope2, "service2")
		assert.Equal(t, svc2_scope1.ID, svc2_scope2.ID) // Same instance
	})

	t.Run("replace fails after build", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))

		// Build provider
		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Try to replace after build
		err = collection.Replace(godi.Scoped, testutil.NewTestLogger)
		assert.ErrorIs(t, err, godi.ErrCollectionBuilt)

		// Try to replace keyed after build
		err = collection.Replace(godi.Scoped, testutil.NewTestLogger, godi.Name("test"))
		assert.ErrorIs(t, err, godi.ErrCollectionBuilt)
	})

	t.Run("replace with complex options", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add service with multiple options
		require.NoError(t, collection.AddSingleton(
			func() testutil.TestHandler { return testutil.NewTestHandler("original") },
			godi.Name("named"),
			godi.Group("handlers"),
		))

		// Replace with different options
		err := collection.Replace(
			godi.Scoped,
			func() testutil.TestHandler { return testutil.NewTestHandler("replaced") },
			godi.Name("named"), // Same name to replace the specific one
		)
		require.NoError(t, err)

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// The service should no longer be in the group
		handlers, err := godi.ResolveGroup[testutil.TestHandler](provider, "handlers")
		require.NoError(t, err)
		assert.Len(t, handlers, 0) // Group should be empty

		// But should be available as keyed
		scope := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		handler := testutil.AssertKeyedServiceResolvable[testutil.TestHandler](t, scope, "named")
		assert.Equal(t, "replaced", handler.Handle())
	})
}

func TestServiceCollection_ExtendedCoverage_ReplaceScenarios(t *testing.T) {
	t.Run("replace removes decorators", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()

		// Add service
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))

		// Add decorator
		require.NoError(t, collection.Decorate(func(logger testutil.TestLogger) testutil.TestLogger {
			// Wrap the logger
			return logger
		}))

		// Replace the service
		err := collection.Replace(godi.Scoped, testutil.NewTestLogger)
		require.NoError(t, err)

		// Decorator should be removed
		assert.Equal(t, 1, collection.Count())
	})
}
