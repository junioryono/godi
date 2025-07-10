package godi_test

import (
	"sync"
	"testing"

	"github.com/junioryono/godi"
)

func TestDefaultServiceProvider(t *testing.T) {
	t.Run("default provider lifecycle", func(t *testing.T) {
		// Ensure clean state
		godi.SetDefaultServiceProvider(nil)

		if godi.DefaultServiceProvider() != nil {
			t.Error("expected nil default provider initially")
		}

		// Create and set provider
		collection := godi.NewServiceCollection()
		collection.AddSingleton(func() string { return "default" })

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		godi.SetDefaultServiceProvider(provider)

		if godi.DefaultServiceProvider() != provider {
			t.Error("default provider not set correctly")
		}

		// Clear default
		godi.SetDefaultServiceProvider(nil)

		if godi.DefaultServiceProvider() != nil {
			t.Error("default provider not cleared")
		}
	})

	t.Run("concurrent access to default provider", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, _ := collection.BuildServiceProvider()
		defer provider.Close()

		const goroutines = 100
		var wg sync.WaitGroup
		wg.Add(goroutines * 2)

		// Half set, half get
		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()
				godi.SetDefaultServiceProvider(provider)
			}()

			go func() {
				defer wg.Done()
				_ = godi.DefaultServiceProvider()
			}()
		}

		wg.Wait()

		// Cleanup
		godi.SetDefaultServiceProvider(nil)
	})
}
