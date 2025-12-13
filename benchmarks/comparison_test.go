// Package benchmarks provides comparative benchmarks between godi and other DI libraries.
//
// Run benchmarks with: go test -bench=. -benchmem ./benchmarks/
package benchmarks

import (
	"context"
	"testing"

	"github.com/junioryono/godi/v4"
	"github.com/samber/do/v2"
	"go.uber.org/dig"
)

// =============================================================================
// Shared Test Types
// =============================================================================

// Simple service with no dependencies
type Logger struct {
	Name string
}

func NewLogger() *Logger {
	return &Logger{Name: "logger"}
}

// Service with 1 dependency
type Config struct {
	Value string
}

func NewConfig() *Config {
	return &Config{Value: "config"}
}

// Service with 2 dependencies
type Database struct {
	Logger *Logger
	Config *Config
}

func NewDatabase(logger *Logger, config *Config) *Database {
	return &Database{Logger: logger, Config: config}
}

// Service with 3 dependencies
type Cache struct {
	Logger   *Logger
	Config   *Config
	Database *Database
}

func NewCache(logger *Logger, config *Config, db *Database) *Cache {
	return &Cache{Logger: logger, Config: config, Database: db}
}

// Service with 5 dependencies (complex)
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

// =============================================================================
// Container/Provider Build Benchmarks
// =============================================================================

func BenchmarkBuild_Godi(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c := godi.NewCollection()
		c.AddSingleton(NewLogger)
		c.AddSingleton(NewConfig)
		c.AddSingleton(NewDatabase)
		c.AddSingleton(NewCache)
		c.AddSingleton(NewDep5)
		c.AddSingleton(NewUserService)
		p, _ := c.Build()
		p.Close()
	}
}

func BenchmarkBuild_Dig(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c := dig.New()
		c.Provide(NewLogger)
		c.Provide(NewConfig)
		c.Provide(NewDatabase)
		c.Provide(NewCache)
		c.Provide(NewDep5)
		c.Provide(NewUserService)
	}
}

func BenchmarkBuild_Do(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		injector := do.New()
		do.Provide(injector, func(i do.Injector) (*Logger, error) { return NewLogger(), nil })
		do.Provide(injector, func(i do.Injector) (*Config, error) { return NewConfig(), nil })
		do.Provide(injector, func(i do.Injector) (*Database, error) {
			logger := do.MustInvoke[*Logger](i)
			config := do.MustInvoke[*Config](i)
			return NewDatabase(logger, config), nil
		})
		do.Provide(injector, func(i do.Injector) (*Cache, error) {
			logger := do.MustInvoke[*Logger](i)
			config := do.MustInvoke[*Config](i)
			db := do.MustInvoke[*Database](i)
			return NewCache(logger, config, db), nil
		})
		do.Provide(injector, func(i do.Injector) (*Dep5, error) { return NewDep5(), nil })
		do.Provide(injector, func(i do.Injector) (*UserService, error) {
			logger := do.MustInvoke[*Logger](i)
			config := do.MustInvoke[*Config](i)
			db := do.MustInvoke[*Database](i)
			cache := do.MustInvoke[*Cache](i)
			dep5 := do.MustInvoke[*Dep5](i)
			return NewUserService(logger, config, db, cache, dep5), nil
		})
		injector.Shutdown()
	}
}

// =============================================================================
// Simple Resolution Benchmarks (No Dependencies)
// =============================================================================

func BenchmarkResolve_Simple_Godi(b *testing.B) {
	c := godi.NewCollection()
	c.AddSingleton(NewLogger)
	p, _ := c.Build()
	defer p.Close()

	// Warm up
	godi.MustResolve[*Logger](p)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = godi.MustResolve[*Logger](p)
	}
}

func BenchmarkResolve_Simple_Dig(b *testing.B) {
	c := dig.New()
	c.Provide(NewLogger)

	// Warm up
	c.Invoke(func(l *Logger) {})

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.Invoke(func(l *Logger) {})
	}
}

func BenchmarkResolve_Simple_Do(b *testing.B) {
	injector := do.New()
	do.Provide(injector, func(i do.Injector) (*Logger, error) { return NewLogger(), nil })

	// Warm up
	do.MustInvoke[*Logger](injector)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = do.MustInvoke[*Logger](injector)
	}
}

// =============================================================================
// Complex Resolution Benchmarks (5 Dependencies)
// =============================================================================

func BenchmarkResolve_Complex_Godi(b *testing.B) {
	c := godi.NewCollection()
	c.AddSingleton(NewLogger)
	c.AddSingleton(NewConfig)
	c.AddSingleton(NewDatabase)
	c.AddSingleton(NewCache)
	c.AddSingleton(NewDep5)
	c.AddSingleton(NewUserService)
	p, _ := c.Build()
	defer p.Close()

	// Warm up
	godi.MustResolve[*UserService](p)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = godi.MustResolve[*UserService](p)
	}
}

func BenchmarkResolve_Complex_Dig(b *testing.B) {
	c := dig.New()
	c.Provide(NewLogger)
	c.Provide(NewConfig)
	c.Provide(NewDatabase)
	c.Provide(NewCache)
	c.Provide(NewDep5)
	c.Provide(NewUserService)

	// Warm up
	c.Invoke(func(u *UserService) {})

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c.Invoke(func(u *UserService) {})
	}
}

func BenchmarkResolve_Complex_Do(b *testing.B) {
	injector := do.New()
	do.Provide(injector, func(i do.Injector) (*Logger, error) { return NewLogger(), nil })
	do.Provide(injector, func(i do.Injector) (*Config, error) { return NewConfig(), nil })
	do.Provide(injector, func(i do.Injector) (*Database, error) {
		logger := do.MustInvoke[*Logger](i)
		config := do.MustInvoke[*Config](i)
		return NewDatabase(logger, config), nil
	})
	do.Provide(injector, func(i do.Injector) (*Cache, error) {
		logger := do.MustInvoke[*Logger](i)
		config := do.MustInvoke[*Config](i)
		db := do.MustInvoke[*Database](i)
		return NewCache(logger, config, db), nil
	})
	do.Provide(injector, func(i do.Injector) (*Dep5, error) { return NewDep5(), nil })
	do.Provide(injector, func(i do.Injector) (*UserService, error) {
		logger := do.MustInvoke[*Logger](i)
		config := do.MustInvoke[*Config](i)
		db := do.MustInvoke[*Database](i)
		cache := do.MustInvoke[*Cache](i)
		dep5 := do.MustInvoke[*Dep5](i)
		return NewUserService(logger, config, db, cache, dep5), nil
	})

	// Warm up
	do.MustInvoke[*UserService](injector)

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = do.MustInvoke[*UserService](injector)
	}
}

// =============================================================================
// Transient Resolution Benchmarks (New Instance Each Time)
// =============================================================================

func BenchmarkResolve_Transient_Godi(b *testing.B) {
	c := godi.NewCollection()
	c.AddTransient(NewLogger)
	p, _ := c.Build()
	defer p.Close()

	scope, _ := p.CreateScope(context.Background())
	defer scope.Close()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = godi.MustResolve[*Logger](scope)
	}
}

func BenchmarkResolve_Transient_Do(b *testing.B) {
	injector := do.New()
	do.ProvideTransient(injector, func(i do.Injector) (*Logger, error) { return NewLogger(), nil })

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = do.MustInvoke[*Logger](injector)
	}
}

// Note: Dig doesn't have built-in transient support

// =============================================================================
// Concurrent Resolution Benchmarks
// =============================================================================

func BenchmarkResolve_Concurrent_Godi(b *testing.B) {
	c := godi.NewCollection()
	c.AddSingleton(NewLogger)
	c.AddSingleton(NewConfig)
	c.AddSingleton(NewDatabase)
	c.AddSingleton(NewCache)
	c.AddSingleton(NewDep5)
	c.AddSingleton(NewUserService)
	p, _ := c.Build()
	defer p.Close()

	// Warm up
	godi.MustResolve[*UserService](p)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = godi.MustResolve[*UserService](p)
		}
	})
}

func BenchmarkResolve_Concurrent_Dig(b *testing.B) {
	c := dig.New()
	c.Provide(NewLogger)
	c.Provide(NewConfig)
	c.Provide(NewDatabase)
	c.Provide(NewCache)
	c.Provide(NewDep5)
	c.Provide(NewUserService)

	// Warm up
	c.Invoke(func(u *UserService) {})

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			c.Invoke(func(u *UserService) {})
		}
	})
}

func BenchmarkResolve_Concurrent_Do(b *testing.B) {
	injector := do.New()
	do.Provide(injector, func(i do.Injector) (*Logger, error) { return NewLogger(), nil })
	do.Provide(injector, func(i do.Injector) (*Config, error) { return NewConfig(), nil })
	do.Provide(injector, func(i do.Injector) (*Database, error) {
		logger := do.MustInvoke[*Logger](i)
		config := do.MustInvoke[*Config](i)
		return NewDatabase(logger, config), nil
	})
	do.Provide(injector, func(i do.Injector) (*Cache, error) {
		logger := do.MustInvoke[*Logger](i)
		config := do.MustInvoke[*Config](i)
		db := do.MustInvoke[*Database](i)
		return NewCache(logger, config, db), nil
	})
	do.Provide(injector, func(i do.Injector) (*Dep5, error) { return NewDep5(), nil })
	do.Provide(injector, func(i do.Injector) (*UserService, error) {
		logger := do.MustInvoke[*Logger](i)
		config := do.MustInvoke[*Config](i)
		db := do.MustInvoke[*Database](i)
		cache := do.MustInvoke[*Cache](i)
		dep5 := do.MustInvoke[*Dep5](i)
		return NewUserService(logger, config, db, cache, dep5), nil
	})

	// Warm up
	do.MustInvoke[*UserService](injector)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = do.MustInvoke[*UserService](injector)
		}
	})
}

// =============================================================================
// Scope Creation Benchmarks (godi unique feature)
// =============================================================================

func BenchmarkScope_Create_Godi(b *testing.B) {
	c := godi.NewCollection()
	c.AddSingleton(NewLogger)
	c.AddScoped(NewConfig)
	c.AddScoped(NewDatabase)
	p, _ := c.Build()
	defer p.Close()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		scope, _ := p.CreateScope(context.Background())
		scope.Close()
	}
}

func BenchmarkScope_CreateAndResolve_Godi(b *testing.B) {
	c := godi.NewCollection()
	c.AddSingleton(NewLogger)
	c.AddScoped(NewConfig)
	c.AddScoped(NewDatabase)
	p, _ := c.Build()
	defer p.Close()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		scope, _ := p.CreateScope(context.Background())
		_ = godi.MustResolve[*Database](scope)
		scope.Close()
	}
}

// =============================================================================
// First Resolution Benchmarks (Cold Start)
// =============================================================================

func BenchmarkResolve_FirstTime_Godi(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c := godi.NewCollection()
		c.AddSingleton(NewLogger)
		c.AddSingleton(NewConfig)
		c.AddSingleton(NewDatabase)
		c.AddSingleton(NewCache)
		c.AddSingleton(NewDep5)
		c.AddSingleton(NewUserService)
		p, _ := c.Build()
		_ = godi.MustResolve[*UserService](p)
		p.Close()
	}
}

func BenchmarkResolve_FirstTime_Dig(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		c := dig.New()
		c.Provide(NewLogger)
		c.Provide(NewConfig)
		c.Provide(NewDatabase)
		c.Provide(NewCache)
		c.Provide(NewDep5)
		c.Provide(NewUserService)
		c.Invoke(func(u *UserService) {})
	}
}

func BenchmarkResolve_FirstTime_Do(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		injector := do.New()
		do.Provide(injector, func(i do.Injector) (*Logger, error) { return NewLogger(), nil })
		do.Provide(injector, func(i do.Injector) (*Config, error) { return NewConfig(), nil })
		do.Provide(injector, func(i do.Injector) (*Database, error) {
			logger := do.MustInvoke[*Logger](i)
			config := do.MustInvoke[*Config](i)
			return NewDatabase(logger, config), nil
		})
		do.Provide(injector, func(i do.Injector) (*Cache, error) {
			logger := do.MustInvoke[*Logger](i)
			config := do.MustInvoke[*Config](i)
			db := do.MustInvoke[*Database](i)
			return NewCache(logger, config, db), nil
		})
		do.Provide(injector, func(i do.Injector) (*Dep5, error) { return NewDep5(), nil })
		do.Provide(injector, func(i do.Injector) (*UserService, error) {
			logger := do.MustInvoke[*Logger](i)
			config := do.MustInvoke[*Config](i)
			db := do.MustInvoke[*Database](i)
			cache := do.MustInvoke[*Cache](i)
			dep5 := do.MustInvoke[*Dep5](i)
			return NewUserService(logger, config, db, cache, dep5), nil
		})
		_ = do.MustInvoke[*UserService](injector)
		injector.Shutdown()
	}
}
