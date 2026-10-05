package godi

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/junioryono/godi/v6/internal/reflection"
)

// Provider is the main dependency injection container interface
type Provider interface {
	// root seals the interface: only godi implements Provider and Scope, so
	// methods can be added without breaking anyone. Code that only resolves
	// services, and test doubles, should depend on Resolver instead.
	root() *provider

	Disposable

	// Returns the unique identifier for this provider instance.
	ID() string

	// Resolves a service of the specified type from the root scope.
	Get(serviceType reflect.Type) (any, error)

	// Resolves a keyed service of the specified type from the root scope.
	GetKeyed(serviceType reflect.Type, name string) (any, error)

	// Resolves all services of the specified type in a group from the root scope.
	GetGroup(serviceType reflect.Type, group string) ([]any, error)

	// Creates a new service scope for resolving services.
	CreateScope(ctx context.Context) (Scope, error)
}

// provider is the concrete implementation of Provider
type provider struct {
	id string

	// Service registry (immutable after build)
	services map[registryKey]*descriptor
	groups   map[groupID][]*descriptor

	// Singletons in creation order (see creationOrder). Immutable after build.
	singletonOrder []*descriptor

	// building is true while Build creates singletons: a singleton resolved
	// before its turn (at runtime, through an injected Resolver) is then
	// created on demand.
	building atomic.Bool

	// built is set once Build has completed, root scope initializers
	// included. An injected ScopeFactory refuses to create scopes before.
	built atomic.Bool

	// validateScopes is set by WithScopeValidation. Immutable after build.
	validateScopes bool

	// started is set by the first godi.Start.
	started atomic.Bool

	// registered lists the registered service types (registeredTypes).
	// Immutable after build.
	registered []reflect.Type

	// waitMu guards scopeFlight.waitingFor, the wait-for graph between
	// in-flight constructions (see awaitFlight).
	waitMu sync.Mutex

	// constructed lists the singletons constructed so far, before
	// decoration, in creation order (Start, HealthCheck).
	constructed   []createdSingleton
	constructedMu sync.Mutex

	// descriptors are the registrations in registration order (Describe).
	// Immutable after build.
	descriptors []*descriptor

	// observer is set by WithObserver. Immutable after build.
	observer Observer

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

// GetKeyed resolves the service registered under name from the root scope.
func (p *provider) GetKeyed(serviceType reflect.Type, name string) (any, error) {
	return p.getKeyed(nil, serviceType, keyOf(name))
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
		return nil, ErrServiceKeyEmpty
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
	s, err := p.createScope(nil, ctx, false)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// createScope creates a scope: a top-level scope when parent is nil, else a
// child of parent (which closes it). ctx defaults to the parent's context, or
// context.Background() for a top-level scope.
// createScope creates a child of parent (nil for the root scope). restricted
// marks a scope created through an injected ScopeFactory (see
// scope.restricted).
func (p *provider) createScope(parent *scope, ctx context.Context, restricted bool) (*scope, error) {
	if p.disposed.Load() != 0 {
		return nil, ErrProviderDisposed
	}
	if parent != nil && parent.disposed.Load() != 0 {
		return nil, ErrScopeDisposed
	}

	if ctx == nil {
		ctx = context.Background()
		if parent != nil {
			ctx = parent.context
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	// Create scope with cancellable context
	ctx, cancel := context.WithCancel(ctx)
	child, err := newScope(p, parent, ctx, cancel, restricted)
	if err != nil {
		if parent != nil {
			return nil, fmt.Errorf("failed to create child scope: %w", err)
		}
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		_ = child.Close()
		return nil, err
	}

	// Track the child in its parent. Re-check disposal under the lock: Close
	// may have run (and enumerated children) since the check above, in which
	// case the child must be torn down by us.
	if parent != nil {
		parent.childrenMu.Lock()
		if parent.disposed.Load() != 0 {
			parent.childrenMu.Unlock()
			_ = child.Close()
			return nil, ErrScopeDisposed
		}
		if parent.children == nil {
			parent.children = make(map[*scope]struct{}, 2)
		}
		parent.children[child] = struct{}{}
		parent.childrenMu.Unlock()
	}

	// Track in the provider, re-checking both the provider's and the
	// parent's disposal: inserting an already-closed scope into p.scopes
	// would leak the entry and hand the caller a disposed scope.
	p.scopesMu.Lock()
	if p.disposed.Load() != 0 {
		p.scopesMu.Unlock()
		_ = child.Close()
		return nil, ErrProviderDisposed
	}
	if parent != nil && parent.disposed.Load() != 0 {
		p.scopesMu.Unlock()
		_ = child.Close()
		return nil, ErrScopeDisposed
	}
	p.scopes[child] = struct{}{}
	p.scopesMu.Unlock()

	return child, nil
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
	// Claim the teardown here so that only the first caller starts a worker;
	// later callers just wait on its completion or their own context.
	if p.disposed.CompareAndSwap(0, 1) {
		go p.finishTeardown(ctx)
	}
	select {
	case <-p.closeDone:
		return p.closeErr
	case <-ctx.Done():
		select {
		case <-p.closeDone:
			return p.closeErr
		default:
		}
		return shutdownIncomplete(DisposalProvider, ctx)
	}
}

// closeAndWait runs the teardown on the calling goroutine if it is the first
// to close the provider, otherwise waits for the teardown in progress.
func (p *provider) closeAndWait(ctx context.Context) error {
	if !p.disposed.CompareAndSwap(0, 1) {
		<-p.closeDone
		return p.closeErr
	}
	p.finishTeardown(ctx)
	return p.closeErr
}

// finishTeardown runs the teardown claimed by the caller and publishes its
// result.
func (p *provider) finishTeardown(ctx context.Context) {
	p.closeErr = p.teardown(ctx)
	close(p.closeDone)
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
			if err := p.disposeObserved(ctx, disposables[i], ""); err != nil {
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
			Context: DisposalProvider,
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
		p.closeOrphan(orphan, "")
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
	p.closeOrphan(p.track(instance, true), "")
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

	typeKey := registryKey{Type: serviceType, Key: key}
	return p.services[typeKey]
}

// registeredTypes returns the distinct registered service types in
// registration order, for "did you mean" suggestions on a failed lookup.
// Computed once at Build.
func (p *provider) registeredTypes() []reflect.Type {
	return p.registered
}

func (p *provider) computeRegisteredTypes() []reflect.Type {
	seen := make(map[reflect.Type]struct{}, len(p.descriptors))
	types := make([]reflect.Type, 0, len(p.descriptors))
	for _, d := range p.descriptors {
		if d.VoidReturn {
			continue
		}
		if _, dup := seen[d.Type]; !dup {
			seen[d.Type] = struct{}{}
			types = append(types, d.Type)
		}
	}
	return types
}

// findGroupDescriptors finds all descriptors for a specific type within a group.
// Returns an empty slice if the type is nil, group is empty, or no services are found.
func (p *provider) findGroupDescriptors(serviceType reflect.Type, group string) []*descriptor {
	if serviceType == nil || group == "" {
		return nil
	}

	groupKey := groupID{Type: serviceType, Group: group}
	return p.groups[groupKey]
}

// createAllSingletonsWithContext creates all singleton instances with context cancellation support.
// The context is checked before each singleton creation, allowing for graceful cancellation
// during the build process.
func (p *provider) createAllSingletonsWithContext(ctx context.Context) error {
	// Singletons a constructor resolves at runtime (through an injected
	// Resolver) are invisible to the static order below; while building,
	// they are created on demand instead of failing.
	p.building.Store(true)
	defer p.building.Store(false)

	// Create instances in dependency order (see creationOrder)
	for _, descriptor := range p.singletonOrder {
		// Check context before each singleton creation
		select {
		case <-ctx.Done():
			return &BuildError{
				Phase:   PhaseSingletonCreation,
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
				ServiceKey:  keyName(descriptor.Key),
				Cause:       err,
			}
		}
		if err := ctx.Err(); err != nil {
			return &BuildError{
				Phase:   PhaseSingletonCreation,
				Details: "build deadline expired after singleton creation",
				Cause:   err,
			}
		}
	}

	if err := ctx.Err(); err != nil {
		return &BuildError{
			Phase:   PhaseSingletonCreation,
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
func creationOrder(all []*descriptor, services map[registryKey]*descriptor, groups map[groupID][]*descriptor) []*descriptor {
	order := make([]*descriptor, 0, len(all))
	visited := make(map[*descriptor]bool, len(all))
	var visit func(d *descriptor)
	visit = func(d *descriptor) {
		if visited[d] {
			return
		}
		visited[d] = true
		for _, dep := range d.dependencies() {
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

// singletonsInCreationOrder returns the eager (non-Lazy) singleton
// registrations in creation order.
func singletonsInCreationOrder(all []*descriptor, services map[registryKey]*descriptor, groups map[groupID][]*descriptor) []*descriptor {
	ordered := creationOrder(all, services, groups)
	singletons := ordered[:0]
	// One construction per registration: it publishes every output.
	seen := make(map[*registration]struct{})
	for _, d := range ordered {
		if d.Lifetime != Singleton || d.lazy {
			continue
		}
		if _, done := seen[d.registration]; done {
			continue
		}
		seen[d.registration] = struct{}{}
		singletons = append(singletons, d)
	}
	return singletons
}

// dependencyDescriptors returns the registrations that satisfy dep: every
// member of a group dependency, else the registration of its type and key.
func dependencyDescriptors(dep *reflection.Dependency, services map[registryKey]*descriptor, groups map[groupID][]*descriptor) []*descriptor {
	if dep == nil {
		return nil
	}
	if dep.Group != "" {
		return groups[groupID{Type: dep.Type, Group: dep.Group}]
	}
	if d := services[registryKey{Type: dep.Type, Key: dep.Key}]; d != nil {
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
