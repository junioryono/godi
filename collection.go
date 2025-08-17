package godi

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	"github.com/google/uuid"
	"github.com/junioryono/godi/v4/internal/graph"
	"github.com/junioryono/godi/v4/internal/reflection"
)

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

	// BuildWithOptions creates a Provider with custom options
	// for validation and behavior configuration.
	BuildWithOptions(options *ProviderOptions) (Provider, error)

	// AddModules applies one or more module configurations to the service collection.
	// Modules provide a way to group related service registrations.
	AddModules(modules ...ModuleOption) error

	// AddSingleton registers a service with singleton lifetime.
	// Only one instance is created and shared across all resolutions.
	AddSingleton(service any, opts ...AddOption) error

	// AddScoped registers a service with scoped lifetime.
	// One instance is created per scope and shared within that scope.
	AddScoped(service any, opts ...AddOption) error

	// AddTransient registers a service with transient lifetime.
	// A new instance is created every time the service is resolved.
	AddTransient(service any, opts ...AddOption) error

	// HasService checks if a service exists for the type.
	HasService(serviceType reflect.Type) bool

	// HasKeyedService checks if a keyed service exists.
	HasKeyedService(serviceType reflect.Type, key any) bool

	// Remove removes all services for a given service type.
	Remove(serviceType reflect.Type)

	// RemoveKeyed removes a specific keyed service.
	RemoveKeyed(serviceType reflect.Type, key any)

	// ToSlice returns a copy of all registered service descriptors.
	// This is useful for inspection and debugging.
	ToSlice() []*Descriptor

	// Count returns the number of registered services.
	Count() int
}

// Collection is the core service registry that manages services.
type collection struct {
	mu sync.RWMutex

	// services stores all non-keyed services by type
	services map[TypeKey]*Descriptor

	// groups stores services that belong to groups
	groups map[GroupKey][]*Descriptor
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

// NewCollection creates a new empty Collection instance.
//
// Example:
//
//	collection := godi.NewCollection()
//	collection.AddSingleton(NewLogger)
//	provider, err := collection.Build()
func NewCollection() Collection {
	return &collection{
		services: make(map[TypeKey]*Descriptor),
		groups:   make(map[GroupKey][]*Descriptor),
	}
}

// Build creates a Provider from the registered services using default options.
func (sc *collection) Build() (Provider, error) {
	return sc.BuildWithOptions(nil)
}

// BuildWithOptions creates a Provider with custom options for validation and behavior configuration.
func (sc *collection) BuildWithOptions(options *ProviderOptions) (Provider, error) {
	// Handle build timeout if specified
	if options != nil && options.BuildTimeout > 0 {
		ctx, cancel := context.WithTimeout(context.Background(), options.BuildTimeout)
		defer cancel()

		done := make(chan struct{})
		var buildErr error
		var provider Provider

		go func() {
			provider, buildErr = sc.doBuild()
			close(done)
		}()

		select {
		case <-ctx.Done():
			return nil, &TimeoutError{
				ServiceType: nil,
				Timeout:     options.BuildTimeout,
			}
		case <-done:
			return provider, buildErr
		}
	}

	return sc.doBuild()
}

func (sc *collection) doBuild() (Provider, error) {
	// Get all descriptors before locking to avoid deadlock
	allDescriptors := sc.ToSlice()

	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Phase 1: Validate dependency graph
	if err := sc.validateDependencyGraph(); err != nil {
		return nil, &BuildError{
			Phase:   "validation",
			Details: "dependency graph validation failed",
			Cause:   err,
		}
	}

	// Phase 2: Validate lifetimes
	if err := sc.validateLifetimes(); err != nil {
		return nil, &BuildError{
			Phase:   "validation",
			Details: "lifetime validation failed",
			Cause:   err,
		}
	}

	// Phase 3: Build dependency graph
	g := graph.NewDependencyGraph()

	for _, descriptor := range allDescriptors {
		if descriptor == nil {
			continue // Skip nil descriptors gracefully
		}

		if err := g.AddProvider(descriptor); err != nil {
			return nil, &BuildError{
				Phase:   "graph",
				Details: fmt.Sprintf("failed to add provider %v", formatType(descriptor.Type)),
				Cause:   err,
			}
		}
	}

	// Phase 4: Create provider
	p := &provider{
		id:          uuid.NewString(),
		services:    sc.services,
		groups:      sc.groups,
		graph:       g,
		analyzer:    reflection.New(),
		singletons:  make(map[instanceKey]any),
		disposables: make([]Disposable, 0),
		scopes:      make(map[*scope]struct{}),
	}

	// Phase 5: Create root scope
	rootCtx := context.Background()
	p.rootScope = newScope(p, nil, rootCtx, nil)

	// Phase 6: Create singletons
	if err := p.createAllSingletons(); err != nil {
		// Clean up partially created provider
		if err = p.Close(); err != nil {
			return nil, &BuildError{
				Phase:   "cleanup",
				Details: "failed to clean up partially created provider",
				Cause:   err,
			}
		}

		return nil, &BuildError{
			Phase:   "singleton-creation",
			Details: "failed to initialize singletons",
			Cause:   err,
		}
	}

	return p, nil
}

// AddModules applies one or more module configurations to the service collection.
func (sc *collection) AddModules(modules ...ModuleOption) error {
	for _, module := range modules {
		if module == nil {
			continue
		}

		if err := module(sc); err != nil {
			return err
		}
	}

	return nil
}

// AddSingleton adds a singleton service to the collection.
func (sc *collection) AddSingleton(service any, opts ...AddOption) error {
	return sc.addService(service, Singleton, opts...)
}

// AddScoped adds a scoped service to the collection.
func (sc *collection) AddScoped(service any, opts ...AddOption) error {
	return sc.addService(service, Scoped, opts...)
}

// AddTransient adds a transient service to the collection.
func (sc *collection) AddTransient(service any, opts ...AddOption) error {
	return sc.addService(service, Transient, opts...)
}

// HasService checks if a service exists for the type
func (r *collection) HasService(t reflect.Type) bool {
	if t == nil {
		return false
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	typeKey := TypeKey{Type: t}
	_, ok := r.services[typeKey]
	return ok
}

// HasKeyedService checks if a keyed service exists
func (r *collection) HasKeyedService(t reflect.Type, key any) bool {
	if t == nil {
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

// Remove removes all services for a given type
func (r *collection) Remove(t reflect.Type) {
	if t == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	typeKey := TypeKey{Type: t}
	delete(r.services, typeKey)
}

// RemoveKeyed removes a specific keyed service
func (r *collection) RemoveKeyed(t reflect.Type, key any) {
	if t == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	typeKey := TypeKey{Type: t, Key: key}
	delete(r.services, typeKey)
}

// ToSlice returns a copy of all registered service descriptors
func (r *collection) ToSlice() []*Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Use a map to track unique descriptors and avoid duplicates
	seen := make(map[*Descriptor]bool)
	descriptors := make([]*Descriptor, 0)

	// Add regular services
	for _, service := range r.services {
		if service != nil && !seen[service] {
			descriptors = append(descriptors, service)
			seen[service] = true
		}
	}

	// Add grouped services
	for _, groupServices := range r.groups {
		for _, service := range groupServices {
			if service != nil && !seen[service] {
				descriptors = append(descriptors, service)
				seen[service] = true
			}
		}
	}

	return descriptors
}

// Count returns the number of registered services in the collection.
func (r *collection) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Use a map to track unique descriptors and avoid duplicates
	seen := make(map[*Descriptor]bool)

	// Count regular services
	for _, service := range r.services {
		if service != nil {
			seen[service] = true
		}
	}

	// Count grouped services
	for _, groupServices := range r.groups {
		for _, service := range groupServices {
			if service != nil {
				seen[service] = true
			}
		}
	}

	return len(seen)
}

var (
	// Reserved types that are handled specially by the framework
	reservedTypes = map[reflect.Type]struct{}{
		reflect.TypeOf((*context.Context)(nil)).Elem(): {},
		reflect.TypeOf((*Provider)(nil)).Elem():        {},
		reflect.TypeOf((*Scope)(nil)).Elem():           {},
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

	// Create descriptor from constructor
	descriptor, err := newDescriptor(service, lifetime, opts...)
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

	// Parse options to handle special registration cases
	options := &addOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt.applyAddOption(options)
		}
	}

	// Validate options
	if optErr := options.Validate(); optErr != nil {
		return &RegistrationError{
			ServiceType: descriptor.Type,
			Operation:   "validate options",
			Cause:       optErr,
		}
	}

	// Check if this is a multi-return constructor (not an Out struct)
	analyzer := reflection.New()
	info, err := analyzer.Analyze(service)
	if err != nil {
		return &ReflectionAnalysisError{
			Constructor: service,
			Operation:   "analyze",
			Cause:       err,
		}
	}

	// Handle multiple return types (not Out structs)
	if info.IsFunc && len(info.Returns) > 1 && !info.IsResultObject {
		// Filter out error returns to get actual service types
		nonErrorReturns := make([]reflection.ReturnInfo, 0)
		for _, ret := range info.Returns {
			if !ret.IsError {
				nonErrorReturns = append(nonErrorReturns, ret)
			}
		}

		// If we have multiple non-error returns, register each as a separate service
		if len(nonErrorReturns) > 1 {
			for i, ret := range nonErrorReturns {
				// Create a descriptor for each return type
				typeDescriptor := &Descriptor{
					Type:            ret.Type,
					Lifetime:        descriptor.Lifetime,
					Constructor:     descriptor.Constructor,
					ConstructorType: descriptor.ConstructorType,
					Dependencies:    descriptor.Dependencies,
					Group:           descriptor.Group,
					As:              descriptor.As,
					IsInstance:      false,
					ReturnIndex:     ret.Index,
					IsMultiReturn:   true,
					isFunc:          descriptor.isFunc,
					isParamObject:   descriptor.isParamObject,
					paramFields:     descriptor.paramFields,
				}

				// Apply name/key only to the first return if specified
				if options.Name != "" && i == 0 {
					typeDescriptor.Key = options.Name
				} else if options.Name != "" {
					// For subsequent returns, leave them unkeyed
					typeDescriptor.Key = nil
				}

				// Register each type descriptor
				if err := r.registerDescriptor(typeDescriptor); err != nil {
					return &RegistrationError{
						ServiceType: ret.Type,
						Operation:   "register multi-return type",
						Cause:       err,
					}
				}
			}
			return nil
		}
	}

	// Handle As option - register under interface types
	if len(options.As) > 0 {
		// When As is specified, register the service under each interface type
		for _, iface := range options.As {
			interfaceType := reflect.TypeOf(iface).Elem()

			// Validate that the service type implements the interface
			if !descriptor.Type.Implements(interfaceType) && !reflect.PointerTo(descriptor.Type).Implements(interfaceType) {
				return &TypeMismatchError{
					Expected: interfaceType,
					Actual:   descriptor.Type,
					Context:  "interface implementation",
				}
			}

			// Create a new descriptor for the interface type
			interfaceDescriptor := &Descriptor{
				Type:            interfaceType,
				Key:             descriptor.Key,
				Lifetime:        descriptor.Lifetime,
				Constructor:     descriptor.Constructor,
				ConstructorType: descriptor.ConstructorType,
				Dependencies:    descriptor.Dependencies,
				Group:           descriptor.Group,
				As:              options.As,
				IsInstance:      descriptor.IsInstance,
				Instance:        descriptor.Instance,
				ReturnIndex:     descriptor.ReturnIndex,
				IsMultiReturn:   descriptor.IsMultiReturn,
				isFunc:          descriptor.isFunc,
				isResultObject:  descriptor.isResultObject,
				resultFields:    descriptor.resultFields,
				isParamObject:   descriptor.isParamObject,
				paramFields:     descriptor.paramFields,
			}

			// Register the interface descriptor
			if err := r.registerDescriptor(interfaceDescriptor); err != nil {
				return &RegistrationError{
					ServiceType: interfaceType,
					Operation:   "register as interface",
					Cause:       err,
				}
			}
		}

		// If As is specified, we only register under interface types, not the concrete type
		return nil
	}

	// Register the descriptor normally
	return r.registerDescriptor(descriptor)
}

// registerDescriptor registers a descriptor in the appropriate collections based on its type.
// Regular services are registered by type and key,
// and grouped services are registered in their respective groups.
func (r *collection) registerDescriptor(descriptor *Descriptor) error {
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

	return nil
}

// validateDependencyGraph validates the entire dependency graph for cycles.
// It builds a graph of all service dependencies and checks for circular dependencies
// that would prevent successful resolution at runtime.
func (r *collection) validateDependencyGraph() error {
	// Build the dependency graph
	g := graph.NewDependencyGraph()

	// Add all providers to the graph
	for _, descriptor := range r.services {
		if descriptor != nil {
			if err := g.AddProvider(descriptor); err != nil {
				return &GraphOperationError{
					Operation: "add",
					NodeType:  descriptor.Type,
					NodeKey:   descriptor.Key,
					Cause:     err,
				}
			}
		}
	}

	for _, descriptors := range r.groups {
		for _, descriptor := range descriptors {
			if descriptor != nil {
				if err := g.AddProvider(descriptor); err != nil {
					return &GraphOperationError{
						Operation: "add",
						NodeType:  descriptor.Type,
						NodeKey:   descriptor.Key,
						Cause:     err,
					}
				}
			}
		}
	}

	// Check for cycles
	if err := g.DetectCycles(); err != nil {
		return err
	}

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

	checkDescriptor := func(descriptor *Descriptor) error {
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

			depKey := instanceKey{Type: dep.Type, Key: dep.Key, Group: dep.Group}
			depLifetime, ok := lifetimes[depKey]
			if !ok {
				continue
			}

			if depLifetime == Scoped {
				return &LifetimeConflictError{
					ServiceType: descriptor.Type,
					Current:     descriptor.Lifetime,
					Requested:   depLifetime,
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
