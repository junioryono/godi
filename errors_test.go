package godi

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSentinelErrors(t *testing.T) {
	// Test that all sentinel errors are defined and have appropriate messages
	sentinelErrors := []struct {
		err     error
		message string
	}{
		{ErrServiceNotFound, "service not found"},
		{ErrServiceKeyNil, "service key cannot be nil"},
		{ErrInvalidServiceType, "invalid service type"},
		{ErrDisposed, "disposed"},
		{ErrNilScope, "scope cannot be nil"},
		{ErrScopeDisposed, "scope has been disposed"},
		{ErrProviderDisposed, "service provider has been disposed"},
		{ErrNilConstructor, "constructor cannot be nil"},
		{ErrNilServiceProvider, "service provider cannot be nil"},
		{ErrConstructorNotFunction, "constructor must be a function"},
		{ErrConstructorNoReturn, "constructor must return at least one value"},
		{ErrConstructorTooManyReturns, "constructor must return at most 2 values"},
		{ErrConstructorInvalidSecondReturn, "constructor's second return value must be error"},
		{ErrConstructorMultipleIn, "constructor cannot have multiple In parameters"},
		{ErrConstructorOutMaxReturns, "constructor with Out must return at most 2 values"},
		{ErrDecoratorNil, "decorator cannot be nil"},
		{ErrDecoratorNotFunction, "decorator must be a function"},
		{ErrDecoratorNoParams, "decorator must have at least one parameter"},
		{ErrDecoratorNoReturn, "decorator must return at least one value"},
		{ErrCollectionBuilt, "service collection has already been built"},
		{ErrCollectionModifyAfterBuild, "cannot modify service collection after build"},
		{ErrDescriptorNil, "descriptor cannot be nil"},
		{ErrNoConstructorOrDecorator, "constructor or decorator must be provided"},
		{ErrBothConstructorAndDecorator, "cannot have both constructor and decorator"},
		{ErrServicesNil, "services cannot be nil"},
		{ErrProviderFunctionNotFound, "provider function not found"},
		{ErrKeyedProviderFunctionNotFound, "keyed provider function not found"},
		{ErrConstructorMustReturnValue, "constructor must be a function that returns at least one value"},
		{ErrServiceHasNoConstructor, "service has no constructor"},
		{ErrDescriptorHasNoConstructor, "descriptor has no constructor"},
		{ErrFailedToCreateScope, "failed to create dig scope: dig container is nil"},
		{ErrResultObjectConstructor, "constructor returns Out - use collection.Add* methods"},
		{ErrReplaceResultObject, "replace not supported for result object constructors"},
		{ErrScopeNotInContext, "no scope found in context"},
	}

	for _, tt := range sentinelErrors {
		t.Run(tt.message, func(t *testing.T) {
			assert.NotNil(t, tt.err)
			assert.Equal(t, tt.message, tt.err.Error())
		})
	}
}

func TestLifetimeError(t *testing.T) {
	tests := []struct {
		name     string
		value    any
		expected string
	}{
		{
			name:     "string value",
			value:    "invalid",
			expected: "invalid service lifetime: invalid",
		},
		{
			name:     "int value",
			value:    999,
			expected: "invalid service lifetime: 999",
		},
		{
			name:     "nil value",
			value:    nil,
			expected: "invalid service lifetime: <nil>",
		},
		{
			name:     "struct value",
			value:    struct{ Name string }{Name: "test"},
			expected: "invalid service lifetime: {test}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := LifetimeError{Value: tt.value}
			assert.Equal(t, tt.expected, err.Error())
		})
	}
}

func TestLifetimeConflictError(t *testing.T) {
	serviceType := reflect.TypeOf((*TestService)(nil))

	tests := []struct {
		name      string
		current   Lifetime
		requested Lifetime
	}{
		{
			name:      "singleton to scoped",
			current:   Singleton,
			requested: Scoped,
		},
		{
			name:      "scoped to transient",
			current:   Scoped,
			requested: Transient,
		},
		{
			name:      "transient to singleton",
			current:   Transient,
			requested: Singleton,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := LifetimeConflictError{
				ServiceType: serviceType,
				Current:     tt.current,
				Requested:   tt.requested,
			}

			msg := err.Error()
			assert.Contains(t, msg, formatType(serviceType))
			assert.Contains(t, msg, tt.current.String())
			assert.Contains(t, msg, tt.requested.String())
			assert.Contains(t, msg, "already registered")
			assert.Contains(t, msg, "use Replace")
		})
	}
}

func TestAlreadyRegisteredError(t *testing.T) {
	serviceType := reflect.TypeOf((*TestService)(nil))

	err := AlreadyRegisteredError{ServiceType: serviceType}
	msg := err.Error()

	assert.Contains(t, msg, formatType(serviceType))
	assert.Contains(t, msg, "already registered")
	assert.Contains(t, msg, "use keyed services, groups, or Replace")
}

func TestResolutionError(t *testing.T) {
	serviceType := reflect.TypeOf((*TestService)(nil))
	causeErr := errors.New("underlying cause")

	t.Run("without key", func(t *testing.T) {
		err := ResolutionError{
			ServiceType: serviceType,
			ServiceKey:  nil,
			Cause:       causeErr,
		}

		msg := err.Error()
		assert.Contains(t, msg, "unable to resolve")
		assert.Contains(t, msg, formatType(serviceType))
		assert.Contains(t, msg, causeErr.Error())
		assert.NotContains(t, msg, "key=")

		// Test unwrap
		unwrapped := errors.Unwrap(err)
		assert.Equal(t, causeErr, unwrapped)
	})

	t.Run("with string key", func(t *testing.T) {
		err := ResolutionError{
			ServiceType: serviceType,
			ServiceKey:  "primary",
			Cause:       causeErr,
		}

		msg := err.Error()
		assert.Contains(t, msg, "unable to resolve")
		assert.Contains(t, msg, formatType(serviceType))
		assert.Contains(t, msg, "[key=primary]")
		assert.Contains(t, msg, causeErr.Error())
	})

	t.Run("with int key", func(t *testing.T) {
		err := ResolutionError{
			ServiceType: serviceType,
			ServiceKey:  42,
			Cause:       causeErr,
		}

		msg := err.Error()
		assert.Contains(t, msg, "[key=42]")
	})

	t.Run("error chain", func(t *testing.T) {
		err := ResolutionError{
			ServiceType: serviceType,
			Cause:       ErrServiceNotFound,
		}

		assert.True(t, errors.Is(err, ErrServiceNotFound))
	})
}

func TestTimeoutError(t *testing.T) {
	serviceType := reflect.TypeOf((*TestService)(nil))
	timeout := 5 * time.Second

	err := TimeoutError{
		ServiceType: serviceType,
		Timeout:     timeout,
	}

	t.Run("error message", func(t *testing.T) {
		msg := err.Error()
		assert.Contains(t, msg, "resolution of")
		assert.Contains(t, msg, formatType(serviceType))
		assert.Contains(t, msg, "timed out after")
		assert.Contains(t, msg, "5s")
	})

	t.Run("Is method", func(t *testing.T) {
		assert.True(t, errors.Is(err, context.DeadlineExceeded))
		assert.False(t, errors.Is(err, context.Canceled))
		assert.False(t, errors.Is(err, ErrServiceNotFound))
	})
}

func TestRegistrationError(t *testing.T) {
	serviceType := reflect.TypeOf((*TestService)(nil))
	causeErr := errors.New("registration failed")

	operations := []string{"provide", "decorate", "register", "add"}

	for _, op := range operations {
		t.Run(op, func(t *testing.T) {
			err := RegistrationError{
				ServiceType: serviceType,
				Operation:   op,
				Cause:       causeErr,
			}

			msg := err.Error()
			assert.Contains(t, msg, "failed to")
			assert.Contains(t, msg, op)
			assert.Contains(t, msg, formatType(serviceType))
			assert.Contains(t, msg, causeErr.Error())

			// Test unwrap
			unwrapped := errors.Unwrap(err)
			assert.Equal(t, causeErr, unwrapped)
		})
	}
}

func TestMissingConstructorError(t *testing.T) {
	serviceType := reflect.TypeOf((*TestService)(nil))

	t.Run("with context", func(t *testing.T) {
		contexts := []string{"service", "descriptor", "provider"}

		for _, ctx := range contexts {
			err := MissingConstructorError{
				ServiceType: serviceType,
				Context:     ctx,
			}

			msg := err.Error()
			assert.Contains(t, msg, ctx)
			assert.Contains(t, msg, formatType(serviceType))
			assert.Contains(t, msg, "has no constructor")
		}
	})

	t.Run("without context", func(t *testing.T) {
		err := MissingConstructorError{
			ServiceType: serviceType,
			Context:     "",
		}

		msg := err.Error()
		assert.Contains(t, msg, formatType(serviceType))
		assert.Contains(t, msg, "has no constructor")
		assert.NotContains(t, msg, "service")
		assert.NotContains(t, msg, "descriptor")
	})
}

func TestValidationError(t *testing.T) {
	t.Run("with service type", func(t *testing.T) {
		serviceType := reflect.TypeOf((*TestService)(nil))
		err := ValidationError{
			ServiceType: serviceType,
			Message:     "invalid configuration",
		}

		msg := err.Error()
		assert.Contains(t, msg, formatType(serviceType))
		assert.Contains(t, msg, "invalid configuration")
	})

	t.Run("without service type", func(t *testing.T) {
		err := ValidationError{
			ServiceType: nil,
			Message:     "general validation error",
		}

		msg := err.Error()
		assert.Equal(t, "general validation error", msg)
		assert.NotContains(t, msg, ":")
	})
}

func TestModuleError(t *testing.T) {
	causeErr := errors.New("module initialization failed")

	err := ModuleError{
		Module: "DatabaseModule",
		Cause:  causeErr,
	}

	t.Run("error message", func(t *testing.T) {
		msg := err.Error()
		assert.Contains(t, msg, `module "DatabaseModule"`)
		assert.Contains(t, msg, causeErr.Error())
	})

	t.Run("unwrap", func(t *testing.T) {
		unwrapped := errors.Unwrap(err)
		assert.Equal(t, causeErr, unwrapped)
	})

	t.Run("error chain", func(t *testing.T) {
		err := ModuleError{
			Module: "TestModule",
			Cause:  ErrNilConstructor,
		}

		assert.True(t, errors.Is(err, ErrNilConstructor))
	})
}

func TestTypeMismatchError(t *testing.T) {
	expectedType := reflect.TypeOf((*TestInterface)(nil)).Elem()
	actualType := reflect.TypeOf((*TestService)(nil))

	contexts := []string{
		"interface implementation",
		"type assertion",
		"type conversion",
		"decorator parameter",
	}

	for _, ctx := range contexts {
		t.Run(ctx, func(t *testing.T) {
			err := TypeMismatchError{
				Expected: expectedType,
				Actual:   actualType,
				Context:  ctx,
			}

			msg := err.Error()
			assert.Contains(t, msg, ctx)
			assert.Contains(t, msg, "expected")
			assert.Contains(t, msg, formatType(expectedType))
			assert.Contains(t, msg, "got")
			assert.Contains(t, msg, formatType(actualType))
		})
	}
}

func TestFormatType(t *testing.T) {
	tests := []struct {
		name     string
		typ      reflect.Type
		contains string
	}{
		{
			name:     "pointer type",
			typ:      reflect.TypeOf((*TestService)(nil)),
			contains: "TestService",
		},
		{
			name:     "interface type",
			typ:      reflect.TypeOf((*TestInterface)(nil)).Elem(),
			contains: "TestInterface",
		},
		{
			name:     "slice type",
			typ:      reflect.TypeOf([]TestService{}),
			contains: "TestService",
		},
		{
			name:     "map type",
			typ:      reflect.TypeOf(map[string]TestService{}),
			contains: "TestService",
		},
		{
			name:     "nil type",
			typ:      nil,
			contains: "<nil>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatType(tt.typ)
			assert.Contains(t, result, tt.contains)
		})
	}
}

func TestErrorComposition(t *testing.T) {
	t.Run("nested errors", func(t *testing.T) {
		// Create a chain of errors
		innerErr := ErrServiceNotFound
		middleErr := ResolutionError{
			ServiceType: reflect.TypeOf((*TestService)(nil)),
			Cause:       innerErr,
		}
		outerErr := ModuleError{
			Module: "TestModule",
			Cause:  middleErr,
		}

		// Should be able to check for any error in the chain
		assert.True(t, errors.Is(outerErr, innerErr))
		assert.True(t, errors.As(outerErr, &middleErr))
	})

	t.Run("multiple error types", func(t *testing.T) {
		serviceType := reflect.TypeOf((*TestService)(nil))

		errors := []error{
			LifetimeError{Value: "invalid"},
			LifetimeConflictError{ServiceType: serviceType, Current: Singleton, Requested: Scoped},
			AlreadyRegisteredError{ServiceType: serviceType},
			ResolutionError{ServiceType: serviceType, Cause: ErrServiceNotFound},
			TimeoutError{ServiceType: serviceType, Timeout: time.Second},
			RegistrationError{ServiceType: serviceType, Operation: "provide", Cause: ErrNilConstructor},
			MissingConstructorError{ServiceType: serviceType},
			ValidationError{Message: "test"},
			ModuleError{Module: "test", Cause: ErrNilConstructor},
			TypeMismatchError{Expected: serviceType, Actual: serviceType},
		}

		// All errors should have non-empty messages
		for _, err := range errors {
			assert.NotEmpty(t, err.Error())
		}
	})
}

func TestErrorSerialization(t *testing.T) {
	t.Run("JSON serialization", func(t *testing.T) {
		type ErrorWrapper struct {
			Error string `json:"error"`
			Type  string `json:"type"`
		}

		err := LifetimeError{Value: "invalid"}
		wrapper := ErrorWrapper{
			Error: err.Error(),
			Type:  "LifetimeError",
		}

		data, jsonErr := json.Marshal(wrapper)
		require.NoError(t, jsonErr)

		var result ErrorWrapper
		jsonErr = json.Unmarshal(data, &result)
		require.NoError(t, jsonErr)

		assert.Equal(t, wrapper.Error, result.Error)
		assert.Equal(t, wrapper.Type, result.Type)
	})
}

func TestErrorEquality(t *testing.T) {
	t.Run("sentinel errors equality", func(t *testing.T) {
		// Sentinel errors should be equal to themselves
		assert.Equal(t, ErrServiceNotFound, ErrServiceNotFound)
		assert.NotEqual(t, ErrServiceNotFound, ErrServiceKeyNil)

		// Using errors.Is
		assert.True(t, errors.Is(ErrServiceNotFound, ErrServiceNotFound))
		assert.False(t, errors.Is(ErrServiceNotFound, ErrServiceKeyNil))
	})

	t.Run("typed errors equality", func(t *testing.T) {
		serviceType := reflect.TypeOf((*TestService)(nil))

		err1 := LifetimeError{Value: "invalid"}
		err2 := LifetimeError{Value: "invalid"}
		err3 := LifetimeError{Value: "different"}

		// Struct equality
		assert.Equal(t, err1, err2)
		assert.NotEqual(t, err1, err3)

		// Different error types
		err4 := AlreadyRegisteredError{ServiceType: serviceType}
		assert.NotEqual(t, err1, err4)
	})
}

func TestErrorNilHandling(t *testing.T) {
	t.Run("nil cause in wrapped errors", func(t *testing.T) {
		err := ResolutionError{
			ServiceType: reflect.TypeOf((*TestService)(nil)),
			Cause:       nil,
		}

		msg := err.Error()
		assert.Contains(t, msg, "unable to resolve")
		assert.Contains(t, msg, "<nil>")

		unwrapped := errors.Unwrap(err)
		assert.Nil(t, unwrapped)
	})
}

// Benchmark error creation and formatting
func BenchmarkErrorCreation(b *testing.B) {
	serviceType := reflect.TypeOf((*TestService)(nil))

	b.Run("LifetimeError", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			err := LifetimeError{Value: "invalid"}
			_ = err.Error()
		}
	})

	b.Run("ResolutionError", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			err := ResolutionError{
				ServiceType: serviceType,
				ServiceKey:  "key",
				Cause:       ErrServiceNotFound,
			}
			_ = err.Error()
		}
	})

	b.Run("formatType", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = formatType(serviceType)
		}
	})
}
