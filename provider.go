package godi

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junioryono/godi/v5/internal/graph"
	"github.com/junioryono/godi/v5/internal/reflection"
)

// Disposable is implemented by resources that need cleanup.
//
// Close must not recursively call Close on the Provider or Scope that owns the
// resource. Shutdown is serialized so concurrent callers receive the same
// final result, which makes recursive owner shutdown deadlock by definition.
type Disposable interface {
	Close() error
}

type disposableIdentity struct {
	typ   reflect.Type
	value any
}

// identifyDisposable returns a stable identity for reference-backed disposable
// values. Equal struct values are not deduplicated because they may represent
// independently produced resources that must each be closed.
func identifyDisposable(d Disposable) (disposableIdentity, bool) {
	if d == nil {
		return disposableIdentity{}, false
	}
	value := reflect.ValueOf(d)
	if !value.IsValid() {
		return disposableIdentity{}, false
	}
	if value.Kind() != reflect.Pointer && value.Kind() != reflect.Chan {
		return disposableIdentity{}, false
	}
	if value.IsNil() {
		return disposableIdentity{}, false
	}
	return disposableIdentity{typ: value.Type(), value: d}, true
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
	// BuildTimeout specifies a cooperative deadline for building the provider.
	// Constructors that accept context.Context can stop promptly when it is
	// cancelled. Other constructors cannot be preempted, but an expired deadline
	// is checked after they return and can never produce a successful provider.
	BuildTimeout time.Duration
}

// provider is the concrete implementation of Provider
type provider struct {
	id string

	// Service registry (immutable after build)
	services map[TypeKey]*descriptor
	groups   map[GroupKey][]*descriptor

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

	// Scoped descriptors with no return values (initialization functions),
	// invoked when each scope is created. Immutable after build.
	voidReturnScopedDescriptors []*descriptor

	// Track disposable instances for cleanup
	disposables   []Disposable
	disposableSet map[disposableIdentity]struct{}
	disposablesMu sync.Mutex

	// Root scope for provider-level resolution
	rootScope *scope

	// Active scopes for cleanup tracking
	scopes   map[*scope]struct{}
	scopesMu sync.Mutex

	// Scope ID counter (atomic, scoped to this provider)
	scopeCounter atomic.Uint64

	// State
	disposed  atomic.Int32
	closeDone chan struct{}
	closeErr  error
}

// instanceKey uniquely identifies a service instance
type instanceKey struct {
	Type  reflect.Type
	Key   any
	Group string
}

// ID returns the unique identifier for the provider.
// The ID is generated when the provider is built and is unique within the process.
func (p *provider) ID() string {
	return p.id
}

// Get resolves a service from the root scope
func (p *provider) Get(serviceType reflect.Type) (any, error) {
	if p.disposed.Load() != 0 {
		return nil, ErrProviderDisposed
	}

	if serviceType == nil {
		return nil, ErrServiceTypeNil
	}

	return p.rootScope.Get(serviceType)
}

// GetKeyed resolves a keyed service from the root scope
func (p *provider) GetKeyed(serviceType reflect.Type, key any) (any, error) {
	if p.disposed.Load() != 0 {
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
	if p.disposed.Load() != 0 {
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
	if p.disposed.Load() != 0 {
		return nil, ErrProviderDisposed
	}

	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Create scope with cancellable context
	ctx, cancel := context.WithCancel(ctx)
	s, err := newScope(p, nil, ctx, cancel)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		_ = s.Close()
		return nil, err
	}

	// Track scope. Re-check disposal under the lock: Close may have run
	// (and enumerated scopes) between the check at the top of this method
	// and here, in which case this scope must be torn down by us instead
	// of leaking untracked.
	p.scopesMu.Lock()
	if p.disposed.Load() != 0 {
		p.scopesMu.Unlock()
		_ = s.Close()
		return nil, ErrProviderDisposed
	}
	p.scopes[s] = struct{}{}
	p.scopesMu.Unlock()

	// Auto-close on context cancellation. AfterFunc avoids dedicating a
	// goroutine per scope; Close is idempotent, so the callback firing
	// after an explicit Close (which cancels ctx) is harmless.
	context.AfterFunc(ctx, func() {
		// Context cancellation cleanup errors are expected during shutdown
		// and cannot be meaningfully handled, so we ignore them.
		_ = s.Close()
	})

	return s, nil
}

// Close disposes the provider and all its resources
func (p *provider) Close() (result error) {
	if !p.disposed.CompareAndSwap(0, 1) {
		<-p.closeDone
		return p.closeErr
	}
	defer func() {
		p.closeErr = result
		close(p.closeDone)
	}()

	var errors []error

	// Close all scopes
	p.scopesMu.Lock()
	scopes := make([]*scope, 0, len(p.scopes))
	for s := range p.scopes {
		if s.parentScope == nil {
			scopes = append(scopes, s)
		}
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

	// Close root scope. The field is deliberately not nil-ed: concurrent
	// Get/GetKeyed/GetGroup calls read it without synchronization, and a
	// closed root scope already rejects resolution with ErrScopeDisposed.
	if p.rootScope != nil {
		if err := p.rootScope.Close(); err != nil {
			errors = append(errors, fmt.Errorf("root scope: %w", err))
		}
	}

	// Dispose all singleton disposables.
	// disposableSet is deliberately retained: trackDisposable consults it
	// after close so a singleton constructed concurrently with Close is
	// closed eagerly, exactly once, instead of leaking.
	p.disposablesMu.Lock()
	disposables := p.disposables
	p.disposables = nil
	p.disposablesMu.Unlock()

	// Dispose in reverse order of creation; panic-isolate each Close so one
	// misbehaving disposable cannot abort the rest of the teardown loop.
	for i := len(disposables) - 1; i >= 0; i-- {
		if disposables[i] != nil {
			if err := safeClose(disposables[i]); err != nil {
				errors = append(errors, fmt.Errorf("singleton disposable %d: %w", i, err))
			}
		}
	}

	// Clear all internal state - clear singletons from sync.Map.
	// voidReturnScopedDescriptors is deliberately left intact: it is
	// immutable after build and read without synchronization by newScope.
	p.singletonKeysMu.Lock()
	for _, key := range p.singletonKeys {
		p.singletons.Delete(key)
	}
	p.singletonKeys = nil
	p.singletonKeysMu.Unlock()

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

	p.cacheSingleton(key, instance)
	p.trackDisposable(instance)
}

func (p *provider) cacheSingleton(key instanceKey, instance any) {
	p.singletons.Store(key, instance)

	// Track key for iteration during disposal
	p.singletonKeysMu.Lock()
	p.singletonKeys = append(p.singletonKeys, key)
	p.singletonKeysMu.Unlock()

}

func (p *provider) trackDisposable(instance any) {
	if d, ok := instance.(Disposable); ok {
		p.disposablesMu.Lock()
		if identity, identifiable := identifyDisposable(d); identifiable {
			if _, exists := p.disposableSet[identity]; exists {
				p.disposablesMu.Unlock()
				return
			}
			if p.disposableSet == nil {
				p.disposableSet = make(map[disposableIdentity]struct{}, 4)
			}
			p.disposableSet[identity] = struct{}{}
		}
		if p.disposed.Load() != 0 {
			// The provider was closed while the constructor was running;
			// close the orphan eagerly instead of leaking it.
			p.disposablesMu.Unlock()
			closeOrphan(d)
			return
		}
		p.disposables = append(p.disposables, d)
		p.disposablesMu.Unlock()
	}
}

// findDescriptor finds a descriptor for the given service type and optional key.
// Returns nil if no matching descriptor is found in the service registry.
func (p *provider) findDescriptor(serviceType reflect.Type, key any) *descriptor {
	if serviceType == nil {
		return nil
	}

	typeKey := TypeKey{Type: serviceType, Key: key}
	return p.services[typeKey]
}

// findGroupDescriptors finds all descriptors for a specific type within a group.
// Returns an empty slice if the type is nil, group is empty, or no services are found.
func (p *provider) findGroupDescriptors(serviceType reflect.Type, group string) []*descriptor {
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

		descriptor, ok := node.Provider.(*descriptor)
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
		if err := ctx.Err(); err != nil {
			return &BuildError{
				Phase:   "singleton-creation",
				Details: "build deadline expired after singleton creation",
				Cause:   err,
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return &BuildError{
			Phase:   "singleton-creation",
			Details: "build deadline expired after singleton creation",
			Cause:   err,
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

	serviceType := reflect.TypeFor[T]()
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

	serviceType := reflect.TypeFor[T]()
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

	serviceType := reflect.TypeFor[T]()
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
