package godi

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"
)

// An AddOption modifies the default behavior of AddSingleton, AddScoped, and AddTransient.
type AddOption interface {
	applyAddOption(*addOptions)
}

type addOptions struct {
	Name      string
	Group     string
	As        []any
	NoDispose bool
	Lazy      bool
}

// key returns the registration key: the godi.Name, or nil.
func (o *addOptions) key() any {
	return keyOf(o.Name)
}

func (o *addOptions) Validate() error {
	if o.Group != "" {
		if o.Name != "" {
			return &ValidationError{
				ServiceType: nil,
				Cause:       fmt.Errorf("cannot use both godi.Name and godi.Group: name:%q provided with group:%q", o.Name, o.Group),
			}
		}
	}

	// Names must be representable inside a backquoted string. The only
	// limitation for raw string literals as per
	// https://golang.org/ref/spec#raw_string_lit is that they cannot contain
	// backquotes.
	if strings.ContainsRune(o.Name, '`') {
		return &ValidationError{
			ServiceType: nil,
			Cause:       fmt.Errorf("invalid godi.Name(%q): names cannot contain backquotes", o.Name),
		}
	}
	if strings.ContainsRune(o.Group, '`') {
		return &ValidationError{
			ServiceType: nil,
			Cause:       fmt.Errorf("invalid godi.Group(%q): group names cannot contain backquotes", o.Group),
		}
	}

	for _, i := range o.As {
		t := reflect.TypeOf(i)

		if t == nil {
			return &ValidationError{
				ServiceType: nil,
				Cause:       fmt.Errorf("invalid godi.As(nil): argument must be a pointer to an interface"),
			}
		}

		if t.Kind() != reflect.Pointer {
			return &ValidationError{
				ServiceType: nil,
				Cause:       fmt.Errorf("invalid godi.As(%v): argument must be a pointer to an interface", t),
			}
		}

		pointingTo := t.Elem()
		if pointingTo.Kind() != reflect.Interface {
			return &ValidationError{
				ServiceType: nil,
				Cause:       fmt.Errorf("invalid godi.As(*%v): argument must be a pointer to an interface", pointingTo),
			}
		}
	}
	return nil
}

// Name is an AddOption that registers the value produced by a constructor as
// a keyed service under the given name. Resolve it with ResolveKeyed or an In
// field tagged `name:"..."`. For a constructor with several non-error
// returns, every return is registered under the name (as Group adds every
// return to the group); give outputs different names with a result object.
//
// Given,
//
//	func NewReadOnlyConnection(...) (*Connection, error)
//	func NewReadWriteConnection(...) (*Connection, error)
//
// The following will provide two connections to the container: one under the
// name "ro" and the other under the name "rw".
//
//	c.AddSingleton(NewReadOnlyConnection, godi.Name("ro"))
//	c.AddSingleton(NewReadWriteConnection, godi.Name("rw"))
//
// This option cannot be provided for constructors which produce result
// objects.
func Name(name string) AddOption {
	return addNameOption(name)
}

type addNameOption string

func (o addNameOption) String() string {
	return fmt.Sprintf("Name(%q)", string(o))
}

func (o addNameOption) applyAddOption(opt *addOptions) {
	opt.Name = string(o)
}

// Group is an AddOption that adds the values produced by a constructor to the
// specified group. For a constructor with several non-error returns, every
// return is added to the group of its own type. Consume a group with
// ResolveGroup or an In field of slice type tagged `group:"..."`.
//
// This option cannot be combined with Name, and cannot be provided for
// constructors which produce result objects.
func Group(group string) AddOption {
	return addGroupOption(group)
}

type addGroupOption string

func (o addGroupOption) String() string {
	return fmt.Sprintf("Group(%q)", string(o))
}

func (o addGroupOption) applyAddOption(opt *addOptions) {
	opt.Group = string(o)
}

// As is an AddOption that specifies that the value produced by the
// constructor implements the interface T and is provided to the container
// as that interface.
//
// The value will then be available in the container as an implementation of
// T, but not as its concrete type. Pass As multiple times to register the
// value under several interfaces.
//
// For example, the following will make io.Reader and io.Writer available
// in the container, but not the concrete buffer type.
//
//	c.AddSingleton(newBuffer, godi.As[io.Reader](), godi.As[io.Writer]())
//
// That is, the above is equivalent to the following.
//
//	c.AddSingleton(func(...) (io.Reader, io.Writer) {
//	  b := newBuffer(...)
//	  return b, b
//	})
//
// If used with godi.Name, the types specified with godi.As will all use the
// same name. For example,
//
//	c.AddSingleton(newFile, godi.As[io.Reader](), godi.Name("temp"))
//
// The above is equivalent to the following.
//
//	type Result struct {
//	  godi.Out
//
//	  Reader io.Reader `name:"temp"`
//	}
//
//	c.AddSingleton(func(...) Result {
//	  f := newFile(...)
//	  return Result{
//	    Reader: f,
//	  }
//	})
//
// This option cannot be provided for constructors which produce result
// objects or have multiple non-error return values, and reserved types
// (context.Context, godi.Provider, godi.Scope, godi.Resolver,
// godi.ScopeFactory) cannot be registered this way.
func As[T any]() AddOption {
	return addAsOption{new(T)}
}

type addAsOption []any

func (o addAsOption) String() string {
	buf := bytes.NewBufferString("As(")
	for i, iface := range o {
		if i > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString(reflect.TypeOf(iface).Elem().String())
	}
	buf.WriteString(")")
	return buf.String()
}

func (o addAsOption) applyAddOption(opts *addOptions) {
	opts.As = append(opts.As, o...)
}

// Lazy is an AddOption for singletons: the service is created on its first
// resolution instead of at Build. Use it for expensive services that not
// every run needs (optional integrations, multi-command CLIs) and to keep
// tests that resolve part of the graph from constructing the rest.
//
// A lazy singleton is still validated at Build (missing dependencies,
// cycles, lifetimes). It is constructed once, under single-flight; a failed
// construction is not cached, so the next resolution retries. It is disposed
// with the provider, before the services it depends on.
func Lazy() AddOption {
	return addLazyOption{}
}

type addLazyOption struct{}

func (addLazyOption) String() string { return "Lazy()" }

func (addLazyOption) applyAddOption(opt *addOptions) {
	opt.Lazy = true
}

// NoDispose is an AddOption declaring that the values a constructor creates
// are managed outside the container: godi never disposes them. Use it for a
// constructor that returns a resource the application shares or closes
// itself. Values registered as instances (AddSingleton(value)) are never
// disposed anyway: the caller that created them owns them.
//
// A NoDispose value is also never adopted by a scope that merely returns it.
func NoDispose() AddOption {
	return addNoDisposeOption{}
}

type addNoDisposeOption struct{}

func (addNoDisposeOption) String() string { return "NoDispose()" }

func (addNoDisposeOption) applyAddOption(opt *addOptions) {
	opt.NoDispose = true
}

// Instance marks v as a pre-built value to register, not a constructor. Pass
// it to AddSingleton when the value itself is a function, which would
// otherwise be taken for a constructor:
//
//	type Clock func() time.Time
//	services.AddSingleton(godi.Instance(Clock(time.Now)))
//
// Other values can be passed to AddSingleton directly. godi never disposes a
// value registered as an instance: the caller that created it owns it. To hand
// ownership to godi, register a constructor that returns the value instead.
func Instance(v any) any {
	return instanceValue{value: v}
}

// instanceValue wraps a value registered through Instance.
type instanceValue struct{ value any }

// ---------------------------------------------------------------------------
// Build options
// ---------------------------------------------------------------------------

// A BuildOption configures Collection.Build.
type BuildOption interface {
	applyBuildOption(*buildOptions)
}

type buildOptions struct {
	context        context.Context
	timeout        time.Duration
	observer       Observer
	validateScopes bool
}

func newBuildOptions(opts []BuildOption) buildOptions {
	options := buildOptions{validateScopes: true}
	for _, opt := range opts {
		if opt != nil {
			opt.applyBuildOption(&options)
		}
	}
	return options
}

type buildOptionFunc func(*buildOptions)

func (f buildOptionFunc) applyBuildOption(o *buildOptions) { f(o) }

// WithContext sets the parent of the provider's root context: its values are
// visible to services, and its cancellation propagates to them. It also bounds
// Build: Build fails if it is cancelled, and constructors that run during
// Build receive the provider's root context, which is cancelled with it (or
// when a WithBuildTimeout expires) but reports no deadline. A nil ctx means
// context.Background(); the last WithContext wins.
func WithContext(ctx context.Context) BuildOption {
	return buildOptionFunc(func(o *buildOptions) { o.context = ctx })
}

// WithBuildTimeout sets a cooperative deadline for Build. Constructors that
// accept context.Context can stop promptly when it expires; others cannot be
// preempted, but an expired deadline is checked after they return and never
// produces a provider. The deadline bounds Build only: once Build succeeds,
// the context given to singletons is no longer subject to it. A duration of
// zero or less means no timeout.
func WithBuildTimeout(d time.Duration) BuildOption {
	return buildOptionFunc(func(o *buildOptions) { o.timeout = d })
}

// WithObserver sets the Observer that receives construction and disposal
// events, including failures of background cleanup that have no caller to
// report to.
func WithObserver(observer Observer) BuildOption {
	return buildOptionFunc(func(o *buildOptions) { o.observer = observer })
}

// WithScopeValidation sets whether resolving a scoped service, directly or
// through transients, from the provider's root scope fails with
// ErrScopeRequired. Resolved from the root, a "per-request" service becomes
// one instance shared by the whole application. With validation on, the root
// scope also runs no scoped initializers. Validation is on by default; turn
// it off only for applications that deliberately treat the root as a scope.
func WithScopeValidation(enabled bool) BuildOption {
	return buildOptionFunc(func(o *buildOptions) { o.validateScopes = enabled })
}
