# Proposal: Mixed Lifetime Service Groups in Godi

## Executive Summary

After analyzing the Godi dependency injection container codebase, I found that **there is indeed a test case** for resolving groups with mixed lifetimes (singleton/scoped/transient), but it lacks comprehensive validation and edge case coverage. The current implementation appears to handle mixed lifetimes correctly but needs more rigorous testing.

## Current State

### Test Coverage Found

In `collection_test.go:986-1014`, there is a test case that registers services with different lifetimes to the same group:

```go
func TestGroups(t *testing.T) {
    t.Run("multiple services in same group", func(t *testing.T) {
        cl := NewCollection()
        
        err := cl.AddSingleton(newService1, Group("handlers"))  // Singleton
        err = cl.AddSingleton(newService2, Group("handlers"))  // Singleton
        err = cl.AddTransient(newService3, Group("handlers")) // Transient
        
        provider, err := cl.Build()
        services, err := provider.GetGroup(reflect.TypeOf((*TestService)(nil)), "handlers")
        assert.Len(t, services, 3)
    })
}
```

### What Happens Currently

Based on the code analysis:

1. **Registration Phase** (collection.go:608-612):
   - Services are added to groups regardless of lifetime
   - Each service gets assigned a unique key within the group

2. **Build Phase** (provider.go:293-379):
   - Singletons in groups are created at build time
   - All singleton instances are pre-initialized and cached

3. **Resolution Phase** (scope.go:120-158):
   - When `GetGroup` is called, each service is resolved according to its lifetime:
     - **Singleton**: Returns the pre-created instance from provider cache
     - **Scoped**: Creates or returns cached instance from current scope
     - **Transient**: Always creates a new instance

## Expected Behavior (Best Result)

The **best result** for mixed lifetime groups should be:

### 1. **Lifetime Semantics Preserved**
Each service in the group maintains its defined lifetime behavior:
- Singleton services return the same instance across all scopes
- Scoped services return the same instance within a scope, different across scopes
- Transient services return a new instance every time

### 2. **Predictable Resolution Order**
Services should be returned in the order they were registered, regardless of lifetime

### 3. **No Lifetime Contamination**
Services with different lifetimes should not affect each other's behavior

### 4. **Clear Error Handling**
Invalid lifetime combinations should be detected (e.g., singleton depending on scoped service)

## Identified Issues and Recommendations

### Issue 1: Insufficient Test Coverage

**Current State**: Only one basic test exists for mixed lifetime groups

**Recommendation**: Add comprehensive tests:

```go
func TestMixedLifetimeGroups(t *testing.T) {
    t.Run("singleton_scoped_transient_in_same_group", func(t *testing.T) {
        collection := NewCollection()
        
        // Add services with different lifetimes
        collection.AddSingleton(func() *Service { 
            return &Service{ID: "singleton"} 
        }, Group("mixed"))
        
        collection.AddScoped(func() *Service { 
            return &Service{ID: "scoped"} 
        }, Group("mixed"))
        
        collection.AddTransient(func() *Service { 
            return &Service{ID: "transient"} 
        }, Group("mixed"))
        
        provider, _ := collection.Build()
        defer provider.Close()
        
        // Test from root provider
        services1, _ := provider.GetGroup(serviceType, "mixed")
        services2, _ := provider.GetGroup(serviceType, "mixed")
        
        // Singleton should be same instance
        assert.Same(t, services1[0], services2[0])
        
        // Scoped should be same in provider context
        assert.Same(t, services1[1], services2[1])
        
        // Transient should be different
        assert.NotSame(t, services1[2], services2[2])
        
        // Test from scope
        scope1, _ := provider.CreateScope(context.Background())
        scope2, _ := provider.CreateScope(context.Background())
        
        scope1Services, _ := scope1.GetGroup(serviceType, "mixed")
        scope2Services, _ := scope2.GetGroup(serviceType, "mixed")
        
        // Singleton same across scopes
        assert.Same(t, scope1Services[0], scope2Services[0])
        
        // Scoped different across scopes
        assert.NotSame(t, scope1Services[1], scope2Services[1])
        
        // Transient always different
        assert.NotSame(t, scope1Services[2], scope2Services[2])
    })
}
```

### Issue 2: Missing Documentation

**Current State**: No documentation about mixed lifetime behavior in groups

**Recommendation**: Add documentation in `docs/howto/service-groups.md`:

```markdown
## Mixed Lifetime Groups

Service groups can contain services with different lifetimes. Each service 
maintains its lifetime semantics:

- **Singleton**: Same instance everywhere
- **Scoped**: Same instance within a scope
- **Transient**: New instance every time

This is useful for plugin systems where some plugins are stateless (transient),
some maintain request state (scoped), and some are application-wide (singleton).
```

### Issue 3: Potential Performance Consideration

**Current State**: Groups are resolved sequentially, mixing cached and new instances

**Recommendation**: Consider adding a flag to pre-resolve scoped services in groups for better performance in hot paths

## Solution Implementation

### 1. Add Comprehensive Test Suite

Create a new test file `mixed_lifetime_groups_test.go`:

```go
package godi

import (
    "context"
    "sync"
    "testing"
    "github.com/stretchr/testify/assert"
)

func TestMixedLifetimeGroupBehavior(t *testing.T) {
    // Test all combinations of lifetimes
    // Test concurrent access
    // Test disposal ordering
    // Test dependency resolution within mixed groups
}
```

### 2. Add Validation (Optional)

Consider adding a warning when mixing lifetimes in performance-critical paths:

```go
func (c *collection) validateGroupLifetimes() []string {
    var warnings []string
    for groupKey, descriptors := range c.groups {
        lifetimes := make(map[Lifetime]bool)
        for _, d := range descriptors {
            lifetimes[d.Lifetime] = true
        }
        
        if len(lifetimes) > 1 && lifetimes[Transient] {
            warnings = append(warnings, 
                fmt.Sprintf("Group '%s' mixes transient with cached lifetimes, "+
                    "may impact performance", groupKey.Group))
        }
    }
    return warnings
}
```

### 3. Enhance GetGroup Implementation

Current implementation is correct but could benefit from optimization:

```go
func (s *scope) GetGroup(serviceType reflect.Type, group string) ([]any, error) {
    // ... existing validation ...
    
    // Pre-allocate with exact capacity
    instances := make([]any, 0, len(descriptors))
    
    // Consider parallel resolution for transient services
    // when group size is large (>10 services)
    
    // ... existing resolution logic ...
}
```

## Conclusion

The current Godi implementation **correctly handles** mixed lifetime groups, preserving each service's lifetime semantics. However, it needs:

1. **More comprehensive testing** to ensure edge cases are covered
2. **Clear documentation** about the behavior
3. **Performance optimizations** for large mixed groups (optional)

The best result is already achieved in terms of correctness - each service maintains its lifetime behavior independently within the group. The recommendations focus on improving confidence, clarity, and performance rather than fixing broken functionality.

## Priority Actions

1. **High Priority**: Add comprehensive test coverage for mixed lifetime groups
2. **Medium Priority**: Document the mixed lifetime behavior
3. **Low Priority**: Add performance optimizations for large groups