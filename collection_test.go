package godi_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/junioryono/godi"
)

// Test types for dependency injection
type (
	TestLogger interface {
		Log(string)
		GetLogs() []string
	}

	testLogger struct {
		messages []string
	}

	TestDatabase interface {
		Query(string) string
	}

	testDatabase struct {
		name string
	}

	TestService struct {
		ID       string
		Logger   TestLogger
		Database TestDatabase
	}

	TestServiceWithOptional struct {
		godi.In

		Logger   TestLogger `optional:"true"`
		Database TestDatabase
	}

	TestServiceResult struct {
		godi.Out

		Service  *TestService
		Logger   TestLogger   `name:"service"`
		Database TestDatabase `group:"databases"`
	}

	testHandler struct {
		name string
	}
)

func (l *testLogger) Log(msg string) {
	l.messages = append(l.messages, msg)
}

func (d *testDatabase) Query(query string) string {
	return fmt.Sprintf("%s: %s", d.name, query)
}

func newTestLogger() TestLogger {
	return &testLogger{}
}

func (l *testLogger) GetLogs() []string {
	return l.messages
}

func newTestDatabase() TestDatabase {
	return &testDatabase{name: "test-db"}
}

func newTestService(logger TestLogger, db TestDatabase) *TestService {
	return &TestService{
		Logger:   logger,
		Database: db,
	}
}

var errConstructionFailed = errors.New("construction failed")

func newTestServiceWithError() (*TestService, error) {
	return nil, errConstructionFailed
}

func (h *testHandler) Handle() {
	// Implementation
}

// Ensure it implements Handler interface
var _ Handler = (*testHandler)(nil)

func TestNewServiceCollection(t *testing.T) {
	t.Run("creates empty collection", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		if collection == nil {
			t.Fatal("expected non-nil collection")
		}

		if collection.Count() != 0 {
			t.Errorf("expected empty collection, got %d services", collection.Count())
		}
	})
}

func TestServiceCollection_AddSingleton(t *testing.T) {
	t.Run("adds singleton service", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		err := collection.AddSingleton(newTestLogger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if collection.Count() != 1 {
			t.Errorf("expected 1 service, got %d", collection.Count())
		}

		if !collection.Contains(reflect.TypeOf((*TestLogger)(nil)).Elem()) {
			t.Error("expected collection to contain TestLogger")
		}
	})

	t.Run("rejects nil constructor", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		err := collection.AddSingleton(nil)
		if err == nil {
			t.Error("expected error for nil constructor")
		}
	})

	t.Run("rejects duplicate registration", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		err := collection.AddSingleton(newTestLogger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		err = collection.AddSingleton(newTestLogger)
		if err == nil {
			t.Error("expected error for duplicate registration")
		}
	})

	t.Run("accepts keyed services", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		err := collection.AddSingleton(newTestLogger, godi.Name("primary"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		err = collection.AddSingleton(newTestLogger, godi.Name("secondary"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if collection.Count() != 2 {
			t.Errorf("expected 2 services, got %d", collection.Count())
		}
	})

	t.Run("accepts group registration", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		err := collection.AddSingleton(newTestDatabase, godi.Group("databases"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		err = collection.AddSingleton(func() TestDatabase {
			return &testDatabase{name: "secondary"}
		}, godi.Group("databases"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if collection.Count() != 2 {
			t.Errorf("expected 2 services, got %d", collection.Count())
		}
	})

	t.Run("accepts result objects", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		constructor := func() TestServiceResult {
			return TestServiceResult{
				Service:  &TestService{},
				Logger:   newTestLogger(),
				Database: newTestDatabase(),
			}
		}

		err := collection.AddSingleton(constructor)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Result objects register as a single descriptor
		if collection.Count() != 1 {
			t.Errorf("expected 1 descriptor, got %d", collection.Count())
		}
	})
}

func TestServiceCollection_AddScoped(t *testing.T) {
	t.Run("adds scoped service", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		err := collection.AddScoped(newTestService)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		descriptors := collection.ToSlice()
		if len(descriptors) != 1 {
			t.Fatalf("expected 1 descriptor, got %d", len(descriptors))
		}

		if descriptors[0].Lifetime != godi.Scoped {
			t.Errorf("expected Scoped lifetime, got %v", descriptors[0].Lifetime)
		}
	})
}

func TestServiceCollection_AddTransient(t *testing.T) {
	t.Run("adds transient service", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		err := collection.AddTransient(newTestDatabase)
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
}

func TestServiceCollection_Decorate(t *testing.T) {
	t.Run("decorates service", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// First add a service
		err := collection.AddSingleton(newTestLogger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Then decorate it
		decorator := func(logger TestLogger) TestLogger {
			return &testLogger{messages: []string{"decorated"}}
		}

		err = collection.Decorate(decorator)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if collection.Count() != 2 { // Original + decorator
			t.Errorf("expected 2 descriptors, got %d", collection.Count())
		}
	})

	t.Run("rejects nil decorator", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		err := collection.Decorate(nil)
		if err == nil {
			t.Error("expected error for nil decorator")
		}
	})

	t.Run("rejects invalid decorator", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Not a function
		err := collection.Decorate("not a function")
		if err == nil {
			t.Error("expected error for non-function decorator")
		}

		// No parameters
		err = collection.Decorate(func() {})
		if err == nil {
			t.Error("expected error for decorator with no parameters")
		}
	})
}

func TestServiceCollection_Replace(t *testing.T) {
	t.Run("replaces existing service", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Add initial service
		err := collection.AddSingleton(func() TestLogger {
			return &testLogger{messages: []string{"original"}}
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Replace it
		err = collection.Replace(godi.Singleton, func() TestLogger {
			return &testLogger{messages: []string{"replaced"}}
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if collection.Count() != 1 {
			t.Errorf("expected 1 service after replace, got %d", collection.Count())
		}
	})

	t.Run("rejects invalid constructor", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		err := collection.Replace(godi.Singleton, "not a function")
		if err == nil {
			t.Error("expected error for non-function constructor")
		}
	})

	t.Run("rejects result object constructors", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		constructor := func() TestServiceResult {
			return TestServiceResult{}
		}

		err := collection.Replace(godi.Singleton, constructor)
		if err == nil {
			t.Error("expected error for result object constructor")
		}
	})
}

func TestServiceCollection_RemoveAll(t *testing.T) {
	t.Run("removes all services of type", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Add multiple services
		err := collection.AddSingleton(newTestLogger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		err = collection.AddSingleton(newTestDatabase)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Remove TestLogger
		err = collection.RemoveAll(reflect.TypeOf((*TestLogger)(nil)).Elem())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if collection.Count() != 1 {
			t.Errorf("expected 1 service after removal, got %d", collection.Count())
		}

		if collection.Contains(reflect.TypeOf((*TestLogger)(nil)).Elem()) {
			t.Error("TestLogger should have been removed")
		}

		if !collection.Contains(reflect.TypeOf((*TestDatabase)(nil)).Elem()) {
			t.Error("TestDatabase should still be present")
		}
	})

	t.Run("removes keyed services", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Add keyed services
		err := collection.AddSingleton(newTestLogger, godi.Name("primary"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		err = collection.AddSingleton(newTestLogger, godi.Name("secondary"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Remove all TestLogger services
		err = collection.RemoveAll(reflect.TypeOf((*TestLogger)(nil)).Elem())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if collection.Count() != 0 {
			t.Errorf("expected 0 services after removal, got %d", collection.Count())
		}

		if collection.ContainsKeyed(reflect.TypeOf((*TestLogger)(nil)).Elem(), "primary") {
			t.Error("keyed service 'primary' should have been removed")
		}
	})

	t.Run("rejects nil type", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		err := collection.RemoveAll(nil)
		if err == nil {
			t.Error("expected error for nil type")
		}
	})
}

func TestServiceCollection_Clear(t *testing.T) {
	t.Run("clears all services", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Add multiple services
		collection.AddSingleton(newTestLogger)
		collection.AddScoped(newTestDatabase)
		collection.AddTransient(newTestService)

		if collection.Count() < 3 {
			t.Fatalf("expected at least 3 services, got %d", collection.Count())
		}

		collection.Clear()

		if collection.Count() != 0 {
			t.Errorf("expected 0 services after clear, got %d", collection.Count())
		}
	})

	t.Run("can add services after clear", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Add and clear
		collection.AddSingleton(newTestLogger)
		collection.Clear()

		// Should be able to add again
		err := collection.AddSingleton(newTestLogger)
		if err != nil {
			t.Fatalf("unexpected error after clear: %v", err)
		}

		if collection.Count() != 1 {
			t.Errorf("expected 1 service, got %d", collection.Count())
		}
	})
}

func TestServiceCollection_BuildServiceProvider(t *testing.T) {
	t.Run("builds provider with services", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		err := collection.AddSingleton(newTestLogger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		err = collection.AddSingleton(newTestDatabase)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		err = collection.AddScoped(newTestService)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building provider: %v", err)
		}
		defer provider.Close()

		if provider == nil {
			t.Fatal("expected non-nil provider")
		}
	})

	t.Run("builds empty provider", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building empty provider: %v", err)
		}
		defer provider.Close()

		if provider == nil {
			t.Fatal("expected non-nil provider")
		}
	})

	t.Run("prevents building twice", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Try to build again
		_, err = collection.BuildServiceProvider()
		if err == nil {
			t.Error("expected error when building twice")
		}
	})

	t.Run("builds with custom options", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		err := collection.AddSingleton(newTestLogger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		options := &godi.ServiceProviderOptions{
			ValidateOnBuild: true,
		}

		provider, err := collection.BuildServiceProviderWithOptions(options)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		if provider == nil {
			t.Fatal("expected non-nil provider")
		}
	})
}

func TestServiceCollection_ToSlice(t *testing.T) {
	t.Run("returns copy of descriptors", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		collection.AddSingleton(newTestLogger)
		collection.AddScoped(newTestDatabase)

		slice1 := collection.ToSlice()
		slice2 := collection.ToSlice()

		if len(slice1) != 2 {
			t.Errorf("expected 2 descriptors, got %d", len(slice1))
		}

		// Modifying returned slice should not affect collection
		slice1[0] = nil

		slice3 := collection.ToSlice()
		if slice3[0] == nil {
			t.Error("modifying returned slice affected collection")
		}

		// Should be different slice instances
		if &slice1[0] == &slice2[0] {
			t.Error("expected different slice instances")
		}
	})
}

func TestServiceCollection_Contains(t *testing.T) {
	t.Run("checks service existence", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		loggerType := reflect.TypeOf((*TestLogger)(nil)).Elem()
		dbType := reflect.TypeOf((*TestDatabase)(nil)).Elem()

		if collection.Contains(loggerType) {
			t.Error("should not contain TestLogger initially")
		}

		collection.AddSingleton(newTestLogger)

		if !collection.Contains(loggerType) {
			t.Error("should contain TestLogger after adding")
		}

		if collection.Contains(dbType) {
			t.Error("should not contain TestDatabase")
		}
	})
}

func TestServiceCollection_ContainsKeyed(t *testing.T) {
	t.Run("checks keyed service existence", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		loggerType := reflect.TypeOf((*TestLogger)(nil)).Elem()

		if collection.ContainsKeyed(loggerType, "primary") {
			t.Error("should not contain keyed service initially")
		}

		collection.AddSingleton(newTestLogger, godi.Name("primary"))

		if !collection.ContainsKeyed(loggerType, "primary") {
			t.Error("should contain keyed service after adding")
		}

		if collection.ContainsKeyed(loggerType, "secondary") {
			t.Error("should not contain different key")
		}
	})
}

func TestServiceCollection_ThreadSafety(t *testing.T) {
	t.Run("prevents modification after build", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// All modification methods should fail
		err = collection.AddSingleton(newTestLogger)
		if err == nil {
			t.Error("expected error when modifying after build")
		}

		err = collection.AddScoped(newTestDatabase)
		if err == nil {
			t.Error("expected error when modifying after build")
		}

		err = collection.AddTransient(newTestService)
		if err == nil {
			t.Error("expected error when modifying after build")
		}

		err = collection.Decorate(func(l TestLogger) TestLogger { return l })
		if err == nil {
			t.Error("expected error when decorating after build")
		}

		err = collection.Replace(godi.Singleton, newTestLogger)
		if err == nil {
			t.Error("expected error when replacing after build")
		}

		err = collection.RemoveAll(reflect.TypeOf((*TestLogger)(nil)).Elem())
		if err == nil {
			t.Error("expected error when removing after build")
		}
	})
}

func TestServiceCollection_EdgeCases(t *testing.T) {
	t.Run("handles constructor with error", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		err := collection.AddSingleton(newTestServiceWithError)
		if err != nil {
			t.Fatalf("unexpected error during registration: %v", err)
		}

		// Error should occur during resolution, not registration
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building provider: %v", err)
		}
		defer provider.Close()
	})

	t.Run("handles parameter objects", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Add dependencies
		err := collection.AddSingleton(newTestDatabase)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Constructor using parameter object
		constructor := func(params TestServiceWithOptional) *TestService {
			return &TestService{
				Logger:   params.Logger, // Optional, might be nil
				Database: params.Database,
			}
		}

		err = collection.AddScoped(constructor)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building provider: %v", err)
		}
		defer provider.Close()
	})
}

func TestServiceCollection_AddInstance(t *testing.T) {
	t.Run("adds singleton instance", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Create an instance
		logger := &testLogger{messages: []string{"initialized"}}

		// Add the instance directly
		err := collection.AddSingleton(logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if collection.Count() != 1 {
			t.Errorf("expected 1 service, got %d", collection.Count())
		}

		// Build and resolve
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building provider: %v", err)
		}
		defer provider.Close()

		resolved, err := provider.Resolve(reflect.TypeOf((*testLogger)(nil)))
		if err != nil {
			t.Fatalf("unexpected error resolving: %v", err)
		}

		// Should be the exact same instance
		if resolved != logger {
			t.Error("expected same instance")
		}

		// Verify it has the pre-initialized data
		resolvedLogger := resolved.(*testLogger)
		if len(resolvedLogger.messages) != 1 || resolvedLogger.messages[0] != "initialized" {
			t.Error("instance state not preserved")
		}
	})

	t.Run("adds interface instance", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Create an instance that implements an interface
		var logger TestLogger = &testLogger{messages: []string{"interface instance"}}

		// Add the interface instance
		err := collection.AddSingleton(logger, godi.As(new(TestLogger)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building provider: %v", err)
		}
		defer provider.Close()

		// Should be able to resolve as interface
		resolved, err := godi.Resolve[TestLogger](provider)
		if err != nil {
			t.Fatalf("unexpected error resolving: %v", err)
		}

		// Verify it's the same instance
		if resolved != logger {
			t.Error("expected same instance")
		}

		logs := resolved.GetLogs()
		if len(logs) != 1 {
			t.Errorf("expected 1 log message, got %d", len(logs))
		}
	})

	t.Run("adds slice instance", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Create a slice instance
		allowedOrigins := []string{"http://localhost:3000", "https://example.com"}

		// Add the slice directly
		err := collection.AddSingleton(allowedOrigins)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building provider: %v", err)
		}
		defer provider.Close()

		resolved, err := provider.Resolve(reflect.TypeOf([]string{}))
		if err != nil {
			t.Fatalf("unexpected error resolving: %v", err)
		}

		// Should be the same slice
		resolvedSlice := resolved.([]string)
		if len(resolvedSlice) != 2 {
			t.Errorf("expected 2 items, got %d", len(resolvedSlice))
		}
		if resolvedSlice[0] != "http://localhost:3000" || resolvedSlice[1] != "https://example.com" {
			t.Error("slice content not preserved")
		}
	})

	t.Run("adds named instance", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Create instances
		primaryDB := &testDatabase{name: "primary"}
		secondaryDB := &testDatabase{name: "secondary"}

		// Add named instances
		err := collection.AddSingleton(primaryDB, godi.Name("primary"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		err = collection.AddSingleton(secondaryDB, godi.Name("secondary"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building provider: %v", err)
		}
		defer provider.Close()

		// Resolve by key
		primary, err := godi.ResolveKeyed[*testDatabase](provider, "primary")
		if err != nil {
			t.Fatalf("unexpected error resolving primary: %v", err)
		}

		secondary, err := godi.ResolveKeyed[*testDatabase](provider, "secondary")
		if err != nil {
			t.Fatalf("unexpected error resolving secondary: %v", err)
		}

		// Should be the exact instances
		if primary != primaryDB {
			t.Error("expected same primary instance")
		}
		if secondary != secondaryDB {
			t.Error("expected same secondary instance")
		}

		// Verify state
		if primary.name != "primary" {
			t.Errorf("expected primary name, got %s", primary.name)
		}
		if secondary.name != "secondary" {
			t.Errorf("expected secondary name, got %s", secondary.name)
		}
	})

	t.Run("adds group instances", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Create handler instances
		handlers := []Handler{
			&testHandler{name: "handler1"},
			&testHandler{name: "handler2"},
			&testHandler{name: "handler3"},
		}

		// Add instances to group
		for _, h := range handlers {
			err := collection.AddSingleton(h, godi.Group("handlers"), godi.As(new(Handler)))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}

		// Consumer
		type HandlerConsumer struct {
			godi.In
			Handlers []Handler `group:"handlers"`
		}

		var capturedHandlers []Handler
		collection.AddSingleton(func(params HandlerConsumer) *TestService {
			capturedHandlers = params.Handlers
			return &TestService{}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building provider: %v", err)
		}
		defer provider.Close()

		// Trigger resolution
		_, err = godi.Resolve[*TestService](provider)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify all handlers were injected
		if len(capturedHandlers) != 3 {
			t.Fatalf("expected 3 handlers, got %d", len(capturedHandlers))
		}

		// Create a map to track which handlers we've seen
		handlerMap := make(map[Handler]bool)
		for _, h := range handlers {
			handlerMap[h] = true
		}

		// Verify we got the same instances (order doesn't matter)
		for i, capturedHandler := range capturedHandlers {
			if !handlerMap[capturedHandler] {
				t.Errorf("handler at index %d is not one of the original instances", i)
			}
			delete(handlerMap, capturedHandler)
		}

		// Verify we saw all handlers
		if len(handlerMap) != 0 {
			t.Errorf("not all original handlers were found in captured handlers")
		}
	})

	t.Run("adds scoped instance", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Create an instance
		service := &TestService{ID: "scoped-instance"}

		// Add as scoped
		err := collection.AddScoped(service)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building provider: %v", err)
		}
		defer provider.Close()

		// Create two scopes
		scope1 := provider.CreateScope(context.Background())
		defer scope1.Close()
		scope2 := provider.CreateScope(context.Background())
		defer scope2.Close()

		// Resolve in both scopes
		resolved1, err := godi.Resolve[*TestService](scope1)
		if err != nil {
			t.Fatalf("unexpected error in scope1: %v", err)
		}

		resolved2, err := godi.Resolve[*TestService](scope2)
		if err != nil {
			t.Fatalf("unexpected error in scope2: %v", err)
		}

		// Should be the same instance across scopes (since we registered an instance)
		// Note: This is different from normal scoped behavior!
		if resolved1 != resolved2 {
			t.Error("expected same instance across scopes when registering instance")
		}

		if resolved1 != service {
			t.Error("expected original instance")
		}
	})

	t.Run("adds transient instance", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Create an instance
		cache := &testDatabase{name: "transient-cache"}

		// Add as transient
		err := collection.AddTransient(cache)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building provider: %v", err)
		}
		defer provider.Close()

		// Resolve multiple times
		resolved1, err := provider.Resolve(reflect.TypeOf((*testDatabase)(nil)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		resolved2, err := provider.Resolve(reflect.TypeOf((*testDatabase)(nil)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should be the same instance (since we registered an instance, not a constructor)
		// Note: This is different from normal transient behavior!
		if resolved1 != resolved2 {
			t.Error("expected same instance when registering instance as transient")
		}

		if resolved1 != cache {
			t.Error("expected original instance")
		}
	})

	t.Run("mixed constructors and instances", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Add instance
		logger := &testLogger{messages: []string{"pre-initialized"}}
		err := collection.AddSingleton(logger)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Add constructor
		err = collection.AddSingleton(newTestDatabase)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Add constructor that depends on both
		err = collection.AddSingleton(newTestService)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building provider: %v", err)
		}
		defer provider.Close()

		// Resolve the sLogger
		sLogger, err := godi.Resolve[*testLogger](provider)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Verify
		if sLogger != logger {
			t.Error("expected same logger instance")
		}

		// Resolve the database
		db, err := godi.Resolve[TestDatabase](provider)
		if err != nil {
			t.Fatalf("unexpected error resolving database: %v", err)
		}
		if db == nil {
			t.Fatal("expected non-nil database instance")
		}
		if db.Query("test") != "test-db: test" {
			t.Errorf("expected 'test-db: test', got %s", db.Query("test"))
		}
	})

	t.Run("nil instance returns error", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Try to add nil
		err := collection.AddSingleton(nil)
		if err == nil {
			t.Error("expected error for nil instance")
		}

		// Should still get the same error message
		if !errors.Is(err, godi.ErrNilConstructor) {
			t.Errorf("expected ErrNilConstructor, got %v", err)
		}
	})

	t.Run("primitive types", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Add various primitive instances
		err := collection.AddSingleton(42, godi.Name("answer"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		err = collection.AddSingleton("hello world", godi.Name("greeting"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		err = collection.AddSingleton(true, godi.Name("enabled"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error building provider: %v", err)
		}
		defer provider.Close()

		// Resolve primitives
		answer, err := godi.ResolveKeyed[int](provider, "answer")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if answer != 42 {
			t.Errorf("expected 42, got %d", answer)
		}

		greeting, err := godi.ResolveKeyed[string](provider, "greeting")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if greeting != "hello world" {
			t.Errorf("expected 'hello world', got %s", greeting)
		}

		enabled, err := godi.ResolveKeyed[bool](provider, "enabled")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !enabled {
			t.Error("expected true")
		}
	})
}
