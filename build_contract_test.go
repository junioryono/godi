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

func TestBuildCancellation(t *testing.T) {
	t.Parallel()

	// The build context is cancelled by the constructors themselves rather
	// than by racing a wall-clock deadline against Build's setup, so these
	// tests cannot flake on a loaded runner.

	t.Run("cancellation_fails_build_after_constructor_returns", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		constructorReturned := false
		c := NewCollection()
		c.AddSingleton(func() *TService {
			cancel()
			constructorReturned = true
			return NewTService()
		})

		p, err := c.BuildWithContext(ctx)

		// Build waits for the non-cooperative constructor and still fails on
		// the cancellation it cannot deliver mid-flight.
		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, p)
		assert.True(t, constructorReturned)
	})

	t.Run("constructor_observes_deadline_cancellation", func(t *testing.T) {
		t.Parallel()
		observedCancellation := false
		c := NewCollection()
		c.AddSingleton(func(ctx context.Context) (*TService, error) {
			// Cooperative constructor: block until the build deadline fires.
			<-ctx.Done()
			observedCancellation = true
			return nil, ctx.Err()
		})

		p, err := c.BuildWithOptions(&ProviderOptions{BuildTimeout: 200 * time.Millisecond})

		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Nil(t, p)
		assert.True(t, observedCancellation)
	})

	t.Run("cancellation_cleans_partial_singletons", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		resource := NewTDisposable()
		c := NewCollection()
		c.AddSingleton(func() *TDisposable { return resource })
		c.AddSingleton(func(*TDisposable) *TService {
			cancel()
			return NewTService()
		})

		p, err := c.BuildWithContext(ctx)
		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, p)
		assert.True(t, resource.IsClosed())
	})

	t.Run("cancellation_error_remains_primary_when_cleanup_fails", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cleanupErr := errors.New("cleanup failed")
		c := NewCollection()
		c.AddSingleton(func() *TDisposable {
			d := NewTDisposable()
			d.SetCloseError(cleanupErr)
			return d
		})
		c.AddSingleton(func(*TDisposable) *TService {
			cancel()
			return NewTService()
		})

		p, err := c.BuildWithContext(ctx)
		require.Error(t, err)
		assert.Nil(t, p)
		assert.ErrorIs(t, err, context.Canceled)
		assert.ErrorIs(t, err, cleanupErr)
	})
}

func TestBuildContext(t *testing.T) {
	t.Parallel()

	t.Run("visible_to_eager_constructors", func(t *testing.T) {
		t.Parallel()
		type contextKey struct{}
		want := &TService{ID: "build-context"}
		buildCtx := context.WithValue(context.Background(), contextKey{}, want)

		c := NewCollection()
		c.AddSingleton(func(ctx context.Context) (*TService, error) {
			if _, err := FromContext(ctx); err != nil {
				return nil, err
			}
			if ctx.Value(contextKey{}) != want {
				return nil, errors.New("build context value was not preserved")
			}
			return want, nil
		})

		p, err := c.BuildWithContext(buildCtx)
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
	})

	t.Run("override_is_race_safe", func(t *testing.T) {
		t.Parallel()
		started := make(chan struct{})
		stop := make(chan struct{})
		var readers sync.WaitGroup

		c := NewCollection()
		c.AddSingleton(func(p Provider) *TService {
			readers.Go(func() {
				close(started)
				for {
					select {
					case <-stop:
						return
					default:
						_, _ = p.Get(contextType)
					}
				}
			})
			<-started
			return NewTService()
		})

		p, err := c.BuildWithContext(context.Background())
		close(stop)
		readers.Wait()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
	})
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
