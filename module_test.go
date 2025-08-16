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

	t.Run("module with multiple services", func(t *testing.T) {
		module := NewModule("multiple",
			AddSingleton(NewModuleTestService),
			AddScoped(func() *ModuleTestService { return &ModuleTestService{Name: "scoped"} }),
			AddTransient(func() *ModuleTestService { return &ModuleTestService{Name: "transient"} }),
		)

		collection := NewCollection()
		err := module(collection)
		assert.NoError(t, err)
		assert.Equal(t, 3, collection.Count())
	})

	t.Run("nested modules", func(t *testing.T) {
		innerModule := NewModule("inner",
			AddSingleton(NewModuleTestService),
		)

		middleModule := NewModule("middle",
			innerModule,
			AddScoped(func() *ModuleTestService { return &ModuleTestService{Name: "middle"} }),
		)

		outerModule := NewModule("outer",
			middleModule,
			AddTransient(func() *ModuleTestService { return &ModuleTestService{Name: "outer"} }),
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
			AddSingleton(NewModuleTestService), // This should fail
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
		assert.True(t, collection.HasService(reflect.TypeOf((*ModuleTestService)(nil))))
	})

	t.Run("singleton with options", func(t *testing.T) {
		option := AddSingleton(NewModuleTestService, Name("primary"))

		collection := NewCollection()
		err := option(collection)
		assert.NoError(t, err)
		assert.True(t, collection.HasKeyedService(reflect.TypeOf((*ModuleTestService)(nil)), "primary"))
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
		option := AddSingleton(NewModuleTestService, As(new(ModuleTestInterface)))

		collection := NewCollection()
		err := option(collection)
		assert.NoError(t, err)
		assert.True(t, collection.HasService(reflect.TypeOf((*ModuleTestInterface)(nil)).Elem()))
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
		assert.True(t, collection.HasService(reflect.TypeOf((*ModuleTestService)(nil))))
	})

	t.Run("scoped with name", func(t *testing.T) {
		option := AddScoped(NewModuleTestService, Name("scoped"))

		collection := NewCollection()
		err := option(collection)
		assert.NoError(t, err)
		assert.True(t, collection.HasKeyedService(reflect.TypeOf((*ModuleTestService)(nil)), "scoped"))
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
		assert.True(t, collection.HasService(reflect.TypeOf((*ModuleTestService)(nil))))
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

// Test AddDecorator ModuleOption
func TestAddDecoratorModule(t *testing.T) {
	decorator := func(service *ModuleTestService) *ModuleTestService {
		service.Name = "decorated-" + service.Name
		return service
	}

	t.Run("basic decorator", func(t *testing.T) {
		option := AddDecorator(decorator)

		collection := NewCollection()
		err := option(collection)
		assert.NoError(t, err) // Current implementation returns nil
	})

	t.Run("decorator with options", func(t *testing.T) {
		option := AddDecorator(decorator, Name("primary-decorator"))

		collection := NewCollection()
		err := option(collection)
		assert.NoError(t, err)
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
		opt := As(new(ModuleTestInterface))
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

		opt := As(new(Interface1), new(Interface2))
		options := &addOptions{}
		opt.applyAddOption(options)
		assert.Len(t, options.As, 2)
	})

	t.Run("string representation", func(t *testing.T) {
		opt := As(new(ModuleTestInterface))
		str := opt.(fmt.Stringer).String()
		assert.Contains(t, str, "As(")
		assert.Contains(t, str, "ModuleTestInterface")
		assert.Contains(t, str, ")")
	})

	t.Run("multiple interfaces string", func(t *testing.T) {
		type Interface1 interface{ Method1() }
		type Interface2 interface{ Method2() }

		opt := As(new(Interface1), new(Interface2))
		str := opt.(fmt.Stringer).String()
		assert.Contains(t, str, "As(")
		assert.Contains(t, str, "Interface1")
		assert.Contains(t, str, "Interface2")
		assert.Contains(t, str, ", ")
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
		// Core module
		coreModule := NewModule("core",
			AddSingleton(func() *ModuleTestService { return &ModuleTestService{Name: "core"} }),
		)

		// Feature module depends on core
		featureModule := NewModule("feature",
			coreModule,
			AddScoped(func() *ModuleTestService { return &ModuleTestService{Name: "feature"} }),
		)

		// App module combines everything
		appModule := NewModule("app",
			featureModule,
			AddTransient(func() *ModuleTestService { return &ModuleTestService{Name: "app"} }),
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
			opt := As(new(ModuleTestInterface))
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
