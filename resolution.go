package godi

import (
	"fmt"
	"reflect"

	"github.com/junioryono/godi/v3/internal/reflection"
)

// resolutionContext tracks the state during resolution
type resolutionContext struct {
	scope    *scope
	stack    []instanceKey
	depth    int
	maxDepth int
	resolver dependencyResolver
}

// dependencyResolver adapts scope to the reflection.DependencyResolver interface
type dependencyResolver struct {
	scope *scope
	ctx   *resolutionContext
}

func (r dependencyResolver) Resolve(t reflect.Type) (any, error) {
	return r.scope.Get(t)
}

func (r dependencyResolver) ResolveKeyed(t reflect.Type, key any) (any, error) {
	return r.scope.GetKeyed(t, key)
}

func (r dependencyResolver) ResolveGroup(t reflect.Type, group string) ([]any, error) {
	return r.scope.GetGroup(t, group)
}

// createInstance creates a new instance of a service
func (s *scope) createInstance(descriptor *Descriptor) (any, error) {
	if descriptor == nil {
		return nil, fmt.Errorf("descriptor cannot be nil")
	}

	// Create resolution context
	ctx := &resolutionContext{
		scope:    s,
		stack:    make([]instanceKey, 0),
		depth:    0,
		maxDepth: 100,
	}
	ctx.resolver = dependencyResolver{scope: s, ctx: ctx}

	// Analyze constructor
	info, err := s.provider.analyzer.Analyze(descriptor.Constructor.Interface())
	if err != nil {
		return nil, fmt.Errorf("failed to analyze constructor: %w", err)
	}

	// Create invoker
	invoker := reflection.NewConstructorInvoker(s.provider.analyzer)

	// Invoke constructor
	results, err := invoker.Invoke(info, ctx.resolver)
	if err != nil {
		return nil, fmt.Errorf("constructor invocation failed: %w", err)
	}

	// Handle result objects (Out structs)
	if info.IsResultObject && len(results) > 0 {
		processor := reflection.NewResultObjectProcessor(s.provider.analyzer)
		registrations, err := processor.ProcessResultObject(results[0], info.Type.Out(0))
		if err != nil {
			return nil, fmt.Errorf("failed to process result object: %w", err)
		}

		// Find the primary service to return
		for _, reg := range registrations {
			if reg.Type == descriptor.Type && reg.Key == descriptor.Key {
				instance := reg.Value
				// Apply decorators
				decorated, err := s.applyDecorators(instance, descriptor.Type)
				if err != nil {
					return nil, err
				}
				return decorated, nil
			}
		}

		// If no exact match, return the first one
		if len(registrations) > 0 {
			instance := registrations[0].Value
			// Apply decorators
			decorated, err := s.applyDecorators(instance, descriptor.Type)
			if err != nil {
				return nil, err
			}
			return decorated, nil
		}

		return nil, fmt.Errorf("result object produced no services")
	}

	// Regular constructor - get first result
	if len(results) == 0 {
		return nil, fmt.Errorf("constructor returned no values")
	}

	instance := results[0].Interface()

	// Apply decorators
	decorated, err := s.applyDecorators(instance, descriptor.Type)
	if err != nil {
		return nil, err
	}

	return decorated, nil
}

// createAllSingletons creates all singleton instances at build time
func (p *provider) createAllSingletons() error {
	// Get topological sort from dependency graph
	sorted, err := p.graph.TopologicalSort()
	if err != nil {
		return fmt.Errorf("failed to sort dependencies: %w", err)
	}

	// Create instances in dependency order
	for _, node := range sorted {
		// Find the descriptor for this node
		var descriptor *Descriptor

		// Check if it's a provider node
		if provider, ok := node.Provider.(*Descriptor); ok {
			descriptor = provider
		} else {
			// Try to find descriptor by type and key
			descriptor = p.findDescriptor(node.Key.Type, node.Key.Key)
		}

		if descriptor == nil {
			continue
		}

		// Only create singletons
		if descriptor.Lifetime != Singleton {
			continue
		}

		// Create instance key
		key := instanceKey{
			Type: descriptor.Type,
			Key:  descriptor.Key,
		}

		// Check if already created
		if _, exists := p.getSingleton(key); exists {
			continue
		}

		// Create the instance
		instance, err := p.rootScope.createInstance(descriptor)
		if err != nil {
			return fmt.Errorf("failed to create singleton %v: %w", descriptor.Type, err)
		}

		// Store the singleton
		p.setSingleton(key, instance)
	}

	return nil
}
