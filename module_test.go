package godi_test

import (
	"context"
	"errors"
	"testing"

	"github.com/junioryono/godi/v2"
	"github.com/junioryono/godi/v2/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewModule(t *testing.T) {
	t.Run("creates module with services", func(t *testing.T) {
		t.Parallel()

		module := godi.NewModule("test-module",
			godi.AddSingleton(testutil.NewTestLogger),
			godi.AddScoped(testutil.NewTestService),
		)

		provider := godi.NewServiceProvider()
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})
		err := provider.AddModules(module)
		require.NoError(t, err)
	})

	t.Run("empty module", func(t *testing.T) {
		t.Parallel()

		module := godi.NewModule("empty-module")

		provider := godi.NewServiceProvider()
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})
		err := provider.AddModules(module)
		require.NoError(t, err)
	})

	t.Run("module with nil builders", func(t *testing.T) {
		t.Parallel()

		module := godi.NewModule("module-with-nils",
			godi.AddSingleton(testutil.NewTestLogger),
			nil, // Should be skipped
			godi.AddScoped(testutil.NewTestService),
		)

		provider := godi.NewServiceProvider()
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})
		err := provider.AddModules(module)
		require.NoError(t, err)
	})
}

func TestModule_Composition(t *testing.T) {
	t.Run("nested modules", func(t *testing.T) {
		t.Parallel()

		// Create sub-modules
		loggingModule := godi.NewModule("logging",
			godi.AddSingleton(testutil.NewTestLogger),
		)

		dataModule := godi.NewModule("data",
			godi.AddSingleton(testutil.NewTestDatabase),
			godi.AddSingleton(testutil.NewTestCache),
		)

		serviceModule := godi.NewModule("services",
			godi.AddScoped(testutil.NewTestServiceWithDeps),
		)

		// Compose into main module
		appModule := godi.NewModule("app",
			loggingModule,
			dataModule,
			serviceModule,
			godi.AddSingleton(func() string { return "app-config" }),
		)

		provider := godi.NewServiceProvider()
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})
		err := provider.AddModules(appModule)
		require.NoError(t, err)
	})

	t.Run("multiple module registration", func(t *testing.T) {
		t.Parallel()

		module1 := godi.NewModule("module1",
			godi.AddSingleton(testutil.NewTestLogger),
		)

		module2 := godi.NewModule("module2",
			godi.AddSingleton(testutil.NewTestDatabase),
		)

		module3 := godi.NewModule("module3",
			godi.AddSingleton(testutil.NewTestCache),
		)

		provider := godi.NewServiceProvider()
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})
		err := provider.AddModules(module1, module2, module3)
		require.NoError(t, err)
	})
}

func TestModule_ErrorHandling(t *testing.T) {
	t.Run("error in module builder", func(t *testing.T) {
		t.Parallel()

		expectedErr := errors.New("module error")

		module := godi.NewModule("error-module",
			godi.AddSingleton(testutil.NewTestLogger),
			func(s godi.ServiceProvider) error {
				return expectedErr
			},
			godi.AddSingleton(testutil.NewTestDatabase), // Should not be reached
		)

		provider := godi.NewServiceProvider()
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})
		err := provider.AddModules(module)

		assert.Error(t, err)

		var moduleErr godi.ModuleError
		assert.ErrorAs(t, err, &moduleErr)
		assert.Equal(t, "error-module", moduleErr.Module)
		assert.ErrorIs(t, err, expectedErr)
	})

	t.Run("error in nested module", func(t *testing.T) {
		t.Parallel()

		errorSubModule := godi.NewModule("sub-error",
			func(s godi.ServiceProvider) error {
				return testutil.ErrIntentional
			},
		)

		mainModule := godi.NewModule("main",
			godi.AddSingleton(testutil.NewTestLogger),
			errorSubModule,
		)

		provider := godi.NewServiceProvider()
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})
		err := provider.AddModules(mainModule)

		assert.Error(t, err)

		// Should have nested module errors
		var moduleErr godi.ModuleError
		assert.ErrorAs(t, err, &moduleErr)
		assert.Equal(t, "main", moduleErr.Module)

		// The cause should also be a module error
		var causeErr godi.ModuleError
		assert.ErrorAs(t, moduleErr.Cause, &causeErr)
		assert.Equal(t, "sub-error", causeErr.Module)
		assert.ErrorIs(t, err, testutil.ErrIntentional)
	})
}

func TestModule_WithDecorator(t *testing.T) {
	t.Run("module with decorator", func(t *testing.T) {
		t.Parallel()

		type DecoratedLogger struct {
			testutil.TestLogger
			prefix string
		}

		module := godi.NewModule("decorated",
			godi.AddSingleton(testutil.NewTestLogger),
			godi.AddDecorator(func(logger testutil.TestLogger) testutil.TestLogger {
				return &DecoratedLogger{
					TestLogger: logger,
					prefix:     "[DECORATED] ",
				}
			}),
		)

		provider := godi.NewServiceProvider()
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})
		err := provider.AddModules(module)
		require.NoError(t, err)

		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		logger := testutil.AssertServiceResolvable[testutil.TestLogger](t, provider)
		_, ok := logger.(*DecoratedLogger)
		assert.True(t, ok, "logger should be decorated")
	})
}

func TestModule_RealWorldScenarios(t *testing.T) {
	t.Run("web application modules", func(t *testing.T) {
		t.Parallel()

		// Simulate a typical web app module structure

		// Infrastructure module
		infrastructureModule := godi.NewModule("infrastructure",
			godi.AddSingleton(testutil.NewTestLogger),
			godi.AddSingleton(testutil.NewTestDatabase),
			godi.AddSingleton(testutil.NewTestCache),
		)

		// Repository module
		type UserRepository struct {
			db testutil.TestDatabase
		}

		repositoryModule := godi.NewModule("repositories",
			godi.AddScoped(func(db testutil.TestDatabase) *UserRepository {
				return &UserRepository{db: db}
			}),
		)

		// Service module
		type UserService struct {
			repo   *UserRepository
			logger testutil.TestLogger
			cache  testutil.TestCache
		}

		serviceModule := godi.NewModule("services",
			godi.AddScoped(func(repo *UserRepository, logger testutil.TestLogger, cache testutil.TestCache) *UserService {
				return &UserService{
					repo:   repo,
					logger: logger,
					cache:  cache,
				}
			}),
		)

		// Handler module
		type UserHandler struct {
			service *UserService
		}

		handlerModule := godi.NewModule("handlers",
			godi.AddScoped(func(service *UserService) *UserHandler {
				return &UserHandler{service: service}
			}),
		)

		// Main application module
		appModule := godi.NewModule("app",
			infrastructureModule,
			repositoryModule,
			serviceModule,
			handlerModule,
		)

		// Build and test
		provider := godi.NewServiceProvider()
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})
		err := provider.AddModules(appModule)
		require.NoError(t, err)

		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Create request scope
		scope := provider.CreateScope(context.Background())
		t.Cleanup(func() {
			require.NoError(t, scope.Close())
		})

		// Resolve the handler (top of dependency chain)
		handler := testutil.AssertServiceResolvableInScope[*UserHandler](t, scope)
		assert.NotNil(t, handler)
		assert.NotNil(t, handler.service)
		assert.NotNil(t, handler.service.repo)
		assert.NotNil(t, handler.service.logger)
		assert.NotNil(t, handler.service.cache)
		assert.NotNil(t, handler.service.repo.db)
	})

	t.Run("plugin system with groups", func(t *testing.T) {
		t.Parallel()

		// Create plugin modules
		plugin1 := godi.NewModule("plugin1",
			godi.AddSingleton(func() testutil.TestHandler {
				return testutil.NewTestHandler("plugin1-handler")
			}, godi.Group("plugins")),
		)

		plugin2 := godi.NewModule("plugin2",
			godi.AddSingleton(func() testutil.TestHandler {
				return testutil.NewTestHandler("plugin2-handler")
			}, godi.Group("plugins")),
		)

		plugin3 := godi.NewModule("plugin3",
			godi.AddSingleton(func() testutil.TestHandler {
				return testutil.NewTestHandler("plugin3-handler")
			}, godi.Group("plugins")),
		)

		// Core module that uses plugins
		type PluginManager struct {
			plugins []testutil.TestHandler
		}

		coreModule := godi.NewModule("core",
			godi.AddSingleton(func(params struct {
				godi.In
				Plugins []testutil.TestHandler `group:"plugins"`
			}) *PluginManager {
				return &PluginManager{plugins: params.Plugins}
			}),
		)

		// Compose all modules
		appModule := godi.NewModule("app",
			plugin1,
			plugin2,
			plugin3,
			coreModule,
		)

		provider := godi.NewServiceProvider()
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})
		err := provider.AddModules(appModule)
		require.NoError(t, err)

		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Resolve plugin manager
		manager := testutil.AssertServiceResolvable[*PluginManager](t, provider)
		assert.Len(t, manager.plugins, 3)

		// Verify all plugins are loaded
		handlerNames := make(map[string]bool)
		for _, p := range manager.plugins {
			handlerNames[p.Handle()] = true
		}

		assert.True(t, handlerNames["plugin1-handler"])
		assert.True(t, handlerNames["plugin2-handler"])
		assert.True(t, handlerNames["plugin3-handler"])
	})
}

func TestModule_BuilderFunctions(t *testing.T) {
	t.Run("all builder types", func(t *testing.T) {
		t.Parallel()

		// Test that all ModuleOption functions work
		module := godi.NewModule("all-builders",
			// AddSingleton
			godi.AddSingleton(testutil.NewTestLogger),
			godi.AddSingleton(testutil.NewTestDatabase, godi.Name("primary")),

			// AddScoped
			godi.AddScoped(testutil.NewTestService),
			godi.AddScoped(func() *testutil.TestService {
				return &testutil.TestService{ID: "custom"}
			}, godi.Group("services")),

			// AddDecorator
			godi.AddDecorator(func(logger testutil.TestLogger) testutil.TestLogger {
				return logger // Pass through
			}),
		)

		provider := godi.NewServiceProvider()
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})
		err := provider.AddModules(module)
		require.NoError(t, err)
	})
}
