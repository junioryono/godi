package godi

import (
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/junioryono/godi/v3/internal/registry"
)

// InstanceCache manages cached instances based on their lifetime.
type InstanceCache struct {
	// Singleton instances (global, never cleared until shutdown)
	singletons  map[cacheKey]any
	singletonMu sync.RWMutex

	// Scoped instances (per-scope, cleared when scope ends)
	scoped   map[string]*scopeCache // scopeID -> cache
	scopedMu sync.RWMutex

	// Statistics (using atomic operations for thread safety)
	stats struct {
		hits           int64
		misses         int64
		evictions      int64
		singletonCount int32
		scopedCount    int32
		totalScopes    int64
	}
}

// scopeCache holds instances for a specific scope.
type scopeCache struct {
	instances map[cacheKey]any
	mu        sync.RWMutex
	scopeID   string
}

// cacheKey uniquely identifies a cached instance.
type cacheKey struct {
	Type reflect.Type
	Key  any // nil for non-keyed services
}

// CacheStatistics tracks cache performance metrics.
type CacheStatistics struct {
	Hits           int64
	Misses         int64
	SingletonCount int
	ScopedCount    int
	TotalScopes    int
	Evictions      int64
}

// NewInstanceCache creates a new instance cache.
func NewInstanceCache() *InstanceCache {
	return &InstanceCache{
		singletons: make(map[cacheKey]any),
		scoped:     make(map[string]*scopeCache),
	}
}

// Get retrieves an instance from cache based on lifetime and scope.
func (c *InstanceCache) Get(serviceType reflect.Type, key any, lifetime registry.ServiceLifetime, scopeID string) (any, bool) {
	cKey := cacheKey{Type: serviceType, Key: key}

	switch lifetime {
	case registry.Singleton:
		return c.getSingleton(cKey)

	case registry.Scoped:
		return c.getScoped(cKey, scopeID)

	case registry.Transient:
		// Transient services are never cached
		c.recordMiss()
		return nil, false

	default:
		c.recordMiss()
		return nil, false
	}
}

// Set stores an instance in cache based on lifetime and scope.
func (c *InstanceCache) Set(serviceType reflect.Type, key any, instance any, lifetime registry.ServiceLifetime, scopeID string) {
	if instance == nil {
		return
	}

	cKey := cacheKey{Type: serviceType, Key: key}

	switch lifetime {
	case registry.Singleton:
		c.setSingleton(cKey, instance)

	case registry.Scoped:
		c.setScoped(cKey, instance, scopeID)

	case registry.Transient:
		// Transient services are never cached
		return
	}
}

// getSingleton retrieves a singleton instance.
func (c *InstanceCache) getSingleton(key cacheKey) (any, bool) {
	c.singletonMu.RLock()
	defer c.singletonMu.RUnlock()

	if instance, ok := c.singletons[key]; ok {
		c.recordHit()
		return instance, true
	}

	c.recordMiss()
	return nil, false
}

// setSingleton stores a singleton instance.
func (c *InstanceCache) setSingleton(key cacheKey, instance any) {
	c.singletonMu.Lock()
	defer c.singletonMu.Unlock()

	if _, exists := c.singletons[key]; !exists {
		atomic.AddInt32(&c.stats.singletonCount, 1)
	}
	c.singletons[key] = instance
}

// getScoped retrieves a scoped instance.
func (c *InstanceCache) getScoped(key cacheKey, scopeID string) (any, bool) {
	c.scopedMu.RLock()
	scope, exists := c.scoped[scopeID]
	c.scopedMu.RUnlock()

	if !exists {
		c.recordMiss()
		return nil, false
	}

	scope.mu.RLock()
	defer scope.mu.RUnlock()

	if instance, ok := scope.instances[key]; ok {
		c.recordHit()
		return instance, true
	}

	c.recordMiss()
	return nil, false
}

// setScoped stores a scoped instance.
func (c *InstanceCache) setScoped(key cacheKey, instance any, scopeID string) {
	c.scopedMu.Lock()
	scope, exists := c.scoped[scopeID]
	if !exists {
		scope = &scopeCache{
			instances: make(map[cacheKey]any),
			scopeID:   scopeID,
		}
		c.scoped[scopeID] = scope
		atomic.AddInt64(&c.stats.totalScopes, 1)
	}
	c.scopedMu.Unlock()

	scope.mu.Lock()
	defer scope.mu.Unlock()

	if _, exists := scope.instances[key]; !exists {
		atomic.AddInt32(&c.stats.scopedCount, 1)
	}
	scope.instances[key] = instance
}

// Clear removes all cached instances.
func (c *InstanceCache) Clear() {
	// Clear singletons
	c.singletonMu.Lock()
	oldSingletonCount := len(c.singletons)
	c.singletons = make(map[cacheKey]any)
	atomic.StoreInt32(&c.stats.singletonCount, 0)
	c.singletonMu.Unlock()

	// Clear all scopes
	c.scopedMu.Lock()
	oldScopedCount := 0
	for _, scope := range c.scoped {
		scope.mu.Lock()
		oldScopedCount += len(scope.instances)
		scope.mu.Unlock()
	}
	c.scoped = make(map[string]*scopeCache)
	atomic.StoreInt32(&c.stats.scopedCount, 0)
	atomic.StoreInt64(&c.stats.totalScopes, 0)
	c.scopedMu.Unlock()

	// Update eviction stats
	atomic.AddInt64(&c.stats.evictions, int64(oldSingletonCount+oldScopedCount))
}

// ClearScope removes all cached instances for a specific scope.
func (c *InstanceCache) ClearScope(scopeID string) {
	c.scopedMu.Lock()
	scope, exists := c.scoped[scopeID]
	if exists {
		delete(c.scoped, scopeID)
		atomic.AddInt64(&c.stats.totalScopes, -1)
	}
	c.scopedMu.Unlock()

	if exists {
		scope.mu.Lock()
		evicted := len(scope.instances)
		atomic.AddInt32(&c.stats.scopedCount, -int32(evicted))
		scope.mu.Unlock()

		atomic.AddInt64(&c.stats.evictions, int64(evicted))
	}
}

// GetStatistics returns cache statistics (thread-safe).
func (c *InstanceCache) GetStatistics() CacheStatistics {
	return CacheStatistics{
		Hits:           atomic.LoadInt64(&c.stats.hits),
		Misses:         atomic.LoadInt64(&c.stats.misses),
		SingletonCount: int(atomic.LoadInt32(&c.stats.singletonCount)),
		ScopedCount:    int(atomic.LoadInt32(&c.stats.scopedCount)),
		TotalScopes:    int(atomic.LoadInt64(&c.stats.totalScopes)),
		Evictions:      atomic.LoadInt64(&c.stats.evictions),
	}
}

// HasSingleton checks if a singleton instance exists.
func (c *InstanceCache) HasSingleton(serviceType reflect.Type, key any) bool {
	c.singletonMu.RLock()
	defer c.singletonMu.RUnlock()

	cKey := cacheKey{Type: serviceType, Key: key}
	_, exists := c.singletons[cKey]
	return exists
}

// HasScoped checks if a scoped instance exists.
func (c *InstanceCache) HasScoped(serviceType reflect.Type, key any, scopeID string) bool {
	c.scopedMu.RLock()
	scope, exists := c.scoped[scopeID]
	c.scopedMu.RUnlock()

	if !exists {
		return false
	}

	scope.mu.RLock()
	defer scope.mu.RUnlock()

	cKey := cacheKey{Type: serviceType, Key: key}
	_, exists = scope.instances[cKey]
	return exists
}

// GetScopeIDs returns all active scope IDs.
func (c *InstanceCache) GetScopeIDs() []string {
	c.scopedMu.RLock()
	defer c.scopedMu.RUnlock()

	ids := make([]string, 0, len(c.scoped))
	for id := range c.scoped {
		ids = append(ids, id)
	}
	return ids
}

// GetScopeSize returns the number of instances in a scope.
func (c *InstanceCache) GetScopeSize(scopeID string) int {
	c.scopedMu.RLock()
	scope, exists := c.scoped[scopeID]
	c.scopedMu.RUnlock()

	if !exists {
		return 0
	}

	scope.mu.RLock()
	defer scope.mu.RUnlock()

	return len(scope.instances)
}

// recordHit records a cache hit.
func (c *InstanceCache) recordHit() {
	atomic.AddInt64(&c.stats.hits, 1)
}

// recordMiss records a cache miss.
func (c *InstanceCache) recordMiss() {
	atomic.AddInt64(&c.stats.misses, 1)
}

// currentTimestamp returns the current Unix timestamp.
func currentTimestamp() int64 {
	return 0 // Simplified for now, can use time.Now().Unix()
}

// String returns a string representation of the cache key.
func (k cacheKey) String() string {
	if k.Key != nil {
		return fmt.Sprintf("%v[%v]", k.Type, k.Key)
	}
	return fmt.Sprintf("%v", k.Type)
}
