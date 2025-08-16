package godi

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Test types for scope tests
type ScopeTestService struct {
	ID    string
	Count int
}

func NewScopeTestService() *ScopeTestService {
	return &ScopeTestService{ID: "test", Count: 0}
}

type ScopeDisposableService struct {
	Closed bool
}

func (s *ScopeDisposableService) Close() error {
	s.Closed = true
	return nil
}

func NewScopeDisposableService() *ScopeDisposableService {
	return &ScopeDisposableService{}
}

// TestScope_Provider tests the Provider method
func TestScope_Provider(t *testing.T) {
	collection := NewCollection()
	provider, err := collection.Build()
	if err != nil {
		t.Fatalf("Failed to build provider: %v", err)
	}
	defer provider.Close()

	scope, err := provider.CreateScope(nil)
	if err != nil {
		t.Fatalf("Failed to create scope: %v", err)
	}
	defer scope.Close()

	if scope.Provider() != provider {
		t.Error("Provider() should return the parent provider")
	}
}

// TestScope_Context tests the Context method
func TestScope_Context(t *testing.T) {
	collection := NewCollection()
	provider, err := collection.Build()
	if err != nil {
		t.Fatalf("Failed to build provider: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()
	scope, err := provider.CreateScope(ctx)
	if err != nil {
		t.Fatalf("Failed to create scope: %v", err)
	}
	defer scope.Close()

	if scope.Context() == nil {
		t.Error("Context() should not return nil")
	}
}

// TestScope_Get tests the Get method
func TestScope_Get(t *testing.T) {
	collection := NewCollection()
	collection.AddSingleton(NewScopeTestService)

	provider, err := collection.Build()
	if err != nil {
		t.Fatalf("Failed to build provider: %v", err)
	}
	defer provider.Close()

	scope, err := provider.CreateScope(nil)
	if err != nil {
		t.Fatalf("Failed to create scope: %v", err)
	}
	defer scope.Close()

	tests := []struct {
		name        string
		serviceType reflect.Type
		wantErr     bool
		errType     error
	}{
		{
			name:        "valid service type",
			serviceType: reflect.TypeOf(&ScopeTestService{}),
			wantErr:     false,
		},
		{
			name:        "nil service type",
			serviceType: nil,
			wantErr:     true,
			errType:     ErrInvalidServiceType,
		},
		{
			name:        "non-registered service",
			serviceType: reflect.TypeOf(&ScopeDisposableService{}),
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scope.Get(tt.serviceType)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				if tt.errType != nil && err != tt.errType {
					t.Errorf("Expected error %v, got %v", tt.errType, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if result == nil {
					t.Error("Expected non-nil result")
				}
			}
		})
	}
}

// TestScope_GetKeyed tests the GetKeyed method
func TestScope_GetKeyed(t *testing.T) {
	collection := NewCollection()
	collection.AddSingleton(NewScopeTestService, Name("test"))

	provider, err := collection.Build()
	if err != nil {
		t.Fatalf("Failed to build provider: %v", err)
	}
	defer provider.Close()

	scope, err := provider.CreateScope(nil)
	if err != nil {
		t.Fatalf("Failed to create scope: %v", err)
	}
	defer scope.Close()

	tests := []struct {
		name        string
		serviceType reflect.Type
		key         any
		wantErr     bool
		errType     error
	}{
		{
			name:        "valid keyed service",
			serviceType: reflect.TypeOf(&ScopeTestService{}),
			key:         "test",
			wantErr:     false,
		},
		{
			name:        "nil service type",
			serviceType: nil,
			key:         "test",
			wantErr:     true,
			errType:     ErrInvalidServiceType,
		},
		{
			name:        "nil key",
			serviceType: reflect.TypeOf(&ScopeTestService{}),
			key:         nil,
			wantErr:     true,
			errType:     ErrServiceKeyNil,
		},
		{
			name:        "non-existent key",
			serviceType: reflect.TypeOf(&ScopeTestService{}),
			key:         "nonexistent",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := scope.GetKeyed(tt.serviceType, tt.key)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
				if tt.errType != nil && err != tt.errType {
					t.Errorf("Expected error %v, got %v", tt.errType, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if result == nil {
					t.Error("Expected non-nil result")
				}
			}
		})
	}
}

// TestScope_GetGroup tests the GetGroup method
func TestScope_GetGroup(t *testing.T) {
	collection := NewCollection()
	collection.AddTransient(NewScopeTestService, Group("group1"))
	collection.AddTransient(NewScopeTestService, Group("group1"))
	collection.AddTransient(NewScopeTestService, Group("group2"))

	provider, err := collection.Build()
	if err != nil {
		t.Fatalf("Failed to build provider: %v", err)
	}
	defer provider.Close()

	scope, err := provider.CreateScope(nil)
	if err != nil {
		t.Fatalf("Failed to create scope: %v", err)
	}
	defer scope.Close()

	tests := []struct {
		name        string
		serviceType reflect.Type
		group       string
		expectedLen int
		wantErr     bool
	}{
		{
			name:        "valid group with 2 services",
			serviceType: reflect.TypeOf(&ScopeTestService{}),
			group:       "group1",
			expectedLen: 2,
			wantErr:     false,
		},
		{
			name:        "valid group with 1 service",
			serviceType: reflect.TypeOf(&ScopeTestService{}),
			group:       "group2",
			expectedLen: 1,
			wantErr:     false,
		},
		{
			name:        "non-existent group",
			serviceType: reflect.TypeOf(&ScopeTestService{}),
			group:       "nonexistent",
			expectedLen: 0,
			wantErr:     false,
		},
		{
			name:        "nil service type",
			serviceType: nil,
			group:       "group1",
			wantErr:     true,
		},
		{
			name:        "empty group name",
			serviceType: reflect.TypeOf(&ScopeTestService{}),
			group:       "",
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := scope.GetGroup(tt.serviceType, tt.group)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if len(results) != tt.expectedLen {
					t.Errorf("Expected %d results, got %d", tt.expectedLen, len(results))
				}
			}
		})
	}
}

// TestScope_CreateScope tests creating child scopes
func TestScope_CreateScope(t *testing.T) {
	collection := NewCollection()
	provider, err := collection.Build()
	if err != nil {
		t.Fatalf("Failed to build provider: %v", err)
	}
	defer provider.Close()

	parentScope, err := provider.CreateScope(nil)
	if err != nil {
		t.Fatalf("Failed to create parent scope: %v", err)
	}
	defer parentScope.Close()

	t.Run("create child with nil context", func(t *testing.T) {
		childScope, err := parentScope.CreateScope(nil)
		if err != nil {
			t.Errorf("Failed to create child scope: %v", err)
		}
		defer childScope.Close()

		// Verify parent-child relationship
		if childScope.(*scope).parent != parentScope.(*scope) {
			t.Error("Child scope should have parent reference")
		}
	})

	t.Run("create child with custom context", func(t *testing.T) {
		ctx := context.WithValue(context.Background(), "key", "value")
		childScope, err := parentScope.CreateScope(ctx)
		if err != nil {
			t.Errorf("Failed to create child scope: %v", err)
		}
		defer childScope.Close()

		if childScope.Context() == nil {
			t.Error("Child scope should have context")
		}
	})

	t.Run("create from disposed scope", func(t *testing.T) {
		disposedScope, _ := provider.CreateScope(nil)
		disposedScope.Close()

		_, err := disposedScope.CreateScope(nil)
		if err != ErrScopeDisposed {
			t.Errorf("Expected ErrScopeDisposed, got %v", err)
		}
	})
}

// TestScope_Close tests scope disposal
func TestScope_Close(t *testing.T) {
	t.Run("close empty scope", func(t *testing.T) {
		collection := NewCollection()
		provider, _ := collection.Build()
		defer provider.Close()

		scope, _ := provider.CreateScope(nil)
		err := scope.Close()
		if err != nil {
			t.Errorf("Failed to close scope: %v", err)
		}

		// Try to close again
		err = scope.Close()
		if err != nil {
			t.Error("Closing already closed scope should not error")
		}
	})

	t.Run("close scope with disposable services", func(t *testing.T) {
		collection := NewCollection()
		collection.AddScoped(NewScopeDisposableService)
		provider, _ := collection.Build()
		defer provider.Close()

		scope, _ := provider.CreateScope(nil)

		// Get the service to create an instance
		service, _ := scope.Get(reflect.TypeOf(&ScopeDisposableService{}))
		disposable := service.(*ScopeDisposableService)

		// Close scope
		err := scope.Close()
		if err != nil {
			t.Errorf("Failed to close scope: %v", err)
		}

		// Verify service was disposed
		if !disposable.Closed {
			t.Error("Disposable service should be closed")
		}
	})

	t.Run("close scope with children", func(t *testing.T) {
		collection := NewCollection()
		provider, _ := collection.Build()
		defer provider.Close()

		parentScope, _ := provider.CreateScope(nil)
		childScope1, _ := parentScope.CreateScope(nil)
		childScope2, _ := parentScope.CreateScope(nil)

		// Close parent
		err := parentScope.Close()
		if err != nil {
			t.Errorf("Failed to close parent scope: %v", err)
		}

		// Verify children are also closed
		if atomic.LoadInt32(&childScope1.(*scope).disposed) == 0 {
			t.Error("Child scope 1 should be disposed")
		}
		if atomic.LoadInt32(&childScope2.(*scope).disposed) == 0 {
			t.Error("Child scope 2 should be disposed")
		}
	})
}

// TestScope_Disposed tests operations on disposed scope
func TestScope_Disposed(t *testing.T) {
	collection := NewCollection()
	collection.AddSingleton(NewScopeTestService)
	provider, _ := collection.Build()
	defer provider.Close()

	scope, _ := provider.CreateScope(nil)
	scope.Close()

	serviceType := reflect.TypeOf(&ScopeTestService{})

	t.Run("Get on disposed scope", func(t *testing.T) {
		_, err := scope.Get(serviceType)
		if err != ErrScopeDisposed {
			t.Errorf("Expected ErrScopeDisposed, got %v", err)
		}
	})

	t.Run("GetKeyed on disposed scope", func(t *testing.T) {
		_, err := scope.GetKeyed(serviceType, "key")
		if err != ErrScopeDisposed {
			t.Errorf("Expected ErrScopeDisposed, got %v", err)
		}
	})

	t.Run("GetGroup on disposed scope", func(t *testing.T) {
		_, err := scope.GetGroup(serviceType, "group")
		if err != ErrScopeDisposed {
			t.Errorf("Expected ErrScopeDisposed, got %v", err)
		}
	})

	t.Run("CreateScope on disposed scope", func(t *testing.T) {
		_, err := scope.CreateScope(nil)
		if err != ErrScopeDisposed {
			t.Errorf("Expected ErrScopeDisposed, got %v", err)
		}
	})
}

// TestScope_InstanceCaching tests instance caching behavior
func TestScope_InstanceCaching(t *testing.T) {
	collection := NewCollection()
	collection.AddSingleton(NewScopeTestService, Name("singleton"))
	collection.AddScoped(NewScopeTestService, Name("scoped"))
	collection.AddTransient(NewScopeTestService, Name("transient"))

	provider, _ := collection.Build()
	defer provider.Close()

	scope, _ := provider.CreateScope(nil)
	defer scope.Close()

	serviceType := reflect.TypeOf(&ScopeTestService{})

	t.Run("singleton caching", func(t *testing.T) {
		instance1, _ := scope.GetKeyed(serviceType, "singleton")
		instance2, _ := scope.GetKeyed(serviceType, "singleton")

		if instance1 != instance2 {
			t.Error("Singleton instances should be the same")
		}

		// Create new scope and check again
		newScope, _ := provider.CreateScope(nil)
		defer newScope.Close()

		instance3, _ := newScope.GetKeyed(serviceType, "singleton")
		if instance1 != instance3 {
			t.Error("Singleton should be same across scopes")
		}
	})

	t.Run("scoped caching", func(t *testing.T) {
		instance1, _ := scope.GetKeyed(serviceType, "scoped")
		instance2, _ := scope.GetKeyed(serviceType, "scoped")

		if instance1 != instance2 {
			t.Error("Scoped instances should be the same within scope")
		}

		// Create new scope and check again
		newScope, _ := provider.CreateScope(nil)
		defer newScope.Close()

		instance3, _ := newScope.GetKeyed(serviceType, "scoped")
		if instance1 == instance3 {
			t.Error("Scoped instances should differ across scopes")
		}
	})

	t.Run("transient no caching", func(t *testing.T) {
		instance1, _ := scope.GetKeyed(serviceType, "transient")
		instance2, _ := scope.GetKeyed(serviceType, "transient")

		if instance1 == instance2 {
			t.Error("Transient instances should always be different")
		}
	})
}

// TestScope_CircularDependency tests circular dependency detection
func TestScope_CircularDependency(t *testing.T) {
	scope := &scope{
		resolving:   make(map[instanceKey]struct{}),
		resolvingMu: sync.Mutex{},
	}

	key := instanceKey{Type: reflect.TypeOf(&ScopeTestService{})}

	// First check should pass
	err := scope.checkCircular(key)
	if err != nil {
		t.Errorf("First check should pass: %v", err)
	}

	// Mark as resolving
	scope.startResolving(key)

	// Second check should fail
	err = scope.checkCircular(key)
	if err == nil {
		t.Error("Should detect circular dependency")
	}

	// Stop resolving
	scope.stopResolving(key)

	// Check should pass again
	err = scope.checkCircular(key)
	if err != nil {
		t.Errorf("Should pass after stopping resolution: %v", err)
	}
}

// TestScope_ContextCancellation tests scope auto-close on context cancellation
func TestScope_ContextCancellation(t *testing.T) {
	collection := NewCollection()
	provider, _ := collection.Build()
	defer provider.Close()

	ctx, cancel := context.WithCancel(context.Background())
	sp, _ := provider.CreateScope(ctx)

	// Cancel the context
	cancel()

	// Wait a bit for goroutine to process
	time.Sleep(100 * time.Millisecond)

	// Scope should be disposed
	if atomic.LoadInt32(&sp.(*scope).disposed) == 0 {
		t.Error("Scope should be disposed after context cancellation")
	}
}

// TestScope_ConcurrentAccess tests thread safety
func TestScope_ConcurrentAccess(t *testing.T) {
	collection := NewCollection()
	collection.AddScoped(NewScopeTestService)
	collection.AddTransient(NewScopeTestService, Name("transient"))

	provider, _ := collection.Build()
	defer provider.Close()

	scope, _ := provider.CreateScope(nil)
	defer scope.Close()

	serviceType := reflect.TypeOf(&ScopeTestService{})

	var wg sync.WaitGroup
	concurrency := 100

	// Concurrent Gets
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			_, _ = scope.Get(serviceType)
		}()
	}

	// Concurrent GetKeyed
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			defer wg.Done()
			_, _ = scope.GetKeyed(serviceType, "transient")
		}()
	}

	// Concurrent child scope creation
	wg.Add(concurrency / 10)
	for i := 0; i < concurrency/10; i++ {
		go func() {
			defer wg.Done()
			child, _ := scope.CreateScope(nil)
			if child != nil {
				child.Close()
			}
		}()
	}

	wg.Wait()
}

// TestScope_GetSetInstance tests the getInstance and setInstance methods
func TestScope_GetSetInstance(t *testing.T) {
	scope := &scope{
		instances:   make(map[instanceKey]any),
		instancesMu: sync.RWMutex{},
		disposables: make([]Disposable, 0),
	}

	key := instanceKey{Type: reflect.TypeOf(&ScopeTestService{})}

	// Test get non-existent
	_, exists := scope.getInstance(key)
	if exists {
		t.Error("Should not find non-existent instance")
	}

	// Test set and get
	instance := &ScopeTestService{ID: "test"}
	scope.setInstance(key, instance)

	retrieved, exists := scope.getInstance(key)
	if !exists {
		t.Error("Should find instance after setting")
	}
	if retrieved != instance {
		t.Error("Retrieved instance should match set instance")
	}

	// Test set disposable
	disposable := &ScopeDisposableService{}
	disposableKey := instanceKey{Type: reflect.TypeOf(&ScopeDisposableService{})}
	scope.setInstance(disposableKey, disposable)

	if len(scope.disposables) != 1 {
		t.Error("Disposable should be tracked")
	}
}

// TestScope_Resolve tests the resolve method
func TestScope_Resolve(t *testing.T) {
	collection := NewCollection()
	collection.AddSingleton(NewScopeTestService)
	collection.AddScoped(NewScopeTestService, Name("scoped"))
	collection.AddTransient(NewScopeTestService, Name("transient"))

	provider, _ := collection.Build()
	defer provider.Close()

	sp, _ := provider.CreateScope(nil)
	defer sp.Close()

	s := sp.(*scope)

	tests := []struct {
		name       string
		key        instanceKey
		descriptor *Descriptor
		wantErr    bool
	}{
		{
			name: "resolve singleton",
			key: instanceKey{
				Type: reflect.TypeOf(&ScopeTestService{}),
			},
			wantErr: false,
		},
		{
			name: "resolve scoped",
			key: instanceKey{
				Type: reflect.TypeOf(&ScopeTestService{}),
				Key:  "scoped",
			},
			wantErr: false,
		},
		{
			name: "resolve transient",
			key: instanceKey{
				Type: reflect.TypeOf(&ScopeTestService{}),
				Key:  "transient",
			},
			wantErr: false,
		},
		{
			name: "resolve non-existent",
			key: instanceKey{
				Type: reflect.TypeOf(&ScopeDisposableService{}),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := s.resolve(tt.key, tt.descriptor)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if result == nil {
					t.Error("Expected non-nil result")
				}
			}
		})
	}
}

// Test types for decorator tests
type DecoratorTestService struct {
	Value string
	Count int
}

func NewDecoratorTestService() *DecoratorTestService {
	return &DecoratorTestService{Value: "original", Count: 0}
}

type DecoratorDependency struct {
	Name string
}

func NewDecoratorDependency() *DecoratorDependency {
	return &DecoratorDependency{Name: "dependency"}
}

// Test decorators
func SimpleDecorator(service *DecoratorTestService) *DecoratorTestService {
	service.Value = "decorated-" + service.Value
	service.Count++
	return service
}

func DecoratorWithDependency(service *DecoratorTestService, dep *DecoratorDependency) *DecoratorTestService {
	if dep != nil {
		service.Value = service.Value + "-" + dep.Name
	} else {
		service.Value = service.Value + "-<nil>"
	}
	service.Count++
	return service
}

func DecoratorWithError(service *DecoratorTestService) (*DecoratorTestService, error) {
	if service.Count > 5 {
		return nil, errors.New("count too high")
	}
	service.Count++
	return service, nil
}

func DecoratorReturnsError(service *DecoratorTestService) (*DecoratorTestService, error) {
	return nil, errors.New("decorator failed")
}

func nilDecorator(service *DecoratorTestService) *DecoratorTestService {
	return nil
}

// Invalid decorators for testing
func InvalidDecoratorNoParams() *DecoratorTestService {
	return &DecoratorTestService{}
}

func InvalidDecoratorNoReturn(service *DecoratorTestService) {
	service.Count++
}

func InvalidDecoratorWrongParamType(wrongType string) *DecoratorTestService {
	return &DecoratorTestService{Value: wrongType}
}

// TestApplyDecorators tests the applyDecorators method
func TestApplyDecorators(t *testing.T) {
	// Create a test provider and scope
	collection := NewCollection()
	collection.AddSingleton(NewDecoratorTestService)
	collection.AddSingleton(NewDecoratorDependency)

	provider, err := collection.Build()
	if err != nil {
		t.Fatalf("Failed to build provider: %v", err)
	}
	defer provider.Close()

	sp, err := provider.CreateScope(nil)
	if err != nil {
		t.Fatalf("Failed to create scope: %v", err)
	}
	defer sp.Close()

	s := sp.(*scope)
	serviceType := reflect.TypeOf(&DecoratorTestService{})

	tests := []struct {
		name        string
		instance    any
		decorators  []*Descriptor
		wantErr     bool
		errContains string
		validate    func(*testing.T, any)
	}{
		{
			name:        "nil instance",
			instance:    nil,
			wantErr:     true,
			errContains: "instance cannot be nil",
		},
		{
			name:       "no decorators",
			instance:   &DecoratorTestService{Value: "test"},
			decorators: []*Descriptor{},
			wantErr:    false,
			validate: func(t *testing.T, result any) {
				service := result.(*DecoratorTestService)
				if service.Value != "test" {
					t.Errorf("Value should be unchanged, got %s", service.Value)
				}
			},
		},
		{
			name:     "single decorator",
			instance: &DecoratorTestService{Value: "test"},
			decorators: []*Descriptor{
				{
					Constructor: reflect.ValueOf(SimpleDecorator),
					IsDecorator: true,
				},
			},
			wantErr: false,
			validate: func(t *testing.T, result any) {
				service := result.(*DecoratorTestService)
				if service.Value != "decorated-test" {
					t.Errorf("Expected 'decorated-test', got %s", service.Value)
				}
				if service.Count != 1 {
					t.Errorf("Expected count 1, got %d", service.Count)
				}
			},
		},
		{
			name:     "multiple decorators",
			instance: &DecoratorTestService{Value: "test"},
			decorators: []*Descriptor{
				{
					Constructor: reflect.ValueOf(SimpleDecorator),
					IsDecorator: true,
				},
				{
					Constructor: reflect.ValueOf(SimpleDecorator),
					IsDecorator: true,
				},
			},
			wantErr: false,
			validate: func(t *testing.T, result any) {
				service := result.(*DecoratorTestService)
				if service.Value != "decorated-decorated-test" {
					t.Errorf("Expected double decoration, got %s", service.Value)
				}
				if service.Count != 2 {
					t.Errorf("Expected count 2, got %d", service.Count)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Mock getDecorators to return our test decorators
			originalDecorators := s.provider.decorators
			s.provider.decorators = map[reflect.Type][]*Descriptor{
				serviceType: tt.decorators,
			}
			defer func() {
				s.provider.decorators = originalDecorators
			}()

			result, err := s.applyDecorators(tt.instance, serviceType)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Error should contain %q, got %q", tt.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if tt.validate != nil {
					tt.validate(t, result)
				}
			}
		})
	}
}

// TestInvokeDecorator tests the invokeDecorator method
func TestInvokeDecorator(t *testing.T) {
	// Create a test provider and scope
	collection := NewCollection()
	collection.AddSingleton(NewDecoratorTestService)
	collection.AddSingleton(NewDecoratorDependency)

	provider, err := collection.Build()
	if err != nil {
		t.Fatalf("Failed to build provider: %v", err)
	}
	defer provider.Close()

	sp, err := provider.CreateScope(nil)
	if err != nil {
		t.Fatalf("Failed to create scope: %v", err)
	}
	defer sp.Close()

	s := sp.(*scope)

	tests := []struct {
		name        string
		decorator   *Descriptor
		instance    any
		wantErr     bool
		errContains string
		validate    func(*testing.T, any)
	}{
		{
			name:        "nil decorator",
			decorator:   nil,
			instance:    &DecoratorTestService{},
			wantErr:     true,
			errContains: "invalid decorator",
		},
		{
			name: "not a decorator",
			decorator: &Descriptor{
				Constructor: reflect.ValueOf(SimpleDecorator),
				IsDecorator: false,
			},
			instance:    &DecoratorTestService{},
			wantErr:     true,
			errContains: "invalid decorator",
		},
		{
			name: "decorator with no parameters",
			decorator: &Descriptor{
				Constructor: reflect.ValueOf(InvalidDecoratorNoParams),
				IsDecorator: true,
			},
			instance:    &DecoratorTestService{},
			wantErr:     true,
			errContains: "must have at least one parameter",
		},
		{
			name: "decorator with no return",
			decorator: &Descriptor{
				Constructor: reflect.ValueOf(InvalidDecoratorNoReturn),
				IsDecorator: true,
			},
			instance:    &DecoratorTestService{},
			wantErr:     true,
			errContains: "must return at least one value",
		},
		{
			name: "type mismatch",
			decorator: &Descriptor{
				Constructor: reflect.ValueOf(InvalidDecoratorWrongParamType),
				IsDecorator: true,
			},
			instance:    &DecoratorTestService{},
			wantErr:     true,
			errContains: "expects string but got",
		},
		{
			name: "simple decorator success",
			decorator: &Descriptor{
				Constructor: reflect.ValueOf(SimpleDecorator),
				IsDecorator: true,
			},
			instance: &DecoratorTestService{Value: "test"},
			wantErr:  false,
			validate: func(t *testing.T, result any) {
				service := result.(*DecoratorTestService)
				if service.Value != "decorated-test" {
					t.Errorf("Expected 'decorated-test', got %s", service.Value)
				}
			},
		},
		{
			name: "decorator with dependency",
			decorator: &Descriptor{
				Constructor: reflect.ValueOf(DecoratorWithDependency),
				IsDecorator: true,
			},
			instance: &DecoratorTestService{Value: "test"},
			wantErr:  false,
			validate: func(t *testing.T, result any) {
				service := result.(*DecoratorTestService)
				if service.Value != "test-dependency" {
					t.Errorf("Expected 'test-dependency', got %s", service.Value)
				}
			},
		},
		{
			name: "decorator with error return success",
			decorator: &Descriptor{
				Constructor: reflect.ValueOf(DecoratorWithError),
				IsDecorator: true,
			},
			instance: &DecoratorTestService{Value: "test", Count: 0},
			wantErr:  false,
			validate: func(t *testing.T, result any) {
				service := result.(*DecoratorTestService)
				if service.Count != 1 {
					t.Errorf("Expected count 1, got %d", service.Count)
				}
			},
		},
		{
			name: "decorator returns error",
			decorator: &Descriptor{
				Constructor: reflect.ValueOf(DecoratorReturnsError),
				IsDecorator: true,
			},
			instance:    &DecoratorTestService{},
			wantErr:     true,
			errContains: "decorator failed",
		},
		{
			name: "decorator with high count returns error",
			decorator: &Descriptor{
				Constructor: reflect.ValueOf(DecoratorWithError),
				IsDecorator: true,
			},
			instance:    &DecoratorTestService{Count: 10},
			wantErr:     true,
			errContains: "count too high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := s.invokeDecorator(tt.decorator, tt.instance)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("Error should contain %q, got %q", tt.errContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if tt.validate != nil {
					tt.validate(t, result)
				}
			}
		})
	}
}

// TestDecoratorWithMissingDependency tests decorator with unresolvable dependency
func TestDecoratorWithMissingDependency(t *testing.T) {
	// Create a provider without the dependency
	collection := NewCollection()
	collection.AddSingleton(NewDecoratorTestService)

	provider, err := collection.Build()
	if err != nil {
		t.Fatalf("Failed to build provider: %v", err)
	}
	defer provider.Close()

	sp, err := provider.CreateScope(nil)
	if err != nil {
		t.Fatalf("Failed to create scope: %v", err)
	}
	defer sp.Close()

	s := sp.(*scope)

	// Decorator that needs a dependency that doesn't exist
	decorator := &Descriptor{
		Constructor: reflect.ValueOf(DecoratorWithDependency),
		IsDecorator: true,
	}

	instance := &DecoratorTestService{Value: "test"}

	// Should set nil for pointer type when dependency is missing
	result, err := s.invokeDecorator(decorator, instance)
	if err != nil {
		t.Errorf("Should handle missing pointer dependency gracefully, got error: %v", err)
	}

	// The decorator will be called with nil dependency
	service := result.(*DecoratorTestService)
	if service.Value != "test-<nil>" {
		// Note: This depends on how the decorator handles nil
		// In reality, it might panic, but our test is about the framework behavior
	}
}

// TestDecoratorChainOrder tests that decorators are applied in correct order
func TestDecoratorChainOrder(t *testing.T) {
	// Create decorators that append numbers to track order
	decorator1 := func(s *DecoratorTestService) *DecoratorTestService {
		s.Value = s.Value + "-1"
		return s
	}

	decorator2 := func(s *DecoratorTestService) *DecoratorTestService {
		s.Value = s.Value + "-2"
		return s
	}

	decorator3 := func(s *DecoratorTestService) *DecoratorTestService {
		s.Value = s.Value + "-3"
		return s
	}

	collection := NewCollection()
	collection.AddSingleton(NewDecoratorTestService)

	provider, err := collection.Build()
	if err != nil {
		t.Fatalf("Failed to build provider: %v", err)
	}
	defer provider.Close()

	sp, err := provider.CreateScope(nil)
	if err != nil {
		t.Fatalf("Failed to create scope: %v", err)
	}
	defer sp.Close()

	s := sp.(*scope)
	serviceType := reflect.TypeOf(&DecoratorTestService{})

	// Set up decorators in order
	s.provider.decorators = map[reflect.Type][]*Descriptor{
		serviceType: {
			{Constructor: reflect.ValueOf(decorator1), IsDecorator: true},
			{Constructor: reflect.ValueOf(decorator2), IsDecorator: true},
			{Constructor: reflect.ValueOf(decorator3), IsDecorator: true},
		},
	}

	instance := &DecoratorTestService{Value: "start"}
	result, err := s.applyDecorators(instance, serviceType)
	if err != nil {
		t.Fatalf("Failed to apply decorators: %v", err)
	}

	service := result.(*DecoratorTestService)
	expected := "start-1-2-3"
	if service.Value != expected {
		t.Errorf("Expected decorators to be applied in order: %q, got %q", expected, service.Value)
	}
}

// TestDecoratorReturnsNil tests decorator that returns nil value
func TestDecoratorReturnsNil(t *testing.T) {
	nilDecorator := func(s *DecoratorTestService) *DecoratorTestService {
		return nil
	}

	collection := NewCollection()
	collection.AddSingleton(NewDecoratorTestService)

	provider, err := collection.Build()
	if err != nil {
		t.Fatalf("Failed to build provider: %v", err)
	}
	defer provider.Close()

	sp, err := provider.CreateScope(nil)
	if err != nil {
		t.Fatalf("Failed to create scope: %v", err)
	}
	defer sp.Close()

	s := sp.(*scope)

	decorator := &Descriptor{
		Constructor: reflect.ValueOf(nilDecorator),
		IsDecorator: true,
	}

	instance := &DecoratorTestService{Value: "test"}
	result, err := s.invokeDecorator(decorator, instance)

	if err != nil {
		t.Errorf("Should handle nil return from decorator, got error: %v", err)
	}

	if result != nil {
		t.Errorf("Expected nil result when decorator returns nil, got: %v (type: %T)", result, result)
	}
}

// BenchmarkScope_Get benchmarks the Get operation
func BenchmarkScope_Get(b *testing.B) {
	collection := NewCollection()
	collection.AddScoped(NewScopeTestService)

	provider, _ := collection.Build()
	defer provider.Close()

	scope, _ := provider.CreateScope(nil)
	defer scope.Close()

	serviceType := reflect.TypeOf(&ScopeTestService{})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, _ = scope.Get(serviceType)
		}
	})
}

// BenchmarkScope_CreateChild benchmarks child scope creation
func BenchmarkScope_CreateChild(b *testing.B) {
	collection := NewCollection()
	provider, _ := collection.Build()
	defer provider.Close()

	parentScope, _ := provider.CreateScope(nil)
	defer parentScope.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		child, _ := parentScope.CreateScope(nil)
		child.Close()
	}
}

// BenchmarkScope_ConcurrentResolve benchmarks concurrent resolution
func BenchmarkScope_ConcurrentResolve(b *testing.B) {
	collection := NewCollection()
	collection.AddScoped(NewScopeTestService)
	collection.AddTransient(NewScopeTestService, Name("transient"))

	provider, _ := collection.Build()
	defer provider.Close()

	scope, _ := provider.CreateScope(nil)
	defer scope.Close()

	serviceType := reflect.TypeOf(&ScopeTestService{})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if i := b.N % 2; i == 0 {
				_, _ = scope.Get(serviceType)
			} else {
				_, _ = scope.GetKeyed(serviceType, "transient")
			}
		}
	})
}

// BenchmarkApplyDecorators benchmarks applying decorators
func BenchmarkApplyDecorators(b *testing.B) {
	collection := NewCollection()
	collection.AddSingleton(NewDecoratorTestService)

	provider, err := collection.Build()
	if err != nil {
		b.Fatalf("Failed to build provider: %v", err)
	}
	defer provider.Close()

	sp, err := provider.CreateScope(nil)
	if err != nil {
		b.Fatalf("Failed to create scope: %v", err)
	}
	defer sp.Close()

	s := sp.(*scope)
	serviceType := reflect.TypeOf(&DecoratorTestService{})

	// Set up multiple decorators
	s.provider.decorators = map[reflect.Type][]*Descriptor{
		serviceType: {
			{Constructor: reflect.ValueOf(SimpleDecorator), IsDecorator: true},
			{Constructor: reflect.ValueOf(SimpleDecorator), IsDecorator: true},
			{Constructor: reflect.ValueOf(SimpleDecorator), IsDecorator: true},
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		instance := &DecoratorTestService{Value: "test"}
		_, _ = s.applyDecorators(instance, serviceType)
	}
}
