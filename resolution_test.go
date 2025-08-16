package godi

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/junioryono/godi/v3/internal/reflection"
)

// Test types for resolution tests
type ResolutionTestService struct {
	Name string
}

func NewResolutionTestService() *ResolutionTestService {
	return &ResolutionTestService{Name: "test"}
}

type ResolutionServiceWithDep struct {
	Service *ResolutionTestService
}

func NewResolutionServiceWithDep(service *ResolutionTestService) *ResolutionServiceWithDep {
	return &ResolutionServiceWithDep{Service: service}
}

type ResolutionServiceWithError struct{}

func NewResolutionServiceWithError() (*ResolutionServiceWithError, error) {
	return nil, errors.New("construction error")
}

// Result object for testing
type ResolutionResult struct {
	Out

	Service *ResolutionTestService
	Other   *ResolutionServiceWithDep
}

func NewResolutionWithResult(service *ResolutionTestService) ResolutionResult {
	return ResolutionResult{
		Service: service,
		Other:   &ResolutionServiceWithDep{Service: service},
	}
}

// TestDependencyResolver tests the dependencyResolver adapter
func TestDependencyResolver(t *testing.T) {
	// Create a test provider and scope
	collection := NewCollection()
	collection.AddSingleton(NewResolutionTestService)
	collection.AddScoped(NewResolutionServiceWithDep)
	collection.AddTransient(NewResolutionTestService, Name("named"))
	collection.AddTransient(NewResolutionTestService, Group("group1"))
	collection.AddTransient(NewResolutionTestService, Group("group1"))

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
	ctx := &resolutionContext{
		scope:    s,
		stack:    make([]instanceKey, 0),
		depth:    0,
		maxDepth: 100,
	}
	resolver := dependencyResolver{scope: s, ctx: ctx}

	t.Run("Resolve", func(t *testing.T) {
		serviceType := reflect.TypeOf(&ResolutionTestService{})
		result, err := resolver.Resolve(serviceType)
		if err != nil {
			t.Errorf("Failed to resolve: %v", err)
		}
		if result == nil {
			t.Error("Expected non-nil result")
		}
		if service, ok := result.(*ResolutionTestService); ok {
			if service.Name != "test" {
				t.Errorf("Expected name 'test', got %s", service.Name)
			}
		} else {
			t.Error("Wrong type returned")
		}
	})

	t.Run("ResolveKeyed", func(t *testing.T) {
		serviceType := reflect.TypeOf(&ResolutionTestService{})
		result, err := resolver.ResolveKeyed(serviceType, "named")
		if err != nil {
			t.Errorf("Failed to resolve keyed: %v", err)
		}
		if result == nil {
			t.Error("Expected non-nil result")
		}
	})

	t.Run("ResolveGroup", func(t *testing.T) {
		serviceType := reflect.TypeOf(&ResolutionTestService{})
		results, err := resolver.ResolveGroup(serviceType, "group1")
		if err != nil {
			t.Errorf("Failed to resolve group: %v", err)
		}
		if len(results) != 2 {
			t.Errorf("Expected 2 results in group, got %d", len(results))
		}
	})
}

// TestCreateInstance tests the createInstance method
func TestCreateInstance(t *testing.T) {
	tests := []struct {
		name        string
		setup       func() (*scope, *Descriptor)
		wantErr     bool
		errContains string
		validate    func(*testing.T, any)
	}{
		{
			name: "nil descriptor",
			setup: func() (*scope, *Descriptor) {
				collection := NewCollection()
				provider, _ := collection.Build()
				sp, _ := provider.CreateScope(nil)
				return sp.(*scope), nil
			},
			wantErr:     true,
			errContains: "descriptor cannot be nil",
		},
		{
			name: "simple constructor",
			setup: func() (*scope, *Descriptor) {
				collection := NewCollection()
				collection.AddSingleton(NewResolutionTestService)
				provider, _ := collection.Build()
				sp, _ := provider.CreateScope(nil)

				descriptor, _ := NewDescriptor(NewResolutionTestService, Transient)
				return sp.(*scope), descriptor
			},
			wantErr: false,
			validate: func(t *testing.T, result any) {
				service, ok := result.(*ResolutionTestService)
				if !ok {
					t.Error("Wrong type returned")
				}
				if service.Name != "test" {
					t.Errorf("Expected name 'test', got %s", service.Name)
				}
			},
		},
		{
			name: "constructor with dependencies",
			setup: func() (*scope, *Descriptor) {
				collection := NewCollection()
				collection.AddSingleton(NewResolutionTestService)
				collection.AddScoped(NewResolutionServiceWithDep)
				provider, _ := collection.Build()
				sp, _ := provider.CreateScope(nil)

				descriptor, _ := NewDescriptor(NewResolutionServiceWithDep, Transient)
				return sp.(*scope), descriptor
			},
			wantErr: false,
			validate: func(t *testing.T, result any) {
				service, ok := result.(*ResolutionServiceWithDep)
				if !ok {
					t.Error("Wrong type returned")
				}
				if service.Service == nil {
					t.Error("Dependency not injected")
				}
				if service.Service.Name != "test" {
					t.Errorf("Expected dependency name 'test', got %s", service.Service.Name)
				}
			},
		},
		{
			name: "constructor returns error",
			setup: func() (*scope, *Descriptor) {
				collection := NewCollection()
				// Use Transient instead of Singleton to avoid build-time failure
				collection.AddTransient(NewResolutionServiceWithError)
				provider, err := collection.Build()
				if err != nil {
					t.Fatalf("Failed to build provider: %v", err)
				}
				sp, err := provider.CreateScope(nil)
				if err != nil {
					t.Fatalf("Failed to create scope: %v", err)
				}

				descriptor, _ := NewDescriptor(NewResolutionServiceWithError, Transient)
				return sp.(*scope), descriptor
			},
			wantErr:     true,
			errContains: "construction error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sp, descriptor := tt.setup()
			if sp != nil {
				defer sp.Close()
			}

			result, err := sp.createInstance(descriptor)

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

// // TestCreateInstanceWithResultObject tests createInstance with result objects
// func TestCreateInstanceWithResultObject(t *testing.T) {
// 	collection := NewCollection()
// 	collection.AddSingleton(NewResolutionTestService)

// 	// Manually create a descriptor with result object info
// 	constructor := reflect.ValueOf(NewResolutionWithResult)
// 	descriptor := &Descriptor{
// 		Type:            reflect.TypeOf(&ResolutionTestService{}),
// 		Lifetime:        Transient,
// 		Constructor:     constructor,
// 		ConstructorType: constructor.Type(),
// 		Dependencies: []*reflection.Dependency{
// 			{
// 				Type: reflect.TypeOf(&ResolutionTestService{}),
// 			},
// 		},
// 	}

// 	// Manually set result object flags
// 	descriptor.isResultObject = true
// 	descriptor.resultFields = []reflection.ResultField{
// 		{
// 			Name: "Service",
// 			Type: reflect.TypeOf(&ResolutionTestService{}),
// 		},
// 		{
// 			Name: "Other",
// 			Type: reflect.TypeOf(&ResolutionServiceWithDep{}),
// 		},
// 	}

// 	provider, err := collection.Build()
// 	if err != nil {
// 		t.Fatalf("Failed to build provider: %v", err)
// 	}
// 	defer provider.Close()

// 	sp, err := provider.CreateScope(nil)
// 	if err != nil {
// 		t.Fatalf("Failed to create scope: %v", err)
// 	}
// 	defer sp.Close()

// 	s := sp.(*scope)

// 	// Mock the analyzer to return proper info
// 	info := &reflection.ConstructorInfo{
// 		IsResultObject: true,
// 		Type:           constructor.Type(),
// 	}
// 	s.provider.analyzer = &mockAnalyzer{
// 		info: info,
// 	}

// 	result, err := s.createInstance(descriptor)
// 	if err != nil {
// 		t.Errorf("Failed to create instance with result object: %v", err)
// 	}

// 	if result == nil {
// 		t.Error("Expected non-nil result")
// 	}
// }

// TestCreateAllSingletons tests the createAllSingletons method
func TestCreateAllSingletons(t *testing.T) {
	tests := []struct {
		name        string
		setup       func() *collection
		wantErr     bool
		errContains string
		validate    func(*testing.T, *provider)
	}{
		{
			name: "no singletons",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddScoped(NewResolutionTestService)
				c.AddTransient(NewResolutionServiceWithDep)
				return c
			},
			wantErr: false,
			validate: func(t *testing.T, p *provider) {
				if len(p.singletons) != 0 {
					t.Errorf("Expected no singletons, got %d", len(p.singletons))
				}
			},
		},
		{
			name: "single singleton",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddSingleton(NewResolutionTestService)
				return c
			},
			wantErr: false,
			validate: func(t *testing.T, p *provider) {
				if len(p.singletons) != 1 {
					t.Errorf("Expected 1 singleton, got %d", len(p.singletons))
				}
				key := instanceKey{Type: reflect.TypeOf(&ResolutionTestService{})}
				if _, exists := p.singletons[key]; !exists {
					t.Error("Singleton not created")
				}
			},
		},
		{
			name: "multiple singletons with dependencies",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddSingleton(NewResolutionTestService)
				c.AddSingleton(NewResolutionServiceWithDep)
				return c
			},
			wantErr: false,
			validate: func(t *testing.T, p *provider) {
				if len(p.singletons) != 2 {
					t.Errorf("Expected 2 singletons, got %d", len(p.singletons))
				}
			},
		},
		{
			name: "singleton with error",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddSingleton(NewResolutionServiceWithError)
				return c
			},
			wantErr:     true,
			errContains: "construction error",
		},
		{
			name: "keyed singletons",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddSingleton(NewResolutionTestService, Name("first"))
				c.AddSingleton(NewResolutionTestService, Name("second"))
				return c
			},
			wantErr: false,
			validate: func(t *testing.T, p *provider) {
				if len(p.singletons) != 2 {
					t.Errorf("Expected 2 keyed singletons, got %d", len(p.singletons))
				}
				key1 := instanceKey{Type: reflect.TypeOf(&ResolutionTestService{}), Key: "first"}
				key2 := instanceKey{Type: reflect.TypeOf(&ResolutionTestService{}), Key: "second"}
				if _, exists := p.singletons[key1]; !exists {
					t.Error("First keyed singleton not created")
				}
				if _, exists := p.singletons[key2]; !exists {
					t.Error("Second keyed singleton not created")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.setup()

			p, err := c.Build()
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
				if p != nil {
					defer p.Close()
					if tt.validate != nil {
						tt.validate(t, p.(*provider))
					}
				}
			}
		})
	}
}

// TestResolutionContext tests the resolutionContext tracking
func TestResolutionContext(t *testing.T) {
	collection := NewCollection()
	collection.AddSingleton(NewResolutionTestService)

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

	ctx := &resolutionContext{
		scope:    s,
		stack:    make([]instanceKey, 0),
		depth:    0,
		maxDepth: 100,
	}

	// Test initial state
	if ctx.depth != 0 {
		t.Errorf("Initial depth should be 0, got %d", ctx.depth)
	}
	if len(ctx.stack) != 0 {
		t.Errorf("Initial stack should be empty, got %d items", len(ctx.stack))
	}

	// Test adding to stack
	key := instanceKey{Type: reflect.TypeOf(&ResolutionTestService{})}
	ctx.stack = append(ctx.stack, key)
	ctx.depth++

	if ctx.depth != 1 {
		t.Errorf("Depth should be 1, got %d", ctx.depth)
	}
	if len(ctx.stack) != 1 {
		t.Errorf("Stack should have 1 item, got %d", len(ctx.stack))
	}

	// Test max depth
	ctx.depth = ctx.maxDepth + 1
	if ctx.depth <= ctx.maxDepth {
		t.Error("Should exceed max depth")
	}
}

// Mock analyzer for testing
type mockAnalyzer struct {
	info *reflection.ConstructorInfo
	err  error
}

func (m *mockAnalyzer) Analyze(constructor any) (*reflection.ConstructorInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.info != nil {
		return m.info, nil
	}
	// Default behavior
	return &reflection.ConstructorInfo{
		Type:   reflect.TypeOf(constructor),
		IsFunc: true,
	}, nil
}

func (m *mockAnalyzer) GetDependencies(constructor any) ([]*reflection.Dependency, error) {
	return nil, nil
}

// BenchmarkCreateInstance benchmarks instance creation
func BenchmarkCreateInstance(b *testing.B) {
	collection := NewCollection()
	collection.AddSingleton(NewResolutionTestService)
	collection.AddScoped(NewResolutionServiceWithDep)

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
	descriptor, _ := NewDescriptor(NewResolutionServiceWithDep, Transient)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = s.createInstance(descriptor)
	}
}

// BenchmarkCreateAllSingletons benchmarks singleton creation
func BenchmarkCreateAllSingletons(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		collection := NewCollection()
		for j := 0; j < 10; j++ {
			collection.AddSingleton(NewResolutionTestService)
		}
		b.StartTimer()

		provider, _ := collection.Build()
		provider.Close()
	}
}
