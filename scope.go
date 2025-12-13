package godi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/junioryono/godi/v4/internal/reflection"
)

// Scope provides an isolated resolution context
type Scope interface {
	Provider

	Provider() Provider
	Context() context.Context
}

// scope provides an isolated resolution context
type scope struct {
	id           string
	rootProvider *provider
	parentScope  *scope
	context      context.Context
	cancel       context.CancelFunc

	// Scoped instances (isolated per scope)
	instances   map[instanceKey]any
	instancesMu sync.RWMutex

	// Track disposable scoped instances
	disposables   []Disposable
	disposablesMu sync.Mutex

	// Child scopes for hierarchical cleanup
	children   map[*scope]struct{}
	childrenMu sync.Mutex

	// State
	disposed int32 // atomic
}

func newScope(rootProvider *provider, parent *scope, ctx context.Context, cancel context.CancelFunc) (*scope, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Generate scope ID using provider's counter (scoped to this provider)
	scopeNum := atomic.AddUint64(&rootProvider.scopeCounter, 1)

	s := &scope{
		id:           "s" + strconv.FormatUint(scopeNum, 36),
		rootProvider: rootProvider,
		parentScope:  parent,
		cancel:       cancel,
		instances:    make(map[instanceKey]any, 8), // Pre-size for typical usage
		disposables:  make([]Disposable, 0, 4),
		children:     make(map[*scope]struct{}, 2),
	}

	ctx = context.WithValue(ctx, scopeContextKey{}, s)
	s.context = ctx

	// Initialize scoped services with no returns (initialization functions)
	// These need to be called when the scope is created
	for _, descriptor := range rootProvider.voidReturnScopedDescriptors {
		if _, err := s.createInstance(descriptor); err != nil {
			return nil, &ResolutionError{
				ServiceType: descriptor.Type,
				ServiceKey:  descriptor.Key,
				Cause:       fmt.Errorf("failed to initialize scoped service: %w", err),
			}

		}
	}

	return s, nil
}

// Provider returns the parent provider that created this scope.
// The provider contains the service registry and dependency graph.
func (s *scope) Provider() Provider {
	return s.rootProvider
}

// Context returns the context associated with this scope.
// The context is used for cancellation and can carry request-scoped values.
func (s *scope) Context() context.Context {
	return s.context
}

// ID returns the unique identifier for this scope.
// This ID is a UUID generated when the scope is created.
func (s *scope) ID() string {
	return s.id
}

// Get resolves a service in this scope
func (s *scope) Get(serviceType reflect.Type) (any, error) {
	if atomic.LoadInt32(&s.disposed) != 0 {
		return nil, ErrScopeDisposed
	}

	if serviceType == nil {
		return nil, ErrServiceTypeNil
	}

	key := instanceKey{Type: serviceType}
	return s.resolve(key, nil)
}

// GetKeyed resolves a keyed service in this scope
func (s *scope) GetKeyed(serviceType reflect.Type, serviceKey any) (any, error) {
	if atomic.LoadInt32(&s.disposed) != 0 {
		return nil, ErrScopeDisposed
	}

	if serviceType == nil {
		return nil, ErrServiceTypeNil
	}

	if serviceKey == nil {
		return nil, ErrServiceKeyNil
	}

	key := instanceKey{Type: serviceType, Key: serviceKey}
	return s.resolve(key, nil)
}

// GetGroup resolves all services in a group
func (s *scope) GetGroup(serviceType reflect.Type, group string) ([]any, error) {
	if atomic.LoadInt32(&s.disposed) != 0 {
		return nil, ErrScopeDisposed
	}

	if serviceType == nil {
		return nil, ErrServiceTypeNil
	}

	if group == "" {
		return nil, &ValidationError{
			ServiceType: serviceType,
			Cause:       ErrGroupNameEmpty,
		}
	}

	// Find all descriptors in the group
	descriptors := s.rootProvider.findGroupDescriptors(serviceType, group)
	if len(descriptors) == 0 {
		return []any{}, nil
	}

	instances := make([]any, 0, len(descriptors))
	for _, descriptor := range descriptors {
		key := instanceKey{Type: descriptor.Type, Key: descriptor.Key, Group: descriptor.Group}
		instance, err := s.resolve(key, descriptor)
		if err != nil {
			return nil, &ResolutionError{
				ServiceType: descriptor.Type,
				ServiceKey:  descriptor.Key,
				Cause:       fmt.Errorf("failed to resolve group member: %w", err),
			}
		}

		instances = append(instances, instance)
	}

	return instances, nil
}

// CreateScope creates a child scope
func (s *scope) CreateScope(ctx context.Context) (Scope, error) {
	if atomic.LoadInt32(&s.disposed) != 0 {
		return nil, ErrScopeDisposed
	}

	if ctx == nil {
		ctx = s.context
	}

	ctx, cancel := context.WithCancel(ctx)
	child, err := newScope(s.rootProvider, s, ctx, cancel)
	if err != nil {
		return nil, fmt.Errorf("failed to create child scope: %w", err)
	}

	// Track child
	s.childrenMu.Lock()
	s.children[child] = struct{}{}
	s.childrenMu.Unlock()

	// Track in provider
	s.rootProvider.scopesMu.Lock()
	s.rootProvider.scopes[child] = struct{}{}
	s.rootProvider.scopesMu.Unlock()

	// Auto-close on context cancellation
	go func() {
		<-ctx.Done()
		if err := child.Close(); err != nil {
			// Context cancellation cleanup errors are expected during shutdown
			// and cannot be meaningfully handled, so we ignore them
			_ = err
		}
	}()

	return child, nil
}

// Close disposes the scope and all its resources
func (s *scope) Close() error {
	if !atomic.CompareAndSwapInt32(&s.disposed, 0, 1) {
		return nil // Already closed
	}

	var errs []error

	// Cancel context
	if s.cancel != nil {
		s.cancel()
	}

	// Close all children first
	s.childrenMu.Lock()
	children := make([]*scope, 0, len(s.children))
	for child := range s.children {
		children = append(children, child)
	}
	s.children = nil
	s.childrenMu.Unlock()

	for _, child := range children {
		if err := child.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close child scope: %w", err))
		}
	}

	// Dispose all disposable scoped instances in reverse order
	s.disposablesMu.Lock()
	disposables := s.disposables
	s.disposables = nil
	s.disposablesMu.Unlock()

	for i := len(disposables) - 1; i >= 0; i-- {
		if err := disposables[i].Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to dispose scoped instance: %w", err))
		}
	}

	// Remove from parent's children
	if s.parentScope != nil {
		s.parentScope.childrenMu.Lock()
		delete(s.parentScope.children, s)
		s.parentScope.childrenMu.Unlock()
	}

	// Remove from provider's tracking
	if s.rootProvider != nil {
		s.rootProvider.scopesMu.Lock()
		delete(s.rootProvider.scopes, s)
		s.rootProvider.scopesMu.Unlock()
	}

	// Clear instances
	s.instancesMu.Lock()
	s.instances = nil
	s.instancesMu.Unlock()

	if len(errs) > 0 {
		return &DisposalError{
			Context: "scope",
			Errors:  errs,
		}
	}

	return nil
}

// getInstance retrieves a cached instance from this scope in a thread-safe manner.
// Returns the instance and true if found, or nil and false if not cached.
func (s *scope) getInstance(key instanceKey) (any, bool) {
	s.instancesMu.RLock()
	instance, ok := s.instances[key]
	s.instancesMu.RUnlock()
	return instance, ok
}

// setInstance caches an instance in this scope in a thread-safe manner.
// It also tracks the instance if it implements the Disposable interface
// for proper cleanup when the scope is closed.
func (s *scope) setInstance(descriptor *Descriptor, key instanceKey, instance any) {
	switch descriptor.Lifetime {
	case Singleton:
		s.rootProvider.setSingleton(key, instance)
	case Scoped:
		s.instancesMu.Lock()
		s.instances[key] = instance
		s.instancesMu.Unlock()
		fallthrough
	case Transient:
		if d, ok := instance.(Disposable); ok {
			s.disposablesMu.Lock()
			s.disposables = append(s.disposables, d)
			s.disposablesMu.Unlock()
		}
	}
}

var (
	contextType  = reflect.TypeOf((*context.Context)(nil)).Elem()
	providerType = reflect.TypeOf((*Provider)(nil)).Elem()
	scopeType    = reflect.TypeOf((*Scope)(nil)).Elem()
)

// resolve performs the actual service resolution using the appropriate lifetime strategy.
// It handles singleton caching, scoped caching, and transient creation, while also
// detecting circular dependencies during resolution.
func (s *scope) resolve(key instanceKey, descriptor *Descriptor) (any, error) {
	// Find descriptor if not provided
	if descriptor == nil {
		if key.Key == nil && key.Group == "" {
			switch key.Type {
			case contextType:
				return s.context, nil
			case providerType:
				return s.rootProvider, nil
			case scopeType:
				return s, nil
			}
		}

		descriptor = s.rootProvider.findDescriptor(key.Type, key.Key)
		if descriptor == nil {
			return nil, &ResolutionError{
				ServiceType: key.Type,
				ServiceKey:  key.Key,
				Cause:       ErrServiceNotFound,
			}
		}
	}

	// Check cache based on lifetime
	switch descriptor.Lifetime {
	case Singleton:
		// Singletons are created at build time, no circular check needed
		if instance, ok := s.rootProvider.getSingleton(key); ok {
			return instance, nil
		}

		// Singleton should have been created at build time
		return nil, &ResolutionError{
			ServiceType: key.Type,
			ServiceKey:  key.Key,
			Cause:       ErrSingletonNotInitialized,
		}

	case Scoped:
		// Check for circular dependency only when creating new instance
		if instance, ok := s.getInstance(key); ok {
			return instance, nil
		}

		// Create and cache scoped instance
		instance, err := s.createInstance(descriptor)
		if err != nil {
			return nil, err
		}

		return instance, nil

	case Transient:
		// Always create new instance
		return s.createInstance(descriptor)

	default:
		return nil, &LifetimeError{
			Value: descriptor.Lifetime,
		}
	}
}

// createInstance creates a new instance of a service using its constructor.
// It handles regular constructors, result objects (Out structs), multi-return
// constructors, and instance descriptors.
func (s *scope) createInstance(descriptor *Descriptor) (any, error) {
	if descriptor == nil {
		return nil, &ValidationError{
			ServiceType: nil,
			Cause:       ErrDescriptorNil,
		}
	}

	if descriptor.IsInstance {
		instance := descriptor.Instance
		if instance == nil {
			return nil, &ValidationError{
				ServiceType: descriptor.Type,
				Cause:       fmt.Errorf("instance descriptor has nil instance"),
			}
		}

		key := instanceKey{
			Type:  descriptor.Type,
			Key:   descriptor.Key,
			Group: descriptor.Group,
		}

		s.setInstance(descriptor, key, instance)
		return instance, nil
	}

	// Analyze constructor
	info, err := s.rootProvider.analyzer.Analyze(descriptor.Constructor.Interface())
	if err != nil {
		return nil, &ReflectionAnalysisError{
			Constructor: descriptor.Constructor.Interface(),
			Operation:   "analyze",
			Cause:       err,
		}
	}

	// Get cached invoker (reduces allocations)
	invoker := s.rootProvider.analyzer.GetInvoker()

	// Invoke constructor
	results, err := invoker.Invoke(info, s)
	if err != nil {
		// Check if it's a panic error and wrap appropriately
		var panicErr *reflection.PanicError
		if errors.As(err, &panicErr) {
			return nil, &ConstructorPanicError{
				Constructor: descriptor.ConstructorType,
				Panic:       panicErr.Panic,
				Stack:       panicErr.Stack,
			}
		}

		return nil, &ConstructorInvocationError{
			Constructor: descriptor.ConstructorType,
			Parameters:  extractParameterTypes(info),
			Cause:       err,
		}
	}

	if descriptor.VoidReturn {
		emptyStruct := struct{}{}
		key := instanceKey{
			Type:  descriptor.Type,
			Key:   descriptor.Key,
			Group: descriptor.Group,
		}
		s.setInstance(descriptor, key, emptyStruct)
		return emptyStruct, nil
	}

	if len(results) == 0 {
		return nil, &ConstructorInvocationError{
			Constructor: descriptor.ConstructorType,
			Parameters:  nil,
			Cause:       fmt.Errorf("constructor returned no values"),
		}
	}

	// Handle result objects (Out structs)
	if info.IsResultObject {
		processor := reflection.NewResultObjectProcessor(s.rootProvider.analyzer)
		registrations, err := processor.ProcessResultObject(results[0], info.Type.Out(0))
		if err != nil {
			return nil, &ReflectionAnalysisError{
				Constructor: descriptor.Constructor.Interface(),
				Operation:   "process result object",
				Cause:       err,
			}
		}

		// Find the primary service to return
		var primaryService any
		for _, reg := range registrations {
			value := reg.Value

			// Convert empty string key to nil for consistent lookup
			var regKey any
			if reg.Key != "" {
				regKey = reg.Key
			}

			if reg.Type == descriptor.Type && regKey == descriptor.Key {
				primaryService = value
			}

			regDescriptor := s.rootProvider.findDescriptor(reg.Type, regKey)
			if regDescriptor == nil {
				return nil, &ResolutionError{
					ServiceType: reg.Type,
					ServiceKey:  regKey,
					Cause:       fmt.Errorf("no descriptor found for return type %v", reg.Type),
				}
			}

			key := instanceKey{
				Type:  reg.Type,
				Key:   regKey,
				Group: reg.Group,
			}

			s.setInstance(regDescriptor, key, value)
		}

		if primaryService == nil {
			return nil, &ValidationError{
				ServiceType: descriptor.Type,
				Cause:       fmt.Errorf("result object produced no services"),
			}
		}

		return primaryService, nil
	}

	// Handle multi-return constructors
	if descriptor.MultiReturnIndex >= 0 {
		for _, ret := range info.Returns {
			if ret.IsError {
				continue
			}

			value := results[ret.Index].Interface()

			// Find the descriptor for this return type
			serviceDescriptor := s.rootProvider.findDescriptor(ret.Type, nil)
			if serviceDescriptor == nil {
				return nil, &ResolutionError{
					ServiceType: ret.Type,
					ServiceKey:  nil,
					Cause:       fmt.Errorf("no descriptor found for return type %v", ret.Type),
				}
			}

			key := instanceKey{
				Type:  ret.Type,
				Key:   serviceDescriptor.Key,
				Group: serviceDescriptor.Group,
			}

			s.setInstance(serviceDescriptor, key, value)
		}

		return results[descriptor.MultiReturnIndex].Interface(), nil
	}

	instance := results[0].Interface()
	if instance == nil {
		return nil, &ValidationError{
			ServiceType: descriptor.Type,
			Cause:       fmt.Errorf("constructor returned nil instance"),
		}
	}

	key := instanceKey{
		Type:  descriptor.Type,
		Key:   descriptor.Key,
		Group: descriptor.Group,
	}

	s.setInstance(descriptor, key, instance)
	return instance, nil
}

// FromContext retrieves a Scope from the context.
// This is useful in HTTP handlers or other context-aware code.
//
// Example:
//
//	func UserHandler(ctx context.Context) {
//	    scope, err := godi.FromContext(ctx)
//	    if err != nil {
//	        // Handle error - no scope found or context was nil
//	        return
//	    }
//
//	    // Use the scope to resolve services
//	    service, _ := godi.Resolve[*Service](scope)
//	}
func FromContext(ctx context.Context) (Scope, error) {
	if ctx == nil {
		return nil, &ValidationError{
			ServiceType: nil,
			Cause:       errors.New("context cannot be nil"),
		}
	}

	scope, ok := ctx.Value(scopeContextKey{}).(Scope)
	if !ok {
		return nil, &ResolutionError{
			ServiceType: scopeType,
			ServiceKey:  nil,
			Cause:       errors.New("no scope found in context"),
		}
	}

	return scope, nil
}

// scopeContextKey is the key used to store scopes in contexts
type scopeContextKey struct{}
