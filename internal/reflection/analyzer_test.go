package reflection_test

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/junioryono/godi/v4/internal/reflection"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test types
type Database struct {
	ConnectionString string
}

type Logger interface {
	Log(msg string)
}

type ConsoleLogger struct{}

func (c *ConsoleLogger) Log(msg string) {}

type UserService struct {
	DB     *Database
	Logger Logger
}

// Test constructors
func NewDatabase(connStr string) *Database {
	return &Database{ConnectionString: connStr}
}

func NewUserService(db *Database, logger Logger) *UserService {
	return &UserService{DB: db, Logger: logger}
}

func NewUserServiceWithError(db *Database) (*UserService, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &UserService{DB: db}, nil
}

// In parameter object
type ServiceParams struct {
	reflection.In

	Database *Database
	Logger   Logger    `optional:"true"`
	Cache    *Database `name:"cache"`
	Handlers []func()  `group:"handlers"`
}

func NewServiceWithParams(params ServiceParams) *UserService {
	return &UserService{
		DB:     params.Database,
		Logger: params.Logger,
	}
}

// Out result object
type ServiceResults struct {
	reflection.Out

	UserSvc  *UserService
	AdminSvc *UserService `name:"admin"`
	Handler  func()       `group:"handlers"`
}

func NewServices(db *Database) ServiceResults {
	return ServiceResults{
		UserSvc:  &UserService{DB: db},
		AdminSvc: &UserService{DB: db},
		Handler:  func() {},
	}
}

func NewServicesWithError(db *Database) (ServiceResults, error) {
	if db == nil {
		return ServiceResults{}, errors.New("database required")
	}
	return NewServices(db), nil
}

func TestAnalyzer_SimpleConstructor(t *testing.T) {
	analyzer := reflection.New()

	info, err := analyzer.Analyze(NewDatabase)
	require.NoError(t, err, "Failed to analyze constructor")

	assert.True(t, info.IsFunc, "Expected IsFunc to be true")
	assert.False(t, info.IsParamObject, "Expected IsParamObject to be false")
	assert.False(t, info.IsResultObject, "Expected IsResultObject to be false")

	// Check parameters
	assert.Len(t, info.Parameters, 1, "Expected 1 parameter")
	assert.Equal(t, reflect.TypeOf(""), info.Parameters[0].Type, "Expected string parameter type")

	// Check returns
	assert.Len(t, info.Returns, 1, "Expected 1 return value")
	assert.Equal(t, reflect.TypeOf((*Database)(nil)), info.Returns[0].Type, "Expected *Database return type")
}

func TestAnalyzer_ConstructorWithMultipleParams(t *testing.T) {
	analyzer := reflection.New()

	info, err := analyzer.Analyze(NewUserService)
	require.NoError(t, err, "Failed to analyze constructor")

	// Check parameters
	assert.Len(t, info.Parameters, 2, "Expected 2 parameters")
	assert.Equal(t, reflect.TypeOf((*Database)(nil)), info.Parameters[0].Type, "Expected first parameter to be *Database")
	assert.Equal(t, reflect.TypeOf((*Logger)(nil)).Elem(), info.Parameters[1].Type, "Expected second parameter to be Logger interface")

	// Check dependencies
	deps, err := analyzer.GetDependencies(NewUserService)
	require.NoError(t, err, "Failed to get dependencies")
	assert.Len(t, deps, 2, "Expected 2 dependencies")
}

func TestAnalyzer_ConstructorWithError(t *testing.T) {
	analyzer := reflection.New()

	info, err := analyzer.Analyze(NewUserServiceWithError)
	require.NoError(t, err, "Failed to analyze constructor")

	assert.True(t, info.HasErrorReturn, "Expected HasErrorReturn to be true")

	// Check returns
	assert.Len(t, info.Returns, 2, "Expected 2 return values")
	assert.True(t, info.Returns[1].IsError, "Expected second return to be error")
}

func TestAnalyzer_ParamObject(t *testing.T) {
	analyzer := reflection.New()

	info, err := analyzer.Analyze(NewServiceWithParams)
	require.NoError(t, err, "Failed to analyze constructor with param object")

	assert.True(t, info.IsParamObject, "Expected IsParamObject to be true")

	// Check extracted parameters from struct fields
	assert.Len(t, info.Parameters, 4, "Expected 4 parameters from struct fields")

	// Find and check each field
	var dbParam, loggerParam, cacheParam, handlersParam *reflection.ParameterInfo

	for i := range info.Parameters {
		param := &info.Parameters[i]
		switch param.Name {
		case "Database":
			dbParam = param
		case "Logger":
			loggerParam = param
		case "Cache":
			cacheParam = param
		case "Handlers":
			handlersParam = param
		}
	}

	// Check Database field
	require.NotNil(t, dbParam, "Database field not found")
	assert.False(t, dbParam.Optional, "Database should not be optional")

	// Check Logger field
	require.NotNil(t, loggerParam, "Logger field not found")
	assert.True(t, loggerParam.Optional, "Logger should be optional")

	// Check Cache field
	require.NotNil(t, cacheParam, "Cache field not found")
	assert.Equal(t, "cache", cacheParam.Key, "Cache should have key 'cache'")

	// Check Handlers field
	require.NotNil(t, handlersParam, "Handlers field not found")
	assert.Equal(t, "handlers", handlersParam.Group, "Handlers should have group 'handlers'")
	assert.True(t, handlersParam.IsSlice, "Handlers should be a slice")
}

func TestAnalyzer_ResultObject(t *testing.T) {
	analyzer := reflection.New()

	info, err := analyzer.Analyze(NewServices)
	require.NoError(t, err, "Failed to analyze constructor with result object")

	assert.True(t, info.IsResultObject, "Expected IsResultObject to be true")

	// Check extracted returns from struct fields
	assert.Len(t, info.Returns, 3, "Expected 3 returns from struct fields")

	// Find and check each field
	var userSvc, adminSvc, handler *reflection.ReturnInfo

	for i := range info.Returns {
		ret := &info.Returns[i]
		switch ret.Name {
		case "UserSvc":
			userSvc = ret
		case "AdminSvc":
			adminSvc = ret
		case "Handler":
			handler = ret
		}
	}

	// Check UserSvc field
	require.NotNil(t, userSvc, "UserSvc field not found")
	assert.Nil(t, userSvc.Key, "UserSvc should not have a key")

	// Check AdminSvc field
	require.NotNil(t, adminSvc, "AdminSvc field not found")
	assert.Equal(t, "admin", adminSvc.Key, "AdminSvc should have key 'admin'")

	// Check Handler field
	require.NotNil(t, handler, "Handler field not found")
	assert.Equal(t, "handlers", handler.Group, "Handler should have group 'handlers'")
}

func TestAnalyzer_ResultObjectWithError(t *testing.T) {
	analyzer := reflection.New()

	info, err := analyzer.Analyze(NewServicesWithError)
	require.NoError(t, err, "Failed to analyze constructor")

	assert.True(t, info.IsResultObject, "Expected IsResultObject to be true")
	assert.True(t, info.HasErrorReturn, "Expected HasErrorReturn to be true")
}

func TestAnalyzer_NonFunction(t *testing.T) {
	analyzer := reflection.New()

	// Analyze a non-function value
	db := &Database{ConnectionString: "test"}
	info, err := analyzer.Analyze(db)
	require.NoError(t, err, "Failed to analyze non-function")

	assert.False(t, info.IsFunc, "Expected IsFunc to be false for non-function")

	serviceType, err := analyzer.GetServiceType(db)
	require.NoError(t, err, "Failed to get service type")
	assert.Equal(t, reflect.TypeOf(db), serviceType, "Expected service type to match")
}

func TestAnalyzer_GetServiceType(t *testing.T) {
	analyzer := reflection.New()

	tests := []struct {
		name        string
		constructor any
		wantType    reflect.Type
		wantErr     bool
	}{
		{
			name:        "Simple constructor",
			constructor: NewDatabase,
			wantType:    reflect.TypeOf((*Database)(nil)),
		},
		{
			name:        "Constructor with error",
			constructor: NewUserServiceWithError,
			wantType:    reflect.TypeOf((*UserService)(nil)),
		},
		{
			name:        "Result object",
			constructor: NewServices,
			wantType:    reflect.TypeOf(ServiceResults{}),
		},
		{
			name:        "Non-function",
			constructor: &Database{},
			wantType:    reflect.TypeOf((*Database)(nil)),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, err := analyzer.GetServiceType(tt.constructor)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantType, gotType)
			}
		})
	}
}

func TestAnalyzer_GetResultTypes(t *testing.T) {
	analyzer := reflection.New()

	// Test result object with multiple types
	types, err := analyzer.GetResultTypes(NewServices)
	require.NoError(t, err, "Failed to get result types")

	assert.Len(t, types, 3, "Expected 3 result types")

	// Check that UserService and func() types are included
	hasUserService := false
	hasFunc := false

	for _, typ := range types {
		if typ == reflect.TypeOf((*UserService)(nil)) {
			hasUserService = true
		}
		if typ == reflect.TypeOf(func() {}) {
			hasFunc = true
		}
	}

	assert.True(t, hasUserService, "Expected *UserService in result types")
	assert.True(t, hasFunc, "Expected func() in result types")
}

func TestAnalyzer_Caching(t *testing.T) {
	analyzer := reflection.New()

	// Analyze the same constructor twice
	info1, err := analyzer.Analyze(NewDatabase)
	require.NoError(t, err, "First analysis failed")

	info2, err := analyzer.Analyze(NewDatabase)
	require.NoError(t, err, "Second analysis failed")

	// Should return the same cached instance
	assert.Same(t, info1, info2, "Expected cached result to be returned")

	// Check cache size
	assert.GreaterOrEqual(t, analyzer.CacheSize(), 1, "Cache should contain at least one entry")

	// Clear cache and reanalyze
	analyzer.Clear()
	assert.Equal(t, 0, analyzer.CacheSize(), "Cache should be empty after clear")

	info3, err := analyzer.Analyze(NewDatabase)
	require.NoError(t, err, "Third analysis failed")

	// Should be a different instance after cache clear
	assert.NotSame(t, info1, info3, "Expected new analysis after cache clear")
}

func TestValidator(t *testing.T) {
	analyzer := reflection.New()
	validator := reflection.NewValidator(analyzer)

	tests := []struct {
		name        string
		constructor any
		wantErr     bool
		errContains string
	}{
		{
			name:        "Valid simple constructor",
			constructor: NewDatabase,
			wantErr:     false,
		},
		{
			name:        "Valid constructor with error",
			constructor: NewUserServiceWithError,
			wantErr:     false,
		},
		{
			name:        "Valid param object",
			constructor: NewServiceWithParams,
			wantErr:     false,
		},
		{
			name:        "Valid result object",
			constructor: NewServices,
			wantErr:     false,
		},
		{
			name:        "No return values",
			constructor: func() {},
			wantErr:     true,
			errContains: "must return at least one value",
		},
		{
			name:        "Too many returns",
			constructor: func() (int, int, int) { return 1, 2, 3 },
			wantErr:     true,
			errContains: "at most 2 values",
		},
		{
			name:        "Second return not error",
			constructor: func() (int, string) { return 1, "" },
			wantErr:     true,
			errContains: "second return value must be error",
		},
		{
			name:        "Only error return",
			constructor: func() error { return nil },
			wantErr:     true,
			errContains: "must return at least one non-error",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := analyzer.Analyze(tt.constructor)
			require.NoError(t, err, "Analysis failed")

			err = validator.Validate(info)
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTypeFormatter(t *testing.T) {
	formatter := &reflection.TypeFormatter{}

	tests := []struct {
		name     string
		typ      reflect.Type
		expected string
	}{
		{
			name:     "Pointer type",
			typ:      reflect.TypeOf((*Database)(nil)),
			expected: "*Database",
		},
		{
			name:     "Slice type",
			typ:      reflect.TypeOf([]string{}),
			expected: "[]string",
		},
		{
			name:     "Interface type",
			typ:      reflect.TypeOf((*Logger)(nil)).Elem(),
			expected: "Logger",
		},
		{
			name:     "Function type",
			typ:      reflect.TypeOf(func(int) string { return "" }),
			expected: "func(int) string",
		},
		{
			name:     "Map type",
			typ:      reflect.TypeOf(map[string]int{}),
			expected: "map[string]int",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatter.FormatType(tt.typ)
			assert.Equal(t, tt.expected, got)
		})
	}
}

// Additional test types for edge cases
type ComplexService struct {
	Database *Database
	Logger   Logger `optional:"true"`
	_        string // unexported field for testing
}

type CircularA struct {
	B *CircularB
}

type CircularB struct {
	A *CircularA
}

// Test parameter object with all tag types
type FullParamObject struct {
	reflection.In

	Required    *Database
	Optional    Logger    `optional:"true"`
	Named       *Database `name:"backup"`
	Grouped     []func()  `group:"handlers"`
	Ignored     string    `inject:"-"`
	_           string    // unexported field for testing
	Combination *Database `name:"special" optional:"true"`
}

// Test edge cases in analyzer
func TestAnalyzer_EdgeCases(t *testing.T) {
	analyzer := reflection.New()

	tests := []struct {
		name        string
		constructor any
		wantErr     bool
		validate    func(*testing.T, *reflection.ConstructorInfo)
	}{
		{
			name:        "nil constructor",
			constructor: nil,
			wantErr:     true,
		},
		{
			name:        "non-function value",
			constructor: &Database{ConnectionString: "test"},
			wantErr:     false,
			validate: func(t *testing.T, info *reflection.ConstructorInfo) {
				assert.False(t, info.IsFunc, "Expected IsFunc to be false for non-function")
			},
		},
		{
			name:        "function with no parameters",
			constructor: func() *Database { return nil },
			wantErr:     false,
			validate: func(t *testing.T, info *reflection.ConstructorInfo) {
				assert.Len(t, info.Parameters, 0, "Expected 0 parameters")
			},
		},
		{
			name:        "function with no returns",
			constructor: func(db *Database) {},
			wantErr:     false,
			validate: func(t *testing.T, info *reflection.ConstructorInfo) {
				assert.Len(t, info.Returns, 0, "Expected 0 returns")
			},
		},
		{
			name:        "variadic function",
			constructor: func(dbs ...*Database) *UserService { return nil },
			wantErr:     false,
			validate: func(t *testing.T, info *reflection.ConstructorInfo) {
				assert.Len(t, info.Parameters, 1, "Expected 1 parameter for variadic")
				assert.Equal(t, reflect.Slice, info.Parameters[0].Type.Kind(), "Variadic parameter should be slice type")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := analyzer.Analyze(tt.constructor)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				if tt.validate != nil {
					tt.validate(t, info)
				}
			}
		})
	}
}

// Test concurrent analysis and caching
func TestAnalyzer_ConcurrentAnalysis(t *testing.T) {
	analyzer := reflection.New()

	var wg sync.WaitGroup
	errs := make(chan error, 100)

	// Analyze the same constructor concurrently
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			info, err := analyzer.Analyze(NewDatabase)
			if err != nil {
				errs <- err
				return
			}

			if info == nil {
				errs <- errors.New("info is nil")
			}
		}()
	}

	// Analyze different constructors concurrently
	constructors := []any{
		NewDatabase, // Already analyzed above
		NewUserService,
		NewUserServiceWithError,
		NewServiceWithParams,
		NewServices,
	}

	for _, constructor := range constructors {
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(c any) {
				defer wg.Done()

				info, err := analyzer.Analyze(c)
				if err != nil {
					errs <- err
					return
				}

				// Also get dependencies to test that path
				_, err = analyzer.GetDependencies(c)
				if err != nil {
					errs <- err
				}

				// And service type
				_, err = analyzer.GetServiceType(c)
				if err != nil && info.IsFunc { // Non-functions might not have service type
					errs <- err
				}
			}(constructor)
		}
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		assert.NoError(t, err, "Concurrent analysis error")
	}

	// Verify cache is working
	// We have exactly 5 unique constructors being analyzed
	expectedCacheSize := len(constructors) // 5 constructors
	assert.Equal(t, expectedCacheSize, analyzer.CacheSize(), "Expected cache size to match")

	// Additional verification: ensure NewDatabase was only cached once
	// by checking that multiple calls return the same cached instance
	info1, _ := analyzer.Analyze(NewDatabase)
	info2, _ := analyzer.Analyze(NewDatabase)

	assert.Same(t, info1, info2, "Expected same cached instance for NewDatabase")
}

// Test complex parameter object with all features
func TestAnalyzer_ComplexParamObject(t *testing.T) {
	analyzer := reflection.New()

	constructor := func(params FullParamObject) *UserService {
		return &UserService{}
	}

	info, err := analyzer.Analyze(constructor)
	require.NoError(t, err, "Failed to analyze complex param object")

	assert.True(t, info.IsParamObject, "Should detect as param object")

	// Count non-ignored, exported fields
	expectedParams := 5 // Required, Optional, Named, Grouped, Combination
	assert.Len(t, info.Parameters, expectedParams, "Expected parameters count")

	// Verify each field's properties
	fieldMap := make(map[string]reflection.ParameterInfo)
	for _, param := range info.Parameters {
		fieldMap[param.Name] = param
	}

	// Check Required field
	req, ok := fieldMap["Required"]
	assert.True(t, ok, "Required field not found")
	assert.False(t, req.Optional, "Required field should not be optional")
	assert.Nil(t, req.Key, "Required field should not have a key")

	// Check Optional field
	opt, ok := fieldMap["Optional"]
	assert.True(t, ok, "Optional field not found")
	assert.True(t, opt.Optional, "Optional field should be optional")

	// Check Named field
	named, ok := fieldMap["Named"]
	assert.True(t, ok, "Named field not found")
	assert.Equal(t, "backup", named.Key, "Named field should have key 'backup'")

	// Check Grouped field
	grouped, ok := fieldMap["Grouped"]
	assert.True(t, ok, "Grouped field not found")
	assert.Equal(t, "handlers", grouped.Group, "Grouped field should have group 'handlers'")
	assert.True(t, grouped.IsSlice, "Grouped field should be a slice")

	// Check Ignored field is not present
	_, ok = fieldMap["Ignored"]
	assert.False(t, ok, "Ignored field should not be in parameters")

	// Check unexported field is not present
	_, ok = fieldMap["unexported"]
	assert.False(t, ok, "Unexported field should not be in parameters")

	// Check Combination field
	combo, ok := fieldMap["Combination"]
	assert.True(t, ok, "Combination field not found")
	assert.Equal(t, "special", combo.Key, "Combination field should have key 'special'")
	assert.True(t, combo.Optional, "Combination field should be optional")
}

// Test multiple return types support
func TestAnalyzer_MultipleReturns(t *testing.T) {
	tests := []struct {
		name          string
		constructor   any
		expectedTypes []reflect.Type
		hasError      bool
		errorPosition int // -1 if no error
	}{
		{
			name: "single return no error",
			constructor: func() *Database {
				return &Database{}
			},
			expectedTypes: []reflect.Type{
				reflect.TypeOf((*Database)(nil)),
			},
			hasError:      false,
			errorPosition: -1,
		},
		{
			name: "single return with error",
			constructor: func() (*Database, error) {
				return &Database{}, nil
			},
			expectedTypes: []reflect.Type{
				reflect.TypeOf((*Database)(nil)),
			},
			hasError:      true,
			errorPosition: 1,
		},
		{
			name: "multiple returns no error",
			constructor: func() (*Database, *UserService, Logger) {
				return &Database{}, &UserService{}, &ConsoleLogger{}
			},
			expectedTypes: []reflect.Type{
				reflect.TypeOf((*Database)(nil)),
				reflect.TypeOf((*UserService)(nil)),
				reflect.TypeOf((*Logger)(nil)).Elem(),
			},
			hasError:      false,
			errorPosition: -1,
		},
		{
			name: "multiple returns with error",
			constructor: func() (*Database, *UserService, Logger, error) {
				return &Database{}, &UserService{}, &ConsoleLogger{}, nil
			},
			expectedTypes: []reflect.Type{
				reflect.TypeOf((*Database)(nil)),
				reflect.TypeOf((*UserService)(nil)),
				reflect.TypeOf((*Logger)(nil)).Elem(),
			},
			hasError:      true,
			errorPosition: 3,
		},
		{
			name: "three returns with error",
			constructor: func() (*Database, Logger, error) {
				return &Database{}, &ConsoleLogger{}, nil
			},
			expectedTypes: []reflect.Type{
				reflect.TypeOf((*Database)(nil)),
				reflect.TypeOf((*Logger)(nil)).Elem(),
			},
			hasError:      true,
			errorPosition: 2,
		},
	}

	analyzer := reflection.New()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := analyzer.Analyze(tt.constructor)
			require.NoError(t, err, "Failed to analyze constructor")

			// Check HasErrorReturn flag
			assert.Equal(t, tt.hasError, info.HasErrorReturn, "HasErrorReturn mismatch")

			// Get result types
			types, err := analyzer.GetResultTypes(tt.constructor)
			require.NoError(t, err, "Failed to get result types")

			// Check number of types (excluding error)
			assert.Len(t, types, len(tt.expectedTypes), "Unexpected number of types")

			// Check each type
			for i, expectedType := range tt.expectedTypes {
				if i < len(types) {
					assert.Equal(t, expectedType, types[i], "Type mismatch at index %d", i)
				}
			}

			// Verify error position if present
			if tt.hasError && tt.errorPosition >= 0 {
				assert.GreaterOrEqual(t, len(info.Returns), tt.errorPosition+1, "Returns slice too short")
				if tt.errorPosition < len(info.Returns) {
					assert.True(t, info.Returns[tt.errorPosition].IsError, "Expected error at position %d", tt.errorPosition)
				}
			}
		})
	}
}

// Test error handling in builders
func TestParamObjectBuilder_ErrorCases(t *testing.T) {
	analyzer := reflection.New()
	builder := reflection.NewParamObjectBuilder(analyzer)

	// Mock resolver that always fails
	failingResolver := &mockResolver{
		shouldFail: true,
		failError:  errors.New("resolution failed"),
	}

	// Test with non-struct type
	_, err := builder.BuildParamObject(
		reflect.TypeOf(42), // int, not struct
		failingResolver,
	)
	assert.Error(t, err, "Expected error for non-struct type")

	// Test with struct containing required field that fails to resolve
	paramType := reflect.TypeOf(struct {
		reflection.In
		Required *Database
	}{})

	_, err = builder.BuildParamObject(paramType, failingResolver)
	assert.Error(t, err, "Expected error for failed required dependency")

	// Test with optional field that fails to resolve (should succeed)
	optionalType := reflect.TypeOf(struct {
		reflection.In
		Optional *Database `optional:"true"`
	}{})

	_, err = builder.BuildParamObject(optionalType, failingResolver)
	assert.NoError(t, err, "Should succeed with failed optional dependency")
}

// Test TypeFormatter with complex types
func TestTypeFormatter_ComplexTypes(t *testing.T) {
	formatter := &reflection.TypeFormatter{}

	tests := []struct {
		name     string
		typ      reflect.Type
		expected string
	}{
		{
			name:     "nil type",
			typ:      nil,
			expected: "<nil>",
		},
		{
			name:     "channel",
			typ:      reflect.TypeOf(make(chan int)),
			expected: "chan int",
		},
		{
			name:     "send-only channel",
			typ:      reflect.TypeOf(make(chan<- int)),
			expected: "->chan int",
		},
		{
			name:     "receive-only channel",
			typ:      reflect.TypeOf(make(<-chan int)),
			expected: "<-chan int",
		},
		{
			name:     "nested map",
			typ:      reflect.TypeOf(map[string]map[int]bool{}),
			expected: "map[string]map[int]bool",
		},
		{
			name:     "function with multiple params and returns",
			typ:      reflect.TypeOf(func(int, string) (bool, error) { return false, nil }),
			expected: "func(int, string) (bool, error)",
		},
		{
			name:     "unnamed struct",
			typ:      reflect.TypeOf(struct{ Field int }{}),
			expected: "struct{...}",
		},
		{
			name:     "array",
			typ:      reflect.TypeOf([5]int{}),
			expected: "[5]int",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := formatter.FormatType(tt.typ)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Test that different functions with the same signature are cached separately
func TestAnalyzer_DifferentFunctionsWithSameSignature(t *testing.T) {
	analyzer := reflection.New()

	// Create two different functions with identical signatures
	constructor1 := func() *Database {
		return &Database{ConnectionString: "db1"}
	}

	constructor2 := func() *Database {
		return &Database{ConnectionString: "db2"}
	}

	// Analyze both constructors
	info1, err := analyzer.Analyze(constructor1)
	require.NoError(t, err, "Failed to analyze constructor1")

	info2, err := analyzer.Analyze(constructor2)
	require.NoError(t, err, "Failed to analyze constructor2")

	// They should have the same type but different values
	assert.Equal(t, info1.Type, info2.Type, "Expected same type for both constructors")

	// But they should be different ConstructorInfo instances
	assert.NotSame(t, info1, info2, "Expected different ConstructorInfo instances for different functions")

	// Most importantly, their Value fields should be different
	assert.NotEqual(t, info1.Value.Pointer(), info2.Value.Pointer(), "Expected different function pointers in Value field")

	// Test that calling them produces different results
	results1 := info1.Value.Call([]reflect.Value{})
	results2 := info2.Value.Call([]reflect.Value{})

	db1 := results1[0].Interface().(*Database)
	db2 := results2[0].Interface().(*Database)

	assert.Equal(t, "db1", db1.ConnectionString)
	assert.Equal(t, "db2", db2.ConnectionString)
}

// Test that the same function analyzed multiple times returns cached result
func TestAnalyzer_SameFunctionCached(t *testing.T) {
	analyzer := reflection.New()

	constructor := func() *Database {
		return &Database{ConnectionString: "test"}
	}

	// Analyze the same constructor multiple times
	info1, err := analyzer.Analyze(constructor)
	require.NoError(t, err, "Failed to analyze constructor")

	info2, err := analyzer.Analyze(constructor)
	require.NoError(t, err, "Failed to analyze constructor again")

	// Should return the exact same cached instance
	assert.Same(t, info1, info2, "Expected same cached ConstructorInfo instance")

	// Value pointers should be identical
	assert.Equal(t, info1.Value.Pointer(), info2.Value.Pointer(), "Expected same function pointer in cached result")
}

// Test with multiple functions having different signatures
func TestAnalyzer_DifferentSignatures(t *testing.T) {
	analyzer := reflection.New()

	// Different signatures
	constructor1 := func() *Database {
		return &Database{ConnectionString: "db1"}
	}

	constructor2 := func(name string) *Database {
		return &Database{ConnectionString: name}
	}

	constructor3 := func() (*Database, error) {
		return &Database{ConnectionString: "db3"}, nil
	}

	// Analyze all constructors
	info1, err := analyzer.Analyze(constructor1)
	require.NoError(t, err, "Failed to analyze constructor1")

	info2, err := analyzer.Analyze(constructor2)
	require.NoError(t, err, "Failed to analyze constructor2")

	info3, err := analyzer.Analyze(constructor3)
	require.NoError(t, err, "Failed to analyze constructor3")

	// All should be different
	assert.NotSame(t, info1, info2, "Expected different ConstructorInfo for different signatures")
	assert.NotSame(t, info1, info3, "Expected different ConstructorInfo for different signatures")
	assert.NotSame(t, info2, info3, "Expected different ConstructorInfo for different signatures")

	// Types should be different
	assert.NotEqual(t, info1.Type, info2.Type, "Expected different types for different signatures")
	assert.NotEqual(t, info1.Type, info3.Type, "Expected different types for different signatures")
	assert.NotEqual(t, info2.Type, info3.Type, "Expected different types for different signatures")

	// Verify parameter counts
	assert.Len(t, info1.Parameters, 0, "Expected 0 parameters for constructor1")
	assert.Len(t, info2.Parameters, 1, "Expected 1 parameter for constructor2")

	// Verify return counts
	assert.Len(t, info1.Returns, 1, "Expected 1 return for constructor1")
	assert.Len(t, info3.Returns, 2, "Expected 2 returns for constructor3")
	assert.True(t, info3.HasErrorReturn, "Expected constructor3 to have error return")
}

// Test that cache size reflects unique functions
func TestAnalyzer_CacheSizeWithDuplicateFunctions(t *testing.T) {
	analyzer := reflection.New()

	// Clear cache first
	analyzer.Clear()

	// Create multiple functions with same signature
	constructor1 := func() *Database {
		return &Database{ConnectionString: "db1"}
	}

	constructor2 := func() *Database {
		return &Database{ConnectionString: "db2"}
	}

	constructor3 := func() *Database {
		return &Database{ConnectionString: "db3"}
	}

	// Analyze each constructor multiple times
	for i := 0; i < 3; i++ {
		analyzer.Analyze(constructor1)
		analyzer.Analyze(constructor2)
		analyzer.Analyze(constructor3)
	}

	// Cache should have exactly 3 entries (one per unique function)
	assert.Equal(t, 3, analyzer.CacheSize(), "Expected cache size 3")

	// Analyze the same constructors again
	analyzer.Analyze(constructor1)
	analyzer.Analyze(constructor2)
	analyzer.Analyze(constructor3)

	// Cache size should still be 3
	assert.Equal(t, 3, analyzer.CacheSize(), "Expected cache size to remain 3")
}

// Test with methods (bound to receivers)
func TestAnalyzer_Methods(t *testing.T) {
	analyzer := reflection.New()

	// Create separate logger instances
	logger1 := &ConsoleLogger{}
	logger2 := &ConsoleLogger{}

	// Get their Log methods as values
	method1 := logger1.Log
	method2 := logger2.Log

	info1, err := analyzer.Analyze(method1)
	require.NoError(t, err, "Failed to analyze method1")

	info2, err := analyzer.Analyze(method2)
	require.NoError(t, err, "Failed to analyze method2")

	// Methods from different instances might or might not share the same
	// underlying implementation depending on Go's optimization.
	// What we can test is that they have the same type signature
	assert.Equal(t, info1.Type, info2.Type, "Expected same type for methods from same type")

	// Both should be recognized as functions
	assert.True(t, info1.IsFunc, "Expected method1 to be recognized as function")
	assert.True(t, info2.IsFunc, "Expected method2 to be recognized as function")
}

// Test edge case: nil function
func TestAnalyzer_NilFunction(t *testing.T) {
	analyzer := reflection.New()

	var nilFunc func() *Database

	_, err := analyzer.Analyze(nilFunc)
	assert.Error(t, err, "Expected error when analyzing nil function")
}

// Test that Clear actually clears the cache properly
func TestAnalyzer_ClearWithSameSignature(t *testing.T) {
	analyzer := reflection.New()

	constructor1 := func() *Database {
		return &Database{ConnectionString: "db1"}
	}

	constructor2 := func() *Database {
		return &Database{ConnectionString: "db2"}
	}

	// Analyze both
	info1a, _ := analyzer.Analyze(constructor1)
	info2a, _ := analyzer.Analyze(constructor2)

	// Clear cache
	analyzer.Clear()

	// Analyze again
	info1b, _ := analyzer.Analyze(constructor1)
	info2b, _ := analyzer.Analyze(constructor2)

	// After clear, we should get new ConstructorInfo instances
	assert.NotSame(t, info1a, info1b, "Expected new ConstructorInfo for constructor1 after clear")
	assert.NotSame(t, info2a, info2b, "Expected new ConstructorInfo for constructor2 after clear")

	// But the function pointers should still be correct
	assert.Equal(t, reflect.ValueOf(constructor1).Pointer(), info1b.Value.Pointer(), "Wrong function pointer for constructor1 after clear")
	assert.Equal(t, reflect.ValueOf(constructor2).Pointer(), info2b.Value.Pointer(), "Wrong function pointer for constructor2 after clear")
}

// Test GetDependencies edge cases
func TestAnalyzer_GetDependencies(t *testing.T) {
	analyzer := reflection.New()

	t.Run("with nil constructor", func(t *testing.T) {
		_, err := analyzer.GetDependencies(nil)
		assert.Error(t, err, "Expected error for nil constructor")
	})

	t.Run("with valid constructor", func(t *testing.T) {
		deps, err := analyzer.GetDependencies(NewUserService)
		require.NoError(t, err, "GetDependencies failed")
		assert.Len(t, deps, 2, "Expected 2 dependencies")
	})
}

// Test GetServiceType edge cases
func TestAnalyzer_GetServiceTypeEdgeCases(t *testing.T) {
	analyzer := reflection.New()

	t.Run("constructor with no returns", func(t *testing.T) {
		noReturnFunc := func(db *Database) {}

		_, err := analyzer.GetServiceType(noReturnFunc)
		assert.Error(t, err, "Expected error for constructor with no return values")
	})

	t.Run("nil constructor", func(t *testing.T) {
		_, err := analyzer.GetServiceType(nil)
		assert.Error(t, err, "Expected error for nil constructor")
	})

	t.Run("function that only returns error", func(t *testing.T) {
		errorOnlyFunc := func() error {
			return nil
		}

		_, err := analyzer.GetServiceType(errorOnlyFunc)
		assert.Error(t, err, "Expected error for function that only returns error")
	})
}

// Test GetResultTypes edge cases
func TestAnalyzer_GetResultTypesEdgeCases(t *testing.T) {
	analyzer := reflection.New()

	t.Run("nil constructor", func(t *testing.T) {
		_, err := analyzer.GetResultTypes(nil)
		assert.Error(t, err, "Expected error for nil constructor")
	})

	t.Run("result object with error", func(t *testing.T) {
		type ResultWithError struct {
			reflection.Out
			Service *UserService
		}

		resultFunc := func() (ResultWithError, error) {
			return ResultWithError{}, nil
		}

		types, err := analyzer.GetResultTypes(resultFunc)
		require.NoError(t, err, "GetResultTypes failed")

		// Should include the service type but not error
		assert.Len(t, types, 1, "Expected 1 type")
	})

	t.Run("non-result object", func(t *testing.T) {
		simpleFunc := func() *Database {
			return &Database{}
		}

		types, err := analyzer.GetResultTypes(simpleFunc)
		require.NoError(t, err, "GetResultTypes failed")

		assert.Len(t, types, 1, "Expected 1 type")
		assert.Equal(t, reflect.TypeOf((*Database)(nil)), types[0], "Wrong type returned")
	})
}

// Test analyzer with types that don't embed In/Out
func TestAnalyzer_NonInOutTypes(t *testing.T) {
	analyzer := reflection.New()

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
}

// Mock resolver for testing
type mockResolver struct {
	shouldFail bool
	failError  error
	resolved   map[string]any
}

func (m *mockResolver) Get(t reflect.Type) (any, error) {
	if m.shouldFail {
		return nil, m.failError
	}
	if m.resolved != nil {
		return m.resolved[t.String()], nil
	}
	return reflect.New(t.Elem()).Interface(), nil
}

func (m *mockResolver) GetKeyed(t reflect.Type, key any) (any, error) {
	if m.shouldFail {
		return nil, m.failError
	}
	return m.Get(t)
}

func (m *mockResolver) GetGroup(t reflect.Type, group string) ([]any, error) {
	if m.shouldFail {
		return nil, m.failError
	}
	return []any{}, nil
}

// Test caching with closures that capture variables
func TestAnalyzer_Closures(t *testing.T) {
	analyzer := reflection.New()

	// Create closures that capture different values
	makeConstructor := func(connStr string) func() *Database {
		return func() *Database {
			return &Database{ConnectionString: connStr}
		}
	}

	constructor1 := makeConstructor("db1")
	constructor2 := makeConstructor("db2")
	constructor3 := makeConstructor("db3")

	info1, err := analyzer.Analyze(constructor1)
	require.NoError(t, err, "Failed to analyze constructor1")

	info2, err := analyzer.Analyze(constructor2)
	require.NoError(t, err, "Failed to analyze constructor2")

	info3, err := analyzer.Analyze(constructor3)
	require.NoError(t, err, "Failed to analyze constructor3")

	// Each closure should have its own ConstructorInfo
	assert.NotSame(t, info1, info2, "Expected different ConstructorInfo for different closures")
	assert.NotSame(t, info1, info3, "Expected different ConstructorInfo for different closures")
	assert.NotSame(t, info2, info3, "Expected different ConstructorInfo for different closures")

	// Verify each produces the correct result
	results1 := info1.Value.Call([]reflect.Value{})
	db1 := results1[0].Interface().(*Database)
	assert.Equal(t, "db1", db1.ConnectionString, "Constructor 1 result mismatch")

	results2 := info2.Value.Call([]reflect.Value{})
	db2 := results2[0].Interface().(*Database)
	assert.Equal(t, "db2", db2.ConnectionString, "Constructor 2 result mismatch")

	results3 := info3.Value.Call([]reflect.Value{})
	db3 := results3[0].Interface().(*Database)
	assert.Equal(t, "db3", db3.ConnectionString, "Constructor 3 result mismatch")
}

// Benchmark to ensure caching performance isn't degraded
func BenchmarkAnalyzer_SameSignatureDifferentFunctions(b *testing.B) {
	analyzer := reflection.New()

	// Create many functions with the same signature
	constructors := make([]func() *Database, 100)
	for i := 0; i < 100; i++ {
		idx := i
		constructors[i] = func() *Database {
			return &Database{ConnectionString: fmt.Sprintf("db%d", idx)}
		}
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Analyze a random constructor
		idx := i % len(constructors)
		analyzer.Analyze(constructors[idx])
	}
}

// Benchmark cache performance
func BenchmarkAnalyzer_CacheHit(b *testing.B) {
	analyzer := reflection.New()

	// Pre-cache
	analyzer.Analyze(NewDatabase)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		analyzer.Analyze(NewDatabase)
	}
}

func BenchmarkAnalyzer_CacheMiss(b *testing.B) {
	analyzer := reflection.New()

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		analyzer.Clear() // Force cache miss
		analyzer.Analyze(NewDatabase)
	}
}
