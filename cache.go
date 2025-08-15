package godi

import (
	"sync"
)

// instanceCache provides thread-safe caching for service instances
type instanceCache struct {
	instances map[instanceKey]any
	mu        sync.RWMutex
}

// newInstanceCache creates a new instance cache
func newInstanceCache() *instanceCache {
	return &instanceCache{
		instances: make(map[instanceKey]any),
	}
}

// get retrieves an instance from the cache
func (c *instanceCache) get(key instanceKey) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	instance, ok := c.instances[key]
	return instance, ok
}

// set stores an instance in the cache
func (c *instanceCache) set(key instanceKey, instance any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.instances[key] = instance
}

// delete removes an instance from the cache
func (c *instanceCache) delete(key instanceKey) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.instances, key)
}

// clear removes all instances from the cache
func (c *instanceCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.instances = make(map[instanceKey]any)
}

// getAll returns all cached instances
func (c *instanceCache) getAll() map[instanceKey]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Create a copy to avoid external mutations
	copy := make(map[instanceKey]any, len(c.instances))
	for k, v := range c.instances {
		copy[k] = v
	}
	return copy
}
