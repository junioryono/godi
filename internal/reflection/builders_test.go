package reflection_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/junioryono/godi/v3/internal/reflection"
)

// Test types for ResultObjectProcessor
type TestOutStruct struct {
	reflection.Out

	Service1   *Database
	Service2   Logger    `name:"logger2"`
	Service3   *Database `group:"databases"`
	Service4   func()    `group:"handlers"`
	Ignored    string    `inject:"-"`
	unexported string
	NilField   *Database
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

			if (err != nil) != tt.wantErr {
				t.Errorf("ProcessResultObject() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && len(registrations) != tt.wantCount {
				t.Errorf("ProcessResultObject() returned %d registrations, want %d", len(registrations), tt.wantCount)
			}

			// Verify registration details for valid cases
			if !tt.wantErr && len(registrations) > 0 {
				// Check that ignored and unexported fields are not included
				for _, reg := range registrations {
					if reg.Name == "Ignored" || reg.Name == "unexported" {
						t.Errorf("Found ignored or unexported field in registrations: %s", reg.Name)
					}

					// Check key and group are properly set
					if reg.Name == "Service2" && reg.Key != "logger2" {
						t.Errorf("Service2 should have key 'logger2', got %v", reg.Key)
					}
					if reg.Name == "Service3" && reg.Group != "databases" {
						t.Errorf("Service3 should have group 'databases', got %s", reg.Group)
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
	if t.Kind() == reflect.Ptr {
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
				if len(results) != 1 {
					t.Errorf("Expected 1 result, got %d", len(results))
					return
				}
				db := results[0].Interface().(*Database)
				if db.ConnectionString != "connection-string" {
					t.Errorf("Expected connection string 'connection-string', got %s", db.ConnectionString)
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
				if len(results) != 1 {
					t.Errorf("Expected 1 result, got %d", len(results))
					return
				}
				svc := results[0].Interface().(*UserService)
				if svc.DB != db {
					t.Error("Database not properly injected")
				}
				if svc.Logger != logger {
					t.Error("Logger not properly injected")
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
				if len(results) != 2 {
					t.Errorf("Expected 2 results, got %d", len(results))
					return
				}
				if !results[1].IsNil() {
					t.Error("Expected error to be nil")
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
				if len(results) != 1 {
					t.Errorf("Expected 1 result, got %d", len(results))
					return
				}
				svc := results[0].Interface().(*UserService)
				if svc.DB == nil {
					t.Error("Database should not be nil")
				}
			},
		},
		{
			name:          "non-function value",
			constructor:   &Database{ConnectionString: "static"},
			setupResolver: func(r *TestResolver) {},
			wantErr:       false,
			validate: func(t *testing.T, results []reflect.Value) {
				if len(results) != 1 {
					t.Errorf("Expected 1 result, got %d", len(results))
					return
				}
				db := results[0].Interface().(*Database)
				if db.ConnectionString != "static" {
					t.Errorf("Expected 'static', got %s", db.ConnectionString)
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
			if err != nil {
				t.Fatalf("Failed to analyze constructor: %v", err)
			}

			// Invoke constructor
			results, err := invoker.Invoke(info, testResolver)

			if (err != nil) != tt.wantErr {
				t.Errorf("Invoke() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, results)
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

	// Test constructor with groups
	type GroupParams struct {
		reflection.In
		Handlers []func() `group:"handlers"`
	}

	constructor := func(params GroupParams) int {
		return len(params.Handlers)
	}

	info, err := analyzer.Analyze(constructor)
	if err != nil {
		t.Fatalf("Failed to analyze constructor: %v", err)
	}

	results, err := invoker.Invoke(info, resolver)
	if err != nil {
		t.Fatalf("Failed to invoke: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}

	count := results[0].Interface().(int)
	if count != 2 {
		t.Errorf("Expected 2 handlers, got %d", count)
	}

	// Test constructor with keyed dependency
	type KeyedParams struct {
		reflection.In
		MainDB   *Database
		BackupDB *Database `name:"backup"`
	}

	keyedConstructor := func(params KeyedParams) bool {
		return params.MainDB != params.BackupDB
	}

	info2, err := analyzer.Analyze(keyedConstructor)
	if err != nil {
		t.Fatalf("Failed to analyze keyed constructor: %v", err)
	}

	results2, err := invoker.Invoke(info2, resolver)
	if err != nil {
		t.Fatalf("Failed to invoke keyed constructor: %v", err)
	}

	if len(results2) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results2))
	}

	different := results2[0].Interface().(bool)
	if !different {
		t.Error("MainDB and BackupDB should be different instances")
	}
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

			if (err != nil) != tt.wantErr {
				t.Errorf("BuildParamObject() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if err != nil && tt.errMsg != "" {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("Error message should contain %q, got %q", tt.errMsg, err.Error())
				}
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

	nonPtrType := reflect.TypeOf(NonPointerParams{})
	val1, err := builder.BuildParamObject(nonPtrType, resolver)
	if err != nil {
		t.Fatalf("Failed with non-pointer type: %v", err)
	}

	if val1.Kind() == reflect.Ptr {
		params := val1.Elem().Interface().(NonPointerParams)
		if params.DB != db {
			t.Error("DB not properly set in non-pointer params")
		}
	} else {
		params := val1.Interface().(NonPointerParams)
		if params.DB != db {
			t.Error("DB not properly set in non-pointer params")
		}
	}

	// Test with pointer struct type
	ptrType := reflect.TypeOf(&NonPointerParams{})
	val2, err := builder.BuildParamObject(ptrType, resolver)
	if err != nil {
		t.Fatalf("Failed with pointer type: %v", err)
	}

	if val2.Kind() != reflect.Ptr {
		t.Error("Expected pointer result for pointer type")
	}
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
		if err == nil {
			t.Error("Expected error when group resolution fails")
		}
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
		if err == nil {
			t.Error("Expected error when named field resolution fails")
		}
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
		if err == nil {
			t.Error("Expected error when required field resolution fails")
		}

		// Now test with only optional fields failing
		resolver2 := NewTestResolver()
		resolver2.values[reflect.TypeOf((*Database)(nil))] = &Database{ConnectionString: "test"}
		resolver2.keyedValues["backup"] = &Database{ConnectionString: "backup"}
		// Don't provide Logger and handlers - they're optional

		result, err := builder.BuildParamObject(paramType, resolver2)
		if err != nil {
			t.Errorf("Should succeed when only optional fields are missing: %v", err)
		}

		params := result.Interface().(MixedParams)
		if params.Required1 == nil || params.Required2 == nil {
			t.Error("Required fields should be set")
		}
		if params.Optional1 != nil {
			t.Error("Optional1 should be nil when not resolved")
		}
		if len(params.Optional2) > 0 {
			t.Error("Optional2 should be nil or empty when not resolved")
		}
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
		if err != nil {
			t.Fatalf("Failed to analyze: %v", err)
		}

		resolver := NewTestResolver()
		_, err = invoker.Invoke(info, resolver)
		if err == nil {
			t.Error("Expected error when building invalid param object")
		}
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
		if err != nil {
			t.Fatalf("Failed to invoke: %v", err)
		}

		if len(results) != 1 {
			t.Errorf("Expected 1 result, got %d", len(results))
		}

		// The function returns the count of handlers
		count := results[0].Interface().(int)
		if count != 2 {
			t.Errorf("Expected 2 handlers, got %d", count)
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
		if err != nil {
			t.Fatalf("Failed to invoke: %v", err)
		}

		connStr := results[0].Interface().(string)
		if connStr != "backup-db" {
			t.Errorf("Expected 'backup-db', got %s", connStr)
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
		if err != nil {
			t.Fatalf("ProcessResultObject failed: %v", err)
		}

		// Should only have the Service field, not the embedded Out fields
		if len(registrations) != 1 {
			t.Errorf("Expected 1 registration, got %d", len(registrations))
		}

		if registrations[0].Name != "Service" {
			t.Errorf("Expected Service field, got %s", registrations[0].Name)
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
		if err != nil {
			t.Fatalf("ProcessResultObject failed: %v", err)
		}

		// Should include ValidPtr, ZeroValue, and EmptySlice (even if empty)
		// NilPtr should be skipped
		expectedCount := 3
		if len(registrations) != expectedCount {
			t.Errorf("Expected %d registrations, got %d", expectedCount, len(registrations))
		}

		// Verify NilPtr is not included
		for _, reg := range registrations {
			if reg.Name == "NilPtr" {
				t.Error("Nil pointer field should not be included in registrations")
			}
		}
	})
}
