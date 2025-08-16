package godi

import (
	"sync"
	"testing"
)

// TestNewInstanceCache tests the newInstanceCache function
func TestNewInstanceCache(t *testing.T) {
	cache := newInstanceCache()

	if cache == nil {
		t.Fatal("newInstanceCache() returned nil")
	}

	if cache.instances == nil {
		t.Error("instances map not initialized")
	}

	if len(cache.instances) != 0 {
		t.Errorf("Expected empty instances map, got %d items", len(cache.instances))
	}
}

// TestInstanceCache_Get tests the get method
func TestInstanceCache_Get(t *testing.T) {
	cache := newInstanceCache()

	// Test getting non-existent key
	key := instanceKey{Type: nil, Key: "test"}
	_, exists := cache.get(key)
	if exists {
		t.Error("Expected false for non-existent key")
	}

	// Add an instance and test get
	testInstance := &TestService{Value: "test"}
	cache.set(key, testInstance)

	retrieved, exists := cache.get(key)
	if !exists {
		t.Error("Expected true for existing key")
	}

	if retrieved != testInstance {
		t.Error("Retrieved instance doesn't match stored instance")
	}
}

// TestInstanceCache_Set tests the set method
func TestInstanceCache_Set(t *testing.T) {
	cache := newInstanceCache()

	key1 := instanceKey{Type: nil, Key: "key1"}
	key2 := instanceKey{Type: nil, Key: "key2"}

	instance1 := &TestService{Value: "instance1"}
	instance2 := &TestService{Value: "instance2"}

	// Set first instance
	cache.set(key1, instance1)

	// Set second instance
	cache.set(key2, instance2)

	// Verify both are stored
	got1, _ := cache.get(key1)
	got2, _ := cache.get(key2)

	if got1 != instance1 {
		t.Error("First instance not stored correctly")
	}

	if got2 != instance2 {
		t.Error("Second instance not stored correctly")
	}

	// Test overwriting
	instance3 := &TestService{Value: "instance3"}
	cache.set(key1, instance3)

	got1, _ = cache.get(key1)
	if got1 != instance3 {
		t.Error("Instance not overwritten correctly")
	}
}

// TestInstanceCache_Delete tests the delete method
func TestInstanceCache_Delete(t *testing.T) {
	cache := newInstanceCache()

	key := instanceKey{Type: nil, Key: "test"}
	instance := &TestService{Value: "test"}

	// Set and then delete
	cache.set(key, instance)

	// Verify it exists
	_, exists := cache.get(key)
	if !exists {
		t.Error("Instance should exist before deletion")
	}

	// Delete
	cache.delete(key)

	// Verify it's gone
	_, exists = cache.get(key)
	if exists {
		t.Error("Instance should not exist after deletion")
	}

	// Test deleting non-existent key (should not panic)
	cache.delete(instanceKey{Type: nil, Key: "nonexistent"})
}

// TestInstanceCache_Clear tests the clear method
func TestInstanceCache_Clear(t *testing.T) {
	cache := newInstanceCache()

	// Add multiple instances
	for i := 0; i < 5; i++ {
		key := instanceKey{Type: nil, Key: i}
		cache.set(key, &TestService{Value: "test"})
	}

	// Verify they exist
	for i := 0; i < 5; i++ {
		key := instanceKey{Type: nil, Key: i}
		_, exists := cache.get(key)
		if !exists {
			t.Errorf("Instance %d should exist before clear", i)
		}
	}

	// Clear
	cache.clear()

	// Verify all are gone
	for i := 0; i < 5; i++ {
		key := instanceKey{Type: nil, Key: i}
		_, exists := cache.get(key)
		if exists {
			t.Errorf("Instance %d should not exist after clear", i)
		}
	}

	// Verify map is re-initialized
	if cache.instances == nil {
		t.Error("instances map should be re-initialized after clear")
	}
}

// TestInstanceCache_GetAll tests the getAll method
func TestInstanceCache_GetAll(t *testing.T) {
	cache := newInstanceCache()

	// Test empty cache
	all := cache.getAll()
	if len(all) != 0 {
		t.Errorf("Expected empty map, got %d items", len(all))
	}

	// Add instances
	instances := make(map[instanceKey]any)
	for i := 0; i < 3; i++ {
		key := instanceKey{Type: nil, Key: i}
		instance := &TestService{Value: string(rune('a' + i))}
		cache.set(key, instance)
		instances[key] = instance
	}

	// Get all
	all = cache.getAll()

	// Verify count
	if len(all) != len(instances) {
		t.Errorf("Expected %d items, got %d", len(instances), len(all))
	}

	// Verify each instance
	for key, expected := range instances {
		got, exists := all[key]
		if !exists {
			t.Errorf("Key %v not found in getAll result", key)
		}
		if got != expected {
			t.Errorf("Instance mismatch for key %v", key)
		}
	}

	// Test that returned map is a copy
	delete(all, instanceKey{Type: nil, Key: 0})

	// Original should still have all items
	originalAll := cache.getAll()
	if len(originalAll) != len(instances) {
		t.Error("Modifying returned map affected the cache")
	}
}

// TestInstanceCache_ThreadSafety tests concurrent access
func TestInstanceCache_ThreadSafety(t *testing.T) {
	cache := newInstanceCache()

	var wg sync.WaitGroup
	concurrency := 100
	operations := 1000

	// Concurrent sets
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				key := instanceKey{Type: nil, Key: (id * operations) + j}
				cache.set(key, &TestService{Value: "test"})
			}
		}(i)
	}

	// Concurrent gets
	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operations; j++ {
				key := instanceKey{Type: nil, Key: (id * operations) + j}
				cache.get(key)
			}
		}(i)
	}

	// Concurrent deletes
	wg.Add(concurrency / 2)
	for i := 0; i < concurrency/2; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < operations/2; j++ {
				key := instanceKey{Type: nil, Key: (id * operations) + j}
				cache.delete(key)
			}
		}(i)
	}

	// Concurrent getAll
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = cache.getAll()
			}
		}()
	}

	// Concurrent clear
	wg.Add(5)
	for i := 0; i < 5; i++ {
		go func() {
			defer wg.Done()
			cache.clear()
		}()
	}

	wg.Wait()

	// If we get here without deadlock or panic, thread safety is working
}

// TestInstanceCache_MixedOperations tests various operation combinations
func TestInstanceCache_MixedOperations(t *testing.T) {
	cache := newInstanceCache()

	// Set, get, delete, set again
	key := instanceKey{Type: nil, Key: "mixed"}
	instance1 := &TestService{Value: "first"}
	instance2 := &TestService{Value: "second"}

	cache.set(key, instance1)
	got, exists := cache.get(key)
	if !exists || got != instance1 {
		t.Error("First set/get failed")
	}

	cache.delete(key)
	_, exists = cache.get(key)
	if exists {
		t.Error("Delete failed")
	}

	cache.set(key, instance2)
	got, exists = cache.get(key)
	if !exists || got != instance2 {
		t.Error("Second set/get failed")
	}

	// Clear and verify getAll returns empty
	cache.clear()
	all := cache.getAll()
	if len(all) != 0 {
		t.Error("Clear didn't empty cache")
	}
}

// TestInstanceCache_NilValues tests handling of nil values
func TestInstanceCache_NilValues(t *testing.T) {
	cache := newInstanceCache()

	key := instanceKey{Type: nil, Key: "nil"}

	// Set nil value
	cache.set(key, nil)

	// Get nil value
	got, exists := cache.get(key)
	if !exists {
		t.Error("Nil value should be retrievable")
	}
	if got != nil {
		t.Error("Expected nil value, got non-nil")
	}

	// getAll should include nil values
	all := cache.getAll()
	if len(all) != 1 {
		t.Error("getAll should include nil values")
	}
}

// BenchmarkInstanceCache_Get benchmarks the get operation
func BenchmarkInstanceCache_Get(b *testing.B) {
	cache := newInstanceCache()
	key := instanceKey{Type: nil, Key: "bench"}
	cache.set(key, &TestService{Value: "bench"})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cache.get(key)
		}
	})
}

// BenchmarkInstanceCache_Set benchmarks the set operation
func BenchmarkInstanceCache_Set(b *testing.B) {
	cache := newInstanceCache()
	instance := &TestService{Value: "bench"}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			key := instanceKey{Type: nil, Key: i}
			cache.set(key, instance)
			i++
		}
	})
}

// BenchmarkInstanceCache_GetAll benchmarks the getAll operation
func BenchmarkInstanceCache_GetAll(b *testing.B) {
	cache := newInstanceCache()

	// Populate cache
	for i := 0; i < 100; i++ {
		key := instanceKey{Type: nil, Key: i}
		cache.set(key, &TestService{Value: "bench"})
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = cache.getAll()
		}
	})
}
