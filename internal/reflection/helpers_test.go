package reflection_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/junioryono/godi/v4/internal/reflection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test FormatConstructor
func TestTypeFormatter_FormatConstructor(t *testing.T) {
	formatter := &reflection.TypeFormatter{}
	analyzer := reflection.New()

	tests := []struct {
		name        string
		constructor any
		expected    string
	}{
		{
			name:        "nil constructor info",
			constructor: nil,
			expected:    "<nil>",
		},
		{
			name:        "non-function value",
			constructor: &Database{ConnectionString: "test"},
			expected:    "value[*Database]",
		},
		{
			name:        "simple function",
			constructor: NewDatabase,
			expected:    "func(string) *Database",
		},
		{
			name:        "param object function",
			constructor: NewServiceWithParams,
			expected:    "func(struct{In; Database *Database; Logger Logger `optional`; Cache *Database `name:cache`; Handlers []func() `group:handlers`})",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.constructor == nil {
				result := formatter.FormatConstructor(nil)
				assert.Equal(t, tt.expected, result)
				return
			}

			info, err := analyzer.Analyze(tt.constructor)
			require.NoError(t, err, "Failed to analyze constructor")

			result := formatter.FormatConstructor(info)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test DependencyMatcher
func TestDependencyMatcher(t *testing.T) {
	matcher := reflection.NewMatcher()

	t.Run("MatchType", func(t *testing.T) {
		tests := []struct {
			name         string
			providerType reflect.Type
			depType      reflect.Type
			want         bool
		}{
			{
				name:         "exact match",
				providerType: reflect.TypeOf((*Database)(nil)),
				depType:      reflect.TypeOf((*Database)(nil)),
				want:         true,
			},
			{
				name:         "interface implementation",
				providerType: reflect.TypeOf((*ConsoleLogger)(nil)),
				depType:      reflect.TypeOf((*Logger)(nil)).Elem(),
				want:         true,
			},
			{
				name:         "no match",
				providerType: reflect.TypeOf((*Database)(nil)),
				depType:      reflect.TypeOf((*Logger)(nil)).Elem(),
				want:         false,
			},
			{
				name:         "assignable types",
				providerType: reflect.TypeOf(&ConsoleLogger{}),
				depType:      reflect.TypeOf(&ConsoleLogger{}),
				want:         true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := matcher.MatchType(tt.providerType, tt.depType)
				assert.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("MatchKey", func(t *testing.T) {
		tests := []struct {
			name        string
			providerKey any
			depKey      any
			want        bool
		}{
			{
				name:        "both nil",
				providerKey: nil,
				depKey:      nil,
				want:        true,
			},
			{
				name:        "dependency nil accepts any",
				providerKey: "key1",
				depKey:      nil,
				want:        true,
			},
			{
				name:        "exact match",
				providerKey: "key1",
				depKey:      "key1",
				want:        true,
			},
			{
				name:        "no match",
				providerKey: "key1",
				depKey:      "key2",
				want:        false,
			},
			{
				name:        "provider nil, dependency has key",
				providerKey: nil,
				depKey:      "key1",
				want:        false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := matcher.MatchKey(tt.providerKey, tt.depKey)
				assert.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("MatchGroup", func(t *testing.T) {
		tests := []struct {
			name           string
			providerGroups []string
			depGroup       string
			want           bool
		}{
			{
				name:           "match found",
				providerGroups: []string{"handlers", "middleware"},
				depGroup:       "handlers",
				want:           true,
			},
			{
				name:           "no match",
				providerGroups: []string{"handlers", "middleware"},
				depGroup:       "services",
				want:           false,
			},
			{
				name:           "empty dependency group",
				providerGroups: []string{"handlers"},
				depGroup:       "",
				want:           false,
			},
			{
				name:           "empty provider groups",
				providerGroups: []string{},
				depGroup:       "handlers",
				want:           false,
			},
			{
				name:           "nil provider groups",
				providerGroups: nil,
				depGroup:       "handlers",
				want:           false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := matcher.MatchGroup(tt.providerGroups, tt.depGroup)
				assert.Equal(t, tt.want, got)
			})
		}
	})

	t.Run("FormatDependencyError", func(t *testing.T) {
		tests := []struct {
			name     string
			depType  reflect.Type
			key      any
			group    string
			expected string
		}{
			{
				name:     "group dependency",
				depType:  reflect.TypeOf(func() {}),
				key:      nil,
				group:    "handlers",
				expected: `no providers found for group "handlers" of type func()`,
			},
			{
				name:     "keyed dependency",
				depType:  reflect.TypeOf((*Database)(nil)),
				key:      "backup",
				group:    "",
				expected: `no provider found for *Database with key "backup"`,
			},
			{
				name:     "regular dependency",
				depType:  reflect.TypeOf((*Logger)(nil)).Elem(),
				key:      nil,
				group:    "",
				expected: "no provider found for Logger",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got := matcher.FormatDependencyError(tt.depType, tt.key, tt.group)
				assert.Equal(t, tt.expected, got)
			})
		}
	})
}

// Test additional edge cases in TypeFormatter
func TestTypeFormatter_EdgeCases(t *testing.T) {
	formatter := &reflection.TypeFormatter{}

	tests := []struct {
		name     string
		typ      reflect.Type
		expected string
	}{
		{
			name:     "array type",
			typ:      reflect.TypeOf([3]string{}),
			expected: "[3]string",
		},
		{
			name:     "any type",
			typ:      reflect.TypeOf((*any)(nil)).Elem(),
			expected: "any",
		},
		{
			name:     "complex function",
			typ:      reflect.TypeOf(func(int, string, ...float64) (bool, error) { return false, nil }),
			expected: "func(int, string, []float64) (bool, error)",
		},
		{
			name:     "nested pointer",
			typ:      reflect.TypeOf((**Database)(nil)),
			expected: "**Database",
		},
		{
			name:     "slice of pointers",
			typ:      reflect.TypeOf([]*Database{}),
			expected: "[]*Database",
		},
		{
			name:     "map of interfaces",
			typ:      reflect.TypeOf(map[string]Logger{}),
			expected: "map[string]Logger",
		},
		{
			name:     "bidirectional channel",
			typ:      reflect.TypeOf(make(chan int)),
			expected: "chan int",
		},
		{
			name:     "send-only channel",
			typ:      reflect.TypeOf(make(chan<- string)),
			expected: "->chan string",
		},
		{
			name:     "receive-only channel",
			typ:      reflect.TypeOf(make(<-chan bool)),
			expected: "<-chan bool",
		},
		{
			name:     "struct without name",
			typ:      reflect.TypeOf(struct{ X, Y int }{}),
			expected: "struct{...}",
		},
		{
			name:     "function with no params",
			typ:      reflect.TypeOf(func() {}),
			expected: "func()",
		},
		{
			name:     "function with single return",
			typ:      reflect.TypeOf(func() int { return 0 }),
			expected: "func() int",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatter.FormatType(tt.typ)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// Test Validator edge cases
func TestValidator_EdgeCases(t *testing.T) {
	analyzer := reflection.New()
	validator := reflection.NewValidator(analyzer)

	t.Run("Out struct with no exported fields", func(t *testing.T) {
		// Test with Out struct that has no exported fields
		type EmptyOut struct {
			reflection.Out
			_ string // unexported field for testing
		}

		emptyOutConstructor := func() EmptyOut {
			return EmptyOut{}
		}

		info, err := analyzer.Analyze(emptyOutConstructor)
		require.NoError(t, err, "Failed to analyze")

		err = validator.Validate(info)
		assert.Error(t, err, "Expected error for Out struct with no exported fields")
	})

	t.Run("Multiple In parameters", func(t *testing.T) {
		// Test with multiple In parameters
		type In1 struct {
			reflection.In
			DB *Database
		}

		type In2 struct {
			reflection.In
			Logger Logger
		}

		// This is invalid - can't have multiple In parameters
		multiInConstructor := func(in1 In1, in2 In2) *UserService {
			return &UserService{DB: in1.DB, Logger: in2.Logger}
		}

		info, err := analyzer.Analyze(multiInConstructor)
		require.NoError(t, err, "Failed to analyze")

		err = validator.Validate(info)
		assert.Error(t, err, "Expected error for multiple In parameters")
	})

	t.Run("Group field that's not a slice", func(t *testing.T) {
		// Test group field that's not a slice
		type InvalidGroupParam struct {
			reflection.In
			Handler string `group:"handlers"` // Invalid: group on non-slice
		}

		invalidGroupConstructor := func(params InvalidGroupParam) int {
			return 0
		}

		info, err := analyzer.Analyze(invalidGroupConstructor)
		require.NoError(t, err, "Failed to analyze")

		err = validator.Validate(info)
		assert.Error(t, err, "Expected error for group field that's not a slice")
	})

	t.Run("Duplicate field names in In struct", func(t *testing.T) {
		// Test duplicate field names in In struct
		// Note: Go doesn't allow actual duplicate field names, so we test
		// the validator's duplicate detection logic with a mock
		mockInfo := &reflection.ConstructorInfo{
			IsFunc:        true,
			IsParamObject: true,
			Parameters: []reflection.ParameterInfo{
				{Name: "DB", Type: reflect.TypeOf((*Database)(nil))},
				{Name: "DB", Type: reflect.TypeOf((*Database)(nil))}, // Duplicate
			},
			Returns: []reflection.ReturnInfo{
				{Type: reflect.TypeOf(0)},
			},
			Type: reflect.TypeOf(func(struct{}) int { return 0 }),
		}

		err := validator.Validate(mockInfo)
		assert.Error(t, err, "Expected error for duplicate field names")
	})
}

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
		assert.Equal(t, reflect.TypeOf((*Database)(nil)), types[0], "Wrong type returned")
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

	resolver.values[reflect.TypeOf((*Database)(nil))] = mainDB
	resolver.values[reflect.TypeOf((*Logger)(nil)).Elem()] = logger
	resolver.keyedValues["backup"] = backupDB
	resolver.keyedValues["special"] = specialDB
	resolver.groups["handlers"] = handlers

	t.Run("BuildParamObject with all field types", func(t *testing.T) {
		paramType := reflect.TypeOf(ComplexParams{})
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

		paramType := reflect.TypeOf(ComplexParams{})

		// Should fail when required dependencies can't be resolved
		_, err := builder.BuildParamObject(paramType, failResolver)
		assert.Error(t, err, "Should fail when required dependencies can't be resolved")
	})

	t.Run("Only optional fields failing", func(t *testing.T) {
		// Test with only optional fields failing
		type OnlyOptional struct {
			reflection.In
			Optional1 *Database `optional:"true"`
			Optional2 Logger    `optional:"true"`
		}

		failResolver := NewTestResolver()
		failResolver.shouldFail = true
		failResolver.failError = errors.New("optional dep failed")

		optType := reflect.TypeOf(OnlyOptional{})
		result, err := builder.BuildParamObject(optType, failResolver)
		assert.NoError(t, err, "Should succeed with only optional fields failing")

		optParams := result.Interface().(OnlyOptional)
		assert.Nil(t, optParams.Optional1, "Optional field should be nil when resolution fails")
		assert.Nil(t, optParams.Optional2, "Optional field should be nil when resolution fails")
	})
}

// Test TypeFormatter with more complex scenarios
func TestTypeFormatter_MoreComplexScenarios(t *testing.T) {
	formatter := &reflection.TypeFormatter{}

	t.Run("Complex nested types", func(t *testing.T) {
		// Test with nested slice of maps
		complexType := reflect.TypeOf([]map[string][]int{})
		result := formatter.FormatType(complexType)
		expected := "[]map[string][]int"
		assert.Equal(t, expected, result)

		// Test with function returning multiple complex types
		funcType := reflect.TypeOf(func() (map[string]*Database, []Logger, error) {
			return nil, nil, nil
		})
		result = formatter.FormatType(funcType)
		expected = "func() (map[string]*Database, []Logger, error)"
		assert.Equal(t, expected, result)
	})

	t.Run("FormatConstructor with param object having all tag types", func(t *testing.T) {
		type CompleteParams struct {
			reflection.In

			DB       *Database
			Logger   Logger    `optional:"true"`
			Cache    *Database `name:"cache"`
			Handlers []func()  `group:"handlers"`
			Both     *Database `name:"special" optional:"true" group:"dbs"`
		}

		constructor := func(params CompleteParams) int { return 0 }

		analyzer := reflection.New()
		info, err := analyzer.Analyze(constructor)
		require.NoError(t, err, "Failed to analyze")

		result := formatter.FormatConstructor(info)
		// Should contain all the field annotations
		assert.Contains(t, result, "optional", "FormatConstructor should include optional tag")
		assert.Contains(t, result, "name:", "FormatConstructor should include name tag")
		assert.Contains(t, result, "group:", "FormatConstructor should include group tag")
	})
}

// Test Validator with more comprehensive scenarios
func TestValidator_ComprehensiveScenarios(t *testing.T) {
	analyzer := reflection.New()
	validator := reflection.NewValidator(analyzer)

	t.Run("Result object with error but no fields", func(t *testing.T) {
		type EmptyResultWithError struct {
			reflection.Out
		}

		constructor := func() (EmptyResultWithError, error) {
			return EmptyResultWithError{}, nil
		}

		info, err := analyzer.Analyze(constructor)
		require.NoError(t, err, "Failed to analyze")

		err = validator.Validate(info)
		assert.Error(t, err, "Expected error for Out struct with no exported fields")
	})

	t.Run("Valid Out struct with multiple return scenarios", func(t *testing.T) {
		type ValidOut struct {
			reflection.Out
			Service1 *Database
			Service2 Logger
		}

		// Test with just Out
		constructor1 := func() ValidOut {
			return ValidOut{}
		}

		info1, err := analyzer.Analyze(constructor1)
		require.NoError(t, err)
		err = validator.Validate(info1)
		assert.NoError(t, err, "Valid Out struct should pass validation")

		// Test with Out and error
		constructor2 := func() (ValidOut, error) {
			return ValidOut{}, nil
		}

		info2, err := analyzer.Analyze(constructor2)
		require.NoError(t, err)
		err = validator.Validate(info2)
		assert.NoError(t, err, "Valid Out struct with error should pass validation")

		// Test with Out and non-error second return
		constructor3 := func() (ValidOut, string) {
			return ValidOut{}, ""
		}

		info3, err := analyzer.Analyze(constructor3)
		require.NoError(t, err)
		err = validator.Validate(info3)
		assert.Error(t, err, "Out struct with non-error second return should fail validation")
	})

	t.Run("Too many returns for Out struct", func(t *testing.T) {
		type SimpleOut struct {
			reflection.Out
			Service *Database
		}

		// Mock info with too many returns
		mockInfo := &reflection.ConstructorInfo{
			IsFunc:         true,
			IsResultObject: true,
			Type:           reflect.TypeOf(func() (SimpleOut, string, error) { return SimpleOut{}, "", nil }),
			Returns: []reflection.ReturnInfo{
				{Type: reflect.TypeOf(SimpleOut{})},
			},
		}

		err := validator.Validate(mockInfo)
		assert.Error(t, err, "Expected error for Out struct with more than 2 returns")
	})
}
