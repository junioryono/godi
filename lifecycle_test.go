package godi

import (
	"context"
	"errors"
	"io"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TDisposable is a disposable service. A second Close fails, so tests catch
// double disposal.
type TDisposable struct {
	Name     string
	closed   atomic.Bool
	mu       sync.Mutex
	closeErr error
}

func NewTDisposable() *TDisposable {
	return &TDisposable{Name: "disposable"}
}

func (d *TDisposable) Close() error {
	if d.closed.Swap(true) {
		return errors.New("already closed")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.closeErr
}

func (d *TDisposable) IsClosed() bool {
	return d.closed.Load()
}

// SetCloseError sets the error Close returns.
func (d *TDisposable) SetCloseError(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closeErr = err
}

// startRecorder collects Start calls in order.
type startRecorder struct {
	mu    sync.Mutex
	order []string
}

func (r *startRecorder) record(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.order = append(r.order, name)
}

type startableA struct {
	rec *startRecorder
	err error
}

func (s *startableA) Start(context.Context) error {
	s.rec.record("A")
	return s.err
}

type startableB struct{ rec *startRecorder }

func (s *startableB) Start(context.Context) error {
	s.rec.record("B")
	return nil
}

func TestStart(t *testing.T) {
	t.Parallel()

	t.Run("starts_singletons_in_creation_order", func(t *testing.T) {
		t.Parallel()
		rec := &startRecorder{}
		c := NewCollection()
		c.AddSingleton(func(*startableA) *startableB { return &startableB{rec: rec} })
		c.AddSingleton(func() *startableA { return &startableA{rec: rec} })
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		require.NoError(t, Start(context.Background(), p))
		assert.Equal(t, []string{"A", "B"}, rec.order, "dependencies start first")
	})

	t.Run("stops_at_the_first_failure", func(t *testing.T) {
		t.Parallel()
		rec := &startRecorder{}
		boom := errors.New("port in use")
		c := NewCollection()
		c.AddSingleton(func() *startableA { return &startableA{rec: rec, err: boom} })
		c.AddSingleton(func() *startableB { return &startableB{rec: rec} })
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		err = Start(context.Background(), p)
		require.ErrorIs(t, err, boom)
		assert.Contains(t, err.Error(), "startableA")
		assert.Equal(t, []string{"A"}, rec.order)
	})

	t.Run("runs_once", func(t *testing.T) {
		t.Parallel()
		rec := &startRecorder{}
		c := NewCollection()
		c.AddSingleton(func() *startableB { return &startableB{rec: rec} })
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		require.NoError(t, Start(context.Background(), p))
		require.Error(t, Start(context.Background(), p))
		assert.Equal(t, []string{"B"}, rec.order)
	})

	t.Run("does_not_create_lazy_singletons", func(t *testing.T) {
		t.Parallel()
		rec := &startRecorder{}
		c := NewCollection()
		c.AddSingleton(func() *startableB { return &startableB{rec: rec} }, Lazy())
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		require.NoError(t, Start(context.Background(), p))
		assert.Empty(t, rec.order)
	})
}

// valueStarter is a non-pointer Starter: its copies cannot be deduplicated
// by identity.
type valueStarter struct{ rec *startRecorder }

func (s valueStarter) Start(context.Context) error {
	s.rec.record("value")
	return nil
}

type startIface1 interface{ Start(context.Context) error }
type startIface2 interface{ Start(context.Context) error }

func TestStartOncePerService(t *testing.T) {
	t.Parallel()
	rec := &startRecorder{}
	c := NewCollection()
	c.AddSingleton(func() valueStarter { return valueStarter{rec: rec} },
		As[startIface1](), As[startIface2]())
	c.AddModules(Decorate(func(s startIface2) startIface2 { return s }))
	p, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	require.NoError(t, Start(context.Background(), p))
	assert.Equal(t, []string{"value"}, rec.order, "one construction is started once, whatever its aliases")
}

type healthyService struct{}

func (healthyService) HealthCheck(context.Context) error { return nil }

type failingService struct{ err error }

func (s *failingService) HealthCheck(context.Context) error { return s.err }

type slowService struct{}

func (slowService) HealthCheck(ctx context.Context) error {
	<-ctx.Done()
	return ctx.Err()
}

func TestHealthCheck(t *testing.T) {
	t.Parallel()

	t.Run("healthy", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t, AddSingleton(func() healthyService { return healthyService{} }))
		require.NoError(t, HealthCheck(context.Background(), p))
	})

	t.Run("reports_every_failure", func(t *testing.T) {
		t.Parallel()
		down := errors.New("database unreachable")
		p := BuildProvider(t,
			AddSingleton(func() healthyService { return healthyService{} }),
			AddSingleton(func() *failingService { return &failingService{err: down} }),
		)
		err := HealthCheck(context.Background(), p)
		require.ErrorIs(t, err, down)
		assert.Contains(t, err.Error(), "failingService")
	})

	t.Run("bounded_by_the_context", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			p := BuildProvider(t, AddSingleton(func() slowService { return slowService{} }))
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			require.ErrorIs(t, HealthCheck(ctx, p), context.DeadlineExceeded)
		})
	})
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

	t.Run("matches_the_context_error_with_a_custom_cause", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			stuck := &stuckCloser{release: make(chan struct{})}
			c := NewCollection()
			c.AddSingleton(func() *stuckCloser { return stuck })
			p, err := c.Build()
			require.NoError(t, err)

			reason := errors.New("terminating")
			ctx, cancel := context.WithCancelCause(context.Background())
			cancel(reason)
			err = Shutdown(ctx, p)
			assert.ErrorIs(t, err, context.Canceled, "the context error must stay matchable")
			assert.ErrorIs(t, err, reason)
			close(stuck.release)
			require.NoError(t, p.Close())
		})
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

// The caller created a value registered as an instance, so the caller closes
// it; godi disposes only what its constructors create.
func TestInstanceRegistrationsAreNotDisposed(t *testing.T) {
	t.Parallel()

	t.Run("instance", func(t *testing.T) {
		t.Parallel()
		shared := NewTDisposable()
		c := NewCollection()
		c.AddSingleton(shared)
		p, err := c.Build()
		require.NoError(t, err)

		require.NoError(t, p.Close())
		assert.False(t, shared.IsClosed())
	})

	t.Run("instance_wrapper", func(t *testing.T) {
		t.Parallel()
		shared := NewTDisposable()
		c := NewCollection()
		c.AddSingleton(Instance(shared))
		p, err := c.Build()
		require.NoError(t, err)

		require.NoError(t, p.Close())
		assert.False(t, shared.IsClosed())
	})

	t.Run("a_constructor_returning_it_hands_ownership_to_godi", func(t *testing.T) {
		t.Parallel()
		shared := NewTDisposable()
		c := NewCollection()
		c.AddSingleton(func() *TDisposable { return shared })
		p, err := c.Build()
		require.NoError(t, err)

		require.NoError(t, p.Close())
		assert.True(t, shared.IsClosed())
	})
}

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

	t.Run("transient_is_not_adopted_by_a_scoped_consumer", func(t *testing.T) {
		t.Parallel()
		external := NewTDisposable()
		c := NewCollection()
		c.AddTransient(func() *TDisposable { return external }, NoDispose())
		// A scoped service that hands out the externally owned value.
		c.AddScoped(func(d *TDisposable) io.Closer { return d })
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		_, err = Resolve[io.Closer](scope)
		require.NoError(t, err)
		require.NoError(t, scope.Close())
		assert.False(t, external.IsClosed(), "a NoDispose value must not be adopted")
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

// Not parallel: it counts goroutines.
func TestRepeatedShutdownDoesNotLeakGoroutines(t *testing.T) {
	stuck := &stuckCloser{release: make(chan struct{})}
	c := NewCollection()
	c.AddSingleton(func() *stuckCloser { return stuck })
	p, err := c.Build()
	require.NoError(t, err)

	shutdownWithin := func(d time.Duration) {
		ctx, cancel := context.WithTimeout(context.Background(), d)
		defer cancel()
		require.ErrorIs(t, Shutdown(ctx, p), context.DeadlineExceeded)
	}

	shutdownWithin(time.Millisecond) // starts the (stuck) teardown
	before := runtime.NumGoroutine()
	for range 20 {
		shutdownWithin(time.Millisecond)
	}
	// Each timed-out Shutdown used to leave a goroutine waiting for the
	// stuck teardown.
	assert.Less(t, runtime.NumGoroutine()-before, 5)

	close(stuck.release)
	require.NoError(t, p.Close())
}
