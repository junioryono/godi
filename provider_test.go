package godi

import (
	"context"
	"sync"
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

// TestProviderCloseSurvivesDisposablePanic ensures the singleton-disposal loop
// at provider.Close keeps running even if a Close() panics. The provider's
// Close must return a DisposalError and the remaining disposables must still
// be released.
func TestProviderCloseSurvivesDisposablePanic(t *testing.T) {
	t.Parallel()

	c := NewCollection()
	c.AddSingleton(func() *recordingDisposable {
		return &recordingDisposable{}
	})
	c.AddSingleton(func() *panickyDisposable {
		return &panickyDisposable{name: "boom"}
	})

	p, err := c.Build()
	require.NoError(t, err)

	rec, err := Resolve[*recordingDisposable](p)
	require.NoError(t, err)
	_, err = Resolve[*panickyDisposable](p)
	require.NoError(t, err)

	var closeErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("provider.Close propagated panic: %v", r)
			}
		}()
		closeErr = p.Close()
	}()

	require.Error(t, closeErr)
	var disposalErr *DisposalError
	assert.ErrorAs(t, closeErr, &disposalErr)
	assert.True(t, rec.closed.Load(),
		"recording singleton disposable must be closed despite the panic")
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

func TestCreateScopeRacingProviderClose(t *testing.T) {
	t.Parallel()

	for range 500 {
		c := NewCollection()
		c.AddScoped(NewTService)
		p, err := c.Build()
		require.NoError(t, err)

		var wg sync.WaitGroup
		var panicked any
		wg.Add(2)
		go func() {
			defer wg.Done()
			defer func() { panicked = recover() }()
			s, err := p.CreateScope(context.Background())
			if err == nil {
				_ = s.Close()
			}
		}()
		go func() { defer wg.Done(); _ = p.Close() }()
		wg.Wait()

		require.Nil(t, panicked, "CreateScope racing Close must not panic")
	}
}

func TestGetRacingProviderClose(t *testing.T) {
	t.Parallel()

	// Meaningful under -race: provider.Close must not write fields that
	// concurrent Get reads without synchronization.
	for range 300 {
		c := NewCollection()
		c.AddSingleton(NewTService)
		p, err := c.Build()
		require.NoError(t, err)

		var wg sync.WaitGroup
		var panicked any
		wg.Add(2)
		go func() {
			defer wg.Done()
			defer func() { panicked = recover() }()
			_, _ = Resolve[*TService](p)
		}()
		go func() { defer wg.Done(); _ = p.Close() }()
		wg.Wait()

		require.Nil(t, panicked, "Get racing Close must not panic")
	}
}
