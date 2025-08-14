package godi

import (
	"reflect"
	"sync"
)

// ServiceCollection represents a collection of service descriptors that define
// the services available in the dependency injection container.
//
// ServiceCollection follows a builder pattern where services are registered
// with their lifetimes and dependencies, then built into a ServiceProvider.
//
// ServiceCollection is NOT thread-safe. It should be configured in a single
// goroutine before building the ServiceProvider.
//
// Example:
//
//	collection := godi.NewServiceCollection()
//	collection.AddSingleton(NewLogger)
//	collection.AddScoped(NewDatabase)
//
//	provider, err := collection.Build()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer provider.Close()
type ServiceCollection interface {
	// Build creates a ServiceProvider from the registered services
	// using default options.
	Build() (ServiceProvider, error)

	// BuildWithOptions creates a ServiceProvider with custom options
	// for validation and behavior configuration.
	BuildWithOptions(options *ServiceProviderOptions) (ServiceProvider, error)

	// AddModules applies one or more module configurations to the service collection.
	// Modules provide a way to group related service registrations.
	AddModules(modules ...ModuleOption) error

	// AddSingleton registers a service with singleton lifetime.
	// Only one instance is created and shared across all resolutions.
	AddSingleton(constructor any, opts ...ProvideOption) error

	// AddScoped registers a service with scoped lifetime.
	// One instance is created per scope and shared within that scope.
	AddScoped(constructor any, opts ...ProvideOption) error

	// AddTransient registers a service with transient lifetime.
	// A new instance is created every time the service is resolved.
	AddTransient(constructor any, opts ...ProvideOption) error

	// Decorate registers a decorator for a service type.
	// Decorators wrap existing services to modify their behavior.
	Decorate(decorator any, opts ...ProvideOption) error

	// Contains checks if a service type is registered.
	Contains(serviceType reflect.Type) bool

	// ContainsKeyed checks if a keyed service is registered.
	ContainsKeyed(serviceType reflect.Type, key any) bool

	// Remove removes all providers for a given service type.
	Remove(serviceType reflect.Type)

	// RemoveKeyed removes a specific keyed provider.
	RemoveKeyed(serviceType reflect.Type, key any)

	// ToSlice returns a copy of all registered service descriptors.
	// This is useful for inspection and debugging.
	ToSlice() []*Descriptor

	// Count returns the number of registered services.
	Count() int
}

// ServiceCollection is the core service registry that manages providers and decorators.
type serviceCollection struct {
	mu sync.RWMutex

	// providers stores all non-keyed providers by type
	providers map[reflect.Type][]*Descriptor

	// keyedProviders stores providers with keys (named services)
	keyedProviders map[TypeKey][]*Descriptor

	// groups stores providers that belong to groups
	groups map[GroupKey][]*Descriptor

	// decorators stores decorator descriptors by type
	// These are just Descriptors with IsDecorator=true
	decorators map[reflect.Type][]*Descriptor

	// lifetimes tracks the lifetime of each type for validation
	lifetimes map[reflect.Type]ServiceLifetime
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

// NewServiceCollection creates a new empty ServiceCollection instance.
//
// Example:
//
//	collection := godi.NewServiceCollection()
//	collection.AddSingleton(NewLogger)
//	provider, err := collection.Build()
func NewServiceCollection() ServiceCollection {
	return &serviceCollection{
		providers:      make(map[reflect.Type][]*Descriptor),
		keyedProviders: make(map[TypeKey][]*Descriptor),
		groups:         make(map[GroupKey][]*Descriptor),
		decorators:     make(map[reflect.Type][]*Descriptor),
		lifetimes:      make(map[reflect.Type]ServiceLifetime),
	}
}

// Build creates a ServiceProvider from the registered services using default options.
func (sc *serviceCollection) Build() (ServiceProvider, error) {
	return nil, nil
}

// BuildWithOptions creates a ServiceProvider with custom options for validation and behavior configuration.
func (sc *serviceCollection) BuildWithOptions(options *ServiceProviderOptions) (ServiceProvider, error) {
	return nil, nil
}

// AddModules applies one or more module configurations to the service collection.
func (sc *serviceCollection) AddModules(modules ...ModuleOption) error {
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
func (sc *serviceCollection) AddSingleton(constructor interface{}, opts ...ProvideOption) error {
	return sc.addProvider(constructor, Singleton, opts...)
}

// AddScoped adds a scoped service to the collection.
func (sc *serviceCollection) AddScoped(constructor interface{}, opts ...ProvideOption) error {
	return sc.addProvider(constructor, Scoped, opts...)
}

// AddTransient adds a transient service to the collection.
func (sc *serviceCollection) AddTransient(constructor interface{}, opts ...ProvideOption) error {
	return sc.addProvider(constructor, Transient, opts...)
}

// Decorate registers a decorator for a service type.
func (sc *serviceCollection) Decorate(decorator interface{}, opts ...ProvideOption) error {
	return nil
}

// Contains checks if a service type is registered in the collection.
func (r *serviceCollection) Contains(serviceType reflect.Type) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, exists := r.providers[serviceType]
	return exists && len(r.providers[serviceType]) > 0
}

// ContainsKeyed checks if a keyed service is registered in the collection.
func (r *serviceCollection) ContainsKeyed(serviceType reflect.Type, key any) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	typeKey := TypeKey{Type: serviceType, Key: key}
	_, exists := r.keyedProviders[typeKey]
	return exists && len(r.keyedProviders[typeKey]) > 0
}

// HasProvider checks if a provider exists for the type
func (r *serviceCollection) HasProvider(t reflect.Type) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	providers, ok := r.providers[t]
	return ok && len(providers) > 0
}

// HasKeyedProvider checks if a keyed provider exists
func (r *serviceCollection) HasKeyedProvider(t reflect.Type, key any) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	typeKey := TypeKey{Type: t, Key: key}
	providers, ok := r.keyedProviders[typeKey]
	return ok && len(providers) > 0
}

// HasGroup checks if a group has any providers
func (r *serviceCollection) HasGroup(t reflect.Type, group string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	groupKey := GroupKey{Type: t, Group: group}
	providers, ok := r.groups[groupKey]
	return ok && len(providers) > 0
}

// Remove removes all providers for a given type
func (r *serviceCollection) Remove(t reflect.Type) {
	r.mu.Lock()
	defer r.mu.Unlock()

	delete(r.providers, t)
	delete(r.lifetimes, t)

	// Remove from keyed providers
	for key := range r.keyedProviders {
		if key.Type == t {
			delete(r.keyedProviders, key)
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

// RemoveKeyed removes a specific keyed provider
func (r *serviceCollection) RemoveKeyed(t reflect.Type, key any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	typeKey := TypeKey{Type: t, Key: key}
	delete(r.keyedProviders, typeKey)
}

// ToSlice returns a copy of all registered service descriptors
func (r *serviceCollection) ToSlice() []*Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	descriptors := make([]*Descriptor, 0)
	for _, providers := range r.providers {
		for _, provider := range providers {
			descriptors = append(descriptors, provider)
		}
	}
	for _, keyedProviders := range r.keyedProviders {
		for _, provider := range keyedProviders {
			descriptors = append(descriptors, provider)
		}
	}
	for _, groupProviders := range r.groups {
		for _, provider := range groupProviders {
			descriptors = append(descriptors, provider)
		}
	}
	for _, decorators := range r.decorators {
		for _, decorator := range decorators {
			descriptors = append(descriptors, decorator)
		}
	}
	return descriptors
}

// Count returns the number of registered services in the collection.
func (r *serviceCollection) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	count := 0
	for _, providers := range r.providers {
		count += len(providers)
	}
	for _, keyedProviders := range r.keyedProviders {
		count += len(keyedProviders)
	}
	for _, groupProviders := range r.groups {
		count += len(groupProviders)
	}
	for _, decorators := range r.decorators {
		count += len(decorators)
	}
	return count
}

// addProvider registers a new provider
func (r *serviceCollection) addProvider(constructor interface{}, lifetime ServiceLifetime, opts ...ProvideOption) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// if provider == nil {
	// 	return fmt.Errorf("provider cannot be nil")
	// }

	// if provider.Type == nil {
	// 	return fmt.Errorf("provider type cannot be nil")
	// }

	// // If it's a decorator, register it as such
	// if provider.IsDecorator {
	// 	r.decorators[provider.Type] = append(r.decorators[provider.Type], provider)
	// 	return nil
	// }

	// // Validate lifetime consistency for non-keyed, non-group providers
	// if provider.Key == nil && len(provider.Groups) == 0 {
	// 	if existing, ok := r.lifetimes[provider.Type]; ok {
	// 		if existing != provider.Lifetime {
	// 			return fmt.Errorf("type %v already registered with lifetime %v, cannot register with %v",
	// 				provider.Type, existing, provider.Lifetime)
	// 		}
	// 	}
	// 	r.lifetimes[provider.Type] = provider.Lifetime
	// }

	// // Register based on type of provider
	// if provider.Key != nil {
	// 	// Keyed provider
	// 	key := TypeKey{Type: provider.Type, Key: provider.Key}
	// 	r.keyedProviders[key] = append(r.keyedProviders[key], provider)
	// } else {
	// 	// Regular provider
	// 	r.providers[provider.Type] = append(r.providers[provider.Type], provider)
	// }

	// // Register in groups
	// for _, group := range provider.Groups {
	// 	groupKey := GroupKey{Type: provider.Type, Group: group}
	// 	r.groups[groupKey] = append(r.groups[groupKey], provider)
	// }

	return nil
}
