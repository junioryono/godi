package godi

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// Test types for collection tests
type TestService struct {
	Value string
}

func NewTestService() *TestService {
	return &TestService{Value: "test"}
}

type TestServiceWithDep struct {
	Service *TestService
}

func NewTestServiceWithDep(service *TestService) *TestServiceWithDep {
	return &TestServiceWithDep{Service: service}
}

// TestCollectionAddService tests the addService method
func TestCollectionAddService(t *testing.T) {
	tests := []struct {
		name        string
		constructor any
		lifetime    Lifetime
		opts        []AddOption
		wantErr     bool
		errContains string
	}{
		{
			name:        "valid singleton service",
			constructor: NewTestService,
			lifetime:    Singleton,
			opts:        nil,
			wantErr:     false,
		},
		{
			name:        "valid scoped service",
			constructor: NewTestService,
			lifetime:    Scoped,
			opts:        nil,
			wantErr:     false,
		},
		{
			name:        "valid transient service",
			constructor: NewTestService,
			lifetime:    Transient,
			opts:        nil,
			wantErr:     false,
		},
		{
			name:        "service with name",
			constructor: NewTestService,
			lifetime:    Singleton,
			opts:        []AddOption{Name("test")},
			wantErr:     false,
		},
		{
			name:        "service with group",
			constructor: NewTestService,
			lifetime:    Singleton,
			opts:        []AddOption{Group("services")},
			wantErr:     false,
		},
		{
			name:        "nil constructor",
			constructor: nil,
			lifetime:    Singleton,
			opts:        nil,
			wantErr:     true,
			errContains: "constructor cannot be nil",
		},
		{
			name:        "non-function constructor",
			constructor: "not a function",
			lifetime:    Singleton,
			opts:        nil,
			wantErr:     true,
			errContains: "constructor must be a function",
		},
		{
			name:        "function with no returns",
			constructor: func() {},
			lifetime:    Singleton,
			opts:        nil,
			wantErr:     true,
			errContains: "constructor must return at least one value",
		},
		{
			name:        "function with too many returns",
			constructor: func() (int, string, error) { return 0, "", nil },
			lifetime:    Singleton,
			opts:        nil,
			wantErr:     true,
			errContains: "constructor must return at most 2 values",
		},
		{
			name:        "function with non-error second return",
			constructor: func() (int, string) { return 0, "" },
			lifetime:    Singleton,
			opts:        nil,
			wantErr:     true,
			errContains: "constructor's second return value must be error",
		},
		{
			name:        "valid function with error return",
			constructor: func() (*TestService, error) { return NewTestService(), nil },
			lifetime:    Singleton,
			opts:        nil,
			wantErr:     false,
		},
		{
			name:        "service with dependencies",
			constructor: NewTestServiceWithDep,
			lifetime:    Singleton,
			opts:        nil,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collection := NewCollection().(*collection)
			err := collection.addService(tt.constructor, tt.lifetime, tt.opts...)

			if tt.wantErr {
				if err == nil {
					t.Errorf("addService() error = nil, wantErr %v", tt.wantErr)
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("addService() error = %v, want error containing %s", err, tt.errContains)
				}
			} else {
				if err != nil {
					t.Errorf("addService() unexpected error = %v", err)
					return
				}

				// Verify the service was registered
				if tt.constructor != nil {
					serviceType := reflect.TypeOf(tt.constructor).Out(0)

					// Check if it's a named service
					var hasName bool
					for _, opt := range tt.opts {
						if nameOpt, ok := opt.(addNameOption); ok {
							hasName = true
							if !collection.HasKeyedService(serviceType, string(nameOpt)) {
								t.Errorf("Named service not registered")
							}
							break
						}
					}

					// Check if it's a grouped service
					var hasGroup bool
					for _, opt := range tt.opts {
						if groupOpt, ok := opt.(addGroupOption); ok {
							hasGroup = true
							if !collection.HasGroup(serviceType, string(groupOpt)) {
								t.Errorf("Grouped service not registered")
							}
							break
						}
					}

					// If not named or grouped, check regular service
					if !hasName && !hasGroup {
						if !collection.HasService(serviceType) {
							t.Errorf("Service not registered")
						}
					}
				}
			}
		})
	}
}

// TestCollectionAddServiceLifetimeConflict tests lifetime conflict detection
func TestCollectionAddServiceLifetimeConflict(t *testing.T) {
	collection := NewCollection().(*collection)

	// Register as singleton first
	err := collection.AddSingleton(NewTestService)
	if err != nil {
		t.Fatalf("Failed to add singleton: %v", err)
	}

	// Try to register the same type as scoped
	err = collection.AddScoped(NewTestService)
	if err == nil {
		t.Error("Expected lifetime conflict error, got nil")
	} else if !strings.Contains(err.Error(), "already registered") {
		t.Errorf("Expected lifetime conflict error, got: %v", err)
	}

	// Named services should not conflict
	err = collection.AddScoped(NewTestService, Name("scoped"))
	if err != nil {
		t.Errorf("Named service should not conflict: %v", err)
	}

	// Grouped services should not conflict
	err = collection.AddTransient(NewTestService, Group("transient"))
	if err != nil {
		t.Errorf("Grouped service should not conflict: %v", err)
	}
}

// Define test interfaces for As option tests
type Reader interface {
	Read() string
}

type Writer interface {
	Write(string)
}

// Service that implements both interfaces
type FileService struct{}

func (f *FileService) Read() string   { return "data" }
func (f *FileService) Write(s string) {}

func NewFileService() *FileService {
	return &FileService{}
}

// TestCollectionAddServiceWithAs tests the As option
func TestCollectionAddServiceWithAs(t *testing.T) {

	collection := NewCollection().(*collection)

	// Register with As option
	err := collection.AddSingleton(NewFileService, As(new(Reader), new(Writer)))
	if err != nil {
		t.Fatalf("Failed to add service with As: %v", err)
	}

	// Check that interfaces are registered
	readerType := reflect.TypeOf((*Reader)(nil)).Elem()
	writerType := reflect.TypeOf((*Writer)(nil)).Elem()
	fileServiceType := reflect.TypeOf(&FileService{})

	if !collection.HasService(readerType) {
		t.Error("Reader interface not registered")
	}

	if !collection.HasService(writerType) {
		t.Error("Writer interface not registered")
	}

	// The concrete type should not be registered when As is used
	if collection.HasService(fileServiceType) {
		t.Error("Concrete type should not be registered when As is used")
	}
}

// UnimplementedInterface for testing invalid As option
type UnimplementedInterface interface {
	NotImplemented()
}

// TestCollectionAddServiceInvalidAs tests invalid As option usage
func TestCollectionAddServiceInvalidAs(t *testing.T) {
	collection := NewCollection().(*collection)

	// Try to register with an interface the service doesn't implement
	err := collection.AddSingleton(NewTestService, As(new(UnimplementedInterface)))
	if err == nil {
		t.Error("Expected error for unimplemented interface, got nil")
	} else if !strings.Contains(err.Error(), "does not implement interface") {
		t.Errorf("Expected unimplemented interface error, got: %v", err)
	}
}

// ============================================================================
// Test Types and Helpers
// ============================================================================

type Logger struct {
	Level string
}

func NewLogger() *Logger {
	return &Logger{Level: "info"}
}

func NewLoggerWithError(fail bool) (*Logger, error) {
	if fail {
		return nil, errors.New("logger creation failed")
	}
	return &Logger{Level: "info"}, nil
}

type Database struct {
	ConnectionString string
}

func NewDatabase() *Database {
	return &Database{ConnectionString: "localhost"}
}

type Repository struct {
	DB     *Database
	Logger *Logger
}

func NewRepository(db *Database, logger *Logger) *Repository {
	return &Repository{DB: db, Logger: logger}
}

type Service struct {
	Repo   *Repository
	Logger *Logger
}

func NewService(repo *Repository, logger *Logger) *Service {
	return &Service{Repo: repo, Logger: logger}
}

// Interfaces for testing
type Storage interface {
	Store(key string, value any)
	Retrieve(key string) any
}

type Cache interface {
	Get(key string) any
	Set(key string, value any)
}

type MemoryStorage struct {
	data map[string]any
}

func (m *MemoryStorage) Store(key string, value any) {
	if m.data == nil {
		m.data = make(map[string]any)
	}
	m.data[key] = value
}

func (m *MemoryStorage) Retrieve(key string) any {
	return m.data[key]
}

func (m *MemoryStorage) Get(key string) any {
	return m.data[key]
}

func (m *MemoryStorage) Set(key string, value any) {
	if m.data == nil {
		m.data = make(map[string]any)
	}
	m.data[key] = value
}

func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{data: make(map[string]any)}
}

// Circular dependency types
type CircularA struct {
	B *CircularB
}

type CircularB struct {
	A *CircularA
}

func NewCircularA(b *CircularB) *CircularA {
	return &CircularA{B: b}
}

func NewCircularB(a *CircularA) *CircularB {
	return &CircularB{A: a}
}

// ============================================================================
// Collection Interface Tests
// ============================================================================

func TestCollection_Build(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(Collection)
		wantErr bool
	}{
		{
			name: "empty collection builds successfully",
			setup: func(c Collection) {
				// No services added
			},
			wantErr: false,
		},
		{
			name: "collection with single service",
			setup: func(c Collection) {
				c.AddSingleton(NewLogger)
			},
			wantErr: false,
		},
		{
			name: "collection with dependencies",
			setup: func(c Collection) {
				c.AddSingleton(NewDatabase)
				c.AddSingleton(NewLogger)
				c.AddScoped(NewRepository)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCollection()
			if tt.setup != nil {
				tt.setup(c)
			}

			provider, err := c.Build()
			if tt.wantErr {
				if err == nil {
					t.Error("Build() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Build() unexpected error: %v", err)
				}
				if provider == nil && !tt.wantErr {
					t.Error("Build() returned nil provider")
				}
			}
		})
	}
}

func TestCollection_AddModules(t *testing.T) {
	// Create test modules
	loggerModule := NewModule("logger",
		AddSingleton(NewLogger),
	)

	databaseModule := NewModule("database",
		AddSingleton(NewDatabase),
	)

	repositoryModule := NewModule("repository",
		loggerModule,
		databaseModule,
		AddScoped(NewRepository),
	)

	tests := []struct {
		name    string
		modules []ModuleOption
		wantErr bool
		verify  func(*testing.T, Collection)
	}{
		{
			name:    "single module",
			modules: []ModuleOption{loggerModule},
			wantErr: false,
			verify: func(t *testing.T, c Collection) {
				col := c.(*collection)
				if !col.Contains(reflect.TypeOf(&Logger{})) {
					t.Error("Logger not registered")
				}
			},
		},
		{
			name:    "multiple modules",
			modules: []ModuleOption{loggerModule, databaseModule},
			wantErr: false,
			verify: func(t *testing.T, c Collection) {
				col := c.(*collection)
				if !col.Contains(reflect.TypeOf(&Logger{})) {
					t.Error("Logger not registered")
				}
				if !col.Contains(reflect.TypeOf(&Database{})) {
					t.Error("Database not registered")
				}
			},
		},
		{
			name:    "nested modules",
			modules: []ModuleOption{repositoryModule},
			wantErr: false,
			verify: func(t *testing.T, c Collection) {
				col := c.(*collection)
				if !col.Contains(reflect.TypeOf(&Logger{})) {
					t.Error("Logger not registered")
				}
				if !col.Contains(reflect.TypeOf(&Database{})) {
					t.Error("Database not registered")
				}
				if !col.Contains(reflect.TypeOf(&Repository{})) {
					t.Error("Repository not registered")
				}
			},
		},
		{
			name:    "nil module ignored",
			modules: []ModuleOption{nil, loggerModule, nil},
			wantErr: false,
			verify: func(t *testing.T, c Collection) {
				col := c.(*collection)
				if !col.Contains(reflect.TypeOf(&Logger{})) {
					t.Error("Logger not registered")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCollection()
			err := c.AddModules(tt.modules...)

			if tt.wantErr {
				if err == nil {
					t.Error("AddModules() expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("AddModules() unexpected error: %v", err)
				}
				if tt.verify != nil {
					tt.verify(t, c)
				}
			}
		})
	}
}

func TestCollection_Contains(t *testing.T) {
	c := NewCollection()

	// Add various services
	c.AddSingleton(NewLogger)
	c.AddSingleton(NewDatabase, Name("primary"))
	c.AddScoped(NewRepository, Group("repos"))

	tests := []struct {
		name     string
		svcType  reflect.Type
		expected bool
	}{
		{
			name:     "registered singleton",
			svcType:  reflect.TypeOf(&Logger{}),
			expected: true,
		},
		{
			name:     "unregistered type",
			svcType:  reflect.TypeOf(&Service{}),
			expected: false,
		},
		{
			name:     "named service (should be in regular services too)",
			svcType:  reflect.TypeOf(&Database{}),
			expected: false, // Named services are not in regular services
		},
		{
			name:     "grouped service (should be in regular services too)",
			svcType:  reflect.TypeOf(&Repository{}),
			expected: false, // Grouped services are not in regular services
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.Contains(tt.svcType); got != tt.expected {
				t.Errorf("Contains() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCollection_ContainsKeyed(t *testing.T) {
	c := NewCollection()

	// Add keyed services
	c.AddSingleton(NewDatabase, Name("primary"))
	c.AddSingleton(NewDatabase, Name("secondary"))
	c.AddScoped(NewLogger, Name("app"))

	tests := []struct {
		name     string
		svcType  reflect.Type
		key      any
		expected bool
	}{
		{
			name:     "existing keyed service",
			svcType:  reflect.TypeOf(&Database{}),
			key:      "primary",
			expected: true,
		},
		{
			name:     "existing keyed service with different key",
			svcType:  reflect.TypeOf(&Database{}),
			key:      "secondary",
			expected: true,
		},
		{
			name:     "non-existing key for registered type",
			svcType:  reflect.TypeOf(&Database{}),
			key:      "tertiary",
			expected: false,
		},
		{
			name:     "non-existing type",
			svcType:  reflect.TypeOf(&Repository{}),
			key:      "primary",
			expected: false,
		},
		{
			name:     "scoped keyed service",
			svcType:  reflect.TypeOf(&Logger{}),
			key:      "app",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := c.ContainsKeyed(tt.svcType, tt.key); got != tt.expected {
				t.Errorf("ContainsKeyed() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestCollection_Remove(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(Collection)
		remove reflect.Type
		verify func(*testing.T, Collection)
	}{
		{
			name: "remove singleton service",
			setup: func(c Collection) {
				c.AddSingleton(NewLogger)
				c.AddSingleton(NewDatabase)
			},
			remove: reflect.TypeOf(&Logger{}),
			verify: func(t *testing.T, c Collection) {
				if c.Contains(reflect.TypeOf(&Logger{})) {
					t.Error("Logger should be removed")
				}
				if !c.Contains(reflect.TypeOf(&Database{})) {
					t.Error("Database should still exist")
				}
			},
		},
		{
			name: "remove keyed services",
			setup: func(c Collection) {
				c.AddSingleton(NewDatabase, Name("primary"))
				c.AddSingleton(NewDatabase, Name("secondary"))
			},
			remove: reflect.TypeOf(&Database{}),
			verify: func(t *testing.T, c Collection) {
				if c.ContainsKeyed(reflect.TypeOf(&Database{}), "primary") {
					t.Error("Primary database should be removed")
				}
				if c.ContainsKeyed(reflect.TypeOf(&Database{}), "secondary") {
					t.Error("Secondary database should be removed")
				}
			},
		},
		{
			name: "remove grouped services",
			setup: func(c Collection) {
				c.AddSingleton(NewLogger, Group("logging"))
				c.AddSingleton(NewDatabase, Group("data"))
			},
			remove: reflect.TypeOf(&Logger{}),
			verify: func(t *testing.T, c Collection) {
				col := c.(*collection)
				if col.HasGroup(reflect.TypeOf(&Logger{}), "logging") {
					t.Error("Logger group should be removed")
				}
				if !col.HasGroup(reflect.TypeOf(&Database{}), "data") {
					t.Error("Database group should still exist")
				}
			},
		},
		{
			name: "remove non-existing type",
			setup: func(c Collection) {
				c.AddSingleton(NewLogger)
			},
			remove: reflect.TypeOf(&Service{}),
			verify: func(t *testing.T, c Collection) {
				// Should not panic
				if !c.Contains(reflect.TypeOf(&Logger{})) {
					t.Error("Logger should still exist")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCollection()
			if tt.setup != nil {
				tt.setup(c)
			}

			c.Remove(tt.remove)

			if tt.verify != nil {
				tt.verify(t, c)
			}
		})
	}
}

func TestCollection_RemoveKeyed(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(Collection)
		remove struct {
			typ reflect.Type
			key any
		}
		verify func(*testing.T, Collection)
	}{
		{
			name: "remove specific keyed service",
			setup: func(c Collection) {
				c.AddSingleton(NewDatabase, Name("primary"))
				c.AddSingleton(NewDatabase, Name("secondary"))
			},
			remove: struct {
				typ reflect.Type
				key any
			}{
				typ: reflect.TypeOf(&Database{}),
				key: "primary",
			},
			verify: func(t *testing.T, c Collection) {
				if c.ContainsKeyed(reflect.TypeOf(&Database{}), "primary") {
					t.Error("Primary database should be removed")
				}
				if !c.ContainsKeyed(reflect.TypeOf(&Database{}), "secondary") {
					t.Error("Secondary database should still exist")
				}
			},
		},
		{
			name: "remove non-existing keyed service",
			setup: func(c Collection) {
				c.AddSingleton(NewDatabase, Name("primary"))
			},
			remove: struct {
				typ reflect.Type
				key any
			}{
				typ: reflect.TypeOf(&Database{}),
				key: "secondary",
			},
			verify: func(t *testing.T, c Collection) {
				if !c.ContainsKeyed(reflect.TypeOf(&Database{}), "primary") {
					t.Error("Primary database should still exist")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCollection()
			if tt.setup != nil {
				tt.setup(c)
			}

			c.RemoveKeyed(tt.remove.typ, tt.remove.key)

			if tt.verify != nil {
				tt.verify(t, c)
			}
		})
	}
}

func TestCollection_ToSlice(t *testing.T) {
	c := NewCollection()

	// Add various types of services
	c.AddSingleton(NewLogger)
	c.AddSingleton(NewDatabase, Name("primary"))
	c.AddScoped(NewRepository, Group("repos"))
	c.AddTransient(NewService)

	descriptors := c.ToSlice()

	if len(descriptors) != 4 {
		t.Errorf("ToSlice() returned %d descriptors, expected 4", len(descriptors))
	}

	// Verify all services are present
	types := make(map[reflect.Type]bool)
	for _, d := range descriptors {
		types[d.Type] = true
	}

	expectedTypes := []reflect.Type{
		reflect.TypeOf(&Logger{}),
		reflect.TypeOf(&Database{}),
		reflect.TypeOf(&Repository{}),
		reflect.TypeOf(&Service{}),
	}

	for _, typ := range expectedTypes {
		if !types[typ] {
			t.Errorf("Type %v not found in descriptors", typ)
		}
	}
}

func TestCollection_Count(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(Collection)
		expected int
	}{
		{
			name:     "empty collection",
			setup:    func(c Collection) {},
			expected: 0,
		},
		{
			name: "single service",
			setup: func(c Collection) {
				c.AddSingleton(NewLogger)
			},
			expected: 1,
		},
		{
			name: "multiple services",
			setup: func(c Collection) {
				c.AddSingleton(NewLogger)
				c.AddSingleton(NewDatabase)
				c.AddScoped(NewRepository)
			},
			expected: 3,
		},
		{
			name: "services with keys",
			setup: func(c Collection) {
				c.AddSingleton(NewDatabase, Name("primary"))
				c.AddSingleton(NewDatabase, Name("secondary"))
			},
			expected: 2,
		},
		{
			name: "services with groups",
			setup: func(c Collection) {
				c.AddSingleton(NewLogger, Group("logging"))
				c.AddSingleton(NewDatabase, Group("data"))
			},
			expected: 2,
		},
		{
			name: "mixed services",
			setup: func(c Collection) {
				c.AddSingleton(NewLogger)
				c.AddSingleton(NewDatabase, Name("primary"))
				c.AddScoped(NewRepository, Group("repos"))
			},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCollection()
			if tt.setup != nil {
				tt.setup(c)
			}

			if got := c.Count(); got != tt.expected {
				t.Errorf("Count() = %d, want %d", got, tt.expected)
			}
		})
	}
}

// ============================================================================
// Lifetime Tests
// ============================================================================

func TestCollection_LifetimeRegistration(t *testing.T) {
	tests := []struct {
		name     string
		register func(Collection) error
		wantErr  bool
	}{
		{
			name: "singleton registration",
			register: func(c Collection) error {
				return c.AddSingleton(NewLogger)
			},
			wantErr: false,
		},
		{
			name: "scoped registration",
			register: func(c Collection) error {
				return c.AddScoped(NewLogger)
			},
			wantErr: false,
		},
		{
			name: "transient registration",
			register: func(c Collection) error {
				return c.AddTransient(NewLogger)
			},
			wantErr: false,
		},
		{
			name: "conflicting lifetime - singleton then scoped",
			register: func(c Collection) error {
				if err := c.AddSingleton(NewLogger); err != nil {
					return err
				}
				return c.AddScoped(NewLogger)
			},
			wantErr: true,
		},
		{
			name: "conflicting lifetime - scoped then transient",
			register: func(c Collection) error {
				if err := c.AddScoped(NewLogger); err != nil {
					return err
				}
				return c.AddTransient(NewLogger)
			},
			wantErr: true,
		},
		{
			name: "same lifetime multiple times",
			register: func(c Collection) error {
				if err := c.AddSingleton(NewLogger); err != nil {
					return err
				}
				// Should succeed - same lifetime
				return c.AddSingleton(func() *Logger { return &Logger{Level: "debug"} })
			},
			wantErr: false,
		},
		{
			name: "keyed services can have different lifetimes",
			register: func(c Collection) error {
				if err := c.AddSingleton(NewLogger, Name("singleton")); err != nil {
					return err
				}
				return c.AddScoped(NewLogger, Name("scoped"))
			},
			wantErr: false,
		},
		{
			name: "grouped services can have different lifetimes",
			register: func(c Collection) error {
				if err := c.AddSingleton(NewLogger, Group("singleton")); err != nil {
					return err
				}
				return c.AddTransient(NewLogger, Group("transient"))
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCollection()
			err := tt.register(c)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// ============================================================================
// Options Tests
// ============================================================================

func TestCollection_AsOption(t *testing.T) {
	tests := []struct {
		name     string
		register func(Collection) error
		verify   func(*testing.T, Collection)
		wantErr  bool
	}{
		{
			name: "register as single interface",
			register: func(c Collection) error {
				return c.AddSingleton(NewMemoryStorage, As(new(Storage)))
			},
			verify: func(t *testing.T, c Collection) {
				if !c.Contains(reflect.TypeOf((*Storage)(nil)).Elem()) {
					t.Error("Storage interface not registered")
				}
				if c.Contains(reflect.TypeOf(&MemoryStorage{})) {
					t.Error("Concrete type should not be registered with As")
				}
			},
			wantErr: false,
		},
		{
			name: "register as multiple interfaces",
			register: func(c Collection) error {
				return c.AddSingleton(NewMemoryStorage, As(new(Storage), new(Cache)))
			},
			verify: func(t *testing.T, c Collection) {
				if !c.Contains(reflect.TypeOf((*Storage)(nil)).Elem()) {
					t.Error("Storage interface not registered")
				}
				if !c.Contains(reflect.TypeOf((*Cache)(nil)).Elem()) {
					t.Error("Cache interface not registered")
				}
				if c.Contains(reflect.TypeOf(&MemoryStorage{})) {
					t.Error("Concrete type should not be registered with As")
				}
			},
			wantErr: false,
		},
		{
			name: "register as interface with name",
			register: func(c Collection) error {
				return c.AddSingleton(NewMemoryStorage, As(new(Storage)), Name("primary"))
			},
			verify: func(t *testing.T, c Collection) {
				if !c.ContainsKeyed(reflect.TypeOf((*Storage)(nil)).Elem(), "primary") {
					t.Error("Named Storage interface not registered")
				}
			},
			wantErr: false,
		},
		{
			name: "register as interface with group",
			register: func(c Collection) error {
				return c.AddSingleton(NewMemoryStorage, As(new(Storage)), Group("stores"))
			},
			verify: func(t *testing.T, c Collection) {
				col := c.(*collection)
				if !col.HasGroup(reflect.TypeOf((*Storage)(nil)).Elem(), "stores") {
					t.Error("Grouped Storage interface not registered")
				}
			},
			wantErr: false,
		},
		{
			name: "error when type doesn't implement interface",
			register: func(c Collection) error {
				return c.AddSingleton(NewLogger, As(new(Storage)))
			},
			verify:  nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCollection()
			err := tt.register(c)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if !strings.Contains(err.Error(), "does not implement") {
					t.Errorf("Expected 'does not implement' error, got: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if tt.verify != nil {
					tt.verify(t, c)
				}
			}
		})
	}
}

func TestCollection_NameOption(t *testing.T) {
	tests := []struct {
		name     string
		register func(Collection) error
		verify   func(*testing.T, Collection)
		wantErr  bool
	}{
		{
			name: "simple named service",
			register: func(c Collection) error {
				return c.AddSingleton(NewDatabase, Name("primary"))
			},
			verify: func(t *testing.T, c Collection) {
				if !c.ContainsKeyed(reflect.TypeOf(&Database{}), "primary") {
					t.Error("Named service not registered")
				}
			},
			wantErr: false,
		},
		{
			name: "multiple services with different names",
			register: func(c Collection) error {
				if err := c.AddSingleton(NewDatabase, Name("primary")); err != nil {
					return err
				}
				return c.AddSingleton(NewDatabase, Name("secondary"))
			},
			verify: func(t *testing.T, c Collection) {
				if !c.ContainsKeyed(reflect.TypeOf(&Database{}), "primary") {
					t.Error("Primary named service not registered")
				}
				if !c.ContainsKeyed(reflect.TypeOf(&Database{}), "secondary") {
					t.Error("Secondary named service not registered")
				}
			},
			wantErr: false,
		},
		{
			name: "name with special characters",
			register: func(c Collection) error {
				return c.AddSingleton(NewDatabase, Name("db-primary_v2.0"))
			},
			verify: func(t *testing.T, c Collection) {
				if !c.ContainsKeyed(reflect.TypeOf(&Database{}), "db-primary_v2.0") {
					t.Error("Named service with special characters not registered")
				}
			},
			wantErr: false,
		},
		{
			name: "name with backtick should fail",
			register: func(c Collection) error {
				return c.AddSingleton(NewDatabase, Name("db`primary"))
			},
			verify:  nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCollection()
			err := tt.register(c)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if tt.verify != nil {
					tt.verify(t, c)
				}
			}
		})
	}
}

func TestCollection_GroupOption(t *testing.T) {
	tests := []struct {
		name     string
		register func(Collection) error
		verify   func(*testing.T, Collection)
		wantErr  bool
	}{
		{
			name: "simple grouped service",
			register: func(c Collection) error {
				return c.AddSingleton(NewLogger, Group("loggers"))
			},
			verify: func(t *testing.T, c Collection) {
				col := c.(*collection)
				if !col.HasGroup(reflect.TypeOf(&Logger{}), "loggers") {
					t.Error("Grouped service not registered")
				}
			},
			wantErr: false,
		},
		{
			name: "multiple services in same group",
			register: func(c Collection) error {
				if err := c.AddSingleton(NewLogger, Group("services")); err != nil {
					return err
				}
				return c.AddSingleton(NewDatabase, Group("services"))
			},
			verify: func(t *testing.T, c Collection) {
				col := c.(*collection)
				if !col.HasGroup(reflect.TypeOf(&Logger{}), "services") {
					t.Error("Logger not in group")
				}
				if !col.HasGroup(reflect.TypeOf(&Database{}), "services") {
					t.Error("Database not in group")
				}
			},
			wantErr: false,
		},
		{
			name: "group with backtick should fail",
			register: func(c Collection) error {
				return c.AddSingleton(NewLogger, Group("log`gers"))
			},
			verify:  nil,
			wantErr: true,
		},
		{
			name: "cannot use both Name and Group",
			register: func(c Collection) error {
				return c.AddSingleton(NewLogger, Name("primary"), Group("loggers"))
			},
			verify:  nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCollection()
			err := tt.register(c)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
				if tt.verify != nil {
					tt.verify(t, c)
				}
			}
		})
	}
}

// ============================================================================
// Decorator Tests
// ============================================================================

func TestCollection_Decorate(t *testing.T) {
	// Decorator function
	decorateLogger := func(logger *Logger) *Logger {
		return &Logger{Level: "decorated-" + logger.Level}
	}

	tests := []struct {
		name     string
		setup    func(Collection) error
		wantErr  bool
		errMatch string
	}{
		{
			name: "decorate existing service",
			setup: func(c Collection) error {
				if err := c.AddSingleton(NewLogger); err != nil {
					return err
				}
				return c.Decorate(decorateLogger)
			},
			wantErr: false,
		},
		{
			name: "decorate non-existing service",
			setup: func(c Collection) error {
				return c.Decorate(decorateLogger)
			},
			wantErr: false, // Decorators can be registered before services
		},
		{
			name: "multiple decorators",
			setup: func(c Collection) error {
				if err := c.AddSingleton(NewLogger); err != nil {
					return err
				}
				if err := c.Decorate(decorateLogger); err != nil {
					return err
				}
				// Another decorator
				return c.Decorate(func(logger *Logger) *Logger {
					return &Logger{Level: "second-" + logger.Level}
				})
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCollection()
			err := tt.setup(c)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				} else if tt.errMatch != "" && !strings.Contains(err.Error(), tt.errMatch) {
					t.Errorf("Expected error containing %q, got: %v", tt.errMatch, err)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}

// ============================================================================
// Thread Safety Tests
// ============================================================================

func TestCollection_ThreadSafety(t *testing.T) {
	c := NewCollection()

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Concurrent additions
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Add different types of services
			if id%3 == 0 {
				if err := c.AddSingleton(NewLogger, Name(fmt.Sprintf("logger-%d", id))); err != nil {
					errors <- err
				}
			} else if id%3 == 1 {
				if err := c.AddScoped(NewDatabase, Name(fmt.Sprintf("db-%d", id))); err != nil {
					errors <- err
				}
			} else {
				if err := c.AddTransient(NewRepository, Group(fmt.Sprintf("group-%d", id))); err != nil {
					errors <- err
				}
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// Perform various read operations
			c.Contains(reflect.TypeOf(&Logger{}))
			c.ContainsKeyed(reflect.TypeOf(&Database{}), fmt.Sprintf("db-%d", id))
			c.Count()
			c.ToSlice()
		}(i)
	}

	// Concurrent removes
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			if id%2 == 0 {
				c.RemoveKeyed(reflect.TypeOf(&Logger{}), fmt.Sprintf("logger-%d", id))
			} else {
				c.Remove(reflect.TypeOf(&Service{}))
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		t.Errorf("Concurrent operation error: %v", err)
	}
}

// ============================================================================
// Edge Cases and Error Conditions
// ============================================================================

func TestCollection_EdgeCases(t *testing.T) {
	tests := []struct {
		name string
		test func(*testing.T)
	}{
		{
			name: "nil constructor",
			test: func(t *testing.T) {
				c := NewCollection()
				err := c.AddSingleton(nil)
				if err == nil {
					t.Error("Expected error for nil constructor")
				}
				if !strings.Contains(err.Error(), "constructor cannot be nil") {
					t.Errorf("Unexpected error: %v", err)
				}
			},
		},
		{
			name: "non-function constructor",
			test: func(t *testing.T) {
				c := NewCollection()
				err := c.AddSingleton("not a function")
				if err == nil {
					t.Error("Expected error for non-function constructor")
				}
				if !strings.Contains(err.Error(), "constructor must be a function") {
					t.Errorf("Unexpected error: %v", err)
				}
			},
		},
		{
			name: "function with no returns",
			test: func(t *testing.T) {
				c := NewCollection()
				err := c.AddSingleton(func() {})
				if err == nil {
					t.Error("Expected error for function with no returns")
				}
				if !strings.Contains(err.Error(), "constructor must return at least one value") {
					t.Errorf("Unexpected error: %v", err)
				}
			},
		},
		{
			name: "function with too many returns",
			test: func(t *testing.T) {
				c := NewCollection()
				err := c.AddSingleton(func() (int, string, error) { return 0, "", nil })
				if err == nil {
					t.Error("Expected error for function with too many returns")
				}
				if !strings.Contains(err.Error(), "constructor must return at most 2 values") {
					t.Errorf("Unexpected error: %v", err)
				}
			},
		},
		{
			name: "function with invalid second return",
			test: func(t *testing.T) {
				c := NewCollection()
				err := c.AddSingleton(func() (*Logger, string) { return nil, "" })
				if err == nil {
					t.Error("Expected error for invalid second return")
				}
				if !strings.Contains(err.Error(), "second return value must be error") {
					t.Errorf("Unexpected error: %v", err)
				}
			},
		},
		{
			name: "remove from empty collection",
			test: func(t *testing.T) {
				c := NewCollection()
				// Should not panic
				c.Remove(reflect.TypeOf(&Logger{}))
				c.RemoveKeyed(reflect.TypeOf(&Logger{}), "key")
			},
		},
		{
			name: "count empty collection",
			test: func(t *testing.T) {
				c := NewCollection()
				if count := c.Count(); count != 0 {
					t.Errorf("Empty collection count = %d, want 0", count)
				}
			},
		},
		{
			name: "to slice empty collection",
			test: func(t *testing.T) {
				c := NewCollection()
				slice := c.ToSlice()
				if len(slice) != 0 {
					t.Errorf("Empty collection ToSlice len = %d, want 0", len(slice))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, tt.test)
	}
}

// ============================================================================
// Complex Dependency Scenarios
// ============================================================================

func TestCollection_ComplexDependencies(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(Collection) error
		wantErr bool
	}{
		{
			name: "deep dependency chain",
			setup: func(c Collection) error {
				if err := c.AddSingleton(NewLogger); err != nil {
					return err
				}
				if err := c.AddSingleton(NewDatabase); err != nil {
					return err
				}
				if err := c.AddScoped(NewRepository); err != nil {
					return err
				}
				return c.AddTransient(NewService)
			},
			wantErr: false,
		},
		{
			name: "multiple dependencies of same type",
			setup: func(c Collection) error {
				if err := c.AddSingleton(NewLogger, Name("app")); err != nil {
					return err
				}
				if err := c.AddSingleton(NewLogger, Name("audit")); err != nil {
					return err
				}
				// Repository depends on logger - which one?
				return c.AddScoped(NewRepository)
			},
			wantErr: false, // Should work, will use non-keyed resolution
		},
		{
			name: "circular dependency",
			setup: func(c Collection) error {
				if err := c.AddSingleton(NewCircularA); err != nil {
					return err
				}
				return c.AddSingleton(NewCircularB)
			},
			wantErr: false, // Detection happens at build time
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewCollection()
			err := tt.setup(c)

			if tt.wantErr {
				if err == nil {
					t.Error("Expected error, got nil")
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error: %v", err)
				}
			}
		})
	}
}
