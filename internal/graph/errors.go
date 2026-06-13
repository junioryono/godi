package graph

import (
	"fmt"
	"strings"
)

// CircularDependencyError represents a circular dependency in the container.
//
// Node and Path describe the cycle using human-readable type names (the same
// form shown in Error). They are strings rather than internal node keys so
// the error stays fully usable from outside the module.
type CircularDependencyError struct {
	// Node is the service at which the cycle was detected.
	Node string
	// Path is the chain of services forming the cycle, in dependency order.
	Path []string
}

func (e CircularDependencyError) Error() string {
	var b strings.Builder
	b.WriteString("circular dependency detected:\n\n")

	if len(e.Path) == 0 {
		fmt.Fprintf(&b, "    %s\n", e.Node)
		b.WriteString("      ↓\n")
		fmt.Fprintf(&b, "    %s (cycle)\n", e.Node)
	} else {
		// Build a visual representation of the cycle
		for i, node := range e.Path {
			fmt.Fprintf(&b, "    %s\n", node)
			if i < len(e.Path)-1 {
				b.WriteString("      ↓\n")
			}
		}
		// Show the cycle back to the first node
		b.WriteString("      ↓\n")
		fmt.Fprintf(&b, "    %s (cycle)\n", e.Path[0])
	}

	b.WriteString("\nTo resolve this:\n")
	b.WriteString("  • Use an interface to break the dependency\n")
	b.WriteString("  • Use a factory function for lazy initialization\n")
	b.WriteString("  • Restructure to remove the circular relationship\n")

	return b.String()
}
