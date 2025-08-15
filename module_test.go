package godi

import (
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"
)

// Test service types for module tests
type ModuleTestService struct{}

func NewModuleTestService() *ModuleTestService {
	return &ModuleTestService{}
}

type ModuleDependency struct{}

func NewModuleDependency() *ModuleDependency {
	return &ModuleDependency{}
}

type ModuleComplexService struct {
	dep *ModuleDependency
}

func NewModuleComplexService(dep *ModuleDependency) *ModuleComplexService {
	return &ModuleComplexService{dep: dep}
}

// Mock collection for testing
type mockCollection struct {
	addSingletonCalls  int
	addScopedCalls     int
	addTransientCalls  int
	decorateCalls      int
	lastConstructor    any
	lastOpts           []AddOption
	returnError        error
}

func (m *mockCollection) AddSingleton(constructor any, opts ...AddOption) error {
	m.addSingletonCalls++
	m.lastConstructor = constructor
	m.lastOpts = opts
	return m.returnError
}

func (m *mockCollection) AddScoped(constructor any, opts ...AddOption) error {
	m.addScopedCalls++
	m.lastConstructor = constructor
	m.lastOpts = opts
	return m.returnError
}

func (m *mockCollection) AddTransient(constructor any, opts ...AddOption) error {
	m.addTransientCalls++
	m.lastConstructor = constructor
	m.lastOpts = opts
	return m.returnError
}

func (m *mockCollection) Decorate(decorator any, opts ...AddOption) error {
	m.decorateCalls++
	m.lastConstructor = decorator
	m.lastOpts = opts
	return m.returnError
}

func (m *mockCollection) AddModules(modules ...ModuleOption) error {
	return nil
}

func (m *mockCollection) Build() (Provider, error) {
	return nil, nil
}

func (m *mockCollection) BuildWithOptions(options *ProviderOptions) (Provider, error) {
	return nil, nil
}

func (m *mockCollection) Contains(serviceType reflect.Type) bool {
	return false
}

func (m *mockCollection) ContainsKeyed(serviceType reflect.Type, key any) bool {
	return false
}

func (m *mockCollection) Remove(serviceType reflect.Type) {
}

func (m *mockCollection) RemoveKeyed(serviceType reflect.Type, key any) {
}

func (m *mockCollection) ToSlice() []*Descriptor {
	return nil
}

func (m *mockCollection) Count() int {
	return 0
}

// TestNewModule tests the NewModule function
func TestNewModule(t *testing.T) {
	t.Run("simple module", func(t *testing.T) {
		collection := &mockCollection{}
		
		module := NewModule("test",
			AddSingleton(NewModuleTestService),
			AddScoped(NewModuleDependency),
		)
		
		err := module(collection)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		
		if collection.addSingletonCalls != 1 {
			t.Errorf("Expected 1 AddSingleton call, got %d", collection.addSingletonCalls)
		}
		if collection.addScopedCalls != 1 {
			t.Errorf("Expected 1 AddScoped call, got %d", collection.addScopedCalls)
		}
	})
	
	t.Run("module with nil builder", func(t *testing.T) {
		collection := &mockCollection{}
		
		module := NewModule("test",
			AddSingleton(NewModuleTestService),
			nil, // nil builder should be skipped
			AddScoped(NewModuleDependency),
		)
		
		err := module(collection)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		
		if collection.addSingletonCalls != 1 {
			t.Errorf("Expected 1 AddSingleton call, got %d", collection.addSingletonCalls)
		}
		if collection.addScopedCalls != 1 {
			t.Errorf("Expected 1 AddScoped call, got %d", collection.addScopedCalls)
		}
	})
	
	t.Run("module with error", func(t *testing.T) {
		expectedError := errors.New("registration failed")
		collection := &mockCollection{returnError: expectedError}
		
		module := NewModule("test",
			AddSingleton(NewModuleTestService),
		)
		
		err := module(collection)
		if err == nil {
			t.Error("Expected error, got nil")
		}
		
		// Check that it's wrapped in ModuleError
		var moduleErr ModuleError
		if !errors.As(err, &moduleErr) {
			t.Errorf("Expected ModuleError, got %T", err)
		}
		if moduleErr.Module != "test" {
			t.Errorf("Expected module name 'test', got %q", moduleErr.Module)
		}
		if !errors.Is(err, expectedError) {
			t.Error("Error should wrap the original error")
		}
	})
	
	t.Run("nested modules", func(t *testing.T) {
		collection := &mockCollection{}
		
		innerModule := NewModule("inner",
			AddSingleton(NewModuleDependency),
		)
		
		outerModule := NewModule("outer",
			innerModule,
			AddScoped(NewModuleTestService),
		)
		
		err := outerModule(collection)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		
		if collection.addSingletonCalls != 1 {
			t.Errorf("Expected 1 AddSingleton call, got %d", collection.addSingletonCalls)
		}
		if collection.addScopedCalls != 1 {
			t.Errorf("Expected 1 AddScoped call, got %d", collection.addScopedCalls)
		}
	})
}

// TestAddTransient tests the AddTransient ModuleOption
func TestAddTransient(t *testing.T) {
	t.Run("without options", func(t *testing.T) {
		collection := &mockCollection{}
		option := AddTransient(NewModuleTestService)
		
		err := option(collection)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		
		if collection.addTransientCalls != 1 {
			t.Errorf("Expected 1 AddTransient call, got %d", collection.addTransientCalls)
		}
	})
	
	t.Run("with options", func(t *testing.T) {
		collection := &mockCollection{}
		option := AddTransient(NewModuleTestService, Name("test"), Group("group"))
		
		err := option(collection)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		
		if collection.addTransientCalls != 1 {
			t.Errorf("Expected 1 AddTransient call, got %d", collection.addTransientCalls)
		}
		if len(collection.lastOpts) != 2 {
			t.Errorf("Expected 2 options, got %d", len(collection.lastOpts))
		}
	})
	
	t.Run("with error", func(t *testing.T) {
		expectedError := errors.New("transient registration failed")
		collection := &mockCollection{returnError: expectedError}
		option := AddTransient(NewModuleTestService)
		
		err := option(collection)
		if !errors.Is(err, expectedError) {
			t.Errorf("Expected error %v, got %v", expectedError, err)
		}
	})
}

// TestAddDecorator tests the AddDecorator ModuleOption
func TestAddDecorator(t *testing.T) {
	decorator := func(svc *ModuleTestService) *ModuleTestService {
		return svc
	}
	
	t.Run("without options", func(t *testing.T) {
		collection := &mockCollection{}
		option := AddDecorator(decorator)
		
		err := option(collection)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		
		if collection.decorateCalls != 1 {
			t.Errorf("Expected 1 Decorate call, got %d", collection.decorateCalls)
		}
	})
	
	t.Run("with options", func(t *testing.T) {
		collection := &mockCollection{}
		option := AddDecorator(decorator, Name("decorated"))
		
		err := option(collection)
		if err != nil {
			t.Errorf("Unexpected error: %v", err)
		}
		
		if collection.decorateCalls != 1 {
			t.Errorf("Expected 1 Decorate call, got %d", collection.decorateCalls)
		}
		if len(collection.lastOpts) != 1 {
			t.Errorf("Expected 1 option, got %d", len(collection.lastOpts))
		}
	})
	
	t.Run("with error", func(t *testing.T) {
		expectedError := errors.New("decorator registration failed")
		collection := &mockCollection{returnError: expectedError}
		option := AddDecorator(decorator)
		
		err := option(collection)
		if !errors.Is(err, expectedError) {
			t.Errorf("Expected error %v, got %v", expectedError, err)
		}
	})
}

// TestAddNameOption_String tests the String method of addNameOption
func TestAddNameOption_String(t *testing.T) {
	tests := []struct {
		name     string
		option   AddOption
		expected string
	}{
		{
			name:     "simple name",
			option:   Name("test"),
			expected: `Name("test")`,
		},
		{
			name:     "empty name",
			option:   Name(""),
			expected: `Name("")`,
		},
		{
			name:     "special characters",
			option:   Name("test-name_123"),
			expected: `Name("test-name_123")`,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Type assert to get the String method
			if stringer, ok := tt.option.(interface{ String() string }); ok {
				if got := stringer.String(); got != tt.expected {
					t.Errorf("String() = %q, want %q", got, tt.expected)
				}
			} else {
				t.Error("Name option should implement String()")
			}
		})
	}
}

// TestAddGroupOption_String tests the String method of addGroupOption
func TestAddGroupOption_String(t *testing.T) {
	tests := []struct {
		name     string
		option   AddOption
		expected string
	}{
		{
			name:     "simple group",
			option:   Group("test"),
			expected: `Group("test")`,
		},
		{
			name:     "empty group",
			option:   Group(""),
			expected: `Group("")`,
		},
		{
			name:     "special characters",
			option:   Group("test-group_123"),
			expected: `Group("test-group_123")`,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Type assert to get the String method
			if stringer, ok := tt.option.(interface{ String() string }); ok {
				if got := stringer.String(); got != tt.expected {
					t.Errorf("String() = %q, want %q", got, tt.expected)
				}
			} else {
				t.Error("Group option should implement String()")
			}
		})
	}
}

// TestAddAsOption_String tests the String method of addAsOption
func TestAddAsOption_String(t *testing.T) {
	tests := []struct {
		name     string
		option   AddOption
		contains []string
	}{
		{
			name:     "single interface",
			option:   As(new(io.Reader)),
			contains: []string{"As(", "io.Reader", ")"},
		},
		{
			name:     "multiple interfaces",
			option:   As(new(io.Reader), new(io.Writer)),
			contains: []string{"As(", "io.Reader", "io.Writer", ")"},
		},
		{
			name:     "three interfaces",
			option:   As(new(io.Reader), new(io.Writer), new(io.Closer)),
			contains: []string{"As(", "io.Reader", "io.Writer", "io.Closer", ")"},
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Type assert to get the String method
			if stringer, ok := tt.option.(interface{ String() string }); ok {
				got := stringer.String()
				for _, expected := range tt.contains {
					if !strings.Contains(got, expected) {
						t.Errorf("String() should contain %q, got %q", expected, got)
					}
				}
			} else {
				t.Error("As option should implement String()")
			}
		})
	}
}

// TestAddOptions_Validate tests the Validate method of addOptions
func TestAddOptions_Validate(t *testing.T) {
	tests := []struct {
		name    string
		options addOptions
		wantErr bool
		errMsg  string
	}{
		{
			name:    "valid empty options",
			options: addOptions{},
			wantErr: false,
		},
		{
			name:    "valid name only",
			options: addOptions{Name: "test"},
			wantErr: false,
		},
		{
			name:    "valid group only",
			options: addOptions{Group: "test"},
			wantErr: false,
		},
		{
			name:    "both name and group",
			options: addOptions{Name: "test", Group: "group"},
			wantErr: true,
			errMsg:  "cannot use both godi.Name and godi.Group",
		},
		{
			name:    "name with backquote",
			options: addOptions{Name: "test`name"},
			wantErr: true,
			errMsg:  "names cannot contain backquotes",
		},
		{
			name:    "group with backquote",
			options: addOptions{Group: "test`group"},
			wantErr: true,
			errMsg:  "group names cannot contain backquotes",
		},
		{
			name:    "valid As with interface",
			options: addOptions{As: []any{new(io.Reader)}},
			wantErr: false,
		},
		{
			name:    "As with nil",
			options: addOptions{As: []any{nil}},
			wantErr: true,
			errMsg:  "argument must be a pointer to an interface",
		},
		{
			name:    "As with non-pointer",
			options: addOptions{As: []any{io.Reader(nil)}},
			wantErr: true,
			errMsg:  "argument must be a pointer to an interface",
		},
		{
			name:    "As with pointer to struct",
			options: addOptions{As: []any{&ModuleTestService{}}},
			wantErr: true,
			errMsg:  "argument must be a pointer to an interface",
		},
		{
			name:    "multiple valid As interfaces",
			options: addOptions{As: []any{new(io.Reader), new(io.Writer)}},
			wantErr: false,
		},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.options.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
				t.Errorf("Error message should contain %q, got %q", tt.errMsg, err.Error())
			}
		})
	}
}

// TestModuleOptions_Integration tests module options in an integration scenario
func TestModuleOptions_Integration(t *testing.T) {
	// Use real collection to test integration
	c := NewCollection()
	
	// Create a module with various options
	testModule := NewModule("integration",
		AddSingleton(NewModuleDependency),
		AddScoped(NewModuleTestService, Name("test")),
		AddTransient(NewModuleComplexService, Group("complex")),
	)
	
	// Apply the module
	err := c.AddModules(testModule)
	if err != nil {
		t.Fatalf("Failed to add module: %v", err)
	}
	
	// Verify registrations
	coll, ok := c.(*collection)
	if !ok {
		t.Fatal("Failed to type assert collection")
	}
	
	// Check singleton
	if !coll.Contains(reflect.TypeOf(&ModuleDependency{})) {
		t.Error("ModuleDependency should be registered")
	}
	
	// Check scoped with name
	if !coll.ContainsKeyed(reflect.TypeOf(&ModuleTestService{}), "test") {
		t.Error("ModuleTestService should be registered with name 'test'")
	}
	
	// Check transient with group
	if !coll.HasGroup(reflect.TypeOf(&ModuleComplexService{}), "complex") {
		t.Error("ModuleComplexService should be registered in group 'complex'")
	}
}