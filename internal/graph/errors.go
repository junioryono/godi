package graph

import (
	"fmt"
	"strings"
)

// CycleError represents a circular dependency in the graph
type CycleError struct {
	Node NodeKey
	Path []NodeKey
}

// Error implements the error interface
func (e *CycleError) Error() string {
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

// IsCycleError checks if an error is a cycle error
func IsCycleError(err error) bool {
	_, ok := err.(*CycleError)
	return ok
}

// GetCyclePath extracts the cycle path from an error if it's a CycleError
func GetCyclePath(err error) ([]NodeKey, bool) {
	if cycleErr, ok := err.(*CycleError); ok {
		return cycleErr.Path, true
	}
	return nil, false
}
