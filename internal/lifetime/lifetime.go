package lifetime

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junioryono/godi/v3/internal/registry"
)

// Manager manages service lifetimes and disposal.
type Manager struct {
	mu sync.RWMutex

	// Singleton instances and their disposal tracking
	singletons map[instanceKey]*managedInstance

	// Scoped instances by scope ID
	scopes map[string]*ManagedScope

	// Disposal callbacks
	onDispose func(instance any, err error)

	// Statistics
	stats Statistics

	// State
	disposed int32
}

// instanceKey uniquely identifies an instance.
type instanceKey struct {
	Type reflect.Type
	Key  any
}

// managedInstance wraps an instance with lifecycle metadata.
type managedInstance struct {
	instance     any
	disposable   Disposable
	lifetime     registry.ServiceLifetime
	created      time.Time
	lastAccessed time.Time
	accessCount  int64
	scopeID      string // For scoped instances
}

// ManagedScope represents a scope with lifecycle management.
type ManagedScope struct {
	ID         string
	Context    context.Context
	instances  map[instanceKey]*managedInstance
	mu         sync.RWMutex
	disposed   int32
	created    time.Time
	parent     *ManagedScope
	children   []*ManagedScope
	childrenMu sync.RWMutex
}

// Disposable interface for resources that need cleanup.
type Disposable interface {
	Dispose() error
}

// Statistics tracks lifetime manager metrics.
type Statistics struct {
	TotalInstances    int64
	ActiveInstances   int64
	DisposedInstances int64
	TotalScopes       int64
	ActiveScopes      int64
	DisposedScopes    int64
	TotalAccessCount  int64
	AverageLifetime   time.Duration
}

// New creates a new lifetime manager.
func New() *Manager {
	return &Manager{
		singletons: make(map[instanceKey]*managedInstance),
		scopes:     make(map[string]*ManagedScope),
	}
}

// NewWithOptions creates a new lifetime manager with options.
func NewWithOptions(onDispose func(instance any, err error)) *Manager {
	return &Manager{
		singletons: make(map[instanceKey]*managedInstance),
		scopes:     make(map[string]*ManagedScope),
		onDispose:  onDispose,
	}
}

// Track starts tracking an instance's lifetime.
func (m *Manager) Track(
	instance any,
	serviceType reflect.Type,
	key any,
	lifetime registry.ServiceLifetime,
	scopeID string,
) error {
	if m.isDisposed() {
		return fmt.Errorf("lifetime manager is disposed")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	iKey := instanceKey{Type: serviceType, Key: key}

	managed := &managedInstance{
		instance:     instance,
		lifetime:     lifetime,
		created:      time.Now(),
		lastAccessed: time.Now(),
		accessCount:  1,
		scopeID:      scopeID,
	}

	// Check if instance implements Disposable
	if d, ok := instance.(Disposable); ok {
		managed.disposable = d
	}

	// Store based on lifetime
	switch lifetime {
	case registry.Singleton:
		m.singletons[iKey] = managed
		atomic.AddInt64(&m.stats.TotalInstances, 1)
		atomic.AddInt64(&m.stats.ActiveInstances, 1)

	case registry.Scoped:
		scope, exists := m.scopes[scopeID]
		if !exists {
			scope = m.createScope(scopeID, context.Background())
		}

		scope.mu.Lock()
		scope.instances[iKey] = managed
		scope.mu.Unlock()

		atomic.AddInt64(&m.stats.TotalInstances, 1)
		atomic.AddInt64(&m.stats.ActiveInstances, 1)

	case registry.Transient:
		// Track for disposal but don't cache
		if managed.disposable != nil {
			// Store temporarily for disposal tracking
			if scopeID != "" {
				scope, exists := m.scopes[scopeID]
				if !exists {
					scope = m.createScope(scopeID, context.Background())
				}

				scope.mu.Lock()
				// Use a unique key for transients
				transientKey := instanceKey{
					Type: serviceType,
					Key:  fmt.Sprintf("transient_%p", instance),
				}
				scope.instances[transientKey] = managed
				scope.mu.Unlock()
			}
		}

		atomic.AddInt64(&m.stats.TotalInstances, 1)
		atomic.AddInt64(&m.stats.ActiveInstances, 1)
	}

	return nil
}

// Access records access to an instance and returns it.
func (m *Manager) Access(
	serviceType reflect.Type,
	key any,
	lifetime registry.ServiceLifetime,
	scopeID string,
) (any, bool) {
	if m.isDisposed() {
		return nil, false
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	iKey := instanceKey{Type: serviceType, Key: key}

	switch lifetime {
	case registry.Singleton:
		if managed, ok := m.singletons[iKey]; ok {
			managed.lastAccessed = time.Now()
			atomic.AddInt64(&managed.accessCount, 1)
			atomic.AddInt64(&m.stats.TotalAccessCount, 1)
			return managed.instance, true
		}

	case registry.Scoped:
		if scope, exists := m.scopes[scopeID]; exists {
			scope.mu.RLock()
			defer scope.mu.RUnlock()

			if managed, ok := scope.instances[iKey]; ok {
				managed.lastAccessed = time.Now()
				atomic.AddInt64(&managed.accessCount, 1)
				atomic.AddInt64(&m.stats.TotalAccessCount, 1)
				return managed.instance, true
			}
		}
	}

	return nil, false
}

// CreateScope creates a new managed scope.
func (m *Manager) CreateScope(scopeID string, ctx context.Context) *ManagedScope {
	if m.isDisposed() {
		return nil
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	return m.createScope(scopeID, ctx)
}

// createScope creates a new scope (must be called with lock held).
func (m *Manager) createScope(scopeID string, ctx context.Context) *ManagedScope {
	scope := &ManagedScope{
		ID:        scopeID,
		Context:   ctx,
		instances: make(map[instanceKey]*managedInstance),
		created:   time.Now(),
		children:  make([]*ManagedScope, 0),
	}

	m.scopes[scopeID] = scope
	atomic.AddInt64(&m.stats.TotalScopes, 1)
	atomic.AddInt64(&m.stats.ActiveScopes, 1)

	return scope
}

// CreateChildScope creates a child scope.
func (m *Manager) CreateChildScope(parentID, childID string, ctx context.Context) (*ManagedScope, error) {
	if m.isDisposed() {
		return nil, fmt.Errorf("lifetime manager is disposed")
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	parent, exists := m.scopes[parentID]
	if !exists {
		return nil, fmt.Errorf("parent scope %s not found", parentID)
	}

	child := m.createScope(childID, ctx)
	child.parent = parent

	parent.childrenMu.Lock()
	parent.children = append(parent.children, child)
	parent.childrenMu.Unlock()

	return child, nil
}

// DisposeScope disposes a scope and all its instances.
func (m *Manager) DisposeScope(scopeID string) error {
	if m.isDisposed() {
		return fmt.Errorf("lifetime manager is disposed")
	}

	m.mu.Lock()
	scope, exists := m.scopes[scopeID]
	if !exists {
		m.mu.Unlock()
		return fmt.Errorf("scope %s not found", scopeID)
	}
	delete(m.scopes, scopeID)
	m.mu.Unlock()

	return m.disposeScope(scope)
}

// disposeScope disposes a scope and its children.
func (m *Manager) disposeScope(scope *ManagedScope) error {
	if !atomic.CompareAndSwapInt32(&scope.disposed, 0, 1) {
		return nil // Already disposed
	}

	var errs []error

	// Dispose children first
	scope.childrenMu.RLock()
	children := make([]*ManagedScope, len(scope.children))
	copy(children, scope.children)
	scope.childrenMu.RUnlock()

	for _, child := range children {
		if err := m.disposeScope(child); err != nil {
			errs = append(errs, fmt.Errorf("failed to dispose child scope %s: %w", child.ID, err))
		}
	}

	// Dispose instances in reverse order (LIFO)
	scope.mu.Lock()
	instances := make([]*managedInstance, 0, len(scope.instances))
	for _, inst := range scope.instances {
		instances = append(instances, inst)
	}
	scope.instances = nil
	scope.mu.Unlock()

	// Sort by creation time (newest first for LIFO)
	for i := len(instances) - 1; i >= 0; i-- {
		if err := m.disposeInstance(instances[i]); err != nil {
			errs = append(errs, err)
		}
	}

	atomic.AddInt64(&m.stats.ActiveScopes, -1)
	atomic.AddInt64(&m.stats.DisposedScopes, 1)

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// disposeInstance disposes a single instance.
func (m *Manager) disposeInstance(managed *managedInstance) error {
	if managed.disposable == nil {
		atomic.AddInt64(&m.stats.ActiveInstances, -1)
		atomic.AddInt64(&m.stats.DisposedInstances, 1)
		return nil
	}

	err := managed.disposable.Dispose()
	if m.onDispose != nil {
		m.onDispose(managed.instance, err)
	}

	atomic.AddInt64(&m.stats.ActiveInstances, -1)
	atomic.AddInt64(&m.stats.DisposedInstances, 1)

	if err != nil {
		return fmt.Errorf("failed to dispose %T: %w", managed.instance, err)
	}

	return nil
}

// DisposeSingletons disposes all singleton instances.
func (m *Manager) DisposeSingletons() error {
	if m.isDisposed() {
		return fmt.Errorf("lifetime manager is disposed")
	}

	m.mu.Lock()
	singletons := make([]*managedInstance, 0, len(m.singletons))
	for _, inst := range m.singletons {
		singletons = append(singletons, inst)
	}
	m.singletons = make(map[instanceKey]*managedInstance)
	m.mu.Unlock()

	var errs []error

	// Dispose in reverse order
	for i := len(singletons) - 1; i >= 0; i-- {
		if err := m.disposeInstance(singletons[i]); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// Dispose disposes the lifetime manager and all tracked instances.
func (m *Manager) Dispose() error {
	if !atomic.CompareAndSwapInt32(&m.disposed, 0, 1) {
		return nil
	}

	var errs []error

	// Dispose all scopes
	m.mu.Lock()
	scopes := make([]*ManagedScope, 0, len(m.scopes))
	for _, scope := range m.scopes {
		scopes = append(scopes, scope)
	}
	m.scopes = nil
	m.mu.Unlock()

	for _, scope := range scopes {
		if err := m.disposeScope(scope); err != nil {
			errs = append(errs, err)
		}
	}

	// Dispose singletons
	if err := m.DisposeSingletons(); err != nil {
		errs = append(errs, err)
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

// GetStatistics returns lifetime statistics.
func (m *Manager) GetStatistics() Statistics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := m.stats

	// Calculate average lifetime
	var totalLifetime time.Duration
	count := int64(0)

	for _, inst := range m.singletons {
		totalLifetime += time.Since(inst.created)
		count++
	}

	for _, scope := range m.scopes {
		scope.mu.RLock()
		for _, inst := range scope.instances {
			totalLifetime += time.Since(inst.created)
			count++
		}
		scope.mu.RUnlock()
	}

	if count > 0 {
		stats.AverageLifetime = totalLifetime / time.Duration(count)
	}

	return stats
}

// GetScopeInfo returns information about a scope.
func (m *Manager) GetScopeInfo(scopeID string) (*ScopeInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	scope, exists := m.scopes[scopeID]
	if !exists {
		return nil, fmt.Errorf("scope %s not found", scopeID)
	}

	scope.mu.RLock()
	defer scope.mu.RUnlock()

	info := &ScopeInfo{
		ID:            scope.ID,
		Created:       scope.created,
		InstanceCount: len(scope.instances),
		IsDisposed:    atomic.LoadInt32(&scope.disposed) != 0,
	}

	if scope.parent != nil {
		info.ParentID = scope.parent.ID
	}

	scope.childrenMu.RLock()
	info.ChildCount = len(scope.children)
	scope.childrenMu.RUnlock()

	return info, nil
}

// ScopeInfo contains information about a scope.
type ScopeInfo struct {
	ID            string
	ParentID      string
	Created       time.Time
	InstanceCount int
	ChildCount    int
	IsDisposed    bool
}

// isDisposed checks if the manager is disposed.
func (m *Manager) isDisposed() bool {
	return atomic.LoadInt32(&m.disposed) != 0
}
