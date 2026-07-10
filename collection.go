package godi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/junioryono/godi/v5/internal/graph"
	"github.com/junioryono/godi/v5/internal/reflection"
)

// Global atomic counter for fast ID generation (replaces UUID)
var providerIDCounter atomic.Uint64

// Collection represents a collection of service descriptors that define
// the services available in the dependency injection container.
//
// Collection follows a builder pattern where services are registered
// with their lifetimes and dependencies, then built into a Provider.
//
// Collection is NOT thread-safe. It should be configured in a single
// goroutine before building the Provider.
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
	services map[TypeKey]*descriptor

	// groups stores services that belong to groups
	groups map[GroupKey][]*descriptor

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
}

// TypeKey uniquely identifies a keyed service
type TypeKey struct {
	Type reflect.Type
	Key  any
}

// GroupKey uniquely identifies a group of services
type GroupKey struct {
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

// NewCollection creates a new empty Collection instance.
//
// Example:
//
//	collection := godi.NewCollection()
//	collection.AddSingleton(NewLogger)
//	provider, err := collection.Build()
func NewCollection() Collection {
	return &collection{
		services:       make(map[TypeKey]*descriptor, 16), // Pre-size for typical usage
		groups:         make(map[GroupKey][]*descriptor, 4),
		allDescriptors: make([]*descriptor, 0, 16),
		analyzer:       reflection.New(),
	}
}

// Build creates a Provider from the registered services using default options.
func (sc *collection) Build() (Provider, error) {
	return sc.BuildWithContext(context.Background())
}

// BuildWithContext creates a Provider with the given cooperative build context.
// The context is available to eager constructors that depend on context.Context
// and is checked throughout construction.
func (sc *collection) BuildWithContext(ctx context.Context) (Provider, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return sc.doBuild(ctx)
}

// BuildWithOptions creates a Provider with custom options for validation and behavior configuration.
func (sc *collection) BuildWithOptions(options *ProviderOptions) (Provider, error) {
	ctx := context.Background()

	// Handle build timeout if specified
	if options != nil && options.BuildTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, options.BuildTimeout)
		defer cancel()
	}

	return sc.doBuild(ctx)
}

func (sc *collection) doBuild(ctx context.Context) (Provider, error) {
	// Check context before starting
	select {
	case <-ctx.Done():
		return nil, &BuildError{
			Phase:   "initialization",
			Details: "build cancelled before starting",
			Cause:   ctx.Err(),
		}
	default:
	}

	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Surface every recorded registration error before doing any work:
	// the Add* methods defer their errors to Build so callers can register
	// services without per-call error checks.
	if len(sc.errs) > 0 {
		return nil, &BuildError{
			Phase:   "registration",
			Details: "one or more service registrations failed",
			Cause:   errors.Join(sc.errs...),
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

	// Phase 1: Build dependency graph (validates cycles as part of build)
	select {
	case <-ctx.Done():
		return nil, &BuildError{
			Phase:   "graph",
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
				Phase:   "graph",
				Details: fmt.Sprintf("failed to add provider %v", formatType(descriptor.Type)),
				Cause:   err,
			}
		}
	}

	// Phase 1.5: Resolve group dependencies
	// Connect group consumers to actual group member nodes in the graph.
	// Without this, group consumers depend on phantom nodes (Key=nil) that
	// don't match the real group members (Key=1,2,...), causing incorrect
	// topological ordering and ErrSingletonNotInitialized during build.
	g.ResolveGroupDependencies()

	// Phase 2: Validate graph (cycles detected here, not per-add)
	if err := g.DetectCycles(); err != nil {
		return nil, &BuildError{
			Phase:   "validation",
			Details: "dependency graph validation failed",
			Cause:   err,
		}
	}

	// Phase 3: Validate lifetimes
	select {
	case <-ctx.Done():
		return nil, &BuildError{
			Phase:   "validation",
			Details: "build cancelled during lifetime validation",
			Cause:   ctx.Err(),
		}
	default:
	}

	if err := sc.validateLifetimes(); err != nil {
		return nil, &BuildError{
			Phase:   "validation",
			Details: "lifetime validation failed",
			Cause:   err,
		}
	}

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
		graph:                       g,
		analyzer:                    sc.analyzer, // Share analyzer from collection
		singletonKeys:               make([]instanceKey, 0, len(allDescriptors)),
		voidReturnScopedDescriptors: make([]*descriptor, 0, voidCount),
		disposables:                 make([]Disposable, 0, 4),
		disposableSet:               make(map[disposableIdentity]struct{}, 4),
		scopes:                      make(map[*scope]struct{}, 4),
		closeDone:                   make(chan struct{}),
	}

	for _, descriptor := range allDescriptors {
		if descriptor != nil && descriptor.Lifetime == Scoped && descriptor.VoidReturn {
			p.voidReturnScopedDescriptors = append(p.voidReturnScopedDescriptors, descriptor)
		}
	}

	// Phase 5: Create root scope
	select {
	case <-ctx.Done():
		return nil, &BuildError{
			Phase:   "scope-creation",
			Details: "build cancelled during root scope creation",
			Cause:   ctx.Err(),
		}
	default:
	}

	var err error
	rootCtx := context.Background()
	p.rootScope, err = newUninitializedScope(p, nil, rootCtx, nil)
	if err != nil {
		return nil, &BuildError{
			Phase:   "scope-creation",
			Details: "failed to create root scope",
			Cause:   err,
		}
	}

	// Phase 6: Create singletons with context propagation. Decorate the build
	// context so FromContext works inside eager constructors, then clear the
	// atomic override before returning the provider.
	buildCtx := context.WithValue(ctx, scopeContextKey{}, p.rootScope)
	p.rootScope.constructionContext.Store(&scopeConstructionContext{context: buildCtx})
	defer func() {
		p.rootScope.constructionContext.Store(nil)
	}()

	if err := p.createAllSingletonsWithContext(ctx); err != nil {
		buildErr := &BuildError{
			Phase:   "singleton-creation",
			Details: "failed to initialize singletons",
			Cause:   err,
		}
		return nil, joinBuildCleanupError(buildErr, p.Close())
	}

	// Phase 7: Initialize root-scoped side-effect constructors only after all
	// singletons exist. Request/child scopes still initialize them in newScope.
	if err := p.rootScope.initializeScopedServices(); err != nil {
		buildErr := &BuildError{
			Phase:   "scope-initialization",
			Details: "failed to initialize root scoped services",
			Cause:   err,
		}
		return nil, joinBuildCleanupError(buildErr, p.Close())
	}
	if err := ctx.Err(); err != nil {
		buildErr := &BuildError{
			Phase:   "scope-initialization",
			Details: "build deadline expired after root scope initialization",
			Cause:   err,
		}
		return nil, joinBuildCleanupError(buildErr, p.Close())
	}

	return p, nil
}

func joinBuildCleanupError(buildErr, closeErr error) error {
	if closeErr == nil {
		return buildErr
	}
	return errors.Join(
		buildErr,
		&BuildError{
			Phase:   "cleanup",
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

	typeKey := TypeKey{Type: t}
	_, ok := r.services[typeKey]
	return ok
}

// ContainsKeyed checks if a keyed service exists
func (r *collection) ContainsKeyed(t reflect.Type, key any) bool {
	if t == nil {
		return false
	}
	if key != nil && !reflect.TypeOf(key).Comparable() {
		return false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	typeKey := TypeKey{Type: t, Key: key}
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

	groupKey := GroupKey{Type: t, Group: group}
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
	if key != nil && !reflect.TypeOf(key).Comparable() {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	typeKey := TypeKey{Type: t, Key: key}
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
		result = append(result, ServiceInfo{
			ServiceType: d.Type,
			Key:         d.Key,
			Group:       d.Group,
			Lifetime:    d.Lifetime,
		})
	}
	return result
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
	if info == nil {
		// Defensive fallback: a descriptor constructed outside the normal
		// path won't have info stashed. Re-analyze in that case.
		var err error
		info, err = r.analyzer.Analyze(service)
		if err != nil {
			return &ReflectionAnalysisError{
				Constructor: service,
				Operation:   "analyze",
				Cause:       err,
			}
		}
	}

	// Handle result objects (Out structs)
	if info.IsResultObject {
		if options.Name != "" || options.Group != "" {
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
	services map[TypeKey]*descriptor,
	groups map[GroupKey][]*descriptor,
) (
	snapshotAll []*descriptor,
	snapshotServices map[TypeKey]*descriptor,
	snapshotGroups map[GroupKey][]*descriptor,
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

	snapshotServices = make(map[TypeKey]*descriptor, len(services))
	for key, original := range services {
		if clone, ok := clones[original]; ok {
			snapshotServices[key] = clone
		}
	}

	snapshotGroups = make(map[GroupKey][]*descriptor, len(groups))
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
		if options.Name != "" && i == 0 {
			typeDescriptor.Key = options.Name
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
			groupKey := GroupKey{Type: descriptor.Type, Group: descriptor.Group}
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

		key := TypeKey{Type: descriptor.Type, Key: descriptor.Key}
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
		key := TypeKey{Type: descriptor.Type, Key: descriptor.Key}
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
		groupKey := GroupKey{Type: descriptor.Type, Group: descriptor.Group}
		r.groups[groupKey] = append(r.groups[groupKey], descriptor)

		// Set a numeric key for group members
		descriptor.Key = len(r.groups[groupKey])
	}

	// Track in allDescriptors for efficient iteration
	r.allDescriptors = append(r.allDescriptors, descriptor)

	return nil
}

// validateLifetimes ensures singleton and transient services don't depend on scoped services.
// This validation prevents runtime errors where:
// - A singleton (created once) would incorrectly hold a reference to a scoped service
// - A transient (created per request) could outlive and hold a reference to a disposed scoped service
func (c *collection) validateLifetimes() error {
	// Create a map of service lifetimes
	lifetimes := make(map[instanceKey]Lifetime)

	// Populate lifetimes from all services
	for serviceType, descriptor := range c.services {
		if descriptor != nil {
			key := instanceKey{Type: serviceType.Type, Key: descriptor.Key}
			lifetimes[key] = descriptor.Lifetime
		}
	}

	for groupKey, descriptors := range c.groups {
		for _, descriptor := range descriptors {
			if descriptor != nil {
				key := instanceKey{Type: groupKey.Type, Key: descriptor.Key, Group: groupKey.Group}
				lifetimes[key] = descriptor.Lifetime
			}
		}
	}

	checkDescriptor := func(descriptor *descriptor) error {
		if descriptor == nil {
			return nil
		}

		// Skip scoped services - they can depend on anything
		if descriptor.Lifetime == Scoped {
			return nil
		}

		// Both Singleton and Transient cannot depend on Scoped
		for _, dep := range descriptor.Dependencies {
			if dep == nil {
				continue
			}

			// Group dependencies have Key=nil but group members have numeric keys.
			// Check each member's lifetime individually.
			if dep.Group != "" && dep.Key == nil {
				groupKey := GroupKey{Type: dep.Type, Group: dep.Group}
				for _, memberDesc := range c.groups[groupKey] {
					if memberDesc != nil && memberDesc.Lifetime == Scoped {
						return &LifetimeConflictError{
							ServiceType:        descriptor.Type,
							ServiceLifetime:    descriptor.Lifetime,
							DependencyType:     dep.Type,
							DependencyLifetime: Scoped,
						}
					}
				}
				continue
			}

			depKey := instanceKey{Type: dep.Type, Key: dep.Key, Group: dep.Group}
			depLifetime, ok := lifetimes[depKey]
			if !ok {
				continue
			}

			if depLifetime == Scoped {
				return &LifetimeConflictError{
					ServiceType:        descriptor.Type,
					ServiceLifetime:    descriptor.Lifetime,
					DependencyType:     dep.Type,
					DependencyLifetime: depLifetime,
				}
			}
		}

		return nil
	}

	// Check all services
	for _, descriptor := range c.services {
		if err := checkDescriptor(descriptor); err != nil {
			return err
		}
	}

	for _, descriptors := range c.groups {
		for _, descriptor := range descriptors {
			if err := checkDescriptor(descriptor); err != nil {
				return err
			}
		}
	}

	return nil
}
