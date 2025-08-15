package godi

import (
	"errors"
	"reflect"
	"testing"
)

// Test types for descriptor tests
type TestServiceForDescriptor struct {
	Value string
}

func NewTestServiceForDescriptor() *TestServiceForDescriptor {
	return &TestServiceForDescriptor{Value: "test"}
}

func NewTestServiceWithError() (*TestServiceForDescriptor, error) {
	return &TestServiceForDescriptor{Value: "test"}, nil
}

func NewTestServiceReturnsError() (*TestServiceForDescriptor, error) {
	return nil, errors.New("construction failed")
}

type DependencyService struct{}

func NewServiceWithDependency(dep *DependencyService) *TestServiceForDescriptor {
	return &TestServiceForDescriptor{Value: "with-dep"}
}

// Invalid constructors for testing
func InvalidNoReturn() {}

func InvalidTooManyReturns() (int, string, error) {
	return 0, "", nil
}

func InvalidSecondReturnNotError() (int, string) {
	return 0, ""
}

// TestDescriptor_IsProvider tests the IsProvider method
func TestDescriptor_IsProvider(t *testing.T) {
	tests := []struct {
		name        string
		isDecorator bool
		want        bool
	}{
		{
			name:        "provider descriptor",
			isDecorator: false,
			want:        true,
		},
		{
			name:        "decorator descriptor",
			isDecorator: true,
			want:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Descriptor{
				IsDecorator: tt.isDecorator,
			}
			if got := d.IsProvider(); got != tt.want {
				t.Errorf("IsProvider() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDescriptor_GetTargetType tests the GetTargetType method
func TestDescriptor_GetTargetType(t *testing.T) {
	serviceType := reflect.TypeOf(&TestServiceForDescriptor{})
	decoratedType := reflect.TypeOf(&DependencyService{})

	tests := []struct {
		name          string
		descriptor    *Descriptor
		want          reflect.Type
	}{
		{
			name: "provider returns Type",
			descriptor: &Descriptor{
				Type:        serviceType,
				IsDecorator: false,
			},
			want: serviceType,
		},
		{
			name: "decorator with DecoratedType returns DecoratedType",
			descriptor: &Descriptor{
				Type:          serviceType,
				DecoratedType: decoratedType,
				IsDecorator:   true,
			},
			want: decoratedType,
		},
		{
			name: "decorator without DecoratedType returns Type",
			descriptor: &Descriptor{
				Type:          serviceType,
				DecoratedType: nil,
				IsDecorator:   true,
			},
			want: serviceType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.descriptor.GetTargetType(); got != tt.want {
				t.Errorf("GetTargetType() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestDescriptor_GetType tests the GetType method
func TestDescriptor_GetType(t *testing.T) {
	serviceType := reflect.TypeOf(&TestServiceForDescriptor{})
	
	d := &Descriptor{
		Type: serviceType,
	}
	
	if got := d.GetType(); got != serviceType {
		t.Errorf("GetType() = %v, want %v", got, serviceType)
	}
}

// TestDescriptor_GetKey tests the GetKey method
func TestDescriptor_GetKey(t *testing.T) {
	tests := []struct {
		name string
		key  any
	}{
		{
			name: "nil key",
			key:  nil,
		},
		{
			name: "string key",
			key:  "test-key",
		},
		{
			name: "int key",
			key:  42,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &Descriptor{
				Key: tt.key,
			}
			if got := d.GetKey(); got != tt.key {
				t.Errorf("GetKey() = %v, want %v", got, tt.key)
			}
		})
	}
}

// TestDescriptor_GetDependencies tests the GetDependencies method
func TestDescriptor_GetDependencies(t *testing.T) {
	// Create a descriptor with constructor that has dependencies
	descriptor, err := NewDescriptor(NewServiceWithDependency, Singleton)
	if err != nil {
		t.Fatalf("Failed to create descriptor: %v", err)
	}

	deps := descriptor.GetDependencies()
	if len(deps) != 1 {
		t.Errorf("Expected 1 dependency, got %d", len(deps))
	}

	if deps[0].Type != reflect.TypeOf(&DependencyService{}) {
		t.Errorf("Unexpected dependency type: %v", deps[0].Type)
	}
}

// TestNewDescriptor tests the NewDescriptor function
func TestNewDescriptor(t *testing.T) {
	tests := []struct {
		name        string
		constructor any
		lifetime    Lifetime
		opts        []AddOption
		wantErr     bool
		errType     error
		validate    func(t *testing.T, d *Descriptor)
	}{
		{
			name:        "nil constructor",
			constructor: nil,
			lifetime:    Singleton,
			wantErr:     true,
			errType:     ErrNilConstructor,
		},
		{
			name:        "non-function constructor",
			constructor: "not a function",
			lifetime:    Singleton,
			wantErr:     true,
			errType:     ErrConstructorNotFunction,
		},
		{
			name:        "function with no returns",
			constructor: InvalidNoReturn,
			lifetime:    Singleton,
			wantErr:     true,
			errType:     ErrConstructorNoReturn,
		},
		{
			name:        "function with too many returns",
			constructor: InvalidTooManyReturns,
			lifetime:    Singleton,
			wantErr:     true,
			errType:     ErrConstructorTooManyReturns,
		},
		{
			name:        "function with invalid second return",
			constructor: InvalidSecondReturnNotError,
			lifetime:    Singleton,
			wantErr:     true,
			errType:     ErrConstructorInvalidSecondReturn,
		},
		{
			name:        "valid constructor without error",
			constructor: NewTestServiceForDescriptor,
			lifetime:    Singleton,
			wantErr:     false,
			validate: func(t *testing.T, d *Descriptor) {
				if d.Type != reflect.TypeOf(&TestServiceForDescriptor{}) {
					t.Errorf("Unexpected Type: %v", d.Type)
				}
				if d.Lifetime != Singleton {
					t.Errorf("Unexpected Lifetime: %v", d.Lifetime)
				}
				if d.IsDecorator {
					t.Error("Expected IsDecorator to be false")
				}
			},
		},
		{
			name:        "valid constructor with error",
			constructor: NewTestServiceWithError,
			lifetime:    Scoped,
			wantErr:     false,
			validate: func(t *testing.T, d *Descriptor) {
				if d.Type != reflect.TypeOf(&TestServiceForDescriptor{}) {
					t.Errorf("Unexpected Type: %v", d.Type)
				}
				if d.Lifetime != Scoped {
					t.Errorf("Unexpected Lifetime: %v", d.Lifetime)
				}
			},
		},
		{
			name:        "constructor with dependencies",
			constructor: NewServiceWithDependency,
			lifetime:    Transient,
			wantErr:     false,
			validate: func(t *testing.T, d *Descriptor) {
				if len(d.Dependencies) != 1 {
					t.Errorf("Expected 1 dependency, got %d", len(d.Dependencies))
				}
			},
		},
		{
			name:        "with name option",
			constructor: NewTestServiceForDescriptor,
			lifetime:    Singleton,
			opts:        []AddOption{Name("test-name")},
			wantErr:     false,
			validate: func(t *testing.T, d *Descriptor) {
				if d.Key != "test-name" {
					t.Errorf("Expected Key to be 'test-name', got %v", d.Key)
				}
			},
		},
		{
			name:        "with group option",
			constructor: NewTestServiceForDescriptor,
			lifetime:    Singleton,
			opts:        []AddOption{Group("test-group")},
			wantErr:     false,
			validate: func(t *testing.T, d *Descriptor) {
				if len(d.Groups) != 1 || d.Groups[0] != "test-group" {
					t.Errorf("Expected Groups to be ['test-group'], got %v", d.Groups)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d, err := NewDescriptor(tt.constructor, tt.lifetime, tt.opts...)
			
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errType != nil && !errors.Is(err, tt.errType) {
					t.Errorf("Expected error %v, got %v", tt.errType, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				} else if tt.validate != nil {
					tt.validate(t, d)
				}
			}
		})
	}
}

// TestValidateDescriptor tests the ValidateDescriptor function
func TestValidateDescriptor(t *testing.T) {
	validConstructor := reflect.ValueOf(NewTestServiceForDescriptor)
	validType := reflect.TypeOf(&TestServiceForDescriptor{})
	validConstructorType := validConstructor.Type()

	tests := []struct {
		name       string
		descriptor *Descriptor
		wantErr    bool
		errMsg     string
	}{
		{
			name:       "nil descriptor",
			descriptor: nil,
			wantErr:    true,
			errMsg:     "descriptor cannot be nil",
		},
		{
			name: "nil type",
			descriptor: &Descriptor{
				Type:            nil,
				Constructor:     validConstructor,
				ConstructorType: validConstructorType,
				Lifetime:        Singleton,
			},
			wantErr: true,
			errMsg:  "descriptor type cannot be nil",
		},
		{
			name: "invalid constructor",
			descriptor: &Descriptor{
				Type:            validType,
				Constructor:     reflect.Value{},
				ConstructorType: validConstructorType,
				Lifetime:        Singleton,
			},
			wantErr: true,
			errMsg:  "descriptor constructor is invalid",
		},
		{
			name: "nil constructor type",
			descriptor: &Descriptor{
				Type:            validType,
				Constructor:     validConstructor,
				ConstructorType: nil,
				Lifetime:        Singleton,
			},
			wantErr: true,
			errMsg:  "descriptor constructor type cannot be nil",
		},
		{
			name: "invalid lifetime",
			descriptor: &Descriptor{
				Type:            validType,
				Constructor:     validConstructor,
				ConstructorType: validConstructorType,
				Lifetime:        Lifetime(99),
			},
			wantErr: true,
			errMsg:  "invalid service lifetime",
		},
		{
			name: "valid descriptor with Singleton",
			descriptor: &Descriptor{
				Type:            validType,
				Constructor:     validConstructor,
				ConstructorType: validConstructorType,
				Lifetime:        Singleton,
			},
			wantErr: false,
		},
		{
			name: "valid descriptor with Scoped",
			descriptor: &Descriptor{
				Type:            validType,
				Constructor:     validConstructor,
				ConstructorType: validConstructorType,
				Lifetime:        Scoped,
			},
			wantErr: false,
		},
		{
			name: "valid descriptor with Transient",
			descriptor: &Descriptor{
				Type:            validType,
				Constructor:     validConstructor,
				ConstructorType: validConstructorType,
				Lifetime:        Transient,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDescriptor(tt.descriptor)
			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errMsg != "" && !containsString(err.Error(), tt.errMsg) {
					t.Errorf("Expected error containing '%s', got '%v'", tt.errMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// Helper function for string contains check
func containsString(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr || len(s) > len(substr) && containsString(s[1:], substr)
}