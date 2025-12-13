package godi

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junioryono/godi/v4/internal/graph"
	"github.com/junioryono/godi/v4/internal/reflection"
)

// Disposable interface for resources that need cleanup
type Disposable interface {
	Close() error
}

// Provider is the main dependency injection container interface
type Provider interface {
	Disposable

	// Returns the unique identifier for this provider instance.
	ID() string

	// Resolves a service of the specified type from the root scope.
	Get(serviceType reflect.Type) (any, error)

	// Resolves a keyed service of the specified type from the root scope.
	GetKeyed(serviceType reflect.Type, key any) (any, error)

	// Resolves all services of the specified type in a group from the root scope.
	GetGroup(serviceType reflect.Type, group string) ([]any, error)

	// Creates a new service scope for resolving services.
	CreateScope(ctx context.Context) (Scope, error)
}

type ProviderOptions struct {
	// BuildTimeout specifies the maximum time allowed for building the provider.
	// If building takes longer than this duration, it will timeout with an error.
	BuildTimeout time.Duration
}

// provider is the concrete implementation of Provider
type provider struct {
	id string

	// Service registry (immutable after build)
	services map[TypeKey]*Descriptor
	groups   map[GroupKey][]*Descriptor

	// Dependency graph (immutable after build)
	graph *graph.DependencyGraph

	// Reflection analyzer
	analyzer *reflection.Analyzer

	// Singleton instances (created at build time)
	// Using sync.Map for lock-free concurrent reads which are the common case
	singletons sync.Map // map[instanceKey]any

	// Track singleton keys for iteration during disposal
	singletonKeys   []instanceKey
	singletonKeysMu sync.Mutex

	voidReturnScopedDescriptors   []*Descriptor
	voidReturnScopedDescriptorsMu sync.RWMutex

	// Track disposable instances for cleanup
	disposables   []Disposable
	disposablesMu sync.Mutex

	// Root scope for provider-level resolution
	rootScope *scope

	// Active scopes for cleanup tracking
	scopes   map[*scope]struct{}
	scopesMu sync.Mutex

	// State
	disposed int32 // atomic
}

// instanceKey uniquely identifies a service instance
type instanceKey struct {
	Type  reflect.Type
	Key   any
	Group string
}

// ID returns the unique identifier for the provider.
// This ID is a UUID generated when the provider is created during the build process.
func (p *provider) ID() string {
	return p.id
}

// Get resolves a service from the root scope
func (p *provider) Get(serviceType reflect.Type) (any, error) {
	if atomic.LoadInt32(&p.disposed) != 0 {
		return nil, ErrProviderDisposed
	}

	if serviceType == nil {
		return nil, ErrServiceTypeNil
	}

	return p.rootScope.Get(serviceType)
}

// GetKeyed resolves a keyed service from the root scope
func (p *provider) GetKeyed(serviceType reflect.Type, key any) (any, error) {
	if atomic.LoadInt32(&p.disposed) != 0 {
		return nil, ErrProviderDisposed
	}

	if serviceType == nil {
		return nil, ErrServiceTypeNil
	}

	if key == nil {
		return nil, ErrServiceKeyNil
	}

	return p.rootScope.GetKeyed(serviceType, key)
}

// GetGroup resolves all services in a group from the root scope
func (p *provider) GetGroup(serviceType reflect.Type, group string) ([]any, error) {
	if atomic.LoadInt32(&p.disposed) != 0 {
		return nil, ErrProviderDisposed
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

	return p.rootScope.GetGroup(serviceType, group)
}

// CreateScope creates a new service scope
func (p *provider) CreateScope(ctx context.Context) (Scope, error) {
	if atomic.LoadInt32(&p.disposed) != 0 {
		return nil, ErrProviderDisposed
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// Create scope with cancellable context
	ctx, cancel := context.WithCancel(ctx)
	s, err := newScope(p, nil, ctx, cancel)
	if err != nil {
		return nil, err
	}

	// Track scope
	p.scopesMu.Lock()
	p.scopes[s] = struct{}{}
	p.scopesMu.Unlock()

	// Auto-close on context cancellation
	go func() {
		<-ctx.Done()
		if err := s.Close(); err != nil {
			// Context cancellation cleanup errors are expected during shutdown
			// and cannot be meaningfully handled, so we ignore them
			_ = err
		}
	}()

	return s, nil
}

// Close disposes the provider and all its resources
func (p *provider) Close() error {
	if !atomic.CompareAndSwapInt32(&p.disposed, 0, 1) {
		return nil // Already disposed
	}

	var errors []error

	// Close all scopes
	p.scopesMu.Lock()
	scopes := make([]*scope, 0, len(p.scopes))
	for s := range p.scopes {
		scopes = append(scopes, s)
	}
	p.scopes = nil
	p.scopesMu.Unlock()

	for _, s := range scopes {
		if s != nil {
			if err := s.Close(); err != nil {
				errors = append(errors, fmt.Errorf("scope %s: %w", s.ID(), err))
			}
		}
	}

	// Close root scope
	if p.rootScope != nil {
		if err := p.rootScope.Close(); err != nil {
			errors = append(errors, fmt.Errorf("root scope: %w", err))
		}

		p.rootScope = nil
	}

	// Dispose all singleton disposables
	p.disposablesMu.Lock()
	disposables := p.disposables
	p.disposables = nil
	p.disposablesMu.Unlock()

	// Dispose in reverse order of creation
	for i := len(disposables) - 1; i >= 0; i-- {
		if disposables[i] != nil {
			if err := disposables[i].Close(); err != nil {
				errors = append(errors, fmt.Errorf("singleton disposable %d: %w", i, err))
			}
		}
	}

	// Clear all internal state - clear singletons from sync.Map
	p.singletonKeysMu.Lock()
	for _, key := range p.singletonKeys {
		p.singletons.Delete(key)
	}
	p.singletonKeys = nil
	p.singletonKeysMu.Unlock()

	p.voidReturnScopedDescriptorsMu.Lock()
	p.voidReturnScopedDescriptors = nil
	p.voidReturnScopedDescriptorsMu.Unlock()

	if len(errors) > 0 {
		return &DisposalError{
			Context: "provider",
			Errors:  errors,
		}
	}

	return nil
}

// getSingleton retrieves a singleton instance using lock-free sync.Map.
// Returns the instance and true if found, or nil and false if not found.
func (p *provider) getSingleton(key instanceKey) (any, bool) {
	return p.singletons.Load(key)
}

// setSingleton stores a singleton instance using lock-free sync.Map.
// It also tracks the instance if it implements the Disposable interface
// for proper cleanup during provider disposal.
func (p *provider) setSingleton(key instanceKey, instance any) {
	if instance == nil {
		return
	}

	p.singletons.Store(key, instance)

	// Track key for iteration during disposal
	p.singletonKeysMu.Lock()
	p.singletonKeys = append(p.singletonKeys, key)
	p.singletonKeysMu.Unlock()

	// Track if disposable
	if d, ok := instance.(Disposable); ok {
		p.disposablesMu.Lock()
		p.disposables = append(p.disposables, d)
		p.disposablesMu.Unlock()
	}
}

// findDescriptor finds a descriptor for the given service type and optional key.
// Returns nil if no matching descriptor is found in the service registry.
func (p *provider) findDescriptor(serviceType reflect.Type, key any) *Descriptor {
	if serviceType == nil {
		return nil
	}

	typeKey := TypeKey{Type: serviceType, Key: key}
	return p.services[typeKey]
}

// findGroupDescriptors finds all descriptors for a specific type within a group.
// Returns an empty slice if the type is nil, group is empty, or no services are found.
func (p *provider) findGroupDescriptors(serviceType reflect.Type, group string) []*Descriptor {
	if serviceType == nil || group == "" {
		return nil
	}

	groupKey := GroupKey{Type: serviceType, Group: group}
	return p.groups[groupKey]
}

// createAllSingletonsWithContext creates all singleton instances with context cancellation support.
// The context is checked before each singleton creation, allowing for graceful cancellation
// during the build process.
func (p *provider) createAllSingletonsWithContext(ctx context.Context) error {
	// Get topological sort from dependency graph
	sorted, err := p.graph.TopologicalSort()
	if err != nil {
		return &GraphOperationError{
			Operation: "topological sort",
			NodeType:  nil,
			NodeKey:   nil,
			Cause:     err,
		}
	}

	// Create instances in dependency order
	for _, node := range sorted {
		// Check context before each singleton creation
		select {
		case <-ctx.Done():
			return &BuildError{
				Phase:   "singleton-creation",
				Details: "build cancelled during singleton creation",
				Cause:   ctx.Err(),
			}
		default:
		}

		if node == nil || node.Provider == nil {
			continue
		}

		descriptor, ok := node.Provider.(*Descriptor)
		if !ok {
			return &ValidationError{
				ServiceType: nil,
				Cause:       fmt.Errorf("invalid provider type: %T", node.Provider),
			}
		}

		if descriptor.Lifetime != Singleton {
			continue
		}

		// Create instance key
		key := instanceKey{
			Type:  descriptor.Type,
			Key:   descriptor.Key,
			Group: descriptor.Group,
		}

		// Check if already created
		if _, exists := p.getSingleton(key); exists {
			continue
		}

		_, err := p.rootScope.createInstance(descriptor)
		if err != nil {
			return &ResolutionError{
				ServiceType: descriptor.Type,
				ServiceKey:  descriptor.Key,
				Cause:       err,
			}
		}
	}

	return nil
}

// extractParameterTypes extracts parameter types from constructor info.
// Returns a slice of reflect.Type representing each parameter's type,
// or nil if the info is nil.
func extractParameterTypes(info *reflection.ConstructorInfo) []reflect.Type {
	if info == nil {
		return nil
	}

	types := make([]reflect.Type, len(info.Parameters))
	for i, param := range info.Parameters {
		types[i] = param.Type
	}

	return types
}

// Resolve resolves a service of type T from the provider.
// This is a generic convenience function that handles type assertions.
//
// Example:
//
//	logger, err := godi.Resolve[*Logger](provider)
//	if err != nil {
//	    // Handle error
//	}
func Resolve[T any](provider Provider) (T, error) {
	var zero T

	if provider == nil {
		return zero, ErrProviderNil
	}

	serviceType := reflect.TypeOf((*T)(nil)).Elem()
	service, err := provider.Get(serviceType)
	if err != nil {
		return zero, err
	}

	result, ok := service.(T)
	if !ok {
		return zero, &TypeMismatchError{
			Expected: serviceType,
			Actual:   reflect.TypeOf(service),
			Context:  "type assertion",
		}
	}

	return result, nil
}

// MustResolve resolves a service of type T from the provider.
// It panics if the service cannot be resolved. This is useful for
// application initialization where missing services are fatal.
//
// Example:
//
//	// Panics if logger cannot be resolved
//	logger := godi.MustResolve[*Logger](provider)
func MustResolve[T any](provider Provider) T {
	service, err := Resolve[T](provider)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve service: %v", err))
	}

	return service
}

// ResolveKeyed resolves a keyed service of type T from the provider.
//
// Example:
//
//	cache, err := godi.ResolveKeyed[Cache](provider, "redis")
func ResolveKeyed[T any](provider Provider, key any) (T, error) {
	var zero T

	if provider == nil {
		return zero, ErrProviderNil
	}

	if key == nil {
		return zero, ErrServiceKeyNil
	}

	serviceType := reflect.TypeOf((*T)(nil)).Elem()
	service, err := provider.GetKeyed(serviceType, key)
	if err != nil {
		return zero, err
	}

	result, ok := service.(T)
	if !ok {
		return zero, &TypeMismatchError{
			Expected: serviceType,
			Actual:   reflect.TypeOf(service),
			Context:  "type assertion for keyed service",
		}
	}

	return result, nil
}

// MustResolveKeyed resolves a keyed service of type T from the provider.
// It panics if the service cannot be resolved.
//
// Example:
//
//	// Panics if redis cache cannot be resolved
//	cache := godi.MustResolveKeyed[Cache](provider, "redis")
func MustResolveKeyed[T any](provider Provider, key any) T {
	service, err := ResolveKeyed[T](provider, key)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve keyed service %v: %v", key, err))
	}

	return service
}

// ResolveGroup resolves all services of type T in the specified group.
//
// Example:
//
//	handlers, err := godi.ResolveGroup[http.Handler](provider, "routes")
func ResolveGroup[T any](provider Provider, group string) ([]T, error) {
	if provider == nil {
		return nil, ErrProviderNil
	}

	if group == "" {
		return nil, &ValidationError{
			ServiceType: nil,
			Cause:       ErrGroupNameEmpty,
		}
	}

	serviceType := reflect.TypeOf((*T)(nil)).Elem()
	services, err := provider.GetGroup(serviceType, group)
	if err != nil {
		return nil, err
	}

	results := make([]T, 0, len(services))
	for i, service := range services {
		result, ok := service.(T)
		if !ok {
			return nil, &TypeMismatchError{
				Expected: serviceType,
				Actual:   reflect.TypeOf(service),
				Context:  fmt.Sprintf("type assertion for group item %d", i),
			}
		}

		results = append(results, result)
	}

	return results, nil
}

// MustResolveGroup resolves all services of type T in the specified group.
// It panics if the services cannot be resolved.
//
// Example:
//
//	// Panics if handlers cannot be resolved
//	handlers := godi.MustResolveGroup[http.Handler](provider, "routes")
func MustResolveGroup[T any](provider Provider, group string) []T {
	services, err := ResolveGroup[T](provider, group)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve group %s: %v", group, err))
	}

	return services
}
