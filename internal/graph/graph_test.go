package graph_test

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/junioryono/godi/v4"
	"github.com/junioryono/godi/v4/internal/graph"
	"github.com/junioryono/godi/v4/internal/reflection"
	"github.com/stretchr/testify/assert"
)

// Test concurrent graph operations
func TestDependencyGraph_ConcurrentOperations(t *testing.T) {
	type Service0 struct{}
	type Service1 struct{}
	type Service2 struct{}
	type Service3 struct{}
	type Service4 struct{}
	type Service5 struct{}
	type Service6 struct{}
	type Service7 struct{}
	type Service8 struct{}
	type Service9 struct{}

	g := graph.NewDependencyGraph()

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	types := []reflect.Type{
		reflect.TypeOf(Service0{}),
		reflect.TypeOf(Service1{}),
		reflect.TypeOf(Service2{}),
		reflect.TypeOf(Service3{}),
		reflect.TypeOf(Service4{}),
		reflect.TypeOf(Service5{}),
		reflect.TypeOf(Service6{}),
		reflect.TypeOf(Service7{}),
		reflect.TypeOf(Service8{}),
		reflect.TypeOf(Service9{}),
	}

	// Concurrent additions
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			provider := &godi.Descriptor{
				Type:     types[idx],
				Lifetime: godi.Singleton,
				Dependencies: func() []*reflection.Dependency {
					if idx == 0 {
						return nil
					}
					// Each service depends on the previous one
					return []*reflection.Dependency{
						{Type: types[idx-1]},
					}
				}(),
			}

			if err := g.AddProvider(provider); err != nil {
				errors <- fmt.Errorf("failed to add provider %d: %w", idx, err)
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Perform various read operations
			g.Size()
			g.IsAcyclic()
			g.GetRoots()
			g.GetLeaves()

			// Try topological sort
			if _, err := g.TopologicalSort(); err != nil { //nolint:staticcheck // error expected in concurrent test
				// This might fail if graph is being modified
				// Don't treat as error in concurrent test
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		assert.NoError(t, err, "Concurrent operation error")
	}

	// Final verification
	assert.Equal(t, 10, g.Size(), "Expected 10 nodes in graph after concurrent operations")

	// Should be acyclic (linear chain)
	assert.True(t, g.IsAcyclic(), "Graph should be acyclic")
}

// Test complex cycle detection scenarios
func TestDependencyGraph_ComplexCycles(t *testing.T) {
	tests := []struct {
		name          string
		setupGraph    func() (*graph.DependencyGraph, error)
		expectCycle   bool
		cycleIncludes []string
	}{
		{
			name: "self-cycle",
			setupGraph: func() (*graph.DependencyGraph, error) {
				g := graph.NewDependencyGraph()
				type SelfCycleService struct{}
				type1 := reflect.TypeOf(SelfCycleService{})

				provider := &godi.Descriptor{
					Type: type1,
					Dependencies: []*reflection.Dependency{
						{Type: type1}, // Self dependency
					},
				}

				err := g.AddProvider(provider)
				return g, err
			},
			expectCycle:   true,
			cycleIncludes: []string{"SelfCycleService"},
		},
		{
			name: "diamond-no-cycle",
			setupGraph: func() (*graph.DependencyGraph, error) {
				g := graph.NewDependencyGraph()

				// Create diamond: A -> B -> D, A -> C -> D
				type DiamondA struct{}
				type DiamondB struct{}
				type DiamondC struct{}
				type DiamondD struct{}
				typeA := reflect.TypeOf(DiamondA{})
				typeB := reflect.TypeOf(DiamondB{})
				typeC := reflect.TypeOf(DiamondC{})
				typeD := reflect.TypeOf(DiamondD{})

				providers := []*godi.Descriptor{
					{Type: typeD, Dependencies: nil},
					{Type: typeB, Dependencies: []*reflection.Dependency{{Type: typeD}}},
					{Type: typeC, Dependencies: []*reflection.Dependency{{Type: typeD}}},
					{Type: typeA, Dependencies: []*reflection.Dependency{
						{Type: typeB},
						{Type: typeC},
					}},
				}

				for _, p := range providers {
					if err := g.AddProvider(p); err != nil {
						return g, err
					}
				}

				return g, nil
			},
			expectCycle: false,
		},
		{
			name: "complex-multi-cycle",
			setupGraph: func() (*graph.DependencyGraph, error) {
				g := graph.NewDependencyGraph()

				// Create: A -> B -> C -> A (cycle)
				//         B -> D -> E -> B (another cycle)
				type CycleA struct{}
				type CycleB struct{}
				type CycleC struct{}
				typeA := reflect.TypeOf(CycleA{})
				typeB := reflect.TypeOf(CycleB{})
				typeC := reflect.TypeOf(CycleC{})

				g.AddProvider(&godi.Descriptor{
					Type:         typeB,
					Dependencies: []*reflection.Dependency{{Type: typeC}},
				})

				g.AddProvider(&godi.Descriptor{
					Type:         typeC,
					Dependencies: []*reflection.Dependency{{Type: typeA}},
				})

				// This should fail
				err := g.AddProvider(&godi.Descriptor{
					Type:         typeA,
					Dependencies: []*reflection.Dependency{{Type: typeB}},
				})

				return g, err
			},
			expectCycle: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := tt.setupGraph()

			if tt.expectCycle {
				assert.Error(t, err, "Expected cycle error")

				cErr, ok := err.(*graph.CircularDependencyError)
				assert.True(t, ok, "Expected CircularDependencyError, got %T: %v", err, err)

				t.Logf("Cycle path: %v", cErr.Path)

				// Verify expected nodes are in cycle
				for _, expected := range tt.cycleIncludes {
					found := false
					for _, node := range cErr.Path {
						if strings.Contains(node.String(), expected) {
							found = true
							break
						}
					}
					assert.True(t, found, "Expected %s in cycle path", expected)
				}
			} else {
				assert.NoError(t, err, "Unexpected error")
				assert.True(t, g.IsAcyclic(), "Graph should be acyclic")
			}
		})
	}
}

// Test depth calculation with complex graphs
func TestDependencyGraph_DepthCalculation(t *testing.T) {
	// Test types for depth calculation test
	type DepthA struct{}
	type DepthB struct{}
	type DepthC struct{}
	type DepthD struct{}
	type DepthE struct{}
	type DepthF struct{}
	type DepthG struct{}
	type DepthH struct{}
	type DepthI struct{}

	g := graph.NewDependencyGraph()

	// Create a more complex graph:
	//     A
	//    / \
	//   B   C
	//  /|   |\
	// D E   F G
	//  \|   |/
	//    H I

	types := map[string]reflect.Type{
		"A": reflect.TypeOf(DepthA{}),
		"B": reflect.TypeOf(DepthB{}),
		"C": reflect.TypeOf(DepthC{}),
		"D": reflect.TypeOf(DepthD{}),
		"E": reflect.TypeOf(DepthE{}),
		"F": reflect.TypeOf(DepthF{}),
		"G": reflect.TypeOf(DepthG{}),
		"H": reflect.TypeOf(DepthH{}),
		"I": reflect.TypeOf(DepthI{}),
	}

	providers := []*godi.Descriptor{
		// Root - no dependencies
		{Type: types["A"], Dependencies: nil},

		// Second layer - depends on A
		{Type: types["B"], Dependencies: []*reflection.Dependency{{Type: types["A"]}}},
		{Type: types["C"], Dependencies: []*reflection.Dependency{{Type: types["A"]}}},

		// Middle layer - depends on B or C
		{Type: types["D"], Dependencies: []*reflection.Dependency{{Type: types["B"]}}},
		{Type: types["E"], Dependencies: []*reflection.Dependency{{Type: types["B"]}}},
		{Type: types["F"], Dependencies: []*reflection.Dependency{{Type: types["C"]}}},
		{Type: types["G"], Dependencies: []*reflection.Dependency{{Type: types["C"]}}},

		// Leaves - depend on middle layer
		{Type: types["H"], Dependencies: []*reflection.Dependency{
			{Type: types["D"]},
			{Type: types["E"]},
		}},
		{Type: types["I"], Dependencies: []*reflection.Dependency{
			{Type: types["F"]},
			{Type: types["G"]},
		}},
	}

	for _, p := range providers {
		err := g.AddProvider(p)
		assert.NoError(t, err, "Failed to add provider: %v", p.Type)
	}

	g.CalculateDepths()

	// Verify depths
	expectations := map[string]int{
		"A": 0,
		"B": 1,
		"C": 1,
		"D": 2,
		"E": 2,
		"F": 2,
		"G": 2,
		"H": 3,
		"I": 3,
	}

	for name, expectedDepth := range expectations {
		node := g.GetNode(types[name], nil, "")
		assert.NotNil(t, node, "Node %s not found", name)
		if node != nil {
			assert.Equal(t, expectedDepth, node.Depth, "Node %s: expected depth %d, got %d", name, expectedDepth, node.Depth)
		}
	}
}

// Test transitive dependencies with keyed services
func TestDependencyGraph_TransitiveDependenciesWithKeys(t *testing.T) {
	// Test types for transitive dependencies test
	type Database struct{}
	type Cache struct{}
	type Service struct{}

	g := graph.NewDependencyGraph()

	dbType := reflect.TypeOf(Database{})
	cacheType := reflect.TypeOf(Cache{})
	serviceType := reflect.TypeOf(Service{})

	// Create: Service[primary] -> Cache[primary] -> Database[primary]
	//         Service[backup] -> Cache[backup] -> Database[backup]

	providers := []*godi.Descriptor{
		// Databases
		{Type: dbType, Key: "primary", Dependencies: nil},
		{Type: dbType, Key: "backup", Dependencies: nil},

		// Caches
		{Type: cacheType, Key: "primary", Dependencies: []*reflection.Dependency{
			{Type: dbType, Key: "primary"},
		}},
		{Type: cacheType, Key: "backup", Dependencies: []*reflection.Dependency{
			{Type: dbType, Key: "backup"},
		}},

		// Services
		{Type: serviceType, Key: "primary", Dependencies: []*reflection.Dependency{
			{Type: cacheType, Key: "primary"},
		}},
		{Type: serviceType, Key: "backup", Dependencies: []*reflection.Dependency{
			{Type: cacheType, Key: "backup"},
		}},
	}

	for _, p := range providers {
		err := g.AddProvider(p)
		assert.NoError(t, err, "Failed to add provider: %v", p.Type)
	}

	// Get transitive dependencies of primary service
	deps := g.GetTransitiveDependencies(serviceType, "primary", "")

	assert.Len(t, deps, 2, "Expected 2 transitive dependencies")

	// Verify we only get primary chain, not backup
	for _, dep := range deps {
		if dep.Key != nil {
			assert.Equal(t, "primary", dep.Key, "Should only have primary dependencies, got key %v", dep.Key)
		}
	}
}

// Test node removal and graph consistency
func TestDependencyGraph_RemovalConsistency(t *testing.T) {
	// Test types for removal consistency test
	type RemovalService1 struct{}
	type RemovalService2 struct{}
	type RemovalService3 struct{}

	g := graph.NewDependencyGraph()

	type1 := reflect.TypeOf(RemovalService1{})
	type2 := reflect.TypeOf(RemovalService2{})
	type3 := reflect.TypeOf(RemovalService3{})

	// Create chain: 3 -> 2 -> 1
	providers := []*godi.Descriptor{
		{Type: type1, Dependencies: nil},
		{Type: type2, Dependencies: []*reflection.Dependency{{Type: type1}}},
		{Type: type3, Dependencies: []*reflection.Dependency{{Type: type2}}},
	}

	for _, p := range providers {
		g.AddProvider(p)
	}

	// Verify initial state
	assert.Equal(t, 3, g.Size(), "Expected 3 nodes")

	// Remove middle node
	g.RemoveProvider(type2, nil, "")

	assert.Equal(t, 2, g.Size(), "Expected 2 nodes after removal")

	// Service3's dependencies should be updated
	deps := g.GetDependencies(type3, nil, "")
	assert.Empty(t, deps, "Service3 should have no dependencies after Service2 removal")

	// Service1 should have no dependents
	dependents := g.GetDependents(type1, nil, "")
	assert.Empty(t, dependents, "Service1 should have no dependents after Service2 removal")
}

// Benchmark topological sort performance
func BenchmarkDependencyGraph_TopologicalSort(b *testing.B) {
	// Create a large graph
	g := graph.NewDependencyGraph()

	numNodes := 100
	types := make([]reflect.Type, numNodes)

	for i := 0; i < numNodes; i++ {
		types[i] = reflect.TypeOf(fmt.Sprintf("Service%d", i))
	}

	// Create a linear chain of dependencies
	for i := 0; i < numNodes; i++ {
		deps := []*reflection.Dependency{}
		if i > 0 {
			deps = append(deps, &reflection.Dependency{Type: types[i-1]})
		}

		g.AddProvider(&godi.Descriptor{
			Type:         types[i],
			Dependencies: deps,
		})
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := g.TopologicalSort()
		assert.NoError(b, err)
	}
}

// Test Clear function
func TestDependencyGraph_Clear(t *testing.T) {
	// Add some providers
	type ClearTest1 struct{}
	type ClearTest2 struct{}
	type ClearTest3 struct{}

	g := graph.NewDependencyGraph()

	g.AddProvider(&godi.Descriptor{
		Type:         reflect.TypeOf(ClearTest1{}),
		Dependencies: nil,
	})

	g.AddProvider(&godi.Descriptor{
		Type:         reflect.TypeOf(ClearTest2{}),
		Dependencies: []*reflection.Dependency{{Type: reflect.TypeOf(ClearTest1{})}},
	})

	g.AddProvider(&godi.Descriptor{
		Type:         reflect.TypeOf(ClearTest3{}),
		Dependencies: []*reflection.Dependency{{Type: reflect.TypeOf(ClearTest2{})}},
	})

	// Verify graph has nodes
	assert.Equal(t, 3, g.Size(), "Expected 3 nodes")

	// Clear the graph
	g.Clear()

	// Verify graph is empty
	assert.Equal(t, 0, g.Size(), "Expected 0 nodes after clear")

	// Verify we can still add nodes after clearing
	g.AddProvider(&godi.Descriptor{
		Type:         reflect.TypeOf(ClearTest1{}),
		Dependencies: nil,
	})

	assert.Equal(t, 1, g.Size(), "Expected 1 node after adding to cleared graph")
}

// Test HasNode function
func TestDependencyGraph_HasNode(t *testing.T) {
	g := graph.NewDependencyGraph()

	type HasNodeTest struct{}
	testType := reflect.TypeOf(HasNodeTest{})

	// Check node doesn't exist initially
	assert.False(t, g.HasNode(testType, nil, ""), "HasNode should return false for non-existent node")

	// Add the node
	g.AddProvider(&godi.Descriptor{
		Type:         testType,
		Dependencies: nil,
	})

	// Check node exists
	assert.True(t, g.HasNode(testType, nil, ""), "HasNode should return true for existing node")

	// Check with key
	assert.False(t, g.HasNode(testType, "some-key", ""), "HasNode should return false for non-existent keyed node")

	// Add keyed node
	g.AddProvider(&godi.Descriptor{
		Type:         testType,
		Key:          "test-key",
		Dependencies: nil,
	})

	// Check keyed node exists
	assert.True(t, g.HasNode(testType, "test-key", ""), "HasNode should return true for existing keyed node")
}

// Test NodeKey and Node String methods
func TestDependencyGraph_StringMethods(t *testing.T) {
	type StringTest struct{}

	// Test NodeKey.String() without key
	nodeKey := graph.NodeKey{
		Type: reflect.TypeOf(StringTest{}),
		Key:  nil,
	}

	str := nodeKey.String()
	assert.Contains(t, str, "StringTest", "NodeKey.String() should contain type name")

	// Test NodeKey.String() with key
	nodeKeyWithKey := graph.NodeKey{
		Type: reflect.TypeOf(StringTest{}),
		Key:  "test-key",
	}

	strWithKey := nodeKeyWithKey.String()
	assert.Contains(t, strWithKey, "StringTest", "NodeKey.String() should contain type name")
	assert.Contains(t, strWithKey, "test-key", "NodeKey.String() should contain key")

	// Test Node.String()
	node := &graph.Node{
		Key:       nodeKey,
		InDegree:  2,
		OutDegree: 3,
		Depth:     5,
	}

	nodeStr := node.String()
	assert.Contains(t, nodeStr, "StringTest", "Node.String() should contain type name")
	assert.Contains(t, nodeStr, "in:2", "Node.String() should contain InDegree")
	assert.Contains(t, nodeStr, "out:3", "Node.String() should contain OutDegree")
	assert.Contains(t, nodeStr, "depth:5", "Node.String() should contain Depth")
}

// Test CircularDependencyError.Error()
func TestCircularDependencyError(t *testing.T) {
	type ErrorTest struct{}
	testType := reflect.TypeOf(ErrorTest{})

	// Test with empty path
	err1 := graph.CircularDependencyError{
		Node: graph.NodeKey{Type: testType},
		Path: []graph.NodeKey{},
	}

	errStr1 := err1.Error()
	assert.Contains(t, errStr1, "circular dependency detected", "Error should mention circular dependency")
	assert.Contains(t, errStr1, "ErrorTest", "Error should contain node type")

	// Test with path
	err2 := graph.CircularDependencyError{
		Node: graph.NodeKey{Type: testType},
		Path: []graph.NodeKey{
			{Type: reflect.TypeOf("A")},
			{Type: reflect.TypeOf("B")},
			{Type: reflect.TypeOf("C")},
		},
	}

	errStr2 := err2.Error()
	assert.Contains(t, errStr2, "->", "Error with path should contain arrow notation")
}

// Test edge cases for GetDependencies and GetDependents
func TestDependencyGraph_GetMethods_NonExistent(t *testing.T) {
	g := graph.NewDependencyGraph()

	type NonExistent struct{}
	nonExistentType := reflect.TypeOf(NonExistent{})

	// GetDependencies on non-existent node should return nil
	deps := g.GetDependencies(nonExistentType, nil, "")
	assert.Nil(t, deps, "GetDependencies should return nil for non-existent node")

	// GetDependents on non-existent node should return nil
	dependents := g.GetDependents(nonExistentType, nil, "")
	assert.Nil(t, dependents, "GetDependents should return nil for non-existent node")

	// GetNode on non-existent node should return nil
	node := g.GetNode(nonExistentType, nil, "")
	assert.Nil(t, node, "GetNode should return nil for non-existent node")
}

// Test nil provider handling
func TestDependencyGraph_AddProvider_Nil(t *testing.T) {
	g := graph.NewDependencyGraph()

	err := g.AddProvider(nil)
	assert.Error(t, err, "AddProvider should return error for nil provider")
	assert.Contains(t, err.Error(), "nil", "Error should mention nil provider")
}

// Test removing non-existent provider
func TestDependencyGraph_RemoveProvider_NonExistent(t *testing.T) {
	g := graph.NewDependencyGraph()

	type NonExistent struct{}

	// Should not panic when removing non-existent provider
	assert.NotPanics(t, func() {
		g.RemoveProvider(reflect.TypeOf(NonExistent{}), nil, "")
	}, "Should not panic when removing non-existent provider")

	// Graph should still be empty
	assert.Equal(t, 0, g.Size(), "Expected 0 nodes after removing non-existent provider")
}

// Test cache invalidation
func TestDependencyGraph_CacheInvalidation(t *testing.T) {
	// Test types for cache invalidation test
	type CacheService1 struct{}
	type CacheService2 struct{}

	g := graph.NewDependencyGraph()

	type1 := reflect.TypeOf(CacheService1{})
	type2 := reflect.TypeOf(CacheService2{})

	// Add initial provider
	g.AddProvider(&godi.Descriptor{
		Type:         type1,
		Dependencies: nil,
	})

	// Perform topological sort (should cache)
	sorted1, _ := g.TopologicalSort()

	// Add another provider
	g.AddProvider(&godi.Descriptor{
		Type:         type2,
		Dependencies: []*reflection.Dependency{{Type: type1}},
	})

	// Sort again (cache should be invalidated)
	sorted2, _ := g.TopologicalSort()

	assert.NotEqual(t, len(sorted1), len(sorted2), "Cache should have been invalidated after adding provider")

	// Multiple sorts without changes should return consistent results
	sorted3, _ := g.TopologicalSort()
	sorted4, _ := g.TopologicalSort()

	assert.Equal(t, len(sorted3), len(sorted4), "Cached results should be consistent")
}

// Test complex scenarios for better coverage
func TestDependencyGraph_ComplexScenarios(t *testing.T) {
	t.Run("GetTransitiveDependencies with cycles", func(t *testing.T) {
		g := graph.NewDependencyGraph()

		type TransA struct{}
		type TransB struct{}
		type TransC struct{}

		typeA := reflect.TypeOf(TransA{})
		typeB := reflect.TypeOf(TransB{})
		typeC := reflect.TypeOf(TransC{})

		// Create A -> B -> C
		g.AddProvider(&godi.Descriptor{
			Type:         typeC,
			Dependencies: nil,
		})

		g.AddProvider(&godi.Descriptor{
			Type:         typeB,
			Dependencies: []*reflection.Dependency{{Type: typeC}},
		})

		g.AddProvider(&godi.Descriptor{
			Type:         typeA,
			Dependencies: []*reflection.Dependency{{Type: typeB}},
		})

		// Get transitive dependencies of A
		deps := g.GetTransitiveDependencies(typeA, nil, "")

		// Should have B and C
		assert.Len(t, deps, 2, "Expected 2 transitive dependencies")
	})

	t.Run("TopologicalSort with insufficient nodes", func(t *testing.T) {
		g := graph.NewDependencyGraph()

		type SortA struct{}
		type SortB struct{}

		typeA := reflect.TypeOf(SortA{})
		typeB := reflect.TypeOf(SortB{})

		// Add B depending on A, but don't add A as a provider
		g.AddProvider(&godi.Descriptor{
			Type:         typeB,
			Dependencies: []*reflection.Dependency{{Type: typeA}},
		})

		// This creates a node for A without a provider
		sorted, err := g.TopologicalSort()

		// Should succeed - missing providers are OK in the graph
		assert.NoError(t, err, "TopologicalSort should handle missing providers")

		// Should have both nodes
		assert.Len(t, sorted, 2, "Expected 2 nodes in sorted result")
	})

	t.Run("Complex cycle path finding", func(t *testing.T) {
		g := graph.NewDependencyGraph()

		type PathA struct{}
		type PathB struct{}
		type PathC struct{}
		type PathD struct{}

		typeA := reflect.TypeOf(PathA{})
		typeB := reflect.TypeOf(PathB{})
		typeC := reflect.TypeOf(PathC{})
		typeD := reflect.TypeOf(PathD{})

		// Create: A -> B -> C -> D -> B (cycle)
		g.AddProvider(&godi.Descriptor{
			Type:         typeA,
			Dependencies: []*reflection.Dependency{{Type: typeB}},
		})

		g.AddProvider(&godi.Descriptor{
			Type:         typeB,
			Dependencies: []*reflection.Dependency{{Type: typeC}},
		})

		g.AddProvider(&godi.Descriptor{
			Type:         typeC,
			Dependencies: []*reflection.Dependency{{Type: typeD}},
		})

		// This should fail with cycle
		err := g.AddProvider(&godi.Descriptor{
			Type:         typeD,
			Dependencies: []*reflection.Dependency{{Type: typeB}},
		})

		assert.Error(t, err, "Expected cycle error")

		cErr, ok := err.(*graph.CircularDependencyError)
		assert.True(t, ok, "Expected CircularDependencyError, got %T", err)

		// Path should contain the cycle
		assert.GreaterOrEqual(t, len(cErr.Path), 2, "Cycle path should have at least 2 nodes")
	})

	t.Run("DetectCycles with cached results", func(t *testing.T) {
		g := graph.NewDependencyGraph()

		type CycleTest1 struct{}
		type CycleTest2 struct{}

		g.AddProvider(&godi.Descriptor{
			Type:         reflect.TypeOf(CycleTest1{}),
			Dependencies: nil,
		})

		g.AddProvider(&godi.Descriptor{
			Type:         reflect.TypeOf(CycleTest2{}),
			Dependencies: []*reflection.Dependency{{Type: reflect.TypeOf(CycleTest1{})}},
		})

		// First call - should build cache
		err1 := g.DetectCycles()
		assert.NoError(t, err1, "No cycles should be detected")

		// Second call - should use cache
		err2 := g.DetectCycles()
		assert.NoError(t, err2, "No cycles should be detected (cached)")
	})

	t.Run("RemoveProvider with complex dependencies", func(t *testing.T) {
		g := graph.NewDependencyGraph()

		type RemoveA struct{}
		type RemoveB struct{}
		type RemoveC struct{}
		type RemoveD struct{}

		typeA := reflect.TypeOf(RemoveA{})
		typeB := reflect.TypeOf(RemoveB{})
		typeC := reflect.TypeOf(RemoveC{})
		typeD := reflect.TypeOf(RemoveD{})

		// Create diamond: D depends on B and C, both depend on A
		g.AddProvider(&godi.Descriptor{
			Type:         typeA,
			Dependencies: nil,
		})

		g.AddProvider(&godi.Descriptor{
			Type:         typeB,
			Dependencies: []*reflection.Dependency{{Type: typeA}},
		})

		g.AddProvider(&godi.Descriptor{
			Type:         typeC,
			Dependencies: []*reflection.Dependency{{Type: typeA}},
		})

		g.AddProvider(&godi.Descriptor{
			Type: typeD,
			Dependencies: []*reflection.Dependency{
				{Type: typeB},
				{Type: typeC},
			},
		})

		// Remove A - this should break the graph
		g.RemoveProvider(typeA, nil, "")

		// B and C should have no dependencies now
		bDeps := g.GetDependencies(typeB, nil, "")
		assert.Empty(t, bDeps, "B should have no dependencies after A removal")

		cDeps := g.GetDependencies(typeC, nil, "")
		assert.Empty(t, cDeps, "C should have no dependencies after A removal")

		// D should still depend on B and C
		dDeps := g.GetDependencies(typeD, nil, "")
		assert.Len(t, dDeps, 2, "D should still have 2 dependencies")
	})
}

func TestTopologicalSort_DependencyOrder(t *testing.T) {
	type ServiceNoDeps struct{}
	type ServiceWithOneDep struct{}
	type ServiceWithTwoDeps struct{}

	tests := []struct {
		name          string
		setupGraph    func() (*graph.DependencyGraph, error)
		expectedOrder []string // Expected type names in order
	}{
		{
			name: "simple_chain",
			setupGraph: func() (*graph.DependencyGraph, error) {
				g := graph.NewDependencyGraph()

				typeNoDeps := reflect.TypeOf(ServiceNoDeps{})
				typeWithOneDep := reflect.TypeOf(ServiceWithOneDep{})

				// ServiceNoDeps has no dependencies
				err := g.AddProvider(&godi.Descriptor{
					Type:         typeNoDeps,
					Dependencies: nil,
				})
				if err != nil {
					return nil, err
				}

				// ServiceWithOneDep depends on ServiceNoDeps
				err = g.AddProvider(&godi.Descriptor{
					Type: typeWithOneDep,
					Dependencies: []*reflection.Dependency{
						{Type: typeNoDeps},
					},
				})
				if err != nil {
					return nil, err
				}

				return g, nil
			},
			expectedOrder: []string{"ServiceNoDeps", "ServiceWithOneDep"},
		},
		{
			name: "complex_dependencies",
			setupGraph: func() (*graph.DependencyGraph, error) {
				g := graph.NewDependencyGraph()

				typeNoDeps := reflect.TypeOf(ServiceNoDeps{})
				typeWithOneDep := reflect.TypeOf(ServiceWithOneDep{})
				typeWithTwoDeps := reflect.TypeOf(ServiceWithTwoDeps{})

				// ServiceNoDeps has no dependencies
				err := g.AddProvider(&godi.Descriptor{
					Type:         typeNoDeps,
					Dependencies: nil,
				})
				if err != nil {
					return nil, err
				}

				// ServiceWithOneDep depends on ServiceNoDeps
				err = g.AddProvider(&godi.Descriptor{
					Type: typeWithOneDep,
					Dependencies: []*reflection.Dependency{
						{Type: typeNoDeps},
					},
				})
				if err != nil {
					return nil, err
				}

				// ServiceWithTwoDeps depends on both ServiceNoDeps and ServiceWithOneDep
				err = g.AddProvider(&godi.Descriptor{
					Type: typeWithTwoDeps,
					Dependencies: []*reflection.Dependency{
						{Type: typeNoDeps},
						{Type: typeWithOneDep},
					},
				})
				if err != nil {
					return nil, err
				}

				return g, nil
			},
			expectedOrder: []string{"ServiceNoDeps", "ServiceWithOneDep", "ServiceWithTwoDeps"},
		},
		{
			name: "diamond_dependency",
			setupGraph: func() (*graph.DependencyGraph, error) {
				g := graph.NewDependencyGraph()

				// Create diamond: D depends on B and C, both B and C depend on A
				//     A
				//    / \
				//   B   C
				//    \ /
				//     D

				type A struct{}
				type B struct{}
				type C struct{}
				type D struct{}

				typeA := reflect.TypeOf(A{})
				typeB := reflect.TypeOf(B{})
				typeC := reflect.TypeOf(C{})
				typeD := reflect.TypeOf(D{})

				// A has no dependencies
				g.AddProvider(&godi.Descriptor{
					Type:         typeA,
					Dependencies: nil,
				})

				// B depends on A
				g.AddProvider(&godi.Descriptor{
					Type: typeB,
					Dependencies: []*reflection.Dependency{
						{Type: typeA},
					},
				})

				// C depends on A
				g.AddProvider(&godi.Descriptor{
					Type: typeC,
					Dependencies: []*reflection.Dependency{
						{Type: typeA},
					},
				})

				// D depends on B and C
				g.AddProvider(&godi.Descriptor{
					Type: typeD,
					Dependencies: []*reflection.Dependency{
						{Type: typeB},
						{Type: typeC},
					},
				})

				return g, nil
			},
			// A must come first, then B and C (in any order), then D
			expectedOrder: []string{"A", "B|C", "B|C", "D"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g, err := tt.setupGraph()
			assert.NoError(t, err, "Failed to setup graph")

			sorted, err := g.TopologicalSort()
			assert.NoError(t, err, "TopologicalSort should not fail")

			// Extract type names from sorted nodes
			actualOrder := make([]string, len(sorted))
			for i, node := range sorted {
				typeName := node.Key.Type.String()
				// Extract just the type name (remove package prefix)
				if idx := len(typeName) - 1; idx >= 0 {
					for j := idx; j >= 0; j-- {
						if typeName[j] == '.' {
							typeName = typeName[j+1:]
							break
						}
					}
				}
				actualOrder[i] = typeName
			}

			// Verify order
			t.Logf("Expected order: %v", tt.expectedOrder)
			t.Logf("Actual order:   %v", actualOrder)

			// For simple validation, check specific constraints
			if tt.name == "simple_chain" {
				// ServiceNoDeps must come before ServiceWithOneDep
				noDepsIdx := slices.Index(actualOrder, "ServiceNoDeps")
				oneDepIdx := slices.Index(actualOrder, "ServiceWithOneDep")

				assert.NotEqual(t, -1, noDepsIdx, "Missing ServiceNoDeps in result")
				assert.NotEqual(t, -1, oneDepIdx, "Missing ServiceWithOneDep in result")
				if noDepsIdx != -1 && oneDepIdx != -1 {
					assert.Less(t, noDepsIdx, oneDepIdx,
						"ServiceNoDeps (index %d) should come before ServiceWithOneDep (index %d)",
						noDepsIdx, oneDepIdx)
				}
			}

			if tt.name == "complex_dependencies" {
				// Check ordering: NoDeps < WithOneDep < WithTwoDeps
				noDepsIdx := slices.Index(actualOrder, "ServiceNoDeps")
				oneDepIdx := slices.Index(actualOrder, "ServiceWithOneDep")
				twoDepsIdx := slices.Index(actualOrder, "ServiceWithTwoDeps")

				assert.NotEqual(t, -1, noDepsIdx, "Missing ServiceNoDeps in result")
				assert.NotEqual(t, -1, oneDepIdx, "Missing ServiceWithOneDep in result")
				assert.NotEqual(t, -1, twoDepsIdx, "Missing ServiceWithTwoDeps in result")

				if noDepsIdx != -1 && oneDepIdx != -1 && twoDepsIdx != -1 {
					assert.Less(t, noDepsIdx, oneDepIdx,
						"ServiceNoDeps (index %d) should come before ServiceWithOneDep (index %d)",
						noDepsIdx, oneDepIdx)
					assert.Less(t, noDepsIdx, twoDepsIdx,
						"ServiceNoDeps (index %d) should come before ServiceWithTwoDeps (index %d)",
						noDepsIdx, twoDepsIdx)
					assert.Less(t, oneDepIdx, twoDepsIdx,
						"ServiceWithOneDep (index %d) should come before ServiceWithTwoDeps (index %d)",
						oneDepIdx, twoDepsIdx)
				}
			}

			if tt.name == "diamond_dependency" {
				// A must come first, D must come last
				assert.GreaterOrEqual(t, len(actualOrder), 4, "Expected at least 4 nodes")
				if len(actualOrder) >= 4 {
					assert.Equal(t, "A", actualOrder[0], "A should be first")
					assert.Equal(t, "D", actualOrder[3], "D should be last")

					// B and C should be in positions 1 and 2 (any order)
					hasB := actualOrder[1] == "B" || actualOrder[2] == "B"
					hasC := actualOrder[1] == "C" || actualOrder[2] == "C"
					assert.True(t, hasB && hasC, "B and C should be in middle positions, got %v", actualOrder)
				}
			}
		})
	}
}

func TestTopologicalSort_ForDependencyInjection(t *testing.T) {
	g := graph.NewDependencyGraph()

	// Types from the failing test
	type ResolutionTestService struct{}
	type ResolutionServiceWithDep struct{}

	typeTestService := reflect.TypeOf(&ResolutionTestService{})
	typeServiceWithDep := reflect.TypeOf(&ResolutionServiceWithDep{})

	// Add ResolutionTestService (no dependencies)
	err := g.AddProvider(&godi.Descriptor{
		Type:         typeTestService,
		Dependencies: nil,
		Lifetime:     godi.Singleton,
	})
	assert.NoError(t, err, "Failed to add ResolutionTestService")

	// Add ResolutionServiceWithDep (depends on ResolutionTestService)
	err = g.AddProvider(&godi.Descriptor{
		Type: typeServiceWithDep,
		Dependencies: []*reflection.Dependency{
			{Type: typeTestService},
		},
		Lifetime: godi.Singleton,
	})
	assert.NoError(t, err, "Failed to add ResolutionServiceWithDep")

	// Get topological sort
	sorted, err := g.TopologicalSort()
	assert.NoError(t, err, "TopologicalSort should not fail")

	// Log the order
	t.Logf("Topological sort order:")
	for i, node := range sorted {
		t.Logf("  %d: Type=%v, Key=%v", i, node.Key.Type, node.Key.Key)
	}

	// Verify order: ResolutionTestService MUST come before ResolutionServiceWithDep
	assert.Len(t, sorted, 2, "Expected 2 nodes in sorted result")

	// The first node should be ResolutionTestService (no dependencies)
	assert.Equal(t, typeTestService, sorted[0].Key.Type, "First node should be ResolutionTestService (no deps)")

	// The second node should be ResolutionServiceWithDep (depends on first)
	assert.Equal(t, typeServiceWithDep, sorted[1].Key.Type, "Second node should be ResolutionServiceWithDep (has deps)")
}
