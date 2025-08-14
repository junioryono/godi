package godi

var (
	// defaultProvider holds the default ServiceProvider.
	defaultProvider ServiceProvider
)

// SetDefaultServiceProvider sets the default ServiceProvider used by the package-level functions.
// This is similar to slog.SetDefault.
//
// After this call, package-level functions like Resolve, Invoke, etc. will use
// this provider. Pass nil to remove the default provider.
func SetDefaultServiceProvider(provider ServiceProvider) {
	defaultProvider = provider
}

// DefaultServiceProvider returns the current default ServiceProvider.
// Returns nil if no default provider has been set.
func DefaultServiceProvider() ServiceProvider {
	return defaultProvider
}
