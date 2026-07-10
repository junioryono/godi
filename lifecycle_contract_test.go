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

type closeAliasA interface {
	AliasA()
}

type closeAliasB interface {
	AliasB()
}

type countedAliasDisposable struct {
	closeCalls atomic.Int64
}

func (*countedAliasDisposable) AliasA() {}

func (*countedAliasDisposable) AliasB() {}

func (d *countedAliasDisposable) Close() error {
	d.closeCalls.Add(1)
	return nil
}

// countedValueDisposable is deliberately a value type: value disposables are
// deduplicated per produced value, not per referenced counter.
type countedValueDisposable struct {
	closeCalls *atomic.Int64
}

func (countedValueDisposable) AliasA() {}

func (countedValueDisposable) AliasB() {}

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
