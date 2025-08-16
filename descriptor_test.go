package godi

import (
	"errors"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test types for descriptor tests
type DescriptorTestService struct {
	Value string
}

type DescriptorTestInterface interface {
	GetValue() string
}

func (d *DescriptorTestService) GetValue() string {
	return d.Value
}

// Constructor functions for descriptor tests
func NewDescriptorTestService() *DescriptorTestService {
	return &DescriptorTestService{Value: "test"}
}

func NewDescriptorTestServiceWithError() (*DescriptorTestService, error) {
	return &DescriptorTestService{Value: "test"}, nil
}

func NewDescriptorTestServiceReturnsError() (*DescriptorTestService, error) {
	return nil, errors.New("constructor error")
}

func NewDescriptorNoReturn() {
	// No return values
}

// Multiple return constructors for testing
func NewMultipleReturns() (*DescriptorTestService, *DescriptorTestInterface) {
	svc := &DescriptorTestService{Value: "multi"}
	var iface DescriptorTestInterface = svc
	return svc, &iface
}

func NewTripleReturns() (*DescriptorTestService, *DescriptorTestInterface, string) {
	svc := &DescriptorTestService{Value: "triple"}
	var iface DescriptorTestInterface = svc
	return svc, &iface, "config"
}

func NewMultipleReturnsWithError() (*DescriptorTestService, *DescriptorTestInterface, string, error) {
	svc := &DescriptorTestService{Value: "multi-error"}
	var iface DescriptorTestInterface = svc
	return svc, &iface, "config", nil
}

// Parameter object constructor
type DescriptorParamObject struct {
	In
	Service *DescriptorTestService
}

func NewWithParamObject(params DescriptorParamObject) *DescriptorTestService {
	return &DescriptorTestService{Value: params.Service.Value + "-wrapped"}
}

// Result object constructor
type DescriptorResultObject struct {
	Out
	Service1 *DescriptorTestService
	Service2 *DescriptorTestService `name:"named"`
}

func NewWithResultObject() DescriptorResultObject {
	return DescriptorResultObject{
		Service1: &DescriptorTestService{Value: "service1"},
		Service2: &DescriptorTestService{Value: "service2"},
	}
}

// Test newDescriptor
func TestNewDescriptor(t *testing.T) {
	t.Run("basic constructor", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton)
		require.NoError(t, err)
		assert.NotNil(t, descriptor)
		assert.Equal(t, reflect.TypeOf((*DescriptorTestService)(nil)), descriptor.Type)
		assert.Equal(t, Singleton, descriptor.Lifetime)
		assert.False(t, descriptor.IsDecorator)
		assert.NotNil(t, descriptor.Constructor)
		assert.NotNil(t, descriptor.ConstructorType)
	})

	t.Run("constructor with error return", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestServiceWithError, Scoped)
		require.NoError(t, err)
		assert.NotNil(t, descriptor)
		assert.Equal(t, Scoped, descriptor.Lifetime)
		assert.Equal(t, 2, descriptor.ConstructorType.NumOut())
	})

	t.Run("constructor with name option", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Transient, Name("test"))
		require.NoError(t, err)
		assert.NotNil(t, descriptor)
		assert.Equal(t, "test", descriptor.Key)
	})

	t.Run("constructor with group option", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton, Group("services"))
		require.NoError(t, err)
		assert.NotNil(t, descriptor)
		assert.Equal(t, "services", descriptor.Group)
	})

	t.Run("constructor with As option", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton, As(new(DescriptorTestInterface)))
		require.NoError(t, err)
		assert.NotNil(t, descriptor)
		// As option is handled at collection level, not descriptor level
	})

	t.Run("nil constructor", func(t *testing.T) {
		descriptor, err := newDescriptor(nil, Singleton)
		assert.Error(t, err)
		assert.Equal(t, ErrNilConstructor, err)
		assert.Nil(t, descriptor)
	})

	t.Run("non-function constructor", func(t *testing.T) {
		descriptor, err := newDescriptor("not a function", Singleton)
		assert.NoError(t, err)
		assert.NotNil(t, descriptor)
		assert.Equal(t, reflect.TypeOf("not a function"), descriptor.ConstructorType)
		assert.Equal(t, Singleton, descriptor.Lifetime)
		assert.False(t, descriptor.IsDecorator)
	})

	t.Run("constructor with no return", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorNoReturn, Singleton)
		assert.Error(t, err)
		assert.Equal(t, ErrConstructorNoReturn, err)
		assert.Nil(t, descriptor)
	})

	t.Run("constructor with multiple returns", func(t *testing.T) {
		descriptor, err := newDescriptor(NewMultipleReturns, Singleton)
		assert.NoError(t, err)
		assert.NotNil(t, descriptor)
		// Should default to first return type
		assert.Equal(t, reflect.TypeOf((*DescriptorTestService)(nil)), descriptor.Type)
	})

	t.Run("constructor with triple returns", func(t *testing.T) {
		descriptor, err := newDescriptor(NewTripleReturns, Singleton)
		assert.NoError(t, err)
		assert.NotNil(t, descriptor)
		// Should default to first return type
		assert.Equal(t, reflect.TypeOf((*DescriptorTestService)(nil)), descriptor.Type)
	})

	t.Run("constructor with multiple returns and error", func(t *testing.T) {
		descriptor, err := newDescriptor(NewMultipleReturnsWithError, Singleton)
		assert.NoError(t, err)
		assert.NotNil(t, descriptor)
		// Should default to first return type
		assert.Equal(t, reflect.TypeOf((*DescriptorTestService)(nil)), descriptor.Type)
	})

	t.Run("constructor with param object", func(t *testing.T) {
		descriptor, err := newDescriptor(NewWithParamObject, Singleton)
		require.NoError(t, err)
		assert.NotNil(t, descriptor)
		assert.True(t, descriptor.isParamObject)
		assert.NotNil(t, descriptor.paramFields)
	})

	t.Run("constructor with result object", func(t *testing.T) {
		descriptor, err := newDescriptor(NewWithResultObject, Singleton)
		require.NoError(t, err)
		assert.NotNil(t, descriptor)
		assert.True(t, descriptor.isResultObject)
		assert.NotNil(t, descriptor.resultFields)
	})

	t.Run("conflicting options", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton, Name("test"), Group("services"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot use both")
		assert.Nil(t, descriptor)
	})

	t.Run("invalid name with backtick", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton, Name("test`name"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "names cannot contain backquotes")
		assert.Nil(t, descriptor)
	})

	t.Run("invalid group with backtick", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton, Group("test`group"))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "group names cannot contain backquotes")
		assert.Nil(t, descriptor)
	})

	t.Run("invalid As with nil", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton, As(nil))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid godi.As(nil)")
		assert.Nil(t, descriptor)
	})

	t.Run("invalid As with non-pointer", func(t *testing.T) {
		var iface DescriptorTestInterface
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton, As(iface))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "argument must be a pointer to an interface")
		assert.Nil(t, descriptor)
	})

	t.Run("invalid As with non-interface pointer", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton, As(&DescriptorTestService{}))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "argument must be a pointer to an interface")
		assert.Nil(t, descriptor)
	})
}

// Test IsProvider
func TestIsProvider(t *testing.T) {
	t.Run("regular descriptor is provider", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton)
		require.NoError(t, err)
		assert.True(t, descriptor.IsProvider())
	})

	t.Run("decorator is not provider", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton)
		require.NoError(t, err)
		descriptor.IsDecorator = true
		assert.False(t, descriptor.IsProvider())
	})
}

// Test GetTargetType
func TestGetTargetType(t *testing.T) {
	t.Run("provider returns Type", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton)
		require.NoError(t, err)
		targetType := descriptor.GetTargetType()
		assert.Equal(t, descriptor.Type, targetType)
	})

	t.Run("decorator without DecoratedType returns Type", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton)
		require.NoError(t, err)
		descriptor.IsDecorator = true
		targetType := descriptor.GetTargetType()
		assert.Equal(t, descriptor.Type, targetType)
	})

	t.Run("decorator with DecoratedType returns DecoratedType", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton)
		require.NoError(t, err)
		descriptor.IsDecorator = true
		decoratedType := reflect.TypeOf((*DescriptorTestInterface)(nil)).Elem()
		descriptor.DecoratedType = decoratedType
		targetType := descriptor.GetTargetType()
		assert.Equal(t, decoratedType, targetType)
	})
}

// Test GetType
func TestGetType(t *testing.T) {
	descriptor, err := newDescriptor(NewDescriptorTestService, Singleton)
	require.NoError(t, err)

	serviceType := descriptor.GetType()
	assert.Equal(t, reflect.TypeOf((*DescriptorTestService)(nil)), serviceType)
}

// Test GetKey
func TestGetKey(t *testing.T) {
	t.Run("descriptor without key", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton)
		require.NoError(t, err)
		assert.Nil(t, descriptor.GetKey())
	})

	t.Run("descriptor with key", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton, Name("test"))
		require.NoError(t, err)
		assert.Equal(t, "test", descriptor.GetKey())
	})
}

// Test GetGroup
func TestGetGroup(t *testing.T) {
	t.Run("descriptor without group", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton)
		require.NoError(t, err)
		assert.Empty(t, descriptor.GetGroup())
	})

	t.Run("descriptor with group", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton, Group("services"))
		require.NoError(t, err)
		assert.Equal(t, "services", descriptor.GetGroup())
	})
}

// Test GetDependencies
func TestGetDependencies(t *testing.T) {
	t.Run("constructor with no dependencies", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton)
		require.NoError(t, err)
		deps := descriptor.GetDependencies()
		assert.NotNil(t, deps)
		assert.Empty(t, deps)
	})

	t.Run("constructor with dependencies", func(t *testing.T) {
		newWithDep := func(service *DescriptorTestService) *DescriptorTestService {
			return &DescriptorTestService{Value: service.Value + "-dep"}
		}

		descriptor, err := newDescriptor(newWithDep, Singleton)
		require.NoError(t, err)
		deps := descriptor.GetDependencies()
		assert.NotNil(t, deps)
		assert.Len(t, deps, 1)
		assert.Equal(t, reflect.TypeOf((*DescriptorTestService)(nil)), deps[0].Type)
	})
}

// Test Validate
func TestValidate(t *testing.T) {
	t.Run("valid descriptor", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton)
		require.NoError(t, err)
		err = descriptor.Validate()
		assert.NoError(t, err)
	})

	t.Run("nil descriptor", func(t *testing.T) {
		var descriptor *Descriptor
		err := descriptor.Validate()
		assert.Error(t, err)
		assert.Equal(t, ErrDescriptorNil, err)
	})

	t.Run("descriptor with nil type", func(t *testing.T) {
		descriptor := &Descriptor{
			Constructor:     reflect.ValueOf(NewDescriptorTestService),
			ConstructorType: reflect.TypeOf(NewDescriptorTestService),
			Lifetime:        Singleton,
		}
		err := descriptor.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "type cannot be nil")
	})

	t.Run("descriptor with invalid constructor", func(t *testing.T) {
		descriptor := &Descriptor{
			Type:            reflect.TypeOf((*DescriptorTestService)(nil)),
			Constructor:     reflect.Value{}, // Invalid value
			ConstructorType: reflect.TypeOf(NewDescriptorTestService),
			Lifetime:        Singleton,
		}
		err := descriptor.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "constructor is invalid")
	})

	t.Run("descriptor with nil constructor type", func(t *testing.T) {
		descriptor := &Descriptor{
			Type:        reflect.TypeOf((*DescriptorTestService)(nil)),
			Constructor: reflect.ValueOf(NewDescriptorTestService),
			Lifetime:    Singleton,
		}
		err := descriptor.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "constructor type cannot be nil")
	})

	t.Run("descriptor with both key and group", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton)
		require.NoError(t, err)
		descriptor.Key = "key"
		descriptor.Group = "group"
		err = descriptor.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot have both key and group")
	})

	t.Run("descriptor with invalid lifetime", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton)
		require.NoError(t, err)
		descriptor.Lifetime = Lifetime(999) // Invalid lifetime
		err = descriptor.Validate()
		assert.Error(t, err)
		assert.IsType(t, LifetimeError{}, err)
	})
}

// Test different lifetime values
func TestDescriptorLifetimes(t *testing.T) {
	t.Run("singleton lifetime", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Singleton)
		require.NoError(t, err)
		assert.Equal(t, Singleton, descriptor.Lifetime)
		err = descriptor.Validate()
		assert.NoError(t, err)
	})

	t.Run("scoped lifetime", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Scoped)
		require.NoError(t, err)
		assert.Equal(t, Scoped, descriptor.Lifetime)
		err = descriptor.Validate()
		assert.NoError(t, err)
	})

	t.Run("transient lifetime", func(t *testing.T) {
		descriptor, err := newDescriptor(NewDescriptorTestService, Transient)
		require.NoError(t, err)
		assert.Equal(t, Transient, descriptor.Lifetime)
		err = descriptor.Validate()
		assert.NoError(t, err)
	})
}

// Test complex constructors
func TestDescriptorComplexConstructors(t *testing.T) {
	t.Run("constructor with multiple dependencies", func(t *testing.T) {
		type Service1 struct{}
		type Service2 struct{}
		type Service3 struct{}

		newComplex := func(s1 *Service1, s2 *Service2, s3 *Service3) *DescriptorTestService {
			return &DescriptorTestService{Value: "complex"}
		}

		descriptor, err := newDescriptor(newComplex, Singleton)
		require.NoError(t, err)
		deps := descriptor.GetDependencies()
		assert.Len(t, deps, 3)
	})

	t.Run("constructor with optional dependencies", func(t *testing.T) {
		type OptionalParams struct {
			In
			Service1 *DescriptorTestService
			Service2 *DescriptorTestService `optional:"true"`
		}

		newWithOptional := func(params OptionalParams) *DescriptorTestService {
			val := "with-optional"
			if params.Service2 != nil {
				val += "-" + params.Service2.Value
			}
			return &DescriptorTestService{Value: val}
		}

		descriptor, err := newDescriptor(newWithOptional, Singleton)
		require.NoError(t, err)
		assert.True(t, descriptor.isParamObject)
	})

	t.Run("constructor with keyed dependencies", func(t *testing.T) {
		type KeyedParams struct {
			In
			Primary   *DescriptorTestService `name:"primary"`
			Secondary *DescriptorTestService `name:"secondary"`
		}

		newWithKeyed := func(params KeyedParams) *DescriptorTestService {
			return &DescriptorTestService{
				Value: params.Primary.Value + "-" + params.Secondary.Value,
			}
		}

		descriptor, err := newDescriptor(newWithKeyed, Singleton)
		require.NoError(t, err)
		assert.True(t, descriptor.isParamObject)
	})

	t.Run("constructor with group dependencies", func(t *testing.T) {
		type GroupParams struct {
			In
			Services []DescriptorTestService `group:"services"`
		}

		newWithGroup := func(params GroupParams) *DescriptorTestService {
			return &DescriptorTestService{
				Value: "grouped",
			}
		}

		descriptor, err := newDescriptor(newWithGroup, Singleton)
		require.NoError(t, err)
		assert.True(t, descriptor.isParamObject)
	})
}

// Test validateDescriptor
func TestValidateDescriptor(t *testing.T) {
	t.Run("valid descriptor", func(t *testing.T) {
		descriptor, err := newDescriptor(NewValidationServiceD, Singleton)
		require.NoError(t, err)

		err = descriptor.Validate()
		assert.NoError(t, err)
	})

	t.Run("descriptor with nil type", func(t *testing.T) {
		descriptor := &Descriptor{
			Type:            nil,
			Constructor:     reflect.ValueOf(NewValidationServiceD),
			ConstructorType: reflect.TypeOf(NewValidationServiceD),
			Lifetime:        Singleton,
		}

		err := descriptor.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "type cannot be nil")
	})

	t.Run("descriptor with invalid constructor", func(t *testing.T) {
		descriptor := &Descriptor{
			Type:            reflect.TypeOf((*ValidationServiceD)(nil)),
			Constructor:     reflect.Value{},
			ConstructorType: reflect.TypeOf(NewValidationServiceD),
			Lifetime:        Singleton,
		}

		err := descriptor.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "constructor is invalid")
	})

	t.Run("descriptor with invalid service lifetime", func(t *testing.T) {
		descriptor, err := newDescriptor(NewValidationServiceD, Singleton)
		require.NoError(t, err)

		descriptor.Lifetime = Lifetime(999)

		err = descriptor.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid service lifetime")
	})

	t.Run("all valid lifetimes", func(t *testing.T) {
		lifetimes := []Lifetime{Singleton, Scoped, Transient}

		for _, lt := range lifetimes {
			descriptor, err := newDescriptor(NewValidationServiceD, lt)
			require.NoError(t, err)

			err = descriptor.Validate()
			assert.NoError(t, err, "Lifetime %v should be valid", lt)
		}
	})
}

// Benchmark tests
func BenchmarkNewDescriptor(b *testing.B) {
	b.Run("simple constructor", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = newDescriptor(NewDescriptorTestService, Singleton)
		}
	})

	b.Run("constructor with options", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = newDescriptor(NewDescriptorTestService, Singleton, Name("test"), As(new(DescriptorTestInterface)))
		}
	})

	b.Run("constructor with param object", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = newDescriptor(NewWithParamObject, Singleton)
		}
	})

	b.Run("constructor with result object", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_, _ = newDescriptor(NewWithResultObject, Singleton)
		}
	})
}

func BenchmarkDescriptorValidate(b *testing.B) {
	descriptor, _ := newDescriptor(NewDescriptorTestService, Singleton)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = descriptor.Validate()
	}
}
