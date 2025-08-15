package reflection_test

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/junioryono/godi/v3/internal/reflection"
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
	if err != nil {
		t.Fatalf("Failed to analyze constructor: %v", err)
	}

	if !info.IsFunc {
		t.Error("Expected IsFunc to be true")
	}

	if info.IsParamObject {
		t.Error("Expected IsParamObject to be false")
	}

	if info.IsResultObject {
		t.Error("Expected IsResultObject to be false")
	}

	// Check parameters
	if len(info.Parameters) != 1 {
		t.Errorf("Expected 1 parameter, got %d", len(info.Parameters))
	}

	if info.Parameters[0].Type != reflect.TypeOf("") {
		t.Error("Expected string parameter type")
	}

	// Check returns
	if len(info.Returns) != 1 {
		t.Errorf("Expected 1 return value, got %d", len(info.Returns))
	}

	if info.Returns[0].Type != reflect.TypeOf((*Database)(nil)) {
		t.Error("Expected *Database return type")
	}
}

func TestAnalyzer_ConstructorWithMultipleParams(t *testing.T) {
	analyzer := reflection.New()

	info, err := analyzer.Analyze(NewUserService)
	if err != nil {
		t.Fatalf("Failed to analyze constructor: %v", err)
	}

	// Check parameters
	if len(info.Parameters) != 2 {
		t.Errorf("Expected 2 parameters, got %d", len(info.Parameters))
	}

	if info.Parameters[0].Type != reflect.TypeOf((*Database)(nil)) {
		t.Error("Expected first parameter to be *Database")
	}

	if info.Parameters[1].Type != reflect.TypeOf((*Logger)(nil)).Elem() {
		t.Error("Expected second parameter to be Logger interface")
	}

	// Check dependencies
	deps, err := analyzer.GetDependencies(NewUserService)
	if err != nil {
		t.Fatalf("Failed to get dependencies: %v", err)
	}

	if len(deps) != 2 {
		t.Errorf("Expected 2 dependencies, got %d", len(deps))
	}
}

func TestAnalyzer_ConstructorWithError(t *testing.T) {
	analyzer := reflection.New()

	info, err := analyzer.Analyze(NewUserServiceWithError)
	if err != nil {
		t.Fatalf("Failed to analyze constructor: %v", err)
	}

	if !info.HasErrorReturn {
		t.Error("Expected HasErrorReturn to be true")
	}

	// Check returns
	if len(info.Returns) != 2 {
		t.Errorf("Expected 2 return values, got %d", len(info.Returns))
	}

	if !info.Returns[1].IsError {
		t.Error("Expected second return to be error")
	}
}

func TestAnalyzer_ParamObject(t *testing.T) {
	analyzer := reflection.New()

	info, err := analyzer.Analyze(NewServiceWithParams)
	if err != nil {
		t.Fatalf("Failed to analyze constructor with param object: %v", err)
	}

	if !info.IsParamObject {
		t.Error("Expected IsParamObject to be true")
	}

	// Check extracted parameters from struct fields
	if len(info.Parameters) != 4 {
		t.Errorf("Expected 4 parameters from struct fields, got %d", len(info.Parameters))
	}

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
	if dbParam == nil {
		t.Fatal("Database field not found")
	}
	if dbParam.Optional {
		t.Error("Database should not be optional")
	}

	// Check Logger field
	if loggerParam == nil {
		t.Fatal("Logger field not found")
	}
	if !loggerParam.Optional {
		t.Error("Logger should be optional")
	}

	// Check Cache field
	if cacheParam == nil {
		t.Fatal("Cache field not found")
	}
	if cacheParam.Key != "cache" {
		t.Errorf("Cache should have key 'cache', got %v", cacheParam.Key)
	}

	// Check Handlers field
	if handlersParam == nil {
		t.Fatal("Handlers field not found")
	}
	if handlersParam.Group != "handlers" {
		t.Errorf("Handlers should have group 'handlers', got %s", handlersParam.Group)
	}
	if !handlersParam.IsSlice {
		t.Error("Handlers should be a slice")
	}
}

func TestAnalyzer_ResultObject(t *testing.T) {
	analyzer := reflection.New()

	info, err := analyzer.Analyze(NewServices)
	if err != nil {
		t.Fatalf("Failed to analyze constructor with result object: %v", err)
	}

	if !info.IsResultObject {
		t.Error("Expected IsResultObject to be true")
	}

	// Check extracted returns from struct fields
	if len(info.Returns) != 3 {
		t.Errorf("Expected 3 returns from struct fields, got %d", len(info.Returns))
	}

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
	if userSvc == nil {
		t.Fatal("UserSvc field not found")
	}
	if userSvc.Key != nil {
		t.Errorf("UserSvc should not have a key, got %v", userSvc.Key)
	}

	// Check AdminSvc field
	if adminSvc == nil {
		t.Fatal("AdminSvc field not found")
	}
	if adminSvc.Key != "admin" {
		t.Errorf("AdminSvc should have key 'admin', got %v", adminSvc.Key)
	}

	// Check Handler field
	if handler == nil {
		t.Fatal("Handler field not found")
	}
	if handler.Group != "handlers" {
		t.Errorf("Handler should have group 'handlers', got %s", handler.Group)
	}
}

func TestAnalyzer_ResultObjectWithError(t *testing.T) {
	analyzer := reflection.New()

	info, err := analyzer.Analyze(NewServicesWithError)
	if err != nil {
		t.Fatalf("Failed to analyze constructor: %v", err)
	}

	if !info.IsResultObject {
		t.Error("Expected IsResultObject to be true")
	}

	if !info.HasErrorReturn {
		t.Error("Expected HasErrorReturn to be true")
	}
}

func TestAnalyzer_NonFunction(t *testing.T) {
	analyzer := reflection.New()

	// Analyze a non-function value
	db := &Database{ConnectionString: "test"}
	info, err := analyzer.Analyze(db)
	if err != nil {
		t.Fatalf("Failed to analyze non-function: %v", err)
	}

	if info.IsFunc {
		t.Error("Expected IsFunc to be false for non-function")
	}

	serviceType, err := analyzer.GetServiceType(db)
	if err != nil {
		t.Fatalf("Failed to get service type: %v", err)
	}

	if serviceType != reflect.TypeOf(db) {
		t.Errorf("Expected service type %v, got %v", reflect.TypeOf(db), serviceType)
	}
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
			if (err != nil) != tt.wantErr {
				t.Errorf("GetServiceType() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && gotType != tt.wantType {
				t.Errorf("GetServiceType() = %v, want %v", gotType, tt.wantType)
			}
		})
	}
}

func TestAnalyzer_GetResultTypes(t *testing.T) {
	analyzer := reflection.New()

	// Test result object with multiple types
	types, err := analyzer.GetResultTypes(NewServices)
	if err != nil {
		t.Fatalf("Failed to get result types: %v", err)
	}

	if len(types) != 3 {
		t.Errorf("Expected 3 result types, got %d", len(types))
	}

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

	if !hasUserService {
		t.Error("Expected *UserService in result types")
	}
	if !hasFunc {
		t.Error("Expected func() in result types")
	}
}

func TestAnalyzer_Caching(t *testing.T) {
	analyzer := reflection.New()

	// Analyze the same constructor twice
	info1, err := analyzer.Analyze(NewDatabase)
	if err != nil {
		t.Fatalf("First analysis failed: %v", err)
	}

	info2, err := analyzer.Analyze(NewDatabase)
	if err != nil {
		t.Fatalf("Second analysis failed: %v", err)
	}

	// Should return the same cached instance
	if info1 != info2 {
		t.Error("Expected cached result to be returned")
	}

	// Check cache size
	if analyzer.CacheSize() < 1 {
		t.Error("Cache should contain at least one entry")
	}

	// Clear cache and reanalyze
	analyzer.Clear()
	if analyzer.CacheSize() != 0 {
		t.Error("Cache should be empty after clear")
	}

	info3, err := analyzer.Analyze(NewDatabase)
	if err != nil {
		t.Fatalf("Third analysis failed: %v", err)
	}

	// Should be a different instance after cache clear
	if info1 == info3 {
		t.Error("Expected new analysis after cache clear")
	}
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
			if err != nil {
				t.Fatalf("Analysis failed: %v", err)
			}

			err = validator.Validate(info)
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}

			if err != nil && tt.errContains != "" {
				if !contains(err.Error(), tt.errContains) {
					t.Errorf("Error should contain %q, got %q", tt.errContains, err.Error())
				}
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
			if got != tt.expected {
				t.Errorf("FormatType() = %q, want %q", got, tt.expected)
			}
		})
	}
}

// Helper function
func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr ||
		len(s) >= len(substr) && contains(s[1:], substr)
}

// Additional test types for edge cases
type ComplexService struct {
	unexported string // Should be ignored
	Database   *Database
	Logger     Logger `optional:"true"`
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
	unexported  string    // Should be ignored
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
				if info.IsFunc {
					t.Error("Expected IsFunc to be false for non-function")
				}
			},
		},
		{
			name:        "function with no parameters",
			constructor: func() *Database { return nil },
			wantErr:     false,
			validate: func(t *testing.T, info *reflection.ConstructorInfo) {
				if len(info.Parameters) != 0 {
					t.Errorf("Expected 0 parameters, got %d", len(info.Parameters))
				}
			},
		},
		{
			name:        "function with no returns",
			constructor: func(db *Database) {},
			wantErr:     false,
			validate: func(t *testing.T, info *reflection.ConstructorInfo) {
				if len(info.Returns) != 0 {
					t.Errorf("Expected 0 returns, got %d", len(info.Returns))
				}
			},
		},
		{
			name:        "variadic function",
			constructor: func(dbs ...*Database) *UserService { return nil },
			wantErr:     false,
			validate: func(t *testing.T, info *reflection.ConstructorInfo) {
				if len(info.Parameters) != 1 {
					t.Errorf("Expected 1 parameter for variadic, got %d", len(info.Parameters))
				}
				if info.Parameters[0].Type.Kind() != reflect.Slice {
					t.Error("Variadic parameter should be slice type")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info, err := analyzer.Analyze(tt.constructor)

			if (err != nil) != tt.wantErr {
				t.Errorf("Analyze() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && tt.validate != nil {
				tt.validate(t, info)
			}
		})
	}
}

// Test concurrent analysis and caching
func TestAnalyzer_ConcurrentAnalysis(t *testing.T) {
	analyzer := reflection.New()

	var wg sync.WaitGroup
	ers := make(chan error, 100)

	// Analyze the same constructor concurrently
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			info, err := analyzer.Analyze(NewDatabase)
			if err != nil {
				ers <- err
				return
			}

			if info == nil {
				ers <- errors.New("info is nil")
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
					ers <- err
					return
				}

				// Also get dependencies to test that path
				_, err = analyzer.GetDependencies(c)
				if err != nil {
					ers <- err
				}

				// And service type
				_, err = analyzer.GetServiceType(c)
				if err != nil && info.IsFunc { // Non-functions might not have service type
					ers <- err
				}
			}(constructor)
		}
	}

	wg.Wait()
	close(ers)

	for err := range ers {
		t.Errorf("Concurrent analysis error: %v", err)
	}

	// Verify cache is working
	// We have exactly 5 unique constructors being analyzed
	expectedCacheSize := len(constructors) // 5 constructors
	if analyzer.CacheSize() != expectedCacheSize {
		t.Errorf("Expected cache size %d, got %d", expectedCacheSize, analyzer.CacheSize())
	}

	// Additional verification: ensure NewDatabase was only cached once
	// by checking that multiple calls return the same cached instance
	info1, _ := analyzer.Analyze(NewDatabase)
	info2, _ := analyzer.Analyze(NewDatabase)

	if info1 != info2 {
		t.Error("Expected same cached instance for NewDatabase")
	}
}

// Test complex parameter object with all features
func TestAnalyzer_ComplexParamObject(t *testing.T) {
	analyzer := reflection.New()

	constructor := func(params FullParamObject) *UserService {
		return &UserService{}
	}

	info, err := analyzer.Analyze(constructor)
	if err != nil {
		t.Fatalf("Failed to analyze complex param object: %v", err)
	}

	if !info.IsParamObject {
		t.Fatal("Should detect as param object")
	}

	// Count non-ignored, exported fields
	expectedParams := 5 // Required, Optional, Named, Grouped, Combination
	if len(info.Parameters) != expectedParams {
		t.Errorf("Expected %d parameters, got %d", expectedParams, len(info.Parameters))
	}

	// Verify each field's properties
	fieldMap := make(map[string]reflection.ParameterInfo)
	for _, param := range info.Parameters {
		fieldMap[param.Name] = param
	}

	// Check Required field
	if req, ok := fieldMap["Required"]; ok {
		if req.Optional {
			t.Error("Required field should not be optional")
		}
		if req.Key != nil {
			t.Error("Required field should not have a key")
		}
	} else {
		t.Error("Required field not found")
	}

	// Check Optional field
	if opt, ok := fieldMap["Optional"]; ok {
		if !opt.Optional {
			t.Error("Optional field should be optional")
		}
	} else {
		t.Error("Optional field not found")
	}

	// Check Named field
	if named, ok := fieldMap["Named"]; ok {
		if named.Key != "backup" {
			t.Errorf("Named field should have key 'backup', got %v", named.Key)
		}
	} else {
		t.Error("Named field not found")
	}

	// Check Grouped field
	if grouped, ok := fieldMap["Grouped"]; ok {
		if grouped.Group != "handlers" {
			t.Errorf("Grouped field should have group 'handlers', got %s", grouped.Group)
		}
		if !grouped.IsSlice {
			t.Error("Grouped field should be a slice")
		}
	} else {
		t.Error("Grouped field not found")
	}

	// Check Ignored field is not present
	if _, ok := fieldMap["Ignored"]; ok {
		t.Error("Ignored field should not be in parameters")
	}

	// Check unexported field is not present
	if _, ok := fieldMap["unexported"]; ok {
		t.Error("Unexported field should not be in parameters")
	}

	// Check Combination field
	if combo, ok := fieldMap["Combination"]; ok {
		if combo.Key != "special" {
			t.Errorf("Combination field should have key 'special', got %v", combo.Key)
		}
		if !combo.Optional {
			t.Error("Combination field should be optional")
		}
	} else {
		t.Error("Combination field not found")
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
	if err == nil {
		t.Error("Expected error for non-struct type")
	}

	// Test with struct containing required field that fails to resolve
	paramType := reflect.TypeOf(struct {
		reflection.In
		Required *Database
	}{})

	_, err = builder.BuildParamObject(paramType, failingResolver)
	if err == nil {
		t.Error("Expected error for failed required dependency")
	}

	// Test with optional field that fails to resolve (should succeed)
	optionalType := reflect.TypeOf(struct {
		reflection.In
		Optional *Database `optional:"true"`
	}{})

	_, err = builder.BuildParamObject(optionalType, failingResolver)
	if err != nil {
		t.Errorf("Should succeed with failed optional dependency: %v", err)
	}
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
			if result != tt.expected {
				t.Errorf("FormatType() = %q, want %q", result, tt.expected)
			}
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
	if err != nil {
		t.Fatalf("Failed to analyze constructor1: %v", err)
	}

	info2, err := analyzer.Analyze(constructor2)
	if err != nil {
		t.Fatalf("Failed to analyze constructor2: %v", err)
	}

	// They should have the same type but different values
	if info1.Type != info2.Type {
		t.Error("Expected same type for both constructors")
	}

	// But they should be different ConstructorInfo instances
	if info1 == info2 {
		t.Error("Expected different ConstructorInfo instances for different functions")
	}

	// Most importantly, their Value fields should be different
	if info1.Value.Pointer() == info2.Value.Pointer() {
		t.Error("Expected different function pointers in Value field")
	}

	// Test that calling them produces different results
	results1 := info1.Value.Call([]reflect.Value{})
	results2 := info2.Value.Call([]reflect.Value{})

	db1 := results1[0].Interface().(*Database)
	db2 := results2[0].Interface().(*Database)

	if db1.ConnectionString != "db1" {
		t.Errorf("Expected 'db1', got %s", db1.ConnectionString)
	}

	if db2.ConnectionString != "db2" {
		t.Errorf("Expected 'db2', got %s", db2.ConnectionString)
	}
}

// Test that the same function analyzed multiple times returns cached result
func TestAnalyzer_SameFunctionCached(t *testing.T) {
	analyzer := reflection.New()

	constructor := func() *Database {
		return &Database{ConnectionString: "test"}
	}

	// Analyze the same constructor multiple times
	info1, err := analyzer.Analyze(constructor)
	if err != nil {
		t.Fatalf("Failed to analyze constructor: %v", err)
	}

	info2, err := analyzer.Analyze(constructor)
	if err != nil {
		t.Fatalf("Failed to analyze constructor again: %v", err)
	}

	// Should return the exact same cached instance
	if info1 != info2 {
		t.Error("Expected same cached ConstructorInfo instance")
	}

	// Value pointers should be identical
	if info1.Value.Pointer() != info2.Value.Pointer() {
		t.Error("Expected same function pointer in cached result")
	}
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
	if err != nil {
		t.Fatalf("Failed to analyze constructor1: %v", err)
	}

	info2, err := analyzer.Analyze(constructor2)
	if err != nil {
		t.Fatalf("Failed to analyze constructor2: %v", err)
	}

	info3, err := analyzer.Analyze(constructor3)
	if err != nil {
		t.Fatalf("Failed to analyze constructor3: %v", err)
	}

	// All should be different
	if info1 == info2 || info1 == info3 || info2 == info3 {
		t.Error("Expected different ConstructorInfo for different signatures")
	}

	// Types should be different
	if info1.Type == info2.Type || info1.Type == info3.Type || info2.Type == info3.Type {
		t.Error("Expected different types for different signatures")
	}

	// Verify parameter counts
	if len(info1.Parameters) != 0 {
		t.Errorf("Expected 0 parameters for constructor1, got %d", len(info1.Parameters))
	}

	if len(info2.Parameters) != 1 {
		t.Errorf("Expected 1 parameter for constructor2, got %d", len(info2.Parameters))
	}

	// Verify return counts
	if len(info1.Returns) != 1 {
		t.Errorf("Expected 1 return for constructor1, got %d", len(info1.Returns))
	}

	if len(info3.Returns) != 2 {
		t.Errorf("Expected 2 returns for constructor3, got %d", len(info3.Returns))
	}

	if !info3.HasErrorReturn {
		t.Error("Expected constructor3 to have error return")
	}
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
	if analyzer.CacheSize() != 3 {
		t.Errorf("Expected cache size 3, got %d", analyzer.CacheSize())
	}

	// Analyze the same constructors again
	analyzer.Analyze(constructor1)
	analyzer.Analyze(constructor2)
	analyzer.Analyze(constructor3)

	// Cache size should still be 3
	if analyzer.CacheSize() != 3 {
		t.Errorf("Expected cache size to remain 3, got %d", analyzer.CacheSize())
	}
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
	if err != nil {
		t.Fatalf("Failed to analyze method1: %v", err)
	}

	info2, err := analyzer.Analyze(method2)
	if err != nil {
		t.Fatalf("Failed to analyze method2: %v", err)
	}

	// Methods from different instances might or might not share the same
	// underlying implementation depending on Go's optimization.
	// What we can test is that they have the same type signature
	if info1.Type != info2.Type {
		t.Error("Expected same type for methods from same type")
	}

	// Both should be recognized as functions
	if !info1.IsFunc || !info2.IsFunc {
		t.Error("Expected methods to be recognized as functions")
	}
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

// Test edge case: nil function
func TestAnalyzer_NilFunction(t *testing.T) {
	analyzer := reflection.New()

	var nilFunc func() *Database

	_, err := analyzer.Analyze(nilFunc)
	if err == nil {
		t.Error("Expected error when analyzing nil function")
	}
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
	if info1a == info1b {
		t.Error("Expected new ConstructorInfo for constructor1 after clear")
	}

	if info2a == info2b {
		t.Error("Expected new ConstructorInfo for constructor2 after clear")
	}

	// But the function pointers should still be correct
	if info1b.Value.Pointer() != reflect.ValueOf(constructor1).Pointer() {
		t.Error("Wrong function pointer for constructor1 after clear")
	}

	if info2b.Value.Pointer() != reflect.ValueOf(constructor2).Pointer() {
		t.Error("Wrong function pointer for constructor2 after clear")
	}
}

// Test GetDependencies edge cases
func TestAnalyzer_GetDependencies(t *testing.T) {
	analyzer := reflection.New()
	
	t.Run("with nil constructor", func(t *testing.T) {
		_, err := analyzer.GetDependencies(nil)
		if err == nil {
			t.Error("Expected error for nil constructor")
		}
	})
	
	t.Run("with valid constructor", func(t *testing.T) {
		deps, err := analyzer.GetDependencies(NewUserService)
		if err != nil {
			t.Fatalf("GetDependencies failed: %v", err)
		}
		
		if len(deps) != 2 {
			t.Errorf("Expected 2 dependencies, got %d", len(deps))
		}
	})
}

// Test GetServiceType edge cases
func TestAnalyzer_GetServiceTypeEdgeCases(t *testing.T) {
	analyzer := reflection.New()
	
	t.Run("constructor with no returns", func(t *testing.T) {
		noReturnFunc := func(db *Database) {}
		
		_, err := analyzer.GetServiceType(noReturnFunc)
		if err == nil {
			t.Error("Expected error for constructor with no return values")
		}
	})
	
	t.Run("nil constructor", func(t *testing.T) {
		_, err := analyzer.GetServiceType(nil)
		if err == nil {
			t.Error("Expected error for nil constructor")
		}
	})
	
	t.Run("function that only returns error", func(t *testing.T) {
		errorOnlyFunc := func() error {
			return nil
		}
		
		_, err := analyzer.GetServiceType(errorOnlyFunc)
		if err == nil {
			t.Error("Expected error for function that only returns error")
		}
	})
}

// Test GetResultTypes edge cases
func TestAnalyzer_GetResultTypesEdgeCases(t *testing.T) {
	analyzer := reflection.New()
	
	t.Run("nil constructor", func(t *testing.T) {
		_, err := analyzer.GetResultTypes(nil)
		if err == nil {
			t.Error("Expected error for nil constructor")
		}
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
		if err != nil {
			t.Fatalf("GetResultTypes failed: %v", err)
		}
		
		// Should include the service type but not error
		if len(types) != 1 {
			t.Errorf("Expected 1 type, got %d", len(types))
		}
	})
	
	t.Run("non-result object", func(t *testing.T) {
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
	if err != nil {
		t.Fatalf("Failed to analyze: %v", err)
	}
	
	// Should not be detected as param object
	if info.IsParamObject {
		t.Error("Function with non-In parameter should not be detected as param object")
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

// Mock resolver for testing
type mockResolver struct {
	shouldFail bool
	failError  error
	resolved   map[string]any
}

func (m *mockResolver) Resolve(t reflect.Type) (any, error) {
	if m.shouldFail {
		return nil, m.failError
	}
	if m.resolved != nil {
		return m.resolved[t.String()], nil
	}
	return reflect.New(t.Elem()).Interface(), nil
}

func (m *mockResolver) ResolveKeyed(t reflect.Type, key any) (any, error) {
	if m.shouldFail {
		return nil, m.failError
	}
	return m.Resolve(t)
}

func (m *mockResolver) ResolveGroup(t reflect.Type, group string) ([]any, error) {
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
	if err != nil {
		t.Fatalf("Failed to analyze constructor1: %v", err)
	}

	info2, err := analyzer.Analyze(constructor2)
	if err != nil {
		t.Fatalf("Failed to analyze constructor2: %v", err)
	}

	info3, err := analyzer.Analyze(constructor3)
	if err != nil {
		t.Fatalf("Failed to analyze constructor3: %v", err)
	}

	// Each closure should have its own ConstructorInfo
	if info1 == info2 || info1 == info3 || info2 == info3 {
		t.Error("Expected different ConstructorInfo for different closures")
	}

	// Verify each produces the correct result
	results1 := info1.Value.Call([]reflect.Value{})
	db1 := results1[0].Interface().(*Database)
	if db1.ConnectionString != "db1" {
		t.Errorf("Constructor 1: expected db1, got %s", db1.ConnectionString)
	}

	results2 := info2.Value.Call([]reflect.Value{})
	db2 := results2[0].Interface().(*Database)
	if db2.ConnectionString != "db2" {
		t.Errorf("Constructor 2: expected db2, got %s", db2.ConnectionString)
	}

	results3 := info3.Value.Call([]reflect.Value{})
	db3 := results3[0].Interface().(*Database)
	if db3.ConnectionString != "db3" {
		t.Errorf("Constructor 3: expected db3, got %s", db3.ConnectionString)
	}
}
