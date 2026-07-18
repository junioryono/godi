package godi

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
		c.AddSingleton(NewTDisposable)
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

	t.Run("injects_scope", func(t *testing.T) {
		t.Parallel()
		type ScopeSvc struct{ Scope Scope }

		c := NewCollection()
		c.AddScoped(func(s Scope) *ScopeSvc {
			return &ScopeSvc{Scope: s}
		})

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
		c.AddSingleton(func(p Provider) *ProvSvc {
			return &ProvSvc{Prov: p}
		})

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
			_, err := scope.Get(PtrTypeOf[slowScoped]())
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
			v, err := scope.Get(PtrTypeOf[sfSvc]())
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
				_, err := scope.Get(PtrTypeOf[sfLeft]())
				require.NoError(t, err)
			} else {
				_, err := scope.Get(PtrTypeOf[sfRight]())
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
				_, err := scope.Get(PtrTypeOf[sfOutA]())
				require.NoError(t, err)
			} else {
				_, err := scope.Get(PtrTypeOf[sfOutB]())
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
	rec, err := scope.Get(PtrTypeOf[recordingDisposable]())
	require.NoError(t, err)
	_, err = scope.Get(PtrTypeOf[panickyDisposable]())
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

	tgt := PtrTypeOf[composite]()
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
	_, err = scope.Get(PtrTypeOf[svc]())
	require.NoError(t, err)

	before := col.analyzer.AnalyzeCalls()
	const iterations = 50
	for range iterations {
		_, err := scope.Get(PtrTypeOf[svc]())
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
			_, err := child.Get(PtrTypeOf[TService]())
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

func TestGetKeyedNonComparableKey(t *testing.T) {
	t.Parallel()

	c := NewCollection()
	c.AddSingleton(NewTService, Name("a"))
	p, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	// Both a directly non-comparable key and a comparable struct wrapping a
	// non-comparable value in an interface field (which passes a type-level
	// comparability check but panics as a map key).
	keys := []any{
		[]string{"not", "comparable"},
		struct{ V any }{V: []int{1}},
	}
	for _, key := range keys {
		require.NotPanics(t, func() {
			_, err := p.GetKeyed(PtrTypeOf[TService](), key)
			require.Error(t, err)
		})
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
	// Second void-return initializer: succeeds for the root scope (build),
	// fails for every subsequently created scope.
	c.AddScoped(func() error {
		initCalls++
		if initCalls > 1 {
			return errors.New("init failure")
		}
		return nil
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

	initCalls := 0
	c := NewCollection()
	c.AddScoped(func() error {
		initCalls++
		if initCalls > 1 {
			return errors.New("init failure")
		}
		return nil
	})

	pAny, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = pAny.Close() })
	p := pAny.(*provider)

	ctx, cancel := context.WithCancel(context.Background())
	s, err := newScope(p, nil, ctx, cancel)
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

	// Provider() returns the owning provider.
	assert.Same(t, p, s.Provider())

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

		cancel()
		select {
		case <-disposable.closeChan:
		case <-time.After(time.Second):
			t.Fatal("scope was not closed after cancellation")
		}

		require.ErrorIs(t, s.Close(), closeErr)
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
