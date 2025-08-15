package godi

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestLifetimeError tests the LifetimeError type
func TestLifetimeError(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{
			name:     "integer value",
			value:    99,
			expected: "invalid service lifetime: 99",
		},
		{
			name:     "string value",
			value:    "invalid",
			expected: "invalid service lifetime: invalid",
		},
		{
			name:     "nil value",
			value:    nil,
			expected: "invalid service lifetime: <nil>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := LifetimeError{Value: tt.value}
			if err.Error() != tt.expected {
				t.Errorf("Error() = %q, want %q", err.Error(), tt.expected)
			}
		})
	}
}

// TestLifetimeConflictError tests the LifetimeConflictError type
func TestLifetimeConflictError(t *testing.T) {
	serviceType := reflect.TypeOf(&TestService{})
	
	err := LifetimeConflictError{
		ServiceType: serviceType,
		Current:     Singleton,
		Requested:   Scoped,
	}
	
	errMsg := err.Error()
	if !strings.Contains(errMsg, "TestService") {
		t.Errorf("Error message should contain service type name, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "Singleton") {
		t.Errorf("Error message should contain current lifetime, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "Scoped") {
		t.Errorf("Error message should contain requested lifetime, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "use Replace") {
		t.Errorf("Error message should mention Replace option, got: %s", errMsg)
	}
}

// TestAlreadyRegisteredError tests the AlreadyRegisteredError type
func TestAlreadyRegisteredError(t *testing.T) {
	serviceType := reflect.TypeOf(&TestService{})
	
	err := AlreadyRegisteredError{
		ServiceType: serviceType,
	}
	
	errMsg := err.Error()
	if !strings.Contains(errMsg, "TestService") {
		t.Errorf("Error message should contain service type name, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "already registered") {
		t.Errorf("Error message should mention 'already registered', got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "keyed services") || !strings.Contains(errMsg, "groups") {
		t.Errorf("Error message should suggest alternatives, got: %s", errMsg)
	}
}

// TestResolutionError tests the ResolutionError type
func TestResolutionError(t *testing.T) {
	serviceType := reflect.TypeOf(&TestService{})
	cause := errors.New("dependency not found")
	
	tests := []struct {
		name        string
		serviceKey  any
		containsKey bool
	}{
		{
			name:        "non-keyed service",
			serviceKey:  nil,
			containsKey: false,
		},
		{
			name:        "keyed service with string key",
			serviceKey:  "test-key",
			containsKey: true,
		},
		{
			name:        "keyed service with int key",
			serviceKey:  42,
			containsKey: true,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ResolutionError{
				ServiceType: serviceType,
				ServiceKey:  tt.serviceKey,
				Cause:       cause,
			}
			
			errMsg := err.Error()
			if !strings.Contains(errMsg, "TestService") {
				t.Errorf("Error message should contain service type, got: %s", errMsg)
			}
			if !strings.Contains(errMsg, "dependency not found") {
				t.Errorf("Error message should contain cause, got: %s", errMsg)
			}
			if tt.containsKey && !strings.Contains(errMsg, "key=") {
				t.Errorf("Error message should contain key for keyed service, got: %s", errMsg)
			}
			
			// Test Unwrap
			if !errors.Is(err, cause) {
				t.Error("Unwrap() should return the cause error")
			}
		})
	}
}

// TestTimeoutError tests the TimeoutError type
func TestTimeoutError(t *testing.T) {
	serviceType := reflect.TypeOf(&TestService{})
	timeout := 5 * time.Second
	
	err := TimeoutError{
		ServiceType: serviceType,
		Timeout:     timeout,
	}
	
	errMsg := err.Error()
	if !strings.Contains(errMsg, "TestService") {
		t.Errorf("Error message should contain service type, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "5s") {
		t.Errorf("Error message should contain timeout duration, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "timed out") {
		t.Errorf("Error message should mention timeout, got: %s", errMsg)
	}
	
	// Test Is method
	if !err.Is(context.DeadlineExceeded) {
		t.Error("TimeoutError should match context.DeadlineExceeded")
	}
	if err.Is(errors.New("other error")) {
		t.Error("TimeoutError should not match other errors")
	}
}

// TestRegistrationError tests the RegistrationError type
func TestRegistrationError(t *testing.T) {
	serviceType := reflect.TypeOf(&TestService{})
	cause := errors.New("invalid constructor")
	
	tests := []struct {
		name      string
		operation string
	}{
		{
			name:      "provide operation",
			operation: "provide",
		},
		{
			name:      "decorate operation",
			operation: "decorate",
		},
		{
			name:      "register operation",
			operation: "register",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RegistrationError{
				ServiceType: serviceType,
				Operation:   tt.operation,
				Cause:       cause,
			}
			
			errMsg := err.Error()
			if !strings.Contains(errMsg, "TestService") {
				t.Errorf("Error message should contain service type, got: %s", errMsg)
			}
			if !strings.Contains(errMsg, tt.operation) {
				t.Errorf("Error message should contain operation '%s', got: %s", tt.operation, errMsg)
			}
			if !strings.Contains(errMsg, "invalid constructor") {
				t.Errorf("Error message should contain cause, got: %s", errMsg)
			}
			
			// Test Unwrap
			if !errors.Is(err, cause) {
				t.Error("Unwrap() should return the cause error")
			}
		})
	}
}

// TestMissingConstructorError tests the MissingConstructorError type
func TestMissingConstructorError(t *testing.T) {
	serviceType := reflect.TypeOf(&TestService{})
	
	tests := []struct {
		name    string
		context string
		expect  string
	}{
		{
			name:    "without context",
			context: "",
			expect:  "TestService has no constructor",
		},
		{
			name:    "with service context",
			context: "service",
			expect:  "service",
		},
		{
			name:    "with descriptor context",
			context: "descriptor",
			expect:  "descriptor",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := MissingConstructorError{
				ServiceType: serviceType,
				Context:     tt.context,
			}
			
			errMsg := err.Error()
			if !strings.Contains(errMsg, "TestService") {
				t.Errorf("Error message should contain service type, got: %s", errMsg)
			}
			if !strings.Contains(errMsg, "has no constructor") {
				t.Errorf("Error message should mention missing constructor, got: %s", errMsg)
			}
			if tt.context != "" && !strings.Contains(errMsg, tt.context) {
				t.Errorf("Error message should contain context '%s', got: %s", tt.context, errMsg)
			}
		})
	}
}

// TestValidationError tests the ValidationError type
func TestValidationError(t *testing.T) {
	serviceType := reflect.TypeOf(&TestService{})
	
	tests := []struct {
		name        string
		serviceType reflect.Type
		message     string
	}{
		{
			name:        "with service type",
			serviceType: serviceType,
			message:     "invalid configuration",
		},
		{
			name:        "without service type",
			serviceType: nil,
			message:     "validation failed",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidationError{
				ServiceType: tt.serviceType,
				Message:     tt.message,
			}
			
			errMsg := err.Error()
			if !strings.Contains(errMsg, tt.message) {
				t.Errorf("Error message should contain message '%s', got: %s", tt.message, errMsg)
			}
			if tt.serviceType != nil && !strings.Contains(errMsg, "TestService") {
				t.Errorf("Error message should contain service type when provided, got: %s", errMsg)
			}
		})
	}
}

// TestModuleError tests the ModuleError type
func TestModuleError(t *testing.T) {
	cause := errors.New("registration failed")
	
	err := ModuleError{
		Module: "TestModule",
		Cause:  cause,
	}
	
	errMsg := err.Error()
	if !strings.Contains(errMsg, "TestModule") {
		t.Errorf("Error message should contain module name, got: %s", errMsg)
	}
	if !strings.Contains(errMsg, "registration failed") {
		t.Errorf("Error message should contain cause, got: %s", errMsg)
	}
	
	// Test Unwrap
	if !errors.Is(err, cause) {
		t.Error("Unwrap() should return the cause error")
	}
}

// TestTypeMismatchError tests the TypeMismatchError type
func TestTypeMismatchError(t *testing.T) {
	expectedType := reflect.TypeOf((*Reader)(nil)).Elem()
	actualType := reflect.TypeOf(&TestService{})
	
	tests := []struct {
		name    string
		context string
	}{
		{
			name:    "interface implementation",
			context: "interface implementation",
		},
		{
			name:    "type assertion",
			context: "type assertion",
		},
		{
			name:    "type conversion",
			context: "type conversion",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := TypeMismatchError{
				Expected: expectedType,
				Actual:   actualType,
				Context:  tt.context,
			}
			
			errMsg := err.Error()
			if !strings.Contains(errMsg, tt.context) {
				t.Errorf("Error message should contain context '%s', got: %s", tt.context, errMsg)
			}
			if !strings.Contains(errMsg, "Reader") {
				t.Errorf("Error message should contain expected type, got: %s", errMsg)
			}
			if !strings.Contains(errMsg, "TestService") {
				t.Errorf("Error message should contain actual type, got: %s", errMsg)
			}
		})
	}
}

// TestFormatType tests the formatType function
func TestFormatType(t *testing.T) {
	tests := []struct {
		name     string
		typ      reflect.Type
		expected string
	}{
		{
			name:     "nil type",
			typ:      nil,
			expected: "<nil>",
		},
		{
			name:     "struct type",
			typ:      reflect.TypeOf(TestService{}),
			expected: "v3.TestService",
		},
		{
			name:     "pointer type",
			typ:      reflect.TypeOf(&TestService{}),
			expected: "*v3.TestService",
		},
		{
			name:     "interface type",
			typ:      reflect.TypeOf((*Reader)(nil)).Elem(),
			expected: "v3.Reader",
		},
		{
			name:     "slice type",
			typ:      reflect.TypeOf([]TestService{}),
			expected: "[]v3.TestService",
		},
		{
			name:     "map type",
			typ:      reflect.TypeOf(map[string]TestService{}),
			expected: "map[string]v3.TestService",
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatType(tt.typ)
			if result != tt.expected {
				t.Errorf("formatType() = %q, want %q", result, tt.expected)
			}
		})
	}
}

// TestSentinelErrors tests that sentinel errors have expected messages
func TestSentinelErrors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		contains string
	}{
		{"ErrServiceNotFound", ErrServiceNotFound, "service not found"},
		{"ErrServiceKeyNil", ErrServiceKeyNil, "service key cannot be nil"},
		{"ErrInvalidServiceType", ErrInvalidServiceType, "invalid service type"},
		{"ErrDisposed", ErrDisposed, "disposed"},
		{"ErrNilScope", ErrNilScope, "scope cannot be nil"},
		{"ErrScopeDisposed", ErrScopeDisposed, "scope has been disposed"},
		{"ErrProviderDisposed", ErrProviderDisposed, "service provider has been disposed"},
		{"ErrNilConstructor", ErrNilConstructor, "constructor cannot be nil"},
		{"ErrNilServiceProvider", ErrNilServiceProvider, "service provider cannot be nil"},
		{"ErrConstructorNotFunction", ErrConstructorNotFunction, "constructor must be a function"},
		{"ErrConstructorNoReturn", ErrConstructorNoReturn, "constructor must return at least one value"},
		{"ErrConstructorTooManyReturns", ErrConstructorTooManyReturns, "constructor must return at most 2 values"},
		{"ErrConstructorInvalidSecondReturn", ErrConstructorInvalidSecondReturn, "second return value must be error"},
		{"ErrDecoratorNil", ErrDecoratorNil, "decorator cannot be nil"},
		{"ErrDecoratorNotFunction", ErrDecoratorNotFunction, "decorator must be a function"},
		{"ErrDecoratorNoParams", ErrDecoratorNoParams, "decorator must have at least one parameter"},
		{"ErrDecoratorNoReturn", ErrDecoratorNoReturn, "decorator must return at least one value"},
		{"ErrCollectionBuilt", ErrCollectionBuilt, "service collection has already been built"},
		{"ErrCollectionModifyAfterBuild", ErrCollectionModifyAfterBuild, "cannot modify service collection after build"},
		{"ErrDescriptorNil", ErrDescriptorNil, "descriptor cannot be nil"},
		{"ErrServicesNil", ErrServicesNil, "services cannot be nil"},
		{"ErrScopeNotInContext", ErrScopeNotInContext, "no scope found in context"},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !strings.Contains(tt.err.Error(), tt.contains) {
				t.Errorf("%s error message should contain '%s', got: %s", tt.name, tt.contains, tt.err.Error())
			}
		})
	}
}