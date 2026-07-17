package godi

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectionSnapshotIsolation(t *testing.T) {
	t.Parallel()

	t.Run("provider_uses_immutable_snapshot", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTServiceWithID("one"))
		// Transient: resolution consults the descriptor snapshot every time,
		// so it cannot be masked by a pre-materialized singleton instance.
		c.AddTransient(NewTDependency)

		first, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = first.Close() })

		c.Remove(PtrTypeOf[TService]())
		c.Remove(PtrTypeOf[TDependency]())
		c.AddSingleton(NewTServiceWithID("two"))

		second, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = second.Close() })

		firstValue := RequireResolve[*TService](t, first)
		secondValue := RequireResolve[*TService](t, second)

		assert.Equal(t, "one", firstValue.ID)
		assert.Equal(t, "two", secondValue.ID)

		// The first provider's snapshot still contains the removed transient;
		// the second provider must not know it.
		_ = RequireResolve[*TDependency](t, first)
		_, err = Resolve[*TDependency](second)
		require.Error(t, err)
	})

	t.Run("resolution_does_not_race_collection_mutation", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTServiceWithID("one"))

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		const iterations = 100
		var wg sync.WaitGroup
		resolveErrs := make(chan error, iterations)

		wg.Go(func() {
			for range iterations {
				c.Remove(PtrTypeOf[TService]())
				c.AddSingleton(NewTServiceWithID("one"))
			}
		})

		wg.Go(func() {
			for range iterations {
				value, resolveErr := Resolve[*TService](p)
				if resolveErr != nil {
					resolveErrs <- resolveErr
					continue
				}
				if value.ID != "one" {
					resolveErrs <- fmt.Errorf("resolved service ID %q, want %q", value.ID, "one")
				}
			}
		})

		wg.Wait()
		close(resolveErrs)
		for resolveErr := range resolveErrs {
			assert.NoError(t, resolveErr)
		}
	})
}

type aliasReader interface {
	ReadAlias() string
}

type aliasWriter interface {
	WriteAlias() string
}

type aliasService struct {
	id string
}

func (s *aliasService) ReadAlias() string {
	return s.id
}

func (s *aliasService) WriteAlias() string {
	return s.id
}

// newAliasServiceCounter returns a constructor that stamps each instance with
// the invocation count, so tests can assert how many times it ran.
func newAliasServiceCounter() (func() *aliasService, *atomic.Int64) {
	calls := &atomic.Int64{}
	return func() *aliasService {
		return &aliasService{id: "alias-" + strconv.FormatInt(calls.Add(1), 10)}
	}, calls
}

type pointerOnlyAlias interface {
	PointerOnly()
}

type pointerOnlyValue struct{}

func (*pointerOnlyValue) PointerOnly() {}

func TestMultipleAsRegistration(t *testing.T) {
	t.Parallel()

	t.Run("singleton_uses_one_canonical_instance", func(t *testing.T) {
		t.Parallel()
		newAliasService, calls := newAliasServiceCounter()
		c := NewCollection()
		c.AddSingleton(newAliasService, As[aliasReader](), As[aliasWriter]())

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		reader := RequireResolve[aliasReader](t, p)
		writer := RequireResolve[aliasWriter](t, p)

		assert.Same(t, reader.(*aliasService), writer.(*aliasService))
		assert.Equal(t, int64(1), calls.Load())
	})

	t.Run("scoped_shares_within_scope", func(t *testing.T) {
		t.Parallel()
		newAliasService, calls := newAliasServiceCounter()
		c := NewCollection()
		c.AddScoped(newAliasService, As[aliasReader](), As[aliasWriter]())

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		first, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = first.Close() })
		second, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = second.Close() })

		reader := RequireResolveFrom[aliasReader](t, first)
		writer := RequireResolveFrom[aliasWriter](t, first)
		other := RequireResolveFrom[aliasReader](t, second)

		assert.Same(t, reader.(*aliasService), writer.(*aliasService))
		assert.NotSame(t, reader.(*aliasService), other.(*aliasService))
		assert.Equal(t, int64(2), calls.Load())
	})

	t.Run("rejects_value_implemented_only_by_pointer", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func() pointerOnlyValue { return pointerOnlyValue{} }, As[pointerOnlyAlias]())

		require.Error(t, c.Err())
		_, err := c.Build()
		require.Error(t, err)
	})

	t.Run("rolls_back_all_aliases_on_conflict", func(t *testing.T) {
		t.Parallel()
		newAliasService, _ := newAliasServiceCounter()
		c := NewCollection()
		c.AddSingleton(func() aliasWriter { return &aliasService{} })
		require.NoError(t, c.Err())

		c.AddSingleton(newAliasService, As[aliasReader](), As[aliasWriter]())
		require.Error(t, c.Err())

		assert.False(t, c.Contains(TypeOf[aliasReader]()))
		assert.True(t, c.Contains(TypeOf[aliasWriter]()))
		assert.Equal(t, 1, c.Count())
	})
}

func TestInstanceRegistrationLifetime(t *testing.T) {
	t.Parallel()

	t.Run("scoped_rejected", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddScoped(&TService{ID: "shared"})
		require.Error(t, c.Err())
		_, err := c.Build()
		require.Error(t, err)
	})

	t.Run("transient_rejected", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddTransient(&TService{ID: "shared"})
		require.Error(t, c.Err())
		_, err := c.Build()
		require.Error(t, err)
	})

	t.Run("singleton_allowed", func(t *testing.T) {
		t.Parallel()
		instance := &TService{ID: "shared"}
		c := NewCollection()
		c.AddSingleton(instance)
		require.NoError(t, c.Err())

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		resolved := RequireResolve[*TService](t, p)
		assert.Same(t, instance, resolved)
	})
}

func TestUnsupportedConstructorShapes(t *testing.T) {
	t.Parallel()

	t.Run("transient_void_constructor", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddTransient(NewTVoid)
		require.Error(t, c.Err())
	})

	t.Run("variadic_constructor", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func(_ ...*TService) *TDependency { return NewTDependency() })
		require.Error(t, c.Err())
	})

	t.Run("result_object_with_name_option", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTResult, Name("named"))
		require.Error(t, c.Err())
	})

	t.Run("result_object_with_group_option", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTResult, Group("services"))
		require.Error(t, c.Err())
	})
}

func TestCollectionKeyedNonComparableKey(t *testing.T) {
	t.Parallel()

	c := NewCollection()
	c.AddSingleton(NewTService, Name("one"))

	// Both a directly non-comparable key and a comparable struct wrapping a
	// non-comparable value in an interface field (which passes a type-level
	// comparability check but panics as a map key).
	keys := []any{
		[]string{"one"},
		struct{ V any }{V: []int{1}},
	}
	for _, key := range keys {
		require.NotPanics(t, func() {
			assert.False(t, c.ContainsKeyed(PtrTypeOf[TService](), key))
		})
		require.NotPanics(t, func() {
			c.RemoveKeyed(PtrTypeOf[TService](), key)
		})
	}
	assert.Equal(t, 1, c.Count())
}

func TestTypedNilConstructorResult(t *testing.T) {
	t.Parallel()

	t.Run("transient_rejected_at_resolution", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddTransient(func() *TService { return nil })
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		service, err := Resolve[*TService](p)
		require.Error(t, err)
		assert.Nil(t, service)
	})

	t.Run("result_object_interface_field_not_cached", func(t *testing.T) {
		t.Parallel()
		// A typed-nil pointer stored in an interface field reports IsNil() ==
		// false on the interface itself; it must still be treated as "not
		// provided" like a directly nil field, not cached as a valid service.
		type nilResult struct {
			Out
			Iface TInterface
		}
		c := NewCollection()
		c.AddScoped(func() nilResult {
			return nilResult{Iface: (*TService)(nil)}
		})
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		service, err := Resolve[TInterface](p)
		require.Error(t, err)
		assert.Nil(t, service)
	})
}
