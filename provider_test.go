package godi_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junioryono/godi"
	"go.uber.org/dig"
)

// Test types for provider tests
type (
	providerTestLogger interface {
		Log(msg string)
		GetLogs() []string
	}

	providerTestLoggerImpl struct {
		logs []string
		mu   sync.Mutex
	}

	providerTestDatabase interface {
		Query(sql string) string
		Close() error
	}

	providerTestDatabaseImpl struct {
		name     string
		closed   bool
		closeMu  sync.Mutex
		closeErr error
	}

	providerTestCache interface {
		Get(key string) (string, bool)
		Set(key string, value string)
	}

	providerTestCacheImpl struct {
		data map[string]string
		mu   sync.RWMutex
	}

	providerTestService struct {
		Logger   providerTestLogger
		Database providerTestDatabase
		Cache    providerTestCache
		ID       string
	}

	providerTestScopedService struct {
		Context context.Context
		Created time.Time
	}

	providerTestDisposableService struct {
		disposed     bool
		disposedTime time.Time
		disposeErr   error
		mu           sync.Mutex
	}

	providerTestContextDisposableService struct {
		disposed    bool
		ctx         context.Context
		disposeErr  error
		disposeTime time.Duration
		mu          sync.Mutex
	}

	// For parameter object tests
	providerTestParams struct {
		godi.In

		Logger   providerTestLogger
		Database providerTestDatabase
		Cache    providerTestCache `optional:"true"`
	}

	// For result object tests
	providerTestResult struct {
		godi.Out

		Service  *providerTestService
		Logger   providerTestLogger   `name:"service"`
		Database providerTestDatabase `group:"databases"`
	}

	// For circular dependency tests
	circularServiceA struct {
		B *circularServiceB
	}

	circularServiceB struct {
		A *circularServiceA
	}
)

// Implement test interfaces
func (l *providerTestLoggerImpl) Log(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, msg)
}

func (l *providerTestLoggerImpl) GetLogs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]string, len(l.logs))
	copy(result, l.logs)
	return result
}

func (d *providerTestDatabaseImpl) Query(sql string) string {
	return fmt.Sprintf("%s: %s", d.name, sql)
}

func (d *providerTestDatabaseImpl) Close() error {
	d.closeMu.Lock()
	defer d.closeMu.Unlock()

	if d.closed {
		return errAlreadyClosed
	}
	d.closed = true
	return d.closeErr
}

func (c *providerTestCacheImpl) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.data[key]
	return val, ok
}

func (c *providerTestCacheImpl) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data == nil {
		c.data = make(map[string]string)
	}
	c.data[key] = value
}

func (s *providerTestDisposableService) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.disposed {
		return errAlreadyDisposed
	}

	s.disposed = true
	s.disposedTime = time.Now()
	return s.disposeErr
}

func (s *providerTestContextDisposableService) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.disposed {
		return errAlreadyDisposed
	}

	s.ctx = ctx

	if s.disposeTime > 0 {
		select {
		case <-time.After(s.disposeTime):
			// Normal disposal
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	s.disposed = true
	return s.disposeErr
}

// Constructor functions
func newProviderTestLogger() providerTestLogger {
	return &providerTestLoggerImpl{}
}

func newProviderTestDatabase() providerTestDatabase {
	return &providerTestDatabaseImpl{name: "testdb"}
}

func newProviderTestCache() providerTestCache {
	return &providerTestCacheImpl{data: make(map[string]string)}
}

func newProviderTestService(logger providerTestLogger, db providerTestDatabase, cache providerTestCache) *providerTestService {
	return &providerTestService{
		Logger:   logger,
		Database: db,
		Cache:    cache,
		ID:       fmt.Sprintf("service-%d", time.Now().UnixNano()),
	}
}

func newProviderTestScopedService(ctx context.Context) *providerTestScopedService {
	return &providerTestScopedService{
		Context: ctx,
		Created: time.Now(),
	}
}

func newProviderTestDisposableService() *providerTestDisposableService {
	return &providerTestDisposableService{}
}

func newProviderTestContextDisposableService() *providerTestContextDisposableService {
	return &providerTestContextDisposableService{}
}

func newProviderTestServiceWithParams(params providerTestParams) *providerTestService {
	return &providerTestService{
		Logger:   params.Logger,
		Database: params.Database,
		Cache:    params.Cache,
		ID:       "from-params",
	}
}

func newProviderTestResult(logger providerTestLogger, db providerTestDatabase) providerTestResult {
	return providerTestResult{
		Service:  &providerTestService{Logger: logger, Database: db, ID: "from-result"},
		Logger:   logger,
		Database: db,
	}
}

func TestServiceProvider_Creation(t *testing.T) {
	t.Run("creates provider from empty collection", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		if provider == nil {
			t.Fatal("expected non-nil provider")
		}

		if provider.IsDisposed() {
			t.Error("new provider should not be disposed")
		}
	})

	t.Run("creates provider with services", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestLogger)
		collection.AddSingleton(newProviderTestDatabase)
		collection.AddScoped(newProviderTestService)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Verify services are registered
		if !provider.IsService(reflect.TypeOf((*providerTestLogger)(nil)).Elem()) {
			t.Error("logger should be registered")
		}
		if !provider.IsService(reflect.TypeOf((*providerTestDatabase)(nil)).Elem()) {
			t.Error("database should be registered")
		}
		if !provider.IsService(reflect.TypeOf((*providerTestService)(nil))) {
			t.Error("service should be registered")
		}
	})

	t.Run("validates on build when requested", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Add a service with missing dependency
		collection.AddSingleton(func(missing providerTestCache) providerTestLogger {
			return newProviderTestLogger()
		})

		options := &godi.ServiceProviderOptions{
			ValidateOnBuild: true,
		}

		_, err := collection.BuildServiceProviderWithOptions(options)
		if err == nil {
			t.Error("expected validation error for missing dependency")
		}
	})

	t.Run("handles circular dependencies", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Create circular dependency
		collection.AddSingleton(func(b *circularServiceB) *circularServiceA {
			return &circularServiceA{B: b}
		})
		collection.AddSingleton(func(a *circularServiceA) *circularServiceB {
			return &circularServiceB{A: a}
		})

		options := &godi.ServiceProviderOptions{
			ValidateOnBuild: true,
		}

		_, err := collection.BuildServiceProviderWithOptions(options)
		if err == nil {
			t.Error("expected error for circular dependency")
		}

		if !godi.IsCircularDependency(err) {
			t.Errorf("expected circular dependency error, got %T: %v", err, err)
		}
	})
}

func TestServiceProvider_Resolve(t *testing.T) {
	t.Run("resolves singleton service", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestLogger)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Resolve using interface type
		service, err := provider.Resolve(reflect.TypeOf((*providerTestLogger)(nil)).Elem())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		logger, ok := service.(providerTestLogger)
		if !ok {
			t.Fatalf("expected providerTestLogger, got %T", service)
		}

		// Test it works
		logger.Log("test")
		logs := logger.GetLogs()
		if len(logs) != 1 || logs[0] != "test" {
			t.Error("logger not working correctly")
		}
	})

	t.Run("resolves same singleton instance", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestLogger)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		service1, _ := provider.Resolve(reflect.TypeOf((*providerTestLogger)(nil)).Elem())
		service2, _ := provider.Resolve(reflect.TypeOf((*providerTestLogger)(nil)).Elem())

		// Should be same instance
		logger1 := service1.(*providerTestLoggerImpl)
		logger2 := service2.(*providerTestLoggerImpl)

		if logger1 != logger2 {
			t.Error("singleton should return same instance")
		}
	})

	t.Run("resolves interface same singleton instance", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestLogger)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		service1, _ := godi.Resolve[providerTestLogger](provider)
		service2, _ := godi.Resolve[providerTestLogger](provider)

		service1.Log("test1")
		service2.Log("test2")
		logs1 := service1.GetLogs()
		logs2 := service2.GetLogs()
		if len(logs1) != 2 || logs1[0] != "test1" || logs1[0] != logs2[0] || logs1[1] != "test2" || logs1[1] != logs2[1] {
			t.Error("expected same logs for singleton interface resolution")
		}
	})

	t.Run("resolves service with dependencies", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestLogger)
		collection.AddSingleton(newProviderTestDatabase)
		collection.AddSingleton(newProviderTestCache)
		collection.AddSingleton(newProviderTestService)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		service, err := provider.Resolve(reflect.TypeOf((*providerTestService)(nil)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		svc := service.(*providerTestService)
		if svc.Logger == nil {
			t.Error("logger dependency not injected")
		}
		if svc.Database == nil {
			t.Error("database dependency not injected")
		}
		if svc.Cache == nil {
			t.Error("cache dependency not injected")
		}
	})

	t.Run("returns error for unregistered service", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		_, err = provider.Resolve(reflect.TypeOf((*providerTestLogger)(nil)).Elem())
		if err == nil {
			t.Error("expected error for unregistered service")
		}
	})

	t.Run("returns error for nil type", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		_, err = provider.Resolve(nil)
		if err == nil {
			t.Error("expected error for nil type")
		}
		if !errors.Is(err, godi.ErrInvalidServiceType) {
			t.Errorf("expected ErrInvalidServiceType, got %v", err)
		}
	})

	t.Run("returns error when disposed", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		provider.Close()

		_, err = provider.Resolve(reflect.TypeOf((*providerTestLogger)(nil)).Elem())
		if err == nil {
			t.Error("expected error when resolving from disposed provider")
		}
		if !errors.Is(err, godi.ErrProviderDisposed) {
			t.Errorf("expected ErrProviderDisposed, got %v", err)
		}
	})
}

func TestServiceProvider_ResolveGeneric(t *testing.T) {
	t.Run("resolves interface type", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestLogger)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		logger, err := godi.Resolve[providerTestLogger](provider)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if logger == nil {
			t.Fatal("expected non-nil logger")
		}

		logger.Log("test")
		logs := logger.GetLogs()
		if len(logs) != 1 || logs[0] != "test" {
			t.Error("logger not working correctly")
		}
	})

	t.Run("resolves concrete type", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestService)
		collection.AddSingleton(newProviderTestLogger)
		collection.AddSingleton(newProviderTestDatabase)
		collection.AddSingleton(newProviderTestCache)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		service, err := godi.Resolve[providerTestService](provider)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if service.Logger == nil {
			t.Error("logger not injected")
		}
	})

	t.Run("returns error for nil provider", func(t *testing.T) {
		_, err := godi.Resolve[providerTestLogger](nil)
		if err == nil {
			t.Error("expected error for nil provider")
		}
		if !errors.Is(err, godi.ErrNilServiceProvider) {
			t.Errorf("expected ErrNilServiceProvider, got %v", err)
		}
	})
}

func TestServiceProvider_ResolveKeyed(t *testing.T) {
	t.Run("resolves keyed service", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(func() providerTestDatabase {
			return &providerTestDatabaseImpl{name: "primary"}
		}, dig.Name("primary"))
		collection.AddSingleton(func() providerTestDatabase {
			return &providerTestDatabaseImpl{name: "secondary"}
		}, dig.Name("secondary"))

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Resolve primary
		primary, err := provider.ResolveKeyed(reflect.TypeOf((*providerTestDatabase)(nil)).Elem(), "primary")
		if err != nil {
			t.Fatalf("unexpected error resolving primary: %v", err)
		}

		primaryDB := primary.(providerTestDatabase)
		result := primaryDB.Query("SELECT 1")
		if !strings.Contains(result, "primary") {
			t.Error("expected primary database")
		}

		// Resolve secondary
		secondary, err := provider.ResolveKeyed(reflect.TypeOf((*providerTestDatabase)(nil)).Elem(), "secondary")
		if err != nil {
			t.Fatalf("unexpected error resolving secondary: %v", err)
		}

		secondaryDB := secondary.(providerTestDatabase)
		result = secondaryDB.Query("SELECT 1")
		if !strings.Contains(result, "secondary") {
			t.Error("expected secondary database")
		}
	})

	t.Run("returns error for nil key", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		_, err = provider.ResolveKeyed(reflect.TypeOf((*providerTestLogger)(nil)).Elem(), nil)
		if err == nil {
			t.Error("expected error for nil key")
		}
	})

	t.Run("generic helper works", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(func() providerTestDatabase {
			return &providerTestDatabaseImpl{name: "primary"}
		}, dig.Name("primary"))

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		db, err := godi.ResolveKeyed[providerTestDatabase](provider, "primary")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if db == nil {
			t.Fatal("expected non-nil database")
		}

		result := db.Query("SELECT 1")
		if !strings.Contains(result, "primary") {
			t.Error("expected primary database")
		}
	})
}

func TestServiceProvider_IsService(t *testing.T) {
	t.Run("returns true for registered service", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestLogger)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		if !provider.IsService(reflect.TypeOf((*providerTestLogger)(nil)).Elem()) {
			t.Error("expected IsService to return true for registered service")
		}
	})

	t.Run("returns false for unregistered service", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		if provider.IsService(reflect.TypeOf((*providerTestLogger)(nil)).Elem()) {
			t.Error("expected IsService to return false for unregistered service")
		}
	})

	t.Run("returns false when disposed", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestLogger)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		provider.Close()

		if provider.IsService(reflect.TypeOf((*providerTestLogger)(nil)).Elem()) {
			t.Error("expected IsService to return false when disposed")
		}
	})
}

func TestServiceProvider_IsKeyedService(t *testing.T) {
	t.Run("returns true for registered keyed service", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestDatabase, dig.Name("primary"))

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		if !provider.IsKeyedService(reflect.TypeOf((*providerTestDatabase)(nil)).Elem(), "primary") {
			t.Error("expected IsKeyedService to return true")
		}
	})

	t.Run("returns false for wrong key", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestDatabase, dig.Name("primary"))

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		if provider.IsKeyedService(reflect.TypeOf((*providerTestDatabase)(nil)).Elem(), "secondary") {
			t.Error("expected IsKeyedService to return false for wrong key")
		}
	})
}

func TestServiceProvider_CreateScope(t *testing.T) {
	t.Run("creates scope", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddScoped(newProviderTestScopedService)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		if scope == nil {
			t.Fatal("expected non-nil scope")
		}
		defer scope.Close()
	})

	t.Run("injects context into scoped services", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddScoped(newProviderTestScopedService)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		type testContextKey string
		const testKey testContextKey = "test-key"

		ctx := context.WithValue(context.Background(), testKey, "test-value")
		scope := provider.CreateScope(ctx)
		defer scope.Close()

		service, err := scope.Resolve(reflect.TypeOf((*providerTestScopedService)(nil)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		scopedService := service.(*providerTestScopedService)
		if scopedService.Context == nil {
			t.Fatal("expected context to be injected")
		}

		if val := scopedService.Context.Value(testKey); val != "test-value" {
			t.Errorf("expected context value 'test-value', got %v", val)
		}
	})

	t.Run("panics when provider is disposed", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		provider.Close()

		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic when creating scope from disposed provider")
			}
		}()

		provider.CreateScope(context.Background())
	})

	t.Run("singleton services accessible in scope", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestLogger)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		logger, err := scope.Resolve(reflect.TypeOf((*providerTestLogger)(nil)).Elem())
		if err != nil {
			t.Fatalf("unexpected error resolving logger in scope: %v", err)
		}

		if logger == nil {
			t.Fatal("expected non-nil logger in scope")
		}
	})
}

func TestServiceProvider_Invoke(t *testing.T) {
	t.Run("invokes function with dependencies", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestLogger)
		collection.AddSingleton(newProviderTestDatabase)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		var invokedLogger providerTestLogger
		var invokedDB providerTestDatabase

		err = provider.Invoke(func(logger providerTestLogger, db providerTestDatabase) {
			invokedLogger = logger
			invokedDB = db
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if invokedLogger == nil {
			t.Error("logger not injected")
		}
		if invokedDB == nil {
			t.Error("database not injected")
		}
	})

	t.Run("returns function error", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestLogger)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		err = provider.Invoke(func(logger providerTestLogger) error {
			return errTest
		})

		if err == nil {
			t.Error("expected error from invoked function")
		}

		if !errors.Is(err, errTest) {
			t.Errorf("expected errTest, got %v", err)
		}
	})

	t.Run("returns error for missing dependency", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		err = provider.Invoke(func(logger providerTestLogger) {})
		if err == nil {
			t.Error("expected error for missing dependency")
		}
	})

	t.Run("returns error when disposed", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		provider.Close()

		err = provider.Invoke(func() {})
		if err == nil {
			t.Error("expected error when invoking on disposed provider")
		}
		if !errors.Is(err, godi.ErrProviderDisposed) {
			t.Errorf("expected ErrProviderDisposed, got %v", err)
		}
	})
}

func TestServiceProvider_Close(t *testing.T) {
	t.Run("disposes provider", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if provider.IsDisposed() {
			t.Error("new provider should not be disposed")
		}

		err = provider.Close()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if !provider.IsDisposed() {
			t.Error("provider should be disposed after Close")
		}
	})

	t.Run("disposes singleton services", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestDisposableService)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Resolve to create instance
		service, err := provider.Resolve(reflect.TypeOf((*providerTestDisposableService)(nil)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		disposable := service.(*providerTestDisposableService)
		if disposable.disposed {
			t.Error("service should not be disposed initially")
		}

		provider.Close()

		if !disposable.disposed {
			t.Error("singleton service should be disposed when provider is closed")
		}
	})

	t.Run("safe to call multiple times", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		err = provider.Close()
		if err != nil {
			t.Fatalf("first close error: %v", err)
		}

		// Second close should not error
		err = provider.Close()
		if err != nil {
			t.Fatalf("second close error: %v", err)
		}
	})

	t.Run("disposes context-aware services", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestContextDisposableService)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Resolve to create instance
		service, err := provider.Resolve(reflect.TypeOf((*providerTestContextDisposableService)(nil)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		disposable := service.(*providerTestContextDisposableService)
		provider.Close()

		if !disposable.disposed {
			t.Error("context-aware service should be disposed")
		}
		if disposable.ctx == nil {
			t.Error("context should be passed to Close")
		}
	})
}

func TestServiceProvider_BuiltInServices(t *testing.T) {
	t.Run("provides ServiceProvider", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		service1, err := godi.Resolve[godi.ServiceProvider](provider)
		if err != nil {
			t.Fatalf("unexpected error resolving ServiceProvider: %v", err)
		}

		service2, err := provider.Resolve(reflect.TypeOf((*godi.ServiceProvider)(nil)).Elem())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if service1 == nil {
			t.Fatal("expected ServiceProvider to be available")
		}

		svc2, ok := service2.(godi.ServiceProvider)
		if !ok {
			t.Error("expected ServiceProvider interface")
		}

		// Compare the actual instances, not the addresses of local variables
		if service1 != svc2 {
			t.Errorf("expected same instance but got different instances")
		}
	})
}

func TestServiceProvider_ParameterObjects(t *testing.T) {
	t.Run("works with parameter objects", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestLogger)
		collection.AddSingleton(newProviderTestDatabase)
		// Don't add cache - it's optional
		collection.AddSingleton(newProviderTestServiceWithParams)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		service, err := provider.Resolve(reflect.TypeOf((*providerTestService)(nil)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		svc := service.(*providerTestService)
		if svc.Logger == nil {
			t.Error("logger should be injected")
		}
		if svc.Database == nil {
			t.Error("database should be injected")
		}
		if svc.Cache != nil {
			t.Error("optional cache should be nil when not registered")
		}
		if svc.ID != "from-params" {
			t.Error("service should be created from params constructor")
		}
	})
}

func TestServiceProvider_ResultObjects(t *testing.T) {
	t.Run("works with result objects", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestLogger)
		collection.AddSingleton(newProviderTestDatabase)
		collection.AddSingleton(newProviderTestResult)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Should be able to resolve the service
		service, err := provider.Resolve(reflect.TypeOf((*providerTestService)(nil)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		svc := service.(*providerTestService)
		if svc.ID != "from-result" {
			t.Error("service should be from result object")
		}

		// Should be able to resolve named logger
		logger, err := provider.ResolveKeyed(reflect.TypeOf((*providerTestLogger)(nil)).Elem(), "service")
		if err != nil {
			t.Fatalf("unexpected error resolving keyed logger: %v", err)
		}
		if logger == nil {
			t.Error("expected keyed logger")
		}
	})
}

func TestServiceProvider_GroupServices(t *testing.T) {
	t.Run("collects group services", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Add multiple handlers to a group
		collection.AddSingleton(func() Handler {
			return &struct{ Handler }{}
		}, dig.Group("handlers"))

		collection.AddSingleton(func() Handler {
			return &struct{ Handler }{}
		}, dig.Group("handlers"))

		// Consumer of group
		var handlerCount int
		collection.AddSingleton(func(params struct {
			godi.In
			Handlers []Handler `group:"handlers"`
		}) *providerTestService {
			handlerCount = len(params.Handlers)
			return &providerTestService{ID: fmt.Sprintf("handlers-%d", len(params.Handlers))}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		service, err := provider.Resolve(reflect.TypeOf((*providerTestService)(nil)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		svc := service.(*providerTestService)
		if !strings.HasPrefix(svc.ID, "handlers-") {
			t.Error("service should be created with handlers")
		}

		if handlerCount != 2 {
			t.Errorf("expected 2 handlers, got %d", handlerCount)
		}
	})
}

func TestServiceProvider_Concurrency(t *testing.T) {
	t.Run("concurrent resolution", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestLogger)
		collection.AddSingleton(newProviderTestDatabase)
		collection.AddSingleton(newProviderTestCache)
		collection.AddTransient(newProviderTestService)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		const goroutines = 50
		var wg sync.WaitGroup
		wg.Add(goroutines)

		errors := make(chan error, goroutines)

		for i := 0; i < goroutines; i++ {
			go func() {
				defer wg.Done()

				// Resolve different services
				_, err := provider.Resolve(reflect.TypeOf((*providerTestLogger)(nil)).Elem())
				if err != nil {
					errors <- err
					return
				}

				_, err = provider.Resolve(reflect.TypeOf((*providerTestService)(nil)))
				if err != nil {
					errors <- err
					return
				}

				// Create and use scope
				scope := provider.CreateScope(context.Background())
				_, err = scope.Resolve(reflect.TypeOf((*providerTestService)(nil)))
				scope.Close()
				if err != nil {
					errors <- err
				}
			}()
		}

		wg.Wait()
		close(errors)

		for err := range errors {
			t.Errorf("concurrent error: %v", err)
		}
	})

	t.Run("concurrent scope creation", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddScoped(newProviderTestScopedService)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		const goroutines = 20
		scopes := make([]godi.Scope, goroutines)
		var wg sync.WaitGroup
		wg.Add(goroutines)

		for i := 0; i < goroutines; i++ {
			idx := i
			go func() {
				defer wg.Done()
				scopes[idx] = provider.CreateScope(context.Background())
			}()
		}

		wg.Wait()

		// Clean up scopes
		for _, scope := range scopes {
			if scope != nil {
				scope.Close()
			}
		}
	})
}

func TestServiceProvider_Decorators(t *testing.T) {
	t.Run("decorates service", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Original logger
		collection.AddSingleton(newProviderTestLogger)

		// Decorator that wraps the logger
		decoratorCalled := false
		collection.Decorate(func(logger providerTestLogger) providerTestLogger {
			decoratorCalled = true
			// Wrap the original logger
			return &providerTestLoggerImpl{
				logs: []string{"decorated"},
			}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		service, err := provider.Resolve(reflect.TypeOf((*providerTestLogger)(nil)).Elem())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		logger := service.(providerTestLogger)
		logs := logger.GetLogs()

		if len(logs) == 0 || logs[0] != "decorated" {
			t.Error("logger should be decorated")
		}

		if !decoratorCalled {
			t.Error("decorator should have been called")
		}
	})
}

func TestServiceProvider_ErrorHandling(t *testing.T) {
	t.Run("handles panic in constructor", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(func() providerTestLogger {
			panic("constructor panic")
		})

		options := &godi.ServiceProviderOptions{
			RecoverFromPanics: true,
		}

		provider, err := collection.BuildServiceProviderWithOptions(options)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		_, err = provider.Resolve(reflect.TypeOf((*providerTestLogger)(nil)).Elem())
		if err == nil {
			t.Error("expected error from panicking constructor")
		}

		var digPanic dig.PanicError
		if !errors.As(err, &digPanic) {
			t.Errorf("expected panic error, got %T: %v", err, err)
		}
	})

	t.Run("handles constructor error", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(func() (providerTestLogger, error) {
			return nil, errConstructor
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		_, err = provider.Resolve(reflect.TypeOf((*providerTestLogger)(nil)).Elem())
		if err == nil {
			t.Error("expected error from constructor")
		}
	})

	t.Run("handles disposal error", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(func() *providerTestDisposableService {
			return &providerTestDisposableService{
				disposeErr: errDisposal,
			}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Resolve to create instance
		provider.Resolve(reflect.TypeOf((*providerTestDisposableService)(nil)))

		err = provider.Close()
		if err == nil {
			t.Error("expected disposal error")
		}
	})
}

func TestServiceProvider_Options(t *testing.T) {
	t.Run("dry run mode", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		invoked := int32(0)
		collection.AddSingleton(func() providerTestLogger {
			atomic.AddInt32(&invoked, 1)
			return newProviderTestLogger()
		})

		options := &godi.ServiceProviderOptions{
			DryRun: true,
		}

		provider, err := collection.BuildServiceProviderWithOptions(options)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// In dry run mode, constructors aren't actually invoked
		// This is mainly useful for validation
		if atomic.LoadInt32(&invoked) != 0 {
			t.Error("constructor should not be invoked in dry run mode")
		}
	})

	t.Run("deferred acyclic verification", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Create circular dependency
		collection.AddSingleton(func(b *circularServiceB) *circularServiceA {
			return &circularServiceA{B: b}
		})
		collection.AddSingleton(func(a *circularServiceA) *circularServiceB {
			return &circularServiceB{A: a}
		})

		options := &godi.ServiceProviderOptions{
			DeferAcyclicVerification: true,
		}

		// Should not error during build with deferred verification
		provider, err := collection.BuildServiceProviderWithOptions(options)
		if err != nil {
			t.Fatalf("unexpected error with deferred verification: %v", err)
		}
		defer provider.Close()

		// Error should occur on first invoke
		err = provider.Invoke(func(a *circularServiceA) {})
		if err == nil {
			t.Error("expected cycle error on invoke")
		}
	})
}

func TestServiceProvider_TransientLifetime(t *testing.T) {
	t.Run("creates new instance each time for transient", func(t *testing.T) {
		instanceCount := int32(0)
		collection := godi.NewServiceCollection()
		collection.AddTransient(func() *providerTestService {
			atomic.AddInt32(&instanceCount, 1)
			return &providerTestService{
				ID: fmt.Sprintf("instance-%d", atomic.LoadInt32(&instanceCount)),
			}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Resolve multiple times
		service1, err := godi.Resolve[providerTestService](provider)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		service2, err := godi.Resolve[providerTestService](provider)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Should be different instances
		if service1 == service2 {
			t.Error("transient service should create new instances")
		}

		// Should have different IDs
		if service1.ID == service2.ID {
			t.Error("transient services should have different IDs")
		}

		// Should have created 2 instances
		if atomic.LoadInt32(&instanceCount) != 2 {
			t.Errorf("expected 2 instances created, got %d", instanceCount)
		}
	})

	t.Run("disposes all transient instances on scope close", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddTransient(newProviderTestDisposableService)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())

		// Create multiple transient instances in scope
		var services []*providerTestDisposableService
		for i := 0; i < 3; i++ {
			service, err := scope.Resolve(reflect.TypeOf((*providerTestDisposableService)(nil)))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			services = append(services, service.(*providerTestDisposableService))
		}

		// Close scope
		scope.Close()

		// All instances should be disposed
		for i, svc := range services {
			if !svc.disposed {
				t.Errorf("transient instance %d should be disposed", i)
			}
		}
	})
}

func TestServiceProvider_ScopedLifetime(t *testing.T) {
	t.Run("same instance within scope", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddScoped(newProviderTestService)
		collection.AddSingleton(newProviderTestLogger)
		collection.AddSingleton(newProviderTestDatabase)
		collection.AddSingleton(newProviderTestCache)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		// Resolve multiple times within same scope
		service1, _ := scope.Resolve(reflect.TypeOf((*providerTestService)(nil)))
		service2, _ := scope.Resolve(reflect.TypeOf((*providerTestService)(nil)))

		// Should be same instance within scope
		if service1 != service2 {
			t.Error("scoped service should return same instance within scope")
		}
	})

	t.Run("different instances across scopes", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddScoped(newProviderTestService)
		collection.AddSingleton(newProviderTestLogger)
		collection.AddSingleton(newProviderTestDatabase)
		collection.AddSingleton(newProviderTestCache)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope1 := provider.CreateScope(context.Background())
		defer scope1.Close()
		scope2 := provider.CreateScope(context.Background())
		defer scope2.Close()

		service1, _ := scope1.Resolve(reflect.TypeOf((*providerTestService)(nil)))
		service2, _ := scope2.Resolve(reflect.TypeOf((*providerTestService)(nil)))

		// Should be different instances across scopes
		svc1 := service1.(*providerTestService)
		svc2 := service2.(*providerTestService)

		svc1.ID = "scope1"
		svc2.ID = "scope2"

		if svc1 == svc2 {
			t.Error("scoped service should create different instances across scopes")
		}

		if svc1.ID == svc2.ID {
			t.Error("scoped services should have different IDs across scopes")
		}

		service1, _ = scope1.Resolve(reflect.TypeOf((*providerTestService)(nil)))
		if service1.(*providerTestService).ID != "scope1" {
			t.Error("service in scope1 should still be the same instance")
		}
	})
}

func TestServiceProvider_NestedScopes(t *testing.T) {
	// Define trackingDisposable type for this test suite
	type trackingDisposable struct {
		name     string
		disposed bool
		mu       sync.Mutex
	}

	// Method to implement Disposable
	closeFunc := func(td *trackingDisposable) error {
		td.mu.Lock()
		defer td.mu.Unlock()
		td.disposed = true
		return nil
	}

	t.Run("nested scope inherits from parent", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestLogger)
		collection.AddScoped(newProviderTestDatabase)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Create parent scope
		parentScope := provider.CreateScope(context.Background())
		defer parentScope.Close()

		// Create child scope from parent
		childScope := parentScope.CreateScope(context.Background())
		defer childScope.Close()

		// Child should be able to resolve singleton
		logger, err := childScope.Resolve(reflect.TypeOf((*providerTestLogger)(nil)).Elem())
		if err != nil {
			t.Errorf("child scope should resolve singleton: %v", err)
		}
		if logger == nil {
			t.Error("expected non-nil logger")
		}
	})

	t.Run("scope disposal order", func(t *testing.T) {
		disposables := []*trackingDisposable{}
		mu := sync.Mutex{}

		collection := godi.NewServiceCollection()

		// Create a service that tracks disposal
		collection.AddScoped(func() godi.Disposable {
			mu.Lock()
			td := &trackingDisposable{name: "child-service"}
			disposables = append(disposables, td)
			mu.Unlock()

			// Return a closure that implements Disposable
			return closerFunc(func() error {
				return closeFunc(td)
			})
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		parentScope := provider.CreateScope(context.Background())
		childScope := parentScope.CreateScope(context.Background())

		// Create service in child scope
		childScope.Resolve(reflect.TypeOf((*godi.Disposable)(nil)).Elem())

		// Close in order: child first, then parent
		childScope.Close()
		parentScope.Close()
		provider.Close()

		// Verify disposal happened
		disposed := false
		for _, td := range disposables {
			if td.disposed {
				disposed = true
				break
			}
		}

		if !disposed && len(disposables) > 0 {
			t.Error("expected at least one disposal to occur")
		}
	})
}

func (f closerFunc) Close() error {
	return f()
}

func TestServiceProvider_ComplexDependencyGraphs(t *testing.T) {
	t.Run("deep dependency chain", func(t *testing.T) {
		// Create a deep dependency chain: A -> B -> C -> D -> E
		type serviceE struct{ Value string }
		type serviceD struct{ E *serviceE }
		type serviceC struct{ D *serviceD }
		type serviceB struct{ C *serviceC }
		type serviceA struct{ B *serviceB }

		collection := godi.NewServiceCollection()
		collection.AddSingleton(func() *serviceE { return &serviceE{Value: "deep"} })
		collection.AddSingleton(func(e *serviceE) *serviceD { return &serviceD{E: e} })
		collection.AddSingleton(func(d *serviceD) *serviceC { return &serviceC{D: d} })
		collection.AddSingleton(func(c *serviceC) *serviceB { return &serviceB{C: c} })
		collection.AddSingleton(func(b *serviceB) *serviceA { return &serviceA{B: b} })

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		service, err := provider.Resolve(reflect.TypeOf((*serviceA)(nil)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		a := service.(*serviceA)
		if a.B.C.D.E.Value != "deep" {
			t.Error("deep dependency chain not resolved correctly")
		}
	})

	t.Run("diamond dependency", func(t *testing.T) {
		// Create a diamond dependency:
		//     A
		//    / \
		//   B   C
		//    \ /
		//     D
		type serviceA struct{ Value string }
		type serviceB struct{ A *serviceA }
		type serviceC struct{ A *serviceA }
		type serviceD struct {
			B *serviceB
			C *serviceC
		}

		instanceCount := 0
		collection := godi.NewServiceCollection()
		collection.AddSingleton(func() *serviceA {
			instanceCount++
			return &serviceA{Value: "shared"}
		})
		collection.AddSingleton(func(a *serviceA) *serviceB { return &serviceB{A: a} })
		collection.AddSingleton(func(a *serviceA) *serviceC { return &serviceC{A: a} })
		collection.AddSingleton(func(b *serviceB, c *serviceC) *serviceD { return &serviceD{B: b, C: c} })

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		service, err := provider.Resolve(reflect.TypeOf((*serviceD)(nil)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		d := service.(*serviceD)

		// Both paths should lead to the same instance
		if d.B.A != d.C.A {
			t.Error("singleton should be shared in diamond dependency")
		}

		// Should only create one instance
		if instanceCount != 1 {
			t.Errorf("expected 1 instance, got %d", instanceCount)
		}
	})
}

func TestServiceProvider_EdgeCases(t *testing.T) {
	t.Run("nil context becomes background context", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddScoped(newProviderTestScopedService)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.TODO())
		defer scope.Close()

		service, err := scope.Resolve(reflect.TypeOf((*providerTestScopedService)(nil)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		scopedService := service.(*providerTestScopedService)
		if scopedService.Context == nil {
			t.Error("expected non-nil context")
		}
	})

	t.Run("empty provider can still resolve built-ins", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Should be able to resolve ServiceProvider
		sp, err := provider.Resolve(reflect.TypeOf((*godi.ServiceProvider)(nil)).Elem())
		if err != nil {
			t.Errorf("should resolve ServiceProvider: %v", err)
		}
		if sp == nil {
			t.Error("expected non-nil ServiceProvider")
		}
	})

	t.Run("very large number of services", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Add many services
		const serviceCount = 1000
		for i := 0; i < serviceCount; i++ {
			id := i // Capture loop variable
			collection.AddSingleton(func() *providerTestService {
				return &providerTestService{ID: fmt.Sprintf("service-%d", id)}
			}, dig.Name(fmt.Sprintf("service%d", id)))
		}

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Resolve a few to ensure it works
		for i := 0; i < 10; i++ {
			service, err := provider.ResolveKeyed(
				reflect.TypeOf((*providerTestService)(nil)),
				fmt.Sprintf("service%d", i),
			)
			if err != nil {
				t.Errorf("failed to resolve service%d: %v", i, err)
			}
			if service == nil {
				t.Errorf("expected non-nil service%d", i)
			}
		}
	})
}

func TestServiceProvider_OptionsValidation(t *testing.T) {
	t.Run("resolution timeout", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Add a slow constructor
		collection.AddSingleton(func() *providerTestService {
			time.Sleep(100 * time.Millisecond)
			return &providerTestService{}
		})

		options := &godi.ServiceProviderOptions{
			ResolutionTimeout: 50 * time.Millisecond,
		}

		provider, err := collection.BuildServiceProviderWithOptions(options)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()
	})

	t.Run("callbacks with nil functions", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestLogger)

		options := &godi.ServiceProviderOptions{
			OnServiceResolved: nil,
			OnServiceError:    nil,
		}

		provider, err := collection.BuildServiceProviderWithOptions(options)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Should not panic when resolving
		_, err = provider.Resolve(reflect.TypeOf((*providerTestLogger)(nil)).Elem())
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestServiceProvider_DisposalErrors(t *testing.T) {
	t.Run("multiple disposal errors are joined", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Add multiple services that fail to dispose
		for i := 0; i < 3; i++ {
			collection.AddSingleton(func() *providerTestDisposableService {
				return &providerTestDisposableService{
					disposeErr: errAlreadyDisposed,
				}
			}, dig.Name(fmt.Sprintf("service%d", i)))
		}

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Resolve all services to create instances
		for i := 0; i < 3; i++ {
			provider.ResolveKeyed(
				reflect.TypeOf((*providerTestDisposableService)(nil)),
				fmt.Sprintf("service%d", i),
			)
		}

		// Close should return joined errors
		err = provider.Close()
		if err == nil {
			t.Error("expected disposal errors")
		}

		// Should contain all error messages
		errStr := err.Error()
		for i := 0; i < 3; i++ {
			if !strings.Contains(errStr, "already disposed") {
				t.Error("expected error to contain 'already disposed'")
			}
		}
	})

	t.Run("context timeout during disposal", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Add a service that takes too long to dispose
		collection.AddSingleton(func() *providerTestContextDisposableService {
			return &providerTestContextDisposableService{
				disposeTime: 200 * time.Millisecond,
			}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Resolve to create instance
		provider.Resolve(reflect.TypeOf((*providerTestContextDisposableService)(nil)))

		// Close should handle timeout
		err = provider.Close()
		// The default timeout is 30 seconds, so this shouldn't timeout
		// But the service should still be disposed
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})
}

func TestServiceProvider_ResultObjectLifetimes(t *testing.T) {
	t.Run("result object with singleton lifetime", func(t *testing.T) {
		callCount := 0
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestLogger)
		collection.AddSingleton(newProviderTestDatabase)
		collection.AddSingleton(func(logger providerTestLogger, db providerTestDatabase) providerTestResult {
			callCount++
			return providerTestResult{
				Service:  &providerTestService{ID: fmt.Sprintf("call-%d", callCount)},
				Logger:   logger,
				Database: db,
			}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Resolve multiple times
		service1, _ := provider.Resolve(reflect.TypeOf((*providerTestService)(nil)))
		service2, _ := provider.Resolve(reflect.TypeOf((*providerTestService)(nil)))

		// Should be same instance (singleton)
		if service1 != service2 {
			t.Error("result object fields should be singleton when constructor is singleton")
		}

		// Constructor should be called only once
		if callCount != 1 {
			t.Errorf("expected constructor to be called once, got %d", callCount)
		}
	})
}

func TestServiceProvider_ResolutionTimeoutActuallyTimesOut(t *testing.T) {
	t.Run("resolution timeout causes actual timeout", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Add a service that blocks forever
		blockChan := make(chan struct{})
		collection.AddSingleton(func() *providerTestService {
			<-blockChan // Block forever
			return &providerTestService{}
		})

		options := &godi.ServiceProviderOptions{
			ResolutionTimeout: 50 * time.Millisecond,
		}

		provider, err := collection.BuildServiceProviderWithOptions(options)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()
		defer close(blockChan) // Cleanup

		start := time.Now()
		_, err = provider.Resolve(reflect.TypeOf((*providerTestService)(nil)))
		elapsed := time.Since(start)

		if err == nil {
			t.Error("expected timeout error")
		}

		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("expected context.DeadlineExceeded error, got: %v", err)
		}

		// Should timeout within reasonable bounds
		if elapsed < 50*time.Millisecond || elapsed > 100*time.Millisecond {
			t.Errorf("expected timeout around 50ms, got %v", elapsed)
		}
	})
}

func TestServiceProvider_PanicInDisposal(t *testing.T) {
	t.Run("handles panic during disposal", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Service that panics on disposal
		collection.AddSingleton(func() godi.Disposable {
			return closerFunc(func() error {
				panic("disposal panic")
			})
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Resolve to create instance
		provider.Resolve(reflect.TypeOf((*godi.Disposable)(nil)).Elem())

		defer func() {
			if r := recover(); r == nil {
				t.Error("expected panic during disposal")
			}
		}()

		err = provider.Close()
		if err == nil {
			t.Error("expected error from disposal panic")
		}
	})
}

func TestServiceProvider_KeyedServiceEdgeCases(t *testing.T) {
	t.Run("nil vs empty string keys", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Register with empty string key
		err := collection.AddSingleton(func() *providerTestService {
			return &providerTestService{ID: "empty-key"}
		}, godi.Name(""))

		if err != nil {
			t.Fatalf("unexpected error with empty key: %v", err)
		}

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Should be able to resolve with empty string
		svc, err := provider.ResolveKeyed(reflect.TypeOf((*providerTestService)(nil)), "")
		if err != nil {
			t.Errorf("should resolve with empty string key: %v", err)
		}

		if svc.(*providerTestService).ID != "empty-key" {
			t.Error("resolved wrong service")
		}
	})
}

func TestServiceProvider_ResolveGenericTypes(t *testing.T) {
	t.Run("resolves pointer type directly", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(func() *providerTestService {
			return &providerTestService{ID: "test-pointer"}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Resolve using pointer type directly (was causing **Type issue)
		service, err := godi.Resolve[*providerTestService](provider)
		if err != nil {
			t.Fatalf("unexpected error resolving *providerTestService: %v", err)
		}

		if service == nil {
			t.Fatal("expected non-nil service")
		}

		if service.ID != "test-pointer" {
			t.Errorf("expected ID 'test-pointer', got %s", service.ID)
		}
	})

	t.Run("resolves non-pointer type by dereferencing", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(func() *providerTestService {
			return &providerTestService{ID: "test-value"}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Resolve using value type (should find *Type and return Type)
		service, err := godi.Resolve[providerTestService](provider)
		if err != nil {
			t.Fatalf("unexpected error resolving providerTestService: %v", err)
		}

		if service.ID != "test-value" {
			t.Errorf("expected ID 'test-value', got %s", service.ID)
		}
	})

	t.Run("resolves interface type", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(newProviderTestLogger)

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Resolve using interface type
		logger, err := godi.Resolve[providerTestLogger](provider)
		if err != nil {
			t.Fatalf("unexpected error resolving interface: %v", err)
		}

		if logger == nil {
			t.Fatal("expected non-nil logger")
		}

		logger.Log("interface test")
		logs := logger.GetLogs()
		if len(logs) != 1 || logs[0] != "interface test" {
			t.Error("logger not working correctly through interface")
		}
	})

	t.Run("resolves keyed pointer type", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(func() *providerTestService {
			return &providerTestService{ID: "keyed-pointer"}
		}, godi.Name("test-key"))

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Resolve keyed service using pointer type
		service, err := godi.ResolveKeyed[*providerTestService](provider, "test-key")
		if err != nil {
			t.Fatalf("unexpected error resolving keyed *providerTestService: %v", err)
		}

		if service == nil {
			t.Fatal("expected non-nil service")
		}

		if service.ID != "keyed-pointer" {
			t.Errorf("expected ID 'keyed-pointer', got %s", service.ID)
		}
	})

	t.Run("resolves keyed interface type", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		collection.AddSingleton(func() providerTestDatabase {
			return &providerTestDatabaseImpl{name: "keyed-interface"}
		}, godi.Name("db-key"))

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Resolve keyed service using interface type
		db, err := godi.ResolveKeyed[providerTestDatabase](provider, "db-key")
		if err != nil {
			t.Fatalf("unexpected error resolving keyed interface: %v", err)
		}

		if db == nil {
			t.Fatal("expected non-nil database")
		}

		result := db.Query("SELECT 1")
		if !strings.Contains(result, "keyed-interface") {
			t.Errorf("expected 'keyed-interface' in result, got %s", result)
		}
	})

	t.Run("error when service not found", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Try to resolve non-existent service
		_, err = godi.Resolve[*providerTestService](provider)
		if err == nil {
			t.Error("expected error for non-existent service")
		}

		// Verify error message contains type information
		if !strings.Contains(err.Error(), "providerTestService") {
			t.Errorf("expected error to mention providerTestService, got: %v", err)
		}
	})

	t.Run("type mismatch error", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		// Register as one interface but try to resolve as another
		collection.AddSingleton(func() providerTestLogger {
			return &providerTestLoggerImpl{}
		})

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// This should fail with type mismatch
		_, err = godi.Resolve[providerTestDatabase](provider)
		if err == nil {
			t.Error("expected type mismatch error")
		}
	})
}

func TestServiceProvider_ResolveGroup(t *testing.T) {
	t.Run("resolves group services", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Add multiple handlers to a group
		collection.AddSingleton(func() Handler {
			return &testHandler{name: "handler1"}
		}, godi.Group("handlers"))

		collection.AddSingleton(func() Handler {
			return &testHandler{name: "handler2"}
		}, godi.Group("handlers"))

		collection.AddSingleton(func() Handler {
			return &testHandler{name: "handler3"}
		}, godi.Group("handlers"))

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Resolve the group
		handlers, err := provider.ResolveGroup(reflect.TypeOf((*Handler)(nil)).Elem(), "handlers")
		if err != nil {
			t.Fatalf("unexpected error resolving group: %v", err)
		}

		if len(handlers) != 3 {
			t.Errorf("expected 3 handlers, got %d", len(handlers))
		}

		// Verify we got the correct handlers
		handlerNames := make(map[string]bool)
		for _, h := range handlers {
			handler, ok := h.(Handler)
			if !ok {
				t.Errorf("expected Handler type, got %T", h)
				continue
			}
			// Assuming testHandler has a Name() method or field
			if th, ok := handler.(*testHandler); ok {
				handlerNames[th.name] = true
			}
		}

		expectedNames := []string{"handler1", "handler2", "handler3"}
		for _, name := range expectedNames {
			if !handlerNames[name] {
				t.Errorf("missing handler: %s", name)
			}
		}
	})

	t.Run("resolves empty group", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Try to resolve a non-existent group
		handlers, err := provider.ResolveGroup(reflect.TypeOf((*Handler)(nil)).Elem(), "nonexistent")
		if err == nil {
			t.Error("expected error for non-existent group")
		}

		if handlers != nil {
			t.Error("expected nil handlers for non-existent group")
		}
	})

	t.Run("resolves with generic helper", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Add handlers with specific types
		collection.AddSingleton(func() Handler {
			return &testHandler{name: "generic1"}
		}, godi.Group("handlers"), godi.As(new(Handler)))

		collection.AddSingleton(func() Handler {
			return &testHandler{name: "generic2"}
		}, godi.Group("handlers"), godi.As(new(Handler)))

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// Use generic helper
		handlers, err := godi.ResolveGroup[Handler](provider, "handlers")
		if err != nil {
			t.Fatalf("unexpected error with generic helper: %v", err)
		}

		if len(handlers) != 2 {
			t.Errorf("expected 2 handlers, got %d", len(handlers))
		}

		// Verify types are correct
		for i, handler := range handlers {
			if handler == nil {
				t.Errorf("handler %d is nil", i)
			}
		}
	})

	t.Run("returns error for nil type", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		_, err = provider.ResolveGroup(nil, "somegroup")
		if err == nil {
			t.Error("expected error for nil type")
		}
		if !errors.Is(err, godi.ErrInvalidServiceType) {
			t.Errorf("expected ErrInvalidServiceType, got %v", err)
		}
	})

	t.Run("returns error for empty group name", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		_, err = provider.ResolveGroup(reflect.TypeOf((*Handler)(nil)).Elem(), "")
		if err == nil {
			t.Error("expected error for empty group name")
		}
		if !strings.Contains(err.Error(), "group name cannot be empty") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("returns error when disposed", func(t *testing.T) {
		collection := godi.NewServiceCollection()
		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		provider.Close()

		_, err = provider.ResolveGroup(reflect.TypeOf((*Handler)(nil)).Elem(), "handlers")
		if err == nil {
			t.Error("expected error when resolving from disposed provider")
		}
		if !errors.Is(err, godi.ErrProviderDisposed) {
			t.Errorf("expected ErrProviderDisposed, got %v", err)
		}
	})

	t.Run("returns error for nil provider in generic helper", func(t *testing.T) {
		_, err := godi.ResolveGroup[Handler](nil, "handlers")
		if err == nil {
			t.Error("expected error for nil provider")
		}
		if !errors.Is(err, godi.ErrNilServiceProvider) {
			t.Errorf("expected ErrNilServiceProvider, got %v", err)
		}
	})

	t.Run("resolves scoped group services", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Add scoped services to a group
		for i := 0; i < 2; i++ {
			idx := i
			collection.AddScoped(func() Handler {
				return &testHandler{name: fmt.Sprintf("scoped-%d", idx)}
			}, godi.Group("scoped-handlers"))
		}

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope1 := provider.CreateScope(context.Background())
		defer scope1.Close()

		scope2 := provider.CreateScope(context.Background())
		defer scope2.Close()

		scope3 := provider.CreateScope(context.Background())
		defer scope3.Close()

		scope4 := provider.CreateScope(context.Background())
		defer scope4.Close()

		// Resolve in scope1
		handlers1, err := scope1.ResolveGroup(reflect.TypeOf((*Handler)(nil)).Elem(), "scoped-handlers")
		if err != nil {
			t.Fatalf("unexpected error in scope1: %v", err)
		}

		if len(handlers1) != 2 {
			t.Errorf("expected 2 scoped handlers, got %d", len(handlers1))
		}

		// Resolve again in scope1 - should be same instances
		handlers1Again, err := scope1.ResolveGroup(reflect.TypeOf((*Handler)(nil)).Elem(), "scoped-handlers")
		if err != nil {
			t.Fatalf("unexpected error in scope1 again: %v", err)
		}

		if len(handlers1Again) != 2 {
			t.Errorf("expected 2 scoped handlers again, got %d", len(handlers1Again))
		}

		// Order by name for consistent comparison
		sort.Slice(handlers1, func(i, j int) bool {
			return handlers1[i].(*testHandler).name < handlers1[j].(*testHandler).name
		})
		sort.Slice(handlers1Again, func(i, j int) bool {
			return handlers1Again[i].(*testHandler).name < handlers1Again[j].(*testHandler).name
		})

		// Verify same instances within scope
		for i := range handlers1 {
			if handlers1[i] != handlers1Again[i] {
				t.Errorf("scoped service %d should return same instance within scope", i)
			}
		}

		// Resolve in scope2 - should be different instances
		handlers2, err := scope2.ResolveGroup(reflect.TypeOf((*Handler)(nil)).Elem(), "scoped-handlers")
		if err != nil {
			t.Fatalf("unexpected error in scope2: %v", err)
		}

		if len(handlers2) != 2 {
			t.Errorf("expected 2 scoped handlers in scope2, got %d", len(handlers2))
		}

		sort.Slice(handlers2, func(i, j int) bool {
			return handlers2[i].(*testHandler).name < handlers2[j].(*testHandler).name
		})

		// Verify different instances across scopes
		for i := range handlers1 {
			if handlers1[i] == handlers2[i] {
				t.Errorf("scoped service %d should create different instances across scopes", i)
			}
		}
	})

	t.Run("transient services cannot be in groups", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Singleton and scoped are allowed in groups
		err := collection.AddSingleton(func() Handler {
			return &testHandler{name: "singleton"}
		}, godi.Group("mixed"))
		if err != nil {
			t.Fatalf("unexpected error adding singleton to group: %v", err)
		}

		err = collection.AddScoped(func() Handler {
			return &testHandler{name: "scoped"}
		}, godi.Group("mixed"))
		if err != nil {
			t.Fatalf("unexpected error adding scoped to group: %v", err)
		}

		// Transient should fail
		err = collection.AddTransient(func() Handler {
			return &testHandler{name: "transient"}
		}, godi.Group("mixed"))
		if err == nil {
			t.Fatal("expected error when adding transient service to group")
		}

		if !errors.Is(err, godi.ErrTransientInGroup) {
			t.Errorf("expected ErrTransientInGroup, got %v", err)
		}

		if !strings.Contains(err.Error(), "transient services cannot be registered in groups") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("resolves mixed singleton and scoped group", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Mix of singleton and scoped in same group
		collection.AddSingleton(func() Handler {
			return &testHandler{name: "singleton"}
		}, godi.Group("mixed"))

		collection.AddScoped(func() Handler {
			return &testHandler{name: "scoped"}
		}, godi.Group("mixed"))

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		handlers, err := scope.ResolveGroup(reflect.TypeOf((*Handler)(nil)).Elem(), "mixed")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(handlers) != 2 {
			t.Errorf("expected 2 handlers, got %d", len(handlers))
		}

		// Verify we got both handlers
		handlerNames := make(map[string]bool)
		for _, h := range handlers {
			if th, ok := h.(*testHandler); ok {
				handlerNames[th.name] = true
			}
		}

		if !handlerNames["singleton"] {
			t.Error("missing singleton handler")
		}
		if !handlerNames["scoped"] {
			t.Error("missing scoped handler")
		}
	})

	t.Run("resolves scoped group with pointer type", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Register a scoped service returning the interface type directly
		collection.AddScoped(func() Handler {
			return &testHandler{name: "scoped-pointer"}
		}, godi.Group("scoped-handlers"))

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		scope := provider.CreateScope(context.Background())
		defer scope.Close()

		// Resolve the group with interface type
		handlers, err := scope.ResolveGroup(reflect.TypeOf((*Handler)(nil)).Elem(), "scoped-handlers")
		if err != nil {
			t.Fatalf("unexpected error resolving scoped group: %v", err)
		}

		if len(handlers) != 1 {
			t.Errorf("expected 1 handler, got %d", len(handlers))
		}
		if handler, ok := handlers[0].(*testHandler); ok {
			if handler.name != "scoped-pointer" {
				t.Errorf("expected handler name 'scoped-pointer', got '%s'", handler.name)
			}
		} else {
			t.Error("expected handler to be of type *testHandler")
		}
	})

	t.Run("type conversion in generic helper", func(t *testing.T) {
		collection := godi.NewServiceCollection()

		// Register returning the interface type directly
		collection.AddSingleton(func() Handler {
			return &testHandler{name: "ptr-to-value"}
		}, godi.Group("handlers"))

		provider, err := collection.BuildServiceProvider()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		defer provider.Close()

		// This should work without As() since we're returning the interface type
		handlers, err := godi.ResolveGroup[Handler](provider, "handlers")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(handlers) != 1 {
			t.Errorf("expected 1 handler, got %d", len(handlers))
		}
	})
}

// Benchmark for group resolution
func BenchmarkServiceProvider_ResolveGroup(b *testing.B) {
	collection := godi.NewServiceCollection()

	// Add 10 services to a group
	for i := 0; i < 10; i++ {
		idx := i
		collection.AddSingleton(func() Handler {
			return &testHandler{name: fmt.Sprintf("handler-%d", idx)}
		}, godi.Group("bench-handlers"))
	}

	provider, err := collection.BuildServiceProvider()
	if err != nil {
		b.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	handlerType := reflect.TypeOf((*Handler)(nil)).Elem()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := provider.ResolveGroup(handlerType, "bench-handlers")
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkServiceProvider_ResolveGroupGeneric(b *testing.B) {
	collection := godi.NewServiceCollection()

	// Add 10 services to a group
	for i := 0; i < 10; i++ {
		idx := i
		collection.AddSingleton(func() Handler {
			return &testHandler{name: fmt.Sprintf("handler-%d", idx)}
		}, godi.Group("bench-handlers"))
	}

	provider, err := collection.BuildServiceProvider()
	if err != nil {
		b.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := godi.ResolveGroup[Handler](provider, "bench-handlers")
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark for scope creation and disposal
func BenchmarkServiceProvider_ScopeLifecycle(b *testing.B) {
	collection := godi.NewServiceCollection()
	collection.AddScoped(newProviderTestScopedService)
	collection.AddScoped(newProviderTestService)
	collection.AddSingleton(newProviderTestLogger)
	collection.AddSingleton(newProviderTestDatabase)
	collection.AddSingleton(newProviderTestCache)

	provider, err := collection.BuildServiceProvider()
	if err != nil {
		b.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scope := provider.CreateScope(ctx)
		// Resolve some services
		scope.Resolve(reflect.TypeOf((*providerTestScopedService)(nil)))
		scope.Resolve(reflect.TypeOf((*providerTestService)(nil)))
		scope.Close()
	}
}

// Benchmark for deeply nested dependencies
func BenchmarkServiceProvider_DeepDependencies(b *testing.B) {
	// Create a chain of 10 dependencies
	type service0 struct{ Value int }
	type service1 struct{ S0 *service0 }
	type service2 struct{ S1 *service1 }
	type service3 struct{ S2 *service2 }
	type service4 struct{ S3 *service3 }
	type service5 struct{ S4 *service4 }
	type service6 struct{ S5 *service5 }
	type service7 struct{ S6 *service6 }
	type service8 struct{ S7 *service7 }
	type service9 struct{ S8 *service8 }

	collection := godi.NewServiceCollection()
	collection.AddSingleton(func() *service0 { return &service0{Value: 42} })
	collection.AddSingleton(func(s *service0) *service1 { return &service1{S0: s} })
	collection.AddSingleton(func(s *service1) *service2 { return &service2{S1: s} })
	collection.AddSingleton(func(s *service2) *service3 { return &service3{S2: s} })
	collection.AddSingleton(func(s *service3) *service4 { return &service4{S3: s} })
	collection.AddSingleton(func(s *service4) *service5 { return &service5{S4: s} })
	collection.AddSingleton(func(s *service5) *service6 { return &service6{S5: s} })
	collection.AddSingleton(func(s *service6) *service7 { return &service7{S6: s} })
	collection.AddSingleton(func(s *service7) *service8 { return &service8{S7: s} })
	collection.AddSingleton(func(s *service8) *service9 { return &service9{S8: s} })

	provider, err := collection.BuildServiceProvider()
	if err != nil {
		b.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	service9Type := reflect.TypeOf((*service9)(nil))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := provider.Resolve(service9Type)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Benchmark tests
func BenchmarkServiceProvider_ResolveSingleton(b *testing.B) {
	collection := godi.NewServiceCollection()
	collection.AddSingleton(newProviderTestLogger)

	provider, err := collection.BuildServiceProvider()
	if err != nil {
		b.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	loggerType := reflect.TypeOf((*providerTestLogger)(nil)).Elem()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := provider.Resolve(loggerType)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkServiceProvider_ResolveTransient(b *testing.B) {
	collection := godi.NewServiceCollection()
	collection.AddTransient(newProviderTestLogger)

	provider, err := collection.BuildServiceProvider()
	if err != nil {
		b.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	// Need to use a scope for transient services
	scope := provider.CreateScope(context.Background())
	defer scope.Close()

	loggerType := reflect.TypeOf((*providerTestLogger)(nil)).Elem()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := scope.Resolve(loggerType)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkServiceProvider_CreateScope(b *testing.B) {
	collection := godi.NewServiceCollection()
	collection.AddScoped(newProviderTestScopedService)

	provider, err := collection.BuildServiceProvider()
	if err != nil {
		b.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scope := provider.CreateScope(ctx)
		scope.Close()
	}
}

func BenchmarkServiceProvider_ConcurrentResolve(b *testing.B) {
	collection := godi.NewServiceCollection()
	collection.AddSingleton(newProviderTestLogger)
	collection.AddSingleton(newProviderTestDatabase)
	collection.AddSingleton(newProviderTestCache)
	collection.AddSingleton(newProviderTestService)

	provider, err := collection.BuildServiceProvider()
	if err != nil {
		b.Fatalf("unexpected error: %v", err)
	}
	defer provider.Close()

	serviceType := reflect.TypeOf((*providerTestService)(nil))

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_, err := provider.Resolve(serviceType)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}
