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
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Service fixtures shared by the package's tests.

// TService is a basic service.
type TService struct {
	ID    string
	Value int
}

func (s *TService) GetID() string { return s.ID }

// TDependency is a basic dependency.
type TDependency struct {
	Name string
}

// TServiceWithDeps depends on TService and TDependency.
type TServiceWithDeps struct {
	Svc *TService
	Dep *TDependency
}

// TInterface is implemented by *TService.
type TInterface interface {
	GetID() string
}

// TMultiA and TMultiB are produced together by multi-return constructors.
type TMultiA struct{ N int }
type TMultiB struct{ N int }

// TCircularA and TCircularB depend on each other.
type TCircularA struct{ B *TCircularB }
type TCircularB struct{ A *TCircularA }

func NewTCircularA(b *TCircularB) *TCircularA { return &TCircularA{B: b} }
func NewTCircularB(a *TCircularA) *TCircularB { return &TCircularB{A: a} }

// TResult is a result object.
type TResult struct {
	Out
	Primary   *TService
	Secondary *TService `name:"secondary"`
	Grouped   *TService `group:"services"`
}

func NewTService() *TService {
	return &TService{ID: "test", Value: 42}
}

func NewTServiceWithID(id string) func() *TService {
	return func() *TService {
		return &TService{ID: id, Value: 42}
	}
}

func NewTDependency() *TDependency {
	return &TDependency{Name: "dep"}
}

func NewTServiceWithDeps(svc *TService, dep *TDependency) *TServiceWithDeps {
	return &TServiceWithDeps{Svc: svc, Dep: dep}
}

func NewTServiceError() (*TService, error) {
	return nil, errors.New("constructor error")
}

func NewTMultiReturnWithError() (*TService, *TDependency, error) {
	return &TService{ID: "multi-err", Value: 2}, &TDependency{Name: "multi-err-dep"}, nil
}

func NewTResult() TResult {
	return TResult{
		Primary:   &TService{ID: "primary", Value: 1},
		Secondary: &TService{ID: "secondary", Value: 2},
		Grouped:   &TService{ID: "grouped", Value: 3},
	}
}

// NewTVoid returns no service.
func NewTVoid() {}

// RequireResolve resolves a service or fails the test.
func RequireResolve[T any](t *testing.T, p Provider) T {
	t.Helper()
	v, err := Resolve[T](p)
	require.NoError(t, err)
	return v
}

// RequireResolveFrom resolves from a scope or fails the test.
func RequireResolveFrom[T any](t *testing.T, s Scope) T {
	t.Helper()
	v, err := Resolve[T](s)
	require.NoError(t, err)
	return v
}

func TestCollectionRegistration(t *testing.T) {
	t.Parallel()

	t.Run("registers_services_with_all_lifetimes", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()

		c.AddSingleton(NewTService)
		c.AddScoped(NewTDependency)
		c.AddTransient(NewTDisposable)

		assert.Equal(t, 3, c.Count())
		assert.True(t, c.Contains(reflect.TypeFor[*TService]()))
		assert.True(t, c.Contains(reflect.TypeFor[*TDependency]()))
		assert.True(t, c.Contains(reflect.TypeFor[*TDisposable]()))
	})

	t.Run("registers_keyed_services", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()

		c.AddSingleton(NewTServiceWithID("primary"), Name("primary"))
		c.AddSingleton(NewTServiceWithID("secondary"), Name("secondary"))

		assert.True(t, c.ContainsKeyed(reflect.TypeFor[*TService](), "primary"))
		assert.True(t, c.ContainsKeyed(reflect.TypeFor[*TService](), "secondary"))
		assert.False(t, c.Contains(reflect.TypeFor[*TService]())) // No default registration
	})

	t.Run("registers_grouped_services", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()

		c.AddSingleton(NewTServiceWithID("h1"), Group("handlers"))
		c.AddSingleton(NewTServiceWithID("h2"), Group("handlers"))

		p, err := c.Build()
		require.NoError(t, err)
		defer p.Close()

		services, err := p.GetGroup(reflect.TypeFor[*TService](), "handlers")
		require.NoError(t, err)
		assert.Len(t, services, 2)
	})

	t.Run("registers_interface_implementations", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()

		c.AddSingleton(NewTService, As[TInterface]())
		assert.True(t, c.Contains(reflect.TypeFor[TInterface]()))

		p, err := c.Build()
		require.NoError(t, err)
		defer p.Close()

		svc, err := p.Get(reflect.TypeFor[TInterface]())
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

	// Found by FuzzRegistrationValidation: every output of a constructor is a
	// service, so the rules for its first return value apply to all of them.
	t.Run("rejects_reserved_types_as_any_output", func(t *testing.T) {
		t.Parallel()
		type Results struct {
			Out
			Ctx context.Context
		}
		for name, ctor := range map[string]any{
			"multi_return": func() (*TService, context.Context) { return &TService{}, context.Background() },
			"out_field":    func() Results { return Results{Ctx: context.Background()} },
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				c := NewCollection()
				c.AddSingleton(ctor)
				require.Error(t, c.Err())
				assert.Contains(t, c.Err().Error(), "reserved")
			})
		}
	})

	t.Run("rejects_out_fields_of_unsupported_types", func(t *testing.T) {
		t.Parallel()
		type ChanOut struct {
			Out
			C chan int
		}
		type ErrorOut struct {
			Out
			Err error
		}
		for name, ctor := range map[string]any{
			"chan":  func() ChanOut { return ChanOut{C: make(chan int)} },
			"error": func() ErrorOut { return ErrorOut{Err: errors.New("x")} },
		} {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				c := NewCollection()
				c.AddSingleton(ctor)
				require.Error(t, c.Err(), "an Out field is checked like a constructor result")
			})
		}
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

	t.Run("rejects_non_nilable_error_return", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func() (*TService, structError) { return NewTService(), structError{} })

		// A struct error can never be nil, so the constructor would always
		// fail; previously resolution panicked calling IsNil on it.
		var err error
		require.NotPanics(t, func() { _, err = c.Build() })
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error return")
	})

	t.Run("rejects_multiple_error_returns", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func() (error, error) { return errors.New("init failed"), nil })

		// Only the last error is inspected, so the first would be dropped
		// and Build would succeed despite the failure.
		err := c.Err()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "error return must be the last return value")
	})

	t.Run("rejects_lazy_without_a_service", func(t *testing.T) {
		t.Parallel()
		// A void constructor has no resolvable output, so a lazy one would
		// silently never run.
		c := NewCollection()
		c.AddSingleton(func(*TService) {}, Lazy())
		require.Error(t, c.Err())
	})

	t.Run("rejects_in_field_with_name_and_group", func(t *testing.T) {
		t.Parallel()
		type Item struct{ ID int }
		type Holder struct{ Items []*Item }
		type Params struct {
			In
			Items []*Item `name:"x" group:"items"`
		}
		c := NewCollection()
		c.AddScoped(func() *Item { return &Item{} }, Group("items"))
		c.AddSingleton(func(p Params) *Holder { return &Holder{Items: p.Items} })

		// Accepting both tags let a singleton receive scoped group members:
		// lifetime validation keyed on the name while resolution used the group.
		_, err := c.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "both name and group")
	})
}

// structError is an error implemented by a non-nilable struct type.
type structError struct{}

func (structError) Error() string { return "struct error" }

func TestCollectionRemove(t *testing.T) {
	t.Parallel()

	t.Run("removes_all_registrations_of_type", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService)
		c.AddSingleton(NewTService, Name("keyed"))
		c.AddSingleton(NewTDependency)

		c.Remove(reflect.TypeFor[*TService]())

		assert.False(t, c.Contains(reflect.TypeFor[*TService]()))
		assert.False(t, c.ContainsKeyed(reflect.TypeFor[*TService](), "keyed")) // Keyed removed too
		assert.True(t, c.Contains(reflect.TypeFor[*TDependency]()))             // Other types untouched
	})

	t.Run("removes_keyed_registration", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService, Name("k1"))
		c.AddSingleton(NewTService, Name("k2"))

		c.RemoveKeyed(reflect.TypeFor[*TService](), "k1")

		assert.False(t, c.ContainsKeyed(reflect.TypeFor[*TService](), "k1"))
		assert.True(t, c.ContainsKeyed(reflect.TypeFor[*TService](), "k2"))
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
		c.Remove(reflect.TypeFor[*TService]())

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		assert.Equal(t, 0, calls, "removed singleton constructor must not run at build")
		_, err = Resolve[*TService](p)
		require.Error(t, err)
	})

	t.Run("removed_output_is_still_disposed", func(t *testing.T) {
		t.Parallel()
		type ResourceOut struct {
			Out
			Resource *TDisposable
			Service  *TService
		}
		registrations := map[string]func(Collection, *atomic.Pointer[TDisposable]){
			"multi_return": func(c Collection, produced *atomic.Pointer[TDisposable]) {
				c.AddScoped(func() (*TDisposable, *TService) {
					d := NewTDisposable()
					produced.Store(d)
					return d, NewTService()
				})
			},
			"result_object": func(c Collection, produced *atomic.Pointer[TDisposable]) {
				c.AddScoped(func() ResourceOut {
					d := NewTDisposable()
					produced.Store(d)
					return ResourceOut{Resource: d, Service: NewTService()}
				})
			},
		}
		for name, register := range registrations {
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				var produced atomic.Pointer[TDisposable]
				c := NewCollection()
				register(c, &produced)
				c.Remove(reflect.TypeFor[*TDisposable]())

				p, err := c.Build()
				require.NoError(t, err)
				t.Cleanup(func() { _ = p.Close() })

				s, err := p.CreateScope(context.Background())
				require.NoError(t, err)
				_, err = Resolve[*TService](s)
				require.NoError(t, err)
				require.NoError(t, s.Close())

				// The constructor still produces the removed output, so the
				// scope that ran it owns its cleanup.
				require.NotNil(t, produced.Load())
				assert.True(t, produced.Load().IsClosed(), "removed output must still be closed")
			})
		}
	})

	t.Run("count_and_toslice_reflect_removal", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService)
		c.AddSingleton(NewTDependency)

		c.Remove(reflect.TypeFor[*TService]())

		assert.Equal(t, 1, c.Count())
		descriptors := c.ToSlice()
		require.Len(t, descriptors, 1)
		assert.Equal(t, reflect.TypeFor[*TDependency](), descriptors[0].ServiceType)
	})

	t.Run("remove_drops_keyed_and_grouped_registrations_of_type", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService)
		c.AddSingleton(NewTService, Name("keyed"))
		c.AddSingleton(NewTService, Group("grouped"))

		c.Remove(reflect.TypeFor[*TService]())

		assert.Equal(t, 0, c.Count())
		assert.False(t, c.Contains(reflect.TypeFor[*TService]()))
		assert.False(t, c.ContainsKeyed(reflect.TypeFor[*TService](), "keyed"))

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		group, err := p.GetGroup(reflect.TypeFor[*TService](), "grouped")
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
		c.RemoveKeyed(reflect.TypeFor[*TService](), "a")

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

		_, err := c.Build(WithContext(ctx))
		require.Error(t, err)
		assert.ErrorIs(t, err, context.Canceled)
	})

	t.Run("detects_missing_dependencies_for_every_lifetime", func(t *testing.T) {
		t.Parallel()
		type Missing struct{}
		type NeedsMissing struct{}
		ctor := func(*Missing) *NeedsMissing { return &NeedsMissing{} }

		for _, lifetime := range []Lifetime{Singleton, Scoped, Transient} {
			t.Run(lifetime.String(), func(t *testing.T) {
				t.Parallel()
				c := NewCollection()
				switch lifetime {
				case Singleton:
					c.AddSingleton(ctor)
				case Scoped:
					c.AddScoped(ctor)
				case Transient:
					c.AddTransient(ctor)
				}

				_, err := c.Build()
				require.Error(t, err, "a missing dependency must fail Build, not the first resolution")
				assert.ErrorIs(t, err, ErrServiceNotFound)
				assert.Contains(t, err.Error(), "*godi.NeedsMissing requires *godi.Missing")
			})
		}
	})

	t.Run("missing_optional_dependency_does_not_fail_build", func(t *testing.T) {
		t.Parallel()
		type Missing struct{}
		type Params struct {
			In
			Missing *Missing `optional:"true"`
		}
		c := NewCollection()
		c.AddScoped(func(p Params) *TService { return NewTService() })

		p, err := c.Build()
		require.NoError(t, err)
		require.NoError(t, p.Close())
	})

	t.Run("constructor_may_inspect_collection", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func() *TService { return &TService{Value: c.Count()} })

		done := make(chan error, 1)
		go func() {
			p, err := c.Build()
			if err == nil {
				err = p.Close()
			}
			done <- err
		}()

		select {
		case err := <-done:
			require.NoError(t, err)
		case <-time.After(5 * time.Second):
			t.Fatal("Build deadlocked: it held the collection lock while running constructors")
		}
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

// A nil Out field means the constructor did not provide that output.
func TestResultObjectAbsentOutputs(t *testing.T) {
	t.Parallel()

	type Present struct{ N int32 }
	type Absent struct{}
	type PartialOut struct {
		Out
		Present *Present
		Absent  *Absent
	}

	t.Run("singleton_builds_and_runs_once", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int32
		c := NewCollection()
		c.AddSingleton(func() PartialOut {
			calls.Add(1)
			return PartialOut{Present: &Present{}}
		})

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		_, err = Resolve[*Present](p)
		require.NoError(t, err)
		_, err = Resolve[*Absent](p)
		require.ErrorIs(t, err, ErrServiceNotFound)
		assert.Equal(t, int32(1), calls.Load(), "constructor must run exactly once")
	})

	t.Run("scoped_keeps_sibling_identity", func(t *testing.T) {
		t.Parallel()
		var calls atomic.Int32
		c := NewCollection()
		c.AddScoped(func() PartialOut {
			return PartialOut{Present: &Present{N: calls.Add(1)}}
		})

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		s, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = s.Close() })

		first, err := Resolve[*Present](s)
		require.NoError(t, err)
		_, err = Resolve[*Absent](s)
		require.ErrorIs(t, err, ErrServiceNotFound)
		second, err := Resolve[*Present](s)
		require.NoError(t, err)

		// Resolving the absent output used to re-run the constructor and
		// overwrite the cached sibling.
		assert.Same(t, first, second, "a scoped instance must be unique within its scope")
		assert.Equal(t, int32(1), calls.Load(), "constructor must run once per scope")
	})

	t.Run("optional_dependency_receives_nil", func(t *testing.T) {
		t.Parallel()
		type Params struct {
			In
			Absent *Absent `optional:"true"`
		}
		c := NewCollection()
		c.AddSingleton(func() PartialOut { return PartialOut{Present: &Present{}} })
		c.AddSingleton(func(p Params) *TService {
			return &TService{ID: fmt.Sprint(p.Absent != nil)}
		})

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		svc, err := Resolve[*TService](p)
		require.NoError(t, err)
		assert.Equal(t, "false", svc.ID)
	})

	t.Run("group_skips_absent_members", func(t *testing.T) {
		t.Parallel()
		type Item struct{ ID int }
		type GroupOut struct {
			Out
			First  *Item `group:"items"`
			Second *Item `group:"items"`
		}
		c := NewCollection()
		c.AddScoped(func() GroupOut { return GroupOut{First: &Item{ID: 1}} })

		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		s, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = s.Close() })

		items, err := ResolveGroup[*Item](s, "items")
		require.NoError(t, err)
		require.Len(t, items, 1)
		assert.Equal(t, 1, items[0].ID)
	})
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

	// The name applies to every output, as Group does.
	b, err := ResolveKeyed[*TMultiB](p, "primary")
	require.NoError(t, err)
	assert.Equal(t, 2, b.N)
	_, err = Resolve[*TMultiB](p)
	require.ErrorIs(t, err, ErrServiceNotFound, "no output is registered without the name")

	assert.Equal(t, 1, calls, "singleton multi-return constructor must run exactly once")

	t.Run("replace_targets_every_named_output", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(func() (*TMultiA, *TMultiB) { return &TMultiA{N: 1}, &TMultiB{N: 2} }, Name("primary"))
		c.AddModules(ReplaceSingleton(func() (*TMultiA, *TMultiB) { return &TMultiA{N: 3}, &TMultiB{N: 4} }, Name("primary")))
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		b, err := ResolveKeyed[*TMultiB](p, "primary")
		require.NoError(t, err)
		assert.Equal(t, 4, b.N)
	})
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

		consumer, err := Resolve[*TOptionalConsumer](NewTestScope(t, p))
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

		// Build validates every registration's dependencies, so the
		// unconstructible TFailing is reported before any resolution.
		_, err := c.Build()
		require.Error(t, err, "missing transitive dependency of an optional service must propagate")
		assert.ErrorIs(t, err, ErrServiceNotFound)
		assert.Contains(t, err.Error(), "*godi.TFailing requires *godi.TDisposable")
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

		_, err = Resolve[*TOptionalConsumer](NewTestScope(t, p))
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
	c.Remove(reflect.TypeFor[*TMultiA]())
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
		assert.Equal(t, PhaseRegistration, buildErr.Phase)

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
	assert.False(t, c.ContainsKeyed(reflect.TypeFor[*TDependency](), "dup"))
	assert.False(t, c.(*collection).HasGroup(reflect.TypeFor[*TService](), "g"))
}

func TestBuildOptions(t *testing.T) {
	t.Parallel()

	t.Run("no_options_builds", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService)
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })

		svc, err := Resolve[*TService](p)
		require.NoError(t, err)
		assert.NotNil(t, svc)
	})

	t.Run("nil_option_and_nil_context_are_ignored", func(t *testing.T) {
		t.Parallel()
		type CtxHolder struct{ Ctx context.Context }
		c := NewCollection()
		c.AddSingleton(func(ctx context.Context) *CtxHolder { return &CtxHolder{Ctx: ctx} })
		//nolint:staticcheck // a nil context is accepted on purpose
		p, err := c.Build(nil, WithContext(nil))
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		holder := RequireResolve[*CtxHolder](t, p)
		assert.NoError(t, holder.Ctx.Err())
	})

	t.Run("last_context_wins", func(t *testing.T) {
		t.Parallel()
		type marker struct{}
		type CtxHolder struct{ Ctx context.Context }
		first := context.WithValue(context.Background(), marker{}, "first")
		second := context.WithValue(context.Background(), marker{}, "second")
		c := NewCollection()
		c.AddSingleton(func(ctx context.Context) *CtxHolder { return &CtxHolder{Ctx: ctx} })
		p, err := c.Build(WithContext(first), WithContext(second))
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		assert.Equal(t, "second", RequireResolve[*CtxHolder](t, p).Ctx.Value(marker{}))
	})

	t.Run("non_positive_timeout_means_none", func(t *testing.T) {
		t.Parallel()
		for _, d := range []time.Duration{0, -time.Second} {
			c := NewCollection()
			c.AddSingleton(NewTService)
			p, err := c.Build(WithBuildTimeout(d))
			require.NoError(t, err, "timeout %v", d)
			require.NoError(t, p.Close())
		}
	})

	t.Run("generous_timeout_builds", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTService)
		p, err := c.Build(WithBuildTimeout(time.Minute))
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
	})

	t.Run("reports_registration_errors", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(nil)
		_, err := c.Build()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "constructor cannot be nil")
	})
}

func TestLifetimeRules(t *testing.T) {
	t.Parallel()

	type Unit struct{ ID int }
	type Handler struct{ Unit *Unit }
	type Cache struct{ Handler *Handler }

	t.Run("transient_may_depend_on_scoped", func(t *testing.T) {
		t.Parallel()
		var units atomic.Int32
		c := NewCollection()
		c.AddScoped(func() *Unit { return &Unit{ID: int(units.Add(1))} })
		c.AddTransient(func(u *Unit) *Handler { return &Handler{Unit: u} })

		// A fresh-per-use handler sharing the request's unit of work used to
		// be rejected at Build.
		p, err := c.Build()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		scope, err := p.CreateScope(context.Background())
		require.NoError(t, err)
		t.Cleanup(func() { _ = scope.Close() })

		h1, err := Resolve[*Handler](scope)
		require.NoError(t, err)
		h2, err := Resolve[*Handler](scope)
		require.NoError(t, err)
		assert.NotSame(t, h1, h2)
		assert.Same(t, h1.Unit, h2.Unit, "transients in one scope share its scoped services")
	})

	t.Run("singleton_capturing_scoped_through_transient_is_rejected", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddScoped(func() *Unit { return &Unit{} })
		c.AddTransient(func(u *Unit) *Handler { return &Handler{Unit: u} })
		c.AddSingleton(func(h *Handler) *Cache { return &Cache{Handler: h} })

		_, err := c.Build()
		var conflict *LifetimeConflictError
		require.ErrorAs(t, err, &conflict)
		assert.Equal(t, reflect.TypeFor[*Cache](), conflict.ServiceType)
		assert.Equal(t, reflect.TypeFor[*Unit](), conflict.DependencyType)
		assert.Equal(t, []reflect.Type{reflect.TypeFor[*Handler]()}, conflict.Via)
		assert.Contains(t, err.Error(), "*godi.Handler")
	})

	t.Run("singleton_capturing_scoped_through_a_sibling_outputs_decorator_is_rejected", func(t *testing.T) {
		t.Parallel()
		type Pair struct{}
		c := NewCollection()
		c.AddScoped(func() *Unit { return &Unit{} })
		c.AddTransient(func() (*Handler, *Pair) { return &Handler{}, &Pair{} })
		// Every construction of the transient pair runs this decorator of
		// *Pair, which needs the scoped *Unit...
		c.AddModules(Decorate(func(p *Pair, _ *Unit) *Pair { return p }))
		// ...so a singleton of the other output captures one scope's *Unit.
		c.AddSingleton(func(*Handler) *Cache { return &Cache{} })

		var conflict *LifetimeConflictError
		require.ErrorAs(t, Validate(c), &conflict)
		assert.Equal(t, reflect.TypeFor[*Cache](), conflict.ServiceType)
		assert.Equal(t, reflect.TypeFor[*Unit](), conflict.DependencyType)
	})

	t.Run("conflicts_are_reported_together_in_registration_order", func(t *testing.T) {
		t.Parallel()
		type SingletonA struct{}
		type SingletonB struct{}
		build := func() error {
			c := NewCollection()
			c.AddScoped(func() *Unit { return &Unit{} })
			c.AddSingleton(func(*Unit) *SingletonA { return &SingletonA{} })
			c.AddSingleton(func(*Unit) *SingletonB { return &SingletonB{} })
			_, err := c.Build()
			return err
		}

		first := build()
		require.Error(t, first)
		msg := first.Error()
		assert.Less(t, strings.Index(msg, "SingletonA"), strings.Index(msg, "SingletonB"))
		assert.Greater(t, strings.Index(msg, "SingletonB"), -1, "every conflict is reported")
		for range 20 {
			assert.Equal(t, msg, build().Error(), "validation errors must be deterministic")
		}
	})
}

func TestValidate(t *testing.T) {
	t.Parallel()

	// Collection is sealed, so a user type can only implement it by
	// embedding one godi returned, and package functions accept it.
	t.Run("accepts_a_collection_embedded_in_a_user_type", func(t *testing.T) {
		t.Parallel()
		type appCollection struct{ Collection }
		c := appCollection{NewCollection()}
		c.AddSingleton(NewTService)
		c.AddModules(Decorate(func(s *TService) *TService { return s }))
		assert.NoError(t, Validate(c))

		c.AddScoped(NewTServiceWithDeps) // *TDependency is not registered
		assert.ErrorIs(t, Validate(c), ErrServiceNotFound)
	})

	t.Run("checks_wiring_without_constructing", func(t *testing.T) {
		t.Parallel()
		constructed := false
		c := NewCollection()
		c.AddSingleton(func(*TDependency) *TService { constructed = true; return NewTService() })
		c.AddSingleton(NewTDependency)

		// Build constructs every singleton (opening databases, ...); a
		// wiring test should not need production infrastructure.
		require.NoError(t, Validate(c))
		assert.False(t, constructed)
	})

	t.Run("reports_missing_dependencies", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddScoped(func(*TDependency) *TService { return NewTService() })
		require.ErrorIs(t, Validate(c), ErrServiceNotFound)
	})

	t.Run("reports_cycles", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTCircularA)
		c.AddSingleton(NewTCircularB)
		var cycle *CircularDependencyError
		require.ErrorAs(t, Validate(c), &cycle)
	})

	t.Run("reports_lifetime_conflicts", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddScoped(NewTDependency)
		c.AddSingleton(func(*TDependency) *TService { return NewTService() })
		var conflict *LifetimeConflictError
		require.ErrorAs(t, Validate(c), &conflict)
	})

	t.Run("reports_registration_errors", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(nil)
		require.ErrorIs(t, Validate(c), ErrConstructorNil)
	})
}

func TestBuildOrder(t *testing.T) {
	t.Parallel()

	t.Run("independent_singletons_follow_registration_order", func(t *testing.T) {
		t.Parallel()
		type S0 struct{}
		type S1 struct{}
		type S2 struct{}
		type S3 struct{}
		type S4 struct{}
		type S5 struct{}
		for range 20 {
			var order []string
			record := func(name string) { order = append(order, name) }
			c := NewCollection()
			c.AddSingleton(func() *S0 { record("S0"); return &S0{} })
			c.AddSingleton(func() *S1 { record("S1"); return &S1{} })
			c.AddSingleton(func() *S2 { record("S2"); return &S2{} })
			c.AddSingleton(func() *S3 { record("S3"); return &S3{} })
			c.AddSingleton(func() *S4 { record("S4"); return &S4{} })
			c.AddSingleton(func() *S5 { record("S5"); return &S5{} })
			p, err := c.Build()
			require.NoError(t, err)
			require.NoError(t, p.Close())
			// Construction (and so disposal) order used to follow map
			// iteration and change from run to run.
			require.Equal(t, []string{"S0", "S1", "S2", "S3", "S4", "S5"}, order)
		}
	})

	t.Run("dependencies_are_created_first", func(t *testing.T) {
		t.Parallel()
		var order []string
		c := NewCollection()
		c.AddSingleton(func(*TDependency) *TService { order = append(order, "service"); return &TService{} })
		c.AddSingleton(func() *TDependency { order = append(order, "dependency"); return &TDependency{} })
		p, err := c.Build()
		require.NoError(t, err)
		require.NoError(t, p.Close())
		assert.Equal(t, []string{"dependency", "service"}, order)
	})

	t.Run("singleton_resolved_dynamically_during_build", func(t *testing.T) {
		t.Parallel()
		type Early struct{ Late *TService }
		for range 20 {
			var lateCalls atomic.Int32
			c := NewCollection()
			// Early resolves Late at runtime, so the static graph cannot
			// order them; Late is registered (and so ordered) after Early.
			c.AddSingleton(func(p Resolver) (*Early, error) {
				late, err := Resolve[*TService](p)
				return &Early{Late: late}, err
			})
			c.AddSingleton(func() *TService { lateCalls.Add(1); return NewTService() })

			p, err := c.Build()
			require.NoError(t, err, "a singleton needed during Build is created on demand")
			early, err := Resolve[*Early](p)
			require.NoError(t, err)
			late, err := Resolve[*TService](p)
			require.NoError(t, err)
			assert.Same(t, late, early.Late)
			assert.Equal(t, int32(1), lateCalls.Load())
			require.NoError(t, p.Close())
		}
	})
}

func TestToSliceHidesInternalKeys(t *testing.T) {
	t.Parallel()
	c := NewCollection()
	c.AddScoped(func() {})                         // void initializer
	c.AddSingleton(NewTService, Group("services")) // group member
	c.AddSingleton(NewTDependency, Name("dep"))    // real key

	infos := c.ToSlice()
	require.Len(t, infos, 3)
	// Void initializers get a synthetic key and group members a positional
	// one; neither is a key a caller can resolve with.
	assert.Nil(t, infos[0].Key)
	assert.Nil(t, infos[1].Key)
	assert.Equal(t, "services", infos[1].Group)
	assert.Equal(t, "dep", infos[2].Key)
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
		assert.Equal(t, reflect.TypeFor[*TService](), info.ServiceType)
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
		assert.Equal(t, reflect.TypeFor[*TService](), info.ServiceType, "mutation leaked into the collection")
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

		p, err := c.Build(WithContext(ctx))

		// Build waits for the non-cooperative constructor and still fails on
		// the cancellation it cannot deliver mid-flight.
		require.ErrorIs(t, err, context.Canceled)
		assert.Nil(t, p)
		assert.True(t, constructorReturned)
	})

	t.Run("constructor_observes_deadline_cancellation", func(t *testing.T) {
		t.Parallel()
		// The bubble's fake clock fires the deadline as soon as the
		// constructor blocks, without waiting in real time.
		synctest.Test(t, func(t *testing.T) {
			observedCancellation := false
			c := NewCollection()
			c.AddSingleton(func(ctx context.Context) (*TService, error) {
				// Cooperative constructor: block until the build deadline fires.
				<-ctx.Done()
				observedCancellation = true
				return nil, ctx.Err()
			})

			p, err := c.Build(WithBuildTimeout(200 * time.Millisecond))

			require.ErrorIs(t, err, context.DeadlineExceeded)
			assert.Nil(t, p)
			assert.True(t, observedCancellation)
		})
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

		p, err := c.Build(WithContext(ctx))
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

		p, err := c.Build(WithContext(ctx))
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

		p, err := c.Build(WithContext(buildCtx))
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
	})

	t.Run("override_is_race_safe", func(t *testing.T) {
		t.Parallel()
		started := make(chan struct{})
		stop := make(chan struct{})
		var readers sync.WaitGroup

		c := NewCollection()
		c.AddSingleton(func(p Resolver) *TService {
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

		p, err := c.Build()
		close(stop)
		readers.Wait()
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
	})

	t.Run("singleton_context_survives_build_timeout", func(t *testing.T) {
		t.Parallel()
		type CtxHolder struct{ Ctx context.Context }
		c := NewCollection()
		c.AddSingleton(func(ctx context.Context) *CtxHolder { return &CtxHolder{Ctx: ctx} })

		p, err := c.Build(WithBuildTimeout(time.Minute))
		require.NoError(t, err)
		holder, err := Resolve[*CtxHolder](p)
		require.NoError(t, err)

		// The timeout bounds Build only; it used to cancel the context every
		// singleton captured as soon as Build returned.
		require.NoError(t, holder.Ctx.Err(), "a successful Build must not cancel singleton contexts")

		require.NoError(t, p.Close())
		assert.ErrorIs(t, holder.Ctx.Err(), context.Canceled, "provider shutdown cancels the context")
	})

	t.Run("options_accept_a_context_and_a_timeout_together", func(t *testing.T) {
		t.Parallel()
		type contextKey struct{}
		type CtxHolder struct{ Ctx context.Context }
		parent, cancel := context.WithCancel(context.WithValue(context.Background(), contextKey{}, "app"))
		c := NewCollection()
		c.AddSingleton(func(ctx context.Context) *CtxHolder { return &CtxHolder{Ctx: ctx} })

		// A parent context and a build timeout compose: the timeout bounds
		// Build, the parent the provider.
		p, err := c.Build(WithContext(parent), WithBuildTimeout(time.Minute))
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		holder, err := Resolve[*CtxHolder](p)
		require.NoError(t, err)

		assert.Equal(t, "app", holder.Ctx.Value(contextKey{}))
		require.NoError(t, holder.Ctx.Err(), "the build timeout does not outlive Build")
		cancel()
		assert.ErrorIs(t, holder.Ctx.Err(), context.Canceled, "the parent context's cancellation propagates")
	})

	t.Run("singleton_context_deadline_is_stable", func(t *testing.T) {
		t.Parallel()
		type CtxHolder struct {
			Ctx         context.Context
			BuildTime   time.Time
			BuildHasDdl bool
		}
		c := NewCollection()
		c.AddSingleton(func(ctx context.Context) *CtxHolder {
			deadline, ok := ctx.Deadline()
			return &CtxHolder{Ctx: ctx, BuildTime: deadline, BuildHasDdl: ok}
		})

		p, err := c.Build(WithBuildTimeout(time.Minute))
		require.NoError(t, err)
		t.Cleanup(func() { _ = p.Close() })
		holder, err := Resolve[*CtxHolder](p)
		require.NoError(t, err)

		// context.Context requires successive Deadline calls to agree; the
		// build timeout must not appear during Build and vanish afterwards.
		deadline, ok := holder.Ctx.Deadline()
		assert.Equal(t, holder.BuildHasDdl, ok)
		assert.Equal(t, holder.BuildTime, deadline)
	})

	t.Run("root_scope_context_cancelled_on_provider_close", func(t *testing.T) {
		t.Parallel()
		type CtxHolder struct{ Ctx context.Context }
		c := NewCollection()
		c.AddTransient(func(ctx context.Context) *CtxHolder { return &CtxHolder{Ctx: ctx} })

		p, err := c.Build()
		require.NoError(t, err)
		holder, err := Resolve[*CtxHolder](p)
		require.NoError(t, err)
		require.NoError(t, holder.Ctx.Err())

		require.NoError(t, p.Close())
		assert.ErrorIs(t, holder.Ctx.Err(), context.Canceled,
			"services resolved from the root scope must observe provider shutdown")
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

		c.Remove(reflect.TypeFor[*TService]())
		c.Remove(reflect.TypeFor[*TDependency]())
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
				c.Remove(reflect.TypeFor[*TService]())
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

		assert.False(t, c.Contains(reflect.TypeFor[aliasReader]()))
		assert.True(t, c.Contains(reflect.TypeFor[aliasWriter]()))
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
			assert.False(t, c.ContainsKeyed(reflect.TypeFor[*TService](), key))
		})
		require.NotPanics(t, func() {
			c.RemoveKeyed(reflect.TypeFor[*TService](), key)
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
