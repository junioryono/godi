package godi

import (
	"context"
	"errors"
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
