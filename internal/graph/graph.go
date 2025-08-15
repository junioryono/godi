package graph

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/junioryono/godi/v3/internal/reflection"
)

// Provider defines the interface for service providers that can be added to the graph.
// This abstraction allows the graph to work with different provider implementations.
type Provider interface {
	// GetType returns the service type this provider produces
	GetType() reflect.Type

	// GetKey returns the optional key for named/keyed services
	GetKey() any

	// GetDependencies returns the analyzed dependencies
	GetDependencies() []*reflection.Dependency
}

// DependencyGraph manages the dependency relationships between services.
// It provides cycle detection, topological sorting, and dependency analysis.
type DependencyGraph struct {
	mu    sync.RWMutex
	nodes map[NodeKey]*Node
	edges map[NodeKey][]NodeKey // adjacency list representation

	// Cache for performance
	sortedNodes      []*Node
	sortedNodesDirty bool
	cycleCache       map[NodeKey]bool
	cycleCacheDirty  bool
}

// NodeKey uniquely identifies a node in the graph
type NodeKey struct {
	Type reflect.Type
	Key  any // for keyed services
}

// Node represents a service in the dependency graph
type Node struct {
	Key      NodeKey
	Provider Provider

	// Graph metadata
	InDegree  int  // number of dependencies
	OutDegree int  // number of dependents
	Visited   bool // for traversal algorithms
	Visiting  bool // for cycle detection
	Depth     int  // depth in dependency tree

	// Dependency information
	Dependencies []NodeKey // services this node depends on
	Dependents   []NodeKey // services that depend on this node
}

// NewDependencyGraph creates a new dependency graph
func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		nodes:            make(map[NodeKey]*Node),
		edges:            make(map[NodeKey][]NodeKey),
		cycleCache:       make(map[NodeKey]bool),
		sortedNodesDirty: true,
		cycleCacheDirty:  true,
	}
}

// AddProvider adds a provider to the graph and analyzes its dependencies
func (g *DependencyGraph) AddProvider(provider Provider) error {
	if provider == nil {
		return fmt.Errorf("provider cannot be nil")
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Create node key
	nodeKey := NodeKey{
		Type: provider.GetType(),
		Key:  provider.GetKey(),
	}

	// Create or update node
	node, exists := g.nodes[nodeKey]
	if !exists {
		node = &Node{
			Key:          nodeKey,
			Dependencies: make([]NodeKey, 0),
			Dependents:   make([]NodeKey, 0),
		}
		g.nodes[nodeKey] = node
	}
	node.Provider = provider

	// Clear existing edges for this node (in case of replacement)
	delete(g.edges, nodeKey)

	// Add edges based on dependencies
	providerDeps := provider.GetDependencies()
	dependencies := make([]NodeKey, 0, len(providerDeps))
	for _, dep := range providerDeps {
		depKey := NodeKey{
			Type: dep.Type,
			Key:  dep.Key,
		}
		dependencies = append(dependencies, depKey)

		// Ensure dependency node exists
		if _, exists := g.nodes[depKey]; !exists {
			g.nodes[depKey] = &Node{
				Key:          depKey,
				Dependencies: make([]NodeKey, 0),
				Dependents:   make([]NodeKey, 0),
			}
		}
	}

	node.Dependencies = dependencies
	g.edges[nodeKey] = dependencies

	// Update in/out degrees
	g.updateDegrees()

	// Mark caches as dirty
	g.sortedNodesDirty = true
	g.cycleCacheDirty = true

	// Check for cycles immediately
	if err := g.detectCyclesFrom(nodeKey); err != nil {
		// Remove the node if it creates a cycle
		delete(g.nodes, nodeKey)
		delete(g.edges, nodeKey)
		g.updateDegrees()
		return err
	}

	return nil
}

// RemoveProvider removes a provider from the graph
func (g *DependencyGraph) RemoveProvider(serviceType reflect.Type, key any) {
	g.mu.Lock()
	defer g.mu.Unlock()

	nodeKey := NodeKey{
		Type: serviceType,
		Key:  key,
	}

	// Store the node before removing for cleanup
	removedNode, exists := g.nodes[nodeKey]
	if !exists {
		return // Node doesn't exist, nothing to remove
	}

	// Remove node and its edges
	delete(g.nodes, nodeKey)
	delete(g.edges, nodeKey)

	// Remove edges pointing to this node and update dependent nodes
	for k, edges := range g.edges {
		filtered := make([]NodeKey, 0, len(edges))
		modified := false

		for _, edge := range edges {
			if edge != nodeKey {
				filtered = append(filtered, edge)
			} else {
				modified = true
			}
		}

		if modified {
			g.edges[k] = filtered

			// Update the Dependencies field of the dependent node
			if dependentNode, exists := g.nodes[k]; exists {
				updatedDeps := make([]NodeKey, 0, len(dependentNode.Dependencies))
				for _, dep := range dependentNode.Dependencies {
					if dep != nodeKey {
						updatedDeps = append(updatedDeps, dep)
					}
				}
				dependentNode.Dependencies = updatedDeps
			}
		}
	}

	// Update Dependents field of nodes that this node depended on
	for _, dependency := range removedNode.Dependencies {
		if depNode, exists := g.nodes[dependency]; exists {
			updatedDependents := make([]NodeKey, 0, len(depNode.Dependents))
			for _, dependent := range depNode.Dependents {
				if dependent != nodeKey {
					updatedDependents = append(updatedDependents, dependent)
				}
			}
			depNode.Dependents = updatedDependents
		}
	}

	// Update degrees
	g.updateDegrees()

	// Mark caches as dirty
	g.sortedNodesDirty = true
	g.cycleCacheDirty = true
}

// updateDegrees recalculates in/out degrees for all nodes
func (g *DependencyGraph) updateDegrees() {
	// Reset all degrees and dependent lists
	for _, node := range g.nodes {
		node.InDegree = 0
		node.OutDegree = 0
		node.Dependents = make([]NodeKey, 0, 4) // Pre-allocate with reasonable capacity
	}

	// Calculate degrees from edges in a single pass
	for from, tos := range g.edges {
		if fromNode, exists := g.nodes[from]; exists {
			fromNode.OutDegree = len(tos)
			fromNode.Dependencies = make([]NodeKey, len(tos))
			copy(fromNode.Dependencies, tos)

			for _, to := range tos {
				if toNode, exists := g.nodes[to]; exists {
					toNode.InDegree++
					toNode.Dependents = append(toNode.Dependents, from)
				}
			}
		}
	}
}

// TopologicalSort returns nodes in dependency order (dependencies first)
func (g *DependencyGraph) TopologicalSort() ([]*Node, error) {
	g.mu.RLock()

	// Return cached result if available
	if !g.sortedNodesDirty && g.sortedNodes != nil {
		result := make([]*Node, len(g.sortedNodes))
		copy(result, g.sortedNodes)
		g.mu.RUnlock()
		return result, nil
	}
	g.mu.RUnlock()

	g.mu.Lock()
	defer g.mu.Unlock()

	// Perform Kahn's algorithm for topological sort
	result := make([]*Node, 0, len(g.nodes))

	// Create working copies of in-degrees
	inDegrees := make(map[NodeKey]int)
	for key, node := range g.nodes {
		inDegrees[key] = node.InDegree
	}

	// Find all nodes with no dependencies
	queue := make([]NodeKey, 0)
	for key, degree := range inDegrees {
		if degree == 0 {
			queue = append(queue, key)
		}
	}

	// Process queue
	for len(queue) > 0 {
		// Dequeue
		current := queue[0]
		queue = queue[1:]

		node := g.nodes[current]
		if node != nil {
			result = append(result, node)

			// Reduce in-degree of dependent nodes
			if edges, exists := g.edges[current]; exists {
				for _, dependent := range edges {
					inDegrees[dependent]--
					if inDegrees[dependent] == 0 {
						queue = append(queue, dependent)
					}
				}
			}
		}
	}

	// Check if all nodes were processed (no cycles)
	if len(result) != len(g.nodes) {
		return nil, fmt.Errorf("circular dependency detected: graph contains %d nodes but only %d could be sorted",
			len(g.nodes), len(result))
	}

	// Cache the result
	g.sortedNodes = result
	g.sortedNodesDirty = false

	// Return a copy
	resultCopy := make([]*Node, len(result))
	copy(resultCopy, result)
	return resultCopy, nil
}

// DetectCycles checks if the graph contains any cycles
func (g *DependencyGraph) DetectCycles() error {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Check cache
	if !g.cycleCacheDirty {
		for key, hasCycle := range g.cycleCache {
			if hasCycle {
				path := g.findCyclePath(key)
				return &CircularDependencyError{
					Node: key,
					Path: path,
				}
			}
		}
		return nil
	}

	// Clear visit flags
	for _, node := range g.nodes {
		node.Visited = false
		node.Visiting = false
	}

	// Clear cycle cache
	g.cycleCache = make(map[NodeKey]bool)

	// Check each node for cycles using DFS
	for key := range g.nodes {
		if !g.nodes[key].Visited {
			if err := g.detectCyclesFrom(key); err != nil {
				g.cycleCacheDirty = false
				return err
			}
		}
	}

	g.cycleCacheDirty = false
	return nil
}

// detectCyclesFrom performs DFS cycle detection from a specific node
func (g *DependencyGraph) detectCyclesFrom(start NodeKey) error {
	node := g.nodes[start]
	if node == nil {
		return nil
	}

	// Use a stack-based approach to avoid deep recursion
	type stackItem struct {
		key      NodeKey
		visiting bool
	}

	stack := []stackItem{{key: start, visiting: true}}
	visiting := make(map[NodeKey]bool)
	visited := make(map[NodeKey]bool)

	for len(stack) > 0 {
		item := stack[len(stack)-1]

		if !item.visiting {
			// Backtracking
			stack = stack[:len(stack)-1]
			delete(visiting, item.key)
			visited[item.key] = true
			g.cycleCache[item.key] = false
			continue
		}

		// Mark as visiting
		if visiting[item.key] {
			// Found a cycle
			path := g.findCyclePath(item.key)
			g.cycleCache[item.key] = true
			return &CircularDependencyError{
				Node: item.key,
				Path: path,
			}
		}

		if visited[item.key] {
			stack = stack[:len(stack)-1]
			continue
		}

		visiting[item.key] = true
		stack[len(stack)-1].visiting = false // Mark for backtracking

		// Add dependencies to stack
		if edges, exists := g.edges[item.key]; exists {
			for _, dep := range edges {
				if !visited[dep] {
					stack = append(stack, stackItem{key: dep, visiting: true})
				}
			}
		}
	}

	return nil
}

// findCyclePath reconstructs the cycle path for error reporting
func (g *DependencyGraph) findCyclePath(start NodeKey) []NodeKey {
	path := []NodeKey{}
	visited := make(map[NodeKey]bool)
	parent := make(map[NodeKey]NodeKey)

	// Use BFS to find the cycle more efficiently
	var findPath func(current NodeKey) bool
	findPath = func(current NodeKey) bool {
		if visited[current] {
			// Found a node we've seen before - reconstruct cycle
			cycle := []NodeKey{current}
			for p := parent[current]; p != current && !visited[p]; p = parent[p] {
				cycle = append([]NodeKey{p}, cycle...)
				visited[p] = true

				// Safety check to prevent infinite loop
				if len(cycle) > len(g.nodes) {
					break
				}
			}
			path = cycle
			return true
		}

		visited[current] = true

		if edges, exists := g.edges[current]; exists {
			for _, next := range edges {
				if _, hasParent := parent[next]; !hasParent {
					parent[next] = current
				}

				if next == start || findPath(next) {
					if len(path) == 0 {
						path = []NodeKey{current}
					} else if path[0] != current {
						path = append([]NodeKey{current}, path...)
					}
					return true
				}
			}
		}

		return false
	}

	findPath(start)

	// Ensure the path shows the complete cycle
	if len(path) > 0 && path[len(path)-1] != start {
		path = append(path, start)
	}

	return path
}

// GetDependencies returns the direct dependencies of a service
func (g *DependencyGraph) GetDependencies(serviceType reflect.Type, key any) []NodeKey {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodeKey := NodeKey{Type: serviceType, Key: key}
	if node, exists := g.nodes[nodeKey]; exists {
		result := make([]NodeKey, len(node.Dependencies))
		copy(result, node.Dependencies)
		return result
	}

	return nil
}

// GetDependents returns services that depend on the given service
func (g *DependencyGraph) GetDependents(serviceType reflect.Type, key any) []NodeKey {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodeKey := NodeKey{Type: serviceType, Key: key}
	if node, exists := g.nodes[nodeKey]; exists {
		result := make([]NodeKey, len(node.Dependents))
		copy(result, node.Dependents)
		return result
	}

	return nil
}

// GetTransitiveDependencies returns all dependencies (direct and indirect)
func (g *DependencyGraph) GetTransitiveDependencies(serviceType reflect.Type, key any) []NodeKey {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodeKey := NodeKey{Type: serviceType, Key: key}
	visited := make(map[NodeKey]bool)
	result := make([]NodeKey, 0)

	var collect func(current NodeKey)
	collect = func(current NodeKey) {
		if visited[current] {
			return
		}
		visited[current] = true

		if edges, exists := g.edges[current]; exists {
			for _, dep := range edges {
				if !visited[dep] {
					result = append(result, dep)
					collect(dep)
				}
			}
		}
	}

	collect(nodeKey)
	return result
}

// GetNode returns the node for a given service
func (g *DependencyGraph) GetNode(serviceType reflect.Type, key any) *Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodeKey := NodeKey{Type: serviceType, Key: key}
	return g.nodes[nodeKey]
}

// HasNode checks if a node exists in the graph
func (g *DependencyGraph) HasNode(serviceType reflect.Type, key any) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodeKey := NodeKey{Type: serviceType, Key: key}
	_, exists := g.nodes[nodeKey]
	return exists
}

// Clear removes all nodes and edges from the graph
func (g *DependencyGraph) Clear() {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.nodes = make(map[NodeKey]*Node)
	g.edges = make(map[NodeKey][]NodeKey)
	g.sortedNodes = nil
	g.sortedNodesDirty = true
	g.cycleCache = make(map[NodeKey]bool)
	g.cycleCacheDirty = true
}

// Size returns the number of nodes in the graph
func (g *DependencyGraph) Size() int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return len(g.nodes)
}

// IsAcyclic returns true if the graph has no cycles
func (g *DependencyGraph) IsAcyclic() bool {
	return g.DetectCycles() == nil
}

// GetRoots returns all nodes with no dependencies (in-degree = 0)
func (g *DependencyGraph) GetRoots() []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	roots := make([]*Node, 0)
	for _, node := range g.nodes {
		if node.InDegree == 0 {
			roots = append(roots, node)
		}
	}

	return roots
}

// GetLeaves returns all nodes with no dependents (out-degree = 0)
func (g *DependencyGraph) GetLeaves() []*Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	leaves := make([]*Node, 0)
	for _, node := range g.nodes {
		if node.OutDegree == 0 {
			leaves = append(leaves, node)
		}
	}

	return leaves
}

// CalculateDepths assigns depth levels to nodes based on their dependencies
func (g *DependencyGraph) CalculateDepths() {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Reset all depths
	for _, node := range g.nodes {
		node.Depth = -1
	}

	// Start from roots (nodes with no dependencies - those that don't depend on anything)
	queue := make([]*Node, 0)
	for _, node := range g.nodes {
		if len(node.Dependencies) == 0 {
			node.Depth = 0
			queue = append(queue, node)
		}
	}

	// BFS to assign depths
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]

		// Update depth of nodes that depend on this one (dependents)
		for _, depKey := range current.Dependents {
			if dep, exists := g.nodes[depKey]; exists {
				newDepth := current.Depth + 1
				if dep.Depth < newDepth {
					dep.Depth = newDepth
					queue = append(queue, dep)
				}
			}
		}
	}
}

// String returns a string representation of the node key
func (k NodeKey) String() string {
	if k.Key != nil {
		return fmt.Sprintf("%v[%v]", k.Type, k.Key)
	}
	return fmt.Sprintf("%v", k.Type)
}

// String returns a string representation of the node
func (n *Node) String() string {
	return fmt.Sprintf("Node{%s, in:%d, out:%d, depth:%d}",
		n.Key.String(), n.InDegree, n.OutDegree, n.Depth)
}
