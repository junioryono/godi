package graph

import (
	"fmt"
	"reflect"

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

// DependencyGraph holds the dependency edges between services and detects
// cycles among them. Build fills it on one goroutine and discards it, so it is
// not safe for concurrent use.
type DependencyGraph struct {
	// providers maps the key of every registered service to its provider.
	// Keys that only appear as dependencies have no entry.
	providers map[NodeKey]Provider
	// order lists node keys (services and dependencies) in insertion order,
	// so traversals, and so cycle reports, do not depend on map iteration.
	order []NodeKey
	known map[NodeKey]struct{}
	edges map[NodeKey][]NodeKey // adjacency list: a node's dependencies
}

// NodeKey uniquely identifies a node in the graph
type NodeKey struct {
	Type  reflect.Type
	Key   any    // for keyed services
	Group string // for grouped services
}

// NewDependencyGraphWithCapacity creates a dependency graph sized for
// capacity services.
func NewDependencyGraphWithCapacity(capacity int) *DependencyGraph {
	return &DependencyGraph{
		providers: make(map[NodeKey]Provider, capacity),
		order:     make([]NodeKey, 0, capacity),
		known:     make(map[NodeKey]struct{}, capacity),
		edges:     make(map[NodeKey][]NodeKey, capacity),
	}
}

// AddProviderDeferred adds a provider and its dependency edges, replacing
// those of a provider previously added under the same key. Cycles are not
// checked here: call ResolveGroupDependencies and then DetectCycles once
// every provider is added.
func (g *DependencyGraph) AddProviderDeferred(provider Provider) error {
	if provider == nil {
		return fmt.Errorf("provider cannot be nil")
	}

	nodeKey := NodeKey{
		Type:  provider.GetType(),
		Key:   provider.GetKey(),
		Group: provider.GetGroup(),
	}
	g.addNode(nodeKey)
	g.providers[nodeKey] = provider

	providerDeps := provider.GetDependencies()
	if len(providerDeps) == 0 {
		delete(g.edges, nodeKey)
		return nil
	}
	dependencies := make([]NodeKey, 0, len(providerDeps))
	for _, dep := range providerDeps {
		depKey := NodeKey{Type: dep.Type, Key: dep.Key, Group: dep.Group}
		dependencies = append(dependencies, depKey)
		g.addNode(depKey)
	}
	g.edges[nodeKey] = dependencies
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
	groupMembers := make(map[groupIndex][]NodeKey)
	phantoms := make(map[NodeKey]struct{})
	for _, key := range g.order {
		if key.Group == "" {
			continue
		}
		_, registered := g.providers[key]
		switch {
		case key.Key != nil && registered:
			idx := groupIndex{Type: key.Type, Group: key.Group}
			groupMembers[idx] = append(groupMembers[idx], key)
		case key.Key == nil && !registered:
			phantoms[key] = struct{}{}
		}
	}
	if len(phantoms) == 0 {
		return
	}

	// Rewire consumers, expanding every phantom edge into the group's
	// member edges.
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

	// Remove the phantom nodes.
	live := g.order[:0]
	for _, key := range g.order {
		if _, phantom := phantoms[key]; phantom {
			delete(g.known, key)
			delete(g.edges, key)
			continue
		}
		live = append(live, key)
	}
	g.order = live
}

// addNode records key as a node, in insertion order, unless it exists.
func (g *DependencyGraph) addNode(key NodeKey) {
	if _, exists := g.known[key]; exists {
		return
	}
	g.known[key] = struct{}{}
	g.order = append(g.order, key)
}

// DetectCycles reports the first dependency cycle, in insertion order, as a
// *CircularDependencyError, or returns nil.
func (g *DependencyGraph) DetectCycles() error {
	// Share the visited set across starting points so each node is explored
	// at most once.
	visited := make(map[NodeKey]bool, len(g.order))
	for _, key := range g.order {
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

		if visiting[item.key] {
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

		for _, dep := range g.edges[item.key] {
			if !visited[dep] {
				stack = append(stack, stackItem{key: dep, visiting: true})
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
