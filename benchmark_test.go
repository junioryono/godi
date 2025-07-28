package godi_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/junioryono/godi/v2"
	"github.com/junioryono/godi/v2/internal/testutil"
)

// Benchmarks to measure performance of key operations

func BenchmarkServiceResolution(b *testing.B) {
	benchmarks := []struct {
		name  string
		setup func() (godi.ServiceProvider, func())
	}{
		{
			name: "singleton_simple",
			setup: func() (godi.ServiceProvider, func()) {
				provider := godi.NewServiceProvider()
				_ = provider.AddSingleton(testutil.NewTestLogger)
				return provider, func() { provider.Close() }
			},
		},
		{
			name: "singleton_with_deps",
			setup: func() (godi.ServiceProvider, func()) {
				provider := godi.NewServiceProvider()
				_ = provider.AddSingleton(testutil.NewTestLogger)
				_ = provider.AddSingleton(testutil.NewTestDatabase)
				_ = provider.AddSingleton(testutil.NewTestCache)
				_ = provider.AddSingleton(testutil.NewTestServiceWithDeps)
				return provider, func() { provider.Close() }
			},
		},
		{
			name: "scoped_simple",
			setup: func() (godi.ServiceProvider, func()) {
				provider := godi.NewServiceProvider()
				_ = provider.AddScoped(testutil.NewTestService)
				scope := provider.CreateScope(context.Background())
				return scope, func() { scope.Close(); provider.Close() }
			},
		},
		{
			name: "keyed_service",
			setup: func() (godi.ServiceProvider, func()) {
				provider := godi.NewServiceProvider()
				_ = provider.AddSingleton(testutil.NewTestLogger, godi.Name("primary"))
				return provider, func() { provider.Close() }
			},
		},
		{
			name: "group_services",
			setup: func() (godi.ServiceProvider, func()) {
				provider := godi.NewServiceProvider()
				for i := 0; i < 10; i++ {
					idx := i
					_ = provider.AddSingleton(func() testutil.TestHandler {
						return testutil.NewTestHandler(fmt.Sprintf("h%d", idx))
					}, godi.Group("handlers"))
				}
				return provider, func() { provider.Close() }
			},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			provider, cleanup := bm.setup()
			defer cleanup()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				switch bm.name {
				case "singleton_simple":
					_, _ = godi.Resolve[testutil.TestLogger](provider.GetRootScope())
				case "singleton_with_deps":
					_, _ = godi.Resolve[*testutil.TestServiceWithDeps](provider.GetRootScope())
				case "scoped_simple":
					_, _ = godi.Resolve[*testutil.TestService](provider.GetRootScope())
				case "keyed_service":
					_, _ = godi.ResolveKeyed[testutil.TestLogger](provider.GetRootScope(), "primary")
				case "group_services":
					_, _ = godi.ResolveGroup[testutil.TestHandler](provider.GetRootScope(), "handlers")
				}
			}
		})
	}
}

func BenchmarkScopeCreation(b *testing.B) {
	provider := godi.NewServiceProvider()
	_ = provider.AddSingleton(testutil.NewTestLogger)
	_ = provider.AddScoped(testutil.NewTestService)

	defer provider.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		scope := provider.CreateScope(context.Background())
		scope.Close()
	}
}

func BenchmarkProviderBuild(b *testing.B) {
	benchmarks := []struct {
		name         string
		serviceCount int
	}{
		{"10_services", 10},
		{"50_services", 50},
		{"100_services", 100},
		{"500_services", 500},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				provider := godi.NewServiceProvider()

				// Add services
				for j := 0; j < bm.serviceCount; j++ {
					idx := j
					_ = provider.AddSingleton(func() interface{} {
						return fmt.Sprintf("service-%d", idx)
					})
				}

				provider.Close()
			}
		})
	}
}

func BenchmarkConcurrentResolution(b *testing.B) {
	provider := godi.NewServiceProvider()
	_ = provider.AddSingleton(testutil.NewTestLogger)
	_ = provider.AddSingleton(testutil.NewTestDatabase)
	_ = provider.AddScoped(testutil.NewTestService)

	defer provider.Close()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Mix of resolutions
			_, _ = godi.Resolve[testutil.TestLogger](provider.GetRootScope())
			_, _ = godi.Resolve[testutil.TestDatabase](provider.GetRootScope())

			scope := provider.CreateScope(context.Background())
			_, _ = godi.Resolve[*testutil.TestService](scope)
			scope.Close()
		}
	})
}

func BenchmarkParameterObjects(b *testing.B) {
	type ServiceParams struct {
		godi.In

		Logger   testutil.TestLogger
		Database testutil.TestDatabase
		Cache    testutil.TestCache
	}

	constructor := func(params ServiceParams) *testutil.TestService {
		return testutil.NewTestService()
	}

	provider := godi.NewServiceProvider()
	_ = provider.AddSingleton(testutil.NewTestLogger)
	_ = provider.AddSingleton(testutil.NewTestDatabase)
	_ = provider.AddSingleton(testutil.NewTestCache)
	_ = provider.AddSingleton(constructor)

	defer provider.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = godi.Resolve[*testutil.TestService](provider.GetRootScope())
	}
}

func BenchmarkResultObjects(b *testing.B) {
	type ServiceBundle struct {
		godi.Out

		Service1 *testutil.TestService
		Service2 *testutil.TestService
		Service3 *testutil.TestService
	}

	constructor := func() ServiceBundle {
		return ServiceBundle{
			Service1: testutil.NewTestService(),
			Service2: testutil.NewTestService(),
			Service3: testutil.NewTestService(),
		}
	}

	provider := godi.NewServiceProvider()
	_ = provider.AddSingleton(constructor)

	defer provider.Close()

	// Force creation
	_, _ = godi.Resolve[*testutil.TestService](provider.GetRootScope())

	b.ResetTimer()
	b.ReportAllocs()

	// Subsequent resolutions should be cached
	for i := 0; i < b.N; i++ {
		_, _ = godi.Resolve[*testutil.TestService](provider.GetRootScope())
	}
}

func BenchmarkComplexDependencyGraph(b *testing.B) {
	// Create a complex dependency graph
	type Level5 struct{ ID string }
	type Level4A struct{ L5 *Level5 }
	type Level4B struct{ L5 *Level5 }
	type Level3A struct {
		L4A *Level4A
		L4B *Level4B
	}
	type Level3B struct {
		L4A *Level4A
		L4B *Level4B
	}
	type Level2A struct {
		L3A *Level3A
		L3B *Level3B
	}
	type Level2B struct {
		L3A *Level3A
		L3B *Level3B
	}
	type Level1 struct {
		L2A *Level2A
		L2B *Level2B
	}

	provider := godi.NewServiceProvider()
	_ = provider.AddSingleton(func() *Level5 { return &Level5{ID: "root"} })
	_ = provider.AddSingleton(func(l5 *Level5) *Level4A { return &Level4A{L5: l5} })
	_ = provider.AddSingleton(func(l5 *Level5) *Level4B { return &Level4B{L5: l5} })
	_ = provider.AddSingleton(func(l4a *Level4A, l4b *Level4B) *Level3A { return &Level3A{L4A: l4a, L4B: l4b} })
	_ = provider.AddSingleton(func(l4a *Level4A, l4b *Level4B) *Level3B { return &Level3B{L4A: l4a, L4B: l4b} })
	_ = provider.AddSingleton(func(l3a *Level3A, l3b *Level3B) *Level2A { return &Level2A{L3A: l3a, L3B: l3b} })
	_ = provider.AddSingleton(func(l3a *Level3A, l3b *Level3B) *Level2B { return &Level2B{L3A: l3a, L3B: l3b} })
	_ = provider.AddSingleton(func(l2a *Level2A, l2b *Level2B) *Level1 { return &Level1{L2A: l2a, L2B: l2b} })

	defer provider.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = godi.Resolve[*Level1](provider.GetRootScope())
	}
}

func BenchmarkModuleLoading(b *testing.B) {
	createModule := func(name string, serviceCount int) godi.ModuleOption {
		builders := make([]godi.ModuleOption, serviceCount)
		for i := 0; i < serviceCount; i++ {
			idx := i
			builders[i] = godi.AddSingleton(func() interface{} {
				return fmt.Sprintf("%s-service-%d", name, idx)
			})
		}
		return godi.NewModule(name, builders...)
	}

	benchmarks := []struct {
		name         string
		moduleCount  int
		servicesEach int
	}{
		{"5x10", 5, 10},
		{"10x10", 10, 10},
		{"5x50", 5, 50},
		{"10x50", 10, 50},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				provider := godi.NewServiceProvider()

				// Create and add modules
				modules := make([]godi.ModuleOption, bm.moduleCount)
				for j := 0; j < bm.moduleCount; j++ {
					modules[j] = createModule(fmt.Sprintf("module%d", j), bm.servicesEach)
				}

				_ = provider.AddModules(modules...)
				provider.Close()
			}
		})
	}
}

func BenchmarkDisposal(b *testing.B) {
	benchmarks := []struct {
		name          string
		serviceCount  int
		disposalRatio float64 // ratio of services that are disposable
	}{
		{"10_all_disposable", 10, 1.0},
		{"100_all_disposable", 100, 1.0},
		{"100_half_disposable", 100, 0.5},
		{"1000_tenth_disposable", 1000, 0.1},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				provider := godi.NewServiceProvider()

				// Add mix of disposable and non-disposable services
				for j := 0; j < bm.serviceCount; j++ {
					idx := j
					if float64(j)/float64(bm.serviceCount) < bm.disposalRatio {
						// Disposable service
						_ = provider.AddSingleton(func() *testutil.TestDisposable {
							return testutil.NewTestDisposable()
						})
					} else {
						// Non-disposable service
						_ = provider.AddSingleton(func() string {
							return fmt.Sprintf("service-%d", idx)
						})
					}
				}

				// Force creation of all services
				for j := 0; j < bm.serviceCount; j++ {
					if float64(j)/float64(bm.serviceCount) < bm.disposalRatio {
						_, _ = godi.Resolve[*testutil.TestDisposable](provider.GetRootScope())
					} else {
						_, _ = godi.Resolve[string](provider.GetRootScope())
					}
				}

				// Measure disposal time
				provider.Close()
			}
		})
	}
}

// Memory allocation benchmarks
func BenchmarkMemoryAllocation(b *testing.B) {
	b.Run("service_descriptor", func(b *testing.B) {
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			provider := godi.NewServiceProvider()
			_ = provider.AddSingleton(testutil.NewTestLogger)
		}
	})

	b.Run("provider_creation", func(b *testing.B) {
		provider := godi.NewServiceProvider()
		_ = provider.AddSingleton(testutil.NewTestLogger)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			provider.Close()
		}
	})

	b.Run("scope_creation", func(b *testing.B) {
		provider := godi.NewServiceProvider()
		_ = provider.AddScoped(testutil.NewTestService)
		defer provider.Close()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			scope := provider.CreateScope(context.Background())
			scope.Close()
		}
	})
}
