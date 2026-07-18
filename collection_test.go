package godi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCollectionRegistration(t *testing.T) {
	t.Parallel()

	t.Run("registers_services_with_all_lifetimes", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()

		c.AddSingleton(NewTService)
		c.AddScoped(NewTDependency)
		c.AddTransient(NewTDisposable)

		assert.Equal(t, 3, c.Count())
		assert.True(t, c.Contains(PtrTypeOf[TService]()))
		assert.True(t, c.Contains(PtrTypeOf[TDependency]()))
		assert.True(t, c.Contains(PtrTypeOf[TDisposable]()))
	})

	t.Run("registers_keyed_services", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()

		c.AddSingleton(NewTServiceWithID("primary"), Name("primary"))
		c.AddSingleton(NewTServiceWithID("secondary"), Name("secondary"))

		assert.True(t, c.ContainsKeyed(PtrTypeOf[TService](), "primary"))
		assert.True(t, c.ContainsKeyed(PtrTypeOf[TService](), "secondary"))
		assert.False(t, c.Contains(PtrTypeOf[TService]())) // No default registration
	})

	t.Run("registers_grouped_services", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()

		c.AddSingleton(NewTServiceWithID("h1"), Group("handlers"))
		c.AddSingleton(NewTServiceWithID("h2"), Group("handlers"))

		p, err := c.Build()
		require.NoError(t, err)
		defer p.Close()

		services, err := p.GetGroup(PtrTypeOf[TService](), "handlers")
		require.NoError(t, err)
		assert.Len(t, services, 2)
	})

	t.Run("registers_interface_implementations", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()

		c.AddSingleton(NewTService, As[TInterface]())
		assert.True(t, c.Contains(TypeOf[TInterface]()))

		p, err := c.Build()
		require.NoError(t, err)
		defer p.Close()

		svc, err := p.Get(TypeOf[TInterface]())
		require.NoError(t, err)
		assert.NotNil(t, svc)
	})

	t.Run("registers_non_function_as_instance", func(t *testing.T) {
		t.Parallel()
		instance := &TService{ID: "pre-built", Value: 999}
		c := NewCollection()

		c.AddSingleton(instance)

		p, err := c.Build()
		require.NoError(t, err)
		defer p.Close()

		svc, err := Resolve[*TService](p)
		require.NoError(t, err)
		assert.Same(t, instance, svc)
	})

	t.Run("registers_multi_return_constructor", func(t *testing.T) {
		t.Parallel()
		invocations := 0
		ctor := func() (*TService, *TDependency) {
			invocations++
			return &TService{ID: "multi"}, &TDependency{Name: "multi"}
		}

		c := NewCollection()
		c.AddSingleton(ctor)

		p, err := c.Build()
		require.NoError(t, err)
		defer p.Close()

		// Both types should be resolvable
		svc, _ := Resolve[*TService](p)
		dep, _ := Resolve[*TDependency](p)
		assert.Equal(t, "multi", svc.ID)
		assert.Equal(t, "multi", dep.Name)

		// Constructor should only be called once
		assert.Equal(t, 1, invocations)
	})
}

func TestCollectionRegistrationErrors(t *testing.T) {
	t.Parallel()

	t.Run("rejects_duplicate_registration", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService)

		c.AddSingleton(NewTService)
		err := c.Err()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})

	t.Run("rejects_nil_constructor", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(nil)
		err := c.Err()
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrConstructorNil)
	})

	t.Run("rejects_name_and_group_together", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService, Name("n"), Group("g"))
		err := c.Err()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot use both")
	})

	t.Run("rejects_invalid_interface_binding", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()

		// Pointer to interface is invalid
		c.AddSingleton(NewTService, As[*TInterface]())
		err := c.Err()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pointer to an interface")
	})

	t.Run("rejects_non_final_error_return", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func() (*TMultiA, error, *TMultiB) { //nolint:staticcheck // ST1008: intentionally invalid signature; the test asserts registration rejects it
			return &TMultiA{}, nil, &TMultiB{}
		})
		err := c.Err()
		require.Error(t, err, "an error return before the last position must be rejected at registration")
		assert.Contains(t, err.Error(), "last return value")
	})

	t.Run("rejects_reserved_type_via_as", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(context.Background(), As[context.Context]())
		err := c.Err()
		require.Error(t, err, "reserved types must not be registrable via As")
		assert.Contains(t, err.Error(), "reserved")
	})

	t.Run("rejects_as_on_result_object", func(t *testing.T) {
		t.Parallel()
		type SimpleOut struct {
			Out
			Svc *TService
		}
		c := NewCollection()
		c.AddSingleton(func() SimpleOut {
			return SimpleOut{Svc: &TService{}}
		}, As[TInterface]())
		err := c.Err()
		require.Error(t, err, "godi.As must be rejected for result object constructors")
		assert.Contains(t, err.Error(), "result object")
	})

	t.Run("rejects_result_object_with_more_than_two_returns", func(t *testing.T) {
		t.Parallel()
		type SimpleOut struct {
			Out
			Svc *TService
		}
		c := NewCollection()
		c.AddSingleton(func() (SimpleOut, string, error) {
			return SimpleOut{Svc: &TService{}}, "", nil
		})
		err := c.Err()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "can return at most (Out, error)")
	})

	t.Run("rejects_result_object_with_non_error_second_return", func(t *testing.T) {
		t.Parallel()
		type SimpleOut struct {
			Out
			Svc *TService
		}
		c := NewCollection()
		c.AddSingleton(func() (SimpleOut, string) {
			return SimpleOut{Svc: &TService{}}, ""
		})
		err := c.Err()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must have error as its second return value")
	})

	t.Run("rejects_group_tag_on_non_slice_field", func(t *testing.T) {
		t.Parallel()
		type BadParams struct {
			In
			Svc *TService `group:"services"`
		}
		c := NewCollection()
		c.AddSingleton(func(p BadParams) *TDependency { return &TDependency{} })
		err := c.Err()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "must be a slice")
	})

	t.Run("rejects_in_struct_mixed_with_other_parameters", func(t *testing.T) {
		t.Parallel()
		type SomeParams struct {
			In
			Svc *TService
		}
		c := NewCollection()
		c.AddSingleton(func(p SomeParams, dep *TDependency) *TDisposable { return nil })
		err := c.Err()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "only parameter")
	})
}

func TestCollectionRemove(t *testing.T) {
	t.Parallel()

	t.Run("removes_all_registrations_of_type", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService)
		c.AddSingleton(NewTService, Name("keyed"))
		c.AddSingleton(NewTDependency)

		c.Remove(PtrTypeOf[TService]())

		assert.False(t, c.Contains(PtrTypeOf[TService]()))
		assert.False(t, c.ContainsKeyed(PtrTypeOf[TService](), "keyed")) // Keyed removed too
		assert.True(t, c.Contains(PtrTypeOf[TDependency]()))             // Other types untouched
	})

	t.Run("removes_keyed_registration", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService, Name("k1"))
		c.AddSingleton(NewTService, Name("k2"))

		c.RemoveKeyed(PtrTypeOf[TService](), "k1")

		assert.False(t, c.ContainsKeyed(PtrTypeOf[TService](), "k1"))
		assert.True(t, c.ContainsKeyed(PtrTypeOf[TService](), "k2"))
	})

	t.Run("build_does_not_construct_removed_singleton", func(t *testing.T) {
		t.Parallel()
		calls := 0
		ctor := func() *TService {
			calls++
			return &TService{ID: "real"}
		}

		c := NewCollection()
		c.AddSingleton(ctor)
		c.Remove(PtrTypeOf[TService]())

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		assert.Equal(t, 0, calls, "removed singleton constructor must not run at build")
		_, err = Resolve[*TService](p)
		require.Error(t, err)
	})

	t.Run("count_and_toslice_reflect_removal", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService)
		c.AddSingleton(NewTDependency)

		c.Remove(PtrTypeOf[TService]())

		assert.Equal(t, 1, c.Count())
		descriptors := c.ToSlice()
		require.Len(t, descriptors, 1)
		assert.Equal(t, PtrTypeOf[TDependency](), descriptors[0].ServiceType)
	})

	t.Run("remove_drops_keyed_and_grouped_registrations_of_type", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService)
		c.AddSingleton(NewTService, Name("keyed"))
		c.AddSingleton(NewTService, Group("grouped"))

		c.Remove(PtrTypeOf[TService]())

		assert.Equal(t, 0, c.Count())
		assert.False(t, c.Contains(PtrTypeOf[TService]()))
		assert.False(t, c.ContainsKeyed(PtrTypeOf[TService](), "keyed"))

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		group, err := p.GetGroup(PtrTypeOf[TService](), "grouped")
		require.NoError(t, err)
		assert.Empty(t, group)
	})

	t.Run("removekeyed_prunes_descriptor", func(t *testing.T) {
		t.Parallel()
		calls := 0
		ctor := func() *TService {
			calls++
			return &TService{ID: "keyed"}
		}

		c := NewCollection()
		c.AddSingleton(ctor, Name("a"))
		c.AddSingleton(NewTService)
		c.RemoveKeyed(PtrTypeOf[TService](), "a")

		assert.Equal(t, 1, c.Count())

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		assert.Equal(t, 0, calls, "removed keyed singleton constructor must not run at build")
		_, err = ResolveKeyed[*TService](p, "a")
		require.Error(t, err)
	})

	t.Run("reregister_after_remove_builds_cleanly", func(t *testing.T) {
		t.Parallel()
		realCalls := 0
		realCtor := func(dep *TDependency) *TService {
			realCalls++
			return &TService{ID: "real"}
		}
		mock := func() *TService { return &TService{ID: "mock"} }

		c := NewCollection()
		c.AddSingleton(NewTDependency)
		c.AddSingleton(realCtor)
		c.AddModules(
			Remove[*TService](),
			AddSingleton(mock),
		)

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		assert.Equal(t, 0, realCalls)
		svc, err := Resolve[*TService](p)
		require.NoError(t, err)
		assert.Equal(t, "mock", svc.ID)
	})
}

func TestCollectionModules(t *testing.T) {
	t.Parallel()

	t.Run("applies_module_registrations", func(t *testing.T) {
		t.Parallel()
		module := NewModule("test",
			AddSingleton(NewTService),
			AddScoped(NewTDependency),
		)

		c := NewCollection()
		c.AddModules(module)
		assert.Equal(t, 2, c.Count())
	})

	t.Run("applies_nested_modules", func(t *testing.T) {
		t.Parallel()
		inner := NewModule("inner", AddSingleton(NewTService))
		outer := NewModule("outer", inner, AddScoped(NewTDependency))

		c := NewCollection()
		c.AddModules(outer)
		assert.Equal(t, 2, c.Count())
	})

	t.Run("wraps_module_errors", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService)

		module := NewModule("dup", AddSingleton(NewTService))
		c.AddModules(module)
		err := c.Err()
		require.Error(t, err)

		var moduleErr *ModuleError
		assert.ErrorAs(t, err, &moduleErr)
		assert.Equal(t, "dup", moduleErr.Module)
	})
}

func TestCollectionBuild(t *testing.T) {
	t.Parallel()

	t.Run("builds_provider_with_dependency_chain", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService)
		c.AddSingleton(NewTDependency)
		c.AddSingleton(NewTServiceWithDeps)

		p, err := c.Build()
		require.NoError(t, err)
		defer p.Close()

		svc, err := Resolve[*TServiceWithDeps](p)
		require.NoError(t, err)
		assert.NotNil(t, svc.Svc)
		assert.NotNil(t, svc.Dep)
	})

	t.Run("detects_circular_dependencies", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTCircularA)
		c.AddSingleton(NewTCircularB)

		_, err := c.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "circular dependency")

		// The public CircularDependencyError must be matchable and expose the
		// cycle as plain strings (no leaked internal node-key type).
		var cycleErr *CircularDependencyError
		require.ErrorAs(t, err, &cycleErr)
		require.NotEmpty(t, cycleErr.Path)
		joined := strings.Join(cycleErr.Path, " -> ")
		assert.Contains(t, joined, "TCircular", "cycle path must name the involved types")
		assert.NotEmpty(t, cycleErr.Node)
	})

	t.Run("detects_lifetime_violations", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddScoped(NewTService)
		c.AddSingleton(func(s *TService) *TDependency {
			return &TDependency{Name: s.ID}
		})

		_, err := c.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "lifetime")
	})

	t.Run("respects_cancelled_context", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := c.BuildWithContext(ctx)
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})
}

func TestCollectionParameterObjects(t *testing.T) {
	t.Parallel()

	type Params struct {
		In
		Service *TService
		Dep     *TDependency `optional:"true"`
	}

	c := NewCollection()
	c.AddSingleton(NewTService)
	c.AddSingleton(func(p Params) *TServiceWithDeps {
		return &TServiceWithDeps{Svc: p.Service, Dep: p.Dep}
	})

	p, err := c.Build()
	require.NoError(t, err)
	defer p.Close()

	svc, err := Resolve[*TServiceWithDeps](p)
	require.NoError(t, err)
	assert.NotNil(t, svc.Svc)
	assert.Nil(t, svc.Dep) // Optional and not registered
}

func TestCollectionResultObjects(t *testing.T) {
	t.Parallel()

	// Use local types to avoid any potential shared type issues
	type ResultConfig struct{ Value string }
	type ResultLogger struct{ Level string }

	type ResultOut struct {
		Out
		Config *ResultConfig
		Logger *ResultLogger `name:"audit"`
	}

	c := NewCollection()
	c.AddSingleton(func() ResultOut {
		return ResultOut{
			Config: &ResultConfig{Value: "test-config"},
			Logger: &ResultLogger{Level: "info"},
		}
	})

	// Verify registration
	assert.True(t, c.Contains(reflect.TypeFor[*ResultConfig]()))
	assert.True(t, c.ContainsKeyed(reflect.TypeFor[*ResultLogger](), "audit"))

	p, err := c.Build()
	require.NoError(t, err)
	defer p.Close()

	cfg, err := p.Get(reflect.TypeFor[*ResultConfig]())
	require.NoError(t, err)
	assert.Equal(t, "test-config", cfg.(*ResultConfig).Value)

	logger, err := p.GetKeyed(reflect.TypeFor[*ResultLogger](), "audit")
	require.NoError(t, err)
	assert.Equal(t, "info", logger.(*ResultLogger).Level)
}

func TestSingletonConsumingGroupViaIn(t *testing.T) {
	t.Parallel()

	type RouteHandler struct{ Name string }

	type RouterParams struct {
		In
		Routes []*RouteHandler `group:"routes"`
	}

	type Router struct {
		Routes []*RouteHandler
	}

	newRouteHandler := func(name string) func() *RouteHandler {
		return func() *RouteHandler {
			return &RouteHandler{Name: name}
		}
	}

	newRouter := func(params RouterParams) *Router {
		return &Router{Routes: params.Routes}
	}

	c := NewCollection()
	c.AddSingleton(newRouteHandler("api"), Group("routes"))
	c.AddSingleton(newRouteHandler("web"), Group("routes"))
	c.AddSingleton(newRouter)

	p, err := c.Build()
	require.NoError(t, err)
	defer p.Close()

	router, err := Resolve[*Router](p)
	require.NoError(t, err)
	assert.NotNil(t, router)
	assert.Len(t, router.Routes, 2)

	// Verify both routes are present
	names := make([]string, len(router.Routes))
	for i, r := range router.Routes {
		names[i] = r.Name
	}
	assert.Contains(t, names, "api")
	assert.Contains(t, names, "web")
}

// TestMultiReturnWithAsRejected: when a multi-return constructor is paired
// with godi.As(...), the registration must fail. The pre-fix code silently
// ignored godi.As for multi-return constructors and registered the concrete
// types instead, which is almost certainly not what the caller wanted.
func TestMultiReturnWithAsRejected(t *testing.T) {
	t.Parallel()

	type asLeft struct{}
	type asRight struct{}
	type asIface interface {
		mark()
	}

	c := NewCollection()
	c.AddSingleton(func() (*asLeft, *asRight) {
		return &asLeft{}, &asRight{}
	}, As[asIface]())
	err := c.Err()

	require.Error(t, err, "multi-return + As must be rejected")
	var regErr *RegistrationError
	assert.ErrorAs(t, err, &regErr)
}

// TestAddServiceAnalyzeCalledOnce asserts that registering a single
// constructor invokes analyzer.Analyze exactly once. Pre-fix the path calls
// Analyze twice — once in newDescriptorWithAnalyzer and again in addService
// — even though the second is a cache hit. The fix folds the second call
// into the first by reusing the info already computed in
// newDescriptorWithAnalyzer.
func TestAddServiceAnalyzeCalledOnce(t *testing.T) {
	t.Parallel()

	col := NewCollection().(*collection)
	before := col.analyzer.AnalyzeCalls()
	col.AddSingleton(NewTService)
	delta := col.analyzer.AnalyzeCalls() - before

	assert.Equal(t, int64(1), delta,
		"AddSingleton must call analyzer.Analyze exactly once (got %d)", delta)
}

func TestGroupLifetimeValidation(t *testing.T) {
	t.Parallel()

	type Handler struct{ Name string }

	type AppParams struct {
		In
		Handlers []*Handler `group:"handlers"`
	}

	type App struct{}

	c := NewCollection()
	// Scoped group member
	c.AddScoped(func() *Handler {
		return &Handler{Name: "scoped"}
	}, Group("handlers"))
	// Singleton consumer of the group
	c.AddSingleton(func(params AppParams) *App {
		return &App{}
	})

	_, err := c.Build()
	assert.Error(t, err, "Singleton consuming scoped group member should fail lifetime validation")
	assert.Contains(t, err.Error(), "lifetime")
}

func TestMultiReturnWithName(t *testing.T) {
	t.Parallel()

	calls := 0
	ctor := func() (*TMultiA, *TMultiB) {
		calls++
		return &TMultiA{N: 1}, &TMultiB{N: 2}
	}

	c := NewCollection()
	c.AddSingleton(ctor, Name("primary"))

	p, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	a, err := ResolveKeyed[*TMultiA](p, "primary")
	require.NoError(t, err)
	assert.Equal(t, 1, a.N)

	b, err := Resolve[*TMultiB](p)
	require.NoError(t, err)
	assert.Equal(t, 2, b.N)

	assert.Equal(t, 1, calls, "singleton multi-return constructor must run exactly once")
}

func TestMultiReturnWithGroup(t *testing.T) {
	t.Parallel()

	c := NewCollection()
	c.AddSingleton(func() (*TMultiA, *TMultiB) {
		return &TMultiA{N: 1}, &TMultiB{N: 2}
	}, Group("g"))

	p, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	as, err := ResolveGroup[*TMultiA](p, "g")
	require.NoError(t, err)
	require.Len(t, as, 1)
	assert.Equal(t, 1, as[0].N)

	bs, err := ResolveGroup[*TMultiB](p, "g")
	require.NoError(t, err)
	require.Len(t, bs, 1)
	assert.Equal(t, 2, bs[0].N)
}

func TestResultObjectWithGroupField(t *testing.T) {
	t.Parallel()

	t.Run("singleton", func(t *testing.T) {
		t.Parallel()
		calls := 0
		ctor := func() TResult {
			calls++
			return TResult{
				Primary:   &TService{ID: "primary"},
				Secondary: &TService{ID: "secondary"},
				Grouped:   &TService{ID: "grouped"},
			}
		}

		c := NewCollection()
		c.AddSingleton(ctor)

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		primary, err := Resolve[*TService](p)
		require.NoError(t, err)
		assert.Equal(t, "primary", primary.ID)

		secondary, err := ResolveKeyed[*TService](p, "secondary")
		require.NoError(t, err)
		assert.Equal(t, "secondary", secondary.ID)

		grouped, err := ResolveGroup[*TService](p, "services")
		require.NoError(t, err)
		require.Len(t, grouped, 1)
		assert.Equal(t, "grouped", grouped[0].ID)

		assert.Equal(t, 1, calls, "result object constructor must run exactly once")
	})

	t.Run("scoped_shares_one_invocation_per_scope", func(t *testing.T) {
		t.Parallel()
		calls := 0
		ctor := func() TResult {
			calls++
			return TResult{
				Primary:   &TService{ID: "primary"},
				Secondary: &TService{ID: "secondary"},
				Grouped:   &TService{ID: "grouped"},
			}
		}

		c := NewCollection()
		c.AddScoped(ctor)

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		s, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = s.Close() })

		_, err = Resolve[*TService](s)
		require.NoError(t, err)
		_, err = ResolveKeyed[*TService](s, "secondary")
		require.NoError(t, err)
		grouped, err := ResolveGroup[*TService](s, "services")
		require.NoError(t, err)
		require.Len(t, grouped, 1)

		assert.Equal(t, 1, calls, "all fields of one result object must come from one invocation per scope")
	})
}

type TFailing struct{}

type TOptionalParams struct {
	In
	Failing *TFailing `optional:"true"`
}

type TOptionalConsumer struct{ Failing *TFailing }

func TestOptionalDependencyErrors(t *testing.T) {
	t.Parallel()

	t.Run("missing_optional_dependency_is_skipped", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddScoped(func(p TOptionalParams) *TOptionalConsumer {
			return &TOptionalConsumer{Failing: p.Failing}
		})

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		consumer, err := Resolve[*TOptionalConsumer](p)
		require.NoError(t, err)
		assert.Nil(t, consumer.Failing)
	})

	t.Run("optional_dependency_with_missing_transitive_dependency_propagates", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		// TFailing IS registered, but its own dependency is not: this is a
		// construction failure of a registered service, not a missing
		// optional, so it must propagate.
		c.AddScoped(func(dep *TDisposable) *TFailing {
			return &TFailing{}
		})
		c.AddScoped(func(p TOptionalParams) *TOptionalConsumer {
			return &TOptionalConsumer{Failing: p.Failing}
		})

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		_, err = Resolve[*TOptionalConsumer](p)
		require.Error(t, err, "missing transitive dependency of an optional service must propagate")
	})

	t.Run("failing_constructor_of_optional_dependency_propagates", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddScoped(func() (*TFailing, error) {
			return nil, errors.New("constructor exploded")
		})
		c.AddScoped(func(p TOptionalParams) *TOptionalConsumer {
			return &TOptionalConsumer{Failing: p.Failing}
		})

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		_, err = Resolve[*TOptionalConsumer](p)
		require.Error(t, err, "a registered optional dependency whose constructor fails must propagate the error")
		assert.Contains(t, err.Error(), "constructor exploded")
	})
}

// Registering the same multi-return constructor twice (legal via groups,
// which assign unique numeric keys) must give each registration its own
// single-flight: concurrent resolution of members from the two registrations
// must not collide.
func TestMultiReturnSameConstructorTwoGroups(t *testing.T) {
	t.Parallel()

	ctor := func() (*TMultiA, *TMultiB) {
		return &TMultiA{N: 1}, &TMultiB{N: 2}
	}

	for range 200 {
		c := NewCollection()
		c.AddScoped(ctor, Group("g1"))
		c.AddScoped(ctor, Group("g2"))

		p, err := c.Build()
		require.NoError(t, err)

		s, err := p.CreateScope(context.Background())
		require.NoError(t, err)

		var wg sync.WaitGroup
		errs := make([]error, 2)
		wg.Add(2)
		go func() { defer wg.Done(); _, errs[0] = ResolveGroup[*TMultiA](s, "g1") }()
		go func() { defer wg.Done(); _, errs[1] = ResolveGroup[*TMultiA](s, "g2") }()
		wg.Wait()

		require.NoError(t, errs[0])
		require.NoError(t, errs[1])

		_ = s.Close()
		_ = p.Close()
	}
}

// Removing one return type of a multi-return constructor and re-registering
// a replacement must not let the old constructor's cached sibling value
// shadow the replacement.
func TestRemoveUnlinksSiblings(t *testing.T) {
	t.Parallel()

	c := NewCollection()
	c.AddSingleton(func() (*TMultiA, *TMultiB) {
		return &TMultiA{N: 1}, &TMultiB{N: 2}
	})
	c.Remove(PtrTypeOf[TMultiA]())
	c.AddSingleton(func() *TMultiA { return &TMultiA{N: 99} })

	p, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	// Resolve B first so the old constructor runs and caches its siblings.
	b, err := Resolve[*TMultiB](p)
	require.NoError(t, err)
	assert.Equal(t, 2, b.N)

	// A must come from the replacement registration, not the removed sibling.
	a, err := Resolve[*TMultiA](p)
	require.NoError(t, err)
	assert.Equal(t, 99, a.N, "removed sibling must not shadow the replacement registration")
}

// An Out struct with same-type fields in two different groups gets numeric
// keys that collide across groups; primary detection must compare the group
// as well so each group member resolves to its own field's value.
func TestResultObjectSameTypeTwoGroups(t *testing.T) {
	t.Parallel()

	type TwoGroupResult struct {
		Out
		First  *TService `group:"rg1"`
		Second *TService `group:"rg2"`
	}

	c := NewCollection()
	c.AddScoped(func() TwoGroupResult {
		return TwoGroupResult{
			First:  &TService{ID: "first"},
			Second: &TService{ID: "second"},
		}
	})

	p, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })

	s, err := p.CreateScope(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })

	g1, err := ResolveGroup[*TService](s, "rg1")
	require.NoError(t, err)
	require.Len(t, g1, 1)
	assert.Equal(t, "first", g1[0].ID)

	g2, err := ResolveGroup[*TService](s, "rg2")
	require.NoError(t, err)
	require.Len(t, g2, 1)
	assert.Equal(t, "second", g2[0].ID)
}

// A failed multi-descriptor registration must roll back the descriptors it
// already registered: phantom sibling links would otherwise corrupt primary
// detection and scoped caching for callers that ignore the Add error.
func TestFailedRegistrationLeavesNoPhantoms(t *testing.T) {
	t.Parallel()

	t.Run("multi_return", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		// Second unkeyed *TMultiA collides with the first: registration
		// must fail and leave the collection untouched.
		c.AddSingleton(func() (*TMultiA, *TMultiA) {
			return &TMultiA{N: 1}, &TMultiA{N: 2}
		})
		err := c.Err()
		require.Error(t, err)
		assert.Equal(t, 0, c.Count(), "failed registration must leave no descriptors behind")

		// A subsequent valid registration must work and resolve to its own
		// constructor's value. Build reports recorded errors, so use a fresh
		// collection for the rebuild.
		c = NewCollection()
		c.AddSingleton(func() *TMultiA { return &TMultiA{N: 99} })
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		a, err := Resolve[*TMultiA](p)
		require.NoError(t, err)
		assert.Equal(t, 99, a.N)
	})

	t.Run("result_object", func(t *testing.T) {
		t.Parallel()
		type DupOut struct {
			Out
			First  *TMultiA
			Second *TMultiA
		}
		c := NewCollection()
		c.AddSingleton(func() DupOut {
			return DupOut{First: &TMultiA{N: 1}, Second: &TMultiA{N: 2}}
		})
		err := c.Err()
		require.Error(t, err)
		assert.Equal(t, 0, c.Count(), "failed registration must leave no descriptors behind")
	})
}

func TestResultObjectFieldNameAndGroupRejected(t *testing.T) {
	t.Parallel()

	type BadOut struct {
		Out
		Svc *TService `name:"x" group:"g"`
	}
	c := NewCollection()
	c.AddSingleton(func() BadOut {
		return BadOut{Svc: &TService{}}
	})
	err := c.Err()
	require.Error(t, err, "a field with both name and group tags must be rejected")
	assert.Contains(t, err.Error(), "both name and group")
	assert.Equal(t, 0, c.Count())
}

func TestAsOnVoidConstructorRejected(t *testing.T) {
	t.Parallel()

	c := NewCollection()
	c.AddSingleton(func() error { return nil }, As[any]())
	err := c.Err()
	require.Error(t, err, "godi.As on a constructor with no service return must be rejected")
	assert.Contains(t, err.Error(), "no service value")
	assert.Equal(t, 0, c.Count())
}

// Issue #28: Add* methods record errors instead of returning them; Build
// (and Err) report everything at once.
func TestDeferredRegistrationErrors(t *testing.T) {
	t.Parallel()

	t.Run("build_reports_all_recorded_errors", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(nil)                             // error 1: nil constructor
		c.AddSingleton(NewTService)                     // fine
		c.AddSingleton(NewTService)                     // error 2: duplicate
		c.AddScoped(NewTService, Name("n"), Group("g")) // error 3: name+group

		_, err := c.Build()
		require.Error(t, err)

		var buildErr *BuildError
		require.ErrorAs(t, err, &buildErr)
		assert.Equal(t, "registration", buildErr.Phase)

		msg := err.Error()
		assert.Contains(t, msg, "constructor cannot be nil")
		assert.Contains(t, msg, "already registered")
		assert.Contains(t, msg, "cannot use both")
	})

	t.Run("err_is_nil_when_all_registrations_succeed", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService)
		c.AddScoped(NewTDependency)
		require.NoError(t, c.Err())

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
	})

	t.Run("module_errors_are_attributed_through_build", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddModules(NewModule("outer",
			NewModule("inner",
				AddSingleton(nil),
			),
		))

		_, err := c.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), `module "outer"`)
		assert.Contains(t, err.Error(), `module "inner"`)

		var moduleErr *ModuleError
		require.ErrorAs(t, err, &moduleErr)
		assert.Equal(t, "outer", moduleErr.Module)
	})

	t.Run("err_matches_sentinels_through_join", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(nil)
		require.ErrorIs(t, c.Err(), ErrConstructorNil)
	})
}

// Covers unregisterDescriptors' group-member rollback branch: an Out struct
// whose earlier field is grouped and whose later field duplicates a keyed
// registration, so rollback must remove both a group member and a keyed
// service.
func TestFailedRegistrationRollsBackGroupMember(t *testing.T) {
	t.Parallel()

	type RollbackOut struct {
		Out
		Grouped *TService    `group:"g"`
		First   *TDependency `name:"dup"`
		Second  *TDependency `name:"dup"` // duplicate keyed -> registration fails
	}

	c := NewCollection()
	c.AddSingleton(func() RollbackOut {
		return RollbackOut{
			Grouped: &TService{},
			First:   &TDependency{},
			Second:  &TDependency{},
		}
	})
	require.Error(t, c.Err())

	// Everything from the failed registration must be gone: no leftover
	// group member, no leftover keyed service, nothing in allDescriptors.
	assert.Equal(t, 0, c.Count(), "failed registration must leave no descriptors")
	assert.False(t, c.ContainsKeyed(PtrTypeOf[TDependency](), "dup"))
	assert.False(t, c.(*collection).HasGroup(PtrTypeOf[TService](), "g"))
}

func TestBuildWithOptions(t *testing.T) {
	t.Parallel()

	t.Run("nil_options_builds", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService)
		p, err := c.BuildWithOptions(nil)
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		svc, err := Resolve[*TService](p)
		require.NoError(t, err)
		assert.NotNil(t, svc)
	})

	t.Run("generous_timeout_builds", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService)
		p, err := c.BuildWithOptions(&ProviderOptions{BuildTimeout: time.Minute})
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
	})

	t.Run("reports_registration_errors", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(nil)
		_, err := c.BuildWithOptions(&ProviderOptions{})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "constructor cannot be nil")
	})
}

// ToSlice returns a read-only ServiceInfo view exposing identity + lifetime,
// not the internal descriptor.
func TestToSliceServiceInfo(t *testing.T) {
	t.Parallel()

	c := NewCollection()
	c.AddSingleton(NewTService)
	c.AddScoped(NewTServiceWithID("k"), Name("keyed"))
	c.AddTransient(NewTServiceWithID("g"), Group("grp"))

	infos := c.ToSlice()
	require.Len(t, infos, 3)

	byLifetime := map[Lifetime]ServiceInfo{}
	for _, info := range infos {
		assert.Equal(t, PtrTypeOf[TService](), info.ServiceType)
		byLifetime[info.Lifetime] = info
	}

	require.Contains(t, byLifetime, Singleton)
	assert.Nil(t, byLifetime[Singleton].Key)
	assert.Empty(t, byLifetime[Singleton].Group)

	require.Contains(t, byLifetime, Scoped)
	assert.Equal(t, "keyed", byLifetime[Scoped].Key)

	require.Contains(t, byLifetime, Transient)
	assert.Equal(t, "grp", byLifetime[Transient].Group)

	// The returned slice is a decoupled snapshot: mutating it must not affect
	// the collection or a subsequent ToSlice call.
	infos[0].ServiceType = nil
	infos[0].Key = "tampered"
	infos[0].Group = "tampered"
	infos[0].Lifetime = Lifetime(99)

	fresh := c.ToSlice()
	require.Len(t, fresh, 3)
	for _, info := range fresh {
		assert.Equal(t, PtrTypeOf[TService](), info.ServiceType, "mutation leaked into the collection")
		assert.NotEqual(t, "tampered", info.Key)
		assert.NotEqual(t, "tampered", info.Group)
	}
}

func TestBuildCancellation(t *testing.T) {
	t.Parallel()

	// The build context is cancelled by the constructors themselves rather
	// than by racing a wall-clock deadline against Build's setup, so these
	// tests cannot flake on a loaded runner.

	t.Run("cancellation_fails_build_after_constructor_returns", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		constructorReturned := false
		c := NewCollection()
		c.AddSingleton(func() *TService {
			cancel()
			constructorReturned = true
			return NewTService()
		})

		p, err := c.BuildWithContext(ctx)

		// Build waits for the non-cooperative constructor and still fails on
		// the cancellation it cannot deliver mid-flight.
		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, p)
		assert.True(t, constructorReturned)
	})

	t.Run("constructor_observes_deadline_cancellation", func(t *testing.T) {
		t.Parallel()
		observedCancellation := false
		c := NewCollection()
		c.AddSingleton(func(ctx context.Context) (*TService, error) {
			// Cooperative constructor: block until the build deadline fires.
			<-ctx.Done()
			observedCancellation = true
			return nil, ctx.Err()
		})

		p, err := c.BuildWithOptions(&ProviderOptions{BuildTimeout: 200 * time.Millisecond})

		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Nil(t, p)
		assert.True(t, observedCancellation)
	})

	t.Run("cancellation_cleans_partial_singletons", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		resource := NewTDisposable()
		c := NewCollection()
		c.AddSingleton(func() *TDisposable { return resource })
		c.AddSingleton(func(*TDisposable) *TService {
			cancel()
			return NewTService()
		})

		p, err := c.BuildWithContext(ctx)
		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, p)
		assert.True(t, resource.IsClosed())
	})

	t.Run("cancellation_error_remains_primary_when_cleanup_fails", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cleanupErr := errors.New("cleanup failed")
		c := NewCollection()
		c.AddSingleton(func() *TDisposable {
			d := NewTDisposable()
			d.SetCloseError(cleanupErr)
			return d
		})
		c.AddSingleton(func(*TDisposable) *TService {
			cancel()
			return NewTService()
		})

		p, err := c.BuildWithContext(ctx)
		require.Error(t, err)
		assert.Nil(t, p)
		assert.ErrorIs(t, err, context.Canceled)
		assert.ErrorIs(t, err, cleanupErr)
	})
}

func TestBuildContext(t *testing.T) {
	t.Parallel()

	t.Run("visible_to_eager_constructors", func(t *testing.T) {
		t.Parallel()
		type contextKey struct{}
		want := &TService{ID: "build-context"}
		buildCtx := context.WithValue(context.Background(), contextKey{}, want)

		c := NewCollection()
		c.AddSingleton(func(ctx context.Context) (*TService, error) {
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
	})

	t.Run("override_is_race_safe", func(t *testing.T) {
		t.Parallel()
		started := make(chan struct{})
		stop := make(chan struct{})
		var readers sync.WaitGroup

		c := NewCollection()
		c.AddSingleton(func(p Provider) *TService {
			readers.Go(func() {
				close(started)
				for {
					select {
					case <-stop:
						return
					default:
						_, _ = p.Get(contextType)
					}
				}
			})
			<-started
			return NewTService()
		})

		p, err := c.BuildWithContext(context.Background())
		close(stop)
		readers.Wait()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
	})
}

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
