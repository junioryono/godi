# Changelog

All notable changes to godi will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Nothing yet

### Changed

- Nothing yet

### Deprecated

- Nothing yet

### Removed

- Nothing yet

### Fixed

- Nothing yet

### Security

- Nothing yet

## [1.2.1] - 2025-07-14

### Added

- Support for registering and resolving primitive types (int, string, bool, etc.) as singleton instances
- Direct registration of primitive values: `collection.AddSingleton(42, godi.Name("answer"))`

### Fixed

- Fixed resolution of services registered with `As(...)` option - interfaces specified via `As` are now properly resolvable
- Fixed type resolution for slices, maps, channels, and functions - these types are now resolved directly instead of as pointers
- Improved handling of non-pointer types in service resolution

### Changed

- Enhanced `determineServiceType[T]()` to handle primitive types correctly
- Improved type resolution logic for collection types (slice, map, chan, func)

### Examples

```go
// Primitive type registration now works
collection.AddSingleton(42, godi.Name("answer"))
collection.AddSingleton("hello world", godi.Name("greeting"))
collection.AddSingleton(true, godi.Name("enabled"))

// Resolution
answer, err := godi.ResolveKeyed[int](provider, "answer")
greeting, err := godi.ResolveKeyed[string](provider, "greeting")

// As(...) option now works correctly
collection.AddSingleton(NewPostgresDB, godi.As(new(Reader), new(Writer)))
reader, err := godi.Resolve[Reader](provider) // Now works!

// Collection types resolved directly
collection.AddSingleton([]string{"a", "b", "c"})
list, err := godi.Resolve[[]string](provider) // Returns []string, not *[]string
```

## [1.2.0] - 2025-07-13

### Added

- `Scope` now embeds `ServiceProvider` interface, providing direct access to all service resolution methods
- Improved API ergonomics - no need to call `.ServiceProvider()` on scopes anymore

### Changed

- **BREAKING**: `Scope` interface now embeds `ServiceProvider` instead of having a `ServiceProvider()` method
- **BREAKING**: Removed `ServiceProvider()` method from `Scope` interface
- Simplified scope usage - all `ServiceProvider` methods are now directly available on `Scope`

### Removed

- **BREAKING**: Removed `ServiceProvider()` method from `Scope` interface and its implementation

### Migration Guide

#### Scope Interface Changes

The `Scope` interface now directly provides all `ServiceProvider` methods through embedding. This eliminates the need for the intermediary `.ServiceProvider()` call.

Before:

```go
scope := provider.CreateScope(ctx)
defer scope.Close()

// Had to call .ServiceProvider() first
service, err := scope.ServiceProvider().Resolve(serviceType)
service, err := godi.Resolve[MyService](scope.ServiceProvider())
```

After:

```go
scope := provider.CreateScope(ctx)
defer scope.Close()

// Direct resolution from scope
service, err := scope.Resolve(serviceType)
service, err := godi.Resolve[MyService](scope)
```

#### Updated Usage Patterns

All service resolution can now be done directly on scopes:

```go
// Direct resolution
service, err := scope.Resolve(serviceType)

// Generic resolution
service, err := godi.Resolve[MyService](scope)

// Keyed resolution
service, err := scope.ResolveKeyed(serviceType, "primary")
service, err := godi.ResolveKeyed[Database](scope, "primary")

// Invoke functions
err := scope.Invoke(func(logger Logger) {
    logger.Log("Hello from scope")
})

// Check service availability
if scope.IsService(serviceType) {
    // Service is registered
}
```

### Why This Change?

This change improves the developer experience by:

- Reducing boilerplate code
- Making the API more intuitive
- Following Go's composition principles more closely
- Eliminating an unnecessary method call in the common path

Since a `Scope` IS a `ServiceProvider` (conceptually), this change makes that relationship explicit in the type system.

### Internal

- Simplified implementation by removing the redundant `ServiceProvider()` method
- Better adherence to the Interface Segregation Principle
- Cleaner interface hierarchy with proper embedding

## [1.1.0] - 2025-07-13

### Added

- `ServiceResolver` interface extracted from `ServiceProvider` for better separation of concerns
- `Scope` now implements `ServiceResolver` interface, allowing direct resolution from scopes
- Module system improvements for better composability

### Changed

- **BREAKING**: `ServiceProvider` interface refactored:
  - `Resolve` and `ResolveKeyed` methods moved to embedded `ServiceResolver` interface
  - `ServiceProvider` now embeds both `ServiceResolver` and `Disposable` interfaces
- **BREAKING**: Module API renamed for clarity:
  - `Module` function renamed to `NewModule`
  - `ModuleBuilder` type renamed to `ModuleOption`
  - `AddModule` function removed (modules can be composed directly)
- **BREAKING**: Generic resolution functions now accept `ServiceResolver` instead of `ServiceProvider`:
  - `Resolve[T](sr ServiceResolver)` instead of `Resolve[T](sp ServiceProvider)`
  - `ResolveKeyed[T](sr ServiceResolver, key)` instead of `ResolveKeyed[T](sp ServiceProvider, key)`
- Improved interface segregation following SOLID principles
- Better support for testing with smaller interface surface area

### Migration Guide

#### ServiceProvider Interface Changes

The `Resolve` and `ResolveKeyed` methods are still available on `ServiceProvider` through the embedded `ServiceResolver` interface. No code changes needed for basic usage.

#### Module System Changes

Before:

```go
var MyModule = godi.Module("mymodule",
    godi.AddSingleton(NewService),
    godi.AddModule(OtherModule),
)
```

After:

```go
var MyModule = godi.NewModule("mymodule",
    godi.AddSingleton(NewService),
    OtherModule, // Modules can be composed directly
)
```

#### Generic Resolution Functions

The generic helper functions now accept the `ServiceResolver` interface:

Before:

```go
service, err := godi.Resolve[MyService](provider)
```

After:

```go
// Still works - ServiceProvider implements ServiceResolver
service, err := godi.Resolve[MyService](provider)

// Also works with scopes directly
service, err := godi.Resolve[MyService](scope)
```

### Internal

- Cleaner interface design with better separation between service resolution and provider lifecycle management
- Improved testability by allowing resolution from smaller interfaces

## [1.0.2] - 2025-07-13

### Fixed

- Fixed thread safety issues in service provider operations by removing shared mutex usage
- Fixed memory leaks by properly cleaning up scope references when disposed
- Improved scope lifecycle management and disposal ordering

### Changed

- Refactored internal scope tracking to use a map-based approach instead of parent-child references
- Simplified mutex usage by removing the shared `digMutex` and using localized locking
- Improved internal ID generation to use timestamp + random bytes instead of UUID dependency
- Refactored service registration to use cleaner error handling patterns

### Removed

- Removed dependency on `github.com/google/uuid` package
- Removed unnecessary mutex locking in dig operations

### Internal

- Better separation of concerns between service provider and scope implementations
- Improved code organization with helper functions for service type determination
- Enhanced disposal tracking with proper context handling

## [1.0.1] - 2025-07-12

### Fixed

- Improved type resolution for generic service resolution
- Fixed resolution of value types (non-pointer types) when services are registered as pointers
- Refactored `Resolve[T]` and `ResolveKeyed[T]` to use shared helper functions for better maintainability

### Changed

- Internal refactoring: extracted `determineServiceType[T]()` and `assertServiceType[T]()` helper functions
- Better handling of pointer vs non-pointer type resolution

### Added

- Support for resolving services as value types: `Resolve[UserService](provider)` now works when service is registered as `*UserService`

## [1.0.0] - 2025-07-10

### Added

- Initial release of godi
- Microsoft-style dependency injection for Go
- Three service lifetimes: Singleton, Scoped, and Transient
- Type-safe service resolution with generics
- Keyed services for multiple implementations
- Service groups for collections
- Module system for organizing services
- Comprehensive error handling
- Service disposal with LIFO ordering
- Request scoping for web applications
- Parameter objects (In) and result objects (Out)
- Service decoration support
- Circular dependency detection
- Automatic lifecycle management
- Thread-safe operations
- Context propagation through scopes
- Service provider options for customization
- Resolution callbacks for monitoring
- Timeout support for service resolution
- Panic recovery option
- Dry run mode for validation

### Features

- **Service Registration**

  - `AddSingleton` - Register singleton services
  - `AddScoped` - Register scoped services
  - `AddTransient` - Register transient services
  - `Decorate` - Add decorators to services
  - `Replace` - Replace existing registrations
  - `RemoveAll` - Remove service registrations

- **Service Resolution**

  - `Resolve[T]` - Type-safe generic resolution
  - `ResolveKeyed[T]` - Type-safe keyed resolution
  - `Invoke` - Execute functions with DI

- **Scoping**

  - `CreateScope` - Create service scopes
  - Automatic disposal of scoped services
  - Context propagation
  - Hierarchical scopes

- **Advanced Features**
  - Module system for grouping services
  - Service groups for collections
  - Keyed services for variants
  - Parameter and result objects
  - Comprehensive error types
  - Lifecycle callbacks

### Examples

- Task management system (Getting Started)
- Blog REST API (Web Application)
- Test utilities and mocking (Testing)

### Documentation

- Comprehensive API reference
- Tutorials for common scenarios
- How-to guides for specific features
- Best practices guide
- Migration guide from other DI solutions

## Version History

### Versioning Policy

godi follows Semantic Versioning:

- **Major version** (1.0.0): Breaking API changes
- **Minor version** (1.1.0): New features, backward compatible
- **Patch version** (1.0.1): Bug fixes, backward compatible

### Compatibility

- Requires Go 1.21 or later (for generics support)
- Compatible with standard library
- Zero external dependencies (except bundled uber/dig)

### Future Releases

Planned features for future releases:

- **v1.2.0** (Planned)

  - Async service resolution
  - Service factories with parameters
  - Enhanced debugging tools
  - Performance optimizations

- **v1.3.0** (Planned)

  - Service middlewares
  - Dynamic service registration
  - Configuration integration
  - Health check integration

- **v2.0.0** (Future)
  - Simplified API
  - Better error messages
  - Enhanced performance
  - Additional lifetime options

## Upgrading

### From v1.0.x to v1.1.0

Version 1.1.0 includes breaking changes to improve the API design:

1. **Update module definitions**:
   - Change `godi.Module` to `godi.NewModule`
   - Remove `godi.AddModule` calls - modules can be composed directly
2. **Update resolution calls** (optional):

   - Generic functions now accept `ServiceResolver` interface
   - Existing code using `ServiceProvider` will continue to work

3. **Test updates**:
   - Mock implementations can now implement smaller `ServiceResolver` interface

### From v0.x to v1.0.0

Version 1.0.0 is the first stable release. If you were using pre-release versions:

1. Update import paths if changed
2. Review breaking changes in API
3. Update service registrations to use new options
4. Test thoroughly before deploying

### Breaking Changes

#### v1.1.0

- `Module` renamed to `NewModule`
- `ModuleBuilder` renamed to `ModuleOption`
- `AddModule` removed
- Resolution functions accept `ServiceResolver` interface

#### v1.0.0

No breaking changes as it's the initial release.

## Support

- **Bug Reports**: [GitHub Issues](https://github.com/junioryono/godi/issues)
- **Feature Requests**: [GitHub Discussions](https://github.com/junioryono/godi/discussions)
- **Security Issues**: See SECURITY.md

## Contributors

- Junior Yono - Initial work and maintenance

See also the list of [contributors](https://github.com/junioryono/godi/contributors) who participated in this project.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
