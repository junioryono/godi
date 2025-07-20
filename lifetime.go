package godi

import (
	"encoding/json"
	"fmt"
)

// ServiceLifetime specifies the lifetime of a service in a ServiceCollection.
// The lifetime determines when instances are created and how they are cached.
// This maps to dig's scoping model while maintaining Microsoft DI semantics.
type ServiceLifetime int

const (
	// Singleton specifies that a single instance of the service will be created.
	// The instance is created on first request and cached for the lifetime of the root provider.
	// Singleton services must not depend on Scoped services.
	// In dig terms, this is a service provided at the root container level.
	Singleton ServiceLifetime = iota

	// Scoped specifies that a new instance of the service will be created for each scope.
	// In web applications, this typically means one instance per HTTP request.
	// Scoped services are disposed when their scope is disposed.
	// In dig terms, this is a service provided at the scope level.
	Scoped
)

// String returns the string representation of the ServiceLifetime.
func (sl ServiceLifetime) String() string {
	switch sl {
	case Singleton:
		return "Singleton"
	case Scoped:
		return "Scoped"
	default:
		return fmt.Sprintf("Unknown(%d)", int(sl))
	}
}

// IsValid checks if the service lifetime is valid.
func (sl ServiceLifetime) IsValid() bool {
	return sl >= Singleton && sl <= Scoped
}

// MarshalText implements encoding.TextMarshaler.
func (sl ServiceLifetime) MarshalText() ([]byte, error) {
	return []byte(sl.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (sl *ServiceLifetime) UnmarshalText(text []byte) error {
	switch string(text) {
	case "Singleton", "singleton":
		*sl = Singleton
	case "Scoped", "scoped":
		*sl = Scoped
	default:
		return &LifetimeError{Value: string(text)}
	}
	return nil
}

// MarshalJSON implements json.Marshaler.
func (sl ServiceLifetime) MarshalJSON() ([]byte, error) {
	return json.Marshal(sl.String())
}

// UnmarshalJSON implements json.Unmarshaler.
func (sl *ServiceLifetime) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	return sl.UnmarshalText([]byte(s))
}
