package godi

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/junioryono/godi/v5/internal/reflection"
)

// decoration is one registered decorator.
type decoration struct {
	fn     reflect.Value
	fnType reflect.Type
	info   *reflection.ConstructorInfo

	// target identifies the decorated registrations: Type plus Key (godi.Name)
	// or Group (godi.Group, decorating every member).
	target reflect.Type
	key    any
	group  string

	// dependencies are the decorator's parameters after the decorated value.
	dependencies []*reflection.Dependency
}

// Decorate is a ModuleOption that wraps a registered service with fn, which
// receives the service and returns its replacement of the same type:
//
//	godi.Decorate(func(next Store, log *slog.Logger) Store {
//	    return &loggingStore{next: next, log: log}
//	})
//
// fn's first parameter selects the decorated service type; with godi.Name the
// keyed registration is decorated, and with godi.Group every member of the
// group. Its other parameters are dependencies, resolved and validated like a
// constructor's. fn may also return an error.
//
// The decorated service keeps its lifetime: fn runs once per singleton, once
// per scope for scoped services, and on every resolution of a transient.
// Several decorators of one service apply in registration order, the first
// innermost. Both the original value and the decorator's result are disposed,
// the decorator's result first. It is a Build error if a decorator matches no
// registration.
func Decorate(fn any, opts ...AddOption) ModuleOption {
	return func(c Collection) error {
		sc, ok := c.(*collection)
		if !ok {
			return errUnsupportedCollection("Decorate")
		}
		dec, err := newDecoration(sc.analyzer, fn, opts)
		if err != nil {
			return err
		}
		sc.mu.Lock()
		sc.decorators = append(sc.decorators, dec)
		sc.mu.Unlock()
		return nil
	}
}

func newDecoration(analyzer *reflection.Analyzer, fn any, opts []AddOption) (*decoration, error) {
	invalid := func(format string, args ...any) error {
		return &RegistrationError{
			Operation: "decorate",
			Cause:     fmt.Errorf(format, args...),
		}
	}
	if fn == nil {
		return nil, invalid("decorator is nil")
	}
	fnType := reflect.TypeOf(fn)
	if fnType.Kind() != reflect.Func {
		return nil, invalid("decorator must be a function, got %T", fn)
	}
	if fnType.IsVariadic() {
		return nil, invalid("decorator %s cannot be variadic", fnType)
	}
	if fnType.NumIn() == 0 {
		return nil, invalid("decorator %s must take the decorated service as its first parameter", fnType)
	}
	target := fnType.In(0)
	errorType := reflect.TypeFor[error]()
	switch {
	case fnType.NumOut() == 1 && fnType.Out(0) == target:
	case fnType.NumOut() == 2 && fnType.Out(0) == target && fnType.Out(1) == errorType:
	default:
		return nil, invalid("decorator %s must return %s or (%s, error)", fnType, target, target)
	}

	options := &addOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt.applyAddOption(options)
		}
	}
	if err := options.Validate(); err != nil {
		return nil, err
	}
	if len(options.As) > 0 || options.NoDispose || options.Lazy {
		return nil, invalid("decorators accept only godi.Name or godi.Group")
	}

	info, err := analyzer.AnalyzeUncached(fn)
	if err != nil {
		return nil, invalid("%v", err)
	}
	if info.IsParamObject {
		return nil, invalid("decorator %s must take the decorated service as its first parameter, not a parameter object", fnType)
	}

	dec := &decoration{
		fn:           reflect.ValueOf(fn),
		fnType:       fnType,
		info:         info,
		target:       target,
		group:        options.Group,
		dependencies: info.Dependencies()[1:],
	}
	dec.key = options.key()
	return dec, nil
}

// attachDecorators attaches the collection's decorators to the matching
// descriptors of a provider snapshot, adding their dependencies to the
// decorated descriptors so they are validated and ordered like constructor
// dependencies.
func attachDecorators(
	decorators []*decoration,
	services map[TypeKey]*descriptor,
	groups map[GroupKey][]*descriptor,
) error {
	var errs []error
	for _, dec := range decorators {
		var targets []*descriptor
		if dec.group != "" {
			targets = groups[GroupKey{Type: dec.target, Group: dec.group}]
		} else if d := services[TypeKey{Type: dec.target, Key: dec.key}]; d != nil {
			targets = []*descriptor{d}
		}
		if len(targets) == 0 {
			errs = append(errs, &RegistrationError{
				ServiceType: dec.target,
				Operation:   "decorate",
				Cause:       errors.New("no registration matches the decorator"),
			})
			continue
		}
		for _, d := range targets {
			d.decorators = append(d.decorators[:len(d.decorators):len(d.decorators)], dec)
			d.Dependencies = append(d.Dependencies[:len(d.Dependencies):len(d.Dependencies)], dec.dependencies...)
		}
	}
	return errors.Join(errs...)
}

// applyDecorators runs d's decorators over value, resolving their
// dependencies through resolver.
func applyDecorators(d *descriptor, value any, resolver reflection.DependencyResolver) (any, error) {
	for _, dec := range d.decorators {
		args := make([]reflect.Value, 1, len(dec.info.Parameters))
		args[0] = reflect.ValueOf(value)
		for i, param := range dec.info.Parameters[1:] {
			arg, err := resolveDecoratorParam(resolver, &dec.info.Parameters[i+1])
			if err != nil {
				return nil, fmt.Errorf("decorate %s: resolve %s: %w", formatType(dec.target), formatType(param.Type), err)
			}
			args = append(args, reflect.ValueOf(arg))
		}

		results, err := callDecorator(dec, args)
		if err != nil {
			return nil, err
		}
		if len(results) == 2 && !results[1].IsNil() {
			return nil, fmt.Errorf("decorate %s: %w", formatType(dec.target), results[1].Interface().(error))
		}
		if reflection.IsNilValue(results[0]) {
			return nil, fmt.Errorf("decorate %s: decorator returned nil", formatType(dec.target))
		}
		value = results[0].Interface()
	}
	return value, nil
}

func resolveDecoratorParam(resolver reflection.DependencyResolver, param *reflection.ParameterInfo) (any, error) {
	if param.Group != "" {
		return nil, fmt.Errorf("group parameters are not supported in decorators")
	}
	if param.Key != nil {
		return resolver.GetKeyed(param.Type, param.Key)
	}
	return resolver.Get(param.Type)
}

func callDecorator(dec *decoration, args []reflect.Value) (results []reflect.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("decorate %s: decorator panicked: %v", formatType(dec.target), r)
		}
	}()
	return dec.fn.Call(args), nil
}

// hasDecorators reports whether any of the descriptors has decorators.
func hasDecorators(descriptors ...*descriptor) bool {
	for _, d := range descriptors {
		if len(d.decorators) > 0 {
			return true
		}
	}
	return false
}
