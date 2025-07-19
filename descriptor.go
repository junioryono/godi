package godi

import (
	"fmt"
	"reflect"
)

// serviceDescriptor describes a service with its type, factory, and lifetime.
// It represents a single service registration in the dependency injection container.
//
// serviceDescriptor is designed to work seamlessly with dig while maintaining
// Microsoft-style DI semantics for lifetimes (Singleton, Scoped).
type serviceDescriptor struct {
	// ServiceType is the exact type returned by the constructor.
	// This could be an interface, struct, or pointer type.
	ServiceType reflect.Type

	// ServiceKey is the key of the service for keyed services.
	// This allows multiple implementations of the same type to be registered.
	ServiceKey interface{}

	// Lifetime is the lifetime of the service (Singleton, Scoped).
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
		if err := validateConstructor(sd.Constructor); err != nil {
			return fmt.Errorf("invalid constructor: %w", err)
		}
	}

	// Validate decorator if present
	if sd.DecorateInfo != nil && sd.DecorateInfo.Decorator != nil {
		if err := validateDecorator(sd.DecorateInfo.Decorator); err != nil {
			return fmt.Errorf("invalid decorator: %w", err)
		}
	}

	return nil
}

// newServiceDescriptor creates a descriptor from a constructor function.
// The service type is inferred from the constructor's return type.
func newServiceDescriptor(constructor interface{}, lifetime ServiceLifetime) (*serviceDescriptor, error) {
	if err := validateConstructor(constructor); err != nil {
		return nil, err
	}

	fnType := reflect.TypeOf(constructor)
	fnInfo := globalTypeCache.getTypeInfo(fnType)

	// Determine service type from return type
	var serviceType reflect.Type
	if fnInfo.NumOut > 0 {
		firstOut := fnInfo.OutTypes[0]
		firstOutInfo := globalTypeCache.getTypeInfo(firstOut)
		if firstOutInfo.IsStruct && firstOutInfo.HasOutField {
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
