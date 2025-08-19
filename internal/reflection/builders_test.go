package reflection_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/junioryono/godi/v4/internal/reflection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test types for ResultObjectProcessor
type TestOutStruct struct {
	reflection.Out

	Service1 *Database
	Service2 Logger    `name:"logger2"`
	Service3 *Database `group:"databases"`
	Service4 func()    `group:"handlers"`
	Ignored  string    `inject:"-"`
	_        string    // unexported field for testing
	NilField *Database
}

// Test ResultObjectProcessor
func TestResultObjectProcessor(t *testing.T) {
	analyzer := reflection.New()
	processor := reflection.NewResultObjectProcessor(analyzer)

	tests := []struct {
		name      string
		input     any
		wantErr   bool
		wantCount int
	}{
		{
			name: "valid out struct",
			input: TestOutStruct{
				Service1: &Database{ConnectionString: "db1"},
				Service2: &ConsoleLogger{},
				Service3: &Database{ConnectionString: "db3"},
				Service4: func() {},
			},
			wantErr:   false,
			wantCount: 4, // 4 non-nil, non-ignored fields
		},
		{
			name: "out struct with nil fields",
			input: TestOutStruct{
				Service1: &Database{ConnectionString: "db1"},
				// Service2 is nil interface
				// Service3 is nil pointer
				Service4: func() {},
				// NilField is nil pointer
			},
			wantErr:   false,
			wantCount: 2, // Only Service1 and Service4 (non-nil values)
		},
		{
			name: "pointer to out struct",
			input: &TestOutStruct{
				Service1: &Database{ConnectionString: "db1"},
				Service2: &ConsoleLogger{},
			},
			wantErr:   false,
			wantCount: 2, // Service1 and Service2 (non-nil values)
		},
		{
			name:      "nil pointer to out struct",
			input:     (*TestOutStruct)(nil),
			wantErr:   true,
			wantCount: 0,
		},
		{
			name:      "non-struct type",
			input:     42,
			wantErr:   true,
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val := reflect.ValueOf(tt.input)
			typ := reflect.TypeOf(tt.input)

			registrations, err := processor.ProcessResultObject(val, typ)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Len(t, registrations, tt.wantCount, "Unexpected number of registrations")
			}

			// Verify registration details for valid cases
			if !tt.wantErr && len(registrations) > 0 {
				// Check that ignored and unexported fields are not included
				for _, reg := range registrations {
					assert.NotEqual(t, "Ignored", reg.Name, "Ignored field should not be in registrations")
					assert.NotEqual(t, "unexported", reg.Name, "Unexported field should not be in registrations")

					// Check key and group are properly set
					if reg.Name == "Service2" {
						assert.Equal(t, "logger2", reg.Key, "Service2 should have key 'logger2'")
					}
					if reg.Name == "Service3" {
						assert.Equal(t, "databases", reg.Group, "Service3 should have group 'databases'")
					}
				}
			}
		})
	}
}

// Mock resolver for testing ConstructorInvoker
type TestResolver struct {
	values      map[reflect.Type]any
	keyedValues map[string]any
	groups      map[string][]any
	shouldFail  bool
	failError   error
}

func NewTestResolver() *TestResolver {
	return &TestResolver{
		values:      make(map[reflect.Type]any),
		keyedValues: make(map[string]any),
		groups:      make(map[string][]any),
	}
}

func (r *TestResolver) Get(t reflect.Type) (any, error) {
	if r.shouldFail {
		return nil, r.failError
	}
	if val, ok := r.values[t]; ok {
		return val, nil
	}
	// Create a new instance as fallback
	if t.Kind() == reflect.Pointer {
		return reflect.New(t.Elem()).Interface(), nil
	}
	return reflect.Zero(t).Interface(), nil
}

func (r *TestResolver) GetKeyed(t reflect.Type, key any) (any, error) {
	if r.shouldFail {
		return nil, r.failError
	}
	keyStr := fmt.Sprintf("%v", key)
	if val, ok := r.keyedValues[keyStr]; ok {
		return val, nil
	}
	return r.Get(t)
}

func (r *TestResolver) GetGroup(t reflect.Type, group string) ([]any, error) {
	if r.shouldFail {
		return nil, r.failError
	}
	if vals, ok := r.groups[group]; ok {
		return vals, nil
	}
	return []any{}, nil
}

// Test ConstructorInvoker
func TestConstructorInvoker(t *testing.T) {
	analyzer := reflection.New()
	invoker := reflection.NewConstructorInvoker(analyzer)

	// Setup test resolver
	resolver := NewTestResolver()
	db := &Database{ConnectionString: "test"}
	logger := &ConsoleLogger{}
	resolver.values[reflect.TypeOf((*Database)(nil))] = db
	resolver.values[reflect.TypeOf((*Logger)(nil)).Elem()] = logger
	resolver.groups["handlers"] = []any{
		func() { fmt.Println("handler1") },
		func() { fmt.Println("handler2") },
	}

	tests := []struct {
		name          string
		constructor   any
		setupResolver func(*TestResolver)
		wantErr       bool
		validate      func(*testing.T, []reflect.Value)
	}{
		{
			name:        "simple constructor",
			constructor: NewDatabase,
			setupResolver: func(r *TestResolver) {
				r.values[reflect.TypeOf("")] = "connection-string"
			},
			wantErr: false,
			validate: func(t *testing.T, results []reflect.Value) {
				assert.Len(t, results, 1, "Expected 1 result")
				if len(results) > 0 {
					db := results[0].Interface().(*Database)
					assert.Equal(t, "connection-string", db.ConnectionString)
				}
			},
		},
		{
			name:        "constructor with multiple params",
			constructor: NewUserService,
			setupResolver: func(r *TestResolver) {
				// Already set up in resolver
			},
			wantErr: false,
			validate: func(t *testing.T, results []reflect.Value) {
				assert.Len(t, results, 1, "Expected 1 result")
				if len(results) > 0 {
					svc := results[0].Interface().(*UserService)
					assert.Equal(t, db, svc.DB, "Database not properly injected")
					assert.Equal(t, logger, svc.Logger, "Logger not properly injected")
				}
			},
		},
		{
			name:        "constructor with error return",
			constructor: NewUserServiceWithError,
			setupResolver: func(r *TestResolver) {
				// Already set up
			},
			wantErr: false,
			validate: func(t *testing.T, results []reflect.Value) {
				assert.Len(t, results, 2, "Expected 2 results")
				if len(results) > 1 {
					assert.True(t, results[1].IsNil(), "Expected error to be nil")
				}
			},
		},
		{
			name:        "constructor with error - nil database",
			constructor: NewUserServiceWithError,
			setupResolver: func(r *TestResolver) {
				r.values[reflect.TypeOf((*Database)(nil))] = (*Database)(nil)
			},
			wantErr: true, // Constructor returns error for nil database
		},
		{
			name:        "constructor with param object",
			constructor: NewServiceWithParams,
			setupResolver: func(r *TestResolver) {
				r.keyedValues["cache"] = &Database{ConnectionString: "cache"}
			},
			wantErr: false,
			validate: func(t *testing.T, results []reflect.Value) {
				assert.Len(t, results, 1, "Expected 1 result")
				if len(results) > 0 {
					svc := results[0].Interface().(*UserService)
					assert.NotNil(t, svc.DB, "Database should not be nil")
				}
			},
		},
		{
			name:          "non-function value",
			constructor:   &Database{ConnectionString: "static"},
			setupResolver: func(r *TestResolver) {},
			wantErr:       false,
			validate: func(t *testing.T, results []reflect.Value) {
				assert.Len(t, results, 1, "Expected 1 result")
				if len(results) > 0 {
					db := results[0].Interface().(*Database)
					assert.Equal(t, "static", db.ConnectionString)
				}
			},
		},
		{
			name:        "failed dependency resolution",
			constructor: NewUserService,
			setupResolver: func(r *TestResolver) {
				r.shouldFail = true
				r.failError = errors.New("dependency not found")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a new resolver with copied data
			testResolver := NewTestResolver()
			// Copy base resolver's data
			for k, v := range resolver.values {
				testResolver.values[k] = v
			}
			for k, v := range resolver.keyedValues {
				testResolver.keyedValues[k] = v
			}
			for k, v := range resolver.groups {
				testResolver.groups[k] = v
			}

			// Apply test-specific setup
			tt.setupResolver(testResolver)

			// Analyze constructor
			info, err := analyzer.Analyze(tt.constructor)
			require.NoError(t, err, "Failed to analyze constructor")

			// Invoke constructor
			results, err := invoker.Invoke(info, testResolver)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, results)
				}
			}
		})
	}
}

// Test resolveParameter with groups and keys
func TestConstructorInvoker_ResolveParameter(t *testing.T) {
	analyzer := reflection.New()
	invoker := reflection.NewConstructorInvoker(analyzer)

	resolver := NewTestResolver()

	// Setup some test values
	db1 := &Database{ConnectionString: "db1"}
	db2 := &Database{ConnectionString: "db2"}
	handler1 := func() { fmt.Println("h1") }
	handler2 := func() { fmt.Println("h2") }

	resolver.values[reflect.TypeOf((*Database)(nil))] = db1
	resolver.keyedValues["backup"] = db2
	resolver.groups["handlers"] = []any{handler1, handler2}

	t.Run("constructor with groups", func(t *testing.T) {
		// Test constructor with groups
		type GroupParams struct {
			reflection.In
			Handlers []func() `group:"handlers"`
		}

		constructor := func(params GroupParams) int {
			return len(params.Handlers)
		}

		info, err := analyzer.Analyze(constructor)
		require.NoError(t, err, "Failed to analyze constructor")

		results, err := invoker.Invoke(info, resolver)
		require.NoError(t, err, "Failed to invoke")

		assert.Len(t, results, 1, "Expected 1 result")
		if len(results) > 0 {
			count := results[0].Interface().(int)
			assert.Equal(t, 2, count, "Expected 2 handlers")
		}
	})

	t.Run("constructor with keyed dependency", func(t *testing.T) {
		// Test constructor with keyed dependency
		type KeyedParams struct {
			reflection.In
			MainDB   *Database
			BackupDB *Database `name:"backup"`
		}

		keyedConstructor := func(params KeyedParams) bool {
			return params.MainDB != params.BackupDB
		}

		info, err := analyzer.Analyze(keyedConstructor)
		require.NoError(t, err, "Failed to analyze keyed constructor")

		results, err := invoker.Invoke(info, resolver)
		require.NoError(t, err, "Failed to invoke keyed constructor")

		assert.Len(t, results, 1, "Expected 1 result")
		if len(results) > 0 {
			different := results[0].Interface().(bool)
			assert.True(t, different, "MainDB and BackupDB should be different instances")
		}
	})
}

// Test edge cases in BuildParamObject
func TestParamObjectBuilder_EdgeCases(t *testing.T) {
	analyzer := reflection.New()
	builder := reflection.NewParamObjectBuilder(analyzer)

	tests := []struct {
		name      string
		paramType reflect.Type
		resolver  reflection.DependencyResolver
		wantErr   bool
		errMsg    string
	}{
		{
			name:      "nil resolver",
			paramType: reflect.TypeOf(ServiceParams{}),
			resolver:  nil,
			wantErr:   true,
			errMsg:    "resolver cannot be nil",
		},
		{
			name:      "nil param type",
			paramType: nil,
			resolver:  NewTestResolver(),
			wantErr:   true,
			errMsg:    "paramType cannot be nil",
		},
		{
			name:      "non-struct type",
			paramType: reflect.TypeOf(42),
			resolver:  NewTestResolver(),
			wantErr:   true,
			errMsg:    "param type must be struct",
		},
		{
			name: "struct with invalid group field",
			paramType: reflect.TypeOf(struct {
				reflection.In
				InvalidGroup string `group:"invalid"` // group on non-slice
			}{}),
			resolver: NewTestResolver(),
			wantErr:  true,
			errMsg:   "group field must be slice",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := builder.BuildParamObject(tt.paramType, tt.resolver)

			if tt.wantErr {
				assert.Error(t, err)
				if tt.errMsg != "" {
					assert.Contains(t, err.Error(), tt.errMsg)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// Test pointer vs non-pointer param types
func TestParamObjectBuilder_PointerTypes(t *testing.T) {
	analyzer := reflection.New()
	builder := reflection.NewParamObjectBuilder(analyzer)

	resolver := NewTestResolver()
	db := &Database{ConnectionString: "test"}
	resolver.values[reflect.TypeOf((*Database)(nil))] = db

	// Test with non-pointer struct type
	type NonPointerParams struct {
		reflection.In
		DB *Database
	}

	t.Run("non-pointer type", func(t *testing.T) {
		nonPtrType := reflect.TypeOf(NonPointerParams{})
		val, err := builder.BuildParamObject(nonPtrType, resolver)
		require.NoError(t, err, "Failed with non-pointer type")

		if val.Kind() == reflect.Pointer {
			params := val.Elem().Interface().(NonPointerParams)
			assert.Equal(t, db, params.DB, "DB not properly set in non-pointer params")
		} else {
			params := val.Interface().(NonPointerParams)
			assert.Equal(t, db, params.DB, "DB not properly set in non-pointer params")
		}
	})

	t.Run("pointer type", func(t *testing.T) {
		// Test with pointer struct type
		ptrType := reflect.TypeOf(&NonPointerParams{})
		val, err := builder.BuildParamObject(ptrType, resolver)
		require.NoError(t, err, "Failed with pointer type")

		assert.Equal(t, reflect.Pointer, val.Kind(), "Expected pointer result for pointer type")
	})
}

// Test ParamObjectBuilder with more error scenarios
func TestParamObjectBuilder_MoreErrorScenarios(t *testing.T) {
	analyzer := reflection.New()
	builder := reflection.NewParamObjectBuilder(analyzer)

	t.Run("Group field resolution error", func(t *testing.T) {
		type GroupParams struct {
			reflection.In
			Handlers []func() `group:"handlers"`
		}

		resolver := NewTestResolver()
		resolver.shouldFail = true
		resolver.failError = errors.New("group resolution failed")

		paramType := reflect.TypeOf(GroupParams{})
		_, err := builder.BuildParamObject(paramType, resolver)
		assert.Error(t, err, "Expected error when group resolution fails")
	})

	t.Run("Named field resolution error", func(t *testing.T) {
		type NamedParams struct {
			reflection.In
			Cache *Database `name:"cache"`
		}

		resolver := NewTestResolver()
		// Make keyed resolution fail
		resolver.shouldFail = true
		resolver.failError = errors.New("keyed resolution failed")

		paramType := reflect.TypeOf(NamedParams{})
		_, err := builder.BuildParamObject(paramType, resolver)
		assert.Error(t, err, "Expected error when named field resolution fails")
	})

	t.Run("Mixed optional and required fields", func(t *testing.T) {
		type MixedParams struct {
			reflection.In
			Required1 *Database
			Optional1 Logger    `optional:"true"`
			Required2 *Database `name:"backup"`
			Optional2 []func()  `group:"handlers" optional:"true"`
		}

		// Test with failing required fields
		failingResolver := NewTestResolver()
		failingResolver.shouldFail = true
		failingResolver.failError = errors.New("database not found")

		paramType := reflect.TypeOf(MixedParams{})
		_, err := builder.BuildParamObject(paramType, failingResolver)
		assert.Error(t, err, "Expected error when required field resolution fails")

		// Now test with only optional fields failing
		resolver2 := NewTestResolver()
		resolver2.values[reflect.TypeOf((*Database)(nil))] = &Database{ConnectionString: "test"}
		resolver2.keyedValues["backup"] = &Database{ConnectionString: "backup"}
		// Don't provide Logger and handlers - they're optional

		result, err := builder.BuildParamObject(paramType, resolver2)
		require.NoError(t, err, "Should succeed when only optional fields are missing")

		params := result.Interface().(MixedParams)
		assert.NotNil(t, params.Required1, "Required1 field should be set")
		assert.NotNil(t, params.Required2, "Required2 field should be set")
		assert.Nil(t, params.Optional1, "Optional1 should be nil when not resolved")
		assert.Empty(t, params.Optional2, "Optional2 should be empty when not resolved")
	})
}

// Test ConstructorInvoker with more param object edge cases
func TestConstructorInvoker_MoreParamObjectEdgeCases(t *testing.T) {
	analyzer := reflection.New()
	invoker := reflection.NewConstructorInvoker(analyzer)

	t.Run("Param object with error in builder", func(t *testing.T) {
		type BadParams struct {
			reflection.In
			InvalidGroup int `group:"numbers"` // Invalid: non-slice with group
		}

		constructor := func(params BadParams) int { return 0 }

		info, err := analyzer.Analyze(constructor)
		require.NoError(t, err, "Failed to analyze")

		resolver := NewTestResolver()
		_, err = invoker.Invoke(info, resolver)
		assert.Error(t, err, "Expected error when building invalid param object")
	})

	t.Run("Regular parameters with groups", func(t *testing.T) {
		// Create a function that takes a slice parameter directly
		// This tests the resolveParameter path for groups
		sliceConstructor := func(handlers []func()) int {
			return len(handlers)
		}

		// Manually create info with group parameter
		info := &reflection.ConstructorInfo{
			Type:   reflect.TypeOf(sliceConstructor),
			Value:  reflect.ValueOf(sliceConstructor),
			IsFunc: true,
			Parameters: []reflection.ParameterInfo{
				{
					Type:     reflect.TypeOf([]func(){}),
					Index:    0,
					Group:    "handlers",
					IsSlice:  true,
					ElemType: reflect.TypeOf(func() {}),
				},
			},
			Returns: []reflection.ReturnInfo{
				{Type: reflect.TypeOf(0), Index: 0},
			},
		}

		resolver := NewTestResolver()
		resolver.groups["handlers"] = []any{
			func() { println("h1") },
			func() { println("h2") },
		}

		results, err := invoker.Invoke(info, resolver)
		require.NoError(t, err, "Failed to invoke")

		assert.Len(t, results, 1, "Expected 1 result")
		if len(results) > 0 {
			// The function returns the count of handlers
			count := results[0].Interface().(int)
			assert.Equal(t, 2, count, "Expected 2 handlers")
		}
	})

	t.Run("Regular parameters with keys", func(t *testing.T) {
		// Create constructor info with keyed parameter
		keyedConstructor := func(db *Database) string {
			return db.ConnectionString
		}

		info := &reflection.ConstructorInfo{
			Type:   reflect.TypeOf(keyedConstructor),
			Value:  reflect.ValueOf(keyedConstructor),
			IsFunc: true,
			Parameters: []reflection.ParameterInfo{
				{
					Type:  reflect.TypeOf((*Database)(nil)),
					Index: 0,
					Key:   "backup",
				},
			},
			Returns: []reflection.ReturnInfo{
				{Type: reflect.TypeOf(""), Index: 0},
			},
		}

		resolver := NewTestResolver()
		resolver.keyedValues["backup"] = &Database{ConnectionString: "backup-db"}

		results, err := invoker.Invoke(info, resolver)
		require.NoError(t, err, "Failed to invoke")

		assert.Len(t, results, 1, "Expected 1 result")
		if len(results) > 0 {
			connStr := results[0].Interface().(string)
			assert.Equal(t, "backup-db", connStr)
		}
	})
}

// Test ResultObjectProcessor with additional edge cases
func TestResultObjectProcessor_AdditionalEdgeCases(t *testing.T) {
	analyzer := reflection.New()
	processor := reflection.NewResultObjectProcessor(analyzer)

	t.Run("Embedded Out field handling", func(t *testing.T) {
		// Test that embedded Out field itself is skipped
		type EmbeddedOut struct {
			reflection.Out
			Service *Database
		}

		val := reflect.ValueOf(EmbeddedOut{Service: &Database{ConnectionString: "test"}})
		typ := reflect.TypeOf(EmbeddedOut{})

		registrations, err := processor.ProcessResultObject(val, typ)
		require.NoError(t, err, "ProcessResultObject failed")

		// Should only have the Service field, not the embedded Out fields
		assert.Len(t, registrations, 1, "Expected 1 registration")
		if len(registrations) > 0 {
			assert.Equal(t, "Service", registrations[0].Name, "Expected Service field")
		}
	})

	t.Run("Invalid field values", func(t *testing.T) {
		type InvalidOut struct {
			reflection.Out
			ValidPtr   *Database
			NilPtr     *Database
			ZeroValue  int
			EmptySlice []func()
		}

		out := InvalidOut{
			ValidPtr:   &Database{ConnectionString: "valid"},
			NilPtr:     nil,
			ZeroValue:  0,
			EmptySlice: []func(){},
		}

		val := reflect.ValueOf(out)
		typ := reflect.TypeOf(out)

		registrations, err := processor.ProcessResultObject(val, typ)
		require.NoError(t, err, "ProcessResultObject failed")

		// Should include ValidPtr, ZeroValue, and EmptySlice (even if empty)
		// NilPtr should be skipped
		expectedCount := 3
		assert.Len(t, registrations, expectedCount, "Unexpected number of registrations")

		// Verify NilPtr is not included
		for _, reg := range registrations {
			assert.NotEqual(t, "NilPtr", reg.Name, "Nil pointer field should not be included in registrations")
		}
	})
}
