package lifetime_test

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/junioryono/godi/v3/internal/lifetime"
	"github.com/junioryono/godi/v3/internal/registry"
)

// Test types
type TestService struct {
	ID       string
	Disposed bool
}

func (s *TestService) Dispose() error {
	s.Disposed = true
	return nil
}

type TestServiceWithContext struct {
	ID       string
	Disposed bool
	Context  context.Context
}

func (s *TestServiceWithContext) Dispose(ctx context.Context) error {
	s.Disposed = true
	s.Context = ctx
	return nil
}

type NonDisposableService struct {
	ID string
}

func TestLifetimeManager_TrackSingleton(t *testing.T) {
	manager := lifetime.New()
	defer manager.Dispose()

	service := &TestService{ID: "test-singleton"}
	serviceType := reflect.TypeOf((*TestService)(nil))

	// Track singleton
	err := manager.Track(service, serviceType, nil, registry.Singleton, "")
	if err != nil {
		t.Fatalf("Failed to track singleton: %v", err)
	}

	// Access singleton
	instance, ok := manager.Access(serviceType, nil, registry.Singleton, "")
	if !ok {
		t.Error("Failed to access singleton")
	}

	if instance != service {
		t.Error("Expected same singleton instance")
	}

	// Check statistics
	stats := manager.GetStatistics()
	if stats.TotalInstances != 1 {
		t.Errorf("Expected 1 total instance, got %d", stats.TotalInstances)
	}

	if stats.ActiveInstances != 1 {
		t.Errorf("Expected 1 active instance, got %d", stats.ActiveInstances)
	}
}

func TestLifetimeManager_TrackScoped(t *testing.T) {
	manager := lifetime.New()
	defer manager.Dispose()

	service1 := &TestService{ID: "scoped-1"}
	service2 := &TestService{ID: "scoped-2"}
	serviceType := reflect.TypeOf((*TestService)(nil))

	// Track in scope1
	err := manager.Track(service1, serviceType, nil, registry.Scoped, "scope1")
	if err != nil {
		t.Fatalf("Failed to track scoped service: %v", err)
	}

	// Track in scope2
	err = manager.Track(service2, serviceType, nil, registry.Scoped, "scope2")
	if err != nil {
		t.Fatalf("Failed to track scoped service: %v", err)
	}

	// Access from scope1
	instance1, ok := manager.Access(serviceType, nil, registry.Scoped, "scope1")
	if !ok {
		t.Error("Failed to access scoped service from scope1")
	}

	if instance1 != service1 {
		t.Error("Expected service1 from scope1")
	}

	// Access from scope2
	instance2, ok := manager.Access(serviceType, nil, registry.Scoped, "scope2")
	if !ok {
		t.Error("Failed to access scoped service from scope2")
	}

	if instance2 != service2 {
		t.Error("Expected service2 from scope2")
	}

	// Check statistics
	stats := manager.GetStatistics()
	if stats.TotalInstances != 2 {
		t.Errorf("Expected 2 total instances, got %d", stats.TotalInstances)
	}
}

func TestLifetimeManager_TrackTransient(t *testing.T) {
	manager := lifetime.New()
	defer manager.Dispose()

	service := &TestService{ID: "transient"}
	serviceType := reflect.TypeOf((*TestService)(nil))

	// Track transient
	err := manager.Track(service, serviceType, nil, registry.Transient, "scope1")
	if err != nil {
		t.Fatalf("Failed to track transient: %v", err)
	}

	// Access should fail (transients are not cached)
	_, ok := manager.Access(serviceType, nil, registry.Transient, "scope1")
	if ok {
		t.Error("Transient should not be accessible (not cached)")
	}

	// But it should still be tracked for disposal
	stats := manager.GetStatistics()
	if stats.TotalInstances != 1 {
		t.Errorf("Expected 1 total instance, got %d", stats.TotalInstances)
	}
}

func TestLifetimeManager_DisposeSingletons(t *testing.T) {
	manager := lifetime.New()

	service1 := &TestService{ID: "singleton-1"}
	service2 := &TestService{ID: "singleton-2"}

	type1 := reflect.TypeOf((*TestService)(nil))
	type2 := reflect.TypeOf((*TestService)(nil))

	// Track singletons
	manager.Track(service1, type1, "key1", registry.Singleton, "")
	manager.Track(service2, type2, "key2", registry.Singleton, "")

	// Dispose singletons
	err := manager.DisposeSingletons()
	if err != nil {
		t.Fatalf("Failed to dispose singletons: %v", err)
	}

	// Check services were disposed
	if !service1.Disposed {
		t.Error("Service1 should be disposed")
	}

	if !service2.Disposed {
		t.Error("Service2 should be disposed")
	}

	// Check statistics
	stats := manager.GetStatistics()
	if stats.DisposedInstances != 2 {
		t.Errorf("Expected 2 disposed instances, got %d", stats.DisposedInstances)
	}

	if stats.ActiveInstances != 0 {
		t.Errorf("Expected 0 active instances, got %d", stats.ActiveInstances)
	}
}

func TestLifetimeManager_DisposeScope(t *testing.T) {
	manager := lifetime.New()
	defer manager.Dispose()

	// Create services in a scope
	service1 := &TestService{ID: "scoped-1"}
	service2 := &TestService{ID: "scoped-2"}

	serviceType := reflect.TypeOf((*TestService)(nil))

	// Track in scope
	manager.Track(service1, serviceType, "key1", registry.Scoped, "test-scope")
	manager.Track(service2, serviceType, "key2", registry.Scoped, "test-scope")

	// Dispose the scope
	err := manager.DisposeScope("test-scope")
	if err != nil {
		t.Fatalf("Failed to dispose scope: %v", err)
	}

	// Check services were disposed
	if !service1.Disposed {
		t.Error("Service1 should be disposed")
	}

	if !service2.Disposed {
		t.Error("Service2 should be disposed")
	}

	// Access should fail after disposal
	_, ok := manager.Access(serviceType, "key1", registry.Scoped, "test-scope")
	if ok {
		t.Error("Should not be able to access disposed scope")
	}
}

// func TestLifetimeManager_DisposalOrder(t *testing.T) {
// 	manager := lifetime.New()
// 	defer manager.Dispose()

// 	var disposalOrder []string
// 	var mu sync.Mutex

// 	// Create services that track disposal order
// 	createService := func(id string) *TestService {
// 		return &TestService{
// 			ID: id,
// 			Dispose: func() error {
// 				mu.Lock()
// 				disposalOrder = append(disposalOrder, id)
// 				mu.Unlock()
// 				return nil
// 			},
// 		}
// 	}

// 	// Track services in order: A, B, C
// 	serviceA := createService("A")
// 	serviceB := createService("B")
// 	serviceC := createService("C")

// 	serviceType := reflect.TypeOf((*TestService)(nil))

// 	manager.Track(serviceA, serviceType, "A", registry.Scoped, "scope")
// 	manager.Track(serviceB, serviceType, "B", registry.Scoped, "scope")
// 	manager.Track(serviceC, serviceType, "C", registry.Scoped, "scope")

// 	// Dispose scope
// 	manager.DisposeScope("scope")

// 	// Check disposal order (should be LIFO: C, B, A)
// 	expected := []string{"C", "B", "A"}
// 	if len(disposalOrder) != len(expected) {
// 		t.Fatalf("Expected %d disposals, got %d", len(expected), len(disposalOrder))
// 	}

// 	for i, id := range expected {
// 		if disposalOrder[i] != id {
// 			t.Errorf("Expected disposal order %v, got %v", expected, disposalOrder)
// 			break
// 		}
// 	}
// }

func TestLifetimeManager_ChildScopes(t *testing.T) {
	manager := lifetime.New()
	defer manager.Dispose()

	// Create parent scope
	_ = manager.CreateScope("parent", context.Background())

	// Create child scope
	_, err := manager.CreateChildScope("parent", "child", context.Background())
	if err != nil {
		t.Fatalf("Failed to create child scope: %v", err)
	}

	// Track services
	parentService := &TestService{ID: "parent-service"}
	childService := &TestService{ID: "child-service"}

	serviceType := reflect.TypeOf((*TestService)(nil))

	manager.Track(parentService, serviceType, nil, registry.Scoped, "parent")
	manager.Track(childService, serviceType, nil, registry.Scoped, "child")

	// Dispose parent (should also dispose child)
	err = manager.DisposeScope("parent")
	if err != nil {
		t.Fatalf("Failed to dispose parent scope: %v", err)
	}

	// Both services should be disposed
	if !parentService.Disposed {
		t.Error("Parent service should be disposed")
	}

	if !childService.Disposed {
		t.Error("Child service should be disposed when parent is disposed")
	}
}

func TestLifetimeManager_DisposableWithContext(t *testing.T) {
	manager := lifetime.New()
	defer manager.Dispose()

	ctx := context.WithValue(context.Background(), "test", "value")
	service := &TestServiceWithContext{ID: "ctx-service"}

	serviceType := reflect.TypeOf((*TestServiceWithContext)(nil))

	// Create scope with context
	_ = manager.CreateScope("test-scope", ctx)

	// Track service
	manager.Track(service, serviceType, nil, registry.Scoped, "test-scope")

	// Dispose scope
	manager.DisposeScope("test-scope")

	// Check service was disposed with context
	if !service.Disposed {
		t.Error("Service should be disposed")
	}

	if service.Context == nil {
		t.Error("Context should be passed to Dispose")
	}

	// Check context value was preserved
	if val := service.Context.Value("test"); val != "value" {
		t.Error("Context value should be preserved")
	}
}

func TestLifetimeManager_NonDisposable(t *testing.T) {
	manager := lifetime.New()
	defer manager.Dispose()

	// Track non-disposable service
	service := &NonDisposableService{ID: "non-disposable"}
	serviceType := reflect.TypeOf((*NonDisposableService)(nil))

	err := manager.Track(service, serviceType, nil, registry.Singleton, "")
	if err != nil {
		t.Fatalf("Failed to track non-disposable: %v", err)
	}

	// Disposal should succeed (no-op for non-disposable)
	err = manager.DisposeSingletons()
	if err != nil {
		t.Errorf("Disposal should succeed for non-disposable: %v", err)
	}
}

func TestLifetimeManager_ConcurrentAccess(t *testing.T) {
	manager := lifetime.New()
	defer manager.Dispose()

	service := &TestService{ID: "concurrent"}
	serviceType := reflect.TypeOf((*TestService)(nil))

	// Track service
	manager.Track(service, serviceType, nil, registry.Singleton, "")

	// Concurrent access
	var wg sync.WaitGroup
	accessCount := int64(0)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			instance, ok := manager.Access(serviceType, nil, registry.Singleton, "")
			if ok && instance == service {
				atomic.AddInt64(&accessCount, 1)
			}
		}()
	}

	wg.Wait()

	if accessCount != 100 {
		t.Errorf("Expected 100 successful accesses, got %d", accessCount)
	}

	// Check access count in statistics
	stats := manager.GetStatistics()
	if stats.TotalAccessCount < 100 {
		t.Errorf("Expected at least 100 total accesses, got %d", stats.TotalAccessCount)
	}
}

func TestScopeManager_CreateAndDispose(t *testing.T) {
	lifetimeManager := lifetime.New()
	defer lifetimeManager.Dispose()

	scopeManager := lifetime.NewScopeManager(lifetimeManager, nil)
	defer scopeManager.Dispose()

	// Create scope
	scope, err := scopeManager.CreateScope(context.Background())
	if err != nil {
		t.Fatalf("Failed to create scope: %v", err)
	}

	if scope.IsDisposed() {
		t.Error("New scope should not be disposed")
	}

	// Get scope
	retrieved, err := scopeManager.GetScope(scope.ID)
	if err != nil {
		t.Fatalf("Failed to get scope: %v", err)
	}

	if retrieved != scope {
		t.Error("Retrieved scope should be same instance")
	}

	// Dispose scope
	err = scopeManager.DisposeScope(scope.ID)
	if err != nil {
		t.Fatalf("Failed to dispose scope: %v", err)
	}

	if !scope.IsDisposed() {
		t.Error("Scope should be disposed")
	}

	// Getting disposed scope should fail
	_, err = scopeManager.GetScope(scope.ID)
	if err == nil {
		t.Error("Should not be able to get disposed scope")
	}
}

func TestScopeManager_ChildScopes(t *testing.T) {
	lifetimeManager := lifetime.New()
	defer lifetimeManager.Dispose()

	scopeManager := lifetime.NewScopeManager(lifetimeManager, nil)
	defer scopeManager.Dispose()

	// Create parent scope
	parent, err := scopeManager.CreateScope(context.Background())
	if err != nil {
		t.Fatalf("Failed to create parent scope: %v", err)
	}

	// Create child scope
	child, err := parent.CreateChild(context.Background())
	if err != nil {
		t.Fatalf("Failed to create child scope: %v", err)
	}

	if child.GetParent() != parent {
		t.Error("Child should have correct parent")
	}

	// Dispose parent (should also dispose child)
	err = scopeManager.DisposeScope(parent.ID)
	if err != nil {
		t.Fatalf("Failed to dispose parent: %v", err)
	}

	if !child.IsDisposed() {
		t.Error("Child should be disposed when parent is disposed")
	}
}

func TestScopeManager_RootScope(t *testing.T) {
	lifetimeManager := lifetime.New()
	defer lifetimeManager.Dispose()

	scopeManager := lifetime.NewScopeManager(lifetimeManager, nil)
	defer scopeManager.Dispose()

	root := scopeManager.GetRootScope()
	if root == nil {
		t.Fatal("Root scope should exist")
	}

	if root.ID != "root" {
		t.Errorf("Root scope should have ID 'root', got %s", root.ID)
	}

	if root.GetParent() != nil {
		t.Error("Root scope should have no parent")
	}

	// Root scope should not be disposable through DisposeScope
	err := scopeManager.DisposeScope("root")
	if err == nil {
		t.Error("Should not be able to dispose root scope directly")
	}
}
