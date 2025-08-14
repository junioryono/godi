package godi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

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

// CycleError represents a circular dependency in the graph
type CycleError struct {
	Node NodeKey
	Path []NodeKey
}

// Error implements the error interface
func (e *CycleError) Error() string {
	if len(e.Path) == 0 {
		return fmt.Sprintf("circular dependency detected involving %s", e.Node.String())
	}

	// Build a visual representation of the cycle
	pathStrs := make([]string, len(e.Path))
	for i, node := range e.Path {
		pathStrs[i] = node.String()
	}

	return fmt.Sprintf("circular dependency detected: %s", strings.Join(pathStrs, " -> "))
}

// IsCycleError checks if an error is a cycle error
func IsCycleError(err error) bool {
	_, ok := err.(*CycleError)
	return ok
}

// GetCyclePath extracts the cycle path from an error if it's a CycleError
func GetCyclePath(err error) ([]NodeKey, bool) {
	if cycleErr, ok := err.(*CycleError); ok {
		return cycleErr.Path, true
	}
	return nil, false
}

// ResolutionError represents an error during service resolution.
type ResolutionError struct {
	ServiceType reflect.Type
	Key         any
	Cause       error
	Stack       []ResolutionFrame // Resolution stack for debugging
}

// ResolutionFrame represents a frame in the resolution stack.
type ResolutionFrame struct {
	ServiceType reflect.Type
	Key         any
	Lifetime    string
	Source      string // Constructor source location
}

// Error implements the error interface.
func (e *ResolutionError) Error() string {
	var msg strings.Builder

	if e.Key != nil {
		msg.WriteString(fmt.Sprintf("failed to resolve %v[%v]", e.ServiceType, e.Key))
	} else {
		msg.WriteString(fmt.Sprintf("failed to resolve %v", e.ServiceType))
	}

	if e.Cause != nil {
		msg.WriteString(fmt.Sprintf(": %v", e.Cause))
	}

	if len(e.Stack) > 0 {
		msg.WriteString("\n\nResolution stack:")
		for i, frame := range e.Stack {
			msg.WriteString(fmt.Sprintf("\n  %d. %s", i+1, frame.String()))
		}
	}

	return msg.String()
}

// Unwrap returns the underlying cause.
func (e *ResolutionError) Unwrap() error {
	return e.Cause
}

// String formats a resolution frame.
func (f ResolutionFrame) String() string {
	if f.Key != nil {
		return fmt.Sprintf("%v[%v] (%s)", f.ServiceType, f.Key, f.Lifetime)
	}
	return fmt.Sprintf("%v (%s)", f.ServiceType, f.Lifetime)
}

// CircularDependencyError represents a circular dependency during resolution.
type CircularDependencyError struct {
	ServiceType reflect.Type
	Key         any
	Chain       []reflect.Type
}

// Error implements the error interface.
func (e *CircularDependencyError) Error() string {
	var msg strings.Builder

	if e.Key != nil {
		msg.WriteString(fmt.Sprintf("circular dependency detected for %v[%v]", e.ServiceType, e.Key))
	} else {
		msg.WriteString(fmt.Sprintf("circular dependency detected for %v", e.ServiceType))
	}

	if len(e.Chain) > 0 {
		msg.WriteString("\nDependency chain: ")
		for i, t := range e.Chain {
			if i > 0 {
				msg.WriteString(" -> ")
			}
			msg.WriteString(fmt.Sprintf("%v", t))
		}
		msg.WriteString(fmt.Sprintf(" -> %v", e.ServiceType))
	}

	return msg.String()
}

// MaxDepthError represents exceeding maximum resolution depth.
type MaxDepthError struct {
	ServiceType reflect.Type
	Depth       int
	MaxDepth    int
}

// Error implements the error interface.
func (e *MaxDepthError) Error() string {
	return fmt.Sprintf("maximum resolution depth %d exceeded while resolving %v (current depth: %d)",
		e.MaxDepth, e.ServiceType, e.Depth)
}

// MissingDependencyError represents a missing dependency.
type MissingDependencyError struct {
	DependentType  reflect.Type
	DependencyType reflect.Type
	Key            any
	Group          string
	Optional       bool
}

// Error implements the error interface.
func (e *MissingDependencyError) Error() string {
	var dependency string

	if e.Group != "" {
		dependency = fmt.Sprintf("group '%s' of type %v", e.Group, e.DependencyType)
	} else if e.Key != nil {
		dependency = fmt.Sprintf("%v[%v]", e.DependencyType, e.Key)
	} else {
		dependency = fmt.Sprintf("%v", e.DependencyType)
	}

	if e.Optional {
		return fmt.Sprintf("%v has optional dependency on %s which could not be resolved",
			e.DependentType, dependency)
	}

	return fmt.Sprintf("%v requires %s which is not registered",
		e.DependentType, dependency)
}

// ConstructorError represents an error during constructor invocation.
type ConstructorError struct {
	ServiceType reflect.Type
	Constructor reflect.Type
	Cause       error
}

// Error implements the error interface.
func (e *ConstructorError) Error() string {
	return fmt.Sprintf("constructor for %v failed: %v", e.ServiceType, e.Cause)
}

// Unwrap returns the underlying cause.
func (e *ConstructorError) Unwrap() error {
	return e.Cause
}

// ValidationError represents a constructor validation error.
type ValidationError struct {
	ServiceType reflect.Type
	Message     string
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed for %v: %s", e.ServiceType, e.Message)
}

// DecoratorError represents an error during decorator application.
type DecoratorError struct {
	ServiceType    reflect.Type
	DecoratorIndex int
	Cause          error
}

// Error implements the error interface.
func (e *DecoratorError) Error() string {
	return fmt.Sprintf("decorator %d for %v failed: %v",
		e.DecoratorIndex, e.ServiceType, e.Cause)
}

// Unwrap returns the underlying cause.
func (e *DecoratorError) Unwrap() error {
	return e.Cause
}

// ErrorHandler provides methods for handling resolution errors.
type ErrorHandler struct {
	onError func(error)
}

// NewErrorHandler creates a new error handler.
func NewErrorHandler(onError func(error)) *ErrorHandler {
	return &ErrorHandler{onError: onError}
}

// Handle handles an error, calling the error callback if set.
func (h *ErrorHandler) Handle(err error) {
	if h.onError != nil && err != nil {
		h.onError(err)
	}
}

// WrapResolutionError wraps an error with resolution context.
func (h *ErrorHandler) WrapResolutionError(
	serviceType reflect.Type,
	key any,
	cause error,
	stack []ResolutionFrame,
) error {
	return &ResolutionError{
		ServiceType: serviceType,
		Key:         key,
		Cause:       cause,
		Stack:       stack,
	}
}

// IsResolutionError checks if an error is a ResolutionError.
func IsResolutionError(err error) bool {
	_, ok := err.(*ResolutionError)
	return ok
}

// IsCircularDependency checks if an error is a CircularDependencyError.
func IsCircularDependency(err error) bool {
	_, ok := err.(*CircularDependencyError)
	return ok
}

// IsMissingDependency checks if an error is a MissingDependencyError.
func IsMissingDependency(err error) bool {
	_, ok := err.(*MissingDependencyError)
	return ok
}

// GetResolutionStack extracts the resolution stack from an error if available.
func GetResolutionStack(err error) ([]ResolutionFrame, bool) {
	if resErr, ok := err.(*ResolutionError); ok {
		return resErr.Stack, true
	}
	return nil, false
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
