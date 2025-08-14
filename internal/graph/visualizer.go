package graph

import (
	"fmt"
	"io"
	"sort"
	"strings"
)

// Visualizer provides methods to visualize the dependency graph
type Visualizer struct {
	graph *DependencyGraph
}

// NewVisualizer creates a new graph visualizer
func NewVisualizer(graph *DependencyGraph) *Visualizer {
	return &Visualizer{graph: graph}
}

// WriteDOT writes the graph in Graphviz DOT format
func (v *Visualizer) WriteDOT(w io.Writer) error {
	v.graph.mu.RLock()
	defer v.graph.mu.RUnlock()

	fmt.Fprintln(w, "digraph dependencies {")
	fmt.Fprintln(w, "  rankdir=LR;")
	fmt.Fprintln(w, "  node [shape=box];")

	// Write nodes with labels
	nodeIDs := make(map[NodeKey]string)
	i := 0
	for key, node := range v.graph.nodes {
		nodeID := fmt.Sprintf("n%d", i)
		nodeIDs[key] = nodeID

		label := v.formatNodeLabel(node)
		color := v.getNodeColor(node)

		fmt.Fprintf(w, "  %s [label=\"%s\", fillcolor=\"%s\", style=filled];\n",
			nodeID, label, color)
		i++
	}

	// Write edges
	for from, tos := range v.graph.edges {
		fromID := nodeIDs[from]
		for _, to := range tos {
			toID := nodeIDs[to]
			fmt.Fprintf(w, "  %s -> %s;\n", fromID, toID)
		}
	}

	fmt.Fprintln(w, "}")
	return nil
}

// WriteText writes a text representation of the graph
func (v *Visualizer) WriteText(w io.Writer) error {
	v.graph.mu.RLock()
	defer v.graph.mu.RUnlock()

	fmt.Fprintln(w, "Dependency Graph:")
	fmt.Fprintln(w, "=================")
	fmt.Fprintln(w)

	// Get sorted nodes
	sorted, err := v.graph.TopologicalSort()
	if err != nil {
		fmt.Fprintf(w, "Warning: Graph contains cycles - %v\n\n", err)
		// Fall back to unsorted listing
		sorted = make([]*Node, 0, len(v.graph.nodes))
		for _, node := range v.graph.nodes {
			sorted = append(sorted, node)
		}
	}

	// Group nodes by depth
	v.graph.CalculateDepths()
	depthGroups := make(map[int][]*Node)
	maxDepth := 0

	for _, node := range sorted {
		depth := node.Depth
		if depth < 0 {
			depth = 999 // Nodes in cycles
		}
		depthGroups[depth] = append(depthGroups[depth], node)
		if depth > maxDepth && depth != 999 {
			maxDepth = depth
		}
	}

	// Print by depth level
	for depth := 0; depth <= maxDepth; depth++ {
		if nodes, exists := depthGroups[depth]; exists {
			fmt.Fprintf(w, "Level %d:\n", depth)
			fmt.Fprintln(w, "--------")

			for _, node := range nodes {
				v.writeNodeDetails(w, node, "  ")
			}
			fmt.Fprintln(w)
		}
	}

	// Print cycle nodes if any
	if cycleNodes, exists := depthGroups[999]; exists {
		fmt.Fprintln(w, "Nodes in Cycles:")
		fmt.Fprintln(w, "----------------")
		for _, node := range cycleNodes {
			v.writeNodeDetails(w, node, "  ")
		}
		fmt.Fprintln(w)
	}

	// Print statistics
	v.writeStatistics(w)

	return nil
}

// WriteAdjacencyList writes the graph as an adjacency list
func (v *Visualizer) WriteAdjacencyList(w io.Writer) error {
	v.graph.mu.RLock()
	defer v.graph.mu.RUnlock()

	fmt.Fprintln(w, "Adjacency List:")
	fmt.Fprintln(w, "===============")
	fmt.Fprintln(w)

	// Sort keys for consistent output
	keys := make([]NodeKey, 0, len(v.graph.edges))
	for key := range v.graph.edges {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i].String() < keys[j].String()
	})

	for _, from := range keys {
		tos := v.graph.edges[from]
		if len(tos) > 0 {
			toStrs := make([]string, len(tos))
			for i, to := range tos {
				toStrs[i] = to.String()
			}
			fmt.Fprintf(w, "%s -> [%s]\n", from.String(), strings.Join(toStrs, ", "))
		} else {
			fmt.Fprintf(w, "%s -> []\n", from.String())
		}
	}

	return nil
}

// formatNodeLabel creates a label for a node
func (v *Visualizer) formatNodeLabel(node *Node) string {
	typeStr := fmt.Sprintf("%v", node.Key.Type)

	// Simplify type string (remove package path for readability)
	parts := strings.Split(typeStr, ".")
	if len(parts) > 1 {
		typeStr = parts[len(parts)-1]
	}

	if node.Key.Key != nil {
		return fmt.Sprintf("%s\\n[%v]\\nIn:%d Out:%d",
			typeStr, node.Key.Key, node.InDegree, node.OutDegree)
	}

	return fmt.Sprintf("%s\\nIn:%d Out:%d",
		typeStr, node.InDegree, node.OutDegree)
}

// getNodeColor determines the color for a node based on its properties
func (v *Visualizer) getNodeColor(node *Node) string {
	if node.Provider == nil {
		return "lightgray" // Missing provider
	}

	switch node.Provider.Lifetime {
	case 0: // Singleton
		return "lightblue"
	case 1: // Scoped
		return "lightgreen"
	case 2: // Transient
		return "lightyellow"
	default:
		return "white"
	}
}

// writeNodeDetails writes detailed information about a node
func (v *Visualizer) writeNodeDetails(w io.Writer, node *Node, indent string) {
	fmt.Fprintf(w, "%s%s\n", indent, node.Key.String())

	if node.Provider != nil {
		fmt.Fprintf(w, "%s  Lifetime: %v\n", indent, node.Provider.Lifetime)

		if len(node.Provider.Groups) > 0 {
			fmt.Fprintf(w, "%s  Groups: %v\n", indent, node.Provider.Groups)
		}
	}

	if len(node.Dependencies) > 0 {
		deps := make([]string, len(node.Dependencies))
		for i, dep := range node.Dependencies {
			deps[i] = dep.String()
		}
		fmt.Fprintf(w, "%s  Dependencies: [%s]\n", indent, strings.Join(deps, ", "))
	}

	if len(node.Dependents) > 0 {
		deps := make([]string, len(node.Dependents))
		for i, dep := range node.Dependents {
			deps[i] = dep.String()
		}
		fmt.Fprintf(w, "%s  Dependents: [%s]\n", indent, strings.Join(deps, ", "))
	}
}

// writeStatistics writes graph statistics
func (v *Visualizer) writeStatistics(w io.Writer) {
	fmt.Fprintln(w, "Statistics:")
	fmt.Fprintln(w, "-----------")
	fmt.Fprintf(w, "  Total nodes: %d\n", len(v.graph.nodes))
	fmt.Fprintf(w, "  Total edges: %d\n", v.countEdges())

	roots := v.graph.GetRoots()
	leaves := v.graph.GetLeaves()

	fmt.Fprintf(w, "  Root nodes (no dependencies): %d\n", len(roots))
	fmt.Fprintf(w, "  Leaf nodes (no dependents): %d\n", len(leaves))

	if v.graph.IsAcyclic() {
		fmt.Fprintln(w, "  Cycles: None (graph is acyclic)")
	} else {
		fmt.Fprintln(w, "  Cycles: DETECTED (graph contains circular dependencies)")
	}

	// Find node with most dependencies
	maxDeps := 0
	var maxDepsNode *Node
	for _, node := range v.graph.nodes {
		if node.InDegree > maxDeps {
			maxDeps = node.InDegree
			maxDepsNode = node
		}
	}

	if maxDepsNode != nil {
		fmt.Fprintf(w, "  Most dependencies: %s (%d)\n",
			maxDepsNode.Key.String(), maxDeps)
	}

	// Find node with most dependents
	maxDependents := 0
	var maxDependentsNode *Node
	for _, node := range v.graph.nodes {
		if node.OutDegree > maxDependents {
			maxDependents = node.OutDegree
			maxDependentsNode = node
		}
	}

	if maxDependentsNode != nil {
		fmt.Fprintf(w, "  Most dependents: %s (%d)\n",
			maxDependentsNode.Key.String(), maxDependents)
	}
}

// countEdges counts the total number of edges in the graph
func (v *Visualizer) countEdges() int {
	count := 0
	for _, edges := range v.graph.edges {
		count += len(edges)
	}
	return count
}
