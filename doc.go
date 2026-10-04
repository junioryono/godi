// Package godi is a dependency injection container for Go with lifetimes,
// scopes, and deterministic resource cleanup.
//
// Services are registered on a [Collection] as constructors (functions whose
// parameters are their dependencies) or as pre-built values, then the
// collection is built into a [Provider] that creates and caches instances:
//
//	services := godi.NewCollection()
//	services.AddSingleton(NewConfig)
//	services.AddSingleton(NewDatabase) // func NewDatabase(*Config) (*Database, error)
//	services.AddScoped(NewUserService) // func NewUserService(*Database) *UserService
//
//	provider, err := services.Build()
//	if err != nil {
//	    log.Fatal(godi.Explain(err))
//	}
//	defer provider.Close()
//
// # Lifetimes
//
// Every registration has a [Lifetime]:
//
//   - [Singleton]: one instance for the provider, created at Build in
//     dependency order (or on first use with [Lazy]) and disposed when the
//     provider is closed.
//   - [Scoped]: one instance per [Scope], such as an HTTP request, disposed
//     when the scope is closed.
//   - [Transient]: a new instance on every resolution.
//
// A singleton may not depend on a scoped service, directly or through
// transients: it would capture one scope's instance for the application's
// lifetime. Transients may depend on scoped services.
//
// # Scopes
//
// [Provider.CreateScope] creates a [Scope]; scopes can be nested (CreateScope on a
// Scope) and are closed with their parent. The scope's context
// carries the scope, so code under a godi HTTP middleware can resolve with
// [ResolveFromContext] or [FromContext]. Cancelling the context a scope was
// created with does not close the scope: cancellation tells the work to stop,
// and the scope's owner closes it once the work is done. With
// [WithScopeValidation], resolving a scoped service from the root
// provider fails with [ErrScopeRequired] instead of silently caching one
// instance for the whole application.
//
// # Build
//
// [Collection.Build] reports every
// registration error at once, then validates the whole graph before running
// any constructor: missing dependencies, cycles, lifetime conflicts, and
// decorators that match no registration. [Validate] runs the same checks
// without constructing anything, for wiring tests that need no
// infrastructure.
//
// # Resolution
//
// [Resolve], [ResolveKeyed], and [ResolveGroup] (and their Must variants)
// resolve typed services from any [Resolver] — a Provider, a Scope, or a
// test double. Constructors and [Invoke] receive context.Context, the
// current [Scope], or the [Provider] when they ask for them.
// [IsService] and [IsKeyedService] check resolvability without constructing.
//
// # Disposal
//
// Values implementing [Disposable] (Close() error), [ContextCloser]
// (Close(ctx) error), or [Shutdowner] (Shutdown(ctx) error) are disposed by
// their owner: singletons by the provider, scoped (and transient) values by
// the scope that created them. Disposal runs in reverse creation order, so
// consumers are closed before their dependencies, and each value is closed
// once, by its longest-lived owner. A transient resolved directly from the
// provider belongs to the caller. [NoDispose] marks values the application
// owns. [Shutdown] disposes a provider or scope like Close but stops waiting
// when its context is done; context-aware resources receive that context.
//
// # Modules and composition
//
// [NewModule] groups registrations ([AddSingleton], [AddScoped],
// [AddTransient], other modules) into a reusable [ModuleOption] applied with
// [Collection.AddModules]; a module is applied at most once per collection.
// [ReplaceSingleton] and friends swap registrations (for fakes in tests),
// [TryAddSingleton] and friends register defaults, and [Decorate] wraps a
// registered service.
//
// # Registration options and features
//
//   - [Name] and [Key] register keyed services; [Group] collects several
//     registrations of one type; [As] registers a service under interfaces.
//   - [In] parameter objects receive dependencies as struct fields, with
//     name, group, and optional tags; [Out] result objects provide several
//     services from one constructor. Constructors may also return several
//     values, and an error last.
//   - [Lazy] defers a singleton's creation to its first resolution.
//   - [Start] runs [Starter] singletons after Build; [HealthCheck] runs
//     [HealthChecker] singletons.
//   - [Explain] (or %+v) expands an error with remediation hints, cycle
//     paths, and constructor stacks; error messages name constructors and
//     their source locations.
//   - [WithObserver] receives construction and disposal events.
//   - [Describe] and [WriteDOT] expose the dependency graph without
//     constructing anything.
//
// # Documentation
//
// Guides, concepts, and integration docs (net/http, Chi, Echo, Fiber, Gin,
// Huma): https://godi.readthedocs.io
//
// API reference and runnable examples:
// https://pkg.go.dev/github.com/junioryono/godi/v6
package godi
