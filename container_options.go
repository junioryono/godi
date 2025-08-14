package godi

import (
	"errors"
	"fmt"
	"reflect"
)

// Common errors
var (
	// Container errors
	ErrContainerDisposed = errors.New("container has been disposed")
	ErrContainerBuilt    = errors.New("container has already been built")
	ErrContainerNotBuilt = errors.New("container has not been built")

	// Scope errors
	ErrScopeDisposed = errors.New("scope has been disposed")
	ErrNilScope      = errors.New("scope cannot be nil")

	// Service errors
	ErrServiceNotFound    = errors.New("service not found")
	ErrInvalidServiceType = errors.New("invalid service type")
	ErrNilInstance        = errors.New("instance cannot be nil")
	ErrNilServiceKey      = errors.New("service key cannot be nil")
	ErrEmptyGroup         = errors.New("group name cannot be empty")

	// Function errors
	ErrNilFunction     = errors.New("function cannot be nil")
	ErrInvalidFunction = errors.New("invalid function")

	// Registration errors
	ErrAlreadyRegistered  = errors.New("service already registered")
	ErrLifetimeConflict   = errors.New("service lifetime conflict")
	ErrCircularDependency = errors.New("circular dependency detected")
)

// ProvideOption configures service registration.
type ProvideOption interface {
	apply(*provideOptions)
}

// provideOptions holds provide configuration.
type provideOptions struct {
	name  string
	group string
	as    []reflect.Type
}

// provideOptionFunc adapts a function to ProvideOption.
type provideOptionFunc func(*provideOptions)

func (f provideOptionFunc) apply(opts *provideOptions) {
	f(opts)
}

// WithName provides a service with a specific name/key.
func WithName(name string) ProvideOption {
	return provideOptionFunc(func(opts *provideOptions) {
		opts.name = name
	})
}

// InGroup adds the service to a group.
func InGroup(group string) ProvideOption {
	return provideOptionFunc(func(opts *provideOptions) {
		opts.group = group
	})
}

// AsType explicitly sets the service type.
func AsType(types ...reflect.Type) ProvideOption {
	return provideOptionFunc(func(opts *provideOptions) {
		opts.as = types
	})
}

// DecorateOption configures decorator registration.
type DecorateOption interface {
	apply(*decorateOptions)
}

// decorateOptions holds decorator configuration.
type decorateOptions struct {
	key any
}

// decorateOptionFunc adapts a function to DecorateOption.
type decorateOptionFunc func(*decorateOptions)

func (f decorateOptionFunc) apply(opts *decorateOptions) {
	f(opts)
}

// applyProvideOption applies a ProvideOption to a provider builder.
func applyProvideOption(builder *registry.ProviderBuilder, opt ProvideOption) *registry.ProviderBuilder {
	opts := &provideOptions{}
	opt.apply(opts)

	if opts.name != "" {
		builder = builder.WithKey(opts.name)
	}

	if opts.group != "" {
		builder = builder.InGroup(opts.group)
	}

	if len(opts.as) > 0 && opts.as[0] != nil {
		builder = builder.WithType(opts.as[0])
	}

	return builder
}

// applyDecorateOption applies a DecorateOption to a decorator builder.
func applyDecorateOption(builder *registry.DecoratorBuilder, opt DecorateOption) *registry.DecoratorBuilder {
	opts := &decorateOptions{}
	opt.apply(opts)

	if opts.key != nil {
		builder = builder.WithKey(opts.key)
	}

	return builder
}

// CircularDependencyError represents a circular dependency.
type CircularDependencyError struct {
	ServiceType reflect.Type
	Key         any
	Chain       []reflect.Type
}

// Error implements the error interface.
func (e *CircularDependencyError) Error() string {
	if e.Key != nil {
		return fmt.Sprintf("circular dependency detected for %v[%v]", e.ServiceType, e.Key)
	}
	return fmt.Sprintf("circular dependency detected for %v", e.ServiceType)
}

// ResolutionError represents a service resolution error.
type ResolutionError struct {
	ServiceType reflect.Type
	Key         any
	Cause       error
}

// Error implements the error interface.
func (e *ResolutionError) Error() string {
	if e.Key != nil {
		return fmt.Sprintf("failed to resolve %v[%v]: %v", e.ServiceType, e.Key, e.Cause)
	}
	return fmt.Sprintf("failed to resolve %v: %v", e.ServiceType, e.Cause)
}

// Unwrap returns the underlying error.
func (e *ResolutionError) Unwrap() error {
	return e.Cause
}
