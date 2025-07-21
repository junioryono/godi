package godi_test

import (
	"encoding/json"
	"testing"

	"github.com/junioryono/godi/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceLifetime_String(t *testing.T) {
	tests := []struct {
		name     string
		lifetime godi.ServiceLifetime
		expected string
	}{
		{
			name:     "singleton",
			lifetime: godi.Singleton,
			expected: "Singleton",
		},
		{
			name:     "scoped",
			lifetime: godi.Scoped,
			expected: "Scoped",
		},
		{
			name:     "invalid",
			lifetime: godi.ServiceLifetime(99),
			expected: "Unknown(99)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, tt.lifetime.String())
		})
	}
}

func TestServiceLifetime_IsValid(t *testing.T) {
	tests := []struct {
		name     string
		lifetime godi.ServiceLifetime
		valid    bool
	}{
		{
			name:     "singleton is valid",
			lifetime: godi.Singleton,
			valid:    true,
		},
		{
			name:     "scoped is valid",
			lifetime: godi.Scoped,
			valid:    true,
		},
		{
			name:     "negative value is invalid",
			lifetime: godi.ServiceLifetime(-1),
			valid:    false,
		},
		{
			name:     "too large value is invalid",
			lifetime: godi.ServiceLifetime(10),
			valid:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.valid, tt.lifetime.IsValid())
		})
	}
}

func TestServiceLifetime_MarshalText(t *testing.T) {
	tests := []struct {
		name     string
		lifetime godi.ServiceLifetime
		expected string
	}{
		{
			name:     "marshal singleton",
			lifetime: godi.Singleton,
			expected: "Singleton",
		},
		{
			name:     "marshal scoped",
			lifetime: godi.Scoped,
			expected: "Scoped",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			text, err := tt.lifetime.MarshalText()
			require.NoError(t, err)
			assert.Equal(t, tt.expected, string(text))
		})
	}
}

func TestServiceLifetime_UnmarshalText(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		expected godi.ServiceLifetime
		wantErr  bool
	}{
		{
			name:     "unmarshal Singleton",
			text:     "Singleton",
			expected: godi.Singleton,
		},
		{
			name:     "unmarshal singleton (lowercase)",
			text:     "singleton",
			expected: godi.Singleton,
		},
		{
			name:     "unmarshal Scoped",
			text:     "Scoped",
			expected: godi.Scoped,
		},
		{
			name:     "unmarshal scoped (lowercase)",
			text:     "scoped",
			expected: godi.Scoped,
		},
		{
			name:    "unmarshal invalid",
			text:    "Invalid",
			wantErr: true,
		},
		{
			name:    "unmarshal empty",
			text:    "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var lifetime godi.ServiceLifetime
			err := lifetime.UnmarshalText([]byte(tt.text))

			if tt.wantErr {
				assert.Error(t, err)

				var lifetimeErr *godi.LifetimeError
				assert.ErrorAs(t, err, &lifetimeErr)
				assert.Equal(t, tt.text, lifetimeErr.Value)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expected, lifetime)
			}
		})
	}
}

func TestServiceLifetime_JSON(t *testing.T) {
	t.Run("marshal to JSON", func(t *testing.T) {
		t.Parallel()

		type Config struct {
			Lifetime godi.ServiceLifetime `json:"lifetime"`
		}

		config := Config{Lifetime: godi.Singleton}
		data, err := json.Marshal(config)

		require.NoError(t, err)
		assert.JSONEq(t, `{"lifetime":"Singleton"}`, string(data))
	})

	t.Run("unmarshal from JSON", func(t *testing.T) {
		t.Parallel()

		type Config struct {
			Lifetime godi.ServiceLifetime `json:"lifetime"`
		}

		tests := []struct {
			name     string
			json     string
			expected godi.ServiceLifetime
			wantErr  bool
		}{
			{
				name:     "valid singleton",
				json:     `{"lifetime":"Singleton"}`,
				expected: godi.Singleton,
			},
			{
				name:     "valid scoped",
				json:     `{"lifetime":"Scoped"}`,
				expected: godi.Scoped,
			},
			{
				name:     "lowercase singleton",
				json:     `{"lifetime":"singleton"}`,
				expected: godi.Singleton,
			},
			{
				name:    "invalid lifetime",
				json:    `{"lifetime":"Invalid"}`,
				wantErr: true,
			},
			{
				name:    "numeric value",
				json:    `{"lifetime":0}`,
				wantErr: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				var config Config
				err := json.Unmarshal([]byte(tt.json), &config)

				if tt.wantErr {
					assert.Error(t, err)
				} else {
					require.NoError(t, err)
					assert.Equal(t, tt.expected, config.Lifetime)
				}
			})
		}
	})
}

func TestServiceLifetime_RoundTrip(t *testing.T) {
	lifetimes := []godi.ServiceLifetime{
		godi.Singleton,
		godi.Scoped,
	}

	t.Run("text marshaling round trip", func(t *testing.T) {
		t.Parallel()

		for _, original := range lifetimes {
			// Marshal
			text, err := original.MarshalText()
			require.NoError(t, err)

			// Unmarshal
			var result godi.ServiceLifetime
			err = result.UnmarshalText(text)
			require.NoError(t, err)

			// Compare
			assert.Equal(t, original, result)
		}
	})

	t.Run("JSON marshaling round trip", func(t *testing.T) {
		t.Parallel()

		for _, original := range lifetimes {
			// Marshal
			data, err := json.Marshal(original)
			require.NoError(t, err)

			// Unmarshal
			var result godi.ServiceLifetime
			err = json.Unmarshal(data, &result)
			require.NoError(t, err)

			// Compare
			assert.Equal(t, original, result)
		}
	})
}

func TestServiceLifetime_Usage(t *testing.T) {
	t.Run("lifetime constants are distinct", func(t *testing.T) {
		t.Parallel()
		assert.NotEqual(t, godi.Singleton, godi.Scoped)
	})

	t.Run("zero value is Singleton", func(t *testing.T) {
		t.Parallel()

		var lifetime godi.ServiceLifetime
		assert.Equal(t, godi.Singleton, lifetime)
		assert.Equal(t, "Singleton", lifetime.String())
	})

	t.Run("lifetimes can be compared", func(t *testing.T) {
		t.Parallel()

		lifetime1 := godi.Singleton
		lifetime2 := godi.Singleton
		lifetime3 := godi.Scoped

		assert.True(t, lifetime1 == lifetime2)
		assert.False(t, lifetime1 == lifetime3)
	})

	t.Run("lifetimes can be used in switch", func(t *testing.T) {
		t.Parallel()

		checkLifetime := func(lt godi.ServiceLifetime) string {
			switch lt {
			case godi.Singleton:
				return "singleton"
			case godi.Scoped:
				return "scoped"
			default:
				return "unknown"
			}
		}

		assert.Equal(t, "singleton", checkLifetime(godi.Singleton))
		assert.Equal(t, "scoped", checkLifetime(godi.Scoped))
		assert.Equal(t, "unknown", checkLifetime(godi.ServiceLifetime(99)))
	})
}

// Table-driven tests for edge cases
func TestServiceLifetime_EdgeCases(t *testing.T) {
	tests := []struct {
		name   string
		action func(t *testing.T)
	}{
		{
			name: "unmarshal with whitespace",
			action: func(t *testing.T) {
				var lifetime godi.ServiceLifetime
				err := lifetime.UnmarshalText([]byte("  Singleton  "))
				// Should fail because it doesn't trim whitespace
				assert.Error(t, err)
			},
		},
		{
			name: "unmarshal case variations",
			action: func(t *testing.T) {
				variations := []string{
					"SINGLETON",
					"SCOPED",
					"Singleton",
					"Scoped",
					"SiNgLeTon",
				}

				for _, v := range variations {
					var lifetime godi.ServiceLifetime
					err := lifetime.UnmarshalText([]byte(v))

					// Only exact matches should work
					if v == "Singleton" || v == "singleton" {
						assert.NoError(t, err)
						assert.Equal(t, godi.Singleton, lifetime)
					} else if v == "Scoped" || v == "scoped" {
						assert.NoError(t, err)
						assert.Equal(t, godi.Scoped, lifetime)
					} else {
						assert.Error(t, err)
					}
				}
			},
		},
		{
			name: "marshal invalid lifetime",
			action: func(t *testing.T) {
				lifetime := godi.ServiceLifetime(99)
				text, err := lifetime.MarshalText()

				// Should still work, returning "Unknown(99)"
				assert.NoError(t, err)
				assert.Equal(t, "Unknown(99)", string(text))
			},
		},
		{
			name: "use in map keys",
			action: func(t *testing.T) {
				// Lifetimes should be usable as map keys
				m := make(map[godi.ServiceLifetime]string)
				m[godi.Singleton] = "singleton"
				m[godi.Scoped] = "scoped"

				assert.Equal(t, "singleton", m[godi.Singleton])
				assert.Equal(t, "scoped", m[godi.Scoped])
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.action(t)
		})
	}
}
