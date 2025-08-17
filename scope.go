package godi

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/google/uuid"
	"github.com/junioryono/godi/v4/internal/graph"
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
	id       string
	provider *provider
	parent   *scope
	context  context.Context
	cancel   context.CancelFunc

	// Scoped instances (isolated per scope)
	instances   map[instanceKey]any
	instancesMu sync.RWMutex

	// Track disposable scoped instances
	disposables   []Disposable
	disposablesMu sync.Mutex

	// Resolution tracking for circular dependency detection
	resolving   map[instanceKey]struct{}
	resolvingMu sync.Mutex

	// Child scopes for hierarchical cleanup
	children   map[*scope]struct{}
	childrenMu sync.Mutex

	// State
	disposed int32 // atomic
}

// Provider returns the parent provider that created this scope.
// The provider contains the service registry and dependency graph.
func (s *scope) Provider() Provider {
	return s.provider
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
	descriptors := s.provider.findGroupDescriptors(serviceType, group)
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

	child := &scope{
		id:          uuid.NewString(),
		provider:    s.provider,
		parent:      s,
		context:     ctx,
		cancel:      cancel,
		instances:   make(map[instanceKey]any),
		disposables: make([]Disposable, 0),
		resolving:   make(map[instanceKey]struct{}),
		children:    make(map[*scope]struct{}),
	}
	ctx = context.WithValue(ctx, scopeContextKey{}, child)

	// Track child
	s.childrenMu.Lock()
	s.children[child] = struct{}{}
	s.childrenMu.Unlock()

	// Track in provider
	s.provider.scopesMu.Lock()
	s.provider.scopes[child] = struct{}{}
	s.provider.scopesMu.Unlock()

	// Auto-close on context cancellation
	go func() {
		<-ctx.Done()
		child.Close()
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
	if s.parent != nil {
		s.parent.childrenMu.Lock()
		delete(s.parent.children, s)
		s.parent.childrenMu.Unlock()
	}

	// Remove from provider's tracking
	if s.provider != nil {
		s.provider.scopesMu.Lock()
		delete(s.provider.scopes, s)
		s.provider.scopesMu.Unlock()
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
func (s *scope) setInstance(key instanceKey, instance any) {
	s.instancesMu.Lock()
	s.instances[key] = instance
	s.instancesMu.Unlock()

	// Track if disposable
	if d, ok := instance.(Disposable); ok {
		s.disposablesMu.Lock()
		s.disposables = append(s.disposables, d)
		s.disposablesMu.Unlock()
	}
}

// checkCircular checks for circular dependencies during resolution.
// Returns an error if the service is already being resolved in the current
// resolution chain, indicating a circular dependency.
func (s *scope) checkCircular(key instanceKey) error {
	s.resolvingMu.Lock()
	_, ok := s.resolving[key]
	s.resolvingMu.Unlock()

	if ok {
		return &CircularDependencyError{
			Node: graph.NodeKey{
				Type: key.Type,
				Key:  key.Key,
			},
		}
	}

	return nil
}

// startResolving marks a service as being resolved to track circular dependencies.
// This should be called before attempting to create a service instance.
func (s *scope) startResolving(key instanceKey) {
	s.resolvingMu.Lock()
	s.resolving[key] = struct{}{}
	s.resolvingMu.Unlock()
}

// stopResolving marks a service as no longer being resolved.
// This should be called after a service instance has been created or resolution fails.
func (s *scope) stopResolving(key instanceKey) {
	s.resolvingMu.Lock()
	delete(s.resolving, key)
	s.resolvingMu.Unlock()
}

// resolve performs the actual service resolution using the appropriate lifetime strategy.
// It handles singleton caching, scoped caching, and transient creation, while also
// detecting circular dependencies during resolution.
func (s *scope) resolve(key instanceKey, descriptor *Descriptor) (any, error) {
	// Find descriptor if not provided
	if descriptor == nil {
		descriptor = s.provider.findDescriptor(key.Type, key.Key)
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
		if instance, ok := s.provider.getSingleton(key); ok {
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

		// Check for circular dependency before creating
		if err := s.checkCircular(key); err != nil {
			return nil, err
		}

		// Mark as resolving
		s.startResolving(key)
		defer s.stopResolving(key)

		// Create and cache scoped instance
		instance, err := s.createInstance(descriptor)
		if err != nil {
			return nil, err
		}

		s.setInstance(key, instance)
		return instance, nil

	case Transient:
		// Check for circular dependency before creating
		if err := s.checkCircular(key); err != nil {
			return nil, err
		}

		// Mark as resolving
		s.startResolving(key)
		defer s.stopResolving(key)

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
		return descriptor.Instance, nil
	}

	// Analyze constructor
	info, err := s.provider.analyzer.Analyze(descriptor.Constructor.Interface())
	if err != nil {
		return nil, &ReflectionAnalysisError{
			Constructor: descriptor.Constructor.Interface(),
			Operation:   "analyze",
			Cause:       err,
		}
	}

	// Create invoker
	invoker := reflection.NewConstructorInvoker(s.provider.analyzer)

	// Invoke constructor
	results, err := invoker.Invoke(info, s)
	if err != nil {
		return nil, &ConstructorInvocationError{
			Constructor: descriptor.ConstructorType,
			Parameters:  extractParameterTypes(info),
			Cause:       err,
		}
	}

	// Handle result objects (Out structs)
	if info.IsResultObject && len(results) > 0 {
		processor := reflection.NewResultObjectProcessor(s.provider.analyzer)
		registrations, err := processor.ProcessResultObject(results[0], info.Type.Out(0))
		if err != nil {
			return nil, &ReflectionAnalysisError{
				Constructor: descriptor.Constructor.Interface(),
				Operation:   "process result object",
				Cause:       err,
			}
		}

		// Find the primary service to return
		for _, reg := range registrations {
			if reg.Type == descriptor.Type && reg.Key == descriptor.Key {
				return reg.Value, nil
			}
		}

		// If no exact match, return the first one
		if len(registrations) > 0 {
			return registrations[0].Value, nil
		}

		return nil, &ValidationError{
			ServiceType: descriptor.Type,
			Cause:       fmt.Errorf("result object produced no services"),
		}
	}

	// Handle multi-return constructors
	if descriptor.IsMultiReturn && descriptor.ReturnIndex >= 0 {
		// Get the specific return value
		if descriptor.ReturnIndex >= len(results) {
			return nil, &ConstructorInvocationError{
				Constructor: descriptor.ConstructorType,
				Parameters:  nil,
				Cause:       fmt.Errorf("invalid return index %d for constructor with %d returns", descriptor.ReturnIndex, len(results)),
			}
		}

		return results[descriptor.ReturnIndex].Interface(), nil
	}

	// Regular constructor - get first result
	if len(results) == 0 {
		return nil, &ConstructorInvocationError{
			Constructor: descriptor.ConstructorType,
			Parameters:  nil,
			Cause:       fmt.Errorf("constructor returned no values"),
		}
	}

	return results[0].Interface(), nil
}

// FromContext retrieves a Scope from the context.
// This is useful in HTTP handlers or other context-aware code.
//
// Example:
//
//	func handler(ctx context.Context) {
//	    if scope, ok := godi.FromContext(ctx); ok {
//	        service, _ := godi.Resolve[*Service](scope)
//	    }
//	}
func FromContext(ctx context.Context) (Scope, bool) {
	if ctx == nil {
		return nil, false
	}

	scope, ok := ctx.Value(scopeContextKey{}).(Scope)
	return scope, ok
}

// scopeContextKey is the key used to store scopes in contexts
type scopeContextKey struct{}
