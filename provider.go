package godi

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junioryono/godi/v5/internal/reflection"
)

// Disposable is implemented by resources that need cleanup. godi also disposes
// services implementing ContextCloser or Shutdowner; see Shutdown.
//
// Close must not recursively call Close on the Provider or Scope that owns the
// resource. Shutdown is serialized so concurrent callers receive the same
// final result, which makes recursive owner shutdown deadlock by definition.
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
	// BuildTimeout specifies a cooperative deadline for building the provider.
	// Constructors that accept context.Context can stop promptly when it is
	// cancelled. Other constructors cannot be preempted, but an expired deadline
	// is checked after they return and can never produce a successful provider.
	// The deadline bounds Build only: once Build succeeds, the context given
	// to singletons is no longer subject to it and is cancelled when the
	// provider closes.
	BuildTimeout time.Duration

	// ValidateScopes rejects resolving a scoped service, directly or through
	// transients, from the provider's root scope (ErrScopeRequired). Resolved
	// from the root, a "per-request" service becomes one instance shared by
	// the whole application. With it set, the root scope also runs no scoped
	// initializers. Recommended; it will be the default in the next major
	// version.
	ValidateScopes bool
}

// provider is the concrete implementation of Provider
type provider struct {
	id string

	// Service registry (immutable after build)
	services map[TypeKey]*descriptor
	groups   map[GroupKey][]*descriptor

	// Singletons in creation order (see creationOrder). Immutable after build.
	singletonOrder []*descriptor

	// building is true while Build creates singletons: a singleton resolved
	// before its turn (at runtime, through an injected Provider or Scope)
	// is then created on demand.
	building atomic.Bool

	// validateScopes is ProviderOptions.ValidateScopes. Immutable after build.
	validateScopes bool

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
	disposables   []any // resources to dispose, in creation order
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
	return p.get(nil, serviceType)
}

// GetKeyed resolves a keyed service from the root scope
func (p *provider) GetKeyed(serviceType reflect.Type, key any) (any, error) {
	return p.getKeyed(nil, serviceType, key)
}

// GetGroup resolves all services in a group from the root scope
func (p *provider) GetGroup(serviceType reflect.Type, group string) ([]any, error) {
	return p.getGroup(nil, serviceType, group)
}

// get resolves a service from the root scope on behalf of parent, the
// construction that requested it (nil for a direct call).
func (p *provider) get(parent *resolveFrame, serviceType reflect.Type) (any, error) {
	if p.disposed.Load() != 0 {
		return nil, ErrProviderDisposed
	}

	if serviceType == nil {
		return nil, ErrServiceTypeNil
	}

	return p.rootScope.get(parent, serviceType)
}

func (p *provider) getKeyed(parent *resolveFrame, serviceType reflect.Type, key any) (any, error) {
	if p.disposed.Load() != 0 {
		return nil, ErrProviderDisposed
	}

	if serviceType == nil {
		return nil, ErrServiceTypeNil
	}

	if key == nil {
		return nil, ErrServiceKeyNil
	}

	return p.rootScope.getKeyed(parent, serviceType, key)
}

func (p *provider) getGroup(parent *resolveFrame, serviceType reflect.Type, group string) ([]any, error) {
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

	return p.rootScope.getGroup(parent, serviceType, group)
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

	return s, nil
}

// Close disposes the provider and all its resources, waiting for cleanup to
// finish. Use Shutdown to bound the wait with a context.
func (p *provider) Close() error {
	return p.shutdown(context.Background())
}

// shutdown disposes the provider, waiting until cleanup finishes or ctx is
// done; see Shutdown.
func (p *provider) shutdown(ctx context.Context) error {
	if ctx.Done() == nil {
		return p.closeAndWait(ctx)
	}
	go func() { _ = p.closeAndWait(ctx) }()
	select {
	case <-p.closeDone:
		return p.closeErr
	case <-ctx.Done():
		select {
		case <-p.closeDone:
			return p.closeErr
		default:
		}
		return shutdownIncomplete("provider", ctx)
	}
}

// closeAndWait runs the teardown on the calling goroutine if it is the first
// to close the provider, otherwise waits for the teardown in progress.
func (p *provider) closeAndWait(ctx context.Context) error {
	if !p.disposed.CompareAndSwap(0, 1) {
		<-p.closeDone
		return p.closeErr
	}
	p.closeErr = p.teardown(ctx)
	close(p.closeDone)
	return p.closeErr
}

// teardown closes the provider's scopes, then disposes its resources in
// reverse creation order. ctx reaches context-aware resources.
func (p *provider) teardown(ctx context.Context) error {
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
			if err := s.closeAndWait(ctx); err != nil {
				errors = append(errors, fmt.Errorf("scope %s: %w", s.ID(), err))
			}
		}
	}

	// Close root scope: this cancels the root context and closes scopes
	// created from it. Its disposables are tracked in the provider's list
	// below. The field is deliberately not nil-ed: concurrent
	// Get/GetKeyed/GetGroup calls read it without synchronization, and a
	// closed root scope already rejects resolution with ErrScopeDisposed.
	if p.rootScope != nil {
		if err := p.rootScope.closeAndWait(ctx); err != nil {
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
			if err := safeDispose(ctx, disposables[i]); err != nil {
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

// setSingleton stores a singleton instance under keys (one per interface
// alias) using lock-free sync.Map. It also takes ownership of the instance's
// disposal (unless the registration is NoDispose, in which case ownership is
// only recorded so no scope adopts it). Ownership is taken before the
// instance is published, so anything built on the published instance is
// disposed before it.
func (p *provider) setSingleton(instance any, dispose bool, keys ...instanceKey) {
	if instance == nil {
		return
	}

	if orphan := p.track(instance, dispose); orphan != nil {
		// The provider was closed while the constructor was running.
		closeOrphan(orphan)
		return
	}
	for _, key := range keys {
		p.cacheSingleton(key, instance)
	}
}

func (p *provider) cacheSingleton(key instanceKey, instance any) {
	p.singletons.Store(key, instance)

	// Track key for iteration during disposal
	p.singletonKeysMu.Lock()
	p.singletonKeys = append(p.singletonKeys, key)
	p.singletonKeysMu.Unlock()
}

// trackDisposable takes ownership of instance's disposal if it is disposable,
// closing it eagerly if the provider has already been closed.
func (p *provider) trackDisposable(instance any) {
	closeOrphan(p.track(instance, true))
}

// track takes ownership of instance's disposal if it is disposable. The
// provider's list holds the singletons and the root scope's disposables in
// creation order, so reverse-order disposal closes every consumer before its
// dependencies. It returns the instance as an orphan, for the caller to close
// outside any lock, if the provider was already closed (the constructor
// outlived Close).
//
// With dispose false (NoDispose registrations) ownership is only recorded,
// so no scope adopts the value, and it is never disposed.
func (p *provider) track(instance any, dispose bool) (orphan any) {
	if !isDisposable(instance) {
		return nil
	}
	p.disposablesMu.Lock()
	defer p.disposablesMu.Unlock()
	if identity, identifiable := identifyDisposable(instance); identifiable {
		if _, exists := p.disposableSet[identity]; exists {
			return nil
		}
		if p.disposableSet == nil {
			p.disposableSet = make(map[disposableIdentity]struct{}, 4)
		}
		p.disposableSet[identity] = struct{}{}
	}
	if !dispose {
		return nil
	}
	if p.disposed.Load() != 0 {
		return instance
	}
	p.disposables = append(p.disposables, instance)
	return nil
}

// owns reports whether the provider owns the disposal of the value with the
// given identity.
func (p *provider) owns(identity disposableIdentity) bool {
	p.disposablesMu.Lock()
	_, ok := p.disposableSet[identity]
	p.disposablesMu.Unlock()
	return ok
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
	// Singletons a constructor resolves at runtime (through an injected
	// Provider or Scope) are invisible to the static order below; while
	// building, they are created on demand instead of failing.
	p.building.Store(true)
	defer p.building.Store(false)

	// Create instances in dependency order (see creationOrder)
	for _, descriptor := range p.singletonOrder {
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

		key := descriptor.instanceKey()

		// Check if already created (by a sibling output or on demand)
		if _, exists := p.getSingleton(key); exists {
			continue
		}

		_, err := p.rootScope.resolveSingletonDuringBuild(nil, key, descriptor)
		if err != nil && !isOutputNotProvided(err) {
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

// creationOrder orders descriptors so that every static dependency precedes
// its dependents, and otherwise follows registration order. The order is
// deterministic, so construction (and reverse disposal) order is the same on
// every run. The dependency graph must already be known to be acyclic.
func creationOrder(all []*descriptor, services map[TypeKey]*descriptor, groups map[GroupKey][]*descriptor) []*descriptor {
	order := make([]*descriptor, 0, len(all))
	visited := make(map[*descriptor]bool, len(all))
	var visit func(d *descriptor)
	visit = func(d *descriptor) {
		if visited[d] {
			return
		}
		visited[d] = true
		for _, dep := range d.Dependencies {
			for _, depDescriptor := range dependencyDescriptors(dep, services, groups) {
				visit(depDescriptor)
			}
		}
		order = append(order, d)
	}
	for _, d := range all {
		if d != nil {
			visit(d)
		}
	}
	return order
}

// singletonsInCreationOrder returns the singleton registrations in creation
// order.
func singletonsInCreationOrder(all []*descriptor, services map[TypeKey]*descriptor, groups map[GroupKey][]*descriptor) []*descriptor {
	ordered := creationOrder(all, services, groups)
	singletons := ordered[:0]
	for _, d := range ordered {
		if d.Lifetime == Singleton {
			singletons = append(singletons, d)
		}
	}
	return singletons
}

// dependencyDescriptors returns the registrations that satisfy dep: every
// member of a group dependency, else the registration of its type and key.
func dependencyDescriptors(dep *reflection.Dependency, services map[TypeKey]*descriptor, groups map[GroupKey][]*descriptor) []*descriptor {
	if dep == nil {
		return nil
	}
	if dep.Group != "" {
		return groups[GroupKey{Type: dep.Type, Group: dep.Group}]
	}
	if d := services[TypeKey{Type: dep.Type, Key: dep.Key}]; d != nil {
		return []*descriptor{d}
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
