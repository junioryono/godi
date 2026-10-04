package godi

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type greeter interface{ Greet() string }

type baseGreeter struct{ TDisposable }

func (*baseGreeter) Greet() string { return "hello" }

// wrapGreeter decorates a greeter, adding a suffix.
type wrapGreeter struct {
	TDisposable
	inner  greeter
	suffix string
}

func (w *wrapGreeter) Greet() string { return w.inner.Greet() + w.suffix }

func TestDecorate(t *testing.T) {
	t.Parallel()

	t.Run("wraps_the_service", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func() greeter { return &baseGreeter{} })
		c.AddModules(Decorate(func(g greeter) greeter { return &wrapGreeter{inner: g, suffix: "!"} }))
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		g, err := Resolve[greeter](p)
		require.NoError(t, err)
		assert.Equal(t, "hello!", g.Greet())
	})

	t.Run("decorators_apply_in_registration_order", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func() greeter { return &baseGreeter{} })
		c.AddModules(
			Decorate(func(g greeter) greeter { return &wrapGreeter{inner: g, suffix: " 1"} }),
			Decorate(func(g greeter) greeter { return &wrapGreeter{inner: g, suffix: " 2"} }),
		)
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		g, err := Resolve[greeter](p)
		require.NoError(t, err)
		assert.Equal(t, "hello 1 2", g.Greet(), "the first decorator is innermost")
	})

	t.Run("decorator_dependencies_are_injected_and_validated", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func() greeter { return &baseGreeter{} })
		c.AddModules(Decorate(func(g greeter, svc *TService) greeter {
			return &wrapGreeter{inner: g, suffix: " " + svc.ID}
		}))
		_, err := c.Build()
		require.ErrorIs(t, err, ErrServiceNotFound, "a decorator's dependencies are checked at Build")

		c.AddSingleton(NewTServiceWithID("from-dep"))
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		g, err := Resolve[greeter](p)
		require.NoError(t, err)
		assert.Equal(t, "hello from-dep", g.Greet())
	})

	t.Run("keyed_and_lifetime_preserving", func(t *testing.T) {
		t.Parallel()
		var built int
		c := NewCollection()
		c.AddScoped(func() greeter { return &baseGreeter{} }, Name("loud"))
		c.AddScoped(func() greeter { return &baseGreeter{} })
		c.AddModules(Decorate(func(g greeter) greeter {
			built++
			return &wrapGreeter{inner: g, suffix: "!"}
		}, Name("loud")))
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = scope.Close() })

		loud1, err := ResolveKeyed[greeter](scope, "loud")
		require.NoError(t, err)
		loud2, err := ResolveKeyed[greeter](scope, "loud")
		require.NoError(t, err)
		plain, err := Resolve[greeter](scope)
		require.NoError(t, err)
		assert.Equal(t, "hello!", loud1.Greet())
		assert.Same(t, loud1, loud2, "a decorated scoped service is still one per scope")
		assert.Equal(t, 1, built)
		assert.Equal(t, "hello", plain.Greet(), "only the matching key is decorated")
	})

	t.Run("group_members", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func() greeter { return &baseGreeter{} }, Group("greeters"))
		c.AddSingleton(func() greeter { return &baseGreeter{} }, Group("greeters"))
		c.AddModules(Decorate(func(g greeter) greeter { return &wrapGreeter{inner: g, suffix: "?"} }, Group("greeters")))
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		all, err := ResolveGroup[greeter](p, "greeters")
		require.NoError(t, err)
		require.Len(t, all, 2)
		for _, g := range all {
			assert.Equal(t, "hello?", g.Greet())
		}
	})

	t.Run("decorator_error_fails_resolution", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("decorator failed")
		c := NewCollection()
		c.AddScoped(func() greeter { return &baseGreeter{} })
		c.AddModules(Decorate(func(g greeter) (greeter, error) { return nil, boom }))
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = scope.Close() })

		_, err = Resolve[greeter](scope)
		require.ErrorIs(t, err, boom)
	})

	t.Run("wrapper_disposed_before_the_wrapped_value", func(t *testing.T) {
		t.Parallel()
		base := &baseGreeter{}
		var wrapper *wrapGreeter
		c := NewCollection()
		c.AddSingleton(func() greeter { return base })
		c.AddModules(Decorate(func(g greeter) greeter {
			wrapper = &wrapGreeter{inner: g}
			return wrapper
		}))
		p, err := c.Build()
		require.NoError(t, err)
		require.NoError(t, p.Close())
		assert.True(t, base.IsClosed(), "the undecorated value is still disposed")
		assert.True(t, wrapper.IsClosed())
	})

	t.Run("no_matching_registration_is_an_error", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddModules(Decorate(func(g greeter) greeter { return g }))
		_, err := c.Build()
		require.Error(t, err)
		assert.True(t, strings.Contains(err.Error(), "decorate"), err.Error())
	})

	t.Run("rejects_invalid_decorators", func(t *testing.T) {
		t.Parallel()
		for name, fn := range map[string]any{
			"not_a_function":       42,
			"no_parameters":        func() greeter { return nil },
			"returns_another_type": func(g greeter) *TService { return nil },
		} {
			c := NewCollection()
			c.AddModules(Decorate(fn))
			assert.Error(t, c.Err(), name)
		}
	})
}
