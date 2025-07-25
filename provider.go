package godi

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junioryono/godi/v2/internal/typecache"
	"go.uber.org/dig"
)

// ServiceProvider is the main dependency injection container interface.
// It provides methods to resolve services, create scopes, and dynamically register services.
//
// ServiceProvider is thread-safe and can be used concurrently.
// Services are resolved lazily on first request.
//
// Example:
//
//	provider := godi.NewServiceProvider()
//	defer provider.Close()
//
//	// Register services dynamically
//	provider.AddSingleton(NewLogger)
//	provider.AddScoped(NewDatabase)
//
//	// Resolve a service
//	logger, err := godi.Resolve[Logger](provider)
//	if err != nil {
//	    log.Fatal(err)
//	}
type ServiceProvider interface {
	// AddModules applies one or more module configurations to the service collection.
	AddModules(modules ...ModuleOption) error

	// AddSingleton registers a service with singleton lifetime.
	// Only one instance is created and shared across all resolutions.
	AddSingleton(constructor interface{}, opts ...ProvideOption) error

	// AddScoped registers a service with scoped lifetime.
	// One instance is created per scope and shared within that scope.
	AddScoped(constructor interface{}, opts ...ProvideOption) error

	// AddService registers a service with the specified lifetime.
	AddService(lifetime ServiceLifetime, constructor interface{}, opts ...ProvideOption) error

	// Replace replaces all registrations of the specified service type.
	Replace(lifetime ServiceLifetime, constructor interface{}, opts ...ProvideOption) error

	// RemoveAll removes all registrations of the specified service type.
	RemoveAll(serviceType reflect.Type) error

	// Service Resolution Methods

	// GetRootScope returns the root scope of the service provider.
	// The root scope is used for singleton services and tracks disposal of singletons.
	// It is also the default scope for resolving services.
	// This scope is created automatically when the provider is built.
	GetRootScope() Scope

	// CreateScope creates a new service scope with the given context.
	// The returned Scope is also a ServiceProvider for that scope.
	CreateScope(ctx context.Context) Scope

	// Resolve gets the service object of the specified type.
	Resolve(serviceType reflect.Type) (interface{}, error)

	// ResolveKeyed gets the service object of the specified type with the specified key.
	ResolveKeyed(serviceType reflect.Type, serviceKey interface{}) (interface{}, error)

	// ResolveGroup gets all services of the specified type registered in a group.
	// This is useful for plugin systems or when you need multiple implementations.
	ResolveGroup(serviceType reflect.Type, groupName string) ([]interface{}, error)

	// IsService determines whether the specified service type is available.
	// This is useful for optional dependencies.
	IsService(serviceType reflect.Type) bool

	// IsKeyedService determines whether the specified keyed service type is available.
	IsKeyedService(serviceType reflect.Type, serviceKey interface{}) bool

	// Decorate provides a decorator for a type that has already been provided in the Scope.
	Decorate(decorator interface{}, opts ...DecorateOption) error

	// Invoke executes a function with dependency injection.
	// All parameters of the function are resolved from the container.
	// The function can optionally return an error.
	Invoke(function interface{}) error

	// IsDisposed returns true if the provider has been disposed.
	IsDisposed() bool

	Disposable
}

// Disposable allows disposal with context for graceful shutdown.
// Services implementing this interface can perform context-aware cleanup.
//
// Example:
//
//	type DatabaseConnection struct {
//	    conn *sql.DB
//	}
//
//	func (dc *DatabaseConnection) Close() error {
//	    return dc.conn.Close()
//	}
type Disposable interface {
	// Close disposes the resource with the provided context.
	// Implementations should respect context cancellation for graceful shutdown.
	Close() error
}

// ServiceProviderOptions configures various ServiceProvider behaviors.
type ServiceProviderOptions struct {
	// OnServiceResolved is called after a service is successfully resolved.
	// This can be used for logging, metrics, or debugging.
	OnServiceResolved func(serviceType reflect.Type, instance interface{}, duration time.Duration)

	// OnServiceError is called when a service resolution fails.
	// This can be used for error tracking and debugging.
	OnServiceError func(serviceType reflect.Type, err error)

	// ResolutionTimeout sets a timeout for individual service resolutions.
	// 0 means no timeout (default).
	ResolutionTimeout time.Duration

	// DryRun when true, disables actual invocation of constructors (for testing).
	DryRun bool

	// RecoverFromPanics when true, recovers from panics in constructors.
	RecoverFromPanics bool

	// DeferAcyclicVerification defers cycle detection until first invoke.
	DeferAcyclicVerification bool
}

// serviceProvider is the default implementation of ServiceProvider.
type serviceProvider struct {
	// Main dig container for singleton services
	digContainer *dig.Container

	// Service descriptors
	descriptors     []*serviceDescriptor
	descriptorIndex map[reflect.Type]struct{}
	keyedIndex      map[typeKeyPair]struct{}

	// Indexes for fast lookup (from collection)
	typeIndex      map[reflect.Type][]*serviceDescriptor
	keyedTypeIndex map[typeKeyPair][]*serviceDescriptor
	lifetimeIndex  map[reflect.Type]ServiceLifetime

	// State
	disposed int32
	mu       sync.RWMutex

	// Options
	options *ServiceProviderOptions

	// Root scope for disposal tracking
	rootScope *serviceProviderScope
	scopes    map[string]*serviceProviderScope
	scopesMu  sync.Mutex

	// Callbacks for dig integration
	providerCallbacks map[uintptr]Callback
	beforeCallbacks   map[uintptr]BeforeCallback
}

// NewServiceProvider creates a new empty ServiceProvider that supports dynamic registration.
//
// Example:
//
//	provider := godi.NewServiceProvider()
//	provider.AddSingleton(NewLogger)
//	provider.AddScoped(NewDatabase)
//	defer provider.Close()
func NewServiceProvider() ServiceProvider {
	return NewServiceProviderWithOptions(nil)
}

// NewServiceProviderWithOptions creates a new ServiceProvider with custom options.
func NewServiceProviderWithOptions(options *ServiceProviderOptions) ServiceProvider {
	if options == nil {
		options = &ServiceProviderOptions{}
	}

	// Create dig options based on our options
	digOpts := []dig.Option{}
	if options.DryRun {
		digOpts = append(digOpts, dig.DryRun(true))
	}
	if options.RecoverFromPanics {
		digOpts = append(digOpts, dig.RecoverFromPanics())
	}
	if options.DeferAcyclicVerification {
		digOpts = append(digOpts, dig.DeferAcyclicVerification())
	}

	provider := &serviceProvider{
		digContainer:      dig.New(digOpts...),
		descriptors:       make([]*serviceDescriptor, 0),
		descriptorIndex:   make(map[reflect.Type]struct{}),
		keyedIndex:        make(map[typeKeyPair]struct{}),
		typeIndex:         make(map[reflect.Type][]*serviceDescriptor),
		keyedTypeIndex:    make(map[typeKeyPair][]*serviceDescriptor),
		lifetimeIndex:     make(map[reflect.Type]ServiceLifetime),
		options:           options,
		scopes:            make(map[string]*serviceProviderScope),
		providerCallbacks: make(map[uintptr]Callback),
		beforeCallbacks:   make(map[uintptr]BeforeCallback),
	}

	// Create root scope
	provider.rootScope = newScope(provider, context.Background())

	// Register built-in services
	provider.registerBuiltInServices()

	return provider
}

// registerBuiltInServices registers context, ServiceProvider, and Scope as built-in services.
func (sp *serviceProvider) registerBuiltInServices() {
	// Register context.Context
	sp.descriptors = append(sp.descriptors, &serviceDescriptor{
		ServiceType: reflect.TypeOf((*context.Context)(nil)).Elem(),
		Lifetime:    Scoped,
		Constructor: func() context.Context {
			return sp.rootScope.ctx
		},
	})

	// Register ServiceProvider
	sp.descriptors = append(sp.descriptors, &serviceDescriptor{
		ServiceType: reflect.TypeOf((*ServiceProvider)(nil)).Elem(),
		Lifetime:    Singleton,
		Constructor: func() ServiceProvider {
			return sp.rootScope
		},
	})

	// Register Scope
	sp.descriptors = append(sp.descriptors, &serviceDescriptor{
		ServiceType: reflect.TypeOf((*Scope)(nil)).Elem(),
		Lifetime:    Scoped,
		Constructor: func() Scope {
			return sp.rootScope
		},
	})

	// Register these in dig container
	sp.digContainer.Provide(func() context.Context { return sp.rootScope.ctx })
	sp.digContainer.Provide(func() ServiceProvider { return sp.rootScope })
	sp.digContainer.Provide(func() Scope { return sp.rootScope })

	// Index built-in services
	for _, desc := range sp.descriptors {
		sp.indexDescriptor(desc)
	}
}

func (sp *serviceProvider) AddModules(modules ...ModuleOption) error {
	if sp.IsDisposed() {
		return ErrProviderDisposed
	}

	for _, module := range modules {
		if module == nil {
			continue
		}

		if err := module(sp); err != nil {
			return err
		}
	}

	return nil
}

// AddSingleton adds a singleton service to the provider.
func (sp *serviceProvider) AddSingleton(constructor interface{}, opts ...ProvideOption) error {
	return sp.AddService(Singleton, constructor, opts...)
}

// AddScoped adds a scoped service to the provider.
func (sp *serviceProvider) AddScoped(constructor interface{}, opts ...ProvideOption) error {
	return sp.AddService(Scoped, constructor, opts...)
}

// AddService adds a service with the specified lifetime.
func (sp *serviceProvider) AddService(lifetime ServiceLifetime, constructor interface{}, opts ...ProvideOption) error {
	if sp.IsDisposed() {
		return ErrProviderDisposed
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

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
			return sp.addInternal(descriptor)
		}
	}

	// Regular service registration
	descriptor, err := newServiceDescriptor(constructor, lifetime)
	if err != nil {
		return fmt.Errorf("invalid %s constructor: %w", lifetime, err)
	}

	processProvideOptions(descriptor, opts)
	return sp.addInternal(descriptor)
}

// addInternal appends a ServiceDescriptor to the provider.
func (sp *serviceProvider) addInternal(descriptor *serviceDescriptor) error {
	if descriptor == nil {
		return ErrDescriptorNil
	}

	// Validate the descriptor
	if err := descriptor.validate(); err != nil {
		return fmt.Errorf("invalid descriptor: %w", err)
	}

	// For non-keyed, non-group services, check lifetime conflicts
	if !descriptor.isKeyedService() && !descriptor.isDecorator() && len(descriptor.Groups) == 0 {
		// Check if we already have this type registered with a different lifetime
		if existingLifetime, exists := sp.lifetimeIndex[descriptor.ServiceType]; exists {
			if existingLifetime != descriptor.Lifetime {
				return LifetimeConflictError{
					ServiceType: descriptor.ServiceType,
					Current:     existingLifetime,
					Requested:   descriptor.Lifetime,
				}
			}
		}

		// Check if we already have a non-keyed, non-group registration
		if existing := sp.typeIndex[descriptor.ServiceType]; len(existing) > 0 {
			for _, desc := range existing {
				if !desc.isKeyedService() && !desc.isDecorator() && len(desc.Groups) == 0 {
					return AlreadyRegisteredError{ServiceType: descriptor.ServiceType}
				}
			}
		}

		// Track the lifetime for this type
		sp.lifetimeIndex[descriptor.ServiceType] = descriptor.Lifetime
	}

	// Add to descriptors
	sp.descriptors = append(sp.descriptors, descriptor)

	// Index the descriptor
	sp.indexDescriptor(descriptor)

	// Register with dig if singleton
	if descriptor.Lifetime == Singleton {
		if err := sp.registerSingletonService(descriptor); err != nil {
			// Remove from descriptors and indexes on failure
			sp.descriptors = sp.descriptors[:len(sp.descriptors)-1]
			sp.removeFromIndexes(descriptor)
			return err
		}
	}

	// Update all existing scopes with the new scoped service
	if descriptor.Lifetime == Scoped {
		sp.scopesMu.Lock()
		for _, scope := range sp.scopes {
			scope.registerScopedService(descriptor)
		}
		sp.scopesMu.Unlock()
	}

	return nil
}

// indexDescriptor adds a descriptor to all relevant indexes.
func (sp *serviceProvider) indexDescriptor(descriptor *serviceDescriptor) {
	// Update type index
	sp.typeIndex[descriptor.ServiceType] = append(sp.typeIndex[descriptor.ServiceType], descriptor)

	// Update keyed index if applicable
	if descriptor.ServiceKey != nil {
		pair := typeKeyPair{
			serviceType: descriptor.ServiceType,
			serviceKey:  descriptor.ServiceKey,
		}
		sp.keyedTypeIndex[pair] = append(sp.keyedTypeIndex[pair], descriptor)
		sp.keyedIndex[pair] = struct{}{}
	} else {
		sp.descriptorIndex[descriptor.ServiceType] = struct{}{}
	}
}

// removeFromIndexes removes a descriptor from all indexes.
func (sp *serviceProvider) removeFromIndexes(descriptor *serviceDescriptor) {
	// Remove from type index
	if descs, exists := sp.typeIndex[descriptor.ServiceType]; exists {
		newDescs := make([]*serviceDescriptor, 0, len(descs)-1)
		for _, d := range descs {
			if d != descriptor {
				newDescs = append(newDescs, d)
			}
		}
		if len(newDescs) > 0 {
			sp.typeIndex[descriptor.ServiceType] = newDescs
		} else {
			delete(sp.typeIndex, descriptor.ServiceType)
		}
	}

	// Remove from keyed index if applicable
	if descriptor.ServiceKey != nil {
		pair := typeKeyPair{
			serviceType: descriptor.ServiceType,
			serviceKey:  descriptor.ServiceKey,
		}
		delete(sp.keyedTypeIndex, pair)
		delete(sp.keyedIndex, pair)
	} else {
		delete(sp.descriptorIndex, descriptor.ServiceType)
	}

	// Remove from lifetime index if it was the only non-keyed service
	if !descriptor.isKeyedService() && len(descriptor.Groups) == 0 {
		hasOtherNonKeyed := false
		for _, desc := range sp.descriptors {
			if desc.ServiceType == descriptor.ServiceType && desc != descriptor &&
				!desc.isKeyedService() && len(desc.Groups) == 0 {
				hasOtherNonKeyed = true
				break
			}
		}
		if !hasOtherNonKeyed {
			delete(sp.lifetimeIndex, descriptor.ServiceType)
		}
	}
}

// Replace replaces all registrations of the specified service type.
func (sp *serviceProvider) Replace(lifetime ServiceLifetime, constructor interface{}, opts ...ProvideOption) error {
	if sp.IsDisposed() {
		return ErrProviderDisposed
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

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
		sp.removeByTypeAndKeyInternal(serviceType, serviceKey)
	} else {
		// Replace only non-keyed, non-group services
		sp.removeNonKeyedByTypeInternal(serviceType)
	}

	// Add new registration
	descriptor, err := newServiceDescriptor(constructor, lifetime)
	if err != nil {
		return fmt.Errorf("invalid constructor: %w", err)
	}

	processProvideOptions(descriptor, opts)
	return sp.addInternal(descriptor)
}

// RemoveAll removes all registrations of the specified service type.
func (sp *serviceProvider) RemoveAll(serviceType reflect.Type) error {
	if sp.IsDisposed() {
		return ErrProviderDisposed
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

	if serviceType == nil {
		return ErrInvalidServiceType
	}

	sp.removeByTypeInternal(serviceType)
	return nil
}

// removeByTypeInternal removes all descriptors of a given type.
func (sp *serviceProvider) removeByTypeInternal(serviceType reflect.Type) {
	// Create new slice without the removed descriptors
	newDescriptors := make([]*serviceDescriptor, 0, len(sp.descriptors))
	for _, desc := range sp.descriptors {
		if desc.ServiceType != serviceType {
			newDescriptors = append(newDescriptors, desc)
		}
	}
	sp.descriptors = newDescriptors

	// Update indexes
	delete(sp.typeIndex, serviceType)
	delete(sp.lifetimeIndex, serviceType)
	delete(sp.descriptorIndex, serviceType)

	// Remove from keyed index
	for key := range sp.keyedTypeIndex {
		if key.serviceType == serviceType {
			delete(sp.keyedTypeIndex, key)
			delete(sp.keyedIndex, key)
		}
	}
}

// removeNonKeyedByTypeInternal removes only non-keyed, non-group descriptors of a given type.
func (sp *serviceProvider) removeNonKeyedByTypeInternal(serviceType reflect.Type) {
	// Create new slice without the removed descriptors
	newDescriptors := make([]*serviceDescriptor, 0, len(sp.descriptors))

	for _, desc := range sp.descriptors {
		// Keep the descriptor if:
		// 1. It's a different type, OR
		// 2. It's the same type but is keyed, OR
		// 3. It's the same type but belongs to a group
		if desc.ServiceType != serviceType || desc.isKeyedService() || len(desc.Groups) > 0 {
			newDescriptors = append(newDescriptors, desc)
		}
	}
	sp.descriptors = newDescriptors

	// Update type index - remove only non-keyed, non-group entries
	if descs, exists := sp.typeIndex[serviceType]; exists {
		newTypeDescs := make([]*serviceDescriptor, 0)
		for _, desc := range descs {
			if desc.isKeyedService() || len(desc.Groups) > 0 {
				newTypeDescs = append(newTypeDescs, desc)
			}
		}
		if len(newTypeDescs) > 0 {
			sp.typeIndex[serviceType] = newTypeDescs
		} else {
			delete(sp.typeIndex, serviceType)
			delete(sp.descriptorIndex, serviceType)
		}
	}

	// Update lifetime index only if no non-keyed services remain
	hasNonKeyed := false
	for _, desc := range sp.descriptors {
		if desc.ServiceType == serviceType && !desc.isKeyedService() && len(desc.Groups) == 0 {
			hasNonKeyed = true
			break
		}
	}
	if !hasNonKeyed {
		delete(sp.lifetimeIndex, serviceType)
	}
}

// removeByTypeAndKeyInternal removes only descriptors with specific type and key.
func (sp *serviceProvider) removeByTypeAndKeyInternal(serviceType reflect.Type, serviceKey interface{}) {
	// Create new slice without the removed descriptors
	newDescriptors := make([]*serviceDescriptor, 0, len(sp.descriptors))

	for _, desc := range sp.descriptors {
		// Keep the descriptor if it's not the one we're looking for
		if desc.ServiceType != serviceType || desc.ServiceKey != serviceKey {
			newDescriptors = append(newDescriptors, desc)
		}
	}
	sp.descriptors = newDescriptors

	// Remove from keyed index
	key := typeKeyPair{serviceType: serviceType, serviceKey: serviceKey}
	delete(sp.keyedTypeIndex, key)
	delete(sp.keyedIndex, key)

	// Update type index
	if descs, exists := sp.typeIndex[serviceType]; exists {
		newTypeDescs := make([]*serviceDescriptor, 0)
		for _, desc := range descs {
			if desc.ServiceKey != serviceKey {
				newTypeDescs = append(newTypeDescs, desc)
			}
		}
		if len(newTypeDescs) > 0 {
			sp.typeIndex[serviceType] = newTypeDescs
		} else {
			delete(sp.typeIndex, serviceType)
		}
	}
}

// registerSingletonService registers a service descriptor with the dig container.
func (sp *serviceProvider) registerSingletonService(desc *serviceDescriptor) error {
	if desc.Lifetime != Singleton {
		return fmt.Errorf("registerSingletonService called with non-singleton lifetime: %s", desc.Lifetime)
	}

	// Handle decorators separately
	if desc.isDecorator() && desc.DecorateInfo != nil {
		err := sp.digContainer.Decorate(desc.DecorateInfo.Decorator, desc.DecorateInfo.DecorateOptions...)
		return err
	}

	// Ensure we have a constructor
	if desc.Constructor == nil {
		return MissingConstructorError{ServiceType: desc.ServiceType, Context: "service"}
	}

	// Check if this is a result object constructor
	isResultObject := false
	fnInfo := typecache.GetTypeInfo(reflect.TypeOf(desc.Constructor))
	if fnInfo.IsFunc && fnInfo.NumOut > 0 {
		firstOutInfo := typecache.GetTypeInfo(fnInfo.OutTypes[0])
		if firstOutInfo.IsStruct && firstOutInfo.HasOutField {
			isResultObject = true
		}
	}

	// For singleton services that are NOT result objects, wrap the constructor
	if !isResultObject {
		// Wrap the constructor to capture disposable instances
		wrappedConstructor := sp.wrapSingletonConstructor(desc.Constructor)
		return sp.digContainer.Provide(wrappedConstructor, desc.ProvideOptions...)
	}

	// For result objects, register directly
	return sp.digContainer.Provide(desc.Constructor, desc.ProvideOptions...)
}

// wrapSingletonConstructor wraps a singleton constructor to track disposable instances.
func (sp *serviceProvider) wrapSingletonConstructor(constructor interface{}) interface{} {
	fnType := reflect.TypeOf(constructor)
	fnInfo := typecache.GetTypeInfo(fnType)
	fnValue := reflect.ValueOf(constructor)

	return reflect.MakeFunc(fnType, func(args []reflect.Value) []reflect.Value {
		// Call the original constructor
		results := fnValue.Call(args)

		// If successful and has a result, check if it's disposable
		if len(results) > 0 && results[0].IsValid() {
			// Use cached info to check if we can call IsNil
			canBeNil := typecache.GetTypeInfo(results[0].Type()).CanBeNil
			if canBeNil && !results[0].IsNil() {
				// Check if there's an error result
				hasError := false
				if fnInfo.NumOut > 1 && fnInfo.HasErrorReturn && results[1].IsValid() && !results[1].IsNil() {
					hasError = true
				}

				if !hasError {
					instance := results[0].Interface()
					// Track the instance in the root scope for disposal
					sp.rootScope.captureDisposable(instance)
				}
			} else if !canBeNil {
				// For non-nillable types, just capture if no error
				hasError := false
				if fnInfo.NumOut > 1 && fnInfo.HasErrorReturn && results[1].IsValid() && !results[1].IsNil() {
					hasError = true
				}

				if !hasError {
					instance := results[0].Interface()
					sp.rootScope.captureDisposable(instance)
				}
			}
		}

		return results
	}).Interface()
}

// GetRootScope returns the root scope of the service provider.
func (sp *serviceProvider) GetRootScope() Scope {
	if sp.IsDisposed() {
		panic(ErrProviderDisposed)
	}

	return sp.rootScope
}

// CreateScope creates a new service scope.
func (sp *serviceProvider) CreateScope(ctx context.Context) Scope {
	if sp.IsDisposed() {
		panic(ErrProviderDisposed)
	}

	if ctx == nil {
		ctx = context.Background()
	}

	newScope := newScope(sp, ctx)
	newScope.parentScope = sp.rootScope
	return newScope
}

// Resolve gets the service object of the specified type.
func (sp *serviceProvider) Resolve(serviceType reflect.Type) (interface{}, error) {
	if sp.IsDisposed() {
		return nil, ErrProviderDisposed
	}

	if serviceType == nil {
		return nil, ErrInvalidServiceType
	}

	return sp.rootScope.Resolve(serviceType)
}

// ResolveKeyed gets the service object of the specified type with the specified key.
func (sp *serviceProvider) ResolveKeyed(serviceType reflect.Type, serviceKey interface{}) (interface{}, error) {
	if sp.IsDisposed() {
		return nil, ErrProviderDisposed
	}

	if serviceType == nil {
		return nil, ErrInvalidServiceType
	}

	if serviceKey == nil {
		return nil, ErrServiceKeyNil
	}

	return sp.rootScope.ResolveKeyed(serviceType, serviceKey)
}

// ResolveGroup gets all services of the specified type registered in a group.
func (sp *serviceProvider) ResolveGroup(serviceType reflect.Type, groupName string) ([]interface{}, error) {
	if sp.IsDisposed() {
		return nil, ErrProviderDisposed
	}

	if serviceType == nil {
		return nil, ErrInvalidServiceType
	}

	if groupName == "" {
		return nil, fmt.Errorf("group name cannot be empty")
	}

	return sp.rootScope.ResolveGroup(serviceType, groupName)
}

// IsService determines whether the specified service type is available.
func (sp *serviceProvider) IsService(serviceType reflect.Type) bool {
	if sp.IsDisposed() {
		return false
	}

	sp.mu.RLock()
	defer sp.mu.RUnlock()

	_, exists := sp.descriptorIndex[serviceType]
	return exists
}

// IsKeyedService determines whether the specified keyed service type is available.
func (sp *serviceProvider) IsKeyedService(serviceType reflect.Type, serviceKey interface{}) bool {
	if sp.IsDisposed() {
		return false
	}

	sp.mu.RLock()
	defer sp.mu.RUnlock()

	key := typeKeyPair{serviceType: serviceType, serviceKey: serviceKey}
	_, exists := sp.keyedIndex[key]
	return exists
}

// Decorate provides a decorator for a type that has already been provided in the Scope.
func (sp *serviceProvider) Decorate(decorator interface{}, opts ...DecorateOption) error {
	if sp.IsDisposed() {
		return ErrProviderDisposed
	}

	if decorator == nil {
		return ErrDecoratorNil
	}

	sp.mu.Lock()
	defer sp.mu.Unlock()

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

	return sp.addInternal(descriptor)
}

// IsDisposed returns true if the provider is disposed.
func (sp *serviceProvider) IsDisposed() bool {
	return atomic.LoadInt32(&sp.disposed) != 0
}

// Close disposes the ServiceProvider.
func (sp *serviceProvider) Close() error {
	if !atomic.CompareAndSwapInt32(&sp.disposed, 0, 1) {
		return nil
	}

	runtime.SetFinalizer(sp, nil)
	return sp.rootScope.Close()
}

// Invoke executes a function with automatic dependency injection.
func (sp *serviceProvider) Invoke(function interface{}) error {
	if sp.IsDisposed() {
		return ErrProviderDisposed
	}

	return sp.rootScope.Invoke(function)
}

// Helper function moved from collection.go
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

// typeKeyPair represents a type-key combination for keyed services.
type typeKeyPair struct {
	serviceType reflect.Type
	serviceKey  interface{}
}

// Resolve is a generic helper function that returns the service as type T.
func Resolve[T any](s ServiceProvider) (T, error) {
	var zero T
	if s == nil {
		return zero, ErrNilServiceProvider
	}

	serviceType, err := determineServiceType[T]()
	if err != nil {
		return zero, err
	}

	service, err := s.Resolve(serviceType)
	if err != nil {
		return zero, fmt.Errorf("unable to resolve service of type %s: %w", formatType(serviceType), err)
	}

	return assertServiceType[T](service, serviceType, nil)
}

// ResolveKeyed is a generic helper function that returns the keyed service as type T.
func ResolveKeyed[T any](s ServiceProvider, serviceKey interface{}) (T, error) {
	var zero T
	if s == nil {
		return zero, ErrNilServiceProvider
	}

	serviceType, err := determineServiceType[T]()
	if err != nil {
		return zero, err
	}

	service, err := s.ResolveKeyed(serviceType, serviceKey)
	if err != nil {
		return zero, fmt.Errorf("unable to resolve service of type %s: %w", formatType(serviceType), err)
	}

	return assertServiceType[T](service, serviceType, serviceKey)
}

// ResolveGroup is a generic helper function that returns the group services as type []T.
func ResolveGroup[T any](s ServiceProvider, groupName string) ([]T, error) {
	if s == nil {
		return nil, ErrNilServiceProvider
	}

	serviceType, err := determineServiceType[T]()
	if err != nil {
		return nil, err
	}

	services, err := s.ResolveGroup(serviceType, groupName)
	if err != nil {
		return nil, fmt.Errorf("unable to resolve group %q of type %s: %w", groupName, formatType(serviceType), err)
	}

	// Convert []interface{} to []T
	result := make([]T, 0, len(services))
	for i, service := range services {
		if service == nil {
			continue
		}

		// Type assertion to T
		if svc, ok := service.(T); ok {
			result = append(result, svc)
			continue
		}

		// If T is a non-pointer type and service is a pointer to T, dereference it
		tType := reflect.TypeOf((*T)(nil)).Elem()
		if tType.Kind() != reflect.Ptr && tType.Kind() != reflect.Interface {
			// T is a value type, check if service is *T
			serviceValue := reflect.ValueOf(service)
			if serviceValue.Kind() == reflect.Ptr && serviceValue.Elem().Type() == tType {
				// Service is *T and we want T, so dereference
				if !serviceValue.IsNil() {
					result = append(result, serviceValue.Elem().Interface().(T))
					continue
				}
			}
		}

		// If we couldn't convert, return an error
		return nil, TypeMismatchError{
			Expected: serviceType,
			Actual:   reflect.TypeOf(service),
			Context:  fmt.Sprintf("group item %d type assertion", i),
		}
	}

	return result, nil
}

// determineServiceType determines the actual service type to resolve based on the generic type T.
func determineServiceType[T any]() (reflect.Type, error) {
	serviceType, _, err := typecache.DetermineServiceTypeCached[T]()
	return serviceType, err
}

// assertServiceType performs type assertion and returns the service as type T.
func assertServiceType[T any](service interface{}, serviceType reflect.Type, serviceKey interface{}) (T, error) {
	var zero T

	if service == nil {
		return zero, ResolutionError{ServiceType: serviceType, ServiceKey: serviceKey, Cause: ErrServiceNotFound}
	}

	// Type assertion to T
	if svc, ok := service.(T); ok {
		return svc, nil
	}

	// If T is a non-pointer type and service is a pointer to T, dereference it
	tType := reflect.TypeOf((*T)(nil)).Elem()
	if tType.Kind() != reflect.Ptr && tType.Kind() != reflect.Interface {
		// T is a value type, check if service is *T
		serviceValue := reflect.ValueOf(service)
		if serviceValue.Kind() == reflect.Ptr && serviceValue.Elem().Type() == tType {
			// Service is *T and we want T, so dereference
			if serviceValue.IsNil() {
				return zero, ResolutionError{ServiceType: serviceType, ServiceKey: serviceKey, Cause: ErrServiceNotFound}
			}
			return serviceValue.Elem().Interface().(T), nil
		}
	}

	var msg string
	if serviceKey != nil {
		msg = "keyed type assertion"
	} else {
		msg = "type assertion"
	}

	return zero, TypeMismatchError{
		Expected: serviceType,
		Actual:   reflect.TypeOf(service),
		Context:  msg,
	}
}
