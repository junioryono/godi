package godi

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolutionHelpers(t *testing.T) {
	t.Parallel()

	t.Run("must_resolve_panics_with_the_error", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t)
		defer func() {
			// The panic value used to be a formatted string, losing the
			// error for errors.Is/As in recover handlers.
			r := recover()
			err, ok := r.(error)
			require.True(t, ok, "panic value is %T", r)
			assert.ErrorIs(t, err, ErrServiceNotFound)
		}()
		MustResolve[*TService](p)
	})

	t.Run("resolve_from_context", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t, AddScoped(NewTService))
		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = scope.Close() })

		svc, err := ResolveFromContext[*TService](scope.Context())
		require.NoError(t, err)
		again, err := Resolve[*TService](scope)
		require.NoError(t, err)
		assert.Same(t, again, svc)

		_, err = ResolveFromContext[*TService](context.Background())
		require.Error(t, err, "no scope in the context")
	})

	t.Run("helpers_accept_any_resolver", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t, AddSingleton(NewTService))
		// A narrow Resolver (e.g. a test double) is enough; the generic
		// helpers no longer require a full Provider.
		var r Resolver = resolverOnly{p}
		svc, err := Resolve[*TService](r)
		require.NoError(t, err)
		assert.NotNil(t, svc)
	})
}

func TestInvoke(t *testing.T) {
	t.Parallel()

	p := BuildProvider(t, AddSingleton(NewTService), AddSingleton(NewTDependency))

	t.Run("resolves_parameters_and_calls", func(t *testing.T) {
		t.Parallel()
		var got *TService
		err := Invoke(p, func(s *TService, d *TDependency) {
			got = s
		})
		require.NoError(t, err)
		assert.NotNil(t, got)
	})

	t.Run("parameter_object", func(t *testing.T) {
		t.Parallel()
		type Params struct {
			In
			Service *TService
			Missing *TScoped `optional:"true"`
		}
		var got Params
		require.NoError(t, Invoke(p, func(params Params) { got = params }))
		assert.NotNil(t, got.Service)
		assert.Nil(t, got.Missing)
	})

	t.Run("returns_the_function_error", func(t *testing.T) {
		t.Parallel()
		boom := errors.New("boom")
		// Unchanged: it is the caller's own error, not a constructor's.
		require.Equal(t, boom, Invoke(p, func(*TService) error { return boom }))
	})

	t.Run("reports_missing_dependencies", func(t *testing.T) {
		t.Parallel()
		require.ErrorIs(t, Invoke(p, func(*TScoped) {}), ErrServiceNotFound)
	})

	t.Run("rejects_non_functions", func(t *testing.T) {
		t.Parallel()
		require.Error(t, Invoke(p, 42))
	})
}

func TestIsService(t *testing.T) {
	t.Parallel()
	p := BuildProvider(t, AddSingleton(NewTService), AddSingleton(NewTDependency, Name("named")))

	assert.True(t, IsService(p, reflect.TypeFor[*TService]()))
	assert.False(t, IsService(p, reflect.TypeFor[*TDependency]()), "only registered under a key")
	assert.True(t, IsKeyedService(p, reflect.TypeFor[*TDependency](), "named"))
	assert.False(t, IsKeyedService(p, reflect.TypeFor[*TDependency](), "other"))
	assert.True(t, IsService(p, reflect.TypeFor[Scope]()), "the container's own types are services")
	assert.True(t, IsService(p, reflect.TypeFor[context.Context]()))

	t.Run("agrees_with_an_injected_resolver", func(t *testing.T) {
		t.Parallel()
		type Holder struct{ Resolver Resolver }
		c := NewCollection()
		c.AddSingleton(func(r Resolver) *Holder { return &Holder{Resolver: r} })
		built, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = built.Close() })
		holder, err := Resolve[*Holder](built)
		require.NoError(t, err)

		assert.True(t, IsService(holder.Resolver, reflect.TypeFor[Resolver]()))
		assert.False(t, IsService(holder.Resolver, reflect.TypeFor[Provider]()), "an injected Resolver refuses the container")
		assert.False(t, IsService(resolverOnly{built}, reflect.TypeFor[*Holder]()), "a Resolver godi did not create cannot be inspected")
	})

	t.Run("agrees_with_validate_scopes", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddScoped(NewTService)
		vp, err := c.Build(WithScopeValidation(true))
		require.NoError(t, err)
		t.Cleanup(func() { _ = vp.Close() })
		scope, err := vp.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = scope.Close() })

		// Resolving it from the provider fails with ErrScopeRequired.
		assert.False(t, IsService(vp, reflect.TypeFor[*TService]()))
		assert.True(t, IsService(scope, reflect.TypeFor[*TService]()))
	})
}
