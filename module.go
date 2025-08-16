package godi

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
)

// ModuleOption represents a registration action within a module.
type ModuleOption func(Collection) error

// NewModule creates a new module with the given name and builders.
// Modules are a way to group related service registrations together.
//
// Example:
//
//	var DatabaseModule = godi.NewModule("database",
//	    godi.AddSingleton(NewDatabaseConnection),
//	    godi.AddScoped(NewUserRepository),
//	    godi.AddScoped(NewOrderRepository),
//	)
//
//	var CacheModule = godi.NewModule("cache",
//	    godi.AddSingleton(cache.New[any]),
//	    godi.AddSingleton(NewCacheMetrics),
//	)
//
//	var AppModule = godi.NewModule("app",
//	    DatabaseModule,
//	    CacheModule,
//	    godi.AddScoped(NewService1),
//	    godi.AddScoped(NewService1, godi.Name("service1")),
//	    godi.AddScoped(NewService1, godi.Name("service2")),
//	)
func NewModule(name string, builders ...ModuleOption) ModuleOption {
	return func(s Collection) error {
		// Execute all builders in order
		for _, builder := range builders {
			if builder == nil {
				continue
			}

			if err := builder(s); err != nil {
				return ModuleError{Module: name, Cause: err}
			}
		}

		return nil
	}
}

// AddSingleton creates a ModuleBuilder for adding a singleton service.
func AddSingleton(service any, opts ...AddOption) ModuleOption {
	return func(s Collection) error {
		return s.AddSingleton(service, opts...)
	}
}

// AddScoped creates a ModuleBuilder for adding a scoped service.
func AddScoped(service any, opts ...AddOption) ModuleOption {
	return func(s Collection) error {
		return s.AddScoped(service, opts...)
	}
}

// AddTransient creates a ModuleBuilder for adding a transient service.
func AddTransient(service any, opts ...AddOption) ModuleOption {
	return func(s Collection) error {
		return s.AddTransient(service, opts...)
	}
}

// AddDecorator creates a ModuleBuilder for adding a decorator to a service.
func AddDecorator(decorator any, opts ...AddOption) ModuleOption {
	return func(s Collection) error {
		return s.Decorate(decorator, opts...)
	}
}

// An AddOption modifies the default behavior of AddSingleton, AddScoped, and AddTransient.
type AddOption interface {
	applyAddOption(*addOptions)
}

type addOptions struct {
	Name  string
	Group string
	As    []any
}

func (o *addOptions) Validate() error {
	if len(o.Group) > 0 {
		if len(o.Name) > 0 {
			return fmt.Errorf("cannot use both godi.Name and godi.Group: name:%q provided with group:%q", o.Name, o.Group)
		}
	}

	// Names must be representable inside a backquoted string. The only
	// limitation for raw string literals as per
	// https://golang.org/ref/spec#raw_string_lit is that they cannot contain
	// backquotes.
	if strings.ContainsRune(o.Name, '`') {
		return fmt.Errorf("invalid godi.Name(%q): names cannot contain backquotes", o.Name)
	}
	if strings.ContainsRune(o.Group, '`') {
		return fmt.Errorf("invalid godi.Group(%q): group names cannot contain backquotes", o.Group)
	}

	for _, i := range o.As {
		t := reflect.TypeOf(i)

		if t == nil {
			return fmt.Errorf("invalid godi.As(nil): argument must be a pointer to an interface")
		}

		if t.Kind() != reflect.Ptr {
			return fmt.Errorf("invalid godi.As(%v): argument must be a pointer to an interface", t)
		}

		pointingTo := t.Elem()
		if pointingTo.Kind() != reflect.Interface {
			return fmt.Errorf("invalid godi.As(*%v): argument must be a pointer to an interface", pointingTo)
		}
	}
	return nil
}

// Name is an AddOption that specifies that all values produced by a
// constructor should have the given name. See also the package documentation
// about Named Values.
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

// Group is an AddOption that specifies that all values produced by a
// constructor should be added to the specified group. See also the package
// documentation about Value Groups.
//
// This option cannot be provided for constructors which produce result
// objects.
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
// constructor implements one or more other interfaces and is provided
// to the container as those interfaces.
//
// As expects one or more pointers to the implemented interfaces. Values
// produced by constructors will be then available in the container as
// implementations of all of those interfaces, but not as the value itself.
//
// For example, the following will make io.Reader and io.Writer available
// in the container, but not buffer.
//
//	c.AddSingleton(newBuffer, godi.As(new(io.Reader), new(io.Writer)))
//
// That is, the above is equivalent to the following.
//
//	c.AddSingleton(func(...) (io.Reader, io.Writer) {
//	  b := newBuffer(...)
//	  return b, b
//	})
//
// If used with godi.Name, the type produced by the constructor and the types
// specified with godi.As will all use the same name. For example,
//
//	c.AddSingleton(newFile, godi.As(new(io.Reader)), godi.Name("temp"))
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
// objects.
func As(i ...any) AddOption {
	return addAsOption(i)
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
