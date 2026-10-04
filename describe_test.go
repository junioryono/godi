package godi

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDescribe(t *testing.T) {
	t.Parallel()

	c := NewCollection()
	c.AddSingleton(NewTService)
	c.AddSingleton(NewTDependency)
	c.AddScoped(NewTServiceWithDeps)
	p, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	t.Run("provider_lists_registrations_with_dependencies", func(t *testing.T) {
		t.Parallel()
		infos := Describe(p)
		require.Len(t, infos, 3)

		withDeps := infos[2]
		assert.Equal(t, PtrTypeOf[TServiceWithDeps](), withDeps.ServiceType)
		assert.Equal(t, Scoped, withDeps.Lifetime)
		assert.Contains(t, withDeps.Constructor, "NewTServiceWithDeps")
		assert.Contains(t, withDeps.Constructor, "testutil_test.go:")
		require.Len(t, withDeps.Dependencies, 2)
		assert.Equal(t, PtrTypeOf[TService](), withDeps.Dependencies[0].Type)
		assert.Equal(t, PtrTypeOf[TDependency](), withDeps.Dependencies[1].Type)
	})

	t.Run("service_info_stays_comparable", func(t *testing.T) {
		t.Parallel()
		// ServiceInfo shipped comparable in v5.1 (usable with == and as a
		// map key); the dependency details live in ServiceDescription.
		assert.True(t, reflect.TypeOf(ServiceInfo{}).Comparable())
		infos := c.ToSlice()
		require.Len(t, infos, 3)
		assert.True(t, Describe(p)[2].ServiceInfo == infos[2])
	})

	t.Run("dot_graph", func(t *testing.T) {
		t.Parallel()
		var b strings.Builder
		require.NoError(t, WriteDOT(&b, Describe(p)))
		dot := b.String()
		assert.True(t, strings.HasPrefix(dot, "digraph godi {"))
		assert.Contains(t, dot, `"*godi.TServiceWithDeps" -> "*godi.TService"`)
		assert.Contains(t, dot, `"*godi.TServiceWithDeps" -> "*godi.TDependency"`)
	})
}
