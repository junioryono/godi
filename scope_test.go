package godi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TScoped is a scoped service.
type TScoped struct {
	Created time.Time
	ScopeID string
}

func NewTScoped() *TScoped {
	return &TScoped{Created: time.Now(), ScopeID: "default"}
}

// TTransient is a transient service; each instance is numbered.
type TTransient struct {
	Instance int
}

var transientCounter atomic.Int64

func NewTTransient() *TTransient {
	return &TTransient{Instance: int(transientCounter.Add(1))}
}

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

		svc1, _ := s1.Get(reflect.TypeFor[*TService]())
		svc2, _ := s2.Get(reflect.TypeFor[*TService]())
		provSvc, _ := p.Get(reflect.TypeFor[*TService]())

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
		svc1a, _ := s1.Get(reflect.TypeFor[*TScoped]())
		svc1b, _ := s1.Get(reflect.TypeFor[*TScoped]())
		assert.Same(t, svc1a, svc1b)

		// Different instance across scopes
		svc2, _ := s2.Get(reflect.TypeFor[*TScoped]())
		assert.NotSame(t, svc1a, svc2)
	})

	t.Run("transient_unique_every_time", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t, AddTransient(NewTTransient))

		scope, _ := p.CreateScope(context.Background())
		defer scope.Close()

		svc1, _ := scope.Get(reflect.TypeFor[*TTransient]())
		svc2, _ := scope.Get(reflect.TypeFor[*TTransient]())

		assert.NotSame(t, svc1, svc2)
	})
}

func TestScopeDisposal(t *testing.T) {
	t.Parallel()

	t.Run("scope_disposes_scoped_services", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t, AddScoped(NewTDisposable))

		scope, _ := p.CreateScope(context.Background())
		svc, _ := scope.Get(reflect.TypeFor[*TDisposable]())
		d := svc.(*TDisposable)

		assert.False(t, d.IsClosed())
		scope.Close()
		assert.True(t, d.IsClosed())
	})

	t.Run("scope_disposes_transient_services", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t, AddTransient(NewTDisposable))

		scope, _ := p.CreateScope(context.Background())
		svc1, _ := scope.Get(reflect.TypeFor[*TDisposable]())
		svc2, _ := scope.Get(reflect.TypeFor[*TDisposable]())
		d1, d2 := svc1.(*TDisposable), svc2.(*TDisposable)

		scope.Close()
		assert.True(t, d1.IsClosed())
		assert.True(t, d2.IsClosed())
	})

	t.Run("scope_does_not_dispose_singletons", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t, AddSingleton(NewTDisposable))

		scope, _ := p.CreateScope(context.Background())
		svc, _ := scope.Get(reflect.TypeFor[*TDisposable]())
		d := svc.(*TDisposable)

		scope.Close()
		assert.False(t, d.IsClosed()) // Singleton outlives scope
	})

	t.Run("scope_does_not_dispose_borrowed_singleton", func(t *testing.T) {
		t.Parallel()
		for _, lifetime := range []Lifetime{Scoped, Transient} {
			t.Run(lifetime.String(), func(t *testing.T) {
				t.Parallel()
				c := NewCollection()
				c.AddSingleton(NewTDisposable)
				// A shorter-lived service that hands out the singleton itself.
				asCloser := func(d *TDisposable) io.Closer { return d }
				if lifetime == Scoped {
					c.AddScoped(asCloser)
				} else {
					c.AddTransient(asCloser)
				}
				p, err := c.Build()
				require.NoError(t, err)
				singleton, err := Resolve[*TDisposable](p)
				require.NoError(t, err)

				for range 2 {
					scope, err := p.CreateScope(context.Background())
					require.NoError(t, err)
					_, err = Resolve[io.Closer](scope)
					require.NoError(t, err)
					require.NoError(t, scope.Close())
					assert.False(t, singleton.IsClosed(), "a scope must not close a singleton it only borrowed")
				}

				require.NoError(t, p.Close(), "the provider closes its singleton exactly once")
				assert.True(t, singleton.IsClosed())
			})
		}
	})

	t.Run("failed_multi_return_closes_produced_sibling", func(t *testing.T) {
		t.Parallel()
		var produced atomic.Pointer[TDisposable]
		c := NewCollection()
		c.AddScoped(func() (*TDisposable, *TService) {
			d := NewTDisposable()
			produced.Store(d)
			return d, nil // nil service fails resolution
		})
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		_, err = Resolve[*TService](scope)
		require.Error(t, err)
		require.NoError(t, scope.Close())

		require.NotNil(t, produced.Load())
		assert.True(t, produced.Load().IsClosed(), "the successfully produced disposable must not leak")
	})

	t.Run("provider_disposes_singletons", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTDisposable)
		p, _ := c.Build()

		svc, _ := p.Get(reflect.TypeFor[*TDisposable]())
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

		_, err := scope.Get(reflect.TypeFor[*TService]())
		assert.ErrorIs(t, err, ErrScopeDisposed)
	})
}

func TestScopeContextCancellation(t *testing.T) {
	t.Parallel()

	for name, nested := range map[string]bool{"top_level": false, "child": true} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				p := BuildProvider(t, AddScoped(NewTDisposable))
				ctx, cancel := context.WithCancel(context.Background())
				createScope := p.CreateScope
				if nested {
					parent, err := p.CreateScope(context.Background())
					require.NoError(t, err)
					t.Cleanup(func() { _ = parent.Close() })
					createScope = parent.CreateScope
				}
				scope, err := createScope(ctx)
				require.NoError(t, err)
				d, err := Resolve[*TDisposable](scope)
				require.NoError(t, err)

				cancel()
				synctest.Wait()

				// Cancellation is a signal to stop work, not proof that it
				// stopped: a handler still unwinding keeps its resources
				// until the scope's owner closes it. Previously the scope
				// was closed from another goroutine underneath the handler.
				require.ErrorIs(t, scope.Context().Err(), context.Canceled)
				assert.False(t, d.IsClosed(), "cancellation must not dispose the scope")
				again, err := Resolve[*TDisposable](scope)
				require.NoError(t, err)
				assert.Same(t, d, again)

				require.NoError(t, scope.Close())
				assert.True(t, d.IsClosed())
			})
		})
	}
}

// TestRootScope pins that the provider's root scope is a scope: it resolves
// scoped services, caching them until the provider closes, and runs scoped
// initializers at Build.
func TestRootScope(t *testing.T) {
	t.Parallel()

	// Not zero-sized: pointers to zero-sized values may all be equal, which
	// would make the identity assertions below meaningless.
	type Unit struct{ _ int }
	type Handler struct{ Unit *Unit }
	build := func(t *testing.T, register func(Collection)) Provider {
		t.Helper()
		c := NewCollection()
		register(c)
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		return p
	}

	t.Run("resolves_and_caches_scoped_services", func(t *testing.T) {
		t.Parallel()
		p := build(t, func(c Collection) { c.AddScoped(func() *Unit { return &Unit{} }) })

		first, err := Resolve[*Unit](p)
		require.NoError(t, err)
		again, err := Resolve[*Unit](p)
		require.NoError(t, err)
		assert.Same(t, first, again, "the root scope caches its scoped instance")

		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = scope.Close() })
		inScope, err := Resolve[*Unit](scope)
		require.NoError(t, err)
		assert.NotSame(t, first, inScope, "a child scope gets its own instance")
	})

	t.Run("resolves_transients_needing_scoped_services", func(t *testing.T) {
		t.Parallel()
		p := build(t, func(c Collection) {
			c.AddScoped(func() *Unit { return &Unit{} })
			c.AddTransient(func(u *Unit) *Handler { return &Handler{Unit: u} })
		})
		h, err := Resolve[*Handler](p)
		require.NoError(t, err)
		u, err := Resolve[*Unit](p)
		require.NoError(t, err)
		assert.Same(t, u, h.Unit)
	})

	t.Run("runs_scoped_initializers_at_build", func(t *testing.T) {
		t.Parallel()
		var runs atomic.Int32
		p := build(t, func(c Collection) { c.AddScoped(func() { runs.Add(1) }) })
		assert.Equal(t, int32(1), runs.Load(), "the root scope runs its initializers at Build")

		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = scope.Close() })
		assert.Equal(t, int32(2), runs.Load(), "and each child scope runs its own")
	})

	t.Run("disposes_scoped_services_when_the_provider_closes", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddScoped(func() *TDisposable { return &TDisposable{} })
		p, err := c.Build()
		require.NoError(t, err)

		d, err := Resolve[*TDisposable](p)
		require.NoError(t, err)
		assert.False(t, d.IsClosed())
		require.NoError(t, p.Close())
		assert.True(t, d.IsClosed())
	})

	// The provider owns what the root scope creates: a failed Close is not a
	// singleton's.
	t.Run("reports_disposal_failures_as_the_providers", func(t *testing.T) {
		t.Parallel()
		closeErr := errors.New("close failed")
		c := NewCollection()
		c.AddScoped(func() *TDisposable {
			d := NewTDisposable()
			d.SetCloseError(closeErr)
			return d
		})
		p, err := c.Build()
		require.NoError(t, err)
		_, err = Resolve[*TDisposable](p)
		require.NoError(t, err)

		err = p.Close()
		require.ErrorIs(t, err, closeErr)
		assert.Contains(t, err.Error(), "provider disposable")
		assert.NotContains(t, err.Error(), "singleton disposable")
	})

	// A failing initializer fails Build, which disposes what the root scope
	// had created.
	t.Run("a_failing_initializer_fails_build", func(t *testing.T) {
		t.Parallel()
		initErr := errors.New("init failed")
		var dep *TDisposable
		c := NewCollection()
		c.AddScoped(func() *TDisposable {
			dep = NewTDisposable()
			return dep
		})
		c.AddScoped(func(*TDisposable) error { return initErr })

		p, err := c.Build()
		require.Error(t, err)
		assert.Nil(t, p)
		assert.ErrorIs(t, err, initErr)
		buildErr, ok := errors.AsType[*BuildError](err)
		require.True(t, ok, "want a *BuildError, got %T", err)
		assert.Equal(t, PhaseScopeInitialization, buildErr.Phase)
		require.NotNil(t, dep, "the initializer's dependency was created in the root scope")
		assert.True(t, dep.IsClosed(), "and disposed when Build failed")
	})

	// Build stops running initializers once its context is cancelled, as it
	// stops creating singletons.
	t.Run("stops_initializers_when_build_is_cancelled", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var second atomic.Bool
		c := NewCollection()
		c.AddScoped(func() { cancel() })
		c.AddScoped(func() { second.Store(true) })

		p, err := c.Build(WithContext(ctx))
		require.Error(t, err)
		assert.Nil(t, p)
		assert.ErrorIs(t, err, context.Canceled)
		assert.False(t, second.Load(), "no initializer runs after the build context is cancelled")
	})
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

		parentSvc, _ := parent.Get(reflect.TypeFor[*TScoped]())
		childSvc, _ := child.Get(reflect.TypeFor[*TScoped]())

		assert.NotSame(t, parentSvc, childSvc)
	})

	t.Run("parent_close_closes_children", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t)

		parent, _ := p.CreateScope(context.Background())
		child, _ := parent.CreateScope(context.Background())
		grandchild, _ := child.CreateScope(context.Background())

		parent.Close()

		_, err := child.Get(reflect.TypeFor[*TService]())
		assert.Error(t, err)
		_, err = grandchild.Get(reflect.TypeFor[*TService]())
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

		primary, _ := scope.GetKeyed(reflect.TypeFor[*TService](), "primary")
		backup, _ := scope.GetKeyed(reflect.TypeFor[*TService](), "backup")

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

		handlers, err := scope.GetGroup(reflect.TypeFor[*TService](), "handlers")
		require.NoError(t, err)
		assert.Len(t, handlers, 3)
	})

	t.Run("empty_group_returns_empty_slice", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t)

		scope, _ := p.CreateScope(context.Background())
		defer scope.Close()

		handlers, err := scope.GetGroup(reflect.TypeFor[*TService](), "nonexistent")
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
		c.AddScoped(func(ctx context.Context) *CtxService {
			return &CtxService{Ctx: ctx}
		})

		p, _ := c.Build()
		defer p.Close()

		ctx := context.WithValue(context.Background(), testContextKey("key"), "value")
		scope, _ := p.CreateScope(ctx)
		defer scope.Close()

		svc, _ := Resolve[*CtxService](scope)
		assert.Equal(t, "value", svc.Ctx.Value(testContextKey("key")))
	})

	t.Run("injects_resolver", func(t *testing.T) {
		t.Parallel()
		type Holder struct{ Resolver Resolver }

		c := NewCollection()
		c.AddScoped(func() *TScoped { return &TScoped{} })
		c.AddScoped(func(r Resolver) *Holder { return &Holder{Resolver: r} })

		built, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = built.Close() })
		scope := NewTestScope(t, built)

		holder, err := Resolve[*Holder](scope)
		require.NoError(t, err)
		// It resolves from the scope running the constructor...
		fromHolder, err := Resolve[*TScoped](holder.Resolver)
		require.NoError(t, err)
		fromScope, err := Resolve[*TScoped](scope)
		require.NoError(t, err)
		assert.Same(t, fromScope, fromHolder)
		// ...but is not the scope: it cannot be closed or create scopes.
		_, isScope := holder.Resolver.(Scope)
		assert.False(t, isScope)
		_, isFactory := holder.Resolver.(ScopeFactory)
		assert.False(t, isFactory)
	})

	// A singleton's Resolver resolves from the root scope, so scoped services
	// it resolves are the root scope's, shared with the provider.
	t.Run("a_singletons_resolver_resolves_from_the_root_scope", func(t *testing.T) {
		t.Parallel()
		type Holder struct{ Resolver Resolver }
		c := NewCollection()
		c.AddScoped(func() *TScoped { return &TScoped{} })
		c.AddSingleton(func(r Resolver) *Holder { return &Holder{Resolver: r} })
		built, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = built.Close() })

		holder, err := Resolve[*Holder](built)
		require.NoError(t, err)
		viaResolver, err := Resolve[*TScoped](holder.Resolver)
		require.NoError(t, err)
		fromRoot, err := Resolve[*TScoped](built)
		require.NoError(t, err)
		assert.Same(t, fromRoot, viaResolver)
	})

	// The container itself stays out of reach: through an injected Resolver,
	// during construction or stored for later, and through the context.
	t.Run("an_injected_resolver_cannot_reach_the_container", func(t *testing.T) {
		t.Parallel()
		type Holder struct{ Resolver Resolver }
		var duringProvider, duringScope error
		c := NewCollection()
		c.AddScoped(func(r Resolver) *Holder {
			_, duringProvider = Resolve[Provider](r)
			_, duringScope = Resolve[Scope](r)
			return &Holder{Resolver: r}
		})
		built, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = built.Close() })

		holder, err := Resolve[*Holder](NewTestScope(t, built))
		require.NoError(t, err)
		require.Error(t, duringProvider)
		require.Error(t, duringScope)
		_, err = Resolve[Provider](holder.Resolver)
		require.Error(t, err, "a stored Resolver cannot reach the container either")
		_, err = Resolve[Scope](holder.Resolver)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "godi.ScopeFactory")

		// Nor through the context it resolves, after construction too.
		ctx, err := Resolve[context.Context](holder.Resolver)
		require.NoError(t, err)
		_, err = FromContext(ctx)
		require.Error(t, err, "a stored Resolver's context carries no scope")
	})

	t.Run("an_injected_context_carries_no_scope", func(t *testing.T) {
		t.Parallel()
		type Holder struct{ Ctx context.Context }
		c := NewCollection()
		c.AddScoped(func(ctx context.Context) *Holder { return &Holder{Ctx: ctx} })
		built, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = built.Close() })
		ctx := context.WithValue(context.Background(), testContextKey("key"), "value")
		scope, err := built.CreateScope(ctx)
		require.NoError(t, err)
		t.Cleanup(func() { _ = scope.Close() })

		holder, err := Resolve[*Holder](scope)
		require.NoError(t, err)
		assert.Equal(t, "value", holder.Ctx.Value(testContextKey("key")), "the scope's context values stay visible")
		_, err = FromContext(holder.Ctx)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "godi.Resolver")

		// Code outside constructors still finds the scope in its context.
		found, err := FromContext(scope.Context())
		require.NoError(t, err)
		assert.Equal(t, scope.ID(), found.ID())
	})

	// Creating a scope while the constructor is still running could run that
	// scope's initializers against the constructor's own unfinished output
	// and deadlock. Found by the Codex review of #66.
	// A factory held by a dependency of the running constructor is caught
	// too. Found by the second Claude review of #66.
	t.Run("scope_factory_held_by_a_dependency_cannot_create_scopes_during_construction", func(t *testing.T) {
		t.Parallel()
		type Spawner struct{ f ScopeFactory }
		type Server struct{}
		c := NewCollection()
		c.AddTransient(func(f ScopeFactory) *Spawner { return &Spawner{f: f} })
		c.AddSingleton(func(s *Spawner) (*Server, error) {
			child, err := s.f.CreateScope(context.Background())
			if err != nil {
				return nil, err
			}
			_ = child.Close()
			return &Server{}, nil
		})
		c.AddScoped(func(*Server) {})

		err := resolveWithin(t, func() error {
			_, buildErr := c.Build()
			return buildErr
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "store it and create scopes later")
	})

	// A scope created through an injected factory does not lead back to the
	// root either. Found by the second Claude review of #66.
	t.Run("scopes_from_an_injected_factory_cannot_reach_the_provider", func(t *testing.T) {
		t.Parallel()
		type Worker struct{ Scopes ScopeFactory }
		c := NewCollection()
		c.AddSingleton(func(f ScopeFactory) *Worker { return &Worker{Scopes: f} })
		built, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = built.Close() })

		worker, err := Resolve[*Worker](built)
		require.NoError(t, err)
		child, err := worker.Scopes.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = child.Close() })
		_, err = Resolve[Provider](child)
		require.Error(t, err)
		grandchild, err := child.CreateScope(context.Background())
		require.NoError(t, err)
		_, err = Resolve[Provider](grandchild)
		require.Error(t, err, "nor through its descendants")
		require.NoError(t, grandchild.Close())

		_, err = Resolve[Provider](NewTestScope(t, built))
		assert.NoError(t, err, "scopes the application creates are unrestricted")
	})

	// A scope is restricted before its initializers run, so a scope they
	// create is too. Found by the third Claude and Codex reviews of #66.
	t.Run("scopes_created_while_a_restricted_scope_initializes_are_restricted", func(t *testing.T) {
		t.Parallel()
		type Holder struct{ Scopes ScopeFactory }
		type Worker struct{ Scopes ScopeFactory }
		// Armed after Build: the root scope runs the initializer during Build,
		// and this test is about the restricted scope created later.
		var armed, once atomic.Bool
		var createErr, resolveErr error
		c := NewCollection()
		c.AddScoped(func(f ScopeFactory) *Holder { return &Holder{Scopes: f} })
		c.AddScoped(func(h *Holder) {
			if !armed.Load() || !once.CompareAndSwap(false, true) {
				return
			}
			grandchild, err := h.Scopes.CreateScope(context.Background())
			if err != nil {
				createErr = err
				return
			}
			defer grandchild.Close()
			_, resolveErr = Resolve[Provider](grandchild)
		})
		c.AddSingleton(func(f ScopeFactory) *Worker { return &Worker{Scopes: f} })
		built, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = built.Close() })
		armed.Store(true)

		worker, err := Resolve[*Worker](built)
		require.NoError(t, err)
		child, err := worker.Scopes.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = child.Close() })
		require.True(t, once.Load(), "the restricted scope's initializer ran")
		require.NoError(t, createErr, "the grandchild is created")
		require.Error(t, resolveErr, "the grandchild must not resolve the Provider")
	})

	// The background-worker pattern: an initializer hands its factory to a
	// goroutine, which creates scopes while the initializing scope is still
	// being set up. Run under -race. Found by the third reviews of #66.
	t.Run("restriction_is_set_before_initializers_run", func(t *testing.T) {
		t.Parallel()
		type Worker struct{ Scopes ScopeFactory }
		done := make(chan error, 1)
		// Armed after Build: the root scope runs the initializer during Build,
		// and the race is in the restricted scope created later.
		var armed, once atomic.Bool
		c := NewCollection()
		c.AddScoped(func(f ScopeFactory) {
			if !armed.Load() || !once.CompareAndSwap(false, true) {
				return
			}
			go func() {
				child, err := f.CreateScope(context.Background())
				if err == nil {
					_ = child.Close()
				}
				done <- err
			}()
		})
		c.AddSingleton(func(f ScopeFactory) *Worker { return &Worker{Scopes: f} })
		built, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = built.Close() })
		armed.Store(true)

		worker, err := Resolve[*Worker](built)
		require.NoError(t, err)
		child, err := worker.Scopes.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = child.Close() })
		require.True(t, once.Load(), "the restricted scope's initializer ran")
		require.NoError(t, <-done, "the worker creates a scope under the restricted one")
	})

	// Build includes the root scope's initializers, which run after the
	// singletons. Found by the third Codex review of #66.
	t.Run("scope_factory_cannot_create_scopes_until_build_completes", func(t *testing.T) {
		t.Parallel()
		type Worker struct{ Scopes ScopeFactory }
		var once atomic.Bool
		var createErr error
		c := NewCollection()
		c.AddSingleton(func(f ScopeFactory) *Worker { return &Worker{Scopes: f} })
		c.AddScoped(func(w *Worker) {
			if !once.CompareAndSwap(false, true) {
				return
			}
			var child Scope
			child, createErr = w.Scopes.CreateScope(context.Background())
			if child != nil {
				_ = child.Close()
			}
		})
		// The root scope runs scoped initializers at Build.
		built, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = built.Close() })
		require.Error(t, createErr)
		assert.Contains(t, createErr.Error(), "store it and create scopes later")
	})

	t.Run("scope_factory_cannot_create_scopes_during_construction", func(t *testing.T) {
		t.Parallel()
		type Server struct{}
		c := NewCollection()
		c.AddSingleton(func(f ScopeFactory) (*Server, error) {
			child, err := f.CreateScope(context.Background())
			if err != nil {
				return nil, err
			}
			_ = child.Close()
			return &Server{}, nil
		})
		c.AddScoped(func(*Server) {}) // an initializer needing the server

		err := resolveWithin(t, func() error {
			_, buildErr := c.Build()
			return buildErr
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "store it and create scopes later")
	})

	t.Run("injects_scope_factory", func(t *testing.T) {
		t.Parallel()
		type Worker struct{ Scopes ScopeFactory }
		c := NewCollection()
		c.AddScoped(NewTDisposable)
		c.AddScoped(func(f ScopeFactory) *Worker { return &Worker{Scopes: f} })
		built, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = built.Close() })
		request, err := built.CreateScope(context.Background())
		require.NoError(t, err)

		worker, err := Resolve[*Worker](request)
		require.NoError(t, err)
		child, err := worker.Scopes.CreateScope(context.Background())
		require.NoError(t, err)
		d, err := Resolve[*TDisposable](child)
		require.NoError(t, err)

		// Scopes it creates are children of the constructor's scope, closed
		// with it while the provider stays open.
		require.NoError(t, request.Close())
		assert.True(t, d.IsClosed())
		_, err = Resolve[*TDisposable](NewTestScope(t, built))
		assert.NoError(t, err, "the provider is still open")
	})

	t.Run("rejects_provider_and_scope_parameters", func(t *testing.T) {
		t.Parallel()
		type Holder struct{}
		type In1 struct {
			In
			S Scope
		}
		for name, register := range map[string]func(Collection){
			"provider":     func(c Collection) { c.AddSingleton(func(Provider) *Holder { return &Holder{} }) },
			"scope":        func(c Collection) { c.AddScoped(func(Scope) *Holder { return &Holder{} }) },
			"in_field":     func(c Collection) { c.AddScoped(func(In1) *Holder { return &Holder{} }) },
			"decorator":    func(c Collection) { c.AddModules(Decorate(func(h *TService, _ Scope) *TService { return h })) },
			"invoke_param": nil,
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				if register == nil {
					err := Invoke(BuildProvider(t), func(Provider) {})
					require.Error(t, err)
					assert.Contains(t, err.Error(), "godi.Resolver")
					return
				}
				c := NewCollection()
				c.AddSingleton(NewTService)
				register(c)
				err := Validate(c)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "godi.Resolver")
			})
		}
	})
}

// resolveWithin runs resolve, failing the test instead of hanging if it
// deadlocks.
func resolveWithin(t *testing.T, resolve func() error) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- resolve() }()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("resolution deadlocked")
		return nil
	}
}

// Constructors that resolve through their injected Resolver are
// outside the static dependency graph, so Build cannot see a cycle there.
func TestDynamicCircularResolution(t *testing.T) {
	t.Parallel()

	// resolveWithin fails the test instead of hanging when resolution
	// deadlocks.
	type SelfA struct{}
	type SelfB struct{}

	t.Run("scoped_self_resolution_through_resolver", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddScoped(func(s Resolver) (*SelfA, error) {
			if _, err := Resolve[*SelfA](s); err != nil {
				return nil, err
			}
			return &SelfA{}, nil
		})
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = scope.Close() })

		err = resolveWithin(t, func() error {
			_, resolveErr := Resolve[*SelfA](scope)
			return resolveErr
		})
		var cycleErr *CircularDependencyError
		require.ErrorAs(t, err, &cycleErr)
		assert.Contains(t, cycleErr.Error(), "SelfA")
	})

	t.Run("transient_self_resolution_through_resolver", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddTransient(func(s Resolver) (*SelfA, error) {
			if _, err := Resolve[*SelfA](s); err != nil {
				return nil, err
			}
			return &SelfA{}, nil
		})
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = scope.Close() })

		// Previously this recursed until the goroutine stack overflowed.
		err = resolveWithin(t, func() error {
			_, resolveErr := Resolve[*SelfA](scope)
			return resolveErr
		})
		var cycleErr *CircularDependencyError
		require.ErrorAs(t, err, &cycleErr)
	})

	t.Run("indirect_cycle_through_resolver", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddScoped(func(s Resolver) (*SelfA, error) {
			_, err := Resolve[*SelfB](s)
			return &SelfA{}, err
		})
		c.AddScoped(func(s Resolver) (*SelfB, error) {
			_, err := Resolve[*SelfA](s)
			return &SelfB{}, err
		})
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = scope.Close() })

		err = resolveWithin(t, func() error {
			_, resolveErr := Resolve[*SelfA](scope)
			return resolveErr
		})
		var cycleErr *CircularDependencyError
		require.ErrorAs(t, err, &cycleErr)
		joined := strings.Join(cycleErr.Path, " -> ")
		assert.Contains(t, joined, "SelfA")
		assert.Contains(t, joined, "SelfB")
	})

	t.Run("singleton_self_resolution_through_resolver", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func(r Resolver) (*SelfA, error) {
			if _, err := Resolve[*SelfA](r); err != nil {
				return nil, err
			}
			return &SelfA{}, nil
		})

		err := resolveWithin(t, func() error {
			_, buildErr := c.Build()
			return buildErr
		})
		var cycleErr *CircularDependencyError
		require.ErrorAs(t, err, &cycleErr)
	})

	t.Run("cycle_across_goroutines", func(t *testing.T) {
		t.Parallel()
		// A and B resolve each other at runtime and are first requested from
		// different goroutines; each constructor waits until both have
		// started, so each nested resolution would wait on the other's
		// in-flight construction.
		startedA, startedB := make(chan struct{}), make(chan struct{})
		c := NewCollection()
		c.AddScoped(func(s Resolver) (*SelfA, error) {
			close(startedA)
			<-startedB
			_, err := Resolve[*SelfB](s)
			return &SelfA{}, err
		})
		c.AddScoped(func(s Resolver) (*SelfB, error) {
			close(startedB)
			<-startedA
			_, err := Resolve[*SelfA](s)
			return &SelfB{}, err
		})
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = scope.Close() })

		errA, errB := make(chan error, 1), make(chan error, 1)
		go func() { _, err := Resolve[*SelfA](scope); errA <- err }()
		go func() { _, err := Resolve[*SelfB](scope); errB <- err }()

		var results []error
		for _, ch := range []chan error{errA, errB} {
			select {
			case err := <-ch:
				results = append(results, err)
			case <-time.After(5 * time.Second):
				t.Fatal("deadlocked: each goroutine waits on the other's construction")
			}
		}
		var cycle *CircularDependencyError
		assert.True(t, errors.As(results[0], &cycle) || errors.As(results[1], &cycle),
			"the cycle is reported: %v / %v", results[0], results[1])
	})

	t.Run("stored_resolver_is_unrestricted_after_construction", func(t *testing.T) {
		t.Parallel()
		type Factory struct{ Resolver Resolver }
		c := NewCollection()
		c.AddTransient(func() *SelfA { return &SelfA{} })
		c.AddScoped(func(s Resolver) *Factory { return &Factory{Resolver: s} })
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = scope.Close() })

		factory, err := Resolve[*Factory](scope)
		require.NoError(t, err)
		// The construction is over: a stored Resolver resolves freely,
		// including the factory's own type.
		_, err = Resolve[*SelfA](factory.Resolver)
		require.NoError(t, err)
		again, err := Resolve[*Factory](factory.Resolver)
		require.NoError(t, err)
		assert.Same(t, factory, again)
	})

	t.Run("concurrent_resolution_is_not_a_cycle", func(t *testing.T) {
		t.Parallel()
		started := make(chan struct{})
		release := make(chan struct{})
		c := NewCollection()
		c.AddScoped(func(s Resolver) *SelfA {
			close(started)
			<-release
			return &SelfA{}
		})
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = scope.Close() })

		first := make(chan *SelfA, 1)
		go func() {
			a, _ := Resolve[*SelfA](scope)
			first <- a
		}()
		<-started
		second := make(chan *SelfA, 1)
		go func() {
			a, _ := Resolve[*SelfA](scope)
			second <- a
		}()
		close(release)

		// Another goroutine waiting on the in-flight construction is not
		// re-entrance: it shares the single instance.
		a1, a2 := <-first, <-second
		require.NotNil(t, a1)
		assert.Same(t, a1, a2)
	})
}

func TestConstructorErrors(t *testing.T) {
	t.Parallel()

	t.Run("propagates_constructor_errors", func(t *testing.T) {
		t.Parallel()
		expectedErr := errors.New("initialization failed")

		c := NewCollection()
		c.AddSingleton(func() (*TService, error) {
			return nil, expectedErr
		})

		_, err := c.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "initialization failed")
	})

	t.Run("recovers_from_panics", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func() *TService {
			panic("constructor panic")
		})

		_, err := c.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "panicked")
	})

	t.Run("scoped_constructor_panic_recovered", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddScoped(func() *TService {
			panic("scoped panic")
		})

		p, _ := c.Build()
		defer p.Close()

		scope, _ := p.CreateScope(context.Background())
		defer scope.Close()

		_, err := scope.Get(reflect.TypeFor[*TService]())
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

		_, err := scope.Get(reflect.TypeFor[*TScoped]())
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
		c.AddSingleton(func() {
			initialized.Store(true)
		})

		p, err := c.Build()
		require.NoError(t, err)
		defer p.Close()

		// Side effect should have run during build
		assert.True(t, initialized.Load())
	})

	t.Run("error_only_constructor_success", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func() error {
			return nil
		})

		_, err := c.Build()
		require.NoError(t, err)
	})

	t.Run("error_only_constructor_failure", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func() error {
			return errors.New("init failed")
		})

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
	c.AddSingleton(func() *Config { return &Config{DSN: "postgres://..."} })
	c.AddSingleton(func(cfg *Config) *DB { return &DB{Config: cfg} })
	c.AddSingleton(func(db *DB) *Cache { return &Cache{DB: db} })
	c.AddScoped(func(db *DB) *UserRepo { return &UserRepo{DB: db} })
	c.AddScoped(func(repo *UserRepo, cache *Cache) *UserSvc {
		return &UserSvc{Repo: repo, Cache: cache}
	})

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

// ----------------------------------------------------------------------------
// Audit regression tests. Each test demonstrates a real bug found during the
// codebase audit; they must remain green after the corresponding fix.
// ----------------------------------------------------------------------------

// TestScopeCloseRaceWithResolve forces the close-vs-resolve interleaving that
// panics with "assignment to entry in nil map" on the pre-fix tree. After the
// fix, every resolver sees either a valid instance or ErrScopeDisposed and
// scope.Close completes without panicking.
func TestScopeCloseRaceWithResolve(t *testing.T) {
	t.Parallel()

	const goroutines = 200

	type slowScoped struct{ id int }

	start := make(chan struct{})
	c := NewCollection()
	c.AddScoped(func() *slowScoped {
		<-start
		return &slowScoped{id: 1}
	})

	p, err := c.Build()
	require.NoError(t, err)
	defer p.Close()

	scope, err := p.CreateScope(context.Background())
	require.NoError(t, err)

	resolverDone := make(chan struct{}, goroutines)
	resolveErrs := make(chan error, goroutines)

	for range goroutines {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					resolveErrs <- fmt.Errorf("panic in Get: %v", r)
				}
				resolverDone <- struct{}{}
			}()
			_, err := scope.Get(reflect.TypeFor[*slowScoped]())
			if err != nil && !errors.Is(err, ErrScopeDisposed) {
				resolveErrs <- err
			}
		}()
	}

	closeResult := make(chan error, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				closeResult <- fmt.Errorf("panic in Close: %v", r)
			}
		}()
		closeResult <- scope.Close()
	}()

	close(start)

	for range goroutines {
		<-resolverDone
	}

	closeErr := <-closeResult
	require.NoError(t, closeErr, "Close must not panic or error")

	close(resolveErrs)
	for err := range resolveErrs {
		t.Errorf("unexpected resolver error: %v", err)
	}
}

// TestScopedSingleFlight asserts that concurrent first-resolves of the same
// Scoped service result in exactly one constructor invocation and identical
// returned pointers. Pre-fix: ctor runs N times and pointers diverge.
func TestScopedSingleFlight(t *testing.T) {
	t.Parallel()

	type sfSvc struct{ id int64 }

	const goroutines = 100
	var ctorCalls atomic.Int64

	c := NewCollection()
	c.AddScoped(func() *sfSvc {
		return &sfSvc{id: ctorCalls.Add(1)}
	})

	p, err := c.Build()
	require.NoError(t, err)
	defer p.Close()

	scope, err := p.CreateScope(context.Background())
	require.NoError(t, err)
	defer scope.Close()

	var wg sync.WaitGroup
	results := make([]*sfSvc, goroutines)
	start := make(chan struct{})

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			v, err := scope.Get(reflect.TypeFor[*sfSvc]())
			require.NoError(t, err)
			results[idx] = v.(*sfSvc)
		}(i)
	}
	close(start)
	wg.Wait()

	assert.Equal(t, int64(1), ctorCalls.Load(),
		"constructor must run exactly once (single-flight)")
	first := results[0]
	require.NotNil(t, first)
	for i, got := range results {
		assert.Samef(t, first, got, "resolver #%d got a different instance", i)
	}
}

// TestScopedMultiReturnSingleFlight covers a multi-return Scoped constructor.
// Concurrent resolves of the two output types must run the constructor
// exactly once.
func TestScopedMultiReturnSingleFlight(t *testing.T) {
	t.Parallel()

	type sfLeft struct{ n int64 }
	type sfRight struct{ n int64 }

	var ctorCalls atomic.Int64
	c := NewCollection()
	c.AddScoped(func() (*sfLeft, *sfRight) {
		n := ctorCalls.Add(1)
		return &sfLeft{n: n}, &sfRight{n: n}
	})

	p, err := c.Build()
	require.NoError(t, err)
	defer p.Close()

	scope, err := p.CreateScope(context.Background())
	require.NoError(t, err)
	defer scope.Close()

	const goroutines = 100
	var wg sync.WaitGroup
	start := make(chan struct{})

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			if idx%2 == 0 {
				_, err := scope.Get(reflect.TypeFor[*sfLeft]())
				require.NoError(t, err)
			} else {
				_, err := scope.Get(reflect.TypeFor[*sfRight]())
				require.NoError(t, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	assert.Equal(t, int64(1), ctorCalls.Load(),
		"multi-return Scoped ctor must run exactly once")
}

// TestScopedOutStructSingleFlight covers the Out-struct fan-out path. The
// constructor must run once even when many goroutines resolve different
// fields concurrently.
func TestScopedOutStructSingleFlight(t *testing.T) {
	t.Parallel()

	type sfOutA struct{ n int64 }
	type sfOutB struct{ n int64 }
	type sfOutResult struct {
		Out
		A *sfOutA
		B *sfOutB
	}

	var ctorCalls atomic.Int64
	c := NewCollection()
	c.AddScoped(func() sfOutResult {
		n := ctorCalls.Add(1)
		return sfOutResult{A: &sfOutA{n: n}, B: &sfOutB{n: n}}
	})

	p, err := c.Build()
	require.NoError(t, err)
	defer p.Close()

	scope, err := p.CreateScope(context.Background())
	require.NoError(t, err)
	defer scope.Close()

	const goroutines = 100
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			if idx%2 == 0 {
				_, err := scope.Get(reflect.TypeFor[*sfOutA]())
				require.NoError(t, err)
			} else {
				_, err := scope.Get(reflect.TypeFor[*sfOutB]())
				require.NoError(t, err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	assert.Equal(t, int64(1), ctorCalls.Load(),
		"Out-struct Scoped ctor must run exactly once")
}

// panickyDisposable's Close() panics; recordingDisposable's Close() records
// that it ran. Used to assert teardown is panic-isolated.
type panickyDisposable struct{ name string }

func (p *panickyDisposable) Close() error {
	panic("intentional panic in " + p.name)
}

type recordingDisposable struct{ closed atomic.Bool }

func (r *recordingDisposable) Close() error {
	r.closed.Store(true)
	return nil
}

// TestScopeCloseSurvivesDisposablePanic: a panicking disposable Close must not
// stop the remaining disposables from being released, and scope.Close itself
// must not propagate the panic.
func TestScopeCloseSurvivesDisposablePanic(t *testing.T) {
	t.Parallel()

	c := NewCollection()
	c.AddTransient(func() *panickyDisposable {
		return &panickyDisposable{name: "boom"}
	})
	c.AddTransient(func() *recordingDisposable {
		return &recordingDisposable{}
	})

	p, err := c.Build()
	require.NoError(t, err)
	defer p.Close()

	scope, err := p.CreateScope(context.Background())
	require.NoError(t, err)

	// Resolve recording first (index 0 in disposables), then panicky (index
	// 1). scope.Close iterates in reverse: panicky fires first; the fix must
	// recover and still Close() the recording disposable.
	rec, err := scope.Get(reflect.TypeFor[*recordingDisposable]())
	require.NoError(t, err)
	_, err = scope.Get(reflect.TypeFor[*panickyDisposable]())
	require.NoError(t, err)

	var closeErr error
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("scope.Close propagated panic: %v", r)
			}
		}()
		closeErr = scope.Close()
	}()

	require.Error(t, closeErr)
	var disposalErr *DisposalError
	assert.ErrorAs(t, closeErr, &disposalErr)
	assert.True(t, rec.(*recordingDisposable).closed.Load(),
		"recording disposable must be closed despite the earlier panic")
}

// TestTransientResolveArgsAreScratchPooled asserts the per-resolve allocation
// count for a multi-arg Transient stays at or below the post-pool target.
// Pre-fix this is 3 allocs/op (args slice + Call's return slice + the
// instance boxing). After the args slice is pooled in
// internal/reflection.ConstructorInvoker.buildArguments, the args allocation
// goes away and we're at 2.
func TestTransientResolveArgsAreScratchPooled(t *testing.T) {
	// Intentionally not parallel: testing.AllocsPerRun cannot be called
	// from a parallel test.

	type leafA struct{}
	type leafB struct{}
	type leafC struct{}
	type composite struct {
		a *leafA
		b *leafB
		c *leafC
	}

	c := NewCollection()
	c.AddSingleton(func() *leafA { return &leafA{} })
	c.AddSingleton(func() *leafB { return &leafB{} })
	c.AddSingleton(func() *leafC { return &leafC{} })
	c.AddTransient(func(a *leafA, b *leafB, cc *leafC) *composite {
		return &composite{a: a, b: b, c: cc}
	})

	p, err := c.Build()
	require.NoError(t, err)
	defer p.Close()

	scope, err := p.CreateScope(context.Background())
	require.NoError(t, err)
	defer scope.Close()

	tgt := reflect.TypeFor[*composite]()
	_, err = scope.Get(tgt) // warmup
	require.NoError(t, err)

	allocs := testing.AllocsPerRun(2000, func() {
		_, _ = scope.Get(tgt)
	})

	// After the args-slice pool fix we expect <= 2 allocs/op for a 3-arg
	// Transient resolve. The baseline pre-fix is 3.
	assert.LessOrEqualf(t, allocs, 2.0,
		"resolve allocs/op = %.2f; expected the pooled args slice to keep it <= 2",
		allocs)
}

// TestResolveDoesNotReanalyzeConstructor verifies that resolving a service
// does not call analyzer.Analyze on the hot path. Pre-fix, scope.createInstance
// re-analyzes the constructor on every resolution (a cache hit, but still
// extra lock-acquisition and interface boxing). Post-fix, the analysis is
// stashed on the Descriptor at build time and the resolver reads it directly.
func TestResolveDoesNotReanalyzeConstructor(t *testing.T) {
	t.Parallel()

	type svc struct{ n int }

	col := NewCollection().(*collection)
	col.AddTransient(func() *svc { return &svc{n: 1} })

	p, err := col.Build()
	require.NoError(t, err)
	defer p.Close()

	scope, err := p.CreateScope(context.Background())
	require.NoError(t, err)
	defer scope.Close()

	// Warm up the resolver path.
	_, err = scope.Get(reflect.TypeFor[*svc]())
	require.NoError(t, err)

	before := col.analyzer.AnalyzeCalls()
	const iterations = 50
	for range iterations {
		_, err := scope.Get(reflect.TypeFor[*svc]())
		require.NoError(t, err)
	}
	delta := col.analyzer.AnalyzeCalls() - before

	assert.Zero(t, delta,
		"resolution must not re-Analyze the constructor (got %d extra calls)", delta)
}

func TestScopedSharedConstructorConcurrentNames(t *testing.T) {
	t.Parallel()

	for range 200 {
		c := NewCollection()
		c.AddScoped(NewTService, Name("one"))
		c.AddScoped(NewTService, Name("two"))

		p, err := c.Build()
		require.NoError(t, err)

		s, err := p.CreateScope(context.Background())
		require.NoError(t, err)

		var wg sync.WaitGroup
		errs := make([]error, 2)
		svcs := make([]*TService, 2)
		wg.Add(2)
		go func() { defer wg.Done(); svcs[0], errs[0] = ResolveKeyed[*TService](s, "one") }()
		go func() { defer wg.Done(); svcs[1], errs[1] = ResolveKeyed[*TService](s, "two") }()
		wg.Wait()

		require.NoError(t, errs[0])
		require.NoError(t, errs[1])
		require.NotSame(t, svcs[0], svcs[1], "each named registration must produce its own instance")

		_ = s.Close()
		_ = p.Close()
	}
}

func TestCreateChildScopeRacingParentClose(t *testing.T) {
	t.Parallel()

	for range 500 {
		c := NewCollection()
		c.AddScoped(NewTService)
		p, err := c.Build()
		require.NoError(t, err)

		parent, err := p.CreateScope(context.Background())
		require.NoError(t, err)

		var wg sync.WaitGroup
		var panicked any
		var child Scope
		var createErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			defer func() { panicked = recover() }()
			child, createErr = parent.CreateScope(context.Background())
		}()
		go func() { defer wg.Done(); _ = parent.Close() }()
		wg.Wait()

		require.Nil(t, panicked, "child CreateScope racing parent Close must not panic")

		// Invariant: once parent.Close has returned, a successfully created
		// child either was closed by the parent or is closed by us now; it
		// must never leak half-tracked.
		if createErr == nil {
			require.NotNil(t, child)
			_ = child.Close()
			_, err := child.Get(reflect.TypeFor[*TService]())
			require.ErrorIs(t, err, ErrScopeDisposed, "child must be disposed after parent close")
		}

		// The provider must not retain a leaked tracking entry: every scope
		// is closed at this point, and closing removes the scope from the
		// provider's map. (Checked before p.Close, which nils the map.)
		prov := p.(*provider)
		prov.scopesMu.Lock()
		leaked := len(prov.scopes)
		prov.scopesMu.Unlock()
		require.Zero(t, leaked, "no scope may leak into provider tracking")

		_ = p.Close()
	}
}

func TestScopeInitFailureCleansUpPartialState(t *testing.T) {
	t.Parallel()

	var captured *TDisposable
	initCalls := 0

	c := NewCollection()
	c.AddScoped(NewTDisposable)
	// First void-return initializer: resolves the disposable (gets tracked).
	c.AddScoped(func(d *TDisposable) {
		captured = d
	})
	// Second void-return initializer: succeeds for the root scope, which runs
	// it at Build, and fails for every scope created after.
	c.AddScoped(func() error {
		initCalls++
		if initCalls == 1 {
			return nil
		}
		return errors.New("init failure")
	})

	p, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	captured = nil

	ctx := t.Context()
	s, err := p.CreateScope(ctx)
	require.Error(t, err)
	require.Nil(t, s)

	require.NotNil(t, captured, "first initializer should have run")
	assert.True(t, captured.IsClosed(), "instances created before the failing initializer must be disposed")
	select {
	case <-ctx.Done():
		t.Error("parent context must not be cancelled by a failed CreateScope")
	default:
	}
}

func TestNewScopeFailureCancelsDerivedContext(t *testing.T) {
	t.Parallel()

	// Succeeds for the root scope, which runs it at Build, then fails.
	var calls atomic.Int32
	c := NewCollection()
	c.AddScoped(func() error {
		if calls.Add(1) == 1 {
			return nil
		}
		return errors.New("init failure")
	})

	pAny, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pAny.Close() })
	p := pAny.(*provider)

	ctx, cancel := context.WithCancel(context.Background())
	s, err := newScope(p, nil, ctx, cancel, false)
	require.Error(t, err)
	require.Nil(t, s)

	select {
	case <-ctx.Done():
		// The derived (cancellable) context must be released on failure.
	default:
		t.Error("newScope failure leaked its cancellable context")
	}
}

func TestScopeAccessors(t *testing.T) {
	t.Parallel()

	c := NewCollection()
	c.AddScoped(NewTService)
	p, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	s, err := p.CreateScope(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	// ID() is non-empty and unique per scope.
	assert.NotEmpty(t, s.ID())
	s2, err := p.CreateScope(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })
	assert.NotEqual(t, s.ID(), s2.ID())
}

func TestScopeCancellationCleanup(t *testing.T) {
	t.Parallel()

	t.Run("close_error_remains_observable", func(t *testing.T) {
		t.Parallel()
		closeErr := errors.New("cancel cleanup failed")
		disposable := NewTDisposable()
		disposable.SetCloseError(closeErr)

		c := NewCollection()
		c.AddScoped(func() *TDisposable { return disposable })
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		ctx, cancel := context.WithCancel(context.Background())
		s, err := p.CreateScope(ctx)
		require.NoError(t, err)
		_, err = Resolve[*TDisposable](s)
		require.NoError(t, err)

		// Cancellation leaves disposal to the owner, whose Close reports
		// the cleanup error.
		cancel()
		assert.False(t, disposable.IsClosed())
		require.ErrorIs(t, s.Close(), closeErr)
		assert.True(t, disposable.IsClosed())
	})

	t.Run("create_scope_rejects_canceled_context", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		s, err := p.CreateScope(ctx)
		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, s)
	})
}
