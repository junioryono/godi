package registry

import "reflect"

// ServiceLifetime represents the lifetime of a service
type ServiceLifetime int

const (
	// Singleton - one instance for the entire application
	Singleton ServiceLifetime = iota

	// Scoped - one instance per scope
	Scoped

	// Transient - new instance every time
	Transient
)

// Descriptor represents both service providers and decorators
type Descriptor struct {
	// Type is the service type this descriptor produces or decorates
	Type reflect.Type

	// Key is optional - for named/keyed services or decorators
	Key any

	// Lifetime determines instance caching behavior (ignored for decorators)
	Lifetime ServiceLifetime

	// Constructor is the reflected function value (provider or decorator function)
	Constructor reflect.Value

	// ConstructorType is the type of the constructor function
	ConstructorType reflect.Type

	// Dependencies are the analyzed dependencies
	Dependencies []*Dependency

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
	resultFields   []ResultField
	isParamObject  bool
	paramFields    []ParamField
}

// Dependency represents a single dependency of a provider or decorator
type Dependency struct {
	// Type of the dependency
	Type reflect.Type

	// Key for named dependencies (optional)
	Key any

	// Group for group dependencies (optional)
	Group string

	// Optional indicates if this dependency can be nil
	Optional bool

	// Index is the parameter position (for regular functions)
	Index int

	// FieldName is the field name (for param objects)
	FieldName string
}

// ResultField represents a field in a result object (Out struct)
type ResultField struct {
	Name  string
	Type  reflect.Type
	Key   any    // for named results
	Group string // for group results
	Index int    // field index in struct
}

// ParamField represents a field in a parameter object (In struct)
type ParamField struct {
	Name     string
	Type     reflect.Type
	Key      any    // for named dependencies
	Group    string // for group dependencies
	Optional bool
	Index    int // field index in struct
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
