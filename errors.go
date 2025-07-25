package godi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/junioryono/godi/v2/internal/typecache"
	"go.uber.org/dig"
)

// ========================================
// Core Error Values (Sentinel Errors)
// ========================================

var (
	// Service resolution errors.
	ErrServiceNotFound             = errors.New("service not found")
	ErrServiceKeyNil               = errors.New("service key cannot be nil")
	ErrInvalidServiceType          = errors.New("invalid service type")
	ErrFailedToExtractService      = errors.New("failed to extract service")
	ErrFailedToExtractKeyedService = errors.New("failed to extract keyed service")

	// Lifecycle errors.
	ErrDisposed         = errors.New("disposed")
	ErrNilScope         = errors.New("scope cannot be nil")
	ErrScopeDisposed    = errors.New("scope has been disposed")
	ErrProviderDisposed = errors.New("service provider has been disposed")

	// Constructor/registration errors.
	ErrNilConstructor                 = errors.New("constructor cannot be nil")
	ErrNilServiceProvider             = errors.New("service provider cannot be nil")
	ErrConstructorNotFunction         = errors.New("constructor must be a function")
	ErrConstructorNoReturn            = errors.New("constructor must return at least one value")
	ErrConstructorTooManyReturns      = errors.New("constructor must return at most 2 values")
	ErrConstructorInvalidSecondReturn = errors.New("constructor's second return value must be error")
	ErrConstructorMultipleIn          = errors.New("constructor cannot have multiple In parameters")
	ErrConstructorOutMaxReturns       = errors.New("constructor with Out must return at most 2 values")

	// Decorator errors.
	ErrDecoratorNil         = errors.New("decorator cannot be nil")
	ErrDecoratorNotFunction = errors.New("decorator must be a function")
	ErrDecoratorNoParams    = errors.New("decorator must have at least one parameter")
	ErrDecoratorNoReturn    = errors.New("decorator must return at least one value")

	// Collection/descriptor errors.
	ErrCollectionBuilt             = errors.New("service collection has already been built")
	ErrCollectionModifyAfterBuild  = errors.New("cannot modify service collection after build")
	ErrDescriptorNil               = errors.New("descriptor cannot be nil")
	ErrNoConstructorOrDecorator    = errors.New("constructor or decorator must be provided")
	ErrBothConstructorAndDecorator = errors.New("cannot have both constructor and decorator")

	// Provider errors.
	ErrServicesNil                   = errors.New("services cannot be nil")
	ErrProviderFunctionNotFound      = errors.New("provider function not found")
	ErrKeyedProviderFunctionNotFound = errors.New("keyed provider function not found")
	ErrConstructorMustReturnValue    = errors.New("constructor must be a function that returns at least one value")
	ErrServiceHasNoConstructor       = errors.New("service has no constructor")
	ErrDescriptorHasNoConstructor    = errors.New("descriptor has no constructor")
	ErrFailedToCreateScope           = errors.New("failed to create dig scope: dig container is nil")

	// Special cases.
	ErrResultObjectConstructor = errors.New("constructor returns Out - use collection.Add* methods")
	ErrReplaceResultObject     = errors.New("replace not supported for result object constructors")

	// Context errors
	ErrScopeNotInContext = errors.New("no scope found in context")
)

// ========================================
// Typed Errors for Rich Context
// ========================================

// LifetimeError indicates an invalid service lifetime value.
type LifetimeError struct {
	Value interface{}
}

func (e LifetimeError) Error() string {
	return fmt.Sprintf("invalid service lifetime: %v", e.Value)
}

// LifetimeConflictError indicates a service is registered with conflicting lifetimes.
type LifetimeConflictError struct {
	ServiceType reflect.Type
	Current     ServiceLifetime
	Requested   ServiceLifetime
}

func (e LifetimeConflictError) Error() string {
	return fmt.Sprintf("service %s already registered as %s, cannot register as %s (use Replace to change)", formatType(e.ServiceType), e.Current, e.Requested)
}

// AlreadyRegisteredError indicates a service type is already registered.
type AlreadyRegisteredError struct {
	ServiceType reflect.Type
}

func (e AlreadyRegisteredError) Error() string {
	return fmt.Sprintf("service %s already registered (use keyed services, groups, or Replace)", formatType(e.ServiceType))
}

// CircularDependencyError represents a circular dependency in the container.
type CircularDependencyError struct {
	ServiceType reflect.Type
	Chain       []reflect.Type // If available from parsing the error
	DigError    error          // The underlying dig error
}

func (e CircularDependencyError) Error() string {
	// If we have the underlying dig error, use its message which has better details
	if e.DigError != nil {
		return e.DigError.Error()
	}

	// Otherwise, format our own message
	if len(e.Chain) == 0 {
		return fmt.Sprintf("circular dependency detected for service: %s", formatType(e.ServiceType))
	}

	// Build a visual representation of the dependency chain
	chain := make([]string, 0, len(e.Chain)+1)
	for _, t := range e.Chain {
		chain = append(chain, formatType(t))
	}
	if e.ServiceType != nil {
		chain = append(chain, formatType(e.ServiceType))
	}

	return fmt.Sprintf("circular dependency detected: %s", strings.Join(chain, " -> "))
}

func (e CircularDependencyError) Unwrap() error {
	return e.DigError
}

// ResolutionError wraps errors that occur during service resolution.
type ResolutionError struct {
	ServiceType reflect.Type
	ServiceKey  interface{} // nil for non-keyed services
	Cause       error
}

func (e ResolutionError) Error() string {
	if e.ServiceKey != nil {
		return fmt.Sprintf("unable to resolve %s[key=%v]: %v", formatType(e.ServiceType), e.ServiceKey, e.Cause)
	}

	return fmt.Sprintf("unable to resolve %s: %v", formatType(e.ServiceType), e.Cause)
}

func (e ResolutionError) Unwrap() error {
	return e.Cause
}

// TimeoutError indicates a service resolution timed out.
type TimeoutError struct {
	ServiceType reflect.Type
	Timeout     time.Duration
}

func (e TimeoutError) Error() string {
	return fmt.Sprintf("resolution of %s timed out after %v", formatType(e.ServiceType), e.Timeout)
}

func (e TimeoutError) Is(target error) bool {
	return errors.Is(target, context.DeadlineExceeded)
}

// RegistrationError wraps errors during service registration.
type RegistrationError struct {
	ServiceType reflect.Type
	Operation   string // "provide", "decorate", etc.
	Cause       error
}

func (e RegistrationError) Error() string {
	return fmt.Sprintf("failed to %s %s: %v", e.Operation, formatType(e.ServiceType), e.Cause)
}

func (e RegistrationError) Unwrap() error {
	return e.Cause
}

// MissingConstructorError indicates a service has no constructor.
type MissingConstructorError struct {
	ServiceType reflect.Type
	Context     string // "service" or "descriptor"
}

func (e MissingConstructorError) Error() string {
	if e.Context != "" {
		return fmt.Sprintf("%s %s has no constructor", e.Context, formatType(e.ServiceType))
	}
	return fmt.Sprintf("%s has no constructor", formatType(e.ServiceType))
}

// ValidationError indicates a validation failure.
type ValidationError struct {
	ServiceType reflect.Type
	Message     string
}

func (e ValidationError) Error() string {
	if e.ServiceType != nil {
		return fmt.Sprintf("%s: %s", formatType(e.ServiceType), e.Message)
	}

	return e.Message
}

// ModuleError wraps errors from module registration.
type ModuleError struct {
	Module string
	Cause  error
}

func (e ModuleError) Error() string {
	return fmt.Sprintf("module %q: %v", e.Module, e.Cause)
}

func (e ModuleError) Unwrap() error {
	return e.Cause
}

// TypeMismatchError indicates a type assertion or conversion failed.
type TypeMismatchError struct {
	Expected reflect.Type
	Actual   reflect.Type
	Context  string // "interface implementation", "type assertion", etc.
}

func (e TypeMismatchError) Error() string {
	return fmt.Sprintf("%s: expected %s, got %s", e.Context, formatType(e.Expected), formatType(e.Actual))
}

// ========================================
// Error Analysis Functions
// ========================================

// IsNotFound checks if an error indicates a service was not found.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}

	// Check our errors
	if errors.Is(err, ErrServiceNotFound) {
		return true
	}

	// Check resolution errors
	var resErr ResolutionError
	if errors.As(err, &resErr) {
		return IsNotFound(resErr.Cause)
	}

	// Check dig errors
	errStr := err.Error()
	return strings.Contains(errStr, "missing type") || strings.Contains(errStr, "not provided")
}

// IsCircularDependency checks if an error is due to circular dependencies.
func IsCircularDependency(err error) bool {
	if err == nil {
		return false
	}

	var circErr CircularDependencyError
	if errors.As(err, &circErr) {
		return true
	}

	return dig.IsCycleDetected(err)
}

// IsDisposed checks if an error indicates a disposed scope or provider.
func IsDisposed(err error) bool {
	if err == nil {
		return false
	}

	return errors.Is(err, ErrDisposed) ||
		errors.Is(err, ErrScopeDisposed) ||
		errors.Is(err, ErrProviderDisposed)
}

// IsTimeout checks if an error is due to a timeout.
func IsTimeout(err error) bool {
	if err == nil {
		return false
	}

	var timeoutErr TimeoutError
	if errors.As(err, &timeoutErr) {
		return true
	}

	return errors.Is(err, context.DeadlineExceeded)
}

// ========================================
// Type Formatting
// ========================================

// formatType formats a reflect.Type for error messages.
func formatType(t reflect.Type) string {
	if t == nil {
		return "<nil>"
	}

	info := typecache.GetTypeInfo(t)
	return info.GetFormattedName()
}
