package godi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/junioryono/godi/v4/internal/graph"
)

// ========================================
// Core Error Values (Sentinel Errors)
// ========================================
// These are base errors that should be wrapped in typed errors when returned.
// Never return these directly to users - always wrap them with context.

var (
	// Service resolution errors.
	ErrServiceNotFound = errors.New("service not found")
	ErrServiceKeyNil   = errors.New("service key cannot be nil")
	ErrServiceTypeNil  = errors.New("service type cannot be nil")

	// Lifecycle errors.
	ErrProviderNil      = errors.New("service provider cannot be nil")
	ErrProviderDisposed = errors.New("service provider has been disposed")
	ErrScopeDisposed    = errors.New("scope has been disposed")

	// Constructor/registration errors.
	ErrConstructorNil         = errors.New("constructor cannot be nil")
	ErrConstructorNotFunction = errors.New("constructor must be a function")
	ErrConstructorNoReturn    = errors.New("constructor must return at least one value")
	ErrConstructorReturnedNil = errors.New("constructor returned nil")

	// Validation errors.
	ErrGroupNameEmpty          = errors.New("group name cannot be empty")
	ErrSingletonNotInitialized = errors.New("singleton not initialized at build time")
	ErrDescriptorNil           = errors.New("descriptor cannot be nil")
)

var (
	_ error = LifetimeError{}
	_ error = LifetimeConflictError{}
	_ error = AlreadyRegisteredError{}
	_ error = ResolutionError{}
	_ error = TimeoutError{}
	_ error = RegistrationError{}
	_ error = ValidationError{}
	_ error = ModuleError{}
	_ error = TypeMismatchError{}
	_ error = ReflectionAnalysisError{}
	_ error = GraphOperationError{}
	_ error = ConstructorInvocationError{}
	_ error = BuildError{}
	_ error = DisposalError{}
	_ error = CircularDependencyError{}
)

// ========================================
// Typed Errors for Rich Context
// ========================================
// Always use these typed errors instead of fmt.Errorf() or errors.New()
// for domain-specific errors. Wrap sentinel errors with these types.

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
	return fmt.Sprintf("service %s already registered as %s, cannot register as %s", formatType(e.ServiceType), e.Current, e.Requested)
}

// AlreadyRegisteredError indicates a service type is already registered.
type AlreadyRegisteredError struct {
	ServiceType reflect.Type
}

func (e AlreadyRegisteredError) Error() string {
	return fmt.Sprintf("service %s already registered (use keyed services or groups)", formatType(e.ServiceType))
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
	Operation   string // "register", "create-descriptor", "validate-descriptor", etc.
	Cause       error
}

func (e RegistrationError) Error() string {
	return fmt.Sprintf("failed to %s %s: %v", e.Operation, formatType(e.ServiceType), e.Cause)
}

func (e RegistrationError) Unwrap() error {
	return e.Cause
}

// ValidationError indicates a validation failure.
type ValidationError struct {
	ServiceType reflect.Type
	Cause       error
}

func (e ValidationError) Error() string {
	if e.ServiceType != nil {
		return fmt.Sprintf("%s: %v", formatType(e.ServiceType), e.Cause)
	}
	return e.Cause.Error()
}

func (e ValidationError) Unwrap() error {
	return e.Cause
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

// ReflectionAnalysisError for reflection/analysis failures
type ReflectionAnalysisError struct {
	Constructor any
	Operation   string // "analyze", "validate", "invoke"
	Cause       error
}

func (e ReflectionAnalysisError) Error() string {
	return fmt.Sprintf("reflection %s failed for constructor %T: %v", e.Operation, e.Constructor, e.Cause)
}

func (e ReflectionAnalysisError) Unwrap() error {
	return e.Cause
}

// GraphOperationError for dependency graph operations
type GraphOperationError struct {
	Operation string // "add", "remove", "sort"
	NodeType  reflect.Type
	NodeKey   any
	Cause     error
}

func (e GraphOperationError) Error() string {
	if e.NodeKey != nil {
		return fmt.Sprintf("graph %s failed for %s[%v]: %v", e.Operation, formatType(e.NodeType), e.NodeKey, e.Cause)
	}
	return fmt.Sprintf("graph %s failed for %s: %v", e.Operation, formatType(e.NodeType), e.Cause)
}

func (e GraphOperationError) Unwrap() error {
	return e.Cause
}

// ConstructorInvocationError for constructor call failures
type ConstructorInvocationError struct {
	Constructor reflect.Type
	Parameters  []reflect.Type
	Cause       error
}

func (e ConstructorInvocationError) Error() string {
	paramStrs := make([]string, len(e.Parameters))
	for i, p := range e.Parameters {
		paramStrs[i] = formatType(p)
	}
	return fmt.Sprintf("failed to invoke %s with parameters [%s]: %v",
		formatType(e.Constructor), strings.Join(paramStrs, ", "), e.Cause)
}

func (e ConstructorInvocationError) Unwrap() error {
	return e.Cause
}

// BuildError wraps errors that occur during provider building
type BuildError struct {
	Phase   string // "validation", "graph", "singleton-creation", etc.
	Details string
	Cause   error
}

func (e BuildError) Error() string {
	return fmt.Sprintf("build failed during %s phase: %s: %v", e.Phase, e.Details, e.Cause)
}

func (e BuildError) Unwrap() error {
	return e.Cause
}

// DisposalError aggregates disposal errors
type DisposalError struct {
	Context string // "provider", "scope", "singleton"
	Errors  []error
}

func (e DisposalError) Error() string {
	if len(e.Errors) == 1 {
		return fmt.Sprintf("%s disposal failed: %v", e.Context, e.Errors[0])
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("%s disposal failed with %d errors:", e.Context, len(e.Errors)))
	for i, err := range e.Errors {
		sb.WriteString(fmt.Sprintf("\n  %d. %v", i+1, err))
	}
	return sb.String()
}

// formatType formats a reflect.Type for error messages.
func formatType(t reflect.Type) string {
	if t == nil {
		return "<nil>"
	}

	// Handle common cases with cleaner output
	switch t.Kind() {
	case reflect.Pointer:
		// Format pointers as *Type instead of *package.Type
		elem := t.Elem()
		if elem.PkgPath() != "" && elem.Name() != "" {
			// Named type with package
			return "*" + elem.Name()
		}
		return t.String()
	case reflect.Slice:
		// Format slices as []Type
		elem := t.Elem()
		if elem.PkgPath() != "" && elem.Name() != "" {
			// Named type with package
			return "[]" + elem.Name()
		}
		return t.String()
	case reflect.Map:
		// Format maps more concisely
		key := t.Key()
		elem := t.Elem()
		keyStr := key.Name()
		if keyStr == "" {
			keyStr = key.String()
		}
		elemStr := elem.Name()
		if elemStr == "" {
			elemStr = elem.String()
		}
		return "map[" + keyStr + "]" + elemStr
	case reflect.Interface:
		// For interfaces, just use the name if available
		if t.Name() != "" {
			return t.Name()
		}
		return t.String()
	case reflect.Struct:
		// For structs, use the short name if available
		if t.Name() != "" {
			return t.Name()
		}
		return t.String()
	case reflect.Func:
		// For functions, use String() which gives a nice representation
		return t.String()
	default:
		// For basic types and others, prefer the name if available
		if t.Name() != "" {
			return t.Name()
		}
		return t.String()
	}
}
