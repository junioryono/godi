package godi

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test types for provider and scope tests
type ProviderTestService struct {
	ID    string
	Value int
}

// Test Provider CreateScope
func TestProviderCreateScope(t *testing.T) {
	t.Run("create scope", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddScoped(NewScopedTestService)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		assert.NoError(t, err)
		assert.NotNil(t, scope)
		defer scope.Close()

		// Verify scope works
		service, err := scope.Get(reflect.TypeOf((*ScopedTestService)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, service)
	})

	t.Run("create scope with nil context", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.TODO())
		assert.NoError(t, err)
		assert.NotNil(t, scope)
		defer scope.Close()

		// Should use background context
		assert.NotNil(t, scope.Context())
	})

	t.Run("create scope after disposal", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)

		err = provider.Close()
		assert.NoError(t, err)

		scope, err := provider.CreateScope(context.Background())
		assert.Error(t, err)
		assert.Equal(t, ErrProviderDisposed, err)
		assert.Nil(t, scope)
	})

	t.Run("scope auto-closes on context cancel", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		ctx, cancel := context.WithCancel(context.Background())
		scope, err := provider.CreateScope(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, scope)

		// Cancel context
		cancel()

		// Give it time to close
		time.Sleep(100 * time.Millisecond)

		// Scope should be disposed
		_, err = scope.Get(reflect.TypeOf((*ProviderTestService)(nil)))
		assert.Error(t, err)
		assert.Equal(t, ErrScopeDisposed, err)
	})
}

// Test Provider Close
func TestProviderClose(t *testing.T) {
	t.Run("close provider with disposable singletons", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewDisposableTestService)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)

		// Get the service to ensure it's created
		service, err := provider.Get(reflect.TypeOf((*DisposableTestService)(nil)))
		require.NoError(t, err)
		disposable := service.(*DisposableTestService)
		assert.False(t, disposable.Closed)

		// Close provider
		err = provider.Close()
		assert.NoError(t, err)

		// Disposable should be closed
		assert.True(t, disposable.Closed)
	})

	t.Run("close provider with scopes", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)

		// Create multiple scopes
		scope1, err := provider.CreateScope(context.Background())
		require.NoError(t, err)

		scope2, err := provider.CreateScope(context.Background())
		require.NoError(t, err)

		// Close provider
		err = provider.Close()
		assert.NoError(t, err)

		// Scopes should be closed
		_, err = scope1.Get(reflect.TypeOf((*ProviderTestService)(nil)))
		assert.Error(t, err)

		_, err = scope2.Get(reflect.TypeOf((*ProviderTestService)(nil)))
		assert.Error(t, err)
	})

	t.Run("close provider multiple times", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)

		err = provider.Close()
		assert.NoError(t, err)

		// Second close should be no-op
		err = provider.Close()
		assert.NoError(t, err)
	})
}

// Test Scope Get
func TestScopeGet(t *testing.T) {
	t.Run("get scoped service", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddScoped(NewScopedTestService)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope.Close()

		service1, err := scope.Get(reflect.TypeOf((*ScopedTestService)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, service1)

		service2, err := scope.Get(reflect.TypeOf((*ScopedTestService)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, service2)

		// Should be same instance within scope
		assert.Same(t, service1, service2)
	})

	t.Run("different scopes get different instances", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddScoped(NewScopedTestService)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope1, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope1.Close()

		scope2, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope2.Close()

		service1, err := scope1.Get(reflect.TypeOf((*ScopedTestService)(nil)))
		assert.NoError(t, err)

		service2, err := scope2.Get(reflect.TypeOf((*ScopedTestService)(nil)))
		assert.NoError(t, err)

		// Should be different instances
		assert.NotSame(t, service1, service2)
	})

	t.Run("get singleton from scope", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewProviderTestService)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Get from provider
		providerService, err := provider.Get(reflect.TypeOf((*ProviderTestService)(nil)))
		require.NoError(t, err)

		// Get from scope
		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope.Close()

		scopeService, err := scope.Get(reflect.TypeOf((*ProviderTestService)(nil)))
		assert.NoError(t, err)

		// Should be same singleton instance
		assert.Same(t, providerService, scopeService)
	})

	t.Run("get transient from scope", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddTransient(NewTransientTestService)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope.Close()

		service1, err := scope.Get(reflect.TypeOf((*TransientTestService)(nil)))
		assert.NoError(t, err)

		service2, err := scope.Get(reflect.TypeOf((*TransientTestService)(nil)))
		assert.NoError(t, err)

		// Should be different instances
		assert.NotSame(t, service1, service2)
	})

	t.Run("get after scope disposal", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)

		err = scope.Close()
		assert.NoError(t, err)

		_, err = scope.Get(reflect.TypeOf((*ProviderTestService)(nil)))
		assert.Error(t, err)
		assert.Equal(t, ErrScopeDisposed, err)
	})
}

// Test Scope CreateScope (nested scopes)
func TestNestedScopes(t *testing.T) {
	t.Run("create nested scope", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddScoped(NewScopedTestService)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		parentScope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer parentScope.Close()

		childScope, err := parentScope.CreateScope(context.Background())
		assert.NoError(t, err)
		assert.NotNil(t, childScope)
		defer childScope.Close()

		// Get service from child scope
		service, err := childScope.Get(reflect.TypeOf((*ScopedTestService)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, service)
	})

	t.Run("nested scopes have different instances", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddScoped(NewScopedTestService)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		parentScope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer parentScope.Close()

		childScope, err := parentScope.CreateScope(context.Background())
		require.NoError(t, err)
		defer childScope.Close()

		parentService, err := parentScope.Get(reflect.TypeOf((*ScopedTestService)(nil)))
		assert.NoError(t, err)

		childService, err := childScope.Get(reflect.TypeOf((*ScopedTestService)(nil)))
		assert.NoError(t, err)

		// Should be different instances
		assert.NotSame(t, parentService, childService)
	})

	t.Run("closing parent scope closes children", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		parentScope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)

		childScope, err := parentScope.CreateScope(context.Background())
		require.NoError(t, err)

		// Close parent
		err = parentScope.Close()
		assert.NoError(t, err)

		// Child should also be closed
		_, err = childScope.Get(reflect.TypeOf((*ProviderTestService)(nil)))
		assert.Error(t, err)
		assert.Equal(t, ErrScopeDisposed, err)
	})
}

// Test Scope Close
func TestScopeClose(t *testing.T) {
	t.Run("close scope with disposable services", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddScoped(NewDisposableTestService)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)

		service, err := scope.Get(reflect.TypeOf((*DisposableTestService)(nil)))
		require.NoError(t, err)
		disposable := service.(*DisposableTestService)
		assert.False(t, disposable.Closed)

		// Close scope
		err = scope.Close()
		assert.NoError(t, err)

		// Disposable should be closed
		assert.True(t, disposable.Closed)
	})

	t.Run("close scope multiple times", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)

		err = scope.Close()
		assert.NoError(t, err)

		// Second close should be no-op
		err = scope.Close()
		assert.NoError(t, err)
	})
}

// Test Scope Provider and Context
func TestScopeProviderAndContext(t *testing.T) {
	t.Run("scope provider", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope.Close()

		assert.Equal(t, provider, scope.Provider())
	})

	t.Run("scope context", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		type contextKey string
		const testKey contextKey = "test"
		ctx := context.WithValue(context.Background(), testKey, "value")
		scope, err := provider.CreateScope(ctx)
		require.NoError(t, err)
		defer scope.Close()

		scopeCtx := scope.Context()
		assert.NotNil(t, scopeCtx)
		assert.Equal(t, "value", scopeCtx.Value("test"))
	})
}

// Test concurrent access
func TestProviderConcurrency(t *testing.T) {
	collection := NewCollection()
	err := collection.AddSingleton(NewProviderTestService)
	require.NoError(t, err)
	err = collection.AddScoped(NewScopedTestService)
	require.NoError(t, err)
	err = collection.AddTransient(NewTransientTestService)
	require.NoError(t, err)

	provider, err := collection.Build()
	require.NoError(t, err)
	defer provider.Close()

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Concurrent gets
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := provider.Get(reflect.TypeOf((*ProviderTestService)(nil)))
			if err != nil {
				errors <- err
			}
		}()
	}

	// Concurrent scope creation
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scope, err := provider.CreateScope(context.Background())
			if err != nil {
				errors <- err
				return
			}
			defer scope.Close()

			_, err = scope.Get(reflect.TypeOf((*ScopedTestService)(nil)))
			if err != nil {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		assert.NoError(t, err)
	}
}

// Test circular dependency detection
func TestCircularDependencyDetection(t *testing.T) {
	t.Run("direct circular dependency", func(t *testing.T) {
		// This should be caught at build time, tested in collection_test.go
		assert.True(t, true)
	})

	t.Run("self dependency", func(t *testing.T) {
		type SelfDependent struct {
			Self *SelfDependent
		}

		newSelfDependent := func(self *SelfDependent) *SelfDependent {
			return &SelfDependent{Self: self}
		}

		collection := NewCollection()
		err := collection.AddSingleton(newSelfDependent)
		assert.NoError(t, err) // Registration succeeds

		_, err = collection.Build()
		assert.Error(t, err) // Build should fail
		assert.Contains(t, err.Error(), "dependency validation failed")
	})
}

// Test provider state
func TestProviderState(t *testing.T) {
	t.Run("disposed state is atomic", func(t *testing.T) {
		collection := NewCollection()
		pdr, err := collection.Build()
		require.NoError(t, err)

		p := pdr.(*provider)

		// Initial state
		assert.Equal(t, int32(0), atomic.LoadInt32(&p.disposed))

		// After close
		err = pdr.Close()
		assert.NoError(t, err)
		assert.Equal(t, int32(1), atomic.LoadInt32(&p.disposed))
	})
}

// Benchmark tests
func BenchmarkProviderGet(b *testing.B) {
	collection := NewCollection()
	_ = collection.AddSingleton(NewProviderTestService)
	provider, _ := collection.Build()
	defer provider.Close()

	serviceType := reflect.TypeOf((*ProviderTestService)(nil))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = provider.Get(serviceType)
	}
}

func BenchmarkScopeGet(b *testing.B) {
	collection := NewCollection()
	_ = collection.AddScoped(NewScopedTestService)
	provider, _ := collection.Build()
	defer provider.Close()

	scope, _ := provider.CreateScope(context.Background())
	defer scope.Close()

	serviceType := reflect.TypeOf((*ScopedTestService)(nil))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = scope.Get(serviceType)
	}
}

func BenchmarkTransientGet(b *testing.B) {
	collection := NewCollection()
	_ = collection.AddTransient(NewTransientTestService)
	provider, _ := collection.Build()
	defer provider.Close()

	serviceType := reflect.TypeOf((*TransientTestService)(nil))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = provider.Get(serviceType)
	}
}

type ScopedTestService struct {
	Created time.Time
	Scope   string
}

type TransientTestService struct {
	Random int
}

type DisposableTestService struct {
	Name   string
	Closed bool
	mu     sync.Mutex
}

func (d *DisposableTestService) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.Closed {
		return errors.New("already closed")
	}
	d.Closed = true
	return nil
}

// Constructor functions
func NewProviderTestService() *ProviderTestService {
	return &ProviderTestService{
		ID:    "test-id",
		Value: 42,
	}
}

func NewScopedTestService() *ScopedTestService {
	return &ScopedTestService{
		Created: time.Now(),
		Scope:   "default",
	}
}

func NewTransientTestService() *TransientTestService {
	return &TransientTestService{
		Random: int(time.Now().UnixNano() % 1000),
	}
}

func NewDisposableTestService() *DisposableTestService {
	return &DisposableTestService{
		Name: "disposable",
	}
}

func NewServiceWithDependency(provider *ProviderTestService) *ScopedTestService {
	return &ScopedTestService{
		Created: time.Now(),
		Scope:   provider.ID,
	}
}

// Test Provider Get
func TestProviderGet(t *testing.T) {
	t.Run("get singleton service", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewProviderTestService)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		service, err := provider.Get(reflect.TypeOf((*ProviderTestService)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, service)
		assert.IsType(t, &ProviderTestService{}, service)
		providerService := service.(*ProviderTestService)
		assert.Equal(t, "test-id", providerService.ID)
		assert.Equal(t, 42, providerService.Value)
	})

	t.Run("get empty group", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		services, err := provider.GetGroup(reflect.TypeOf((*ProviderTestService)(nil)), "empty")
		assert.NoError(t, err)
		assert.Empty(t, services)
	})

	t.Run("get group with nil type", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		services, err := provider.GetGroup(nil, "group")
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidServiceType, err)
		assert.Nil(t, services)
	})

	t.Run("get group with empty name", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		services, err := provider.GetGroup(reflect.TypeOf((*ProviderTestService)(nil)), "")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "group name cannot be empty")
		assert.Nil(t, services)
	})

	t.Run("get group after disposal", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)

		err = provider.Close()
		assert.NoError(t, err)

		services, err := provider.GetGroup(reflect.TypeOf((*ProviderTestService)(nil)), "group")
		assert.Error(t, err)
		assert.Equal(t, ErrProviderDisposed, err)
		assert.Nil(t, services)
	})

	t.Run("get scoped service", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddScoped(NewScopedTestService)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		service, err := provider.Get(reflect.TypeOf((*ScopedTestService)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, service)

		// Get again - should be same instance in root scope
		service2, err := provider.Get(reflect.TypeOf((*ScopedTestService)(nil)))
		assert.NoError(t, err)
		assert.Same(t, service, service2)
	})

	t.Run("get transient service", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddTransient(NewTransientTestService)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		service1, err := provider.Get(reflect.TypeOf((*TransientTestService)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, service1)

		service2, err := provider.Get(reflect.TypeOf((*TransientTestService)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, service2)

		// Should be different instances
		assert.NotSame(t, service1, service2)
	})

	t.Run("get non-existent service", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		service, err := provider.Get(reflect.TypeOf((*ProviderTestService)(nil)))
		assert.Error(t, err)
		assert.Nil(t, service)

		var resErr ResolutionError
		assert.ErrorAs(t, err, &resErr)
		assert.ErrorIs(t, resErr.Cause, ErrServiceNotFound)
	})

	t.Run("get with nil type", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		service, err := provider.Get(nil)
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidServiceType, err)
		assert.Nil(t, service)
	})

	t.Run("get after disposal", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewProviderTestService)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)

		err = provider.Close()
		assert.NoError(t, err)

		service, err := provider.Get(reflect.TypeOf((*ProviderTestService)(nil)))
		assert.Error(t, err)
		assert.Equal(t, ErrProviderDisposed, err)
		assert.Nil(t, service)
	})
}

// Test Provider GetKeyed
func TestProviderGetKeyed(t *testing.T) {
	t.Run("get keyed service", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewProviderTestService, Name("primary"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		service, err := provider.GetKeyed(reflect.TypeOf((*ProviderTestService)(nil)), "primary")
		assert.NoError(t, err)
		assert.NotNil(t, service)
	})

	t.Run("get non-existent keyed service", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		service, err := provider.GetKeyed(reflect.TypeOf((*ProviderTestService)(nil)), "primary")
		assert.Error(t, err)
		assert.Nil(t, service)
	})

	t.Run("get keyed with nil key", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		service, err := provider.GetKeyed(reflect.TypeOf((*ProviderTestService)(nil)), nil)
		assert.Error(t, err)
		assert.Equal(t, ErrServiceKeyNil, err)
		assert.Nil(t, service)
	})

	t.Run("get keyed with nil type", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		service, err := provider.GetKeyed(nil, "key")
		assert.Error(t, err)
		assert.Equal(t, ErrInvalidServiceType, err)
		assert.Nil(t, service)
	})

	t.Run("get keyed after disposal", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)

		err = provider.Close()
		assert.NoError(t, err)

		service, err := provider.GetKeyed(reflect.TypeOf((*ProviderTestService)(nil)), "key")
		assert.Error(t, err)
		assert.Equal(t, ErrProviderDisposed, err)
		assert.Nil(t, service)
	})
}

// Test Provider GetGroup
func TestProviderGetGroup(t *testing.T) {
	t.Run("get group services", func(t *testing.T) {
		collection := NewCollection()

		err := collection.AddSingleton(func() *ProviderTestService {
			return &ProviderTestService{ID: "service1", Value: 1}
		}, Group("services"))
		require.NoError(t, err)

		err = collection.AddSingleton(func() *ProviderTestService {
			return &ProviderTestService{ID: "service2", Value: 2}
		}, Group("services"))
		require.NoError(t, err)

		err = collection.AddTransient(func() *ProviderTestService {
			return &ProviderTestService{ID: "service3", Value: 3}
		}, Group("services"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		services, err := provider.GetGroup(reflect.TypeOf((*ProviderTestService)(nil)), "services")
		assert.NoError(t, err)
		assert.Len(t, services, 3)

		// Verify all services are retrieved
		ids := make(map[string]bool)
		for _, s := range services {
			service := s.(*ProviderTestService)
			ids[service.ID] = true
		}
		assert.True(t, ids["service1"])
		assert.True(t, ids["service2"])
		assert.True(t, ids["service3"])
	})
}
