package reflection_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/junioryono/godi/v4/internal/reflection"
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
				if result != tt.expected {
					t.Errorf("FormatConstructor(nil) = %q, want %q", result, tt.expected)
				}
				return
			}

			info, err := analyzer.Analyze(tt.constructor)
			if err != nil {
				t.Fatalf("Failed to analyze constructor: %v", err)
			}

			result := formatter.FormatConstructor(info)
			if result != tt.expected {
				t.Errorf("FormatConstructor() = %q, want %q", result, tt.expected)
			}
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
				if got != tt.want {
					t.Errorf("MatchType() = %v, want %v", got, tt.want)
				}
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
				if got != tt.want {
					t.Errorf("MatchKey() = %v, want %v", got, tt.want)
				}
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
				if got != tt.want {
					t.Errorf("MatchGroup() = %v, want %v", got, tt.want)
				}
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
				if got != tt.expected {
					t.Errorf("FormatDependencyError() = %q, want %q", got, tt.expected)
				}
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
			if got != tt.expected {
				t.Errorf("FormatType() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// Test Validator edge cases
func TestValidator_EdgeCases(t *testing.T) {
	analyzer := reflection.New()
	validator := reflection.NewValidator(analyzer)

	// Test with Out struct that has no exported fields
	type EmptyOut struct {
		reflection.Out
		unexported string
	}

	emptyOutConstructor := func() EmptyOut {
		return EmptyOut{}
	}

	info, err := analyzer.Analyze(emptyOutConstructor)
	if err != nil {
		t.Fatalf("Failed to analyze: %v", err)
	}

	err = validator.Validate(info)
	if err == nil {
		t.Error("Expected error for Out struct with no exported fields")
	}

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

	info2, err := analyzer.Analyze(multiInConstructor)
	if err != nil {
		t.Fatalf("Failed to analyze: %v", err)
	}

	err = validator.Validate(info2)
	if err == nil {
		t.Error("Expected error for multiple In parameters")
	}

	// Test group field that's not a slice
	type InvalidGroupParam struct {
		reflection.In
		Handler string `group:"handlers"` // Invalid: group on non-slice
	}

	invalidGroupConstructor := func(params InvalidGroupParam) int {
		return 0
	}

	info3, err := analyzer.Analyze(invalidGroupConstructor)
	if err != nil {
		t.Fatalf("Failed to analyze: %v", err)
	}

	err = validator.Validate(info3)
	if err == nil {
		t.Error("Expected error for group field that's not a slice")
	}

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

	err = validator.Validate(mockInfo)
	if err == nil {
		t.Error("Expected error for duplicate field names")
	}
}

// Test more scenarios for coverage
func TestAnalyzer_AdditionalScenarios(t *testing.T) {
	analyzer := reflection.New()

	// Test GetServiceType with function that only returns error
	errorOnlyFunc := func() error {
		return nil
	}

	_, err := analyzer.GetServiceType(errorOnlyFunc)
	if err == nil {
		t.Error("Expected error for function that only returns error")
	}

	// Test GetResultTypes with non-result object
	simpleFunc := func() *Database {
		return &Database{}
	}

	types, err := analyzer.GetResultTypes(simpleFunc)
	if err != nil {
		t.Fatalf("GetResultTypes failed: %v", err)
	}

	if len(types) != 1 {
		t.Errorf("Expected 1 type, got %d", len(types))
	}

	if types[0] != reflect.TypeOf((*Database)(nil)) {
		t.Error("Wrong type returned")
	}

	// Test with a type that doesn't embed In/Out
	// This verifies the analyzer correctly identifies non-In/Out types
	type NotInOut struct {
		Field string
	}

	notInOutFunc := func(n NotInOut) int {
		return 0
	}

	info, err := analyzer.Analyze(notInOutFunc)
	if err != nil {
		t.Fatalf("Failed to analyze: %v", err)
	}

	// Should not be detected as param object
	if info.IsParamObject {
		t.Error("Function with non-In parameter should not be detected as param object")
	}
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

	paramType := reflect.TypeOf(ComplexParams{})
	result, err := builder.BuildParamObject(paramType, resolver)
	if err != nil {
		t.Fatalf("BuildParamObject failed: %v", err)
	}

	params := result.Interface().(ComplexParams)

	// Verify all fields
	if params.Required != mainDB {
		t.Error("Required field not set correctly")
	}

	if params.Optional != logger {
		t.Error("Optional field not set correctly")
	}

	if params.Named != backupDB {
		t.Error("Named field not set correctly")
	}

	if len(params.Grouped) != 2 {
		t.Errorf("Expected 2 handlers, got %d", len(params.Grouped))
	}

	if params.Ignored != "" {
		t.Error("Ignored field should not be set")
	}

	if params.Combination != specialDB {
		t.Error("Combination field not set correctly")
	}

	// Test with failed optional dependency
	failResolver := NewTestResolver()
	failResolver.shouldFail = true
	failResolver.failError = errors.New("optional dep failed")

	// Should fail when required dependencies can't be resolved
	_, err = builder.BuildParamObject(paramType, failResolver)
	if err == nil {
		t.Error("Should fail when required dependencies can't be resolved")
	}

	// Test with only optional fields failing
	type OnlyOptional struct {
		reflection.In
		Optional1 *Database `optional:"true"`
		Optional2 Logger    `optional:"true"`
	}

	optType := reflect.TypeOf(OnlyOptional{})
	result3, err := builder.BuildParamObject(optType, failResolver)
	if err != nil {
		t.Errorf("Should succeed with only optional fields failing: %v", err)
	}

	optParams := result3.Interface().(OnlyOptional)
	if optParams.Optional1 != nil || optParams.Optional2 != nil {
		t.Error("Optional fields should be nil when resolution fails")
	}
}

// Test TypeFormatter with more complex scenarios
func TestTypeFormatter_MoreComplexScenarios(t *testing.T) {
	formatter := &reflection.TypeFormatter{}

	t.Run("Complex nested types", func(t *testing.T) {
		// Test with nested slice of maps
		complexType := reflect.TypeOf([]map[string][]int{})
		result := formatter.FormatType(complexType)
		expected := "[]map[string][]int"
		if result != expected {
			t.Errorf("FormatType() = %q, want %q", result, expected)
		}

		// Test with function returning multiple complex types
		funcType := reflect.TypeOf(func() (map[string]*Database, []Logger, error) {
			return nil, nil, nil
		})
		result = formatter.FormatType(funcType)
		expected = "func() (map[string]*Database, []Logger, error)"
		if result != expected {
			t.Errorf("FormatType() = %q, want %q", result, expected)
		}
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
		if err != nil {
			t.Fatalf("Failed to analyze: %v", err)
		}

		result := formatter.FormatConstructor(info)
		// Should contain all the field annotations
		if !contains(result, "optional") {
			t.Error("FormatConstructor should include optional tag")
		}
		if !contains(result, "name:") {
			t.Error("FormatConstructor should include name tag")
		}
		if !contains(result, "group:") {
			t.Error("FormatConstructor should include group tag")
		}
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
		if err != nil {
			t.Fatalf("Failed to analyze: %v", err)
		}

		err = validator.Validate(info)
		if err == nil {
			t.Error("Expected error for Out struct with no exported fields")
		}
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

		info1, _ := analyzer.Analyze(constructor1)
		err := validator.Validate(info1)
		if err != nil {
			t.Errorf("Valid Out struct should pass validation: %v", err)
		}

		// Test with Out and error
		constructor2 := func() (ValidOut, error) {
			return ValidOut{}, nil
		}

		info2, _ := analyzer.Analyze(constructor2)
		err = validator.Validate(info2)
		if err != nil {
			t.Errorf("Valid Out struct with error should pass validation: %v", err)
		}

		// Test with Out and non-error second return
		constructor3 := func() (ValidOut, string) {
			return ValidOut{}, ""
		}

		info3, _ := analyzer.Analyze(constructor3)
		err = validator.Validate(info3)
		if err == nil {
			t.Error("Out struct with non-error second return should fail validation")
		}
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
			Type:           reflect.TypeOf(func() (SimpleOut, error, string) { return SimpleOut{}, nil, "" }),
			Returns: []reflection.ReturnInfo{
				{Type: reflect.TypeOf(SimpleOut{})},
			},
		}

		err := validator.Validate(mockInfo)
		if err == nil {
			t.Error("Expected error for Out struct with more than 2 returns")
		}
	})
}
