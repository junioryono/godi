package godi

import (
	"bufio"
	"fmt"
	"io"
	"reflect"
	"strconv"
)

// DependencyInfo describes one dependency of a registered service.
type DependencyInfo struct {
	Type     reflect.Type
	Key      string
	Group    string
	Optional bool
}

// ServiceDescription describes a registration in a built provider: its
// identity (ServiceInfo), constructor, and dependencies.
type ServiceDescription struct {
	ServiceInfo
	// Constructor names the constructor function and its source location
	// ("users.NewService (service.go:42)"), or the value's type for an
	// instance registration.
	Constructor string
	// Dependencies are the services the constructor and its decorators
	// receive, in parameter order.
	Dependencies []DependencyInfo
}

// Describe returns the registrations of the provider behind p (a Provider or
// Scope), in registration order, with their constructors and dependencies
// (including decorators'). It constructs nothing. Render it with WriteDOT.
func Describe(p Provider) []ServiceDescription {
	root := rootProviderOf(p)
	if root == nil {
		return nil
	}
	descriptions := make([]ServiceDescription, 0, len(root.descriptors))
	for _, d := range root.descriptors {
		descriptions = append(descriptions, describeDescriptor(d))
	}
	return descriptions
}

func describeDescriptor(d *descriptor) ServiceDescription {
	description := ServiceDescription{
		ServiceInfo: ServiceInfo{
			ServiceType: d.Type,
			Key:         serviceInfoKey(d),
			Group:       d.Group,
			Lifetime:    d.Lifetime,
		},
		Constructor: d.source,
	}
	for _, dep := range d.dependencies() {
		if dep == nil {
			continue
		}
		description.Dependencies = append(description.Dependencies, DependencyInfo{
			Type:     dep.Type,
			Key:      keyName(dep.Key),
			Group:    dep.Group,
			Optional: dep.Optional,
		})
	}
	return description
}

// WriteDOT writes services (from Describe) as a Graphviz digraph with an edge
// from each service to each dependency:
//
//	godi.WriteDOT(os.Stdout, godi.Describe(provider)) // | dot -Tsvg
func WriteDOT(w io.Writer, services []ServiceDescription) error {
	bw := bufio.NewWriter(w)
	_, _ = bw.WriteString("digraph godi {\n\trankdir=LR;\n")
	for i := range services {
		s := &services[i]
		name := dotNodeName(s.ServiceType, s.Key, s.Group)
		node := strconv.Quote(name)
		fmt.Fprintf(bw, "\t%s [label=%s];\n", node, strconv.Quote(name+"\n"+s.Lifetime.String()))
		for _, dep := range s.Dependencies {
			target := strconv.Quote(dotNodeName(dep.Type, dep.Key, dep.Group))
			if dep.Optional {
				fmt.Fprintf(bw, "\t%s -> %s [style=dashed];\n", node, target)
			} else {
				fmt.Fprintf(bw, "\t%s -> %s;\n", node, target)
			}
		}
	}
	_, _ = bw.WriteString("}\n")
	return bw.Flush()
}

func dotNodeName(t reflect.Type, key, group string) string {
	name := formatType(t)
	if key != "" {
		name += fmt.Sprintf(" (key: %s)", key)
	}
	if group != "" {
		name += fmt.Sprintf(" [group: %s]", group)
	}
	return name
}
