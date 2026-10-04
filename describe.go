package godi

import (
	"bufio"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// ---------------------------------------------------------------------------
// Error detail
// ---------------------------------------------------------------------------

// Explain returns err's message followed by the detail godi attaches to its
// errors: remediation hints, "did you mean" suggestions, dependency cycles
// drawn out, and constructor panic stack traces. It walks wrapped and joined
// errors. Error() strings stay one line so they are safe for logs and
// responses; use Explain (or the %+v verb) when diagnosing.
func Explain(err error) string {
	if err == nil {
		return ""
	}
	var details []string
	seen := make(map[string]struct{})
	var walk func(error)
	walk = func(e error) {
		if e == nil {
			return
		}
		if d, ok := e.(interface{ Detail() string }); ok {
			if detail := d.Detail(); detail != "" {
				if _, dup := seen[detail]; !dup {
					seen[detail] = struct{}{}
					details = append(details, detail)
				}
			}
		}
		switch u := e.(type) {
		case interface{ Unwrap() []error }:
			for _, inner := range u.Unwrap() {
				walk(inner)
			}
		case interface{ Unwrap() error }:
			walk(u.Unwrap())
		}
	}
	walk(err)

	if len(details) == 0 {
		return err.Error()
	}
	return err.Error() + "\n\n" + strings.Join(details, "\n\n")
}

// formatError implements fmt.Formatter for godi errors: %+v prints Explain,
// every other verb the one-line message.
func formatError(s fmt.State, verb rune, err error) {
	switch {
	case verb == 'v' && s.Flag('+'):
		_, _ = io.WriteString(s, Explain(err))
	case verb == 'q':
		_, _ = fmt.Fprintf(s, "%q", err.Error())
	default:
		_, _ = io.WriteString(s, err.Error())
	}
}

// Build phases reported in BuildError.Phase.
const (
	PhaseInitialization      = "initialization"
	PhaseRegistration        = "registration"
	PhaseGraph               = "graph"
	PhaseValidation          = "validation"
	PhaseScopeCreation       = "scope-creation"
	PhaseSingletonCreation   = "singleton-creation"
	PhaseScopeInitialization = "scope-initialization"
	PhaseCleanup             = "cleanup"
)

// formatType formats a reflect.Type for messages, package-qualified
// ("*db.Config"): a bare type name is ambiguous across packages.
func formatType(t reflect.Type) string {
	if t == nil {
		return "<nil>"
	}
	return t.String()
}

// majorVersionSuffix matches a module major-version path element ("v5").
var majorVersionSuffix = regexp.MustCompile(`^v\d+\.`)

// funcLocation names a function value and its source location, e.g.
// "users.NewService (service.go:42)".
func funcLocation(fn reflect.Value) string {
	if !fn.IsValid() || fn.Kind() != reflect.Func || fn.IsNil() {
		return ""
	}
	f := runtime.FuncForPC(fn.Pointer())
	if f == nil {
		return fn.Type().String()
	}
	name := f.Name()
	// Trim the import path: "github.com/acme/users.NewService" ->
	// "users.NewService"; a module major version names the package by the
	// element before it: "github.com/acme/godi/v5.New" -> "godi.New".
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		prefix, rest := name[:slash], name[slash+1:]
		if majorVersionSuffix.MatchString(rest) {
			pkg := prefix[strings.LastIndex(prefix, "/")+1:]
			if dot := strings.IndexByte(rest, '.'); dot >= 0 {
				rest = pkg + rest[dot:]
			}
		}
		name = rest
	}
	file, line := f.FileLine(fn.Pointer())
	if file == "" {
		return name
	}
	return fmt.Sprintf("%s (%s:%d)", name, filepath.Base(file), line)
}

// ---------------------------------------------------------------------------
// Observer
// ---------------------------------------------------------------------------

// Observer receives construction and disposal events from a provider and its
// scopes (ProviderOptions.Observer). Methods are called synchronously on the
// goroutine doing the work, so they must be safe for concurrent use and
// should return quickly.
type Observer interface {
	// Constructed reports a constructor call: its service, scope, duration
	// and error (nil on success).
	Constructed(*ConstructedEvent)
	// Disposed reports a resource's cleanup, including cleanup of values
	// produced after their owner closed, which has no caller to report to.
	Disposed(*DisposedEvent)
}

// ConstructedEvent describes one constructor call.
type ConstructedEvent struct {
	ServiceType reflect.Type
	Key         any
	Lifetime    Lifetime
	// ScopeID is the ID of the scope that ran the constructor (the root
	// scope for singletons).
	ScopeID string
	// Constructor names the constructor and its source location.
	Constructor string
	Duration    time.Duration
	Err         error
}

// DisposedEvent describes one resource's cleanup.
type DisposedEvent struct {
	// Type is the disposed value's dynamic type.
	Type reflect.Type
	// ScopeID is the ID of the owning scope, or "" for the provider.
	ScopeID  string
	Duration time.Duration
	Err      error
}

// ---------------------------------------------------------------------------
// Introspection
// ---------------------------------------------------------------------------

// DependencyInfo describes one dependency of a registered service.
type DependencyInfo struct {
	Type     reflect.Type
	Key      any
	Group    string
	Optional bool
}

// Describe returns the registrations of the provider behind p (a Provider or
// Scope), in registration order, with their constructors and dependencies
// (including decorators'). It constructs nothing. Render it with WriteDOT.
func Describe(p Provider) []ServiceInfo {
	root := rootProviderOf(p)
	if root == nil {
		return nil
	}
	infos := make([]ServiceInfo, 0, len(root.descriptors))
	for _, d := range root.descriptors {
		infos = append(infos, describeDescriptor(d))
	}
	return infos
}

func describeDescriptor(d *descriptor) ServiceInfo {
	info := ServiceInfo{
		ServiceType: d.Type,
		Key:         serviceInfoKey(d),
		Group:       d.Group,
		Lifetime:    d.Lifetime,
		Constructor: d.source,
	}
	if len(d.Dependencies) > 0 {
		info.Dependencies = make([]DependencyInfo, 0, len(d.Dependencies))
		for _, dep := range d.Dependencies {
			if dep == nil {
				continue
			}
			info.Dependencies = append(info.Dependencies, DependencyInfo{
				Type:     dep.Type,
				Key:      dep.Key,
				Group:    dep.Group,
				Optional: dep.Optional,
			})
		}
	}
	return info
}

// WriteDOT writes services (from Describe or Collection.ToSlice) as a
// Graphviz digraph with an edge from each service to each dependency:
//
//	godi.WriteDOT(os.Stdout, godi.Describe(provider)) // | dot -Tsvg
func WriteDOT(w io.Writer, services []ServiceInfo) error {
	bw := bufio.NewWriter(w)
	_, _ = bw.WriteString("digraph godi {\n\trankdir=LR;\n")
	for _, s := range services {
		node := strconv.Quote(dotNodeName(s.ServiceType, s.Key, s.Group))
		fmt.Fprintf(bw, "\t%s [label=%s];\n", node, strconv.Quote(dotLabel(&s)))
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

func dotNodeName(t reflect.Type, key any, group string) string {
	name := formatType(t)
	if key != nil {
		name += fmt.Sprintf(" (key: %v)", key)
	}
	if group != "" {
		name += fmt.Sprintf(" [group: %s]", group)
	}
	return name
}

func dotLabel(s *ServiceInfo) string {
	return dotNodeName(s.ServiceType, s.Key, s.Group) + "\n" + s.Lifetime.String()
}
