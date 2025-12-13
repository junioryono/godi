package godi

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectionRegistration(t *testing.T) {
	t.Parallel()

	t.Run("registers_services_with_all_lifetimes", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()

		require.NoError(t, c.AddSingleton(NewTService))
		require.NoError(t, c.AddScoped(NewTDependency))
		require.NoError(t, c.AddTransient(NewTDisposable))

		assert.Equal(t, 3, c.Count())
		assert.True(t, c.Contains(PtrTypeOf[TService]()))
		assert.True(t, c.Contains(PtrTypeOf[TDependency]()))
		assert.True(t, c.Contains(PtrTypeOf[TDisposable]()))
	})

	t.Run("registers_keyed_services", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()

		require.NoError(t, c.AddSingleton(NewTServiceWithID("primary"), Name("primary")))
		require.NoError(t, c.AddSingleton(NewTServiceWithID("secondary"), Name("secondary")))

		assert.True(t, c.ContainsKeyed(PtrTypeOf[TService](), "primary"))
		assert.True(t, c.ContainsKeyed(PtrTypeOf[TService](), "secondary"))
		assert.False(t, c.Contains(PtrTypeOf[TService]())) // No default registration
	})

	t.Run("registers_grouped_services", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()

		require.NoError(t, c.AddSingleton(NewTServiceWithID("h1"), Group("handlers")))
		require.NoError(t, c.AddSingleton(NewTServiceWithID("h2"), Group("handlers")))

		p, err := c.Build()
		require.NoError(t, err)
		defer p.Close()

		services, err := p.GetGroup(PtrTypeOf[TService](), "handlers")
		require.NoError(t, err)
		assert.Len(t, services, 2)
	})

	t.Run("registers_interface_implementations", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()

		require.NoError(t, c.AddSingleton(NewTService, As[TInterface]()))
		assert.True(t, c.Contains(TypeOf[TInterface]()))

		p, err := c.Build()
		require.NoError(t, err)
		defer p.Close()

		svc, err := p.Get(TypeOf[TInterface]())
		require.NoError(t, err)
		assert.NotNil(t, svc)
	})

	t.Run("registers_non_function_as_instance", func(t *testing.T) {
		t.Parallel()
		instance := &TService{ID: "pre-built", Value: 999}
		c := NewCollection()

		require.NoError(t, c.AddSingleton(instance))

		p, err := c.Build()
		require.NoError(t, err)
		defer p.Close()

		svc, err := Resolve[*TService](p)
		require.NoError(t, err)
		assert.Same(t, instance, svc)
	})

	t.Run("registers_multi_return_constructor", func(t *testing.T) {
		t.Parallel()
		invocations := 0
		ctor := func() (*TService, *TDependency) {
			invocations++
			return &TService{ID: "multi"}, &TDependency{Name: "multi"}
		}

		c := NewCollection()
		require.NoError(t, c.AddSingleton(ctor))

		p, err := c.Build()
		require.NoError(t, err)
		defer p.Close()

		// Both types should be resolvable
		svc, _ := Resolve[*TService](p)
		dep, _ := Resolve[*TDependency](p)
		assert.Equal(t, "multi", svc.ID)
		assert.Equal(t, "multi", dep.Name)

		// Constructor should only be called once
		assert.Equal(t, 1, invocations)
	})
}

func TestCollectionRegistrationErrors(t *testing.T) {
	t.Parallel()

	t.Run("rejects_duplicate_registration", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		require.NoError(t, c.AddSingleton(NewTService))

		err := c.AddSingleton(NewTService)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})

	t.Run("rejects_nil_constructor", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		err := c.AddSingleton(nil)
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrConstructorNil)
	})

	t.Run("rejects_name_and_group_together", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		err := c.AddSingleton(NewTService, Name("n"), Group("g"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot use both")
	})

	t.Run("rejects_invalid_interface_binding", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()

		// Pointer to interface is invalid
		err := c.AddSingleton(NewTService, As[*TInterface]())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pointer to an interface")
	})
}

func TestCollectionRemove(t *testing.T) {
	t.Parallel()

	t.Run("removes_default_registration", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		require.NoError(t, c.AddSingleton(NewTService))
		require.NoError(t, c.AddSingleton(NewTService, Name("keyed")))

		c.Remove(PtrTypeOf[TService]())

		assert.False(t, c.Contains(PtrTypeOf[TService]()))
		assert.True(t, c.ContainsKeyed(PtrTypeOf[TService](), "keyed")) // Keyed preserved
	})

	t.Run("removes_keyed_registration", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		require.NoError(t, c.AddSingleton(NewTService, Name("k1")))
		require.NoError(t, c.AddSingleton(NewTService, Name("k2")))

		c.RemoveKeyed(PtrTypeOf[TService](), "k1")

		assert.False(t, c.ContainsKeyed(PtrTypeOf[TService](), "k1"))
		assert.True(t, c.ContainsKeyed(PtrTypeOf[TService](), "k2"))
	})
}

func TestCollectionModules(t *testing.T) {
	t.Parallel()

	t.Run("applies_module_registrations", func(t *testing.T) {
		t.Parallel()
		module := NewModule("test",
			AddSingleton(NewTService),
			AddScoped(NewTDependency),
		)

		c := NewCollection()
		require.NoError(t, c.AddModules(module))
		assert.Equal(t, 2, c.Count())
	})

	t.Run("applies_nested_modules", func(t *testing.T) {
		t.Parallel()
		inner := NewModule("inner", AddSingleton(NewTService))
		outer := NewModule("outer", inner, AddScoped(NewTDependency))

		c := NewCollection()
		require.NoError(t, c.AddModules(outer))
		assert.Equal(t, 2, c.Count())
	})

	t.Run("wraps_module_errors", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		require.NoError(t, c.AddSingleton(NewTService))

		module := NewModule("dup", AddSingleton(NewTService))
		err := c.AddModules(module)
		require.Error(t, err)

		var moduleErr ModuleError
		assert.ErrorAs(t, err, &moduleErr)
		assert.Equal(t, "dup", moduleErr.Module)
	})
}

func TestCollectionBuild(t *testing.T) {
	t.Parallel()

	t.Run("builds_provider_with_dependency_chain", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		require.NoError(t, c.AddSingleton(NewTService))
		require.NoError(t, c.AddSingleton(NewTDependency))
		require.NoError(t, c.AddSingleton(NewTServiceWithDeps))

		p, err := c.Build()
		require.NoError(t, err)
		defer p.Close()

		svc, err := Resolve[*TServiceWithDeps](p)
		require.NoError(t, err)
		assert.NotNil(t, svc.Svc)
		assert.NotNil(t, svc.Dep)
	})

	t.Run("detects_circular_dependencies", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		require.NoError(t, c.AddSingleton(NewTCircularA))
		require.NoError(t, c.AddSingleton(NewTCircularB))

		_, err := c.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circular dependency")
	})

	t.Run("detects_lifetime_violations", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		require.NoError(t, c.AddScoped(NewTService))
		require.NoError(t, c.AddSingleton(func(s *TService) *TDependency {
			return &TDependency{Name: s.ID}
		}))

		_, err := c.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "lifetime")
	})

	t.Run("respects_cancelled_context", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		require.NoError(t, c.AddSingleton(NewTService))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := c.BuildWithContext(ctx)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestCollectionParameterObjects(t *testing.T) {
	t.Parallel()

	type Params struct {
		In
		Service *TService
		Dep     *TDependency `optional:"true"`
	}

	c := NewCollection()
	require.NoError(t, c.AddSingleton(NewTService))
	require.NoError(t, c.AddSingleton(func(p Params) *TServiceWithDeps {
		return &TServiceWithDeps{Svc: p.Service, Dep: p.Dep}
	}))

	p, err := c.Build()
	require.NoError(t, err)
	defer p.Close()

	svc, err := Resolve[*TServiceWithDeps](p)
	require.NoError(t, err)
	assert.NotNil(t, svc.Svc)
	assert.Nil(t, svc.Dep) // Optional and not registered
}

func TestCollectionResultObjects(t *testing.T) {
	t.Parallel()

	// Use local types to avoid any potential shared type issues
	type ResultConfig struct{ Value string }
	type ResultLogger struct{ Level string }

	type ResultOut struct {
		Out
		Config *ResultConfig
		Logger *ResultLogger `name:"audit"`
	}

	c := NewCollection()
	require.NoError(t, c.AddSingleton(func() ResultOut {
		return ResultOut{
			Config: &ResultConfig{Value: "test-config"},
			Logger: &ResultLogger{Level: "info"},
		}
	}))

	// Verify registration
	assert.True(t, c.Contains(reflect.TypeOf((*ResultConfig)(nil))))
	assert.True(t, c.ContainsKeyed(reflect.TypeOf((*ResultLogger)(nil)), "audit"))

	p, err := c.Build()
	require.NoError(t, err)
	defer p.Close()

	cfg, err := p.Get(reflect.TypeOf((*ResultConfig)(nil)))
	require.NoError(t, err)
	assert.Equal(t, "test-config", cfg.(*ResultConfig).Value)

	logger, err := p.GetKeyed(reflect.TypeOf((*ResultLogger)(nil)), "audit")
	require.NoError(t, err)
	assert.Equal(t, "info", logger.(*ResultLogger).Level)
}
