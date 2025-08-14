package resolver_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junioryono/godi/v3/internal/graph"
	"github.com/junioryono/godi/v3/internal/reflection"
	"github.com/junioryono/godi/v3/internal/registry"
	"github.com/junioryono/godi/v3/internal/resolver"
)

// Test types
type Database struct {
	ConnectionString string
}

type Logger interface {
	Log(msg string)
}

type ConsoleLogger struct {
	Messages []string
}

func (c *ConsoleLogger) Log(msg string) {
	c.Messages = append(c.Messages, msg)
}

type Service struct {
	DB     *Database
	Logger Logger
}

// Constructors
func NewDatabase() *Database {
	return &Database{ConnectionString: "test-db"}
}

func NewConsoleLogger() *ConsoleLogger {
	return &ConsoleLogger{Messages: make([]string, 0)}
}

func NewService(db *Database, logger Logger) *Service {
	return &Service{DB: db, Logger: logger}
}

func NewServiceWithError(db *Database) (*Service, error) {
	if db == nil {
		return nil, errors.New("database required")
	}
	return &Service{DB: db}, nil
}

func TestResolver_SimpleSingleton(t *testing.T) {
	reg := registry.NewServiceCollection()
	g := graph.New()
	analyzer := reflection.New()
	r := resolver.New(reg, g, analyzer, nil)

	// Register Database as singleton
	dbProvider := &registry.Descriptor{
		Type:        reflect.TypeOf((*Database)(nil)),
		Lifetime:    registry.Singleton,
		Constructor: reflect.ValueOf(NewDatabase),
	}
	reg.RegisterProvider(dbProvider)
	g.AddProvider(dbProvider)

	// Resolve Database
	instance, err := r.Resolve(reflect.TypeOf((*Database)(nil)), "test-scope")
	if err != nil {
		t.Fatalf("Failed to resolve Database: %v", err)
	}

	db, ok := instance.(*Database)
	if !ok {
		t.Fatalf("Expected *Database, got %T", instance)
	}

	if db.ConnectionString != "test-db" {
		t.Errorf("Expected connection string 'test-db', got %s", db.ConnectionString)
	}

	// Resolve again - should return same instance
	instance2, err := r.Resolve(reflect.TypeOf((*Database)(nil)), "test-scope")
	if err != nil {
		t.Fatalf("Failed to resolve Database second time: %v", err)
	}

	if instance != instance2 {
		t.Error("Expected same singleton instance")
	}
}

func TestResolver_ScopedService(t *testing.T) {
	reg := registry.NewServiceCollection()
	g := graph.New()
	analyzer := reflection.New()
	r := resolver.New(reg, g, analyzer, nil)

	// Register Logger as scoped
	loggerProvider := &registry.Descriptor{
		Type:        reflect.TypeOf((*Logger)(nil)).Elem(),
		Lifetime:    registry.Scoped,
		Constructor: reflect.ValueOf(NewConsoleLogger),
	}
	reg.RegisterProvider(loggerProvider)
	g.AddProvider(loggerProvider)

	// Resolve in scope1
	instance1, err := r.Resolve(reflect.TypeOf((*Logger)(nil)).Elem(), "scope1")
	if err != nil {
		t.Fatalf("Failed to resolve Logger in scope1: %v", err)
	}

	// Resolve again in scope1 - should be same instance
	instance1b, err := r.Resolve(reflect.TypeOf((*Logger)(nil)).Elem(), "scope1")
	if err != nil {
		t.Fatalf("Failed to resolve Logger again in scope1: %v", err)
	}

	if instance1 != instance1b {
		t.Error("Expected same instance within scope")
	}

	// Resolve in scope2 - should be different instance
	instance2, err := r.Resolve(reflect.TypeOf((*Logger)(nil)).Elem(), "scope2")
	if err != nil {
		t.Fatalf("Failed to resolve Logger in scope2: %v", err)
	}

	if instance1 == instance2 {
		t.Error("Expected different instances in different scopes")
	}
}

func TestResolver_TransientService(t *testing.T) {
	reg := registry.NewServiceCollection()
	g := graph.New()
	analyzer := reflection.New()

	options := &resolver.ResolverOptions{
		EnableCaching: true, // Even with caching, transient should not be cached
	}
	r := resolver.New(reg, g, analyzer, options)

	// Register Logger as transient
	loggerProvider := &registry.Descriptor{
		Type:        reflect.TypeOf((*Logger)(nil)).Elem(),
		Lifetime:    registry.Transient,
		Constructor: reflect.ValueOf(NewConsoleLogger),
	}
	reg.RegisterProvider(loggerProvider)
	g.AddProvider(loggerProvider)

	// Resolve multiple times - should always be different
	instance1, err := r.Resolve(reflect.TypeOf((*Logger)(nil)).Elem(), "scope1")
	if err != nil {
		t.Fatalf("Failed to resolve Logger: %v", err)
	}

	instance2, err := r.Resolve(reflect.TypeOf((*Logger)(nil)).Elem(), "scope1")
	if err != nil {
		t.Fatalf("Failed to resolve Logger again: %v", err)
	}

	if instance1 == instance2 {
		t.Error("Expected different instances for transient service")
	}
}

func TestResolver_WithDependencies(t *testing.T) {
	reg := registry.NewServiceCollection()
	g := graph.New()
	analyzer := reflection.New()
	r := resolver.New(reg, g, analyzer, nil)

	// Analyze constructors to get dependencies
	dbDeps, _ := analyzer.GetDependencies(NewDatabase)

	loggerDeps, _ := analyzer.GetDependencies(NewConsoleLogger)

	// Register Database
	dbProvider := &registry.Descriptor{
		Type:         reflect.TypeOf((*Database)(nil)),
		Lifetime:     registry.Singleton,
		Constructor:  reflect.ValueOf(NewDatabase),
		Dependencies: dbDeps,
	}
	reg.RegisterProvider(dbProvider)
	g.AddProvider(dbProvider)

	// Register Logger
	loggerProvider := &registry.Descriptor{
		Type:         reflect.TypeOf((*Logger)(nil)).Elem(),
		Lifetime:     registry.Singleton,
		Constructor:  reflect.ValueOf(NewConsoleLogger),
		Dependencies: loggerDeps,
	}
	reg.RegisterProvider(loggerProvider)
	g.AddProvider(loggerProvider)

	// Register Service with dependencies
	serviceProvider := &registry.Descriptor{
		Type:        reflect.TypeOf((*Service)(nil)),
		Lifetime:    registry.Singleton,
		Constructor: reflect.ValueOf(NewService),
		Dependencies: []*registry.Dependency{
			{Type: reflect.TypeOf((*Database)(nil))},
			{Type: reflect.TypeOf((*Logger)(nil)).Elem()},
		},
	}
	reg.RegisterProvider(serviceProvider)
	g.AddProvider(serviceProvider)

	// Resolve Service - should auto-resolve dependencies
	instance, err := r.Resolve(reflect.TypeOf((*Service)(nil)), "test-scope")
	if err != nil {
		t.Fatalf("Failed to resolve Service: %v", err)
	}

	service, ok := instance.(*Service)
	if !ok {
		t.Fatalf("Expected *Service, got %T", instance)
	}

	if service.DB == nil {
		t.Error("Expected DB to be injected")
	}

	if service.Logger == nil {
		t.Error("Expected Logger to be injected")
	}

	if service.DB.ConnectionString != "test-db" {
		t.Errorf("Expected DB connection string 'test-db', got %s", service.DB.ConnectionString)
	}
}

func TestResolver_KeyedServices(t *testing.T) {
	reg := registry.NewServiceCollection()
	g := graph.New()
	analyzer := reflection.New()

	// Disable caching to ensure we're testing the resolution, not the cache
	r := resolver.New(reg, g, analyzer, &resolver.ResolverOptions{
		EnableCaching: false, // Disable caching for this test
	})

	// Register primary database - use unique constructors
	primaryConstructor := func() *Database {
		return &Database{ConnectionString: "primary-db"}
	}
	primaryDB := &registry.Descriptor{
		Type:        reflect.TypeOf((*Database)(nil)),
		Key:         "primary",
		Lifetime:    registry.Singleton,
		Constructor: reflect.ValueOf(primaryConstructor),
	}
	reg.RegisterProvider(primaryDB)
	g.AddProvider(primaryDB)

	// Register secondary database - use unique constructors
	secondaryConstructor := func() *Database {
		return &Database{ConnectionString: "secondary-db"}
	}
	secondaryDB := &registry.Descriptor{
		Type:        reflect.TypeOf((*Database)(nil)),
		Key:         "secondary",
		Lifetime:    registry.Singleton,
		Constructor: reflect.ValueOf(secondaryConstructor),
	}
	reg.RegisterProvider(secondaryDB)
	g.AddProvider(secondaryDB)

	// Resolve primary
	primary, err := r.ResolveKeyed(reflect.TypeOf((*Database)(nil)), "primary", "test-scope")
	if err != nil {
		t.Fatalf("Failed to resolve primary database: %v", err)
	}

	primaryDB1 := primary.(*Database)
	if primaryDB1.ConnectionString != "primary-db" {
		t.Errorf("Expected 'primary-db', got %s", primaryDB1.ConnectionString)
	}

	// Resolve secondary
	secondary, err := r.ResolveKeyed(reflect.TypeOf((*Database)(nil)), "secondary", "test-scope")
	if err != nil {
		t.Fatalf("Failed to resolve secondary database: %v", err)
	}

	secondaryDB1 := secondary.(*Database)
	if secondaryDB1.ConnectionString != "secondary-db" {
		t.Errorf("Expected 'secondary-db', got %s", secondaryDB1.ConnectionString)
	}
}

func TestResolver_Groups(t *testing.T) {
	reg := registry.NewServiceCollection()
	g := graph.New()
	analyzer := reflection.New()
	r := resolver.New(reg, g, analyzer, nil)

	handlerType := reflect.TypeOf(func() {})

	// Register multiple handlers in a group
	for i := 0; i < 3; i++ {
		idx := i
		provider := &registry.Descriptor{
			Type:     handlerType,
			Groups:   []string{"middleware"},
			Lifetime: registry.Singleton,
			Constructor: reflect.ValueOf(func() func() {
				return func() { fmt.Printf("Handler %d\n", idx) }
			}),
		}
		reg.RegisterProvider(provider)
		g.AddProvider(provider)
	}

	// Resolve group
	handlers, err := r.ResolveGroup(handlerType, "middleware", "test-scope")
	if err != nil {
		t.Fatalf("Failed to resolve handler group: %v", err)
	}

	if len(handlers) != 3 {
		t.Errorf("Expected 3 handlers, got %d", len(handlers))
	}

	// Verify all are functions
	for i, h := range handlers {
		if _, ok := h.(func()); !ok {
			t.Errorf("Handler %d is not a function, got %T", i, h)
		}
	}
}

// This would create a circular dependency if both were registered
// A -> B -> A
type A struct{ B *B }
type B struct{ A *A }

func TestResolver_CircularDependency(t *testing.T) {
	reg := registry.NewServiceCollection()
	g := graph.New()

	providerA := &registry.Descriptor{
		Type:     reflect.TypeOf((*A)(nil)),
		Lifetime: registry.Singleton,
		Constructor: reflect.ValueOf(func(b *B) *A {
			return &A{B: b}
		}),
		Dependencies: []*registry.Dependency{
			{Type: reflect.TypeOf((*B)(nil))},
		},
	}

	providerB := &registry.Descriptor{
		Type:     reflect.TypeOf((*B)(nil)),
		Lifetime: registry.Singleton,
		Constructor: reflect.ValueOf(func(a *A) *B {
			return &B{A: a}
		}),
		Dependencies: []*registry.Dependency{
			{Type: reflect.TypeOf((*A)(nil))},
		},
	}

	// Register A
	reg.RegisterProvider(providerA)
	// Graph should detect the cycle when we try to add B
	err := g.AddProvider(providerA)
	if err != nil {
		t.Fatalf("Failed to add provider A: %v", err)
	}

	reg.RegisterProvider(providerB)
	err = g.AddProvider(providerB)
	if err == nil {
		t.Error("Expected circular dependency error")
	}
}

func TestResolver_MissingDependency(t *testing.T) {
	reg := registry.NewServiceCollection()
	g := graph.New()
	analyzer := reflection.New()
	r := resolver.New(reg, g, analyzer, nil)

	// Register Service but not its dependencies
	serviceProvider := &registry.Descriptor{
		Type:        reflect.TypeOf((*Service)(nil)),
		Lifetime:    registry.Singleton,
		Constructor: reflect.ValueOf(NewService),
		Dependencies: []*registry.Dependency{
			{Type: reflect.TypeOf((*Database)(nil))},
			{Type: reflect.TypeOf((*Logger)(nil)).Elem()},
		},
	}
	reg.RegisterProvider(serviceProvider)
	g.AddProvider(serviceProvider)

	// Try to resolve Service - should fail
	_, err := r.Resolve(reflect.TypeOf((*Service)(nil)), "test-scope")
	if err == nil {
		t.Error("Expected error for missing dependency")
	}
}

func TestInstanceCache(t *testing.T) {
	cache := resolver.NewInstanceCache()

	dbType := reflect.TypeOf((*Database)(nil))
	db1 := &Database{ConnectionString: "db1"}
	db2 := &Database{ConnectionString: "db2"}

	// Test singleton caching
	cache.Set(dbType, nil, db1, registry.Singleton, "scope1")

	cached, ok := cache.Get(dbType, nil, registry.Singleton, "scope1")
	if !ok {
		t.Error("Expected to find cached singleton")
	}

	if cached != db1 {
		t.Error("Expected same singleton instance")
	}

	// Singleton should be available in different scope
	cached, ok = cache.Get(dbType, nil, registry.Singleton, "scope2")
	if !ok {
		t.Error("Expected singleton to be available in different scope")
	}

	// Test scoped caching
	cache.Set(dbType, "scoped", db2, registry.Scoped, "scope1")

	cached, ok = cache.Get(dbType, "scoped", registry.Scoped, "scope1")
	if !ok {
		t.Error("Expected to find cached scoped instance")
	}

	// Scoped should not be available in different scope
	_, ok = cache.Get(dbType, "scoped", registry.Scoped, "scope2")
	if ok {
		t.Error("Expected scoped instance to not be available in different scope")
	}

	// Test transient (never cached)
	cache.Set(dbType, "transient", db1, registry.Transient, "scope1")

	_, ok = cache.Get(dbType, "transient", registry.Transient, "scope1")
	if ok {
		t.Error("Expected transient to not be cached")
	}

	// Test statistics
	stats := cache.GetStatistics()
	if stats.SingletonCount != 1 {
		t.Errorf("Expected 1 singleton, got %d", stats.SingletonCount)
	}

	if stats.ScopedCount != 1 {
		t.Errorf("Expected 1 scoped instance, got %d", stats.ScopedCount)
	}

	// Test scope clearing
	cache.ClearScope("scope1")

	_, ok = cache.Get(dbType, "scoped", registry.Scoped, "scope1")
	if ok {
		t.Error("Expected scoped instance to be cleared")
	}

	// Singleton should still exist
	_, ok = cache.Get(dbType, nil, registry.Singleton, "scope1")
	if !ok {
		t.Error("Expected singleton to survive scope clear")
	}
}

// Test edge cases in resolver initialization
func TestResolver_Initialization(t *testing.T) {
	tests := []struct {
		name      string
		setup     func() (*resolver.Resolver, error)
		wantPanic bool
	}{
		{
			name: "nil registry",
			setup: func() (*resolver.Resolver, error) {
				g := graph.New()
				analyzer := reflection.New()
				r := resolver.New(nil, g, analyzer, nil) // Should handle nil registry
				return r, nil
			},
			wantPanic: true,
		},
		{
			name: "nil graph",
			setup: func() (*resolver.Resolver, error) {
				reg := registry.NewServiceCollection()
				analyzer := reflection.New()
				r := resolver.New(reg, nil, analyzer, nil) // Should handle nil graph
				return r, nil
			},
			wantPanic: true,
		},
		{
			name: "nil analyzer",
			setup: func() (*resolver.Resolver, error) {
				reg := registry.NewServiceCollection()
				g := graph.New()
				r := resolver.New(reg, g, nil, nil) // Should handle nil analyzer
				return r, nil
			},
			wantPanic: true,
		},
		{
			name: "valid components",
			setup: func() (*resolver.Resolver, error) {
				reg := registry.NewServiceCollection()
				g := graph.New()
				analyzer := reflection.New()
				r := resolver.New(reg, g, analyzer, nil)
				return r, nil
			},
			wantPanic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.wantPanic {
				defer func() {
					if r := recover(); r == nil {
						t.Error("Expected panic but didn't get one")
					}
				}()
			}

			_, err := tt.setup()
			if err != nil && !tt.wantPanic {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// Test concurrent resolution with different scopes
func TestResolver_ConcurrentScopedResolution(t *testing.T) {
	reg := registry.NewServiceCollection()
	g := graph.New()
	analyzer := reflection.New()
	r := resolver.New(reg, g, analyzer, &resolver.ResolverOptions{
		EnableCaching:      true,
		MaxResolutionDepth: 100,
	})

	// Register a scoped service
	provider := &registry.Descriptor{
		Type:        reflect.TypeOf((*Database)(nil)),
		Lifetime:    registry.Scoped,
		Constructor: reflect.ValueOf(NewDatabase),
	}
	reg.RegisterProvider(provider)
	g.AddProvider(provider)

	// Concurrent resolution in different scopes
	var wg sync.WaitGroup
	errors := make(chan error, 100)
	instances := make(chan any, 100)

	for i := 0; i < 10; i++ {
		scopeID := fmt.Sprintf("scope-%d", i)

		for j := 0; j < 10; j++ {
			wg.Add(1)
			go func(scope string) {
				defer wg.Done()

				instance, err := r.Resolve(reflect.TypeOf((*Database)(nil)), scope)
				if err != nil {
					errors <- err
					return
				}

				instances <- instance
			}(scopeID)
		}
	}

	wg.Wait()
	close(errors)
	close(instances)

	// Check for errors
	for err := range errors {
		t.Errorf("Resolution error: %v", err)
	}

	// Verify we got instances
	instanceCount := len(instances)
	if instanceCount != 100 {
		t.Errorf("Expected 100 instances, got %d", instanceCount)
	}

	// Verify scoped instances are properly isolated
	scopeInstances := make(map[string]any)
	for i := 0; i < 10; i++ {
		scopeID := fmt.Sprintf("scope-%d", i)

		// Resolve again in each scope
		instance, err := r.Resolve(reflect.TypeOf((*Database)(nil)), scopeID)
		if err != nil {
			t.Errorf("Failed to resolve in scope %s: %v", scopeID, err)
			continue
		}

		// Check if it's the same instance within the scope
		if prev, exists := scopeInstances[scopeID]; exists {
			if prev != instance {
				t.Errorf("Expected same instance within scope %s", scopeID)
			}
		}
		scopeInstances[scopeID] = instance
	}

	// Clear specific scope and verify
	r.ClearScopeCache("scope-0")

	newInstance, err := r.Resolve(reflect.TypeOf((*Database)(nil)), "scope-0")
	if err != nil {
		t.Errorf("Failed to resolve after cache clear: %v", err)
	}

	if oldInstance, exists := scopeInstances["scope-0"]; exists && oldInstance == newInstance {
		t.Error("Expected new instance after cache clear")
	}
}

// Test resolution depth limit
func TestResolver_MaxDepthProtection(t *testing.T) {
	reg := registry.NewServiceCollection()
	g := graph.New()
	analyzer := reflection.New()

	r := resolver.New(reg, g, analyzer, &resolver.ResolverOptions{
		EnableCaching:      false, // Disable caching
		EnableValidation:   false,
		MaxResolutionDepth: 1, // Only allow depth of 1
	})

	// Register Database with no dependencies
	dbProvider := &registry.Descriptor{
		Type:         reflect.TypeOf((*Database)(nil)),
		Lifetime:     registry.Transient,
		Constructor:  reflect.ValueOf(NewDatabase),
		Dependencies: nil,
	}
	reg.RegisterProvider(dbProvider)
	g.AddProvider(dbProvider)

	// Register Logger with no dependencies
	loggerProvider := &registry.Descriptor{
		Type:         reflect.TypeOf((*Logger)(nil)).Elem(),
		Lifetime:     registry.Transient,
		Constructor:  reflect.ValueOf(NewConsoleLogger),
		Dependencies: nil,
	}
	reg.RegisterProvider(loggerProvider)
	g.AddProvider(loggerProvider)

	serviceDeps, _ := analyzer.GetDependencies(NewService)
	serviceProvider := &registry.Descriptor{
		Type:         reflect.TypeOf((*Service)(nil)),
		Lifetime:     registry.Transient,
		Constructor:  reflect.ValueOf(NewService),
		Dependencies: serviceDeps,
	}
	reg.RegisterProvider(serviceProvider)
	g.AddProvider(serviceProvider)

	// Try to resolve Service with MaxDepth=1
	// Should fail because it needs to resolve dependencies at depth 2
	_, err := r.Resolve(reflect.TypeOf((*Service)(nil)), "test-scope")

	if err == nil {
		t.Error("Expected max depth error")
		return
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "depth") || !strings.Contains(errStr, "exceed") {
		t.Errorf("Expected depth exceeded error message, got: %v", err)
	}
}

// Test cache statistics accuracy
func TestInstanceCache_Statistics(t *testing.T) {
	cache := resolver.NewInstanceCache()

	dbType := reflect.TypeOf((*Database)(nil))

	// Test hits and misses
	_, found := cache.Get(dbType, nil, registry.Singleton, "scope1")
	if found {
		t.Error("Expected cache miss")
	}

	// Set and get
	cache.Set(dbType, nil, &Database{}, registry.Singleton, "scope1")

	_, found = cache.Get(dbType, nil, registry.Singleton, "scope1")
	if !found {
		t.Error("Expected cache hit")
	}

	// Get from different scope (singleton should be found)
	_, found = cache.Get(dbType, nil, registry.Singleton, "scope2")
	if !found {
		t.Error("Expected singleton to be found in different scope")
	}

	stats := cache.GetStatistics()

	if stats.Hits < 2 {
		t.Errorf("Expected at least 2 hits, got %d", stats.Hits)
	}

	if stats.Misses < 1 {
		t.Errorf("Expected at least 1 miss, got %d", stats.Misses)
	}

	if stats.SingletonCount != 1 {
		t.Errorf("Expected 1 singleton, got %d", stats.SingletonCount)
	}
}

// Test decorator processor with complex scenarios
func TestDecoratorProcessor_ComplexScenarios(t *testing.T) {
	reg := registry.NewServiceCollection()
	analyzer := reflection.New()
	processor := resolver.NewDecoratorProcessor(reg, analyzer)

	loggerType := reflect.TypeOf((*Logger)(nil)).Elem()

	// Register multiple decorators with different priorities
	// When sorted by priority (ascending), they're applied in order:
	// Priority 0 wraps the base first
	// Priority 1 wraps that result
	// Priority 2 wraps everything (outermost)
	decorators := []string{
		"[FIRST]",  // Priority 0 - wraps base
		"[SECOND]", // Priority 1 - wraps FIRST
		"[THIRD]",  // Priority 2 - wraps SECOND (outermost)
	}

	for _, d := range decorators {
		dec := d // Capture loop variable
		decorator := &registry.Descriptor{
			Type: loggerType,
			Constructor: reflect.ValueOf(func(logger Logger) Logger {
				return &wrappedLogger{
					inner:  logger,
					prefix: dec,
				}
			}),
			DecoratedType: reflect.TypeOf(func(Logger) Logger { return nil }),
		}
		reg.RegisterDecorator(decorator)
	}

	// Create base logger
	baseLogger := &ConsoleLogger{}

	// Create mock resolution context
	ctx := &mockResolutionContext{
		instances: make(map[string]any),
	}

	// Apply decorators
	decorated, err := processor.ApplyDecorators(baseLogger, loggerType, ctx)
	if err != nil {
		t.Fatalf("Failed to apply decorators: %v", err)
	}

	// After applying in order (0, 1, 2), the result should be:
	// [THIRD] wrapping [SECOND] wrapping [FIRST] wrapping base
	// So the outermost is [THIRD]
	logger := decorated.(Logger)

	if wrapper, ok := logger.(*wrappedLogger); ok {
		if wrapper.prefix != "[THIRD]" {
			t.Errorf("Expected outermost wrapper to have prefix [THIRD], got %s", wrapper.prefix)
		}

		// Check inner layers
		if inner1, ok := wrapper.inner.(*wrappedLogger); ok {
			if inner1.prefix != "[SECOND]" {
				t.Errorf("Expected second wrapper to have prefix [SECOND], got %s", inner1.prefix)
			}

			if inner2, ok := inner1.inner.(*wrappedLogger); ok {
				if inner2.prefix != "[FIRST]" {
					t.Errorf("Expected innermost wrapper to have prefix [FIRST], got %s", inner2.prefix)
				}

				// Innermost wrapped should be ConsoleLogger
				if _, ok := inner2.inner.(*ConsoleLogger); !ok {
					t.Error("Expected base to be ConsoleLogger")
				}
			} else {
				t.Error("Expected first wrapper layer")
			}
		} else {
			t.Error("Expected second wrapper layer")
		}
	} else {
		t.Error("Expected decorated logger to be wrapped")
	}
}

// Test error handler functionality
func TestErrorHandler_ErrorWrapping(t *testing.T) {
	handler := resolver.NewErrorHandler(func(err error) {
		// Log errors (in test, we just verify it's called)
	})

	serviceType := reflect.TypeOf((*Database)(nil))
	cause := errors.New("original error")

	stack := []resolver.ResolutionFrame{
		{
			ServiceType: reflect.TypeOf((*Service)(nil)),
			Lifetime:    "Singleton",
		},
		{
			ServiceType: serviceType,
			Key:         "primary",
			Lifetime:    "Scoped",
		},
	}

	wrapped := handler.WrapResolutionError(serviceType, "primary", cause, stack)

	if !resolver.IsResolutionError(wrapped) {
		t.Error("Expected ResolutionError type")
	}

	resErr := wrapped.(*resolver.ResolutionError)

	if resErr.ServiceType != serviceType {
		t.Error("Service type mismatch")
	}

	if resErr.Key != "primary" {
		t.Error("Key mismatch")
	}

	if !errors.Is(wrapped, cause) {
		t.Error("Should be able to unwrap to original cause")
	}

	extractedStack, ok := resolver.GetResolutionStack(wrapped)
	if !ok {
		t.Error("Should be able to extract stack")
	}

	if len(extractedStack) != len(stack) {
		t.Errorf("Stack length mismatch: expected %d, got %d", len(stack), len(extractedStack))
	}
}

// Test ResolveAll functionality
func TestResolver_ResolveAll(t *testing.T) {
	reg := registry.NewServiceCollection()
	g := graph.New()
	analyzer := reflection.New()

	resolveCount := int32(0)
	errorCount := int32(0)

	r := resolver.New(reg, g, analyzer, &resolver.ResolverOptions{
		EnableCaching: true,
		OnResolved: func(serviceType reflect.Type, instance any, duration time.Duration) {
			atomic.AddInt32(&resolveCount, 1)
			t.Logf("Resolved: %v", serviceType)
		},
		OnError: func(serviceType reflect.Type, err error) {
			atomic.AddInt32(&errorCount, 1)
			t.Logf("Error resolving %v: %v", serviceType, err)
		},
	})

	// Analyze dependencies properly
	dbDeps, _ := analyzer.GetDependencies(NewDatabase)
	loggerDeps, _ := analyzer.GetDependencies(NewConsoleLogger)
	serviceDeps, _ := analyzer.GetDependencies(NewService)

	// Register multiple services
	providers := []*registry.Descriptor{
		{
			Type:         reflect.TypeOf((*Database)(nil)),
			Lifetime:     registry.Singleton,
			Constructor:  reflect.ValueOf(NewDatabase),
			Dependencies: dbDeps,
		},
		{
			Type:         reflect.TypeOf((*Logger)(nil)).Elem(),
			Lifetime:     registry.Singleton,
			Constructor:  reflect.ValueOf(NewConsoleLogger),
			Dependencies: loggerDeps,
		},
		{
			Type:         reflect.TypeOf((*Service)(nil)),
			Lifetime:     registry.Singleton,
			Constructor:  reflect.ValueOf(NewService),
			Dependencies: serviceDeps,
		},
	}

	for _, p := range providers {
		if err := reg.RegisterProvider(p); err != nil {
			t.Fatalf("Failed to register provider: %v", err)
		}
		if err := g.AddProvider(p); err != nil {
			t.Fatalf("Failed to add provider to graph: %v", err)
		}
	}

	// Resolve all
	err := r.ResolveAll("test-scope")
	if err != nil {
		t.Errorf("ResolveAll failed: %v", err)
	}

	// Wait a bit for callbacks
	time.Sleep(10 * time.Millisecond)

	resolvedCount := atomic.LoadInt32(&resolveCount)
	erroredCount := atomic.LoadInt32(&errorCount)

	// All three should be resolved
	if resolvedCount < 3 {
		t.Errorf("Expected at least 3 resolutions, got %d", resolvedCount)
	}

	if erroredCount > 0 {
		t.Errorf("Expected no errors, got %d", erroredCount)
	}
}

// Benchmark cache performance
func BenchmarkInstanceCache_Get(b *testing.B) {
	cache := resolver.NewInstanceCache()

	dbType := reflect.TypeOf((*Database)(nil))
	db := &Database{ConnectionString: "benchmark"}

	// Pre-populate cache
	cache.Set(dbType, nil, db, registry.Singleton, "scope")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cache.Get(dbType, nil, registry.Singleton, "scope")
		}
	})
}

func BenchmarkResolver_Resolution(b *testing.B) {
	reg := registry.NewServiceCollection()
	g := graph.New()
	analyzer := reflection.New()
	r := resolver.New(reg, g, analyzer, nil)

	// Setup a complex dependency graph
	providers := []*registry.Descriptor{
		{
			Type:        reflect.TypeOf((*Database)(nil)),
			Lifetime:    registry.Singleton,
			Constructor: reflect.ValueOf(NewDatabase),
		},
		{
			Type:        reflect.TypeOf((*Logger)(nil)).Elem(),
			Lifetime:    registry.Singleton,
			Constructor: reflect.ValueOf(NewConsoleLogger),
		},
		{
			Type:        reflect.TypeOf((*Service)(nil)),
			Lifetime:    registry.Transient,
			Constructor: reflect.ValueOf(NewService),
			Dependencies: []*registry.Dependency{
				{Type: reflect.TypeOf((*Database)(nil))},
				{Type: reflect.TypeOf((*Logger)(nil)).Elem()},
			},
		},
	}

	for _, p := range providers {
		reg.RegisterProvider(p)
		g.AddProvider(p)
	}

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_, err := r.Resolve(reflect.TypeOf((*Service)(nil)), "bench-scope")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Mock types for testing
type wrappedLogger struct {
	inner  Logger
	prefix string
}

func (w *wrappedLogger) Log(msg string) {
	w.inner.Log(w.prefix + " " + msg)
}

type mockResolutionContext struct {
	instances map[string]any
}

// Clear implements resolver.ResolutionContext.
func (m *mockResolutionContext) Clear() {
	panic("unimplemented")
}

// DecrementDepth implements resolver.ResolutionContext.
func (m *mockResolutionContext) DecrementDepth() {
	panic("unimplemented")
}

// GetDepth implements resolver.ResolutionContext.
func (m *mockResolutionContext) GetDepth() int {
	panic("unimplemented")
}

// GetInstance implements resolver.ResolutionContext.
func (m *mockResolutionContext) GetInstance(serviceType reflect.Type, key any) (any, bool) {
	panic("unimplemented")
}

// GetLifetime implements resolver.ResolutionContext.
func (m *mockResolutionContext) GetLifetime() registry.ServiceLifetime {
	panic("unimplemented")
}

// GetResolver implements resolver.ResolutionContext.
func (m *mockResolutionContext) GetResolver() *resolver.Resolver {
	panic("unimplemented")
}

// GetScopeID implements resolver.ResolutionContext.
func (m *mockResolutionContext) GetScopeID() string {
	panic("unimplemented")
}

// IncrementDepth implements resolver.ResolutionContext.
func (m *mockResolutionContext) IncrementDepth() int {
	panic("unimplemented")
}

// IsResolving implements resolver.ResolutionContext.
func (m *mockResolutionContext) IsResolving(serviceType reflect.Type, key any) bool {
	panic("unimplemented")
}

// SetInstance implements resolver.ResolutionContext.
func (m *mockResolutionContext) SetInstance(serviceType reflect.Type, key any, instance any) {
	panic("unimplemented")
}

// SetLifetime implements resolver.ResolutionContext.
func (m *mockResolutionContext) SetLifetime(lifetime registry.ServiceLifetime) {
	panic("unimplemented")
}

// StartResolving implements resolver.ResolutionContext.
func (m *mockResolutionContext) StartResolving(serviceType reflect.Type, key any) error {
	panic("unimplemented")
}

// StopResolving implements resolver.ResolutionContext.
func (m *mockResolutionContext) StopResolving(serviceType reflect.Type, key any) {
	panic("unimplemented")
}

func (m *mockResolutionContext) Resolve(t reflect.Type) (any, error) {
	if instance, ok := m.instances[t.String()]; ok {
		return instance, nil
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockResolutionContext) ResolveKeyed(t reflect.Type, key any) (any, error) {
	keyStr := fmt.Sprintf("%v[%v]", t, key)
	if instance, ok := m.instances[keyStr]; ok {
		return instance, nil
	}
	return nil, fmt.Errorf("not found")
}

func (m *mockResolutionContext) ResolveGroup(t reflect.Type, group string) ([]any, error) {
	return []any{}, nil
}

var _ resolver.ResolutionContext = (*mockResolutionContext)(nil)
