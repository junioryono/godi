// Package godi provides a powerful dependency injection container for Go applications.
// It offers familiar patterns for developers coming from .NET and other DI frameworks,
// while maintaining idiomatic Go code and zero magic through compile-time safety.
//
// # Overview
//
// godi makes dependency injection in Go simple and familiar. The library provides:
//   - Three service lifetimes: Singleton, Scoped, and Transient
//   - Automatic dependency resolution and injection
//   - Support for interfaces and concrete types
//   - Constructor injection with automatic wiring
//   - Decorator pattern support
//   - Module system for organizing services
//   - Thread-safe operations
//   - Zero struct tags or code generation required
//
// # Basic Usage
//
// Create a service collection, register your services, build a provider, and resolve:
//
//	services := godi.NewCollection()
//	services.AddSingleton(NewLogger)
//	services.AddScoped(NewUserService)
//
//	provider, err := services.Build()
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer provider.Close()
//
//	userService, err := godi.Resolve[*UserService](provider)
//
// # Service Lifetimes
//
// godi supports three service lifetimes:
//
//   - Singleton: One instance created and shared across the entire application
//   - Scoped: One instance per scope (useful for per-request isolation in web apps)
//   - Transient: New instance created every time the service is requested
//
// # Dependency Injection
//
// Services can declare dependencies through their constructor parameters:
//
//	func NewUserService(db *Database, logger Logger) *UserService {
//	    return &UserService{
//	        db:     db,
//	        logger: logger,
//	    }
//	}
//
// godi automatically resolves and injects these dependencies when creating the service.
//
// # Parameter Objects (In)
//
// For constructors with many dependencies, use parameter objects with embedded godi.In:
//
//	type ServiceParams struct {
//	    godi.In
//
//	    Database *sql.DB
//	    Logger   Logger         `optional:"true"`
//	    Cache    Cache          `name:"redis"`
//	    Handlers []http.Handler `group:"routes"`
//	}
//
//	func NewService(params ServiceParams) *Service {
//	    // Use params.Database, params.Logger, etc.
//	}
//
// # Result Objects (Out)
//
// Constructors can return multiple services using result objects with embedded godi.Out:
//
//	type ServiceResult struct {
//	    godi.Out
//
//	    UserService  *UserService
//	    AdminService *AdminService `name:"admin"`
//	    Handler      http.Handler  `group:"routes"`
//	}
//
//	func NewServices(db *sql.DB) ServiceResult {
//	    // Create and return multiple services
//	}
//
// # Keyed Services
//
// Register multiple implementations of the same interface using keys:
//
//	services.AddSingleton(NewRedisCache, godi.Name("redis"))
//	services.AddSingleton(NewMemoryCache, godi.Name("memory"))
//
//	// Resolve specific implementation
//	cache, err := godi.ResolveKeyed[Cache](provider, "redis")
//
// # Service Groups
//
// Group related services together:
//
//	services.AddScoped(NewUserHandler, godi.Group("handlers"))
//	services.AddScoped(NewAdminHandler, godi.Group("handlers"))
//
//	// Resolve all handlers
//	handlers, err := godi.ResolveGroup[http.Handler](provider, "handlers")
//
// # Modules
//
// Organize service registrations into reusable modules:
//
//	var DatabaseModule = godi.NewModule("database",
//	    godi.AddSingleton(NewDatabaseConnection),
//	    godi.AddScoped(NewUserRepository),
//	)
//
//	services.AddModules(DatabaseModule)
//
// # Decorators
//
// Wrap services with additional behavior:
//
//	func LoggingDecorator(inner Service, logger Logger) Service {
//	    return &loggingService{inner: inner, logger: logger}
//	}
//
//	services.AddScoped(NewService)
//	services.Decorate(LoggingDecorator)
//
// # Scopes
//
// Create isolated scopes for request-scoped services:
//
//	http.HandleFunc("/users", func(w http.ResponseWriter, r *http.Request) {
//	    scope := provider.CreateScope(r.Context())
//	    defer scope.Close()
//
//	    // Services resolved in this scope are isolated
//	    service, _ := godi.Resolve[*UserService](scope)
//	})
//
// # Thread Safety
//
// All godi operations are thread-safe. The Provider and Scope types can be safely
// used from multiple goroutines concurrently.
//
// # Error Handling
//
// godi provides detailed error types for different failure scenarios:
//   - CircularDependencyError: Circular dependency detected
//   - ResolutionError: Service resolution failed
//   - LifetimeConflictError: Service registered with conflicting lifetimes
//   - ValidationError: Service validation failed
//
// # Best Practices
//
//   - Register services during application startup
//   - Use interfaces for flexibility and testability
//   - Prefer constructor injection over property injection
//   - Use scoped services for request-specific state
//   - Always close providers and scopes to release resources
//   - Use modules to organize related services
//   - Validate service configuration early with Build()
package godi
