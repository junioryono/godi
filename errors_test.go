package godi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestErrors(t *testing.T) {
	t.Parallel()

	// Common types for error tests
	svcType := reflect.TypeOf((*TService)(nil))
	depType := reflect.TypeOf((*TDependency)(nil))
	baseCause := errors.New("base error")

	t.Run("LifetimeError", func(t *testing.T) {
		t.Parallel()
		err := LifetimeError{Value: "invalid"}
		assert.Equal(t, "invalid service lifetime: invalid", err.Error())
	})

	t.Run("LifetimeConflictError", func(t *testing.T) {
		t.Parallel()
		err := LifetimeConflictError{
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
		assert.Contains(t, errStr, "To resolve this")
	})

	t.Run("AlreadyRegisteredError", func(t *testing.T) {
		t.Parallel()
		err := AlreadyRegisteredError{ServiceType: svcType}
		assert.Contains(t, err.Error(), "already registered")
	})

	t.Run("ResolutionError", func(t *testing.T) {
		t.Parallel()

		t.Run("without_key", func(t *testing.T) {
			t.Parallel()
			err := ResolutionError{
				ServiceType: svcType,
				ServiceKey:  nil,
				Cause:       baseCause,
			}
			errStr := err.Error()
			assert.Contains(t, errStr, "service not found")
			assert.NotContains(t, errStr, "key:")
			assert.ErrorIs(t, err, baseCause)
		})

		t.Run("with_key", func(t *testing.T) {
			t.Parallel()
			err := ResolutionError{
				ServiceType: svcType,
				ServiceKey:  "primary",
				Cause:       baseCause,
			}
			errStr := err.Error()
			assert.Contains(t, errStr, "key: primary")
			assert.Contains(t, errStr, "service not found")
			assert.ErrorIs(t, err, baseCause)
		})

		t.Run("actionable_message", func(t *testing.T) {
			t.Parallel()
			err := ResolutionError{
				ServiceType: svcType,
				Cause:       ErrServiceNotFound,
			}
			assert.Contains(t, err.Error(), "Make sure the service is registered")
		})
	})

	t.Run("TimeoutError", func(t *testing.T) {
		t.Parallel()
		err := TimeoutError{
			ServiceType: svcType,
			Timeout:     5 * time.Second,
		}
		errStr := err.Error()
		assert.Contains(t, errStr, "timed out")
		assert.Contains(t, errStr, "5s")
		assert.ErrorIs(t, err, context.DeadlineExceeded)
	})

	t.Run("RegistrationError", func(t *testing.T) {
		t.Parallel()
		err := RegistrationError{
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
			err := ValidationError{ServiceType: svcType, Cause: baseCause}
			assert.Contains(t, err.Error(), "TService")
			assert.ErrorIs(t, err, baseCause)
		})

		t.Run("without_type", func(t *testing.T) {
			t.Parallel()
			err := ValidationError{ServiceType: nil, Cause: baseCause}
			assert.Equal(t, baseCause.Error(), err.Error())
			assert.ErrorIs(t, err, baseCause)
		})
	})

	t.Run("ModuleError", func(t *testing.T) {
		t.Parallel()
		err := ModuleError{Module: "TestModule", Cause: baseCause}
		assert.Contains(t, err.Error(), `module "TestModule"`)
		assert.ErrorIs(t, err, baseCause)
	})

	t.Run("TypeMismatchError", func(t *testing.T) {
		t.Parallel()
		err := TypeMismatchError{
			Expected: svcType,
			Actual:   reflect.TypeOf(""),
			Context:  "type assertion",
		}
		errStr := err.Error()
		assert.Contains(t, errStr, "type assertion")
		assert.Contains(t, errStr, "expected")
		assert.Contains(t, errStr, "got")
		assert.Contains(t, errStr, "string")
	})

	t.Run("ReflectionAnalysisError", func(t *testing.T) {
		t.Parallel()
		err := ReflectionAnalysisError{
			Constructor: func() *TService { return nil },
			Operation:   "analyze",
			Cause:       baseCause,
		}
		errStr := err.Error()
		assert.Contains(t, errStr, "reflection analyze failed")
		assert.Contains(t, errStr, "constructor")
		assert.ErrorIs(t, err, baseCause)
	})

	t.Run("GraphOperationError", func(t *testing.T) {
		t.Parallel()

		t.Run("without_key", func(t *testing.T) {
			t.Parallel()
			err := GraphOperationError{
				Operation: "add",
				NodeType:  svcType,
				NodeKey:   nil,
				Cause:     baseCause,
			}
			errStr := err.Error()
			assert.Contains(t, errStr, "graph add failed")
			assert.NotContains(t, errStr, "[")
			assert.ErrorIs(t, err, baseCause)
		})

		t.Run("with_key", func(t *testing.T) {
			t.Parallel()
			err := GraphOperationError{
				Operation: "add",
				NodeType:  svcType,
				NodeKey:   "primary",
				Cause:     baseCause,
			}
			assert.Contains(t, err.Error(), "[primary]")
			assert.ErrorIs(t, err, baseCause)
		})
	})

	t.Run("ConstructorInvocationError", func(t *testing.T) {
		t.Parallel()
		err := ConstructorInvocationError{
			Constructor: reflect.TypeOf(func(*TService) *TDependency { return nil }),
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
		err := BuildError{
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
			err := DisposalError{
				Context: "provider",
				Errors:  []error{errors.New("close failed")},
			}
			errStr := err.Error()
			assert.Contains(t, errStr, "provider disposal failed")
			assert.NotContains(t, errStr, "errors:")
		})

		t.Run("multiple", func(t *testing.T) {
			t.Parallel()
			err := DisposalError{
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
			Constructor: reflect.TypeOf(func() *TService { return nil }),
			Panic:       "nil pointer",
			Stack:       []byte("goroutine 1 [running]:"),
		}
		errMsg := err.Error()
		assert.Contains(t, errMsg, "panicked")
		assert.Contains(t, errMsg, "nil pointer")
		assert.Contains(t, errMsg, "Constructors should be pure dependency wiring")
		assert.Contains(t, errMsg, "To resolve this")
		assert.Contains(t, errMsg, "Stack trace")
	})

	t.Run("ErrorWrapping", func(t *testing.T) {
		t.Parallel()
		wrappers := []error{
			&ResolutionError{Cause: baseCause},
			&RegistrationError{Cause: baseCause},
			&ValidationError{Cause: baseCause},
			&ModuleError{Cause: baseCause},
			&ReflectionAnalysisError{Cause: baseCause},
			&GraphOperationError{Cause: baseCause},
			&ConstructorInvocationError{Cause: baseCause},
			&BuildError{Cause: baseCause},
		}
		for _, wrapper := range wrappers {
			assert.ErrorIs(t, wrapper, baseCause, "%T should wrap base error", wrapper)
		}
	})
}

func TestFormatType(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		typ      reflect.Type
		contains string
	}{
		{"nil", nil, "<nil>"},
		{"pointer", reflect.TypeOf((*TService)(nil)), "*TService"},
		{"slice", reflect.TypeOf([]TService{}), "[]TService"},
		{"map", reflect.TypeOf(map[string]int{}), "map[string]int"},
		{"interface", reflect.TypeOf((*fmt.Stringer)(nil)).Elem(), "Stringer"},
		{"struct", reflect.TypeOf(TService{}), "TService"},
		{"func", reflect.TypeOf(func() {}), "func()"},
		{"basic", reflect.TypeOf(42), "int"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Contains(t, formatType(tc.typ), tc.contains)
		})
	}
}
