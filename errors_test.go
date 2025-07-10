package godi_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/junioryono/godi"
)

var (
	errDependencyNotFound = errors.New("dependency not found")
	errDigMissingType     = errors.New("missing type: *godi.TestService")
	errRandom             = errors.New("random error")
	errNotCircular        = errors.New("not circular")
	errNotDisposed        = errors.New("not disposed")
	errAlreadyClosed      = errors.New("already closed")
	errIntentional        = errors.New("intentional error")
	errStopHere           = errors.New("stop here")
	errAlreadyDisposed    = errors.New("already disposed")
	errTest               = errors.New("test error")
	errConstructor        = errors.New("constructor error")
	errDisposal           = errors.New("disposal error")
)

func TestLifetimeError(t *testing.T) {
	t.Run("with string value", func(t *testing.T) {
		err := godi.LifetimeError{
			Value: "invalid",
		}

		expected := "invalid service lifetime: invalid"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})

	t.Run("with int value", func(t *testing.T) {
		err := godi.LifetimeError{
			Value: 42,
		}

		expected := "invalid service lifetime: 42"
		if err.Error() != expected {
			t.Errorf("expected %q, got %q", expected, err.Error())
		}
	})
}

func TestCircularDependencyError(t *testing.T) {
	t.Run("with dig error", func(t *testing.T) {
		// Create a mock dig error
		err := godi.CircularDependencyError{
			ServiceType: reflect.TypeOf((*TestService)(nil)),
			DigError:    errDigMissingType,
		}

		if err.Error() != "dig: cycle detected" {
			t.Errorf("expected dig error message, got %q", err.Error())
		}

		if !errors.Is(err.Unwrap(), errDigMissingType) {
			t.Error("Unwrap should return the dig error")
		}
	})

	t.Run("without dig error but with chain", func(t *testing.T) {
		err := godi.CircularDependencyError{
			ServiceType: reflect.TypeOf((*TestService)(nil)),
			Chain: []reflect.Type{
				reflect.TypeOf((*TestLogger)(nil)).Elem(),
				reflect.TypeOf((*TestDatabase)(nil)).Elem(),
			},
		}

		errStr := err.Error()
		if !strings.Contains(errStr, "circular dependency detected") {
			t.Error("expected 'circular dependency detected' in error")
		}
		if !strings.Contains(errStr, "TestLogger") {
			t.Error("expected chain to contain TestLogger")
		}
		if !strings.Contains(errStr, "TestDatabase") {
			t.Error("expected chain to contain TestDatabase")
		}
		if !strings.Contains(errStr, "TestService") {
			t.Error("expected chain to contain TestService")
		}
		if !strings.Contains(errStr, "->") {
			t.Error("expected chain to use '->' separator")
		}
	})

	t.Run("without dig error or chain", func(t *testing.T) {
		err := godi.CircularDependencyError{
			ServiceType: reflect.TypeOf((*TestService)(nil)),
		}

		errStr := err.Error()
		if !strings.Contains(errStr, "circular dependency detected") {
			t.Error("expected 'circular dependency detected' in error")
		}
		if !strings.Contains(errStr, "TestService") {
			t.Error("expected service type in error")
		}
	})
}

func TestResolutionError(t *testing.T) {
	t.Run("non-keyed service", func(t *testing.T) {
		err := godi.ResolutionError{
			ServiceType: reflect.TypeOf((*TestLogger)(nil)).Elem(),
			Cause:       errDependencyNotFound,
		}

		errStr := err.Error()
		if !strings.Contains(errStr, "unable to resolve service") {
			t.Error("expected 'unable to resolve service' in error")
		}
		if !strings.Contains(errStr, "TestLogger") {
			t.Error("expected service type in error")
		}
		if !strings.Contains(errStr, "dependency not found") {
			t.Error("expected cause in error")
		}

		if !errors.Is(err.Unwrap(), errDependencyNotFound) {
			t.Error("Unwrap should return the dig error")
		}
	})

	t.Run("keyed service", func(t *testing.T) {
		err := godi.ResolutionError{
			ServiceType: reflect.TypeOf((*TestDatabase)(nil)).Elem(),
			ServiceKey:  "primary",
			Cause:       godi.ErrServiceNotFound,
		}

		errStr := err.Error()
		if !strings.Contains(errStr, "unable to resolve keyed service") {
			t.Error("expected 'unable to resolve keyed service' in error")
		}
		if !strings.Contains(errStr, "TestDatabase") {
			t.Error("expected service type in error")
		}
		if !strings.Contains(errStr, "primary") {
			t.Error("expected service key in error")
		}
	})
}

func TestCommonErrors(t *testing.T) {
	// Test that common errors are defined and have expected messages
	errorTests := []struct {
		err      error
		contains string
	}{
		{godi.ErrServiceNotFound, "service not found"},
		{godi.ErrScopeDisposed, "scope has been disposed"},
		{godi.ErrProviderDisposed, "provider has been disposed"},
		{godi.ErrInvalidServiceType, "invalid service type"},
		{godi.ErrNilConstructor, "constructor cannot be nil"},
		{godi.ErrNilServiceProvider, "service provider cannot be nil"},
	}

	for _, tt := range errorTests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			if !strings.Contains(tt.err.Error(), tt.contains) {
				t.Errorf("expected error %v to contain %q", tt.err, tt.contains)
			}
		})
	}
}

func TestErrorAnalysisFunctions(t *testing.T) {
	t.Run("IsNotFound", func(t *testing.T) {
		// Direct match
		if !godi.IsNotFound(godi.ErrServiceNotFound) {
			t.Error("expected true for ErrServiceNotFound")
		}

		// Wrapped in ResolutionError
		resErr := godi.ResolutionError{Cause: godi.ErrServiceNotFound}
		if !godi.IsNotFound(resErr) {
			t.Error("expected true for wrapped ErrServiceNotFound")
		}

		// Dig-style error
		if !godi.IsNotFound(errDigMissingType) {
			t.Error("expected true for dig missing type error")
		}

		// Not a service not found error
		if godi.IsNotFound(errRandom) {
			t.Error("expected false for random error")
		}

		// Nil error
		if godi.IsNotFound(nil) {
			t.Error("expected false for nil error")
		}
	})

	t.Run("IsCircularDependency", func(t *testing.T) {
		// Our error type
		circErr := godi.CircularDependencyError{ServiceType: reflect.TypeOf((*TestService)(nil))}
		if !godi.IsCircularDependency(circErr) {
			t.Error("expected true for CircularDependencyError")
		}

		// Non-circular error
		if godi.IsCircularDependency(errNotCircular) {
			t.Error("expected false for non-circular error")
		}

		// Nil error
		if godi.IsCircularDependency(nil) {
			t.Error("expected false for nil error")
		}
	})

	t.Run("IsDisposed", func(t *testing.T) {
		// Scope disposed
		if !godi.IsDisposed(godi.ErrScopeDisposed) {
			t.Error("expected true for ErrScopeDisposed")
		}

		// Provider disposed
		if !godi.IsDisposed(godi.ErrProviderDisposed) {
			t.Error("expected true for ErrProviderDisposed")
		}

		// Other error
		if godi.IsDisposed(errNotDisposed) {
			t.Error("expected false for non-disposed error")
		}

		// Nil error
		if godi.IsDisposed(nil) {
			t.Error("expected false for nil error")
		}
	})
}
