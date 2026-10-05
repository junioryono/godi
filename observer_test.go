package godi

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		p, err := c.Build(WithObserver(obs.observer()))
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
		assert.Equal(t, reflect.TypeFor[*TDisposable](), obs.constructed[0].ServiceType)
		assert.Equal(t, Singleton, obs.constructed[0].Lifetime)
		assert.NoError(t, obs.constructed[0].Err)
		assert.Equal(t, reflect.TypeFor[*TService](), obs.constructed[1].ServiceType)
		assert.Equal(t, scope.ID(), obs.constructed[1].ScopeID)
		assert.Error(t, obs.constructed[1].Err)
		assert.Contains(t, obs.constructed[1].Constructor, "NewTServiceError")

		require.Len(t, obs.disposed, 1)
		assert.Equal(t, reflect.TypeFor[*TDisposable](), obs.disposed[0].Type)
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
		p, err := c.Build(WithObserver(obs.observer()))
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

	// The provider owns what the root scope creates, so the root scope's
	// orphans report ScopeID "", as its regular disposals do.
	t.Run("root_scope_orphans_are_the_providers", func(t *testing.T) {
		t.Parallel()
		obs := &recordingObserver{}
		started := make(chan struct{})
		release := make(chan struct{})
		c := NewCollection()
		c.AddScoped(func() *TDisposable {
			close(started)
			<-release
			return NewTDisposable()
		})
		p, err := c.Build(WithObserver(obs.observer()))
		require.NoError(t, err)

		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = Resolve[*TDisposable](p)
		}()
		<-started
		closed := make(chan error, 1)
		go func() { closed <- p.Close() }()
		// Close disposes before the constructor returns, so the value is an orphan.
		require.Eventually(t, func() bool { return p.(*provider).disposed.Load() != 0 }, time.Second, time.Millisecond)
		close(release)
		<-done
		<-closed

		obs.mu.Lock()
		defer obs.mu.Unlock()
		require.Len(t, obs.disposed, 1)
		assert.Empty(t, obs.disposed[0].ScopeID)
	})
}
