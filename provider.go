package godi

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junioryono/godi/v3/internal/typecache"
	"go.uber.org/dig"
)

// ServiceProvider is the main dependency injection container interface.
// It provides methods to resolve services and create scopes.
//
// ServiceProvider is thread-safe and can be used concurrently.
// Services are resolved lazily on first request.
//
// Example:
//
//	provider, err := collection.Build()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer provider.Close()
//
//	// Resolve a service
//	logger, err := godi.Resolve[Logger](provider)
//	if err != nil {
//	    log.Fatal(err)
//	}
type ServiceProvider interface {
	// GetRootScope returns the root scope of the service provider.
	// The root scope is used for singleton services and tracks disposal of singletons.
	// It is also the default scope for resolving services.
	// This scope is created automatically when the provider is built.
	GetRootScope() Scope

	// IsService determines whether the specified service type is available.
	// This is useful for optional dependencies.
	IsService(serviceType reflect.Type) bool

	// IsKeyedService determines whether the specified keyed service type is available.
	IsKeyedService(serviceType reflect.Type, serviceKey interface{}) bool

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

	// Invoke executes a function with dependency injection.
	// All parameters of the function are resolved from the container.
	// The function can optionally return an error.
	Invoke(function interface{}) error

	// IsDisposed returns true if the provider has been disposed.
	IsDisposed() bool

	Disposable
}

// ServiceProviderOptions configures various ServiceProvider behaviors.
type ServiceProviderOptions struct {
	// ValidateOnBuild determines whether to validate all services can be created during BuildServiceProvider.
	// This can catch configuration errors early but may impact startup time for large containers.
	ValidateOnBuild bool

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
	descriptors     []*Descriptor
	descriptorIndex map[reflect.Type]struct{}
	keyedIndex      map[typeKeyPair]struct{}

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

// newServiceProviderWithOptions creates a new ServiceProvider from the given service collection.
func newServiceProviderWithOptions(services ServiceCollection, options *ServiceProviderOptions) (ServiceProvider, error) {
	if services == nil {
		return nil, ErrServicesNil
	}

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
		descriptors:       services.ToSlice(),
		descriptorIndex:   make(map[reflect.Type]struct{}),
		keyedIndex:        make(map[typeKeyPair]struct{}),
		options:           options,
		scopes:            make(map[string]*serviceProviderScope),
		providerCallbacks: make(map[uintptr]Callback),
		beforeCallbacks:   make(map[uintptr]BeforeCallback),
	}

	// Register all services with dig based on lifetime
	for _, desc := range provider.descriptors {
		if desc.Lifetime != Singleton {
			continue
		}

		if err := provider.registerSingletonService(desc); err != nil {
			return nil, RegistrationError{
				ServiceType: desc.ServiceType,
				Operation:   "register",
				Cause:       err,
			}
		}
	}

	provider.rootScope = newScope(provider, context.Background())

	provider.descriptors = append(provider.descriptors, []*serviceDescriptor{
		{
			ServiceType: reflect.TypeOf((*context.Context)(nil)).Elem(),
			Lifetime:    Scoped, // Important: must be scoped
			Constructor: func() context.Context {
				return provider.rootScope.ctx
			},
		},
		{
			ServiceType: reflect.TypeOf((*ServiceProvider)(nil)).Elem(),
			Lifetime:    Singleton,
			Constructor: func() ServiceProvider {
				return provider.rootScope
			},
		},
		{
			ServiceType: reflect.TypeOf((*Scope)(nil)).Elem(),
			Lifetime:    Scoped,
			Constructor: func() Scope {
				return provider.rootScope // Default, will be overridden in scope
			},
		},
	}...)

	// Build indexes
	for _, desc := range provider.descriptors {
		if desc.isKeyedService() {
			key := typeKeyPair{serviceType: desc.ServiceType, serviceKey: desc.ServiceKey}
			provider.keyedIndex[key] = struct{}{}
		} else {
			provider.descriptorIndex[desc.ServiceType] = struct{}{}
		}
	}

	if err := provider.digContainer.Provide(func() context.Context {
		return provider.rootScope.ctx
	}); err != nil {
		return nil, fmt.Errorf("failed to register context.Context: %w", err)
	}

	// Register ServiceProvider
	if err := provider.digContainer.Provide(func() ServiceProvider {
		return provider.rootScope
	}); err != nil {
		return nil, fmt.Errorf("failed to register ServiceProvider: %w", err)
	}

	// Register Scope
	if err := provider.digContainer.Provide(func() Scope {
		return provider.rootScope
	}); err != nil {
		return nil, fmt.Errorf("failed to register Scope: %w", err)
	}

	// Validate if requested
	if options.ValidateOnBuild {
		if err := provider.validateAllServices(); err != nil {
			return nil, err
		}
	}

	return provider, nil
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

// validateAllServices validates that all services can be constructed.
func (sp *serviceProvider) validateAllServices() error {
	// Use dig's built-in validation by attempting a dry run
	if sp.options.DryRun {
		return nil // Already in dry run mode
	}

	// Create a test invocation that touches all services
	var testFuncs []interface{}
	for _, desc := range sp.descriptors {
		if desc.Lifetime == Singleton && desc.Constructor != nil {
			// Create a test function that requests this service
			serviceType := desc.ServiceType
			testFunc := reflect.MakeFunc(
				reflect.FuncOf([]reflect.Type{serviceType}, []reflect.Type{}, false),
				func(args []reflect.Value) []reflect.Value {
					return nil
				},
			).Interface()
			testFuncs = append(testFuncs, testFunc)
		}
	}

	// Try to invoke all test functions
	for _, testFunc := range testFuncs {
		err := sp.digContainer.Invoke(testFunc)
		if err != nil {
			// Check if it's a cycle error
			if dig.IsCycleDetected(err) {
				// Convert to our cycle error format
				return &CircularDependencyError{
					ServiceType: reflect.TypeOf(testFunc).In(0),
					Chain:       []reflect.Type{}, // dig doesn't expose the chain
					DigError:    err,
				}
			}
			return err
		}
	}

	return nil
}

// GetRootScope returns the root scope of the service provider.
func (sp *serviceProvider) GetRootScope() Scope {
	if sp.IsDisposed() {
		panic(ErrProviderDisposed)
	}

	return sp.rootScope
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

// Decorate provides a decorator for a type that has already been provided in the Scope.
func (sp *serviceProvider) Decorate(decorator interface{}, opts ...DecorateOption) error {
	if sp.IsDisposed() {
		return ErrProviderDisposed
	}

	if decorator == nil {
		return ErrDecoratorNil
	}

	return sp.digContainer.Decorate(decorator, opts...)
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
