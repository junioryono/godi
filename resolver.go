package godi

import (
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/junioryono/godi/v3/internal/reflection"
	"github.com/junioryono/godi/v3/internal/registry"
)

// ResolutionContext defines the interface for tracking resolution state
type ResolutionContext interface {
	// GetScopeID returns the current scope ID
	GetScopeID() string

	// GetLifetime returns the current lifetime context
	GetLifetime() registry.ServiceLifetime

	// SetLifetime sets the current lifetime context
	SetLifetime(lifetime registry.ServiceLifetime)

	// GetDepth returns the current resolution depth
	GetDepth() int

	// IncrementDepth increments and returns the new depth
	IncrementDepth() int

	// DecrementDepth decrements the depth
	DecrementDepth()

	// IsResolving checks if a service is currently being resolved
	IsResolving(serviceType reflect.Type, key any) bool

	// StartResolving marks a service as being resolved
	StartResolving(serviceType reflect.Type, key any) error

	// StopResolving marks a service as no longer being resolved
	StopResolving(serviceType reflect.Type, key any)

	// GetInstance retrieves a cached instance from this resolution
	GetInstance(serviceType reflect.Type, key any) (any, bool)

	// SetInstance caches an instance for this resolution
	SetInstance(serviceType reflect.Type, key any, instance any)

	// GetResolver returns the parent resolver
	GetResolver() *Resolver

	// Clear cleans up the context (called on error paths)
	Clear()
}

// resolutionContext is the default implementation of ResolutionContext
type resolutionContext struct {
	// Scope information
	scopeID  string
	lifetime registry.ServiceLifetime

	// Resolution tracking
	resolving   map[resolutionKey]bool
	resolvingMu sync.RWMutex
	depth       int
	depthMu     sync.Mutex

	// Instance cache for this resolution
	instances   map[resolutionKey]any
	instancesMu sync.RWMutex

	// Parent resolver
	resolver *Resolver
}

// Resolver is the main dependency resolution engine.
type Resolver struct {
	registry *registry.ServiceCollection
	graph    *DependencyGraph
	analyzer *reflection.Analyzer
	cache    *InstanceCache

	// Components for resolution
	invoker   *reflection.ConstructorInvoker
	builder   *reflection.ParamObjectBuilder
	processor *reflection.ResultObjectProcessor
	matcher   *reflection.DependencyMatcher

	// Decorator processor
	decoratorProcessor *DecoratorProcessor

	// Options
	options *ResolverOptions

	// Mutex for thread safety
	mu sync.RWMutex
}

// ResolverOptions configures the resolver behavior.
type ResolverOptions struct {
	// EnableValidation performs constructor validation before resolution
	EnableValidation bool

	// EnableCaching controls instance caching
	EnableCaching bool

	// MaxResolutionDepth prevents infinite recursion
	MaxResolutionDepth int

	// OnResolved is called after successful resolution
	OnResolved func(serviceType reflect.Type, instance any, duration time.Duration)

	// OnError is called when resolution fails
	OnError func(serviceType reflect.Type, err error)
}

// DefaultOptions returns default resolver options.
func DefaultOptions() *ResolverOptions {
	return &ResolverOptions{
		EnableValidation:   true,
		EnableCaching:      true,
		MaxResolutionDepth: 100,
	}
}

// NewResolver creates a new resolver with the given components.
func NewResolver(
	reg *registry.ServiceCollection,
	g *DependencyGraph,
	analyzer *reflection.Analyzer,
	options *ResolverOptions,
) *Resolver {
	// Validate required components
	if reg == nil {
		panic("registry cannot be nil")
	}
	if g == nil {
		panic("graph cannot be nil")
	}
	if analyzer == nil {
		panic("analyzer cannot be nil")
	}

	if options == nil {
		options = DefaultOptions()
	}

	r := &Resolver{
		registry:           reg,
		graph:              g,
		analyzer:           analyzer,
		cache:              NewInstanceCache(),
		options:            options,
		invoker:            reflection.NewConstructorInvoker(analyzer),
		builder:            reflection.NewParamObjectBuilder(analyzer),
		processor:          reflection.NewResultObjectProcessor(analyzer),
		matcher:            reflection.NewMatcher(),
		decoratorProcessor: NewDecoratorProcessor(reg, analyzer),
	}

	return r
}

// resolutionKey uniquely identifies a service being resolved.
type resolutionKey struct {
	Type reflect.Type
	Key  any
}

// Implementation of ResolutionContext interface

func (c *resolutionContext) GetScopeID() string {
	return c.scopeID
}

func (c *resolutionContext) GetLifetime() registry.ServiceLifetime {
	return c.lifetime
}

func (c *resolutionContext) SetLifetime(lifetime registry.ServiceLifetime) {
	c.lifetime = lifetime
}

func (c *resolutionContext) GetDepth() int {
	c.depthMu.Lock()
	defer c.depthMu.Unlock()
	return c.depth
}

func (c *resolutionContext) IncrementDepth() int {
	c.depthMu.Lock()
	defer c.depthMu.Unlock()
	c.depth++
	return c.depth
}

func (c *resolutionContext) DecrementDepth() {
	c.depthMu.Lock()
	defer c.depthMu.Unlock()
	if c.depth > 0 {
		c.depth--
	}
}

func (c *resolutionContext) IsResolving(serviceType reflect.Type, key any) bool {
	c.resolvingMu.RLock()
	defer c.resolvingMu.RUnlock()

	resKey := resolutionKey{Type: serviceType, Key: key}
	return c.resolving[resKey]
}

func (c *resolutionContext) StartResolving(serviceType reflect.Type, key any) error {
	c.resolvingMu.Lock()
	defer c.resolvingMu.Unlock()

	resKey := resolutionKey{Type: serviceType, Key: key}
	if c.resolving[resKey] {
		return fmt.Errorf("circular dependency detected for %v", serviceType)
	}

	c.resolving[resKey] = true
	return nil
}

func (c *resolutionContext) StopResolving(serviceType reflect.Type, key any) {
	c.resolvingMu.Lock()
	defer c.resolvingMu.Unlock()

	resKey := resolutionKey{Type: serviceType, Key: key}
	delete(c.resolving, resKey)
}

func (c *resolutionContext) GetInstance(serviceType reflect.Type, key any) (any, bool) {
	c.instancesMu.RLock()
	defer c.instancesMu.RUnlock()

	resKey := resolutionKey{Type: serviceType, Key: key}
	instance, ok := c.instances[resKey]
	return instance, ok
}

func (c *resolutionContext) SetInstance(serviceType reflect.Type, key any, instance any) {
	c.instancesMu.Lock()
	defer c.instancesMu.Unlock()

	resKey := resolutionKey{Type: serviceType, Key: key}
	c.instances[resKey] = instance
}

func (c *resolutionContext) GetResolver() *Resolver {
	return c.resolver
}

func (c *resolutionContext) Clear() {
	c.resolvingMu.Lock()
	c.resolving = make(map[resolutionKey]bool)
	c.resolvingMu.Unlock()

	c.instancesMu.Lock()
	c.instances = make(map[resolutionKey]any)
	c.instancesMu.Unlock()

	c.depthMu.Lock()
	c.depth = 0
	c.depthMu.Unlock()
}

// Resolve resolves a service of the given type.
func (r *Resolver) Resolve(serviceType reflect.Type, scopeID string) (any, error) {
	if serviceType == nil {
		return nil, fmt.Errorf("service type cannot be nil")
	}

	ctx := r.createContext(scopeID)
	defer ctx.Clear() // Clean up on any exit path

	return r.resolveWithContext(ctx, serviceType, nil)
}

// ResolveKeyed resolves a keyed service.
func (r *Resolver) ResolveKeyed(serviceType reflect.Type, key any, scopeID string) (any, error) {
	if serviceType == nil {
		return nil, fmt.Errorf("service type cannot be nil")
	}

	if key == nil {
		return nil, fmt.Errorf("key cannot be nil for keyed service")
	}

	ctx := r.createContext(scopeID)
	defer ctx.Clear() // Clean up on any exit path

	return r.resolveWithContext(ctx, serviceType, key)
}

// ResolveGroup resolves all services in a group.
func (r *Resolver) ResolveGroup(serviceType reflect.Type, group string, scopeID string) ([]any, error) {
	if serviceType == nil {
		return nil, fmt.Errorf("service type cannot be nil")
	}

	if group == "" {
		return nil, fmt.Errorf("group name cannot be empty")
	}

	ctx := r.createContext(scopeID)
	defer ctx.Clear() // Clean up on any exit path

	return r.resolveGroupWithContext(ctx, serviceType, group)
}

// ResolveAll resolves all registered services (for eager initialization).
func (r *Resolver) ResolveAll(scopeID string) error {
	r.mu.RLock()
	providers := r.registry.GetAllProviders()
	r.mu.RUnlock()

	ctx := r.createContext(scopeID)
	defer ctx.Clear() // Clean up on any exit path

	// Get topological order from graph
	sorted, err := r.graph.TopologicalSort()
	if err != nil {
		return fmt.Errorf("failed to sort dependencies: %w", err)
	}

	// Resolve in dependency order
	for _, node := range sorted {
		if node.Provider != nil {
			// Force resolution through the normal path which will handle callbacks
			if node.Provider.Key != nil {
				_, err = r.ResolveKeyed(node.Provider.Type, node.Provider.Key, scopeID)
			} else {
				_, err = r.Resolve(node.Provider.Type, scopeID)
			}

			if err != nil && r.options.OnError != nil {
				r.options.OnError(node.Provider.Type, err)
			}
		}
	}

	// Resolve any providers not in the graph
	for _, provider := range providers {
		if _, exists := ctx.GetInstance(provider.Type, provider.Key); !exists {
			_, err := r.resolveProvider(ctx, provider)
			if err != nil && r.options.OnError != nil {
				r.options.OnError(provider.Type, err)
			}
		}
	}

	return nil
}

// createContext creates a new resolution context.
func (r *Resolver) createContext(scopeID string) ResolutionContext {
	return &resolutionContext{
		scopeID:   scopeID,
		resolving: make(map[resolutionKey]bool),
		instances: make(map[resolutionKey]any),
		resolver:  r,
	}
}

// resolveWithContext performs the actual resolution.
func (r *Resolver) resolveWithContext(ctx ResolutionContext, serviceType reflect.Type, key any) (any, error) {
	// Check recursion depth
	depth := ctx.IncrementDepth()
	defer ctx.DecrementDepth()

	if r.options.MaxResolutionDepth > 0 && depth > r.options.MaxResolutionDepth {
		return nil, &MaxDepthError{
			ServiceType: serviceType,
			Depth:       depth,
			MaxDepth:    r.options.MaxResolutionDepth,
		}
	}

	// Check for circular dependency
	if err := ctx.StartResolving(serviceType, key); err != nil {
		return nil, &CircularDependencyError{
			ServiceType: serviceType,
			Key:         key,
		}
	}
	defer ctx.StopResolving(serviceType, key)

	// Check context cache first
	if instance, ok := ctx.GetInstance(serviceType, key); ok {
		return instance, nil
	}

	// Check global cache based on lifetime
	if r.options.EnableCaching {
		if instance, ok := r.checkCache(serviceType, key, ctx.GetScopeID()); ok {
			ctx.SetInstance(serviceType, key, instance)
			return instance, nil
		}
	}

	// Get provider from registry
	var provider *registry.Descriptor
	var err error
	startTime := time.Now()

	if key != nil {
		provider, err = r.registry.GetKeyedProvider(serviceType, key)
	} else {
		provider, err = r.registry.GetProvider(serviceType)
	}

	if err != nil {
		return nil, &MissingDependencyError{
			DependencyType: serviceType,
			Key:            key,
		}
	}

	// Update context lifetime
	ctx.SetLifetime(provider.Lifetime)

	// Resolve the provider
	instance, err := r.resolveProvider(ctx, provider)
	if err != nil {
		return nil, err
	}

	// Apply decorators
	if r.decoratorProcessor != nil {
		decorated, err := r.decoratorProcessor.ApplyDecorators(instance, serviceType, ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to apply decorators: %w", err)
		}

		instance = decorated
	}

	// Cache the instance
	ctx.SetInstance(serviceType, key, instance)
	if r.options.EnableCaching {
		r.cacheInstance(serviceType, key, instance, provider.Lifetime, ctx.GetScopeID())
	}

	// Call callback
	if r.options.OnResolved != nil {
		duration := time.Since(startTime)
		r.options.OnResolved(serviceType, instance, duration)
	}

	return instance, nil
}

// resolveGroupWithContext resolves all services in a group.
func (r *Resolver) resolveGroupWithContext(ctx ResolutionContext, serviceType reflect.Type, group string) ([]any, error) {
	// Get all providers in the group
	providers, err := r.registry.GetGroupProviders(serviceType, group)
	if err != nil {
		return nil, err
	}

	instances := make([]any, 0, len(providers))

	for _, provider := range providers {
		// Resolve each provider
		instance, err := r.resolveProvider(ctx, provider)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve group member: %w", err)
		}

		instances = append(instances, instance)

		// Cache if needed
		if r.options.EnableCaching && provider.Key != nil {
			ctx.SetInstance(provider.Type, provider.Key, instance)
			r.cacheInstance(provider.Type, provider.Key, instance, provider.Lifetime, ctx.GetScopeID())
		}
	}

	return instances, nil
}

// resolveProvider resolves a specific provider.
func (r *Resolver) resolveProvider(ctx ResolutionContext, provider *registry.Descriptor) (any, error) {
	// Analyze the constructor
	info, err := r.analyzer.Analyze(provider.Constructor.Interface())
	if err != nil {
		return nil, fmt.Errorf("failed to analyze constructor: %w", err)
	}

	// Validate if enabled
	if r.options.EnableValidation {
		validator := reflection.NewValidator(r.analyzer)
		if err := validator.Validate(info); err != nil {
			return nil, &ValidationError{
				ServiceType: provider.Type,
				Message:     err.Error(),
			}
		}
	}

	// Create a dependency resolver adapter
	depResolver := &contextDependencyResolver{ctx: ctx}

	// Invoke the constructor
	results, err := r.invoker.Invoke(info, depResolver)
	if err != nil {
		return nil, &ConstructorError{
			ServiceType: provider.Type,
			Constructor: info.Type,
			Cause:       err,
		}
	}

	// Handle result objects (Out structs)
	if info.IsResultObject && len(results) > 0 {
		registrations, err := r.processor.ProcessResultObject(results[0], info.Type.Out(0))
		if err != nil {
			return nil, fmt.Errorf("failed to process result object: %w", err)
		}

		// Register each service from the result
		for _, reg := range registrations {
			// Cache the instance
			ctx.SetInstance(reg.Type, reg.Key, reg.Value)

			if r.options.EnableCaching {
				r.cacheInstance(reg.Type, reg.Key, reg.Value, provider.Lifetime, ctx.GetScopeID())
			}

			// Return the first non-keyed service as the primary result
			if reg.Key == "" && provider.Type == reg.Type {
				return reg.Value, nil
			}
		}

		// If no primary service found, return the first one
		if len(registrations) > 0 {
			return registrations[0].Value, nil
		}
	}

	// Regular constructor - return first result
	if len(results) > 0 {
		return results[0].Interface(), nil
	}

	return nil, fmt.Errorf("constructor returned no values")
}

// checkCache checks if an instance exists in cache.
func (r *Resolver) checkCache(serviceType reflect.Type, key any, scopeID string) (any, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	// Determine lifetime first
	var lifetime registry.ServiceLifetime
	var found bool

	if key != nil {
		if provider, err := r.registry.GetKeyedProvider(serviceType, key); err == nil {
			lifetime = provider.Lifetime
			found = true
		}
	} else {
		if provider, err := r.registry.GetProvider(serviceType); err == nil {
			lifetime = provider.Lifetime
			found = true
		}
	}

	if !found {
		return nil, false
	}

	// Now check the cache with the correct key
	return r.cache.Get(serviceType, key, lifetime, scopeID)
}

// cacheInstance stores an instance in the appropriate cache.
func (r *Resolver) cacheInstance(serviceType reflect.Type, key any, instance any, lifetime registry.ServiceLifetime, scopeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cache.Set(serviceType, key, instance, lifetime, scopeID)
}

// ClearCache clears all cached instances.
func (r *Resolver) ClearCache() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cache.Clear()
}

// ClearScopeCache clears cached instances for a specific scope.
func (r *Resolver) ClearScopeCache(scopeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cache.ClearScope(scopeID)
}

// GetCacheStatistics returns cache statistics (safe accessor)
func (r *Resolver) GetCacheStatistics() *CacheStatistics {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := r.cache.GetStatistics()
	return &stats
}

// contextDependencyResolver adapts ResolutionContext to DependencyResolver interface.
type contextDependencyResolver struct {
	ctx ResolutionContext
}

func (r *contextDependencyResolver) Resolve(t reflect.Type) (any, error) {
	return r.ctx.GetResolver().resolveWithContext(r.ctx, t, nil)
}

func (r *contextDependencyResolver) ResolveKeyed(t reflect.Type, key any) (any, error) {
	return r.ctx.GetResolver().resolveWithContext(r.ctx, t, key)
}

func (r *contextDependencyResolver) ResolveGroup(t reflect.Type, group string) ([]any, error) {
	return r.ctx.GetResolver().resolveGroupWithContext(r.ctx, t, group)
}
