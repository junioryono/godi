package graph

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/junioryono/godi/v6/internal/reflection"
)

// Provider defines the interface for service providers that can be added to the graph.
// This abstraction allows the graph to work with different provider implementations.
type Provider interface {
	// GetType returns the service type this provider produces
	GetType() reflect.Type

	// GetKey returns the optional key for named/keyed services
	GetKey() any

	// GetGroup returns the optional group for grouped services
	GetGroup() string

	// GetDependencies returns the analyzed dependencies
	GetDependencies() []*reflection.Dependency
}

// DependencyGraph manages the dependency relationships between services.
// It provides cycle detection, topological sorting, and dependency analysis.
type DependencyGraph struct {
	mu    sync.RWMutex
	nodes map[NodeKey]*Node
	// order lists node keys in insertion order, so traversals (and so cycle
	// reports and sort order) do not depend on map iteration. It may still
	// hold keys of removed nodes, which traversals skip.
	order []NodeKey
	edges map[NodeKey][]NodeKey // adjacency list representation

	// Cache for performance
	sortedNodes      []*Node
	sortedNodesDirty bool
}

// NodeKey uniquely identifies a node in the graph
type NodeKey struct {
	Type  reflect.Type
	Key   any    // for keyed services
	Group string // for grouped services
}

// Node represents a service in the dependency graph
type Node struct {
	Key      NodeKey
	Provider Provider

	// Graph metadata
	InDegree  int // number of dependencies
	OutDegree int // number of dependents

	// Dependency information
	Dependencies []NodeKey // services this node depends on
	Dependents   []NodeKey // services that depend on this node
}

// NewDependencyGraph creates a new dependency graph
func NewDependencyGraph() *DependencyGraph {
	return &DependencyGraph{
		nodes:            make(map[NodeKey]*Node),
		edges:            make(map[NodeKey][]NodeKey),
		sortedNodesDirty: true,
	}
}

// NewDependencyGraphWithCapacity creates a new dependency graph with pre-sized maps
func NewDependencyGraphWithCapacity(capacity int) *DependencyGraph {
	return &DependencyGraph{
		nodes:            make(map[NodeKey]*Node, capacity),
		edges:            make(map[NodeKey][]NodeKey, capacity),
		sortedNodesDirty: true,
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
		Type:  provider.GetType(),
		Key:   provider.GetKey(),
		Group: provider.GetGroup(),
	}

	// Create or update node
	node, exists := g.nodes[nodeKey]
	if !exists {
		node = g.addNode(nodeKey)
	}
	node.Provider = provider

	// Clear existing edges for this node (in case of replacement)
	delete(g.edges, nodeKey)

	// Add edges based on dependencies
	providerDeps := provider.GetDependencies()
	dependencies := make([]NodeKey, 0, len(providerDeps))
	for _, dep := range providerDeps {
		depKey := NodeKey{
			Type:  dep.Type,
			Key:   dep.Key,
			Group: dep.Group,
		}
		dependencies = append(dependencies, depKey)

		// Ensure dependency node exists
		if _, exists := g.nodes[depKey]; !exists {
			g.addNode(depKey)
		}
	}

	node.Dependencies = dependencies
	g.edges[nodeKey] = dependencies

	// Update in/out degrees
	g.updateDegrees()

	// Mark caches as dirty
	g.sortedNodesDirty = true

	// Check for cycles immediately
	if err := g.detectCyclesFrom(nodeKey, make(map[NodeKey]bool, len(g.nodes))); err != nil {
		// Remove the node if it creates a cycle
		delete(g.nodes, nodeKey)
		delete(g.edges, nodeKey)
		g.updateDegrees()
		return err
	}

	return nil
}

// AddProviderDeferred adds a provider to the graph without immediate cycle detection.
// This is faster for bulk additions - call DetectCycles() after all providers are added.
func (g *DependencyGraph) AddProviderDeferred(provider Provider) error {
	if provider == nil {
		return fmt.Errorf("provider cannot be nil")
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	// Create node key
	nodeKey := NodeKey{
		Type:  provider.GetType(),
		Key:   provider.GetKey(),
		Group: provider.GetGroup(),
	}

	// Create or update node
	node, exists := g.nodes[nodeKey]
	if !exists {
		node = g.addNode(nodeKey)
	}
	node.Provider = provider

	// Add edges based on dependencies. Edges are fully replaced so that
	// re-registering a provider never merges in stale edges from a
	// previous registration.
	providerDeps := provider.GetDependencies()
	if len(providerDeps) > 0 {
		dependencies := make([]NodeKey, 0, len(providerDeps))
		for _, dep := range providerDeps {
			depKey := NodeKey{
				Type:  dep.Type,
				Key:   dep.Key,
				Group: dep.Group,
			}
			dependencies = append(dependencies, depKey)

			// Ensure dependency node exists (minimal allocation)
			if _, exists := g.nodes[depKey]; !exists {
				g.addNode(depKey)
			}
		}
		node.Dependencies = dependencies
		g.edges[nodeKey] = dependencies
	} else {
		node.Dependencies = nil
		delete(g.edges, nodeKey)
	}

	// Mark caches as dirty (defer degree updates to DetectCycles)
	g.sortedNodesDirty = true

	return nil
}

// groupIndex identifies a group by type and name, without a specific key.
type groupIndex struct {
	Type  reflect.Type
	Group string
}

// ResolveGroupDependencies resolves phantom group dependency nodes by connecting
// consumers directly to actual group member nodes. This must be called after all
// providers are added via AddProviderDeferred and before DetectCycles.
//
// When a service depends on a group (e.g., group:"routes"), the dependency is
// recorded as NodeKey{Type: T, Key: nil, Group: "routes"}. However, actual group
// members are registered with numeric keys like NodeKey{Type: T, Key: 1, Group: "routes"}.
// This method replaces phantom group dependencies with edges to real group members.
func (g *DependencyGraph) ResolveGroupDependencies() {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Step 1: Build an index of real group members
	groupMembers := make(map[groupIndex][]NodeKey)
	for _, key := range g.liveKeys() {
		node := g.nodes[key]
		if key.Group != "" && key.Key != nil && node.Provider != nil {
			idx := groupIndex{Type: key.Type, Group: key.Group}
			groupMembers[idx] = append(groupMembers[idx], key)
		}
	}

	// Step 2: Find phantom group nodes (Group != "", Key == nil, no Provider)
	phantoms := make(map[NodeKey]struct{})
	for _, key := range g.liveKeys() {
		if isPhantomGroupNode(key, g.nodes[key]) {
			phantoms[key] = struct{}{}
		}
	}
	if len(phantoms) == 0 {
		return
	}

	// Step 3: Rewire consumers in a single pass over the edges, expanding
	// every phantom edge into the group's member edges.
	for consumerKey, edges := range g.edges {
		var newEdges []NodeKey
		for i, edge := range edges {
			if _, ok := phantoms[edge]; !ok {
				if newEdges != nil {
					newEdges = append(newEdges, edge)
				}
				continue
			}
			if newEdges == nil {
				newEdges = make([]NodeKey, i, len(edges))
				copy(newEdges, edges[:i])
			}
			newEdges = append(newEdges, groupMembers[groupIndex{Type: edge.Type, Group: edge.Group}]...)
		}
		if newEdges != nil {
			g.edges[consumerKey] = newEdges
		}
	}

	// Step 4: Remove the phantom nodes
	for phantomKey := range phantoms {
		delete(g.nodes, phantomKey)
		delete(g.edges, phantomKey)
	}

	// Mark caches as dirty since edges changed
	g.sortedNodesDirty = true
}

// addNode creates the node for key and records its insertion order. The
// caller must hold the write lock and know that the node does not exist.
func (g *DependencyGraph) addNode(key NodeKey) *Node {
	node := &Node{
		Key:          key,
		Dependencies: make([]NodeKey, 0, 4),
		Dependents:   make([]NodeKey, 0, 4),
	}
	g.nodes[key] = node
	g.order = append(g.order, key)
	return node
}

// liveKeys returns the keys of the existing nodes in insertion order,
// compacting the order list past removed nodes.
func (g *DependencyGraph) liveKeys() []NodeKey {
	live := g.order[:0]
	for _, key := range g.order {
		if _, exists := g.nodes[key]; exists {
			live = append(live, key)
		}
	}
	// A node removed and re-added appears twice; keep its first position.
	if len(live) != len(g.nodes) {
		seen := make(map[NodeKey]struct{}, len(g.nodes))
		unique := live[:0]
		for _, key := range live {
			if _, dup := seen[key]; !dup {
				seen[key] = struct{}{}
				unique = append(unique, key)
			}
		}
		live = unique
	}
	g.order = live
	return live
}

func isPhantomGroupNode(key NodeKey, node *Node) bool {
	return key.Group != "" && key.Key == nil && node.Provider == nil
}

// updateDegrees recalculates in/out degrees for all nodes
func (g *DependencyGraph) updateDegrees() {
	// Reset all degrees and dependent lists
	for _, node := range g.nodes {
		node.InDegree = 0
		node.OutDegree = 0
		node.Dependents = make([]NodeKey, 0, 4) // Pre-allocate with reasonable capacity
	}

	// Calculate degrees from edges in a single pass, in insertion order so
	// that dependents (and so the sort order) are deterministic.
	for _, from := range g.liveKeys() {
		tos, hasEdges := g.edges[from]
		if fromNode := g.nodes[from]; hasEdges {
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

	// Create working copies of dependency counts
	// We need to count how many dependencies each node has
	keys := g.liveKeys()
	depCounts := make(map[NodeKey]int, len(keys))
	for _, key := range keys {
		depCounts[key] = len(g.nodes[key].Dependencies)
	}

	// Find all nodes with no dependencies, in insertion order
	queue := make([]NodeKey, 0)
	for _, key := range keys {
		if depCounts[key] == 0 {
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

			// For each node that depends on the current node,
			// reduce its dependency count
			for _, dependent := range node.Dependents {
				depCounts[dependent]--
				if depCounts[dependent] == 0 {
					queue = append(queue, dependent)
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

	// Update degrees first (may have been deferred from AddProviderDeferred)
	g.updateDegrees()

	// Check each node for cycles using DFS, sharing the visited set across
	// starting points so each node is explored at most once.
	visited := make(map[NodeKey]bool, len(g.nodes))
	for _, key := range g.liveKeys() {
		if !visited[key] {
			if err := g.detectCyclesFrom(key, visited); err != nil {
				return err
			}
		}
	}

	return nil
}

// detectCyclesFrom performs DFS cycle detection from a specific node.
// The visited map is shared across calls so each node is explored once.
func (g *DependencyGraph) detectCyclesFrom(start NodeKey, visited map[NodeKey]bool) error {
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

	for len(stack) > 0 {
		item := stack[len(stack)-1]

		if !item.visiting {
			// Backtracking
			stack = stack[:len(stack)-1]
			delete(visiting, item.key)
			visited[item.key] = true
			continue
		}

		// Mark as visiting
		if visiting[item.key] {
			// Found a cycle
			path := g.findCyclePath(item.key)
			pathStrs := make([]string, len(path))
			for i, k := range path {
				pathStrs[i] = k.String()
			}
			return &CircularDependencyError{
				Node: item.key.String(),
				Path: pathStrs,
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

// findCyclePath returns the nodes of a cycle through start, in dependency
// order, each once: start depends on path[1], ..., and the last node depends
// on start. Edges are followed in insertion order, so the result is
// deterministic.
func (g *DependencyGraph) findCyclePath(start NodeKey) []NodeKey {
	var path []NodeKey
	visited := make(map[NodeKey]bool)
	var dfs func(node NodeKey) bool
	dfs = func(node NodeKey) bool {
		path = append(path, node)
		visited[node] = true
		for _, next := range g.edges[node] {
			if next == start {
				return true
			}
			if !visited[next] && dfs(next) {
				return true
			}
		}
		path = path[:len(path)-1]
		return false
	}
	if dfs(start) {
		return path
	}
	return []NodeKey{start}
}

// GetDependencies returns the direct dependencies of a service
func (g *DependencyGraph) GetDependencies(serviceType reflect.Type, key any, group string) []NodeKey {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodeKey := NodeKey{Type: serviceType, Key: key, Group: group}
	if node, exists := g.nodes[nodeKey]; exists {
		result := make([]NodeKey, len(node.Dependencies))
		copy(result, node.Dependencies)
		return result
	}

	return nil
}

// GetNode returns the node for a given service
func (g *DependencyGraph) GetNode(serviceType reflect.Type, key any, group string) *Node {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodeKey := NodeKey{Type: serviceType, Key: key, Group: group}
	return g.nodes[nodeKey]
}

// HasNode checks if a node exists in the graph
func (g *DependencyGraph) HasNode(serviceType reflect.Type, key any, group string) bool {
	g.mu.RLock()
	defer g.mu.RUnlock()

	nodeKey := NodeKey{Type: serviceType, Key: key, Group: group}
	_, exists := g.nodes[nodeKey]
	return exists
}

// Size returns the number of nodes in the graph
func (g *DependencyGraph) Size() int {
	g.mu.RLock()
	defer g.mu.RUnlock()

	return len(g.nodes)
}

// String returns a string representation of the node key
func (k NodeKey) String() string {
	var str = k.Type.String()
	if k.Key != nil {
		str += fmt.Sprintf(":%v", k.Key)
	}
	if k.Group != "" {
		str += fmt.Sprintf(" [%s]", k.Group)
	}
	return str
}

// String returns a string representation of the node
func (n *Node) String() string {
	return fmt.Sprintf("Node{%s, in:%d, out:%d}",
		n.Key.String(), n.InDegree, n.OutDegree)
}
