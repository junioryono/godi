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
- Microservices architecture (Microservices)

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

- **v1.1.0** (Planned)

  - Async service resolution
  - Service factories with parameters
  - Enhanced debugging tools
  - Performance optimizations

- **v1.2.0** (Planned)

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

### From v0.x to v1.0.0

Version 1.0.0 is the first stable release. If you were using pre-release versions:

1. Update import paths if changed
2. Review breaking changes in API
3. Update service registrations to use new options
4. Test thoroughly before deploying

### Breaking Changes

No breaking changes in v1.0.0 as it's the initial release.

## Support

- **Bug Reports**: [GitHub Issues](https://github.com/junioryono/godi/issues)
- **Feature Requests**: [GitHub Discussions](https://github.com/junioryono/godi/discussions)
- **Security Issues**: See SECURITY.md

## Contributors

- Junior Yono - Initial work and maintenance

See also the list of [contributors](https://github.com/junioryono/godi/contributors) who participated in this project.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
