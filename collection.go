package godi

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/junioryono/godi/v3/internal/graph"
	"github.com/junioryono/godi/v3/internal/reflection"
)

// Collection represents a collection of service descriptors that define
// the services available in the dependency injection container.
//
// Collection follows a builder pattern where services are registered
// with their lifetimes and dependencies, then built into a Provider.
//
// Collection is NOT thread-safe. It should be configured in a single
// goroutine before building the Provider.
//
// Example:
//
//	collection := godi.NewCollection()
//	collection.AddSingleton(NewLogger)
//	collection.AddScoped(NewDatabase)
//
//	provider, err := collection.Build()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer provider.Close()
type Collection interface {
	// Build creates a Provider from the registered services
	// using default options.
	Build() (Provider, error)

	// BuildWithOptions creates a Provider with custom options
	// for validation and behavior configuration.
	BuildWithOptions(options *ProviderOptions) (Provider, error)

	// AddModules applies one or more module configurations to the service collection.
	// Modules provide a way to group related service registrations.
	AddModules(modules ...ModuleOption) error

	// AddSingleton registers a service with singleton lifetime.
	// Only one instance is created and shared across all resolutions.
	AddSingleton(constructor any, opts ...AddOption) error

	// AddScoped registers a service with scoped lifetime.
	// One instance is created per scope and shared within that scope.
	AddScoped(constructor any, opts ...AddOption) error

	// AddTransient registers a service with transient lifetime.
	// A new instance is created every time the service is resolved.
	AddTransient(constructor any, opts ...AddOption) error

	// Decorate registers a decorator for a service type.
	// Decorators wrap existing services to modify their behavior.
	Decorate(decorator any, opts ...AddOption) error

	// HasService checks if a service exists for the type.
	HasService(serviceType reflect.Type) bool

	// HasKeyedService checks if a keyed service exists.
	HasKeyedService(serviceType reflect.Type, key any) bool

	// Remove removes all services for a given service type.
	Remove(serviceType reflect.Type)

	// RemoveKeyed removes a specific keyed service.
	RemoveKeyed(serviceType reflect.Type, key any)

	// ToSlice returns a copy of all registered service descriptors.
	// This is useful for inspection and debugging.
	ToSlice() []*Descriptor

	// Count returns the number of registered services.
	Count() int
}

// Collection is the core service registry that manages services and decorators.
type collection struct {
	mu sync.RWMutex

	// services stores all non-keyed services by type
	services map[TypeKey]*Descriptor

	// groups stores services that belong to groups
	groups map[GroupKey][]*Descriptor

	// decorators stores decorator descriptors by type
	// These are just Descriptors with IsDecorator=true
	decorators map[reflect.Type][]*Descriptor
}

// TypeKey uniquely identifies a keyed service
type TypeKey struct {
	Type reflect.Type
	Key  any
}

// GroupKey uniquely identifies a group of services
type GroupKey struct {
	Type  reflect.Type
	Group string
}

// NewCollection creates a new empty Collection instance.
//
// Example:
//
//	collection := godi.NewCollection()
//	collection.AddSingleton(NewLogger)
//	provider, err := collection.Build()
func NewCollection() Collection {
	return &collection{
		services:   make(map[TypeKey]*Descriptor),
		groups:     make(map[GroupKey][]*Descriptor),
		decorators: make(map[reflect.Type][]*Descriptor),
	}
}

// Build creates a Provider from the registered services using default options.
func (sc *collection) Build() (Provider, error) {
	return sc.BuildWithOptions(nil)
}

// BuildWithOptions creates a Provider with custom options for validation and behavior configuration.
func (sc *collection) BuildWithOptions(options *ProviderOptions) (Provider, error) {
	// Get all descriptors before locking to avoid deadlock
	allDescriptors := sc.ToSlice()

	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Validate the dependency graph
	if err := validateDependencyGraph(sc); err != nil {
		return nil, fmt.Errorf("dependency validation failed: %w", err)
	}

	// Validate lifetime consistency
	if err := validateLifetimes(sc); err != nil {
		return nil, fmt.Errorf("lifetime validation failed: %w", err)
	}

	// Build the dependency graph
	g := graph.NewDependencyGraph()

	// Add all providers to the graph
	for _, descriptor := range allDescriptors {
		if !descriptor.IsDecorator {
			if err := g.AddProvider(descriptor); err != nil {
				return nil, fmt.Errorf("failed to build dependency graph: %w", err)
			}
		}
	}

	// Create the provider
	p := &provider{
		services:    sc.services,
		groups:      sc.groups,
		decorators:  sc.decorators,
		graph:       g,
		analyzer:    reflection.New(),
		singletons:  make(map[instanceKey]any),
		disposables: make([]Disposable, 0),
		scopes:      make(map[*scope]struct{}),
		built:       true,
	}

	// Create root scope
	rootCtx := context.Background()
	p.rootScope = &scope{
		provider:    p,
		context:     rootCtx,
		instances:   make(map[instanceKey]any),
		disposables: make([]Disposable, 0),
		resolving:   make(map[instanceKey]struct{}),
		children:    make(map[*scope]struct{}),
	}

	// Eagerly create all singletons
	if err := p.createAllSingletons(); err != nil {
		return nil, fmt.Errorf("failed to initialize singletons: %w", err)
	}

	return p, nil
}

// AddModules applies one or more module configurations to the service collection.
func (sc *collection) AddModules(modules ...ModuleOption) error {
	for _, module := range modules {
		if module == nil {
			continue
		}

		if err := module(sc); err != nil {
			return err
		}
	}

	return nil
}

// AddSingleton adds a singleton service to the collection.
func (sc *collection) AddSingleton(constructor any, opts ...AddOption) error {
	return sc.addService(constructor, Singleton, opts...)
}

// AddScoped adds a scoped service to the collection.
func (sc *collection) AddScoped(constructor any, opts ...AddOption) error {
	return sc.addService(constructor, Scoped, opts...)
}

// AddTransient adds a transient service to the collection.
func (sc *collection) AddTransient(constructor any, opts ...AddOption) error {
	return sc.addService(constructor, Transient, opts...)
}

// Decorate registers a decorator for a service type.
func (sc *collection) Decorate(decorator any, opts ...AddOption) error {
	return nil
}

// HasService checks if a service exists for the type
func (r *collection) HasService(t reflect.Type) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	typeKey := TypeKey{Type: t}
	_, ok := r.services[typeKey]
	return ok
}

// HasKeyedService checks if a keyed service exists
func (r *collection) HasKeyedService(t reflect.Type, key any) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	typeKey := TypeKey{Type: t, Key: key}
	_, ok := r.services[typeKey]
	return ok
}

// HasGroup checks if a group has any services
func (r *collection) HasGroup(t reflect.Type, group string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	groupKey := GroupKey{Type: t, Group: group}
	services, ok := r.groups[groupKey]
	return ok && len(services) > 0
}

// Remove removes all services for a given type
func (r *collection) Remove(t reflect.Type) {
	r.mu.Lock()
	defer r.mu.Unlock()

	typeKey := TypeKey{Type: t}
	delete(r.services, typeKey)
}

// RemoveKeyed removes a specific keyed service
func (r *collection) RemoveKeyed(t reflect.Type, key any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	typeKey := TypeKey{Type: t, Key: key}
	delete(r.services, typeKey)
}

// ToSlice returns a copy of all registered service descriptors
func (r *collection) ToSlice() []*Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Use a map to track unique descriptors and avoid duplicates
	seen := make(map[*Descriptor]bool)
	descriptors := make([]*Descriptor, 0)

	// Add regular services
	for _, service := range r.services {
		if !seen[service] {
			descriptors = append(descriptors, service)
			seen[service] = true
		}
	}

	// Add grouped services
	for _, groupServices := range r.groups {
		for _, service := range groupServices {
			if !seen[service] {
				descriptors = append(descriptors, service)
				seen[service] = true
			}
		}
	}

	// Add decorators
	for _, decoratorList := range r.decorators {
		for _, decorator := range decoratorList {
			if !seen[decorator] {
				descriptors = append(descriptors, decorator)
				seen[decorator] = true
			}
		}
	}

	return descriptors
}

// Count returns the number of registered services in the collection.
func (r *collection) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Use a map to track unique descriptors and avoid duplicates
	seen := make(map[*Descriptor]bool)

	// Count regular services
	for _, service := range r.services {
		seen[service] = true
	}

	// Count grouped services
	for _, groupServices := range r.groups {
		for _, service := range groupServices {
			seen[service] = true
		}
	}

	// Count decorators
	for _, decoratorList := range r.decorators {
		for _, decorator := range decoratorList {
			seen[decorator] = true
		}
	}

	return len(seen)
}

// addService registers a new service
func (r *collection) addService(constructor any, lifetime Lifetime, opts ...AddOption) error {
	// Create descriptor from constructor
	descriptor, err := newDescriptor(constructor, lifetime, opts...)
	if err != nil {
		return err
	}

	// Validate the descriptor
	if err := descriptor.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Parse options to handle special registration cases
	options := &addOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt.applyAddOption(options)
		}
	}

	// Handle As option - register under interface types
	if len(options.As) > 0 {
		// When As is specified, register the service under each interface type
		for _, iface := range options.As {
			interfaceType := reflect.TypeOf(iface).Elem()

			// Validate that the service type implements the interface
			if !descriptor.Type.Implements(interfaceType) {
				return fmt.Errorf("type %v does not implement interface %v", descriptor.Type, interfaceType)
			}

			// Create a new descriptor for the interface type
			interfaceDescriptor := &Descriptor{
				Type:            interfaceType,
				Key:             descriptor.Key,
				Lifetime:        descriptor.Lifetime,
				Constructor:     descriptor.Constructor,
				ConstructorType: descriptor.ConstructorType,
				Dependencies:    descriptor.Dependencies,
				Group:           descriptor.Group,
				As:              options.As,
				IsDecorator:     false,
				isFunc:          descriptor.isFunc,
				isResultObject:  descriptor.isResultObject,
				resultFields:    descriptor.resultFields,
				isParamObject:   descriptor.isParamObject,
				paramFields:     descriptor.paramFields,
			}

			// Register the interface descriptor
			if err := r.registerDescriptor(interfaceDescriptor); err != nil {
				return err
			}
		}

		// If As is specified, we only register under interface types, not the concrete type
		return nil
	}

	// Register the descriptor normally
	return r.registerDescriptor(descriptor)
}

// registerDescriptor registers a descriptor in the appropriate collections
func (r *collection) registerDescriptor(descriptor *Descriptor) error {
	// If it's a decorator, register it as such
	if descriptor.IsDecorator {
		r.decorators[descriptor.Type] = append(r.decorators[descriptor.Type], descriptor)
		return nil
	}

	// Register based on type of service
	if descriptor.Key != nil || descriptor.Group == "" {
		key := TypeKey{Type: descriptor.Type, Key: descriptor.Key}
		if _, exists := r.services[key]; exists {
			if descriptor.Key == nil {
				return fmt.Errorf("type %v already registered", descriptor.Type)
			}

			return fmt.Errorf("type %v with key %v already registered", descriptor.Type, descriptor.Key)
		}

		r.services[key] = descriptor
	} else {
		groupKey := GroupKey{Type: descriptor.Type, Group: descriptor.Group}
		r.groups[groupKey] = append(r.groups[groupKey], descriptor)
	}

	return nil
}
