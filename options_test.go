package godi

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistrationValuesAndKeys(t *testing.T) {
	t.Parallel()

	t.Run("instance_of_a_function_type", func(t *testing.T) {
		t.Parallel()
		type Clock func() time.Time
		fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		clock := Clock(func() time.Time { return fixed })

		// A function value was always taken for a constructor.
		c := NewCollection()
		c.AddSingleton(Instance(clock))
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		got, err := Resolve[Clock](p)
		require.NoError(t, err)
		assert.Equal(t, fixed, got())
	})

	t.Run("non_string_keys", func(t *testing.T) {
		t.Parallel()
		type Region int
		const eu, us Region = 1, 2
		c := NewCollection()
		c.AddSingleton(NewTServiceWithID("eu"), Key(eu))
		c.AddSingleton(NewTServiceWithID("us"), Key(us))
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		svc, err := ResolveKeyed[*TService](p, us)
		require.NoError(t, err)
		assert.Equal(t, "us", svc.ID)
		assert.True(t, c.ContainsKeyed(reflect.TypeFor[*TService](), eu))
	})

	t.Run("key_must_be_comparable", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService, Key([]int{1}))
		require.Error(t, c.Err())
	})

	t.Run("key_must_equal_itself", func(t *testing.T) {
		t.Parallel()
		// NaN is comparable but never equal to itself, so the registration
		// could never be found.
		c := NewCollection()
		c.AddSingleton(NewTService, Key(math.NaN()))
		require.Error(t, c.Err())
	})
}

func TestLazySingleton(t *testing.T) {
	t.Parallel()

	t.Run("created_on_first_resolution", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int32
		c := NewCollection()
		c.AddSingleton(func() *TService { calls.Add(1); return NewTService() }, Lazy())
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		assert.Zero(t, calls.Load(), "a lazy singleton is not created at Build")

		first, err := Resolve[*TService](p)
		require.NoError(t, err)
		second, err := Resolve[*TService](p)
		require.NoError(t, err)
		assert.Same(t, first, second)
		assert.Equal(t, int32(1), calls.Load())
	})

	t.Run("concurrent_first_resolution_constructs_once", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int32
		c := NewCollection()
		c.AddSingleton(func() *TService { calls.Add(1); return NewTService() }, Lazy())
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		var wg sync.WaitGroup
		results := make([]*TService, 16)
		for i := range results {
			wg.Go(func() {
				scope, err := p.CreateScope(context.Background())
				if !assert.NoError(t, err) {
					return
				}
				defer scope.Close()
				results[i], _ = Resolve[*TService](scope)
			})
		}
		wg.Wait()
		assert.Equal(t, int32(1), calls.Load())
		for _, r := range results {
			assert.Same(t, results[0], r)
		}
	})

	t.Run("a_failed_construction_is_retried", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int32
		c := NewCollection()
		c.AddSingleton(func() (*TService, error) {
			if calls.Add(1) == 1 {
				return nil, errors.New("not yet")
			}
			return NewTService(), nil
		}, Lazy())
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		_, err = Resolve[*TService](p)
		require.Error(t, err)
		_, err = Resolve[*TService](p)
		require.NoError(t, err, "failures are not cached")
	})

	t.Run("still_validated_at_build", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func(*TDependency) *TService { return NewTService() }, Lazy())
		_, err := c.Build()
		require.ErrorIs(t, err, ErrServiceNotFound)
	})

	t.Run("disposed_before_its_dependencies", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func() *orderedWriter { return &orderedWriter{} })
		c.AddSingleton(func(w *orderedWriter) *orderedBuffer { return &orderedBuffer{w: w} }, Lazy())
		p, err := c.Build()
		require.NoError(t, err)
		buf, err := Resolve[*orderedBuffer](p)
		require.NoError(t, err)
		require.NoError(t, p.Close())
		assert.True(t, buf.writerOpenAtClose)
	})
}
