package graph

import (
	"fmt"
	"strings"
)

// CircularDependencyError represents a circular dependency in the container.
type CircularDependencyError struct {
	Node NodeKey
	Path []NodeKey
}

func (e CircularDependencyError) Error() string {
	var b strings.Builder
	b.WriteString("circular dependency detected:\n\n")

	if len(e.Path) == 0 {
		b.WriteString(fmt.Sprintf("    %s\n", e.Node.String()))
		b.WriteString("      ↓\n")
		b.WriteString(fmt.Sprintf("    %s (cycle)\n", e.Node.String()))
	} else {
		// Build a visual representation of the cycle
		for i, node := range e.Path {
			b.WriteString(fmt.Sprintf("    %s\n", node.String()))
			if i < len(e.Path)-1 {
				b.WriteString("      ↓\n")
			}
		}
		// Show the cycle back to the first node
		if len(e.Path) > 0 {
			b.WriteString("      ↓\n")
			b.WriteString(fmt.Sprintf("    %s (cycle)\n", e.Path[0].String()))
		}
	}

	b.WriteString("\nTo resolve this:\n")
	b.WriteString("  • Use an interface to break the dependency\n")
	b.WriteString("  • Use a factory function for lazy initialization\n")
	b.WriteString("  • Restructure to remove the circular relationship\n")

	return b.String()
}
