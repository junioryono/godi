package godi

import (
	"encoding/json"
	"fmt"
)

// Lifetime specifies the lifetime of a service in a Collection.
// The lifetime determines when instances are created and how they are cached.
type Lifetime int

const (
	// Singleton specifies that a single instance of the service will be created.
	// The instance is created on first request and cached for the lifetime of the root provider.
	// Singleton services must not depend on Scoped services.
	Singleton Lifetime = iota

	// Scoped specifies that a new instance of the service will be created for each scope.
	// In web applications, this typically means one instance per HTTP request.
	// Scoped services are disposed when their scope is disposed.
	Scoped

	// Transient specifies that a new instance of the service will be created every time it is requested.
	// Transient services are never cached and always create new instances.
	Transient
)

// String returns the string representation of the ServiceLifetime.
func (sl Lifetime) String() string {
	switch sl {
	case Singleton:
		return "Singleton"
	case Scoped:
		return "Scoped"
	case Transient:
		return "Transient"
	default:
		return fmt.Sprintf("Unknown(%d)", int(sl))
	}
}

// IsValid checks if the service lifetime is valid.
// Returns true if the lifetime is Singleton, Scoped, or Transient.
func (sl Lifetime) IsValid() bool {
	return sl >= Singleton && sl <= Transient
}

// MarshalText implements encoding.TextMarshaler interface.
// Converts the lifetime to its string representation for text-based serialization.
func (sl Lifetime) MarshalText() ([]byte, error) {
	return []byte(sl.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler interface.
// Parses a string representation back into a Lifetime value.
func (sl *Lifetime) UnmarshalText(text []byte) error {
	switch string(text) {
	case "Singleton", "singleton":
		*sl = Singleton
	case "Scoped", "scoped":
		*sl = Scoped
	case "Transient", "transient":
		*sl = Transient
	default:
		return &LifetimeError{Value: string(text)}
	}
	return nil
}

// MarshalJSON implements json.Marshaler interface.
// Serializes the lifetime as a JSON string.
func (sl Lifetime) MarshalJSON() ([]byte, error) {
	return json.Marshal(sl.String())
}

// UnmarshalJSON implements json.Unmarshaler interface.
// Deserializes a JSON string back into a Lifetime value.
func (sl *Lifetime) UnmarshalJSON(data []byte) error {
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}

	return sl.UnmarshalText([]byte(s))
}
