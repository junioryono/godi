package godi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

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
	ServiceProvider

	// ID returns the unique ID of this scope.
	ID() string

	// Context returns the context associated with this scope.
	Context() context.Context

	// IsRootScope returns true if this provider is the root scope.
	IsRootScope() bool

	// GetRootScope returns the root scope of this provider.
	GetRootScope() Scope

	// Parent returns the parent scope of this scope.
	Parent() Scope
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

// serviceProviderScope implements Scope, ServiceProvider, ServiceScopeFactory.
type serviceProviderScope struct {
	ctx     context.Context
	scopeID string

	digScope        *dig.Scope
	parentScope     *serviceProviderScope
	serviceProvider *serviceProvider

	disposed      int32
	disposables   []DisposableWithContext
	disposablesMu sync.RWMutex
}

// newServiceProviderScope creates a new ServiceProvider scope.
func newServiceProviderScope(provider *serviceProvider, ctx context.Context) *serviceProviderScope {
	if ctx == nil {
		ctx = context.Background()
	}

	scope := &serviceProviderScope{
		ctx:             ctx,
		scopeID:         generateID(),
		serviceProvider: provider,
	}

	scope.ctx = withCurrentScope(ctx, scope)

	// Create a dig scope for non-root scopes - must hold provider's mutex
	provider.scopesMu.Lock()
	defer provider.scopesMu.Unlock()

	provider.scopes[scope.scopeID] = scope
	scope.digScope = provider.digContainer.Scope(scope.scopeID)

	if scope.digScope == nil {
		panic(ErrFailedToCreateScope)
	}

	// Register context in the dig scope
	if err := scope.digScope.Provide(func() context.Context { return ctx }); err != nil {
		panic(ErrFailedToCreateScope)
	}

	// Register the ServiceProvider in the dig scope (override the root registration)
	if err := scope.digScope.Provide(func() ServiceProvider { return scope }); err != nil {
		panic(fmt.Errorf("failed to register context in dig scope %s: %w", scope.scopeID, err))
	}

	// Register the Scope in the dig scope
	if err := scope.digScope.Provide(func() Scope { return scope }); err != nil {
		panic(fmt.Errorf("failed to register scope in dig scope %s: %w", scope.scopeID, err))
	}

	// Register scoped services in this dig scope
	for _, desc := range provider.descriptors {
		if desc.Lifetime != Scoped {
			continue
		}

		// Create provider options
		opts := []ProvideOption{}

		if desc.isKeyedService() {
			opts = append(opts, Name(fmt.Sprintf("%v", desc.ServiceKey)))
		}

		// Handle groups if specified in metadata
		if group, ok := desc.Metadata["group"].(string); ok && group != "" {
			opts = append(opts, Group(group))
		}

		// Handle 'asOptions' if specified
		if asOpts, ok := desc.Metadata["asOptions"].([]ProvideOption); ok {
			opts = append(opts, asOpts...)
		}

		// Use the constructor from the descriptor
		if desc.Constructor == nil {
			// This shouldn't happen if descriptor is validated
			err := MissingConstructorError{ServiceType: desc.ServiceType, Context: "descriptor"}
			panic(fmt.Errorf("failed to register scoped service %s: %w", desc.ServiceType, err))
		}

		// Wrap the constructor to track instances for disposal
		wrappedConstructor := scope.wrapConstructorForTracking(desc.Constructor)
		err := scope.digScope.Provide(wrappedConstructor, opts...)
		if err != nil {
			panic(fmt.Errorf("failed to register scoped service %s: %w", desc.ServiceType, err))
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

func (scope *serviceProviderScope) IsRootScope() bool {
	if scope.IsDisposed() {
		panic(ErrScopeDisposed)
	}

	return scope.parentScope == nil
}

func (scope *serviceProviderScope) GetRootScope() Scope {
	if scope.IsDisposed() {
		panic(ErrScopeDisposed)
	}

	// Traverse up to find the root scope
	current := scope
	for current.parentScope != nil {
		current = current.parentScope
	}

	return current
}

func (scope *serviceProviderScope) Parent() Scope {
	if scope.IsDisposed() {
		panic(ErrScopeDisposed)
	}

	if scope.parentScope == nil {
		return nil
	}

	return scope.parentScope
}

func (scope *serviceProviderScope) wrapConstructorForTracking(constructor interface{}) interface{} {
	fnType := reflect.TypeOf(constructor)
	fnValue := reflect.ValueOf(constructor)

	return reflect.MakeFunc(fnType, func(args []reflect.Value) []reflect.Value {
		// Call the original constructor
		results := fnValue.Call(args)

		// Track the instance if successful
		if len(results) > 0 && results[0].IsValid() {
			instance := results[0].Interface()
			scope.captureDisposable(instance)
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
	if scope.serviceProvider.options.ResolutionTimeout > 0 {
		ctx, cancel = context.WithTimeout(scope.ctx, scope.serviceProvider.options.ResolutionTimeout)
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
			Timeout:     scope.serviceProvider.options.ResolutionTimeout,
		}
	}

	// Calculate resolution duration
	duration := time.Since(startTime)

	// Call callbacks
	if err == nil && scope.serviceProvider.options.OnServiceResolved != nil {
		scope.serviceProvider.options.OnServiceResolved(serviceType, result, duration)
	} else if err != nil && scope.serviceProvider.options.OnServiceError != nil {
		scope.serviceProvider.options.OnServiceError(serviceType, err)
	}

	return result, err
}

func (scope *serviceProviderScope) resolveService(serviceType reflect.Type) (interface{}, error) {
	// First, try to resolve a provider function for this type (for transient services)
	providerFuncType := reflect.FuncOf([]reflect.Type{}, []reflect.Type{serviceType}, false)

	var providerFunc interface{}

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
	scope.serviceProvider.scopesMu.Lock()
	providerErr := scope.digScope.Invoke(providerExtractor.Interface())
	scope.serviceProvider.scopesMu.Unlock()

	// If we found a provider function, call it to get a new instance
	if providerErr == nil && providerFunc != nil {
		// Call the provider function
		results := reflect.ValueOf(providerFunc).Call(nil)
		if len(results) > 0 && results[0].IsValid() {
			result := results[0].Interface()

			// Track the instance for disposal in this scope
			scope.captureDisposable(result)
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
	scope.serviceProvider.scopesMu.Lock()
	resolveErr = scope.digScope.Invoke(fn.Interface())
	scope.serviceProvider.scopesMu.Unlock()

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
	if err == nil && scope.serviceProvider.options.OnServiceResolved != nil {
		scope.serviceProvider.options.OnServiceResolved(serviceType, result, duration)
	} else if err != nil && scope.serviceProvider.options.OnServiceError != nil {
		scope.serviceProvider.options.OnServiceError(serviceType, err)
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
	scope.serviceProvider.scopesMu.Lock()
	providerErr = scope.digScope.Invoke(providerExtractor.Interface())
	scope.serviceProvider.scopesMu.Unlock()

	// If we found a provider function, call it to get a new instance
	if providerErr == nil && providerFunc != nil {
		// Call the provider function
		results := reflect.ValueOf(providerFunc).Call(nil)
		if len(results) > 0 && results[0].IsValid() {
			result := results[0].Interface()

			// Track the instance for disposal in this scope
			scope.captureDisposable(result)
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
	scope.serviceProvider.scopesMu.Lock()
	resolveErr = scope.digScope.Invoke(fn.Interface())
	scope.serviceProvider.scopesMu.Unlock()

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
	return scope.serviceProvider.IsService(serviceType)
}

// IsKeyedService implements ServiceProvider.
func (scope *serviceProviderScope) IsKeyedService(serviceType reflect.Type, serviceKey interface{}) bool {
	if scope.IsDisposed() {
		return false
	}
	return scope.serviceProvider.IsKeyedService(serviceType, serviceKey)
}

// CreateScope implements ServiceScopeFactory.
func (scope *serviceProviderScope) CreateScope(ctx context.Context) Scope {
	if scope.IsDisposed() {
		panic(ErrScopeDisposed)
	}

	newScope := newServiceProviderScope(scope.serviceProvider, ctx)
	newScope.parentScope = scope
	return newScope
}

// IsDisposed implements ServiceProvider.
func (scope *serviceProviderScope) IsDisposed() bool {
	return atomic.LoadInt32(&scope.disposed) != 0
}

// captureDisposable captures a service for disposal when the scope is disposed.
func (scope *serviceProviderScope) captureDisposable(service interface{}) {
	if service == scope {
		return
	}

	var disposable DisposableWithContext
	switch v := service.(type) {
	case Disposable:
		disposable = &contextDisposableWrapper{
			disposable: v,
		}
	case DisposableWithContext:
		disposable = v
	default:
		return
	}

	scope.disposablesMu.Lock()
	defer scope.disposablesMu.Unlock()
	scope.disposables = append(scope.disposables, disposable)
}

func (scope *serviceProviderScope) Close() error {
	if !atomic.CompareAndSwapInt32(&scope.disposed, 0, 1) {
		return nil
	}

	// Remove the scope from the provider's scopes map
	scope.serviceProvider.scopesMu.Lock()
	delete(scope.serviceProvider.scopes, scope.scopeID)
	scope.serviceProvider.scopesMu.Unlock()

	runtime.SetFinalizer(scope, nil)

	scope.disposablesMu.Lock()
	toDispose := make([]DisposableWithContext, len(scope.disposables))
	copy(toDispose, scope.disposables)
	scope.disposables = nil
	scope.disposablesMu.Unlock()

	var errs []error

	// Dispose in reverse order (LIFO)
	for i := len(toDispose) - 1; i >= 0; i-- {
		if err := toDispose[i].Close(scope.ctx); err != nil {
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
	scope.serviceProvider.scopesMu.Lock()
	defer scope.serviceProvider.scopesMu.Unlock()
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
	disposable Disposable
}

func (w *contextDisposableWrapper) Close(ctx context.Context) error {
	return w.disposable.Close()
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

func generateID() string {
	// Timestamp (8 bytes)
	timestamp := time.Now().UnixNano()

	// Random bytes (8 bytes)
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		panic(err)
	}

	// Combine timestamp and random
	return fmt.Sprintf("%016x%s", timestamp, hex.EncodeToString(randomBytes))
}
