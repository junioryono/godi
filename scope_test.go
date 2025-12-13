package godi

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testContextKey string

func TestScopeLifetimeSemantics(t *testing.T) {
	t.Parallel()

	t.Run("singleton_shared_across_scopes", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t, AddSingleton(NewTService))

		s1, _ := p.CreateScope(context.Background())
		defer s1.Close()
		s2, _ := p.CreateScope(context.Background())
		defer s2.Close()

		svc1, _ := s1.Get(PtrTypeOf[TService]())
		svc2, _ := s2.Get(PtrTypeOf[TService]())
		provSvc, _ := p.Get(PtrTypeOf[TService]())

		assert.Same(t, svc1, svc2)
		assert.Same(t, svc1, provSvc)
	})

	t.Run("scoped_unique_per_scope", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t, AddScoped(NewTScoped))

		s1, _ := p.CreateScope(context.Background())
		defer s1.Close()
		s2, _ := p.CreateScope(context.Background())
		defer s2.Close()

		// Same instance within scope
		svc1a, _ := s1.Get(PtrTypeOf[TScoped]())
		svc1b, _ := s1.Get(PtrTypeOf[TScoped]())
		assert.Same(t, svc1a, svc1b)

		// Different instance across scopes
		svc2, _ := s2.Get(PtrTypeOf[TScoped]())
		assert.NotSame(t, svc1a, svc2)
	})

	t.Run("transient_unique_every_time", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t, AddTransient(NewTTransient))

		scope, _ := p.CreateScope(context.Background())
		defer scope.Close()

		svc1, _ := scope.Get(PtrTypeOf[TTransient]())
		svc2, _ := scope.Get(PtrTypeOf[TTransient]())

		assert.NotSame(t, svc1, svc2)
	})
}

func TestScopeDisposal(t *testing.T) {
	t.Parallel()

	t.Run("scope_disposes_scoped_services", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t, AddScoped(NewTDisposable))

		scope, _ := p.CreateScope(context.Background())
		svc, _ := scope.Get(PtrTypeOf[TDisposable]())
		d := svc.(*TDisposable)

		assert.False(t, d.IsClosed())
		scope.Close()
		assert.True(t, d.IsClosed())
	})

	t.Run("scope_disposes_transient_services", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t, AddTransient(NewTDisposable))

		scope, _ := p.CreateScope(context.Background())
		svc1, _ := scope.Get(PtrTypeOf[TDisposable]())
		svc2, _ := scope.Get(PtrTypeOf[TDisposable]())
		d1, d2 := svc1.(*TDisposable), svc2.(*TDisposable)

		scope.Close()
		assert.True(t, d1.IsClosed())
		assert.True(t, d2.IsClosed())
	})

	t.Run("scope_does_not_dispose_singletons", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t, AddSingleton(NewTDisposable))

		scope, _ := p.CreateScope(context.Background())
		svc, _ := scope.Get(PtrTypeOf[TDisposable]())
		d := svc.(*TDisposable)

		scope.Close()
		assert.False(t, d.IsClosed()) // Singleton outlives scope
	})

	t.Run("provider_disposes_singletons", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		require.NoError(t, c.AddSingleton(NewTDisposable))
		p, _ := c.Build()

		svc, _ := p.Get(PtrTypeOf[TDisposable]())
		d := svc.(*TDisposable)

		p.Close()
		assert.True(t, d.IsClosed())
	})

	t.Run("provider_closes_active_scopes", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		p, _ := c.Build()

		scope, _ := p.CreateScope(context.Background())
		p.Close()

		_, err := scope.Get(PtrTypeOf[TService]())
		assert.ErrorIs(t, err, ErrScopeDisposed)
	})
}

func TestScopeContextCancellation(t *testing.T) {
	t.Parallel()

	p := BuildProvider(t)
	ctx, cancel := context.WithCancel(context.Background())
	scope, err := p.CreateScope(ctx)
	require.NoError(t, err)

	cancel()
	time.Sleep(50 * time.Millisecond) // Allow cancellation to propagate

	_, err = scope.Get(PtrTypeOf[TService]())
	assert.ErrorIs(t, err, ErrScopeDisposed)
}

func TestNestedScopes(t *testing.T) {
	t.Parallel()

	t.Run("child_gets_own_scoped_instances", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t, AddScoped(NewTScoped))

		parent, _ := p.CreateScope(context.Background())
		defer parent.Close()
		child, _ := parent.CreateScope(context.Background())
		defer child.Close()

		parentSvc, _ := parent.Get(PtrTypeOf[TScoped]())
		childSvc, _ := child.Get(PtrTypeOf[TScoped]())

		assert.NotSame(t, parentSvc, childSvc)
	})

	t.Run("parent_close_closes_children", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t)

		parent, _ := p.CreateScope(context.Background())
		child, _ := parent.CreateScope(context.Background())
		grandchild, _ := child.CreateScope(context.Background())

		parent.Close()

		_, err := child.Get(PtrTypeOf[TService]())
		assert.Error(t, err)
		_, err = grandchild.Get(PtrTypeOf[TService]())
		assert.Error(t, err)
	})
}

func TestKeyedAndGroupedResolution(t *testing.T) {
	t.Parallel()

	t.Run("resolves_keyed_services", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t,
			AddScoped(NewTServiceWithID("primary"), Name("primary")),
			AddScoped(NewTServiceWithID("backup"), Name("backup")),
		)

		scope, _ := p.CreateScope(context.Background())
		defer scope.Close()

		primary, _ := scope.GetKeyed(PtrTypeOf[TService](), "primary")
		backup, _ := scope.GetKeyed(PtrTypeOf[TService](), "backup")

		assert.Equal(t, "primary", primary.(*TService).ID)
		assert.Equal(t, "backup", backup.(*TService).ID)
	})

	t.Run("resolves_grouped_services", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t,
			AddScoped(NewTServiceWithID("h1"), Group("handlers")),
			AddScoped(NewTServiceWithID("h2"), Group("handlers")),
			AddTransient(NewTServiceWithID("h3"), Group("handlers")),
		)

		scope, _ := p.CreateScope(context.Background())
		defer scope.Close()

		handlers, err := scope.GetGroup(PtrTypeOf[TService](), "handlers")
		require.NoError(t, err)
		assert.Len(t, handlers, 3)
	})

	t.Run("empty_group_returns_empty_slice", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t)

		scope, _ := p.CreateScope(context.Background())
		defer scope.Close()

		handlers, err := scope.GetGroup(PtrTypeOf[TService](), "nonexistent")
		require.NoError(t, err)
		assert.Empty(t, handlers)
	})
}

func TestBuiltinServiceInjection(t *testing.T) {
	t.Parallel()

	t.Run("injects_context", func(t *testing.T) {
		t.Parallel()
		type CtxService struct{ Ctx context.Context }

		c := NewCollection()
		require.NoError(t, c.AddScoped(func(ctx context.Context) *CtxService {
			return &CtxService{Ctx: ctx}
		}))

		p, _ := c.Build()
		defer p.Close()

		ctx := context.WithValue(context.Background(), testContextKey("key"), "value")
		scope, _ := p.CreateScope(ctx)
		defer scope.Close()

		svc, _ := Resolve[*CtxService](scope)
		assert.Equal(t, "value", svc.Ctx.Value(testContextKey("key")))
	})

	t.Run("injects_scope", func(t *testing.T) {
		t.Parallel()
		type ScopeSvc struct{ Scope Scope }

		c := NewCollection()
		require.NoError(t, c.AddScoped(func(s Scope) *ScopeSvc {
			return &ScopeSvc{Scope: s}
		}))

		p, _ := c.Build()
		defer p.Close()

		scope, _ := p.CreateScope(context.Background())
		defer scope.Close()

		svc, _ := Resolve[*ScopeSvc](scope)
		assert.Same(t, scope, svc.Scope)
	})

	t.Run("injects_provider", func(t *testing.T) {
		t.Parallel()
		type ProvSvc struct{ Prov Provider }

		c := NewCollection()
		require.NoError(t, c.AddSingleton(func(p Provider) *ProvSvc {
			return &ProvSvc{Prov: p}
		}))

		p, _ := c.Build()
		defer p.Close()

		svc, _ := Resolve[*ProvSvc](p)
		assert.Same(t, p, svc.Prov)
	})
}

func TestConstructorErrors(t *testing.T) {
	t.Parallel()

	t.Run("propagates_constructor_errors", func(t *testing.T) {
		t.Parallel()
		expectedErr := errors.New("initialization failed")

		c := NewCollection()
		require.NoError(t, c.AddSingleton(func() (*TService, error) {
			return nil, expectedErr
		}))

		_, err := c.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "initialization failed")
	})

	t.Run("recovers_from_panics", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		require.NoError(t, c.AddSingleton(func() *TService {
			panic("constructor panic")
		}))

		_, err := c.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "panicked")
	})

	t.Run("scoped_constructor_panic_recovered", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		require.NoError(t, c.AddScoped(func() *TService {
			panic("scoped panic")
		}))

		p, _ := c.Build()
		defer p.Close()

		scope, _ := p.CreateScope(context.Background())
		defer scope.Close()

		_, err := scope.Get(PtrTypeOf[TService]())
		require.Error(t, err)
		assert.Contains(t, err.Error(), "panicked")
	})
}

func TestDisposedStateErrors(t *testing.T) {
	t.Parallel()

	t.Run("disposed_provider_rejects_scope_creation", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		p, _ := c.Build()
		p.Close()

		_, err := p.CreateScope(context.Background())
		assert.ErrorIs(t, err, ErrProviderDisposed)
	})

	t.Run("disposed_scope_rejects_resolution", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t, AddScoped(NewTScoped))

		scope, _ := p.CreateScope(context.Background())
		scope.Close()

		_, err := scope.Get(PtrTypeOf[TScoped]())
		assert.ErrorIs(t, err, ErrScopeDisposed)
	})

	t.Run("multiple_close_is_safe", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		p, _ := c.Build()

		require.NoError(t, p.Close())
		require.NoError(t, p.Close()) // Should not error or panic
	})
}

func TestVoidAndErrorOnlyConstructors(t *testing.T) {
	t.Parallel()

	t.Run("void_constructor_for_side_effects", func(t *testing.T) {
		t.Parallel()
		var initialized atomic.Bool

		c := NewCollection()
		require.NoError(t, c.AddSingleton(func() {
			initialized.Store(true)
		}))

		p, err := c.Build()
		require.NoError(t, err)
		defer p.Close()

		// Side effect should have run during build
		assert.True(t, initialized.Load())
	})

	t.Run("error_only_constructor_success", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		require.NoError(t, c.AddSingleton(func() error {
			return nil
		}))

		_, err := c.Build()
		require.NoError(t, err)
	})

	t.Run("error_only_constructor_failure", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		require.NoError(t, c.AddSingleton(func() error {
			return errors.New("init failed")
		}))

		_, err := c.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "init failed")
	})
}

func TestComplexDependencyGraph(t *testing.T) {
	t.Parallel()

	// Simulates a realistic application setup
	type (
		Config   struct{ DSN string }
		DB       struct{ Config *Config }
		Cache    struct{ DB *DB }
		UserRepo struct{ DB *DB }
		UserSvc  struct {
			Repo  *UserRepo
			Cache *Cache
		}
	)

	c := NewCollection()
	require.NoError(t, c.AddSingleton(func() *Config { return &Config{DSN: "postgres://..."} }))
	require.NoError(t, c.AddSingleton(func(cfg *Config) *DB { return &DB{Config: cfg} }))
	require.NoError(t, c.AddSingleton(func(db *DB) *Cache { return &Cache{DB: db} }))
	require.NoError(t, c.AddScoped(func(db *DB) *UserRepo { return &UserRepo{DB: db} }))
	require.NoError(t, c.AddScoped(func(repo *UserRepo, cache *Cache) *UserSvc {
		return &UserSvc{Repo: repo, Cache: cache}
	}))

	p, err := c.Build()
	require.NoError(t, err)
	defer p.Close()

	scope, _ := p.CreateScope(context.Background())
	defer scope.Close()

	svc, err := Resolve[*UserSvc](scope)
	require.NoError(t, err)

	// Verify the dependency chain is wired correctly
	assert.NotNil(t, svc.Repo)
	assert.NotNil(t, svc.Repo.DB)
	assert.NotNil(t, svc.Repo.DB.Config)
	assert.Equal(t, "postgres://...", svc.Repo.DB.Config.DSN)
	assert.NotNil(t, svc.Cache)
	assert.Same(t, svc.Repo.DB, svc.Cache.DB) // Singleton shared
}
