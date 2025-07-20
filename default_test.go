package godi_test

import (
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/junioryono/godi"
	"github.com/junioryono/godi/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultServiceProvider(t *testing.T) {
	// Save original default to restore after tests
	originalDefault := godi.DefaultServiceProvider()
	t.Cleanup(func() {
		godi.SetDefaultServiceProvider(originalDefault)
	})

	t.Run("initially nil", func(t *testing.T) {
		t.Parallel()

		// Clear default
		godi.SetDefaultServiceProvider(nil)

		provider := godi.DefaultServiceProvider()
		assert.Nil(t, provider)
	})

	t.Run("can set and get default provider", func(t *testing.T) {
		t.Parallel()

		// Create a provider
		testProvider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(testutil.NewTestLogger).
			BuildProvider()

		// Set as default
		godi.SetDefaultServiceProvider(testProvider)

		// Get default
		defaultProvider := godi.DefaultServiceProvider()
		assert.NotNil(t, defaultProvider)
		assert.Equal(t, testProvider, defaultProvider)

		// Should be able to resolve services
		logger := testutil.AssertServiceResolvable[testutil.TestLogger](t, defaultProvider)
		assert.NotNil(t, logger)
	})

	t.Run("can clear default provider", func(t *testing.T) {
		t.Parallel()

		// Set a provider
		testProvider := testutil.NewServiceCollectionBuilder(t).BuildProvider()
		godi.SetDefaultServiceProvider(testProvider)

		// Verify it's set
		assert.NotNil(t, godi.DefaultServiceProvider())

		// Clear it
		godi.SetDefaultServiceProvider(nil)

		// Verify it's cleared
		assert.Nil(t, godi.DefaultServiceProvider())
	})

	t.Run("setting disposed provider", func(t *testing.T) {
		t.Parallel()

		// Create and dispose a provider
		testProvider := testutil.NewServiceCollectionBuilder(t).BuildProvider()
		require.NoError(t, testProvider.Close())

		// Set as default (this is allowed)
		godi.SetDefaultServiceProvider(testProvider)

		// Get default
		defaultProvider := godi.DefaultServiceProvider()
		assert.NotNil(t, defaultProvider)
		assert.True(t, defaultProvider.IsDisposed())

		// Operations should fail
		_, err := defaultProvider.Resolve(reflect.TypeOf((*interface{})(nil)).Elem())
		assert.ErrorIs(t, err, godi.ErrProviderDisposed)
	})

	t.Run("concurrent access is safe", func(t *testing.T) {
		t.Parallel()

		const goroutines = 100

		// Create multiple providers
		providers := make([]godi.ServiceProvider, 10)
		for i := 0; i < len(providers); i++ {
			providers[i] = testutil.NewServiceCollectionBuilder(t).
				WithSingleton(func(idx int) func() string {
					return func() string { return fmt.Sprintf("provider-%d", idx) }
				}(i)).
				BuildProvider()
		}

		// Concurrently set and get default
		done := make(chan bool)
		for i := 0; i < goroutines; i++ {
			go func(idx int) {
				defer func() { done <- true }()

				// Randomly set or get
				if idx%2 == 0 {
					providerIdx := idx % len(providers)
					godi.SetDefaultServiceProvider(providers[providerIdx])
				} else {
					provider := godi.DefaultServiceProvider()
					// Provider might be nil or any of the test providers
					_ = provider
				}
			}(i)
		}

		// Wait for all goroutines
		for i := 0; i < goroutines; i++ {
			<-done
		}

		// Should not panic or deadlock
	})
}

func TestDefaultServiceProvider_RealWorldUsage(t *testing.T) {
	// Save and restore original default
	originalDefault := godi.DefaultServiceProvider()
	t.Cleanup(func() {
		godi.SetDefaultServiceProvider(originalDefault)
	})

	t.Run("package-level initialization pattern", func(t *testing.T) {
		t.Parallel()

		// Simulate package initialization
		var initOnce sync.Once
		initializeApp := func() {
			initOnce.Do(func() {
				collection := godi.NewServiceCollection()
				_ = collection.AddSingleton(testutil.NewTestLogger)
				_ = collection.AddSingleton(testutil.NewTestDatabase)
				_ = collection.AddScoped(testutil.NewTestService)

				provider, err := collection.BuildServiceProvider()
				if err != nil {
					panic(err)
				}

				godi.SetDefaultServiceProvider(provider)
			})
		}

		// Initialize
		initializeApp()

		// Use default provider throughout app
		provider := godi.DefaultServiceProvider()
		require.NotNil(t, provider)

		logger := testutil.AssertServiceResolvable[testutil.TestLogger](t, provider)
		assert.NotNil(t, logger)
	})

	t.Run("testing with default provider", func(t *testing.T) {
		t.Parallel()

		// In tests, you might want to set a mock provider
		mockProvider := testutil.NewServiceCollectionBuilder(t).
			WithSingleton(func() testutil.TestLogger {
				logger := testutil.NewTestLogger()
				logger.Log("mock logger")
				return logger
			}).
			BuildProvider()

		// Set for test
		godi.SetDefaultServiceProvider(mockProvider)
		t.Cleanup(func() {
			godi.SetDefaultServiceProvider(nil)
		})

		// Code under test would use the default
		provider := godi.DefaultServiceProvider()
		logger := testutil.AssertServiceResolvable[testutil.TestLogger](t, provider)

		logs := logger.GetLogs()
		assert.Contains(t, logs, "mock logger")
	})
}

// Example of how default provider might be used in practice
func ExampleDefaultServiceProvider() {
	// Initialize application services
	collection := godi.NewServiceCollection()
	_ = collection.AddSingleton(func() string { return "example service" })

	provider, _ := collection.BuildServiceProvider()
	godi.SetDefaultServiceProvider(provider)

	// Later in the application...
	if defaultProvider := godi.DefaultServiceProvider(); defaultProvider != nil {
		// Use the default provider
		_ = defaultProvider
	}
}
