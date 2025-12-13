package godi

import (
	"context"
	"reflect"
	"sync"
	"testing"
)

// Benchmark service types
type BenchService struct {
	Name string
}

type BenchDep1 struct{ Value int }
type BenchDep2 struct{ Value int }
type BenchDep3 struct{ Value int }
type BenchDep4 struct{ Value int }
type BenchDep5 struct{ Value int }

type BenchServiceWith1Dep struct {
	Dep1 *BenchDep1
}

type BenchServiceWith3Deps struct {
	Dep1 *BenchDep1
	Dep2 *BenchDep2
	Dep3 *BenchDep3
}

type BenchServiceWith5Deps struct {
	Dep1 *BenchDep1
	Dep2 *BenchDep2
	Dep3 *BenchDep3
	Dep4 *BenchDep4
	Dep5 *BenchDep5
}

// Constructors for benchmarks
func NewBenchService() *BenchService {
	return &BenchService{Name: "bench"}
}

func NewBenchDep1() *BenchDep1 { return &BenchDep1{Value: 1} }
func NewBenchDep2() *BenchDep2 { return &BenchDep2{Value: 2} }
func NewBenchDep3() *BenchDep3 { return &BenchDep3{Value: 3} }
func NewBenchDep4() *BenchDep4 { return &BenchDep4{Value: 4} }
func NewBenchDep5() *BenchDep5 { return &BenchDep5{Value: 5} }

func NewBenchServiceWith1Dep(dep1 *BenchDep1) *BenchServiceWith1Dep {
	return &BenchServiceWith1Dep{Dep1: dep1}
}

func NewBenchServiceWith3Deps(dep1 *BenchDep1, dep2 *BenchDep2, dep3 *BenchDep3) *BenchServiceWith3Deps {
	return &BenchServiceWith3Deps{Dep1: dep1, Dep2: dep2, Dep3: dep3}
}

func NewBenchServiceWith5Deps(dep1 *BenchDep1, dep2 *BenchDep2, dep3 *BenchDep3, dep4 *BenchDep4, dep5 *BenchDep5) *BenchServiceWith5Deps {
	return &BenchServiceWith5Deps{Dep1: dep1, Dep2: dep2, Dep3: dep3, Dep4: dep4, Dep5: dep5}
}

// setupBenchProvider creates a provider with the specified configuration
func setupBenchProvider(b *testing.B, lifetime Lifetime, deps int) Provider {
	b.Helper()

	c := NewCollection()

	// Add dependencies based on count
	if deps >= 1 {
		switch lifetime {
		case Singleton:
			c.AddSingleton(NewBenchDep1)
		case Scoped:
			c.AddScoped(NewBenchDep1)
		case Transient:
			c.AddTransient(NewBenchDep1)
		}
	}
	if deps >= 2 {
		switch lifetime {
		case Singleton:
			c.AddSingleton(NewBenchDep2)
		case Scoped:
			c.AddScoped(NewBenchDep2)
		case Transient:
			c.AddTransient(NewBenchDep2)
		}
	}
	if deps >= 3 {
		switch lifetime {
		case Singleton:
			c.AddSingleton(NewBenchDep3)
		case Scoped:
			c.AddScoped(NewBenchDep3)
		case Transient:
			c.AddTransient(NewBenchDep3)
		}
	}
	if deps >= 4 {
		switch lifetime {
		case Singleton:
			c.AddSingleton(NewBenchDep4)
		case Scoped:
			c.AddScoped(NewBenchDep4)
		case Transient:
			c.AddTransient(NewBenchDep4)
		}
	}
	if deps >= 5 {
		switch lifetime {
		case Singleton:
			c.AddSingleton(NewBenchDep5)
		case Scoped:
			c.AddScoped(NewBenchDep5)
		case Transient:
			c.AddTransient(NewBenchDep5)
		}
	}

	// Add main service
	switch deps {
	case 0:
		switch lifetime {
		case Singleton:
			c.AddSingleton(NewBenchService)
		case Scoped:
			c.AddScoped(NewBenchService)
		case Transient:
			c.AddTransient(NewBenchService)
		}
	case 1:
		switch lifetime {
		case Singleton:
			c.AddSingleton(NewBenchServiceWith1Dep)
		case Scoped:
			c.AddScoped(NewBenchServiceWith1Dep)
		case Transient:
			c.AddTransient(NewBenchServiceWith1Dep)
		}
	case 3:
		switch lifetime {
		case Singleton:
			c.AddSingleton(NewBenchServiceWith3Deps)
		case Scoped:
			c.AddScoped(NewBenchServiceWith3Deps)
		case Transient:
			c.AddTransient(NewBenchServiceWith3Deps)
		}
	case 5:
		switch lifetime {
		case Singleton:
			c.AddSingleton(NewBenchServiceWith5Deps)
		case Scoped:
			c.AddScoped(NewBenchServiceWith5Deps)
		case Transient:
			c.AddTransient(NewBenchServiceWith5Deps)
		}
	}

	p, err := c.Build()
	if err != nil {
		b.Fatalf("failed to build provider: %v", err)
	}

	b.Cleanup(func() {
		p.Close()
	})

	return p
}

// BenchmarkResolution tests resolution performance for different lifetimes and dependency counts
func BenchmarkResolution(b *testing.B) {
	cases := []struct {
		name     string
		lifetime Lifetime
		deps     int
		target   reflect.Type
	}{
		{"Singleton/0deps", Singleton, 0, reflect.TypeOf((*BenchService)(nil))},
		{"Singleton/1dep", Singleton, 1, reflect.TypeOf((*BenchServiceWith1Dep)(nil))},
		{"Singleton/3deps", Singleton, 3, reflect.TypeOf((*BenchServiceWith3Deps)(nil))},
		{"Singleton/5deps", Singleton, 5, reflect.TypeOf((*BenchServiceWith5Deps)(nil))},
		{"Scoped/0deps", Scoped, 0, reflect.TypeOf((*BenchService)(nil))},
		{"Scoped/1dep", Scoped, 1, reflect.TypeOf((*BenchServiceWith1Dep)(nil))},
		{"Scoped/3deps", Scoped, 3, reflect.TypeOf((*BenchServiceWith3Deps)(nil))},
		{"Scoped/5deps", Scoped, 5, reflect.TypeOf((*BenchServiceWith5Deps)(nil))},
		{"Transient/0deps", Transient, 0, reflect.TypeOf((*BenchService)(nil))},
		{"Transient/1dep", Transient, 1, reflect.TypeOf((*BenchServiceWith1Dep)(nil))},
		{"Transient/3deps", Transient, 3, reflect.TypeOf((*BenchServiceWith3Deps)(nil))},
		{"Transient/5deps", Transient, 5, reflect.TypeOf((*BenchServiceWith5Deps)(nil))},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			p := setupBenchProvider(b, tc.lifetime, tc.deps)
			scope, err := p.CreateScope(context.Background())
			if err != nil {
				b.Fatalf("failed to create scope: %v", err)
			}
			defer scope.Close()

			// Warm up the cache for scoped services
			_, _ = scope.Get(tc.target)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = scope.Get(tc.target)
			}
		})
	}
}

// BenchmarkConcurrentResolution tests concurrent resolution performance
func BenchmarkConcurrentResolution(b *testing.B) {
	cases := []struct {
		name     string
		lifetime Lifetime
		deps     int
		target   reflect.Type
	}{
		{"Singleton/5deps", Singleton, 5, reflect.TypeOf((*BenchServiceWith5Deps)(nil))},
		{"Scoped/5deps", Scoped, 5, reflect.TypeOf((*BenchServiceWith5Deps)(nil))},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			p := setupBenchProvider(b, tc.lifetime, tc.deps)

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				scope, err := p.CreateScope(context.Background())
				if err != nil {
					b.Errorf("failed to create scope: %v", err)
					return
				}
				defer scope.Close()

				// Warm up
				_, _ = scope.Get(tc.target)

				for pb.Next() {
					_, _ = scope.Get(tc.target)
				}
			})
		})
	}
}

// BenchmarkScopeCreation tests scope creation performance
func BenchmarkScopeCreation(b *testing.B) {
	cases := []struct {
		name string
		deps int
	}{
		{"0deps", 0},
		{"5deps", 5},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			p := setupBenchProvider(b, Scoped, tc.deps)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				scope, _ := p.CreateScope(context.Background())
				scope.Close()
			}
		})
	}
}

// BenchmarkScopeWithResolution tests the full scope lifecycle with resolution
func BenchmarkScopeWithResolution(b *testing.B) {
	cases := []struct {
		name   string
		deps   int
		target reflect.Type
	}{
		{"0deps", 0, reflect.TypeOf((*BenchService)(nil))},
		{"5deps", 5, reflect.TypeOf((*BenchServiceWith5Deps)(nil))},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			p := setupBenchProvider(b, Scoped, tc.deps)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				scope, _ := p.CreateScope(context.Background())
				_, _ = scope.Get(tc.target)
				scope.Close()
			}
		})
	}
}

// BenchmarkGenericResolve tests the generic Resolve function
func BenchmarkGenericResolve(b *testing.B) {
	c := NewCollection()
	c.AddSingleton(NewBenchService)
	p, _ := c.Build()
	defer p.Close()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = Resolve[*BenchService](p)
	}
}

// BenchmarkProviderBuild tests provider build performance
func BenchmarkProviderBuild(b *testing.B) {
	cases := []struct {
		name      string
		services  int
		singletons int
	}{
		{"10services/5singletons", 10, 5},
		{"50services/25singletons", 50, 25},
		{"100services/50singletons", 100, 50},
	}

	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				c := NewCollection()

				// Add singletons
				for j := 0; j < tc.singletons; j++ {
					c.AddSingleton(func() *BenchService {
						return &BenchService{Name: "singleton"}
					})
				}

				// Add scoped services
				for j := 0; j < tc.services-tc.singletons; j++ {
					c.AddScoped(func() *BenchDep1 {
						return &BenchDep1{Value: j}
					})
				}

				p, err := c.Build()
				if err != nil {
					b.Fatalf("failed to build: %v", err)
				}
				p.Close()
			}
		})
	}
}

// BenchmarkConcurrentScopeCreation tests concurrent scope creation
func BenchmarkConcurrentScopeCreation(b *testing.B) {
	p := setupBenchProvider(b, Scoped, 5)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			scope, _ := p.CreateScope(context.Background())
			scope.Close()
		}
	})
}

// BenchmarkMapVsSyncMap compares map with RWMutex vs sync.Map performance
// This helps us understand the potential improvement from switching to sync.Map
func BenchmarkMapVsSyncMap(b *testing.B) {
	type key struct {
		t reflect.Type
		k any
	}

	// Setup test data
	testType := reflect.TypeOf((*BenchService)(nil))
	testKey := key{t: testType, k: nil}
	testValue := &BenchService{Name: "test"}

	b.Run("RWMutex/Read", func(b *testing.B) {
		var mu sync.RWMutex
		m := make(map[key]any)
		m[testKey] = testValue

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				mu.RLock()
				_ = m[testKey]
				mu.RUnlock()
			}
		})
	})

	b.Run("SyncMap/Read", func(b *testing.B) {
		var m sync.Map
		m.Store(testKey, testValue)

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, _ = m.Load(testKey)
			}
		})
	})

	b.Run("RWMutex/ReadWrite", func(b *testing.B) {
		var mu sync.RWMutex
		m := make(map[key]any)

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				if i%10 == 0 {
					mu.Lock()
					m[testKey] = testValue
					mu.Unlock()
				} else {
					mu.RLock()
					_ = m[testKey]
					mu.RUnlock()
				}
				i++
			}
		})
	})

	b.Run("SyncMap/ReadWrite", func(b *testing.B) {
		var m sync.Map

		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				if i%10 == 0 {
					m.Store(testKey, testValue)
				} else {
					_, _ = m.Load(testKey)
				}
				i++
			}
		})
	})
}
