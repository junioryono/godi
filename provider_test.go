package godi

import (
	"reflect"
	"testing"
)

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
						Type:   serviceType,
						Groups: []string{"test-group"},
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

// TestProviderOptions tests the ProviderOptions struct
func TestProviderOptions(t *testing.T) {
	tests := []struct {
		name          string
		options       ProviderOptions
		validateGraph bool
		strictMode    bool
	}{
		{
			name: "default options",
			options: ProviderOptions{},
			validateGraph: false,
			strictMode: false,
		},
		{
			name: "validate graph enabled",
			options: ProviderOptions{
				ValidateGraph: true,
			},
			validateGraph: true,
			strictMode: false,
		},
		{
			name: "strict mode enabled",
			options: ProviderOptions{
				StrictMode: true,
			},
			validateGraph: false,
			strictMode: true,
		},
		{
			name: "both options enabled",
			options: ProviderOptions{
				ValidateGraph: true,
				StrictMode: true,
			},
			validateGraph: true,
			strictMode: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.options.ValidateGraph != tt.validateGraph {
				t.Errorf("ValidateGraph = %v, want %v", tt.options.ValidateGraph, tt.validateGraph)
			}
			if tt.options.StrictMode != tt.strictMode {
				t.Errorf("StrictMode = %v, want %v", tt.options.StrictMode, tt.strictMode)
			}
		})
	}
}

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