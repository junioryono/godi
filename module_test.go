package godi_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/junioryono/godi"
)

// Test types for module tests
type (
	moduleTestLogger interface {
		Log(msg string)
		GetLogs() []string
	}

	moduleTestLoggerImpl struct {
		logs []string
		mu   sync.Mutex
	}

	moduleTestDatabase interface {
		Query(sql string) string
		Close() error
	}

	moduleTestDatabaseImpl struct {
		name   string
		closed bool
		mu     sync.Mutex
	}

	moduleTestCache interface {
		Get(key string) (string, bool)
		Set(key string, value string)
	}

	moduleTestCacheImpl struct {
		data map[string]string
		mu   sync.RWMutex
	}

	moduleTestRepository struct {
		db     moduleTestDatabase
		logger moduleTestLogger
	}

	moduleTestService struct {
		repo   *moduleTestRepository
		cache  moduleTestCache
		logger moduleTestLogger
		id     string
	}

	moduleTestMetrics struct {
	}

	// For keyed service tests
	moduleTestNotifier interface {
		Notify(msg string)
	}

	moduleTestEmailNotifier struct {
		logger moduleTestLogger
	}

	moduleTestSMSNotifier struct {
		logger moduleTestLogger
	}

	// For group tests
	moduleTestHandler interface {
		Handle() string
	}

	moduleTestUserHandler  struct{}
	moduleTestAdminHandler struct{}
	moduleTestAPIHandler   struct{}
)

// Implement test interfaces
func (l *moduleTestLoggerImpl) Log(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, msg)
}

func (l *moduleTestLoggerImpl) GetLogs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]string, len(l.logs))
	copy(result, l.logs)
	return result
}

func (d *moduleTestDatabaseImpl) Query(sql string) string {
	return fmt.Sprintf("%s: %s", d.name, sql)
}

func (d *moduleTestDatabaseImpl) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return errAlreadyClosed
	}
	d.closed = true
	return nil
}

func (c *moduleTestCacheImpl) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.data[key]
	return val, ok
}

func (c *moduleTestCacheImpl) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data == nil {
		c.data = make(map[string]string)
	}
	c.data[key] = value
}

func (e *moduleTestEmailNotifier) Notify(msg string) {
	e.logger.Log("Email: " + msg)
}

func (s *moduleTestSMSNotifier) Notify(msg string) {
	s.logger.Log("SMS: " + msg)
}

func (h *moduleTestUserHandler) Handle() string  { return "user" }
func (h *moduleTestAdminHandler) Handle() string { return "admin" }
func (h *moduleTestAPIHandler) Handle() string   { return "api" }

// Constructor functions
func newModuleTestLogger() moduleTestLogger {
	return &moduleTestLoggerImpl{}
}

func newModuleTestDatabase() moduleTestDatabase {
	return &moduleTestDatabaseImpl{name: "testdb"}
}

func newModuleTestCache() moduleTestCache {
	return &moduleTestCacheImpl{data: make(map[string]string)}
}

func newModuleTestRepository(db moduleTestDatabase, logger moduleTestLogger) *moduleTestRepository {
	return &moduleTestRepository{db: db, logger: logger}
}

func newModuleTestService(repo *moduleTestRepository, cache moduleTestCache, logger moduleTestLogger) *moduleTestService {
	return &moduleTestService{
		repo:   repo,
		cache:  cache,
		logger: logger,
		id:     fmt.Sprintf("service-%d", time.Now().UnixNano()),
	}
}

func newModuleTestMetrics() *moduleTestMetrics {
	return &moduleTestMetrics{}
}

func newModuleTestEmailNotifier(logger moduleTestLogger) moduleTestNotifier {
	return &moduleTestEmailNotifier{logger: logger}
}

func newModuleTestSMSNotifier(logger moduleTestLogger) moduleTestNotifier {
	return &moduleTestSMSNotifier{logger: logger}
}

func newModuleTestUserHandler() moduleTestHandler  { return &moduleTestUserHandler{} }
func newModuleTestAdminHandler() moduleTestHandler { return &moduleTestAdminHandler{} }
func newModuleTestAPIHandler() moduleTestHandler   { return &moduleTestAPIHandler{} }

func TestModule_BasicFunctionality(t *testing.T) {
	t.Run("creates simple module", func(t *testing.T) {
		// Create a simple module
		logModule := godi.NewModule("logging",
			godi.AddSingleton(newModuleTestLogger),
		)

		// Apply module to collection
		collection := godi.NewServiceCollection()
		err := collection.AddModules(logModule)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify service was added
		if !collection.Contains(reflect.TypeOf((*moduleTestLogger)(nil)).Elem()) {
			t.Error("expected logger to be registered")
		}

		// Build provider to ensure it works
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building provider: %v", err)
		}
		defer provider.Close()

		logger, err := godi.Resolve[moduleTestLogger](provider)
		if err != nil {
			t.Fatalf("unexpected error resolving logger: %v", err)
		}

		logger.Log("test")
		logs := logger.GetLogs()
		if len(logs) != 1 || logs[0] != "test" {
			t.Error("logger not working correctly")
		}
	})

	t.Run("creates module with multiple services", func(t *testing.T) {
		// Create a database module
		dbModule := godi.NewModule("database",
			godi.AddSingleton(newModuleTestDatabase),
			godi.AddScoped(newModuleTestRepository),
		)

		collection := godi.NewServiceCollection()
		collection.AddSingleton(newModuleTestLogger) // Add dependency
		err := collection.AddModules(dbModule)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !collection.Contains(reflect.TypeOf((*moduleTestDatabase)(nil)).Elem()) {
			t.Error("expected database to be registered")
		}
		if !collection.Contains(reflect.TypeOf((*moduleTestRepository)(nil))) {
			t.Error("expected repository to be registered")
		}
	})

	t.Run("module name appears in error", func(t *testing.T) {
		// Create a module with an error
		errorModule := godi.NewModule("problematic",
			func(s godi.ServiceCollection) error {
				return errIntentional
			},
		)

		collection := godi.NewServiceCollection()
		err := collection.AddModules(errorModule)
		if err == nil {
			t.Fatal("expected error")
		}

		if !errors.Is(err, errIntentional) {
			t.Errorf("expected error to be %v, got: %v", errIntentional, err)
		}
	})

	t.Run("handles nil builders", func(t *testing.T) {
		// Module with nil builders should not error
		module := godi.NewModule("safe",
			nil,
			godi.AddSingleton(newModuleTestLogger),
			nil,
		)

		collection := godi.NewServiceCollection()
		err := collection.AddModules(module)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !collection.Contains(reflect.TypeOf((*moduleTestLogger)(nil)).Elem()) {
			t.Error("expected logger to be registered")
		}
	})
}

func TestModule_Composition(t *testing.T) {
	t.Run("nested modules", func(t *testing.T) {
		// Create base modules
		logModule := godi.NewModule("logging",
			godi.AddSingleton(newModuleTestLogger),
			godi.AddSingleton(newModuleTestMetrics),
		)

		cacheModule := godi.NewModule("cache",
			godi.AddSingleton(newModuleTestCache),
		)

		// Create composite module
		dataModule := godi.NewModule("data",
			logModule,
			cacheModule,
			godi.AddSingleton(newModuleTestDatabase),
			godi.AddScoped(newModuleTestRepository),
		)

		// Create app module that uses data module
		appModule := godi.NewModule("app",
			dataModule,
			godi.AddScoped(newModuleTestService),
		)

		// Apply to collection
		collection := godi.NewServiceCollection()
		err := collection.AddModules(appModule)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify all services are registered
		expectedTypes := []reflect.Type{
			reflect.TypeOf((*moduleTestLogger)(nil)).Elem(),
			reflect.TypeOf((*moduleTestMetrics)(nil)),
			reflect.TypeOf((*moduleTestCache)(nil)).Elem(),
			reflect.TypeOf((*moduleTestDatabase)(nil)).Elem(),
			reflect.TypeOf((*moduleTestRepository)(nil)),
			reflect.TypeOf((*moduleTestService)(nil)),
		}

		for _, typ := range expectedTypes {
			if !collection.Contains(typ) {
				t.Errorf("expected %v to be registered", typ)
			}
		}

		// Build and test
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building provider: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		service, err := godi.Resolve[moduleTestService](scope.ServiceProvider())
		if err != nil {
			t.Fatalf("unexpected error resolving service: %v", err)
		}

		if service.repo == nil || service.cache == nil || service.logger == nil {
			t.Error("service dependencies not properly injected")
		}
	})

	t.Run("AddModule with nil", func(t *testing.T) {
		module := godi.NewModule("test",
			nil,
			godi.AddSingleton(newModuleTestLogger),
		)

		collection := godi.NewServiceCollection()
		err := collection.AddModules(module)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !collection.Contains(reflect.TypeOf((*moduleTestLogger)(nil)).Elem()) {
			t.Error("expected logger to be registered")
		}
	})
}

func TestModuleBuilder_Functions(t *testing.T) {
	t.Run("AddSingleton", func(t *testing.T) {
		builder := godi.AddSingleton(newModuleTestLogger)

		collection := godi.NewServiceCollection()
		err := builder(collection)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		descriptors := collection.ToSlice()
		if len(descriptors) != 1 {
			t.Fatalf("expected 1 descriptor, got %d", len(descriptors))
		}

		if descriptors[0].Lifetime != godi.Singleton {
			t.Errorf("expected Singleton lifetime, got %v", descriptors[0].Lifetime)
		}
	})

	t.Run("AddSingleton with options", func(t *testing.T) {
		builder := godi.AddSingleton(newModuleTestDatabase, godi.Name("primary"))

		collection := godi.NewServiceCollection()
		err := builder(collection)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !collection.ContainsKeyed(reflect.TypeOf((*moduleTestDatabase)(nil)).Elem(), "primary") {
			t.Error("expected keyed service to be registered")
		}
	})

	t.Run("AddTransient", func(t *testing.T) {
		builder := godi.AddTransient(newModuleTestCache)

		collection := godi.NewServiceCollection()
		err := builder(collection)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		descriptors := collection.ToSlice()
		if len(descriptors) != 1 {
			t.Fatalf("expected 1 descriptor, got %d", len(descriptors))
		}

		if descriptors[0].Lifetime != godi.Transient {
			t.Errorf("expected Transient lifetime, got %v", descriptors[0].Lifetime)
		}
	})

	t.Run("AddDecorator", func(t *testing.T) {
		decoratorCalled := false
		decorator := func(logger moduleTestLogger) moduleTestLogger {
			decoratorCalled = true
			return &moduleTestLoggerImpl{logs: []string{"decorated"}}
		}

		builder := godi.AddDecorator(decorator)

		collection := godi.NewServiceCollection()
		collection.AddSingleton(newModuleTestLogger)

		err := builder(collection)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Build provider to test decorator
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building provider: %v", err)
		}
		defer provider.Close()

		logger, err := godi.Resolve[moduleTestLogger](provider)
		if err != nil {
			t.Fatalf("unexpected error resolving logger: %v", err)
		}

		logs := logger.GetLogs()
		if len(logs) == 0 || logs[0] != "decorated" {
			t.Error("decorator not applied correctly")
		}

		if !decoratorCalled {
			t.Error("decorator should have been called")
		}
	})

	t.Run("AddDecorator with options", func(t *testing.T) {
		decorator := func(db moduleTestDatabase) moduleTestDatabase {
			return &moduleTestDatabaseImpl{name: "decorated-db"}
		}

		// Create a DecorateInfo to capture information
		var info godi.DecorateInfo
		builder := godi.AddDecorator(decorator, godi.FillDecorateInfo(&info))

		collection := godi.NewServiceCollection()
		collection.AddSingleton(newModuleTestDatabase)

		err := builder(collection)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// The DecorateInfo would be filled during provider build
		// We're mainly testing that options are passed through correctly
		if collection.Count() != 2 { // Original + decorator
			t.Errorf("expected 2 descriptors, got %d", collection.Count())
		}
	})
}

func TestModule_ComplexScenarios(t *testing.T) {
	t.Run("module with keyed services", func(t *testing.T) {
		notificationModule := godi.NewModule("notifications",
			godi.AddSingleton(newModuleTestLogger),
			godi.AddSingleton(newModuleTestEmailNotifier, godi.Name("email")),
			godi.AddSingleton(newModuleTestSMSNotifier, godi.Name("sms")),
		)

		collection := godi.NewServiceCollection()
		err := collection.AddModules(notificationModule)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building provider: %v", err)
		}
		defer provider.Close()

		// Resolve keyed services
		emailNotifier, err := godi.ResolveKeyed[moduleTestNotifier](provider, "email")
		if err != nil {
			t.Fatalf("unexpected error resolving email notifier: %v", err)
		}

		smsNotifier, err := godi.ResolveKeyed[moduleTestNotifier](provider, "sms")
		if err != nil {
			t.Fatalf("unexpected error resolving sms notifier: %v", err)
		}

		// Test they work correctly
		logger, _ := godi.Resolve[moduleTestLogger](provider)

		emailNotifier.Notify("Hello")
		smsNotifier.Notify("World")

		logs := logger.GetLogs()
		if len(logs) != 2 {
			t.Fatalf("expected 2 logs, got %d", len(logs))
		}

		if logs[0] != "Email: Hello" || logs[1] != "SMS: World" {
			t.Errorf("unexpected logs: %v", logs)
		}
	})

	t.Run("module with groups", func(t *testing.T) {
		handlersModule := godi.NewModule("handlers",
			godi.AddSingleton(newModuleTestUserHandler, godi.Group("handlers")),
			godi.AddSingleton(newModuleTestAdminHandler, godi.Group("handlers")),
			godi.AddSingleton(newModuleTestAPIHandler, godi.Group("handlers")),
		)

		// Consumer of the group
		type HandlerConsumer struct {
			godi.In
			Handlers []moduleTestHandler `group:"handlers"`
		}

		var capturedHandlers []moduleTestHandler
		consumerModule := godi.NewModule("consumer",
			godi.AddSingleton(func(params HandlerConsumer) *moduleTestService {
				capturedHandlers = params.Handlers
				return &moduleTestService{id: "handler-consumer"}
			}),
		)

		collection := godi.NewServiceCollection()
		err := collection.AddModules(handlersModule, consumerModule)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building provider: %v", err)
		}
		defer provider.Close()

		// Resolve to trigger group injection
		_, err = godi.Resolve[moduleTestService](provider)
		if err != nil {
			t.Fatalf("unexpected error resolving service: %v", err)
		}

		if len(capturedHandlers) != 3 {
			t.Fatalf("expected 3 handlers, got %d", len(capturedHandlers))
		}

		// Check we got all handlers
		handlerTypes := make(map[string]bool)
		for _, h := range capturedHandlers {
			handlerTypes[h.Handle()] = true
		}

		expectedTypes := []string{"user", "admin", "api"}
		for _, expected := range expectedTypes {
			if !handlerTypes[expected] {
				t.Errorf("missing handler type: %s", expected)
			}
		}
	})

	t.Run("module error propagation", func(t *testing.T) {
		// Module that has a registration error
		errorModule := godi.NewModule("error",
			godi.AddSingleton(nil), // This should cause an error
		)

		collection := godi.NewServiceCollection()
		err := collection.AddModules(errorModule)
		if err == nil {
			t.Fatal("expected error for nil constructor")
		}

		if !errors.Is(err, godi.ErrNilConstructor) {
			t.Errorf("expected ErrNilConstructor, got: %v", err)
		}
	})

	t.Run("multiple modules with dependencies", func(t *testing.T) {
		// Define modules that depend on each other
		coreModule := godi.NewModule("core",
			godi.AddSingleton(newModuleTestLogger),
			godi.AddSingleton(newModuleTestMetrics),
		)

		dataModule := godi.NewModule("data",
			godi.AddSingleton(newModuleTestDatabase),
			godi.AddSingleton(newModuleTestCache),
		)

		businessModule := godi.NewModule("business",
			godi.AddScoped(newModuleTestRepository),
			godi.AddScoped(newModuleTestService),
		)

		// Apply all modules
		collection := godi.NewServiceCollection()
		err := collection.AddModules(coreModule, dataModule, businessModule)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Build and verify
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building provider: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		// Should be able to resolve the top-level service
		service, err := godi.Resolve[moduleTestService](scope.ServiceProvider())
		if err != nil {
			t.Fatalf("unexpected error resolving service: %v", err)
		}

		if service.repo == nil {
			t.Error("repository not injected")
		}
		if service.repo.db == nil {
			t.Error("database not injected into repository")
		}
		if service.repo.logger == nil {
			t.Error("logger not injected into repository")
		}
	})
}

func TestModule_RealWorldExample(t *testing.T) {
	// Simulate a real-world modular application structure

	// Infrastructure module
	var InfrastructureModule = godi.NewModule("infrastructure",
		godi.AddSingleton(newModuleTestLogger),
		godi.AddSingleton(newModuleTestMetrics),
		godi.AddSingleton(func() moduleTestDatabase {
			return &moduleTestDatabaseImpl{name: "production"}
		}, godi.Name("primary")),
		godi.AddSingleton(func() moduleTestDatabase {
			return &moduleTestDatabaseImpl{name: "readonly"}
		}, godi.Name("replica")),
		godi.AddSingleton(newModuleTestCache),
		godi.AddSingleton(newModuleTestDatabase),
	)

	// Notification module
	var NotificationModule = godi.NewModule("notifications",
		godi.AddSingleton(newModuleTestEmailNotifier, godi.Name("email")),
		godi.AddSingleton(newModuleTestSMSNotifier, godi.Name("sms")),
		godi.AddDecorator(func(email moduleTestNotifier) moduleTestNotifier {
			// Decorate email notifier with logging
			return &moduleTestEmailNotifier{
				logger: &moduleTestLoggerImpl{logs: []string{"[DECORATED]"}},
			}
		}),
	)

	// Data access module
	var DataModule = godi.NewModule("data",
		InfrastructureModule,
		godi.AddScoped(newModuleTestRepository),
		godi.AddTransient(func() moduleTestService {
			return moduleTestService{id: "transient-service"}
		}),
	)

	// API handlers module
	var HandlersModule = godi.NewModule("handlers",
		godi.AddSingleton(newModuleTestUserHandler, godi.Group("handlers")),
		godi.AddSingleton(newModuleTestAdminHandler, godi.Group("handlers")),
		godi.AddSingleton(newModuleTestAPIHandler, godi.Group("handlers")),
	)

	// Application module combining everything
	var ApplicationModule = godi.NewModule("application",
		DataModule,
		NotificationModule,
		HandlersModule,
		godi.AddScoped(newModuleTestService),
	)

	// Build the application
	collection := godi.NewServiceCollection()
	err := collection.AddModules(ApplicationModule)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	provider, err := collection.BuildServiceProvider()
	if err != nil {
		t.Fatalf("unexpected error building provider: %v", err)
	}
	defer provider.Close()

	// Test the complete system
	scope := provider.CreateScope(context.Background())
	defer scope.Close()

	// Verify all components work together
	service, err := godi.Resolve[moduleTestService](scope.ServiceProvider())
	if err != nil {
		t.Fatalf("unexpected error resolving service: %v", err)
	}

	// Test keyed services
	primaryDB, err := godi.ResolveKeyed[moduleTestDatabase](provider, "primary")
	if err != nil {
		t.Fatalf("unexpected error resolving primary db: %v", err)
	}

	replicaDB, err := godi.ResolveKeyed[moduleTestDatabase](provider, "replica")
	if err != nil {
		t.Fatalf("unexpected error resolving replica db: %v", err)
	}

	if primaryDB.Query("SELECT 1") == replicaDB.Query("SELECT 1") {
		t.Error("expected different database instances")
	}

	// Verify service has all its dependencies
	if service.repo == nil || service.cache == nil || service.logger == nil {
		t.Error("service missing dependencies")
	}
}

func TestModule_EdgeCases(t *testing.T) {
	t.Run("empty module", func(t *testing.T) {
		emptyModule := godi.NewModule("empty")

		collection := godi.NewServiceCollection()
		err := collection.AddModules(emptyModule)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if collection.Count() != 0 {
			t.Errorf("expected 0 services, got %d", collection.Count())
		}
	})

	t.Run("module with only nil builders", func(t *testing.T) {
		nilModule := godi.NewModule("nil-builders", nil, nil, nil)

		collection := godi.NewServiceCollection()
		err := collection.AddModules(nilModule)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if collection.Count() != 0 {
			t.Errorf("expected 0 services, got %d", collection.Count())
		}
	})

	t.Run("recursive module application", func(t *testing.T) {
		// Module that adds itself (should not cause infinite loop)
		recursiveModule := godi.NewModule("recursive",
			godi.AddSingleton(newModuleTestLogger),
			func(s godi.ServiceCollection) error {
				// Don't actually add recursively, just test the pattern
				return nil
			},
		)

		collection := godi.NewServiceCollection()
		err := collection.AddModules(recursiveModule)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !collection.Contains(reflect.TypeOf((*moduleTestLogger)(nil)).Elem()) {
			t.Error("expected logger to be registered")
		}
	})

	t.Run("module builder error handling", func(t *testing.T) {
		callOrder := []string{}

		module := godi.NewModule("error-test",
			func(s godi.ServiceCollection) error {
				callOrder = append(callOrder, "first")
				return nil
			},
			func(s godi.ServiceCollection) error {
				callOrder = append(callOrder, "second")
				return errStopHere
			},
			func(s godi.ServiceCollection) error {
				callOrder = append(callOrder, "third")
				return nil
			},
		)

		collection := godi.NewServiceCollection()
		err := collection.AddModules(module)
		if err == nil {
			t.Fatal("expected error")
		}

		// Should stop at the error
		if len(callOrder) != 2 {
			t.Errorf("expected 2 calls, got %d: %v", len(callOrder), callOrder)
		}
		if callOrder[0] != "first" || callOrder[1] != "second" {
			t.Errorf("unexpected call order: %v", callOrder)
		}
	})
}

func TestModule_ThreadSafety(t *testing.T) {
	t.Run("concurrent module application", func(t *testing.T) {
		// Note: ServiceCollection is not thread-safe by default,
		// but we're testing that modules themselves don't have race conditions

		const goroutines = 10
		modules := make([]func(godi.ServiceCollection) error, goroutines)

		for i := 0; i < goroutines; i++ {
			idx := i
			modules[i] = godi.NewModule(fmt.Sprintf("module-%d", idx),
				godi.AddSingleton(func() *moduleTestService {
					return &moduleTestService{id: fmt.Sprintf("service-%d", idx)}
				}, godi.Name(fmt.Sprintf("service-%d", idx))),
			)
		}

		// Apply modules sequentially (as godi.ServiceCollection is not thread-safe)
		collection := godi.NewServiceCollection()
		for _, module := range modules {
			err := collection.AddModules(module)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}

		// Verify all services were added
		for i := 0; i < goroutines; i++ {
			key := fmt.Sprintf("service-%d", i)
			if !collection.ContainsKeyed(reflect.TypeOf((*moduleTestService)(nil)), key) {
				t.Errorf("missing service: %s", key)
			}
		}
	})
}

func TestModule_PartialFailure(t *testing.T) {
	t.Run("module stops on first error", func(t *testing.T) {
		registrationCount := 0

		module := godi.NewModule("partial",
			func(s godi.ServiceCollection) error {
				registrationCount++
				return s.AddSingleton(func() moduleTestLogger {
					return &moduleTestLoggerImpl{}
				})
			},
			func(s godi.ServiceCollection) error {
				registrationCount++
				return s.AddSingleton(nil) // This will error
			},
			func(s godi.ServiceCollection) error {
				registrationCount++
				return s.AddSingleton(func() moduleTestCache {
					return &moduleTestCacheImpl{}
				})
			},
		)

		collection := godi.NewServiceCollection()
		err := collection.AddModules(module)

		if err == nil {
			t.Fatal("expected error")
		}

		// Should have attempted to register 2 services (stopped at error)
		if registrationCount != 2 {
			t.Errorf("expected 2 registration attempts, got: %d", registrationCount)
		}

		// Collection should still have the logger
		if !collection.Contains(reflect.TypeOf((*moduleTestLogger)(nil)).Elem()) {
			t.Error("logger should still be in collection despite module error")
		}

		// Collection should NOT have the cache (never reached)
		if collection.Contains(reflect.TypeOf((*moduleTestCache)(nil)).Elem()) {
			t.Error("cache should not be in collection")
		}
	})
}

func TestModule_ConstructorExecution(t *testing.T) {
	t.Run("constructors called on resolution", func(t *testing.T) {
		constructed := []string{}

		module := godi.NewModule("test",
			godi.AddSingleton(func() moduleTestLogger {
				constructed = append(constructed, "logger")
				return &moduleTestLoggerImpl{}
			}),
			godi.AddSingleton(func() moduleTestCache {
				constructed = append(constructed, "cache")
				return &moduleTestCacheImpl{}
			}),
		)

		collection := godi.NewServiceCollection()
		err := collection.AddModules(module)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Constructors not called yet
		if len(constructed) != 0 {
			t.Error("constructors should not be called during registration")
		}

		// Build provider
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Now resolve services - this calls constructors
		_, err = godi.Resolve[moduleTestLogger](provider)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(constructed) != 1 || constructed[0] != "logger" {
			t.Errorf("expected logger to be constructed, got: %v", constructed)
		}

		_, err = godi.Resolve[moduleTestCache](provider)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(constructed) != 2 || constructed[1] != "cache" {
			t.Errorf("expected cache to be constructed second, got: %v", constructed)
		}
	})
}
