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

type slowBuildService struct{}

func TestBuildTimeoutAfterSlowFinalConstructorReturnsError(t *testing.T) {
	c := NewCollection()
	c.AddSingleton(func() *slowBuildService {
		time.Sleep(30 * time.Millisecond)
		return &slowBuildService{}
	})

	started := time.Now()
	p, err := c.BuildWithOptions(&ProviderOptions{BuildTimeout: 5 * time.Millisecond})
	elapsed := time.Since(started)

	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, p)
	assert.GreaterOrEqual(t, elapsed, 30*time.Millisecond)
}

func TestBuildContextLetsConstructorCancelPromptly(t *testing.T) {
	observedCancellation := make(chan struct{})
	c := NewCollection()
	c.AddSingleton(func(ctx context.Context) (*slowBuildService, error) {
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
}

func TestBuildContextContainsRootScope(t *testing.T) {
	type contextKey struct{}
	want := &slowBuildService{}
	buildCtx := context.WithValue(context.Background(), contextKey{}, want)

	c := NewCollection()
	c.AddSingleton(func(ctx context.Context) (*slowBuildService, error) {
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
}

func TestBuildContextOverrideIsRaceSafe(t *testing.T) {
	started := make(chan struct{})
	stop := make(chan struct{})
	var readers sync.WaitGroup

	c := NewCollection()
	c.AddSingleton(func(p Provider) *slowBuildService {
		readers.Add(1)
		go func() {
			defer readers.Done()
			close(started)
			for {
				select {
				case <-stop:
					return
				default:
					_, _ = p.Get(contextType)
				}
			}
		}()
		<-started
		return &slowBuildService{}
	})

	p, err := c.BuildWithContext(context.Background())
	close(stop)
	readers.Wait()
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
}

type buildDisposable struct {
	closed atomic.Bool
}

func (d *buildDisposable) Close() error {
	d.closed.Store(true)
	return nil
}

type dependentSlowBuildService struct {
	resource *buildDisposable
}

func TestBuildTimeoutCleansPartialSingletons(t *testing.T) {
	resource := &buildDisposable{}
	c := NewCollection()
	c.AddSingleton(func() *buildDisposable { return resource })
	c.AddSingleton(func(d *buildDisposable) *dependentSlowBuildService {
		time.Sleep(30 * time.Millisecond)
		return &dependentSlowBuildService{resource: d}
	})

	p, err := c.BuildWithOptions(&ProviderOptions{BuildTimeout: 5 * time.Millisecond})
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, p)
	assert.True(t, resource.closed.Load())
}

type initializerDependency struct{}

func TestRootScopedInitializerRunsAfterSingletonCreation(t *testing.T) {
	var calls atomic.Int64
	c := NewCollection()
	c.AddSingleton(func() *initializerDependency { return &initializerDependency{} })
	c.AddScoped(func(*initializerDependency) {
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
}

func TestBuildTimeoutErrorRemainsPrimaryWhenCleanupFails(t *testing.T) {
	cleanupErr := errors.New("cleanup failed")
	c := NewCollection()
	c.AddSingleton(func() *failingDisposable {
		return &failingDisposable{closed: make(chan struct{}), err: cleanupErr}
	})
	c.AddSingleton(func(*failingDisposable) *slowBuildService {
		time.Sleep(30 * time.Millisecond)
		return &slowBuildService{}
	})

	p, err := c.BuildWithOptions(&ProviderOptions{BuildTimeout: 5 * time.Millisecond})
	require.Error(t, err)
	assert.Nil(t, p)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.ErrorIs(t, err, cleanupErr)
}
