package graph

import (
	"fmt"
	"io"
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
	// Path is the chain of services forming the cycle, in dependency order,
	// each listed once: the last depends on the first.
	Path []string
}

func (e CircularDependencyError) cycle() []string {
	if len(e.Path) == 0 {
		return []string{e.Node}
	}
	return e.Path
}

func (e CircularDependencyError) Error() string {
	cycle := e.cycle()
	return "circular dependency detected: " + strings.Join(cycle, " -> ") + " -> " + cycle[0]
}

// Detail draws the cycle and explains how to break it.
func (e CircularDependencyError) Detail() string {
	cycle := e.cycle()
	var b strings.Builder
	for _, node := range cycle {
		fmt.Fprintf(&b, "    %s\n      ↓\n", node)
	}
	fmt.Fprintf(&b, "    %s (cycle)\n", cycle[0])

	b.WriteString("\nTo resolve this:\n")
	b.WriteString("  • Use an interface to break the dependency\n")
	b.WriteString("  • Use a factory function for lazy initialization\n")
	b.WriteString("  • Restructure to remove the circular relationship")
	return b.String()
}

// Format prints Error, or with %+v the message followed by Detail.
func (e CircularDependencyError) Format(s fmt.State, verb rune) {
	switch {
	case verb == 'v' && s.Flag('+'):
		_, _ = io.WriteString(s, e.Error()+"\n\n"+e.Detail())
	case verb == 'q':
		_, _ = fmt.Fprintf(s, "%q", e.Error())
	default:
		_, _ = io.WriteString(s, e.Error())
	}
}
