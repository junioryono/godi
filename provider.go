package godi

import (
	"reflect"
)

// Provider is the main dependency injection container interface
type Provider interface {
	// Close cleans up the provider and all its resources
	Close() error
}

// ProviderOptions configures the behavior of the Provider
type ProviderOptions struct {
	// ValidateGraph validates the dependency graph for cycles
	ValidateGraph bool
	
	// StrictMode enforces stricter validation rules
	StrictMode bool
}

// provider is the concrete implementation of Provider
type provider struct {
	services      map[reflect.Type][]*Descriptor
	keyedServices map[TypeKey][]*Descriptor
	groups        map[GroupKey][]*Descriptor
	decorators    map[reflect.Type][]*Descriptor
}

// Close cleans up the provider
func (p *provider) Close() error {
	// Cleanup logic will be implemented later
	return nil
}
