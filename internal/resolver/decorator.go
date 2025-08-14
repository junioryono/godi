package resolver

import (
	"fmt"
	"reflect"

	"github.com/junioryono/godi/v3/internal/reflection"
	"github.com/junioryono/godi/v3/internal/registry"
)

// DecoratorProcessor handles decorator application during resolution.
type DecoratorProcessor struct {
	registry *registry.ServiceCollection
	analyzer *reflection.Analyzer
	invoker  *reflection.ConstructorInvoker
}

// NewDecoratorProcessor creates a new decorator processor.
func NewDecoratorProcessor(
	reg *registry.ServiceCollection,
	analyzer *reflection.Analyzer,
) *DecoratorProcessor {
	if reg == nil {
		panic("registry cannot be nil")
	}
	if analyzer == nil {
		panic("analyzer cannot be nil")
	}

	return &DecoratorProcessor{
		registry: reg,
		analyzer: analyzer,
		invoker:  reflection.NewConstructorInvoker(analyzer),
	}
}

// ApplyDecorators applies all registered decorators to an instance.
func (p *DecoratorProcessor) ApplyDecorators(
	instance any,
	serviceType reflect.Type,
	ctx ResolutionContext,
) (any, error) {
	if instance == nil {
		return nil, fmt.Errorf("instance cannot be nil")
	}

	// Get decorators for this type
	decorators := p.registry.GetDecorators(serviceType)
	if len(decorators) == 0 {
		return instance, nil
	}

	// Apply decorators in reverse order
	current := instance
	for i := len(decorators) - 1; i >= 0; i-- {
		decorator := decorators[i]
		decorated, err := p.applyDecorator(current, decorator, ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to apply decorator %d for %v: %w", i, serviceType, err)
		}
		current = decorated
	}

	return current, nil
}

// applyDecorator applies a single decorator to an instance.
func (p *DecoratorProcessor) applyDecorator(
	instance any,
	decorator *registry.Descriptor,
	ctx ResolutionContext,
) (any, error) {
	// Analyze the decorator function
	info, err := p.analyzer.Analyze(decorator.Constructor.Interface())
	if err != nil {
		return nil, fmt.Errorf("failed to analyze decorator: %w", err)
	}

	// Validate decorator signature
	if err := p.validateDecorator(info, instance); err != nil {
		return nil, err
	}

	// Build arguments for decorator
	args, err := p.buildDecoratorArgs(info, instance, ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to build decorator arguments: %w", err)
	}

	// Call the decorator
	results := decorator.Constructor.Call(args)

	// Handle error return if present
	if len(results) > 1 && !results[len(results)-1].IsNil() {
		if err, ok := results[len(results)-1].Interface().(error); ok {
			return nil, fmt.Errorf("decorator error: %w", err)
		}
	}

	// Return decorated instance
	if len(results) > 0 {
		return results[0].Interface(), nil
	}

	return nil, fmt.Errorf("decorator returned no value")
}

// validateDecorator validates that a decorator can be applied to an instance.
func (p *DecoratorProcessor) validateDecorator(info *reflection.ConstructorInfo, instance any) error {
	if !info.IsFunc {
		return fmt.Errorf("decorator must be a function")
	}

	if len(info.Parameters) == 0 {
		return fmt.Errorf("decorator must have at least one parameter")
	}

	if len(info.Returns) == 0 {
		return fmt.Errorf("decorator must return at least one value")
	}

	// Check that first parameter matches instance type
	instanceType := reflect.TypeOf(instance)
	firstParamType := info.Parameters[0].Type

	if !instanceType.AssignableTo(firstParamType) {
		return fmt.Errorf("decorator expects %v but got %v", firstParamType, instanceType)
	}

	// Check that return type matches or is compatible
	firstReturnType := info.Returns[0].Type
	if !firstReturnType.AssignableTo(instanceType) && !instanceType.AssignableTo(firstReturnType) {
		return fmt.Errorf("decorator returns %v but expected compatible with %v",
			firstReturnType, instanceType)
	}

	return nil
}

// buildDecoratorArgs builds the argument list for a decorator function.
func (p *DecoratorProcessor) buildDecoratorArgs(
	info *reflection.ConstructorInfo,
	instance any,
	ctx ResolutionContext,
) ([]reflect.Value, error) {
	args := make([]reflect.Value, len(info.Parameters))

	// First argument is the instance being decorated
	args[0] = reflect.ValueOf(instance)

	// Resolve additional dependencies if any
	depResolver := &contextDependencyResolver{ctx: ctx}
	for i := 1; i < len(info.Parameters); i++ {
		param := info.Parameters[i]

		var resolved any
		var err error

		if param.Group != "" {
			resolved, err = depResolver.ResolveGroup(param.Type, param.Group)
		} else if param.Key != nil {
			resolved, err = depResolver.ResolveKeyed(param.Type, param.Key)
		} else {
			resolved, err = depResolver.Resolve(param.Type)
		}

		if err != nil {
			if !param.Optional {
				return nil, fmt.Errorf("failed to resolve decorator dependency %d: %w", i, err)
			}
			// Optional parameter - use zero value
			args[i] = reflect.Zero(param.Type)
		} else {
			args[i] = reflect.ValueOf(resolved)
		}
	}

	return args, nil
}

// DecoratorChain represents a chain of decorators to be applied.
type DecoratorChain struct {
	decorators []DecoratorFunc
}

// DecoratorFunc is a function that decorates an instance.
type DecoratorFunc func(instance any) (any, error)

// NewDecoratorChain creates a new decorator chain.
func NewDecoratorChain(decorators ...DecoratorFunc) *DecoratorChain {
	return &DecoratorChain{
		decorators: decorators,
	}
}

// Apply applies all decorators in the chain to an instance.
func (c *DecoratorChain) Apply(instance any) (any, error) {
	current := instance

	for i, decorator := range c.decorators {
		decorated, err := decorator(current)
		if err != nil {
			return nil, fmt.Errorf("decorator %d failed: %w", i, err)
		}
		current = decorated
	}

	return current, nil
}

// Add adds a decorator to the chain.
func (c *DecoratorChain) Add(decorator DecoratorFunc) {
	c.decorators = append(c.decorators, decorator)
}

// Len returns the number of decorators in the chain.
func (c *DecoratorChain) Len() int {
	return len(c.decorators)
}
