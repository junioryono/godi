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

func TestLifetimeError(t *testing.T) {
	err := LifetimeError{Value: "invalid"}
	expected := "invalid service lifetime: invalid"
	assert.Equal(t, expected, err.Error(), "LifetimeError should return correct error message")
}

func TestLifetimeConflictError(t *testing.T) {
	serviceType := reflect.TypeOf((*testService)(nil))
	dependencyType := reflect.TypeOf((*testDependency)(nil))
	err := LifetimeConflictError{
		ServiceType:        serviceType,
		ServiceLifetime:    Singleton,
		DependencyType:     dependencyType,
		DependencyLifetime: Scoped,
	}

	errStr := err.Error()
	assert.Contains(t, errStr, "Singleton service", "Error should mention service lifetime")
	assert.Contains(t, errStr, "Scoped service", "Error should mention dependency lifetime")
	assert.Contains(t, errStr, "cannot depend on", "Error should explain the relationship")
}

func TestAlreadyRegisteredError(t *testing.T) {
	serviceType := reflect.TypeOf((*testService)(nil))
	err := AlreadyRegisteredError{ServiceType: serviceType}

	errStr := err.Error()
	assert.Contains(t, errStr, "already registered", "Error should mention already registered")
}

func TestResolutionError(t *testing.T) {
	t.Run("without key", func(t *testing.T) {
		serviceType := reflect.TypeOf((*testService)(nil))
		cause := errors.New("dependency not found")
		err := ResolutionError{
			ServiceType: serviceType,
			ServiceKey:  nil,
			Cause:       cause,
		}

		errStr := err.Error()
		assert.Contains(t, errStr, "unable to resolve", "Error should mention resolution failure")
		assert.NotContains(t, errStr, "key=", "Error should not mention key when nil")
		assert.ErrorIs(t, err, cause, "Unwrap should return the cause")
	})

	t.Run("with key", func(t *testing.T) {
		serviceType := reflect.TypeOf((*testService)(nil))
		cause := errors.New("keyed service not found")
		err := ResolutionError{
			ServiceType: serviceType,
			ServiceKey:  "primary",
			Cause:       cause,
		}

		errStr := err.Error()
		assert.Contains(t, errStr, "key=primary", "Error should mention key")
		assert.Contains(t, errStr, "unable to resolve", "Error should mention resolution failure")
		assert.ErrorIs(t, err, cause, "Unwrap should return the cause")
	})
}

func TestTimeoutError(t *testing.T) {
	serviceType := reflect.TypeOf((*testService)(nil))
	err := TimeoutError{
		ServiceType: serviceType,
		Timeout:     5 * time.Second,
	}

	errStr := err.Error()
	assert.Contains(t, errStr, "timed out", "Error should mention timeout")
	assert.Contains(t, errStr, "5s", "Error should include timeout duration")
	assert.ErrorIs(t, err, context.DeadlineExceeded, "TimeoutError should match context.DeadlineExceeded")
}

func TestRegistrationError(t *testing.T) {
	serviceType := reflect.TypeOf((*testService)(nil))
	cause := errors.New("invalid constructor")
	err := RegistrationError{
		ServiceType: serviceType,
		Operation:   "provide",
		Cause:       cause,
	}

	errStr := err.Error()
	assert.Contains(t, errStr, "failed to provide", "Error should mention operation")
	assert.Contains(t, errStr, "testService", "Error should mention service type")
	assert.ErrorIs(t, err, cause, "Unwrap should return the cause")
}

func TestValidationError(t *testing.T) {
	t.Run("with service type", func(t *testing.T) {
		serviceType := reflect.TypeOf((*testService)(nil))
		cause := errors.New("invalid parameters")
		err := ValidationError{
			ServiceType: serviceType,
			Cause:       cause,
		}

		errStr := err.Error()
		assert.Contains(t, errStr, "testService", "Error should mention service type")
		assert.ErrorIs(t, err, cause, "Unwrap should return the cause")
	})

	t.Run("without service type", func(t *testing.T) {
		cause := errors.New("validation failed")
		err := ValidationError{
			ServiceType: nil,
			Cause:       cause,
		}

		assert.Equal(t, cause.Error(), err.Error(), "Error should use cause directly when no service type")
		assert.ErrorIs(t, err, cause, "Unwrap should return the cause")
	})
}

func TestModuleError(t *testing.T) {
	cause := errors.New("registration failed")
	err := ModuleError{
		Module: "TestModule",
		Cause:  cause,
	}

	errStr := err.Error()
	assert.Contains(t, errStr, "module \"TestModule\"", "Error should mention module name")
	assert.ErrorIs(t, err, cause, "Unwrap should return the cause")
}

func TestTypeMismatchError(t *testing.T) {
	expected := reflect.TypeOf((*testService)(nil))
	actual := reflect.TypeOf("")
	err := TypeMismatchError{
		Expected: expected,
		Actual:   actual,
		Context:  "type assertion",
	}

	errStr := err.Error()
	assert.Contains(t, errStr, "type assertion", "Error should mention context")
	assert.Contains(t, errStr, "expected", "Error should mention expected type")
	assert.Contains(t, errStr, "got", "Error should mention actual type")
	assert.Contains(t, errStr, "*testService", "Error should mention expected type *testService")
	assert.Contains(t, errStr, "string", "Error should mention actual type string")
	assert.Equal(t, "type assertion: expected *testService, got string", errStr, "Error should format correctly")
}

func TestReflectionAnalysisError(t *testing.T) {
	constructor := func() *testService { return nil }
	cause := errors.New("invalid function signature")
	err := ReflectionAnalysisError{
		Constructor: constructor,
		Operation:   "analyze",
		Cause:       cause,
	}

	errStr := err.Error()
	assert.Contains(t, errStr, "reflection analyze failed", "Error should mention reflection operation")
	assert.Contains(t, errStr, "constructor", "Error should mention constructor type")
	assert.Contains(t, errStr, "testService", "Error should mention testService constructor")
	assert.ErrorIs(t, err, cause, "Unwrap should return the cause")
}

func TestGraphOperationError(t *testing.T) {
	t.Run("without key", func(t *testing.T) {
		nodeType := reflect.TypeOf((*testService)(nil))
		cause := errors.New("cycle detected")
		err := GraphOperationError{
			Operation: "add",
			NodeType:  nodeType,
			NodeKey:   nil,
			Cause:     cause,
		}

		errStr := err.Error()
		assert.Contains(t, errStr, "graph add failed for *testService: cycle detected", "Error should mention graph operation and cause")
		assert.NotContains(t, errStr, "[", "Error should not include brackets when no key")
		assert.NotContains(t, errStr, "]", "Error should not include brackets when no key")
		assert.ErrorIs(t, err, cause, "Unwrap should return the cause")
	})

	t.Run("with key", func(t *testing.T) {
		nodeType := reflect.TypeOf((*testService)(nil))
		cause := errors.New("node exists")
		err := GraphOperationError{
			Operation: "add",
			NodeType:  nodeType,
			NodeKey:   "primary",
			Cause:     cause,
		}

		errStr := err.Error()
		assert.Contains(t, errStr, "graph add failed for *testService[primary]: node exists", "Error should mention graph operation, key, and cause")
		assert.ErrorIs(t, err, cause, "Unwrap should return the cause")
	})
}

func TestConstructorInvocationError(t *testing.T) {
	constructorType := reflect.TypeOf(func(*testService) *testDependency { return nil })
	paramTypes := []reflect.Type{reflect.TypeOf((*testService)(nil))}
	cause := errors.New("service not found")

	err := ConstructorInvocationError{
		Constructor: constructorType,
		Parameters:  paramTypes,
		Cause:       cause,
	}

	errStr := err.Error()
	assert.Contains(t, errStr, "failed to invoke", "Error should mention invocation failure")
	assert.Contains(t, errStr, "with parameters", "Error should mention parameters")
	assert.Contains(t, errStr, "*testService", "Error should mention parameter type *testService")
	assert.ErrorIs(t, err, cause, "Unwrap should return the cause")
}

func TestBuildError(t *testing.T) {
	cause := errors.New("validation failed")
	err := BuildError{
		Phase:   "validation",
		Details: "circular dependency detected",
		Cause:   cause,
	}

	errStr := err.Error()
	assert.Contains(t, errStr, "build failed during validation phase", "Error should mention build phase")
	assert.Contains(t, errStr, "circular dependency detected", "Error should include details")
	assert.ErrorIs(t, err, cause, "Unwrap should return the cause")
}

func TestDisposalError(t *testing.T) {
	t.Run("single error", func(t *testing.T) {
		err := DisposalError{
			Context: "provider",
			Errors:  []error{errors.New("close failed")},
		}

		errStr := err.Error()
		assert.Contains(t, errStr, "provider disposal failed", "Error should mention disposal context")
		assert.NotContains(t, errStr, "errors:", "Single error should not use plural format")
		assert.Len(t, err.Errors, 1, "DisposalError should contain exactly one error")
		assert.Equal(t, "close failed", err.Errors[0].Error(), "DisposalError should contain the correct error message")
	})

	t.Run("multiple errors", func(t *testing.T) {
		err := DisposalError{
			Context: "scope",
			Errors: []error{
				errors.New("service1 close failed"),
				errors.New("service2 close failed"),
			},
		}

		errStr := err.Error()
		assert.Contains(t, errStr, "scope disposal failed with 2 errors:", "Error should mention error count")
		assert.Contains(t, errStr, "1. service1 close failed", "Error should enumerate first error")
		assert.Contains(t, errStr, "2. service2 close failed", "Error should enumerate second error")
		assert.Len(t, err.Errors, 2, "DisposalError should contain exactly two errors")
		assert.Equal(t, "service1 close failed", err.Errors[0].Error(), "First error should match")
		assert.Equal(t, "service2 close failed", err.Errors[1].Error(), "Second error should match")
	})
}

func TestFormatType(t *testing.T) {
	tests := []struct {
		name     string
		typ      reflect.Type
		contains string
	}{
		{
			name:     "nil type",
			typ:      nil,
			contains: "<nil>",
		},
		{
			name:     "pointer type",
			typ:      reflect.TypeOf((*testService)(nil)),
			contains: "*testService",
		},
		{
			name:     "slice type",
			typ:      reflect.TypeOf([]testService{}),
			contains: "[]testService",
		},
		{
			name:     "map type",
			typ:      reflect.TypeOf(map[string]int{}),
			contains: "map[string]int",
		},
		{
			name:     "interface type",
			typ:      reflect.TypeOf((*fmt.Stringer)(nil)).Elem(),
			contains: "Stringer",
		},
		{
			name:     "struct type",
			typ:      reflect.TypeOf(testService{}),
			contains: "testService",
		},
		{
			name:     "func type",
			typ:      reflect.TypeOf(func() {}),
			contains: "func()",
		},
		{
			name:     "basic type",
			typ:      reflect.TypeOf(42),
			contains: "int",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatType(tt.typ)
			assert.Contains(t, result, tt.contains, "formatType should contain expected substring")
		})
	}
}

func TestErrorWrapping(t *testing.T) {
	// Test that our errors properly implement error wrapping
	baseErr := errors.New("base error")

	wrappers := []error{
		&ResolutionError{Cause: baseErr},
		&RegistrationError{Cause: baseErr},
		&ValidationError{Cause: baseErr},
		&ModuleError{Cause: baseErr},
		&ReflectionAnalysisError{Cause: baseErr},
		&GraphOperationError{Cause: baseErr},
		&ConstructorInvocationError{Cause: baseErr},
		&BuildError{Cause: baseErr},
	}

	for _, wrapper := range wrappers {
		assert.ErrorIs(t, wrapper, baseErr, "%T should wrap base error", wrapper)
	}
}
