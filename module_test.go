package godi

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModule(t *testing.T) {
	t.Parallel()

	t.Run("NewModule", func(t *testing.T) {
		t.Parallel()

		t.Run("empty", func(t *testing.T) {
			t.Parallel()
			module := NewModule("empty")
			c := NewCollection()
			require.NoError(t, module(c))
			assert.Equal(t, 0, c.Count())
		})

		t.Run("single_service", func(t *testing.T) {
			t.Parallel()
			module := NewModule("single", AddSingleton(NewTService))
			c := NewCollection()
			require.NoError(t, module(c))
			assert.Equal(t, 1, c.Count())
		})

		t.Run("duplicate_type_error", func(t *testing.T) {
			t.Parallel()
			module := NewModule("dup",
				AddSingleton(NewTService),
				AddScoped(NewTService),
			)
			c := NewCollection()
			err := module(c)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "already registered")
		})

		t.Run("keyed_services", func(t *testing.T) {
			t.Parallel()
			module := NewModule("keyed",
				AddSingleton(NewTService),
				AddScoped(NewTService, Name("scoped")),
				AddTransient(NewTService, Name("transient")),
			)
			c := NewCollection()
			require.NoError(t, module(c))
			assert.Equal(t, 3, c.Count())
			assert.True(t, c.Contains(PtrTypeOf[TService]()))
			assert.True(t, c.ContainsKeyed(PtrTypeOf[TService](), "scoped"))
			assert.True(t, c.ContainsKeyed(PtrTypeOf[TService](), "transient"))
		})

		t.Run("grouped_services", func(t *testing.T) {
			t.Parallel()
			module := NewModule("grouped",
				AddTransient(NewTServiceWithID("h1"), Group("handlers")),
				AddTransient(NewTServiceWithID("h2"), Group("handlers")),
				AddTransient(NewTServiceWithID("h3"), Group("handlers")),
			)
			cl := NewCollection()
			require.NoError(t, module(cl))
			assert.Equal(t, 3, cl.Count())
			assert.True(t, cl.(*collection).HasGroup(PtrTypeOf[TService](), "handlers"))
		})

		t.Run("nested", func(t *testing.T) {
			t.Parallel()
			type (
				Inner  struct{ Name string }
				Middle struct{ Name string }
				Outer  struct{ Name string }
			)
			inner := NewModule("inner", AddSingleton(func() *Inner { return &Inner{Name: "inner"} }))
			middle := NewModule("middle", inner, AddScoped(func() *Middle { return &Middle{Name: "middle"} }))
			outer := NewModule("outer", middle, AddTransient(func() *Outer { return &Outer{Name: "outer"} }))

			c := NewCollection()
			require.NoError(t, outer(c))
			assert.Equal(t, 3, c.Count())
		})

		t.Run("error_from_duplicate", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			require.NoError(t, c.AddSingleton(NewTService))

			module := NewModule("dup", AddSingleton(NewTService))
			err := module(c)
			require.Error(t, err)
			var moduleErr ModuleError
			assert.ErrorAs(t, err, &moduleErr)
			assert.Equal(t, "dup", moduleErr.Module)
		})

		t.Run("nil_builder_skipped", func(t *testing.T) {
			t.Parallel()
			module := NewModule("nil", nil, AddSingleton(NewTService), nil)
			c := NewCollection()
			require.NoError(t, module(c))
			assert.Equal(t, 1, c.Count())
		})
	})

	t.Run("AddLifetimes", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name string
			add  ModuleOption
		}{
			{"singleton", AddSingleton(NewTService)},
			{"scoped", AddScoped(NewTService)},
			{"transient", AddTransient(NewTService)},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				c := NewCollection()
				require.NoError(t, tc.add(c))
				assert.Equal(t, 1, c.Count())
				assert.True(t, c.Contains(PtrTypeOf[TService]()))
			})
		}

		t.Run("with_name", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			require.NoError(t, AddSingleton(NewTService, Name("primary"))(c))
			assert.True(t, c.ContainsKeyed(PtrTypeOf[TService](), "primary"))
		})

		t.Run("with_group", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			require.NoError(t, AddSingleton(NewTService, Group("services"))(c))
			assert.True(t, c.(*collection).HasGroup(PtrTypeOf[TService](), "services"))
		})

		t.Run("with_As", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			require.NoError(t, AddSingleton(NewTService, As[TInterface]())(c))
			assert.True(t, c.Contains(TypeOf[TInterface]()))
		})
	})

	t.Run("Remove", func(t *testing.T) {
		t.Parallel()

		t.Run("removes_service", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			require.NoError(t, c.AddSingleton(NewTService))
			assert.True(t, c.Contains(PtrTypeOf[TService]()))

			require.NoError(t, c.AddModules(Remove[*TService]()))
			assert.False(t, c.Contains(PtrTypeOf[TService]()))
		})

		t.Run("removes_interface", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			require.NoError(t, c.AddSingleton(NewTService, As[TInterface]()))
			assert.True(t, c.Contains(TypeOf[TInterface]()))

			require.NoError(t, c.AddModules(Remove[TInterface]()))
			assert.False(t, c.Contains(TypeOf[TInterface]()))
		})

		t.Run("remove_and_replace", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			require.NoError(t, c.AddSingleton(NewTServiceWithID("original")))

			require.NoError(t, c.AddModules(
				Remove[*TService](),
				AddSingleton(NewTServiceWithID("replacement")),
			))
			assert.True(t, c.Contains(PtrTypeOf[TService]()))
		})

		t.Run("non_existent_is_noop", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			require.NoError(t, c.AddModules(Remove[*TService]()))
			assert.Equal(t, 0, c.Count())
		})
	})

	t.Run("RemoveKeyed", func(t *testing.T) {
		t.Parallel()

		t.Run("removes_keyed", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			require.NoError(t, c.AddSingleton(NewTService, Name("primary")))
			require.NoError(t, c.AddSingleton(NewTService, Name("secondary")))

			require.NoError(t, c.AddModules(RemoveKeyed[*TService]("primary")))
			assert.False(t, c.ContainsKeyed(PtrTypeOf[TService](), "primary"))
			assert.True(t, c.ContainsKeyed(PtrTypeOf[TService](), "secondary"))
		})

		t.Run("nil_key_removes_default", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			require.NoError(t, c.AddSingleton(NewTService))
			assert.True(t, c.Contains(PtrTypeOf[TService]()))

			require.NoError(t, c.AddModules(RemoveKeyed[*TService](nil)))
			assert.False(t, c.Contains(PtrTypeOf[TService]()))
		})

		t.Run("non_existent_is_noop", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			require.NoError(t, c.AddSingleton(NewTService, Name("existing")))

			require.NoError(t, c.AddModules(RemoveKeyed[*TService]("nonexistent")))
			assert.True(t, c.ContainsKeyed(PtrTypeOf[TService](), "existing"))
			assert.Equal(t, 1, c.Count())
		})
	})

	t.Run("Options", func(t *testing.T) {
		t.Parallel()

		t.Run("Name", func(t *testing.T) {
			t.Parallel()
			opt := Name("test")
			opts := &addOptions{}
			opt.applyAddOption(opts)
			assert.Equal(t, "test", opts.Name)
			assert.Equal(t, `Name("test")`, opt.(fmt.Stringer).String())
		})

		t.Run("Group", func(t *testing.T) {
			t.Parallel()
			opt := Group("handlers")
			opts := &addOptions{}
			opt.applyAddOption(opts)
			assert.Equal(t, "handlers", opts.Group)
			assert.Equal(t, `Group("handlers")`, opt.(fmt.Stringer).String())
		})

		t.Run("As", func(t *testing.T) {
			t.Parallel()
			opt := As[TInterface]()
			opts := &addOptions{}
			opt.applyAddOption(opts)
			assert.Len(t, opts.As, 1)
			assert.Contains(t, opt.(fmt.Stringer).String(), "TInterface")
		})
	})

	t.Run("OptionsValidate", func(t *testing.T) {
		t.Parallel()

		cases := []struct {
			name    string
			opts    *addOptions
			wantErr string
		}{
			{"valid", &addOptions{Name: "test"}, ""},
			{"name_and_group", &addOptions{Name: "n", Group: "g"}, "cannot use both"},
			{"name_backtick", &addOptions{Name: "n`ame"}, "backquotes"},
			{"group_backtick", &addOptions{Group: "g`roup"}, "backquotes"},
			{"nil_As", &addOptions{As: []any{nil}}, "invalid"},
			{"non_pointer_As", &addOptions{As: []any{TInterface(nil)}}, "pointer to an interface"},
			{"non_interface_As", &addOptions{As: []any{&TService{}}}, "pointer to an interface"},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				t.Parallel()
				err := tc.opts.Validate()
				if tc.wantErr == "" {
					assert.NoError(t, err)
				} else {
					require.Error(t, err)
					assert.Contains(t, err.Error(), tc.wantErr)
				}
			})
		}
	})

	t.Run("ComplexPatterns", func(t *testing.T) {
		t.Parallel()

		t.Run("database_module", func(t *testing.T) {
			t.Parallel()
			type (
				DB       struct{ Conn string }
				UserRepo struct{ DB *DB }
				PostRepo struct{ DB *DB }
			)

			module := NewModule("db",
				AddSingleton(func() *DB { return &DB{Conn: "test"} }),
				AddScoped(func(db *DB) *UserRepo { return &UserRepo{DB: db} }),
				AddScoped(func(db *DB) *PostRepo { return &PostRepo{DB: db} }),
			)

			c := NewCollection()
			require.NoError(t, c.AddModules(module))
			assert.Equal(t, 3, c.Count())
		})

		t.Run("error_propagation", func(t *testing.T) {
			t.Parallel()
			failing := func(c Collection) error { return errors.New("intentional") }
			module := NewModule("failing", ModuleOption(failing))

			c := NewCollection()
			err := c.AddModules(module)
			require.Error(t, err)
			var moduleErr ModuleError
			assert.ErrorAs(t, err, &moduleErr)
			assert.Equal(t, "failing", moduleErr.Module)
			assert.Contains(t, moduleErr.Cause.Error(), "intentional")
		})

		t.Run("multi_keyed_loggers", func(t *testing.T) {
			t.Parallel()
			type Logger struct{ Name string }

			module := NewModule("logging",
				AddSingleton(func() *Logger { return &Logger{Name: "default"} }),
				AddSingleton(func() *Logger { return &Logger{Name: "debug"} }, Name("debug")),
				AddSingleton(func() *Logger { return &Logger{Name: "audit"} }, Name("audit")),
			)

			c := NewCollection()
			require.NoError(t, c.AddModules(module))
			assert.Equal(t, 3, c.Count())
			assert.True(t, c.Contains(reflect.TypeOf((*Logger)(nil))))
			assert.True(t, c.ContainsKeyed(reflect.TypeOf((*Logger)(nil)), "debug"))
			assert.True(t, c.ContainsKeyed(reflect.TypeOf((*Logger)(nil)), "audit"))
		})
	})
}
