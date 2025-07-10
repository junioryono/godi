package godi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.uber.org/dig"
)

// Scope defines a disposable service scope.
// Scopes are used to control the lifetime of scoped services.
//
// In web applications, a scope is typically created for each HTTP request,
// ensuring that services like database connections are properly managed
// and disposed at the end of the request.
//
// Example:
//
//	scope := provider.CreateScope(ctx)
//	defer scope.Close()
//
//	scopedProvider := scope.ServiceProvider()
//	service, err := godi.Resolve[MyService](scopedProvider)
type Scope interface {
	// ID returns the unique ID of this scope.
	ID() string

	// Context returns the context associated with this scope.
	Context() context.Context

	// ServiceProvider returns the ServiceProvider for this scope.
	// All services resolved from this provider will be scoped to this scope's lifetime.
	ServiceProvider() ServiceProvider

	// IsRootScope returns true if this provider is the root scope.
	IsRootScope() bool

	// GetRootScope returns the root scope of this provider.
	GetRootScope() Scope

	// Parent returns the parent scope of this scope.
	Parent() Scope

	// Dispose the scope and all scoped services.
	// Services are disposed in reverse order of creation (LIFO).
	// This method is safe to call multiple times.
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

// DisposableWithContext allows disposal with context for graceful shutdown.
// Services implementing this interface can perform context-aware cleanup.
//
// Example:
//
//	type DatabaseConnection struct {
//	    conn *sql.DB
//	}
//
//	func (dc *DatabaseConnection) Close(ctx context.Context) error {
//	    done := make(chan error, 1)
//	    go func() {
//	        done <- dc.conn.Close()
//	    }()
//
//	    select {
//	    case err := <-done:
//	        return err
//	    case <-ctx.Done():
//	        return ctx.Err()
//	    }
//	}
type DisposableWithContext interface {
	// Close disposes the resource with the provided context.
	// Implementations should respect context cancellation for graceful shutdown.
	Close(ctx context.Context) error
}

// ServiceScopeFactory defines a factory for creating service scopes.
// This interface is automatically available in the service provider.
type ServiceScopeFactory interface {
	// CreateScope creates a new Scope with the given context.
	// The context is available for injection into scoped services.
	CreateScope(ctx context.Context) Scope
}

// serviceProviderScope implements Scope, ServiceProvider, ServiceScopeFactory.
type serviceProviderScope struct {
	// Dig scope for this service scope
	digScope *dig.Scope

	// Parent provider
	rootProvider *serviceProvider

	// State
	disposed    int32
	disposables []Disposable
	isRootScope bool
	ctx         context.Context
	sync        sync.RWMutex

	// Scope tracking
	scopeID string
	parent  *serviceProviderScope

	// Track services created in this scope for disposal
	scopedInstances map[reflect.Type][]interface{}
}

// newServiceProviderScope creates a new ServiceProvider scope.
func newServiceProviderScope(provider *serviceProvider, isRootScope bool, ctx context.Context) *serviceProviderScope {
	if ctx == nil {
		ctx = context.Background()
	}

	scope := &serviceProviderScope{
		rootProvider:    provider,
		isRootScope:     isRootScope,
		ctx:             ctx,
		scopeID:         uuid.NewString(),
		scopedInstances: make(map[reflect.Type][]interface{}),
	}

	scope.ctx = withCurrentScope(ctx, scope)

	if isRootScope {
		// Root scope uses the main container directly
		scope.digScope = &dig.Scope{} // This is a placeholder - we use provider.digContainer
	} else {
		// Create a dig scope for non-root scopes - must hold provider's digMutex
		provider.digMutex.Lock()
		scope.digScope = provider.digContainer.Scope(scope.scopeID)

		if scope.digScope == nil {
			panic(ErrFailedToCreateScope)
		}

		// Register context in the dig scope
		if err := scope.digScope.Provide(func() context.Context { return ctx }); err != nil {
			provider.digMutex.Unlock()
			panic(ErrFailedToCreateScope)
		}

		// Register the ServiceProvider in the dig scope (override the root registration)
		if err := scope.digScope.Provide(func() ServiceProvider { return scope }); err != nil {
			provider.digMutex.Unlock()
			panic(fmt.Errorf("failed to register context in dig scope %s: %w", scope.scopeID, err))
		}

		// Register ServiceScopeFactory in the dig scope
		if err := scope.digScope.Provide(func() ServiceScopeFactory { return scope }); err != nil {
			provider.digMutex.Unlock()
			panic(fmt.Errorf("failed to register ServiceProvider in dig scope %s: %w", scope.scopeID, err))
		}
		provider.digMutex.Unlock()

		// Register scoped services in this dig scope
		for _, desc := range provider.descriptors {
			if desc.Lifetime == Scoped {
				if err := scope.registerScopedService(desc); err != nil {
					// Log error but continue - this is during initialization
					fmt.Printf("Warning: failed to register scoped service %s: %v\n", formatType(desc.ServiceType), err)
				}
			}
		}
	}

	runtime.SetFinalizer(scope, (*serviceProviderScope).finalize)

	return scope
}

func (scope *serviceProviderScope) ID() string {
	if scope.IsDisposed() {
		panic(ErrScopeDisposed)
	}

	return scope.scopeID
}

func (scope *serviceProviderScope) Context() context.Context {
	if scope.IsDisposed() {
		panic(ErrScopeDisposed)
	}

	return scope.ctx
}

// ServiceProvider implements Scope.ServiceProvider.
func (scope *serviceProviderScope) ServiceProvider() ServiceProvider {
	if scope.IsDisposed() {
		panic(ErrScopeDisposed)
	}

	return scope
}

func (scope *serviceProviderScope) IsRootScope() bool {
	if scope.IsDisposed() {
		panic(ErrScopeDisposed)
	}

	return scope.isRootScope
}

func (scope *serviceProviderScope) GetRootScope() Scope {
	if scope.IsDisposed() {
		panic(ErrScopeDisposed)
	}

	if scope.isRootScope {
		return scope
	}

	// Traverse up to find the root scope
	current := scope
	for current.parent != nil {
		current = current.parent
	}

	return current
}

func (scope *serviceProviderScope) Parent() Scope {
	if scope.IsDisposed() {
		panic(ErrScopeDisposed)
	}

	if scope.parent == nil {
		return nil
	}

	return scope.parent
}

func (scope *serviceProviderScope) registerScopedService(desc *serviceDescriptor) error {
	// Create provider options
	opts := []ProvideOption{}

	if desc.isKeyedService() {
		opts = append(opts, Name(fmt.Sprintf("%v", desc.ServiceKey)))
	}

	// Handle groups if specified in metadata
	if group, ok := desc.Metadata["group"].(string); ok && group != "" {
		opts = append(opts, Group(group))
	}

	// Handle 'as' interfaces if specified
	if interfaces, ok := desc.Metadata["as"].([]interface{}); ok {
		opts = append(opts, As(interfaces...))
	}

	// Use the constructor from the descriptor
	if desc.Constructor != nil {
		// Wrap the constructor to track instances for disposal
		wrappedConstructor := scope.wrapConstructorForTracking(desc.Constructor, desc.ServiceType)
		scope.rootProvider.digMutex.Lock()
		err := scope.digScope.Provide(wrappedConstructor, opts...)
		scope.rootProvider.digMutex.Unlock()
		return err
	}

	// This shouldn't happen if descriptor is validated
	return MissingConstructorError{ServiceType: desc.ServiceType, Context: "descriptor"}
}

func (scope *serviceProviderScope) wrapConstructorForTracking(constructor interface{}, serviceType reflect.Type) interface{} {
	fnType := reflect.TypeOf(constructor)
	fnValue := reflect.ValueOf(constructor)

	return reflect.MakeFunc(fnType, func(args []reflect.Value) []reflect.Value {
		// Call the original constructor
		results := fnValue.Call(args)

		// Track the instance if successful
		if len(results) > 0 && results[0].IsValid() {
			instance := results[0].Interface()

			scope.sync.Lock()
			scope.scopedInstances[serviceType] = append(scope.scopedInstances[serviceType], instance)
			scope.captureDisposable(instance, false)
			scope.sync.Unlock()
		}

		return results
	}).Interface()
}

// Resolve implements ServiceProvider.
func (scope *serviceProviderScope) Resolve(serviceType reflect.Type) (interface{}, error) {
	if scope.IsDisposed() {
		return nil, ErrScopeDisposed
	}

	if serviceType == nil {
		return nil, ErrInvalidServiceType
	}

	// Create a context with timeout if configured
	var ctx context.Context
	var cancel context.CancelFunc
	if scope.rootProvider.options.ResolutionTimeout > 0 {
		ctx, cancel = context.WithTimeout(scope.ctx, scope.rootProvider.options.ResolutionTimeout)
		defer cancel()
	} else {
		ctx = scope.ctx
	}

	// Create a channel for the result
	type resolveResult struct {
		value interface{}
		err   error
	}
	resultChan := make(chan resolveResult, 1)

	// Record start time for metrics
	startTime := time.Now()

	// Run the resolution in a goroutine
	go func() {
		result, err := scope.resolveService(serviceType)
		resultChan <- resolveResult{value: result, err: err}
	}()

	// Wait for result or timeout
	var result interface{}
	var err error

	select {
	case res := <-resultChan:
		result = res.value
		err = res.err
	case <-ctx.Done():
		err = TimeoutError{
			ServiceType: serviceType,
			Timeout:     scope.rootProvider.options.ResolutionTimeout,
		}
	}

	// Calculate resolution duration
	duration := time.Since(startTime)

	// Call callbacks
	if err == nil && scope.rootProvider.options.OnServiceResolved != nil {
		scope.rootProvider.options.OnServiceResolved(serviceType, result, duration)
	} else if err != nil && scope.rootProvider.options.OnServiceError != nil {
		scope.rootProvider.options.OnServiceError(serviceType, err)
	}

	return result, err
}

func (scope *serviceProviderScope) resolveService(serviceType reflect.Type) (interface{}, error) {
	// First, try to resolve a provider function for this type (for transient services)
	providerFuncType := reflect.FuncOf([]reflect.Type{}, []reflect.Type{serviceType}, false)

	var providerFunc interface{}
	var providerErr error

	// Try to get the provider function
	providerExtractor := reflect.MakeFunc(
		reflect.FuncOf([]reflect.Type{providerFuncType}, []reflect.Type{reflect.TypeOf((*error)(nil)).Elem()}, false),
		func(args []reflect.Value) []reflect.Value {
			if len(args) > 0 && args[0].IsValid() {
				providerFunc = args[0].Interface()
				return []reflect.Value{reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())}
			}

			return []reflect.Value{reflect.ValueOf(ErrProviderFunctionNotFound)}
		},
	)

	// Try to invoke the provider extractor
	scope.rootProvider.digMutex.Lock()
	if scope.isRootScope {
		providerErr = scope.rootProvider.digContainer.Invoke(providerExtractor.Interface())
	} else {
		providerErr = scope.digScope.Invoke(providerExtractor.Interface())
	}
	scope.rootProvider.digMutex.Unlock()

	// If we found a provider function, call it to get a new instance
	if providerErr == nil && providerFunc != nil {
		// Call the provider function
		results := reflect.ValueOf(providerFunc).Call(nil)
		if len(results) > 0 && results[0].IsValid() {
			result := results[0].Interface()

			// Track the instance for disposal in this scope
			scope.sync.Lock()
			scope.scopedInstances[serviceType] = append(scope.scopedInstances[serviceType], result)
			scope.captureDisposable(result, false)
			scope.sync.Unlock()

			return result, nil
		}
	}

	// If no provider function, resolve normally (for non-transient services)
	var result interface{}
	var resolveErr error

	// Build the extraction function dynamically
	fnType := reflect.FuncOf([]reflect.Type{serviceType}, []reflect.Type{reflect.TypeOf((*error)(nil)).Elem()}, false)
	fn := reflect.MakeFunc(fnType, func(args []reflect.Value) []reflect.Value {
		if len(args) > 0 && args[0].IsValid() {
			result = args[0].Interface()
			return []reflect.Value{reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())}
		}

		err := ResolutionError{ServiceType: serviceType, Cause: ErrFailedToExtractService}
		return []reflect.Value{reflect.ValueOf(err)}
	})

	// Invoke through dig with mutex protection
	scope.rootProvider.digMutex.Lock()
	if scope.isRootScope {
		resolveErr = scope.rootProvider.digContainer.Invoke(fn.Interface())
	} else {
		resolveErr = scope.digScope.Invoke(fn.Interface())
	}
	scope.rootProvider.digMutex.Unlock()

	return result, resolveErr
}

// ResolveKeyed implements ServiceProvider.
func (scope *serviceProviderScope) ResolveKeyed(serviceType reflect.Type, serviceKey interface{}) (interface{}, error) {
	if scope.IsDisposed() {
		return nil, ErrScopeDisposed
	}

	if serviceType == nil {
		return nil, ErrInvalidServiceType
	}

	if serviceKey == nil {
		return nil, ErrServiceKeyNil
	}

	// Record start time for metrics
	startTime := time.Now()

	result, err := scope.resolveKeyedService(serviceType, serviceKey)

	// Calculate resolution duration
	duration := time.Since(startTime)

	// Record metrics and callbacks
	if err == nil && scope.rootProvider.options.OnServiceResolved != nil {
		scope.rootProvider.options.OnServiceResolved(serviceType, result, duration)
	} else if err != nil && scope.rootProvider.options.OnServiceError != nil {
		scope.rootProvider.options.OnServiceError(serviceType, err)
	}

	return result, err
}

func (scope *serviceProviderScope) resolveKeyedService(serviceType reflect.Type, serviceKey interface{}) (interface{}, error) {
	// First, try to resolve a provider function for this keyed service (for transient services)
	providerFuncType := reflect.FuncOf([]reflect.Type{}, []reflect.Type{serviceType}, false)

	// Create a parameter struct to request the keyed provider function
	paramType := reflect.StructOf([]reflect.StructField{
		{
			Name:      "In",
			Type:      reflect.TypeOf(In{}),
			Anonymous: true,
		},
		{
			Name: "Provider",
			Type: providerFuncType,
			Tag:  reflect.StructTag(fmt.Sprintf(`name:"%v"`, serviceKey)),
		},
	})

	var providerFunc interface{}
	var providerErr error

	// Try to get the keyed provider function
	providerExtractor := reflect.MakeFunc(
		reflect.FuncOf([]reflect.Type{paramType}, []reflect.Type{reflect.TypeOf((*error)(nil)).Elem()}, false),
		func(args []reflect.Value) []reflect.Value {
			if len(args) > 0 && args[0].IsValid() {
				providerField := args[0].FieldByName("Provider")
				if providerField.IsValid() && !providerField.IsZero() {
					providerFunc = providerField.Interface()
					return []reflect.Value{reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())}
				}
			}

			return []reflect.Value{reflect.ValueOf(ErrKeyedProviderFunctionNotFound)}
		},
	)

	// Try to invoke the provider extractor
	scope.rootProvider.digMutex.Lock()
	if scope.isRootScope {
		providerErr = scope.rootProvider.digContainer.Invoke(providerExtractor.Interface())
	} else {
		providerErr = scope.digScope.Invoke(providerExtractor.Interface())
	}
	scope.rootProvider.digMutex.Unlock()

	// If we found a provider function, call it to get a new instance
	if providerErr == nil && providerFunc != nil {
		// Call the provider function
		results := reflect.ValueOf(providerFunc).Call(nil)
		if len(results) > 0 && results[0].IsValid() {
			result := results[0].Interface()

			// Track the instance for disposal in this scope
			scope.sync.Lock()
			scope.scopedInstances[serviceType] = append(scope.scopedInstances[serviceType], result)
			scope.captureDisposable(result, false)
			scope.sync.Unlock()

			return result, nil
		}
	}

	// If no provider function, resolve normally (for non-transient keyed services)
	// This is complex with dig's current API - we need to use reflection
	// to create a struct with the right name tag
	paramType = reflect.StructOf([]reflect.StructField{
		{
			Name:      "In",
			Type:      reflect.TypeOf(In{}),
			Anonymous: true,
		},
		{
			Name: "Service",
			Type: serviceType,
			Tag:  reflect.StructTag(fmt.Sprintf(`name:"%v"`, serviceKey)),
		},
	})

	// Create extraction function
	var result interface{}
	fnType := reflect.FuncOf([]reflect.Type{paramType}, []reflect.Type{reflect.TypeOf((*error)(nil)).Elem()}, false)
	fn := reflect.MakeFunc(fnType, func(args []reflect.Value) []reflect.Value {
		if len(args) > 0 && args[0].IsValid() {
			serviceField := args[0].FieldByName("Service")
			if serviceField.IsValid() {
				result = serviceField.Interface()
				return []reflect.Value{reflect.Zero(reflect.TypeOf((*error)(nil)).Elem())}
			}
		}

		err := ResolutionError{ServiceType: serviceType, ServiceKey: serviceKey, Cause: ErrFailedToExtractKeyedService}
		return []reflect.Value{reflect.ValueOf(err)}
	})

	// Invoke through dig with mutex protection
	var resolveErr error
	scope.rootProvider.digMutex.Lock()
	if scope.isRootScope {
		resolveErr = scope.rootProvider.digContainer.Invoke(fn.Interface())
	} else {
		resolveErr = scope.digScope.Invoke(fn.Interface())
	}
	scope.rootProvider.digMutex.Unlock()

	if resolveErr != nil {
		return nil, resolveErr
	}

	return result, nil
}

// IsService implements ServiceProvider.
func (scope *serviceProviderScope) IsService(serviceType reflect.Type) bool {
	if scope.IsDisposed() {
		return false
	}
	return scope.rootProvider.IsService(serviceType)
}

// IsKeyedService implements ServiceProvider.
func (scope *serviceProviderScope) IsKeyedService(serviceType reflect.Type, serviceKey interface{}) bool {
	if scope.IsDisposed() {
		return false
	}
	return scope.rootProvider.IsKeyedService(serviceType, serviceKey)
}

// CreateScope implements ServiceScopeFactory.
func (scope *serviceProviderScope) CreateScope(ctx context.Context) Scope {
	if scope.IsDisposed() {
		panic(ErrScopeDisposed)
	}

	newScope := newServiceProviderScope(scope.rootProvider, false, ctx)
	newScope.parent = scope
	return newScope
}

// IsDisposed implements ServiceProvider.
func (scope *serviceProviderScope) IsDisposed() bool {
	return atomic.LoadInt32(&scope.disposed) != 0
}

// captureDisposable captures a service for disposal when the scope is disposed.
func (scope *serviceProviderScope) captureDisposable(service interface{}, shouldLock bool) {
	if service == scope {
		return
	}

	var disposable Disposable
	switch v := service.(type) {
	case Disposable:
		disposable = v
	case DisposableWithContext:
		disposable = &contextDisposableWrapper{
			disposable:     v,
			defaultTimeout: 30 * time.Second,
		}
	default:
		return
	}

	if shouldLock {
		scope.sync.Lock()
		defer scope.sync.Unlock()
	}

	if scope.IsDisposed() {
		disposable.Close()
		return
	}

	scope.disposables = append(scope.disposables, disposable)
}

func (scope *serviceProviderScope) Close() error {
	if !atomic.CompareAndSwapInt32(&scope.disposed, 0, 1) {
		return nil
	}

	runtime.SetFinalizer(scope, nil)

	scope.sync.Lock()
	toDispose := make([]Disposable, len(scope.disposables))
	copy(toDispose, scope.disposables)
	scope.disposables = nil
	scope.scopedInstances = nil
	scope.sync.Unlock()

	var errs []error
	// Dispose in reverse order (LIFO)
	for i := len(toDispose) - 1; i >= 0; i-- {
		if err := toDispose[i].Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// Invoke implements ServiceProvider.
func (scope *serviceProviderScope) Invoke(function interface{}) error {
	if scope.IsDisposed() {
		return ErrScopeDisposed
	}

	// Always use dig for invocation with mutex protection
	scope.rootProvider.digMutex.Lock()
	defer scope.rootProvider.digMutex.Unlock()

	if scope.isRootScope {
		return scope.rootProvider.digContainer.Invoke(function)
	}
	return scope.digScope.Invoke(function)
}

// finalize is called by the garbage collector.
func (scope *serviceProviderScope) finalize() {
	if !scope.IsDisposed() {
		scope.Close()
	}
}

// contextDisposableWrapper wraps DisposableWithContext as Disposable.
type contextDisposableWrapper struct {
	disposable     DisposableWithContext
	defaultTimeout time.Duration
}

func (w *contextDisposableWrapper) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), w.defaultTimeout)
	defer cancel()
	return w.disposable.Close(ctx)
}

// scopeContextKey is the key for storing the current scope in context.
type scopeContextKey struct{}

// withCurrentScope returns a context with the current scope.
func withCurrentScope(ctx context.Context, scope *serviceProviderScope) context.Context {
	return context.WithValue(ctx, scopeContextKey{}, scope)
}

// currentScopeFromContext gets the current scope from context.
func currentScopeFromContext(ctx context.Context) (*serviceProviderScope, bool) {
	scope, ok := ctx.Value(scopeContextKey{}).(*serviceProviderScope)
	return scope, ok
}
