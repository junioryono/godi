package godi

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
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
