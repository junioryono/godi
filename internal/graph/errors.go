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
	if len(e.Path) == 0 {
		return fmt.Sprintf("circular dependency detected involving %s", e.Node.String())
	}

	// Build a visual representation of the cycle
	pathStrs := make([]string, len(e.Path))
	for i, node := range e.Path {
		pathStrs[i] = node.String()
	}

	return fmt.Sprintf("circular dependency detected: %s", strings.Join(pathStrs, " -> "))
}
