# Changelog

All notable changes to godi are documented here. This project follows [Semantic Versioning](https://semver.org/) and uses [Conventional Commits](https://www.conventionalcommits.org/) for automatic versioning.


## [4.0.0](https://github.com/junioryono/godi/compare/v3.0.0...v4.0.0) (2025-08-18)


### ⚠ BREAKING CHANGES

* Module path updated from v3 to v4
* Complete architectural refactor of the dependency injection system
* Service registration now happens through Collection interface instead of ServiceProvider
* Provider interface is now immutable after build
* Scope extends Provider interface for unified API
* Removed Decorator functionality
* Changed HasService to Contains and HasKeyedService to ContainsKeyed
* Transient services can no longer depend on scoped services

Migration Guide:
- Update import paths to use `/v4`
- Replace ServiceProvider with Collection for service registration
- Use Collection.Build() to create an immutable Provider
- Update all resolution calls to use Provider interface
- Replace HasService/HasKeyedService calls with Contains/ContainsKeyed

### Features

* refactor entire DI system with new Collection and Provider architecture
* add immutable Provider pattern with build-time validation
* implement new reflection system for improved performance
* add support for instance registration without constructors
* allow multiple return values from constructors
* add comprehensive error wrapping and typed errors
* implement dependency graph with cycle detection
* add newScope method for scope creation
* register internal services when building collection


### Bug Fixes

* do not allow transient instances to depend on scoped instances
* fix not being able to get scope from context
* cache instances that do not have a constructor
* fix lifetime tests and validation
* fix security issues in dependency resolution
* fix graph dependency resolution issues
* fix scope context test
* fix all tests after refactoring


### Performance Improvements

* refactor reflection system for better performance
* refactor instance caching mechanisms


### Code Refactoring

* rename HasService to Contains and HasKeyedService to ContainsKeyed
* remove decorator functionality
* clean up createInstance method
* restructure error handling with typed errors
* update to v4 module path

## [3.0.0](https://github.com/junioryono/godi/compare/v2.1.0...v3.0.0) (2025-07-28)


### ⚠ BREAKING CHANGES

* All resolution functions (Resolve, ResolveKeyed, ResolveGroup)
now accept a Scope parameter instead of ServiceProvider. This enforces proper
lifetime constraints between Singleton and Scoped services.

Migration: Replace ServiceProvider parameters with Scope in all calls to:
- Resolve[T](s ServiceProvider) -> Resolve[T](s Scope)
- ResolveKeyed[T](s ServiceProvider, key) -> ResolveKeyed[T](s Scope, key)
- ResolveGroup[T](s ServiceProvider, group) -> ResolveGroup[T](s Scope, group)

### Features

* replace ServiceProvider with Scope in resolution functions ([f131f95](https://github.com/junioryono/godi/commit/f131f9576e358aa0d98a6c1b93a8d5f2bd869a96))

## [2.1.0](https://github.com/junioryono/godi/compare/v2.0.5...v2.1.0) (2025-07-22)


### Features

* add Decorate method to ServiceProvider and ServiceProviderScope for enhanced service modification ([4e19a8c](https://github.com/junioryono/godi/commit/4e19a8c5fde0a18f5dbab0adf71c79b8f2fd7ce8))

## [2.0.5](https://github.com/junioryono/godi/compare/v2.0.4...v2.0.5) (2025-07-22)


### Bug Fixes

* add context and scope as scoped services in service provider ([d269cfc](https://github.com/junioryono/godi/commit/d269cfc30afc642be7e7a8942417d2220790374e))
* enhance service provider to register built-in services and improve scope handling ([96177a6](https://github.com/junioryono/godi/commit/96177a649f0589d31e9f355f96488fd451cb71ca))

## [2.0.4](https://github.com/junioryono/godi/compare/v2.0.3...v2.0.4) (2025-07-22)


### Bug Fixes

* resolve context registration conflict in service scope ([43ceb3a](https://github.com/junioryono/godi/commit/43ceb3a55ad9bac458c96bbc964d3c63bf1b12ea))

## [2.0.3](https://github.com/junioryono/godi/compare/v2.0.2...v2.0.3) (2025-07-22)


### Bug Fixes

* handle duplicate service registrations with As and Group options ([e30fd66](https://github.com/junioryono/godi/commit/e30fd66bc284ec496b2ad2515a7512106ca344b4))

## [2.0.2](https://github.com/junioryono/godi/compare/v2.0.1...v2.0.2) (2025-07-21)


### Bug Fixes

* update installation instructions to use v2 of godi ([9c62926](https://github.com/junioryono/godi/commit/9c62926fe975d2dd07e2f2ab53935d034a79c329))

## [2.0.1](https://github.com/junioryono/godi/compare/v2.0.0...v2.0.1) (2025-07-21)


### Bug Fixes

* correct expected output in TestLastSegment for version path ([fb7de97](https://github.com/junioryono/godi/commit/fb7de979a986a8277d2e6d610ba1187e22942433))
* update module configuration for v2 compatibility ([cd064f1](https://github.com/junioryono/godi/commit/cd064f1257d3b463f41ad92efcedadf764069e09))

## [2.0.0](https://github.com/junioryono/godi/compare/v1.6.2...v2.0.0) (2025-07-21)


### ⚠ BREAKING CHANGES

* Service lifetimes have been redesigned and group resolution has been added. This is a breaking change from the previous API.
* redesign service lifetimes and add group resolution (#9)

### Features

* redesign service lifetimes and add group resolution ([#9](https://github.com/junioryono/godi/issues/9)) ([550159f](https://github.com/junioryono/godi/commit/550159fffd8af8bd67deaeb61a08aeb75f027f3b))
* trigger major release for service lifetime redesign ([e9d649f](https://github.com/junioryono/godi/commit/e9d649f1b15fdaf9240f609d7c0866c53c77753d))

## [1.6.2](https://github.com/junioryono/godi/compare/v1.6.1...v1.6.2) (2025-07-17)


### Bug Fixes

* update changelog reference to use markdown file and remove obsolete changelog.rst ([011286d](https://github.com/junioryono/godi/commit/011286d1d97e29700ad36fea2a2a28fa9c395202))

## [1.6.1](https://github.com/junioryono/godi/compare/v1.6.0...v1.6.1) (2025-07-17)


### Bug Fixes

* update changelog handling to skip commit and tag creation ([3d0538b](https://github.com/junioryono/godi/commit/3d0538b8117b8c8496b01caed05516cdbc45088e))

## [1.6.0](https://github.com/junioryono/godi/compare/v1.5.1...v1.6.0) (2025-07-17)


### Features

* ensure changelog has proper formatting and amend commit if necessary ([93a8ac2](https://github.com/junioryono/godi/commit/93a8ac262ab66684e144b47396741d746b9abb4b))

## [1.5.1](https://github.com/junioryono/godi/compare/v1.5.0...v1.5.1) (2025-07-17)


### Bug Fixes

* update installation instructions to use version placeholders ([4cb376e](https://github.com/junioryono/godi/commit/4cb376ec684fbe159b5655dae5c808feaa544b3d))

## [1.5.0](https://github.com/junioryono/godi/compare/v1.4.0...v1.5.0) (2025-07-17)


### Features

* update version handling in Sphinx config and adjust installation instructions to use version placeholders ([b156b4c](https://github.com/junioryono/godi/commit/b156b4ca0e662540634442f0dca58674b80ada04))

## [1.4.0](https://github.com/junioryono/godi/compare/v1.3.3...v1.4.0) (2025-07-17)


### Features

* add 'docs' to allowed scopes in PR title validation ([c117eaf](https://github.com/junioryono/godi/commit/c117eafad73fb0eba32474c19aed761f8b5293d4))
* implement type caching for reflection to improve performance ([#6](https://github.com/junioryono/godi/issues/6)) ([c709315](https://github.com/junioryono/godi/commit/c7093154270807b12e8c792aa47cdd3fc6957f8d))

## [1.3.3](https://github.com/junioryono/godi/compare/v1.3.2...v1.3.3) (2025-07-16)


### Bug Fixes

* **ci:** allow deps scope for Dependabot commits PR ([0f92ef1](https://github.com/junioryono/godi/commit/0f92ef1aed2b4c95dc631f2af646290aa0a48ba3))

## [1.3.2](https://github.com/junioryono/godi/compare/v1.3.1...v1.3.2) (2025-07-16)


### Bug Fixes

* **ci:** allow deps scope for Dependabot commits ([3c8bcbc](https://github.com/junioryono/godi/commit/3c8bcbc2089fb3c2fbacfdde51ff3f75b673954a))

## [1.3.1](https://github.com/junioryono/godi/compare/v1.3.0...v1.3.1) (2025-07-16)


### Bug Fixes

* **release:** update git message format to use {tag} instead of v{version} ([67d7ea7](https://github.com/junioryono/godi/commit/67d7ea7c7848111d88295238c47a4bcce63b9e32))

## [1.3.0](https://github.com/junioryono/godi/compare/v1.2.2...v1.3.0) (2025-07-16)


### Features

* **scope:** add public ScopeFromContext function with error handling ([7a95acb](https://github.com/junioryono/godi/commit/7a95acbe0f805871b4d71258c8d81495540c84c2))

## [1.2.2](https://github.com/junioryono/godi/compare/v1.2.1...v1.2.2) (2025-07-16)

## [1.2.1](https://github.com/junioryono/godi/compare/v1.2.0...v1.2.1) (2025-07-15)

## [1.2.0](https://github.com/junioryono/godi/compare/v1.1.0...v1.2.0) (2025-07-14)

## [1.1.0](https://github.com/junioryono/godi/compare/v1.0.2...v1.1.0) (2025-07-14)


### ⚠ BREAKING CHANGES

* refactor interfaces and module system for better composability (v1.1.0)

### Features

* refactor interfaces and module system for better composability (v1.1.0) ([8d7bb85](https://github.com/junioryono/godi/commit/8d7bb85019d73e838fbfee9771bd022ebfbe6635))

## [1.0.2](https://github.com/junioryono/godi/compare/v1.0.1...v1.0.2) (2025-07-13)

## [1.0.1](https://github.com/junioryono/godi/compare/v1.0.0...v1.0.1) (2025-07-12)

## 1.0.0 (2025-07-11)

