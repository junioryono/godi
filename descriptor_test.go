package godi

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Local types needed for descriptor-specific tests
type descriptorParamObj struct {
	In
	Svc *TService
}

type descriptorResultObj struct {
	Out
	Svc1 *TService
	Svc2 *TService `name:"named"`
}

func newDescriptorParamObj(params descriptorParamObj) *TService {
	return &TService{ID: params.Svc.ID + "-wrapped"}
}

func newDescriptorResultObj() descriptorResultObj {
	return descriptorResultObj{
		Svc1: &TService{ID: "svc1"},
		Svc2: &TService{ID: "svc2"},
	}
}

func TestDescriptor(t *testing.T) {
	t.Parallel()

	t.Run("NewDescriptor", func(t *testing.T) {
		t.Parallel()

		t.Run("basic", func(t *testing.T) {
			t.Parallel()
			d, err := newDescriptor(NewTService, Singleton)
			require.NoError(t, err)
			assert.NotNil(t, d)
			assert.Equal(t, PtrTypeOf[TService](), d.Type)
			assert.Equal(t, Singleton, d.Lifetime)
			assert.NotNil(t, d.Constructor)
			assert.NotNil(t, d.ConstructorType)
		})

		t.Run("with_error_return", func(t *testing.T) {
			t.Parallel()
			d, err := newDescriptor(NewTServiceWithError, Scoped)
			require.NoError(t, err)
			assert.Equal(t, Scoped, d.Lifetime)
			assert.Equal(t, 2, d.ConstructorType.NumOut())
		})

		t.Run("all_lifetimes", func(t *testing.T) {
			t.Parallel()
			for _, lt := range []Lifetime{Singleton, Scoped, Transient} {
				d, err := newDescriptor(NewTService, lt)
				require.NoError(t, err)
				assert.Equal(t, lt, d.Lifetime)
				assert.NoError(t, d.Validate())
			}
		})

		t.Run("with_name", func(t *testing.T) {
			t.Parallel()
			d, err := newDescriptor(NewTService, Transient, Name("test"))
			require.NoError(t, err)
			assert.Equal(t, "test", d.Key)
		})

		t.Run("with_group", func(t *testing.T) {
			t.Parallel()
			d, err := newDescriptor(NewTService, Singleton, Group("services"))
			require.NoError(t, err)
			assert.Equal(t, "services", d.Group)
		})

		t.Run("with_As", func(t *testing.T) {
			t.Parallel()
			d, err := newDescriptor(NewTService, Singleton, As[TInterface]())
			require.NoError(t, err)
			assert.NotNil(t, d)
		})

		t.Run("multi_return", func(t *testing.T) {
			t.Parallel()
			cases := []struct {
				name string
				ctor any
			}{
				{"two_returns", NewTMultiReturn},
				{"with_error", NewTMultiReturnWithError},
				{"triple", NewTTripleReturn},
			}
			for _, tc := range cases {
				t.Run(tc.name, func(t *testing.T) {
					d, err := newDescriptor(tc.ctor, Singleton)
					require.NoError(t, err)
					assert.Equal(t, PtrTypeOf[TService](), d.Type)
				})
			}
		})

		t.Run("param_object", func(t *testing.T) {
			t.Parallel()
			d, err := newDescriptor(newDescriptorParamObj, Singleton)
			require.NoError(t, err)
			assert.True(t, d.isParamObject)
			assert.NotNil(t, d.paramFields)
		})

		t.Run("result_object", func(t *testing.T) {
			t.Parallel()
			d, err := newDescriptor(newDescriptorResultObj, Singleton)
			require.NoError(t, err)
			assert.True(t, d.isResultObject)
			assert.NotNil(t, d.resultFields)
		})

		t.Run("non_function", func(t *testing.T) {
			t.Parallel()
			d, err := newDescriptor("not a function", Singleton)
			require.NoError(t, err)
			assert.Equal(t, reflect.TypeOf(""), d.ConstructorType)
		})

		t.Run("void_constructor", func(t *testing.T) {
			t.Parallel()
			d, err := newDescriptor(NewTVoid, Singleton)
			require.NoError(t, err)
			assert.NotNil(t, d)
		})
	})

	t.Run("NewDescriptor_Errors", func(t *testing.T) {
		t.Parallel()

		t.Run("nil", func(t *testing.T) {
			t.Parallel()
			d, err := newDescriptor(nil, Singleton)
			require.Error(t, err)
			var valErr *ValidationError
			assert.ErrorAs(t, err, &valErr)
			assert.ErrorIs(t, valErr.Cause, ErrConstructorNil)
			assert.Nil(t, d)
		})

		t.Run("name_and_group", func(t *testing.T) {
			t.Parallel()
			_, err := newDescriptor(NewTService, Singleton, Name("n"), Group("g"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "cannot use both")
		})

		t.Run("backtick_in_name", func(t *testing.T) {
			t.Parallel()
			_, err := newDescriptor(NewTService, Singleton, Name("test`name"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "backquotes")
		})

		t.Run("backtick_in_group", func(t *testing.T) {
			t.Parallel()
			_, err := newDescriptor(NewTService, Singleton, Group("test`group"))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "backquotes")
		})

		t.Run("invalid_As_pointer_to_pointer", func(t *testing.T) {
			t.Parallel()
			_, err := newDescriptor(NewTService, Singleton, As[*TInterface]())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "pointer to an interface")
		})

		t.Run("invalid_As_pointer_to_struct", func(t *testing.T) {
			t.Parallel()
			_, err := newDescriptor(NewTService, Singleton, As[*TService]())
			require.Error(t, err)
			assert.Contains(t, err.Error(), "pointer to an interface")
		})
	})

	t.Run("Getters", func(t *testing.T) {
		t.Parallel()

		t.Run("GetType", func(t *testing.T) {
			t.Parallel()
			d, _ := newDescriptor(NewTService, Singleton)
			assert.Equal(t, PtrTypeOf[TService](), d.GetType())
		})

		t.Run("GetKey", func(t *testing.T) {
			t.Parallel()
			d1, _ := newDescriptor(NewTService, Singleton)
			assert.Nil(t, d1.GetKey())

			d2, _ := newDescriptor(NewTService, Singleton, Name("k"))
			assert.Equal(t, "k", d2.GetKey())
		})

		t.Run("GetGroup", func(t *testing.T) {
			t.Parallel()
			d1, _ := newDescriptor(NewTService, Singleton)
			assert.Empty(t, d1.GetGroup())

			d2, _ := newDescriptor(NewTService, Singleton, Group("g"))
			assert.Equal(t, "g", d2.GetGroup())
		})

		t.Run("GetDependencies", func(t *testing.T) {
			t.Parallel()
			d1, _ := newDescriptor(NewTService, Singleton)
			assert.Empty(t, d1.GetDependencies())

			withDep := func(s *TService) *TDependency { return &TDependency{Name: s.ID} }
			d2, _ := newDescriptor(withDep, Singleton)
			deps := d2.GetDependencies()
			assert.Len(t, deps, 1)
			assert.Equal(t, PtrTypeOf[TService](), deps[0].Type)
		})
	})

	t.Run("Validate", func(t *testing.T) {
		t.Parallel()

		t.Run("valid", func(t *testing.T) {
			t.Parallel()
			d, _ := newDescriptor(NewTService, Singleton)
			assert.NoError(t, d.Validate())
		})

		t.Run("nil_type", func(t *testing.T) {
			t.Parallel()
			d := &Descriptor{
				Constructor:     reflect.ValueOf(NewTService),
				ConstructorType: reflect.TypeOf(NewTService),
				Lifetime:        Singleton,
			}
			err := d.Validate()
			require.Error(t, err)
			var valErr *ValidationError
			assert.ErrorAs(t, err, &valErr)
			assert.ErrorIs(t, valErr.Cause, ErrDescriptorNil)
		})

		t.Run("invalid_constructor", func(t *testing.T) {
			t.Parallel()
			d := &Descriptor{
				Type:            PtrTypeOf[TService](),
				Constructor:     reflect.Value{},
				ConstructorType: reflect.TypeOf(NewTService),
				Lifetime:        Singleton,
			}
			err := d.Validate()
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrConstructorNil)
		})

		t.Run("nil_constructor_type", func(t *testing.T) {
			t.Parallel()
			d := &Descriptor{
				Type:        PtrTypeOf[TService](),
				Constructor: reflect.ValueOf(NewTService),
				Lifetime:    Singleton,
			}
			err := d.Validate()
			require.Error(t, err)
			assert.ErrorIs(t, err, ErrConstructorNil)
		})

		t.Run("key_and_group", func(t *testing.T) {
			t.Parallel()
			d, _ := newDescriptor(NewTService, Singleton)
			d.Key = "key"
			d.Group = "group"
			err := d.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "cannot have both key and group")
		})

		t.Run("invalid_lifetime", func(t *testing.T) {
			t.Parallel()
			d, _ := newDescriptor(NewTService, Singleton)
			d.Lifetime = Lifetime(999)
			err := d.Validate()
			require.Error(t, err)
			assert.IsType(t, LifetimeError{}, err)
		})
	})

	t.Run("ComplexConstructors", func(t *testing.T) {
		t.Parallel()

		t.Run("multiple_deps", func(t *testing.T) {
			t.Parallel()
			type (
				S1 struct{}
				S2 struct{}
				S3 struct{}
			)
			ctor := func(*S1, *S2, *S3) *TService { return nil }
			d, _ := newDescriptor(ctor, Singleton)
			assert.Len(t, d.GetDependencies(), 3)
		})

		t.Run("optional_dep", func(t *testing.T) {
			t.Parallel()
			type OptParams struct {
				In
				Svc *TService
				Opt *TService `optional:"true"`
			}
			ctor := func(p OptParams) *TService { return nil }
			d, _ := newDescriptor(ctor, Singleton)
			assert.True(t, d.isParamObject)
		})

		t.Run("keyed_deps", func(t *testing.T) {
			t.Parallel()
			type KeyedParams struct {
				In
				Primary   *TService `name:"primary"`
				Secondary *TService `name:"secondary"`
			}
			ctor := func(p KeyedParams) *TService { return nil }
			d, _ := newDescriptor(ctor, Singleton)
			assert.True(t, d.isParamObject)
		})

		t.Run("group_deps", func(t *testing.T) {
			t.Parallel()
			type GroupParams struct {
				In
				Services []*TService `group:"services"`
			}
			ctor := func(p GroupParams) *TService { return nil }
			d, _ := newDescriptor(ctor, Singleton)
			assert.True(t, d.isParamObject)
		})
	})
}
