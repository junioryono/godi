package godi

import (
	"context"
	"errors"
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

	t.Run("names_are_the_only_keys", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTServiceWithID("eu"), Name("eu"))
		c.AddSingleton(NewTServiceWithID("unnamed"))
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		svc, err := ResolveKeyed[*TService](p, "eu")
		require.NoError(t, err)
		assert.Equal(t, "eu", svc.ID)

		// "" is no name: it is rejected where a name is required, and
		// means the unnamed registration in collection lookups.
		_, err = ResolveKeyed[*TService](p, "")
		require.ErrorIs(t, err, ErrServiceKeyEmpty)
		assert.True(t, c.ContainsKeyed(reflect.TypeFor[*TService](), ""))

		infos := c.ToSlice()
		require.Len(t, infos, 2)
		assert.Equal(t, "eu", infos[0].Key)
		assert.Equal(t, "", infos[1].Key)
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
