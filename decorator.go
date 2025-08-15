package godi

import (
	"fmt"
	"reflect"
)

// applyDecorators applies all registered decorators to an instance
func (s *scope) applyDecorators(instance any, serviceType reflect.Type) (any, error) {
	if instance == nil {
		return nil, fmt.Errorf("instance cannot be nil")
	}

	// Get decorators for this type
	decorators := s.provider.getDecorators(serviceType)
	if len(decorators) == 0 {
		return instance, nil
	}

	current := instance

	// Apply decorators in registration order (first registered = innermost)
	for i, decorator := range decorators {
		decorated, err := s.invokeDecorator(decorator, current)
		if err != nil {
			return nil, fmt.Errorf("decorator %d failed for %v: %w", i, serviceType, err)
		}
		current = decorated
	}

	return current, nil
}

// invokeDecorator invokes a single decorator
func (s *scope) invokeDecorator(decorator *Descriptor, instance any) (any, error) {
	if decorator == nil || !decorator.IsDecorator {
		return nil, fmt.Errorf("invalid decorator")
	}

	// Get the decorator function value
	decoratorFunc := decorator.Constructor

	// Validate decorator signature
	decoratorType := decoratorFunc.Type()
	if decoratorType.NumIn() < 1 {
		return nil, fmt.Errorf("decorator must have at least one parameter")
	}
	if decoratorType.NumOut() < 1 {
		return nil, fmt.Errorf("decorator must return at least one value")
	}

	// Check that first parameter matches instance type
	instanceType := reflect.TypeOf(instance)
	firstParamType := decoratorType.In(0)

	if !instanceType.AssignableTo(firstParamType) {
		return nil, fmt.Errorf("decorator expects %v but got %v", firstParamType, instanceType)
	}

	// Build arguments for decorator
	args := make([]reflect.Value, decoratorType.NumIn())
	args[0] = reflect.ValueOf(instance)

	// Resolve additional dependencies if any
	for i := 1; i < decoratorType.NumIn(); i++ {
		paramType := decoratorType.In(i)

		// Try to resolve the dependency
		dep, err := s.Get(paramType)
		if err != nil {
			// Check if parameter is a pointer and can be nil
			if paramType.Kind() == reflect.Ptr {
				args[i] = reflect.Zero(paramType)
			} else {
				return nil, fmt.Errorf("failed to resolve decorator dependency %d: %w", i, err)
			}
		} else {
			args[i] = reflect.ValueOf(dep)
		}
	}

	// Call the decorator
	results := decoratorFunc.Call(args)

	// Handle error return if present
	if decoratorType.NumOut() > 1 {
		lastResult := results[len(results)-1]
		if lastResult.Type().Implements(reflect.TypeOf((*error)(nil)).Elem()) {
			if !lastResult.IsNil() {
				if err, ok := lastResult.Interface().(error); ok {
					return nil, fmt.Errorf("decorator error: %w", err)
				}
			}
		}
	}

	// Return decorated instance
	if len(results) > 0 {
		return results[0].Interface(), nil
	}

	return nil, fmt.Errorf("decorator returned no value")
}
