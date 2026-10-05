package godi

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/junioryono/godi/v6/internal/reflection"
)

// Resolver is the resolution half of Provider and Scope. The generic helpers
// (Resolve, ResolveKeyed, ResolveGroup and their Must variants) accept any
// Resolver, so code that only resolves services can depend on this narrower
// interface — and tests can supply a small fake.
type Resolver interface {
	Get(serviceType reflect.Type) (any, error)
	GetKeyed(serviceType reflect.Type, name string) (any, error)
	GetGroup(serviceType reflect.Type, group string) ([]any, error)
}

// Resolve resolves a service of type T from the provider.
// This is a generic convenience function that handles type assertions.
//
// Example:
//
//	logger, err := godi.Resolve[*Logger](provider)
//	if err != nil {
//	    // Handle error
//	}
func Resolve[T any](provider Resolver) (T, error) {
	var zero T

	if provider == nil {
		return zero, ErrProviderNil
	}

	serviceType := reflect.TypeFor[T]()
	service, err := provider.Get(serviceType)
	if err != nil {
		return zero, err
	}

	result, ok := service.(T)
	if !ok {
		return zero, &TypeMismatchError{
			Expected: serviceType,
			Actual:   reflect.TypeOf(service),
			Context:  "type assertion",
		}
	}

	return result, nil
}

// MustResolve resolves a service of type T from the provider.
// It panics if the service cannot be resolved. This is useful for
// application initialization where missing services are fatal.
//
// Example:
//
//	// Panics if logger cannot be resolved
//	logger := godi.MustResolve[*Logger](provider)
func MustResolve[T any](provider Resolver) T {
	service, err := Resolve[T](provider)
	if err != nil {
		panic(fmt.Errorf("godi: failed to resolve service: %w", err))
	}

	return service
}

// ResolveKeyed resolves a keyed service of type T from the provider.
//
// Example:
//
//	cache, err := godi.ResolveKeyed[Cache](provider, "redis")
func ResolveKeyed[T any](provider Resolver, name string) (T, error) {
	var zero T

	if provider == nil {
		return zero, ErrProviderNil
	}

	if name == "" {
		return zero, ErrServiceKeyEmpty
	}

	serviceType := reflect.TypeFor[T]()
	service, err := provider.GetKeyed(serviceType, name)
	if err != nil {
		return zero, err
	}

	result, ok := service.(T)
	if !ok {
		return zero, &TypeMismatchError{
			Expected: serviceType,
			Actual:   reflect.TypeOf(service),
			Context:  "type assertion for keyed service",
		}
	}

	return result, nil
}

// MustResolveKeyed resolves a keyed service of type T from the provider.
// It panics if the service cannot be resolved.
//
// Example:
//
//	// Panics if redis cache cannot be resolved
//	cache := godi.MustResolveKeyed[Cache](provider, "redis")
func MustResolveKeyed[T any](provider Resolver, name string) T {
	service, err := ResolveKeyed[T](provider, name)
	if err != nil {
		panic(fmt.Errorf("godi: failed to resolve keyed service %q: %w", name, err))
	}

	return service
}

// ResolveGroup resolves all services of type T in the specified group.
//
// Example:
//
//	handlers, err := godi.ResolveGroup[http.Handler](provider, "routes")
func ResolveGroup[T any](provider Resolver, group string) ([]T, error) {
	if provider == nil {
		return nil, ErrProviderNil
	}

	if group == "" {
		return nil, &ValidationError{
			ServiceType: nil,
			Cause:       ErrGroupNameEmpty,
		}
	}

	serviceType := reflect.TypeFor[T]()
	services, err := provider.GetGroup(serviceType, group)
	if err != nil {
		return nil, err
	}

	results := make([]T, 0, len(services))
	for i, service := range services {
		result, ok := service.(T)
		if !ok {
			return nil, &TypeMismatchError{
				Expected: serviceType,
				Actual:   reflect.TypeOf(service),
				Context:  fmt.Sprintf("type assertion for group item %d", i),
			}
		}

		results = append(results, result)
	}

	return results, nil
}

// MustResolveGroup resolves all services of type T in the specified group.
// It panics if the services cannot be resolved.
//
// Example:
//
//	// Panics if handlers cannot be resolved
//	handlers := godi.MustResolveGroup[http.Handler](provider, "routes")
func MustResolveGroup[T any](provider Resolver, group string) []T {
	services, err := ResolveGroup[T](provider, group)
	if err != nil {
		panic(fmt.Errorf("godi: failed to resolve group %s: %w", group, err))
	}

	return services
}

// ResolveFromContext resolves a service of type T from the scope stored in
// ctx (see FromContext), as HTTP handlers under a godi scope middleware do:
//
//	svc, err := godi.ResolveFromContext[*UserService](r.Context())
func ResolveFromContext[T any](ctx context.Context) (T, error) {
	scope, err := FromContext(ctx)
	if err != nil {
		var zero T
		return zero, err
	}
	return Resolve[T](scope)
}

// ---------------------------------------------------------------------------
// Invoke and IsService
// ---------------------------------------------------------------------------

// invokeAnalyzer analyzes functions passed to Invoke without caching them:
// Invoke is called with fresh closures (e.g. per request), and caching would
// retain every one of them.
var invokeAnalyzer = reflection.New(reflection.WithNotFound(isNotFound))

// Invoke calls fn with its parameters resolved from p (a Provider or Scope),
// for side effects. fn's parameters follow constructor rules, including a
// godi.In parameter object; fn may return nothing or an error, which Invoke
// returns.
//
//	err := godi.Invoke(scope, func(db *sql.DB, log *slog.Logger) error {
//	    return migrate(db, log)
//	})
func Invoke(r Resolver, fn any) error {
	if r == nil {
		return ErrProviderNil
	}
	if fn == nil {
		return &ValidationError{Cause: errors.New("godi.Invoke: function is nil")}
	}
	fnType := reflect.TypeOf(fn)
	if fnType.Kind() != reflect.Func {
		return &ValidationError{Cause: fmt.Errorf("godi.Invoke: expected a function, got %T", fn)}
	}
	if fnType.IsVariadic() {
		return &ValidationError{Cause: errors.New("godi.Invoke: variadic functions are not supported")}
	}
	switch {
	case fnType.NumOut() == 0:
	case fnType.NumOut() == 1 && fnType.Out(0) == reflect.TypeFor[error]():
	default:
		return &ValidationError{Cause: fmt.Errorf("godi.Invoke: %s must return nothing or error", fnType)}
	}

	info, err := invokeAnalyzer.AnalyzeUncached(fn)
	if err != nil {
		return &reflectionAnalysisError{Constructor: fn, Operation: "analyze", Cause: err}
	}
	for _, param := range info.Parameters {
		if err := containerParameterError(param.Type, param.Key, param.Group); err != nil {
			return &ValidationError{Cause: fmt.Errorf("godi.Invoke: %w", err)}
		}
	}
	if _, err := invokeAnalyzer.GetInvoker().Invoke(info, r); err != nil {
		if panicErr, ok := errors.AsType[*reflection.PanicError](err); ok {
			return &ConstructorPanicError{Constructor: fnType, Panic: panicErr.Panic, Stack: panicErr.Stack}
		}
		// fn's own error is returned unchanged.
		if returned, ok := err.(*reflection.ReturnedError); ok {
			return returned.Err
		}
		return err
	}
	return nil
}

// IsService reports whether serviceType can be resolved (without a key) from
// r: it is registered, or it is one of the container's own types
// (context.Context, godi.Resolver, godi.ScopeFactory, and, except through an
// injected Resolver, godi.Provider and godi.Scope). It constructs nothing.
// r must be a Provider, a Scope, or a Resolver godi injected; for any other
// Resolver (a wrapper or test double) it reports false.
func IsService(r Resolver, serviceType reflect.Type) bool {
	if serviceType == nil {
		return false
	}
	if _, reserved := reservedTypes[serviceType]; reserved {
		if _, injected := r.(*frameResolver); injected && (serviceType == providerType || serviceType == scopeType) {
			return false // see frameResolver.Get
		}
		return rootProviderOf(r) != nil
	}
	return resolvableFrom(r, serviceType, nil)
}

// IsKeyedService reports whether serviceType is registered under name and
// can be resolved from r (see IsService for what r may be). It constructs
// nothing.
func IsKeyedService(r Resolver, serviceType reflect.Type, name string) bool {
	if serviceType == nil || name == "" {
		return false
	}
	return resolvableFrom(r, serviceType, name)
}

// resolvableFrom reports whether a registration of serviceType and key exists
// and can be resolved from p: with ValidateScopes, scoped services cannot be
// resolved from the root provider.
func resolvableFrom(r Resolver, serviceType reflect.Type, key any) bool {
	root := rootProviderOf(r)
	if root == nil {
		return false
	}
	d := root.findDescriptor(serviceType, key)
	if d == nil {
		return false
	}
	return d.Lifetime != Scoped || !root.validateScopes || !resolvesFromRoot(r)
}

// resolvesFromRoot reports whether p resolves from the provider's root scope.
func resolvesFromRoot(r Resolver) bool {
	switch v := r.(type) {
	case *provider:
		return true
	case *scope:
		return v.isRoot
	case *frameResolver:
		return v.scope.isRoot
	default:
		return false
	}
}

// rootProviderOf returns the godi provider behind r, or nil if r is nil or
// not implemented by godi (a test double).
func rootProviderOf(r Resolver) *provider {
	if impl, ok := r.(interface{ root() *provider }); ok {
		return impl.root()
	}
	return nil
}

func (p *provider) root() *provider { return p }

func (s *scope) root() *provider { return s.rootProvider }
