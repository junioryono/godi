package godi

import (
	"context"
	"reflect"
	"sync"
	"testing"
)

// Benchmark service types
type BenchmarkService1 struct{ ID int }
type BenchmarkService2 struct{ Dep1 *BenchmarkService1 }
type BenchmarkService3 struct {
	Dep1 *BenchmarkService1
	Dep2 *BenchmarkService2
}
type BenchmarkService4 struct {
	Dep1 *BenchmarkService1
	Dep2 *BenchmarkService2
	Dep3 *BenchmarkService3
}

func newBenchmarkService1() *BenchmarkService1 {
	return &BenchmarkService1{ID: 1}
}

func newBenchmarkService2(dep1 *BenchmarkService1) *BenchmarkService2 {
	return &BenchmarkService2{Dep1: dep1}
}

func newBenchmarkService3(dep1 *BenchmarkService1, dep2 *BenchmarkService2) *BenchmarkService3 {
	return &BenchmarkService3{Dep1: dep1, Dep2: dep2}
}

func newBenchmarkService4(dep1 *BenchmarkService1, dep2 *BenchmarkService2, dep3 *BenchmarkService3) *BenchmarkService4 {
	return &BenchmarkService4{Dep1: dep1, Dep2: dep2, Dep3: dep3}
}

// BenchmarkProviderBuild benchmarks provider building with various service counts
func BenchmarkProviderBuild(b *testing.B) {
	benchmarks := []struct {
		name  string
		setup func() Collection
	}{
		{
			name: "1Service",
			setup: func() Collection {
				c := NewCollection()
				c.AddSingleton(newBenchmarkService1)
				return c
			},
		},
		{
			name: "10Services",
			setup: func() Collection {
				c := NewCollection()
				for i := 0; i < 10; i++ {
					c.AddSingleton(func() *BenchmarkService1 { return &BenchmarkService1{ID: i} })
				}
				return c
			},
		},
		{
			name: "100Services",
			setup: func() Collection {
				c := NewCollection()
				for i := 0; i < 100; i++ {
					c.AddSingleton(func() *BenchmarkService1 { return &BenchmarkService1{ID: i} })
				}
				return c
			},
		},
		{
			name: "WithDependencies",
			setup: func() Collection {
				c := NewCollection()
				c.AddSingleton(newBenchmarkService1)
				c.AddSingleton(newBenchmarkService2)
				c.AddSingleton(newBenchmarkService3)
				c.AddSingleton(newBenchmarkService4)
				return c
			},
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				collection := bm.setup()
				provider, err := collection.Build()
				if err != nil {
					b.Fatal(err)
				}
				provider.Close()
			}
		})
	}
}

// BenchmarkSingletonResolution benchmarks singleton service resolution
func BenchmarkSingletonResolution(b *testing.B) {
	collection := NewCollection()
	collection.AddSingleton(newBenchmarkService1)
	collection.AddSingleton(newBenchmarkService2)

	provider, err := collection.Build()
	if err != nil {
		b.Fatal(err)
	}
	defer provider.Close()

	serviceType := reflect.TypeOf((*BenchmarkService1)(nil))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := provider.Get(serviceType)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkScopedResolution benchmarks scoped service resolution
func BenchmarkScopedResolution(b *testing.B) {
	collection := NewCollection()
	collection.AddScoped(newBenchmarkService1)
	collection.AddScoped(newBenchmarkService2)

	provider, err := collection.Build()
	if err != nil {
		b.Fatal(err)
	}
	defer provider.Close()

	scope, err := provider.CreateScope(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	defer scope.Close()

	serviceType := reflect.TypeOf((*BenchmarkService1)(nil))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := scope.Get(serviceType)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTransientResolution benchmarks transient service resolution
func BenchmarkTransientResolution(b *testing.B) {
	collection := NewCollection()
	collection.AddTransient(newBenchmarkService1)
	collection.AddTransient(newBenchmarkService2)

	provider, err := collection.Build()
	if err != nil {
		b.Fatal(err)
	}
	defer provider.Close()

	serviceType := reflect.TypeOf((*BenchmarkService1)(nil))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := provider.Get(serviceType)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkScopeCreation benchmarks scope creation
func BenchmarkScopeCreation(b *testing.B) {
	collection := NewCollection()
	collection.AddSingleton(newBenchmarkService1)
	collection.AddScoped(newBenchmarkService2)

	provider, err := collection.Build()
	if err != nil {
		b.Fatal(err)
	}
	defer provider.Close()

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scope, err := provider.CreateScope(ctx)
		if err != nil {
			b.Fatal(err)
		}
		scope.Close()
	}
}

// BenchmarkConcurrentResolution benchmarks concurrent service resolution
func BenchmarkConcurrentResolution(b *testing.B) {
	collection := NewCollection()
	collection.AddSingleton(newBenchmarkService1)
	collection.AddSingleton(newBenchmarkService2)
	collection.AddScoped(newBenchmarkService3)

	provider, err := collection.Build()
	if err != nil {
		b.Fatal(err)
	}
	defer provider.Close()

	serviceType1 := reflect.TypeOf((*BenchmarkService1)(nil))
	serviceType2 := reflect.TypeOf((*BenchmarkService2)(nil))

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			// Resolve singleton
			_, err := provider.Get(serviceType1)
			if err != nil {
				b.Fatal(err)
			}

			// Resolve another singleton
			_, err = provider.Get(serviceType2)
			if err != nil {
				b.Fatal(err)
			}

			// Create scope and resolve scoped service
			scope, err := provider.CreateScope(context.Background())
			if err != nil {
				b.Fatal(err)
			}
			_, err = scope.Get(reflect.TypeOf((*BenchmarkService3)(nil)))
			if err != nil {
				b.Fatal(err)
			}
			scope.Close()
		}
	})
}

// BenchmarkKeyedServiceResolution benchmarks keyed service resolution
func BenchmarkKeyedServiceResolution(b *testing.B) {
	collection := NewCollection()
	collection.AddSingleton(newBenchmarkService1, Name("service1"))
	collection.AddSingleton(newBenchmarkService1, Name("service2"))
	collection.AddSingleton(newBenchmarkService1, Name("service3"))

	provider, err := collection.Build()
	if err != nil {
		b.Fatal(err)
	}
	defer provider.Close()

	serviceType := reflect.TypeOf((*BenchmarkService1)(nil))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := "service" + string(rune('1'+(i%3)))
		_, err := provider.GetKeyed(serviceType, key)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkGroupResolution benchmarks group service resolution
func BenchmarkGroupResolution(b *testing.B) {
	collection := NewCollection()
	for i := 0; i < 10; i++ {
		collection.AddSingleton(newBenchmarkService1, Group("group1"))
	}

	provider, err := collection.Build()
	if err != nil {
		b.Fatal(err)
	}
	defer provider.Close()

	serviceType := reflect.TypeOf((*BenchmarkService1)(nil))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		services, err := provider.GetGroup(serviceType, "group1")
		if err != nil {
			b.Fatal(err)
		}
		if len(services) != 10 {
			b.Fatalf("expected 10 services, got %d", len(services))
		}
	}
}

// BenchmarkDeepDependencyChain benchmarks resolution of services with deep dependency chains
func BenchmarkDeepDependencyChain(b *testing.B) {
	collection := NewCollection()
	collection.AddSingleton(newBenchmarkService1)
	collection.AddSingleton(newBenchmarkService2)
	collection.AddSingleton(newBenchmarkService3)
	collection.AddSingleton(newBenchmarkService4)

	provider, err := collection.Build()
	if err != nil {
		b.Fatal(err)
	}
	defer provider.Close()

	serviceType := reflect.TypeOf((*BenchmarkService4)(nil))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := provider.Get(serviceType)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkParameterObject benchmarks services using parameter objects
func BenchmarkParameterObject(b *testing.B) {
	type ServiceParams struct {
		In
		Dep1 *BenchmarkService1
		Dep2 *BenchmarkService2
	}

	newService := func(params ServiceParams) *BenchmarkService3 {
		return &BenchmarkService3{
			Dep1: params.Dep1,
			Dep2: params.Dep2,
		}
	}

	collection := NewCollection()
	collection.AddSingleton(newBenchmarkService1)
	collection.AddSingleton(newBenchmarkService2)
	collection.AddScoped(newService)

	provider, err := collection.Build()
	if err != nil {
		b.Fatal(err)
	}
	defer provider.Close()

	serviceType := reflect.TypeOf((*BenchmarkService3)(nil))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scope, _ := provider.CreateScope(context.Background())
		_, err := scope.Get(serviceType)
		if err != nil {
			b.Fatal(err)
		}
		scope.Close()
	}
}

// BenchmarkResultObject benchmarks services using result objects
func BenchmarkResultObject(b *testing.B) {
	type ServiceResult struct {
		Out
		Service1 *BenchmarkService1
		Service2 *BenchmarkService2
	}

	newServices := func() ServiceResult {
		return ServiceResult{
			Service1: newBenchmarkService1(),
			Service2: &BenchmarkService2{},
		}
	}

	collection := NewCollection()
	collection.AddSingleton(newServices)

	provider, err := collection.Build()
	if err != nil {
		b.Fatal(err)
	}
	defer provider.Close()

	serviceType1 := reflect.TypeOf((*BenchmarkService1)(nil))
	serviceType2 := reflect.TypeOf((*BenchmarkService2)(nil))

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := provider.Get(serviceType1)
		if err != nil {
			b.Fatal(err)
		}
		_, err = provider.Get(serviceType2)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// Service interface for decorator benchmark
type BenchmarkServiceInterface interface {
	Execute() int
}

// BaseService for decorator benchmark
type BenchmarkBaseService struct{}

func (s *BenchmarkBaseService) Execute() int { return 1 }

// BenchmarkDecorator benchmarks decorator application
func BenchmarkDecorator(b *testing.B) {
	decorator := func(inner BenchmarkServiceInterface) BenchmarkServiceInterface {
		return &struct{ BenchmarkServiceInterface }{inner}
	}

	collection := NewCollection()
	collection.AddScoped(func() BenchmarkServiceInterface { return &BenchmarkBaseService{} })
	collection.Decorate(decorator)

	provider, err := collection.Build()
	if err != nil {
		b.Fatal(err)
	}
	defer provider.Close()

	serviceType := reflect.TypeOf((*BenchmarkServiceInterface)(nil)).Elem()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		scope, _ := provider.CreateScope(context.Background())
		_, err := scope.Get(serviceType)
		if err != nil {
			b.Fatal(err)
		}
		scope.Close()
	}
}

// BenchmarkMemoryAllocation benchmarks memory allocation during resolution
func BenchmarkMemoryAllocation(b *testing.B) {
	collection := NewCollection()
	collection.AddTransient(newBenchmarkService1)
	collection.AddTransient(newBenchmarkService2)
	collection.AddTransient(newBenchmarkService3)

	provider, err := collection.Build()
	if err != nil {
		b.Fatal(err)
	}
	defer provider.Close()

	serviceType := reflect.TypeOf((*BenchmarkService3)(nil))

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, err := provider.Get(serviceType)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkLargeServiceGraph benchmarks a large service graph
func BenchmarkLargeServiceGraph(b *testing.B) {
	collection := NewCollection()

	// Create a large interconnected service graph
	for i := 0; i < 50; i++ {
		idx := i
		collection.AddSingleton(func() *BenchmarkService1 {
			return &BenchmarkService1{ID: idx}
		})
	}

	provider, err := collection.Build()
	if err != nil {
		b.Fatal(err)
	}
	defer provider.Close()

	b.ResetTimer()
	var wg sync.WaitGroup
	for i := 0; i < b.N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			scope, _ := provider.CreateScope(context.Background())
			defer scope.Close()
		}()
	}
	wg.Wait()
}