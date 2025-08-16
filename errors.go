package godi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/junioryono/godi/v3/internal/graph"
	"github.com/junioryono/godi/v3/internal/typecache"
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
	ErrConstructorTooManyReturns      = errors.New("constructor must return at most 2 values")        // Deprecated: kept for compatibility
	ErrConstructorInvalidSecondReturn = errors.New("constructor's second return value must be error") // Deprecated: kept for compatibility
	ErrConstructorInvalidErrorReturn  = errors.New("constructor's last return value must be error if it returns an error")
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
	Value any
}

func (e LifetimeError) Error() string {
	return fmt.Sprintf("invalid service lifetime: %v", e.Value)
}

// LifetimeConflictError indicates a service is registered with conflicting lifetimes.
type LifetimeConflictError struct {
	ServiceType reflect.Type
	Current     Lifetime
	Requested   Lifetime
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

// Type aliases for graph package types to maintain backward compatibility
type CircularDependencyError = graph.CircularDependencyError

// ResolutionError wraps errors that occur during service resolution.
type ResolutionError struct {
	ServiceType reflect.Type
	ServiceKey  any // nil for non-keyed services
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

// formatType formats a reflect.Type for error messages.
func formatType(t reflect.Type) string {
	if t == nil {
		return "<nil>"
	}

	info := typecache.GetTypeInfo(t)
	return info.GetFormattedName()
}
