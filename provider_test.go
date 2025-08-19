package godi

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Test types for provider tests
type testService struct {
	ID    string
	Value int
}

type testDependency struct {
	Name string
}

type testServiceWithDep struct {
	Service *testService
	Dep     *testDependency
}

func TestResolve(t *testing.T) {
	t.Run("successful resolution", func(t *testing.T) {
		collection := NewCollection()
		collection.AddSingleton(func() *testService {
			return &testService{ID: "test", Value: 42}
		})

		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		service, err := Resolve[*testService](provider)
		assert.NoError(t, err, "Resolve should not fail")
		assert.NotNil(t, service, "Service should not be nil")
		assert.Equal(t, "test", service.ID, "Service ID should match")
		assert.Equal(t, 42, service.Value, "Service Value should match")
	})

	t.Run("nil provider", func(t *testing.T) {
		_, err := Resolve[*testService](nil)
		assert.ErrorIs(t, err, ErrProviderNil, "Expected ErrProviderNil for nil provider")
	})

	t.Run("service not found", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		_, err = Resolve[*testService](provider)
		assert.Error(t, err, "Expected error for unregistered service")
	})

	t.Run("type mismatch", func(t *testing.T) {
		collection := NewCollection()
		collection.AddSingleton(func() string {
			return "not a testService"
		})

		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		// Try to resolve as testService but string was registered
		_, err = Resolve[*testService](provider)
		assert.Error(t, err, "Expected type mismatch error")
	})
}

func TestMustResolve(t *testing.T) {
	t.Run("successful resolution", func(t *testing.T) {
		collection := NewCollection()
		collection.AddSingleton(func() *testService {
			return &testService{ID: "test", Value: 42}
		})

		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		service := MustResolve[*testService](provider)
		assert.Equal(t, "test", service.ID, "Service ID should match")
		assert.Equal(t, 42, service.Value, "Service Value should match")
	})

	t.Run("panics on error", func(t *testing.T) {
		defer func() {
			err := recover()
			assert.NotNil(t, err, "Expected panic but didn't get one")
		}()

		MustResolve[*testService](nil)
	})
}

func TestResolveKeyed(t *testing.T) {
	t.Run("successful resolution", func(t *testing.T) {
		collection := NewCollection()
		collection.AddSingleton(func() *testService {
			return &testService{ID: "keyed", Value: 100}
		}, Name("primary"))

		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		service, err := ResolveKeyed[*testService](provider, "primary")
		assert.NoError(t, err, "ResolveKeyed should not fail")
		assert.NotNil(t, service, "Service should not be nil")
		assert.Equal(t, "keyed", service.ID, "Service ID should match")
		assert.Equal(t, 100, service.Value, "Service Value should match")
	})

	t.Run("nil provider", func(t *testing.T) {
		_, err := ResolveKeyed[*testService](nil, "key")
		assert.ErrorIs(t, err, ErrProviderNil, "Expected ErrProviderNil for nil provider")
	})

	t.Run("nil key", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		_, err = ResolveKeyed[*testService](provider, nil)
		assert.ErrorIs(t, err, ErrServiceKeyNil, "Expected ErrServiceKeyNil for nil key")
	})

	t.Run("key not found", func(t *testing.T) {
		collection := NewCollection()
		collection.AddSingleton(func() *testService {
			return &testService{ID: "keyed", Value: 100}
		}, Name("primary"))

		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		_, err = ResolveKeyed[*testService](provider, "nonexistent")
		assert.Error(t, err, "Expected error for non-existent key")
	})

	t.Run("type mismatch", func(t *testing.T) {
		collection := NewCollection()
		collection.AddSingleton(func() string {
			return "not a testService"
		}, Name("key"))

		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		_, err = ResolveKeyed[*testService](provider, "key")
		assert.Error(t, err, "Expected type mismatch error")
	})
}

func TestMustResolveKeyed(t *testing.T) {
	t.Run("successful resolution", func(t *testing.T) {
		collection := NewCollection()
		collection.AddSingleton(func() *testService {
			return &testService{ID: "keyed", Value: 100}
		}, Name("primary"))

		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		service := MustResolveKeyed[*testService](provider, "primary")
		assert.Equal(t, "keyed", service.ID, "Service ID should match")
		assert.Equal(t, 100, service.Value, "Service Value should match")
	})

	t.Run("panics on error", func(t *testing.T) {
		defer func() {
			err := recover()
			assert.NotNil(t, err, "Expected panic but didn't get one")
		}()

		MustResolveKeyed[*testService](nil, "key")
	})
}

func TestResolveGroup(t *testing.T) {
	t.Run("successful resolution", func(t *testing.T) {
		collection := NewCollection()
		collection.AddSingleton(func() *testService {
			return &testService{ID: "svc1", Value: 1}
		}, Group("handlers"))
		collection.AddSingleton(func() *testService {
			return &testService{ID: "svc2", Value: 2}
		}, Group("handlers"))

		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		services, err := ResolveGroup[*testService](provider, "handlers")
		assert.NoError(t, err, "ResolveGroup should not fail")
		assert.Len(t, services, 2, "Expected 2 services in group")
		assert.Equal(t, "svc1", services[0].ID, "First service ID should match")
		assert.Equal(t, 1, services[0].Value, "First service Value should match")
		assert.Equal(t, "svc2", services[1].ID, "Second service ID should match")
		assert.Equal(t, 2, services[1].Value, "Second service Value should match")
	})

	t.Run("nil provider", func(t *testing.T) {
		_, err := ResolveGroup[*testService](nil, "group")
		assert.ErrorIs(t, err, ErrProviderNil, "Expected ErrProviderNil for nil provider")
	})

	t.Run("empty group name", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		_, err = ResolveGroup[*testService](provider, "")
		assert.Error(t, err, "Expected error for empty group name")
		assert.ErrorIs(t, err, ErrGroupNameEmpty, "Expected ErrGroupNameEmpty for empty group name")
		assert.IsType(t, &ValidationError{}, err, "Expected ValidationError for empty group name")
	})

	t.Run("group not found", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		services, err := ResolveGroup[*testService](provider, "nonexistent")
		assert.NoError(t, err, "ResolveGroup should not fail for non-existent group")
		assert.Len(t, services, 0, "Expected empty services for non-existent group")
	})

	t.Run("type mismatch in group", func(t *testing.T) {
		collection := NewCollection()
		collection.AddSingleton(func() string {
			return "not a testService"
		}, Group("handlers"))

		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		// Since group returns empty when no matching type found, not an error
		services, err := ResolveGroup[*testService](provider, "handlers")
		assert.NoError(t, err, "ResolveGroup should not fail for type mismatch")
		assert.Len(t, services, 0, "Expected empty services for type mismatch")
	})
}

func TestMustResolveGroup(t *testing.T) {
	t.Run("successful resolution", func(t *testing.T) {
		collection := NewCollection()
		collection.AddSingleton(func() *testService {
			return &testService{ID: "svc1", Value: 1}
		}, Group("handlers"))

		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		services := MustResolveGroup[*testService](provider, "handlers")
		assert.Len(t, services, 1, "Expected 1 service in group")
		assert.Equal(t, "svc1", services[0].ID, "Service ID should match")
		assert.Equal(t, 1, services[0].Value, "Service Value should match")
	})

	t.Run("panics on error", func(t *testing.T) {
		defer func() {
			err := recover()
			assert.NotNil(t, err, "Expected panic but didn't get one")
		}()

		MustResolveGroup[*testService](nil, "group")
	})
}

func TestProvider_ID(t *testing.T) {
	collection := NewCollection()
	provider, err := collection.Build()
	assert.NoError(t, err, "Build should not fail")
	defer provider.Close()

	id := provider.ID()
	assert.NotEmpty(t, id, "Expected non-empty ID")

	// ID should remain constant
	id2 := provider.ID()
	assert.Equal(t, id, id2, "Expected ID to remain constant")
}

func TestFromContext(t *testing.T) {
	t.Run("scope in context", func(t *testing.T) {
		collection := NewCollection()
		collection.AddScoped(func() *testService {
			return &testService{ID: "scoped", Value: 200}
		})

		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		ctx := context.Background()
		scope, err := provider.CreateScope(ctx)
		assert.NoError(t, err, "CreateScope should not fail")
		defer scope.Close()

		scope, err = FromContext(scope.Context())
		assert.NoError(t, err, "FromContext should not fail")
		assert.NotNil(t, scope, "Expected non-nil scope from context")
	})
}

func TestExtractParameterTypes(t *testing.T) {
	t.Run("nil info", func(t *testing.T) {
		types := extractParameterTypes(nil)
		assert.Nil(t, types, "Expected nil for nil info")
	})

	t.Run("with parameters through multi-return", func(t *testing.T) {
		collection := NewCollection()

		// Add dependencies first
		collection.AddSingleton(func() *testService {
			return &testService{ID: "base", Value: 1}
		})
		collection.AddSingleton(func() *testDependency {
			return &testDependency{Name: "dep"}
		})

		// Add a constructor with dependencies
		collection.AddSingleton(func(s *testService, d *testDependency) *testServiceWithDep {
			return &testServiceWithDep{Service: s, Dep: d}
		})

		// Build will internally use extractParameterTypes for multi-return singletons
		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		// Verify the service was created correctly
		service, err := Resolve[*testServiceWithDep](provider)
		assert.NoError(t, err, "Resolve should not fail")
		assert.NotNil(t, service, "Service should not be nil")
		assert.Equal(t, "base", service.Service.ID, "Service ID should match")
		assert.Equal(t, 1, service.Service.Value, "Service Value should match")
		assert.Equal(t, "dep", service.Dep.Name, "Dependency Name should match")
	})
}

// Test multi-return constructors which use extractParameterTypes internally
func TestMultiReturnConstructor(t *testing.T) {
	collection := NewCollection()

	// Multi-return constructor
	collection.AddSingleton(func() (*testService, *testDependency, error) {
		return &testService{ID: "multi", Value: 300}, &testDependency{Name: "dep"}, nil
	})

	provider, err := collection.Build()
	assert.NoError(t, err, "Build should not fail")
	defer provider.Close()

	service, err := Resolve[*testService](provider)
	assert.NoError(t, err, "Resolve should not fail")
	assert.NotNil(t, service, "Service should not be nil")
	assert.Equal(t, "multi", service.ID, "Service ID should match")
	assert.Equal(t, 300, service.Value, "Service Value should match")

	dep, err := Resolve[*testDependency](provider)
	assert.NoError(t, err, "Resolve dependency should not fail")
	assert.NotNil(t, dep, "Dependency should not be nil")
	assert.Equal(t, "dep", dep.Name, "Dependency Name should match")
}

func TestProviderGetMethods(t *testing.T) {
	t.Run("Get with nil type", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		_, err = provider.Get(nil)
		assert.ErrorIs(t, err, ErrServiceTypeNil, "Expected ErrServiceTypeNil for nil type")
	})

	t.Run("GetKeyed with nil type", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		_, err = provider.GetKeyed(nil, "key")
		assert.ErrorIs(t, err, ErrServiceTypeNil, "Expected ErrServiceTypeNil for nil type")
	})

	t.Run("GetGroup with nil type", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		defer provider.Close()

		_, err = provider.GetGroup(nil, "group")
		assert.ErrorIs(t, err, ErrServiceTypeNil, "Expected ErrServiceTypeNil for nil type")
	})

	t.Run("Get after disposal", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		provider.Close()

		_, err = provider.Get(reflect.TypeOf((*testService)(nil)))
		assert.ErrorIs(t, err, ErrProviderDisposed, "Expected ErrProviderDisposed after Close")
	})

	t.Run("GetKeyed after disposal", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		provider.Close()

		_, err = provider.GetKeyed(reflect.TypeOf((*testService)(nil)), "key")
		assert.ErrorIs(t, err, ErrProviderDisposed, "Expected ErrProviderDisposed after Close")
	})

	t.Run("GetGroup after disposal", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		provider.Close()

		_, err = provider.GetGroup(reflect.TypeOf((*testService)(nil)), "group")
		assert.ErrorIs(t, err, ErrProviderDisposed, "Expected ErrProviderDisposed after Close")
	})

	t.Run("CreateScope after disposal", func(t *testing.T) {
		collection := NewCollection()
		provider, err := collection.Build()
		assert.NoError(t, err, "Build should not fail")
		provider.Close()

		_, err = provider.CreateScope(context.Background())
		assert.ErrorIs(t, err, ErrProviderDisposed, "Expected ErrProviderDisposed after Close")
	})
}
