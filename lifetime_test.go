package godi_test

import (
	"context"
	"encoding/json"
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

func TestServiceLifetime(t *testing.T) {
	t.Run("constants", func(t *testing.T) {
		// Verify constant values
		if godi.Singleton != 0 {
			t.Errorf("Singleton should be 0, got %d", godi.Singleton)
		}
		if godi.Scoped != 1 {
			t.Errorf("Scoped should be 1, got %d", godi.Scoped)
		}
	})

	t.Run("String", func(t *testing.T) {
		tests := []struct {
			lifetime godi.ServiceLifetime
			expected string
		}{
			{godi.Singleton, "Singleton"},
			{godi.Scoped, "Scoped"},
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

		for _, lifetime := range []godi.ServiceLifetime{godi.Singleton, godi.Scoped} {
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

func TestDisposableWithContextInterface(t *testing.T) {
	// Verify our test type implements the interface
	var _ godi.DisposableWithContext = (*lifetimeTestDisposableWithContext)(nil)
}
