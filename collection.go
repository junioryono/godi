package godi

import (
	"fmt"
	"reflect"
	"sync"
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

	// Contains checks if a service type is registered.
	Contains(serviceType reflect.Type) bool

	// ContainsKeyed checks if a keyed service is registered.
	ContainsKeyed(serviceType reflect.Type, key any) bool

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
	services map[reflect.Type][]*Descriptor

	// keyedServices stores services with keys (named services)
	keyedServices map[TypeKey][]*Descriptor

	// groups stores services that belong to groups
	groups map[GroupKey][]*Descriptor

	// decorators stores decorator descriptors by type
	// These are just Descriptors with IsDecorator=true
	decorators map[reflect.Type][]*Descriptor

	// lifetimes tracks the lifetime of each type for validation
	lifetimes map[reflect.Type]Lifetime
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

// typeKeyPair represents a type-key combination for keyed services.
type typeKeyPair struct {
	serviceType reflect.Type
	serviceKey  any
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
		services:      make(map[reflect.Type][]*Descriptor),
		keyedServices: make(map[TypeKey][]*Descriptor),
		groups:        make(map[GroupKey][]*Descriptor),
		decorators:    make(map[reflect.Type][]*Descriptor),
		lifetimes:     make(map[reflect.Type]Lifetime),
	}
}

// Build creates a Provider from the registered services using default options.
func (sc *collection) Build() (Provider, error) {
	return sc.BuildWithOptions(nil)
}

// BuildWithOptions creates a Provider with custom options for validation and behavior configuration.
func (sc *collection) BuildWithOptions(options *ProviderOptions) (Provider, error) {
	// For now, return a placeholder provider
	// This will be fully implemented when the provider implementation is complete
	return &provider{
		services:      sc.services,
		keyedServices: sc.keyedServices,
		groups:        sc.groups,
		decorators:    sc.decorators,
	}, nil
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

// Contains checks if a service type is registered in the collection.
func (r *collection) Contains(serviceType reflect.Type) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.services[serviceType]
	return exists && len(r.services[serviceType]) > 0
}

// ContainsKeyed checks if a keyed service is registered in the collection.
func (r *collection) ContainsKeyed(serviceType reflect.Type, key any) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	typeKey := TypeKey{Type: serviceType, Key: key}
	_, exists := r.keyedServices[typeKey]
	return exists && len(r.keyedServices[typeKey]) > 0
}

// HasService checks if a service exists for the type
func (r *collection) HasService(t reflect.Type) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	services, ok := r.services[t]
	return ok && len(services) > 0
}

// HasKeyedService checks if a keyed service exists
func (r *collection) HasKeyedService(t reflect.Type, key any) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	typeKey := TypeKey{Type: t, Key: key}
	services, ok := r.keyedServices[typeKey]
	return ok && len(services) > 0
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

	delete(r.services, t)
	delete(r.lifetimes, t)

	// Remove from keyed services
	for key := range r.keyedServices {
		if key.Type == t {
			delete(r.keyedServices, key)
		}
	}

	// Remove from groups
	for key := range r.groups {
		if key.Type == t {
			delete(r.groups, key)
		}
	}

	// Remove decorators for this type
	delete(r.decorators, t)
}

// RemoveKeyed removes a specific keyed service
func (r *collection) RemoveKeyed(t reflect.Type, key any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	typeKey := TypeKey{Type: t, Key: key}
	delete(r.keyedServices, typeKey)
}

// ToSlice returns a copy of all registered service descriptors
func (r *collection) ToSlice() []*Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	// Use a map to track unique descriptors and avoid duplicates
	seen := make(map[*Descriptor]bool)
	descriptors := make([]*Descriptor, 0)
	
	// Add regular services
	for _, services := range r.services {
		for _, service := range services {
			if !seen[service] {
				descriptors = append(descriptors, service)
				seen[service] = true
			}
		}
	}
	
	// Add keyed services
	for _, keyedServices := range r.keyedServices {
		for _, service := range keyedServices {
			if !seen[service] {
				descriptors = append(descriptors, service)
				seen[service] = true
			}
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
	for _, services := range r.services {
		for _, service := range services {
			seen[service] = true
		}
	}
	
	// Count keyed services
	for _, keyedServices := range r.keyedServices {
		for _, service := range keyedServices {
			seen[service] = true
		}
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
	descriptor, err := NewDescriptor(constructor, lifetime, opts...)
	if err != nil {
		return err
	}

	// Validate the descriptor
	if err := ValidateDescriptor(descriptor); err != nil {
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
				Groups:          descriptor.Groups,
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

	// Validate lifetime consistency for non-keyed, non-grouped services
	// Note: grouped and keyed services can have different lifetimes from the base service
	if descriptor.Key == nil && len(descriptor.Groups) == 0 {
		if existing, ok := r.lifetimes[descriptor.Type]; ok {
			if existing != descriptor.Lifetime {
				return fmt.Errorf("type %v already registered with lifetime %v, cannot register with %v",
					descriptor.Type, existing, descriptor.Lifetime)
			}
		}
		// Store lifetime for regular services only
		r.lifetimes[descriptor.Type] = descriptor.Lifetime
	}

	// Register based on type of service
	if descriptor.Key != nil {
		// Keyed service - only in keyedServices
		key := TypeKey{Type: descriptor.Type, Key: descriptor.Key}
		r.keyedServices[key] = append(r.keyedServices[key], descriptor)
	} else if len(descriptor.Groups) > 0 {
		// Grouped service - only in groups (not in regular services)
		// Register in groups only, handled below
	} else {
		// Regular service - only in services
		r.services[descriptor.Type] = append(r.services[descriptor.Type], descriptor)
	}

	// Register in groups
	for _, group := range descriptor.Groups {
		groupKey := GroupKey{Type: descriptor.Type, Group: group}
		r.groups[groupKey] = append(r.groups[groupKey], descriptor)
	}

	return nil
}
