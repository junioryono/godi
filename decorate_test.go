package godi

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

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

// decorateNeedingService is a decorator with a dependency.
func decorateNeedingService(g greeter, _ *TService) greeter { return g }

// plainGreeter is a decorator that is not disposable.
type plainGreeter struct{ inner greeter }

func (g plainGreeter) Greet() string { return g.inner.Greet() }

// closingGreeter is a disposable decorator that closes the value it wraps.
type closingGreeter struct {
	TDisposable
	inner greeter
}

func (g *closingGreeter) Greet() string { return g.inner.Greet() }

func (g *closingGreeter) Close() error {
	if err := g.TDisposable.Close(); err != nil {
		return err
	}
	if c, ok := g.inner.(io.Closer); ok {
		return c.Close()
	}
	return nil
}

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

	t.Run("a_non_disposable_decorator_leaves_the_wrapped_value_to_godi", func(t *testing.T) {
		t.Parallel()
		base := &baseGreeter{}
		c := NewCollection()
		c.AddSingleton(func() greeter { return base })
		c.AddModules(Decorate(func(g greeter) greeter { return plainGreeter{inner: g} }))
		p, err := c.Build()
		require.NoError(t, err)
		require.NoError(t, p.Close())
		assert.True(t, base.IsClosed(), "the wrapped value is still disposed")
	})

	t.Run("decorator_resolving_its_own_service_reports_a_cycle", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddScoped(func() greeter { return &baseGreeter{} })
		c.AddModules(Decorate(func(g greeter, s Resolver) (greeter, error) {
			_, err := Resolve[greeter](s)
			return g, err
		}))
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = scope.Close() })

		done := make(chan error, 1)
		go func() {
			_, err := Resolve[greeter](scope)
			done <- err
		}()
		select {
		case err := <-done:
			var cycle *CircularDependencyError
			require.ErrorAs(t, err, &cycle)
		case <-time.After(5 * time.Second):
			t.Fatal("deadlocked: the decorator's Scope was not part of the construction")
		}
	})

	t.Run("decorator_depending_on_a_sibling_output_is_rejected", func(t *testing.T) {
		t.Parallel()
		type First struct{}
		for _, lifetime := range []Lifetime{Singleton, Scoped} {
			t.Run(lifetime.String(), func(t *testing.T) {
				t.Parallel()
				c := NewCollection()
				ctor := func() (*First, greeter) { return &First{}, &baseGreeter{} }
				if lifetime == Singleton {
					c.AddSingleton(ctor)
				} else {
					c.AddScoped(ctor)
				}
				// Decorating greeter needs *First, which the same
				// constructor call is still producing: it could never be
				// resolved (Build used to deadlock on it).
				c.AddModules(Decorate(func(g greeter, _ *First) greeter { return g }))
				require.Error(t, Validate(c))
				_, err := c.Build()
				require.Error(t, err)
				assert.Contains(t, err.Error(), "same constructor")
			})
		}
	})

	t.Run("indirect_dependency_on_a_sibling_output_is_rejected", func(t *testing.T) {
		t.Parallel()
		type First struct{}
		type Middle struct{}
		c := NewCollection()
		c.AddScoped(func() (*First, greeter) { return &First{}, &baseGreeter{} })
		c.AddScoped(func(*First) *Middle { return &Middle{} })
		// greeter's decorator needs *Middle, which needs *First: the same
		// construction again.
		c.AddModules(Decorate(func(g greeter, _ *Middle) greeter { return g }))
		err := Validate(c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "same constructor")
	})

	t.Run("dependency_on_a_sibling_output_through_another_decorator_is_rejected", func(t *testing.T) {
		t.Parallel()
		type First struct{}
		type Middle struct{}
		c := NewCollection()
		c.AddSingleton(func() (*First, greeter) { return &First{}, &baseGreeter{} })
		c.AddSingleton(func() *Middle { return &Middle{} })
		// greeter's decorator needs *Middle, whose own decorator (registered
		// later) needs *First: the same construction again.
		c.AddModules(
			Decorate(func(g greeter, _ *Middle) greeter { return g }),
			Decorate(func(m *Middle, _ *First) *Middle { return m }),
		)
		err := Validate(c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "same constructor")
		_, err = c.Build()
		assert.ErrorContains(t, err, "same constructor")
	})

	t.Run("dependency_on_a_sibling_output_through_another_constructions_decorator_is_rejected", func(t *testing.T) {
		t.Parallel()
		type First struct{}
		type Other struct{}
		type OtherSibling struct{}
		c := NewCollection()
		c.AddScoped(func() (*First, greeter) { return &First{}, &baseGreeter{} })
		c.AddScoped(func() (*Other, *OtherSibling) { return &Other{}, &OtherSibling{} })
		// greeter's decorator needs *Other; constructing *Other also
		// decorates *OtherSibling, whose decorator needs *First.
		c.AddModules(
			Decorate(func(g greeter, _ *Other) greeter { return g }),
			Decorate(func(o *OtherSibling, _ *First) *OtherSibling { return o }),
		)
		err := Validate(c)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "same constructor")
	})

	t.Run("missing_decorator_dependency_names_the_decorator", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func() greeter { return &baseGreeter{} })
		c.AddModules(Decorate(decorateNeedingService))
		_, err := c.Build()
		var missing *MissingDependencyError
		require.ErrorAs(t, err, &missing)
		assert.Contains(t, missing.Constructor, "decorateNeedingService")
	})

	t.Run("a_disposable_decorator_owns_what_it_wraps", func(t *testing.T) {
		t.Parallel()
		base := &baseGreeter{}
		var outer, inner *closingGreeter
		c := NewCollection()
		c.AddSingleton(func() greeter { return base })
		c.AddModules(
			Decorate(func(g greeter) greeter { inner = &closingGreeter{inner: g}; return inner }),
			Decorate(func(g greeter) greeter { outer = &closingGreeter{inner: g}; return outer }),
		)
		p, err := c.Build()
		require.NoError(t, err)

		// Each layer closes the value it wraps; godi closes only the
		// outermost disposable layer, so each layer is closed exactly once
		// (TDisposable fails on a second Close).
		require.NoError(t, p.Close())
		assert.True(t, outer.IsClosed())
		assert.True(t, inner.IsClosed(), "the middle layer used to be dropped")
		assert.True(t, base.IsClosed())
	})

	t.Run("a_disposable_decorator_of_an_alias_owns_the_shared_value", func(t *testing.T) {
		t.Parallel()
		aliasOrders := map[string][]AddOption{
			"decorated_alias_first": {As[greeter](), As[io.Closer]()},
			"bare_alias_first":      {As[io.Closer](), As[greeter]()},
		}
		for _, lifetime := range []Lifetime{Singleton, Scoped} {
			for order, aliases := range aliasOrders {
				t.Run(lifetime.String()+"/"+order, func(t *testing.T) {
					t.Parallel()
					base := &baseGreeter{}
					c := NewCollection()
					ctor := func() *baseGreeter { return base }
					if lifetime == Singleton {
						c.AddSingleton(ctor, aliases...)
					} else {
						c.AddScoped(ctor, aliases...)
					}
					// Only the greeter alias is decorated; the io.Closer alias
					// is the same value published bare, but the wrapper owns it.
					c.AddModules(Decorate(func(g greeter) greeter { return &closingGreeter{inner: g} }))
					p, err := c.Build()
					require.NoError(t, err)
					scope, err := p.CreateScope(context.Background())
					require.NoError(t, err)

					got, err := Resolve[io.Closer](scope)
					require.NoError(t, err)
					assert.Same(t, base, got)
					wrapper, err := Resolve[greeter](scope)
					require.NoError(t, err)

					// TDisposable fails on a second Close.
					require.NoError(t, scope.Close())
					require.NoError(t, p.Close())
					assert.True(t, wrapper.(*closingGreeter).IsClosed())
					assert.True(t, base.IsClosed())
				})
			}
		}
	})

	t.Run("a_failing_decorator_releases_every_produced_output", func(t *testing.T) {
		t.Parallel()
		var first *TDisposable
		c := NewCollection()
		c.AddScoped(func() (*TDisposable, greeter) {
			first = NewTDisposable()
			return first, &baseGreeter{}
		})
		c.AddModules(Decorate(func(g greeter) (greeter, error) { return nil, errors.New("boom") }))
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)

		_, err = Resolve[greeter](scope)
		require.Error(t, err)
		require.NoError(t, scope.Close())
		require.NotNil(t, first)
		assert.True(t, first.IsClosed(), "the undecorated sibling must not leak")
	})

	t.Run("a_failing_decorator_dependency_releases_every_produced_output", func(t *testing.T) {
		t.Parallel()
		type failing struct{}
		var first *TDisposable
		var base *baseGreeter
		c := NewCollection()
		c.AddScoped(func() (*TDisposable, greeter) {
			first, base = NewTDisposable(), &baseGreeter{}
			return first, base
		})
		c.AddScoped(func() (*failing, error) { return nil, errors.New("dependency failed") })
		c.AddModules(Decorate(func(g greeter, _ *failing) greeter { return g }))
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)

		// The construction publishes all of its outputs or none.
		_, err = Resolve[*TDisposable](scope)
		require.Error(t, err)
		require.NoError(t, scope.Close())
		require.NotNil(t, first)
		assert.True(t, first.IsClosed(), "the undecorated sibling must not leak")
		assert.True(t, base.IsClosed())
	})

	t.Run("an_instance_decorators_transient_dependencies_are_owned_with_it", func(t *testing.T) {
		t.Parallel()
		base := &baseGreeter{}
		var dep *TDisposable
		c := NewCollection()
		c.AddSingleton(base)
		c.AddTransient(func() *TDisposable { dep = NewTDisposable(); return dep })
		// Like a constructor's, the decorator's transient dependency belongs
		// to the singleton it decorates.
		c.AddModules(Decorate(func(g *baseGreeter, _ *TDisposable) *baseGreeter { return g }))
		p, err := c.Build()
		require.NoError(t, err)

		require.NoError(t, p.Close())
		assert.False(t, base.IsClosed(), "the caller owns an instance registration")
		require.NotNil(t, dep)
		assert.True(t, dep.IsClosed())
	})

	t.Run("lifecycle_hooks_reach_the_decorated_service", func(t *testing.T) {
		t.Parallel()
		rec := &startRecorder{}
		c := NewCollection()
		c.AddSingleton(func() *startableB { return &startableB{rec: rec} })
		c.AddModules(Decorate(func(s *startableB) *startableB { return &startableB{rec: &startRecorder{}} }))
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		// Start and HealthCheck use the constructed service, not the
		// decorators' results.
		require.NoError(t, Start(context.Background(), p))
		assert.Equal(t, []string{"B"}, rec.order)
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
