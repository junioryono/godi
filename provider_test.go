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

// BuildProvider builds a provider from the given modules and closes it when
// the test ends.
func BuildProvider(t *testing.T, opts ...ModuleOption) Provider {
	t.Helper()
	c := NewCollection()
	if len(opts) > 0 {
		c.AddModules(opts...)
	}
	p, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// NewTestScope creates a scope of p and closes it when the test ends.
func NewTestScope(t *testing.T, p Provider) Scope {
	t.Helper()
	s, err := p.CreateScope(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

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

			_, err := p.Get(reflect.TypeFor[*TService]())
			assert.ErrorIs(t, err, ErrProviderDisposed)

			_, err = p.GetKeyed(reflect.TypeFor[*TService](), "key")
			assert.ErrorIs(t, err, ErrProviderDisposed)

			_, err = p.GetGroup(reflect.TypeFor[*TService](), "group")
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

		// Only a root scope without scope validation acts as a scope.
		p, err := c.Build(WithScopeValidation(false))
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

func (o *recordingObserver) observer() Observer {
	return Observer{Constructed: o.Constructed, Disposed: o.Disposed}
}

// resolverOnly exposes only the Resolver methods of a Provider.
type resolverOnly struct{ p Provider }

func (r resolverOnly) Get(t reflect.Type) (any, error)                  { return r.p.Get(t) }
func (r resolverOnly) GetKeyed(t reflect.Type, k any) (any, error)      { return r.p.GetKeyed(t, k) }
func (r resolverOnly) GetGroup(t reflect.Type, g string) ([]any, error) { return r.p.GetGroup(t, g) }

// funcCloser adapts a function to Disposable.
type funcCloser struct{ close func() error }

func (c funcCloser) Close() error { return c.close() }

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
		synctest.Test(t, func(t *testing.T) {
			closeErr := errors.New("provider close failed")
			disposable := &blockingDisposable{
				started: make(chan struct{}),
				release: make(chan struct{}),
				err:     closeErr,
			}

			c := NewCollection()
			c.AddSingleton(func() *blockingDisposable { return disposable })
			p, err := c.Build()
			require.NoError(t, err)

			firstResult := make(chan error, 1)
			go func() { firstResult <- p.Close() }()
			<-disposable.started

			secondResult := make(chan error, 1)
			go func() { secondResult <- p.Close() }()

			// Once every goroutine is durably blocked, the second Close must
			// be waiting for the first one's cleanup rather than returning.
			synctest.Wait()
			select {
			case err := <-secondResult:
				t.Fatalf("second Close returned before cleanup completed: %v", err)
			default:
			}

			close(disposable.release)
			require.ErrorIs(t, <-firstResult, closeErr)
			require.ErrorIs(t, <-secondResult, closeErr)
			assert.Equal(t, int64(1), disposable.calls.Load())
		})
	})

	t.Run("scope_close_waits_and_returns_same_error", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
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
			defer func() { _ = p.Close() }()

			s, err := p.CreateScope(context.Background())
			require.NoError(t, err)
			_, err = Resolve[*blockingDisposable](s)
			require.NoError(t, err)

			firstResult := make(chan error, 1)
			go func() { firstResult <- s.Close() }()
			<-disposable.started

			secondResult := make(chan error, 1)
			go func() { secondResult <- s.Close() }()

			// Once every goroutine is durably blocked, the second Close must
			// be waiting for the first one's cleanup rather than returning.
			synctest.Wait()
			select {
			case err := <-secondResult:
				t.Fatalf("second Close returned before cleanup completed: %v", err)
			default:
			}

			close(disposable.release)
			require.ErrorIs(t, <-firstResult, closeErr)
			require.ErrorIs(t, <-secondResult, closeErr)
			assert.Equal(t, int64(1), disposable.calls.Load())
		})
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
