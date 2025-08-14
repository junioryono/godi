package testutil

import (
	"testing"

	"github.com/junioryono/godi/v3"
	"github.com/stretchr/testify/require"
)

// ServiceCollectionBuilder provides a fluent interface for building test service collections
type ServiceCollectionBuilder struct {
	t          *testing.T
	collection godi.ServiceCollection
}

// NewServiceCollectionBuilder creates a new ServiceCollectionBuilder
func NewServiceCollectionBuilder(t *testing.T) *ServiceCollectionBuilder {
	return &ServiceCollectionBuilder{
		t:          t,
		collection: godi.NewServiceCollection(),
	}
}

// WithSingleton adds a singleton service to the collection
func (b *ServiceCollectionBuilder) WithSingleton(constructor interface{}, opts ...godi.ProvideOption) *ServiceCollectionBuilder {
	require.NoError(b.t, b.collection.AddSingleton(constructor, opts...))
	return b
}

// WithScoped adds a scoped service to the collection
func (b *ServiceCollectionBuilder) WithScoped(constructor interface{}, opts ...godi.ProvideOption) *ServiceCollectionBuilder {
	require.NoError(b.t, b.collection.AddScoped(constructor, opts...))
	return b
}

// WithDecorator adds a decorator to the collection
func (b *ServiceCollectionBuilder) WithDecorator(decorator interface{}, opts ...godi.DecorateOption) *ServiceCollectionBuilder {
	require.NoError(b.t, b.collection.Decorate(decorator, opts...))
	return b
}

// WithModule adds a module to the collection
func (b *ServiceCollectionBuilder) WithModule(module godi.ModuleOption) *ServiceCollectionBuilder {
	require.NoError(b.t, b.collection.AddModules(module))
	return b
}

// Build returns the built service collection
func (b *ServiceCollectionBuilder) Build() godi.ServiceCollection {
	return b.collection
}

// BuildProvider builds and returns a ServiceProvider from the collection
func (b *ServiceCollectionBuilder) BuildProvider(opts ...*godi.ServiceProviderOptions) godi.ServiceProvider {
	var providerOpts *godi.ServiceProviderOptions
	if len(opts) > 0 {
		providerOpts = opts[0]
	}

	provider, err := b.collection.BuildServiceProviderWithOptions(providerOpts)
	require.NoError(b.t, err, "failed to build service provider")

	b.t.Cleanup(func() {
		if !provider.IsDisposed() {
			require.NoError(b.t, provider.Close())
		}
	})

	return provider
}

// MustBuildProvider builds a ServiceProvider and fails the test if there's an error
func (b *ServiceCollectionBuilder) MustBuildProvider() godi.ServiceProvider {
	return b.BuildProvider()
}

// ProviderBuilder provides a fluent interface for building test providers with options
type ProviderBuilder struct {
	t          *testing.T
	collection godi.ServiceCollection
	options    *godi.ServiceProviderOptions
}

// NewProviderBuilder creates a new ProviderBuilder
func NewProviderBuilder(t *testing.T) *ProviderBuilder {
	return &ProviderBuilder{
		t:          t,
		collection: godi.NewServiceCollection(),
		options:    &godi.ServiceProviderOptions{},
	}
}

// WithCollection sets the service collection
func (b *ProviderBuilder) WithCollection(collection godi.ServiceCollection) *ProviderBuilder {
	b.collection = collection
	return b
}

// WithValidation enables validation on build
func (b *ProviderBuilder) WithValidation() *ProviderBuilder {
	b.options.ValidateOnBuild = true
	return b
}

// WithRecoverFromPanics enables panic recovery
func (b *ProviderBuilder) WithRecoverFromPanics() *ProviderBuilder {
	b.options.RecoverFromPanics = true
	return b
}

// WithDryRun enables dry run mode
func (b *ProviderBuilder) WithDryRun() *ProviderBuilder {
	b.options.DryRun = true
	return b
}

// WithOptions sets custom provider options
func (b *ProviderBuilder) WithOptions(opts *godi.ServiceProviderOptions) *ProviderBuilder {
	b.options = opts
	return b
}

// Build creates the ServiceProvider
func (b *ProviderBuilder) Build() (godi.ServiceProvider, error) {
	provider, err := b.collection.BuildServiceProviderWithOptions(b.options)
	if err != nil {
		return nil, err
	}

	b.t.Cleanup(func() {
		if !provider.IsDisposed() {
			require.NoError(b.t, provider.Close())
		}
	})

	return provider, nil
}

// MustBuild creates the ServiceProvider and fails the test if there's an error
func (b *ProviderBuilder) MustBuild() godi.ServiceProvider {
	provider, err := b.Build()
	require.NoError(b.t, err, "failed to build service provider")
	return provider
}
