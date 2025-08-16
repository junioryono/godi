package godi

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLifetimeString(t *testing.T) {
	tests := []struct {
		name     string
		lifetime Lifetime
		expected string
	}{
		{
			name:     "singleton",
			lifetime: Singleton,
			expected: "Singleton",
		},
		{
			name:     "scoped",
			lifetime: Scoped,
			expected: "Scoped",
		},
		{
			name:     "transient",
			lifetime: Transient,
			expected: "Transient",
		},
		{
			name:     "unknown",
			lifetime: Lifetime(999),
			expected: "Unknown(999)",
		},
		{
			name:     "negative unknown",
			lifetime: Lifetime(-1),
			expected: "Unknown(-1)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.lifetime.String()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLifetimeIsValid(t *testing.T) {
	tests := []struct {
		name     string
		lifetime Lifetime
		valid    bool
	}{
		{
			name:     "singleton is valid",
			lifetime: Singleton,
			valid:    true,
		},
		{
			name:     "scoped is valid",
			lifetime: Scoped,
			valid:    true,
		},
		{
			name:     "transient is valid",
			lifetime: Transient,
			valid:    true,
		},
		{
			name:     "negative is invalid",
			lifetime: Lifetime(-1),
			valid:    false,
		},
		{
			name:     "too large is invalid",
			lifetime: Lifetime(3),
			valid:    false,
		},
		{
			name:     "very large is invalid",
			lifetime: Lifetime(999),
			valid:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.lifetime.IsValid()
			assert.Equal(t, tt.valid, result)
		})
	}
}

func TestLifetimeMarshalText(t *testing.T) {
	tests := []struct {
		name     string
		lifetime Lifetime
		expected string
	}{
		{
			name:     "marshal singleton",
			lifetime: Singleton,
			expected: "Singleton",
		},
		{
			name:     "marshal scoped",
			lifetime: Scoped,
			expected: "Scoped",
		},
		{
			name:     "marshal transient",
			lifetime: Transient,
			expected: "Transient",
		},
		{
			name:     "marshal unknown",
			lifetime: Lifetime(999),
			expected: "Unknown(999)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.lifetime.MarshalText()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(result))
		})
	}
}

func TestLifetimeUnmarshalText(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Lifetime
		wantErr  bool
	}{
		{
			name:     "unmarshal Singleton",
			input:    "Singleton",
			expected: Singleton,
			wantErr:  false,
		},
		{
			name:     "unmarshal singleton lowercase",
			input:    "singleton",
			expected: Singleton,
			wantErr:  false,
		},
		{
			name:     "unmarshal Scoped",
			input:    "Scoped",
			expected: Scoped,
			wantErr:  false,
		},
		{
			name:     "unmarshal scoped lowercase",
			input:    "scoped",
			expected: Scoped,
			wantErr:  false,
		},
		{
			name:     "unmarshal Transient",
			input:    "Transient",
			expected: Transient,
			wantErr:  false,
		},
		{
			name:     "unmarshal transient lowercase",
			input:    "transient",
			expected: Transient,
			wantErr:  false,
		},
		{
			name:     "unmarshal invalid",
			input:    "Invalid",
			expected: Lifetime(0),
			wantErr:  true,
		},
		{
			name:     "unmarshal empty",
			input:    "",
			expected: Lifetime(0),
			wantErr:  true,
		},
		{
			name:     "unmarshal random text",
			input:    "random",
			expected: Lifetime(0),
			wantErr:  true,
		},
		{
			name:     "unmarshal with spaces",
			input:    " Singleton ",
			expected: Lifetime(0),
			wantErr:  true,
		},
		{
			name:     "unmarshal mixed case",
			input:    "SiNgLeToN",
			expected: Lifetime(0),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var lifetime Lifetime
			err := lifetime.UnmarshalText([]byte(tt.input))

			if tt.wantErr {
				assert.Error(t, err)
				var lifetimeErr *LifetimeError
				assert.IsType(t, lifetimeErr, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, lifetime)
			}
		})
	}
}

func TestLifetimeMarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		lifetime Lifetime
		expected string
	}{
		{
			name:     "marshal singleton JSON",
			lifetime: Singleton,
			expected: `"Singleton"`,
		},
		{
			name:     "marshal scoped JSON",
			lifetime: Scoped,
			expected: `"Scoped"`,
		},
		{
			name:     "marshal transient JSON",
			lifetime: Transient,
			expected: `"Transient"`,
		},
		{
			name:     "marshal unknown JSON",
			lifetime: Lifetime(999),
			expected: `"Unknown(999)"`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := tt.lifetime.MarshalJSON()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(result))
		})
	}
}

func TestLifetimeUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected Lifetime
		wantErr  bool
	}{
		{
			name:     "unmarshal Singleton JSON",
			input:    `"Singleton"`,
			expected: Singleton,
			wantErr:  false,
		},
		{
			name:     "unmarshal singleton lowercase JSON",
			input:    `"singleton"`,
			expected: Singleton,
			wantErr:  false,
		},
		{
			name:     "unmarshal Scoped JSON",
			input:    `"Scoped"`,
			expected: Scoped,
			wantErr:  false,
		},
		{
			name:     "unmarshal scoped lowercase JSON",
			input:    `"scoped"`,
			expected: Scoped,
			wantErr:  false,
		},
		{
			name:     "unmarshal Transient JSON",
			input:    `"Transient"`,
			expected: Transient,
			wantErr:  false,
		},
		{
			name:     "unmarshal transient lowercase JSON",
			input:    `"transient"`,
			expected: Transient,
			wantErr:  false,
		},
		{
			name:     "unmarshal invalid JSON",
			input:    `"Invalid"`,
			expected: Lifetime(0),
			wantErr:  true,
		},
		{
			name:     "unmarshal empty JSON",
			input:    `""`,
			expected: Lifetime(0),
			wantErr:  true,
		},
		{
			name:     "unmarshal null JSON",
			input:    `null`,
			expected: Lifetime(0),
			wantErr:  true,
		},
		{
			name:     "unmarshal number JSON",
			input:    `0`,
			expected: Lifetime(0),
			wantErr:  true,
		},
		{
			name:     "unmarshal invalid JSON format",
			input:    `Singleton`,
			expected: Lifetime(0),
			wantErr:  true,
		},
		{
			name:     "unmarshal array JSON",
			input:    `["Singleton"]`,
			expected: Lifetime(0),
			wantErr:  true,
		},
		{
			name:     "unmarshal object JSON",
			input:    `{"lifetime": "Singleton"}`,
			expected: Lifetime(0),
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var lifetime Lifetime
			err := lifetime.UnmarshalJSON([]byte(tt.input))

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, lifetime)
			}
		})
	}
}

func TestLifetimeJSONRoundTrip(t *testing.T) {
	lifetimes := []Lifetime{
		Singleton,
		Scoped,
		Transient,
	}

	for _, original := range lifetimes {
		t.Run(original.String(), func(t *testing.T) {
			// Marshal to JSON
			data, err := json.Marshal(original)
			require.NoError(t, err)

			// Unmarshal back
			var result Lifetime
			err = json.Unmarshal(data, &result)
			require.NoError(t, err)

			// Should be equal
			assert.Equal(t, original, result)
		})
	}
}

func TestLifetimeInStruct(t *testing.T) {
	type Config struct {
		ServiceLifetime Lifetime `json:"lifetime"`
		Name            string   `json:"name"`
	}

	t.Run("marshal struct with lifetime", func(t *testing.T) {
		config := Config{
			ServiceLifetime: Singleton,
			Name:            "test-service",
		}

		data, err := json.Marshal(config)
		require.NoError(t, err)

		expected := `{"lifetime":"Singleton","name":"test-service"}`
		assert.JSONEq(t, expected, string(data))
	})

	t.Run("unmarshal struct with lifetime", func(t *testing.T) {
		data := `{"lifetime":"Scoped","name":"scoped-service"}`

		var config Config
		err := json.Unmarshal([]byte(data), &config)
		require.NoError(t, err)

		assert.Equal(t, Scoped, config.ServiceLifetime)
		assert.Equal(t, "scoped-service", config.Name)
	})

	t.Run("unmarshal struct with invalid lifetime", func(t *testing.T) {
		data := `{"lifetime":"Invalid","name":"test"}`

		var config Config
		err := json.Unmarshal([]byte(data), &config)
		assert.Error(t, err)
	})
}

func TestLifetimeConstants(t *testing.T) {
	t.Run("constant values", func(t *testing.T) {
		// Verify the constant values are as expected
		assert.Equal(t, Lifetime(0), Singleton)
		assert.Equal(t, Lifetime(1), Scoped)
		assert.Equal(t, Lifetime(2), Transient)
	})

	t.Run("constant ordering", func(t *testing.T) {
		// Verify the ordering is maintained
		assert.Less(t, int(Singleton), int(Scoped))
		assert.Less(t, int(Scoped), int(Transient))
	})
}

func TestLifetimeComparison(t *testing.T) {
	t.Run("equality", func(t *testing.T) {
		assert.Equal(t, Singleton, Singleton)
		assert.Equal(t, Scoped, Scoped)
		assert.Equal(t, Transient, Transient)

		assert.NotEqual(t, Singleton, Scoped)
		assert.NotEqual(t, Singleton, Transient)
		assert.NotEqual(t, Scoped, Transient)
	})

	t.Run("zero value", func(t *testing.T) {
		var lifetime Lifetime
		assert.Equal(t, Singleton, lifetime) // Zero value is Singleton
	})
}

func TestLifetimeSwitch(t *testing.T) {
	testFunc := func(lt Lifetime) string {
		switch lt {
		case Singleton:
			return "singleton"
		case Scoped:
			return "scoped"
		case Transient:
			return "transient"
		default:
			return "unknown"
		}
	}

	tests := []struct {
		lifetime Lifetime
		expected string
	}{
		{Singleton, "singleton"},
		{Scoped, "scoped"},
		{Transient, "transient"},
		{Lifetime(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := testFunc(tt.lifetime)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestLifetimePointer(t *testing.T) {
	t.Run("pointer operations", func(t *testing.T) {
		lifetime := Singleton
		ptr := &lifetime

		assert.Equal(t, Singleton, *ptr)

		*ptr = Scoped
		assert.Equal(t, Scoped, lifetime)
	})

	t.Run("nil pointer unmarshal", func(t *testing.T) {
		var ptr *Lifetime
		data := []byte("Singleton")

		// This should panic or handle gracefully
		assert.Panics(t, func() {
			_ = ptr.UnmarshalText(data)
		})
	})
}

func TestLifetimeSlice(t *testing.T) {
	t.Run("slice of lifetimes", func(t *testing.T) {
		lifetimes := []Lifetime{Singleton, Scoped, Transient}

		assert.Len(t, lifetimes, 3)
		assert.Contains(t, lifetimes, Singleton)
		assert.Contains(t, lifetimes, Scoped)
		assert.Contains(t, lifetimes, Transient)
	})

	t.Run("marshal slice of lifetimes", func(t *testing.T) {
		lifetimes := []Lifetime{Singleton, Scoped, Transient}

		data, err := json.Marshal(lifetimes)
		require.NoError(t, err)

		expected := `["Singleton","Scoped","Transient"]`
		assert.JSONEq(t, expected, string(data))
	})

	t.Run("unmarshal slice of lifetimes", func(t *testing.T) {
		data := `["Singleton","Scoped","Transient"]`

		var lifetimes []Lifetime
		err := json.Unmarshal([]byte(data), &lifetimes)
		require.NoError(t, err)

		assert.Len(t, lifetimes, 3)
		assert.Equal(t, Singleton, lifetimes[0])
		assert.Equal(t, Scoped, lifetimes[1])
		assert.Equal(t, Transient, lifetimes[2])
	})
}

func TestLifetimeMap(t *testing.T) {
	t.Run("map with lifetime keys", func(t *testing.T) {
		m := map[Lifetime]string{
			Singleton: "singleton-value",
			Scoped:    "scoped-value",
			Transient: "transient-value",
		}

		assert.Len(t, m, 3)
		assert.Equal(t, "singleton-value", m[Singleton])
		assert.Equal(t, "scoped-value", m[Scoped])
		assert.Equal(t, "transient-value", m[Transient])
	})

	t.Run("map with lifetime values", func(t *testing.T) {
		m := map[string]Lifetime{
			"service1": Singleton,
			"service2": Scoped,
			"service3": Transient,
		}

		data, err := json.Marshal(m)
		require.NoError(t, err)

		var result map[string]Lifetime
		err = json.Unmarshal(data, &result)
		require.NoError(t, err)

		assert.Equal(t, m, result)
	})
}

func TestLifetimeEdgeCases(t *testing.T) {
	t.Run("very large lifetime value", func(t *testing.T) {
		lifetime := Lifetime(int(^uint(0) >> 1)) // Max int
		assert.False(t, lifetime.IsValid())
		str := lifetime.String()
		assert.Contains(t, str, "Unknown")
	})

	t.Run("negative lifetime value", func(t *testing.T) {
		lifetime := Lifetime(-100)
		assert.False(t, lifetime.IsValid())
		str := lifetime.String()
		assert.Equal(t, "Unknown(-100)", str)
	})

	t.Run("unmarshal with extra whitespace", func(t *testing.T) {
		inputs := []string{
			`"Singleton"`,
			`" Singleton"`,
			`"Singleton "`,
			`" Singleton "`,
			`"\tSingleton\t"`,
			`"\nSingleton\n"`,
		}

		for i, input := range inputs {
			t.Run(fmt.Sprintf("input_%d", i), func(t *testing.T) {
				var lifetime Lifetime
				err := json.Unmarshal([]byte(input), &lifetime)

				if i == 0 {
					// Only the first one (no whitespace) should succeed
					assert.NoError(t, err)
					assert.Equal(t, Singleton, lifetime)
				} else {
					// Others should fail
					assert.Error(t, err)
				}
			})
		}
	})

	t.Run("concurrent access", func(t *testing.T) {
		// Test that lifetime operations are safe for concurrent use
		lifetime := Singleton

		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = lifetime.String()
				_ = lifetime.IsValid()
				_, _ = lifetime.MarshalText()
				_, _ = lifetime.MarshalJSON()
			}()
		}
		wg.Wait()
	})
}

// Benchmark tests
func BenchmarkLifetimeString(b *testing.B) {
	lifetimes := []Lifetime{Singleton, Scoped, Transient, Lifetime(999)}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = lifetimes[i%len(lifetimes)].String()
	}
}

func BenchmarkLifetimeIsValid(b *testing.B) {
	lifetimes := []Lifetime{Singleton, Scoped, Transient, Lifetime(999)}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = lifetimes[i%len(lifetimes)].IsValid()
	}
}

func BenchmarkLifetimeMarshalText(b *testing.B) {
	lifetime := Singleton

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = lifetime.MarshalText()
	}
}

func BenchmarkLifetimeUnmarshalText(b *testing.B) {
	data := []byte("Singleton")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var lifetime Lifetime
		_ = lifetime.UnmarshalText(data)
	}
}

func BenchmarkLifetimeMarshalJSON(b *testing.B) {
	lifetime := Scoped

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = lifetime.MarshalJSON()
	}
}

func BenchmarkLifetimeUnmarshalJSON(b *testing.B) {
	data := []byte(`"Scoped"`)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var lifetime Lifetime
		_ = lifetime.UnmarshalJSON(data)
	}
}

func BenchmarkLifetimeSwitch(b *testing.B) {
	lifetimes := []Lifetime{Singleton, Scoped, Transient}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lt := lifetimes[i%len(lifetimes)]
		switch lt {
		case Singleton:
			// Do nothing
		case Scoped:
			// Do nothing
		case Transient:
			// Do nothing
		}
	}
}
