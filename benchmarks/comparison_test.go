// Package benchmarks compares godi with uber-go/dig and samber/do.
//
// Run them from the repository root with: make benchmark
//
// Every library is driven through its idiomatic public API for the same
// service graph, and every setup or resolution error fails the benchmark.
// The libraries do not expose identical operations, so read the results with
// these differences in mind:
//
//   - godi resolves by type with Resolve[T]; do resolves by type with
//     Invoke[T]. Both return (T, error) and are compared directly.
//   - dig has no resolve-by-type API. The only way to obtain a value is
//     Container.Invoke with a function that receives it, which inspects that
//     function by reflection on every call. The function is created once,
//     outside the timed loop, so the dig numbers measure Invoke itself rather
//     than closure allocation; they still include the reflective call.
//   - dig has no transient lifetime and no scopes, so it is absent from those
//     benchmarks.
//
// Results are written to package-level sinks so the compiler cannot discard
// the resolved values.
package benchmarks

import (
	"context"
	"testing"

	"github.com/junioryono/godi/v5"
	"github.com/samber/do/v2"
	"go.uber.org/dig"
)

// =============================================================================
// Shared Test Types
// =============================================================================

// Logger has no dependencies.
type Logger struct {
	Name string
}

func NewLogger() *Logger {
	return &Logger{Name: "logger"}
}

// Config has no dependencies.
type Config struct {
	Value string
}

func NewConfig() *Config {
	return &Config{Value: "config"}
}

// Database has 2 dependencies.
type Database struct {
	Logger *Logger
	Config *Config
}

func NewDatabase(logger *Logger, config *Config) *Database {
	return &Database{Logger: logger, Config: config}
}

// Cache has 3 dependencies.
type Cache struct {
	Logger   *Logger
	Config   *Config
	Database *Database
}

func NewCache(logger *Logger, config *Config, db *Database) *Cache {
	return &Cache{Logger: logger, Config: config, Database: db}
}

// UserService has 5 dependencies and a 3-level deep graph.
type UserService struct {
	Logger   *Logger
	Config   *Config
	Database *Database
	Cache    *Cache
	Dep5     *Dep5
}

type Dep5 struct {
	Value int
}

func NewDep5() *Dep5 {
	return &Dep5{Value: 5}
}

func NewUserService(logger *Logger, config *Config, db *Database, cache *Cache, dep5 *Dep5) *UserService {
	return &UserService{Logger: logger, Config: config, Database: db, Cache: cache, Dep5: dep5}
}

var (
	sinkLogger      *Logger
	sinkDatabase    *Database
	sinkUserService *UserService
)

// =============================================================================
// Container setup shared by the benchmarks
// =============================================================================

// constructors is the full graph in registration order.
var constructors = []any{NewLogger, NewConfig, NewDatabase, NewCache, NewDep5, NewUserService}

func addGodiSingletons(c godi.Collection) {
	for _, constructor := range constructors {
		c.AddSingleton(constructor)
	}
}

func buildGodi(tb testing.TB, collection godi.Collection) godi.Provider {
	tb.Helper()
	provider, err := collection.Build()
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := provider.Close(); err != nil {
			tb.Error(err)
		}
	})
	return provider
}

func provideDig(c *dig.Container, ctors ...any) error {
	for _, constructor := range ctors {
		if err := c.Provide(constructor); err != nil {
			return err
		}
	}
	return nil
}

func newDig(tb testing.TB, ctors ...any) *dig.Container {
	tb.Helper()
	c := dig.New()
	if err := provideDig(c, ctors...); err != nil {
		tb.Fatal(err)
	}
	return c
}

// provideDo registers the full graph. do providers resolve their own
// dependencies, so each one is a closure over the injector.
func provideDo(injector do.Injector) {
	do.Provide(injector, func(do.Injector) (*Logger, error) { return NewLogger(), nil })
	do.Provide(injector, func(do.Injector) (*Config, error) { return NewConfig(), nil })
	do.Provide(injector, func(i do.Injector) (*Database, error) {
		logger, err := do.Invoke[*Logger](i)
		if err != nil {
			return nil, err
		}
		config, err := do.Invoke[*Config](i)
		if err != nil {
			return nil, err
		}
		return NewDatabase(logger, config), nil
	})
	do.Provide(injector, func(i do.Injector) (*Cache, error) {
		logger, err := do.Invoke[*Logger](i)
		if err != nil {
			return nil, err
		}
		config, err := do.Invoke[*Config](i)
		if err != nil {
			return nil, err
		}
		db, err := do.Invoke[*Database](i)
		if err != nil {
			return nil, err
		}
		return NewCache(logger, config, db), nil
	})
	do.Provide(injector, func(do.Injector) (*Dep5, error) { return NewDep5(), nil })
	do.Provide(injector, func(i do.Injector) (*UserService, error) {
		logger, err := do.Invoke[*Logger](i)
		if err != nil {
			return nil, err
		}
		config, err := do.Invoke[*Config](i)
		if err != nil {
			return nil, err
		}
		db, err := do.Invoke[*Database](i)
		if err != nil {
			return nil, err
		}
		cache, err := do.Invoke[*Cache](i)
		if err != nil {
			return nil, err
		}
		dep5, err := do.Invoke[*Dep5](i)
		if err != nil {
			return nil, err
		}
		return NewUserService(logger, config, db, cache, dep5), nil
	})
}

func newDo(tb testing.TB, provide func(do.Injector)) *do.RootScope {
	tb.Helper()
	injector := do.New()
	provide(injector)
	tb.Cleanup(func() {
		if report := injector.Shutdown(); report != nil && !report.Succeed {
			tb.Error(report)
		}
	})
	return injector
}

// mustResolve resolves once outside the timed loop, so singleton benchmarks
// measure cached lookups and setup failures are reported before timing.
func mustResolve[T any](tb testing.TB, resolve func() (T, error)) T {
	tb.Helper()
	value, err := resolve()
	if err != nil {
		tb.Fatal(err)
	}
	return value
}

// =============================================================================
// Registration: describe the 6-service graph
//
// godi records descriptors (constructor analysis happens here; graph
// validation is deferred to Build). dig's Provide analyzes each constructor
// and checks the graph for cycles. do stores lazy provider closures.
// =============================================================================

func BenchmarkRegistration_Godi(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		addGodiSingletons(godi.NewCollection())
	}
}

func BenchmarkRegistration_Dig(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if err := provideDig(dig.New(), constructors...); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkRegistration_Do(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		provideDo(do.New())
	}
}

// =============================================================================
// Resolve a cached singleton with no dependencies
// =============================================================================

func BenchmarkResolve_Simple_Godi(b *testing.B) {
	c := godi.NewCollection()
	c.AddSingleton(NewLogger)
	p := buildGodi(b, c)
	mustResolve(b, func() (*Logger, error) { return godi.Resolve[*Logger](p) })

	b.ReportAllocs()
	for b.Loop() {
		logger, err := godi.Resolve[*Logger](p)
		if err != nil {
			b.Fatal(err)
		}
		sinkLogger = logger
	}
}

// Measures Container.Invoke with a pre-built function (see package docs).
func BenchmarkResolve_Simple_Dig(b *testing.B) {
	c := newDig(b, NewLogger)
	var logger *Logger
	receive := func(l *Logger) { logger = l }
	if err := c.Invoke(receive); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		if err := c.Invoke(receive); err != nil {
			b.Fatal(err)
		}
		sinkLogger = logger
	}
}

func BenchmarkResolve_Simple_Do(b *testing.B) {
	injector := newDo(b, func(i do.Injector) {
		do.Provide(i, func(do.Injector) (*Logger, error) { return NewLogger(), nil })
	})
	mustResolve(b, func() (*Logger, error) { return do.Invoke[*Logger](injector) })

	b.ReportAllocs()
	for b.Loop() {
		logger, err := do.Invoke[*Logger](injector)
		if err != nil {
			b.Fatal(err)
		}
		sinkLogger = logger
	}
}

// =============================================================================
// Resolve a cached singleton with 5 dependencies
// =============================================================================

func BenchmarkResolve_Complex_Godi(b *testing.B) {
	c := godi.NewCollection()
	addGodiSingletons(c)
	p := buildGodi(b, c)
	mustResolve(b, func() (*UserService, error) { return godi.Resolve[*UserService](p) })

	b.ReportAllocs()
	for b.Loop() {
		service, err := godi.Resolve[*UserService](p)
		if err != nil {
			b.Fatal(err)
		}
		sinkUserService = service
	}
}

// Measures Container.Invoke with a pre-built function (see package docs).
func BenchmarkResolve_Complex_Dig(b *testing.B) {
	c := newDig(b, constructors...)
	var service *UserService
	receive := func(u *UserService) { service = u }
	if err := c.Invoke(receive); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	for b.Loop() {
		if err := c.Invoke(receive); err != nil {
			b.Fatal(err)
		}
		sinkUserService = service
	}
}

func BenchmarkResolve_Complex_Do(b *testing.B) {
	injector := newDo(b, provideDo)
	mustResolve(b, func() (*UserService, error) { return do.Invoke[*UserService](injector) })

	b.ReportAllocs()
	for b.Loop() {
		service, err := do.Invoke[*UserService](injector)
		if err != nil {
			b.Fatal(err)
		}
		sinkUserService = service
	}
}

// =============================================================================
// Resolve a transient (a new instance per call) from the root container.
// dig has no transient lifetime.
// =============================================================================

func BenchmarkResolve_Transient_Godi(b *testing.B) {
	c := godi.NewCollection()
	c.AddTransient(NewLogger)
	p := buildGodi(b, c)

	b.ReportAllocs()
	for b.Loop() {
		logger, err := godi.Resolve[*Logger](p)
		if err != nil {
			b.Fatal(err)
		}
		sinkLogger = logger
	}
}

func BenchmarkResolve_Transient_Do(b *testing.B) {
	injector := newDo(b, func(i do.Injector) {
		do.ProvideTransient(i, func(do.Injector) (*Logger, error) { return NewLogger(), nil })
	})

	b.ReportAllocs()
	for b.Loop() {
		logger, err := do.Invoke[*Logger](injector)
		if err != nil {
			b.Fatal(err)
		}
		sinkLogger = logger
	}
}

// =============================================================================
// Resolve the 5-dependency singleton from parallel goroutines
// =============================================================================

func BenchmarkResolve_Concurrent_Godi(b *testing.B) {
	c := godi.NewCollection()
	addGodiSingletons(c)
	p := buildGodi(b, c)
	mustResolve(b, func() (*UserService, error) { return godi.Resolve[*UserService](p) })

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := godi.Resolve[*UserService](p); err != nil {
				b.Error(err)
				return
			}
		}
	})
}

// Measures Container.Invoke with a pre-built function (see package docs).
func BenchmarkResolve_Concurrent_Dig(b *testing.B) {
	c := newDig(b, constructors...)
	receive := func(*UserService) {}
	if err := c.Invoke(receive); err != nil {
		b.Fatal(err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := c.Invoke(receive); err != nil {
				b.Error(err)
				return
			}
		}
	})
}

func BenchmarkResolve_Concurrent_Do(b *testing.B) {
	injector := newDo(b, provideDo)
	mustResolve(b, func() (*UserService, error) { return do.Invoke[*UserService](injector) })

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := do.Invoke[*UserService](injector); err != nil {
				b.Error(err)
				return
			}
		}
	})
}

// =============================================================================
// Scopes (godi only: dig has no scopes, and do scopes are named containers
// rather than per-request lifetimes)
// =============================================================================

func newScopedGodi(b *testing.B) godi.Provider {
	b.Helper()
	c := godi.NewCollection()
	c.AddSingleton(NewLogger)
	c.AddScoped(NewConfig)
	c.AddScoped(NewDatabase)
	return buildGodi(b, c)
}

func BenchmarkScope_Create_Godi(b *testing.B) {
	p := newScopedGodi(b)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		scope, err := p.CreateScope(ctx)
		if err != nil {
			b.Fatal(err)
		}
		if err := scope.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkScope_CreateAndResolve_Godi(b *testing.B) {
	p := newScopedGodi(b)
	ctx := context.Background()

	b.ReportAllocs()
	for b.Loop() {
		scope, err := p.CreateScope(ctx)
		if err != nil {
			b.Fatal(err)
		}
		db, err := godi.Resolve[*Database](scope)
		if err != nil {
			b.Fatal(err)
		}
		sinkDatabase = db
		if err := scope.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

// =============================================================================
// Cold start: create a container, register the graph, resolve the
// 5-dependency service once, and release the container.
//
// godi's Build validates the whole graph before the first resolution; dig
// and do validate lazily on first Invoke. godi and do run their shutdown
// (Close, Shutdown); dig has none.
// =============================================================================

func BenchmarkResolve_FirstTime_Godi(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		c := godi.NewCollection()
		addGodiSingletons(c)
		p, err := c.Build()
		if err != nil {
			b.Fatal(err)
		}
		service, err := godi.Resolve[*UserService](p)
		if err != nil {
			b.Fatal(err)
		}
		sinkUserService = service
		if err := p.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResolve_FirstTime_Dig(b *testing.B) {
	b.ReportAllocs()
	receive := func(u *UserService) { sinkUserService = u }
	for b.Loop() {
		c := dig.New()
		if err := provideDig(c, constructors...); err != nil {
			b.Fatal(err)
		}
		if err := c.Invoke(receive); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkResolve_FirstTime_Do(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		injector := do.New()
		provideDo(injector)
		service, err := do.Invoke[*UserService](injector)
		if err != nil {
			b.Fatal(err)
		}
		sinkUserService = service
		if report := injector.Shutdown(); report != nil && !report.Succeed {
			b.Fatal(report)
		}
	}
}
