package godi

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

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

func TestRootScopeInitializers(t *testing.T) {
	t.Parallel()

	t.Run("run_after_singleton_creation", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int64
		c := NewCollection()
		c.AddSingleton(NewTDependency)
		c.AddScoped(func(*TDependency) {
			calls.Add(1)
		})

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		assert.Equal(t, int64(1), calls.Load(), "root scope initializer should run during Build")

		s, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		require.NoError(t, s.Close())
		assert.Equal(t, int64(2), calls.Load(), "request scope initializer should run after singleton availability")
	})
}

type closeAliasA interface {
	AliasA()
}

type closeAliasB interface {
	AliasB()
}

type countedAliasDisposable struct {
	closeCalls atomic.Int64
}

func (d *countedAliasDisposable) Close() error {
	d.closeCalls.Add(1)
	return nil
}

// countedValueDisposable is deliberately a value type: value disposables are
// deduplicated per produced value, not per referenced counter.
type countedValueDisposable struct {
	closeCalls *atomic.Int64
}

func (d countedValueDisposable) Close() error {
	d.closeCalls.Add(1)
	return nil
}

func TestDisposableCloseDeduplication(t *testing.T) {
	t.Parallel()

	t.Run("aliased_pointer_closes_once", func(t *testing.T) {
		t.Parallel()
		disposable := &countedAliasDisposable{}
		c := NewCollection()
		c.AddSingleton(func() *countedAliasDisposable { return disposable }, As[closeAliasA](), As[closeAliasB]())

		p, err := c.Build()
		require.NoError(t, err)
		require.NoError(t, p.Close())
		assert.Equal(t, int64(1), disposable.closeCalls.Load())
	})

	t.Run("multi_return_same_pointer_closes_once", func(t *testing.T) {
		t.Parallel()
		disposable := &countedAliasDisposable{}
		c := NewCollection()
		c.AddSingleton(func() (closeAliasA, closeAliasB) {
			return disposable, disposable
		})

		p, err := c.Build()
		require.NoError(t, err)
		require.NoError(t, p.Close())
		assert.Equal(t, int64(1), disposable.closeCalls.Load())
	})

	t.Run("independent_equal_values_both_close", func(t *testing.T) {
		t.Parallel()
		var closeCalls atomic.Int64
		value := countedValueDisposable{closeCalls: &closeCalls}

		c := NewCollection()
		c.AddSingleton(func() (closeAliasA, closeAliasB) {
			return value, value
		})

		p, err := c.Build()
		require.NoError(t, err)
		require.NoError(t, p.Close())
		assert.Equal(t, int64(2), closeCalls.Load())
	})

	t.Run("orphaned_shared_value_closes_once", func(t *testing.T) {
		t.Parallel()
		disposable := &countedAliasDisposable{}
		ctorStarted := make(chan struct{})
		release := make(chan struct{})

		c := NewCollection()
		c.AddScoped(func() (closeAliasA, closeAliasB) {
			close(ctorStarted)
			<-release
			return disposable, disposable
		})
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		s, err := p.CreateScope(context.Background())
		require.NoError(t, err)

		resolveDone := make(chan struct{})
		go func() {
			defer close(resolveDone)
			_, _ = Resolve[closeAliasA](s)
		}()

		// Close the scope while the constructor is still running, then let it
		// finish: both sibling registrations orphan the same value, which must
		// still be closed exactly once.
		<-ctorStarted
		require.NoError(t, s.Close())
		close(release)
		<-resolveDone

		assert.Equal(t, int64(1), disposable.closeCalls.Load())
	})

	t.Run("disposable_tracked_after_provider_close_is_closed_once", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		p, err := c.Build()
		require.NoError(t, err)
		require.NoError(t, p.Close())

		// A constructor that outlives a cancelled Build registers its result
		// after Close; the orphan must be closed eagerly, and only once.
		disposable := &countedAliasDisposable{}
		p.(*provider).trackDisposable(disposable)
		assert.Equal(t, int64(1), disposable.closeCalls.Load())
		p.(*provider).trackDisposable(disposable)
		assert.Equal(t, int64(1), disposable.closeCalls.Load())
	})

	t.Run("aliased_value_closes_once", func(t *testing.T) {
		t.Parallel()
		var closeCalls atomic.Int64
		value := countedValueDisposable{closeCalls: &closeCalls}

		c := NewCollection()
		c.AddSingleton(func() countedValueDisposable { return value }, As[closeAliasA](), As[closeAliasB]())

		p, err := c.Build()
		require.NoError(t, err)
		require.NoError(t, p.Close())
		assert.Equal(t, int64(1), closeCalls.Load())
	})
}

// stuckCloser blocks Close until released.
type stuckCloser struct {
	release chan struct{}
	closed  atomic.Bool
}

func (c *stuckCloser) Close() error {
	<-c.release
	c.closed.Store(true)
	return nil
}

// ctxCloser records the context its Close(ctx) received.
type ctxCloser struct {
	deadline    time.Time
	hasDeadline bool
	calls       atomic.Int32
}

func (c *ctxCloser) Close(ctx context.Context) error {
	c.deadline, c.hasDeadline = ctx.Deadline()
	c.calls.Add(1)
	return nil
}

// gracefulServer models *http.Server: a graceful Shutdown(ctx) that can
// time out, plus an abrupt Close().
type gracefulServer struct {
	shutdownBlocks bool
	shutdowns      atomic.Int32
	closes         atomic.Int32
}

func (s *gracefulServer) Shutdown(ctx context.Context) error {
	s.shutdowns.Add(1)
	if s.shutdownBlocks {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func (s *gracefulServer) Close() error {
	s.closes.Add(1)
	return nil
}

// shutdownOnly has graceful shutdown but no Close.
type shutdownOnly struct{ shutdowns atomic.Int32 }

func (s *shutdownOnly) Shutdown(context.Context) error {
	s.shutdowns.Add(1)
	return nil
}

func TestShutdown(t *testing.T) {
	t.Parallel()

	t.Run("returns_when_the_deadline_passes", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			stuck := &stuckCloser{release: make(chan struct{})}
			c := NewCollection()
			c.AddSingleton(func() *stuckCloser { return stuck })
			p, err := c.Build()
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			// Close() has no bound: one stuck disposer would hang shutdown.
			err = Shutdown(ctx, p)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			var disposalErr *DisposalError
			require.ErrorAs(t, err, &disposalErr)
			assert.False(t, stuck.closed.Load())

			// Cleanup keeps running; Close waits for it to finish.
			close(stuck.release)
			require.NoError(t, p.Close())
			assert.True(t, stuck.closed.Load())
		})
	})

	t.Run("context_aware_closers_receive_the_shutdown_context", func(t *testing.T) {
		t.Parallel()
		closer := &ctxCloser{}
		c := NewCollection()
		c.AddSingleton(func() *ctxCloser { return closer })
		p, err := c.Build()
		require.NoError(t, err)

		deadline := time.Now().Add(time.Hour)
		ctx, cancel := context.WithDeadline(context.Background(), deadline)
		defer cancel()
		require.NoError(t, Shutdown(ctx, p))

		assert.Equal(t, int32(1), closer.calls.Load())
		assert.True(t, closer.hasDeadline)
		assert.Equal(t, deadline, closer.deadline)
	})

	t.Run("graceful_shutdown_falls_back_to_close_on_timeout", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			server := &gracefulServer{shutdownBlocks: true}
			c := NewCollection()
			c.AddSingleton(func() *gracefulServer { return server })
			p, err := c.Build()
			require.NoError(t, err)

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_ = Shutdown(ctx, p)
			require.NoError(t, p.Close())

			// The http.Server pattern: Shutdown(ctx), then Close() when the
			// graceful shutdown runs out of time.
			assert.Equal(t, int32(1), server.shutdowns.Load())
			assert.Equal(t, int32(1), server.closes.Load())
		})
	})

	t.Run("plain_close_keeps_using_close", func(t *testing.T) {
		t.Parallel()
		server := &gracefulServer{shutdownBlocks: true}
		c := NewCollection()
		c.AddSingleton(func() *gracefulServer { return server })
		p, err := c.Build()
		require.NoError(t, err)

		// Without a deadline, a graceful shutdown could wait forever.
		require.NoError(t, p.Close())
		assert.Zero(t, server.shutdowns.Load())
		assert.Equal(t, int32(1), server.closes.Load())
	})

	t.Run("shutdown_only_resources_are_disposed", func(t *testing.T) {
		t.Parallel()
		resource := &shutdownOnly{}
		c := NewCollection()
		c.AddSingleton(func() *shutdownOnly { return resource })
		p, err := c.Build()
		require.NoError(t, err)

		require.NoError(t, p.Close())
		assert.Equal(t, int32(1), resource.shutdowns.Load())
	})

	t.Run("scope_shutdown_disposes_children_first", func(t *testing.T) {
		t.Parallel()
		var order []string
		var mu sync.Mutex
		record := func(name string) func() error {
			return func() error {
				mu.Lock()
				defer mu.Unlock()
				order = append(order, name)
				return nil
			}
		}
		type ParentRes struct{ funcCloser }
		type ChildRes struct{ funcCloser }
		c := NewCollection()
		c.AddScoped(func() *ParentRes { return &ParentRes{funcCloser{record("parent")}} })
		c.AddScoped(func() *ChildRes { return &ChildRes{funcCloser{record("child")}} })
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		parent, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		_, err = Resolve[*ParentRes](parent)
		require.NoError(t, err)
		child, err := parent.CreateScope(context.Background())
		require.NoError(t, err)
		_, err = Resolve[*ChildRes](child)
		require.NoError(t, err)

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		require.NoError(t, Shutdown(ctx, parent))
		assert.Equal(t, []string{"child", "parent"}, order)
	})

	t.Run("bounds_any_disposable", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			stuck := &stuckCloser{release: make(chan struct{})}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.ErrorIs(t, Shutdown(ctx, stuck), context.DeadlineExceeded)
			close(stuck.release)
			synctest.Wait()
			assert.True(t, stuck.closed.Load())
		})
	})
}

func TestLazySingleton(t *testing.T) {
	t.Parallel()

	t.Run("created_on_first_resolution", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int32
		c := NewCollection()
		c.AddSingleton(func() *TService { calls.Add(1); return NewTService() }, Lazy())
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		assert.Zero(t, calls.Load(), "a lazy singleton is not created at Build")

		first, err := Resolve[*TService](p)
		require.NoError(t, err)
		second, err := Resolve[*TService](p)
		require.NoError(t, err)
		assert.Same(t, first, second)
		assert.Equal(t, int32(1), calls.Load())
	})

	t.Run("concurrent_first_resolution_constructs_once", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int32
		c := NewCollection()
		c.AddSingleton(func() *TService { calls.Add(1); return NewTService() }, Lazy())
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		var wg sync.WaitGroup
		results := make([]*TService, 16)
		for i := range results {
			wg.Go(func() {
				scope, err := p.CreateScope(context.Background())
				if !assert.NoError(t, err) {
					return
				}
				defer scope.Close()
				results[i], _ = Resolve[*TService](scope)
			})
		}
		wg.Wait()
		assert.Equal(t, int32(1), calls.Load())
		for _, r := range results {
			assert.Same(t, results[0], r)
		}
	})

	t.Run("a_failed_construction_is_retried", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int32
		c := NewCollection()
		c.AddSingleton(func() (*TService, error) {
			if calls.Add(1) == 1 {
				return nil, errors.New("not yet")
			}
			return NewTService(), nil
		}, Lazy())
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		_, err = Resolve[*TService](p)
		require.Error(t, err)
		_, err = Resolve[*TService](p)
		require.NoError(t, err, "failures are not cached")
	})

	t.Run("still_validated_at_build", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func(*TDependency) *TService { return NewTService() }, Lazy())
		_, err := c.Build()
		require.ErrorIs(t, err, ErrServiceNotFound)
	})

	t.Run("disposed_before_its_dependencies", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func() *orderedWriter { return &orderedWriter{} })
		c.AddSingleton(func(w *orderedWriter) *orderedBuffer { return &orderedBuffer{w: w} }, Lazy())
		p, err := c.Build()
		require.NoError(t, err)
		buf, err := Resolve[*orderedBuffer](p)
		require.NoError(t, err)
		require.NoError(t, p.Close())
		assert.True(t, buf.writerOpenAtClose)
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
		require.ErrorIs(t, Invoke(p, func(*TService) error { return boom }), boom)
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
	p := BuildProvider(t, AddSingleton(NewTService), AddScoped(NewTDependency, Name("named")))

	assert.True(t, IsService(p, PtrTypeOf[TService]()))
	assert.False(t, IsService(p, PtrTypeOf[TDependency]()), "only registered under a key")
	assert.True(t, IsKeyedService(p, PtrTypeOf[TDependency](), "named"))
	assert.False(t, IsKeyedService(p, PtrTypeOf[TDependency](), "other"))
	assert.True(t, IsService(p, reflect.TypeFor[Scope]()), "the container's own types are services")
	assert.True(t, IsService(p, reflect.TypeFor[context.Context]()))
}

// recordingObserver collects observer events.
type recordingObserver struct {
	mu          sync.Mutex
	constructed []ConstructedEvent
	disposed    []DisposedEvent
}

func (o *recordingObserver) Constructed(e *ConstructedEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.constructed = append(o.constructed, *e)
}

func (o *recordingObserver) Disposed(e *DisposedEvent) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.disposed = append(o.disposed, *e)
}

func TestObserver(t *testing.T) {
	t.Parallel()

	t.Run("construction_and_disposal", func(t *testing.T) {
		t.Parallel()
		obs := &recordingObserver{}
		closeErr := errors.New("close failed")
		c := NewCollection()
		c.AddSingleton(func() *TDisposable {
			d := NewTDisposable()
			d.SetCloseError(closeErr)
			return d
		})
		c.AddScoped(NewTServiceError)
		p, err := c.BuildWithOptions(&ProviderOptions{Observer: obs})
		require.NoError(t, err)

		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		_, err = Resolve[*TService](scope)
		require.Error(t, err)
		require.NoError(t, scope.Close())
		require.Error(t, p.Close())

		obs.mu.Lock()
		defer obs.mu.Unlock()
		require.Len(t, obs.constructed, 2)
		assert.Equal(t, PtrTypeOf[TDisposable](), obs.constructed[0].ServiceType)
		assert.Equal(t, Singleton, obs.constructed[0].Lifetime)
		assert.NoError(t, obs.constructed[0].Err)
		assert.Equal(t, PtrTypeOf[TService](), obs.constructed[1].ServiceType)
		assert.Equal(t, scope.ID(), obs.constructed[1].ScopeID)
		assert.Error(t, obs.constructed[1].Err)
		assert.Contains(t, obs.constructed[1].Constructor, "NewTServiceError")

		require.Len(t, obs.disposed, 1)
		assert.Equal(t, PtrTypeOf[TDisposable](), obs.disposed[0].Type)
		assert.ErrorIs(t, obs.disposed[0].Err, closeErr)
	})

	t.Run("orphan_cleanup_failures_are_observable", func(t *testing.T) {
		t.Parallel()
		obs := &recordingObserver{}
		closeErr := errors.New("orphan close failed")
		started := make(chan struct{})
		release := make(chan struct{})
		c := NewCollection()
		c.AddScoped(func() *TDisposable {
			close(started)
			<-release
			d := NewTDisposable()
			d.SetCloseError(closeErr)
			return d
		})
		p, err := c.BuildWithOptions(&ProviderOptions{Observer: obs})
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)

		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = Resolve[*TDisposable](scope)
		}()
		<-started
		require.NoError(t, scope.Close())
		close(release)
		<-done

		// The value was produced after its scope closed and is disposed
		// with no caller to report to; the failure used to vanish.
		obs.mu.Lock()
		defer obs.mu.Unlock()
		require.Len(t, obs.disposed, 1)
		assert.ErrorIs(t, obs.disposed[0].Err, closeErr)
	})
}

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

// resolverOnly exposes only the Resolver methods of a Provider.
type resolverOnly struct{ p Provider }

func (r resolverOnly) Get(t reflect.Type) (any, error)                  { return r.p.Get(t) }
func (r resolverOnly) GetKeyed(t reflect.Type, k any) (any, error)      { return r.p.GetKeyed(t, k) }
func (r resolverOnly) GetGroup(t reflect.Type, g string) ([]any, error) { return r.p.GetGroup(t, g) }

func TestRegistrationValuesAndKeys(t *testing.T) {
	t.Parallel()

	t.Run("instance_of_a_function_type", func(t *testing.T) {
		t.Parallel()
		type Clock func() time.Time
		fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		clock := Clock(func() time.Time { return fixed })

		// A function value was always taken for a constructor.
		c := NewCollection()
		c.AddSingleton(Instance(clock))
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		got, err := Resolve[Clock](p)
		require.NoError(t, err)
		assert.Equal(t, fixed, got())
	})

	t.Run("non_string_keys", func(t *testing.T) {
		t.Parallel()
		type Region int
		const eu, us Region = 1, 2
		c := NewCollection()
		c.AddSingleton(NewTServiceWithID("eu"), Key(eu))
		c.AddSingleton(NewTServiceWithID("us"), Key(us))
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		svc, err := ResolveKeyed[*TService](p, us)
		require.NoError(t, err)
		assert.Equal(t, "us", svc.ID)
		assert.True(t, c.ContainsKeyed(PtrTypeOf[TService](), eu))
	})

	t.Run("key_must_be_comparable", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService, Key([]int{1}))
		require.Error(t, c.Err())
	})
}

// funcCloser adapts a function to Disposable.
type funcCloser struct{ close func() error }

func (c funcCloser) Close() error { return c.close() }

func TestNoDispose(t *testing.T) {
	t.Parallel()

	t.Run("instance_supplied_by_the_caller", func(t *testing.T) {
		t.Parallel()
		shared := NewTDisposable()
		c := NewCollection()
		c.AddSingleton(shared, NoDispose())
		p, err := c.Build()
		require.NoError(t, err)

		require.NoError(t, p.Close())
		assert.False(t, shared.IsClosed(), "the caller owns a NoDispose registration")
	})

	t.Run("scoped_constructor", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddScoped(NewTDisposable, NoDispose())
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		d, err := Resolve[*TDisposable](scope)
		require.NoError(t, err)
		require.NoError(t, scope.Close())
		assert.False(t, d.IsClosed())
	})

	t.Run("interface_aliases", func(t *testing.T) {
		t.Parallel()
		disposable := &countedAliasDisposable{}
		c := NewCollection()
		c.AddSingleton(func() *countedAliasDisposable { return disposable },
			As[closeAliasA](), As[closeAliasB](), NoDispose())
		p, err := c.Build()
		require.NoError(t, err)
		require.NoError(t, p.Close())
		assert.Zero(t, disposable.closeCalls.Load())
	})
}

// orderedWriter and orderedBuffer model a singleton that flushes into its
// dependency when closed.
type orderedWriter struct{ TDisposable }

type orderedBuffer struct {
	w                 *orderedWriter
	writerOpenAtClose bool
}

func (b *orderedBuffer) Close() error {
	b.writerOpenAtClose = !b.w.IsClosed()
	return nil
}

func TestProviderDisposalOwnership(t *testing.T) {
	t.Parallel()

	t.Run("singleton_closes_before_its_transient_dependency", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddTransient(func() *orderedWriter { return &orderedWriter{} })
		c.AddSingleton(func(w *orderedWriter) *orderedBuffer { return &orderedBuffer{w: w} })

		p, err := c.Build()
		require.NoError(t, err)
		buf, err := Resolve[*orderedBuffer](p)
		require.NoError(t, err)

		// The root scope (holding the transient) used to be closed before
		// the singletons that depend on it.
		require.NoError(t, p.Close())
		assert.True(t, buf.writerOpenAtClose, "a singleton's dependency must still be open when the singleton closes")
		assert.True(t, buf.w.IsClosed(), "the dependency is closed after its consumer")
	})

	t.Run("transients_resolved_from_provider_are_caller_owned", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddTransient(NewTDisposable)
		p, err := c.Build()
		require.NoError(t, err)

		resolved := make([]*TDisposable, 0, 100)
		for range 100 {
			d, err := Resolve[*TDisposable](p)
			require.NoError(t, err)
			resolved = append(resolved, d)
		}

		// Tracking them for provider shutdown retained every resolution for
		// the provider's lifetime: an unbounded leak in a long-running app.
		pp := p.(*provider)
		pp.disposablesMu.Lock()
		tracked := len(pp.disposables)
		pp.disposablesMu.Unlock()
		pp.rootScope.disposablesMu.Lock()
		tracked += len(pp.rootScope.disposables)
		pp.rootScope.disposablesMu.Unlock()
		assert.Zero(t, tracked, "transients resolved from the provider must not accumulate")

		require.NoError(t, p.Close())
		for _, d := range resolved {
			assert.False(t, d.IsClosed(), "the caller owns transients it resolved from the provider")
		}
	})
}

// blockingDisposable blocks Close until released so tests can observe
// concurrent Close calls waiting on the same in-flight cleanup.
type blockingDisposable struct {
	started chan struct{}
	release chan struct{}
	err     error
	calls   atomic.Int64
}

func (d *blockingDisposable) Close() error {
	if d.calls.Add(1) == 1 {
		close(d.started)
	}
	<-d.release
	return d.err
}

func TestConcurrentClose(t *testing.T) {
	t.Parallel()

	t.Run("provider_close_waits_and_returns_same_error", func(t *testing.T) {
		t.Parallel()
		closeErr := errors.New("provider close failed")
		disposable := &blockingDisposable{
			started: make(chan struct{}),
			release: make(chan struct{}),
			err:     closeErr,
		}

		c := NewCollection()
		c.AddSingleton(disposable)
		p, err := c.Build()
		require.NoError(t, err)

		firstResult := make(chan error, 1)
		go func() { firstResult <- p.Close() }()
		<-disposable.started

		secondResult := make(chan error, 1)
		go func() { secondResult <- p.Close() }()

		select {
		case err := <-secondResult:
			t.Fatalf("second Close returned before cleanup completed: %v", err)
		case <-time.After(20 * time.Millisecond):
		}

		close(disposable.release)
		require.ErrorIs(t, <-firstResult, closeErr)
		require.ErrorIs(t, <-secondResult, closeErr)
		assert.Equal(t, int64(1), disposable.calls.Load())
	})

	t.Run("scope_close_waits_and_returns_same_error", func(t *testing.T) {
		t.Parallel()
		closeErr := errors.New("scope close failed")
		disposable := &blockingDisposable{
			started: make(chan struct{}),
			release: make(chan struct{}),
			err:     closeErr,
		}

		c := NewCollection()
		c.AddScoped(func() *blockingDisposable { return disposable })
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		s, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		_, err = Resolve[*blockingDisposable](s)
		require.NoError(t, err)

		firstResult := make(chan error, 1)
		go func() { firstResult <- s.Close() }()
		<-disposable.started

		secondResult := make(chan error, 1)
		go func() { secondResult <- s.Close() }()

		select {
		case err := <-secondResult:
			t.Fatalf("second Close returned before cleanup completed: %v", err)
		case <-time.After(20 * time.Millisecond):
		}

		close(disposable.release)
		require.ErrorIs(t, <-firstResult, closeErr)
		require.ErrorIs(t, <-secondResult, closeErr)
		assert.Equal(t, int64(1), disposable.calls.Load())
	})
}

func TestProviderCloseErrorAggregation(t *testing.T) {
	t.Parallel()

	t.Run("child_scope_error_reported_once", func(t *testing.T) {
		t.Parallel()
		closeErr := errors.New("child cleanup failed")
		c := NewCollection()
		c.AddScoped(func() *TDisposable {
			d := NewTDisposable()
			d.SetCloseError(closeErr)
			return d
		})

		p, err := c.Build()
		require.NoError(t, err)
		parent, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		child, err := parent.CreateScope(context.Background())
		require.NoError(t, err)
		_, err = Resolve[*TDisposable](child)
		require.NoError(t, err)

		err = p.Close()
		require.ErrorIs(t, err, closeErr)
		assert.Equal(t, 1, countErrorOccurrences(err, closeErr))
	})
}

func countErrorOccurrences(err, target error) int {
	if err == nil {
		return 0
	}
	if err == target {
		return 1
	}
	if multi, ok := err.(interface{ Unwrap() []error }); ok {
		count := 0
		for _, child := range multi.Unwrap() {
			count += countErrorOccurrences(child, target)
		}
		return count
	}
	return countErrorOccurrences(errors.Unwrap(err), target)
}

func (*countedAliasDisposable) AliasA() {}

func (*countedAliasDisposable) AliasB() {}

func (countedValueDisposable) AliasA() {}

func (countedValueDisposable) AliasB() {}
