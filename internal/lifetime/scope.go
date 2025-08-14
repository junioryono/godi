package lifetime

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/junioryono/godi/v3/internal/registry"
	"github.com/junioryono/godi/v3/internal/resolver"
)

// ScopeManager manages service scopes and their lifecycles.
type ScopeManager struct {
	lifetimeManager *Manager
	resolver        *resolver.Resolver

	// Active scopes
	scopes   map[string]*ServiceScope
	scopesMu sync.RWMutex

	// Root scope
	rootScope *ServiceScope

	// State
	disposed int32
}

// ServiceScope represents a service scope with resolution capabilities.
type ServiceScope struct {
	ID      string
	Context context.Context
	manager *ScopeManager
	managed *ManagedScope
	parent  *ServiceScope

	// Resolution tracking
	resolving   map[resolutionKey]bool
	resolvingMu sync.Mutex

	// State
	disposed int32
	created  time.Time
}

// resolutionKey for tracking active resolutions.
type resolutionKey struct {
	Type any
	Key  any
}

// NewScopeManager creates a new scope manager.
func NewScopeManager(
	lifetimeManager *Manager,
	resolver *resolver.Resolver,
) *ScopeManager {
	sm := &ScopeManager{
		lifetimeManager: lifetimeManager,
		resolver:        resolver,
		scopes:          make(map[string]*ServiceScope),
	}

	// Create root scope
	sm.rootScope = sm.createRootScope()

	return sm
}

// createRootScope creates the root service scope.
func (sm *ScopeManager) createRootScope() *ServiceScope {
	ctx := context.Background()
	managed := sm.lifetimeManager.CreateScope("root", ctx)

	scope := &ServiceScope{
		ID:        "root",
		Context:   ctx,
		manager:   sm,
		managed:   managed,
		created:   time.Now(),
		resolving: make(map[resolutionKey]bool),
	}

	sm.scopesMu.Lock()
	sm.scopes["root"] = scope
	sm.scopesMu.Unlock()

	return scope
}

// GetRootScope returns the root scope.
func (sm *ScopeManager) GetRootScope() *ServiceScope {
	return sm.rootScope
}

// CreateScope creates a new service scope.
func (sm *ScopeManager) CreateScope(ctx context.Context) (*ServiceScope, error) {
	if sm.isDisposed() {
		return nil, fmt.Errorf("scope manager is disposed")
	}

	scopeID := generateScopeID()
	return sm.CreateScopeWithID(scopeID, ctx)
}

// CreateScopeWithID creates a new service scope with a specific ID.
func (sm *ScopeManager) CreateScopeWithID(scopeID string, ctx context.Context) (*ServiceScope, error) {
	if sm.isDisposed() {
		return nil, fmt.Errorf("scope manager is disposed")
	}

	if ctx == nil {
		ctx = context.Background()
	}

	// Check if scope already exists
	sm.scopesMu.RLock()
	if _, exists := sm.scopes[scopeID]; exists {
		sm.scopesMu.RUnlock()
		return nil, fmt.Errorf("scope %s already exists", scopeID)
	}
	sm.scopesMu.RUnlock()

	// Create managed scope
	managed := sm.lifetimeManager.CreateScope(scopeID, ctx)

	scope := &ServiceScope{
		ID:        scopeID,
		Context:   ctx,
		manager:   sm,
		managed:   managed,
		parent:    sm.rootScope,
		created:   time.Now(),
		resolving: make(map[resolutionKey]bool),
	}

	// Register scope
	sm.scopesMu.Lock()
	sm.scopes[scopeID] = scope
	sm.scopesMu.Unlock()

	return scope, nil
}

// CreateChildScope creates a child scope.
func (sm *ScopeManager) CreateChildScope(parentID string, ctx context.Context) (*ServiceScope, error) {
	if sm.isDisposed() {
		return nil, fmt.Errorf("scope manager is disposed")
	}

	sm.scopesMu.RLock()
	parent, exists := sm.scopes[parentID]
	sm.scopesMu.RUnlock()

	if !exists {
		return nil, fmt.Errorf("parent scope %s not found", parentID)
	}

	childID := generateScopeID()

	// Create managed child scope
	managed, err := sm.lifetimeManager.CreateChildScope(parentID, childID, ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create child scope: %w", err)
	}

	scope := &ServiceScope{
		ID:        childID,
		Context:   ctx,
		manager:   sm,
		managed:   managed,
		parent:    parent,
		created:   time.Now(),
		resolving: make(map[resolutionKey]bool),
	}

	// Register scope
	sm.scopesMu.Lock()
	sm.scopes[childID] = scope
	sm.scopesMu.Unlock()

	return scope, nil
}

// GetScope retrieves a scope by ID.
func (sm *ScopeManager) GetScope(scopeID string) (*ServiceScope, error) {
	sm.scopesMu.RLock()
	defer sm.scopesMu.RUnlock()

	scope, exists := sm.scopes[scopeID]
	if !exists {
		return nil, fmt.Errorf("scope %s not found", scopeID)
	}

	if scope.IsDisposed() {
		return nil, fmt.Errorf("scope %s is disposed", scopeID)
	}

	return scope, nil
}

// DisposeScope disposes a scope and its resources.
func (sm *ScopeManager) DisposeScope(scopeID string) error {
	if sm.isDisposed() {
		return fmt.Errorf("scope manager is disposed")
	}

	sm.scopesMu.Lock()
	scope, exists := sm.scopes[scopeID]
	if !exists {
		sm.scopesMu.Unlock()
		return fmt.Errorf("scope %s not found", scopeID)
	}
	delete(sm.scopes, scopeID)
	sm.scopesMu.Unlock()

	return scope.Dispose()
}

// Dispose disposes the scope manager and all scopes.
func (sm *ScopeManager) Dispose() error {
	if !atomic.CompareAndSwapInt32(&sm.disposed, 0, 1) {
		return nil
	}

	// Dispose all scopes except root
	sm.scopesMu.Lock()
	scopes := make([]*ServiceScope, 0, len(sm.scopes))
	for _, scope := range sm.scopes {
		if scope.ID != "root" {
			scopes = append(scopes, scope)
		}
	}
	sm.scopes = nil
	sm.scopesMu.Unlock()

	for _, scope := range scopes {
		scope.Dispose()
	}

	// Finally dispose root
	if sm.rootScope != nil {
		return sm.rootScope.Dispose()
	}

	return nil
}

// isDisposed checks if the manager is disposed.
func (sm *ScopeManager) isDisposed() bool {
	return atomic.LoadInt32(&sm.disposed) != 0
}

// ServiceScope methods

// Resolve resolves a service in this scope.
func (s *ServiceScope) Resolve(serviceType any) (any, error) {
	if s.IsDisposed() {
		return nil, fmt.Errorf("scope %s is disposed", s.ID)
	}

	// Convert serviceType to reflect.Type
	var reflectType reflect.Type
	switch t := serviceType.(type) {
	case reflect.Type:
		reflectType = t
	default:
		reflectType = reflect.TypeOf(serviceType)
		if reflectType.Kind() == reflect.Ptr {
			reflectType = reflectType.Elem()
		}
	}

	// Track resolution to detect cycles
	key := resolutionKey{Type: serviceType}

	s.resolvingMu.Lock()
	if s.resolving[key] {
		s.resolvingMu.Unlock()
		return nil, fmt.Errorf("circular dependency detected for %v", serviceType)
	}
	s.resolving[key] = true
	s.resolvingMu.Unlock()

	defer func() {
		s.resolvingMu.Lock()
		delete(s.resolving, key)
		s.resolvingMu.Unlock()
	}()

	// Use the resolver with this scope's ID
	return s.manager.resolver.Resolve(reflectType, s.ID)
}

// ResolveKeyed resolves a keyed service in this scope.
func (s *ServiceScope) ResolveKeyed(serviceType any, serviceKey any) (any, error) {
	if s.IsDisposed() {
		return nil, fmt.Errorf("scope %s is disposed", s.ID)
	}

	// Track resolution
	key := resolutionKey{Type: serviceType, Key: serviceKey}

	s.resolvingMu.Lock()
	if s.resolving[key] {
		s.resolvingMu.Unlock()
		return nil, fmt.Errorf("circular dependency detected for %v[%v]", serviceType, serviceKey)
	}
	s.resolving[key] = true
	s.resolvingMu.Unlock()

	defer func() {
		s.resolvingMu.Lock()
		delete(s.resolving, key)
		s.resolvingMu.Unlock()
	}()

	// Use the resolver with this scope's ID
	return nil, fmt.Errorf("resolver integration not implemented")
}

// Track tracks an instance in this scope.
func (s *ServiceScope) Track(instance any, serviceType any, lifetime registry.ServiceLifetime) error {
	if s.IsDisposed() {
		return fmt.Errorf("scope %s is disposed", s.ID)
	}

	// Convert serviceType to reflect.Type
	var reflectType reflect.Type
	switch t := serviceType.(type) {
	case reflect.Type:
		reflectType = t
	default:
		reflectType = reflect.TypeOf(serviceType)
	}

	// Delegate to lifetime manager
	return s.manager.lifetimeManager.Track(instance, reflectType, nil, lifetime, s.ID)
}

// CreateChild creates a child scope.
func (s *ServiceScope) CreateChild(ctx context.Context) (*ServiceScope, error) {
	if s.IsDisposed() {
		return nil, fmt.Errorf("scope %s is disposed", s.ID)
	}

	return s.manager.CreateChildScope(s.ID, ctx)
}

// Dispose disposes the scope and its resources.
func (s *ServiceScope) Dispose() error {
	if !atomic.CompareAndSwapInt32(&s.disposed, 0, 1) {
		return nil
	}

	// Dispose through lifetime manager
	return s.manager.lifetimeManager.DisposeScope(s.ID)
}

// IsDisposed checks if the scope is disposed.
func (s *ServiceScope) IsDisposed() bool {
	return atomic.LoadInt32(&s.disposed) != 0
}

// LastAccessed returns when the scope was last accessed.
func (s *ServiceScope) LastAccessed() time.Time {
	// This would track actual access times
	// For now, return creation time
	return s.created
}

// GetParent returns the parent scope.
func (s *ServiceScope) GetParent() *ServiceScope {
	return s.parent
}

// GetContext returns the scope's context.
func (s *ServiceScope) GetContext() context.Context {
	return s.Context
}

// generateScopeID generates a unique scope ID.
func generateScopeID() string {
	// Simple implementation - in production use UUID or similar
	return fmt.Sprintf("scope_%d", time.Now().UnixNano())
}
