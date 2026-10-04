package godi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/junioryono/godi/v6/internal/graph"
	"github.com/junioryono/godi/v6/internal/reflection"
)

// Global atomic counter for fast ID generation (replaces UUID)
var providerIDCounter atomic.Uint64

// Collection represents a collection of service descriptors that define
// the services available in the dependency injection container.
//
// Collection follows a builder pattern where services are registered
// with their lifetimes and dependencies, then built into a Provider.
//
// The registration methods are safe for concurrent use, but registration
// should be complete before Build: Build works from a snapshot of the
// collection, so later registrations do not affect providers already built.
// A Collection can be built more than once, and each Build produces an
// independent Provider.
//
// Example:
//
//	collection := godi.NewCollection()
//	collection.AddSingleton(NewLogger)
//	collection.AddScoped(NewDatabase)
//
//	provider, err := collection.Build()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer provider.Close()
type Collection interface {
	// impl seals the interface: only godi implements Collection, so methods
	// can be added without breaking anyone.
	impl() *collection

	// Build creates a Provider from the registered services
	// using default options.
	Build() (Provider, error)

	// BuildWithContext creates a Provider with the given context.
	// Eager constructors can depend on context.Context and cooperate with
	// cancellation; the context is also checked throughout construction.
	BuildWithContext(ctx context.Context) (Provider, error)

	// BuildWithOptions creates a Provider with custom options
	// for validation and behavior configuration.
	BuildWithOptions(options *ProviderOptions) (Provider, error)

	// AddModules applies one or more module configurations to the service collection.
	// Modules provide a way to group related service registrations.
	// Registration errors are recorded and reported by Build (or Err).
	AddModules(modules ...ModuleOption)

	// AddSingleton registers a service with singleton lifetime.
	// Only one instance is created and shared across all resolutions.
	// Registration errors are recorded and reported by Build (or Err).
	AddSingleton(service any, opts ...AddOption)

	// AddScoped registers a service with scoped lifetime.
	// One instance is created per scope and shared within that scope.
	// The service must be a constructor, not a pre-built instance.
	// Registration errors are recorded and reported by Build (or Err).
	AddScoped(service any, opts ...AddOption)

	// AddTransient registers a service with transient lifetime.
	// A new instance is created every time the service is resolved.
	// The service must be a constructor that returns a service value.
	// Registration errors are recorded and reported by Build (or Err).
	AddTransient(service any, opts ...AddOption)

	// Err returns all registration errors recorded so far, joined into a
	// single error, or nil if every registration succeeded. Build returns
	// the same errors, so checking Err is only needed when inspecting the
	// collection before building.
	Err() error

	// Contains checks if a service exists for the type.
	Contains(serviceType reflect.Type) bool

	// ContainsKeyed checks if a keyed service exists.
	ContainsKeyed(serviceType reflect.Type, key any) bool

	// Remove removes all services for a given service type.
	Remove(serviceType reflect.Type)

	// RemoveKeyed removes a specific keyed service.
	RemoveKeyed(serviceType reflect.Type, key any)

	// ToSlice returns a read-only snapshot of all registered services for
	// inspection and debugging.
	ToSlice() []ServiceInfo

	// Count returns the number of registered services.
	Count() int
}

// Collection is the core service registry that manages services.
type collection struct {
	mu sync.RWMutex

	// services stores all non-keyed services by type
	services map[registryKey]*descriptor

	// groups stores services that belong to groups
	groups map[groupID][]*descriptor

	// allDescriptors tracks all unique descriptors for efficient iteration
	allDescriptors []*descriptor

	// analyzer is shared across all registrations for caching
	analyzer *reflection.Analyzer

	// errs accumulates registration errors so Build can report them all at
	// once; the Add* methods do not return errors.
	errs []error

	// moduleStack tracks the modules currently being applied so that
	// registration errors recorded inside a module carry the module's name.
	moduleStack []string

	// appliedModules records the modules (NewModule values) already applied.
	appliedModules map[*moduleIdentity]struct{}

	// decorators are the registered decorators, in registration order.
	decorators []*decoration
}

// registryKey identifies a registration: its service type and key (nil for
// unkeyed services).
type registryKey struct {
	Type reflect.Type
	Key  any
}

// groupID identifies a value group: its member type and name.
type groupID struct {
	Type  reflect.Type
	Group string
}

// ServiceInfo is a read-only description of a registered service, returned by
// Collection.ToSlice for inspection and debugging. It intentionally exposes
// only the stable identity of a registration, not godi's internal wiring.
type ServiceInfo struct {
	// ServiceType is the type the service resolves as.
	ServiceType reflect.Type
	// Key is the name for keyed services, or nil.
	Key any
	// Group is the value-group name for grouped services, or "".
	Group string
	// Lifetime is the service's lifetime (Singleton, Scoped, or Transient).
	Lifetime Lifetime
}

func (sc *collection) impl() *collection { return sc }

// NewCollection creates a new empty Collection instance.
//
// Example:
//
//	collection := godi.NewCollection()
//	collection.AddSingleton(NewLogger)
//	provider, err := collection.Build()
func NewCollection() Collection {
	return &collection{
		services:       make(map[registryKey]*descriptor, 16), // Pre-size for typical usage
		groups:         make(map[groupID][]*descriptor, 4),
		allDescriptors: make([]*descriptor, 0, 16),
		analyzer:       reflection.New(reflection.WithNotFound(isNotFound)),
	}
}

// Build creates a Provider from the registered services using default options.
func (sc *collection) Build() (Provider, error) {
	return sc.BuildWithContext(context.Background())
}

// BuildWithContext creates a Provider with the given cooperative build context.
// The context is available to eager constructors that depend on context.Context
// and is checked throughout construction. It also parents the provider's root
// context, so its values remain visible and its cancellation propagates.
func (sc *collection) BuildWithContext(ctx context.Context) (Provider, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return sc.doBuild(ctx, ctx, nil)
}

// BuildWithOptions creates a Provider configured by options (which may be
// nil): a parent context, a build timeout, scope validation, and an observer.
func (sc *collection) BuildWithOptions(options *ProviderOptions) (Provider, error) {
	parent := context.Background()
	if options != nil && options.Context != nil {
		parent = options.Context
	}
	ctx := parent

	// Handle build timeout if specified. The timeout bounds Build only: the
	// provider's root context is detached from it once Build succeeds.
	if options != nil && options.BuildTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.BuildTimeout)
		defer cancel()
	}

	return sc.doBuild(parent, ctx, options)
}

// doBuild builds a provider. parent becomes the parent of the provider's root
// context; ctx bounds the build itself and is visible (deadline and
// cancellation) to constructors that run during Build. options may be nil.
func (sc *collection) doBuild(parent, ctx context.Context, options *ProviderOptions) (Provider, error) {
	// Check context before starting
	select {
	case <-ctx.Done():
		return nil, &BuildError{
			Phase:   PhaseInitialization,
			Details: "build cancelled before starting",
			Cause:   ctx.Err(),
		}
	default:
	}

	// Phases 1-3: snapshot and validate (see plan).
	built, planErr := sc.plan(ctx)
	if planErr != nil {
		return nil, planErr
	}
	allDescriptors, services, groups := built.all, built.services, built.groups

	// Phase 4: Create provider with fast ID generation
	// Count void-return scoped descriptors for pre-allocation
	voidCount := 0
	for _, d := range allDescriptors {
		if d != nil && d.Lifetime == Scoped && d.VoidReturn {
			voidCount++
		}
	}

	p := &provider{
		id:                          "p" + strconv.FormatUint(providerIDCounter.Add(1), 36),
		services:                    services,
		groups:                      groups,
		singletonOrder:              singletonsInCreationOrder(allDescriptors, services, groups),
		validateScopes:              options != nil && options.ValidateScopes,
		descriptors:                 allDescriptors,
		observer:                    observerOf(options),
		analyzer:                    sc.analyzer, // Share analyzer from collection
		singletonKeys:               make([]instanceKey, 0, len(allDescriptors)),
		voidReturnScopedDescriptors: make([]*descriptor, 0, voidCount),
		disposables:                 make([]any, 0, 4),
		disposableSet:               make(map[disposableIdentity]struct{}, 4),
		scopes:                      make(map[*scope]struct{}, 4),
		closeDone:                   make(chan struct{}),
	}

	for _, descriptor := range allDescriptors {
		if descriptor != nil && descriptor.Lifetime == Scoped && descriptor.VoidReturn {
			p.voidReturnScopedDescriptors = append(p.voidReturnScopedDescriptors, descriptor)
		}
	}
	p.registered = p.computeRegisteredTypes()

	// Phase 5: Create root scope
	select {
	case <-ctx.Done():
		return nil, &BuildError{
			Phase:   PhaseScopeCreation,
			Details: "build cancelled during root scope creation",
			Cause:   ctx.Err(),
		}
	default:
	}

	var err error
	rootCtx := newProviderContext(parent, ctx)
	p.rootScope, err = newUninitializedScope(p, nil, rootCtx, rootCtx.cancel)
	if err != nil {
		return nil, &BuildError{
			Phase:   PhaseScopeCreation,
			Details: "failed to create root scope",
			Cause:   err,
		}
	}
	p.rootScope.isRoot = true

	// Phase 6: Create singletons. Eager constructors receive the root scope's
	// context, which reports the build context's deadline and cancellation
	// until Build succeeds.
	if err := p.createAllSingletonsWithContext(ctx); err != nil {
		buildErr := &BuildError{
			Phase:   PhaseSingletonCreation,
			Details: "failed to initialize singletons",
			Cause:   err,
		}
		return nil, joinBuildCleanupError(buildErr, p.Close())
	}

	// Phase 7: Initialize root-scoped side-effect constructors only after all
	// singletons exist. Request/child scopes still initialize them in newScope.
	// With ValidateScopes the root scope holds no scoped services, so it runs
	// no scoped initializers either.
	if !p.validateScopes {
		if err := p.rootScope.initializeScopedServices(); err != nil {
			buildErr := &BuildError{
				Phase:   PhaseScopeInitialization,
				Details: "failed to initialize root scoped services",
				Cause:   err,
			}
			return nil, joinBuildCleanupError(buildErr, p.Close())
		}
	}
	if err := ctx.Err(); err != nil {
		buildErr := &BuildError{
			Phase:   PhaseScopeInitialization,
			Details: "build deadline expired after root scope initialization",
			Cause:   err,
		}
		return nil, joinBuildCleanupError(buildErr, p.Close())
	}

	// Detach the root context from the build context. If the build context
	// was cancelled first, the root context is already cancelled too, so the
	// provider must not be returned.
	if !rootCtx.finishBuild() {
		buildErr := &BuildError{
			Phase:   PhaseScopeInitialization,
			Details: "build cancelled while finishing",
			Cause:   ctx.Err(),
		}
		return nil, joinBuildCleanupError(buildErr, p.Close())
	}

	return p, nil
}

// buildPlan is a validated, provider-owned snapshot of a collection.
type buildPlan struct {
	all      []*descriptor
	services map[registryKey]*descriptor
	groups   map[groupID][]*descriptor
}

// plan snapshots the collection and validates it (registration errors,
// decorators, cycles, lifetimes, missing dependencies) without constructing
// anything. ctx is checked between phases.
func (sc *collection) plan(ctx context.Context) (*buildPlan, error) {
	// Hold the collection lock only to read it. Constructors run later in
	// Build and may call collection methods (Count, Contains, ...), which
	// would deadlock against a lock held for the whole build.
	sc.mu.Lock()

	// Surface every recorded registration error before doing any work:
	// the Add* methods defer their errors to Build so callers can register
	// services without per-call error checks.
	if len(sc.errs) > 0 {
		err := errors.Join(sc.errs...)
		sc.mu.Unlock()
		return nil, &BuildError{
			Phase:   PhaseRegistration,
			Details: "one or more service registrations failed",
			Cause:   err,
		}
	}

	// Build a provider-owned snapshot. Collections remain reusable after Build,
	// so providers must never retain the collection's mutable maps, slices, or
	// sibling links.
	allDescriptors, services, groups := snapshotRegistrations(
		sc.allDescriptors,
		sc.services,
		sc.groups,
	)
	decorators := append([]*decoration(nil), sc.decorators...)
	sc.mu.Unlock()

	// Decorators add dependencies to the services they decorate, so attach
	// them before the graph and validation see those dependencies.
	decoratorSources, err := attachDecorators(decorators, services, groups)
	if err != nil {
		return nil, &BuildError{
			Phase:   PhaseRegistration,
			Details: "invalid decorators",
			Cause:   err,
		}
	}

	// Phase 1: Build dependency graph (validates cycles as part of build)
	select {
	case <-ctx.Done():
		return nil, &BuildError{
			Phase:   PhaseGraph,
			Details: "build cancelled during graph construction",
			Cause:   ctx.Err(),
		}
	default:
	}

	g := graph.NewDependencyGraphWithCapacity(len(allDescriptors))

	for _, descriptor := range allDescriptors {
		if descriptor == nil {
			continue
		}

		if err := g.AddProviderDeferred(descriptor); err != nil {
			return nil, &BuildError{
				Phase:   PhaseGraph,
				Details: fmt.Sprintf("failed to add provider %v", formatType(descriptor.Type)),
				Cause:   err,
			}
		}
	}

	// Phase 1.5: Resolve group dependencies
	// Connect group consumers to actual group member nodes in the graph.
	// Without this, group consumers depend on phantom nodes (Key=nil) that
	// don't match the real group members (Key=1,2,...), hiding cycles that
	// run through a group.
	g.ResolveGroupDependencies()

	// Phase 2: Validate graph (cycles detected here, not per-add)
	if err := g.DetectCycles(); err != nil {
		return nil, &BuildError{
			Phase:   PhaseValidation,
			Details: "dependency graph validation failed",
			Cause:   err,
		}
	}

	// Phase 3: Validate lifetimes
	select {
	case <-ctx.Done():
		return nil, &BuildError{
			Phase:   PhaseValidation,
			Details: "build cancelled during lifetime validation",
			Cause:   ctx.Err(),
		}
	default:
	}

	if err := validateLifetimes(allDescriptors, services, groups); err != nil {
		return nil, &BuildError{
			Phase:   PhaseValidation,
			Details: "lifetime validation failed",
			Cause:   err,
		}
	}

	if err := validateDependencies(allDescriptors, services, decoratorSources); err != nil {
		return nil, &BuildError{
			Phase:   PhaseValidation,
			Details: "missing dependencies",
			Cause:   err,
		}
	}

	return &buildPlan{all: allDescriptors, services: services, groups: groups}, nil
}

// providerContext is the root scope's context. It is cancelled by
// Provider.Close (or by the BuildWithContext parent). While Build runs it is
// also cancelled by the build context and reports that context's error, so
// eager constructors observe a build timeout; once Build succeeds it is
// detached from the build context, so the timeout cannot cancel the context
// that singletons keep.
type providerContext struct {
	context.Context
	cancel context.CancelFunc

	build     context.Context
	stopBuild func() bool
	building  atomic.Bool
}

func newProviderContext(parent, build context.Context) *providerContext {
	ctx, cancel := context.WithCancel(parent)
	c := &providerContext{Context: ctx, cancel: cancel, build: build}
	c.building.Store(true)
	if build != parent {
		c.stopBuild = context.AfterFunc(build, cancel)
	}
	return c
}

// finishBuild detaches the context from the build context. It reports false
// when the build context was cancelled first (and so cancelled this context).
func (c *providerContext) finishBuild() bool {
	if c.stopBuild != nil && !c.stopBuild() {
		return false
	}
	c.building.Store(false)
	return true
}

// Deadline is deliberately inherited from the parent, not the build context:
// context.Context requires successive Deadline calls to agree, so a build
// deadline cannot be reported during Build and dropped afterwards. The
// build deadline still reaches constructors as cancellation, and Err
// reports DeadlineExceeded.

func (c *providerContext) Err() error {
	err := c.Context.Err()
	if err != nil && c.building.Load() {
		// Report why the build was cancelled (e.g. DeadlineExceeded).
		if buildErr := c.build.Err(); buildErr != nil {
			return buildErr
		}
	}
	return err
}

func joinBuildCleanupError(buildErr, closeErr error) error {
	if closeErr == nil {
		return buildErr
	}
	return errors.Join(
		buildErr,
		&BuildError{
			Phase:   PhaseCleanup,
			Details: "failed to clean up partially created provider",
			Cause:   closeErr,
		},
	)
}

// AddModules applies one or more module configurations to the service collection.
// Errors returned by module functions are recorded and reported by Build.
func (sc *collection) AddModules(modules ...ModuleOption) {
	for _, module := range modules {
		if module == nil {
			continue
		}

		if err := module(sc); err != nil {
			sc.recordErr(err)
		}
	}
}

// AddSingleton adds a singleton service to the collection.
// Registration errors are recorded and reported by Build (or Err).
func (sc *collection) AddSingleton(service any, opts ...AddOption) {
	sc.recordErr(sc.addService(service, Singleton, opts...))
}

// AddScoped adds a scoped service to the collection.
// Registration errors are recorded and reported by Build (or Err).
func (sc *collection) AddScoped(service any, opts ...AddOption) {
	sc.recordErr(sc.addService(service, Scoped, opts...))
}

// AddTransient adds a transient service to the collection.
// Registration errors are recorded and reported by Build (or Err).
func (sc *collection) AddTransient(service any, opts ...AddOption) {
	sc.recordErr(sc.addService(service, Transient, opts...))
}

// recordErr stores a registration error for Build to report, wrapping it
// with the names of the modules being applied (innermost last) so the
// failure is attributable.
func (sc *collection) recordErr(err error) {
	if err == nil {
		return
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()

	for i := len(sc.moduleStack) - 1; i >= 0; i-- {
		// Avoid double-wrapping: module functions may already return
		// ModuleError for the innermost module.
		var moduleErr *ModuleError
		if errors.As(err, &moduleErr) && moduleErr.Module == sc.moduleStack[i] {
			continue
		}
		err = &ModuleError{Module: sc.moduleStack[i], Cause: err}
	}

	sc.errs = append(sc.errs, err)
}

// Err returns all registration errors recorded so far, joined into a single
// error, or nil if every registration succeeded.
func (sc *collection) Err() error {
	sc.mu.RLock()
	defer sc.mu.RUnlock()

	return errors.Join(sc.errs...)
}

// moduleIdentity identifies a module value created by NewModule. It is a
// non-zero-size type so every module gets a distinct pointer.
type moduleIdentity struct{ _ byte }

// markModuleApplied records that the module was applied, reporting false if
// it already had been.
func (sc *collection) markModuleApplied(id *moduleIdentity) bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if _, applied := sc.appliedModules[id]; applied {
		return false
	}
	if sc.appliedModules == nil {
		sc.appliedModules = make(map[*moduleIdentity]struct{})
	}
	sc.appliedModules[id] = struct{}{}
	return true
}

// pushModule and popModule maintain the module attribution stack used by
// recordErr. They are invoked by NewModule via interface assertion.
func (sc *collection) pushModule(name string) {
	sc.mu.Lock()
	sc.moduleStack = append(sc.moduleStack, name)
	sc.mu.Unlock()
}

func (sc *collection) popModule() {
	sc.mu.Lock()
	if len(sc.moduleStack) > 0 {
		sc.moduleStack = sc.moduleStack[:len(sc.moduleStack)-1]
	}
	sc.mu.Unlock()
}

// Contains checks if a service exists for the type
func (r *collection) Contains(t reflect.Type) bool {
	if t == nil {
		return false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	typeKey := registryKey{Type: t}
	_, ok := r.services[typeKey]
	return ok
}

// ContainsKeyed checks if a keyed service exists
func (r *collection) ContainsKeyed(t reflect.Type, key any) bool {
	if t == nil {
		return false
	}
	// Value-level comparability: a comparable static type can still wrap a
	// non-comparable value in an interface field and panic as a map key.
	if key != nil && !reflect.ValueOf(key).Comparable() {
		return false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	typeKey := registryKey{Type: t, Key: key}
	_, ok := r.services[typeKey]
	return ok
}

// HasGroup checks if a group has any services registered for the specified type and group name.
// Returns false if the type is nil, group name is empty, or no services are registered in the group.
func (r *collection) HasGroup(t reflect.Type, group string) bool {
	if t == nil || group == "" {
		return false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	groupKey := groupID{Type: t, Group: group}
	services, ok := r.groups[groupKey]
	return ok && len(services) > 0
}

// Remove removes all services for a given type: the unkeyed registration,
// every keyed registration, and every group member of that type.
func (r *collection) Remove(t reflect.Type) {
	if t == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	removed := make(map[*descriptor]struct{})
	for key, descriptor := range r.services {
		if key.Type == t {
			removed[descriptor] = struct{}{}
			delete(r.services, key)
		}
	}
	for key, descriptors := range r.groups {
		if key.Type == t {
			for _, descriptor := range descriptors {
				removed[descriptor] = struct{}{}
			}
			delete(r.groups, key)
		}
	}

	r.pruneDescriptors(removed)
}

// RemoveKeyed removes a specific keyed service
func (r *collection) RemoveKeyed(t reflect.Type, key any) {
	if t == nil {
		return
	}
	// Value-level comparability: a comparable static type can still wrap a
	// non-comparable value in an interface field and panic as a map key.
	if key != nil && !reflect.ValueOf(key).Comparable() {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	typeKey := registryKey{Type: t, Key: key}
	d, ok := r.services[typeKey]
	if !ok {
		return
	}

	delete(r.services, typeKey)
	r.pruneDescriptors(map[*descriptor]struct{}{d: {}})
}

// pruneDescriptors drops the given descriptors from allDescriptors so that
// Build, Count, and ToSlice no longer see them. Without this, removed
// singletons would still be constructed at build time.
func (r *collection) pruneDescriptors(removed map[*descriptor]struct{}) {
	if len(removed) == 0 {
		return
	}

	kept := r.allDescriptors[:0]
	for _, d := range r.allDescriptors {
		if _, ok := removed[d]; !ok {
			kept = append(kept, d)
		}
	}
	// Zero the tail so the backing array doesn't pin removed descriptors.
	for i := len(kept); i < len(r.allDescriptors); i++ {
		r.allDescriptors[i] = nil
	}
	r.allDescriptors = kept

	// Unlink removed descriptors from survivors' sibling lists. Otherwise a
	// surviving sibling's constructor invocation would still cache instances
	// under the removed registration's keys, shadowing any replacement
	// registered after the removal.
	for _, d := range r.allDescriptors {
		if len(d.siblings) == 0 {
			continue
		}
		pruned := false
		for _, sibling := range d.siblings {
			if _, ok := removed[sibling]; ok {
				pruned = true
				break
			}
		}
		if !pruned {
			continue
		}
		surviving := make([]*descriptor, 0, len(d.siblings))
		for _, sibling := range d.siblings {
			if _, ok := removed[sibling]; !ok {
				surviving = append(surviving, sibling)
			}
		}
		for _, sibling := range surviving {
			sibling.siblings = surviving
		}
	}
}

// ToSlice returns a copy of all registered service descriptors
func (r *collection) ToSlice() []ServiceInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]ServiceInfo, 0, len(r.allDescriptors))
	for _, d := range r.allDescriptors {
		if d == nil {
			continue
		}
		result = append(result, describeDescriptor(d).ServiceInfo)
	}
	return result
}

// serviceInfoKey returns the key a caller can resolve d with, hiding keys
// godi generated internally (void initializers, group member positions).
func serviceInfoKey(d *descriptor) any {
	if d.syntheticKey {
		return nil
	}
	return d.Key
}

// Count returns the number of registered services in the collection.
func (r *collection) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.allDescriptors)
}

var (
	// Reserved types that are handled specially by the framework
	reservedTypes = map[reflect.Type]struct{}{
		reflect.TypeFor[context.Context](): {},
		reflect.TypeFor[Provider]():        {},
		reflect.TypeFor[Scope]():           {},
	}
)

// addService registers a new service with the specified lifetime and options.
// It performs validation, creates descriptors, handles multi-return constructors,
// and manages interface registrations when using the As option.
func (r *collection) addService(service any, lifetime Lifetime, opts ...AddOption) error {
	// Validate inputs
	if service == nil {
		return &ValidationError{
			ServiceType: nil,
			Cause:       ErrConstructorNil,
		}
	}

	// Create descriptor from constructor using shared analyzer
	descriptor, err := newDescriptorWithAnalyzer(service, lifetime, r.analyzer, opts...)
	if err != nil {
		return &RegistrationError{
			ServiceType: nil,
			Operation:   "create descriptor",
			Cause:       err,
		}
	}

	// Validate the descriptor
	if validationErr := descriptor.Validate(); validationErr != nil {
		return &RegistrationError{
			ServiceType: descriptor.Type,
			Operation:   "validate descriptor",
			Cause:       validationErr,
		}
	}

	// Check if the service type is reserved
	if _, isReserved := reservedTypes[descriptor.Type]; isReserved {
		return &ValidationError{
			ServiceType: descriptor.Type,
			Cause:       fmt.Errorf("service type %s is reserved and cannot be registered", formatType(descriptor.Type)),
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// newDescriptorWithAnalyzer already parsed options and validated them,
	// and Analyze() was called on the way through. Re-parse the options
	// locally so we can inspect them (Name/Group/As), but skip the second
	// Analyze call and the second Validate by reading the cached info off
	// the descriptor.
	options := &addOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt.applyAddOption(options)
		}
	}

	info := descriptor.info

	// Handle result objects (Out structs)
	if info.IsResultObject {
		if options.key() != nil || options.Group != "" {
			return &RegistrationError{
				ServiceType: descriptor.Type,
				Operation:   "register result object",
				Cause:       fmt.Errorf("godi.Name and godi.Group cannot be applied to a result object (godi.Out) constructor; put name or group tags on its fields"),
			}
		}
		// godi.As is ambiguous for result objects: it's unclear which field
		// the interface should bind to. Reject explicitly rather than
		// silently dropping the option.
		if len(options.As) > 0 {
			return &RegistrationError{
				ServiceType: descriptor.Type,
				Operation:   "register result object",
				Cause:       fmt.Errorf("godi.As cannot be combined with a result object (godi.Out) constructor; use a name or group tag on the field instead"),
			}
		}
		return r.registerResultObjectFields(descriptor)
	}

	// Handle multiple return types (not Out structs)
	if handled, err := r.registerMultiReturn(descriptor, info, options); handled {
		return err
	}

	// Handle As option - register under interface types.
	// If As is specified, we only register under interface types, not the concrete type.
	if len(options.As) > 0 {
		return r.registerAliases(descriptor, options)
	}

	// Register the descriptor normally
	return r.registerDescriptor(descriptor)
}

// registerAliases registers a descriptor under each interface type in
// options.As instead of its concrete type. The aliases are linked as siblings
// so one constructor invocation caches every interface entry. Caller must hold
// r.mu.
func (r *collection) registerAliases(d *descriptor, options *addOptions) error {
	// A void or error-only constructor produces no service value to bind
	// to an interface. Reject rather than registering an empty struct
	// placeholder under the interface type.
	if d.VoidReturn {
		return &RegistrationError{
			ServiceType: d.Type,
			Operation:   "register as interface",
			Cause:       fmt.Errorf("godi.As cannot be combined with a constructor that returns no service value"),
		}
	}

	// Validate every alias before committing any of them. A single Add call is
	// transactional: either all requested interfaces are registered or none
	// are.
	interfaceDescriptors := make([]*descriptor, 0, len(options.As))
	seenInterfaces := make(map[reflect.Type]struct{}, len(options.As))
	for _, iface := range options.As {
		interfaceType := reflect.TypeOf(iface).Elem()
		if _, duplicate := seenInterfaces[interfaceType]; duplicate {
			return &RegistrationError{
				ServiceType: interfaceType,
				Operation:   "register as interface",
				Cause:       fmt.Errorf("interface %s was specified more than once", formatType(interfaceType)),
			}
		}
		seenInterfaces[interfaceType] = struct{}{}

		// Reserved types are special-cased by the resolver and cannot be
		// registered, not even via As.
		if _, isReserved := reservedTypes[interfaceType]; isReserved {
			return &ValidationError{
				ServiceType: interfaceType,
				Cause:       fmt.Errorf("service type %s is reserved and cannot be registered", formatType(interfaceType)),
			}
		}

		// Validate that the service type implements the interface
		if !d.Type.Implements(interfaceType) {
			return &TypeMismatchError{
				Expected: interfaceType,
				Actual:   d.Type,
				Context:  "interface implementation",
			}
		}

		// Create a new descriptor for the interface type
		interfaceDescriptor := d.clone()
		interfaceDescriptor.Type = interfaceType
		interfaceDescriptor.As = options.As
		interfaceDescriptor.isAlias = true
		interfaceDescriptors = append(interfaceDescriptors, interfaceDescriptor)
	}

	for _, interfaceDescriptor := range interfaceDescriptors {
		interfaceDescriptor.siblings = interfaceDescriptors
	}

	registered := make([]*descriptor, 0, len(interfaceDescriptors))
	for _, interfaceDescriptor := range interfaceDescriptors {
		if err := r.registerDescriptor(interfaceDescriptor); err != nil {
			r.unregisterDescriptors(registered)
			return &RegistrationError{
				ServiceType: interfaceDescriptor.Type,
				Operation:   "register as interface",
				Cause:       err,
			}
		}
		registered = append(registered, interfaceDescriptor)
	}

	return nil
}

// snapshotRegistrations clones the mutable registration graph owned by a
// collection. Descriptor metadata and constructor analysis are immutable after
// registration, but descriptors and their sibling slices are rewritten by
// Remove, so those links must be remapped to provider-owned clones.
func snapshotRegistrations(
	all []*descriptor,
	services map[registryKey]*descriptor,
	groups map[groupID][]*descriptor,
) (
	snapshotAll []*descriptor,
	snapshotServices map[registryKey]*descriptor,
	snapshotGroups map[groupID][]*descriptor,
) {
	clones := make(map[*descriptor]*descriptor, len(all))
	snapshotAll = make([]*descriptor, 0, len(all))

	for _, original := range all {
		if original == nil {
			continue
		}
		clone := *original
		clone.siblings = nil
		clone.As = append([]any(nil), original.As...)
		clone.Dependencies = append([]*reflection.Dependency(nil), original.Dependencies...)
		clone.resultFields = append([]reflection.ResultField(nil), original.resultFields...)
		clone.paramFields = append([]reflection.ParamField(nil), original.paramFields...)
		clones[original] = &clone
		snapshotAll = append(snapshotAll, &clone)
	}

	for original, clone := range clones {
		if len(original.siblings) == 0 {
			continue
		}
		clone.siblings = make([]*descriptor, 0, len(original.siblings))
		for _, sibling := range original.siblings {
			if siblingClone, ok := clones[sibling]; ok {
				clone.siblings = append(clone.siblings, siblingClone)
			}
		}
	}

	snapshotServices = make(map[registryKey]*descriptor, len(services))
	for key, original := range services {
		if clone, ok := clones[original]; ok {
			snapshotServices[key] = clone
		}
	}

	snapshotGroups = make(map[groupID][]*descriptor, len(groups))
	for key, originals := range groups {
		members := make([]*descriptor, 0, len(originals))
		for _, original := range originals {
			if clone, ok := clones[original]; ok {
				members = append(members, clone)
			}
		}
		snapshotGroups[key] = members
	}

	return snapshotAll, snapshotServices, snapshotGroups
}

// registerResultObjectFields registers each exported field of a result
// object (Out struct) as its own service. The fields all share the same
// constructor and are linked as siblings so one invocation can cache every
// field under its own registration (key or group). The result object type
// itself is not registered. Caller must hold r.mu.
func (r *collection) registerResultObjectFields(d *descriptor) error {
	// No fields to register
	if len(d.resultFields) == 0 {
		return nil
	}

	fieldDescriptors := make([]*descriptor, 0, len(d.resultFields))
	for _, field := range d.resultFields {
		// A field cannot be both keyed and grouped: the resolver caches and
		// looks up under exactly one of the two, so accepting both would
		// register a service that can never be resolved consistently.
		if field.Key != nil && field.Group != "" {
			return &RegistrationError{
				ServiceType: field.Type,
				Operation:   "register result object field",
				Cause:       fmt.Errorf("field %s cannot have both name and group tags", field.Name),
			}
		}

		fieldDescriptor := d.clone()
		fieldDescriptor.Type = field.Type
		fieldDescriptor.Key = field.Key
		fieldDescriptor.Group = field.Group
		fieldDescriptor.resultFieldIndex = field.Index
		fieldDescriptors = append(fieldDescriptors, fieldDescriptor)
	}

	for _, fieldDescriptor := range fieldDescriptors {
		fieldDescriptor.siblings = fieldDescriptors
	}

	registered := make([]*descriptor, 0, len(fieldDescriptors))
	for _, fieldDescriptor := range fieldDescriptors {
		if err := r.registerDescriptor(fieldDescriptor); err != nil {
			// Roll back the fields registered so far: leaving them in place
			// would keep sibling links to never-registered descriptors,
			// corrupting primary detection and scoped caching for callers
			// that ignore the Add error.
			r.unregisterDescriptors(registered)
			return &RegistrationError{
				ServiceType: fieldDescriptor.Type,
				Operation:   "register result object field",
				Cause:       err,
			}
		}
		registered = append(registered, fieldDescriptor)
	}

	return nil
}

// registerMultiReturn registers each non-error return of a multi-return
// constructor as its own service, linking the descriptors as siblings.
// Returns handled=false when the constructor has at most one non-error
// return, in which case the caller proceeds with normal registration.
// Caller must hold r.mu.
func (r *collection) registerMultiReturn(d *descriptor, info *reflection.ConstructorInfo, options *addOptions) (bool, error) {
	if !info.IsFunc || len(info.Returns) <= 1 {
		return false, nil
	}

	// Filter out error returns to get actual service types
	nonErrorReturns := make([]reflection.ReturnInfo, 0)
	for _, ret := range info.Returns {
		if !ret.IsError {
			nonErrorReturns = append(nonErrorReturns, ret)
		}
	}

	if len(nonErrorReturns) <= 1 {
		return false, nil
	}

	// godi.As is ambiguous for multi-return constructors: it's unclear which
	// return value the interface should bind to. Reject explicitly rather
	// than silently dropping the option.
	if len(options.As) > 0 {
		return true, &RegistrationError{
			ServiceType: d.Type,
			Operation:   "register multi-return type",
			Cause:       fmt.Errorf("godi.As cannot be combined with a multi-return constructor; register a wrapper constructor that returns the desired interface"),
		}
	}

	typeDescriptors := make([]*descriptor, 0, len(nonErrorReturns))
	for i, ret := range nonErrorReturns {
		typeDescriptor := d.clone()
		typeDescriptor.Type = ret.Type
		typeDescriptor.MultiReturnIndex = ret.Index

		// Apply name/key only to the first return if specified
		typeDescriptor.Key = nil
		if options.key() != nil && i == 0 {
			typeDescriptor.Key = options.key()
		}

		typeDescriptors = append(typeDescriptors, typeDescriptor)
	}

	// Link the descriptors as siblings: one constructor invocation produces
	// every return value, so instance creation caches each of them under its
	// own registration (key or group).
	for _, typeDescriptor := range typeDescriptors {
		typeDescriptor.siblings = typeDescriptors
	}

	registered := make([]*descriptor, 0, len(typeDescriptors))
	for _, typeDescriptor := range typeDescriptors {
		if err := r.registerDescriptor(typeDescriptor); err != nil {
			// Roll back the returns registered so far (see
			// registerResultObjectFields for why phantom siblings are
			// harmful).
			r.unregisterDescriptors(registered)
			return true, &RegistrationError{
				ServiceType: typeDescriptor.Type,
				Operation:   "register multi-return type",
				Cause:       err,
			}
		}
		registered = append(registered, typeDescriptor)
	}

	return true, nil
}

// unregisterDescriptors removes descriptors that were registered earlier in
// a multi-descriptor registration whose later step failed, restoring the
// collection to its pre-call state so no phantom sibling links remain
// reachable. Caller must hold r.mu.
func (r *collection) unregisterDescriptors(batch []*descriptor) {
	if len(batch) == 0 {
		return
	}

	removed := make(map[*descriptor]struct{}, len(batch))
	for _, descriptor := range batch {
		removed[descriptor] = struct{}{}

		if descriptor.Group != "" {
			// Registered as a group member (key and group are mutually
			// exclusive at registration; the numeric key was assigned by
			// registerDescriptor).
			groupKey := groupID{Type: descriptor.Type, Group: descriptor.Group}
			members := r.groups[groupKey]
			kept := members[:0]
			for _, member := range members {
				if member != descriptor {
					kept = append(kept, member)
				}
			}
			if len(kept) == 0 {
				delete(r.groups, groupKey)
			} else {
				r.groups[groupKey] = kept
			}
			continue
		}

		key := registryKey{Type: descriptor.Type, Key: descriptor.Key}
		if r.services[key] == descriptor {
			delete(r.services, key)
		}
	}

	r.pruneDescriptors(removed)
}

// registerDescriptor registers a descriptor in the appropriate collections based on its type.
// Regular services are registered by type and key,
// and grouped services are registered in their respective groups.
func (r *collection) registerDescriptor(descriptor *descriptor) error {
	// Register based on type of service
	if descriptor.Key != nil || descriptor.Group == "" {
		key := registryKey{Type: descriptor.Type, Key: descriptor.Key}
		if _, exists := r.services[key]; exists {
			if descriptor.Key == nil {
				return &AlreadyRegisteredError{ServiceType: descriptor.Type}
			}
			return &RegistrationError{
				ServiceType: descriptor.Type,
				Operation:   "register",
				Cause:       &AlreadyRegisteredError{ServiceType: descriptor.Type},
			}
		}

		r.services[key] = descriptor
	} else {
		groupKey := groupID{Type: descriptor.Type, Group: descriptor.Group}
		r.groups[groupKey] = append(r.groups[groupKey], descriptor)

		// Set a numeric key for group members
		descriptor.Key = len(r.groups[groupKey])
		descriptor.syntheticKey = true
	}

	// Track in allDescriptors for efficient iteration
	r.allDescriptors = append(r.allDescriptors, descriptor)

	return nil
}

// validateLifetimes rejects singletons that capture a scoped service, either
// directly or through a chain of transients: the singleton would keep one
// scope's instance for the application's lifetime. Every conflict is
// reported, in registration order.
//
// Transients may depend on scoped services: resolved from a scope, they share
// that scope's instances. (Resolving them from the root provider is what
// ProviderOptions.ValidateScopes rejects.)
func validateLifetimes(all []*descriptor, services map[registryKey]*descriptor, groups map[groupID][]*descriptor) error {
	// scopedReach memoizes, per descriptor, the scoped service reachable
	// from it through transients only, and the transients on the way.
	type reach struct {
		scoped *descriptor
		via    []*descriptor
	}
	memo := make(map[*descriptor]*reach)
	var reachScoped func(d *descriptor) *reach
	reachScoped = func(d *descriptor) *reach {
		switch d.Lifetime {
		case Scoped:
			return &reach{scoped: d}
		case Transient:
			if r, ok := memo[d]; ok {
				return r
			}
			memo[d] = &reach{} // the graph is acyclic; this only guards re-entry
			// Constructing d also runs the decorators of its other outputs.
			for _, dep := range constructionDependencies(d) {
				for _, depDescriptor := range dependencyDescriptors(dep, services, groups) {
					if r := reachScoped(depDescriptor); r.scoped != nil {
						found := &reach{scoped: r.scoped, via: append([]*descriptor{d}, r.via...)}
						memo[d] = found
						return found
					}
				}
			}
			return memo[d]
		default:
			// A singleton dependency is validated as a singleton itself.
			return &reach{}
		}
	}

	var errs []error
	reported := make(map[any]struct{})
	for _, d := range all {
		if d == nil || d.Lifetime != Singleton {
			continue
		}
		// Sibling outputs of one constructor share its dependencies.
		fkey := flightKey(d)
		if _, done := reported[fkey]; done {
			continue
		}
	dependencies:
		for _, dep := range d.Dependencies {
			for _, depDescriptor := range dependencyDescriptors(dep, services, groups) {
				r := reachScoped(depDescriptor)
				if r.scoped == nil {
					continue
				}
				via := make([]reflect.Type, len(r.via))
				for i, transient := range r.via {
					via[i] = transient.Type
				}
				errs = append(errs, &LifetimeConflictError{
					ServiceType:        d.Type,
					ServiceLifetime:    Singleton,
					DependencyType:     r.scoped.Type,
					DependencyLifetime: Scoped,
					Via:                via,
				})
				reported[fkey] = struct{}{}
				break dependencies
			}
		}
	}
	return errors.Join(errs...)
}

// validateDependencies reports every required constructor dependency that has
// no registration, for all lifetimes. Without it, a scoped or transient
// service with a missing dependency would only fail at its first resolution.
// Optional and group dependencies may legitimately be empty, and reserved
// types (context.Context, Provider, Scope) are supplied by the container.
// dependencySource names the function that declared dep: the decorator that
// added it, else d's constructor.
func dependencySource(d *descriptor, dep *reflection.Dependency, decoratorSources map[*reflection.Dependency]string) string {
	if source, ok := decoratorSources[dep]; ok {
		return source
	}
	return d.source
}

func validateDependencies(all []*descriptor, services map[registryKey]*descriptor, decoratorSources map[*reflection.Dependency]string) error {
	var errs []error
	// Descriptors derived from one constructor (multi-return values, result
	// object fields, interface aliases) share its dependencies: report each
	// once. Decorator dependencies are per descriptor and checked as well.
	checked := make(map[*reflection.Dependency]struct{})
	for _, d := range all {
		if d == nil {
			continue
		}

		serviceType := d.Type
		if d.VoidReturn {
			serviceType = d.ConstructorType
		}
		for _, dep := range d.Dependencies {
			if dep == nil || dep.Optional || dep.Group != "" {
				continue
			}
			if _, done := checked[dep]; done {
				continue
			}
			checked[dep] = struct{}{}
			if dep.Key == nil {
				if _, reserved := reservedTypes[dep.Type]; reserved {
					continue
				}
			}
			if _, ok := services[registryKey{Type: dep.Type, Key: dep.Key}]; ok {
				continue
			}
			errs = append(errs, &MissingDependencyError{
				ServiceType:    serviceType,
				DependencyType: dep.Type,
				DependencyKey:  dep.Key,
				Constructor:    dependencySource(d, dep, decoratorSources),
			})
		}
	}
	return errors.Join(errs...)
}

// Validate checks a collection's wiring without constructing anything:
// registration errors, missing dependencies, dependency cycles, lifetime
// conflicts, and decorators that match no registration — everything Build
// checks before it creates singletons. Use it in tests so they don't need
// the infrastructure (databases, servers) that singleton constructors open.
func Validate(c Collection) error {
	_, err := c.impl().plan(context.Background())
	return err
}
