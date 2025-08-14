package godi

import (
	"errors"
	"fmt"
	"reflect"

	"github.com/junioryono/godi/v3/internal/registry"
)

// Resolve is a generic helper function that resolves a service as type T.
func Resolve[T any](provider ScopeProvider) (T, error) {
	var zero T

	serviceType := reflect.TypeOf((*T)(nil)).Elem()

	instance, err := provider.Resolve(serviceType)
	if err != nil {
		return zero, err
	}

	result, ok := instance.(T)
	if !ok {
		return zero, fmt.Errorf("type assertion failed: expected %T, got %T", zero, instance)
	}

	return result, nil
}

// ResolveKeyed is a generic helper function that resolves a keyed service as type T.
func ResolveKeyed[T any](provider ScopeProvider, key any) (T, error) {
	var zero T

	serviceType := reflect.TypeOf((*T)(nil)).Elem()

	instance, err := provider.ResolveKeyed(serviceType, key)
	if err != nil {
		return zero, err
	}

	result, ok := instance.(T)
	if !ok {
		return zero, fmt.Errorf("type assertion failed: expected %T, got %T", zero, instance)
	}

	return result, nil
}

// ResolveGroup is a generic helper function that resolves a group of services as []T.
func ResolveGroup[T any](provider ScopeProvider, group string) ([]T, error) {
	serviceType := reflect.TypeOf((*T)(nil)).Elem()

	instances, err := provider.ResolveGroup(serviceType, group)
	if err != nil {
		return nil, err
	}

	results := make([]T, 0, len(instances))
	for i, instance := range instances {
		result, ok := instance.(T)
		if !ok {
			return nil, fmt.Errorf("type assertion failed for item %d: expected %T, got %T",
				i, *new(T), instance)
		}
		results = append(results, result)
	}

	return results, nil
}

// MustResolve resolves a service and panics on error.
func MustResolve[T any](provider ScopeProvider) T {
	result, err := Resolve[T](provider)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve %T: %v", *new(T), err))
	}
	return result
}

// MustResolveKeyed resolves a keyed service and panics on error.
func MustResolveKeyed[T any](provider ScopeProvider, key any) T {
	result, err := ResolveKeyed[T](provider, key)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve %T[%v]: %v", *new(T), key, err))
	}
	return result
}

// MustResolveGroup resolves a group of services and panics on error.
func MustResolveGroup[T any](provider ScopeProvider, group string) []T {
	results, err := ResolveGroup[T](provider, group)
	if err != nil {
		panic(fmt.Sprintf("failed to resolve group %q of %T: %v", group, *new(T), err))
	}
	return results
}

// IsRegistered checks if a service type is registered.
func IsRegistered[T any](container *Container) bool {
	serviceType := reflect.TypeOf((*T)(nil)).Elem()
	return container.registry.HasProvider(serviceType)
}

// IsKeyedRegistered checks if a keyed service type is registered.
func IsKeyedRegistered[T any](container *Container, key any) bool {
	serviceType := reflect.TypeOf((*T)(nil)).Elem()
	return container.registry.HasKeyedProvider(serviceType, key)
}

// ContainerBuilder provides a fluent API for building a container.
type ContainerBuilder struct {
	container *Container
	err       error
}

// NewBuilder creates a new container builder.
func NewBuilder() *ContainerBuilder {
	return &ContainerBuilder{
		container: NewContainer(),
	}
}

// NewBuilderWithOptions creates a new container builder with options.
func NewBuilderWithOptions(options *ContainerOptions) *ContainerBuilder {
	return &ContainerBuilder{
		container: NewContainerWithOptions(options),
	}
}

// WithOptions sets container options.
func (b *ContainerBuilder) WithOptions(options *ContainerOptions) *ContainerBuilder {
	if b.err != nil {
		return b
	}

	b.container.options = options
	return b
}

// RegisterSingleton registers a singleton service.
func (b *ContainerBuilder) RegisterSingleton(constructor any, opts ...ProvideOption) *ContainerBuilder {
	if b.err != nil {
		return b
	}

	if err := b.container.RegisterSingleton(constructor, opts...); err != nil {
		b.err = err
	}

	return b
}

// RegisterScoped registers a scoped service.
func (b *ContainerBuilder) RegisterScoped(constructor any, opts ...ProvideOption) *ContainerBuilder {
	if b.err != nil {
		return b
	}

	if err := b.container.RegisterScoped(constructor, opts...); err != nil {
		b.err = err
	}

	return b
}

// RegisterTransient registers a transient service.
func (b *ContainerBuilder) RegisterTransient(constructor any, opts ...ProvideOption) *ContainerBuilder {
	if b.err != nil {
		return b
	}

	if err := b.container.RegisterTransient(constructor, opts...); err != nil {
		b.err = err
	}

	return b
}

// RegisterDecorator registers a decorator.
func (b *ContainerBuilder) RegisterDecorator(decorator any, opts ...DecorateOption) *ContainerBuilder {
	if b.err != nil {
		return b
	}

	if err := b.container.RegisterDecorator(decorator, opts...); err != nil {
		b.err = err
	}

	return b
}

// RegisterModule registers services from a module.
func (b *ContainerBuilder) RegisterModule(module Module) *ContainerBuilder {
	if b.err != nil {
		return b
	}

	if err := module.Register(b.container); err != nil {
		b.err = err
	}

	return b
}

// Build finalizes the container configuration.
func (b *ContainerBuilder) Build() (*Container, error) {
	if b.err != nil {
		return nil, b.err
	}

	if err := b.container.Build(); err != nil {
		return nil, err
	}

	return b.container, nil
}

// Module represents a group of related service registrations.
type Module interface {
	Register(container *Container) error
}

// ModuleFunc adapts a function to the Module interface.
type ModuleFunc func(container *Container) error

// Register implements the Module interface.
func (f ModuleFunc) Register(container *Container) error {
	return f(container)
}

// CreateModule creates a module from registration functions.
func CreateModule(name string, register func(container *Container) error) Module {
	return ModuleFunc(func(container *Container) error {
		if err := register(container); err != nil {
			return fmt.Errorf("module %s: %w", name, err)
		}
		return nil
	})
}

// ServiceDescriptor describes a service registration.
type ServiceDescriptor struct {
	ServiceType reflect.Type
	Lifetime    registry.ServiceLifetime
	Constructor any
	Key         any
	Group       string
}

// RegisterServices registers multiple services from descriptors.
func RegisterServices(container *Container, descriptors ...ServiceDescriptor) error {
	for _, desc := range descriptors {
		opts := make([]ProvideOption, 0)

		if desc.Key != nil {
			opts = append(opts, WithName(fmt.Sprintf("%v", desc.Key)))
		}

		if desc.Group != "" {
			opts = append(opts, InGroup(desc.Group))
		}

		if desc.ServiceType != nil {
			opts = append(opts, AsType(desc.ServiceType))
		}

		if err := container.Register(desc.Lifetime, desc.Constructor, opts...); err != nil {
			return fmt.Errorf("failed to register %v: %w", desc.ServiceType, err)
		}
	}

	return nil
}

// Helper to check specific error types
func IsCircularDependencyError(err error) bool {
	var circErr *CircularDependencyError
	return errors.As(err, &circErr)
}
