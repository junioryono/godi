package godi

import (
	"context"
	"reflect"
	"testing"
)

// TestProvider_Get tests the Get method
func TestProvider_Get(t *testing.T) {
	collection := NewCollection()
	collection.AddSingleton(NewTestService)

	provider, err := collection.Build()
	if err != nil {
		t.Fatalf("Failed to build provider: %v", err)
	}
	defer provider.Close()

	serviceType := reflect.TypeOf(&TestService{})

	// Test successful get
	result, err := provider.Get(serviceType)
	if err != nil {
		t.Errorf("Failed to get service: %v", err)
	}
	if result == nil {
		t.Error("Expected non-nil result")
	}

	// Test get after dispose
	provider.Close()
	_, err = provider.Get(serviceType)
	if err != ErrProviderDisposed {
		t.Errorf("Expected ErrProviderDisposed, got %v", err)
	}
}

// TestProvider_GetKeyed tests the GetKeyed method
func TestProvider_GetKeyed(t *testing.T) {
	collection := NewCollection()
	collection.AddSingleton(NewTestService, Name("test"))

	provider, err := collection.Build()
	if err != nil {
		t.Fatalf("Failed to build provider: %v", err)
	}
	defer provider.Close()

	serviceType := reflect.TypeOf(&TestService{})

	// Test successful get
	result, err := provider.GetKeyed(serviceType, "test")
	if err != nil {
		t.Errorf("Failed to get keyed service: %v", err)
	}
	if result == nil {
		t.Error("Expected non-nil result")
	}

	// Test get after dispose
	provider.Close()
	_, err = provider.GetKeyed(serviceType, "test")
	if err != ErrProviderDisposed {
		t.Errorf("Expected ErrProviderDisposed, got %v", err)
	}
}

// TestProvider_GetGroup tests the GetGroup method
func TestProvider_GetGroup(t *testing.T) {
	collection := NewCollection()
	collection.AddTransient(NewTestService, Group("test"))
	collection.AddTransient(NewTestService, Group("test"))

	provider, err := collection.Build()
	if err != nil {
		t.Fatalf("Failed to build provider: %v", err)
	}
	defer provider.Close()

	serviceType := reflect.TypeOf(&TestService{})

	// Test successful get
	results, err := provider.GetGroup(serviceType, "test")
	if err != nil {
		t.Errorf("Failed to get group: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	// Test get after dispose
	provider.Close()
	_, err = provider.GetGroup(serviceType, "test")
	if err != ErrProviderDisposed {
		t.Errorf("Expected ErrProviderDisposed, got %v", err)
	}
}

// TestProvider_CreateScope tests the CreateScope method
func TestProvider_CreateScope(t *testing.T) {
	collection := NewCollection()
	provider, err := collection.Build()
	if err != nil {
		t.Fatalf("Failed to build provider: %v", err)
	}
	defer provider.Close()

	// Test create scope with nil context
	scope, err := provider.CreateScope(nil)
	if err != nil {
		t.Errorf("Failed to create scope: %v", err)
	}
	if scope == nil {
		t.Error("Expected non-nil scope")
	}
	defer scope.Close()

	// Test create scope with context
	ctx := context.Background()
	scope2, err := provider.CreateScope(ctx)
	if err != nil {
		t.Errorf("Failed to create scope with context: %v", err)
	}
	if scope2 == nil {
		t.Error("Expected non-nil scope")
	}
	defer scope2.Close()

	// Test create scope after dispose
	provider.Close()
	_, err = provider.CreateScope(nil)
	if err != ErrProviderDisposed {
		t.Errorf("Expected ErrProviderDisposed, got %v", err)
	}
}

// TestProvider_GetSetSingleton tests singleton management
func TestProvider_GetSetSingleton(t *testing.T) {
	p := &provider{
		singletons:  make(map[instanceKey]any),
		disposables: make([]Disposable, 0),
	}

	key := instanceKey{Type: reflect.TypeOf(&TestService{})}

	// Test get non-existent
	_, exists := p.getSingleton(key)
	if exists {
		t.Error("Should not find non-existent singleton")
	}

	// Test set and get
	instance := &TestService{Value: "test"}
	p.setSingleton(key, instance)

	retrieved, exists := p.getSingleton(key)
	if !exists {
		t.Error("Should find singleton after setting")
	}
	if retrieved != instance {
		t.Error("Retrieved singleton should match set instance")
	}
}

// TestProvider_FindDescriptor tests the findDescriptor method
func TestProvider_FindDescriptor(t *testing.T) {
	serviceType := reflect.TypeOf(&TestService{})
	descriptor := &Descriptor{
		Type:     serviceType,
		Lifetime: Singleton,
	}

	p := &provider{
		services: map[reflect.Type][]*Descriptor{
			serviceType: {descriptor},
		},
		keyedServices: map[TypeKey][]*Descriptor{
			{Type: serviceType, Key: "test"}: {descriptor},
		},
	}

	// Test find non-keyed
	found := p.findDescriptor(serviceType, nil)
	if found != descriptor {
		t.Error("Should find non-keyed descriptor")
	}

	// Test find keyed
	found = p.findDescriptor(serviceType, "test")
	if found != descriptor {
		t.Error("Should find keyed descriptor")
	}

	// Test find non-existent
	found = p.findDescriptor(serviceType, "nonexistent")
	if found != nil {
		t.Error("Should not find non-existent descriptor")
	}
}

// TestProvider_FindGroupDescriptors tests the findGroupDescriptors method
func TestProvider_FindGroupDescriptors(t *testing.T) {
	serviceType := reflect.TypeOf(&TestService{})
	descriptor1 := &Descriptor{Type: serviceType, Lifetime: Transient}
	descriptor2 := &Descriptor{Type: serviceType, Lifetime: Transient}

	p := &provider{
		groups: map[GroupKey][]*Descriptor{
			{Type: serviceType, Group: "test"}: {descriptor1, descriptor2},
		},
	}

	// Test find existing group
	found := p.findGroupDescriptors(serviceType, "test")
	if len(found) != 2 {
		t.Errorf("Expected 2 descriptors, got %d", len(found))
	}

	// Test find non-existent group
	found = p.findGroupDescriptors(serviceType, "nonexistent")
	if len(found) != 0 {
		t.Error("Should not find descriptors for non-existent group")
	}
}

// TestProvider_GetDecorators tests the getDecorators method
func TestProvider_GetDecorators(t *testing.T) {
	serviceType := reflect.TypeOf(&TestService{})
	decorator := &Descriptor{
		Type:        serviceType,
		IsDecorator: true,
	}

	p := &provider{
		decorators: map[reflect.Type][]*Descriptor{
			serviceType: {decorator},
		},
	}

	// Test get existing decorators
	found := p.getDecorators(serviceType)
	if len(found) != 1 {
		t.Errorf("Expected 1 decorator, got %d", len(found))
	}

	// Test get non-existent decorators
	otherType := reflect.TypeOf(&TestServiceWithDep{})
	found = p.getDecorators(otherType)
	if len(found) != 0 {
		t.Error("Should not find decorators for non-decorated type")
	}
}

// TestProvider_Close tests the Close method of provider
func TestProvider_Close(t *testing.T) {
	tests := []struct {
		name    string
		setup   func() *provider
		wantErr bool
	}{
		{
			name: "close empty provider",
			setup: func() *provider {
				return &provider{
					services:      make(map[reflect.Type][]*Descriptor),
					keyedServices: make(map[TypeKey][]*Descriptor),
					groups:        make(map[GroupKey][]*Descriptor),
					decorators:    make(map[reflect.Type][]*Descriptor),
				}
			},
			wantErr: false,
		},
		{
			name: "close provider with services",
			setup: func() *provider {
				p := &provider{
					services:      make(map[reflect.Type][]*Descriptor),
					keyedServices: make(map[TypeKey][]*Descriptor),
					groups:        make(map[GroupKey][]*Descriptor),
					decorators:    make(map[reflect.Type][]*Descriptor),
				}
				// Add some test data
				serviceType := reflect.TypeOf(&TestService{})
				p.services[serviceType] = []*Descriptor{
					{
						Type:     serviceType,
						Lifetime: Singleton,
					},
				}
				return p
			},
			wantErr: false,
		},
		{
			name: "close provider with keyed services",
			setup: func() *provider {
				p := &provider{
					services:      make(map[reflect.Type][]*Descriptor),
					keyedServices: make(map[TypeKey][]*Descriptor),
					groups:        make(map[GroupKey][]*Descriptor),
					decorators:    make(map[reflect.Type][]*Descriptor),
				}
				// Add keyed service
				serviceType := reflect.TypeOf(&TestService{})
				key := TypeKey{Type: serviceType, Key: "test"}
				p.keyedServices[key] = []*Descriptor{
					{
						Type:     serviceType,
						Key:      "test",
						Lifetime: Scoped,
					},
				}
				return p
			},
			wantErr: false,
		},
		{
			name: "close provider with groups",
			setup: func() *provider {
				p := &provider{
					services:      make(map[reflect.Type][]*Descriptor),
					keyedServices: make(map[TypeKey][]*Descriptor),
					groups:        make(map[GroupKey][]*Descriptor),
					decorators:    make(map[reflect.Type][]*Descriptor),
				}
				// Add grouped service
				serviceType := reflect.TypeOf(&TestService{})
				groupKey := GroupKey{Type: serviceType, Group: "test-group"}
				p.groups[groupKey] = []*Descriptor{
					{
						Type:     serviceType,
						Groups:   []string{"test-group"},
						Lifetime: Transient,
					},
				}
				return p
			},
			wantErr: false,
		},
		{
			name: "close provider with decorators",
			setup: func() *provider {
				p := &provider{
					services:      make(map[reflect.Type][]*Descriptor),
					keyedServices: make(map[TypeKey][]*Descriptor),
					groups:        make(map[GroupKey][]*Descriptor),
					decorators:    make(map[reflect.Type][]*Descriptor),
				}
				// Add decorator
				serviceType := reflect.TypeOf(&TestService{})
				p.decorators[serviceType] = []*Descriptor{
					{
						Type:        serviceType,
						IsDecorator: true,
					},
				}
				return p
			},
			wantErr: false,
		},
		{
			name: "close nil provider fields",
			setup: func() *provider {
				return &provider{}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := tt.setup()
			err := p.Close()
			if (err != nil) != tt.wantErr {
				t.Errorf("Close() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// // TestProviderOptions tests the ProviderOptions struct
// func TestProviderOptions(t *testing.T) {
// 	tests := []struct {
// 		name          string
// 		options       ProviderOptions
// 		validateGraph bool
// 		strictMode    bool
// 	}{
// 		{
// 			name: "default options",
// 			options: ProviderOptions{},
// 			validateGraph: false,
// 			strictMode: false,
// 		},
// 		{
// 			name: "validate graph enabled",
// 			options: ProviderOptions{
// 				ValidateGraph: true,
// 			},
// 			validateGraph: true,
// 			strictMode: false,
// 		},
// 		{
// 			name: "strict mode enabled",
// 			options: ProviderOptions{
// 				StrictMode: true,
// 			},
// 			validateGraph: false,
// 			strictMode: true,
// 		},
// 		{
// 			name: "both options enabled",
// 			options: ProviderOptions{
// 				ValidateGraph: true,
// 				StrictMode: true,
// 			},
// 			validateGraph: true,
// 			strictMode: true,
// 		},
// 	}

// 	for _, tt := range tests {
// 		t.Run(tt.name, func(t *testing.T) {
// 			if tt.options.ValidateGraph != tt.validateGraph {
// 				t.Errorf("ValidateGraph = %v, want %v", tt.options.ValidateGraph, tt.validateGraph)
// 			}
// 			if tt.options.StrictMode != tt.strictMode {
// 				t.Errorf("StrictMode = %v, want %v", tt.options.StrictMode, tt.strictMode)
// 			}
// 		})
// 	}
// }

// TestProvider_Interface tests that provider implements Provider interface
func TestProvider_Interface(t *testing.T) {
	var _ Provider = (*provider)(nil) // Compile-time check

	p := &provider{
		services:      make(map[reflect.Type][]*Descriptor),
		keyedServices: make(map[TypeKey][]*Descriptor),
		groups:        make(map[GroupKey][]*Descriptor),
		decorators:    make(map[reflect.Type][]*Descriptor),
	}

	// Test that it implements the interface methods
	if _, ok := interface{}(p).(Provider); !ok {
		t.Error("provider does not implement Provider interface")
	}
}

// TestProvider_Structure tests the internal structure of provider
func TestProvider_Structure(t *testing.T) {
	p := &provider{
		services:      make(map[reflect.Type][]*Descriptor),
		keyedServices: make(map[TypeKey][]*Descriptor),
		groups:        make(map[GroupKey][]*Descriptor),
		decorators:    make(map[reflect.Type][]*Descriptor),
		singletons:    make(map[instanceKey]any),
		disposables:   make([]Disposable, 0),
		scopes:        make(map[*scope]struct{}),
	}

	// Verify the fields exist and are of correct types
	if p.services == nil {
		t.Error("services map should not be nil")
	}
	if p.keyedServices == nil {
		t.Error("keyedServices map should not be nil")
	}
	if p.groups == nil {
		t.Error("groups map should not be nil")
	}
	if p.decorators == nil {
		t.Error("decorators map should not be nil")
	}

	// Test adding entries to verify map types
	serviceType := reflect.TypeOf(&TestService{})
	descriptor := &Descriptor{
		Type:     serviceType,
		Lifetime: Singleton,
	}

	// Test services map
	p.services[serviceType] = []*Descriptor{descriptor}
	if len(p.services[serviceType]) != 1 {
		t.Error("Failed to add to services map")
	}

	// Test keyedServices map
	key := TypeKey{Type: serviceType, Key: "test"}
	p.keyedServices[key] = []*Descriptor{descriptor}
	if len(p.keyedServices[key]) != 1 {
		t.Error("Failed to add to keyedServices map")
	}

	// Test groups map
	groupKey := GroupKey{Type: serviceType, Group: "test-group"}
	p.groups[groupKey] = []*Descriptor{descriptor}
	if len(p.groups[groupKey]) != 1 {
		t.Error("Failed to add to groups map")
	}

	// Test decorators map
	p.decorators[serviceType] = []*Descriptor{descriptor}
	if len(p.decorators[serviceType]) != 1 {
		t.Error("Failed to add to decorators map")
	}
}
