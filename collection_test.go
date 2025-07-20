package godi_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/junioryono/godi"
	"github.com/junioryono/godi/internal/testutil"
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
