package godi

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/junioryono/godi/v3/internal/graph"
	"github.com/junioryono/godi/v3/internal/reflection"
)

// Disposable interface for resources that need cleanup
type Disposable interface {
	Close() error
}

// Provider is the main dependency injection container interface
type Provider interface {
	Disposable

	// Resolution methods
	Get(serviceType reflect.Type) (any, error)
	GetKeyed(serviceType reflect.Type, key any) (any, error)
	GetGroup(serviceType reflect.Type, group string) ([]any, error)

	// Scope management
	CreateScope(ctx context.Context) (Scope, error)
}

type ProviderOptions struct {
}

// provider is the concrete implementation of Provider
type provider struct {
	// Service registry (immutable after build)
	services   map[TypeKey]*Descriptor
	groups     map[GroupKey][]*Descriptor
	decorators map[reflect.Type][]*Descriptor

	// Dependency graph (immutable after build)
	graph *graph.DependencyGraph

	// Reflection analyzer
	analyzer *reflection.Analyzer

	// Singleton instances (created at build time)
	singletons   map[instanceKey]any
	singletonsMu sync.RWMutex

	// Track disposable instances for cleanup
	disposables   []Disposable
	disposablesMu sync.Mutex

	// Root scope for provider-level resolution
	rootScope *scope

	// Active scopes for cleanup tracking
	scopes   map[*scope]struct{}
	scopesMu sync.Mutex

	// State
	built    bool
	disposed int32 // atomic
}

// instanceKey uniquely identifies a service instance
type instanceKey struct {
	Type  reflect.Type
	Key   any
	Group string
}

// Get resolves a service from the root scope
func (p *provider) Get(serviceType reflect.Type) (any, error) {
	if atomic.LoadInt32(&p.disposed) != 0 {
		return nil, ErrProviderDisposed
	}

	return p.rootScope.Get(serviceType)
}

// GetKeyed resolves a keyed service from the root scope
func (p *provider) GetKeyed(serviceType reflect.Type, key any) (any, error) {
	if atomic.LoadInt32(&p.disposed) != 0 {
		return nil, ErrProviderDisposed
	}

	return p.rootScope.GetKeyed(serviceType, key)
}

// GetGroup resolves all services in a group from the root scope
func (p *provider) GetGroup(serviceType reflect.Type, group string) ([]any, error) {
	if atomic.LoadInt32(&p.disposed) != 0 {
		return nil, ErrProviderDisposed
	}

	return p.rootScope.GetGroup(serviceType, group)
}

// CreateScope creates a new service scope
func (p *provider) CreateScope(ctx context.Context) (Scope, error) {
	if atomic.LoadInt32(&p.disposed) != 0 {
		return nil, ErrProviderDisposed
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// Create scope with cancellable context
	scopeCtx, cancel := context.WithCancel(ctx)

	s := &scope{
		provider:    p,
		parent:      nil,
		context:     scopeCtx,
		cancel:      cancel,
		instances:   make(map[instanceKey]any),
		disposables: make([]Disposable, 0),
		resolving:   make(map[instanceKey]struct{}),
		children:    make(map[*scope]struct{}),
	}

	// Track scope
	p.scopesMu.Lock()
	p.scopes[s] = struct{}{}
	p.scopesMu.Unlock()

	// Auto-close on context cancellation
	go func() {
		<-scopeCtx.Done()
		s.Close()
	}()

	return s, nil
}

// Close disposes the provider and all its resources
func (p *provider) Close() error {
	if !atomic.CompareAndSwapInt32(&p.disposed, 0, 1) {
		return nil // Already disposed
	}

	var errs []error

	// Close all scopes
	p.scopesMu.Lock()
	scopes := make([]*scope, 0, len(p.scopes))
	for s := range p.scopes {
		scopes = append(scopes, s)
	}
	p.scopes = nil
	p.scopesMu.Unlock()

	for _, s := range scopes {
		if err := s.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close scope: %w", err))
		}
	}

	// Close root scope
	if p.rootScope != nil {
		if err := p.rootScope.Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to close root scope: %w", err))
		}
	}

	// Dispose all singleton disposables
	p.disposablesMu.Lock()
	disposables := p.disposables
	p.disposables = nil
	p.disposablesMu.Unlock()

	// Dispose in reverse order of creation
	for i := len(disposables) - 1; i >= 0; i-- {
		if err := disposables[i].Close(); err != nil {
			errs = append(errs, fmt.Errorf("failed to dispose singleton: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("provider disposal errors: %v", errs)
	}

	return nil
}

// getSingleton retrieves a singleton instance
func (p *provider) getSingleton(key instanceKey) (any, bool) {
	p.singletonsMu.RLock()
	instance, ok := p.singletons[key]
	p.singletonsMu.RUnlock()
	return instance, ok
}

// setSingleton stores a singleton instance
func (p *provider) setSingleton(key instanceKey, instance any) {
	p.singletonsMu.Lock()
	p.singletons[key] = instance
	p.singletonsMu.Unlock()

	// Track if disposable
	if d, ok := instance.(Disposable); ok {
		p.disposablesMu.Lock()
		p.disposables = append(p.disposables, d)
		p.disposablesMu.Unlock()
	}
}

// findDescriptor finds a descriptor for the given service
func (p *provider) findDescriptor(serviceType reflect.Type, key any) *Descriptor {
	typeKey := TypeKey{Type: serviceType, Key: key}
	return p.services[typeKey]
}

// findGroupDescriptors finds all descriptors for a group
func (p *provider) findGroupDescriptors(serviceType reflect.Type, group string) []*Descriptor {
	groupKey := GroupKey{Type: serviceType, Group: group}
	return p.groups[groupKey]
}

// getDecorators returns decorators for a service type
func (p *provider) getDecorators(serviceType reflect.Type) []*Descriptor {
	return p.decorators[serviceType]
}

// createAllSingletons creates all singleton instances at build time
func (p *provider) createAllSingletons() error {
	// Get topological sort from dependency graph
	sorted, err := p.graph.TopologicalSort()
	if err != nil {
		return fmt.Errorf("failed to sort dependencies: %w", err)
	}

	// Create instances in dependency order
	for _, node := range sorted {
		descriptor := node.Provider.(*Descriptor)
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

		// Create the instance
		instance, err := p.rootScope.createInstance(descriptor)
		if err != nil {
			return fmt.Errorf("failed to create singleton %v: %w", descriptor.Type, err)
		}

		// Store the singleton
		p.setSingleton(key, instance)
	}

	return nil
}
