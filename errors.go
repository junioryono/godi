package godi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/junioryono/godi/v5/internal/graph"
	"github.com/junioryono/godi/v5/internal/reflection"
)

// ========================================
// Core Error Values (Sentinel Errors)
// ========================================
// These are base errors that should be wrapped in typed errors when returned.
// Never return these directly to users - always wrap them with context.

var (
	// Service resolution errors.
	// ErrServiceNotFound is shared with the internal reflection package so
	// the parameter builder can distinguish "not registered" from
	// "registered but failed to construct" for optional dependencies.
	ErrServiceNotFound = reflection.ErrServiceNotFound
	ErrServiceKeyNil   = errors.New("service key cannot be nil")
	ErrServiceTypeNil  = errors.New("service type cannot be nil")

	// Lifecycle errors.
	ErrProviderNil      = errors.New("service provider cannot be nil")
	ErrProviderDisposed = errors.New("service provider has been disposed")
	ErrScopeDisposed    = errors.New("scope has been disposed")

	// ErrScopeRequired is the cause reported when ProviderOptions.ValidateScopes
	// is set and a scoped service is resolved from the provider's root scope
	// rather than from a scope created with CreateScope.
	ErrScopeRequired = errors.New("scoped service resolved from the root provider; resolve it from a scope created with CreateScope")

	// Validation errors.
	ErrConstructorNil          = errors.New("constructor cannot be nil")
	ErrGroupNameEmpty          = errors.New("group name cannot be empty")
	ErrSingletonNotInitialized = errors.New("singleton not initialized at build time")
	ErrDescriptorNil           = errors.New("descriptor cannot be nil")
)

// All typed errors are returned as pointers. Match them with
// errors.AsType (Go 1.26+) or errors.As using a pointer target:
//
//	if resErr, ok := errors.AsType[*godi.ResolutionError](err); ok {
//	    log.Printf("failed to resolve %s", resErr.ServiceType)
//	}
var (
	_ error = (*LifetimeError)(nil)
	_ error = (*LifetimeConflictError)(nil)
	_ error = (*AlreadyRegisteredError)(nil)
	_ error = (*ResolutionError)(nil)
	_ error = (*TimeoutError)(nil)
	_ error = (*RegistrationError)(nil)
	_ error = (*ValidationError)(nil)
	_ error = (*ModuleError)(nil)
	_ error = (*TypeMismatchError)(nil)
	_ error = (*ReflectionAnalysisError)(nil)
	_ error = (*GraphOperationError)(nil)
	_ error = (*ConstructorInvocationError)(nil)
	_ error = (*ConstructorPanicError)(nil)
	_ error = (*BuildError)(nil)
	_ error = (*DisposalError)(nil)
	_ error = (*CircularDependencyError)(nil)
	_ error = (*MissingDependencyError)(nil)
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

// LifetimeConflictError indicates a service has an invalid dependency due to lifetime constraints.
// For example, a Singleton service cannot depend on a Scoped service.
type LifetimeConflictError struct {
	ServiceType        reflect.Type
	ServiceLifetime    Lifetime
	DependencyType     reflect.Type
	DependencyLifetime Lifetime
	// Via lists the transient services through which ServiceType reaches
	// DependencyType, in dependency order; empty for a direct dependency.
	Via []reflect.Type
}

func (e LifetimeConflictError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "lifetime conflict: %s (%s) cannot depend on %s (%s)",
		formatType(e.ServiceType), e.ServiceLifetime,
		formatType(e.DependencyType), e.DependencyLifetime)
	if len(e.Via) > 0 {
		via := make([]string, len(e.Via))
		for i, t := range e.Via {
			via[i] = formatType(t)
		}
		fmt.Fprintf(&b, " (via %s)", strings.Join(via, " -> "))
	}
	return b.String()
}

// Detail explains the conflict and how to resolve it; see Explain.
func (e LifetimeConflictError) Detail() string {
	var b strings.Builder
	if e.ServiceLifetime == Singleton {
		b.WriteString("Singleton services are created once and live for the application lifetime.\n")
		b.WriteString("Scoped services are created per-scope and may have different values in different scopes.\n")
		b.WriteString("A singleton depending on a scoped service would capture a single scope's value,\n")
		b.WriteString("which is almost certainly not what you want.\n\n")
	}
	b.WriteString("To resolve this:\n")
	fmt.Fprintf(&b, "  • Change %s to Scoped lifetime\n", formatType(e.ServiceType))
	fmt.Fprintf(&b, "  • Change %s to Singleton lifetime\n", formatType(e.DependencyType))
	fmt.Fprintf(&b, "  • Pass %s to %s's methods per call instead of holding it",
		formatType(e.DependencyType), formatType(e.ServiceType))
	return b.String()
}

func (e LifetimeConflictError) Format(s fmt.State, verb rune) { formatError(s, verb, e) }

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
	Available   []reflect.Type // Types that ARE registered (optional, for suggestions)
}

func (e ResolutionError) Error() string {
	var b strings.Builder

	// Only a missing registration is "not found"; a registered service whose
	// construction failed is reported as a resolution failure.
	notFound := e.Cause == nil || e.ServiceNotFound()
	if notFound {
		b.WriteString("service not found: ")
	} else {
		b.WriteString("failed to resolve ")
	}
	b.WriteString(formatType(e.ServiceType))
	if e.ServiceKey != nil {
		fmt.Fprintf(&b, " (key: %v)", e.ServiceKey)
	}

	if e.Cause != nil && e.Cause != ErrServiceNotFound {
		fmt.Fprintf(&b, ": %v", e.Cause)
	}

	return b.String()
}

// Detail suggests similar registered types for a missing service; see
// Explain.
func (e ResolutionError) Detail() string {
	if e.Cause != nil && !e.ServiceNotFound() {
		return ""
	}
	var b strings.Builder
	if similar := findSimilarTypes(e.ServiceType, e.Available); len(similar) > 0 {
		b.WriteString("Did you mean one of these?\n")
		for _, t := range similar {
			fmt.Fprintf(&b, "  • %s\n", formatType(t))
		}
		b.WriteString("\n")
	}
	b.WriteString("Make sure the service is registered with the correct lifetime and type.")
	return b.String()
}

func (e ResolutionError) Format(s fmt.State, verb rune) { formatError(s, verb, e) }

func (e ResolutionError) Unwrap() error {
	return e.Cause
}

// ServiceNotFound reports whether this error represents a direct "no provider
// registered" failure (as opposed to a registered provider whose construction
// failed). Used by the parameter builder to decide whether an optional
// dependency may be skipped.
func (e ResolutionError) ServiceNotFound() bool {
	return e.Cause == ErrServiceNotFound || e.Cause == errOutputNotProvided
}

// errOutputNotProvided is the cause reported for a result-object (godi.Out)
// field the constructor left nil: the service is registered, but this
// construction did not provide it. It counts as "not found", so optional
// dependencies receive their zero value and groups skip the member.
var errOutputNotProvided error = outputNotProvidedError{}

type outputNotProvidedError struct{}

func (outputNotProvidedError) Error() string {
	return "the constructor left this result object field nil"
}

func (outputNotProvidedError) Unwrap() error { return ErrServiceNotFound }

// isOutputNotProvided reports whether err is a direct "result object field
// was nil" resolution failure.
func isOutputNotProvided(err error) bool {
	resErr, ok := err.(*ResolutionError)
	return ok && resErr.Cause == errOutputNotProvided
}

// MissingDependencyError reports, at Build time, a constructor parameter whose
// type (and key) has no registration.
type MissingDependencyError struct {
	ServiceType    reflect.Type
	DependencyType reflect.Type
	DependencyKey  any // nil for non-keyed dependencies
	// Constructor names the constructor and its source location.
	Constructor string
}

func (e MissingDependencyError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s requires %s", formatType(e.ServiceType), formatType(e.DependencyType))
	if e.DependencyKey != nil {
		fmt.Fprintf(&b, " (key: %v)", e.DependencyKey)
	}
	b.WriteString(" (not registered)")
	if e.Constructor != "" {
		fmt.Fprintf(&b, " [constructor %s]", e.Constructor)
	}
	return b.String()
}

// Unwrap lets errors.Is(err, ErrServiceNotFound) match.
func (e MissingDependencyError) Unwrap() error {
	return ErrServiceNotFound
}

// findSimilarTypes finds types with similar names using a simple substring/prefix match
func findSimilarTypes(target reflect.Type, available []reflect.Type) []reflect.Type {
	if target == nil || len(available) == 0 {
		return nil
	}

	targetName := target.String()
	targetShortName := target.Name()
	if targetShortName == "" {
		targetShortName = targetName
	}

	var similar []reflect.Type
	for _, t := range available {
		if t == nil || t == target {
			continue
		}

		typeName := t.String()
		typeShortName := t.Name()
		if typeShortName == "" {
			typeShortName = typeName
		}

		// Check for name similarity:
		// - Same short name (different packages)
		// - One contains the other
		// - Similar length and many common characters
		if targetShortName == typeShortName ||
			strings.Contains(strings.ToLower(typeName), strings.ToLower(targetShortName)) ||
			strings.Contains(strings.ToLower(targetName), strings.ToLower(typeShortName)) {
			similar = append(similar, t)
		}

		// Limit suggestions
		if len(similar) >= 5 {
			break
		}
	}

	return similar
}

// TimeoutError indicates a service resolution timed out.
//
// Deprecated: godi never returns TimeoutError; build timeouts are reported as
// a BuildError matching context.DeadlineExceeded. It will be removed in the
// next major version.
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
	// Location names the constructor function and its source location
	// ("users.NewService (service.go:42)"), when known.
	Location string
}

func (e ConstructorInvocationError) Error() string {
	if e.Location != "" {
		return fmt.Sprintf("constructor %s failed: %v", e.Location, e.Cause)
	}
	paramStrs := make([]string, len(e.Parameters))
	for i, p := range e.Parameters {
		paramStrs[i] = formatType(p)
	}
	return fmt.Sprintf("failed to invoke %s with parameters [%s]: %v",
		formatType(e.Constructor), strings.Join(paramStrs, ", "), e.Cause)
}

func (e ConstructorInvocationError) Format(s fmt.State, verb rune) { formatError(s, verb, e) }

func (e ConstructorInvocationError) Unwrap() error {
	return e.Cause
}

// ConstructorPanicError indicates a constructor panicked during invocation.
// It captures the panic value and stack trace for debugging; they are part
// of Detail (Explain, %+v), not of Error.
type ConstructorPanicError struct {
	Constructor reflect.Type
	Panic       any
	Stack       []byte
	// Location names the constructor function and its source location,
	// when known.
	Location string
}

func (e ConstructorPanicError) Error() string {
	name := e.Location
	if name == "" {
		name = formatType(e.Constructor)
	}
	return strings.ReplaceAll(fmt.Sprintf("constructor %s panicked: %v", name, e.Panic), "\n", " ")
}

// Detail gives guidance and the panic's stack trace; see Explain.
func (e ConstructorPanicError) Detail() string {
	var b strings.Builder
	b.WriteString("Constructors should be pure dependency wiring - avoid operations that can panic.\n")
	b.WriteString("Critical operations that can fail belong in application initialization, not constructors.\n\n")
	b.WriteString("To resolve this:\n")
	b.WriteString("  • Check for nil pointer dereferences in your constructor\n")
	b.WriteString("  • Move panic-prone initialization to a separate Init() method\n")
	b.WriteString("  • Add nil checks for dependencies before using them")
	if len(e.Stack) > 0 {
		b.WriteString("\n\nStack trace:\n")
		b.Write(e.Stack)
	}
	return b.String()
}

func (e ConstructorPanicError) Format(s fmt.State, verb rune) { formatError(s, verb, e) }

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
	fmt.Fprintf(&sb, "%s disposal failed with %d errors:", e.Context, len(e.Errors))
	for i, err := range e.Errors {
		fmt.Fprintf(&sb, "\n  %d. %v", i+1, err)
	}
	return sb.String()
}

// Unwrap exposes every cleanup failure to errors.Is and errors.As.
func (e DisposalError) Unwrap() []error {
	return e.Errors
}

// fmt.Formatter: %+v prints Explain (message plus detail), other verbs the
// one-line message.

func (e BuildError) Format(s fmt.State, verb rune)              { formatError(s, verb, e) }
func (e DisposalError) Format(s fmt.State, verb rune)           { formatError(s, verb, e) }
func (e RegistrationError) Format(s fmt.State, verb rune)       { formatError(s, verb, e) }
func (e ValidationError) Format(s fmt.State, verb rune)         { formatError(s, verb, e) }
func (e ModuleError) Format(s fmt.State, verb rune)             { formatError(s, verb, e) }
func (e MissingDependencyError) Format(s fmt.State, verb rune)  { formatError(s, verb, e) }
func (e GraphOperationError) Format(s fmt.State, verb rune)     { formatError(s, verb, e) }
func (e ReflectionAnalysisError) Format(s fmt.State, verb rune) { formatError(s, verb, e) }
