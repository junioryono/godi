package godi

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test types for collection tests
type TestService struct {
	Name string
}

type TestServiceWithDep struct {
	Service *TestService
}

type TestInterface interface {
	GetName() string
}

func (t *TestService) GetName() string {
	return t.Name
}

type TestDisposable struct {
	Closed bool
	mu     sync.Mutex
}

func (t *TestDisposable) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.Closed = true
	return nil
}

// Constructor functions for tests
func NewTestService() *TestService {
	return &TestService{Name: "test"}
}

func NewTestServiceWithName(name string) *TestService {
	return &TestService{Name: name}
}

func NewTestServiceWithDep(service *TestService) *TestServiceWithDep {
	return &TestServiceWithDep{Service: service}
}

func NewTestServiceWithError() (*TestService, error) {
	return nil, errors.New("test error")
}

func NewTestDisposable() *TestDisposable {
	return &TestDisposable{}
}

// Constructor with multiple return values
func NewMultipleServices() (*TestService, *TestServiceWithDep) {
	service := &TestService{Name: "multi"}
	return service, &TestServiceWithDep{Service: service}
}

// Constructor that returns nothing (should fail)
func NewNothing() {
	// This should fail validation
}

// Constructor that returns too many values (should fail)
func NewTooMany() (*TestService, *TestServiceWithDep, error) {
	return nil, nil, nil
}

// Constructor with invalid second return (should fail)
func NewInvalidSecondReturn() (*TestService, string) {
	return nil, ""
}

// Test NewCollection
func TestNewCollection(t *testing.T) {
	cl := NewCollection()
	assert.NotNil(t, cl)
	assert.Equal(t, 0, cl.Count())

	// Verify it's the correct type
	c, ok := cl.(*collection)
	assert.True(t, ok)
	assert.NotNil(t, c.services)
	assert.NotNil(t, c.groups)
	assert.NotNil(t, c.decorators)
}

// Test AddSingleton
func TestAddSingleton(t *testing.T) {
	t.Run("basic singleton", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewTestService)
		assert.NoError(t, err)
		assert.Equal(t, 1, collection.Count())
		assert.True(t, collection.HasService(reflect.TypeOf((*TestService)(nil))))
	})

	t.Run("singleton with error return", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewTestServiceWithError)
		assert.NoError(t, err)
		assert.Equal(t, 1, collection.Count())
	})

	t.Run("singleton with name", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewTestService, Name("primary"))
		assert.NoError(t, err)
		assert.True(t, collection.HasKeyedService(reflect.TypeOf((*TestService)(nil)), "primary"))
	})

	t.Run("singleton with group", func(t *testing.T) {
		cl := NewCollection()
		err := cl.AddSingleton(NewTestService, Group("services"))
		assert.NoError(t, err)

		c := cl.(*collection)
		assert.True(t, c.HasGroup(reflect.TypeOf((*TestService)(nil)), "services"))
	})

	t.Run("singleton with interface", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewTestService, As(new(TestInterface)))
		assert.NoError(t, err)
		// When using As, it registers under the interface type, not the concrete type
		assert.True(t, collection.HasService(reflect.TypeOf((*TestInterface)(nil)).Elem()))
	})

	t.Run("duplicate singleton should fail", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewTestService)
		assert.NoError(t, err)

		err = collection.AddSingleton(NewTestService)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "already registered")
	})

	t.Run("nil constructor should fail", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(nil)
		assert.Error(t, err)
		assert.Equal(t, ErrNilConstructor, err)
	})

	t.Run("non-function constructor should fail", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton("not a function")
		assert.Error(t, err)
		assert.Equal(t, ErrConstructorNotFunction, err)
	})

	t.Run("constructor with no return should fail", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewNothing)
		assert.Error(t, err)
		assert.Equal(t, ErrConstructorNoReturn, err)
	})

	t.Run("constructor with too many returns should fail", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewTooMany)
		assert.Error(t, err)
		assert.Equal(t, ErrConstructorTooManyReturns, err)
	})

	t.Run("constructor with invalid second return should fail", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewInvalidSecondReturn)
		assert.Error(t, err)
		assert.Equal(t, ErrConstructorInvalidSecondReturn, err)
	})
}

// Test AddScoped
func TestAddScoped(t *testing.T) {
	t.Run("basic scoped", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddScoped(NewTestService)
		assert.NoError(t, err)
		assert.Equal(t, 1, collection.Count())
		assert.True(t, collection.HasService(reflect.TypeOf((*TestService)(nil))))
	})

	t.Run("scoped with dependencies", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddScoped(NewTestService)
		assert.NoError(t, err)

		err = collection.AddScoped(NewTestServiceWithDep)
		assert.NoError(t, err)
		assert.Equal(t, 2, collection.Count())
	})

	t.Run("scoped with name", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddScoped(NewTestService, Name("scoped"))
		assert.NoError(t, err)
		assert.True(t, collection.HasKeyedService(reflect.TypeOf((*TestService)(nil)), "scoped"))
	})

	t.Run("multiple scoped with different names", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddScoped(NewTestService, Name("scoped1"))
		assert.NoError(t, err)

		err = collection.AddScoped(NewTestService, Name("scoped2"))
		assert.NoError(t, err)

		assert.Equal(t, 2, collection.Count())
		assert.True(t, collection.HasKeyedService(reflect.TypeOf((*TestService)(nil)), "scoped1"))
		assert.True(t, collection.HasKeyedService(reflect.TypeOf((*TestService)(nil)), "scoped2"))
	})
}

// Test AddTransient
func TestAddTransient(t *testing.T) {
	t.Run("basic transient", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddTransient(NewTestService)
		assert.NoError(t, err)
		assert.Equal(t, 1, collection.Count())
		assert.True(t, collection.HasService(reflect.TypeOf((*TestService)(nil))))
	})

	t.Run("transient with disposable", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddTransient(NewTestDisposable)
		assert.NoError(t, err)
		assert.Equal(t, 1, collection.Count())
	})

	t.Run("transient in group", func(t *testing.T) {
		cl := NewCollection()
		err := cl.AddTransient(NewTestService, Group("transients"))
		assert.NoError(t, err)

		c := cl.(*collection)
		assert.True(t, c.HasGroup(reflect.TypeOf((*TestService)(nil)), "transients"))
	})
}

// Test Decorate
func TestDecorate(t *testing.T) {
	t.Run("basic decorator", func(t *testing.T) {
		collection := NewCollection()

		// Add a service first
		err := collection.AddSingleton(NewTestService)
		assert.NoError(t, err)

		// Add a decorator (currently returns nil - implementation needed)
		decorator := func(service *TestService) *TestService {
			service.Name = "decorated"
			return service
		}

		err = collection.Decorate(decorator)
		// Current implementation returns nil
		assert.NoError(t, err)
	})
}

// Test HasService
func TestHasService(t *testing.T) {
	collection := NewCollection()
	serviceType := reflect.TypeOf((*TestService)(nil))

	assert.False(t, collection.HasService(serviceType))

	err := collection.AddSingleton(NewTestService)
	assert.NoError(t, err)

	assert.True(t, collection.HasService(serviceType))
}

// Test HasKeyedService
func TestHasKeyedService(t *testing.T) {
	collection := NewCollection()
	serviceType := reflect.TypeOf((*TestService)(nil))

	assert.False(t, collection.HasKeyedService(serviceType, "test"))

	err := collection.AddSingleton(NewTestService, Name("test"))
	assert.NoError(t, err)

	assert.True(t, collection.HasKeyedService(serviceType, "test"))
	assert.False(t, collection.HasKeyedService(serviceType, "other"))
}

// Test Remove
func TestRemove(t *testing.T) {
	collection := NewCollection()
	serviceType := reflect.TypeOf((*TestService)(nil))

	err := collection.AddSingleton(NewTestService)
	assert.NoError(t, err)
	assert.True(t, collection.HasService(serviceType))

	collection.Remove(serviceType)
	assert.False(t, collection.HasService(serviceType))
	assert.Equal(t, 0, collection.Count())
}

// Test RemoveKeyed
func TestRemoveKeyed(t *testing.T) {
	collection := NewCollection()
	serviceType := reflect.TypeOf((*TestService)(nil))

	err := collection.AddSingleton(NewTestService, Name("test"))
	assert.NoError(t, err)
	assert.True(t, collection.HasKeyedService(serviceType, "test"))

	collection.RemoveKeyed(serviceType, "test")
	assert.False(t, collection.HasKeyedService(serviceType, "test"))
	assert.Equal(t, 0, collection.Count())
}

// Test ToSlice
func TestToSlice(t *testing.T) {
	collection := NewCollection()

	// Empty collection
	descriptors := collection.ToSlice()
	assert.Empty(t, descriptors)

	// Add services
	err := collection.AddSingleton(NewTestService)
	assert.NoError(t, err)

	err = collection.AddScoped(NewTestServiceWithDep)
	assert.NoError(t, err)

	descriptors = collection.ToSlice()
	assert.Len(t, descriptors, 2)

	// Verify descriptors
	for _, d := range descriptors {
		assert.NotNil(t, d)
		assert.NotNil(t, d.Type)
		assert.NotNil(t, d.Constructor)
	}
}

// Test Count
func TestCount(t *testing.T) {
	collection := NewCollection()
	assert.Equal(t, 0, collection.Count())

	err := collection.AddSingleton(NewTestService)
	assert.NoError(t, err)
	assert.Equal(t, 1, collection.Count())

	err = collection.AddScoped(NewTestServiceWithDep)
	assert.NoError(t, err)
	assert.Equal(t, 2, collection.Count())

	// Add to group
	err = collection.AddTransient(NewTestService, Group("group1"))
	assert.NoError(t, err)
	assert.Equal(t, 3, collection.Count())

	// Add another to same group
	err = collection.AddTransient(NewTestServiceWithDep, Group("group1"))
	assert.NoError(t, err)
	assert.Equal(t, 4, collection.Count())
}

// Test AddModules
func TestAddModules(t *testing.T) {
	t.Run("single module", func(t *testing.T) {
		collection := NewCollection()

		module := NewModule("test",
			AddSingleton(NewTestService),
			AddScoped(NewTestServiceWithDep),
		)

		err := collection.AddModules(module)
		assert.NoError(t, err)
		assert.Equal(t, 2, collection.Count())
	})

	t.Run("multiple modules", func(t *testing.T) {
		collection := NewCollection()

		module1 := NewModule("module1",
			AddSingleton(NewTestService),
		)

		module2 := NewModule("module2",
			AddScoped(NewTestServiceWithDep),
		)

		err := collection.AddModules(module1, module2)
		assert.NoError(t, err)
		assert.Equal(t, 2, collection.Count())
	})

	t.Run("nested modules", func(t *testing.T) {
		collection := NewCollection()

		innerModule := NewModule("inner",
			AddSingleton(NewTestService),
		)

		outerModule := NewModule("outer",
			innerModule,
			AddScoped(NewTestServiceWithDep),
		)

		err := collection.AddModules(outerModule)
		assert.NoError(t, err)
		assert.Equal(t, 2, collection.Count())
	})

	t.Run("module with error", func(t *testing.T) {
		collection := NewCollection()

		// Add a service
		err := collection.AddSingleton(NewTestService)
		assert.NoError(t, err)

		// Module that tries to add duplicate
		module := NewModule("error",
			AddSingleton(NewTestService), // This should fail
		)

		err = collection.AddModules(module)
		assert.Error(t, err)
		assert.IsType(t, ModuleError{}, err)
	})

	t.Run("nil module", func(t *testing.T) {
		collection := NewCollection()

		err := collection.AddModules(nil)
		assert.NoError(t, err) // Should handle nil gracefully
	})
}

// Create circular dependency
type ServiceA struct{ B *ServiceB }
type ServiceB struct{ A *ServiceA }

// Test Build
func TestBuild(t *testing.T) {
	t.Run("build empty collection", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		assert.NoError(t, err)
		assert.NotNil(t, provider)
		defer provider.Close()
	})

	t.Run("build with services", func(t *testing.T) {
		collection := NewCollection()

		err := collection.AddSingleton(NewTestService)
		assert.NoError(t, err)

		err = collection.AddScoped(NewTestServiceWithDep)
		assert.NoError(t, err)

		provider, err := collection.Build()
		assert.NoError(t, err)
		assert.NotNil(t, provider)
		defer provider.Close()

		// Verify we can resolve services
		service, err := provider.Get(reflect.TypeOf((*TestService)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, service)
	})

	t.Run("build with circular dependency", func(t *testing.T) {
		collection := NewCollection()

		newServiceA := func(b *ServiceB) *ServiceA { return &ServiceA{B: b} }
		newServiceB := func(a *ServiceA) *ServiceB { return &ServiceB{A: a} }

		err := collection.AddSingleton(newServiceA)
		assert.NoError(t, err)

		err = collection.AddSingleton(newServiceB)
		assert.NoError(t, err)

		_, err = collection.Build()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "dependency validation failed")
	})

	t.Run("build with lifetime violation", func(t *testing.T) {
		collection := NewCollection()

		// Scoped service
		err := collection.AddScoped(NewTestService)
		assert.NoError(t, err)

		// Singleton depending on scoped (should fail)
		newSingletonWithScoped := func(service *TestService) *TestServiceWithDep {
			return &TestServiceWithDep{Service: service}
		}

		err = collection.AddSingleton(newSingletonWithScoped)
		assert.NoError(t, err)

		_, err = collection.Build()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "lifetime validation failed")
	})

	t.Run("build with disposable singleton", func(t *testing.T) {
		collection := NewCollection()

		err := collection.AddSingleton(NewTestDisposable)
		assert.NoError(t, err)

		provider, err := collection.Build()
		assert.NoError(t, err)
		assert.NotNil(t, provider)

		// Get the disposable
		service, err := provider.Get(reflect.TypeOf((*TestDisposable)(nil)))
		assert.NoError(t, err)
		disposable := service.(*TestDisposable)
		assert.False(t, disposable.Closed)

		// Close provider should dispose
		err = provider.Close()
		assert.NoError(t, err)
		assert.True(t, disposable.Closed)
	})
}

// Test BuildWithOptions
func TestBuildWithOptions(t *testing.T) {
	t.Run("build with nil options", func(t *testing.T) {
		collection := NewCollection()

		err := collection.AddSingleton(NewTestService)
		assert.NoError(t, err)

		provider, err := collection.BuildWithOptions(nil)
		assert.NoError(t, err)
		assert.NotNil(t, provider)
		defer provider.Close()
	})

	t.Run("build with options", func(t *testing.T) {
		collection := NewCollection()

		err := collection.AddSingleton(NewTestService)
		assert.NoError(t, err)

		options := &ProviderOptions{}
		provider, err := collection.BuildWithOptions(options)
		assert.NoError(t, err)
		assert.NotNil(t, provider)
		defer provider.Close()
	})
}

// Test concurrent access
func TestCollectionConcurrency(t *testing.T) {
	collection := NewCollection()

	// Pre-add a service to avoid all goroutines trying to add the same type
	err := collection.AddSingleton(NewTestService)
	require.NoError(t, err)

	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Concurrent adds with different keys
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("service-%d", id)
			err := collection.AddSingleton(NewTestService, Name(key))
			if err != nil {
				errors <- err
			}
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			serviceType := reflect.TypeOf((*TestService)(nil))
			_ = collection.HasService(serviceType)
			_ = collection.Count()
			_ = collection.ToSlice()
		}(i)
	}

	// Concurrent removes
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			key := fmt.Sprintf("service-%d", id)
			serviceType := reflect.TypeOf((*TestService)(nil))
			collection.RemoveKeyed(serviceType, key)
		}(i)
	}

	wg.Wait()
	close(errors)

	// Check for errors
	for err := range errors {
		assert.NoError(t, err)
	}
}

// Test edge cases
func TestCollectionEdgeCases(t *testing.T) {
	t.Run("add service with both name and group should fail", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewTestService, Name("test"), Group("group"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot use both")
	})

	t.Run("add service with invalid interface in As", func(t *testing.T) {
		collection := NewCollection()

		// TestService doesn't implement fmt.Stringer
		err := collection.AddSingleton(NewTestService, As(new(fmt.Stringer)))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not implement interface")
	})

	t.Run("add service with nil in As", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewTestService, As(nil))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid godi.As(nil)")
	})

	t.Run("add service with non-pointer in As", func(t *testing.T) {
		collection := NewCollection()
		var iface TestInterface
		err := collection.AddSingleton(NewTestService, As(iface))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "argument must be a pointer to an interface")
	})

	t.Run("add service with non-interface pointer in As", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewTestService, As(&TestService{}))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "argument must be a pointer to an interface")
	})

	t.Run("name with backtick should fail", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewTestService, Name("test`name"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "names cannot contain backquotes")
	})

	t.Run("group with backtick should fail", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewTestService, Group("test`group"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "group names cannot contain backquotes")
	})
}

// Test parameter objects (In)
func TestParameterObjects(t *testing.T) {
	type ServiceParams struct {
		In

		Service *TestService
	}

	newServiceWithParams := func(params ServiceParams) *TestServiceWithDep {
		return &TestServiceWithDep{Service: params.Service}
	}

	t.Run("constructor with In parameter", func(t *testing.T) {
		collection := NewCollection()

		err := collection.AddSingleton(NewTestService)
		assert.NoError(t, err)

		err = collection.AddSingleton(newServiceWithParams)
		assert.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		service, err := provider.Get(reflect.TypeOf((*TestServiceWithDep)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, service)
	})
}

// Test result objects (Out)
func TestResultObjects(t *testing.T) {
	type ServiceResult struct {
		Out

		Service1 *TestService
		Service2 *TestServiceWithDep `name:"named"`
	}

	newServicesWithResult := func() ServiceResult {
		service := &TestService{Name: "result"}
		return ServiceResult{
			Service1: service,
			Service2: &TestServiceWithDep{Service: service},
		}
	}

	t.Run("constructor with Out result", func(t *testing.T) {
		collection := NewCollection()

		err := collection.AddSingleton(newServicesWithResult)
		assert.NoError(t, err)

		// Note: The actual behavior depends on the reflection package's handling of Out
		// This test verifies the registration doesn't fail
	})
}

// Test groups
func TestGroups(t *testing.T) {
	t.Run("multiple services in same group", func(t *testing.T) {
		cl := NewCollection()

		newService1 := func() *TestService { return &TestService{Name: "1"} }
		newService2 := func() *TestService { return &TestService{Name: "2"} }
		newService3 := func() *TestService { return &TestService{Name: "3"} }

		err := cl.AddSingleton(newService1, Group("handlers"))
		assert.NoError(t, err)

		err = cl.AddSingleton(newService2, Group("handlers"))
		assert.NoError(t, err)

		err = cl.AddTransient(newService3, Group("handlers"))
		assert.NoError(t, err)

		c := cl.(*collection)
		assert.True(t, c.HasGroup(reflect.TypeOf((*TestService)(nil)), "handlers"))

		// Build and verify group resolution
		provider, err := cl.Build()
		require.NoError(t, err)
		defer provider.Close()

		services, err := provider.GetGroup(reflect.TypeOf((*TestService)(nil)), "handlers")
		assert.NoError(t, err)
		assert.Len(t, services, 3)
	})
}

// Benchmark tests
func BenchmarkCollectionAdd(b *testing.B) {
	b.Run("AddSingleton", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			collection := NewCollection()
			_ = collection.AddSingleton(NewTestService)
		}
	})

	b.Run("AddScoped", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			collection := NewCollection()
			_ = collection.AddScoped(NewTestService)
		}
	})

	b.Run("AddTransient", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			collection := NewCollection()
			_ = collection.AddTransient(NewTestService)
		}
	})
}

func BenchmarkCollectionBuild(b *testing.B) {
	collection := NewCollection()
	_ = collection.AddSingleton(NewTestService)
	_ = collection.AddScoped(NewTestServiceWithDep)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		provider, _ := collection.Build()
		provider.Close()
	}
}

func BenchmarkCollectionConcurrentRead(b *testing.B) {
	collection := NewCollection()
	_ = collection.AddSingleton(NewTestService)
	serviceType := reflect.TypeOf((*TestService)(nil))

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = collection.HasService(serviceType)
			_ = collection.Count()
		}
	})
}
