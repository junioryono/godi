package godi_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/junioryono/godi"
)

// Test types for lifetime tests
type (
	lifetimeTestDisposableWithContext struct {
		disposed     bool
		disposedCtx  context.Context
		disposeDelay time.Duration
		disposeError error
	}

	mockFactory struct{}
)

func (s *lifetimeTestDisposableWithContext) Close(ctx context.Context) error {
	s.disposedCtx = ctx

	if s.disposeDelay > 0 {
		select {
		case <-time.After(s.disposeDelay):
			// Normal disposal
		case <-ctx.Done():
			// Context cancelled
			return ctx.Err()
		}
	}

	s.disposed = true
	return s.disposeError
}

func (m mockFactory) CreateScope(ctx context.Context) godi.Scope {
	return &MockScope{id: "test"}
}

func TestServiceLifetime(t *testing.T) {
	t.Run("constants", func(t *testing.T) {
		// Verify constant values
		if godi.Singleton != 0 {
			t.Errorf("Singleton should be 0, got %d", godi.Singleton)
		}
		if godi.Scoped != 1 {
			t.Errorf("Scoped should be 1, got %d", godi.Scoped)
		}
		if godi.Transient != 2 {
			t.Errorf("Transient should be 2, got %d", godi.Transient)
		}
	})

	t.Run("String", func(t *testing.T) {
		tests := []struct {
			lifetime godi.ServiceLifetime
			expected string
		}{
			{godi.Singleton, "Singleton"},
			{godi.Scoped, "Scoped"},
			{godi.Transient, "Transient"},
			{godi.ServiceLifetime(999), "Unknown(999)"},
		}

		for _, tt := range tests {
			if got := tt.lifetime.String(); got != tt.expected {
				t.Errorf("lifetime %d: expected %q, got %q", tt.lifetime, tt.expected, got)
			}
		}
	})

	t.Run("IsValid", func(t *testing.T) {
		tests := []struct {
			lifetime godi.ServiceLifetime
			valid    bool
		}{
			{godi.Singleton, true},
			{godi.Scoped, true},
			{godi.Transient, true},
			{godi.ServiceLifetime(-1), false},
			{godi.ServiceLifetime(3), false},
			{godi.ServiceLifetime(999), false},
		}

		for _, tt := range tests {
			if got := tt.lifetime.IsValid(); got != tt.valid {
				t.Errorf("lifetime %d: expected IsValid=%v, got %v", tt.lifetime, tt.valid, got)
			}
		}
	})
}

func TestServiceLifetime_Marshaling(t *testing.T) {
	t.Run("MarshalText", func(t *testing.T) {
		tests := []struct {
			lifetime godi.ServiceLifetime
			expected string
		}{
			{godi.Singleton, "Singleton"},
			{godi.Scoped, "Scoped"},
			{godi.Transient, "Transient"},
		}

		for _, tt := range tests {
			data, err := tt.lifetime.MarshalText()
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if string(data) != tt.expected {
				t.Errorf("lifetime %s: expected %q, got %q", tt.lifetime, tt.expected, string(data))
			}
		}
	})

	t.Run("UnmarshalText", func(t *testing.T) {
		tests := []struct {
			text     string
			expected godi.ServiceLifetime
			wantErr  bool
		}{
			{"Singleton", godi.Singleton, false},
			{"singleton", godi.Singleton, false},
			{"Scoped", godi.Scoped, false},
			{"scoped", godi.Scoped, false},
			{"Transient", godi.Transient, false},
			{"transient", godi.Transient, false},
			{"Invalid", godi.ServiceLifetime(0), true},
			{"", godi.ServiceLifetime(0), true},
		}

		for _, tt := range tests {
			var lifetime godi.ServiceLifetime
			err := lifetime.UnmarshalText([]byte(tt.text))

			if tt.wantErr {
				if err == nil {
					t.Errorf("text %q: expected error, got nil", tt.text)
				}
				continue
			}

			if err != nil {
				t.Errorf("text %q: unexpected error: %v", tt.text, err)
			}
			if lifetime != tt.expected {
				t.Errorf("text %q: expected %v, got %v", tt.text, tt.expected, lifetime)
			}
		}
	})

	t.Run("JSON roundtrip", func(t *testing.T) {
		type testStruct struct {
			Lifetime godi.ServiceLifetime `json:"lifetime"`
		}

		for _, lifetime := range []godi.ServiceLifetime{godi.Singleton, godi.Scoped, godi.Transient} {
			original := testStruct{Lifetime: lifetime}

			data, err := json.Marshal(original)
			if err != nil {
				t.Errorf("failed to marshal %v: %v", lifetime, err)
				continue
			}

			var decoded testStruct
			err = json.Unmarshal(data, &decoded)
			if err != nil {
				t.Errorf("failed to unmarshal %v: %v", lifetime, err)
				continue
			}

			if decoded.Lifetime != original.Lifetime {
				t.Errorf("roundtrip failed: expected %v, got %v", original.Lifetime, decoded.Lifetime)
			}
		}
	})
}

// Mock interfaces for testing
type MockScope struct {
	serviceProvider godi.ServiceProvider
	closed          bool
	id              string
}

func (m *MockScope) ID() string {
	return m.id
}

func (m *MockScope) Context() context.Context {
	return context.Background()
}

func (m *MockScope) ServiceProvider() godi.ServiceProvider {
	return m.serviceProvider
}

func (m *MockScope) IsRootScope() bool {
	return m.id == "test"
}

func (m *MockScope) GetRootScope() godi.Scope {
	if m.IsRootScope() {
		return m
	}

	return nil // In a real implementation, this would return the root scope
}

func (m *MockScope) Parent() godi.Scope {
	return nil // No parent for mock scope
}

func (m *MockScope) Close() error {
	m.closed = true
	return nil
}

func (m *MockScope) String() string {
	return "MockScope{id: " + m.id + "}"
}

// Resolve implements godi.Scope.
func (m *MockScope) Resolve(serviceType reflect.Type) (interface{}, error) {
	panic("unimplemented")
}

// ResolveKeyed implements godi.Scope.
func (m *MockScope) ResolveKeyed(serviceType reflect.Type, serviceKey interface{}) (interface{}, error) {
	panic("unimplemented")
}

func TestScopeInterface(t *testing.T) {
	// This test verifies that our mock implements the Scope interface
	var _ godi.Scope = (*MockScope)(nil)
}

func TestDisposableWithContextInterface(t *testing.T) {
	// Verify our test type implements the interface
	var _ godi.DisposableWithContext = (*lifetimeTestDisposableWithContext)(nil)
}

func TestServiceScopeFactoryInterface(t *testing.T) {
	var _ godi.ServiceScopeFactory = mockFactory{}
}
