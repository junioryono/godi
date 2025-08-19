package godi

import (
	"context"
	"errors"
	"fmt"
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
	t.Run("close scope with disposable singleton service", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewDisposableTestService)
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

		// Disposable should not be closed because it's a singleton
		assert.False(t, disposable.Closed)
	})

	t.Run("close scope with disposable scoped service", func(t *testing.T) {
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

	t.Run("close scope with disposable transient service", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddTransient(NewDisposableTestService)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)

		service1, err := scope.Get(reflect.TypeOf((*DisposableTestService)(nil)))
		require.NoError(t, err)
		disposable1 := service1.(*DisposableTestService)
		assert.False(t, disposable1.Closed)

		service2, err := scope.Get(reflect.TypeOf((*DisposableTestService)(nil)))
		require.NoError(t, err)
		assert.NotSame(t, service1, service2)
		disposable2 := service2.(*DisposableTestService)

		// Close scope
		err = scope.Close()
		assert.NoError(t, err)

		// Disposable should be closed
		assert.True(t, disposable1.Closed)
		assert.True(t, disposable2.Closed)
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
		assert.Equal(t, "value", scopeCtx.Value(testKey))
	})

	t.Run("scope ID", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope.Close()

		// ID should be non-empty
		id := scope.ID()
		assert.NotEmpty(t, id)

		// ID should remain constant
		id2 := scope.ID()
		assert.Equal(t, id, id2)
	})
}

// Test Scope GetKeyed
func TestScopeGetKeyed(t *testing.T) {
	t.Run("get keyed service", func(t *testing.T) {
		collection := NewCollection()

		// Add keyed services
		err := collection.AddScoped(func() *ScopedTestService {
			return &ScopedTestService{Scope: "primary"}
		}, Name("primary"))
		require.NoError(t, err)

		err = collection.AddScoped(func() *ScopedTestService {
			return &ScopedTestService{Scope: "secondary"}
		}, Name("secondary"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope.Close()

		// Get primary
		serviceType := reflect.TypeOf((*ScopedTestService)(nil))
		primary, err := scope.GetKeyed(serviceType, "primary")
		require.NoError(t, err)
		assert.NotNil(t, primary)
		assert.Equal(t, "primary", primary.(*ScopedTestService).Scope)

		// Get secondary
		secondary, err := scope.GetKeyed(serviceType, "secondary")
		require.NoError(t, err)
		assert.NotNil(t, secondary)
		assert.Equal(t, "secondary", secondary.(*ScopedTestService).Scope)

		// Same key should return same instance
		primary2, err := scope.GetKeyed(serviceType, "primary")
		require.NoError(t, err)
		assert.Same(t, primary, primary2)
	})

	t.Run("keyed service not found", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope.Close()

		serviceType := reflect.TypeOf((*ScopedTestService)(nil))
		_, err = scope.GetKeyed(serviceType, "nonexistent")
		assert.Error(t, err)
	})

	t.Run("nil key", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope.Close()

		serviceType := reflect.TypeOf((*ScopedTestService)(nil))
		_, err = scope.GetKeyed(serviceType, nil)
		assert.Error(t, err)
		assert.Equal(t, ErrServiceKeyNil, err)
	})

	t.Run("nil type", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope.Close()

		_, err = scope.GetKeyed(nil, "key")
		assert.Error(t, err)
		assert.Equal(t, ErrServiceTypeNil, err)
	})
}

// Test Scope GetGroup
func TestScopeGetGroup(t *testing.T) {
	t.Run("get group services", func(t *testing.T) {
		collection := NewCollection()

		// Add services to a group
		err := collection.AddScoped(func() *ScopedTestService {
			return &ScopedTestService{Scope: "handler1"}
		}, Group("handlers"))
		require.NoError(t, err)

		err = collection.AddScoped(func() *ScopedTestService {
			return &ScopedTestService{Scope: "handler2"}
		}, Group("handlers"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope.Close()

		serviceType := reflect.TypeOf((*ScopedTestService)(nil))
		services, err := scope.GetGroup(serviceType, "handlers")
		require.NoError(t, err)
		assert.Len(t, services, 2)

		// Verify both services are present
		names := make([]string, 0, 2)
		for _, svc := range services {
			names = append(names, svc.(*ScopedTestService).Scope)
		}
		assert.Contains(t, names, "handler1")
		assert.Contains(t, names, "handler2")

		// Getting the same group again should return the same instances
		services2, err := scope.GetGroup(serviceType, "handlers")
		require.NoError(t, err)
		assert.Len(t, services2, 2)
		for i := range services {
			assert.Same(t, services[i], services2[i])
		}
	})

	t.Run("empty group", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope.Close()

		serviceType := reflect.TypeOf((*ScopedTestService)(nil))
		services, err := scope.GetGroup(serviceType, "nonexistent")
		require.NoError(t, err)
		assert.Empty(t, services)
	})

	t.Run("empty group name", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope.Close()

		serviceType := reflect.TypeOf((*ScopedTestService)(nil))
		_, err = scope.GetGroup(serviceType, "")
		assert.Error(t, err)
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
	errs := make(chan error, 100)

	// Concurrent gets
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := provider.Get(reflect.TypeOf((*ProviderTestService)(nil)))
			if err != nil {
				errs <- err
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
				errs <- err
				return
			}
			defer scope.Close()

			_, err = scope.Get(reflect.TypeOf((*ScopedTestService)(nil)))
			if err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
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
		assert.Contains(t, err.Error(), "circular dependency detected")
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
		assert.Equal(t, ErrServiceTypeNil, err)
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

		var resErr *ResolutionError
		assert.ErrorAs(t, err, &resErr)
		assert.ErrorIs(t, err, ErrServiceNotFound)
	})

	t.Run("get with nil type", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		service, err := provider.Get(nil)
		assert.Error(t, err)
		assert.Equal(t, ErrServiceTypeNil, err)
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
		assert.Equal(t, ErrServiceTypeNil, err)
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

// Test primitive type registration
func TestPrimitiveTypes(t *testing.T) {
	t.Run("register int value", func(t *testing.T) {
		collection := NewCollection()

		// Register an int value directly
		err := collection.AddSingleton(42, Name("answer"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Retrieve the int value
		value, err := provider.GetKeyed(reflect.TypeOf(0), "answer")
		assert.NoError(t, err)
		assert.Equal(t, 42, value)
	})

	t.Run("register string value", func(t *testing.T) {
		collection := NewCollection()

		// Register a string value directly
		err := collection.AddSingleton("hello world", Name("greeting"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Retrieve the string value
		value, err := provider.GetKeyed(reflect.TypeOf(""), "greeting")
		assert.NoError(t, err)
		assert.Equal(t, "hello world", value)
	})

	t.Run("register bool value", func(t *testing.T) {
		collection := NewCollection()

		// Register a bool value directly
		err := collection.AddSingleton(true, Name("enabled"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Retrieve the bool value
		value, err := provider.GetKeyed(reflect.TypeOf(false), "enabled")
		assert.NoError(t, err)
		assert.Equal(t, true, value)
	})

	t.Run("register float64 value", func(t *testing.T) {
		collection := NewCollection()

		// Register a float64 value directly
		err := collection.AddSingleton(3.14159, Name("pi"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Retrieve the float64 value
		value, err := provider.GetKeyed(reflect.TypeOf(0.0), "pi")
		assert.NoError(t, err)
		assert.Equal(t, 3.14159, value)
	})

	t.Run("register slice value", func(t *testing.T) {
		collection := NewCollection()

		// Register a slice value directly
		numbers := []int{1, 2, 3, 4, 5}
		err := collection.AddSingleton(numbers, Name("numbers"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Retrieve the slice value
		value, err := provider.GetKeyed(reflect.TypeOf([]int{}), "numbers")
		assert.NoError(t, err)
		assert.Equal(t, numbers, value)
	})

	t.Run("register map value", func(t *testing.T) {
		collection := NewCollection()

		// Register a map value directly
		config := map[string]string{
			"host": "localhost",
			"port": "8080",
		}
		err := collection.AddSingleton(config, Name("config"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Retrieve the map value
		value, err := provider.GetKeyed(reflect.TypeOf(map[string]string{}), "config")
		assert.NoError(t, err)
		assert.Equal(t, config, value)
	})

	t.Run("register channel", func(t *testing.T) {
		collection := NewCollection()

		// Register a channel directly
		ch := make(chan int, 10)
		err := collection.AddSingleton(ch, Name("events"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Retrieve the channel
		value, err := provider.GetKeyed(reflect.TypeOf(make(chan int)), "events")
		assert.NoError(t, err)
		assert.Equal(t, ch, value) // Should be the same channel
	})

	t.Run("register complex numbers", func(t *testing.T) {
		collection := NewCollection()

		// Register complex numbers
		c64 := complex64(1 + 2i)
		c128 := complex128(3 + 4i)

		err := collection.AddSingleton(c64, Name("c64"))
		require.NoError(t, err)

		err = collection.AddSingleton(c128, Name("c128"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Retrieve complex numbers
		val64, err := provider.GetKeyed(reflect.TypeOf(complex64(0)), "c64")
		assert.NoError(t, err)
		assert.Equal(t, c64, val64)

		val128, err := provider.GetKeyed(reflect.TypeOf(complex128(0)), "c128")
		assert.NoError(t, err)
		assert.Equal(t, c128, val128)
	})
}

// Test instance registration
func TestInstanceRegistration(t *testing.T) {
	type Config struct {
		Host string
		Port int
	}

	type Database struct {
		ConnectionString string
		MaxConnections   int
	}

	t.Run("register struct instance", func(t *testing.T) {
		collection := NewCollection()

		// Create an instance
		config := &Config{
			Host: "localhost",
			Port: 8080,
		}

		// Register the instance directly
		err := collection.AddSingleton(config)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Retrieve the instance
		value, err := provider.Get(reflect.TypeOf((*Config)(nil)))
		assert.NoError(t, err)
		assert.Same(t, config, value) // Should be the exact same instance
	})

	t.Run("register multiple instances with keys", func(t *testing.T) {
		collection := NewCollection()

		// Create instances
		primaryDB := &Database{
			ConnectionString: "primary.db",
			MaxConnections:   100,
		}

		secondaryDB := &Database{
			ConnectionString: "secondary.db",
			MaxConnections:   50,
		}

		// Register instances with keys
		err := collection.AddSingleton(primaryDB, Name("primary"))
		require.NoError(t, err)

		err = collection.AddSingleton(secondaryDB, Name("secondary"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Retrieve instances
		primary, err := provider.GetKeyed(reflect.TypeOf((*Database)(nil)), "primary")
		assert.NoError(t, err)
		assert.Same(t, primaryDB, primary)

		secondary, err := provider.GetKeyed(reflect.TypeOf((*Database)(nil)), "secondary")
		assert.NoError(t, err)
		assert.Same(t, secondaryDB, secondary)
	})

	t.Run("register instance in group", func(t *testing.T) {
		collection := NewCollection()

		// Create instances
		configs := []*Config{
			{Host: "server1", Port: 8080},
			{Host: "server2", Port: 8081},
			{Host: "server3", Port: 8082},
		}

		// Register instances in a group
		for _, cfg := range configs {
			err := collection.AddSingleton(cfg, Group("servers"))
			require.NoError(t, err)
		}

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Retrieve all instances from group
		servers, err := provider.GetGroup(reflect.TypeOf((*Config)(nil)), "servers")
		assert.NoError(t, err)
		assert.Len(t, servers, 3)

		// Verify they are the same instances
		for i, server := range servers {
			assert.Same(t, configs[i], server)
		}
	})

	t.Run("register instance with interface", func(t *testing.T) {
		collection := NewCollection()

		// Create an instance that implements an interface
		service := &TestService{Name: "instance-service"}

		// Register as interface
		err := collection.AddSingleton(service, As(new(TestInterface)))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Retrieve via interface
		value, err := provider.Get(reflect.TypeOf((*TestInterface)(nil)).Elem())
		assert.NoError(t, err)
		assert.Same(t, service, value)
	})

	t.Run("mix instances and constructors", func(t *testing.T) {
		collection := NewCollection()

		// Register an instance
		config := &Config{Host: "localhost", Port: 8080}
		err := collection.AddSingleton(config)
		require.NoError(t, err)

		// Register a constructor that depends on the instance
		newDatabase := func(cfg *Config) *Database {
			return &Database{
				ConnectionString: fmt.Sprintf("%s:%d", cfg.Host, cfg.Port),
				MaxConnections:   10,
			}
		}
		err = collection.AddSingleton(newDatabase)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Retrieve the database
		db, err := provider.Get(reflect.TypeOf((*Database)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, db)

		database := db.(*Database)
		assert.Equal(t, "localhost:8080", database.ConnectionString)
	})

	t.Run("scoped instance behavior", func(t *testing.T) {
		collection := NewCollection()

		// Register an instance as scoped
		config := &Config{Host: "localhost", Port: 8080}
		err := collection.AddScoped(config)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Create two scopes
		scope1, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope1.Close()

		scope2, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope2.Close()

		// Get from both scopes
		val1, err := scope1.Get(reflect.TypeOf((*Config)(nil)))
		assert.NoError(t, err)

		val2, err := scope2.Get(reflect.TypeOf((*Config)(nil)))
		assert.NoError(t, err)

		// Should be the same instance (because we registered an instance, not a constructor)
		assert.Same(t, val1, val2)
		assert.Same(t, config, val1)
	})

	t.Run("transient instance behavior", func(t *testing.T) {
		collection := NewCollection()

		// Register an instance as transient
		config := &Config{Host: "localhost", Port: 8080}
		err := collection.AddTransient(config)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Get multiple times
		val1, err := provider.Get(reflect.TypeOf((*Config)(nil)))
		assert.NoError(t, err)

		val2, err := provider.Get(reflect.TypeOf((*Config)(nil)))
		assert.NoError(t, err)

		// Should be the same instance (because we registered an instance, not a constructor)
		// Note: Even though it's transient, the wrapped constructor always returns the same instance
		assert.Same(t, val1, val2)
		assert.Same(t, config, val1)
	})
}

// Test edge cases
func TestInstanceRegistrationEdgeCases(t *testing.T) {
	t.Run("register nil instance", func(t *testing.T) {
		collection := NewCollection()

		err := collection.AddSingleton(nil)
		assert.Error(t, err)
		var valErr *ValidationError
		assert.ErrorAs(t, err, &valErr)
		assert.ErrorIs(t, valErr.Cause, ErrConstructorNil)
	})

	t.Run("register nil pointer", func(t *testing.T) {
		collection := NewCollection()

		type Config struct {
			Host string
		}

		var config *Config = nil
		err := collection.AddSingleton(config)
		assert.Error(t, err)
		var regErr *RegistrationError
		assert.ErrorAs(t, err, &regErr)
		var valErr *ValidationError
		assert.ErrorAs(t, regErr.Cause, &valErr)
		assert.ErrorIs(t, valErr.Cause, ErrConstructorNil)
	})

	t.Run("register empty string", func(t *testing.T) {
		collection := NewCollection()

		err := collection.AddSingleton("", Name("empty"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		value, err := provider.GetKeyed(reflect.TypeOf(""), "empty")
		assert.NoError(t, err)
		assert.Equal(t, "", value)
	})

	t.Run("register zero values", func(t *testing.T) {
		collection := NewCollection()

		// Register various zero values
		err := collection.AddSingleton(0, Name("zero-int"))
		require.NoError(t, err)

		err = collection.AddSingleton(false, Name("zero-bool"))
		require.NoError(t, err)

		err = collection.AddSingleton(0.0, Name("zero-float"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Verify zero values are preserved
		intVal, err := provider.GetKeyed(reflect.TypeOf(0), "zero-int")
		assert.NoError(t, err)
		assert.Equal(t, 0, intVal)

		boolVal, err := provider.GetKeyed(reflect.TypeOf(false), "zero-bool")
		assert.NoError(t, err)
		assert.Equal(t, false, boolVal)

		floatVal, err := provider.GetKeyed(reflect.TypeOf(0.0), "zero-float")
		assert.NoError(t, err)
		assert.Equal(t, 0.0, floatVal)
	})

	t.Run("register function value as instance", func(t *testing.T) {
		collection := NewCollection()

		// Register a function value (not as constructor, but as a value itself)
		myFunc := func(x int) int { return x * 2 }

		// This will be treated as a constructor since it's a function
		// To register it as a value, we need to wrap it
		wrapperConstructor := func() func(int) int { return myFunc }

		err := collection.AddSingleton(wrapperConstructor, Name("multiplier"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Retrieve the function
		value, err := provider.GetKeyed(reflect.TypeOf((func(int) int)(nil)), "multiplier")
		assert.NoError(t, err)

		fn := value.(func(int) int)
		assert.Equal(t, 10, fn(5))
	})
}

// Test mixing constructors and instances
func TestMixedRegistration(t *testing.T) {
	type Logger struct {
		Level string
	}

	type Config struct {
		Host string
		Port int
	}

	type Service struct {
		Logger *Logger
		Config *Config
	}

	t.Run("constructor depends on instance", func(t *testing.T) {
		collection := NewCollection()

		// Register instances
		logger := &Logger{Level: "debug"}
		config := &Config{Host: "localhost", Port: 8080}

		err := collection.AddSingleton(logger)
		require.NoError(t, err)

		err = collection.AddSingleton(config)
		require.NoError(t, err)

		// Register constructor that depends on instances
		newService := func(l *Logger, c *Config) *Service {
			return &Service{Logger: l, Config: c}
		}

		err = collection.AddSingleton(newService)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Retrieve service
		svc, err := provider.Get(reflect.TypeOf((*Service)(nil)))
		assert.NoError(t, err)

		service := svc.(*Service)
		assert.Same(t, logger, service.Logger)
		assert.Same(t, config, service.Config)
	})

	t.Run("instance depends on constructor", func(t *testing.T) {
		collection := NewCollection()

		// Register constructor first
		newLogger := func() *Logger {
			return &Logger{Level: "info"}
		}
		err := collection.AddSingleton(newLogger)
		require.NoError(t, err)

		// Cannot directly register an instance that depends on constructor
		// But we can register a service that uses the logger
		newConfig := func() *Config {
			return &Config{Host: "example.com", Port: 443}
		}
		err = collection.AddSingleton(newConfig)
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Both should work
		logger, err := provider.Get(reflect.TypeOf((*Logger)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, logger)

		config, err := provider.Get(reflect.TypeOf((*Config)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, config)
	})
}

// Test types for multi-return integration tests
type IntegrationDatabase struct {
	ConnectionString string
}

type IntegrationCache struct {
	Provider string
}

type IntegrationLogger struct {
	Level string
}

type IntegrationUserRepository struct {
	DB    *IntegrationDatabase
	Cache *IntegrationCache
}

type IntegrationUserService struct {
	Repo   *IntegrationUserRepository
	Logger *IntegrationLogger
}

type IntegrationAdminService struct {
	UserService *IntegrationUserService
	Logger      *IntegrationLogger
}

type IntegrationNotificationService struct {
	Logger *IntegrationLogger
}

// Test multi-return with scoped lifetime
func TestMultiReturnScopedLifetime(t *testing.T) {
	// Multi-return constructor
	NewInfrastructure := func() (*IntegrationDatabase, *IntegrationCache, *IntegrationLogger) {
		return &IntegrationDatabase{ConnectionString: "postgres://localhost"},
			&IntegrationCache{Provider: "redis"},
			&IntegrationLogger{Level: "info"}
	}

	collection := NewCollection()
	err := collection.AddScoped(NewInfrastructure)
	require.NoError(t, err)

	provider, err := collection.Build()
	require.NoError(t, err)
	defer provider.Close()

	// Create two scopes
	scope1, err := provider.CreateScope(context.Background())
	require.NoError(t, err)
	defer scope1.Close()

	scope2, err := provider.CreateScope(context.Background())
	require.NoError(t, err)
	defer scope2.Close()

	// Get services from scope1
	db1, err := scope1.Get(reflect.TypeOf((*IntegrationDatabase)(nil)))
	require.NoError(t, err)
	cache1, err := scope1.Get(reflect.TypeOf((*IntegrationCache)(nil)))
	require.NoError(t, err)
	logger1, err := scope1.Get(reflect.TypeOf((*IntegrationLogger)(nil)))
	require.NoError(t, err)

	// Get services from scope2
	db2, err := scope2.Get(reflect.TypeOf((*IntegrationDatabase)(nil)))
	require.NoError(t, err)
	cache2, err := scope2.Get(reflect.TypeOf((*IntegrationCache)(nil)))
	require.NoError(t, err)
	logger2, err := scope2.Get(reflect.TypeOf((*IntegrationLogger)(nil)))
	require.NoError(t, err)

	// Services from different scopes should be different instances
	assert.NotSame(t, db1, db2)
	assert.NotSame(t, cache1, cache2)
	assert.NotSame(t, logger1, logger2)

	// But within same scope, multiple gets should return same instance
	db1Again, err := scope1.Get(reflect.TypeOf((*IntegrationDatabase)(nil)))
	require.NoError(t, err)
	assert.Same(t, db1, db1Again)
}

// Test multi-return with dependencies across scopes
func TestMultiReturnWithDependencies(t *testing.T) {
	NewInfrastructure := func() (*IntegrationDatabase, *IntegrationCache, *IntegrationLogger) {
		return &IntegrationDatabase{ConnectionString: "postgres://localhost"},
			&IntegrationCache{Provider: "redis"},
			&IntegrationLogger{Level: "info"}
	}

	NewServices := func(db *IntegrationDatabase, cache *IntegrationCache, logger *IntegrationLogger) (*IntegrationUserRepository, *IntegrationUserService, *IntegrationAdminService) {
		repo := &IntegrationUserRepository{DB: db, Cache: cache}
		userSvc := &IntegrationUserService{Repo: repo, Logger: logger}
		adminSvc := &IntegrationAdminService{UserService: userSvc, Logger: logger}
		return repo, userSvc, adminSvc
	}

	collection := NewCollection()

	// Register infrastructure (multi-return)
	err := collection.AddSingleton(NewInfrastructure)
	require.NoError(t, err)

	// Register services that depend on infrastructure (also multi-return)
	err = collection.AddScoped(NewServices)
	require.NoError(t, err)

	provider, err := collection.Build()
	require.NoError(t, err)
	defer provider.Close()

	// Create scope for scoped services
	scope, err := provider.CreateScope(context.Background())
	require.NoError(t, err)
	defer scope.Close()

	// Resolve services
	userRepo, err := scope.Get(reflect.TypeOf((*IntegrationUserRepository)(nil)))
	require.NoError(t, err)
	assert.NotNil(t, userRepo)
	repo := userRepo.(*IntegrationUserRepository)
	assert.NotNil(t, repo.DB)
	assert.NotNil(t, repo.Cache)

	userSvc, err := scope.Get(reflect.TypeOf((*IntegrationUserService)(nil)))
	require.NoError(t, err)
	assert.NotNil(t, userSvc)
	svc := userSvc.(*IntegrationUserService)
	assert.Equal(t, repo.DB, svc.Repo.DB)
	assert.Equal(t, repo.Cache, svc.Repo.Cache)

	adminSvc, err := scope.Get(reflect.TypeOf((*IntegrationAdminService)(nil)))
	require.NoError(t, err)
	assert.NotNil(t, adminSvc)
	admin := adminSvc.(*IntegrationAdminService)
	assert.Equal(t, svc.Repo.DB, admin.UserService.Repo.DB)
	assert.Equal(t, svc.Repo.Cache, admin.UserService.Repo.Cache)
	assert.Equal(t, svc.Logger, admin.UserService.Logger)
}

// TestBuiltinServices tests that context, scope, and provider are automatically registered
func TestBuiltinServices(t *testing.T) {
	t.Run("context injection", func(t *testing.T) {
		collection := NewCollection()

		// Add a service that requires context
		type ServiceWithContext struct {
			ctx context.Context
		}

		collection.AddScoped(func(ctx context.Context) *ServiceWithContext {
			return &ServiceWithContext{ctx: ctx}
		})

		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		// Create a scope with custom context
		type contextKey string
		const testKey contextKey = "test"
		customCtx := context.WithValue(context.Background(), testKey, "value")
		scope, err := provider.CreateScope(customCtx)
		assert.NoError(t, err, "CreateScope should not fail")
		defer scope.Close()

		// Resolve the service
		serviceType := reflect.TypeOf((*ServiceWithContext)(nil))
		service, err := scope.Get(serviceType)
		assert.NoError(t, err, "Get should not fail")

		svc := service.(*ServiceWithContext)
		assert.NotNil(t, svc.ctx, "Context should not be nil")
		assert.Equal(t, "value", svc.ctx.Value(testKey), "Context value should match expected")
	})

	t.Run("scope injection", func(t *testing.T) {
		collection := NewCollection()

		// Add a service that requires scope
		type ServiceWithScope struct {
			scope Scope
		}

		collection.AddScoped(func(scope Scope) *ServiceWithScope {
			return &ServiceWithScope{scope: scope}
		})

		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		assert.NoError(t, err, "CreateScope should not fail")
		defer scope.Close()

		// Resolve the service
		serviceType := reflect.TypeOf((*ServiceWithScope)(nil))
		service, err := scope.Get(serviceType)
		assert.NoError(t, err, "Get should not fail")

		svc := service.(*ServiceWithScope)
		assert.NotNil(t, svc.scope, "Scope should not be nil")
		assert.Equal(t, scope.ID(), svc.scope.ID(), "Scope ID should match the created scope ID")
	})

	t.Run("provider injection", func(t *testing.T) {
		collection := NewCollection()

		// Add a service that requires provider
		type ServiceWithProvider struct {
			provider Provider
		}

		collection.AddScoped(func(provider Provider) *ServiceWithProvider {
			return &ServiceWithProvider{provider: provider}
		})

		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		assert.NoError(t, err, "CreateScope should not fail")
		defer scope.Close()

		// Resolve the service
		serviceType := reflect.TypeOf((*ServiceWithProvider)(nil))
		service, err := scope.Get(serviceType)
		assert.NoError(t, err, "Get should not fail")

		svc := service.(*ServiceWithProvider)
		assert.NotNil(t, svc.provider, "Provider should not be nil")
		assert.Equal(t, provider.ID(), svc.provider.ID(), "Provider ID should match the created provider ID")
	})

	t.Run("combined injection with In struct", func(t *testing.T) {
		collection := NewCollection()

		// Add a service that requires all built-in services
		type ServiceParams struct {
			In

			Context  context.Context
			Scope    Scope
			Provider Provider
		}

		type ComplexService struct {
			ctx      context.Context
			scope    Scope
			provider Provider
		}

		collection.AddScoped(func(params ServiceParams) *ComplexService {
			return &ComplexService{
				ctx:      params.Context,
				scope:    params.Scope,
				provider: params.Provider,
			}
		})

		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		type contextKey string
		const testKey contextKey = "test"
		customCtx := context.WithValue(context.Background(), testKey, "value")
		scope, err := provider.CreateScope(customCtx)
		assert.NoError(t, err, "CreateScope should not fail")
		defer scope.Close()

		// Resolve the service
		serviceType := reflect.TypeOf((*ComplexService)(nil))
		service, err := scope.Get(serviceType)
		assert.NoError(t, err, "Get should not fail")

		svc := service.(*ComplexService)
		assert.NotNil(t, svc.ctx, "Context should not be nil")
		assert.Equal(t, "value", svc.ctx.Value(testKey), "Context value should match expected")
		assert.NotNil(t, svc.scope, "Scope should not be nil")
		assert.Equal(t, scope.ID(), svc.scope.ID(), "Scope ID should match the created scope ID")
		assert.NotNil(t, svc.provider, "Provider should not be nil")
		assert.Equal(t, provider.ID(), svc.provider.ID(), "Provider ID should match the created provider ID")
	})

	t.Run("child scope inherits provider", func(t *testing.T) {
		collection := NewCollection()

		type ServiceWithProvider struct {
			provider Provider
		}

		collection.AddScoped(func(provider Provider) *ServiceWithProvider {
			return &ServiceWithProvider{provider: provider}
		})

		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		// Create parent scope
		parentScope, err := provider.CreateScope(context.Background())
		assert.NoError(t, err, "CreateScope should not fail")
		defer parentScope.Close()

		// Create child scope
		childScope, err := parentScope.CreateScope(context.Background())
		assert.NoError(t, err, "CreateScope should not fail")
		defer childScope.Close()

		// Resolve from child scope
		serviceType := reflect.TypeOf((*ServiceWithProvider)(nil))
		service, err := childScope.Get(serviceType)
		assert.NoError(t, err, "Get should not fail")

		svc := service.(*ServiceWithProvider)
		assert.NotNil(t, svc.provider, "Provider should not be nil")
		assert.Equal(t, provider.ID(), svc.provider.ID(), "Provider ID should match the created provider ID")
	})
}

// Benchmark tests
func BenchmarkInstanceRegistration(b *testing.B) {
	type Service struct {
		Name string
	}

	b.Run("AddSingleton with instance", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			collection := NewCollection()
			instance := &Service{Name: "bench"}
			_ = collection.AddSingleton(instance)
		}
	})

	b.Run("AddSingleton with constructor", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			collection := NewCollection()
			constructor := func() *Service { return &Service{Name: "bench"} }
			_ = collection.AddSingleton(constructor)
		}
	})

	b.Run("AddSingleton with primitive", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			collection := NewCollection()
			_ = collection.AddSingleton(42, Name("answer"))
		}
	})
}

// Benchmark multi-return performance
func BenchmarkMultiReturnResolution(b *testing.B) {
	NewInfrastructure := func() (*IntegrationDatabase, *IntegrationCache, *IntegrationLogger) {
		return &IntegrationDatabase{ConnectionString: "postgres://localhost"},
			&IntegrationCache{Provider: "redis"},
			&IntegrationLogger{Level: "info"}
	}

	collection := NewCollection()
	_ = collection.AddSingleton(NewInfrastructure)

	provider, _ := collection.Build()
	defer provider.Close()

	dbType := reflect.TypeOf((*IntegrationDatabase)(nil))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = provider.Get(dbType)
	}
}

func BenchmarkMultiReturnVsSingle(b *testing.B) {
	b.Run("multi-return", func(b *testing.B) {
		NewInfrastructure := func() (*IntegrationDatabase, *IntegrationCache, *IntegrationLogger) {
			return &IntegrationDatabase{}, &IntegrationCache{}, &IntegrationLogger{}
		}

		collection := NewCollection()
		_ = collection.AddSingleton(NewInfrastructure)
		provider, _ := collection.Build()
		defer provider.Close()

		types := []reflect.Type{
			reflect.TypeOf((*IntegrationDatabase)(nil)),
			reflect.TypeOf((*IntegrationCache)(nil)),
			reflect.TypeOf((*IntegrationLogger)(nil)),
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, t := range types {
				_, _ = provider.Get(t)
			}
		}
	})

	b.Run("single-return", func(b *testing.B) {
		collection := NewCollection()
		_ = collection.AddSingleton(func() *IntegrationDatabase { return &IntegrationDatabase{} })
		_ = collection.AddSingleton(func() *IntegrationCache { return &IntegrationCache{} })
		_ = collection.AddSingleton(func() *IntegrationLogger { return &IntegrationLogger{} })
		provider, _ := collection.Build()
		defer provider.Close()

		types := []reflect.Type{
			reflect.TypeOf((*IntegrationDatabase)(nil)),
			reflect.TypeOf((*IntegrationCache)(nil)),
			reflect.TypeOf((*IntegrationLogger)(nil)),
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, t := range types {
				_, _ = provider.Get(t)
			}
		}
	})
}

// Test service types for mixed lifetime groups
type MixedService struct {
	ID       string
	Instance int
}

var mixedInstanceCounter int
var mixedCounterMu sync.Mutex

func newMixedSingleton() *MixedService {
	mixedCounterMu.Lock()
	defer mixedCounterMu.Unlock()
	mixedInstanceCounter++
	return &MixedService{ID: "singleton", Instance: mixedInstanceCounter}
}

func newMixedScoped() *MixedService {
	mixedCounterMu.Lock()
	defer mixedCounterMu.Unlock()
	mixedInstanceCounter++
	return &MixedService{ID: "scoped", Instance: mixedInstanceCounter}
}

func newMixedTransient() *MixedService {
	mixedCounterMu.Lock()
	defer mixedCounterMu.Unlock()
	mixedInstanceCounter++
	return &MixedService{ID: "transient", Instance: mixedInstanceCounter}
}

// TestMixedLifetimeGroups tests comprehensive scenarios with mixed lifetime services in groups
func TestMixedLifetimeGroups(t *testing.T) {
	t.Run("singleton_scoped_transient_in_same_group", func(t *testing.T) {
		// Reset counter
		mixedCounterMu.Lock()
		mixedInstanceCounter = 0
		mixedCounterMu.Unlock()

		collection := NewCollection()

		// Add services with different lifetimes to the same group
		err := collection.AddSingleton(newMixedSingleton, Group("mixed"))
		require.NoError(t, err)

		err = collection.AddScoped(newMixedScoped, Group("mixed"))
		require.NoError(t, err)

		err = collection.AddTransient(newMixedTransient, Group("mixed"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Test from root provider
		services1, err := ResolveGroup[*MixedService](provider, "mixed")
		require.NoError(t, err)
		assert.Len(t, services1, 3)

		services2, err := ResolveGroup[*MixedService](provider, "mixed")
		require.NoError(t, err)
		assert.Len(t, services2, 3)

		// Singleton should be same instance (instance number 1)
		assert.Equal(t, services1[0].ID, "singleton")
		assert.Equal(t, services1[0].Instance, 1)
		assert.Same(t, services1[0], services2[0])

		// Scoped should be same in provider context (instance number 2)
		assert.Equal(t, services1[1].ID, "scoped")
		assert.Equal(t, services1[1].Instance, 2)
		assert.Same(t, services1[1], services2[1])

		// Transient should be different (instance 3 and 4)
		assert.Equal(t, services1[2].ID, "transient")
		assert.Equal(t, services1[2].Instance, 3)
		assert.Equal(t, services2[2].ID, "transient")
		assert.Equal(t, services2[2].Instance, 4)
		assert.NotSame(t, services1[2], services2[2])

		// Test from different scopes
		scope1, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope1.Close()

		scope2, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope2.Close()

		scope1Services, err := ResolveGroup[*MixedService](scope1, "mixed")
		require.NoError(t, err)
		assert.Len(t, scope1Services, 3)

		scope2Services, err := ResolveGroup[*MixedService](scope2, "mixed")
		require.NoError(t, err)
		assert.Len(t, scope2Services, 3)

		// Singleton same across scopes (still instance 1)
		assert.Equal(t, scope1Services[0].Instance, 1)
		assert.Equal(t, scope2Services[0].Instance, 1)
		assert.Same(t, scope1Services[0], scope2Services[0])

		// Scoped different across scopes (instances 5 and 6)
		assert.Equal(t, scope1Services[1].ID, "scoped")
		assert.Equal(t, scope2Services[1].ID, "scoped")
		assert.NotSame(t, scope1Services[1], scope2Services[1])
		assert.NotEqual(t, scope1Services[1].Instance, scope2Services[1].Instance)

		// Transient always different
		assert.Equal(t, scope1Services[2].ID, "transient")
		assert.Equal(t, scope2Services[2].ID, "transient")
		assert.NotSame(t, scope1Services[2], scope2Services[2])
		assert.NotEqual(t, scope1Services[2].Instance, scope2Services[2].Instance)

		// Get from same scope twice - verify scoped consistency
		scope1Services2, err := ResolveGroup[*MixedService](scope1, "mixed")
		require.NoError(t, err)

		// Singleton still same
		assert.Same(t, scope1Services[0], scope1Services2[0])

		// Scoped same within scope
		assert.Same(t, scope1Services[1], scope1Services2[1])

		// Transient different even within same scope
		assert.NotSame(t, scope1Services[2], scope1Services2[2])
	})

	t.Run("all_singleton_group", func(t *testing.T) {
		collection := NewCollection()

		err := collection.AddSingleton(func() *MixedService {
			return &MixedService{ID: "s1"}
		}, Group("singletons"))

		require.NoError(t, err)

		err = collection.AddSingleton(func() *MixedService {
			return &MixedService{ID: "s2"}
		}, Group("singletons"))
		require.NoError(t, err)

		err = collection.AddSingleton(func() *MixedService {
			return &MixedService{ID: "s3"}
		}, Group("singletons"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// All should be created at build time
		services1, err := ResolveGroup[*MixedService](provider, "singletons")
		require.NoError(t, err)
		assert.Len(t, services1, 3)

		services2, err := ResolveGroup[*MixedService](provider, "singletons")
		require.NoError(t, err)

		// All should be the same instances
		for i := 0; i < 3; i++ {
			assert.Same(t, services1[i], services2[i])
		}
	})

	t.Run("all_scoped_group", func(t *testing.T) {
		collection := NewCollection()

		for i := 1; i <= 3; i++ {
			id := i // Capture loop variable
			err := collection.AddScoped(func() *MixedService {
				return &MixedService{ID: string(rune('a' + id - 1))}
			}, Group("scoped"))
			require.NoError(t, err)
		}

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope1, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope1.Close()

		scope2, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope2.Close()

		scope1Services1, err := ResolveGroup[*MixedService](scope1, "scoped")
		require.NoError(t, err)

		scope1Services2, err := ResolveGroup[*MixedService](scope1, "scoped")
		require.NoError(t, err)

		scope2Services, err := ResolveGroup[*MixedService](scope2, "scoped")
		require.NoError(t, err)

		// Same instances within scope
		for i := 0; i < 3; i++ {
			assert.Same(t, scope1Services1[i], scope1Services2[i])
		}

		// Different instances across scopes
		for i := 0; i < 3; i++ {
			assert.NotSame(t, scope1Services1[i], scope2Services[i])
		}
	})

	t.Run("all_transient_group", func(t *testing.T) {
		collection := NewCollection()

		for i := 1; i <= 3; i++ {
			id := i // Capture loop variable
			err := collection.AddTransient(func() *MixedService {
				return &MixedService{ID: string(rune('x' + id - 1))}
			}, Group("transient"))
			require.NoError(t, err)
		}

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		services1, err := ResolveGroup[*MixedService](provider, "transient")
		require.NoError(t, err)

		services2, err := ResolveGroup[*MixedService](provider, "transient")
		require.NoError(t, err)

		// All should be different instances
		for i := 0; i < 3; i++ {
			assert.NotSame(t, services1[i], services2[i])
		}
	})

	t.Run("mixed_group_ordering_preserved", func(t *testing.T) {
		collection := NewCollection()

		// Register in specific order: transient, singleton, scoped, transient
		err := collection.AddTransient(func() *MixedService {
			return &MixedService{ID: "t1"}
		}, Group("ordered"))
		require.NoError(t, err)

		err = collection.AddSingleton(func() *MixedService {
			return &MixedService{ID: "s1"}
		}, Group("ordered"))
		require.NoError(t, err)

		err = collection.AddScoped(func() *MixedService {
			return &MixedService{ID: "sc1"}
		}, Group("ordered"))
		require.NoError(t, err)

		err = collection.AddTransient(func() *MixedService {
			return &MixedService{ID: "t2"}
		}, Group("ordered"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		services, err := ResolveGroup[*MixedService](provider, "ordered")
		require.NoError(t, err)
		assert.Len(t, services, 4)

		// Verify order is preserved
		assert.Equal(t, "t1", services[0].ID)
		assert.Equal(t, "s1", services[1].ID)
		assert.Equal(t, "sc1", services[2].ID)
		assert.Equal(t, "t2", services[3].ID)
	})

	t.Run("concurrent_mixed_group_resolution", func(t *testing.T) {
		collection := NewCollection()

		// Add mixed lifetime services
		err := collection.AddSingleton(func() *MixedService {
			return &MixedService{ID: "singleton"}
		}, Group("concurrent"))
		require.NoError(t, err)

		err = collection.AddScoped(func() *MixedService {
			return &MixedService{ID: "scoped"}
		}, Group("concurrent"))
		require.NoError(t, err)

		err = collection.AddTransient(func() *MixedService {
			return &MixedService{ID: "transient"}
		}, Group("concurrent"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		var wg sync.WaitGroup
		errs := make(chan error, 20)
		results := make(chan []*MixedService, 20)

		// Concurrent resolution from provider
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				services, err := ResolveGroup[*MixedService](provider, "concurrent")
				if err != nil {
					errs <- err
					return
				}
				results <- services
			}()
		}

		// Concurrent resolution from different scopes
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				scope, err := provider.CreateScope(context.Background())
				if err != nil {
					errs <- err
					return
				}
				defer scope.Close()

				services, err := ResolveGroup[*MixedService](scope, "concurrent")
				if err != nil {
					errs <- err
					return
				}
				results <- services
			}()
		}

		wg.Wait()
		close(errs)
		close(results)

		// Check for errors
		for err := range errs {
			assert.NoError(t, err)
		}

		// Verify all results have 3 services
		for services := range results {
			assert.Len(t, services, 3)
			assert.Equal(t, "singleton", services[0].ID)
			assert.Equal(t, "scoped", services[1].ID)
			assert.Equal(t, "transient", services[2].ID)
		}
	})

	t.Run("mixed_group_with_dependencies", func(t *testing.T) {
		// Test that services in mixed groups can depend on each other
		type BaseService struct {
			Name string
		}

		type DependentService struct {
			Base *BaseService
			Type string
		}

		collection := NewCollection()

		// Add base service as singleton
		err := collection.AddSingleton(func() *BaseService {
			return &BaseService{Name: "base"}
		})
		require.NoError(t, err)

		// Add dependent services with different lifetimes to a group
		err = collection.AddSingleton(func(base *BaseService) *DependentService {
			return &DependentService{Base: base, Type: "singleton"}
		}, Group("deps"))
		require.NoError(t, err)

		err = collection.AddScoped(func(base *BaseService) *DependentService {
			return &DependentService{Base: base, Type: "scoped"}
		}, Group("deps"))
		require.NoError(t, err)

		err = collection.AddTransient(func(base *BaseService) *DependentService {
			return &DependentService{Base: base, Type: "transient"}
		}, Group("deps"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		services, err := ResolveGroup[*DependentService](provider, "deps")
		require.NoError(t, err)
		assert.Len(t, services, 3)

		// All should have the same base service (singleton)
		base := services[0].Base
		for _, svc := range services {
			assert.Same(t, base, svc.Base)
			assert.Equal(t, "base", svc.Base.Name)
		}
	})

	t.Run("empty_mixed_group", func(t *testing.T) {
		collection := NewCollection()

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Non-existent group should return empty slice
		services, err := ResolveGroup[*MixedService](provider, "nonexistent")
		require.NoError(t, err)
		assert.Empty(t, services)
	})

	t.Run("mixed_group_disposal_order", func(t *testing.T) {
		// Test that disposable services in mixed groups are disposed correctly
		type DisposableService struct {
			ID       string
			Disposed bool
		}

		newDisposable := func(id string) func() *DisposableService {
			return func() *DisposableService {
				return &DisposableService{ID: id}
			}
		}

		collection := NewCollection()

		// Add disposable services with different lifetimes
		err := collection.AddSingleton(newDisposable("singleton"), Group("disposables"))
		require.NoError(t, err)

		err = collection.AddScoped(newDisposable("scoped"), Group("disposables"))
		require.NoError(t, err)

		err = collection.AddTransient(newDisposable("transient"), Group("disposables"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)

		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)

		// Resolve services in scope
		services, err := ResolveGroup[*DisposableService](scope, "disposables")
		require.NoError(t, err)
		assert.Len(t, services, 3)

		// Close scope - should dispose scoped service
		err = scope.Close()
		assert.NoError(t, err)

		// Close provider - should dispose singleton
		err = provider.Close()
		assert.NoError(t, err)
	})

	t.Run("singleton_and_transient_only", func(t *testing.T) {
		// Common pattern: mix singleton (shared state) with transient (stateless handlers)
		collection := NewCollection()

		// Singleton for shared state
		err := collection.AddSingleton(func() *MixedService {
			return &MixedService{ID: "shared-state"}
		}, Group("handlers"))
		require.NoError(t, err)

		// Transients for stateless handlers
		for i := 0; i < 3; i++ {
			id := i
			err2 := collection.AddTransient(func() *MixedService {
				return &MixedService{ID: string(rune('a' + id))}
			}, Group("handlers"))
			require.NoError(t, err2)
		}

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		services1, err := ResolveGroup[*MixedService](provider, "handlers")
		require.NoError(t, err)

		services2, err := ResolveGroup[*MixedService](provider, "handlers")
		require.NoError(t, err)

		// First service (singleton) should be same
		assert.Same(t, services1[0], services2[0])

		// Rest (transients) should be different
		for i := 1; i < 4; i++ {
			assert.NotSame(t, services1[i], services2[i])
		}
	})

	t.Run("scoped_and_transient_only", func(t *testing.T) {
		// Pattern: mix scoped (request state) with transient (validators)
		collection := NewCollection()

		// Scoped for request state
		err := collection.AddScoped(func() *MixedService {
			return &MixedService{ID: "request-state"}
		}, Group("processors"))
		require.NoError(t, err)

		// Transients for validators
		err = collection.AddTransient(func() *MixedService {
			return &MixedService{ID: "validator1"}
		}, Group("processors"))
		require.NoError(t, err)

		err = collection.AddTransient(func() *MixedService {
			return &MixedService{ID: "validator2"}
		}, Group("processors"))
		require.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope.Close()

		services1, err := ResolveGroup[*MixedService](scope, "processors")
		require.NoError(t, err)

		services2, err := ResolveGroup[*MixedService](scope, "processors")
		require.NoError(t, err)

		// Scoped should be same within scope
		assert.Same(t, services1[0], services2[0])

		// Transients should be different
		assert.NotSame(t, services1[1], services2[1])
		assert.NotSame(t, services1[2], services2[2])
	})
}

// BenchmarkMixedLifetimeGroupResolution benchmarks resolving groups with mixed lifetimes
func BenchmarkMixedLifetimeGroupResolution(b *testing.B) {
	collection := NewCollection()

	// Add services with different lifetimes
	_ = collection.AddSingleton(func() *MixedService {
		return &MixedService{ID: "singleton"}
	}, Group("bench"))

	_ = collection.AddScoped(func() *MixedService {
		return &MixedService{ID: "scoped"}
	}, Group("bench"))

	_ = collection.AddTransient(func() *MixedService {
		return &MixedService{ID: "transient"}
	}, Group("bench"))

	provider, _ := collection.Build()
	defer provider.Close()

	scope, _ := provider.CreateScope(context.Background())
	defer scope.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ResolveGroup[*MixedService](scope, "bench")
	}
}

// BenchmarkLargeGroupMixedLifetimes benchmarks resolving large groups with mixed lifetimes
func BenchmarkLargeGroupMixedLifetimes(b *testing.B) {
	collection := NewCollection()

	// Add 100 services with mixed lifetimes
	for i := 0; i < 33; i++ {
		id := i
		_ = collection.AddSingleton(func() *MixedService {
			return &MixedService{ID: "s", Instance: id}
		}, Group("large"))
	}

	for i := 0; i < 33; i++ {
		id := i
		_ = collection.AddScoped(func() *MixedService {
			return &MixedService{ID: "sc", Instance: id}
		}, Group("large"))
	}

	for i := 0; i < 34; i++ {
		id := i
		_ = collection.AddTransient(func() *MixedService {
			return &MixedService{ID: "t", Instance: id}
		}, Group("large"))
	}

	provider, _ := collection.Build()
	defer provider.Close()

	scope, _ := provider.CreateScope(context.Background())
	defer scope.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ResolveGroup[*MixedService](scope, "large")
	}
}
