package godi

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLifetime(t *testing.T) {
	t.Parallel()

	// All valid lifetimes for reuse in tests
	validLifetimes := []Lifetime{Singleton, Scoped, Transient}

	t.Run("String", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			lt   Lifetime
			want string
		}{
			{Singleton, "Singleton"},
			{Scoped, "Scoped"},
			{Transient, "Transient"},
			{Lifetime(-1), "Unknown(-1)"},
			{Lifetime(999), "Unknown(999)"},
		}
		for _, tc := range cases {
			assert.Equal(t, tc.want, tc.lt.String())
		}
	})

	t.Run("IsValid", func(t *testing.T) {
		t.Parallel()
		for _, lt := range validLifetimes {
			assert.True(t, lt.IsValid(), "%s should be valid", lt)
		}
		assert.False(t, Lifetime(-1).IsValid())
		assert.False(t, Lifetime(3).IsValid())
		assert.False(t, Lifetime(999).IsValid())
	})

	t.Run("Constants", func(t *testing.T) {
		t.Parallel()
		// Verify constant values (important for serialization compatibility)
		assert.Equal(t, Lifetime(0), Singleton)
		assert.Equal(t, Lifetime(1), Scoped)
		assert.Equal(t, Lifetime(2), Transient)

		// Zero value should be Singleton
		var zero Lifetime
		assert.Equal(t, Singleton, zero)
	})

	t.Run("TextRoundTrip", func(t *testing.T) {
		t.Parallel()
		for _, lt := range validLifetimes {
			data, err := lt.MarshalText()
			require.NoError(t, err)

			var got Lifetime
			require.NoError(t, got.UnmarshalText(data))
			assert.Equal(t, lt, got)
		}
	})

	t.Run("UnmarshalText", func(t *testing.T) {
		t.Parallel()

		t.Run("valid_inputs", func(t *testing.T) {
			cases := []struct {
				input string
				want  Lifetime
			}{
				{"Singleton", Singleton},
				{"singleton", Singleton},
				{"Scoped", Scoped},
				{"scoped", Scoped},
				{"Transient", Transient},
				{"transient", Transient},
			}
			for _, tc := range cases {
				var got Lifetime
				err := got.UnmarshalText([]byte(tc.input))
				require.NoError(t, err, "input: %s", tc.input)
				assert.Equal(t, tc.want, got)
			}
		})

		t.Run("invalid_inputs", func(t *testing.T) {
			inputs := []string{"", "Invalid", "random", " Singleton ", "SiNgLeToN"}
			for _, input := range inputs {
				var got Lifetime
				err := got.UnmarshalText([]byte(input))
				assert.Error(t, err, "input: %q should error", input)
				var ltErr *LifetimeError
				assert.IsType(t, ltErr, err)
			}
		})
	})

	t.Run("JSONRoundTrip", func(t *testing.T) {
		t.Parallel()
		for _, lt := range validLifetimes {
			data, err := json.Marshal(lt)
			require.NoError(t, err)

			var got Lifetime
			require.NoError(t, json.Unmarshal(data, &got))
			assert.Equal(t, lt, got)
		}
	})

	t.Run("UnmarshalJSON", func(t *testing.T) {
		t.Parallel()

		t.Run("valid", func(t *testing.T) {
			cases := []struct {
				input string
				want  Lifetime
			}{
				{`"Singleton"`, Singleton},
				{`"singleton"`, Singleton},
				{`"Scoped"`, Scoped},
				{`"scoped"`, Scoped},
				{`"Transient"`, Transient},
				{`"transient"`, Transient},
			}
			for _, tc := range cases {
				var got Lifetime
				err := json.Unmarshal([]byte(tc.input), &got)
				require.NoError(t, err, "input: %s", tc.input)
				assert.Equal(t, tc.want, got)
			}
		})

		t.Run("invalid", func(t *testing.T) {
			inputs := []string{
				`"Invalid"`, `""`, `null`, `0`,
				`Singleton`, `["Singleton"]`, `{"lifetime":"Singleton"}`,
			}
			for _, input := range inputs {
				var got Lifetime
				err := json.Unmarshal([]byte(input), &got)
				assert.Error(t, err, "input: %s should error", input)
			}
		})
	})

	t.Run("JSONInStruct", func(t *testing.T) {
		t.Parallel()
		type Config struct {
			Lifetime Lifetime `json:"lifetime"`
			Name     string   `json:"name"`
		}

		// Marshal
		cfg := Config{Lifetime: Singleton, Name: "test"}
		data, err := json.Marshal(cfg)
		require.NoError(t, err)
		assert.JSONEq(t, `{"lifetime":"Singleton","name":"test"}`, string(data))

		// Unmarshal
		var got Config
		require.NoError(t, json.Unmarshal([]byte(`{"lifetime":"Scoped","name":"svc"}`), &got))
		assert.Equal(t, Scoped, got.Lifetime)
		assert.Equal(t, "svc", got.Name)

		// Invalid
		err = json.Unmarshal([]byte(`{"lifetime":"Invalid"}`), &got)
		assert.Error(t, err)
	})

	t.Run("JSONSlice", func(t *testing.T) {
		t.Parallel()
		lifetimes := []Lifetime{Singleton, Scoped, Transient}

		data, err := json.Marshal(lifetimes)
		require.NoError(t, err)
		assert.JSONEq(t, `["Singleton","Scoped","Transient"]`, string(data))

		var got []Lifetime
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, lifetimes, got)
	})

	t.Run("JSONMap", func(t *testing.T) {
		t.Parallel()
		m := map[string]Lifetime{"a": Singleton, "b": Scoped}

		data, err := json.Marshal(m)
		require.NoError(t, err)

		var got map[string]Lifetime
		require.NoError(t, json.Unmarshal(data, &got))
		assert.Equal(t, m, got)
	})

	t.Run("NilPointerUnmarshal", func(t *testing.T) {
		t.Parallel()
		var ptr *Lifetime
		assert.Panics(t, func() {
			_ = ptr.UnmarshalText([]byte("Singleton"))
		})
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		t.Parallel()
		lt := Singleton
		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = lt.String()
				_ = lt.IsValid()
				_, _ = lt.MarshalText()
				_, _ = lt.MarshalJSON()
			}()
		}
		wg.Wait()
	})
}
