package godi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLifetimeError(t *testing.T) {
	err := LifetimeError{Value: "invalid"}
	expected := "invalid service lifetime: invalid"
	if err.Error() != expected {
		t.Errorf("Expected %q, got %q", expected, err.Error())
	}
}

func TestLifetimeConflictError(t *testing.T) {
	serviceType := reflect.TypeOf((*testService)(nil))
	err := LifetimeConflictError{
		ServiceType: serviceType,
		Current:     Singleton,
		Requested:   Scoped,
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "already registered as Singleton") {
		t.Errorf("Error should mention current lifetime: %s", errStr)
	}
	if !strings.Contains(errStr, "cannot register as Scoped") {
		t.Errorf("Error should mention requested lifetime: %s", errStr)
	}
}

func TestAlreadyRegisteredError(t *testing.T) {
	serviceType := reflect.TypeOf((*testService)(nil))
	err := AlreadyRegisteredError{ServiceType: serviceType}

	errStr := err.Error()
	if !strings.Contains(errStr, "already registered") {
		t.Errorf("Error should mention already registered: %s", errStr)
	}
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
		if !strings.Contains(errStr, "unable to resolve") {
			t.Errorf("Error should mention resolution failure: %s", errStr)
		}
		if strings.Contains(errStr, "key=") {
			t.Errorf("Error should not mention key when nil: %s", errStr)
		}

		if err.Unwrap() != cause {
			t.Error("Unwrap should return the cause")
		}
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
		if !strings.Contains(errStr, "key=primary") {
			t.Errorf("Error should mention key: %s", errStr)
		}
	})
}

func TestTimeoutError(t *testing.T) {
	serviceType := reflect.TypeOf((*testService)(nil))
	err := TimeoutError{
		ServiceType: serviceType,
		Timeout:     5 * time.Second,
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "timed out") {
		t.Errorf("Error should mention timeout: %s", errStr)
	}
	if !strings.Contains(errStr, "5s") {
		t.Errorf("Error should include timeout duration: %s", errStr)
	}

	// Test Is method
	if !err.Is(context.DeadlineExceeded) {
		t.Error("TimeoutError should match context.DeadlineExceeded")
	}
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
	if !strings.Contains(errStr, "failed to provide") {
		t.Errorf("Error should mention operation: %s", errStr)
	}

	if err.Unwrap() != cause {
		t.Error("Unwrap should return the cause")
	}
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
		if !strings.Contains(errStr, "testService") {
			t.Errorf("Error should mention service type: %s", errStr)
		}

		if err.Unwrap() != cause {
			t.Error("Unwrap should return the cause")
		}
	})

	t.Run("without service type", func(t *testing.T) {
		cause := errors.New("validation failed")
		err := ValidationError{
			ServiceType: nil,
			Cause:       cause,
		}

		if err.Error() != cause.Error() {
			t.Errorf("Error should use cause directly when no service type: %s", err.Error())
		}
	})
}

func TestModuleError(t *testing.T) {
	cause := errors.New("registration failed")
	err := ModuleError{
		Module: "TestModule",
		Cause:  cause,
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "module \"TestModule\"") {
		t.Errorf("Error should mention module name: %s", errStr)
	}

	if err.Unwrap() != cause {
		t.Error("Unwrap should return the cause")
	}
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
	if !strings.Contains(errStr, "type assertion") {
		t.Errorf("Error should mention context: %s", errStr)
	}
	if !strings.Contains(errStr, "expected") {
		t.Errorf("Error should mention expected type: %s", errStr)
	}
	if !strings.Contains(errStr, "got") {
		t.Errorf("Error should mention actual type: %s", errStr)
	}
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
	if !strings.Contains(errStr, "reflection analyze failed") {
		t.Errorf("Error should mention reflection operation: %s", errStr)
	}

	if err.Unwrap() != cause {
		t.Error("Unwrap should return the cause")
	}
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
		if !strings.Contains(errStr, "graph add failed") {
			t.Errorf("Error should mention graph operation: %s", errStr)
		}
		if strings.Contains(errStr, "[") && strings.Contains(errStr, "]") {
			t.Errorf("Error should not include brackets when no key: %s", errStr)
		}

		if err.Unwrap() != cause {
			t.Error("Unwrap should return the cause")
		}
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
		if !strings.Contains(errStr, "[primary]") {
			t.Errorf("Error should include key in brackets: %s", errStr)
		}
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
	if !strings.Contains(errStr, "failed to invoke") {
		t.Errorf("Error should mention invocation failure: %s", errStr)
	}
	if !strings.Contains(errStr, "with parameters") {
		t.Errorf("Error should mention parameters: %s", errStr)
	}

	if err.Unwrap() != cause {
		t.Error("Unwrap should return the cause")
	}
}

func TestBuildError(t *testing.T) {
	cause := errors.New("validation failed")
	err := BuildError{
		Phase:   "validation",
		Details: "circular dependency detected",
		Cause:   cause,
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "build failed during validation phase") {
		t.Errorf("Error should mention build phase: %s", errStr)
	}
	if !strings.Contains(errStr, "circular dependency detected") {
		t.Errorf("Error should include details: %s", errStr)
	}

	if err.Unwrap() != cause {
		t.Error("Unwrap should return the cause")
	}
}

func TestDisposalError(t *testing.T) {
	t.Run("single error", func(t *testing.T) {
		err := DisposalError{
			Context: "provider",
			Errors:  []error{errors.New("close failed")},
		}

		errStr := err.Error()
		if !strings.Contains(errStr, "provider disposal failed") {
			t.Errorf("Error should mention disposal context: %s", errStr)
		}
		if strings.Contains(errStr, "errors:") {
			t.Errorf("Single error should not use plural format: %s", errStr)
		}
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
		if !strings.Contains(errStr, "scope disposal failed with 2 errors:") {
			t.Errorf("Error should mention error count: %s", errStr)
		}
		if !strings.Contains(errStr, "1.") || !strings.Contains(errStr, "2.") {
			t.Errorf("Error should enumerate errors: %s", errStr)
		}
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
			if !strings.Contains(result, tt.contains) {
				t.Errorf("formatType(%v) = %q, want to contain %q", tt.typ, result, tt.contains)
			}
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
		if !errors.Is(wrapper, baseErr) {
			t.Errorf("%T should wrap base error", wrapper)
		}
	}
}
