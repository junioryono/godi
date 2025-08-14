package container

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junioryono/godi/v3/internal/lifetime"
	"github.com/junioryono/godi/v3/internal/reflection"
	"github.com/junioryono/godi/v3/internal/registry"
)

// Scope represents a service scope within the container.
type Scope struct {
	container    *Container
	managedScope *lifetime.ServiceScope
	id           string
	context      context.Context
	isRoot       bool

	// Resolution tracking
	resolving   map[resolutionKey]bool
	resolvingMu sync.Mutex

	// Statistics
	resolutionCount int64
	disposed        int32
}

// resolutionKey for tracking active resolutions.
type resolutionKey struct {
	Type reflect.Type
	Key  any
}

// ScopeProvider is the interface for scope operations.
type ScopeProvider interface {
	// CreateScope creates a child scope
	CreateScope(ctx context.Context) (*Scope, error)

	// Resolve resolves a service
	Resolve(serviceType reflect.Type) (any, error)

	// ResolveKeyed resolves a keyed service
	ResolveKeyed(serviceType reflect.Type, key any) (any, error)

	// ResolveGroup resolves all services in a group
	ResolveGroup(serviceType reflect.Type, group string) ([]any, error)

	// Invoke executes a function with dependency injection
	Invoke(function any) error

	// Dispose disposes the scope
	Dispose() error
}

// ID returns the scope ID.
func (s *Scope) ID() string {
	return s.id
}

// Context returns the scope's context.
func (s *Scope) Context() context.Context {
	return s.context
}

// IsRoot returns true if this is the root scope.
func (s *Scope) IsRoot() bool {
	return s.isRoot
}

// CreateScope creates a child scope.
func (s *Scope) CreateScope(ctx context.Context) (*Scope, error) {
	if s.isDisposed() {
		return nil, ErrScopeDisposed
	}

	if ctx == nil {
		ctx = s.context
	}

	// Create child managed scope
	childManaged, err := s.managedScope.CreateChild(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create child scope: %w", err)
	}

	child := &Scope{
		container:    s.container,
		managedScope: childManaged,
		id:           childManaged.ID,
		context:      ctx,
		isRoot:       false,
		resolving:    make(map[resolutionKey]bool),
	}

	return child, nil
}

// Resolve resolves a service in this scope.
func (s *Scope) Resolve(serviceType reflect.Type) (any, error) {
	if s.isDisposed() {
		return nil, ErrScopeDisposed
	}

	if serviceType == nil {
		return nil, ErrInvalidServiceType
	}

	// Track resolution for cycle detection
	key := resolutionKey{Type: serviceType}
	if err := s.trackResolution(key, true); err != nil {
		return nil, err
	}
	defer s.trackResolution(key, false)

	// Record start time
	startTime := time.Now()

	// Resolve through the resolver
	instance, err := s.container.resolver.Resolve(serviceType, s.id)

	// Update statistics
	duration := time.Since(startTime)
	s.updateStatistics(serviceType, err, duration)

	if err != nil {
		return nil, fmt.Errorf("failed to resolve %v: %w", serviceType, err)
	}

	// Track instance for disposal
	if err := s.trackInstance(instance, serviceType, nil); err != nil {
		// Log but don't fail resolution
		if s.container.options.OnServiceError != nil {
			s.container.options.OnServiceError(serviceType, err)
		}
	}

	return instance, nil
}

// ResolveKeyed resolves a keyed service in this scope.
func (s *Scope) ResolveKeyed(serviceType reflect.Type, serviceKey any) (any, error) {
	if s.isDisposed() {
		return nil, ErrScopeDisposed
	}

	if serviceType == nil {
		return nil, ErrInvalidServiceType
	}

	if serviceKey == nil {
		return nil, ErrNilServiceKey
	}

	// Track resolution
	key := resolutionKey{Type: serviceType, Key: serviceKey}
	if err := s.trackResolution(key, true); err != nil {
		return nil, err
	}
	defer s.trackResolution(key, false)

	// Record start time
	startTime := time.Now()

	// Resolve through the resolver
	instance, err := s.container.resolver.ResolveKeyed(serviceType, serviceKey, s.id)

	// Update statistics
	duration := time.Since(startTime)
	s.updateStatistics(serviceType, err, duration)

	if err != nil {
		return nil, fmt.Errorf("failed to resolve %v[%v]: %w", serviceType, serviceKey, err)
	}

	// Track instance for disposal
	if err := s.trackInstance(instance, serviceType, serviceKey); err != nil {
		// Log but don't fail resolution
		if s.container.options.OnServiceError != nil {
			s.container.options.OnServiceError(serviceType, err)
		}
	}

	return instance, nil
}

// ResolveGroup resolves all services in a group.
func (s *Scope) ResolveGroup(serviceType reflect.Type, group string) ([]any, error) {
	if s.isDisposed() {
		return nil, ErrScopeDisposed
	}

	if serviceType == nil {
		return nil, ErrInvalidServiceType
	}

	if group == "" {
		return nil, ErrEmptyGroup
	}

	// Record start time
	startTime := time.Now()

	// Resolve through the resolver
	instances, err := s.container.resolver.ResolveGroup(serviceType, group, s.id)

	// Update statistics
	duration := time.Since(startTime)
	s.updateStatistics(serviceType, err, duration)

	if err != nil {
		return nil, fmt.Errorf("failed to resolve group %q of %v: %w", group, serviceType, err)
	}

	// Track instances for disposal
	for _, instance := range instances {
		if err := s.trackInstance(instance, serviceType, fmt.Sprintf("group:%s", group)); err != nil {
			// Log but don't fail resolution
			if s.container.options.OnServiceError != nil {
				s.container.options.OnServiceError(serviceType, err)
			}
		}
	}

	return instances, nil
}

// Invoke executes a function with dependency injection.
func (s *Scope) Invoke(function any) error {
	if s.isDisposed() {
		return ErrScopeDisposed
	}

	if function == nil {
		return ErrNilFunction
	}

	// Analyze the function
	info, err := s.container.analyzer.Analyze(function)
	if err != nil {
		return fmt.Errorf("failed to analyze function: %w", err)
	}

	// Create a dependency resolver for this scope
	depResolver := &scopeDependencyResolver{scope: s}

	// Create invoker
	invoker := reflection.NewConstructorInvoker(s.container.analyzer)

	// Invoke the function
	results, err := invoker.Invoke(info, depResolver)
	if err != nil {
		return fmt.Errorf("failed to invoke function: %w", err)
	}

	// Check for error return
	if info.HasErrorReturn && len(results) > 0 {
		lastResult := results[len(results)-1]
		if !lastResult.IsNil() {
			if err, ok := lastResult.Interface().(error); ok {
				return fmt.Errorf("function returned error: %w", err)
			}
		}
	}

	return nil
}

// RegisterScoped registers a scoped service that only exists in this scope.
func (s *Scope) RegisterScoped(constructor any, opts ...ProvideOption) error {
	if s.isDisposed() {
		return ErrScopeDisposed
	}

	if s.isRoot {
		// Root scope delegates to container
		return s.container.RegisterScoped(constructor, opts...)
	}

	// For non-root scopes, this would register a scope-local service
	// This is an advanced feature that requires additional implementation
	return fmt.Errorf("scope-local registration not yet implemented")
}

// Dispose disposes the scope and all its resources.
func (s *Scope) Dispose() error {
	if !atomic.CompareAndSwapInt32(&s.disposed, 0, 1) {
		return nil
	}

	if s.isRoot {
		// Root scope is disposed with the container
		return fmt.Errorf("cannot dispose root scope directly")
	}

	// Dispose through managed scope
	return s.managedScope.Dispose()
}

// IsDisposed returns true if the scope is disposed.
func (s *Scope) IsDisposed() bool {
	return s.isDisposed()
}

// GetStatistics returns scope statistics.
func (s *Scope) GetStatistics() ScopeStatistics {
	return ScopeStatistics{
		ID:              s.id,
		ResolutionCount: atomic.LoadInt64(&s.resolutionCount),
		IsRoot:          s.isRoot,
		IsDisposed:      s.isDisposed(),
	}
}

// trackResolution tracks active resolutions for cycle detection.
func (s *Scope) trackResolution(key resolutionKey, start bool) error {
	s.resolvingMu.Lock()
	defer s.resolvingMu.Unlock()

	if start {
		if s.resolving[key] {
			return &CircularDependencyError{
				ServiceType: key.Type,
				Key:         key.Key,
			}
		}
		s.resolving[key] = true
	} else {
		delete(s.resolving, key)
	}

	return nil
}

// trackInstance tracks an instance for disposal.
func (s *Scope) trackInstance(instance any, serviceType reflect.Type, key any) error {
	if instance == nil {
		return nil
	}

	// Determine lifetime
	var lifetime registry.ServiceLifetime

	if key != nil {
		provider, err := s.container.registry.GetKeyedProvider(serviceType, key)
		if err == nil {
			lifetime = provider.Lifetime
		}
	} else {
		provider, err := s.container.registry.GetProvider(serviceType)
		if err == nil {
			lifetime = provider.Lifetime
		}
	}

	// Track with lifetime manager
	return s.container.lifetimeManager.Track(instance, serviceType, key, lifetime, s.id)
}

// updateStatistics updates resolution statistics.
func (s *Scope) updateStatistics(serviceType reflect.Type, err error, duration time.Duration) {
	atomic.AddInt64(&s.resolutionCount, 1)

	// Update container statistics
	if err == nil {
		atomic.AddInt64(&s.container.stats.ResolvedInstances, 1)

		// Add to total resolution time (simplified - should use atomic for duration)
		s.container.mu.Lock()
		s.container.stats.TotalResolutionTime += duration
		s.container.mu.Unlock()
	} else {
		atomic.AddInt64(&s.container.stats.FailedResolutions, 1)
	}

	// Call callback
	if err == nil && s.container.options.OnServiceResolved != nil {
		s.container.options.OnServiceResolved(serviceType, nil, duration)
	} else if err != nil && s.container.options.OnServiceError != nil {
		s.container.options.OnServiceError(serviceType, err)
	}
}

// isDisposed checks if the scope is disposed.
func (s *Scope) isDisposed() bool {
	return atomic.LoadInt32(&s.disposed) != 0
}

// ScopeStatistics contains scope metrics.
type ScopeStatistics struct {
	ID              string
	ResolutionCount int64
	IsRoot          bool
	IsDisposed      bool
}

// scopeDependencyResolver adapts Scope to DependencyResolver interface.
type scopeDependencyResolver struct {
	scope *Scope
}

func (r *scopeDependencyResolver) Resolve(t reflect.Type) (any, error) {
	return r.scope.Resolve(t)
}

func (r *scopeDependencyResolver) ResolveKeyed(t reflect.Type, key any) (any, error) {
	return r.scope.ResolveKeyed(t, key)
}

func (r *scopeDependencyResolver) ResolveGroup(t reflect.Type, group string) ([]any, error) {
	return r.scope.ResolveGroup(t, group)
}
