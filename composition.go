package godi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"

	"github.com/junioryono/godi/v5/internal/reflection"
)

// ---------------------------------------------------------------------------
// Lazy singletons
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Replace and TryAdd
// ---------------------------------------------------------------------------

// ReplaceSingleton is a ModuleOption that replaces the existing registrations
// of the service's type (and name, with godi.Name; or interfaces, with
// godi.As) with a singleton registration of service. It is an error if
// nothing is registered to replace, so a replacement ordered before the
// original registration is reported instead of silently doing nothing.
//
// Only the matching outputs are replaced: if the original registration was a
// multi-return or godi.Out constructor that also provides other services,
// that constructor still runs for them.
func ReplaceSingleton(service any, opts ...AddOption) ModuleOption {
	return replaceService(service, Singleton, opts)
}

// ReplaceScoped is like ReplaceSingleton for a scoped registration.
func ReplaceScoped(service any, opts ...AddOption) ModuleOption {
	return replaceService(service, Scoped, opts)
}

// ReplaceTransient is like ReplaceSingleton for a transient registration.
func ReplaceTransient(service any, opts ...AddOption) ModuleOption {
	return replaceService(service, Transient, opts)
}

// TryAddSingleton is a ModuleOption that registers service as a singleton
// only if nothing is registered yet for its type (and name, with godi.Name;
// or interfaces, with godi.As). Libraries use it to provide defaults that an
// application may already have registered.
func TryAddSingleton(service any, opts ...AddOption) ModuleOption {
	return tryAddService(service, Singleton, opts)
}

// TryAddScoped is like TryAddSingleton for a scoped registration.
func TryAddScoped(service any, opts ...AddOption) ModuleOption {
	return tryAddService(service, Scoped, opts)
}

// TryAddTransient is like TryAddSingleton for a transient registration.
func TryAddTransient(service any, opts ...AddOption) ModuleOption {
	return tryAddService(service, Transient, opts)
}

func replaceService(service any, lifetime Lifetime, opts []AddOption) ModuleOption {
	return func(c Collection) error {
		sc, ok := c.(*collection)
		if !ok {
			return errUnsupportedCollection("Replace")
		}
		targets, err := sc.registrationTargets(service, lifetime, opts)
		if err != nil {
			return err
		}

		sc.mu.Lock()
		removed := make(map[*descriptor]struct{}, len(targets))
		for _, target := range targets {
			if d, exists := sc.services[target]; exists {
				delete(sc.services, target)
				removed[d] = struct{}{}
			}
		}
		if len(removed) == 0 {
			sc.mu.Unlock()
			return &RegistrationError{
				ServiceType: targets[0].Type,
				Operation:   "replace",
				Cause:       fmt.Errorf("nothing to replace: no registration for %s", describeTargets(targets)),
			}
		}
		sc.pruneDescriptors(removed)
		sc.mu.Unlock()

		return sc.addService(service, lifetime, opts...)
	}
}

func tryAddService(service any, lifetime Lifetime, opts []AddOption) ModuleOption {
	return func(c Collection) error {
		sc, ok := c.(*collection)
		if !ok {
			return errUnsupportedCollection("TryAdd")
		}
		targets, err := sc.registrationTargets(service, lifetime, opts)
		if err != nil {
			return err
		}

		sc.mu.RLock()
		for _, target := range targets {
			if _, exists := sc.services[target]; exists {
				sc.mu.RUnlock()
				return nil
			}
		}
		sc.mu.RUnlock()

		return sc.addService(service, lifetime, opts...)
	}
}

// registrationTargets returns the registry keys a registration of service
// with opts would occupy. Group registrations and result objects are not
// supported by Replace and TryAdd.
func (sc *collection) registrationTargets(service any, lifetime Lifetime, opts []AddOption) ([]TypeKey, error) {
	d, err := newDescriptorWithAnalyzer(service, lifetime, sc.analyzer, opts...)
	if err != nil {
		return nil, err
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	options := &addOptions{}
	for _, opt := range opts {
		if opt != nil {
			opt.applyAddOption(options)
		}
	}
	if options.Group != "" {
		return nil, &ValidationError{
			ServiceType: d.Type,
			Cause:       errors.New("godi.Replace and godi.TryAdd do not support godi.Group; use Remove and Add for group members"),
		}
	}
	if d.VoidReturn {
		return nil, &ValidationError{
			ServiceType: d.Type,
			Cause:       errors.New("godi.Replace and godi.TryAdd need a constructor that returns a service; this one returns no service"),
		}
	}
	if d.info.IsResultObject {
		return nil, &ValidationError{
			ServiceType: d.Type,
			Cause:       errors.New("godi.Replace and godi.TryAdd do not support result objects (godi.Out); register its fields individually"),
		}
	}

	if len(options.As) > 0 {
		targets := make([]TypeKey, 0, len(options.As))
		for _, iface := range options.As {
			targets = append(targets, TypeKey{Type: reflect.TypeOf(iface).Elem(), Key: d.Key})
		}
		return targets, nil
	}

	var targets []TypeKey
	first := true
	for _, ret := range d.info.Returns {
		if ret.IsError {
			continue
		}
		var key any
		if first {
			key = d.Key
		}
		first = false
		targets = append(targets, TypeKey{Type: ret.Type, Key: key})
	}
	if len(targets) == 0 {
		targets = append(targets, TypeKey{Type: d.Type, Key: d.Key})
	}
	return targets, nil
}

func describeTargets(targets []TypeKey) string {
	s := ""
	for i, t := range targets {
		if i > 0 {
			s += ", "
		}
		s += formatType(t.Type)
		if t.Key != nil {
			s += fmt.Sprintf(" (key: %v)", t.Key)
		}
	}
	return s
}

func errUnsupportedCollection(operation string) error {
	return fmt.Errorf("godi.%s requires a Collection created by godi.NewCollection", operation)
}

// ---------------------------------------------------------------------------
// Validate
// ---------------------------------------------------------------------------

// Validate checks a collection's wiring without constructing anything:
// registration errors, missing dependencies, dependency cycles, lifetime
// conflicts, and decorators that match no registration — everything Build
// checks before it creates singletons. Use it in tests so they don't need
// the infrastructure (databases, servers) that singleton constructors open.
func Validate(c Collection) error {
	sc, ok := c.(*collection)
	if !ok {
		return errUnsupportedCollection("Validate")
	}
	_, err := sc.plan(context.Background())
	return err
}

// ---------------------------------------------------------------------------
// Invoke and IsService
// ---------------------------------------------------------------------------

// invokeAnalyzer analyzes functions passed to Invoke without caching them:
// Invoke is called with fresh closures (e.g. per request), and caching would
// retain every one of them.
var invokeAnalyzer = reflection.New()

// Invoke calls fn with its parameters resolved from p (a Provider or Scope),
// for side effects. fn's parameters follow constructor rules, including a
// godi.In parameter object; fn may return nothing or an error, which Invoke
// returns.
//
//	err := godi.Invoke(scope, func(db *sql.DB, log *slog.Logger) error {
//	    return migrate(db, log)
//	})
func Invoke(p Provider, fn any) error {
	if p == nil {
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
		return &ReflectionAnalysisError{Constructor: fn, Operation: "analyze", Cause: err}
	}
	if _, err := invokeAnalyzer.GetInvoker().Invoke(info, p); err != nil {
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
// p, a Provider or Scope: it is registered, or it is one of the container's
// own types (context.Context, godi.Provider, godi.Scope). It constructs
// nothing.
func IsService(p Provider, serviceType reflect.Type) bool {
	if serviceType == nil {
		return false
	}
	if _, reserved := reservedTypes[serviceType]; reserved {
		return true
	}
	return resolvableFrom(p, serviceType, nil)
}

// IsKeyedService reports whether serviceType is registered under key in p,
// a Provider or Scope. It constructs nothing.
func IsKeyedService(p Provider, serviceType reflect.Type, key any) bool {
	if serviceType == nil || key == nil || !reflect.ValueOf(key).Comparable() {
		return false
	}
	return resolvableFrom(p, serviceType, key)
}

// resolvableFrom reports whether a registration of serviceType and key exists
// and can be resolved from p: with ValidateScopes, scoped services cannot be
// resolved from the root provider.
func resolvableFrom(p Provider, serviceType reflect.Type, key any) bool {
	root := rootProviderOf(p)
	if root == nil {
		return false
	}
	d := root.findDescriptor(serviceType, key)
	if d == nil {
		return false
	}
	return d.Lifetime != Scoped || !root.validateScopes || !resolvesFromRoot(p)
}

// resolvesFromRoot reports whether p resolves from the provider's root scope.
func resolvesFromRoot(p Provider) bool {
	switch v := p.(type) {
	case *provider, *frameProvider:
		return true
	case *scope:
		return v.isRoot
	case *frameScope:
		return v.isRoot
	default:
		return false
	}
}

// rootProviderOf returns the godi provider behind p, or nil if p is not
// implemented by godi.
func rootProviderOf(p Provider) *provider {
	if r, ok := p.(interface{ root() *provider }); ok {
		return r.root()
	}
	return nil
}

func (p *provider) root() *provider { return p }

func (s *scope) root() *provider { return s.rootProvider }

// ---------------------------------------------------------------------------
// Start and HealthCheck
// ---------------------------------------------------------------------------

// Starter is implemented by singletons that have startup work to run after
// the whole graph is built, such as starting a server or a consumer loop.
type Starter interface {
	Start(ctx context.Context) error
}

// Start calls Start(ctx) on every created singleton that implements Starter,
// in creation order (dependencies first), stopping at the first error. Lazy
// singletons that have not been resolved yet are not created. Start runs at
// most once per provider. Stop started services by closing the provider
// (godi.Shutdown with a deadline): disposal runs in reverse creation order.
func Start(ctx context.Context, p Provider) error {
	root := rootProviderOf(p)
	if root == nil {
		return errors.New("godi.Start requires a Provider built by godi")
	}
	if root.disposed.Load() != 0 {
		return ErrProviderDisposed
	}
	if !root.started.CompareAndSwap(false, true) {
		return errors.New("godi.Start: the provider has already been started")
	}
	for _, s := range root.createdSingletons() {
		starter, ok := s.instance.(Starter)
		if !ok {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := starter.Start(ctx); err != nil {
			return fmt.Errorf("start %s: %w", formatType(s.serviceType), err)
		}
	}
	return nil
}

// HealthChecker is implemented by singletons that can report their health,
// such as a database pool pinging its server.
type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}

// HealthCheck runs HealthCheck(ctx) concurrently on every created singleton
// that implements HealthChecker and returns their failures joined, each
// prefixed with its service type, or nil when all are healthy. It creates
// no services.
func HealthCheck(ctx context.Context, p Provider) error {
	root := rootProviderOf(p)
	if root == nil {
		return errors.New("godi.HealthCheck requires a Provider built by godi")
	}
	if root.disposed.Load() != 0 {
		return ErrProviderDisposed
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, s := range root.createdSingletons() {
		checker, ok := s.instance.(HealthChecker)
		if !ok {
			continue
		}
		wg.Go(func() {
			if err := checker.HealthCheck(ctx); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: %w", formatType(s.serviceType), err))
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

type createdSingleton struct {
	serviceType reflect.Type
	instance    any
}

// recordConstructed adds a constructed singleton (before decoration) to the
// inventory that Start and HealthCheck act on.
func (p *provider) recordConstructed(serviceType reflect.Type, instance any) {
	p.constructedMu.Lock()
	p.constructed = append(p.constructed, createdSingleton{serviceType: serviceType, instance: instance})
	p.constructedMu.Unlock()
}

// createdSingletons returns the singletons constructed so far, before
// decoration, in creation order, each once (interface aliases and sibling
// outputs share an instance).
func (p *provider) createdSingletons() []createdSingleton {
	p.constructedMu.Lock()
	constructed := append([]createdSingleton(nil), p.constructed...)
	p.constructedMu.Unlock()

	seen := make(map[disposableIdentity]struct{}, len(constructed))
	created := constructed[:0]
	for _, c := range constructed {
		if identity, identifiable := identifyDisposable(c.instance); identifiable {
			if _, dup := seen[identity]; dup {
				continue
			}
			seen[identity] = struct{}{}
		}
		created = append(created, c)
	}
	return created
}
