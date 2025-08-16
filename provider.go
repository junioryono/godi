package godi

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
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
	services   map[TypeKey]*Descriptor
	groups     map[GroupKey][]*Descriptor
	decorators map[reflect.Type][]*Descriptor

	// Dependency graph (immutable after build)
	graph *graph.DependencyGraph

	// Reflection analyzer
	analyzer *reflection.Analyzer

	// Singleton instances (created at build time)
	singletons   map[instanceKey]any
	singletonsMu sync.RWMutex

	// Track disposable instances for cleanup
	disposables   []Disposable
	disposablesMu sync.Mutex

	// Root scope for provider-level resolution
	rootScope *scope

	// Active scopes for cleanup tracking
	scopes   map[*scope]struct{}
	scopesMu sync.Mutex

	// State
	built    bool
	disposed int32 // atomic
}

// instanceKey uniquely identifies a service instance
type instanceKey struct {
	Type  reflect.Type
	Key   any
	Group string
}

// ID returns the unique identifier for the provider
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
	scopeCtx, cancel := context.WithCancel(ctx)

	s := &scope{
		id:          uuid.NewString(),
		provider:    p,
		parent:      nil,
		context:     scopeCtx,
		cancel:      cancel,
		instances:   make(map[instanceKey]any),
		disposables: make([]Disposable, 0),
		resolving:   make(map[instanceKey]struct{}),
		children:    make(map[*scope]struct{}),
	}

	// Track scope
	p.scopesMu.Lock()
	p.scopes[s] = struct{}{}
	p.scopesMu.Unlock()

	// Auto-close on context cancellation
	go func() {
		<-scopeCtx.Done()
		s.Close()
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

	// Clear all internal state
	p.singletonsMu.Lock()
	p.singletons = nil
	p.singletonsMu.Unlock()

	if len(errors) > 0 {
		return &DisposalError{
			Context: "provider",
			Errors:  errors,
		}
	}

	return nil
}

// getSingleton retrieves a singleton instance
func (p *provider) getSingleton(key instanceKey) (any, bool) {
	p.singletonsMu.RLock()
	instance, ok := p.singletons[key]
	p.singletonsMu.RUnlock()
	return instance, ok
}

// setSingleton stores a singleton instance
func (p *provider) setSingleton(key instanceKey, instance any) {
	if instance == nil {
		return
	}

	p.singletonsMu.Lock()
	p.singletons[key] = instance
	p.singletonsMu.Unlock()

	// Track if disposable
	if d, ok := instance.(Disposable); ok {
		p.disposablesMu.Lock()
		p.disposables = append(p.disposables, d)
		p.disposablesMu.Unlock()
	}
}

// findDescriptor finds a descriptor for the given service
func (p *provider) findDescriptor(serviceType reflect.Type, key any) *Descriptor {
	if serviceType == nil {
		return nil
	}

	typeKey := TypeKey{Type: serviceType, Key: key}
	return p.services[typeKey]
}

// findGroupDescriptors finds all descriptors for a group
func (p *provider) findGroupDescriptors(serviceType reflect.Type, group string) []*Descriptor {
	if serviceType == nil || group == "" {
		return nil
	}

	groupKey := GroupKey{Type: serviceType, Group: group}
	return p.groups[groupKey]
}

// getDecorators returns decorators for a service type
func (p *provider) getDecorators(serviceType reflect.Type) []*Descriptor {
	if serviceType == nil {
		return nil
	}

	return p.decorators[serviceType]
}

// createAllSingletons creates all singleton instances at build time with enhanced error handling
func (p *provider) createAllSingletons() error {
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

	// Track which constructors have been invoked for multi-return
	invokedConstructors := make(map[uintptr][]reflect.Value)

	// Create instances in dependency order
	for _, node := range sorted {
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

		// Handle instance descriptors specially for singletons
		var instance any

		if descriptor.IsInstance {
			// For instances, use the stored value directly
			instance = descriptor.Instance
		} else if descriptor.IsMultiReturn {
			// For multi-return constructors, check if we've already invoked this constructor
			constructorPtr := descriptor.Constructor.Pointer()

			if results, invoked := invokedConstructors[constructorPtr]; invoked {
				// Use cached results
				if descriptor.ReturnIndex >= 0 && descriptor.ReturnIndex < len(results) {
					instance = results[descriptor.ReturnIndex].Interface()
				} else {
					return &ConstructorInvocationError{
						Constructor: descriptor.ConstructorType,
						Parameters:  nil,
						Cause:       fmt.Errorf("invalid return index %d for cached multi-return constructor", descriptor.ReturnIndex),
					}
				}
			} else {
				// Invoke the constructor and cache all results
				info, err := p.analyzer.Analyze(descriptor.Constructor.Interface())
				if err != nil {
					return &ReflectionAnalysisError{
						Constructor: descriptor.Constructor.Interface(),
						Operation:   "analyze",
						Cause:       err,
					}
				}

				invoker := reflection.NewConstructorInvoker(p.analyzer)
				results, err := invoker.Invoke(info, p.rootScope)
				if err != nil {
					return &ConstructorInvocationError{
						Constructor: descriptor.ConstructorType,
						Parameters:  extractParameterTypes(info),
						Cause:       err,
					}
				}

				// Cache the results
				invokedConstructors[constructorPtr] = results

				// Get the specific instance for this descriptor
				if descriptor.ReturnIndex >= 0 && descriptor.ReturnIndex < len(results) {
					instance = results[descriptor.ReturnIndex].Interface()
				} else {
					return &ConstructorInvocationError{
						Constructor: descriptor.ConstructorType,
						Parameters:  nil,
						Cause:       fmt.Errorf("invalid return index %d for multi-return constructor with %d returns", descriptor.ReturnIndex, len(results)),
					}
				}
			}
		} else {
			// Create the instance through constructor
			var err error
			instance, err = p.rootScope.createInstance(descriptor)
			if err != nil {
				return &ResolutionError{
					ServiceType: descriptor.Type,
					ServiceKey:  descriptor.Key,
					Cause:       err,
				}
			}
		}

		// Validate instance is not nil
		if instance == nil {
			return &ResolutionError{
				ServiceType: descriptor.Type,
				ServiceKey:  descriptor.Key,
				Cause:       ErrConstructorReturnedNil,
			}
		}

		// Store the singleton
		p.setSingleton(key, instance)
	}

	return nil
}

// extractParameterTypes extracts parameter types from constructor info
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
