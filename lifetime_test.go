package godi

import (
	"encoding/json"
	"testing"
)

// TestLifetime_String tests the String method of Lifetime
func TestLifetime_String(t *testing.T) {
	tests := []struct {
		name     string
		lifetime Lifetime
		expected string
	}{
		{
			name:     "Singleton",
			lifetime: Singleton,
			expected: "Singleton",
		},
		{
			name:     "Scoped",
			lifetime: Scoped,
			expected: "Scoped",
		},
		{
			name:     "Transient",
			lifetime: Transient,
			expected: "Transient",
		},
		{
			name:     "Unknown value",
			lifetime: Lifetime(99),
			expected: "Unknown(99)",
		},
		{
			name:     "Negative value",
			lifetime: Lifetime(-1),
			expected: "Unknown(-1)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.lifetime.String(); got != tt.expected {
				t.Errorf("String() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// TestLifetime_IsValid tests the IsValid method of Lifetime
func TestLifetime_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		lifetime Lifetime
		expected bool
	}{
		{
			name:     "Singleton is valid",
			lifetime: Singleton,
			expected: true,
		},
		{
			name:     "Scoped is valid",
			lifetime: Scoped,
			expected: true,
		},
		{
			name:     "Transient is valid",
			lifetime: Transient,
			expected: true,
		},
		{
			name:     "Negative value is invalid",
			lifetime: Lifetime(-1),
			expected: false,
		},
		{
			name:     "Value beyond Transient is invalid",
			lifetime: Lifetime(3),
			expected: false,
		},
		{
			name:     "Large value is invalid",
			lifetime: Lifetime(100),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.lifetime.IsValid(); got != tt.expected {
				t.Errorf("IsValid() = %v, want %v", got, tt.expected)
			}
		})
	}
}

// TestLifetime_MarshalText tests the MarshalText method
func TestLifetime_MarshalText(t *testing.T) {
	tests := []struct {
		name     string
		lifetime Lifetime
		expected string
		wantErr  bool
	}{
		{
			name:     "Marshal Singleton",
			lifetime: Singleton,
			expected: "Singleton",
			wantErr:  false,
		},
		{
			name:     "Marshal Scoped",
			lifetime: Scoped,
			expected: "Scoped",
			wantErr:  false,
		},
		{
			name:     "Marshal Transient",
			lifetime: Transient,
			expected: "Transient",
			wantErr:  false,
		},
		{
			name:     "Marshal unknown value",
			lifetime: Lifetime(99),
			expected: "Unknown(99)",
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.lifetime.MarshalText()
			if (err != nil) != tt.wantErr {
				t.Errorf("MarshalText() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if string(got) != tt.expected {
				t.Errorf("MarshalText() = %q, want %q", string(got), tt.expected)
			}
		})
	}
}

// TestLifetime_UnmarshalText tests the UnmarshalText method
func TestLifetime_UnmarshalText(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected Lifetime
		wantErr  bool
	}{
		{
			name:     "Unmarshal Singleton",
			text:     "Singleton",
			expected: Singleton,
			wantErr:  false,
		},
		{
			name:     "Unmarshal singleton (lowercase)",
			text:     "singleton",
			expected: Singleton,
			wantErr:  false,
		},
		{
			name:     "Unmarshal Scoped",
			text:     "Scoped",
			expected: Scoped,
			wantErr:  false,
		},
		{
			name:     "Unmarshal scoped (lowercase)",
			text:     "scoped",
			expected: Scoped,
			wantErr:  false,
		},
		{
			name:     "Unmarshal Transient",
			text:     "Transient",
			expected: Transient,
			wantErr:  false,
		},
		{
			name:     "Unmarshal transient (lowercase)",
			text:     "transient",
			expected: Transient,
			wantErr:  false,
		},
		{
			name:     "Unmarshal invalid value",
			text:     "Invalid",
			expected: Lifetime(0),
			wantErr:  true,
		},
		{
			name:     "Unmarshal empty string",
			text:     "",
			expected: Lifetime(0),
			wantErr:  true,
		},
		{
			name:     "Unmarshal mixed case (should fail)",
			text:     "SiNgLeTon",
			expected: Lifetime(0),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var lifetime Lifetime
			err := lifetime.UnmarshalText([]byte(tt.text))
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalText() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && lifetime != tt.expected {
				t.Errorf("UnmarshalText() result = %v, want %v", lifetime, tt.expected)
			}
			if tt.wantErr && err != nil {
				// Verify error type
				if _, ok := err.(*LifetimeError); !ok {
					t.Errorf("Expected LifetimeError, got %T", err)
				}
			}
		})
	}
}

// TestLifetime_MarshalJSON tests JSON marshaling
func TestLifetime_MarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		lifetime Lifetime
		expected string
	}{
		{
			name:     "Marshal Singleton to JSON",
			lifetime: Singleton,
			expected: `"Singleton"`,
		},
		{
			name:     "Marshal Scoped to JSON",
			lifetime: Scoped,
			expected: `"Scoped"`,
		},
		{
			name:     "Marshal Transient to JSON",
			lifetime: Transient,
			expected: `"Transient"`,
		},
		{
			name:     "Marshal unknown value to JSON",
			lifetime: Lifetime(99),
			expected: `"Unknown(99)"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.lifetime.MarshalJSON()
			if err != nil {
				t.Errorf("MarshalJSON() error = %v", err)
				return
			}
			if string(got) != tt.expected {
				t.Errorf("MarshalJSON() = %s, want %s", string(got), tt.expected)
			}
		})
	}
}

// TestLifetime_UnmarshalJSON tests JSON unmarshaling
func TestLifetime_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		expected Lifetime
		wantErr  bool
	}{
		{
			name:     "Unmarshal Singleton from JSON",
			json:     `"Singleton"`,
			expected: Singleton,
			wantErr:  false,
		},
		{
			name:     "Unmarshal singleton (lowercase) from JSON",
			json:     `"singleton"`,
			expected: Singleton,
			wantErr:  false,
		},
		{
			name:     "Unmarshal Scoped from JSON",
			json:     `"Scoped"`,
			expected: Scoped,
			wantErr:  false,
		},
		{
			name:     "Unmarshal Transient from JSON",
			json:     `"Transient"`,
			expected: Transient,
			wantErr:  false,
		},
		{
			name:     "Unmarshal invalid value from JSON",
			json:     `"Invalid"`,
			expected: Lifetime(0),
			wantErr:  true,
		},
		{
			name:     "Unmarshal non-string JSON",
			json:     `123`,
			expected: Lifetime(0),
			wantErr:  true,
		},
		{
			name:     "Unmarshal invalid JSON",
			json:     `{invalid}`,
			expected: Lifetime(0),
			wantErr:  true,
		},
		{
			name:     "Unmarshal null JSON",
			json:     `null`,
			expected: Lifetime(0),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var lifetime Lifetime
			err := lifetime.UnmarshalJSON([]byte(tt.json))
			if (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && lifetime != tt.expected {
				t.Errorf("UnmarshalJSON() result = %v, want %v", lifetime, tt.expected)
			}
		})
	}
}

// TestLifetime_JSONRoundTrip tests that JSON marshaling and unmarshaling are consistent
func TestLifetime_JSONRoundTrip(t *testing.T) {
	lifetimes := []Lifetime{Singleton, Scoped, Transient}

	for _, original := range lifetimes {
		t.Run(original.String(), func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(original)
			if err != nil {
				t.Fatalf("Failed to marshal: %v", err)
			}

			// Unmarshal from JSON
			var result Lifetime
			err = json.Unmarshal(data, &result)
			if err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}

			// Check equality
			if result != original {
				t.Errorf("Round trip failed: got %v, want %v", result, original)
			}
		})
	}
}

// TestLifetime_TextRoundTrip tests that text marshaling and unmarshaling are consistent
func TestLifetime_TextRoundTrip(t *testing.T) {
	lifetimes := []Lifetime{Singleton, Scoped, Transient}

	for _, original := range lifetimes {
		t.Run(original.String(), func(t *testing.T) {
			// Marshal to text
			data, err := original.MarshalText()
			if err != nil {
				t.Fatalf("Failed to marshal: %v", err)
			}

			// Unmarshal from text
			var result Lifetime
			err = result.UnmarshalText(data)
			if err != nil {
				t.Fatalf("Failed to unmarshal: %v", err)
			}

			// Check equality
			if result != original {
				t.Errorf("Round trip failed: got %v, want %v", result, original)
			}
		})
	}
}

// TestLifetime_Constants tests that constants have expected values
func TestLifetime_Constants(t *testing.T) {
	// Ensure constants have specific values to maintain compatibility
	if Singleton != 0 {
		t.Errorf("Singleton should be 0, got %d", Singleton)
	}
	if Scoped != 1 {
		t.Errorf("Scoped should be 1, got %d", Scoped)
	}
	if Transient != 2 {
		t.Errorf("Transient should be 2, got %d", Transient)
	}
}