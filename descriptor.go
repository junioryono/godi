package godi

import (
	"fmt"
	"reflect"

	"github.com/junioryono/godi/v3/internal/reflection"
)

// Descriptor represents both service providers and decorators
type Descriptor struct {
	// Type is the service type this descriptor produces or decorates
	Type reflect.Type

	// Key is optional - for named/keyed services or decorators
	Key any

	// Group this provider belongs to
	Group string

	// Lifetime determines instance caching behavior (ignored for decorators)
	Lifetime Lifetime

	// Constructor is the reflected function value (provider or decorator function)
	Constructor reflect.Value

	// ConstructorType is the type of the constructor function
	ConstructorType reflect.Type

	// Dependencies are the analyzed dependencies
	Dependencies []*reflection.Dependency

	// As is an optional list of interface types this service can be registered as
	// This is typically used for interface-based services
	As []any

	// IsDecorator indicates if this descriptor is a decorator
	IsDecorator bool

	// DecoratedType is the type being decorated (only for decorators)
	// This is typically the same as Type but kept separate for clarity
	DecoratedType reflect.Type

	// IsInstance indicates if this descriptor holds an instance value
	IsInstance bool

	// Instance is the actual instance value when IsInstance is true
	Instance any

	// ReturnIndex indicates which return value this descriptor represents
	// -1 for single returns or Out structs, >= 0 for specific return index in multi-return
	ReturnIndex int

	// IsMultiReturn indicates if this descriptor is from a multi-return constructor
	IsMultiReturn bool

	// Analysis results cached for performance
	isFunc         bool
	isResultObject bool
	resultFields   []reflection.ResultField
	isParamObject  bool
	paramFields    []reflection.ParamField
}

// newDescriptor creates a new descriptor from a constructor with the given lifetime and options
func newDescriptor(constructor any, lifetime Lifetime, opts ...AddOption) (*Descriptor, error) {
	if constructor == nil {
		return nil, ErrNilConstructor
	}

	// Parse options
	options := &addOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt.applyAddOption(options)
		}
	}

	// Validate options
	if err := options.Validate(); err != nil {
		return nil, err
	}

	// Get constructor value and type
	constructorValue := reflect.ValueOf(constructor)

	// Check for nil pointers
	if !constructorValue.IsValid() || (constructorValue.Kind() == reflect.Ptr && constructorValue.IsNil()) {
		return nil, ErrNilConstructor
	}

	constructorType := constructorValue.Type()

	// Check if it's an instance (not a function)
	isInstance := constructorType.Kind() != reflect.Func

	// For functions, validate return values
	if !isInstance {
		// Validate return values
		numReturns := constructorType.NumOut()
		if numReturns == 0 {
			return nil, ErrConstructorNoReturn
		}

		// Check if the last return is an error (if any)
		if numReturns > 0 {
			errorType := reflect.TypeOf((*error)(nil)).Elem()

			// If there's an error type in the returns, it must be the last one
			for i := 0; i < numReturns-1; i++ {
				if constructorType.Out(i).Implements(errorType) {
					// Found error in non-last position - this is allowed but unusual
					// The analyzer will handle this case by treating it as a regular type
					break
				}
			}
		}
	}

	// Create analyzer to analyze the constructor
	analyzer := reflection.New()
	info, err := analyzer.Analyze(constructor)
	if err != nil {
		return nil, fmt.Errorf("failed to analyze constructor: %w", err)
	}

	// Get dependencies from analyzer
	dependencies, err := analyzer.GetDependencies(constructor)
	if err != nil {
		return nil, fmt.Errorf("failed to get dependencies: %w", err)
	}

	// Get the service type
	var serviceType reflect.Type
	if isInstance {
		// For instances, the service type is the type of the instance
		serviceType = constructorType
	} else {
		// For functions, the service type is the first return value
		serviceType = constructorType.Out(0)
	}

	// Create descriptor
	descriptor := &Descriptor{
		Type:            serviceType,
		Lifetime:        lifetime,
		Constructor:     constructorValue,
		ConstructorType: constructorType,
		Dependencies:    dependencies,
		Group:           options.Group,
		IsDecorator:     false,
		IsInstance:      isInstance,
		Instance:        nil,
		ReturnIndex:     -1, // Default: not a multi-return descriptor
		IsMultiReturn:   false,
	}

	// Store the instance if it's not a function
	if isInstance {
		descriptor.Instance = constructor
	}

	// Apply options
	if options.Name != "" {
		descriptor.Key = options.Name
	}

	// Handle As option - this allows the service to be registered under interface types
	if len(options.As) > 0 {
		// For As option, we'll need to handle this at the collection level
		// since it affects how the service is registered
	}

	// Cache analysis results for performance
	descriptor.isFunc = info.IsFunc
	descriptor.isResultObject = info.IsResultObject
	descriptor.isParamObject = info.IsParamObject

	// Store param fields if it's a param object
	if info.IsParamObject && len(info.Parameters) > 0 {
		descriptor.paramFields = make([]reflection.ParamField, 0, len(info.Parameters))
		for _, param := range info.Parameters {
			descriptor.paramFields = append(descriptor.paramFields, reflection.ParamField{
				Name:     param.Name,
				Type:     param.Type,
				Key:      param.Key,
				Group:    param.Group,
				Optional: param.Optional,
				Index:    param.Index,
			})
		}
	}

	// Store result fields if it's a result object
	if info.IsResultObject && len(info.Returns) > 0 {
		descriptor.resultFields = make([]reflection.ResultField, 0, len(info.Returns))
		for _, ret := range info.Returns {
			if !ret.IsError {
				descriptor.resultFields = append(descriptor.resultFields, reflection.ResultField{
					Name:  ret.Name,
					Type:  ret.Type,
					Key:   ret.Key,
					Group: ret.Group,
					Index: ret.Index,
				})
			}
		}
	}

	return descriptor, nil
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

// GetGroup returns the groups this provider belongs to
// Implements the Provider interface from the graph package
func (d *Descriptor) GetGroup() string {
	return d.Group
}

// GetDependencies returns the analyzed dependencies
// Implements the Provider interface from the graph package
func (d *Descriptor) GetDependencies() []*reflection.Dependency {
	return d.Dependencies
}

// Validate validates a descriptor
func (d *Descriptor) Validate() error {
	if d == nil {
		return ErrDescriptorNil
	}

	if d.Type == nil {
		return fmt.Errorf("descriptor type cannot be nil")
	}

	if !d.Constructor.IsValid() {
		return fmt.Errorf("descriptor constructor is invalid")
	}

	if d.ConstructorType == nil {
		return fmt.Errorf("descriptor constructor type cannot be nil")
	}

	if d.Key != nil && d.Group != "" {
		return fmt.Errorf("descriptor cannot have both key and group set")
	}

	// Validate lifetime
	switch d.Lifetime {
	case Singleton, Scoped, Transient:
		// Valid lifetimes
	default:
		return LifetimeError{Value: d.Lifetime}
	}

	return nil
}
