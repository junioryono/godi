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
			require.NoError(t, module(c), "Add errors are recorded, not returned")
			err := c.Err()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "already registered")
			assert.Contains(t, err.Error(), `module "dup"`)
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
			assert.True(t, c.Contains(reflect.TypeFor[*TService]()))
			assert.True(t, c.ContainsKeyed(reflect.TypeFor[*TService](), "scoped"))
			assert.True(t, c.ContainsKeyed(reflect.TypeFor[*TService](), "transient"))
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
			assert.True(t, cl.(*collection).HasGroup(reflect.TypeFor[*TService](), "handlers"))
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
			c.AddSingleton(NewTService)

			module := NewModule("dup", AddSingleton(NewTService))
			require.NoError(t, module(c), "Add errors are recorded, not returned")
			err := c.Err()
			require.Error(t, err)
			var moduleErr *ModuleError
			assert.ErrorAs(t, err, &moduleErr)
			assert.Equal(t, "dup", moduleErr.Module)
		})

		t.Run("nil_builder_skipped", func(t *testing.T) {
			t.Parallel()
			var nilOption ModuleOption
			module := NewModule("nil", nil, AddSingleton(NewTService), nilOption)
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
				assert.True(t, c.Contains(reflect.TypeFor[*TService]()))
			})
		}

		t.Run("with_name", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			require.NoError(t, AddSingleton(NewTService, Name("primary"))(c))
			assert.True(t, c.ContainsKeyed(reflect.TypeFor[*TService](), "primary"))
		})

		t.Run("with_group", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			require.NoError(t, AddSingleton(NewTService, Group("services"))(c))
			assert.True(t, c.(*collection).HasGroup(reflect.TypeFor[*TService](), "services"))
		})

		t.Run("with_As", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			require.NoError(t, AddSingleton(NewTService, As[TInterface]())(c))
			assert.True(t, c.Contains(reflect.TypeFor[TInterface]()))
		})
	})

	t.Run("Remove", func(t *testing.T) {
		t.Parallel()

		t.Run("removes_service", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			c.AddSingleton(NewTService)
			assert.True(t, c.Contains(reflect.TypeFor[*TService]()))

			c.AddModules(Remove[*TService]())
			assert.False(t, c.Contains(reflect.TypeFor[*TService]()))
		})

		t.Run("removes_interface", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			c.AddSingleton(NewTService, As[TInterface]())
			assert.True(t, c.Contains(reflect.TypeFor[TInterface]()))

			c.AddModules(Remove[TInterface]())
			assert.False(t, c.Contains(reflect.TypeFor[TInterface]()))
		})

		t.Run("remove_and_replace", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			c.AddSingleton(NewTServiceWithID("original"))

			c.AddModules(
				Remove[*TService](),
				AddSingleton(NewTServiceWithID("replacement")),
			)
			assert.True(t, c.Contains(reflect.TypeFor[*TService]()))
		})

		t.Run("non_existent_is_noop", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			c.AddModules(Remove[*TService]())
			assert.Equal(t, 0, c.Count())
		})
	})

	t.Run("RemoveKeyed", func(t *testing.T) {
		t.Parallel()

		t.Run("removes_keyed", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			c.AddSingleton(NewTService, Name("primary"))
			c.AddSingleton(NewTService, Name("secondary"))

			c.AddModules(RemoveKeyed[*TService]("primary"))
			assert.False(t, c.ContainsKeyed(reflect.TypeFor[*TService](), "primary"))
			assert.True(t, c.ContainsKeyed(reflect.TypeFor[*TService](), "secondary"))
		})

		t.Run("nil_key_removes_default", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			c.AddSingleton(NewTService)
			assert.True(t, c.Contains(reflect.TypeFor[*TService]()))

			c.AddModules(RemoveKeyed[*TService](nil))
			assert.False(t, c.Contains(reflect.TypeFor[*TService]()))
		})

		t.Run("non_existent_is_noop", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			c.AddSingleton(NewTService, Name("existing"))

			c.AddModules(RemoveKeyed[*TService]("nonexistent"))
			assert.True(t, c.ContainsKeyed(reflect.TypeFor[*TService](), "existing"))
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
			c.AddModules(module)
			assert.Equal(t, 3, c.Count())
		})

		t.Run("error_propagation", func(t *testing.T) {
			t.Parallel()
			failing := func(c Collection) error { return errors.New("intentional") }
			module := NewModule("failing", ModuleOption(failing))

			c := NewCollection()
			c.AddModules(module)
			err := c.Err()
			require.Error(t, err)
			var moduleErr *ModuleError
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
			c.AddModules(module)
			assert.Equal(t, 3, c.Count())
			assert.True(t, c.Contains(reflect.TypeFor[*Logger]()))
			assert.True(t, c.ContainsKeyed(reflect.TypeFor[*Logger](), "debug"))
			assert.True(t, c.ContainsKeyed(reflect.TypeFor[*Logger](), "audit"))
		})
	})
}

func TestModuleDeduplication(t *testing.T) {
	t.Parallel()

	logging := NewModule("logging", AddSingleton(NewTService))
	users := NewModule("users", logging, AddScoped(NewTDependency))
	orders := NewModule("orders", logging, AddScoped(NewTScoped))

	// Both feature modules include the shared logging module: a diamond.
	// Applying the same module value twice used to fail with
	// AlreadyRegisteredError.
	c := NewCollection()
	c.AddModules(users, orders)
	require.NoError(t, c.Err())
	assert.Equal(t, 3, c.Count())

	p, err := c.Build()
	require.NoError(t, err)
	require.NoError(t, p.Close())
}

func TestReplace(t *testing.T) {
	t.Parallel()

	t.Run("replaces_the_registration", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTServiceWithID("real"))
		c.AddModules(ReplaceSingleton(NewTServiceWithID("fake")))
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		svc, err := Resolve[*TService](p)
		require.NoError(t, err)
		assert.Equal(t, "fake", svc.ID)
	})

	t.Run("replacement_constructor_runs_instead_of_the_original", func(t *testing.T) {
		t.Parallel()
		originalRan := false
		c := NewCollection()
		c.AddSingleton(func() *TService { originalRan = true; return NewTService() })
		c.AddModules(ReplaceScoped(NewTServiceWithID("fake")))
		p, err := c.Build()
		require.NoError(t, err)
		require.NoError(t, p.Close())
		assert.False(t, originalRan)
	})

	t.Run("keyed", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTServiceWithID("primary"), Name("db"))
		c.AddSingleton(NewTServiceWithID("other"))
		c.AddModules(ReplaceSingleton(NewTServiceWithID("mock"), Name("db")))
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		keyed, err := ResolveKeyed[*TService](p, "db")
		require.NoError(t, err)
		assert.Equal(t, "mock", keyed.ID)
		plain, err := Resolve[*TService](p)
		require.NoError(t, err)
		assert.Equal(t, "other", plain.ID, "only the matching key is replaced")
	})

	t.Run("rejects_constructors_without_a_service", func(t *testing.T) {
		t.Parallel()
		// A void constructor's key is generated per registration, so it
		// could never match anything.
		c := NewCollection()
		c.AddSingleton(func() {})
		c.AddModules(ReplaceSingleton(func() {}), TryAddSingleton(func() {}))
		err := c.Err()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "no service")
	})

	t.Run("nothing_to_replace_is_an_error", func(t *testing.T) {
		t.Parallel()
		// Unlike Remove, a Replace that matches nothing (e.g. ordered before
		// the original registration) is reported instead of being a no-op.
		c := NewCollection()
		c.AddModules(ReplaceSingleton(NewTService))
		err := c.Err()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "nothing to replace")
	})
}

func TestTryAdd(t *testing.T) {
	t.Parallel()

	t.Run("adds_when_absent", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddModules(TryAddSingleton(NewTService))
		assert.True(t, c.Contains(reflect.TypeFor[*TService]()))
	})

	t.Run("keeps_the_existing_registration", func(t *testing.T) {
		t.Parallel()
		// A library registers a default that the application may already
		// have provided.
		c := NewCollection()
		c.AddSingleton(NewTServiceWithID("app"))
		c.AddModules(TryAddSingleton(NewTServiceWithID("library-default")))
		require.NoError(t, c.Err())

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		svc, err := Resolve[*TService](p)
		require.NoError(t, err)
		assert.Equal(t, "app", svc.ID)
	})

	t.Run("keyed", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTServiceWithID("a"), Name("a"))
		c.AddModules(
			TryAddSingleton(NewTServiceWithID("ignored"), Name("a")),
			TryAddSingleton(NewTServiceWithID("b"), Name("b")),
		)
		require.NoError(t, c.Err())
		assert.True(t, c.ContainsKeyed(reflect.TypeFor[*TService](), "b"))
		assert.Equal(t, 2, c.Count())
	})
}
