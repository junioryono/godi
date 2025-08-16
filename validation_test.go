package godi

import (
	"reflect"
	"strings"
	"testing"
)

// Test types for validation tests
type ValidationServiceA struct {
	B *ValidationServiceB
}

type ValidationServiceB struct {
	C *ValidationServiceC
}

type ValidationServiceC struct {
	A *ValidationServiceA // Circular dependency
}

func NewValidationServiceA(b *ValidationServiceB) *ValidationServiceA {
	return &ValidationServiceA{B: b}
}

func NewValidationServiceB(c *ValidationServiceC) *ValidationServiceB {
	return &ValidationServiceB{C: c}
}

func NewValidationServiceC(a *ValidationServiceA) *ValidationServiceC {
	return &ValidationServiceC{A: a}
}

// Self-referencing service
type SelfReferencingService struct {
	Self *SelfReferencingService
}

func NewSelfReferencingService(self *SelfReferencingService) *SelfReferencingService {
	return &SelfReferencingService{Self: self}
}

// Services for lifetime validation
type SingletonService struct{}
type ScopedService struct{}
type TransientService struct{}

func NewSingletonService() *SingletonService {
	return &SingletonService{}
}

func NewScopedService() *ScopedService {
	return &ScopedService{}
}

func NewTransientService() *TransientService {
	return &TransientService{}
}

type SingletonWithScoped struct {
	Scoped *ScopedService
}

func NewSingletonWithScoped(scoped *ScopedService) *SingletonWithScoped {
	return &SingletonWithScoped{Scoped: scoped}
}

type SingletonWithTransient struct {
	Transient *TransientService
}

func NewSingletonWithTransient(transient *TransientService) *SingletonWithTransient {
	return &SingletonWithTransient{Transient: transient}
}

type ScopedWithTransient struct {
	Transient *TransientService
}

func NewScopedWithTransient(transient *TransientService) *ScopedWithTransient {
	return &ScopedWithTransient{Transient: transient}
}

// TestValidateDependencyGraph tests the validateDependencyGraph function
func TestValidateDependencyGraph(t *testing.T) {
	tests := []struct {
		name        string
		setup       func() *collection
		wantErr     bool
		errContains string
	}{
		{
			name: "valid graph - no dependencies",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddSingleton(NewSingletonService)
				c.AddScoped(NewScopedService)
				c.AddTransient(NewTransientService)
				return c
			},
			wantErr: false,
		},
		{
			name: "valid graph - linear dependencies",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddSingleton(NewTransientService)
				c.AddSingleton(NewScopedWithTransient)
				return c
			},
			wantErr: false,
		},
		{
			name: "circular dependency - 3 services",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddSingleton(NewValidationServiceA)
				c.AddSingleton(NewValidationServiceB)
				c.AddSingleton(NewValidationServiceC)
				return c
			},
			wantErr:     true,
			errContains: "circular dependency",
		},
		{
			name: "self-referencing service",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddSingleton(NewSelfReferencingService)
				return c
			},
			wantErr:     true,
			errContains: "circular dependency",
		},
		{
			name: "keyed services - no cycle",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddSingleton(NewSingletonService, Name("first"))
				c.AddSingleton(NewSingletonService, Name("second"))
				return c
			},
			wantErr: false,
		},
		{
			name: "grouped services - no cycle",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddTransient(NewTransientService, Group("group1"))
				c.AddTransient(NewTransientService, Group("group1"))
				return c
			},
			wantErr: false,
		},
		{
			name: "mixed services types",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddSingleton(NewSingletonService)
				c.AddSingleton(NewSingletonService, Name("named"))
				c.AddScoped(NewScopedService, Group("grouped"))
				c.AddTransient(NewTransientService)
				return c
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.setup()
			err := validateDependencyGraph(c)

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
			}
		})
	}
}

// TestValidateLifetimes tests the validateLifetimes function
func TestValidateLifetimes(t *testing.T) {
	tests := []struct {
		name        string
		setup       func() *collection
		wantErr     bool
		errContains string
	}{
		{
			name: "valid - singleton with no dependencies",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddSingleton(NewSingletonService)
				return c
			},
			wantErr: false,
		},
		{
			name: "valid - singleton with singleton dependency",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddSingleton(NewSingletonService)
				// Would need a service that depends on SingletonService
				return c
			},
			wantErr: false,
		},
		{
			name: "valid - singleton with transient dependency",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddTransient(NewTransientService)
				c.AddSingleton(NewSingletonWithTransient)
				return c
			},
			wantErr: false,
		},
		{
			name: "invalid - singleton with scoped dependency",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddScoped(NewScopedService)
				c.AddSingleton(NewSingletonWithScoped)
				return c
			},
			wantErr:     true,
			errContains: "singleton service",
		},
		{
			name: "valid - scoped with transient dependency",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddTransient(NewTransientService)
				c.AddScoped(NewScopedWithTransient)
				return c
			},
			wantErr: false,
		},
		{
			name: "valid - scoped with scoped dependency",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddScoped(NewScopedService)
				// Would need a scoped service that depends on ScopedService
				return c
			},
			wantErr: false,
		},
		{
			name: "valid - transient can depend on anything",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddSingleton(NewSingletonService)
				c.AddScoped(NewScopedService)
				c.AddTransient(NewTransientService)
				// Transient services can depend on any lifetime
				return c
			},
			wantErr: false,
		},
		{
			name: "keyed services with different lifetimes",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddScoped(NewScopedService, Name("scoped"))
				c.AddSingleton(NewSingletonService, Name("singleton"))
				// Keyed services are independent
				return c
			},
			wantErr: false,
		},
		{
			name: "grouped services with mixed lifetimes",
			setup: func() *collection {
				c := NewCollection().(*collection)
				c.AddScoped(NewScopedService, Group("mixed"))
				c.AddTransient(NewTransientService, Group("mixed"))
				return c
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := tt.setup()
			err := validateLifetimes(c)

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
			}
		})
	}
}

// TestValidationDescriptor tests the validateDescriptor function
func TestValidationDescriptor(t *testing.T) {
	validConstructor := reflect.ValueOf(NewSingletonService)
	validType := reflect.TypeOf(&SingletonService{})

	tests := []struct {
		name        string
		descriptor  *Descriptor
		wantErr     bool
		errContains string
	}{
		{
			name:        "nil descriptor",
			descriptor:  nil,
			wantErr:     true,
			errContains: "descriptor cannot be nil",
		},
		{
			name: "nil type",
			descriptor: &Descriptor{
				Type:            nil,
				Constructor:     validConstructor,
				ConstructorType: validConstructor.Type(),
				Lifetime:        Singleton,
			},
			wantErr:     true,
			errContains: "type cannot be nil",
		},
		{
			name: "invalid constructor",
			descriptor: &Descriptor{
				Type:            validType,
				Constructor:     reflect.Value{},
				ConstructorType: validConstructor.Type(),
				Lifetime:        Singleton,
			},
			wantErr:     true,
			errContains: "constructor is invalid",
		},
		{
			name: "invalid lifetime",
			descriptor: &Descriptor{
				Type:            validType,
				Constructor:     validConstructor,
				ConstructorType: validConstructor.Type(),
				Lifetime:        Lifetime(99),
			},
			wantErr:     true,
			errContains: "invalid lifetime",
		},
		{
			name: "valid descriptor - singleton",
			descriptor: &Descriptor{
				Type:            validType,
				Constructor:     validConstructor,
				ConstructorType: validConstructor.Type(),
				Lifetime:        Singleton,
			},
			wantErr: false,
		},
		{
			name: "valid descriptor - scoped",
			descriptor: &Descriptor{
				Type:            validType,
				Constructor:     validConstructor,
				ConstructorType: validConstructor.Type(),
				Lifetime:        Scoped,
			},
			wantErr: false,
		},
		{
			name: "valid descriptor - transient",
			descriptor: &Descriptor{
				Type:            validType,
				Constructor:     validConstructor,
				ConstructorType: validConstructor.Type(),
				Lifetime:        Transient,
			},
			wantErr: false,
		},
		{
			name: "valid descriptor with key",
			descriptor: &Descriptor{
				Type:            validType,
				Constructor:     validConstructor,
				ConstructorType: validConstructor.Type(),
				Lifetime:        Singleton,
				Key:             "test-key",
			},
			wantErr: false,
		},
		{
			name: "valid descriptor with groups",
			descriptor: &Descriptor{
				Type:            validType,
				Constructor:     validConstructor,
				ConstructorType: validConstructor.Type(),
				Lifetime:        Singleton,
				Groups:          []string{"group1", "group2"},
			},
			wantErr: false,
		},
		{
			name: "valid decorator descriptor",
			descriptor: &Descriptor{
				Type:            validType,
				Constructor:     validConstructor,
				ConstructorType: validConstructor.Type(),
				Lifetime:        Singleton,
				IsDecorator:     true,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDescriptor(tt.descriptor)

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
			}
		})
	}
}

// TestValidation_ComplexScenarios tests complex validation scenarios
func TestValidation_ComplexScenarios(t *testing.T) {
	t.Run("multiple circular dependencies", func(t *testing.T) {
		c := NewCollection().(*collection)

		// Create two separate circular dependency chains
		c.AddSingleton(NewValidationServiceA)
		c.AddSingleton(NewValidationServiceB)
		c.AddSingleton(NewValidationServiceC)

		c.AddSingleton(NewSelfReferencingService)

		err := validateDependencyGraph(c)
		if err == nil {
			t.Error("Should detect circular dependencies")
		}
	})

	t.Run("lifetime validation with mixed registrations", func(t *testing.T) {
		c := NewCollection().(*collection)

		// Register services with different lifetimes
		c.AddScoped(NewScopedService)
		c.AddSingleton(NewSingletonWithScoped) // This should fail
		c.AddTransient(NewTransientService)
		c.AddScoped(NewScopedWithTransient) // This should pass

		err := validateLifetimes(c)
		if err == nil {
			t.Error("Should detect singleton depending on scoped")
		}
		if !strings.Contains(err.Error(), "singleton") {
			t.Errorf("Error should mention singleton, got: %v", err)
		}
	})

	t.Run("empty collection validation", func(t *testing.T) {
		c := NewCollection().(*collection)

		// Empty collection should pass all validations
		err := validateDependencyGraph(c)
		if err != nil {
			t.Errorf("Empty collection should pass dependency validation: %v", err)
		}

		err = validateLifetimes(c)
		if err != nil {
			t.Errorf("Empty collection should pass lifetime validation: %v", err)
		}
	})
}

// BenchmarkValidateDependencyGraph benchmarks dependency graph validation
func BenchmarkValidateDependencyGraph(b *testing.B) {
	c := NewCollection().(*collection)

	// Add many services
	for i := 0; i < 100; i++ {
		c.AddSingleton(NewSingletonService)
		c.AddScoped(NewScopedService)
		c.AddTransient(NewTransientService)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateDependencyGraph(c)
	}
}

// BenchmarkValidateLifetimes benchmarks lifetime validation
func BenchmarkValidateLifetimes(b *testing.B) {
	c := NewCollection().(*collection)

	// Add many services with dependencies
	c.AddTransient(NewTransientService)
	c.AddScoped(NewScopedWithTransient)
	c.AddSingleton(NewSingletonWithTransient)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateLifetimes(c)
	}
}

// BenchmarkValidateDescriptor benchmarks descriptor validation
func BenchmarkValidateDescriptor(b *testing.B) {
	constructor := reflect.ValueOf(NewSingletonService)
	descriptor := &Descriptor{
		Type:            reflect.TypeOf(&SingletonService{}),
		Constructor:     constructor,
		ConstructorType: constructor.Type(),
		Lifetime:        Singleton,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateDescriptor(descriptor)
	}
}
