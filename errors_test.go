package godi

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestErrors(t *testing.T) {
	t.Parallel()

	// Common types for error tests
	svcType := reflect.TypeFor[*TService]()
	depType := reflect.TypeFor[*TDependency]()
	baseCause := errors.New("base error")

	t.Run("LifetimeError", func(t *testing.T) {
		t.Parallel()
		err := &LifetimeError{Value: "invalid"}
		assert.Equal(t, "invalid service lifetime: invalid", err.Error())
	})

	t.Run("LifetimeConflictError", func(t *testing.T) {
		t.Parallel()
		err := &LifetimeConflictError{
			ServiceType:        svcType,
			ServiceLifetime:    Singleton,
			DependencyType:     depType,
			DependencyLifetime: Scoped,
		}
		errStr := err.Error()
		assert.Contains(t, errStr, "lifetime conflict")
		assert.Contains(t, errStr, "Singleton")
		assert.Contains(t, errStr, "Scoped")
		assert.Contains(t, errStr, "cannot depend on")
		assert.Contains(t, Explain(err), "To resolve this")
	})

	t.Run("AlreadyRegisteredError", func(t *testing.T) {
		t.Parallel()
		err := &AlreadyRegisteredError{ServiceType: svcType}
		assert.Contains(t, err.Error(), "already registered")
	})

	t.Run("ResolutionError", func(t *testing.T) {
		t.Parallel()

		t.Run("without_key", func(t *testing.T) {
			t.Parallel()
			err := &ResolutionError{
				ServiceType: svcType,
				ServiceKey:  nil,
				Cause:       ErrServiceNotFound,
			}
			errStr := err.Error()
			assert.Contains(t, errStr, "service not found")
			assert.NotContains(t, errStr, "key:")
			assert.ErrorIs(t, err, ErrServiceNotFound)
		})

		t.Run("with_key", func(t *testing.T) {
			t.Parallel()
			err := &ResolutionError{
				ServiceType: svcType,
				ServiceKey:  "primary",
				Cause:       ErrServiceNotFound,
			}
			errStr := err.Error()
			assert.Contains(t, errStr, "key: primary")
			assert.Contains(t, errStr, "service not found")
			assert.ErrorIs(t, err, ErrServiceNotFound)
		})

		t.Run("other_cause_is_not_reported_as_not_found", func(t *testing.T) {
			t.Parallel()
			err := &ResolutionError{
				ServiceType: svcType,
				ServiceKey:  "primary",
				Cause:       baseCause,
			}
			// A registered service whose construction failed was found.
			errStr := err.Error()
			assert.NotContains(t, errStr, "service not found")
			assert.Contains(t, errStr, "failed to resolve")
			assert.Contains(t, errStr, "key: primary")
			assert.Contains(t, errStr, "base error")
			assert.ErrorIs(t, err, baseCause)
		})

		t.Run("build_failure_is_not_reported_as_not_found", func(t *testing.T) {
			t.Parallel()
			c := NewCollection()
			c.AddSingleton(func() (*TService, error) { return nil, baseCause })

			_, err := c.Build()
			assert.ErrorIs(t, err, baseCause)
			assert.NotContains(t, err.Error(), "service not found")
		})

		t.Run("actionable_message", func(t *testing.T) {
			t.Parallel()
			err := &ResolutionError{
				ServiceType: svcType,
				Cause:       ErrServiceNotFound,
			}
			assert.Contains(t, Explain(err), "Make sure the service is registered")
		})
	})

	t.Run("RegistrationError", func(t *testing.T) {
		t.Parallel()
		err := &RegistrationError{
			ServiceType: svcType,
			Operation:   "provide",
			Cause:       baseCause,
		}
		errStr := err.Error()
		assert.Contains(t, errStr, "failed to provide")
		assert.Contains(t, errStr, "TService")
		assert.ErrorIs(t, err, baseCause)
	})

	t.Run("ValidationError", func(t *testing.T) {
		t.Parallel()

		t.Run("with_type", func(t *testing.T) {
			t.Parallel()
			err := &ValidationError{ServiceType: svcType, Cause: baseCause}
			assert.Contains(t, err.Error(), "TService")
			assert.ErrorIs(t, err, baseCause)
		})

		t.Run("without_type", func(t *testing.T) {
			t.Parallel()
			err := &ValidationError{ServiceType: nil, Cause: baseCause}
			assert.Equal(t, baseCause.Error(), err.Error())
			assert.ErrorIs(t, err, baseCause)
		})
	})

	t.Run("ModuleError", func(t *testing.T) {
		t.Parallel()
		err := &ModuleError{Module: "TestModule", Cause: baseCause}
		assert.Contains(t, err.Error(), `module "TestModule"`)
		assert.ErrorIs(t, err, baseCause)
	})

	t.Run("TypeMismatchError", func(t *testing.T) {
		t.Parallel()
		err := &TypeMismatchError{
			Expected: svcType,
			Actual:   reflect.TypeFor[string](),
			Context:  "type assertion",
		}
		errStr := err.Error()
		assert.Contains(t, errStr, "type assertion")
		assert.Contains(t, errStr, "expected")
		assert.Contains(t, errStr, "got")
		assert.Contains(t, errStr, "string")
	})

	t.Run("reflectionAnalysisError", func(t *testing.T) {
		t.Parallel()
		err := &reflectionAnalysisError{
			Constructor: func() *TService { return nil },
			Operation:   "analyze",
			Cause:       baseCause,
		}
		errStr := err.Error()
		assert.Contains(t, errStr, "reflection analyze failed")
		assert.Contains(t, errStr, "constructor")
		assert.ErrorIs(t, err, baseCause)
	})

	t.Run("ConstructorInvocationError", func(t *testing.T) {
		t.Parallel()
		err := &ConstructorInvocationError{
			Constructor: reflect.TypeFor[func(*TService) *TDependency](),
			Parameters:  []reflect.Type{svcType},
			Cause:       baseCause,
		}
		errStr := err.Error()
		assert.Contains(t, errStr, "failed to invoke")
		assert.Contains(t, errStr, "with parameters")
		assert.ErrorIs(t, err, baseCause)
	})

	t.Run("BuildError", func(t *testing.T) {
		t.Parallel()
		err := &BuildError{
			Phase:   "validation",
			Details: "circular dependency detected",
			Cause:   baseCause,
		}
		errStr := err.Error()
		assert.Contains(t, errStr, "build failed during validation phase")
		assert.Contains(t, errStr, "circular dependency detected")
		assert.ErrorIs(t, err, baseCause)
	})

	t.Run("DisposalError", func(t *testing.T) {
		t.Parallel()

		t.Run("single", func(t *testing.T) {
			t.Parallel()
			err := &DisposalError{
				Context: "provider",
				Errors:  []error{errors.New("close failed")},
			}
			errStr := err.Error()
			assert.Contains(t, errStr, "provider disposal failed")
			assert.NotContains(t, errStr, "errors:")
		})

		t.Run("multiple", func(t *testing.T) {
			t.Parallel()
			err := &DisposalError{
				Context: "scope",
				Errors: []error{
					errors.New("service1 close failed"),
					errors.New("service2 close failed"),
				},
			}
			errStr := err.Error()
			assert.Contains(t, errStr, "scope disposal failed with 2 errors:")
			assert.Contains(t, errStr, "1. service1 close failed")
			assert.Contains(t, errStr, "2. service2 close failed")
		})
	})

	t.Run("ConstructorPanicError", func(t *testing.T) {
		t.Parallel()
		err := &ConstructorPanicError{
			Constructor: reflect.TypeFor[func() *TService](),
			Panic:       "nil pointer",
			Stack:       []byte("goroutine 1 [running]:"),
		}
		errMsg := err.Error()
		assert.Contains(t, errMsg, "panicked")
		assert.Contains(t, errMsg, "nil pointer")
		// The stack and guidance are detail, not message: Error() strings
		// end up in logs and (through framework error handlers) responses.
		assert.NotContains(t, errMsg, "goroutine 1")
		assert.NotContains(t, errMsg, "\n")

		detail := Explain(err)
		assert.Contains(t, detail, "To resolve this")
		assert.Contains(t, detail, "Stack trace")
		assert.Contains(t, detail, "goroutine 1")
		assert.Equal(t, detail, fmt.Sprintf("%+v", err))
	})

	t.Run("messages_are_one_line_with_detail_on_request", func(t *testing.T) {
		t.Parallel()
		conflict := &LifetimeConflictError{
			ServiceType: svcType, ServiceLifetime: Singleton,
			DependencyType: depType, DependencyLifetime: Scoped,
		}
		notFound := &ResolutionError{ServiceType: svcType, Cause: ErrServiceNotFound}
		for _, err := range []error{conflict, notFound} {
			assert.NotContains(t, err.Error(), "\n", "%T", err)
		}
		assert.Contains(t, Explain(conflict), "To resolve this")
		assert.Contains(t, Explain(notFound), "Make sure the service is registered")

		// Detail survives wrapping, including errors.Join.
		wrapped := &BuildError{Phase: PhaseValidation, Cause: errors.Join(conflict, notFound)}
		detail := Explain(wrapped)
		assert.Contains(t, detail, "To resolve this")
		assert.Contains(t, detail, "Make sure the service is registered")
	})

	t.Run("constructor_failures_name_the_function_and_location", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTServiceError)
		_, err := c.Build()
		require.Error(t, err)
		// Two constructors with the same signature used to be
		// indistinguishable: only func() (*TService, error) was printed.
		assert.Contains(t, err.Error(), "NewTServiceError")
		assert.Contains(t, err.Error(), "collection_test.go:")
	})

	t.Run("a_constructors_own_error_follows_its_name", func(t *testing.T) {
		t.Parallel()
		refused := errors.New("connection refused")
		c := NewCollection()
		c.AddSingleton(func() (*TService, error) { return nil, refused })
		_, err := c.Build()
		require.ErrorIs(t, err, refused)
		// Not "... failed: constructor error: connection refused".
		assert.Contains(t, err.Error(), ") failed: connection refused")
	})

	t.Run("a_reflect_made_constructor_is_named_by_its_type", func(t *testing.T) {
		t.Parallel()
		fnType := reflect.TypeFor[func() (*TService, error)]()
		ctor := reflect.MakeFunc(fnType, func([]reflect.Value) []reflect.Value {
			return []reflect.Value{reflect.Zero(reflect.TypeFor[*TService]()), reflect.ValueOf(errors.New("boom"))}
		}).Interface()
		c := NewCollection()
		c.AddSingleton(ctor)
		_, err := c.Build()
		require.Error(t, err)
		// Every reflect.MakeFunc function shares one runtime stub, whose
		// name and assembly location say nothing about the constructor.
		assert.NotContains(t, err.Error(), "makeFuncStub")
		assert.Contains(t, err.Error(), "func() (*godi.TService, error)")
	})

	t.Run("missing_dependency_names_the_constructor", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddScoped(NewTServiceWithDeps)
		_, err := c.Build()
		var missing *MissingDependencyError
		require.ErrorAs(t, err, &missing)
		assert.Contains(t, missing.Constructor, "NewTServiceWithDeps")
	})

	t.Run("not_found_suggests_similar_registrations", func(t *testing.T) {
		t.Parallel()
		p := BuildProvider(t, AddSingleton(NewTService))
		// A common slip: resolving the value type of a pointer registration.
		_, err := Resolve[TService](p)
		require.ErrorIs(t, err, ErrServiceNotFound)
		detail := Explain(err)
		assert.Contains(t, detail, "Did you mean")
		assert.Contains(t, detail, "*godi.TService")
	})

	t.Run("cycles_list_each_service_once", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTCircularA)
		c.AddSingleton(NewTCircularB)
		_, err := c.Build()
		var cycle *CircularDependencyError
		require.ErrorAs(t, err, &cycle)
		// The start node used to be reported twice before "(cycle)".
		assert.Equal(t, 1, strings.Count(strings.Join(cycle.Path, " "), "TCircularA"), cycle.Path)
		assert.NotContains(t, cycle.Error(), "\n")
	})

	t.Run("build_phases_are_constants", func(t *testing.T) {
		t.Parallel()
		c := NewCollection()
		c.AddSingleton(NewTCircularA)
		c.AddSingleton(NewTCircularB)
		_, err := c.Build()
		var buildErr *BuildError
		require.ErrorAs(t, err, &buildErr)
		assert.Equal(t, PhaseValidation, buildErr.Phase)
	})

	t.Run("ErrorWrapping", func(t *testing.T) {
		t.Parallel()
		wrappers := []error{
			&ResolutionError{Cause: baseCause},
			&RegistrationError{Cause: baseCause},
			&ValidationError{Cause: baseCause},
			&ModuleError{Cause: baseCause},
			&reflectionAnalysisError{Cause: baseCause},
			&ConstructorInvocationError{Cause: baseCause},
			&BuildError{Cause: baseCause},
		}
		for _, wrapper := range wrappers {
			assert.ErrorIs(t, wrapper, baseCause, "%T should wrap base error", wrapper)
		}
	})
}

// godi returns its errors as pointers, so only the pointer types implement
// error: errors.As with a value-typed target (&godi.ResolutionError{}) would
// otherwise compile and silently never match.
func TestErrorTypesUsePointerReceivers(t *testing.T) {
	t.Parallel()
	errorType := reflect.TypeFor[error]()
	for _, value := range []any{
		LifetimeError{},
		LifetimeConflictError{},
		AlreadyRegisteredError{},
		CircularDependencyError{},
		ResolutionError{},
		MissingDependencyError{},
		RegistrationError{},
		ValidationError{},
		ModuleError{},
		TypeMismatchError{},
		ConstructorInvocationError{},
		ConstructorPanicError{},
		BuildError{},
		DisposalError{},
	} {
		typ := reflect.TypeOf(value)
		assert.False(t, typ.Implements(errorType), "%s must not implement error", typ)
		assert.True(t, reflect.PointerTo(typ).Implements(errorType), "*%s must implement error", typ)
	}

	wrapped := fmt.Errorf("outer: %w", &ResolutionError{ServiceType: reflect.TypeFor[*TService](), Cause: ErrServiceNotFound})
	resolutionErr, ok := errors.AsType[*ResolutionError](wrapped)
	require.True(t, ok)
	assert.Equal(t, reflect.TypeFor[*TService](), resolutionErr.ServiceType)
}

func TestFormatType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		typ      reflect.Type
		contains string
	}{
		// Types are package-qualified: a bare *Config is ambiguous across
		// packages.
		{"nil", nil, "<nil>"},
		{"pointer", reflect.TypeFor[*TService](), "*godi.TService"},
		{"slice", reflect.TypeFor[[]TService](), "[]godi.TService"},
		{"map", reflect.TypeFor[map[string]int](), "map[string]int"},
		{"interface", reflect.TypeFor[fmt.Stringer](), "fmt.Stringer"},
		{"struct", reflect.TypeFor[TService](), "godi.TService"},
		{"func", reflect.TypeFor[func()](), "func()"},
		{"basic", reflect.TypeFor[int](), "int"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.contains, formatType(tc.typ))
		})
	}
}
