package godi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junioryono/godi/v6/internal/reflection"
)

// ScopeFactory creates scopes. Provider and Scope implement it. A
// constructor that creates scopes of its own (a background worker, say)
// depends on ScopeFactory rather than on the Provider or Scope: injected, it
// creates child scopes of the scope resolving the constructor (the root
// scope for singletons), which are closed with it.
type ScopeFactory interface {
	CreateScope(ctx context.Context) (Scope, error)
}

// Scope provides an isolated resolution context
type Scope interface {
	Provider

	Context() context.Context
}

// scope provides an isolated resolution context
type scope struct {
	id           string
	rootProvider *provider
	parentScope  *scope
	context      context.Context
	cancel       context.CancelFunc

	// isRoot marks the provider's root scope. Its disposables live as long as
	// the provider, so they are tracked in the provider's creation-ordered
	// list alongside the singletons: a singleton and the root-scope
	// instances it depends on are then disposed in reverse creation order.
	isRoot bool

	// Scoped instances (isolated per scope)
	instances   map[instanceKey]any
	instancesMu sync.RWMutex

	// In-flight constructor invocations (single-flight per registration).
	// Without this, two goroutines requesting the same Scoped service can both
	// miss the cache and both run the constructor, violating the per-scope
	// uniqueness guarantee. For multi-return / Out-struct constructors, the
	// key is the registration's canonical sibling descriptor so that all
	// sister output types of one registration share one flight (see flightKey).
	inflight sync.Map // map[any]*scopeFlight

	// Track disposable scoped instances
	disposables   []any // resources to dispose, in creation order
	disposableSet map[disposableIdentity]struct{}
	disposablesMu sync.Mutex

	// Child scopes for hierarchical cleanup
	children   map[*scope]struct{}
	childrenMu sync.Mutex

	// State
	disposed  atomic.Int32
	closeDone chan struct{}
	closeErr  error
}

// scopeFlight coordinates a single-flight constructor invocation. The first
// goroutine to LoadOrStore one of these runs createInstance; later goroutines
// for the same flight key block on done and read the cached value out.
type scopeFlight struct {
	done     chan struct{}
	instance any
	err      error

	// waitingFor is the flight that this flight's construction is waiting
	// on, if any (guarded by provider.waitMu); see awaitFlight.
	waitingFor *scopeFlight
}

// resolveFrame is one in-progress constructor invocation. It is the
// DependencyResolver handed to the constructor invoker, so each dependency
// resolved for a constructor knows which construction requested it. Frames
// link to the requesting construction, forming the chain from the outermost
// resolution to the current constructor.
//
// Frames are only allocated where the chain is consulted: in the root scope
// (transient ownership, see trackTransient), and from any constructor that
// receives the container itself (cycle detection, see checkCycle) down.
type resolveFrame struct {
	scope      *scope
	parent     *resolveFrame
	descriptor *descriptor

	// active is true while the constructor runs. A Scope or Provider handed
	// to the constructor outlives it; once the construction is over,
	// resolutions through it are no longer part of the construction.
	active atomic.Bool

	// flight is the single-flight this construction leads, or nil
	// (transients).
	flight *scopeFlight
}

// awaitFlight waits for another construction's flight to finish, on behalf
// of the construction chain ending at parent. Before waiting it checks the
// wait-for graph: if target's construction is (transitively) waiting on a
// flight this chain leads, waiting would deadlock — typically two services
// resolving each other at runtime, first requested from different
// goroutines — and a CircularDependencyError is returned instead.
func (p *provider) awaitFlight(parent *resolveFrame, d *descriptor, target *scopeFlight) error {
	var held []*scopeFlight
	var path []string
	for f := parent; f != nil; f = f.parent {
		if f.active.Load() {
			path = append(path, describeService(f.descriptor))
			if f.flight != nil {
				held = append(held, f.flight)
			}
		}
	}
	if len(held) == 0 {
		<-target.done
		return nil
	}

	p.waitMu.Lock()
	for f := target; f != nil; f = f.waitingFor {
		if slices.Contains(held, f) {
			p.waitMu.Unlock()
			slices.Reverse(path)
			return &CircularDependencyError{
				Node: describeService(d),
				Path: append(path, describeService(d)),
			}
		}
	}
	for _, h := range held {
		h.waitingFor = target
	}
	p.waitMu.Unlock()

	<-target.done

	p.waitMu.Lock()
	for _, h := range held {
		h.waitingFor = nil
	}
	p.waitMu.Unlock()
	return nil
}

func (f *resolveFrame) Get(serviceType reflect.Type) (any, error) {
	return f.scope.get(f, serviceType)
}

func (f *resolveFrame) GetKeyed(serviceType reflect.Type, key any) (any, error) {
	return f.scope.getKeyed(f, serviceType, key)
}

func (f *resolveFrame) GetGroup(serviceType reflect.Type, group string) ([]any, error) {
	return f.scope.getGroup(f, serviceType, group)
}

// ifActive returns f while its constructor is running, else nil.
func (f *resolveFrame) ifActive() *resolveFrame {
	if f != nil && f.active.Load() {
		return f
	}
	return nil
}

// frameResolver is the Resolver injected into a constructor or decorator: it
// resolves from the scope running the constructor, attributing resolutions to
// the in-progress construction, so a constructor that (directly or
// indirectly) resolves itself gets a CircularDependencyError instead of
// deadlocking (scoped) or overflowing the stack (transient). After the
// constructor returns it resolves without that attribution. It does not embed
// the scope, so it cannot be closed or turned back into a Scope.
type frameResolver struct {
	scope *scope
	frame *resolveFrame
}

func (f *frameResolver) Get(serviceType reflect.Type) (any, error) {
	return f.scope.get(f.frame.ifActive(), serviceType)
}

func (f *frameResolver) GetKeyed(serviceType reflect.Type, key any) (any, error) {
	return f.scope.getKeyed(f.frame.ifActive(), serviceType, key)
}

func (f *frameResolver) GetGroup(serviceType reflect.Type, group string) ([]any, error) {
	return f.scope.getGroup(f.frame.ifActive(), serviceType, group)
}

func (f *frameResolver) root() *provider { return f.scope.rootProvider }

// scopeFactory is the ScopeFactory injected into a constructor: it creates
// child scopes of the scope resolving the constructor (the root scope for
// singletons), which are closed with it.
type scopeFactory struct {
	scope *scope
}

func (f scopeFactory) CreateScope(ctx context.Context) (Scope, error) {
	return f.scope.CreateScope(ctx)
}

// frameScope is the Scope injected into a constructor: the resolving scope,
// with resolutions made through it attributed to the in-progress
// construction. A constructor that (directly or indirectly) resolves itself
// through it gets a CircularDependencyError instead of deadlocking (scoped)
// or overflowing the stack (transient). After the constructor returns it
// behaves exactly like the scope.
type frameScope struct {
	*scope
	frame *resolveFrame
}

func (f *frameScope) Get(serviceType reflect.Type) (any, error) {
	return f.get(f.frame.ifActive(), serviceType)
}

func (f *frameScope) GetKeyed(serviceType reflect.Type, key any) (any, error) {
	return f.getKeyed(f.frame.ifActive(), serviceType, key)
}

func (f *frameScope) GetGroup(serviceType reflect.Type, group string) ([]any, error) {
	return f.getGroup(f.frame.ifActive(), serviceType, group)
}

// hasCachedOwner reports whether a construction requested through parent is
// owned by a cached (singleton or scoped) service of scope s: some frame in
// the chain within s, possibly through other transients, is not transient.
func hasCachedOwner(s *scope, parent *resolveFrame) bool {
	for f := parent; f != nil && f.scope == s && f.active.Load(); f = f.parent {
		if f.descriptor.Lifetime != Transient {
			return true
		}
	}
	return false
}

// checkCycle reports a CircularDependencyError if constructing d in scope s
// on behalf of parent would re-enter a construction of d (or of a sibling
// output of the same constructor) that is still in progress in s. Such
// re-entrance happens only through an injected Resolver (or the scope of an
// injected context): the static dependency graph is checked for cycles at
// Build.
func (s *scope) checkCycle(parent *resolveFrame, d *descriptor) error {
	fkey := flightKey(d)
	for f := parent; f != nil; f = f.parent {
		if f.scope != s || !f.active.Load() || flightKey(f.descriptor) != fkey {
			continue
		}
		// Path: from the re-entered construction to the current one.
		var path []string
		for g := parent; g != nil; g = g.parent {
			path = append(path, describeService(g.descriptor))
			if g == f {
				break
			}
		}
		slices.Reverse(path)
		return &CircularDependencyError{
			Node: describeService(d),
			Path: path,
		}
	}
	return nil
}

// describeService names a registration in cycle reports, in the same form as
// the dependency graph's cycle errors.
func describeService(d *descriptor) string {
	name := d.Type.String()
	if d.Key != nil {
		name += fmt.Sprintf(":%v", d.Key)
	}
	if d.Group != "" {
		name += fmt.Sprintf(" [%s]", d.Group)
	}
	return name
}

// absentOutput is cached in place of a result-object (godi.Out) field the
// constructor left nil. Caching the absence keeps the constructor from being
// re-run (and replacing its other, already cached outputs) on every attempt
// to resolve the missing field.
type absentOutput struct{}

func newScope(rootProvider *provider, parent *scope, ctx context.Context, cancel context.CancelFunc) (*scope, error) {
	s, err := newUninitializedScope(rootProvider, parent, ctx, cancel)
	if err != nil {
		return nil, err
	}

	if err := s.initializeScopedServices(); err != nil {
		// Tear down the partially initialized scope: dispose instances
		// created by earlier initializers and release the cancellable
		// context so neither leaks.
		_ = s.Close()
		return nil, err
	}

	return s, nil
}

// newUninitializedScope creates a scope without running scoped initializers.
// Build uses it for the root scope so initializers run after singletons are
// created; every other caller should use newScope.
func newUninitializedScope(
	rootProvider *provider,
	parent *scope,
	ctx context.Context,
	cancel context.CancelFunc,
) (*scope, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		if cancel != nil {
			cancel()
		}
		return nil, err
	}

	// Generate scope ID using provider's counter (scoped to this provider)
	scopeNum := rootProvider.scopeCounter.Add(1)

	s := &scope{
		id:           "s" + strconv.FormatUint(scopeNum, 36),
		rootProvider: rootProvider,
		parentScope:  parent,
		cancel:       cancel,
		instances:    make(map[instanceKey]any, 8), // Pre-size for typical usage
		closeDone:    make(chan struct{}),
		// disposables, disposableSet and children are lazily allocated on
		// first use.
	}

	ctx = context.WithValue(ctx, scopeContextKey{}, s)
	s.context = ctx

	return s, nil
}

func (s *scope) initializeScopedServices() error {
	for _, descriptor := range s.rootProvider.voidReturnScopedDescriptors {
		if _, err := s.createInstance(nil, descriptor, nil); err != nil {
			return &ResolutionError{
				ServiceType: descriptor.Type,
				ServiceKey:  descriptor.Key,
				Cause:       fmt.Errorf("failed to initialize scoped service: %w", err),
			}
		}
	}
	return nil
}

// Context returns the context associated with this scope.
// The context is used for cancellation and can carry request-scoped values.
func (s *scope) Context() context.Context {
	return s.context
}

// ID returns the unique identifier for this scope.
// The ID is generated when the scope is created and is unique within its provider.
func (s *scope) ID() string {
	return s.id
}

// Get resolves a service in this scope
func (s *scope) Get(serviceType reflect.Type) (any, error) {
	return s.get(nil, serviceType)
}

// GetKeyed resolves a keyed service in this scope
func (s *scope) GetKeyed(serviceType reflect.Type, serviceKey any) (any, error) {
	return s.getKeyed(nil, serviceType, serviceKey)
}

// GetGroup resolves all services in a group
func (s *scope) GetGroup(serviceType reflect.Type, group string) ([]any, error) {
	return s.getGroup(nil, serviceType, group)
}

// get resolves a service on behalf of parent, the construction that requested
// it (nil for a direct call).
func (s *scope) get(parent *resolveFrame, serviceType reflect.Type) (any, error) {
	if s.disposed.Load() != 0 {
		return nil, ErrScopeDisposed
	}

	if serviceType == nil {
		return nil, ErrServiceTypeNil
	}

	key := instanceKey{Type: serviceType}
	instance, err := s.resolve(parent, key, nil)
	// If Close ran while resolve was in flight, surface that as
	// ErrScopeDisposed instead of a stale "not found" / dangling instance.
	if s.disposed.Load() != 0 {
		return nil, ErrScopeDisposed
	}
	return instance, err
}

func (s *scope) getKeyed(parent *resolveFrame, serviceType reflect.Type, serviceKey any) (any, error) {
	if s.disposed.Load() != 0 {
		return nil, ErrScopeDisposed
	}

	if serviceType == nil {
		return nil, ErrServiceTypeNil
	}

	if serviceKey == nil {
		return nil, ErrServiceKeyNil
	}

	// Keys are used in map lookups; a non-comparable key would panic there.
	// Value-level comparability: a comparable static type can still wrap a
	// non-comparable value in an interface field and panic as a map key.
	if !reflect.ValueOf(serviceKey).Comparable() {
		return nil, &ValidationError{
			ServiceType: serviceType,
			Cause:       fmt.Errorf("service key of type %T is not comparable and cannot be used as a key", serviceKey),
		}
	}

	key := instanceKey{Type: serviceType, Key: serviceKey}
	instance, err := s.resolve(parent, key, nil)
	if s.disposed.Load() != 0 {
		return nil, ErrScopeDisposed
	}
	return instance, err
}

func (s *scope) getGroup(parent *resolveFrame, serviceType reflect.Type, group string) ([]any, error) {
	if s.disposed.Load() != 0 {
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
		instance, err := s.resolve(parent, key, descriptor)
		if err != nil {
			// A result-object constructor that left this member's field
			// nil did not provide it: the group simply has one fewer member.
			if isOutputNotProvided(err) {
				continue
			}
			// Normalize close-vs-resolve races to ErrScopeDisposed, the same
			// way Get and GetKeyed do.
			if s.disposed.Load() != 0 {
				return nil, ErrScopeDisposed
			}
			return nil, &ResolutionError{
				ServiceType: descriptor.Type,
				ServiceKey:  descriptor.Key,
				Cause:       fmt.Errorf("failed to resolve group member: %w", err),
			}
		}

		instances = append(instances, instance)
	}

	if s.disposed.Load() != 0 {
		return nil, ErrScopeDisposed
	}
	return instances, nil
}

// CreateScope creates a child scope, closed with this scope. A nil ctx
// defaults to this scope's context.
func (s *scope) CreateScope(ctx context.Context) (Scope, error) {
	child, err := s.rootProvider.createScope(s, ctx)
	if err != nil {
		return nil, err
	}
	return child, nil
}

// Close disposes the scope and all its resources, waiting for cleanup to
// finish. Use Shutdown to bound the wait with a context.
//
// Cancelling the context the scope was created with does not close it:
// cancellation tells the work in the scope to stop, not that it has stopped,
// so the scope's owner closes it once the work is done.
func (s *scope) Close() error {
	return s.shutdown(context.Background())
}

// shutdown disposes the scope, waiting until cleanup finishes or ctx is done;
// see Shutdown.
func (s *scope) shutdown(ctx context.Context) error {
	if ctx.Done() == nil {
		return s.closeAndWait(ctx)
	}
	// Claim the teardown here so that only the first caller starts a worker;
	// later callers just wait on its completion or their own context.
	if s.disposed.CompareAndSwap(0, 1) {
		go s.finishTeardown(ctx)
	}
	select {
	case <-s.closeDone:
		return s.closeErr
	case <-ctx.Done():
		select {
		case <-s.closeDone:
			return s.closeErr
		default:
		}
		return shutdownIncomplete(DisposalScope, ctx)
	}
}

// closeAndWait runs the teardown on the calling goroutine if it is the first
// to close the scope, otherwise waits for the teardown in progress.
func (s *scope) closeAndWait(ctx context.Context) error {
	if !s.disposed.CompareAndSwap(0, 1) {
		<-s.closeDone
		return s.closeErr
	}
	s.finishTeardown(ctx)
	return s.closeErr
}

// finishTeardown runs the teardown claimed by the caller and publishes its
// result.
func (s *scope) finishTeardown(ctx context.Context) {
	s.closeErr = s.teardown(ctx)
	close(s.closeDone)
}

// teardown cancels the scope's context, closes its child scopes (waiting for
// each, so children are fully disposed before their parent's resources), then
// disposes its resources in reverse creation order. ctx reaches
// context-aware resources.
func (s *scope) teardown(ctx context.Context) error {
	var errs []error

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
		if err := child.closeAndWait(ctx); err != nil {
			errs = append(errs, fmt.Errorf("failed to close child scope: %w", err))
		}
	}

	// Dispose all disposable scoped instances in reverse order.
	// disposableSet is deliberately retained: track consults it after close
	// so orphaned constructor results shared across sibling registrations
	// are still closed exactly once.
	s.disposablesMu.Lock()
	disposables := s.disposables
	s.disposables = nil
	s.disposablesMu.Unlock()

	for i := len(disposables) - 1; i >= 0; i-- {
		if err := s.rootProvider.disposeObserved(ctx, disposables[i], s.id); err != nil {
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
			Context: DisposalScope,
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

// cachedInstance converts a cache hit into a resolution result, reporting a
// cached absent result-object output as "not provided".
func cachedInstance(key instanceKey, instance any) (any, error) {
	if _, absent := instance.(absentOutput); absent {
		return nil, &ResolutionError{
			ServiceType: key.Type,
			ServiceKey:  key.Key,
			Cause:       errOutputNotProvided,
		}
	}
	return instance, nil
}

// setInstance caches an instance produced for descriptor (for cacheable
// lifetimes) and takes ownership of its disposal. parent is the construction
// that requested the instance's constructor.
func (s *scope) setInstance(parent *resolveFrame, descriptor *descriptor, key instanceKey, instance any) {
	dispose := !descriptor.noDispose
	switch descriptor.Lifetime {
	case Singleton:
		s.rootProvider.setSingleton(instance, dispose, key)
	case Scoped:
		s.publishScoped(instance, dispose, key)
	case Transient:
		switch {
		case dispose:
			s.trackTransient(parent, instance)
		case !s.isRoot || hasCachedOwner(s, parent):
			// NoDispose: record the value as owned elsewhere so a consumer
			// in this scope that hands it out does not adopt (and close) it.
			s.track(instance, false)
		}
	}
}

// setAbsent records that the constructor did not provide descriptor's output
// (a nil result-object field), so resolving it fails without re-running the
// constructor.
func (s *scope) setAbsent(descriptor *descriptor, key instanceKey) {
	switch descriptor.Lifetime {
	case Singleton:
		s.rootProvider.cacheSingleton(key, absentOutput{})
	case Scoped:
		s.instancesMu.Lock()
		if s.instances != nil {
			s.instances[key] = absentOutput{}
		}
		s.instancesMu.Unlock()
	}
}

// trackProduced takes ownership of a value a constructor produced that is not
// cached under any registration (its registration was removed), so it is
// still disposed with the construction's owner.
func (s *scope) trackProduced(parent *resolveFrame, requested *descriptor, instance any) {
	if requested.noDispose {
		return
	}
	switch requested.Lifetime {
	case Singleton:
		s.rootProvider.trackDisposable(instance)
	case Scoped:
		s.rootProvider.closeOrphan(s.track(instance, true), s.id)
	case Transient:
		s.trackTransient(parent, instance)
	}
}

// publishScoped caches a scoped instance under keys and takes ownership of
// its disposal.
//
// Ownership is taken before the instance is published: a concurrent
// resolution that finds it in the cache and builds a consumer on it must be
// tracked after it, so that reverse-order disposal closes the consumer first.
// If the scope has been closed while the constructor ran, the instance is
// closed eagerly (if Disposable) and not cached. With dispose false
// (NoDispose) ownership is only recorded, so child scopes don't adopt it.
func (s *scope) publishScoped(instance any, dispose bool, keys ...instanceKey) {
	s.instancesMu.Lock()
	orphan := s.track(instance, dispose)
	if orphan == nil && s.instances != nil {
		for _, key := range keys {
			s.instances[key] = instance
		}
	}
	s.instancesMu.Unlock()
	// Close outside the lock: Close may resolve from this scope.
	s.rootProvider.closeOrphan(orphan, s.id)
}

// trackTransient takes ownership of a transient instance's disposal.
//
// Transients resolved directly from the root scope (Provider.Get and
// friends) are owned by the caller: tracking them would retain every
// resolution until provider shutdown. A root-scope transient is tracked only
// when a cached service of the root scope depends on it (directly or through
// other transients), since that owner lives until shutdown anyway.
func (s *scope) trackTransient(parent *resolveFrame, instance any) {
	if s.isRoot && !hasCachedOwner(s, parent) {
		return
	}
	s.rootProvider.closeOrphan(s.track(instance, true), s.id)
}

// track takes ownership of instance's disposal if it is disposable. It
// returns the instance as an orphan, for the caller to close outside any
// lock, if the owner was already closed. Instances already owned by this
// scope, or by a longer-lived owner (a parent scope or the provider) that
// merely lent them, are not tracked again. With dispose false (NoDispose)
// ownership is only recorded and the instance is never disposed.
func (s *scope) track(instance any, dispose bool) (orphan any) {
	if !isDisposable(instance) {
		return nil
	}
	if s.isRoot {
		return s.rootProvider.track(instance, dispose)
	}

	identity, identifiable := identifyDisposable(instance)
	// e.g. a scoped service that returns a singleton: the singleton is
	// borrowed, and closing it with this scope would break the provider.
	if identifiable && s.ownedByLongerLived(identity) {
		return nil
	}

	s.disposablesMu.Lock()
	defer s.disposablesMu.Unlock()
	if identifiable {
		if _, exists := s.disposableSet[identity]; exists {
			return nil
		}
		if s.disposableSet == nil {
			s.disposableSet = make(map[disposableIdentity]struct{}, 4)
		}
		s.disposableSet[identity] = struct{}{}
	}
	if !dispose {
		return nil
	}
	if s.disposed.Load() != 0 {
		return instance
	}
	s.disposables = append(s.disposables, instance)
	return nil
}

// ownedByLongerLived reports whether a parent scope or the provider already
// owns the disposal of the value with the given identity.
func (s *scope) ownedByLongerLived(identity disposableIdentity) bool {
	for ancestor := s.parentScope; ancestor != nil && !ancestor.isRoot; ancestor = ancestor.parentScope {
		if ancestor.owns(identity) {
			return true
		}
	}
	return s.rootProvider.owns(identity)
}

// owns reports whether this scope owns the disposal of the value with the
// given identity.
func (s *scope) owns(identity disposableIdentity) bool {
	s.disposablesMu.Lock()
	_, ok := s.disposableSet[identity]
	s.disposablesMu.Unlock()
	return ok
}

// ownedByAnyone reports whether this scope or a longer-lived owner owns the
// disposal of v.
func (s *scope) ownedByAnyone(v any) bool {
	identity, identifiable := identifyDisposable(v)
	if !identifiable {
		return false
	}
	if s.isRoot {
		return s.rootProvider.owns(identity)
	}
	return s.owns(identity) || s.ownedByLongerLived(identity)
}

// flightKey computes a single-flight key for a descriptor. Multi-return and
// Out-struct constructors produce several sibling descriptors that share one
// constructor invocation; flightKey returns the registration's canonical
// sibling (siblings[0], a pointer shared by every descriptor of one Add*
// call) so one in-flight call serves all of them. Every other descriptor is
// its own flight: the same constructor function may be registered several
// times (under different names, or in different groups), and each
// registration must produce its own instances — which is why the constructor
// pointer is NOT a valid key.
func flightKey(d *descriptor) any {
	if len(d.siblings) > 0 {
		return d.siblings[0]
	}
	return d
}

// resolveScopedSingleFlight runs createInstance for a Scoped descriptor under
// single-flight: concurrent resolutions of the same key (or of sister output
// keys from the same multi-return ctor) share one constructor invocation.
func (s *scope) resolveScopedSingleFlight(parent *resolveFrame, key instanceKey, descriptor *descriptor) (any, error) {
	// Waiting on a flight that this resolution's own construction chain is
	// running would never finish.
	if err := s.checkCycle(parent, descriptor); err != nil {
		return nil, err
	}

	fkey := flightKey(descriptor)
	newFlight := &scopeFlight{done: make(chan struct{})}
	raw, loaded := s.inflight.LoadOrStore(fkey, newFlight)
	flight := raw.(*scopeFlight)

	if loaded {
		if err := s.rootProvider.awaitFlight(parent, descriptor, flight); err != nil {
			return nil, err
		}
		// Sister flights may have cached our key during their createInstance.
		if instance, ok := s.getInstance(key); ok {
			return cachedInstance(key, instance)
		}
		if flight.err != nil {
			return nil, flight.err
		}
		return nil, &ResolutionError{
			ServiceType: key.Type,
			ServiceKey:  key.Key,
			Cause:       ErrServiceNotFound,
		}
	}

	defer func() {
		s.inflight.Delete(fkey)
		close(flight.done)
	}()

	// Re-check the cache: another flight might have completed and been
	// deleted between our initial getInstance miss and LoadOrStore.
	if instance, ok := s.getInstance(key); ok {
		flight.instance, flight.err = cachedInstance(key, instance)
		return flight.instance, flight.err
	}

	flight.instance, flight.err = s.createInstance(parent, descriptor, flight)
	return flight.instance, flight.err
}

// resolveSingletonDuringBuild creates a singleton under single-flight while
// the provider is building, in the root scope. Build's own creation loop and
// on-demand resolutions from constructors (possibly on other goroutines)
// share one construction per registration.
func (s *scope) resolveSingletonDuringBuild(parent *resolveFrame, key instanceKey, descriptor *descriptor) (any, error) {
	if err := s.checkCycle(parent, descriptor); err != nil {
		return nil, err
	}

	fkey := flightKey(descriptor)
	newFlight := &scopeFlight{done: make(chan struct{})}
	raw, loaded := s.inflight.LoadOrStore(fkey, newFlight)
	flight := raw.(*scopeFlight)

	if loaded {
		if err := s.rootProvider.awaitFlight(parent, descriptor, flight); err != nil {
			return nil, err
		}
		if instance, ok := s.rootProvider.getSingleton(key); ok {
			return cachedInstance(key, instance)
		}
		if flight.err != nil {
			return nil, flight.err
		}
		return nil, &ResolutionError{
			ServiceType: key.Type,
			ServiceKey:  key.Key,
			Cause:       errSingletonNotInitialized,
		}
	}

	defer func() {
		s.inflight.Delete(fkey)
		close(flight.done)
	}()

	if instance, ok := s.rootProvider.getSingleton(key); ok {
		flight.instance, flight.err = cachedInstance(key, instance)
		return flight.instance, flight.err
	}

	flight.instance, flight.err = s.createInstance(parent, descriptor, flight)
	return flight.instance, flight.err
}

var (
	contextType      = reflect.TypeFor[context.Context]()
	providerType     = reflect.TypeFor[Provider]()
	scopeType        = reflect.TypeFor[Scope]()
	resolverType     = reflect.TypeFor[Resolver]()
	scopeFactoryType = reflect.TypeFor[ScopeFactory]()
)

// resolve performs the actual service resolution using the appropriate lifetime
// strategy: singleton and scoped caching, or transient creation. parent is the
// construction that requested the service (nil for a direct call).
func (s *scope) resolve(parent *resolveFrame, key instanceKey, descriptor *descriptor) (any, error) {
	// Find descriptor if not provided
	if descriptor == nil {
		if key.Key == nil && key.Group == "" {
			switch key.Type {
			case contextType:
				if parent != nil {
					// The scope found in the context (FromContext,
					// ResolveFromContext) is attributed to the construction,
					// like an injected Resolver.
					return context.WithValue(s.context, scopeContextKey{}, &frameScope{scope: s, frame: parent}), nil
				}
				return s.context, nil
			case resolverType:
				return &frameResolver{scope: s, frame: parent}, nil
			case scopeFactoryType:
				return scopeFactory{scope: s}, nil
			case providerType:
				// Only direct resolutions get here: constructors cannot
				// depend on Provider or Scope.
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
				Available:   s.rootProvider.registeredTypes(),
			}
		}
	}

	// Check cache based on lifetime
	switch descriptor.Lifetime {
	case Singleton:
		// Singletons are created at build time, no circular check needed
		if instance, ok := s.rootProvider.getSingleton(key); ok {
			return cachedInstance(key, instance)
		}

		// A singleton resolved before its turn during Build (at runtime,
		// through an injected Resolver) is created on demand.
		// A Lazy singleton is created on its first resolution.
		if s.rootProvider.building.Load() || descriptor.lazy {
			return s.rootProvider.rootScope.resolveSingletonDuringBuild(parent, key, descriptor)
		}

		// Singleton should have been created at build time
		return nil, &ResolutionError{
			ServiceType: key.Type,
			ServiceKey:  key.Key,
			Cause:       errSingletonNotInitialized,
		}

	case Scoped:
		if s.isRoot && s.rootProvider.validateScopes {
			return nil, &ResolutionError{
				ServiceType: key.Type,
				ServiceKey:  key.Key,
				Cause:       ErrScopeRequired,
			}
		}
		if instance, ok := s.getInstance(key); ok {
			return cachedInstance(key, instance)
		}
		return s.resolveScopedSingleFlight(parent, key, descriptor)

	case Transient:
		// Always create new instance
		if err := s.checkCycle(parent, descriptor); err != nil {
			return nil, err
		}
		return s.createInstance(parent, descriptor, nil)

	default:
		return nil, &LifetimeError{
			Value: descriptor.Lifetime,
		}
	}
}

// createInstance creates a new instance of a service using its constructor.
// It handles regular constructors, result objects (Out structs), multi-return
// constructors, and instance descriptors. parent is the construction that
// requested this one (nil for a direct call).
func (s *scope) createInstance(parent *resolveFrame, descriptor *descriptor, flight *scopeFlight) (instance any, err error) {
	if descriptor == nil {
		return nil, &ValidationError{
			ServiceType: nil,
			Cause:       errDescriptorNil,
		}
	}

	if constructed := s.rootProvider.observer.Constructed; constructed != nil && !descriptor.IsInstance {
		start := time.Now()
		defer func() {
			constructed(&ConstructedEvent{
				ServiceType: descriptor.Type,
				Key:         serviceInfoKey(descriptor),
				Lifetime:    descriptor.Lifetime,
				ScopeID:     s.id,
				Constructor: descriptor.source,
				Duration:    time.Since(start),
				Err:         err,
			})
		}()
	}

	// The constructor's (and decorators') dependencies are resolved through
	// a frame recording this construction as their requester where that
	// chain is consulted: in the root scope it decides who owns the
	// transients the construction receives (see trackTransient), and when
	// the container itself is injected it detects re-entrance (see
	// checkCycle). Elsewhere the frame allocation is skipped.
	var resolver reflection.DependencyResolver = s
	paramCount := 0
	if descriptor.info != nil {
		paramCount = len(descriptor.info.Parameters)
	}
	if (paramCount > 0 || len(descriptor.decorators) > 0) &&
		(s.isRoot || parent != nil || descriptor.injectsContainer) {
		frame := &resolveFrame{scope: s, parent: parent, descriptor: descriptor, flight: flight}
		frame.active.Store(true)
		// The construction lasts until its outputs are decorated and
		// published.
		defer frame.active.Store(false)
		resolver = frame
	}

	if descriptor.IsInstance {
		if descriptor.Instance == nil {
			return nil, &ValidationError{
				ServiceType: descriptor.Type,
				Cause:       fmt.Errorf("instance descriptor has nil instance"),
			}
		}

		return s.publishValue(parent, descriptor, descriptor.Instance, resolver)
	}

	// The constructor was analyzed at registration.
	info := descriptor.info

	// Get cached invoker (reduces allocations)
	invoker := s.rootProvider.analyzer.GetInvoker()

	results, err := invoker.Invoke(info, resolver)
	if err != nil {
		// Check if it's a panic error and wrap appropriately
		if panicErr, ok := errors.AsType[*reflection.PanicError](err); ok {
			return nil, &ConstructorPanicError{
				Constructor: descriptor.ConstructorType,
				Panic:       panicErr.Panic,
				Stack:       panicErr.Stack,
				Location:    descriptor.source,
			}
		}

		return nil, &ConstructorInvocationError{
			Constructor: descriptor.ConstructorType,
			Parameters:  extractParameterTypes(info),
			Cause:       err,
			Location:    descriptor.source,
		}
	}

	if descriptor.VoidReturn {
		emptyStruct := struct{}{}
		s.setInstance(parent, descriptor, descriptor.instanceKey(), emptyStruct)
		return emptyStruct, nil
	}

	if len(results) == 0 {
		return nil, &ConstructorInvocationError{
			Constructor: descriptor.ConstructorType,
			Parameters:  nil,
			Cause:       fmt.Errorf("constructor returned no values"),
		}
	}

	// Reject typed nil service results before caching any sibling output. A
	// typed nil stored in an interface is not equal to nil, so checking the
	// boxed value after Interface() would incorrectly report a successful
	// resolution.
	if err := validateServiceResults(info, results); err != nil {
		// Nothing will own the outputs that were produced successfully;
		// close them rather than leak them.
		s.closeProducedOutputs(descriptor, info, results)
		return nil, err
	}

	if info.IsResultObject {
		return s.publishResultObject(parent, descriptor, info, results[0], resolver)
	}

	if descriptor.MultiReturnIndex >= 0 {
		return s.publishMultiReturn(parent, descriptor, info, results, resolver)
	}

	instance = results[0].Interface()
	if instance == nil {
		return nil, &ValidationError{
			ServiceType: descriptor.Type,
			Cause:       fmt.Errorf("constructor returned nil instance"),
		}
	}

	return s.publishValue(parent, descriptor, instance, resolver)
}

// stagedOutput is one output of a construction, decorated and ready to
// publish.
type stagedOutput struct {
	target *descriptor
	key    instanceKey
	// layers holds the constructed value, then each decorator's result.
	layers    []any
	isPrimary bool
}

func (o *stagedOutput) value() any { return o.layers[len(o.layers)-1] }

// publishValue decorates and publishes the single value a constructor (or an
// instance registration) produced for d, under every interface alias, and
// returns the value d resolves to.
func (s *scope) publishValue(parent *resolveFrame, d *descriptor, value any, resolver reflection.DependencyResolver) (any, error) {
	aliased := d.isAlias && d.Lifetime != Transient && len(d.siblings) > 0
	if !aliased && len(d.decorators) == 0 {
		// Common case, kept allocation-free: one undecorated output.
		if d.Lifetime == Singleton {
			s.rootProvider.recordConstructed(d.Type, value)
		}
		s.setInstance(parent, d, d.instanceKey(), value)
		return value, nil
	}
	if aliased && !hasDecorators(d.siblings...) {
		if d.Lifetime == Singleton {
			s.rootProvider.recordConstructed(d.Type, value)
		}
		s.setAliasedInstance(parent, d, d.instanceKey(), value)
		return value, nil
	}

	targets := []*descriptor{d}
	if aliased {
		// Decorated aliases: each interface gets its own decorators' result.
		targets = d.siblings
	}
	outputs := make([]stagedOutput, len(targets))
	for i, target := range targets {
		outputs[i] = stagedOutput{target: target, key: target.instanceKey(), layers: []any{value}, isPrimary: target == d}
	}
	primary, err := s.publishOutputs(parent, d, outputs, resolver)
	if err == nil && aliased && d.Lifetime == Singleton {
		s.rootProvider.recordConstructed(d.Type, value)
	}
	return primary, err
}

// publishOutputs decorates every output, then publishes them all: either
// every output is published, or — when a decorator fails — none is and every
// value produced so far (constructed values and decorator results) is
// released.
func (s *scope) publishOutputs(
	parent *resolveFrame,
	requested *descriptor,
	outputs []stagedOutput,
	resolver reflection.DependencyResolver,
) (primary any, err error) {
	for i := range outputs {
		if len(outputs[i].target.decorators) == 0 {
			continue
		}
		outputs[i].layers, err = applyDecorators(outputs[i].target, outputs[i].layers[0], resolver)
		if err != nil {
			s.discardOutputs(requested, outputs)
			return nil, err
		}
	}
	for i := range outputs {
		s.commitOutput(parent, &outputs[i], wrappedElsewhere(outputs, i))
		if outputs[i].isPrimary {
			primary = outputs[i].value()
		}
	}
	return primary, nil
}

// wrappedElsewhere reports whether the value outputs[i] was constructed with
// is owned by a disposable decorator result of another output: interface
// aliases share one constructed value, and when one alias's decorator wraps
// it, that wrapper owns it even where the value is also published bare.
func wrappedElsewhere(outputs []stagedOutput, i int) bool {
	base, ok := identifyDisposable(outputs[i].layers[0])
	if !ok {
		return false
	}
	for j := range outputs {
		if j == i || ownerLayer(outputs[j].layers) <= 0 {
			continue
		}
		if other, ok := identifyDisposable(outputs[j].layers[0]); ok && other == base {
			return true
		}
	}
	return false
}

// commitOutput takes ownership of a staged output and caches it. Of its
// layers, godi disposes only the outermost disposable one (see ownerLayer);
// the others are recorded as owned so no scope adopts them. With
// baseWrapped, a disposable decorator result of another output owns the
// constructed value, which is then disposed only through that wrapper.
func (s *scope) commitOutput(parent *resolveFrame, out *stagedOutput, baseWrapped bool) {
	t := out.target
	if t.Lifetime == Singleton && !t.isAlias {
		// Start and HealthCheck act on the constructed service, not on
		// decorators' results. (Interface aliases share one constructed
		// value, recorded once by publishValue.)
		s.rootProvider.recordConstructed(t.Type, out.layers[0])
	}

	final := len(out.layers) - 1
	owner := ownerLayer(out.layers)
	if baseWrapped && owner == 0 {
		owner = -1
		if t.Lifetime != Transient {
			// Recorded before setInstance, which would otherwise take
			// ownership of a bare constructed value.
			s.markOwned(t, out.layers[0])
		}
	}
	if owner >= 0 && owner != final {
		s.trackProduced(parent, t, out.layers[owner])
	}
	s.setInstance(parent, t, out.key, out.layers[final])
	if t.Lifetime != Transient {
		for i, layer := range out.layers[:final] {
			if i != owner && isDisposable(layer) {
				s.markOwned(t, layer)
			}
		}
	}
}

// discardOutputs releases the values of outputs that will not be published:
// the owning layer of each, unless something else owns it.
func (s *scope) discardOutputs(requested *descriptor, outputs []stagedOutput) {
	seen := make(map[disposableIdentity]struct{})
	for i, out := range outputs {
		owner := ownerLayer(out.layers)
		if owner < 0 || owner == 0 && wrappedElsewhere(outputs, i) {
			continue
		}
		v := out.layers[owner]
		if identity, ok := identifyDisposable(v); ok {
			if _, dup := seen[identity]; dup {
				continue
			}
			seen[identity] = struct{}{}
		}
		s.discardProduced(requested, v)
	}
}

// markOwned records v as owned without disposing it (the layer that owns it
// will), so that no scope adopts it.
func (s *scope) markOwned(d *descriptor, v any) {
	if d.Lifetime == Singleton {
		s.rootProvider.track(v, false)
		return
	}
	s.track(v, false)
}

// discardProduced closes a value produced for d that will not be published,
// unless something else owns it or d is NoDispose.
func (s *scope) discardProduced(d *descriptor, value any) {
	if d.noDispose || !isDisposable(value) || s.ownedByAnyone(value) {
		return
	}
	s.rootProvider.closeOrphan(value, s.id)
}

// publishResultObject caches every field of a constructed result object (Out
// struct) under its registration and returns the field requested resolves to.
func (s *scope) publishResultObject(
	parent *resolveFrame,
	requested *descriptor,
	info *reflection.ConstructorInfo,
	result reflect.Value,
	resolver reflection.DependencyResolver,
) (any, error) {
	fields, err := reflection.ResultObjectOutputs(result, info.Returns)
	if err != nil {
		return nil, &reflectionAnalysisError{
			Constructor: requested.Constructor.Interface(),
			Operation:   "process result object",
			Cause:       err,
		}
	}

	outputs := make([]stagedOutput, 0, len(fields))
	var absent []*descriptor
	primaryAbsent := false
	next := 0
	for i, field := range fields {
		ret := info.Returns[i]

		// Each field's registered descriptor is a sibling of the one being
		// resolved, matched by field index. This works for keyed and grouped
		// fields alike, whose registry keys differ from their struct tags.
		var target *descriptor
		target, next = requested.siblingForField(field.Index, next)
		if target == nil {
			// The field's registration was removed from the collection.
			// Don't fall back to the registry, which could find (and
			// wrongly shadow) a replacement registration of the same type;
			// but the constructor still produced the value, so this
			// construction must still dispose it.
			if field.Present {
				s.trackProduced(parent, requested, field.Value)
			}
			continue
		}

		isPrimary := target == requested ||
			(ret.Type == requested.Type && target.Key == requested.Key && target.Group == requested.Group)

		if !field.Present {
			absent = append(absent, target)
			if isPrimary {
				primaryAbsent = true
			}
			continue
		}
		outputs = append(outputs, stagedOutput{target: target, key: target.instanceKey(), layers: []any{field.Value}, isPrimary: isPrimary})
	}

	primaryService, err := s.publishOutputs(parent, requested, outputs, resolver)
	if err != nil {
		return nil, err
	}
	// Record the absent fields only once the construction succeeded.
	for _, target := range absent {
		s.setAbsent(target, target.instanceKey())
	}
	if primaryService == nil {
		if primaryAbsent {
			return nil, &ResolutionError{
				ServiceType: requested.Type,
				ServiceKey:  requested.Key,
				Cause:       errOutputNotProvided,
			}
		}
		return nil, &ValidationError{
			ServiceType: requested.Type,
			Cause:       fmt.Errorf("result object produced no services"),
		}
	}

	return primaryService, nil
}

// publishMultiReturn caches every return value of a multi-return constructor
// under its registration and returns the value requested resolves to.
func (s *scope) publishMultiReturn(
	parent *resolveFrame,
	requested *descriptor,
	info *reflection.ConstructorInfo,
	results []reflect.Value,
	resolver reflection.DependencyResolver,
) (any, error) {
	// Cache every return value under its sibling's registration (which
	// carries the actual key or group assigned at Add time).
	outputs := make([]stagedOutput, 0, len(requested.siblings))
	for _, sibling := range requested.siblings {
		outputs = append(outputs, stagedOutput{
			target:    sibling,
			key:       sibling.instanceKey(),
			layers:    []any{results[sibling.MultiReturnIndex].Interface()},
			isPrimary: sibling == requested,
		})
	}

	// Return values whose registration was removed are still produced by
	// this call, so this construction must still dispose them.
	for _, ret := range info.Returns {
		if !ret.IsError && !requested.hasSiblingForReturn(ret.Index) {
			s.trackProduced(parent, requested, results[ret.Index].Interface())
		}
	}

	return s.publishOutputs(parent, requested, outputs, resolver)
}

// closeProducedOutputs closes the disposable service values of a constructor
// call whose results were rejected, unless a longer-lived owner already owns
// them (e.g. a singleton the constructor merely returned) or the registration
// is NoDispose.
func (s *scope) closeProducedOutputs(requested *descriptor, info *reflection.ConstructorInfo, results []reflect.Value) {
	if requested.noDispose {
		return
	}
	var closed map[disposableIdentity]struct{}
	for _, ret := range info.Returns {
		if ret.IsError || reflection.IsNilValue(results[ret.Index]) {
			continue
		}
		d := results[ret.Index].Interface()
		if !isDisposable(d) || s.ownedByAnyone(d) {
			continue
		}
		if identity, identifiable := identifyDisposable(d); identifiable {
			if _, done := closed[identity]; done {
				continue
			}
			if closed == nil {
				closed = make(map[disposableIdentity]struct{}, 2)
			}
			closed[identity] = struct{}{}
		}
		s.rootProvider.closeOrphan(d, s.id)
	}
}

func validateServiceResults(info *reflection.ConstructorInfo, results []reflect.Value) error {
	if info.IsResultObject {
		return nil
	}
	for _, ret := range info.Returns {
		if ret.IsError {
			continue
		}
		if reflection.IsNilValue(results[ret.Index]) {
			return &ValidationError{
				ServiceType: ret.Type,
				Cause:       fmt.Errorf("constructor returned nil service value at index %d", ret.Index),
			}
		}
	}
	return nil
}

// setAliasedInstance stores one produced value under every interface alias for
// cacheable lifetimes. Transients deliberately store only the requested alias:
// each resolution is a distinct constructor invocation.
func (s *scope) setAliasedInstance(parent *resolveFrame, descriptor *descriptor, key instanceKey, instance any) {
	if !descriptor.isAlias || descriptor.Lifetime == Transient || len(descriptor.siblings) == 0 {
		s.setInstance(parent, descriptor, key, instance)
		return
	}

	keys := make([]instanceKey, len(descriptor.siblings))
	for i, alias := range descriptor.siblings {
		keys[i] = alias.instanceKey()
	}

	switch descriptor.Lifetime {
	case Singleton:
		s.rootProvider.setSingleton(instance, !descriptor.noDispose, keys...)
	case Scoped:
		s.publishScoped(instance, !descriptor.noDispose, keys...)
	}
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
