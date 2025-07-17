package godi_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junioryono/godi"
)

// Test types for scope tests
type (
	scopeTestService struct {
		ID        string
		CreatedAt time.Time
		Scope     string
	}

	scopeTestDisposable struct {
		ID           string
		disposed     bool
		disposeError error
		disposeTime  time.Time
		mu           sync.Mutex
	}

	scopeTestContextAware struct {
		ctx context.Context
	}
)

func (s *scopeTestDisposable) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.disposed {
		return errAlreadyDisposed
	}

	s.disposed = true
	s.disposeTime = time.Now()
	return s.disposeError
}

func TestServiceProviderScope_Creation(t *testing.T) {
	t.Run("creates root scope", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// The provider itself is the root scope
		rootScope := provider.GetRootScope()
		if rootScope == nil {
			t.Fatal("expected root scope to be created")
		}

		if !rootScope.IsRootScope() {
			t.Error("root scope should have isRootScope=true")
		}

		if rootScope.ID() == "" {
			t.Error("root scope should have a scope ID")
		}
	})

	t.Run("creates child scope with context", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		type ctxKey string
		const testKey ctxKey = "test-key"
		ctx := context.WithValue(context.Background(), testKey, "test-value")

		scope := provider.CreateScope(ctx)
		defer scope.Close()

		// Verify it's not a root scope
		// scopeImpl := scope.(*serviceProviderScope)
		if scope.IsRootScope() {
			t.Error("child scope should have isRootScope=false")
		}

		// Verify context is stored
		if scope.Context().Value(testKey) != "test-value" {
			t.Error("scope should preserve context")
		}

		// Verify scope has unique ID
		if scope.ID() == "" {
			t.Error("scope should have a scope ID")
		}

		// Verify parent is set
		if scope.Parent() == nil {
			t.Error("child scope should have parent set")
		}
	})

	t.Run("scope ID is unique", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scopeIDs := make(map[string]bool)

		// Create multiple scopes
		for i := 0; i < 10; i++ {
			scope := provider.CreateScope(context.Background())

			if scopeIDs[scope.ID()] {
				t.Errorf("duplicate scope ID: %s", scope.ID())
			}
			scopeIDs[scope.ID()] = true

			scope.Close()
		}
	})
}

func TestServiceProviderScope_ServiceProvider(t *testing.T) {
	t.Run("panics when disposed", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		scope.Close()

		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic when calling ServiceProvider on disposed scope")
			}
		}()

		scope.GetRootScope()
	})
}

func TestServiceProviderScope_Resolve(t *testing.T) {
	t.Run("resolves service in scope", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddScoped(func() *scopeTestService {
			return &scopeTestService{
				ID:        "scoped",
				CreatedAt: time.Now(),
				Scope:     "test",
			}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		service, err := scope.Resolve(reflect.TypeOf((*scopeTestService)(nil)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		svc := service.(*scopeTestService)
		if svc.ID != "scoped" {
			t.Errorf("expected ID 'scoped', got %s", svc.ID)
		}
	})

	t.Run("returns error when disposed", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(func() *scopeTestService {
			return &scopeTestService{}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		scope.Close()

		// Since scope implements ServiceProvider, we can call Resolve directly
		// without going through ServiceProvider() which panics when disposed
		_, err = scope.(godi.ServiceProvider).Resolve(reflect.TypeOf((*scopeTestService)(nil)))
		if err == nil {
			t.Error("expected error when resolving from disposed scope")
		}
		if !errors.Is(err, godi.ErrScopeDisposed) {
			t.Errorf("expected ErrScopeDisposed, got %v", err)
		}
	})

	// Alternative: If you want to test the panic behavior:
	t.Run("ServiceProvider panics when disposed", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		scope.Close()

		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic when calling ServiceProvider on disposed scope")
			} else if !errors.Is(r.(error), godi.ErrScopeDisposed) {
				t.Errorf("expected panic with ErrScopeDisposed, got %v", r)
			}
		}()

		scope.GetRootScope() // This should panic
	})

	t.Run("calls resolution callbacks", func(t *testing.T) {
		resolvedType := reflect.Type(nil)
		resolvedInstance := interface{}(nil)

		collection := godi.NewServiceCollection()
		collection.AddSingleton(func() *scopeTestService {
			return &scopeTestService{ID: "callback-test"}
		})

		options := &godi.ServiceProviderOptions{
			OnServiceResolved: func(serviceType reflect.Type, instance interface{}, duration time.Duration) {
				resolvedType = serviceType
				resolvedInstance = instance
			},
		}

		provider, err := collection.BuildServiceProviderWithOptions(options)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		scope.Resolve(reflect.TypeOf((*scopeTestService)(nil)))

		// Verify callback was called
		if resolvedType != reflect.TypeOf((*scopeTestService)(nil)) {
			t.Error("callback not called with correct type")
		}
		if resolvedInstance == nil {
			t.Error("callback not called with instance")
		}
	})
}

func TestServiceProviderScope_ResolveKeyed(t *testing.T) {
	t.Run("resolves keyed service", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(func() *scopeTestService {
			return &scopeTestService{ID: "primary"}
		}, godi.Name("primary"))
		collection.AddSingleton(func() *scopeTestService {
			return &scopeTestService{ID: "secondary"}
		}, godi.Name("secondary"))

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		// Resolve primary
		primary, err := scope.ResolveKeyed(
			reflect.TypeOf((*scopeTestService)(nil)),
			"primary",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		primarySvc := primary.(*scopeTestService)
		if primarySvc.ID != "primary" {
			t.Errorf("expected ID 'primary', got %s", primarySvc.ID)
		}

		// Resolve secondary
		secondary, err := scope.ResolveKeyed(
			reflect.TypeOf((*scopeTestService)(nil)),
			"secondary",
		)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		secondarySvc := secondary.(*scopeTestService)
		if secondarySvc.ID != "secondary" {
			t.Errorf("expected ID 'secondary', got %s", secondarySvc.ID)
		}
	})

	t.Run("returns error for nil key", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		_, err = scope.ResolveKeyed(
			reflect.TypeOf((*scopeTestService)(nil)),
			nil,
		)
		if err == nil {
			t.Error("expected error for nil key")
		}
		if !strings.Contains(err.Error(), "service key cannot be nil") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestServiceProviderScope_CreateScope(t *testing.T) {
	t.Run("creates nested scope", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		parentScope := provider.CreateScope(context.Background())
		defer parentScope.Close()

		childScope := parentScope.CreateScope(context.Background())
		defer childScope.Close()

		if childScope.Parent() != parentScope {
			t.Error("child scope should have correct parent")
		}
	})

	t.Run("panics when parent is disposed", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		scope.Close()

		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic when creating scope from disposed parent")
			}
		}()

		scope.CreateScope(context.Background())
	})
}

// closerFunc is a helper type to wrap a function as an Disposable
type closerFunc func() error

func TestServiceProviderScope_Close(t *testing.T) {

	t.Run("safe to call multiple times", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())

		// First close
		err = scope.Close()
		if err != nil {
			t.Errorf("first close error: %v", err)
		}

		// Second close
		err = scope.Close()
		if err != nil {
			t.Errorf("second close should not error: %v", err)
		}
	})

	t.Run("removes finalizer", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())

		// Close should remove finalizer
		err = scope.Close()
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}

		// Force GC to ensure no panic from finalizer
		runtime.GC()
		runtime.Gosched()
	})
}

func TestServiceProviderScope_Invoke(t *testing.T) {
	t.Run("invokes function in scope", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddScoped(func() *scopeTestService {
			return &scopeTestService{ID: "scoped"}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		var invokedService *scopeTestService
		err = scope.Invoke(func(svc *scopeTestService) {
			invokedService = svc
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if invokedService == nil {
			t.Error("service not injected")
		}
		if invokedService.ID != "scoped" {
			t.Errorf("expected ID 'scoped', got %s", invokedService.ID)
		}
	})

	// This test should be updated to expect an error when registering the same type with different lifetimes
	t.Run("cannot register same type with different lifetimes", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Root service
		err := collection.AddSingleton(func() *scopeTestService {
			return &scopeTestService{ID: "root"}
		})
		if err != nil {
			t.Fatalf("unexpected error adding singleton: %v", err)
		}

		// Attempting to add the same type as scoped should fail
		err = collection.AddScoped(func() *scopeTestService {
			return &scopeTestService{ID: "scoped"}
		})
		if err == nil {
			t.Fatal("expected error when registering same type with different lifetime")
		}

		var lifetimeErr godi.LifetimeConflictError
		if !errors.As(err, &lifetimeErr) {
			t.Errorf("expected ErrBothConstructorAndDecorator, got %v", err)
		}
	})

	// If you need different implementations in different scopes, use keyed services
	t.Run("uses keyed services for different implementations", func(t *testing.T) {
		rootInvoked := false
		scopedInvoked := false

		collection := godi.NewServiceCollection()

		// Root service with key
		collection.AddSingleton(func() *scopeTestService {
			rootInvoked = true
			return &scopeTestService{ID: "root"}
		}, godi.Name("root"))

		// Scoped service with different key
		collection.AddScoped(func() *scopeTestService {
			scopedInvoked = true
			return &scopeTestService{ID: "scoped"}
		}, godi.Name("scoped"))

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Resolve keyed services
		rootService, err := provider.ResolveKeyed(reflect.TypeOf((*scopeTestService)(nil)), "root")
		if err != nil {
			t.Fatalf("failed to resolve root service: %v", err)
		}

		if !rootInvoked {
			t.Error("root service should be invoked")
		}

		if rootService.(*scopeTestService).ID != "root" {
			t.Errorf("expected root service, got %s", rootService.(*scopeTestService).ID)
		}

		// Reset flags
		rootInvoked = false
		scopedInvoked = false

		// Resolve in scope
		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		scopedService, err := scope.ResolveKeyed(reflect.TypeOf((*scopeTestService)(nil)), "scoped")
		if err != nil {
			t.Fatalf("failed to resolve scoped service: %v", err)
		}

		if !scopedInvoked {
			t.Error("scoped service should be invoked")
		}

		if scopedService.(*scopeTestService).ID != "scoped" {
			t.Errorf("expected scoped service, got %s", scopedService.(*scopeTestService).ID)
		}
	})

	// Or use Replace if you really need to change the lifetime
	t.Run("can replace service with different lifetime", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Initial registration as singleton
		err := collection.AddSingleton(func() *scopeTestService {
			return &scopeTestService{ID: "singleton"}
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Replace with scoped
		err = collection.Replace(godi.Scoped, func() *scopeTestService {
			return &scopeTestService{ID: "scoped"}
		})
		if err != nil {
			t.Fatalf("unexpected error replacing: %v", err)
		}

		// Should be able to build and use the scoped version
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		service, err := godi.Resolve[scopeTestService](scope)
		if err != nil {
			t.Fatalf("failed to resolve service: %v", err)
		}

		if service.ID != "scoped" {
			t.Errorf("expected scoped service, got %s", service.ID)
		}
	})
}

func TestServiceProviderScope_Context(t *testing.T) {
	t.Run("preserves context", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		type ctxKey string
		const (
			key1 ctxKey = "key1"
			key2 ctxKey = "key2"
		)

		ctx := context.WithValue(context.Background(), key1, "value1")
		ctx = context.WithValue(ctx, key2, "value2")

		scope := provider.CreateScope(ctx)
		defer scope.Close()

		ctxR, err := scope.Resolve(reflect.TypeOf((*context.Context)(nil)).Elem())
		if err != nil {
			t.Fatalf("unexpected error resolving context: %v", err)
		}

		cR := ctxR.(context.Context)
		if cR.Value(key1) != "value1" {
			t.Error("context value 1 not preserved")
		}

		if cR.Value(key2) != "value2" {
			t.Error("context value 2 not preserved")
		}
	})

	t.Run("context available for injection", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddScoped(func(ctx context.Context) *scopeTestContextAware {
			return &scopeTestContextAware{ctx: ctx}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		type ctxKey string
		const testKey ctxKey = "test"
		ctx := context.WithValue(context.Background(), testKey, "injected")

		scope := provider.CreateScope(ctx)
		defer scope.Close()

		service, err := scope.Resolve(reflect.TypeOf((*scopeTestContextAware)(nil)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		ctxAware := service.(*scopeTestContextAware)
		if ctxAware.ctx.Value(testKey) != "injected" {
			t.Error("context not properly injected")
		}
	})
}

func TestServiceProviderScope_Concurrency(t *testing.T) {
	t.Run("concurrent resolution in same scope", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddScoped(func() *scopeTestService {
			// Simulate some work
			time.Sleep(10 * time.Millisecond)
			return &scopeTestService{
				ID:        fmt.Sprintf("instance-%d", time.Now().UnixNano()),
				CreatedAt: time.Now(),
			}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		const goroutines = 10
		results := make([]*scopeTestService, goroutines)
		var wg sync.WaitGroup
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			idx := i
			go func() {
				defer wg.Done()
				service, err := scope.Resolve(reflect.TypeOf((*scopeTestService)(nil)))
				if err != nil {
					t.Errorf("goroutine %d: %v", idx, err)
					return
				}
				results[idx] = service.(*scopeTestService)
			}()
		}

		wg.Wait()

		// All should be the same instance (scoped)
		first := results[0]
		for i := 1; i < goroutines; i++ {
			if results[i] != first {
				t.Errorf("expected same instance, got different at index %d", i)
			}
		}
	})

	t.Run("concurrent disposal", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		const scopeCount = 20
		scopes := make([]godi.Scope, scopeCount)

		// Create scopes
		for i := 0; i < scopeCount; i++ {
			scopes[i] = provider.CreateScope(context.Background())
		}

		// Close concurrently
		var wg sync.WaitGroup
		wg.Add(scopeCount)

		closeCount := int32(0)
		for i := 0; i < scopeCount; i++ {
			scope := scopes[i]
			go func() {
				defer wg.Done()
				err := scope.Close()
				if err == nil {
					atomic.AddInt32(&closeCount, 1)
				}
			}()
		}

		wg.Wait()

		if atomic.LoadInt32(&closeCount) != scopeCount {
			t.Errorf("expected %d successful closes, got %d", scopeCount, closeCount)
		}
	})
}

func TestServiceProviderScope_TransientWithDependencies(t *testing.T) {
	t.Run("transient service with scoped dependencies", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Scoped dependency
		collection.AddScoped(func() *scopeTestService {
			return &scopeTestService{ID: "scoped-dep"}
		})

		// Transient service depending on scoped
		collection.AddTransient(func(svc *scopeTestService) *scopeTestDisposable {
			return &scopeTestDisposable{ID: fmt.Sprintf("transient-with-%s", svc.ID)}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope1 := provider.CreateScope(context.Background())
		defer scope1.Close()

		// Resolve transient multiple times in same scope
		svc1, err := scope1.Resolve(reflect.TypeOf((*scopeTestDisposable)(nil)))
		if err != nil {
			t.Fatalf("unexpected error resolving transient: %v", err)
		}

		svc2, err := scope1.Resolve(reflect.TypeOf((*scopeTestDisposable)(nil)))
		if err != nil {
			t.Fatalf("unexpected error resolving transient: %v", err)
		}

		// Should be different instances but with same scoped dependency
		if svc1 == svc2 {
			t.Error("transient services should be different instances")
		}

		// Both should reference the same scoped dependency
		if !strings.Contains(svc1.(*scopeTestDisposable).ID, "scoped-dep") {
			t.Error("transient should have scoped dependency")
		}
	})
}

// Benchmarks
func BenchmarkServiceProviderScope_Resolve(b *testing.B) {
	collection := godi.NewServiceCollection()
	collection.AddScoped(func() *scopeTestService {
		return &scopeTestService{ID: "bench"}
	})

	provider, err := collection.BuildServiceProvider()
	if err != nil {
		b.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	scope := provider.CreateScope(context.Background())
	defer scope.Close()

	serviceType := reflect.TypeOf((*scopeTestService)(nil))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := scope.Resolve(serviceType)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkServiceProviderScope_Disposal(b *testing.B) {
	collection := godi.NewServiceCollection()

	// Add services that need disposal
	for i := 0; i < 10; i++ {
		collection.AddScoped(func() *scopeTestDisposable {
			return &scopeTestDisposable{ID: "bench"}
		}, godi.Name(fmt.Sprintf("service%d", i)))
	}

	provider, err := collection.BuildServiceProvider()
	if err != nil {
		b.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scope := provider.CreateScope(context.Background())

		// Resolve all services to create instances
		for j := 0; j < 10; j++ {
			scope.ResolveKeyed(
				reflect.TypeOf((*scopeTestDisposable)(nil)),
				fmt.Sprintf("service%d", j),
			)
		}

		scope.Close()
	}
}

func BenchmarkServiceProviderScope_ConcurrentResolve(b *testing.B) {
	collection := godi.NewServiceCollection()
	collection.AddScoped(func() *scopeTestService {
		return &scopeTestService{ID: "concurrent"}
	})

	provider, err := collection.BuildServiceProvider()
	if err != nil {
		b.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	scope := provider.CreateScope(context.Background())
	defer scope.Close()

	serviceType := reflect.TypeOf((*scopeTestService)(nil))

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := scope.Resolve(serviceType)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
