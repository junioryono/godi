package godi_test

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/junioryono/godi/v3"
	"github.com/junioryono/godi/v3/internal/graph"
	"github.com/junioryono/godi/v3/internal/registry"
)

// Test concurrent graph operations
func TestDependencyGraph_ConcurrentOperations(t *testing.T) {
	g := graph.New()

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	types := make([]reflect.Type, 10)
	for i := 0; i < 10; i++ {
		types[i] = reflect.TypeOf(fmt.Sprintf("Service%d", i))
	}

	// Concurrent additions
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			provider := &registry.Descriptor{
				Type:     types[idx],
				Lifetime: registry.Singleton,
				Dependencies: func() []*registry.Dependency {
					if idx == 0 {
						return nil
					}
					// Each service depends on the previous one
					return []*registry.Dependency{
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
			if _, err := g.TopologicalSort(); err != nil {
				// This might fail if graph is being modified
				// Don't treat as error in concurrent test
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Error(err)
	}

	// Final verification
	if g.Size() != 10 {
		t.Errorf("Expected 10 nodes, got %d", g.Size())
	}

	// Should be acyclic (linear chain)
	if !g.IsAcyclic() {
		t.Error("Graph should be acyclic")
	}
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
				g := graph.New()
				type1 := reflect.TypeOf("Service1")

				provider := &registry.Descriptor{
					Type: type1,
					Dependencies: []*registry.Dependency{
						{Type: type1}, // Self dependency
					},
				}

				err := g.AddProvider(provider)
				return g, err
			},
			expectCycle:   true,
			cycleIncludes: []string{"Service1"},
		},
		{
			name: "diamond-no-cycle",
			setupGraph: func() (*graph.DependencyGraph, error) {
				g := graph.New()

				// Create diamond: A -> B -> D, A -> C -> D
				typeA := reflect.TypeOf("A")
				typeB := reflect.TypeOf("B")
				typeC := reflect.TypeOf("C")
				typeD := reflect.TypeOf("D")

				providers := []*registry.Descriptor{
					{Type: typeD, Dependencies: nil},
					{Type: typeB, Dependencies: []*registry.Dependency{{Type: typeD}}},
					{Type: typeC, Dependencies: []*registry.Dependency{{Type: typeD}}},
					{Type: typeA, Dependencies: []*registry.Dependency{
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
				g := graph.New()

				// Create: A -> B -> C -> A (cycle)
				//         B -> D -> E -> B (another cycle)
				typeA := reflect.TypeOf("A")
				typeB := reflect.TypeOf("B")
				typeC := reflect.TypeOf("C")

				g.AddProvider(&registry.Descriptor{
					Type:         typeB,
					Dependencies: []*registry.Dependency{{Type: typeC}},
				})

				g.AddProvider(&registry.Descriptor{
					Type:         typeC,
					Dependencies: []*registry.Dependency{{Type: typeA}},
				})

				// This should fail
				err := g.AddProvider(&registry.Descriptor{
					Type:         typeA,
					Dependencies: []*registry.Dependency{{Type: typeB}},
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
				if err == nil {
					t.Error("Expected cycle error, got nil")
				}

				if !graph.IsCycleError(err) {
					t.Errorf("Expected CycleError, got %T: %v", err, err)
				}

				if path, ok := graph.GetCyclePath(err); ok {
					t.Logf("Cycle path: %v", path)

					// Verify expected nodes are in cycle
					for _, expected := range tt.cycleIncludes {
						found := false
						for _, node := range path {
							if strings.Contains(node.String(), expected) {
								found = true
								break
							}
						}
						if !found {
							t.Errorf("Expected %s in cycle path", expected)
						}
					}
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}

				if !g.IsAcyclic() {
					t.Error("Graph should be acyclic")
				}
			}
		})
	}
}

// Test depth calculation with complex graphs
func TestDependencyGraph_DepthCalculation(t *testing.T) {
	g := graph.New()

	// Create a more complex graph:
	//     A
	//    / \
	//   B   C
	//  /|   |\
	// D E   F G
	//  \|   |/
	//    H I

	types := make(map[string]reflect.Type)
	for _, name := range []string{"A", "B", "C", "D", "E", "F", "G", "H", "I"} {
		types[name] = reflect.TypeOf(name)
	}

	providers := []*registry.Descriptor{
		// Leaves first
		{Type: types["H"], Dependencies: []*registry.Dependency{
			{Type: types["D"]},
			{Type: types["E"]},
		}},
		{Type: types["I"], Dependencies: []*registry.Dependency{
			{Type: types["F"]},
			{Type: types["G"]},
		}},

		// Middle layer
		{Type: types["D"], Dependencies: []*registry.Dependency{{Type: types["B"]}}},
		{Type: types["E"], Dependencies: []*registry.Dependency{{Type: types["B"]}}},
		{Type: types["F"], Dependencies: []*registry.Dependency{{Type: types["C"]}}},
		{Type: types["G"], Dependencies: []*registry.Dependency{{Type: types["C"]}}},

		// Second layer
		{Type: types["B"], Dependencies: []*registry.Dependency{{Type: types["A"]}}},
		{Type: types["C"], Dependencies: []*registry.Dependency{{Type: types["A"]}}},

		// Root
		{Type: types["A"], Dependencies: nil},
	}

	for _, p := range providers {
		if err := g.AddProvider(p); err != nil {
			t.Fatalf("Failed to add provider: %v", err)
		}
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
		node := g.GetNode(types[name], nil)
		if node == nil {
			t.Errorf("Node %s not found", name)
			continue
		}

		if node.Depth != expectedDepth {
			t.Errorf("Node %s: expected depth %d, got %d", name, expectedDepth, node.Depth)
		}
	}
}

// Test transitive dependencies with keyed services
func TestDependencyGraph_TransitiveDependenciesWithKeys(t *testing.T) {
	g := graph.New()

	dbType := reflect.TypeOf("Database")
	cacheType := reflect.TypeOf("Cache")
	serviceType := reflect.TypeOf("Service")

	// Create: Service[primary] -> Cache[primary] -> Database[primary]
	//         Service[backup] -> Cache[backup] -> Database[backup]

	providers := []*registry.Descriptor{
		// Databases
		{Type: dbType, Key: "primary", Dependencies: nil},
		{Type: dbType, Key: "backup", Dependencies: nil},

		// Caches
		{Type: cacheType, Key: "primary", Dependencies: []*registry.Dependency{
			{Type: dbType, Key: "primary"},
		}},
		{Type: cacheType, Key: "backup", Dependencies: []*registry.Dependency{
			{Type: dbType, Key: "backup"},
		}},

		// Services
		{Type: serviceType, Key: "primary", Dependencies: []*registry.Dependency{
			{Type: cacheType, Key: "primary"},
		}},
		{Type: serviceType, Key: "backup", Dependencies: []*registry.Dependency{
			{Type: cacheType, Key: "backup"},
		}},
	}

	for _, p := range providers {
		if err := g.AddProvider(p); err != nil {
			t.Fatalf("Failed to add provider: %v", err)
		}
	}

	// Get transitive dependencies of primary service
	deps := g.GetTransitiveDependencies(serviceType, "primary")

	if len(deps) != 2 {
		t.Errorf("Expected 2 transitive dependencies, got %d", len(deps))
	}

	// Verify we only get primary chain, not backup
	for _, dep := range deps {
		if dep.Key != nil && dep.Key != "primary" {
			t.Errorf("Should only have primary dependencies, got key %v", dep.Key)
		}
	}
}

// Test node removal and graph consistency
func TestDependencyGraph_RemovalConsistency(t *testing.T) {
	g := graph.New()

	type1 := reflect.TypeOf("Service1")
	type2 := reflect.TypeOf("Service2")
	type3 := reflect.TypeOf("Service3")

	// Create chain: 3 -> 2 -> 1
	providers := []*registry.Descriptor{
		{Type: type1, Dependencies: nil},
		{Type: type2, Dependencies: []*registry.Dependency{{Type: type1}}},
		{Type: type3, Dependencies: []*registry.Dependency{{Type: type2}}},
	}

	for _, p := range providers {
		g.AddProvider(p)
	}

	// Verify initial state
	if g.Size() != 3 {
		t.Errorf("Expected 3 nodes, got %d", g.Size())
	}

	// Remove middle node
	g.RemoveProvider(type2, nil)

	if g.Size() != 2 {
		t.Errorf("Expected 2 nodes after removal, got %d", g.Size())
	}

	// Service3's dependencies should be updated
	deps := g.GetDependencies(type3, nil)
	if len(deps) != 0 {
		t.Errorf("Service3 should have no dependencies after Service2 removal, got %d", len(deps))
	}

	// Service1 should have no dependents
	dependents := g.GetDependents(type1, nil)
	if len(dependents) != 0 {
		t.Errorf("Service1 should have no dependents after Service2 removal, got %d", len(dependents))
	}
}

// Benchmark topological sort performance
func BenchmarkDependencyGraph_TopologicalSort(b *testing.B) {
	// Create a large graph
	g := godi.NewDependencyGraph()

	numNodes := 100
	types := make([]reflect.Type, numNodes)

	for i := 0; i < numNodes; i++ {
		types[i] = reflect.TypeOf(fmt.Sprintf("Service%d", i))
	}

	// Create a linear chain of dependencies
	for i := 0; i < numNodes; i++ {
		deps := []*registry.Dependency{}
		if i > 0 {
			deps = append(deps, &registry.Dependency{Type: types[i-1]})
		}

		g.AddProvider(&registry.Descriptor{
			Type:         types[i],
			Dependencies: deps,
		})
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := g.TopologicalSort()
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Test cache invalidation
func TestDependencyGraph_CacheInvalidation(t *testing.T) {
	g := graph.New()

	type1 := reflect.TypeOf("Service1")
	type2 := reflect.TypeOf("Service2")

	// Add initial provider
	g.AddProvider(&registry.Descriptor{
		Type:         type1,
		Dependencies: nil,
	})

	// Perform topological sort (should cache)
	sorted1, _ := g.TopologicalSort()

	// Add another provider
	g.AddProvider(&registry.Descriptor{
		Type:         type2,
		Dependencies: []*registry.Dependency{{Type: type1}},
	})

	// Sort again (cache should be invalidated)
	sorted2, _ := g.TopologicalSort()

	if len(sorted1) == len(sorted2) {
		t.Error("Cache should have been invalidated after adding provider")
	}

	// Multiple sorts without changes should return consistent results
	sorted3, _ := g.TopologicalSort()
	sorted4, _ := g.TopologicalSort()

	if len(sorted3) != len(sorted4) {
		t.Error("Cached results should be consistent")
	}
}
