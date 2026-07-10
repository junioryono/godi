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

	t.Run("timeout_after_slow_final_constructor", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func() *TService {
			time.Sleep(30 * time.Millisecond)
			return NewTService()
		})

		started := time.Now()
		p, err := c.BuildWithOptions(&ProviderOptions{BuildTimeout: 5 * time.Millisecond})
		elapsed := time.Since(started)

		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Nil(t, p)
		assert.GreaterOrEqual(t, elapsed, 30*time.Millisecond)
	})

	t.Run("constructor_observes_cancellation", func(t *testing.T) {
		t.Parallel()
		observedCancellation := make(chan struct{})
		c := NewCollection()
		c.AddSingleton(func(ctx context.Context) (*TService, error) {
			<-ctx.Done()
			close(observedCancellation)
			return nil, ctx.Err()
		})

		type buildResult struct {
			provider Provider
			err      error
		}
		result := make(chan buildResult, 1)
		go func() {
			p, err := c.BuildWithOptions(&ProviderOptions{BuildTimeout: 10 * time.Millisecond})
			result <- buildResult{provider: p, err: err}
		}()

		select {
		case <-observedCancellation:
		case <-time.After(2 * time.Second):
			t.Fatal("constructor did not observe build-context cancellation")
		}

		select {
		case got := <-result:
			require.ErrorIs(t, got.err, context.DeadlineExceeded)
			assert.Nil(t, got.provider)
		case <-time.After(2 * time.Second):
			t.Fatal("Build did not return after constructor cancellation")
		}
	})

	t.Run("timeout_cleans_partial_singletons", func(t *testing.T) {
		t.Parallel()
		resource := NewTDisposable()
		c := NewCollection()
		c.AddSingleton(func() *TDisposable { return resource })
		c.AddSingleton(func(*TDisposable) *TService {
			time.Sleep(30 * time.Millisecond)
			return NewTService()
		})

		p, err := c.BuildWithOptions(&ProviderOptions{BuildTimeout: 5 * time.Millisecond})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Nil(t, p)
		assert.True(t, resource.IsClosed())
	})

	t.Run("timeout_error_remains_primary_when_cleanup_fails", func(t *testing.T) {
		t.Parallel()
		cleanupErr := errors.New("cleanup failed")
		c := NewCollection()
		c.AddSingleton(func() *TDisposable {
			d := NewTDisposable()
			d.SetCloseError(cleanupErr)
			return d
		})
		c.AddSingleton(func(*TDisposable) *TService {
			time.Sleep(30 * time.Millisecond)
			return NewTService()
		})

		p, err := c.BuildWithOptions(&ProviderOptions{BuildTimeout: 5 * time.Millisecond})
		require.Error(t, err)
		assert.Nil(t, p)
		assert.ErrorIs(t, err, context.DeadlineExceeded)
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
