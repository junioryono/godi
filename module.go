package godi

// ModuleOption represents a registration action within a module.
type ModuleOption func(ServiceCollection) error

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
//	    godi.AddScoped(NewAppService),
//	)
func NewModule(name string, builders ...ModuleOption) ModuleOption {
	return func(s ServiceCollection) error {
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
func AddSingleton(constructor interface{}, opts ...ProvideOption) ModuleOption {
	return func(s ServiceCollection) error {
		return s.AddSingleton(constructor, opts...)
	}
}

// AddScoped creates a ModuleBuilder for adding a scoped service.
func AddScoped(constructor interface{}, opts ...ProvideOption) ModuleOption {
	return func(s ServiceCollection) error {
		return s.AddScoped(constructor, opts...)
	}
}

// AddTransient creates a ModuleBuilder for adding a transient service.
func AddTransient(constructor interface{}, opts ...ProvideOption) ModuleOption {
	return func(s ServiceCollection) error {
		return s.AddTransient(constructor, opts...)
	}
}

// AddDecorator creates a ModuleBuilder for adding a decorator to a service.
func AddDecorator(decorator interface{}, opts ...ProvideOption) ModuleOption {
	return func(s ServiceCollection) error {
		return s.Decorate(decorator, opts...)
	}
}
