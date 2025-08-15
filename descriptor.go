package godi

import (
	"reflect"

	"github.com/junioryono/godi/v3/internal/reflection"
)

// Descriptor represents both service providers and decorators
type Descriptor struct {
	// Type is the service type this descriptor produces or decorates
	Type reflect.Type

	// Key is optional - for named/keyed services or decorators
	Key any

	// Lifetime determines instance caching behavior (ignored for decorators)
	Lifetime Lifetime

	// Constructor is the reflected function value (provider or decorator function)
	Constructor reflect.Value

	// ConstructorType is the type of the constructor function
	ConstructorType reflect.Type

	// Dependencies are the analyzed dependencies
	Dependencies []*reflection.Dependency

	// Groups this provider belongs to (not used for decorators)
	Groups []string

	// IsDecorator indicates if this descriptor is a decorator
	IsDecorator bool

	// DecoratedType is the type being decorated (only for decorators)
	// This is typically the same as Type but kept separate for clarity
	DecoratedType reflect.Type

	// Analysis results cached for performance
	isAnalyzed     bool
	isResultObject bool
	resultFields   []reflection.ResultField
	isParamObject  bool
	paramFields    []reflection.ParamField
}

// IsProvider returns true if this descriptor is a provider (not a decorator)
func (d *Descriptor) IsProvider() bool {
	return !d.IsDecorator
}

// GetTargetType returns the type this descriptor targets
// For providers, this is the type they provide
// For decorators, this is the type they decorate
func (d *Descriptor) GetTargetType() reflect.Type {
	if d.IsDecorator && d.DecoratedType != nil {
		return d.DecoratedType
	}

	return d.Type
}

// GetType returns the service type this descriptor produces
// Implements the Provider interface from the graph package
func (d *Descriptor) GetType() reflect.Type {
	return d.Type
}

// GetKey returns the optional key for named/keyed services
// Implements the Provider interface from the graph package
func (d *Descriptor) GetKey() any {
	return d.Key
}

// GetDependencies returns the analyzed dependencies
// Implements the Provider interface from the graph package
func (d *Descriptor) GetDependencies() []*reflection.Dependency {
	return d.Dependencies
}
