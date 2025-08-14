package container

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junioryono/godi/v3/internal/graph"
	"github.com/junioryono/godi/v3/internal/lifetime"
	"github.com/junioryono/godi/v3/internal/reflection"
	"github.com/junioryono/godi/v3/internal/registry"
	"github.com/junioryono/godi/v3/internal/resolver"
)

// Container is the main dependency injection container.
// It integrates all components to provide a complete DI solution.
type Container struct {
	// Core components
	registry        *registry.ServiceCollection
	graph           *graph.DependencyGraph
	analyzer        *reflection.Analyzer
	resolver        *resolver.Resolver
	lifetimeManager *lifetime.Manager
	scopeManager    *lifetime.ScopeManager

	// Configuration
	options *ContainerOptions

	// State management
	mu       sync.RWMutex
	built    bool
	disposed int32

	// Root scope
	rootScope *Scope

	// Statistics
	stats ContainerStatistics
}

// ContainerOptions configures the container behavior.
type ContainerOptions struct {
	// EnableValidation performs constructor validation
	EnableValidation bool

	// EnableCaching controls instance caching
	EnableCaching bool

	// EnableLazyLoading delays instance creation until first use
	EnableLazyLoading bool

	// OnServiceRegistered callback
	OnServiceRegistered func(serviceType reflect.Type, lifetime registry.ServiceLifetime)

	// OnServiceResolved callback
	OnServiceResolved func(serviceType reflect.Type, instance any, duration time.Duration)

	// OnServiceError callback
	OnServiceError func(serviceType reflect.Type, err error)

	// OnDispose callback for disposal events
	OnDispose func(instance any, err error)
}

// ContainerStatistics tracks container metrics.
type ContainerStatistics struct {
	RegisteredServices    int64
	ResolvedInstances     int64
	FailedResolutions     int64
	TotalResolutionTime   time.Duration
	AverageResolutionTime time.Duration
	CacheHits             int64
	CacheMisses           int64
}

// DefaultContainerOptions returns default container options.
func DefaultContainerOptions() *ContainerOptions {
	return &ContainerOptions{
		EnableValidation:  true,
		EnableCaching:     true,
		EnableLazyLoading: false,
	}
}

// New creates a new container with default options.
func New() *Container {
	return NewWithOptions(nil)
}

// NewWithOptions creates a new container with custom options.
func NewWithOptions(options *ContainerOptions) *Container {
	if options == nil {
		options = DefaultContainerOptions()
	}

	// Create core components
	reg := registry.NewServiceCollection()
	g := graph.New()
	analyzer := reflection.New()

	// Create lifetime manager with disposal callback
	lifetimeManager := lifetime.NewWithOptions(options.OnDispose)

	// Create resolver with options
	resolverOptions := &resolver.ResolverOptions{
		EnableValidation: options.EnableValidation,
		EnableCaching:    options.EnableCaching,
		OnResolved:       options.OnServiceResolved,
		OnError:          options.OnServiceError,
	}

	r := resolver.New(reg, g, analyzer, resolverOptions)

	// Create scope manager
	scopeManager := lifetime.NewScopeManager(lifetimeManager, r)

	container := &Container{
		registry:        reg,
		graph:           g,
		analyzer:        analyzer,
		resolver:        r,
		lifetimeManager: lifetimeManager,
		scopeManager:    scopeManager,
		options:         options,
	}

	// Create root scope
	container.rootScope = container.createRootScope()

	return container
}

// Register registers a service with the container.
func (c *Container) Register(lifetime registry.ServiceLifetime, constructor any, opts ...ProvideOption) error {
	if c.isDisposed() {
		return ErrContainerDisposed
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.built {
		return ErrContainerBuilt
	}

	// Analyze constructor
	info, err := c.analyzer.Analyze(constructor)
	if err != nil {
		return fmt.Errorf("failed to analyze constructor: %w", err)
	}

	// Validate if enabled
	if c.options.EnableValidation {
		validator := reflection.NewValidator(c.analyzer)
		if err := validator.Validate(info); err != nil {
			return fmt.Errorf("constructor validation failed: %w", err)
		}
	}

	// Get service type
	serviceType, err := c.analyzer.GetServiceType(constructor)
	if err != nil {
		return fmt.Errorf("failed to determine service type: %w", err)
	}

	// Get dependencies
	deps, err := c.analyzer.GetDependencies(constructor)
	if err != nil {
		return fmt.Errorf("failed to get dependencies: %w", err)
	}

	// Build provider
	builder := registry.NewProviderBuilder(constructor).
		WithType(serviceType).
		WithLifetime(lifetime).
		WithDependencies(deps...)

	// Apply options
	for _, opt := range opts {
		builder = applyProvideOption(builder, opt)
	}

	provider, err := builder.Build()
	if err != nil {
		return fmt.Errorf("failed to build provider: %w", err)
	}

	// Register with registry
	if err := c.registry.RegisterProvider(provider); err != nil {
		return fmt.Errorf("failed to register provider: %w", err)
	}

	// Add to dependency graph
	if err := c.graph.AddProvider(provider); err != nil {
		// Remove from registry on graph failure
		c.registry.RemoveProvider(serviceType)
		return fmt.Errorf("failed to add to dependency graph: %w", err)
	}

	// Update statistics
	atomic.AddInt64(&c.stats.RegisteredServices, 1)

	// Call callback
	if c.options.OnServiceRegistered != nil {
		c.options.OnServiceRegistered(serviceType, lifetime)
	}

	return nil
}

// RegisterSingleton registers a singleton service.
func (c *Container) RegisterSingleton(constructor any, opts ...ProvideOption) error {
	return c.Register(registry.Singleton, constructor, opts...)
}

// RegisterScoped registers a scoped service.
func (c *Container) RegisterScoped(constructor any, opts ...ProvideOption) error {
	return c.Register(registry.Scoped, constructor, opts...)
}

// RegisterTransient registers a transient service.
func (c *Container) RegisterTransient(constructor any, opts ...ProvideOption) error {
	return c.Register(registry.Transient, constructor, opts...)
}

// RegisterDecorator registers a decorator for a type.
func (c *Container) RegisterDecorator(decorator any, opts ...DecorateOption) error {
	if c.isDisposed() {
		return ErrContainerDisposed
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.built {
		return ErrContainerBuilt
	}

	// Build decorator
	builder := registry.NewDecoratorBuilder(decorator)

	// Apply options
	for _, opt := range opts {
		builder = applyDecorateOption(builder, opt)
	}

	dec, err := builder.Build()
	if err != nil {
		return fmt.Errorf("failed to build decorator: %w", err)
	}

	// Register with registry
	if err := c.registry.RegisterDecorator(dec); err != nil {
		return fmt.Errorf("failed to register decorator: %w", err)
	}

	return nil
}

// Build finalizes the container configuration and prepares it for use.
func (c *Container) Build() error {
	if c.isDisposed() {
		return ErrContainerDisposed
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.built {
		return nil // Already built
	}

	// Verify dependency graph is acyclic
	if err := c.graph.DetectCycles(); err != nil {
		return fmt.Errorf("dependency cycles detected: %w", err)
	}

	// Eagerly instantiate singletons if not lazy loading
	if !c.options.EnableLazyLoading {
		if err := c.instantiateSingletons(); err != nil {
			return fmt.Errorf("failed to instantiate singletons: %w", err)
		}
	}

	c.built = true
	return nil
}

// CreateScope creates a new service scope.
func (c *Container) CreateScope(ctx context.Context) (*Scope, error) {
	if c.isDisposed() {
		return nil, ErrContainerDisposed
	}

	if !c.built {
		if err := c.Build(); err != nil {
			return nil, fmt.Errorf("failed to build container: %w", err)
		}
	}

	// Create managed scope
	managedScope, err := c.scopeManager.CreateScope(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create scope: %w", err)
	}

	// Wrap in our Scope type
	scope := &Scope{
		container:    c,
		managedScope: managedScope,
		id:           managedScope.ID,
		context:      ctx,
		resolving:    make(map[resolutionKey]bool),
	}

	return scope, nil
}

// Resolve resolves a service from the root scope.
func (c *Container) Resolve(serviceType reflect.Type) (any, error) {
	if c.isDisposed() {
		return nil, ErrContainerDisposed
	}

	if !c.built {
		if err := c.Build(); err != nil {
			return nil, fmt.Errorf("failed to build container: %w", err)
		}
	}

	return c.rootScope.Resolve(serviceType)
}

// ResolveKeyed resolves a keyed service from the root scope.
func (c *Container) ResolveKeyed(serviceType reflect.Type, key any) (any, error) {
	if c.isDisposed() {
		return nil, ErrContainerDisposed
	}

	if !c.built {
		if err := c.Build(); err != nil {
			return nil, fmt.Errorf("failed to build container: %w", err)
		}
	}

	return c.rootScope.ResolveKeyed(serviceType, key)
}

// ResolveGroup resolves all services in a group from the root scope.
func (c *Container) ResolveGroup(serviceType reflect.Type, group string) ([]any, error) {
	if c.isDisposed() {
		return nil, ErrContainerDisposed
	}

	if !c.built {
		if err := c.Build(); err != nil {
			return nil, fmt.Errorf("failed to build container: %w", err)
		}
	}

	return c.rootScope.ResolveGroup(serviceType, group)
}

// Invoke executes a function with dependency injection.
func (c *Container) Invoke(function any) error {
	if c.isDisposed() {
		return ErrContainerDisposed
	}

	if !c.built {
		if err := c.Build(); err != nil {
			return fmt.Errorf("failed to build container: %w", err)
		}
	}

	return c.rootScope.Invoke(function)
}

func (c *Container) RemoveAll(serviceType reflect.Type) error {
	if c.isDisposed() {
		return ErrContainerDisposed
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Remove from registry
	c.registry.RemoveProvider(serviceType)

	// Remove from graph
	c.graph.RemoveProvider(serviceType, nil)

	// Clear resolver cache
	c.resolver.ClearCache()

	return nil
}

func (c *Container) IsRegistered(serviceType reflect.Type) bool {
	if c.isDisposed() {
		return false
	}

	return c.registry.HasProvider(serviceType)
}

func (c *Container) IsKeyedRegistered(serviceType reflect.Type, key any) bool {
	if c.isDisposed() {
		return false
	}

	return c.registry.HasKeyedProvider(serviceType, key)
}

// GetStatistics returns container statistics.
func (c *Container) GetStatistics() ContainerStatistics {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := c.stats

	// Calculate average resolution time
	if stats.ResolvedInstances > 0 {
		stats.AverageResolutionTime = stats.TotalResolutionTime / time.Duration(stats.ResolvedInstances)
	}

	// Get cache statistics from resolver
	if cacheStats := c.resolver.GetCacheStatistics(); cacheStats != nil {
		stats.CacheHits = cacheStats.Hits
		stats.CacheMisses = cacheStats.Misses
	}

	return stats
}

// Dispose disposes the container and all its resources.
func (c *Container) Dispose() error {
	if !atomic.CompareAndSwapInt32(&c.disposed, 0, 1) {
		return nil
	}

	var errs []error

	// Dispose scope manager (disposes all scopes)
	if err := c.scopeManager.Dispose(); err != nil {
		errs = append(errs, fmt.Errorf("failed to dispose scope manager: %w", err))
	}

	// Dispose lifetime manager (disposes all tracked instances)
	if err := c.lifetimeManager.Dispose(); err != nil {
		errs = append(errs, fmt.Errorf("failed to dispose lifetime manager: %w", err))
	}

	// Clear resolver cache
	c.resolver.ClearCache()

	// Clear analyzer cache
	c.analyzer.Clear()

	// Clear registry
	c.registry.Clear()

	// Clear graph
	c.graph.Clear()

	if len(errs) > 0 {
		return fmt.Errorf("disposal errors: %v", errs)
	}

	return nil
}

// IsDisposed checks if the container is disposed.
func (c *Container) IsDisposed() bool {
	return c.isDisposed()
}

// createRootScope creates the root scope for the container.
func (c *Container) createRootScope() *Scope {
	rootManagedScope := c.scopeManager.GetRootScope()

	return &Scope{
		container:    c,
		managedScope: rootManagedScope,
		id:           "root",
		context:      context.Background(),
		isRoot:       true,
		resolving:    make(map[resolutionKey]bool),
	}
}

// instantiateSingletons eagerly creates all singleton instances.
func (c *Container) instantiateSingletons() error {
	// Get topological sort of dependencies
	sorted, err := c.graph.TopologicalSort()
	if err != nil {
		return fmt.Errorf("failed to sort dependencies: %w", err)
	}

	// Instantiate in dependency order
	for _, node := range sorted {
		if node.Provider != nil && node.Provider.Lifetime == registry.Singleton {
			// Resolve to trigger instantiation
			_, err := c.resolver.Resolve(node.Provider.Type, "root")
			if err != nil {
				// Log error but continue
				if c.options.OnServiceError != nil {
					c.options.OnServiceError(node.Provider.Type, err)
				}
			}
		}
	}

	return nil
}

// isDisposed checks if the container is disposed.
func (c *Container) isDisposed() bool {
	return atomic.LoadInt32(&c.disposed) != 0
}

func (c *Container) IsCircularDependency(err error) bool {
	return err != nil && (errors.Is(err, ErrCircularDependency) || IsCircularDependencyError(err))
}
