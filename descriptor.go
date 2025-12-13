package godi

import (
	"fmt"
	"reflect"
	"strconv"
	"sync/atomic"

	"github.com/junioryono/godi/v4/internal/reflection"
)

// Global atomic counter for fast void-return service key generation
var voidKeyCounter uint64

// Descriptor represents services
type Descriptor struct {
	// Type is the service type this descriptor produces
	Type reflect.Type

	// Key is optional - for named/keyed services
	Key any

	// Group this provider belongs to
	Group string

	// Lifetime determines instance caching behavior
	Lifetime Lifetime

	// Constructor is the reflected function value
	Constructor reflect.Value

	// ConstructorType is the type of the constructor function
	ConstructorType reflect.Type

	// Dependencies are the analyzed dependencies
	Dependencies []*reflection.Dependency

	// As is an optional list of interface types this service can be registered as
	// This is typically used for interface-based services
	As []any

	// IsInstance indicates if this descriptor holds an instance value
	IsInstance bool

	// Instance is the actual instance value when IsInstance is true
	Instance any

	// ReturnIndex indicates which return value this descriptor represents
	// -1 for single returns or Out structs, >= 0 for specific return index in multi-return
	MultiReturnIndex int

	// VoidReturn indicates if the constructor has no valid return values
	VoidReturn bool

	// Analysis results cached for performance
	isFunc         bool
	isResultObject bool
	resultFields   []reflection.ResultField
	isParamObject  bool
	paramFields    []reflection.ParamField
}

// newDescriptor creates a new descriptor from a service with the given lifetime and options
func newDescriptor(service any, lifetime Lifetime, opts ...AddOption) (*Descriptor, error) {
	return newDescriptorWithAnalyzer(service, lifetime, nil, opts...)
}

// newDescriptorWithAnalyzer creates a new descriptor using the provided analyzer for caching
func newDescriptorWithAnalyzer(service any, lifetime Lifetime, analyzer *reflection.Analyzer, opts ...AddOption) (*Descriptor, error) {
	if service == nil {
		return nil, &ValidationError{
			ServiceType: nil,
			Cause:       ErrConstructorNil,
		}
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
	constructorValue := reflect.ValueOf(service)

	// Check for nil pointers
	if !constructorValue.IsValid() || (constructorValue.Kind() == reflect.Pointer && constructorValue.IsNil()) {
		return nil, &ValidationError{
			ServiceType: nil,
			Cause:       ErrConstructorNil,
		}
	}

	constructorType := constructorValue.Type()

	// Check if it's an instance (not a function)
	isInstance := constructorType.Kind() != reflect.Func

	// Use provided analyzer or create one (for backward compatibility)
	if analyzer == nil {
		analyzer = reflection.New()
	}

	info, err := analyzer.Analyze(service)
	if err != nil {
		return nil, &ReflectionAnalysisError{
			Constructor: service,
			Operation:   "analyze",
			Cause:       err,
		}
	}

	// Get dependencies from analyzer (uses cache)
	dependencies, err := analyzer.GetDependencies(service)
	if err != nil {
		return nil, &ReflectionAnalysisError{
			Constructor: service,
			Operation:   "dependencies",
			Cause:       err,
		}
	}

	// Create descriptor
	descriptor := &Descriptor{
		Lifetime:         lifetime,
		Constructor:      constructorValue,
		ConstructorType:  constructorType,
		Dependencies:     dependencies,
		Group:            options.Group,
		IsInstance:       isInstance,
		Instance:         nil,
		MultiReturnIndex: -1,
	}

	// Store the instance if it's not a function
	if isInstance {
		descriptor.Instance = service
		descriptor.Type = constructorType
	} else {
		numReturns := constructorType.NumOut()

		descriptor.VoidReturn = numReturns == 0
		if !descriptor.VoidReturn {
			// Check if there are only errors in returns
			areAllErrors := true
			for i := range numReturns {
				if !constructorType.Out(i).Implements(reflect.TypeOf((*error)(nil)).Elem()) {
					areAllErrors = false
					break
				}
			}

			descriptor.VoidReturn = areAllErrors
		}

		if descriptor.VoidReturn {
			descriptor.Type = reflect.TypeOf((*struct{})(nil)).Elem()
			if descriptor.Key == nil {
				// Use fast atomic counter instead of UUID
				descriptor.Key = "v" + strconv.FormatUint(atomic.AddUint64(&voidKeyCounter, 1), 36)
			}
		} else {
			// Normal function with returns
			descriptor.Type = constructorType.Out(0)
		}
	}

	// Apply options
	if options.Name != "" {
		descriptor.Key = options.Name
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

// GetType returns the service type this descriptor produces.
// This method implements the Provider interface from the graph package,
// enabling the descriptor to participate in dependency resolution.
func (d *Descriptor) GetType() reflect.Type {
	return d.Type
}

// GetKey returns the optional key for named/keyed services.
// Returns nil for non-keyed services. This method implements the Provider
// interface from the graph package for keyed service resolution.
func (d *Descriptor) GetKey() any {
	return d.Key
}

// GetGroup returns the group this provider belongs to.
// Returns empty string if not part of a group. This method implements
// the Provider interface from the graph package for group-based resolution.
func (d *Descriptor) GetGroup() string {
	return d.Group
}

// GetDependencies returns the analyzed dependencies for this descriptor.
// These dependencies must be resolved before this service can be created.
// This method implements the Provider interface from the graph package.
func (d *Descriptor) GetDependencies() []*reflection.Dependency {
	return d.Dependencies
}

// Validate validates the descriptor's configuration.
// It checks that the descriptor has a valid type, constructor, and lifetime,
// and ensures that key and group are not both set simultaneously.
func (d *Descriptor) Validate() error {
	if d.Type == nil {
		return &ValidationError{
			ServiceType: nil,
			Cause:       ErrDescriptorNil,
		}
	}

	if !d.Constructor.IsValid() {
		return &ValidationError{
			ServiceType: d.Type,
			Cause:       ErrConstructorNil,
		}
	}

	if d.ConstructorType == nil {
		return &ValidationError{
			ServiceType: d.Type,
			Cause:       ErrConstructorNil,
		}
	}

	if d.Key != nil && d.Group != "" {
		return &ValidationError{
			ServiceType: d.Type,
			Cause:       fmt.Errorf("descriptor cannot have both key and group set"),
		}
	}

	// Validate lifetime
	switch d.Lifetime {
	case Singleton, Scoped, Transient:
		// Valid lifetimes
	default:
		return LifetimeError{Value: d.Lifetime}
	}

	// For function constructors, validate return types
	if d.isFunc && !d.VoidReturn {
		if err := d.validateReturnTypes(); err != nil {
			return err
		}
	}

	// Validate parameter types (dependencies)
	if err := d.validateParameterTypes(); err != nil {
		return err
	}

	return nil
}

// validateReturnTypes validates that constructor return types are valid
func (d *Descriptor) validateReturnTypes() error {
	if d.ConstructorType == nil || d.ConstructorType.Kind() != reflect.Func {
		return nil
	}

	numOut := d.ConstructorType.NumOut()
	for i := 0; i < numOut; i++ {
		outType := d.ConstructorType.Out(i)

		// Check for invalid types
		if outType.Kind() == reflect.Invalid {
			return &ValidationError{
				ServiceType: d.Type,
				Cause:       fmt.Errorf("constructor return type at index %d is invalid", i),
			}
		}

		// Skip error type validation
		if outType.Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			continue
		}

		// Check for chan return types (generally not suitable for DI)
		if outType.Kind() == reflect.Chan {
			return &ValidationError{
				ServiceType: d.Type,
				Cause:       fmt.Errorf("constructor return type at index %d is a channel type, which is not supported as a service type", i),
			}
		}

		// Check for unsafe pointer
		if outType.Kind() == reflect.UnsafePointer {
			return &ValidationError{
				ServiceType: d.Type,
				Cause:       fmt.Errorf("constructor return type at index %d is an unsafe pointer, which is not supported as a service type", i),
			}
		}
	}

	return nil
}

// validateParameterTypes validates that constructor parameter types are valid for DI
func (d *Descriptor) validateParameterTypes() error {
	for _, dep := range d.Dependencies {
		if dep == nil {
			continue
		}

		depType := dep.Type
		if depType == nil {
			continue
		}

		// Check for invalid dependency types
		if depType.Kind() == reflect.Invalid {
			return &ValidationError{
				ServiceType: d.Type,
				Cause:       fmt.Errorf("dependency type is invalid"),
			}
		}

		// Check for unsupported primitive types as dependencies (not slices for groups)
		// Group dependencies are slices, which should not have chan/unsafe pointer validation
		if dep.Group == "" {
			switch depType.Kind() {
			case reflect.Chan:
				return &ValidationError{
					ServiceType: d.Type,
					Cause:       fmt.Errorf("channel type %s is not supported as a dependency; use an interface or struct instead", depType),
				}
			case reflect.UnsafePointer:
				return &ValidationError{
					ServiceType: d.Type,
					Cause:       fmt.Errorf("unsafe pointer is not supported as a dependency"),
				}
			}
		}
	}

	return nil
}
