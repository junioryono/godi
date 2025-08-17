package godi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"testing"
	"time"

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

// Multiple return constructors for testing
func NewMultipleServices() (*TestService, *TestServiceWithDep) {
	svc := &TestService{Name: "multi"}
	dep := &TestServiceWithDep{Service: svc}
	return svc, dep
}

func NewTripleServices() (*TestService, *TestServiceWithDep, *TestDisposable) {
	svc := &TestService{Name: "triple"}
	dep := &TestServiceWithDep{Service: svc}
	disp := &TestDisposable{}
	return svc, dep, disp
}

func NewMultipleServicesWithError() (*TestService, *TestServiceWithDep, error) {
	svc := &TestService{Name: "multi-error"}
	dep := &TestServiceWithDep{Service: svc}
	return svc, dep, nil
}

func NewQuadServices() (*TestService, *TestServiceWithDep, *TestDisposable, string) {
	svc := &TestService{Name: "quad"}
	dep := &TestServiceWithDep{Service: svc}
	disp := &TestDisposable{}
	return svc, dep, disp, "config"
}

func NewQuadServicesWithError() (*TestService, *TestServiceWithDep, *TestDisposable, string, error) {
	svc := &TestService{Name: "quad-error"}
	dep := &TestServiceWithDep{Service: svc}
	disp := &TestDisposable{}
	return svc, dep, disp, "config", nil
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

// Constructor that returns nothing (should fail)
func NewNothing() {
	// This should fail validation
}

// Constructor that returns multiple values (now valid with multi-return support)
func NewTooMany() (*TestService, *TestServiceWithDep, error) {
	return &TestService{Name: "many"}, &TestServiceWithDep{}, nil
}

// Constructor with multiple non-error returns (now valid with multi-return support)
func NewInvalidSecondReturn() (service *TestService, config string) {
	return &TestService{Name: "multi"}, "config"
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
}

// Test AddSingleton
func TestAddSingleton(t *testing.T) {
	t.Run("basic singleton", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewTestService)
		assert.NoError(t, err)
		assert.Equal(t, 1, collection.Count())
		assert.True(t, collection.Contains(reflect.TypeOf((*TestService)(nil))))
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
		assert.True(t, collection.ContainsKeyed(reflect.TypeOf((*TestService)(nil)), "primary"))
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
		assert.True(t, collection.Contains(reflect.TypeOf((*TestInterface)(nil)).Elem()))
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
		var valErr *ValidationError
		assert.ErrorAs(t, err, &valErr)
		assert.ErrorIs(t, valErr.Cause, ErrConstructorNil)
	})

	t.Run("non-function constructor should work as instance", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton("not a function", Name("str"))
		assert.NoError(t, err) // Instance registration is valid
		assert.True(t, collection.ContainsKeyed(reflect.TypeOf(""), "str"))
	})

	t.Run("constructor with no return should fail", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewNothing)
		assert.Error(t, err)
		var regErr *RegistrationError
		assert.ErrorAs(t, err, &regErr)
		var valErr *ValidationError
		assert.ErrorAs(t, regErr.Cause, &valErr)
		assert.ErrorIs(t, valErr.Cause, ErrConstructorNoReturn)
	})

	t.Run("constructor with multiple returns now valid", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewTooMany)
		assert.NoError(t, err) // Now valid with multi-return support

		// Both types should be registered
		assert.True(t, collection.Contains(reflect.TypeOf((*TestService)(nil))))
		assert.True(t, collection.Contains(reflect.TypeOf((*TestServiceWithDep)(nil))))
	})

	t.Run("constructor with multiple non-error returns now valid", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddSingleton(NewInvalidSecondReturn)
		assert.NoError(t, err) // Now valid with multi-return support

		// Both types should be registered
		assert.True(t, collection.Contains(reflect.TypeOf((*TestService)(nil))))
		assert.True(t, collection.Contains(reflect.TypeOf(""))) // string type
	})
}

// Test AddScoped
func TestAddScoped(t *testing.T) {
	t.Run("basic scoped", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddScoped(NewTestService)
		assert.NoError(t, err)
		assert.Equal(t, 1, collection.Count())
		assert.True(t, collection.Contains(reflect.TypeOf((*TestService)(nil))))
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
		assert.True(t, collection.ContainsKeyed(reflect.TypeOf((*TestService)(nil)), "scoped"))
	})

	t.Run("multiple scoped with different names", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddScoped(NewTestService, Name("scoped1"))
		assert.NoError(t, err)

		err = collection.AddScoped(NewTestService, Name("scoped2"))
		assert.NoError(t, err)

		assert.Equal(t, 2, collection.Count())
		assert.True(t, collection.ContainsKeyed(reflect.TypeOf((*TestService)(nil)), "scoped1"))
		assert.True(t, collection.ContainsKeyed(reflect.TypeOf((*TestService)(nil)), "scoped2"))
	})
}

// Test AddTransient
func TestAddTransient(t *testing.T) {
	t.Run("basic transient", func(t *testing.T) {
		collection := NewCollection()
		err := collection.AddTransient(NewTestService)
		assert.NoError(t, err)
		assert.Equal(t, 1, collection.Count())
		assert.True(t, collection.Contains(reflect.TypeOf((*TestService)(nil))))
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

// Test Contains
func TestContains(t *testing.T) {
	collection := NewCollection()
	serviceType := reflect.TypeOf((*TestService)(nil))

	assert.False(t, collection.Contains(serviceType))

	err := collection.AddSingleton(NewTestService)
	assert.NoError(t, err)

	assert.True(t, collection.Contains(serviceType))
}

// Test HasKeyedService
func TestHasKeyedService(t *testing.T) {
	collection := NewCollection()
	serviceType := reflect.TypeOf((*TestService)(nil))

	assert.False(t, collection.ContainsKeyed(serviceType, "test"))

	err := collection.AddSingleton(NewTestService, Name("test"))
	assert.NoError(t, err)

	assert.True(t, collection.ContainsKeyed(serviceType, "test"))
	assert.False(t, collection.ContainsKeyed(serviceType, "other"))
}

// Test Remove
func TestRemove(t *testing.T) {
	t.Run("remove existing service", func(t *testing.T) {
		collection := NewCollection()
		serviceType := reflect.TypeOf((*TestService)(nil))

		err := collection.AddSingleton(NewTestService)
		assert.NoError(t, err)
		assert.True(t, collection.Contains(serviceType))

		collection.Remove(serviceType)
		assert.False(t, collection.Contains(serviceType))
		assert.Equal(t, 0, collection.Count())
	})

	t.Run("remove non-existent service", func(t *testing.T) {
		collection := NewCollection()
		serviceType := reflect.TypeOf((*TestService)(nil))

		// Remove a service that was never added
		collection.Remove(serviceType)
		assert.False(t, collection.Contains(serviceType))
	})

	t.Run("remove with nil type", func(t *testing.T) {
		collection := NewCollection()

		// Should not panic with nil type
		collection.Remove(nil)
	})

	t.Run("remove only non-keyed services", func(t *testing.T) {
		collection := NewCollection()
		serviceType := reflect.TypeOf((*TestService)(nil))

		err := collection.AddSingleton(NewTestService)
		assert.NoError(t, err)
		err = collection.AddSingleton(NewTestService, Name("key1"))
		assert.NoError(t, err)

		collection.Remove(serviceType)
		assert.False(t, collection.Contains(serviceType))
		// Keyed services should remain
		assert.True(t, collection.ContainsKeyed(serviceType, "key1"))
	})
}

// Test RemoveKeyed
func TestRemoveKeyed(t *testing.T) {
	t.Run("remove existing keyed service", func(t *testing.T) {
		collection := NewCollection()
		serviceType := reflect.TypeOf((*TestService)(nil))

		err := collection.AddSingleton(NewTestService, Name("test"))
		assert.NoError(t, err)
		assert.True(t, collection.ContainsKeyed(serviceType, "test"))

		collection.RemoveKeyed(serviceType, "test")
		assert.False(t, collection.ContainsKeyed(serviceType, "test"))
		assert.Equal(t, 0, collection.Count())
	})

	t.Run("remove non-existent keyed service", func(t *testing.T) {
		collection := NewCollection()
		serviceType := reflect.TypeOf((*TestService)(nil))

		// Remove a keyed service that was never added
		collection.RemoveKeyed(serviceType, "nonexistent")
		assert.False(t, collection.ContainsKeyed(serviceType, "nonexistent"))
	})

	t.Run("remove with nil type or key", func(t *testing.T) {
		collection := NewCollection()
		serviceType := reflect.TypeOf((*TestService)(nil))

		// Should not panic with nil type
		collection.RemoveKeyed(nil, "key")

		// Should not panic with nil key
		collection.RemoveKeyed(serviceType, nil)
	})

	t.Run("preserve other keyed services", func(t *testing.T) {
		collection := NewCollection()
		serviceType := reflect.TypeOf((*TestService)(nil))

		err := collection.AddSingleton(NewTestService, Name("key1"))
		assert.NoError(t, err)
		err = collection.AddSingleton(NewTestService, Name("key2"))
		assert.NoError(t, err)

		collection.RemoveKeyed(serviceType, "key1")
		assert.False(t, collection.ContainsKeyed(serviceType, "key1"))
		assert.True(t, collection.ContainsKeyed(serviceType, "key2"))
	})
}

// Test ToSlice
func TestToSlice(t *testing.T) {
	t.Run("empty collection", func(t *testing.T) {
		collection := NewCollection()
		descriptors := collection.ToSlice()
		assert.Empty(t, descriptors)
	})

	t.Run("with services", func(t *testing.T) {
		collection := NewCollection()

		// Add services
		err := collection.AddSingleton(NewTestService)
		assert.NoError(t, err)

		err = collection.AddScoped(NewTestServiceWithDep)
		assert.NoError(t, err)

		descriptors := collection.ToSlice()
		assert.Len(t, descriptors, 2)

		// Verify descriptors
		for _, d := range descriptors {
			assert.NotNil(t, d)
			assert.NotNil(t, d.Type)
			assert.NotNil(t, d.Constructor)
		}
	})

	t.Run("with keyed and grouped services", func(t *testing.T) {
		collection := NewCollection()

		// Add various types of services
		err := collection.AddSingleton(NewTestService)
		assert.NoError(t, err)

		err = collection.AddSingleton(NewTestService, Name("keyed"))
		assert.NoError(t, err)

		err = collection.AddSingleton(NewTestService, Group("group1"))
		assert.NoError(t, err)

		descriptors := collection.ToSlice()
		assert.Len(t, descriptors, 3)

		// Count different types
		var regular, keyed, grouped int
		for _, d := range descriptors {
			switch {
			case d.Group != "":
				grouped++
			case d.Key != nil:
				keyed++
			default:
				regular++
			}
		}

		assert.Equal(t, 1, regular)
		assert.Equal(t, 1, keyed)
		assert.Equal(t, 1, grouped)
	})

	t.Run("descriptor immutability", func(t *testing.T) {
		collection := NewCollection()

		err := collection.AddSingleton(NewTestService)
		assert.NoError(t, err)

		// Get descriptors
		descriptors1 := collection.ToSlice()
		descriptors2 := collection.ToSlice()

		// Should return different slices
		assert.NotSame(t, &descriptors1, &descriptors2)

		// But contain the same data
		assert.Equal(t, descriptors1, descriptors2)
	})
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
		assert.Contains(t, err.Error(), "circular dependency detected")
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

	t.Run("build with timeout", func(t *testing.T) {
		collection := NewCollection()

		// Add a constructor that takes some time
		err := collection.AddSingleton(func() *TestService {
			// Simulate some work
			return &TestService{Name: "timeout-test"}
		})
		assert.NoError(t, err)

		// Set a reasonable timeout
		options := &ProviderOptions{
			BuildTimeout: 5 * time.Second,
		}
		provider, err := collection.BuildWithOptions(options)
		assert.NoError(t, err)
		assert.NotNil(t, provider)
		defer provider.Close()
	})

	t.Run("build with timeout exceeded", func(t *testing.T) {
		collection := NewCollection()

		// Add a constructor that blocks indefinitely
		blockChan := make(chan struct{})
		err := collection.AddSingleton(func() *TestService {
			<-blockChan // Block forever
			return &TestService{Name: "blocked"}
		})
		assert.NoError(t, err)

		// Set a very short timeout
		options := &ProviderOptions{
			BuildTimeout: 10 * time.Millisecond,
		}

		provider, err := collection.BuildWithOptions(options)
		assert.Error(t, err)
		assert.Nil(t, provider)

		// Verify it's a timeout error
		var timeoutErr *TimeoutError
		assert.True(t, errors.As(err, &timeoutErr))

		close(blockChan) // Clean up
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
			_ = collection.Contains(serviceType)
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
		var typeMismatchErr *TypeMismatchError
		assert.ErrorAs(t, err, &typeMismatchErr)
		assert.Equal(t, "interface implementation", typeMismatchErr.Context)
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

// Test types for validation tests
type ValidationServiceA struct {
	B *ValidationServiceB
}

type ValidationServiceB struct {
	C *ValidationServiceC
}

type ValidationServiceC struct {
	A *ValidationServiceA // Creates a cycle
}

type ValidationServiceD struct {
	// No dependencies
}

type ValidationServiceE struct {
	D *ValidationServiceD
}

// Constructor functions
func NewValidationServiceA(b *ValidationServiceB) *ValidationServiceA {
	return &ValidationServiceA{B: b}
}

func NewValidationServiceB(c *ValidationServiceC) *ValidationServiceB {
	return &ValidationServiceB{C: c}
}

func NewValidationServiceC(a *ValidationServiceA) *ValidationServiceC {
	return &ValidationServiceC{A: a}
}

func NewValidationServiceD() *ValidationServiceD {
	return &ValidationServiceD{}
}

func NewValidationServiceE(d *ValidationServiceD) *ValidationServiceE {
	return &ValidationServiceE{D: d}
}

// Test validateDependencyGraph
func TestValidateDependencyGraph(t *testing.T) {
	t.Run("valid dependency graph", func(t *testing.T) {
		c := NewCollection().(*collection)

		// Add services without cycles
		d1, err := newDescriptor(NewValidationServiceD, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		d2, err := newDescriptor(NewValidationServiceE, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		err = c.validateDependencyGraph()
		assert.NoError(t, err)
	})

	t.Run("circular dependency", func(t *testing.T) {
		c := NewCollection().(*collection)

		// Create circular dependency A -> B -> C -> A
		dA, err := newDescriptor(NewValidationServiceA, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: dA.Type}] = dA

		dB, err := newDescriptor(NewValidationServiceB, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: dB.Type}] = dB

		dC, err := newDescriptor(NewValidationServiceC, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: dC.Type}] = dC

		err = c.validateDependencyGraph()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circular dependency")
	})

	t.Run("self dependency", func(t *testing.T) {
		type SelfDependent struct {
			Self *SelfDependent
		}

		newSelfDependent := func(self *SelfDependent) *SelfDependent {
			return &SelfDependent{Self: self}
		}

		c := NewCollection().(*collection)

		d, err := newDescriptor(newSelfDependent, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d.Type}] = d

		err = c.validateDependencyGraph()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circular dependency")
	})

	t.Run("graph with groups", func(t *testing.T) {
		c := NewCollection().(*collection)

		// Add services to groups
		d1, err := newDescriptor(NewValidationServiceD, Singleton)
		require.NoError(t, err)
		d1.Group = "test-group"

		d2, err := newDescriptor(func() *ValidationServiceD {
			return &ValidationServiceD{}
		}, Singleton)
		require.NoError(t, err)
		d2.Group = "test-group"

		groupKey := GroupKey{
			Type:  reflect.TypeOf((*ValidationServiceD)(nil)),
			Group: "test-group",
		}
		c.groups[groupKey] = []*Descriptor{d1, d2}

		err = c.validateDependencyGraph()
		assert.NoError(t, err)
	})

	t.Run("empty collection", func(t *testing.T) {
		c := NewCollection().(*collection)

		err := c.validateDependencyGraph()
		assert.NoError(t, err)
	})

	t.Run("complex valid graph", func(t *testing.T) {
		// Create a more complex but valid dependency graph
		type Service1 struct{}
		type Service2 struct{ S1 *Service1 }
		type Service3 struct {
			S1 *Service1
			S2 *Service2
		}
		type Service4 struct {
			S2 *Service2
			S3 *Service3
		}

		c := NewCollection().(*collection)

		d1, err := newDescriptor(func() *Service1 { return &Service1{} }, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		d2, err := newDescriptor(func(s1 *Service1) *Service2 {
			return &Service2{S1: s1}
		}, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		d3, err := newDescriptor(func(s1 *Service1, s2 *Service2) *Service3 {
			return &Service3{S1: s1, S2: s2}
		}, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d3.Type}] = d3

		d4, err := newDescriptor(func(s2 *Service2, s3 *Service3) *Service4 {
			return &Service4{S2: s2, S3: s3}
		}, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d4.Type}] = d4

		err = c.validateDependencyGraph()
		assert.NoError(t, err)
	})
}

// Test validateLifetimes
func TestValidateLifetimes(t *testing.T) {
	t.Run("valid lifetimes", func(t *testing.T) {
		c := NewCollection().(*collection)

		// Singleton depending on singleton - OK
		d1, err := newDescriptor(NewValidationServiceD, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		d2, err := newDescriptor(NewValidationServiceE, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		err = c.validateLifetimes()
		assert.NoError(t, err)
	})

	t.Run("singleton depending on scoped", func(t *testing.T) {
		c := NewCollection().(*collection)

		// Scoped service
		d1, err := newDescriptor(NewValidationServiceD, Scoped)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		// Singleton depending on scoped - NOT OK
		d2, err := newDescriptor(NewValidationServiceE, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		err = c.validateLifetimes()
		assert.Error(t, err)
		var lifetimeConflictErr *LifetimeConflictError
		assert.ErrorAs(t, err, &lifetimeConflictErr)
		assert.Equal(t, Singleton, lifetimeConflictErr.Current)
		assert.Equal(t, Scoped, lifetimeConflictErr.Requested)
	})

	t.Run("singleton depending on transient", func(t *testing.T) {
		c := NewCollection().(*collection)

		// Transient service
		d1, err := newDescriptor(NewValidationServiceD, Transient)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		// Singleton depending on transient - OK (transient is created fresh each time)
		d2, err := newDescriptor(NewValidationServiceE, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		err = c.validateLifetimes()
		assert.NoError(t, err)
	})

	t.Run("scoped depending on singleton", func(t *testing.T) {
		c := NewCollection().(*collection)

		// Singleton service
		d1, err := newDescriptor(NewValidationServiceD, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		// Scoped depending on singleton - OK
		d2, err := newDescriptor(NewValidationServiceE, Scoped)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		err = c.validateLifetimes()
		assert.NoError(t, err)
	})

	t.Run("scoped depending on scoped", func(t *testing.T) {
		c := NewCollection().(*collection)

		// Scoped service
		d1, err := newDescriptor(NewValidationServiceD, Scoped)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		// Scoped depending on scoped - OK
		d2, err := newDescriptor(NewValidationServiceE, Scoped)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		err = c.validateLifetimes()
		assert.NoError(t, err)
	})

	t.Run("transient depending on scoped", func(t *testing.T) {
		c := NewCollection().(*collection)

		// Scoped service
		d1, err := newDescriptor(NewValidationServiceD, Scoped)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		// Transient depending on scoped - NOT OK (aligns with .NET DI rules)
		d2, err := newDescriptor(NewValidationServiceE, Transient)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		err = c.validateLifetimes()
		assert.Error(t, err)
		var lifetimeConflictErr *LifetimeConflictError
		assert.ErrorAs(t, err, &lifetimeConflictErr)
		assert.Equal(t, Transient, lifetimeConflictErr.Current)
		assert.Equal(t, Scoped, lifetimeConflictErr.Requested)
	})

	t.Run("transient depending on singleton", func(t *testing.T) {
		c := NewCollection().(*collection)

		// Singleton service
		d1, err := newDescriptor(NewValidationServiceD, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		// Transient depending on singleton - OK
		d2, err := newDescriptor(NewValidationServiceE, Transient)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		err = c.validateLifetimes()
		assert.NoError(t, err)
	})

	t.Run("transient depending on transient", func(t *testing.T) {
		c := NewCollection().(*collection)

		// Transient service
		d1, err := newDescriptor(NewValidationServiceD, Transient)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		// Transient depending on transient - OK
		d2, err := newDescriptor(NewValidationServiceE, Transient)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		err = c.validateLifetimes()
		assert.NoError(t, err)
	})

	t.Run("empty collection", func(t *testing.T) {
		c := NewCollection().(*collection)

		err := c.validateLifetimes()
		assert.NoError(t, err)
	})
}

// Test complex validation scenarios
func TestComplexValidationScenarios(t *testing.T) {
	t.Run("multiple dependency chains", func(t *testing.T) {
		// Create a complex but valid dependency structure
		type Logger struct{}
		type Database struct{ Log *Logger }
		type Cache struct{ Log *Logger }
		type Repository struct {
			DB    *Database
			Cache *Cache
		}
		type Service struct {
			Repo *Repository
			Log  *Logger
		}

		c := NewCollection().(*collection)

		// All singletons - should be valid
		descriptors := []struct {
			constructor any
			lifetime    Lifetime
		}{
			{func() *Logger { return &Logger{} }, Singleton},
			{func(log *Logger) *Database { return &Database{Log: log} }, Singleton},
			{func(log *Logger) *Cache { return &Cache{Log: log} }, Singleton},
			{func(db *Database, cache *Cache) *Repository {
				return &Repository{DB: db, Cache: cache}
			}, Singleton},
			{func(repo *Repository, log *Logger) *Service {
				return &Service{Repo: repo, Log: log}
			}, Singleton},
		}

		for _, desc := range descriptors {
			d, err := newDescriptor(desc.constructor, desc.lifetime)
			require.NoError(t, err)
			c.services[TypeKey{Type: d.Type}] = d
		}

		// Should pass both validations
		err := c.validateDependencyGraph()
		assert.NoError(t, err)

		err = c.validateLifetimes()
		assert.NoError(t, err)
	})

	t.Run("mixed lifetime validation", func(t *testing.T) {
		type Service1 struct{}
		type Service2 struct{ S1 *Service1 }
		type Service3 struct{ S2 *Service2 }

		c := NewCollection().(*collection)

		// Service1: Singleton
		d1, err := newDescriptor(func() *Service1 { return &Service1{} }, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		// Service2: Scoped, depends on Singleton (OK)
		d2, err := newDescriptor(func(s1 *Service1) *Service2 {
			return &Service2{S1: s1}
		}, Scoped)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		err = c.validateLifetimes()
		assert.NoError(t, err)

		// Service3: Transient, depends on Singleton (OK)
		d3, err := newDescriptor(func(s1 *Service1) *Service2 {
			return &Service2{S1: s1}
		}, Transient, Name("service3"))
		require.NoError(t, err)
		c.services[TypeKey{Type: d3.Type, Key: d3.Key}] = d3

		err = c.validateLifetimes()
		assert.NoError(t, err)

		// Service3: Transient, depends on Scoped (NOT OK)
		d4, err := newDescriptor(func(s2 *Service2) *Service3 {
			return &Service3{S2: s2}
		}, Transient)
		require.NoError(t, err)
		c.services[TypeKey{Type: d4.Type}] = d4

		err = c.validateLifetimes()
		assert.Error(t, err)
		var lifetimeConflictErr *LifetimeConflictError
		assert.ErrorAs(t, err, &lifetimeConflictErr)

		// Now change Service3 to Singleton - should fail
		d3.Lifetime = Singleton
		err = c.validateLifetimes()
		assert.Error(t, err)
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
			_ = collection.Contains(serviceType)
			_ = collection.Count()
		}
	})
}

// Benchmark validation
func BenchmarkValidateDependencyGraph(b *testing.B) {
	c := NewCollection().(*collection)

	// Add some services
	d1, _ := newDescriptor(NewValidationServiceD, Singleton)
	c.services[TypeKey{Type: d1.Type}] = d1

	d2, _ := newDescriptor(NewValidationServiceE, Singleton)
	c.services[TypeKey{Type: d2.Type}] = d2

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.validateDependencyGraph()
	}
}

func BenchmarkValidateLifetimes(b *testing.B) {
	c := NewCollection().(*collection)

	// Add some services
	d1, _ := newDescriptor(NewValidationServiceD, Singleton)
	c.services[TypeKey{Type: d1.Type}] = d1

	d2, _ := newDescriptor(NewValidationServiceE, Scoped)
	c.services[TypeKey{Type: d2.Type}] = d2

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = c.validateLifetimes()
	}
}

// Test multiple return constructor registration
func TestCollectionMultipleReturns(t *testing.T) {
	t.Run("register constructor with two returns", func(t *testing.T) {
		collection := NewCollection()

		err := collection.AddSingleton(NewMultipleServices)
		assert.NoError(t, err)

		// Both types should be registered
		assert.True(t, collection.Contains(reflect.TypeOf((*TestService)(nil))))
		assert.True(t, collection.Contains(reflect.TypeOf((*TestServiceWithDep)(nil))))

		// Build and resolve
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		svc1, err := provider.Get(reflect.TypeOf((*TestService)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, svc1)
		assert.Equal(t, "multi", svc1.(*TestService).Name)

		svc2, err := provider.Get(reflect.TypeOf((*TestServiceWithDep)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, svc2)
		assert.Same(t, svc1, svc2.(*TestServiceWithDep).Service)
	})

	t.Run("register constructor with three returns", func(t *testing.T) {
		collection := NewCollection()

		err := collection.AddScoped(NewTripleServices)
		assert.NoError(t, err)

		// All three types should be registered
		assert.True(t, collection.Contains(reflect.TypeOf((*TestService)(nil))))
		assert.True(t, collection.Contains(reflect.TypeOf((*TestServiceWithDep)(nil))))
		assert.True(t, collection.Contains(reflect.TypeOf((*TestDisposable)(nil))))

		// Build and resolve
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		scope, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope.Close()

		svc1, err := scope.Get(reflect.TypeOf((*TestService)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, svc1)

		svc2, err := scope.Get(reflect.TypeOf((*TestServiceWithDep)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, svc2)

		svc3, err := scope.Get(reflect.TypeOf((*TestDisposable)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, svc3)
	})

	t.Run("register constructor with multiple returns and error", func(t *testing.T) {
		collection := NewCollection()

		err := collection.AddTransient(NewMultipleServicesWithError)
		assert.NoError(t, err)

		// Both non-error types should be registered
		assert.True(t, collection.Contains(reflect.TypeOf((*TestService)(nil))))
		assert.True(t, collection.Contains(reflect.TypeOf((*TestServiceWithDep)(nil))))

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		svc1, err := provider.Get(reflect.TypeOf((*TestService)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, svc1)

		svc2, err := provider.Get(reflect.TypeOf((*TestServiceWithDep)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, svc2)
	})

	t.Run("register constructor with four returns", func(t *testing.T) {
		collection := NewCollection()

		err := collection.AddSingleton(NewQuadServices)
		assert.NoError(t, err)

		// All four types should be registered
		assert.True(t, collection.Contains(reflect.TypeOf((*TestService)(nil))))
		assert.True(t, collection.Contains(reflect.TypeOf((*TestServiceWithDep)(nil))))
		assert.True(t, collection.Contains(reflect.TypeOf((*TestDisposable)(nil))))
		assert.True(t, collection.Contains(reflect.TypeOf(""))) // string type
	})

	t.Run("register constructor with named option applies to first return", func(t *testing.T) {
		collection := NewCollection()

		err := collection.AddSingleton(NewMultipleServices, Name("primary"))
		assert.NoError(t, err)

		// First type should be keyed
		assert.True(t, collection.ContainsKeyed(reflect.TypeOf((*TestService)(nil)), "primary"))
		// Second type should not be keyed
		assert.False(t, collection.ContainsKeyed(reflect.TypeOf((*TestServiceWithDep)(nil)), "primary"))
		assert.True(t, collection.Contains(reflect.TypeOf((*TestServiceWithDep)(nil))))
	})

	t.Run("multiple returns maintain single constructor invocation", func(t *testing.T) {
		// Track invocations
		invocations := 0
		trackingConstructor := func() (*TestService, *TestServiceWithDep) {
			invocations++
			svc := &TestService{Name: "tracked"}
			dep := &TestServiceWithDep{Service: svc}
			return svc, dep
		}

		collection := NewCollection()
		err := collection.AddSingleton(trackingConstructor)
		assert.NoError(t, err)

		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()

		// Get both services
		svc1, err := provider.Get(reflect.TypeOf((*TestService)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, svc1)

		svc2, err := provider.Get(reflect.TypeOf((*TestServiceWithDep)(nil)))
		assert.NoError(t, err)
		assert.NotNil(t, svc2)

		// Constructor should only be invoked once for singletons
		assert.Equal(t, 1, invocations)

		// Services should be related (same instance)
		assert.Same(t, svc1, svc2.(*TestServiceWithDep).Service)
	})
}
