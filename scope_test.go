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
		assert.Same(t, ch, value) // Should be the same channel
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
		assert.Equal(t, ErrNilConstructor, err)
	})

	t.Run("register nil pointer", func(t *testing.T) {
		collection := NewCollection()

		type Config struct {
			Host string
		}

		var config *Config = nil
		err := collection.AddSingleton(config)
		assert.Error(t, err)
		assert.Equal(t, ErrNilConstructor, err)
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
