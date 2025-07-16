# Changelog

All notable changes to godi will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

<!-- Releases will be automatically added here by the CI/CD pipeline -->
<!-- Do not edit above this line -->

## Previous Releases

Below are releases before automated changelog generation was implemented.

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

## [1.0.2] - 2025-07-13

### Fixed

- Fixed thread safety issues in service provider operations by removing shared mutex usage
- Fixed memory leaks by properly cleaning up scope references when disposed
- Improved scope lifecycle management and disposal ordering

### Changed

- Refactored internal scope tracking to use a map-based approach instead of parent-child references
- Simplified mutex usage by removing the shared `digMutex` and using localized locking
- Improved internal ID generation to use timestamp + random bytes instead of UUID dependency

### Removed

- Removed dependency on `github.com/google/uuid` package

## [1.0.1] - 2025-07-12

### Fixed

- Improved type resolution for generic service resolution
- Fixed resolution of value types (non-pointer types) when services are registered as pointers

### Changed

- Internal refactoring: extracted `determineServiceType[T]()` and `assertServiceType[T]()` helper functions

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

[1.2.1]: https://github.com/junioryono/godi/compare/v1.2.0...v1.2.1
[1.2.0]: https://github.com/junioryono/godi/compare/v1.1.0...v1.2.0
[1.1.0]: https://github.com/junioryono/godi/compare/v1.0.2...v1.1.0
[1.0.2]: https://github.com/junioryono/godi/compare/v1.0.1...v1.0.2
[1.0.1]: https://github.com/junioryono/godi/compare/v1.0.0...v1.0.1
[1.0.0]: https://github.com/junioryono/godi/releases/tag/v1.0.0
