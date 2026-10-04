package godi

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
)

// An AddOption modifies the default behavior of AddSingleton, AddScoped, and AddTransient.
type AddOption interface {
	applyAddOption(*addOptions)
}

type addOptions struct {
	Name      string
	Key       any
	Group     string
	As        []any
	NoDispose bool
	Lazy      bool
}

// key returns the registration key from godi.Key or godi.Name, or nil.
func (o *addOptions) key() any {
	if o.Key != nil {
		return o.Key
	}
	if o.Name != "" {
		return o.Name
	}
	return nil
}

func (o *addOptions) Validate() error {
	if o.Key != nil {
		switch {
		case o.Name != "":
			return &ValidationError{Cause: fmt.Errorf("cannot use both godi.Key and godi.Name")}
		case o.Group != "":
			return &ValidationError{Cause: fmt.Errorf("cannot use both godi.Key and godi.Group")}
		case !reflect.ValueOf(o.Key).Comparable():
			return &ValidationError{Cause: fmt.Errorf("invalid godi.Key(%v): key of type %T is not comparable", o.Key, o.Key)}
		case !reflect.ValueOf(o.Key).Equal(reflect.ValueOf(o.Key)):
			// e.g. NaN: comparable, but a lookup could never match it.
			return &ValidationError{Cause: fmt.Errorf("invalid godi.Key(%v): the key is not equal to itself", o.Key)}
		}
	}
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
// returns, the name applies to the first non-error return only; the other
// returns are registered without a name.
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

// Key is an AddOption that registers the service under key, which may be any
// comparable value (a string, an enum constant, a struct). Resolve it with
// ResolveKeyed or GetKeyed using an equal key. godi.Name(s) is the same as
// godi.Key(s) for a string s; struct tags (name:"...") can refer to string
// keys only.
func Key(key any) AddOption {
	return addKeyOption{key: key}
}

type addKeyOption struct{ key any }

func (o addKeyOption) String() string { return fmt.Sprintf("Key(%v)", o.key) }

func (o addKeyOption) applyAddOption(opt *addOptions) {
	opt.Key = o.key
}

// Group is an AddOption that adds the values produced by a constructor to the
// specified group. For a constructor with several non-error returns, every
// return is added to the group of its own type. Consume a group with
// ResolveGroup or an In field of slice type tagged `group:"..."`.
//
// This option cannot be combined with Name or Key, and cannot be provided for
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
// (context.Context, godi.Provider, godi.Scope) cannot be registered this way.
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

// NoDispose is an AddOption declaring that the registered service's lifetime
// is managed outside the container: godi never disposes it. Use it for
// resources the application shares or closes itself, such as a pre-built
// *sql.DB or os.Stdout passed to AddSingleton.
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
// Other values can be passed to AddSingleton directly.
func Instance(v any) any {
	return instanceValue{value: v}
}

// instanceValue wraps a value registered through Instance.
type instanceValue struct{ value any }
