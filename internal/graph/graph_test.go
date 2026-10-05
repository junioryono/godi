package graph

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/junioryono/godi/v6/internal/reflection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testProvider is a minimal Provider, decoupling the graph tests from the
// godi package's descriptor type.
type testProvider struct {
	Type         reflect.Type
	Key          any
	Group        string
	Dependencies []*reflection.Dependency
}

func (p *testProvider) GetType() reflect.Type                     { return p.Type }
func (p *testProvider) GetKey() any                               { return p.Key }
func (p *testProvider) GetGroup() string                          { return p.Group }
func (p *testProvider) GetDependencies() []*reflection.Dependency { return p.Dependencies }

type (
	nodeA         struct{}
	nodeB         struct{}
	nodeC         struct{}
	nodeD         struct{}
	groupMember   struct{}
	groupConsumer struct{}
)

var (
	typeA        = reflect.TypeFor[nodeA]()
	typeB        = reflect.TypeFor[nodeB]()
	typeC        = reflect.TypeFor[nodeC]()
	typeD        = reflect.TypeFor[nodeD]()
	memberType   = reflect.TypeFor[groupMember]()
	consumerType = reflect.TypeFor[groupConsumer]()
)

func deps(types ...reflect.Type) []*reflection.Dependency {
	out := make([]*reflection.Dependency, len(types))
	for i, t := range types {
		out[i] = &reflection.Dependency{Type: t}
	}
	return out
}

func build(t testing.TB, providers ...*testProvider) *DependencyGraph {
	t.Helper()
	g := NewDependencyGraphWithCapacity(len(providers))
	for _, p := range providers {
		require.NoError(t, g.AddProviderDeferred(p))
	}
	return g
}

func TestAddProviderDeferred(t *testing.T) {
	t.Parallel()

	t.Run("rejects_a_nil_provider", func(t *testing.T) {
		t.Parallel()
		assert.Error(t, NewDependencyGraphWithCapacity(0).AddProviderDeferred(nil))
	})

	t.Run("records_nodes_in_insertion_order", func(t *testing.T) {
		t.Parallel()
		g := build(t,
			&testProvider{Type: typeA, Dependencies: deps(typeC, typeB)},
			&testProvider{Type: typeB},
		)
		assert.Equal(t, []NodeKey{{Type: typeA}, {Type: typeC}, {Type: typeB}}, g.order)
		assert.Equal(t, []NodeKey{{Type: typeC}, {Type: typeB}}, g.edges[NodeKey{Type: typeA}])
	})

	// Re-registering a key replaces its edges rather than merging in stale
	// ones from the previous registration.
	t.Run("replacement_clears_stale_edges", func(t *testing.T) {
		t.Parallel()
		g := build(t,
			&testProvider{Type: typeA, Dependencies: deps(typeB)},
			&testProvider{Type: typeA},
		)
		assert.Empty(t, g.edges[NodeKey{Type: typeA}])
		assert.NoError(t, g.DetectCycles())
	})
}

func TestDetectCycles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		providers []*testProvider
		path      []string // nil: no cycle
	}{
		{
			name:      "self_cycle",
			providers: []*testProvider{{Type: typeA, Dependencies: deps(typeA)}},
			path:      []string{typeA.String()},
		},
		{
			name: "diamond_without_cycle",
			providers: []*testProvider{
				{Type: typeA, Dependencies: deps(typeB, typeC)},
				{Type: typeB, Dependencies: deps(typeD)},
				{Type: typeC, Dependencies: deps(typeD)},
				{Type: typeD},
			},
		},
		{
			name: "three_node_cycle_in_dependency_order",
			providers: []*testProvider{
				{Type: typeA, Dependencies: deps(typeB)},
				{Type: typeB, Dependencies: deps(typeC)},
				{Type: typeC, Dependencies: deps(typeA)},
			},
			path: []string{typeA.String(), typeB.String(), typeC.String()},
		},
		{
			name: "cycle_reached_through_an_acyclic_prefix",
			providers: []*testProvider{
				{Type: typeD, Dependencies: deps(typeA)},
				{Type: typeA, Dependencies: deps(typeB)},
				{Type: typeB, Dependencies: deps(typeA)},
			},
			path: []string{typeA.String(), typeB.String()},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := build(t, tt.providers...).DetectCycles()
			if tt.path == nil {
				assert.NoError(t, err)
				return
			}
			cycle, ok := err.(*CircularDependencyError)
			require.True(t, ok, "got %T: %v", err, err)
			assert.Equal(t, tt.path, cycle.Path)
		})
	}

	// With several cycles, the one reported must not depend on map iteration.
	t.Run("deterministic", func(t *testing.T) {
		t.Parallel()
		report := func() string {
			err := build(t,
				&testProvider{Type: typeA, Dependencies: deps(typeB)},
				&testProvider{Type: typeB, Dependencies: deps(typeA)},
				&testProvider{Type: typeC, Dependencies: deps(typeD)},
				&testProvider{Type: typeD, Dependencies: deps(typeC)},
			).DetectCycles()
			require.Error(t, err)
			return err.Error()
		}
		first := report()
		assert.Contains(t, first, typeA.String(), "the first registered cycle is reported")
		for range 50 {
			assert.Equal(t, first, report())
		}
	})
}

func TestResolveGroupDependencies(t *testing.T) {
	t.Parallel()

	member := func(key int, group string) *testProvider {
		return &testProvider{Type: memberType, Key: key, Group: group}
	}
	consumerOf := func(dependencies ...*reflection.Dependency) *testProvider {
		return &testProvider{Type: consumerType, Dependencies: dependencies}
	}
	groupDep := func(group string) *reflection.Dependency {
		return &reflection.Dependency{Type: memberType, Group: group}
	}
	consumer := NodeKey{Type: consumerType}

	t.Run("connects_consumers_to_members_and_removes_the_placeholder", func(t *testing.T) {
		t.Parallel()
		for name, g := range map[string]*DependencyGraph{
			"members_first":  build(t, member(1, "routes"), member(2, "routes"), consumerOf(groupDep("routes"))),
			"consumer_first": build(t, consumerOf(groupDep("routes")), member(1, "routes"), member(2, "routes")),
		} {
			g.ResolveGroupDependencies()
			assert.Equal(t, []NodeKey{
				{Type: memberType, Key: 1, Group: "routes"},
				{Type: memberType, Key: 2, Group: "routes"},
			}, g.edges[consumer], name)
			placeholder := NodeKey{Type: memberType, Group: "routes"}
			assert.NotContains(t, g.known, placeholder, name)
			assert.NotContains(t, g.order, placeholder, name)
		}
	})

	t.Run("keeps_other_edges_and_drops_empty_groups", func(t *testing.T) {
		t.Parallel()
		plain := reflect.TypeFor[string]()
		g := build(t,
			member(1, "a"), member(2, "a"), member(1, "b"),
			&testProvider{Type: plain},
			consumerOf(&reflection.Dependency{Type: plain}, groupDep("a"), groupDep("b"), groupDep("missing")),
		)
		g.ResolveGroupDependencies()
		assert.Equal(t, []NodeKey{
			{Type: plain},
			{Type: memberType, Key: 1, Group: "a"},
			{Type: memberType, Key: 2, Group: "a"},
			{Type: memberType, Key: 1, Group: "b"},
		}, g.edges[consumer])
		for _, group := range []string{"a", "b", "missing"} {
			assert.NotContains(t, g.known, NodeKey{Type: memberType, Group: group})
		}
	})

	t.Run("is_a_no_op_without_group_dependencies", func(t *testing.T) {
		t.Parallel()
		g := build(t, member(1, "routes"))
		order := append([]NodeKey(nil), g.order...)
		g.ResolveGroupDependencies()
		assert.Equal(t, order, g.order)
	})

	// The placeholder hides member edges: only after resolution does a member
	// that depends on its group's consumer form a cycle.
	t.Run("exposes_cycles_through_a_group", func(t *testing.T) {
		t.Parallel()
		g := build(t,
			consumerOf(groupDep("routes")),
			&testProvider{Type: memberType, Key: 1, Group: "routes", Dependencies: deps(consumerType)},
		)
		g.ResolveGroupDependencies()
		assert.Error(t, g.DetectCycles())
	})
}

func TestCircularDependencyError(t *testing.T) {
	t.Parallel()

	withoutPath := &CircularDependencyError{Node: typeA.String()}
	assert.Equal(t, "circular dependency detected: "+typeA.String()+" -> "+typeA.String(), withoutPath.Error())

	err := &CircularDependencyError{Node: "A", Path: []string{"A", "B", "C"}}
	assert.Equal(t, "circular dependency detected: A -> B -> C -> A", err.Error())
	assert.Contains(t, err.Detail(), "↓", "the detail draws the cycle")
	assert.Contains(t, fmt.Sprintf("%+v", err), "A (cycle)", "%+v includes the detail")
	assert.Equal(t, fmt.Sprintf("%q", err.Error()), fmt.Sprintf("%q", err))
}

func TestNodeKeyString(t *testing.T) {
	t.Parallel()
	assert.Equal(t, typeA.String(), NodeKey{Type: typeA}.String())
	assert.Equal(t, typeA.String()+":primary", NodeKey{Type: typeA, Key: "primary"}.String())
	assert.Equal(t, typeA.String()+":1 [routes]", NodeKey{Type: typeA, Key: 1, Group: "routes"}.String())
}

// BenchmarkResolveGroupDependencies measures expanding group dependencies when
// many consumers each depend on their own value group.
func BenchmarkResolveGroupDependencies(b *testing.B) {
	const groups = 500
	providers := make([]*testProvider, 0, 2*groups)
	for i := range groups {
		group := fmt.Sprintf("g%d", i)
		providers = append(providers,
			&testProvider{Type: memberType, Key: 1, Group: group},
			&testProvider{
				Type:         consumerType,
				Key:          group,
				Dependencies: []*reflection.Dependency{{Type: memberType, Group: group}},
			},
		)
	}

	b.ReportAllocs()
	for b.Loop() {
		g := build(b, providers...)
		g.ResolveGroupDependencies()
	}
}
