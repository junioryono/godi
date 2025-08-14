package resolver

import (
	"fmt"
	"reflect"
	"strings"
)

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
