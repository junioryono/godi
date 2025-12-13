package godi

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProvider(t *testing.T) {
	t.Parallel()

	t.Run("Resolve", func(t *testing.T) {
		t.Parallel()

		t.Run("successful", func(t *testing.T) {
			t.Parallel()
			p := BuildProvider(t, AddSingleton(NewTServiceWithID("test")))
			svc, err := Resolve[*TService](p)
			require.NoError(t, err)
			assert.Equal(t, "test", svc.ID)
		})

		t.Run("nil_provider", func(t *testing.T) {
			t.Parallel()
			_, err := Resolve[*TService](nil)
			assert.ErrorIs(t, err, ErrProviderNil)
		})

		t.Run("not_found", func(t *testing.T) {
			t.Parallel()
			p := BuildProvider(t)
			_, err := Resolve[*TService](p)
			require.Error(t, err)
		})

		t.Run("type_mismatch", func(t *testing.T) {
			t.Parallel()
			p := BuildProvider(t, AddSingleton(func() string { return "str" }))
			_, err := Resolve[*TService](p)
			require.Error(t, err)
		})
	})

	t.Run("MustResolve", func(t *testing.T) {
		t.Parallel()

		t.Run("successful", func(t *testing.T) {
			t.Parallel()
			p := BuildProvider(t, AddSingleton(NewTServiceWithID("test")))
			svc := MustResolve[*TService](p)
			assert.Equal(t, "test", svc.ID)
		})

		t.Run("panics", func(t *testing.T) {
			t.Parallel()
			assert.Panics(t, func() { MustResolve[*TService](nil) })
		})
	})

	t.Run("ResolveKeyed", func(t *testing.T) {
		t.Parallel()

		t.Run("successful", func(t *testing.T) {
			t.Parallel()
			p := BuildProvider(t, AddSingleton(NewTServiceWithID("keyed"), Name("primary")))
			svc, err := ResolveKeyed[*TService](p, "primary")
			require.NoError(t, err)
			assert.Equal(t, "keyed", svc.ID)
		})

		t.Run("nil_provider", func(t *testing.T) {
			t.Parallel()
			_, err := ResolveKeyed[*TService](nil, "key")
			assert.ErrorIs(t, err, ErrProviderNil)
		})

		t.Run("nil_key", func(t *testing.T) {
			t.Parallel()
			p := BuildProvider(t)
			_, err := ResolveKeyed[*TService](p, nil)
			assert.ErrorIs(t, err, ErrServiceKeyNil)
		})

		t.Run("not_found", func(t *testing.T) {
			t.Parallel()
			p := BuildProvider(t, AddSingleton(NewTService, Name("primary")))
			_, err := ResolveKeyed[*TService](p, "nonexistent")
			require.Error(t, err)
		})
	})

	t.Run("MustResolveKeyed", func(t *testing.T) {
		t.Parallel()

		t.Run("successful", func(t *testing.T) {
			t.Parallel()
			p := BuildProvider(t, AddSingleton(NewTServiceWithID("keyed"), Name("primary")))
			svc := MustResolveKeyed[*TService](p, "primary")
			assert.Equal(t, "keyed", svc.ID)
		})

		t.Run("panics", func(t *testing.T) {
			t.Parallel()
			assert.Panics(t, func() { MustResolveKeyed[*TService](nil, "key") })
		})
	})

	t.Run("ResolveGroup", func(t *testing.T) {
		t.Parallel()

		t.Run("successful", func(t *testing.T) {
			t.Parallel()
			p := BuildProvider(t,
				AddSingleton(NewTServiceWithID("svc1"), Group("handlers")),
				AddSingleton(NewTServiceWithID("svc2"), Group("handlers")),
			)
			services, err := ResolveGroup[*TService](p, "handlers")
			require.NoError(t, err)
			assert.Len(t, services, 2)
		})

		t.Run("nil_provider", func(t *testing.T) {
			t.Parallel()
			_, err := ResolveGroup[*TService](nil, "group")
			assert.ErrorIs(t, err, ErrProviderNil)
		})

		t.Run("empty_group_name", func(t *testing.T) {
			t.Parallel()
			p := BuildProvider(t)
			_, err := ResolveGroup[*TService](p, "")
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrGroupNameEmpty)
		})

		t.Run("not_found", func(t *testing.T) {
			t.Parallel()
			p := BuildProvider(t)
			services, err := ResolveGroup[*TService](p, "nonexistent")
			require.NoError(t, err)
			assert.Empty(t, services)
		})
	})

	t.Run("MustResolveGroup", func(t *testing.T) {
		t.Parallel()

		t.Run("successful", func(t *testing.T) {
			t.Parallel()
			p := BuildProvider(t, AddSingleton(NewTService, Group("handlers")))
			services := MustResolveGroup[*TService](p, "handlers")
			assert.Len(t, services, 1)
		})

		t.Run("panics", func(t *testing.T) {
			t.Parallel()
			assert.Panics(t, func() { MustResolveGroup[*TService](nil, "group") })
		})
	})

	t.Run("ID", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t)
		id := p.ID()
		assert.NotEmpty(t, id)
		assert.Equal(t, id, p.ID()) // Should remain constant
	})

	t.Run("FromContext", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t, AddScoped(NewTService))
		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope.Close()

		s, err := FromContext(scope.Context())
		require.NoError(t, err)
		assert.NotNil(t, s)
	})

	t.Run("GetMethods", func(t *testing.T) {
		t.Parallel()

		t.Run("nil_type", func(t *testing.T) {
			t.Parallel()
			p := BuildProvider(t)
			_, err := p.Get(nil)
			assert.ErrorIs(t, err, ErrServiceTypeNil)
		})

		t.Run("GetKeyed_nil_type", func(t *testing.T) {
			t.Parallel()
			p := BuildProvider(t)
			_, err := p.GetKeyed(nil, "key")
			assert.ErrorIs(t, err, ErrServiceTypeNil)
		})

		t.Run("GetGroup_nil_type", func(t *testing.T) {
			t.Parallel()
			p := BuildProvider(t)
			_, err := p.GetGroup(nil, "group")
			assert.ErrorIs(t, err, ErrServiceTypeNil)
		})

		t.Run("after_disposal", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			p, _ := c.Build()
			p.Close()

			_, err := p.Get(PtrTypeOf[TService]())
			assert.ErrorIs(t, err, ErrProviderDisposed)

			_, err = p.GetKeyed(PtrTypeOf[TService](), "key")
			assert.ErrorIs(t, err, ErrProviderDisposed)

			_, err = p.GetGroup(PtrTypeOf[TService](), "group")
			assert.ErrorIs(t, err, ErrProviderDisposed)

			_, err = p.CreateScope(context.Background())
			assert.ErrorIs(t, err, ErrProviderDisposed)
		})
	})

	t.Run("MultiReturn", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t, AddSingleton(NewTMultiReturnWithError))

		svc, err := Resolve[*TService](p)
		require.NoError(t, err)
		assert.NotNil(t, svc)

		dep, err := Resolve[*TDependency](p)
		require.NoError(t, err)
		assert.NotNil(t, dep)
	})

	t.Run("DependencyInjection", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t,
			AddSingleton(NewTService),
			AddSingleton(NewTDependency),
			AddSingleton(NewTServiceWithDeps),
		)

		swd, err := Resolve[*TServiceWithDeps](p)
		require.NoError(t, err)
		assert.NotNil(t, swd.Svc)
		assert.NotNil(t, swd.Dep)
	})
}

func TestExtractParameterTypes(t *testing.T) {
	t.Parallel()

	t.Run("nil_info", func(t *testing.T) {
		t.Parallel()
		types := extractParameterTypes(nil)
		assert.Nil(t, types)
	})

	t.Run("with_parameters", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t,
			AddSingleton(NewTService),
			AddSingleton(NewTDependency),
			AddSingleton(NewTServiceWithDeps),
		)

		swd, err := Resolve[*TServiceWithDeps](p)
		require.NoError(t, err)
		assert.NotNil(t, swd)
		assert.Equal(t, "test", swd.Svc.ID)
		assert.Equal(t, "dep", swd.Dep.Name)
	})
}
