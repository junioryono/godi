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

	// source names the decorator function and its location.
	source string

	// injectsContainer reports whether the decorator receives godi.Scope or
	// godi.Provider.
	injectsContainer bool
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
// innermost.
//
// A decorator's result that is itself disposable owns the value it wraps:
// godi disposes only the outermost disposable layer, which should close what
// it wraps; a non-disposable wrapper leaves the wrapped value to godi. Start
// and HealthCheck act on the constructed service, not on decorators' results.
//
// It is a Build error if a decorator matches no registration, or depends
// (directly or through other services) on another output of the decorated
// service's own constructor.
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
		source:       funcLocation(reflect.ValueOf(fn)),
		fnType:       fnType,
		info:         info,
		target:       target,
		group:        options.Group,
		dependencies: info.Dependencies()[1:],
	}
	for _, param := range info.Parameters[1:] {
		if param.Key == nil && (param.Type == scopeType || param.Type == providerType || param.Type == contextType) {
			dec.injectsContainer = true
		}
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
) (sources map[*reflection.Dependency]string, err error) {
	sources = make(map[*reflection.Dependency]string)
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
			if err := checkDecoratorDependencies(dec, d, services, groups); err != nil {
				errs = append(errs, err)
				continue
			}
			d.decorators = append(d.decorators[:len(d.decorators):len(d.decorators)], dec)
			d.Dependencies = append(d.Dependencies[:len(d.Dependencies):len(d.Dependencies)], dec.dependencies...)
			for _, dep := range dec.dependencies {
				sources[dep] = dec.source
			}
			// A decorator that receives the container can resolve
			// dynamically: its construction needs a frame for cycle
			// detection.
			if dec.injectsContainer {
				d.injectsContainer = true
			}
		}
	}
	return sources, errors.Join(errs...)
}

// checkDecoratorDependencies rejects a decorator of d that depends, directly
// or through other services, on an output of d's own constructor: that output
// is still being produced when the decorator runs, so it could never be
// resolved.
func checkDecoratorDependencies(dec *decoration, d *descriptor, services map[TypeKey]*descriptor, groups map[GroupKey][]*descriptor) error {
	target := flightKey(d)
	visited := make(map[*descriptor]bool)
	var reach func(deps []*reflection.Dependency) *descriptor
	reach = func(deps []*reflection.Dependency) *descriptor {
		for _, dep := range deps {
			for _, depDescriptor := range dependencyDescriptors(dep, services, groups) {
				if flightKey(depDescriptor) == target {
					return depDescriptor
				}
				if visited[depDescriptor] {
					continue
				}
				visited[depDescriptor] = true
				if hit := reach(depDescriptor.Dependencies); hit != nil {
					return hit
				}
			}
		}
		return nil
	}
	if hit := reach(dec.dependencies); hit != nil {
		return &RegistrationError{
			ServiceType: dec.target,
			Operation:   "decorate",
			Cause: fmt.Errorf("decorator %s depends on %s, which the same constructor produces (%s)",
				dec.source, formatType(hit.Type), d.source),
		}
	}
	return nil
}

// applyDecorators runs d's decorators over value, resolving their
// dependencies through resolver. It returns every layer: value, then each
// decorator's result. On failure the layers produced so far are returned with
// the error, so the caller can release them.
func applyDecorators(d *descriptor, value any, resolver reflection.DependencyResolver) ([]any, error) {
	layers := make([]any, 1, len(d.decorators)+1)
	layers[0] = value
	for _, dec := range d.decorators {
		args := make([]reflect.Value, 1, len(dec.info.Parameters))
		args[0] = reflect.ValueOf(value)
		for i, param := range dec.info.Parameters[1:] {
			arg, err := resolveDecoratorParam(resolver, &dec.info.Parameters[i+1])
			if err != nil {
				return layers, fmt.Errorf("decorate %s: resolve %s: %w", formatType(dec.target), formatType(param.Type), err)
			}
			args = append(args, reflect.ValueOf(arg))
		}

		results, err := callDecorator(dec, args)
		if err != nil {
			return layers, err
		}
		if len(results) == 2 && !results[1].IsNil() {
			return layers, fmt.Errorf("decorate %s: %w", formatType(dec.target), results[1].Interface().(error))
		}
		if reflection.IsNilValue(results[0]) {
			return layers, fmt.Errorf("decorate %s: decorator returned nil", formatType(dec.target))
		}
		value = results[0].Interface()
		layers = append(layers, value)
	}
	return layers, nil
}

// ownerLayer returns the index of the layer godi disposes: the outermost
// disposable one. A disposable decorator result owns (and closes) the value
// it wraps; a non-disposable one leaves it to godi. It returns -1 when no
// layer is disposable.
func ownerLayer(layers []any) int {
	for i := len(layers) - 1; i >= 0; i-- {
		if isDisposable(layers[i]) {
			return i
		}
	}
	return -1
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
