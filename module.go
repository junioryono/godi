package godi

// ModuleBuilder represents a registration action within a module.
type ModuleBuilder func(ServiceCollection) error

// Module creates a new module with the given name and builders.
// Modules are a way to group related service registrations together.
//
// Example:
//
//	var DatabaseModule = godi.Module("database",
//	    godi.AddSingleton(NewDatabaseConnection),
//	    godi.AddScoped(NewUserRepository),
//	    godi.AddScoped(NewOrderRepository),
//	)
//
//	var CacheModule = godi.Module("cache",
//	    godi.AddSingleton(cache.New[any]),
//	    godi.AddSingleton(NewCacheMetrics),
//	)
//
//	var AppModule = godi.Module("app",
//	    godi.AddModule(DatabaseModule),
//	    godi.AddModule(CacheModule),
//	    godi.AddScoped(NewAppService),
//	)
func Module(name string, builders ...ModuleBuilder) func(ServiceCollection) error {
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

// AddModule creates a ModuleBuilder that adds another module.
func AddModule(module func(ServiceCollection) error) ModuleBuilder {
	return func(s ServiceCollection) error {
		if module == nil {
			return nil
		}
		return module(s)
	}
}

// AddSingleton creates a ModuleBuilder for adding a singleton service.
func AddSingleton(constructor interface{}, opts ...ProvideOption) ModuleBuilder {
	return func(s ServiceCollection) error {
		return s.AddSingleton(constructor, opts...)
	}
}

// AddScoped creates a ModuleBuilder for adding a scoped service.
func AddScoped(constructor interface{}, opts ...ProvideOption) ModuleBuilder {
	return func(s ServiceCollection) error {
		return s.AddScoped(constructor, opts...)
	}
}

// AddTransient creates a ModuleBuilder for adding a transient service.
func AddTransient(constructor interface{}, opts ...ProvideOption) ModuleBuilder {
	return func(s ServiceCollection) error {
		return s.AddTransient(constructor, opts...)
	}
}

// AddDecorator creates a ModuleBuilder for adding a decorator.
func AddDecorator(decorator interface{}, opts ...DecorateOption) ModuleBuilder {
	return func(s ServiceCollection) error {
		return s.Decorate(decorator, opts...)
	}
}
