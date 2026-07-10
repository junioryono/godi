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

func TestAliasedDisposableClosesOnce(t *testing.T) {
	disposable := &countedAliasDisposable{}
	c := NewCollection()
	c.AddSingleton(func() *countedAliasDisposable { return disposable }, As[closeAliasA](), As[closeAliasB]())

	p, err := c.Build()
	require.NoError(t, err)
	require.NoError(t, p.Close())
	assert.Equal(t, int64(1), disposable.closeCalls.Load())
}

func TestMultiReturnSameDisposableClosesOnce(t *testing.T) {
	disposable := &countedAliasDisposable{}
	c := NewCollection()
	c.AddSingleton(func() (closeAliasA, closeAliasB) {
		return disposable, disposable
	})

	p, err := c.Build()
	require.NoError(t, err)
	require.NoError(t, p.Close())
	assert.Equal(t, int64(1), disposable.closeCalls.Load())
}

type countedValueDisposable struct {
	closeCalls *atomic.Int64
}

func (countedValueDisposable) AliasA() {}

func (countedValueDisposable) AliasB() {}

func (d countedValueDisposable) Close() error {
	d.closeCalls.Add(1)
	return nil
}

func TestIndependentEqualValueDisposablesBothClose(t *testing.T) {
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
}

func TestAliasedValueDisposableClosesOnce(t *testing.T) {
	var closeCalls atomic.Int64
	value := countedValueDisposable{closeCalls: &closeCalls}

	c := NewCollection()
	c.AddSingleton(func() countedValueDisposable { return value }, As[closeAliasA](), As[closeAliasB]())

	p, err := c.Build()
	require.NoError(t, err)
	require.NoError(t, p.Close())
	assert.Equal(t, int64(1), closeCalls.Load())
}

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

func TestConcurrentProviderCloseWaitsAndReturnsSameError(t *testing.T) {
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
}

func TestConcurrentScopeCloseWaitsAndReturnsSameError(t *testing.T) {
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
}

type failingDisposable struct {
	closed chan struct{}
	err    error
}

func (d *failingDisposable) Close() error {
	select {
	case <-d.closed:
	default:
		close(d.closed)
	}
	return d.err
}

func TestCancellationCloseErrorRemainsObservable(t *testing.T) {
	closeErr := errors.New("cancel cleanup failed")
	disposable := &failingDisposable{closed: make(chan struct{}), err: closeErr}

	c := NewCollection()
	c.AddScoped(func() *failingDisposable { return disposable })
	p, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	s, err := p.CreateScope(ctx)
	require.NoError(t, err)
	_, err = Resolve[*failingDisposable](s)
	require.NoError(t, err)

	cancel()
	select {
	case <-disposable.closed:
	case <-time.After(time.Second):
		t.Fatal("scope was not closed after cancellation")
	}

	require.ErrorIs(t, s.Close(), closeErr)
}

func TestCreateScopeRejectsCanceledContext(t *testing.T) {
	c := NewCollection()
	p, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s, err := p.CreateScope(ctx)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, s)
}

func TestProviderCloseReportsChildScopeErrorOnce(t *testing.T) {
	closeErr := errors.New("child cleanup failed")
	c := NewCollection()
	c.AddScoped(func() *failingDisposable {
		return &failingDisposable{closed: make(chan struct{}), err: closeErr}
	})

	p, err := c.Build()
	require.NoError(t, err)
	parent, err := p.CreateScope(context.Background())
	require.NoError(t, err)
	child, err := parent.CreateScope(context.Background())
	require.NoError(t, err)
	_, err = Resolve[*failingDisposable](child)
	require.NoError(t, err)

	err = p.Close()
	require.ErrorIs(t, err, closeErr)
	assert.Equal(t, 1, countErrorOccurrences(err, closeErr))
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
