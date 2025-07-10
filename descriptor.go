package godi

import (
	"fmt"
	"reflect"
)

// serviceDescriptor describes a service with its type, factory, and lifetime.
// It represents a single service registration in the dependency injection container.
//
// serviceDescriptor is designed to work seamlessly with dig while maintaining
// Microsoft-style DI semantics for lifetimes (Singleton, Scoped, Transient).
type serviceDescriptor struct {
	// ServiceType is the exact type returned by the constructor.
	// This could be an interface, struct, or pointer type.
	ServiceType reflect.Type

	// ServiceKey is the key of the service for keyed services.
	// This allows multiple implementations of the same type to be registered.
	ServiceKey interface{}

	// Lifetime is the lifetime of the service (Singleton, Scoped, or Transient).
	Lifetime ServiceLifetime

	// Constructor is the original constructor function.
	// This should be a function that dig can call directly.
	Constructor interface{}

	// ProvideOptions are the dig options to use when registering this service.
	ProvideOptions []ProvideOption

	// DecorateInfo if set, indicates this is a decorator rather than a provider.
	DecorateInfo *decorateDescriptor

	// Metadata stores optional metadata about the service
	// for debugging and advanced scenarios.
	Metadata map[string]interface{}
}

// decorateDescriptor describes a decorator registration.
type decorateDescriptor struct {
	// Decorator is the decorator function
	Decorator interface{}

	// DecorateOptions are the dig options for decoration
	DecorateOptions []DecorateOption
}

// validate validates the service descriptor to ensure it's properly configured.
func (sd *serviceDescriptor) validate() error {
	if sd.ServiceType == nil {
		return ErrInvalidServiceType
	}

	if !sd.Lifetime.IsValid() {
		return LifetimeError{Value: sd.Lifetime}
	}

	if sd.Constructor == nil && sd.DecorateInfo == nil {
		return ErrNoConstructorOrDecorator
	}

	if sd.Constructor != nil && sd.DecorateInfo != nil {
		return ErrBothConstructorAndDecorator
	}

	// Validate constructor if present
	if sd.Constructor != nil {
		if err := validateConstructorForDig(sd.Constructor); err != nil {
			return fmt.Errorf("invalid constructor: %w", err)
		}
	}

	// Validate decorator if present
	if sd.DecorateInfo != nil && sd.DecorateInfo.Decorator != nil {
		if err := validateDecoratorForDig(sd.DecorateInfo.Decorator); err != nil {
			return fmt.Errorf("invalid decorator: %w", err)
		}
	}

	return nil
}

// validateConstructorForDig validates that a constructor is compatible with dig.
func validateConstructorForDig(constructor interface{}) error {
	if constructor == nil {
		return ErrNilConstructor
	}

	fnType := reflect.TypeOf(constructor)
	if fnType.Kind() != reflect.Func {
		return ErrConstructorNotFunction
	}

	// Check if any parameter uses In
	usesDigIn := false
	for i := 0; i < fnType.NumIn(); i++ {
		inType := fnType.In(i)
		if inType.Kind() == reflect.Struct && IsIn(inType) {
			if usesDigIn {
				return ErrConstructorMultipleIn
			}

			usesDigIn = true
		}
	}

	// Check outputs
	usesDigOut := false
	if fnType.NumOut() > 0 {
		outType := fnType.Out(0)
		if outType.Kind() == reflect.Struct && IsOut(outType) {
			usesDigOut = true
		}
	}

	if usesDigOut {
		// Can only return the result object or (result object, error)
		if fnType.NumOut() > 2 {
			return ErrConstructorOutMaxReturns
		}

		if fnType.NumOut() == 2 {
			errorType := reflect.TypeOf((*error)(nil)).Elem()
			if !fnType.Out(1).Implements(errorType) {
				return ErrConstructorInvalidSecondReturn
			}
		}
	} else {
		// Regular constructor validation
		if fnType.NumOut() == 0 {
			return ErrConstructorNoReturn
		}

		if fnType.NumOut() > 2 {
			return ErrConstructorTooManyReturns
		}

		if fnType.NumOut() == 2 {
			errorType := reflect.TypeOf((*error)(nil)).Elem()
			if !fnType.Out(1).Implements(errorType) {
				return ErrConstructorInvalidSecondReturn
			}
		}
	}

	return nil
}

// validateDecoratorForDig validates that a decorator is compatible with dig.
func validateDecoratorForDig(decorator interface{}) error {
	if decorator == nil {
		return ErrDecoratorNil
	}

	fnType := reflect.TypeOf(decorator)
	if fnType.Kind() != reflect.Func {
		return ErrDecoratorNotFunction
	}

	// Decorators must have at least one input (the value being decorated)
	if fnType.NumIn() == 0 {
		return ErrDecoratorNoParams
	}

	// Decorators must return at least one value
	if fnType.NumOut() == 0 {
		return ErrDecoratorNoReturn
	}

	return nil
}

// newServiceDescriptor creates a descriptor from a constructor function.
// The service type is inferred from the constructor's return type.
func newServiceDescriptor(constructor interface{}, lifetime ServiceLifetime) (*serviceDescriptor, error) {
	if err := validateConstructorForDig(constructor); err != nil {
		return nil, err
	}

	fnType := reflect.TypeOf(constructor)

	// Determine service type from return type
	var serviceType reflect.Type
	if fnType.NumOut() > 0 {
		firstOut := fnType.Out(0)
		if firstOut.Kind() == reflect.Struct && IsOut(firstOut) {
			// This is a result object - we'll handle it differently
			return nil, ErrResultObjectConstructor
		}

		serviceType = firstOut
	}

	sd := &serviceDescriptor{
		ServiceType:    serviceType,
		Lifetime:       lifetime,
		Constructor:    constructor,
		ProvideOptions: []ProvideOption{},
		Metadata:       make(map[string]interface{}),
	}

	return sd, nil
}

// isKeyedService indicates whether the service is a keyed service.
func (sd *serviceDescriptor) isKeyedService() bool {
	return sd.ServiceKey != nil
}

// isDecorator indicates whether this descriptor represents a decorator.
func (sd *serviceDescriptor) isDecorator() bool {
	return sd.DecorateInfo != nil
}
