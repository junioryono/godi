package godi

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test types for module tests
type ModuleTestService struct {
	Name string
}

type ModuleTestInterface interface {
	GetName() string
}

func (m *ModuleTestService) GetName() string {
	return m.Name
}

// Constructor functions
func NewModuleTestService() *ModuleTestService {
	return &ModuleTestService{Name: "module-test"}
}

func NewModuleTestServiceWithName(name string) *ModuleTestService {
	return &ModuleTestService{Name: name}
}

// Test NewModule
func TestNewModule(t *testing.T) {
	t.Run("empty module", func(t *testing.T) {
		module := NewModule("empty")
		assert.NotNil(t, module)

		collection := NewCollection()
		err := module(collection)
		assert.NoError(t, err)
		assert.Equal(t, 0, collection.Count())
	})

	t.Run("module with single service", func(t *testing.T) {
		module := NewModule("single",
			AddSingleton(NewModuleTestService),
		)

		collection := NewCollection()
		err := module(collection)
		assert.NoError(t, err)
		assert.Equal(t, 1, collection.Count())
	})

	t.Run("module with multiple registrations of same service type (should error)", func(t *testing.T) {
		// This test expects an error because we're trying to register
		// the same type (*ModuleTestService) multiple times without keys
		module := NewModule("multiple",
			AddSingleton(NewModuleTestService),
			// These should fail because *ModuleTestService is already registered
			AddScoped(func() *ModuleTestService { return &ModuleTestService{Name: "scoped"} }),
			AddTransient(func() *ModuleTestService { return &ModuleTestService{Name: "transient"} }),
		)

		collection := NewCollection()
		err := module(collection)
		assert.Error(t, err, "Should error when registering same type multiple times without keys")
		assert.Contains(t, err.Error(), "already registered")
	})

	t.Run("module with multiple of same service (using keys)", func(t *testing.T) {
		// This test shows the correct way to register the same type multiple times
		// by using the Name() option to create keyed services
		module := NewModule("multiple-keyed",
			AddSingleton(NewModuleTestService), // Default (non-keyed)
			AddScoped(func() *ModuleTestService {
				return &ModuleTestService{Name: "scoped"}
			}, Name("scoped-service")), // Keyed with "scoped-service"
			AddTransient(func() *ModuleTestService {
				return &ModuleTestService{Name: "transient"}
			}, Name("transient-service")), // Keyed with "transient-service"
		)

		collection := NewCollection()
		err := module(collection)
		assert.NoError(t, err)
		assert.Equal(t, 3, collection.Count(), "Should have 3 services: 1 default and 2 keyed")

		// Verify the services are registered correctly
		assert.True(t, collection.Contains(reflect.TypeOf((*ModuleTestService)(nil))))
		assert.True(t, collection.ContainsKeyed(reflect.TypeOf((*ModuleTestService)(nil)), "scoped-service"))
		assert.True(t, collection.ContainsKeyed(reflect.TypeOf((*ModuleTestService)(nil)), "transient-service"))
	})

	t.Run("module with multiple services using groups", func(t *testing.T) {
		// Another way to register multiple instances of the same type is using groups
		module := NewModule("multiple-grouped",
			AddTransient(func() *ModuleTestService {
				return &ModuleTestService{Name: "handler1"}
			}, Group("handlers")),
			AddTransient(func() *ModuleTestService {
				return &ModuleTestService{Name: "handler2"}
			}, Group("handlers")),
			AddTransient(func() *ModuleTestService {
				return &ModuleTestService{Name: "handler3"}
			}, Group("handlers")),
		)

		cl := NewCollection()
		err := module(cl)
		assert.NoError(t, err)
		assert.Equal(t, 3, cl.Count())

		c := cl.(*collection)
		assert.True(t, c.HasGroup(reflect.TypeOf((*ModuleTestService)(nil)), "handlers"))
	})

	t.Run("nested modules", func(t *testing.T) {
		innerModule := NewModule("inner",
			AddSingleton(NewModuleTestService),
		)

		// Use different service types or keys to avoid conflicts
		type MiddleService struct{ Name string }
		type OuterService struct{ Name string }

		middleModule := NewModule("middle",
			innerModule,
			AddScoped(func() *MiddleService { return &MiddleService{Name: "middle"} }),
		)

		outerModule := NewModule("outer",
			middleModule,
			AddTransient(func() *OuterService { return &OuterService{Name: "outer"} }),
		)

		collection := NewCollection()
		err := outerModule(collection)
		assert.NoError(t, err)
		assert.Equal(t, 3, collection.Count())
	})

	t.Run("module with error", func(t *testing.T) {
		// First add a service
		collection := NewCollection()
		err := collection.AddSingleton(NewModuleTestService)
		require.NoError(t, err)

		// Module that tries to add duplicate
		module := NewModule("error",
			AddSingleton(NewModuleTestService), // This should fail - duplicate
		)

		err = module(collection)
		assert.Error(t, err)
		var moduleErr ModuleError
		assert.ErrorAs(t, err, &moduleErr)
		assert.Equal(t, "error", moduleErr.Module)
	})

	t.Run("module with nil builder", func(t *testing.T) {
		module := NewModule("with-nil",
			nil, // Should be skipped
			AddSingleton(NewModuleTestService),
			nil, // Should be skipped
		)

		collection := NewCollection()
		err := module(collection)
		assert.NoError(t, err)
		assert.Equal(t, 1, collection.Count())
	})
}

// Test AddSingleton ModuleOption
func TestAddSingletonModule(t *testing.T) {
	t.Run("basic singleton", func(t *testing.T) {
		option := AddSingleton(NewModuleTestService)

		collection := NewCollection()
		err := option(collection)
		assert.NoError(t, err)
		assert.Equal(t, 1, collection.Count())
		assert.True(t, collection.Contains(reflect.TypeOf((*ModuleTestService)(nil))))
	})

	t.Run("singleton with options", func(t *testing.T) {
		option := AddSingleton(NewModuleTestService, Name("primary"))

		collection := NewCollection()
		err := option(collection)
		assert.NoError(t, err)
		assert.True(t, collection.ContainsKeyed(reflect.TypeOf((*ModuleTestService)(nil)), "primary"))
	})

	t.Run("singleton with group", func(t *testing.T) {
		option := AddSingleton(NewModuleTestService, Group("services"))

		cl := NewCollection()
		err := option(cl)
		assert.NoError(t, err)

		c := cl.(*collection)
		assert.True(t, c.HasGroup(reflect.TypeOf((*ModuleTestService)(nil)), "services"))
	})

	t.Run("singleton with interface", func(t *testing.T) {
		option := AddSingleton(NewModuleTestService, As[ModuleTestInterface]())

		collection := NewCollection()
		err := option(collection)
		assert.NoError(t, err)
		assert.True(t, collection.Contains(reflect.TypeOf((*ModuleTestInterface)(nil)).Elem()))
	})
}

// Test AddScoped ModuleOption
func TestAddScopedModule(t *testing.T) {
	t.Run("basic scoped", func(t *testing.T) {
		option := AddScoped(NewModuleTestService)

		collection := NewCollection()
		err := option(collection)
		assert.NoError(t, err)
		assert.Equal(t, 1, collection.Count())
		assert.True(t, collection.Contains(reflect.TypeOf((*ModuleTestService)(nil))))
	})

	t.Run("scoped with name", func(t *testing.T) {
		option := AddScoped(NewModuleTestService, Name("scoped"))

		collection := NewCollection()
		err := option(collection)
		assert.NoError(t, err)
		assert.True(t, collection.ContainsKeyed(reflect.TypeOf((*ModuleTestService)(nil)), "scoped"))
	})
}

// Test AddTransient ModuleOption
func TestAddTransientModule(t *testing.T) {
	t.Run("basic transient", func(t *testing.T) {
		option := AddTransient(NewModuleTestService)

		collection := NewCollection()
		err := option(collection)
		assert.NoError(t, err)
		assert.Equal(t, 1, collection.Count())
		assert.True(t, collection.Contains(reflect.TypeOf((*ModuleTestService)(nil))))
	})

	t.Run("transient with group", func(t *testing.T) {
		option := AddTransient(NewModuleTestService, Group("transients"))

		cl := NewCollection()
		err := option(cl)
		assert.NoError(t, err)

		c := cl.(*collection)
		assert.True(t, c.HasGroup(reflect.TypeOf((*ModuleTestService)(nil)), "transients"))
	})
}

// Test Remove ModuleOption
func TestRemoveModuleOption(t *testing.T) {
	t.Run("removes service by type", func(t *testing.T) {
		collection := NewCollection()

		// Add a service
		err := collection.AddSingleton(NewModuleTestService)
		require.NoError(t, err)

		serviceType := reflect.TypeOf((*ModuleTestService)(nil))
		assert.True(t, collection.Contains(serviceType))

		// Remove using generic function
		err = collection.AddModules(Remove[*ModuleTestService]())
		require.NoError(t, err)

		assert.False(t, collection.Contains(serviceType))
	})

	t.Run("removes interface service", func(t *testing.T) {
		collection := NewCollection()

		// Add a service as interface
		err := collection.AddSingleton(NewModuleTestService, As[ModuleTestInterface]())
		require.NoError(t, err)

		interfaceType := reflect.TypeOf((*ModuleTestInterface)(nil)).Elem()
		assert.True(t, collection.Contains(interfaceType))

		// Remove using generic function
		err = collection.AddModules(Remove[ModuleTestInterface]())
		require.NoError(t, err)

		assert.False(t, collection.Contains(interfaceType))
	})

	t.Run("remove and replace in same module", func(t *testing.T) {
		collection := NewCollection()

		// Add initial service
		err := collection.AddSingleton(func() *ModuleTestService {
			return &ModuleTestService{Name: "original"}
		})
		require.NoError(t, err)

		serviceType := reflect.TypeOf((*ModuleTestService)(nil))
		assert.True(t, collection.Contains(serviceType))

		// Remove and replace in one module
		err = collection.AddModules(
			Remove[*ModuleTestService](),
			AddSingleton(func() *ModuleTestService {
				return &ModuleTestService{Name: "replacement"}
			}),
		)
		require.NoError(t, err)

		assert.True(t, collection.Contains(serviceType))
	})

	t.Run("remove non-existent service is no-op", func(t *testing.T) {
		collection := NewCollection()

		// Remove a service that was never added
		err := collection.AddModules(Remove[*ModuleTestService]())
		require.NoError(t, err)

		assert.Equal(t, 0, collection.Count())
	})
}

// Test RemoveKeyed ModuleOption
func TestRemoveKeyedModuleOption(t *testing.T) {
	t.Run("removes keyed service", func(t *testing.T) {
		collection := NewCollection()

		// Add keyed services
		err := collection.AddSingleton(NewModuleTestService, Name("primary"))
		require.NoError(t, err)
		err = collection.AddSingleton(NewModuleTestService, Name("secondary"))
		require.NoError(t, err)

		serviceType := reflect.TypeOf((*ModuleTestService)(nil))
		assert.True(t, collection.ContainsKeyed(serviceType, "primary"))
		assert.True(t, collection.ContainsKeyed(serviceType, "secondary"))

		// Remove only the primary one
		err = collection.AddModules(RemoveKeyed[*ModuleTestService]("primary"))
		require.NoError(t, err)

		assert.False(t, collection.ContainsKeyed(serviceType, "primary"))
		assert.True(t, collection.ContainsKeyed(serviceType, "secondary"))
	})

	t.Run("remove and replace keyed service", func(t *testing.T) {
		collection := NewCollection()

		// Add initial keyed service
		err := collection.AddSingleton(func() *ModuleTestService {
			return &ModuleTestService{Name: "original"}
		}, Name("test"))
		require.NoError(t, err)

		serviceType := reflect.TypeOf((*ModuleTestService)(nil))
		assert.True(t, collection.ContainsKeyed(serviceType, "test"))

		// Remove and replace
		err = collection.AddModules(
			RemoveKeyed[*ModuleTestService]("test"),
			AddSingleton(func() *ModuleTestService {
				return &ModuleTestService{Name: "replacement"}
			}, Name("test")),
		)
		require.NoError(t, err)

		assert.True(t, collection.ContainsKeyed(serviceType, "test"))
	})

	t.Run("remove non-keyed service with nil key", func(t *testing.T) {
		collection := NewCollection()

		// Add non-keyed service
		err := collection.AddSingleton(NewModuleTestService)
		require.NoError(t, err)

		serviceType := reflect.TypeOf((*ModuleTestService)(nil))
		assert.True(t, collection.Contains(serviceType))

		// Remove with nil key (removes the non-keyed service)
		err = collection.AddModules(RemoveKeyed[*ModuleTestService](nil))
		require.NoError(t, err)

		assert.False(t, collection.Contains(serviceType))
	})

	t.Run("remove non-existent keyed service is no-op", func(t *testing.T) {
		collection := NewCollection()

		// Add one keyed service
		err := collection.AddSingleton(NewModuleTestService, Name("existing"))
		require.NoError(t, err)

		// Remove a different key
		err = collection.AddModules(RemoveKeyed[*ModuleTestService]("nonexistent"))
		require.NoError(t, err)

		// Original service should still exist
		serviceType := reflect.TypeOf((*ModuleTestService)(nil))
		assert.True(t, collection.ContainsKeyed(serviceType, "existing"))
		assert.Equal(t, 1, collection.Count())
	})
}

// Test Name option
func TestNameOption(t *testing.T) {
	t.Run("basic name", func(t *testing.T) {
		opt := Name("test")
		assert.NotNil(t, opt)

		options := &addOptions{}
		opt.applyAddOption(options)
		assert.Equal(t, "test", options.Name)
	})

	t.Run("string representation", func(t *testing.T) {
		opt := Name("test-name")
		str := opt.(fmt.Stringer).String()
		assert.Equal(t, `Name("test-name")`, str)
	})

	t.Run("empty name", func(t *testing.T) {
		opt := Name("")
		options := &addOptions{}
		opt.applyAddOption(options)
		assert.Equal(t, "", options.Name)
	})

	t.Run("name with special characters", func(t *testing.T) {
		opt := Name("test-name_123")
		options := &addOptions{}
		opt.applyAddOption(options)
		assert.Equal(t, "test-name_123", options.Name)
	})
}

// Test Group option
func TestGroupOption(t *testing.T) {
	t.Run("basic group", func(t *testing.T) {
		opt := Group("handlers")
		assert.NotNil(t, opt)

		options := &addOptions{}
		opt.applyAddOption(options)
		assert.Equal(t, "handlers", options.Group)
	})

	t.Run("string representation", func(t *testing.T) {
		opt := Group("test-group")
		str := opt.(fmt.Stringer).String()
		assert.Equal(t, `Group("test-group")`, str)
	})

	t.Run("empty group", func(t *testing.T) {
		opt := Group("")
		options := &addOptions{}
		opt.applyAddOption(options)
		assert.Equal(t, "", options.Group)
	})
}

// Test As option
func TestAsOption(t *testing.T) {
	t.Run("single interface", func(t *testing.T) {
		opt := As[ModuleTestInterface]()
		assert.NotNil(t, opt)

		options := &addOptions{}
		opt.applyAddOption(options)
		assert.Len(t, options.As, 1)
		assert.Equal(t, reflect.TypeOf((*ModuleTestInterface)(nil)).Elem(),
			reflect.TypeOf(options.As[0]).Elem())
	})

	t.Run("multiple interfaces", func(t *testing.T) {
		type Interface1 interface{ Method1() }
		type Interface2 interface{ Method2() }

		opt1 := As[Interface1]()
		opt2 := As[Interface2]()
		options := &addOptions{}
		opt1.applyAddOption(options)
		opt2.applyAddOption(options)
		assert.Len(t, options.As, 2)
	})

	t.Run("string representation", func(t *testing.T) {
		opt := As[ModuleTestInterface]()
		str := opt.(fmt.Stringer).String()
		assert.Contains(t, str, "As(")
		assert.Contains(t, str, "ModuleTestInterface")
		assert.Contains(t, str, ")")
	})
}

// Test addOptions validation
func TestAddOptionsValidate(t *testing.T) {
	t.Run("valid options", func(t *testing.T) {
		options := &addOptions{
			Name: "test",
		}
		err := options.Validate()
		assert.NoError(t, err)
	})

	t.Run("name and group conflict", func(t *testing.T) {
		options := &addOptions{
			Name:  "test",
			Group: "group",
		}
		err := options.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot use both godi.Name and godi.Group")
	})

	t.Run("name with backtick", func(t *testing.T) {
		options := &addOptions{
			Name: "test`name",
		}
		err := options.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "names cannot contain backquotes")
	})

	t.Run("group with backtick", func(t *testing.T) {
		options := &addOptions{
			Group: "test`group",
		}
		err := options.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "group names cannot contain backquotes")
	})

	t.Run("nil in As", func(t *testing.T) {
		options := &addOptions{
			As: []any{nil},
		}
		err := options.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid godi.As(nil)")
	})

	t.Run("non-pointer in As", func(t *testing.T) {
		var iface ModuleTestInterface
		options := &addOptions{
			As: []any{iface},
		}
		err := options.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "argument must be a pointer to an interface")
	})

	t.Run("non-interface pointer in As", func(t *testing.T) {
		options := &addOptions{
			As: []any{&ModuleTestService{}},
		}
		err := options.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "argument must be a pointer to an interface")
	})
}

// Test complex module scenarios
func TestComplexModules(t *testing.T) {
	t.Run("database module pattern", func(t *testing.T) {
		type Database struct{ ConnectionString string }
		type UserRepository struct{ DB *Database }
		type OrderRepository struct{ DB *Database }

		newDB := func() *Database { return &Database{ConnectionString: "test"} }
		newUserRepo := func(db *Database) *UserRepository { return &UserRepository{DB: db} }
		newOrderRepo := func(db *Database) *OrderRepository { return &OrderRepository{DB: db} }

		databaseModule := NewModule("database",
			AddSingleton(newDB),
			AddScoped(newUserRepo),
			AddScoped(newOrderRepo),
		)

		collection := NewCollection()
		err := collection.AddModules(databaseModule)
		assert.NoError(t, err)
		assert.Equal(t, 3, collection.Count())
	})

	t.Run("module composition", func(t *testing.T) {
		// Use different types to avoid conflicts
		type CoreService struct{ Name string }
		type FeatureService struct{ Name string }
		type AppService struct{ Name string }

		// Core module
		coreModule := NewModule("core",
			AddSingleton(func() *CoreService { return &CoreService{Name: "core"} }),
		)

		// Feature module depends on core
		featureModule := NewModule("feature",
			coreModule,
			AddScoped(func() *FeatureService { return &FeatureService{Name: "feature"} }),
		)

		// App module combines everything
		appModule := NewModule("app",
			featureModule,
			AddTransient(func() *AppService { return &AppService{Name: "app"} }),
		)

		collection := NewCollection()
		err := collection.AddModules(appModule)
		assert.NoError(t, err)
		assert.Equal(t, 3, collection.Count())
	})

	t.Run("module with groups", func(t *testing.T) {
		handlersModule := NewModule("handlers",
			AddTransient(func() *ModuleTestService { return &ModuleTestService{Name: "handler1"} }, Group("handlers")),
			AddTransient(func() *ModuleTestService { return &ModuleTestService{Name: "handler2"} }, Group("handlers")),
			AddTransient(func() *ModuleTestService { return &ModuleTestService{Name: "handler3"} }, Group("handlers")),
		)

		cl := NewCollection()
		err := cl.AddModules(handlersModule)
		assert.NoError(t, err)
		assert.Equal(t, 3, cl.Count())

		c := cl.(*collection)
		assert.True(t, c.HasGroup(reflect.TypeOf((*ModuleTestService)(nil)), "handlers"))
	})

	t.Run("module error propagation", func(t *testing.T) {
		failingBuilder := func(c Collection) error {
			return errors.New("intentional error")
		}

		module := NewModule("failing",
			ModuleOption(failingBuilder),
		)

		collection := NewCollection()
		err := collection.AddModules(module)
		assert.Error(t, err)
		var moduleErr ModuleError
		assert.ErrorAs(t, err, &moduleErr)
		assert.Equal(t, "failing", moduleErr.Module)
		assert.Contains(t, moduleErr.Cause.Error(), "intentional error")
	})

	t.Run("real-world example with keyed services", func(t *testing.T) {
		// Example showing how to use keyed services in a module
		type Logger struct{ Name string }

		loggingModule := NewModule("logging",
			// Register different logger implementations with keys
			AddSingleton(func() *Logger { return &Logger{Name: "default"} }),
			AddSingleton(func() *Logger { return &Logger{Name: "debug"} }, Name("debug")),
			AddSingleton(func() *Logger { return &Logger{Name: "audit"} }, Name("audit")),
		)

		collection := NewCollection()
		err := collection.AddModules(loggingModule)
		assert.NoError(t, err)
		assert.Equal(t, 3, collection.Count())

		// Verify all three loggers are registered
		assert.True(t, collection.Contains(reflect.TypeOf((*Logger)(nil))))
		assert.True(t, collection.ContainsKeyed(reflect.TypeOf((*Logger)(nil)), "debug"))
		assert.True(t, collection.ContainsKeyed(reflect.TypeOf((*Logger)(nil)), "audit"))
	})
}

// Benchmark tests
func BenchmarkModule(b *testing.B) {
	b.Run("simple module", func(b *testing.B) {
		module := NewModule("bench",
			AddSingleton(NewModuleTestService),
		)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			collection := NewCollection()
			_ = module(collection)
		}
	})

	b.Run("complex module", func(b *testing.B) {
		module := NewModule("complex",
			AddSingleton(NewModuleTestService),
			AddScoped(NewModuleTestService, Name("scoped")),
			AddTransient(NewModuleTestService, Group("group")),
		)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			collection := NewCollection()
			_ = module(collection)
		}
	})

	b.Run("nested modules", func(b *testing.B) {
		inner := NewModule("inner",
			AddSingleton(NewModuleTestService),
		)

		outer := NewModule("outer",
			inner,
			AddScoped(NewModuleTestService, Name("outer")),
		)

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			collection := NewCollection()
			_ = outer(collection)
		}
	})
}

func BenchmarkAddOptions(b *testing.B) {
	b.Run("Name", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			opt := Name("test")
			options := &addOptions{}
			opt.applyAddOption(options)
		}
	})

	b.Run("Group", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			opt := Group("test")
			options := &addOptions{}
			opt.applyAddOption(options)
		}
	})

	b.Run("As", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			opt := As[ModuleTestInterface]()
			options := &addOptions{}
			opt.applyAddOption(options)
		}
	})

	b.Run("Validate", func(b *testing.B) {
		options := &addOptions{
			Name: "test",
			As:   []any{new(ModuleTestInterface)},
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_ = options.Validate()
		}
	})
}
