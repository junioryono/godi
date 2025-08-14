package godi_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/junioryono/godi/v3"
	"github.com/junioryono/godi/v3/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLifetimeError(t *testing.T) {
	t.Run("formats error message correctly", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name     string
			value    interface{}
			expected string
		}{
			{
				name:     "string value",
				value:    "invalid",
				expected: "invalid service lifetime: invalid",
			},
			{
				name:     "numeric value",
				value:    99,
				expected: "invalid service lifetime: 99",
			},
			{
				name:     "nil value",
				value:    nil,
				expected: "invalid service lifetime: <nil>",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := godi.LifetimeError{Value: tt.value}
				assert.Equal(t, tt.expected, err.Error())
			})
		}
	})
}

func TestLifetimeConflictError(t *testing.T) {
	t.Run("formats error message with type names", func(t *testing.T) {
		t.Parallel()

		err := godi.LifetimeConflictError{
			ServiceType: reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
			Current:     godi.Singleton,
			Requested:   godi.Scoped,
		}

		expected := "service testutil.TestLogger already registered as Singleton, cannot register as Scoped (use Replace to change)"
		assert.Equal(t, expected, err.Error())
	})

	t.Run("handles nil service type", func(t *testing.T) {
		t.Parallel()

		err := godi.LifetimeConflictError{
			ServiceType: nil,
			Current:     godi.Singleton,
			Requested:   godi.Scoped,
		}

		assert.Contains(t, err.Error(), "<nil>")
	})
}

func TestAlreadyRegisteredError(t *testing.T) {
	t.Run("formats error message", func(t *testing.T) {
		t.Parallel()

		err := godi.AlreadyRegisteredError{
			ServiceType: reflect.TypeOf((*testutil.TestService)(nil)),
		}

		expected := "service *testutil.TestService already registered (use keyed services, groups, or Replace)"
		assert.Equal(t, expected, err.Error())
	})
}

func TestCircularDependencyError(t *testing.T) {
	t.Run("with underlying dig error", func(t *testing.T) {
		t.Parallel()

		digErr := errors.New("dig: cycle detected")
		err := godi.CircularDependencyError{
			ServiceType: reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
			DigError:    digErr,
		}

		assert.Equal(t, "dig: cycle detected", err.Error())
		assert.ErrorIs(t, err, digErr)
	})

	t.Run("without chain", func(t *testing.T) {
		t.Parallel()

		err := godi.CircularDependencyError{
			ServiceType: reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
		}

		expected := "circular dependency detected for service: testutil.TestLogger"
		assert.Equal(t, expected, err.Error())
	})

	t.Run("with dependency chain", func(t *testing.T) {
		t.Parallel()

		err := godi.CircularDependencyError{
			ServiceType: reflect.TypeOf((*testutil.TestService)(nil)),
			Chain: []reflect.Type{
				reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
				reflect.TypeOf((*testutil.TestDatabase)(nil)).Elem(),
				reflect.TypeOf((*testutil.TestCache)(nil)).Elem(),
			},
		}

		expected := "circular dependency detected: testutil.TestLogger -> testutil.TestDatabase -> testutil.TestCache -> *testutil.TestService"
		assert.Equal(t, expected, err.Error())
	})

	t.Run("Unwrap returns dig error", func(t *testing.T) {
		t.Parallel()

		digErr := errors.New("underlying error")
		err := godi.CircularDependencyError{DigError: digErr}

		assert.ErrorIs(t, err, digErr)
	})
}

func TestResolutionError(t *testing.T) {
	t.Run("non-keyed service", func(t *testing.T) {
		t.Parallel()

		cause := errors.New("not found")
		err := godi.ResolutionError{
			ServiceType: reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
			Cause:       cause,
		}

		expected := "unable to resolve testutil.TestLogger: not found"
		assert.Equal(t, expected, err.Error())
		assert.ErrorIs(t, err, cause)
	})

	t.Run("keyed service", func(t *testing.T) {
		t.Parallel()

		cause := errors.New("key not found")
		err := godi.ResolutionError{
			ServiceType: reflect.TypeOf((*testutil.TestDatabase)(nil)).Elem(),
			ServiceKey:  "primary",
			Cause:       cause,
		}

		expected := "unable to resolve testutil.TestDatabase[key=primary]: key not found"
		assert.Equal(t, expected, err.Error())
		assert.ErrorIs(t, err, cause)
	})

	t.Run("complex key type", func(t *testing.T) {
		t.Parallel()

		type complexKey struct {
			Name string
			ID   int
		}

		err := godi.ResolutionError{
			ServiceType: reflect.TypeOf((*testutil.TestService)(nil)),
			ServiceKey:  complexKey{Name: "test", ID: 123},
			Cause:       godi.ErrServiceNotFound,
		}

		assert.Contains(t, err.Error(), "[key={test 123}]")
	})
}

func TestTimeoutError(t *testing.T) {
	t.Run("formats error message", func(t *testing.T) {
		t.Parallel()

		err := godi.TimeoutError{
			ServiceType: reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
			Timeout:     5 * time.Second,
		}

		expected := "resolution of testutil.TestLogger timed out after 5s"
		assert.Equal(t, expected, err.Error())
	})

	t.Run("Is returns true for DeadlineExceeded", func(t *testing.T) {
		t.Parallel()

		err := godi.TimeoutError{
			ServiceType: reflect.TypeOf((*testutil.TestService)(nil)),
			Timeout:     time.Millisecond,
		}

		assert.True(t, errors.Is(err, context.DeadlineExceeded))
	})
}

func TestRegistrationError(t *testing.T) {
	t.Run("formats error message", func(t *testing.T) {
		t.Parallel()

		cause := errors.New("invalid configuration")
		err := godi.RegistrationError{
			ServiceType: reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
			Operation:   "register",
			Cause:       cause,
		}

		expected := "failed to register testutil.TestLogger: invalid configuration"
		assert.Equal(t, expected, err.Error())
		assert.ErrorIs(t, err, cause)
	})

	t.Run("different operations", func(t *testing.T) {
		t.Parallel()

		operations := []string{"provide", "decorate", "validate"}

		for _, op := range operations {
			err := godi.RegistrationError{
				ServiceType: reflect.TypeOf((*testutil.TestService)(nil)),
				Operation:   op,
				Cause:       godi.ErrNilConstructor,
			}

			assert.Contains(t, err.Error(), fmt.Sprintf("failed to %s", op))
		}
	})
}

func TestMissingConstructorError(t *testing.T) {
	t.Run("with context", func(t *testing.T) {
		t.Parallel()

		err := godi.MissingConstructorError{
			ServiceType: reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
			Context:     "service",
		}

		expected := "service testutil.TestLogger has no constructor"
		assert.Equal(t, expected, err.Error())
	})

	t.Run("without context", func(t *testing.T) {
		t.Parallel()

		err := godi.MissingConstructorError{
			ServiceType: reflect.TypeOf((*testutil.TestDatabase)(nil)).Elem(),
		}

		expected := "testutil.TestDatabase has no constructor"
		assert.Equal(t, expected, err.Error())
	})
}

func TestValidationError(t *testing.T) {
	t.Run("with service type", func(t *testing.T) {
		t.Parallel()

		err := godi.ValidationError{
			ServiceType: reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
			Message:     "invalid lifetime",
		}

		expected := "testutil.TestLogger: invalid lifetime"
		assert.Equal(t, expected, err.Error())
	})

	t.Run("without service type", func(t *testing.T) {
		t.Parallel()

		err := godi.ValidationError{
			Message: "general validation error",
		}

		assert.Equal(t, "general validation error", err.Error())
	})
}

func TestModuleError(t *testing.T) {
	t.Run("wraps underlying error", func(t *testing.T) {
		t.Parallel()

		cause := errors.New("module initialization failed")
		err := godi.ModuleError{
			Module: "database",
			Cause:  cause,
		}

		expected := `module "database": module initialization failed`
		assert.Equal(t, expected, err.Error())
		assert.ErrorIs(t, err, cause)
	})

	t.Run("nested module errors", func(t *testing.T) {
		t.Parallel()

		innerErr := godi.ModuleError{
			Module: "repository",
			Cause:  godi.ErrNilConstructor,
		}

		outerErr := godi.ModuleError{
			Module: "application",
			Cause:  innerErr,
		}

		assert.Contains(t, outerErr.Error(), "application")
		assert.ErrorIs(t, outerErr, godi.ErrNilConstructor)
	})
}

func TestTypeMismatchError(t *testing.T) {
	t.Run("interface implementation", func(t *testing.T) {
		t.Parallel()

		err := godi.TypeMismatchError{
			Expected: reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
			Actual:   reflect.TypeOf((*testutil.TestDatabase)(nil)).Elem(),
			Context:  "interface implementation",
		}

		expected := "interface implementation: expected testutil.TestLogger, got testutil.TestDatabase"
		assert.Equal(t, expected, err.Error())
	})

	t.Run("type assertion", func(t *testing.T) {
		t.Parallel()

		err := godi.TypeMismatchError{
			Expected: reflect.TypeOf((*testutil.TestService)(nil)),
			Actual:   reflect.TypeOf(testutil.TestService{}),
			Context:  "type assertion",
		}

		expected := "type assertion: expected *testutil.TestService, got testutil.TestService"
		assert.Equal(t, expected, err.Error())
	})
}

func TestIsNotFound(t *testing.T) {
	t.Run("direct ErrServiceNotFound", func(t *testing.T) {
		t.Parallel()
		assert.True(t, godi.IsNotFound(godi.ErrServiceNotFound))
	})

	t.Run("wrapped in ResolutionError", func(t *testing.T) {
		t.Parallel()

		err := godi.ResolutionError{
			ServiceType: reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
			Cause:       godi.ErrServiceNotFound,
		}
		assert.True(t, godi.IsNotFound(err))
	})

	t.Run("dig missing type error", func(t *testing.T) {
		t.Parallel()

		// Simulate dig errors
		missingTypeErr := errors.New("missing type: testutil.TestLogger")
		assert.True(t, godi.IsNotFound(missingTypeErr))

		notProvidedErr := errors.New("type *testutil.TestService is not provided")
		assert.True(t, godi.IsNotFound(notProvidedErr))
	})

	t.Run("nested resolution errors", func(t *testing.T) {
		t.Parallel()

		innerErr := godi.ResolutionError{
			ServiceType: reflect.TypeOf((*testutil.TestDatabase)(nil)).Elem(),
			Cause:       errors.New("missing type: testutil.TestDatabase"),
		}

		outerErr := godi.ResolutionError{
			ServiceType: reflect.TypeOf((*testutil.TestService)(nil)),
			Cause:       innerErr,
		}

		assert.True(t, godi.IsNotFound(outerErr))
	})

	t.Run("not a not-found error", func(t *testing.T) {
		t.Parallel()

		assert.False(t, godi.IsNotFound(nil))
		assert.False(t, godi.IsNotFound(errors.New("some other error")))
		assert.False(t, godi.IsNotFound(godi.ErrNilConstructor))
	})
}

type A struct{ B *B }
type B struct{ A *A }

func TestIsCircularDependency(t *testing.T) {
	t.Run("CircularDependencyError", func(t *testing.T) {
		t.Parallel()

		err := godi.CircularDependencyError{
			ServiceType: reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
		}
		assert.True(t, godi.IsCircularDependency(err))
	})

	t.Run("dig cycle detected", func(t *testing.T) {
		t.Parallel()

		collection := godi.NewServiceCollection()
		collection.AddSingleton(func(b *B) *A { return &A{B: b} })
		collection.AddSingleton(func(a *A) *B { return &B{A: a} })

		_, err := collection.BuildServiceProvider()
		require.Error(t, err)
		assert.True(t, godi.IsCircularDependency(err))
	})

	t.Run("not a circular dependency", func(t *testing.T) {
		t.Parallel()

		assert.False(t, godi.IsCircularDependency(nil))
		assert.False(t, godi.IsCircularDependency(errors.New("some error")))
		assert.False(t, godi.IsCircularDependency(godi.ErrServiceNotFound))
	})
}

func TestIsDisposed(t *testing.T) {
	t.Run("disposal errors", func(t *testing.T) {
		t.Parallel()

		assert.True(t, godi.IsDisposed(godi.ErrDisposed))
		assert.True(t, godi.IsDisposed(godi.ErrScopeDisposed))
		assert.True(t, godi.IsDisposed(godi.ErrProviderDisposed))
	})

	t.Run("wrapped disposal errors", func(t *testing.T) {
		t.Parallel()

		wrapped := fmt.Errorf("operation failed: %w", godi.ErrScopeDisposed)
		assert.True(t, godi.IsDisposed(wrapped))
	})

	t.Run("not disposal errors", func(t *testing.T) {
		t.Parallel()

		assert.False(t, godi.IsDisposed(nil))
		assert.False(t, godi.IsDisposed(errors.New("some error")))
		assert.False(t, godi.IsDisposed(godi.ErrServiceNotFound))
	})
}

func TestIsTimeout(t *testing.T) {
	t.Run("TimeoutError", func(t *testing.T) {
		t.Parallel()

		err := godi.TimeoutError{
			ServiceType: reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
			Timeout:     time.Second,
		}
		assert.True(t, godi.IsTimeout(err))
	})

	t.Run("context deadline exceeded", func(t *testing.T) {
		t.Parallel()
		assert.True(t, godi.IsTimeout(context.DeadlineExceeded))
	})

	t.Run("wrapped deadline exceeded", func(t *testing.T) {
		t.Parallel()

		wrapped := fmt.Errorf("operation failed: %w", context.DeadlineExceeded)
		assert.True(t, godi.IsTimeout(wrapped))
	})

	t.Run("not timeout errors", func(t *testing.T) {
		t.Parallel()

		assert.False(t, godi.IsTimeout(nil))
		assert.False(t, godi.IsTimeout(errors.New("some error")))
		assert.False(t, godi.IsTimeout(context.Canceled))
	})
}

func TestSentinelErrors(t *testing.T) {
	t.Run("all sentinel errors have messages", func(t *testing.T) {
		t.Parallel()

		sentinelErrors := []error{
			godi.ErrServiceNotFound,
			godi.ErrServiceKeyNil,
			godi.ErrInvalidServiceType,
			godi.ErrFailedToExtractService,
			godi.ErrFailedToExtractKeyedService,
			godi.ErrDisposed,
			godi.ErrScopeDisposed,
			godi.ErrProviderDisposed,
			godi.ErrNilConstructor,
			godi.ErrNilServiceProvider,
			godi.ErrConstructorNotFunction,
			godi.ErrConstructorNoReturn,
			godi.ErrConstructorTooManyReturns,
			godi.ErrConstructorInvalidSecondReturn,
			godi.ErrConstructorMultipleIn,
			godi.ErrConstructorOutMaxReturns,
			godi.ErrDecoratorNil,
			godi.ErrDecoratorNotFunction,
			godi.ErrDecoratorNoParams,
			godi.ErrDecoratorNoReturn,
			godi.ErrCollectionBuilt,
			godi.ErrCollectionModifyAfterBuild,
			godi.ErrDescriptorNil,
			godi.ErrNoConstructorOrDecorator,
			godi.ErrBothConstructorAndDecorator,
			godi.ErrServicesNil,
			godi.ErrProviderFunctionNotFound,
			godi.ErrKeyedProviderFunctionNotFound,
			godi.ErrConstructorMustReturnValue,
			godi.ErrServiceHasNoConstructor,
			godi.ErrDescriptorHasNoConstructor,
			godi.ErrFailedToCreateScope,
			godi.ErrResultObjectConstructor,
			godi.ErrReplaceResultObject,
			godi.ErrScopeNotInContext,
		}

		for _, err := range sentinelErrors {
			assert.NotEmpty(t, err.Error(), "error %v has empty message", err)

			// Test error identity
			assert.ErrorIs(t, err, err)

			// Test wrapping
			wrapped := fmt.Errorf("wrapped: %w", err)
			assert.ErrorIs(t, wrapped, err)
		}
	})
}

func TestErrorEdgeCases(t *testing.T) {
	t.Run("nil type formatting", func(t *testing.T) {
		t.Parallel()

		// Test various errors with nil types
		testCases := []struct {
			name        string
			err         error
			shouldCheck bool // Some errors might not format nil types
		}{
			{
				name:        "LifetimeConflictError",
				err:         godi.LifetimeConflictError{ServiceType: nil, Current: godi.Singleton, Requested: godi.Scoped},
				shouldCheck: true,
			},
			{
				name:        "AlreadyRegisteredError",
				err:         godi.AlreadyRegisteredError{ServiceType: nil},
				shouldCheck: true,
			},
			{
				name:        "CircularDependencyError",
				err:         godi.CircularDependencyError{ServiceType: nil},
				shouldCheck: true,
			},
			{
				name:        "ResolutionError",
				err:         godi.ResolutionError{ServiceType: nil, Cause: godi.ErrServiceNotFound},
				shouldCheck: true,
			},
			{
				name:        "TimeoutError",
				err:         godi.TimeoutError{ServiceType: nil, Timeout: time.Second},
				shouldCheck: true,
			},
			{
				name:        "RegistrationError",
				err:         godi.RegistrationError{ServiceType: nil, Operation: "test", Cause: errors.New("test")},
				shouldCheck: true,
			},
			{
				name:        "MissingConstructorError",
				err:         godi.MissingConstructorError{ServiceType: nil, Context: "test"},
				shouldCheck: true,
			},
			{
				name:        "ValidationError with nil ServiceType",
				err:         godi.ValidationError{ServiceType: nil, Message: "test"},
				shouldCheck: false, // This one just returns the message when ServiceType is nil
			},
			{
				name:        "TypeMismatchError",
				err:         godi.TypeMismatchError{Expected: nil, Actual: nil, Context: "test"},
				shouldCheck: true,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.NotPanics(t, func() {
					msg := tc.err.Error()
					if tc.shouldCheck {
						assert.Contains(t, msg, "<nil>")
					}
				})
			})
		}
	})

	t.Run("complex type formatting", func(t *testing.T) {
		t.Parallel()

		// Test with complex nested types
		complexType := reflect.TypeOf(map[string][]chan<- func(context.Context) (*testutil.TestService, error){})

		err := godi.ResolutionError{
			ServiceType: complexType,
			Cause:       godi.ErrServiceNotFound,
		}

		// Should not panic and should produce readable output
		assert.NotPanics(t, func() {
			msg := err.Error()
			assert.Contains(t, msg, "map[string][]chan<- func(context.Context) (*testutil.TestService, error)")
		})
	})

	t.Run("empty chain in CircularDependencyError", func(t *testing.T) {
		t.Parallel()

		err := godi.CircularDependencyError{
			ServiceType: nil,
			Chain:       []reflect.Type{},
		}

		assert.NotPanics(t, func() {
			msg := err.Error()
			assert.Contains(t, msg, "circular dependency detected")
		})
	})

	t.Run("very long timeout formatting", func(t *testing.T) {
		t.Parallel()

		err := godi.TimeoutError{
			ServiceType: reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
			Timeout:     24*time.Hour + 30*time.Minute + 45*time.Second,
		}

		msg := err.Error()
		assert.Contains(t, msg, "24h30m45s")
	})

	t.Run("error analysis with deeply nested errors", func(t *testing.T) {
		t.Parallel()

		// Create deeply nested error chain
		err := godi.ErrServiceNotFound
		for i := 0; i < 10; i++ {
			err = godi.ResolutionError{
				ServiceType: reflect.TypeOf(fmt.Sprintf("Service%d", i)),
				Cause:       err,
			}
		}

		assert.True(t, godi.IsNotFound(err))
	})

	t.Run("concurrent error checking", func(t *testing.T) {
		t.Parallel()

		err := godi.CircularDependencyError{
			ServiceType: reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
		}

		// Run concurrent checks
		done := make(chan bool, 100)
		for i := 0; i < 100; i++ {
			go func() {
				assert.True(t, godi.IsCircularDependency(err))
				done <- true
			}()
		}

		// Wait for all goroutines
		for i := 0; i < 100; i++ {
			<-done
		}
	})
}
