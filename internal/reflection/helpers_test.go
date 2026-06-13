package reflection_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/junioryono/godi/v5/internal/reflection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test more scenarios for coverage
func TestAnalyzer_AdditionalScenarios(t *testing.T) {
	analyzer := reflection.New()

	t.Run("GetServiceType with function that only returns error", func(t *testing.T) {
		// Test GetServiceType with function that only returns error
		errorOnlyFunc := func() error {
			return nil
		}

		_, err := analyzer.GetServiceType(errorOnlyFunc)
		assert.Error(t, err, "Expected error for function that only returns error")
	})

	t.Run("GetResultTypes with non-result object", func(t *testing.T) {
		// Test GetResultTypes with non-result object
		simpleFunc := func() *Database {
			return &Database{}
		}

		types, err := analyzer.GetResultTypes(simpleFunc)
		require.NoError(t, err, "GetResultTypes failed")

		assert.Len(t, types, 1, "Expected 1 type")
		assert.Equal(t, reflect.TypeFor[*Database](), types[0], "Wrong type returned")
	})

	t.Run("Type that doesn't embed In/Out", func(t *testing.T) {
		// Test with a type that doesn't embed In/Out
		// This verifies the analyzer correctly identifies non-In/Out types
		type NotInOut struct {
			Field string
		}

		notInOutFunc := func(n NotInOut) int {
			return 0
		}

		info, err := analyzer.Analyze(notInOutFunc)
		require.NoError(t, err, "Failed to analyze")

		// Should not be detected as param object
		assert.False(t, info.IsParamObject, "Function with non-In parameter should not be detected as param object")
	})
}

// Test more ParamObjectBuilder scenarios
func TestParamObjectBuilder_MoreScenarios(t *testing.T) {
	analyzer := reflection.New()
	builder := reflection.NewParamObjectBuilder(analyzer)

	resolver := NewTestResolver()

	// Test with all field types
	type ComplexParams struct {
		reflection.In

		Required    *Database
		Optional    Logger    `optional:"true"`
		Named       *Database `name:"backup"`
		Grouped     []func()  `group:"handlers"`
		Ignored     string    `inject:"-"`
		Combination *Database `name:"special" optional:"true"`
	}

	// Setup resolver
	mainDB := &Database{ConnectionString: "main"}
	backupDB := &Database{ConnectionString: "backup"}
	specialDB := &Database{ConnectionString: "special"}
	logger := &ConsoleLogger{}
	handlers := []any{
		func() { println("h1") },
		func() { println("h2") },
	}

	resolver.values[reflect.TypeFor[*Database]()] = mainDB
	resolver.values[reflect.TypeFor[Logger]()] = logger
	resolver.keyedValues["backup"] = backupDB
	resolver.keyedValues["special"] = specialDB
	resolver.groups["handlers"] = handlers

	t.Run("BuildParamObject with all field types", func(t *testing.T) {
		paramType := reflect.TypeFor[ComplexParams]()
		result, err := builder.BuildParamObject(paramType, resolver)
		require.NoError(t, err, "BuildParamObject failed")

		params := result.Interface().(ComplexParams)

		// Verify all fields
		assert.Equal(t, mainDB, params.Required, "Required field not set correctly")
		assert.Equal(t, logger, params.Optional, "Optional field not set correctly")
		assert.Equal(t, backupDB, params.Named, "Named field not set correctly")
		assert.Len(t, params.Grouped, 2, "Expected 2 handlers")
		assert.Empty(t, params.Ignored, "Ignored field should not be set")
		assert.Equal(t, specialDB, params.Combination, "Combination field not set correctly")
	})

	t.Run("Failed required dependency", func(t *testing.T) {
		// Test with failed optional dependency
		failResolver := NewTestResolver()
		failResolver.shouldFail = true
		failResolver.failError = errors.New("optional dep failed")

		paramType := reflect.TypeFor[ComplexParams]()

		// Should fail when required dependencies can't be resolved
		_, err := builder.BuildParamObject(paramType, failResolver)
		assert.Error(t, err, "Should fail when required dependencies can't be resolved")
	})

	t.Run("Only optional fields failing", func(t *testing.T) {
		type OnlyOptional struct {
			reflection.In
			Optional1 *Database `optional:"true"`
			Optional2 Logger    `optional:"true"`
		}
		optType := reflect.TypeFor[OnlyOptional]()

		// A generic construction failure must propagate even for optional
		// fields; optional only forgives "not registered".
		failResolver := NewTestResolver()
		failResolver.shouldFail = true
		failResolver.failError = errors.New("optional dep failed")

		_, err := builder.BuildParamObject(optType, failResolver)
		assert.Error(t, err, "construction failure of an optional dependency must propagate")

		// Missing (not registered) optional dependencies are skipped.
		notFoundResolver := NewTestResolver()
		notFoundResolver.shouldFail = true
		notFoundResolver.failError = reflection.ErrServiceNotFound

		result, err := builder.BuildParamObject(optType, notFoundResolver)
		assert.NoError(t, err, "missing optional dependencies should be skipped")

		optParams := result.Interface().(OnlyOptional)
		assert.Nil(t, optParams.Optional1, "Optional field should be nil when not registered")
		assert.Nil(t, optParams.Optional2, "Optional field should be nil when not registered")
	})
}
