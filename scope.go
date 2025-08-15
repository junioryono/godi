package godi

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/junioryono/godi/v3/internal/graph"
)

// Scope provides an isolated resolution context
type Scope interface {
	Provider

	Provider() Provider
	Context() context.Context
}

// scope provides an isolated resolution context
type scope struct {
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

// Provider returns the parent provider
func (s *scope) Provider() Provider {
	return s.provider
}

// Context returns the scope's context
func (s *scope) Context() context.Context {
	return s.context
}

// Get resolves a service in this scope
func (s *scope) Get(serviceType reflect.Type) (any, error) {
	if atomic.LoadInt32(&s.disposed) != 0 {
		return nil, ErrScopeDisposed
	}

	if serviceType == nil {
		return nil, ErrInvalidServiceType
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
		return nil, ErrInvalidServiceType
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
		return nil, ErrInvalidServiceType
	}

	if group == "" {
		return nil, fmt.Errorf("group name cannot be empty")
	}

	// Find all descriptors in the group
	descriptors := s.provider.findGroupDescriptors(serviceType, group)
	if len(descriptors) == 0 {
		return []any{}, nil
	}

	instances := make([]any, 0, len(descriptors))
	for _, descriptor := range descriptors {
		key := instanceKey{Type: descriptor.Type, Key: descriptor.Key}
		instance, err := s.resolve(key, descriptor)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve group member %v: %w", descriptor.Type, err)
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

	// Create child scope with cancellable context
	scopeCtx, cancel := context.WithCancel(ctx)

	child := &scope{
		provider:    s.provider,
		parent:      s,
		context:     scopeCtx,
		cancel:      cancel,
		instances:   make(map[instanceKey]any),
		disposables: make([]Disposable, 0),
		resolving:   make(map[instanceKey]struct{}),
		children:    make(map[*scope]struct{}),
	}

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
		<-scopeCtx.Done()
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
		return fmt.Errorf("scope disposal errors: %v", errs)
	}

	return nil
}

// getInstance retrieves a cached instance from this scope
func (s *scope) getInstance(key instanceKey) (any, bool) {
	s.instancesMu.RLock()
	defer s.instancesMu.RUnlock()
	instance, ok := s.instances[key]
	return instance, ok
}

// setInstance caches an instance in this scope
func (s *scope) setInstance(key instanceKey, instance any) {
	s.instancesMu.Lock()
	defer s.instancesMu.Unlock()
	s.instances[key] = instance

	// Track if disposable
	if d, ok := instance.(Disposable); ok {
		s.disposablesMu.Lock()
		s.disposables = append(s.disposables, d)
		s.disposablesMu.Unlock()
	}
}

// checkCircular checks for circular dependencies
func (s *scope) checkCircular(key instanceKey) error {
	s.resolvingMu.Lock()
	defer s.resolvingMu.Unlock()

	if _, ok := s.resolving[key]; ok {
		return &CircularDependencyError{
			Node: graph.NodeKey{
				Type: key.Type,
				Key:  key.Key,
			},
		}
	}
	return nil
}

// startResolving marks a service as being resolved
func (s *scope) startResolving(key instanceKey) {
	s.resolvingMu.Lock()
	defer s.resolvingMu.Unlock()
	s.resolving[key] = struct{}{}
}

// stopResolving marks a service as no longer being resolved
func (s *scope) stopResolving(key instanceKey) {
	s.resolvingMu.Lock()
	defer s.resolvingMu.Unlock()
	delete(s.resolving, key)
}

// resolve performs the actual service resolution
func (s *scope) resolve(key instanceKey, descriptor *Descriptor) (any, error) {
	// Check for circular dependency
	if err := s.checkCircular(key); err != nil {
		return nil, err
	}

	// Mark as resolving
	s.startResolving(key)
	defer s.stopResolving(key)

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
		if instance, ok := s.provider.getSingleton(key); ok {
			return instance, nil
		}
		// Singleton should have been created at build time
		return nil, fmt.Errorf("singleton %v not initialized at build time", key.Type)

	case Scoped:
		if instance, ok := s.getInstance(key); ok {
			return instance, nil
		}
		// Create and cache scoped instance
		instance, err := s.createInstance(descriptor)
		if err != nil {
			return nil, err
		}
		s.setInstance(key, instance)
		return instance, nil

	case Transient:
		// Always create new instance
		return s.createInstance(descriptor)

	default:
		return nil, fmt.Errorf("unknown lifetime: %v", descriptor.Lifetime)
	}
}
