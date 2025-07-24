package godi

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/junioryono/godi/v2/internal/typecache"
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
//	provider, err := collection.BuildServiceProvider()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer provider.Close()
type ServiceCollection interface {
	// BuildServiceProvider creates a ServiceProvider from the registered services
	// using default options.
	BuildServiceProvider() (ServiceProvider, error)

	// BuildServiceProviderWithOptions creates a ServiceProvider with custom options
	// for validation and behavior configuration.
	BuildServiceProviderWithOptions(options *ServiceProviderOptions) (ServiceProvider, error)

	// ToSlice returns a copy of all registered service descriptors.
	// This is useful for inspection and debugging.
	ToSlice() []*serviceDescriptor

	// AddModules applies one or more module configurations to the service collection.
	// Modules provide a way to group related service registrations.
	AddModules(modules ...ModuleOption) error

	// AddSingleton registers a service with singleton lifetime.
	// Only one instance is created and shared across all resolutions.
	AddSingleton(constructor interface{}, opts ...ProvideOption) error

	// AddScoped registers a service with scoped lifetime.
	// One instance is created per scope and shared within that scope.
	AddScoped(constructor interface{}, opts ...ProvideOption) error

	// Decorate registers a decorator for a type.
	// Decorators can modify or replace existing services.
	Decorate(decorator interface{}, opts ...DecorateOption) error

	// Replace replaces all registrations of the specified service type.
	Replace(lifetime ServiceLifetime, constructor interface{}, opts ...ProvideOption) error

	// RemoveAll removes all registrations of the specified service type.
	RemoveAll(serviceType reflect.Type) error

	// Clear removes all service registrations.
	Clear()

	// Count returns the number of registered services.
	Count() int

	// Contains checks if a service type is registered.
	Contains(serviceType reflect.Type) bool

	// ContainsKeyed checks if a keyed service is registered.
	ContainsKeyed(serviceType reflect.Type, key interface{}) bool
}

// serviceCollection is the default implementation of ServiceCollection.
type serviceCollection struct {
	descriptors []*serviceDescriptor
	// Index for fast lookup by type
	typeIndex map[reflect.Type][]*serviceDescriptor
	// Index for fast lookup by type and key
	keyedTypeIndex map[typeKeyPair][]*serviceDescriptor
	// Track if collection has been built (for safety)
	built bool
	// Track lifetimes for validation
	lifetimeIndex map[reflect.Type]ServiceLifetime
}

// typeKeyPair represents a type-key combination for keyed services.
type typeKeyPair struct {
	serviceType reflect.Type
	serviceKey  interface{}
}

// NewServiceCollection creates a new empty ServiceCollection instance.
//
// Example:
//
//	collection := godi.NewServiceCollection()
//	collection.AddSingleton(NewLogger)
//	provider, err := collection.BuildServiceProvider()
func NewServiceCollection() ServiceCollection {
	return &serviceCollection{
		descriptors:    make([]*serviceDescriptor, 0),
		typeIndex:      make(map[reflect.Type][]*serviceDescriptor),
		keyedTypeIndex: make(map[typeKeyPair][]*serviceDescriptor),
		lifetimeIndex:  make(map[reflect.Type]ServiceLifetime),
		built:          false,
	}
}

// BuildServiceProvider builds a ServiceProvider from the collection using default options.
func (sc *serviceCollection) BuildServiceProvider() (ServiceProvider, error) {
	return sc.BuildServiceProviderWithOptions(nil)
}

// BuildServiceProviderWithOptions builds a ServiceProvider with custom options.
func (sc *serviceCollection) BuildServiceProviderWithOptions(options *ServiceProviderOptions) (ServiceProvider, error) {
	if sc.built {
		return nil, ErrCollectionBuilt
	}

	// Validate all descriptors before building
	for _, descriptor := range sc.descriptors {
		if err := descriptor.validate(); err != nil {
			return nil, ValidationError{ServiceType: descriptor.ServiceType, Message: err.Error()}
		}
	}

	provider, err := newServiceProviderWithOptions(sc, options)
	if err != nil {
		return nil, fmt.Errorf("failed to create service provider: %w", err)
	}

	sc.built = true
	return provider, nil
}

// ToSlice returns a copy of the internal slice of ServiceDescriptors.
// The returned slice is safe to modify without affecting the collection.
func (sc *serviceCollection) ToSlice() []*serviceDescriptor {
	result := make([]*serviceDescriptor, len(sc.descriptors))
	copy(result, sc.descriptors)
	return result
}

// Count returns the number of registered services.
func (sc *serviceCollection) Count() int {
	return len(sc.descriptors)
}

// Contains checks if a service type is registered.
func (sc *serviceCollection) Contains(serviceType reflect.Type) bool {
	_, exists := sc.typeIndex[serviceType]
	return exists
}

// ContainsKeyed checks if a keyed service is registered.
func (sc *serviceCollection) ContainsKeyed(serviceType reflect.Type, key interface{}) bool {
	pair := typeKeyPair{serviceType: serviceType, serviceKey: key}
	_, exists := sc.keyedTypeIndex[pair]
	return exists
}

// AddModules applies one or more module configurations to the service collection.
func (sc *serviceCollection) AddModules(modules ...ModuleOption) error {
	if sc.built {
		return ErrCollectionModifyAfterBuild
	}

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
	return sc.addWithLifetime(constructor, Singleton, opts...)
}

// AddScoped adds a scoped service to the collection.
func (sc *serviceCollection) AddScoped(constructor interface{}, opts ...ProvideOption) error {
	return sc.addWithLifetime(constructor, Scoped, opts...)
}

// addWithLifetime is a helper to reduce duplication.
func (sc *serviceCollection) addWithLifetime(constructor interface{}, lifetime ServiceLifetime, opts ...ProvideOption) error {
	if sc.built {
		return ErrCollectionBuilt
	}

	if constructor == nil {
		return ErrNilConstructor
	}

	// Check if this is a function or a value
	constructorType := reflect.TypeOf(constructor)
	constructorInfo := typecache.GetTypeInfo(constructorType)

	if !constructorInfo.IsFunc {
		// It's not a function, so it's an instance - wrap it in a constructor
		instance := constructor
		instanceType := constructorType

		// Create a properly typed constructor function
		fnType := reflect.FuncOf([]reflect.Type{}, []reflect.Type{instanceType}, false)
		fnValue := reflect.MakeFunc(fnType, func(args []reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.ValueOf(instance)}
		})

		constructor = fnValue.Interface()
	}

	// Check if this returns a result object
	if constructorInfo.IsFunc && constructorInfo.NumOut > 0 {
		outInfo := typecache.GetTypeInfo(constructorInfo.OutTypes[0])
		if outInfo.IsStruct && outInfo.HasOutField {
			descriptor := &serviceDescriptor{
				ServiceType:    constructorInfo.OutTypes[0],
				Lifetime:       lifetime,
				Constructor:    constructor,
				ProvideOptions: opts,
			}
			return sc.addInternal(descriptor)
		}
	}

	// Regular service registration
	descriptor, err := newServiceDescriptor(constructor, lifetime)
	if err != nil {
		return fmt.Errorf("invalid %s constructor: %w", lifetime, err)
	}

	processProvideOptions(descriptor, opts)
	return sc.addInternal(descriptor)
}

// processProvideOptions extracts metadata from dig options.
func processProvideOptions(descriptor *serviceDescriptor, opts []ProvideOption) {
	for _, opt := range opts {
		if opt == nil {
			continue
		}

		// Get string representation of the option
		optStr := fmt.Sprintf("%v", opt)

		// Extract Name
		if descriptor.ServiceKey == nil && strings.HasPrefix(optStr, "Name(") {
			start := strings.Index(optStr, `"`) + 1
			end := strings.LastIndex(optStr, `"`)
			if start > 0 && end > start {
				descriptor.ServiceKey = optStr[start:end]
				descriptor.ProvideOptions = append(descriptor.ProvideOptions, Name(fmt.Sprintf("%v", descriptor.ServiceKey)))
			}
		} else if strings.HasPrefix(optStr, "Group(") {
			start := strings.Index(optStr, `"`) + 1
			end := strings.LastIndex(optStr, `"`)
			if start > 0 && end > start {
				group := optStr[start:end]
				descriptor.Groups = append(descriptor.Groups, group)
				descriptor.ProvideOptions = append(descriptor.ProvideOptions, Group(group))
			}
		} else {
			descriptor.ProvideOptions = append(descriptor.ProvideOptions, opt)
		}
	}
}

// Decorate registers a decorator for a type.
func (sc *serviceCollection) Decorate(decorator interface{}, opts ...DecorateOption) error {
	if sc.built {
		return ErrCollectionBuilt
	}

	if decorator == nil {
		return ErrDecoratorNil
	}

	// Create a decorator descriptor
	fnType := reflect.TypeOf(decorator)
	fnInfo := typecache.GetTypeInfo(fnType)

	if !fnInfo.IsFunc || fnInfo.NumIn == 0 {
		return ErrDecoratorNoParams
	}

	// The first parameter type is what's being decorated
	decoratedType := fnInfo.InTypes[0]

	descriptor := &serviceDescriptor{
		ServiceType: decoratedType,
		Lifetime:    Singleton, // Decorators don't have lifetime
		DecorateInfo: &decorateDescriptor{
			Decorator:       decorator,
			DecorateOptions: opts,
		},
	}

	return sc.addInternal(descriptor)
}

// Replace replaces all registrations of the specified service type.
func (sc *serviceCollection) Replace(lifetime ServiceLifetime, constructor interface{}, opts ...ProvideOption) error {
	if sc.built {
		return ErrCollectionBuilt
	}

	// Determine the service type
	fnType := reflect.TypeOf(constructor)
	fnInfo := typecache.GetTypeInfo(fnType)

	if !fnInfo.IsFunc || fnInfo.NumOut == 0 {
		return ErrConstructorMustReturnValue
	}

	serviceType := fnInfo.OutTypes[0]
	serviceInfo := typecache.GetTypeInfo(serviceType)

	if serviceInfo.IsStruct && serviceInfo.HasOutField {
		// For result objects, we can't easily determine what to replace
		return ErrReplaceResultObject
	}

	// Extract the service key if provided in options
	var serviceKey interface{}
	for _, opt := range opts {
		if opt == nil {
			continue
		}

		optStr := fmt.Sprintf("%v", opt)
		if !strings.HasPrefix(optStr, "Name(") {
			continue
		}

		start := strings.Index(optStr, `"`) + 1
		end := strings.LastIndex(optStr, `"`)
		if start > 0 && end > start {
			serviceKey = optStr[start:end]
			break
		}
	}

	// Remove existing registrations based on whether a key is provided
	if serviceKey != nil {
		// Replace only the keyed service
		sc.removeByTypeAndKeyInternal(serviceType, serviceKey)
	} else {
		// Replace only non-keyed, non-group services
		sc.removeNonKeyedByTypeInternal(serviceType)
	}

	// Add new registration
	descriptor, err := newServiceDescriptor(constructor, lifetime)
	if err != nil {
		return fmt.Errorf("invalid constructor: %w", err)
	}

	processProvideOptions(descriptor, opts)
	return sc.addInternal(descriptor)
}

// RemoveAll removes all registrations of the specified service type.
func (sc *serviceCollection) RemoveAll(serviceType reflect.Type) error {
	if sc.built {
		return ErrCollectionBuilt
	}

	if serviceType == nil {
		return ErrInvalidServiceType
	}

	sc.removeByTypeInternal(serviceType)
	return nil
}

// Clear removes all service registrations.
func (sc *serviceCollection) Clear() {
	if sc.built {
		// Clear should work even after building for testing scenarios
		sc.built = false
	}

	sc.descriptors = make([]*serviceDescriptor, 0)
	sc.typeIndex = make(map[reflect.Type][]*serviceDescriptor)
	sc.keyedTypeIndex = make(map[typeKeyPair][]*serviceDescriptor)
	sc.lifetimeIndex = make(map[reflect.Type]ServiceLifetime)
}

// addInternal appends a ServiceDescriptor to the collection.
func (sc *serviceCollection) addInternal(descriptor *serviceDescriptor) error {
	if descriptor == nil {
		return ErrDescriptorNil
	}

	// Validate is called during build, but we can do early validation here
	if err := descriptor.validate(); err != nil {
		return fmt.Errorf("invalid descriptor: %w", err)
	}

	// For non-keyed, non-group services, check lifetime conflicts
	if !descriptor.isKeyedService() && !descriptor.isDecorator() && len(descriptor.Groups) == 0 {
		// Check if we already have this type registered with a different lifetime
		if existingLifetime, exists := sc.lifetimeIndex[descriptor.ServiceType]; exists {
			if existingLifetime != descriptor.Lifetime {
				return LifetimeConflictError{
					ServiceType: descriptor.ServiceType,
					Current:     existingLifetime,
					Requested:   descriptor.Lifetime,
				}
			}
		}

		// Check if we already have a non-keyed, non-group registration
		if existing := sc.typeIndex[descriptor.ServiceType]; len(existing) > 0 {
			for _, desc := range existing {
				if !desc.isKeyedService() && !desc.isDecorator() && len(desc.Groups) == 0 {
					return AlreadyRegisteredError{ServiceType: descriptor.ServiceType}
				}
			}
		}

		// Track the lifetime for this type
		sc.lifetimeIndex[descriptor.ServiceType] = descriptor.Lifetime
	}

	sc.descriptors = append(sc.descriptors, descriptor)

	// Update indexes
	if descriptor.ServiceKey != nil {
		pair := typeKeyPair{
			serviceType: descriptor.ServiceType,
			serviceKey:  descriptor.ServiceKey,
		}
		sc.keyedTypeIndex[pair] = append(sc.keyedTypeIndex[pair], descriptor)
	}

	// Always add to type index for lookups
	sc.typeIndex[descriptor.ServiceType] = append(sc.typeIndex[descriptor.ServiceType], descriptor)

	return nil
}

// removeByTypeInternal removes all descriptors of a given type.
func (sc *serviceCollection) removeByTypeInternal(serviceType reflect.Type) {
	// Create new slice without the removed descriptors
	newDescriptors := make([]*serviceDescriptor, 0, len(sc.descriptors))
	for _, desc := range sc.descriptors {
		if desc.ServiceType != serviceType {
			newDescriptors = append(newDescriptors, desc)
		}
	}
	sc.descriptors = newDescriptors

	// Update indexes
	delete(sc.typeIndex, serviceType)
	delete(sc.lifetimeIndex, serviceType)

	// Remove from keyed index
	for key := range sc.keyedTypeIndex {
		if key.serviceType == serviceType {
			delete(sc.keyedTypeIndex, key)
		}
	}
}

// removeNonKeyedByTypeInternal removes only non-keyed, non-group descriptors of a given type.
func (sc *serviceCollection) removeNonKeyedByTypeInternal(serviceType reflect.Type) {
	// Create new slice without the removed descriptors
	newDescriptors := make([]*serviceDescriptor, 0, len(sc.descriptors))
	removedIndexes := make(map[int]bool)

	for i, desc := range sc.descriptors {
		// Keep the descriptor if:
		// 1. It's a different type, OR
		// 2. It's the same type but is keyed, OR
		// 3. It's the same type but belongs to a group
		if desc.ServiceType != serviceType || desc.isKeyedService() || len(desc.Groups) > 0 {
			newDescriptors = append(newDescriptors, desc)
		} else {
			removedIndexes[i] = true
		}
	}
	sc.descriptors = newDescriptors

	// Update type index - remove only non-keyed, non-group entries
	if descs, exists := sc.typeIndex[serviceType]; exists {
		newTypeDescs := make([]*serviceDescriptor, 0)
		for _, desc := range descs {
			if desc.isKeyedService() || len(desc.Groups) > 0 {
				newTypeDescs = append(newTypeDescs, desc)
			}
		}
		if len(newTypeDescs) > 0 {
			sc.typeIndex[serviceType] = newTypeDescs
		} else {
			delete(sc.typeIndex, serviceType)
		}
	}

	// Update lifetime index only if no non-keyed services remain
	hasNonKeyed := false
	for _, desc := range sc.descriptors {
		if desc.ServiceType == serviceType && !desc.isKeyedService() && len(desc.Groups) == 0 {
			hasNonKeyed = true
			break
		}
	}
	if !hasNonKeyed {
		delete(sc.lifetimeIndex, serviceType)
	}
}

// removeByTypeAndKeyInternal removes only descriptors with specific type and key.
func (sc *serviceCollection) removeByTypeAndKeyInternal(serviceType reflect.Type, serviceKey interface{}) {
	// Create new slice without the removed descriptors
	newDescriptors := make([]*serviceDescriptor, 0, len(sc.descriptors))

	for _, desc := range sc.descriptors {
		// Keep the descriptor if it's not the one we're looking for
		if desc.ServiceType != serviceType || desc.ServiceKey != serviceKey {
			newDescriptors = append(newDescriptors, desc)
		}
	}
	sc.descriptors = newDescriptors

	// Remove from keyed index
	key := typeKeyPair{serviceType: serviceType, serviceKey: serviceKey}
	delete(sc.keyedTypeIndex, key)

	// Update type index
	if descs, exists := sc.typeIndex[serviceType]; exists {
		newTypeDescs := make([]*serviceDescriptor, 0)
		for _, desc := range descs {
			if desc.ServiceKey != serviceKey {
				newTypeDescs = append(newTypeDescs, desc)
			}
		}
		if len(newTypeDescs) > 0 {
			sc.typeIndex[serviceType] = newTypeDescs
		} else {
			delete(sc.typeIndex, serviceType)
		}
	}
}
