package godi

import (
	"errors"
	"fmt"
	"reflect"
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
//
// A module value is applied to a collection at most once, so a module shared
// by several others (a diamond: users and orders both include logging) is
// registered once.
func NewModule(name string, builders ...ModuleOption) ModuleOption {
	identity := new(moduleIdentity)
	return func(s Collection) error {
		// Attribute registration errors recorded by the builders (whose Add*
		// calls defer errors to Build) to this module by name.
		if c, ok := s.(*collection); ok {
			if !c.markModuleApplied(identity) {
				return nil
			}
			c.pushModule(name)
			defer c.popModule()
		}

		// Execute all builders in order
		for _, builder := range builders {
			if builder == nil {
				continue
			}

			if err := builder(s); err != nil {
				return &ModuleError{Module: name, Cause: err}
			}
		}

		return nil
	}
}

// AddSingleton returns a ModuleOption that registers a singleton service.
// Registration errors are recorded on the collection and reported by Build.
func AddSingleton(service any, opts ...AddOption) ModuleOption {
	return func(s Collection) error {
		s.AddSingleton(service, opts...)
		return nil
	}
}

// AddScoped returns a ModuleOption that registers a scoped service.
// Registration errors are recorded on the collection and reported by Build.
func AddScoped(service any, opts ...AddOption) ModuleOption {
	return func(s Collection) error {
		s.AddScoped(service, opts...)
		return nil
	}
}

// AddTransient returns a ModuleOption that registers a transient service.
// Registration errors are recorded on the collection and reported by Build.
func AddTransient(service any, opts ...AddOption) ModuleOption {
	return func(s Collection) error {
		s.AddTransient(service, opts...)
		return nil
	}
}

// Remove creates a ModuleOption for removing all services of type T.
// This is useful for testing scenarios where you need to replace a service
// with a mock implementation.
//
// Example:
//
//	c.AddModules(
//	    godi.Remove[posthog.Client](),
//	    godi.AddSingleton(infrastructure.NewPostHogClientMock),
//	    // ... other modules
//	)
//	// Any registration errors surface from c.Build().
func Remove[T any]() ModuleOption {
	return func(c Collection) error {
		c.Remove(reflect.TypeFor[T]())
		return nil
	}
}

// RemoveKeyed creates a ModuleOption for removing a specific keyed service of type T.
// This allows you to remove only services registered with a specific key.
//
// Example:
//
//	c.AddModules(
//	    godi.RemoveKeyed[database.Connection]("primary"),
//	    godi.AddSingleton(NewMockConnection, godi.Name("primary")),
//	    // ... other modules
//	)
//	// Any registration errors surface from c.Build().
func RemoveKeyed[T any](key any) ModuleOption {
	return func(c Collection) error {
		c.RemoveKeyed(reflect.TypeFor[T](), key)
		return nil
	}
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
func (sc *collection) registrationTargets(service any, lifetime Lifetime, opts []AddOption) ([]registryKey, error) {
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
		targets := make([]registryKey, 0, len(options.As))
		for _, iface := range options.As {
			targets = append(targets, registryKey{Type: reflect.TypeOf(iface).Elem(), Key: d.Key})
		}
		return targets, nil
	}

	var targets []registryKey
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
		targets = append(targets, registryKey{Type: ret.Type, Key: key})
	}
	if len(targets) == 0 {
		targets = append(targets, registryKey{Type: d.Type, Key: d.Key})
	}
	return targets, nil
}

func describeTargets(targets []registryKey) string {
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
