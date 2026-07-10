package godi

import (
	"context"
	"fmt"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type snapshotService struct {
	value string
}

func newSnapshotServiceOne() *snapshotService {
	return &snapshotService{value: "one"}
}

func newSnapshotServiceTwo() *snapshotService {
	return &snapshotService{value: "two"}
}

func TestBuiltProviderUsesImmutableCollectionSnapshot(t *testing.T) {
	c := NewCollection()
	c.AddSingleton(newSnapshotServiceOne)

	first, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = first.Close() })

	c.Remove(reflect.TypeFor[*snapshotService]())
	c.AddSingleton(newSnapshotServiceTwo)

	second, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = second.Close() })

	firstValue, err := Resolve[*snapshotService](first)
	require.NoError(t, err)
	secondValue, err := Resolve[*snapshotService](second)
	require.NoError(t, err)

	assert.Equal(t, "one", firstValue.value)
	assert.Equal(t, "two", secondValue.value)
}

func TestBuiltProviderDoesNotRaceCollectionMutation(t *testing.T) {
	c := NewCollection()
	c.AddSingleton(newSnapshotServiceOne)

	p, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	const iterations = 100
	var wg sync.WaitGroup
	resolveErrs := make(chan error, iterations)
	wg.Add(2)

	go func() {
		defer wg.Done()
		for range iterations {
			c.Remove(reflect.TypeFor[*snapshotService]())
			c.AddSingleton(newSnapshotServiceOne)
		}
	}()

	go func() {
		defer wg.Done()
		for range iterations {
			value, resolveErr := Resolve[*snapshotService](p)
			if resolveErr != nil {
				resolveErrs <- resolveErr
				continue
			}
			if value.value != "one" {
				resolveErrs <- fmt.Errorf("resolved snapshot value %q, want %q", value.value, "one")
			}
		}
	}()

	wg.Wait()
	close(resolveErrs)
	for resolveErr := range resolveErrs {
		assert.NoError(t, resolveErr)
	}
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

var aliasConstructorCalls atomic.Int64

func newAliasService() *aliasService {
	call := aliasConstructorCalls.Add(1)
	return &aliasService{id: "alias-" + string(rune('0'+call))}
}

func TestMultipleAsSingletonUsesOneCanonicalInstance(t *testing.T) {
	aliasConstructorCalls.Store(0)
	c := NewCollection()
	c.AddSingleton(newAliasService, As[aliasReader](), As[aliasWriter]())

	p, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	reader, err := Resolve[aliasReader](p)
	require.NoError(t, err)
	writer, err := Resolve[aliasWriter](p)
	require.NoError(t, err)

	assert.Same(t, reader.(*aliasService), writer.(*aliasService))
	assert.Equal(t, int64(1), aliasConstructorCalls.Load())
}

func TestMultipleAsScopedSharesWithinScope(t *testing.T) {
	aliasConstructorCalls.Store(0)
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

	reader, err := Resolve[aliasReader](first)
	require.NoError(t, err)
	writer, err := Resolve[aliasWriter](first)
	require.NoError(t, err)
	other, err := Resolve[aliasReader](second)
	require.NoError(t, err)

	assert.Same(t, reader.(*aliasService), writer.(*aliasService))
	assert.NotSame(t, reader.(*aliasService), other.(*aliasService))
	assert.Equal(t, int64(2), aliasConstructorCalls.Load())
}

type pointerOnlyAlias interface {
	PointerOnly()
}

type pointerOnlyValue struct{}

func (*pointerOnlyValue) PointerOnly() {}

func TestAsRejectsValueImplementedOnlyByPointer(t *testing.T) {
	c := NewCollection()
	c.AddSingleton(func() pointerOnlyValue { return pointerOnlyValue{} }, As[pointerOnlyAlias]())

	require.Error(t, c.Err())
	_, err := c.Build()
	require.Error(t, err)
}

func TestAsRegistrationRollsBackAllAliases(t *testing.T) {
	c := NewCollection()
	c.AddSingleton(func() aliasWriter { return &aliasService{} })
	require.NoError(t, c.Err())

	c.AddSingleton(newAliasService, As[aliasReader](), As[aliasWriter]())
	require.Error(t, c.Err())

	assert.False(t, c.Contains(reflect.TypeFor[aliasReader]()))
	assert.True(t, c.Contains(reflect.TypeFor[aliasWriter]()))
	assert.Equal(t, 1, c.Count())
}

func TestInstanceRegistrationRequiresSingletonLifetime(t *testing.T) {
	t.Run("scoped", func(t *testing.T) {
		c := NewCollection()
		c.AddScoped(&snapshotService{value: "shared"})
		require.Error(t, c.Err())
		_, err := c.Build()
		require.Error(t, err)
	})

	t.Run("transient", func(t *testing.T) {
		c := NewCollection()
		c.AddTransient(&snapshotService{value: "shared"})
		require.Error(t, c.Err())
		_, err := c.Build()
		require.Error(t, err)
	})

	t.Run("singleton", func(t *testing.T) {
		instance := &snapshotService{value: "shared"}
		c := NewCollection()
		c.AddSingleton(instance)
		require.NoError(t, c.Err())

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		resolved, err := Resolve[*snapshotService](p)
		require.NoError(t, err)
		assert.Same(t, instance, resolved)
	})
}

func TestUnsupportedConstructorShapesFailAtRegistration(t *testing.T) {
	t.Run("transient void constructor", func(t *testing.T) {
		c := NewCollection()
		c.AddTransient(func() {})
		require.Error(t, c.Err())
	})

	t.Run("variadic constructor", func(t *testing.T) {
		c := NewCollection()
		c.AddSingleton(func(_ ...*snapshotService) *aliasService { return &aliasService{} })
		require.Error(t, c.Err())
	})

	t.Run("result object with name option", func(t *testing.T) {
		type result struct {
			Out
			Service *snapshotService
		}
		c := NewCollection()
		c.AddSingleton(func() result { return result{Service: newSnapshotServiceOne()} }, Name("named"))
		require.Error(t, c.Err())
	})

	t.Run("result object with group option", func(t *testing.T) {
		type result struct {
			Out
			Service *snapshotService
		}
		c := NewCollection()
		c.AddSingleton(func() result { return result{Service: newSnapshotServiceOne()} }, Group("services"))
		require.Error(t, c.Err())
	})
}

func TestCollectionKeyOperationsRejectNonComparableKeys(t *testing.T) {
	c := NewCollection()
	c.AddSingleton(newSnapshotServiceOne, Name("one"))
	nonComparable := []string{"one"}

	require.NotPanics(t, func() {
		assert.False(t, c.ContainsKeyed(reflect.TypeFor[*snapshotService](), nonComparable))
	})
	require.NotPanics(t, func() {
		c.RemoveKeyed(reflect.TypeFor[*snapshotService](), nonComparable)
	})
	assert.Equal(t, 1, c.Count())
}

type typedNilService struct{}

func TestTypedNilConstructorResultIsRejected(t *testing.T) {
	c := NewCollection()
	c.AddTransient(func() *typedNilService { return nil })
	p, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	service, err := Resolve[*typedNilService](p)
	require.Error(t, err)
	assert.Nil(t, service)
}
