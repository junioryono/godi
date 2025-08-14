package container_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/junioryono/godi/v3/internal/container"
	"github.com/junioryono/godi/v3/internal/registry"
)

// Test types
type Database struct {
	ConnectionString string
	Disposed         bool
}

func (d *Database) Dispose() error {
	d.Disposed = true
	return nil
}

type Logger interface {
	Log(message string)
}

type ConsoleLogger struct {
	Messages []string
}

func (c *ConsoleLogger) Log(message string) {
	c.Messages = append(c.Messages, message)
}

type UserService struct {
	DB     *Database
	Logger Logger
}

// Test constructors
func NewDatabase(connStr string) *Database {
	return &Database{ConnectionString: connStr}
}

func NewDefaultDatabase() *Database {
	return &Database{ConnectionString: "default-db"}
}

func NewConsoleLogger() Logger {
	return &ConsoleLogger{Messages: make([]string, 0)}
}

func NewUserService(db *Database, logger Logger) *UserService {
	return &UserService{DB: db, Logger: logger}
}

func NewUserServiceWithError(db *Database) (*UserService, error) {
	if db == nil {
		return nil, errors.New("database required")
	}
	return &UserService{DB: db}, nil
}

func TestContainer_BasicRegistration(t *testing.T) {
	c := container.New()
	defer c.Dispose()

	// Register services
	err := c.RegisterSingleton(NewDefaultDatabase)
	if err != nil {
		t.Fatalf("Failed to register Database: %v", err)
	}

	err = c.RegisterSingleton(NewConsoleLogger)
	if err != nil {
		t.Fatalf("Failed to register Logger: %v", err)
	}

	err = c.RegisterSingleton(NewUserService)
	if err != nil {
		t.Fatalf("Failed to register UserService: %v", err)
	}

	// Build container
	err = c.Build()
	if err != nil {
		t.Fatalf("Failed to build container: %v", err)
	}

	// Resolve UserService
	serviceType := reflect.TypeOf((*UserService)(nil))
	instance, err := c.Resolve(serviceType)
	if err != nil {
		t.Fatalf("Failed to resolve UserService: %v", err)
	}

	userService, ok := instance.(*UserService)
	if !ok {
		t.Fatalf("Expected *UserService, got %T", instance)
	}

	if userService.DB == nil {
		t.Error("Expected DB to be injected")
	}

	if userService.Logger == nil {
		t.Error("Expected Logger to be injected")
	}
}

func TestContainer_GenericHelpers(t *testing.T) {
	c := container.New()
	defer c.Dispose()

	// Register services
	c.RegisterSingleton(NewDefaultDatabase)
	c.RegisterSingleton(NewConsoleLogger)
	c.RegisterSingleton(NewUserService)

	c.Build()

	// Use generic helper
	userService, err := container.Resolve[*UserService](c)
	if err != nil {
		t.Fatalf("Failed to resolve UserService: %v", err)
	}

	if userService.DB == nil {
		t.Error("Expected DB to be injected")
	}

	// Test MustResolve
	db := container.MustResolve[*Database](c)
	if db.ConnectionString != "default-db" {
		t.Errorf("Expected 'default-db', got %s", db.ConnectionString)
	}
}

func TestContainer_Lifetimes(t *testing.T) {
	c := container.New()
	defer c.Dispose()

	// Register with different lifetimes
	c.RegisterSingleton(NewDefaultDatabase)
	c.RegisterScoped(NewConsoleLogger)
	c.RegisterTransient(func() *UserService { return &UserService{} })

	c.Build()

	// Test singleton
	db1 := container.MustResolve[*Database](c)
	db2 := container.MustResolve[*Database](c)

	if db1 != db2 {
		t.Error("Expected same singleton instance")
	}

	// Test scoped
	scope1, _ := c.CreateScope(context.Background())
	defer scope1.Dispose()

	logger1a := container.MustResolve[Logger](scope1)
	logger1b := container.MustResolve[Logger](scope1)

	if logger1a != logger1b {
		t.Error("Expected same instance within scope")
	}

	scope2, _ := c.CreateScope(context.Background())
	defer scope2.Dispose()

	logger2 := container.MustResolve[Logger](scope2)

	if logger1a == logger2 {
		t.Error("Expected different instances in different scopes")
	}

	// Test transient
	svc1 := container.MustResolve[*UserService](c)
	svc2 := container.MustResolve[*UserService](c)

	if svc1 == svc2 {
		t.Error("Expected different transient instances")
	}
}

func TestContainer_KeyedServices(t *testing.T) {
	c := container.New()
	defer c.Dispose()

	// Register keyed databases
	c.RegisterSingleton(
		func() *Database { return &Database{ConnectionString: "primary"} },
		container.WithName("primary"),
	)

	c.RegisterSingleton(
		func() *Database { return &Database{ConnectionString: "secondary"} },
		container.WithName("secondary"),
	)

	c.Build()

	// Resolve keyed services
	primary, err := container.ResolveKeyed[*Database](c, "primary")
	if err != nil {
		t.Fatalf("Failed to resolve primary database: %v", err)
	}

	if primary.ConnectionString != "primary" {
		t.Errorf("Expected 'primary', got %s", primary.ConnectionString)
	}

	secondary, err := container.ResolveKeyed[*Database](c, "secondary")
	if err != nil {
		t.Fatalf("Failed to resolve secondary database: %v", err)
	}

	if secondary.ConnectionString != "secondary" {
		t.Errorf("Expected 'secondary', got %s", secondary.ConnectionString)
	}
}

func TestContainer_Groups(t *testing.T) {
	c := container.New()
	defer c.Dispose()

	// Register multiple handlers in a group
	c.RegisterSingleton(
		func() func() { return func() {} },
		container.InGroup("middleware"),
	)

	c.RegisterSingleton(
		func() func() { return func() {} },
		container.InGroup("middleware"),
	)

	c.RegisterSingleton(
		func() func() { return func() {} },
		container.InGroup("middleware"),
	)

	c.Build()

	// Resolve group
	handlers, err := container.ResolveGroup[func()](c, "middleware")
	if err != nil {
		t.Fatalf("Failed to resolve middleware group: %v", err)
	}

	if len(handlers) != 3 {
		t.Errorf("Expected 3 handlers, got %d", len(handlers))
	}
}

func TestContainer_Decorator(t *testing.T) {
	c := container.New()
	defer c.Dispose()

	// Register base logger
	c.RegisterSingleton(func() Logger {
		return &ConsoleLogger{Messages: make([]string, 0)}
	})

	// Register decorator that adds prefix
	c.RegisterDecorator(func(logger Logger) Logger {
		return &prefixLogger{
			prefix: "[INFO] ",
			inner:  logger,
		}
	})

	c.Build()

	// Resolve decorated logger
	logger := container.MustResolve[Logger](c)

	// Check if decorator was applied
	if _, ok := logger.(*prefixLogger); !ok {
		t.Error("Expected logger to be decorated")
	}
}

type prefixLogger struct {
	prefix string
	inner  Logger
}

func (p *prefixLogger) Log(message string) {
	p.inner.Log(p.prefix + message)
}

func TestContainer_Invoke(t *testing.T) {
	c := container.New()
	defer c.Dispose()

	c.RegisterSingleton(NewDefaultDatabase)
	c.RegisterSingleton(NewConsoleLogger)

	c.Build()

	var capturedDB *Database
	var capturedLogger Logger

	// Invoke function with dependencies
	err := c.Invoke(func(db *Database, logger Logger) {
		capturedDB = db
		capturedLogger = logger
	})

	if err != nil {
		t.Fatalf("Failed to invoke function: %v", err)
	}

	if capturedDB == nil {
		t.Error("Expected DB to be injected")
	}

	if capturedLogger == nil {
		t.Error("Expected Logger to be injected")
	}

	// Test invoke with error return
	err = c.Invoke(func(db *Database) error {
		if db == nil {
			return errors.New("no database")
		}
		return nil
	})

	if err != nil {
		t.Errorf("Invoke should succeed: %v", err)
	}
}

func TestContainer_Disposal(t *testing.T) {
	c := container.New()

	// Register disposable service
	c.RegisterSingleton(NewDefaultDatabase)
	c.Build()

	// Resolve to create instance
	db := container.MustResolve[*Database](c)

	// Dispose container
	err := c.Dispose()
	if err != nil {
		t.Fatalf("Failed to dispose container: %v", err)
	}

	// Check that service was disposed
	if !db.Disposed {
		t.Error("Expected database to be disposed")
	}

	// Further operations should fail
	_, err = c.Resolve(reflect.TypeOf((*Database)(nil)))
	if err == nil {
		t.Error("Expected error after disposal")
	}
}

func TestContainer_ScopeDisposal(t *testing.T) {
	c := container.New()
	defer c.Dispose()

	// Register scoped disposable service
	c.RegisterScoped(NewDefaultDatabase)
	c.Build()

	// Create scope and resolve
	scope, _ := c.CreateScope(context.Background())
	db := container.MustResolve[*Database](scope)

	// Dispose scope
	err := scope.Dispose()
	if err != nil {
		t.Fatalf("Failed to dispose scope: %v", err)
	}

	// Check that service was disposed
	if !db.Disposed {
		t.Error("Expected database to be disposed with scope")
	}
}

func TestContainer_Builder(t *testing.T) {
	// Use builder pattern
	c, err := container.NewBuilder().
		RegisterSingleton(NewDefaultDatabase).
		RegisterSingleton(NewConsoleLogger).
		RegisterSingleton(NewUserService).
		Build()

	if err != nil {
		t.Fatalf("Failed to build container: %v", err)
	}
	defer c.Dispose()

	// Resolve service
	userService := container.MustResolve[*UserService](c)

	if userService.DB == nil || userService.Logger == nil {
		t.Error("Expected dependencies to be injected")
	}
}

func TestContainer_Module(t *testing.T) {
	// Create a module
	databaseModule := container.CreateModule("database", func(c *container.Container) error {
		return c.RegisterSingleton(NewDefaultDatabase)
	})

	loggingModule := container.CreateModule("logging", func(c *container.Container) error {
		return c.RegisterSingleton(NewConsoleLogger)
	})

	// Use modules with builder
	c, err := container.NewBuilder().
		RegisterModule(databaseModule).
		RegisterModule(loggingModule).
		RegisterSingleton(NewUserService).
		Build()

	if err != nil {
		t.Fatalf("Failed to build container with modules: %v", err)
	}
	defer c.Dispose()

	// Resolve service
	userService := container.MustResolve[*UserService](c)

	if userService.DB == nil || userService.Logger == nil {
		t.Error("Expected dependencies from modules to be injected")
	}
}

func TestContainer_ConcurrentResolution(t *testing.T) {
	c := container.New()
	defer c.Dispose()

	c.RegisterSingleton(NewDefaultDatabase)
	c.RegisterSingleton(NewConsoleLogger)
	c.RegisterScoped(NewUserService)

	c.Build()

	// Create multiple scopes and resolve concurrently
	var wg sync.WaitGroup
	errorChan := make(chan error, 100)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			scope, err := c.CreateScope(context.Background())
			if err != nil {
				errorChan <- err
				return
			}
			defer scope.Dispose()

			// Resolve multiple times
			for j := 0; j < 10; j++ {
				_, err := container.Resolve[*UserService](scope)
				if err != nil {
					errorChan <- err
					return
				}
			}
		}()
	}

	wg.Wait()
	close(errorChan)

	// Check for errors
	for err := range errorChan {
		t.Errorf("Concurrent resolution error: %v", err)
	}
}

// Register services that would create a circular dependency
type A struct{ B *B }
type B struct{ A *A }

func TestContainer_CircularDependency(t *testing.T) {
	c := container.New()
	defer c.Dispose()

	c.RegisterSingleton(func(b *B) *A { return &A{B: b} })
	c.RegisterSingleton(func(a *A) *B { return &B{A: a} })

	// Build should detect the cycle
	err := c.Build()
	if err == nil {
		t.Error("Expected circular dependency error")
	}

	if !container.IsCircularDependencyError(err) {
		t.Errorf("Expected circular dependency error, got: %v", err)
	}
}

func TestContainer_MissingDependency(t *testing.T) {
	c := container.New()
	defer c.Dispose()

	// Register service with missing dependency
	c.RegisterSingleton(NewUserService)

	c.Build()

	// Resolution should fail
	_, err := container.Resolve[*UserService](c)
	if err == nil {
		t.Error("Expected missing dependency error")
	}
}

func TestContainer_Statistics(t *testing.T) {
	c := container.New()
	defer c.Dispose()

	c.RegisterSingleton(NewDefaultDatabase)
	c.RegisterSingleton(NewConsoleLogger)
	c.RegisterSingleton(NewUserService)

	c.Build()

	// Resolve services
	container.MustResolve[*Database](c)
	container.MustResolve[Logger](c)
	container.MustResolve[*UserService](c)

	// Check statistics
	stats := c.GetStatistics()

	if stats.RegisteredServices != 3 {
		t.Errorf("Expected 3 registered services, got %d", stats.RegisteredServices)
	}

	if stats.ResolvedInstances < 3 {
		t.Errorf("Expected at least 3 resolved instances, got %d", stats.ResolvedInstances)
	}

	if stats.FailedResolutions != 0 {
		t.Errorf("Expected 0 failed resolutions, got %d", stats.FailedResolutions)
	}
}

func TestContainer_Options(t *testing.T) {
	resolvedCount := 0
	errorCount := 0

	options := &container.ContainerOptions{
		EnableValidation: true,
		EnableCaching:    true,
		OnServiceResolved: func(serviceType reflect.Type, instance any, duration time.Duration) {
			resolvedCount++
		},
		OnServiceError: func(serviceType reflect.Type, err error) {
			errorCount++
		},
	}

	c := container.NewWithOptions(options)
	defer c.Dispose()

	c.RegisterSingleton(NewDefaultDatabase)
	c.Build()

	// Successful resolution
	container.MustResolve[*Database](c)

	if resolvedCount != 1 {
		t.Errorf("Expected OnServiceResolved to be called once, got %d", resolvedCount)
	}

	// Failed resolution
	type UnregisteredService struct{}
	_, err := container.Resolve[*UnregisteredService](c)
	if err == nil {
		t.Error("Expected resolution to fail")
	}

	if errorCount != 1 {
		t.Errorf("Expected OnServiceError to be called once, got %d", errorCount)
	}
}

func TestContainer_ServiceDescriptors(t *testing.T) {
	c := container.New()
	defer c.Dispose()

	// Register using descriptors
	descriptors := []container.ServiceDescriptor{
		{
			ServiceType: reflect.TypeOf((*Database)(nil)),
			Lifetime:    registry.Singleton,
			Constructor: NewDefaultDatabase,
		},
		{
			ServiceType: reflect.TypeOf((*Logger)(nil)).Elem(),
			Lifetime:    registry.Singleton,
			Constructor: NewConsoleLogger,
		},
		{
			ServiceType: reflect.TypeOf((*UserService)(nil)),
			Lifetime:    registry.Scoped,
			Constructor: NewUserService,
		},
	}

	err := container.RegisterServices(c, descriptors...)
	if err != nil {
		t.Fatalf("Failed to register services: %v", err)
	}

	c.Build()

	// Create scope for scoped service
	scope, _ := c.CreateScope(context.Background())
	defer scope.Dispose()

	// Resolve all services
	db := container.MustResolve[*Database](scope)
	logger := container.MustResolve[Logger](scope)
	userService := container.MustResolve[*UserService](scope)

	if db == nil || logger == nil || userService == nil {
		t.Error("Failed to resolve services from descriptors")
	}
}

func TestContainer_IsRegistered(t *testing.T) {
	c := container.New()
	defer c.Dispose()

	// Check before registration
	if container.IsRegistered[*Database](c) {
		t.Error("Database should not be registered yet")
	}

	// Register and check
	c.RegisterSingleton(NewDefaultDatabase)

	if !container.IsRegistered[*Database](c) {
		t.Error("Database should be registered")
	}

	// Check keyed service
	c.RegisterSingleton(
		func() *Database { return &Database{ConnectionString: "keyed"} },
		container.WithName("special"),
	)

	if !container.IsKeyedRegistered[*Database](c, "special") {
		t.Error("Keyed database should be registered")
	}

	if container.IsKeyedRegistered[*Database](c, "nonexistent") {
		t.Error("Non-existent keyed database should not be registered")
	}
}

func TestContainer_ErrorTypes(t *testing.T) {
	c := container.New()
	defer c.Dispose()

	// Test container disposed error
	c.Dispose()

	err := c.RegisterSingleton(NewDefaultDatabase)
	if !errors.Is(err, container.ErrContainerDisposed) {
		t.Errorf("Expected ErrContainerDisposed, got: %v", err)
	}

	// Test scope disposed error
	c2 := container.New()
	defer c2.Dispose()

	c2.RegisterSingleton(NewDefaultDatabase)
	c2.Build()

	scope, _ := c2.CreateScope(context.Background())
	scope.Dispose()

	_, err = container.Resolve[*Database](scope)
	if !errors.Is(err, container.ErrScopeDisposed) {
		t.Errorf("Expected ErrScopeDisposed, got: %v", err)
	}
}
