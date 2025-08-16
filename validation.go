package godi

import (
	"fmt"

	"github.com/junioryono/godi/v3/internal/graph"
)

// validateDependencyGraph validates the entire dependency graph for cycles
func validateDependencyGraph(c *collection) error {
	// Build the dependency graph
	g := graph.NewDependencyGraph()

	// Add all providers to the graph
	for _, descriptor := range c.services {
		if err := g.AddProvider(descriptor); err != nil {
			return fmt.Errorf("failed to add provider %v to graph: %w", descriptor.Type, err)
		}
	}

	for _, descriptors := range c.groups {
		for _, descriptor := range descriptors {
			if err := g.AddProvider(descriptor); err != nil {
				return fmt.Errorf("failed to add group provider %v to graph: %w", descriptor.Type, err)
			}
		}
	}

	// Check for cycles
	if err := g.DetectCycles(); err != nil {
		return err
	}

	return nil
}

// validateLifetimes ensures singleton services don't depend on scoped services
func validateLifetimes(c *collection) error {
	// Create a map of service lifetimes
	lifetimes := make(map[instanceKey]Lifetime)

	// Populate lifetimes from all services
	for serviceType, descriptor := range c.services {
		key := instanceKey{Type: serviceType.Type, Key: descriptor.Key}
		lifetimes[key] = descriptor.Lifetime
	}

	for groupKey, descriptors := range c.groups {
		for _, descriptor := range descriptors {
			key := instanceKey{Type: groupKey.Type, Key: descriptor.Key, Group: groupKey.Group}
			lifetimes[key] = descriptor.Lifetime
		}
	}

	checkDescriptor := func(descriptor *Descriptor) error {
		if descriptor.Lifetime != Singleton {
			return nil // Only validate singleton dependencies
		}

		// Get dependencies
		for _, dep := range descriptor.Dependencies {
			depKey := instanceKey{Type: dep.Type, Key: dep.Key, Group: dep.Group}

			// Find the lifetime of the dependency
			if depLifetime, ok := lifetimes[depKey]; ok {
				if depLifetime == Scoped {
					return fmt.Errorf(
						"singleton service %v cannot depend on scoped service %v",
						descriptor.Type, dep.Type,
					)
				}
			}
		}

		return nil
	}

	// Check all services
	for _, descriptor := range c.services {
		if err := checkDescriptor(descriptor); err != nil {
			return err
		}
	}

	for _, descriptors := range c.groups {
		for _, descriptor := range descriptors {
			if err := checkDescriptor(descriptor); err != nil {
				return err
			}
		}
	}

	return nil
}

// validateDescriptor validates a single descriptor
func validateDescriptor(descriptor *Descriptor) error {
	if descriptor == nil {
		return ErrDescriptorNil
	}

	if descriptor.Type == nil {
		return fmt.Errorf("descriptor type cannot be nil")
	}

	if !descriptor.Constructor.IsValid() {
		return fmt.Errorf("descriptor constructor is invalid")
	}

	// Validate lifetime
	switch descriptor.Lifetime {
	case Singleton, Scoped, Transient:
		// Valid lifetimes
	default:
		return fmt.Errorf("invalid lifetime: %v", descriptor.Lifetime)
	}

	return nil
}
