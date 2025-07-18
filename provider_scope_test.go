package godi_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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

	// Singleton services
	lifetimeSingletonService struct {
		ID          string
		Transient   *lifetimeTransientService
		TransientID string // To verify we get different transient instances
	}

	lifetimeSingletonWithScoped struct {
		ID     string
		Scoped *lifetimeScopedService // This should fail during validation
	}

	// Scoped services
	lifetimeScopedService struct {
		ID          string
		Singleton   *lifetimeSingletonOnlyService
		Transient   *lifetimeTransientService
		TransientID string
		SingletonID string
	}

	// Transient services
	lifetimeTransientService struct {
		ID        string
		Singleton *lifetimeSingletonOnlyService
		Scoped    *lifetimeScopedOnlyService
	}

	// Pure services for dependencies
	lifetimeSingletonOnlyService struct {
		ID string
	}

	lifetimeScopedOnlyService struct {
		ID string
	}

	lifetimeTransientOnlyService struct {
		ID string
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

// Constructors
func newLifetimeSingletonService(transient *lifetimeTransientService) *lifetimeSingletonService {
	return &lifetimeSingletonService{
		ID:          fmt.Sprintf("singleton-%s", generateID()),
		Transient:   transient,
		TransientID: transient.ID,
	}
}

func newLifetimeSingletonWithScoped(scoped *lifetimeScopedService) *lifetimeSingletonWithScoped {
	return &lifetimeSingletonWithScoped{
		ID:     fmt.Sprintf("singleton-%s", generateID()),
		Scoped: scoped,
	}
}

func newLifetimeScopedService(singleton *lifetimeSingletonOnlyService, transient *lifetimeTransientService) *lifetimeScopedService {
	return &lifetimeScopedService{
		ID:          fmt.Sprintf("scoped-%s", generateID()),
		Singleton:   singleton,
		Transient:   transient,
		TransientID: transient.ID,
		SingletonID: singleton.ID,
	}
}

func newLifetimeTransientService(singleton *lifetimeSingletonOnlyService, scoped *lifetimeScopedOnlyService) *lifetimeTransientService {
	return &lifetimeTransientService{
		ID:        fmt.Sprintf("transient-%s", generateID()),
		Singleton: singleton,
		Scoped:    scoped,
	}
}

func newLifetimeSingletonOnlyService() *lifetimeSingletonOnlyService {
	return &lifetimeSingletonOnlyService{
		ID: fmt.Sprintf("singleton-only-%s", generateID()),
	}
}

func newLifetimeScopedOnlyService() *lifetimeScopedOnlyService {
	return &lifetimeScopedOnlyService{
		ID: fmt.Sprintf("scoped-only-%s", generateID()),
	}
}

func newLifetimeTransientOnlyService() *lifetimeTransientOnlyService {
	return &lifetimeTransientOnlyService{
		ID: fmt.Sprintf("transient-only-%s", generateID()),
	}
}

func generateID() string {
	// Timestamp (8 bytes)
	timestamp := time.Now().UnixNano()

	// Random bytes (8 bytes)
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		panic(err)
	}

	// Combine timestamp and random
	return fmt.Sprintf("%016x%s", timestamp, hex.EncodeToString(randomBytes))
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

func TestLifetimeDependencies_SingletonAccessingTransient(t *testing.T) {
	// t.Run("singleton can access transient", func(t *testing.T) {
	// 	collection := godi.NewServiceCollection()

	// 	// Register services
	// 	collection.AddSingleton(newLifetimeSingletonOnlyService)
	// 	collection.AddTransient(func() *lifetimeTransientService {
	// 		return &lifetimeTransientService{
	// 			ID: fmt.Sprintf("transient-%d", time.Now().UnixNano()),
	// 		}
	// 	})
	// 	collection.AddSingleton(newLifetimeSingletonService)

	// 	provider, err := collection.BuildServiceProvider()
	// 	if err != nil {
	// 		t.Fatalf("unexpected error: %v", err)
	// 	}
	// 	defer provider.Close()

	// 	// Resolve singleton multiple times
	// 	svc1, err := godi.Resolve[*lifetimeSingletonService](provider)
	// 	if err != nil {
	// 		t.Fatalf("unexpected error resolving singleton: %v", err)
	// 	}

	// 	svc2, err := godi.Resolve[*lifetimeSingletonService](provider)
	// 	if err != nil {
	// 		t.Fatalf("unexpected error resolving singleton: %v", err)
	// 	}

	// 	// Should be same singleton instance
	// 	if svc1 != svc2 {
	// 		t.Error("singleton instances should be the same")
	// 	}

	// 	// The transient inside should be the same (captured at singleton creation)
	// 	if svc1.TransientID != svc2.TransientID {
	// 		t.Error("transient captured by singleton should be the same")
	// 	}
	// })

	// t.Run("singleton with transient factory pattern", func(t *testing.T) {
	// 	// This test shows how a singleton can get new transient instances
	// 	// by injecting a factory function instead of the service directly
	// 	collection := godi.NewServiceCollection()

	// 	collection.AddTransient(newLifetimeTransientOnlyService)

	// 	// Singleton that takes a factory function
	// 	type singletonWithFactory struct {
	// 		ID               string
	// 		TransientFactory func() *lifetimeTransientOnlyService
	// 		mu               sync.Mutex
	// 		createdInstances []string
	// 	}

	// 	collection.AddSingleton(func() *singletonWithFactory {
	// 		return &singletonWithFactory{
	// 			ID: "singleton-factory",
	// 		}
	// 	})

	// 	provider, err := collection.BuildServiceProvider()
	// 	if err != nil {
	// 		t.Fatalf("unexpected error: %v", err)
	// 	}
	// 	defer provider.Close()

	// 	svc, err := godi.Resolve[*singletonWithFactory](provider)
	// 	if err != nil {
	// 		t.Fatalf("unexpected error: %v", err)
	// 	}

	// 	// In a real scenario, the singleton would have a method that uses
	// 	// the factory to create transient instances as needed
	// 	// This demonstrates that singletons can work with transients
	// 	// but need to be careful about lifetime management
	// 	svc.mu.Lock()
	// 	defer svc.mu.Unlock()
	// 	for i := 0; i < 5; i++ {
	// 		transient := svc.TransientFactory()
	// 		svc.createdInstances = append(svc.createdInstances, transient.ID)
	// 	}

	// 	if len(svc.createdInstances) != 5 {
	// 		t.Errorf("expected 5 transient instances, got %d", len(svc.createdInstances))
	// 	}

	// 	for _, id := range svc.createdInstances {
	// 		if !strings.HasPrefix(id, "transient-only-") {
	// 			t.Errorf("unexpected transient ID: %s", id)
	// 		}
	// 	}
	// })
}

func TestLifetimeDependencies_SingletonCannotAccessScoped(t *testing.T) {
	t.Run("singleton cannot access scoped - validation error", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Register services
		collection.AddScoped(newLifetimeScopedOnlyService)
		collection.AddSingleton(func(scoped *lifetimeScopedOnlyService) *lifetimeSingletonWithScoped {
			return &lifetimeSingletonWithScoped{
				ID: "invalid-singleton",
			}
		})

		options := &godi.ServiceProviderOptions{
			ValidateOnBuild: true,
		}

		_, err := collection.BuildServiceProviderWithOptions(options)
		if err == nil {
			t.Fatal("expected validation error when singleton depends on scoped")
		}

		// The error should indicate the dependency issue
		if !godi.IsNotFound(err) {
			t.Errorf("expected dependency not found error, got: %v", err)
		}
	})

	t.Run("singleton cannot access scoped - runtime error without validation", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Register services
		collection.AddScoped(newLifetimeScopedOnlyService)
		collection.AddSingleton(func(scoped *lifetimeScopedOnlyService) *lifetimeSingletonWithScoped {
			return &lifetimeSingletonWithScoped{
				ID: "invalid-singleton",
			}
		})

		// Build without validation
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error during build: %v", err)
		}
		defer provider.Close()

		// Should fail when trying to resolve
		_, err = godi.Resolve[*lifetimeSingletonWithScoped](provider)
		if err == nil {
			t.Fatal("expected error when resolving singleton that depends on scoped")
		}
	})
}

func TestLifetimeDependencies_ScopedAccessingTransient(t *testing.T) {
	t.Run("scoped can access transient", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Register services
		collection.AddSingleton(newLifetimeSingletonOnlyService)
		collection.AddScoped(newLifetimeScopedOnlyService)
		collection.AddTransient(newLifetimeTransientService)
		collection.AddScoped(newLifetimeScopedService)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		// Resolve scoped service multiple times in same scope
		svc1, err := godi.Resolve[*lifetimeScopedService](scope)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		svc2, err := godi.Resolve[*lifetimeScopedService](scope)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should be same scoped instance
		if svc1 != svc2 {
			t.Error("scoped instances should be the same within scope")
		}

		// The transient inside should be the same (captured at scoped creation)
		if svc1.TransientID != svc2.TransientID {
			t.Error("transient captured by scoped should be the same")
		}

		// Create new scope
		scope2 := provider.CreateScope(context.Background())
		defer scope2.Close()

		svc3, err := godi.Resolve[*lifetimeScopedService](scope2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should be different scoped instance
		if svc1 == svc3 {
			t.Error("scoped instances should be different across scopes")
		}

		// Should have different transient
		if svc1.TransientID == svc3.TransientID {
			t.Error("transient should be different in different scopes")
		}
	})
}

func TestLifetimeDependencies_TransientAccessingScopedAndSingleton(t *testing.T) {
	t.Run("transient can access both scoped and singleton", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Register services
		collection.AddSingleton(newLifetimeSingletonOnlyService)
		collection.AddScoped(newLifetimeScopedOnlyService)
		collection.AddTransient(newLifetimeTransientService)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		// Resolve transient multiple times
		trans1, err := godi.Resolve[*lifetimeTransientService](scope)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		trans2, err := godi.Resolve[*lifetimeTransientService](scope)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should be different transient instances
		if trans1 == trans2 {
			t.Error("transient instances should be different")
		}
		if trans1.ID == trans2.ID {
			t.Error("transient IDs should be different")
		}

		// But they should reference the same singleton
		if trans1.Singleton.ID != trans2.Singleton.ID {
			t.Error("transients should reference the same singleton")
		}

		// And the same scoped service (within the same scope)
		if trans1.Scoped.ID != trans2.Scoped.ID {
			t.Error("transients should reference the same scoped service within scope")
		}
	})
}

func TestLifetimeDependencies_ComplexDependencyChain(t *testing.T) {
	t.Run("complex dependency chain with all lifetimes", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Register a complex chain
		collection.AddSingleton(func() *lifetimeSingletonOnlyService {
			return &lifetimeSingletonOnlyService{ID: "root-singleton"}
		})

		collection.AddScoped(func(singleton *lifetimeSingletonOnlyService) *lifetimeScopedOnlyService {
			return &lifetimeScopedOnlyService{
				ID: fmt.Sprintf("scoped-with-singleton-%s", singleton.ID),
			}
		})

		collection.AddTransient(func(singleton *lifetimeSingletonOnlyService, scoped *lifetimeScopedOnlyService) *lifetimeTransientService {
			return &lifetimeTransientService{
				ID:        fmt.Sprintf("transient-%d", time.Now().UnixNano()),
				Singleton: singleton,
				Scoped:    scoped,
			}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Test in first scope
		scope1 := provider.CreateScope(context.Background())
		defer scope1.Close()

		trans1_1, _ := godi.Resolve[*lifetimeTransientService](scope1)
		trans1_2, _ := godi.Resolve[*lifetimeTransientService](scope1)

		// Different transient instances
		if trans1_1 == trans1_2 {
			t.Error("transient instances should be different")
		}

		// Same singleton reference
		if trans1_1.Singleton.ID != "root-singleton" {
			t.Error("singleton ID mismatch")
		}
		if trans1_1.Singleton.ID != trans1_2.Singleton.ID {
			t.Error("singleton should be the same")
		}

		// Same scoped reference within scope
		if trans1_1.Scoped.ID != trans1_2.Scoped.ID {
			t.Error("scoped should be the same within scope")
		}

		// Test in second scope
		scope2 := provider.CreateScope(context.Background())
		defer scope2.Close()

		trans2_1, _ := godi.Resolve[*lifetimeTransientService](scope2)

		// Same singleton across scopes
		if trans1_1.Singleton.ID != trans2_1.Singleton.ID {
			t.Error("singleton should be the same across scopes")
		}

		// Different scoped across scopes
		if trans1_1.Scoped.ID == trans2_1.Scoped.ID {
			t.Error("scoped should be different across scopes")
		}
	})
}

func TestLifetimeDependencies_TransientResolutionPatterns(t *testing.T) {
	t.Run("transient resolved through different paths", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		instanceCount := 0
		collection.AddTransient(func() *lifetimeTransientOnlyService {
			instanceCount++
			return &lifetimeTransientOnlyService{
				ID: fmt.Sprintf("transient-%d", instanceCount),
			}
		})

		// Service A depends on transient
		type serviceA struct {
			Transient *lifetimeTransientOnlyService
		}
		collection.AddScoped(func(t *lifetimeTransientOnlyService) *serviceA {
			return &serviceA{Transient: t}
		})

		// Service B also depends on transient
		type serviceB struct {
			Transient *lifetimeTransientOnlyService
		}
		collection.AddScoped(func(t *lifetimeTransientOnlyService) *serviceB {
			return &serviceB{Transient: t}
		})

		// Service C depends on both A and B
		type serviceC struct {
			A *serviceA
			B *serviceB
		}
		collection.AddScoped(func(a *serviceA, b *serviceB) *serviceC {
			return &serviceC{A: a, B: b}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		// Resolve service C
		svcC, err := godi.Resolve[*serviceC](scope)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// A and B should have different transient instances
		if svcC.A.Transient.ID == svcC.B.Transient.ID {
			t.Error("transient instances should be different when resolved through different paths")
		}

		// Direct resolution should also give a new instance
		directTransient, err := godi.Resolve[*lifetimeTransientOnlyService](scope)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if directTransient.ID == svcC.A.Transient.ID || directTransient.ID == svcC.B.Transient.ID {
			t.Error("directly resolved transient should be a new instance")
		}
	})
}

func TestLifetimeDependencies_LifetimeMismatchErrors(t *testing.T) {
	t.Run("detects lifetime mismatches during validation", func(t *testing.T) {
		testCases := []struct {
			name        string
			setup       func(godi.ServiceCollection)
			shouldError bool
			errorMsg    string
		}{
			{
				name: "singleton depending on scoped",
				setup: func(c godi.ServiceCollection) {
					c.AddScoped(func() *lifetimeScopedOnlyService {
						return &lifetimeScopedOnlyService{ID: "test"}
					})
					c.AddSingleton(func(scoped *lifetimeScopedOnlyService) *lifetimeSingletonOnlyService {
						return &lifetimeSingletonOnlyService{ID: "invalid"}
					})
				},
				shouldError: true,
				errorMsg:    "singleton cannot depend on scoped",
			},
			{
				name: "singleton depending on transient is allowed",
				setup: func(c godi.ServiceCollection) {
					c.AddTransient(func() *lifetimeTransientOnlyService {
						return &lifetimeTransientOnlyService{ID: "test"}
					})
					c.AddSingleton(func(trans *lifetimeTransientOnlyService) *lifetimeSingletonOnlyService {
						return &lifetimeSingletonOnlyService{ID: trans.ID}
					})
				},
				shouldError: false,
			},
			{
				name: "scoped depending on singleton is allowed",
				setup: func(c godi.ServiceCollection) {
					c.AddSingleton(func() *lifetimeSingletonOnlyService {
						return &lifetimeSingletonOnlyService{ID: "test"}
					})
					c.AddScoped(func(singleton *lifetimeSingletonOnlyService) *lifetimeScopedOnlyService {
						return &lifetimeScopedOnlyService{ID: singleton.ID}
					})
				},
				shouldError: false,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				collection := godi.NewServiceCollection()
				tc.setup(collection)

				options := &godi.ServiceProviderOptions{
					ValidateOnBuild: true,
				}

				_, err := collection.BuildServiceProviderWithOptions(options)

				if tc.shouldError && err == nil {
					t.Errorf("expected error for %s", tc.errorMsg)
				} else if !tc.shouldError && err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			})
		}
	})
}

// Test concurrent transient resolution
func TestTransientServices_Concurrency(t *testing.T) {
	t.Run("concurrent transient resolution creates unique instances", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		var counter int32
		collection.AddTransient(func() *lifetimeTransientOnlyService {
			id := atomic.AddInt32(&counter, 1)
			return &lifetimeTransientOnlyService{
				ID: fmt.Sprintf("transient-%d", id),
			}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		const goroutines = 100
		instances := make([]*lifetimeTransientOnlyService, goroutines)
		var wg sync.WaitGroup
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			idx := i
			go func() {
				defer wg.Done()
				inst, err := godi.Resolve[*lifetimeTransientOnlyService](scope)
				if err != nil {
					t.Errorf("goroutine %d: %v", idx, err)
					return
				}
				instances[idx] = inst
			}()
		}

		wg.Wait()

		// Verify all instances are unique
		seen := make(map[string]bool)
		for i, inst := range instances {
			if inst == nil {
				t.Errorf("instance %d is nil", i)
				continue
			}
			if seen[inst.ID] {
				t.Errorf("duplicate instance ID: %s", inst.ID)
			}
			seen[inst.ID] = true
		}

		// Should have created exactly 'goroutines' instances
		if int(atomic.LoadInt32(&counter)) != goroutines {
			t.Errorf("expected %d instances, got %d", goroutines, counter)
		}
	})
}

// Test transient with groups
func TestTransientServices_Groups(t *testing.T) {
	t.Run("transient services in groups are not allowed", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// This should fail because transient services cannot be in groups
		err := collection.AddTransient(func() Handler {
			return &testHandler{name: "transient-handler"}
		}, godi.Group("handlers"))

		if err == nil {
			t.Fatal("expected error when adding transient service to group")
		}

		if !errors.Is(err, godi.ErrTransientInGroup) {
			t.Errorf("expected ErrTransientInGroup, got %v", err)
		}
	})
}

// Test transient services with keyed registration
func TestTransientServices_Keyed(t *testing.T) {
	t.Run("keyed transient services work correctly", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		var primaryCount, secondaryCount int32

		collection.AddTransient(func() *lifetimeTransientOnlyService {
			count := atomic.AddInt32(&primaryCount, 1)
			return &lifetimeTransientOnlyService{
				ID: fmt.Sprintf("primary-%d", count),
			}
		}, godi.Name("primary"))

		collection.AddTransient(func() *lifetimeTransientOnlyService {
			count := atomic.AddInt32(&secondaryCount, 1)
			return &lifetimeTransientOnlyService{
				ID: fmt.Sprintf("secondary-%d", count),
			}
		}, godi.Name("secondary"))

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		// Resolve primary multiple times
		primary1, err := godi.ResolveKeyed[*lifetimeTransientOnlyService](scope, "primary")
		if err != nil {
			t.Fatalf("error resolving primary: %v", err)
		}

		primary2, err := godi.ResolveKeyed[*lifetimeTransientOnlyService](scope, "primary")
		if err != nil {
			t.Fatalf("error resolving primary: %v", err)
		}

		// Should be different instances
		if primary1 == primary2 {
			t.Error("keyed transient instances should be different")
		}
		if primary1.ID == primary2.ID {
			t.Errorf("expected different IDs, got %s and %s", primary1.ID, primary2.ID)
		}

		// Resolve secondary
		secondary1, err := godi.ResolveKeyed[*lifetimeTransientOnlyService](scope, "secondary")
		if err != nil {
			t.Fatalf("error resolving secondary: %v", err)
		}

		// Should have correct prefix
		if !strings.HasPrefix(primary1.ID, "primary-") {
			t.Errorf("primary should have 'primary-' prefix, got %s", primary1.ID)
		}
		if !strings.HasPrefix(secondary1.ID, "secondary-") {
			t.Errorf("secondary should have 'secondary-' prefix, got %s", secondary1.ID)
		}

		// Counts should be correct
		if atomic.LoadInt32(&primaryCount) != 2 {
			t.Errorf("expected 2 primary instances, got %d", primaryCount)
		}
		if atomic.LoadInt32(&secondaryCount) != 1 {
			t.Errorf("expected 1 secondary instance, got %d", secondaryCount)
		}
	})
}

// Test transient disposal
func TestTransientServices_Disposal(t *testing.T) {
	t.Run("transient services are disposed when scope closes", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		collection.AddTransient(func() *scopeTestDisposable {
			return &scopeTestDisposable{
				ID: fmt.Sprintf("transient-%d", time.Now().UnixNano()),
			}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		var instances []*scopeTestDisposable

		// Create scope and resolve multiple transients
		func() {
			scope := provider.CreateScope(context.Background())
			defer scope.Close()

			for i := 0; i < 5; i++ {
				inst, err := godi.Resolve[*scopeTestDisposable](scope)
				if err != nil {
					t.Fatalf("error resolving transient: %v", err)
				}
				instances = append(instances, inst)
			}

			// All should be different instances
			for i := 0; i < len(instances)-1; i++ {
				for j := i + 1; j < len(instances); j++ {
					if instances[i] == instances[j] {
						t.Errorf("instances %d and %d should be different", i, j)
					}
				}
			}

			// None should be disposed yet
			for i, inst := range instances {
				if inst.disposed {
					t.Errorf("instance %d should not be disposed yet", i)
				}
			}
		}() // Scope closes here

		// All instances should now be disposed
		for i, inst := range instances {
			if !inst.disposed {
				t.Errorf("instance %d should be disposed after scope close", i)
			}
		}
	})
}

// Test transient with complex constructors
func TestTransientServices_ComplexConstructors(t *testing.T) {
	t.Run("transient with multiple dependencies", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Register dependencies
		collection.AddSingleton(func() *lifetimeSingletonOnlyService {
			return &lifetimeSingletonOnlyService{ID: "singleton"}
		})

		collection.AddScoped(func() *lifetimeScopedOnlyService {
			return &lifetimeScopedOnlyService{ID: "scoped"}
		})

		// Complex transient with parameter object
		type transientParams struct {
			godi.In

			Singleton *lifetimeSingletonOnlyService
			Scoped    *lifetimeScopedOnlyService
			Context   context.Context
		}

		type complexTransient struct {
			ID          string
			SingletonID string
			ScopedID    string
			HasContext  bool
		}

		collection.AddTransient(func(params transientParams) *complexTransient {
			return &complexTransient{
				ID:          fmt.Sprintf("complex-%d", time.Now().UnixNano()),
				SingletonID: params.Singleton.ID,
				ScopedID:    params.Scoped.ID,
				HasContext:  params.Context != nil,
			}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		type ctxKey string
		const testKey ctxKey = "test"
		ctx := context.WithValue(context.Background(), testKey, "value")
		scope := provider.CreateScope(ctx)
		defer scope.Close()

		// Resolve multiple times
		inst1, err := godi.Resolve[*complexTransient](scope)
		if err != nil {
			t.Fatalf("error resolving complex transient: %v", err)
		}

		inst2, err := godi.Resolve[*complexTransient](scope)
		if err != nil {
			t.Fatalf("error resolving complex transient: %v", err)
		}

		// Should be different instances
		if inst1 == inst2 {
			t.Error("instances should be different")
		}
		if inst1.ID == inst2.ID {
			t.Error("instance IDs should be different")
		}

		// But should have same dependencies
		if inst1.SingletonID != inst2.SingletonID {
			t.Error("singleton dependency should be the same")
		}
		if inst1.ScopedID != inst2.ScopedID {
			t.Error("scoped dependency should be the same within scope")
		}

		// Should have context
		if !inst1.HasContext || !inst2.HasContext {
			t.Error("context should be available")
		}
	})
}

// Test transient with result objects
func TestTransientServices_ResultObjects(t *testing.T) {
	t.Run("transient with result object", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		type transientResult struct {
			godi.Out

			Service1 *lifetimeTransientOnlyService
			Service2 *lifetimeTransientOnlyService `name:"special"`
		}

		var counter int32
		collection.AddTransient(func() transientResult {
			count := atomic.AddInt32(&counter, 1)
			return transientResult{
				Service1: &lifetimeTransientOnlyService{
					ID: fmt.Sprintf("service1-%d", count),
				},
				Service2: &lifetimeTransientOnlyService{
					ID: fmt.Sprintf("service2-%d", count),
				},
			}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		// Resolve the services
		svc1_1, err := godi.Resolve[*lifetimeTransientOnlyService](scope)
		if err != nil {
			t.Fatalf("error resolving service1: %v", err)
		}

		svc1_2, err := godi.Resolve[*lifetimeTransientOnlyService](scope)
		if err != nil {
			t.Fatalf("error resolving service1 again: %v", err)
		}

		// Should be different instances (transient)
		if svc1_1 == svc1_2 {
			t.Error("transient result object services should create new instances")
		}

		// Resolve keyed service
		svcSpecial, err := godi.ResolveKeyed[*lifetimeTransientOnlyService](scope, "special")
		if err != nil {
			t.Fatalf("error resolving special service: %v", err)
		}

		if svcSpecial == nil {
			t.Error("special service should not be nil")
		}

		// Counter should reflect all invocations
		expectedCount := int32(3) // Two regular + one keyed
		if atomic.LoadInt32(&counter) != expectedCount {
			t.Errorf("expected %d invocations, got %d", expectedCount, counter)
		}
	})
}

// Benchmark tests for different lifetime resolution patterns
func BenchmarkLifetimeDependencies_TransientResolution(b *testing.B) {
	collection := godi.NewServiceCollection()
	collection.AddTransient(newLifetimeTransientOnlyService)

	provider, _ := collection.BuildServiceProvider()
	defer provider.Close()

	scope := provider.CreateScope(context.Background())
	defer scope.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = godi.Resolve[*lifetimeTransientOnlyService](scope)
	}
}

func BenchmarkLifetimeDependencies_ComplexChain(b *testing.B) {
	collection := godi.NewServiceCollection()

	collection.AddSingleton(newLifetimeSingletonOnlyService)
	collection.AddScoped(newLifetimeScopedOnlyService)
	collection.AddTransient(newLifetimeTransientService)

	provider, _ := collection.BuildServiceProvider()
	defer provider.Close()

	scope := provider.CreateScope(context.Background())
	defer scope.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = godi.Resolve[*lifetimeTransientService](scope)
	}
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
