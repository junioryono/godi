package godi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/junioryono/godi/v5/internal/reflection"
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
	// constructionContext atomically overrides context.Context resolution while
	// Build invokes eager constructors. Constructors can receive Provider and
	// resolve from other goroutines, so the override must be race-safe.
	constructionContext atomic.Pointer[scopeConstructionContext]
	cancel              context.CancelFunc

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
	disposables   []Disposable
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
}

type scopeConstructionContext struct {
	context context.Context
}

func newScope(rootProvider *provider, parent *scope, ctx context.Context, cancel context.CancelFunc) (*scope, error) {
	return newScopeWithInitialization(rootProvider, parent, ctx, cancel, true)
}

func newScopeWithoutInitialization(
	rootProvider *provider,
	parent *scope,
	ctx context.Context,
	cancel context.CancelFunc,
) (*scope, error) {
	return newScopeWithInitialization(rootProvider, parent, ctx, cancel, false)
}

func newScopeWithInitialization(
	rootProvider *provider,
	parent *scope,
	ctx context.Context,
	cancel context.CancelFunc,
	initialize bool,
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
		id:            "s" + strconv.FormatUint(scopeNum, 36),
		rootProvider:  rootProvider,
		parentScope:   parent,
		cancel:        cancel,
		instances:     make(map[instanceKey]any, 8), // Pre-size for typical usage
		disposableSet: make(map[disposableIdentity]struct{}, 4),
		closeDone:     make(chan struct{}),
		// disposables and children are lazily allocated on first use.
	}

	ctx = context.WithValue(ctx, scopeContextKey{}, s)
	s.context = ctx

	if initialize {
		if err := s.initializeScopedServices(); err != nil {
			// Tear down the partially initialized scope: dispose instances
			// created by earlier initializers and release the cancellable
			// context so neither leaks.
			_ = s.Close()
			return nil, err
		}
	}

	return s, nil
}

func (s *scope) initializeScopedServices() error {
	for _, descriptor := range s.rootProvider.voidReturnScopedDescriptors {
		if _, err := s.createInstance(descriptor); err != nil {
			return &ResolutionError{
				ServiceType: descriptor.Type,
				ServiceKey:  descriptor.Key,
				Cause:       fmt.Errorf("failed to initialize scoped service: %w", err),
			}
		}
	}
	return nil
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
// The ID is generated when the scope is created and is unique within its provider.
func (s *scope) ID() string {
	return s.id
}

// Get resolves a service in this scope
func (s *scope) Get(serviceType reflect.Type) (any, error) {
	if s.disposed.Load() != 0 {
		return nil, ErrScopeDisposed
	}

	if serviceType == nil {
		return nil, ErrServiceTypeNil
	}

	key := instanceKey{Type: serviceType}
	instance, err := s.resolve(key, nil)
	// If Close ran while resolve was in flight, surface that as
	// ErrScopeDisposed instead of a stale "not found" / dangling instance.
	if s.disposed.Load() != 0 {
		return nil, ErrScopeDisposed
	}
	return instance, err
}

// GetKeyed resolves a keyed service in this scope
func (s *scope) GetKeyed(serviceType reflect.Type, serviceKey any) (any, error) {
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
	if !reflect.TypeOf(serviceKey).Comparable() {
		return nil, &ValidationError{
			ServiceType: serviceType,
			Cause:       fmt.Errorf("service key of type %T is not comparable and cannot be used as a key", serviceKey),
		}
	}

	key := instanceKey{Type: serviceType, Key: serviceKey}
	instance, err := s.resolve(key, nil)
	if s.disposed.Load() != 0 {
		return nil, ErrScopeDisposed
	}
	return instance, err
}

// GetGroup resolves all services in a group
func (s *scope) GetGroup(serviceType reflect.Type, group string) ([]any, error) {
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
		instance, err := s.resolve(key, descriptor)
		if err != nil {
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

// CreateScope creates a child scope
func (s *scope) CreateScope(ctx context.Context) (Scope, error) {
	if s.disposed.Load() != 0 {
		return nil, ErrScopeDisposed
	}

	if ctx == nil {
		ctx = s.context
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(ctx)
	child, err := newScope(s.rootProvider, s, ctx, cancel)
	if err != nil {
		return nil, fmt.Errorf("failed to create child scope: %w", err)
	}
	if err := ctx.Err(); err != nil {
		_ = child.Close()
		return nil, err
	}

	// Track child. Re-check disposal under the lock: Close may have run
	// (and enumerated children) between the check at the top of this method
	// and here, in which case the child must be torn down by us.
	s.childrenMu.Lock()
	if s.disposed.Load() != 0 {
		s.childrenMu.Unlock()
		_ = child.Close()
		return nil, ErrScopeDisposed
	}
	if s.children == nil {
		s.children = make(map[*scope]struct{}, 2)
	}
	s.children[child] = struct{}{}
	s.childrenMu.Unlock()

	// Track in provider, re-checking both the provider's and this scope's
	// disposal. The parent may have closed (and closed the child via the
	// children map) between the tracking step above and here; inserting the
	// already-closed child into provider.scopes would leak the entry forever
	// and hand the caller a disposed scope with a nil error.
	s.rootProvider.scopesMu.Lock()
	if s.rootProvider.disposed.Load() != 0 {
		s.rootProvider.scopesMu.Unlock()
		_ = child.Close()
		return nil, ErrProviderDisposed
	}
	if s.disposed.Load() != 0 {
		s.rootProvider.scopesMu.Unlock()
		_ = child.Close()
		return nil, ErrScopeDisposed
	}
	s.rootProvider.scopes[child] = struct{}{}
	s.rootProvider.scopesMu.Unlock()

	// Auto-close on context cancellation. AfterFunc avoids dedicating a
	// goroutine per scope; Close is idempotent, so the callback firing
	// after an explicit Close (which cancels ctx) is harmless.
	context.AfterFunc(ctx, func() {
		// Context cancellation cleanup errors are expected during shutdown
		// and cannot be meaningfully handled, so we ignore them.
		_ = child.Close()
	})

	return child, nil
}

// Close disposes the scope and all its resources
func (s *scope) Close() (result error) {
	if !s.disposed.CompareAndSwap(0, 1) {
		<-s.closeDone
		return s.closeErr
	}
	defer func() {
		s.closeErr = result
		close(s.closeDone)
	}()

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
	s.disposableSet = nil
	s.disposablesMu.Unlock()

	for i := len(disposables) - 1; i >= 0; i-- {
		if err := safeClose(disposables[i]); err != nil {
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
//
// If the scope has been closed between when the caller decided to create
// this instance and when setInstance runs, the instance is closed eagerly
// (if Disposable) and not cached. This is the fix for the close-vs-resolve
// race: previously a write to s.instances after Close set it to nil would
// panic with "assignment to entry in nil map".
func (s *scope) setInstance(descriptor *descriptor, key instanceKey, instance any) {
	switch descriptor.Lifetime {
	case Singleton:
		s.rootProvider.setSingleton(key, instance)
	case Scoped:
		s.instancesMu.Lock()
		if s.instances == nil {
			s.instancesMu.Unlock()
			closeOrphan(instance)
			return
		}
		s.instances[key] = instance
		s.instancesMu.Unlock()
		s.appendDisposable(instance)
	case Transient:
		s.appendDisposable(instance)
	}
}

// appendDisposable tracks a Disposable instance for cleanup at scope close.
// If the scope is already closed, the instance is closed eagerly to avoid a
// leak.
func (s *scope) appendDisposable(instance any) {
	d, ok := instance.(Disposable)
	if !ok {
		return
	}
	s.disposablesMu.Lock()
	if s.disposed.Load() != 0 {
		s.disposablesMu.Unlock()
		closeOrphan(d)
		return
	}
	if identity, identifiable := identifyDisposable(d); identifiable {
		if _, exists := s.disposableSet[identity]; exists {
			s.disposablesMu.Unlock()
			return
		}
		if s.disposableSet == nil {
			s.disposableSet = make(map[disposableIdentity]struct{}, 4)
		}
		s.disposableSet[identity] = struct{}{}
	}
	s.disposables = append(s.disposables, d)
	s.disposablesMu.Unlock()
}

// closeOrphan closes a Disposable produced for a scope that has already been
// torn down. Panics from the disposable's Close are recovered (we have no
// caller to report to and we don't want to crash the goroutine that produced
// the orphan).
func closeOrphan(v any) {
	d, ok := v.(Disposable)
	if !ok {
		return
	}
	defer func() {
		_ = recover()
	}()
	_ = d.Close()
}

// safeClose calls d.Close() with panic recovery so a single misbehaving
// disposable can't abort the rest of a teardown loop. Recovered panics are
// returned as a wrapped error so the caller can aggregate them into a
// DisposalError.
func safeClose(d Disposable) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic during Close: %v", r)
		}
	}()
	return d.Close()
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
func (s *scope) resolveScopedSingleFlight(key instanceKey, descriptor *descriptor) (any, error) {
	fkey := flightKey(descriptor)
	newFlight := &scopeFlight{done: make(chan struct{})}
	raw, loaded := s.inflight.LoadOrStore(fkey, newFlight)
	flight := raw.(*scopeFlight)

	if loaded {
		<-flight.done
		// Sister flights may have cached our key during their createInstance.
		if instance, ok := s.getInstance(key); ok {
			return instance, nil
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
		flight.instance = instance
		return instance, nil
	}

	flight.instance, flight.err = s.createInstance(descriptor)
	return flight.instance, flight.err
}

var (
	contextType  = reflect.TypeFor[context.Context]()
	providerType = reflect.TypeFor[Provider]()
	scopeType    = reflect.TypeFor[Scope]()
)

// resolve performs the actual service resolution using the appropriate lifetime strategy.
// It handles singleton caching, scoped caching, and transient creation, while also
// detecting circular dependencies during resolution.
func (s *scope) resolve(key instanceKey, descriptor *descriptor) (any, error) {
	// Find descriptor if not provided
	if descriptor == nil {
		if key.Key == nil && key.Group == "" {
			switch key.Type {
			case contextType:
				if override := s.constructionContext.Load(); override != nil {
					return override.context, nil
				}
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
		if instance, ok := s.getInstance(key); ok {
			return instance, nil
		}
		return s.resolveScopedSingleFlight(key, descriptor)

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
func (s *scope) createInstance(descriptor *descriptor) (any, error) {
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

		s.setAliasedInstance(descriptor, key, instance)
		return instance, nil
	}

	// Read the pre-analyzed constructor info stashed on the descriptor at
	// registration time. Falls back to a fresh Analyze for descriptors that
	// were created outside the normal Add* path (e.g. constructed directly
	// in tests).
	info := descriptor.info
	if info == nil {
		var err error
		info, err = s.rootProvider.analyzer.Analyze(descriptor.Constructor.Interface())
		if err != nil {
			return nil, &ReflectionAnalysisError{
				Constructor: descriptor.Constructor.Interface(),
				Operation:   "analyze",
				Cause:       err,
			}
		}
	}

	// Get cached invoker (reduces allocations)
	invoker := s.rootProvider.analyzer.GetInvoker()

	// Invoke constructor
	results, err := invoker.Invoke(info, s)
	if err != nil {
		// Check if it's a panic error and wrap appropriately
		if panicErr, ok := errors.AsType[*reflection.PanicError](err); ok {
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

	// Reject typed nil service results before caching any sibling output. A
	// typed nil stored in an interface is not equal to nil, so checking the
	// boxed value after Interface() would incorrectly report a successful
	// resolution.
	if err := validateServiceResults(info, results); err != nil {
		return nil, err
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

			// Each field's registered descriptor is a sibling of the one
			// being resolved, matched by field index. This works for keyed
			// and grouped fields alike, whose registry keys differ from
			// their struct tags.
			// Convert empty string key to nil for consistent lookup
			var regKey any
			if reg.Key != "" {
				regKey = reg.Key
			}

			regDescriptor := descriptor.siblingForField(reg.Index)
			if regDescriptor == nil {
				if len(descriptor.siblings) > 0 {
					// Sibling-linked registration with no sibling for this
					// field: the field's registration was removed from the
					// collection. Skip it rather than falling back to the
					// registry, which could find (and wrongly shadow) a
					// replacement registration of the same type.
					continue
				}
				// Fallback for descriptors constructed outside the normal
				// Add* path (no sibling links): registry lookup by type/key.
				regDescriptor = s.rootProvider.findDescriptor(reg.Type, regKey)
			}
			if regDescriptor == nil {
				return nil, &ResolutionError{
					ServiceType: reg.Type,
					ServiceKey:  regKey,
					Cause:       fmt.Errorf("no descriptor found for return type %v", reg.Type),
				}
			}

			if regDescriptor == descriptor ||
				(reg.Type == descriptor.Type && regDescriptor.Key == descriptor.Key && regDescriptor.Group == descriptor.Group) {
				primaryService = value
			}

			key := instanceKey{
				Type:  regDescriptor.Type,
				Key:   regDescriptor.Key,
				Group: regDescriptor.Group,
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
		if len(descriptor.siblings) > 0 {
			// Cache every return value under its sibling's registration
			// (which carries the actual key or group assigned at Add time).
			for _, sibling := range descriptor.siblings {
				value := results[sibling.MultiReturnIndex].Interface()
				key := instanceKey{
					Type:  sibling.Type,
					Key:   sibling.Key,
					Group: sibling.Group,
				}
				s.setInstance(sibling, key, value)
			}
		} else {
			// Fallback for descriptors constructed outside the normal Add*
			// path (no sibling links): registry lookup per return type.
			for _, ret := range info.Returns {
				if ret.IsError {
					continue
				}

				value := results[ret.Index].Interface()

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

	s.setAliasedInstance(descriptor, key, instance)
	return instance, nil
}

func isNilServiceResult(value reflect.Value) bool {
	if !value.IsValid() {
		return true
	}
	for value.Kind() == reflect.Interface {
		if value.IsNil() {
			return true
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
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
		if isNilServiceResult(results[ret.Index]) {
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
func (s *scope) setAliasedInstance(descriptor *descriptor, key instanceKey, instance any) {
	if !descriptor.isAlias || descriptor.Lifetime == Transient || len(descriptor.siblings) == 0 {
		s.setInstance(descriptor, key, instance)
		return
	}

	switch descriptor.Lifetime {
	case Singleton:
		for _, alias := range descriptor.siblings {
			s.rootProvider.cacheSingleton(descriptorInstanceKey(alias), instance)
		}
		s.rootProvider.trackDisposable(instance)
	case Scoped:
		s.instancesMu.Lock()
		if s.instances == nil {
			s.instancesMu.Unlock()
			closeOrphan(instance)
			return
		}
		for _, alias := range descriptor.siblings {
			s.instances[descriptorInstanceKey(alias)] = instance
		}
		s.instancesMu.Unlock()
		s.appendDisposable(instance)
	}
}

func descriptorInstanceKey(descriptor *descriptor) instanceKey {
	return instanceKey{
		Type:  descriptor.Type,
		Key:   descriptor.Key,
		Group: descriptor.Group,
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
