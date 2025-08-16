package typecache

import (
	"reflect"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/dig"
)

// Test types for various scenarios
type SimpleStruct struct {
	Name string
	Age  int
}

type InterfaceType interface {
	Method() string
}

type ImplementationType struct {
	Value string
}

func (i *ImplementationType) Method() string {
	return i.Value
}

type StructWithTags struct {
	Field1 string `name:"custom" optional:"true"`
	Field2 int    `group:"numbers"`
	Field3 bool   `json:"field_3"`
}

type StructWithDigIn struct {
	dig.In

	Service1 InterfaceType
	Service2 *SimpleStruct `optional:"true"`
	Services []string      `group:"strings"`
}

type StructWithDigOut struct {
	dig.Out

	Result1 InterfaceType `name:"primary"`
	Result2 *SimpleStruct `group:"structs"`
	Result3 string
}

type ComplexStruct struct {
	InterfaceField InterfaceType
	PointerField   *SimpleStruct
	SliceField     []string
	MapField       map[string]int
	ChanField      chan bool
	FuncField      func(int) string
}

func TestGetTypeInfo_BasicTypes(t *testing.T) {
	t.Run("primitive types", func(t *testing.T) {
		tests := []struct {
			name        string
			typ         reflect.Type
			isPrimitive bool
			canBeNil    bool
		}{
			{"bool", reflect.TypeOf(true), true, false},
			{"int", reflect.TypeOf(42), true, false},
			{"int64", reflect.TypeOf(int64(42)), true, false},
			{"float64", reflect.TypeOf(3.14), true, false},
			{"string", reflect.TypeOf("test"), true, false},
			{"complex128", reflect.TypeOf(complex(1, 2)), true, false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				info := GetTypeInfo(tt.typ)

				assert.NotNil(t, info)
				assert.Equal(t, tt.typ, info.Type)
				assert.Equal(t, tt.isPrimitive, info.IsPrimitive)
				assert.Equal(t, tt.canBeNil, info.CanBeNil)
				assert.Equal(t, tt.typ.Kind(), info.Kind)
			})
		}
	})

	t.Run("composite types", func(t *testing.T) {
		tests := []struct {
			name     string
			typ      reflect.Type
			kind     reflect.Kind
			canBeNil bool
		}{
			{"slice", reflect.TypeOf([]int{}), reflect.Slice, true},
			{"map", reflect.TypeOf(map[string]int{}), reflect.Map, true},
			{"chan", reflect.TypeOf(make(chan int)), reflect.Chan, true},
			{"func", reflect.TypeOf(func() {}), reflect.Func, true},
			{"interface", reflect.TypeOf((*InterfaceType)(nil)).Elem(), reflect.Interface, true},
			{"pointer", reflect.TypeOf((*SimpleStruct)(nil)), reflect.Ptr, true},
			{"struct", reflect.TypeOf(SimpleStruct{}), reflect.Struct, false},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				info := GetTypeInfo(tt.typ)

				assert.NotNil(t, info)
				assert.Equal(t, tt.typ, info.Type)
				assert.Equal(t, tt.kind, info.Kind)
				assert.Equal(t, tt.canBeNil, info.CanBeNil)
				assert.False(t, info.IsPrimitive)
			})
		}
	})
}

func TestGetTypeInfo_StructAnalysis(t *testing.T) {
	t.Run("simple struct", func(t *testing.T) {
		typ := reflect.TypeOf(SimpleStruct{})
		info := GetTypeInfo(typ)

		assert.True(t, info.IsStruct)
		assert.Equal(t, 2, info.NumFields)
		assert.Len(t, info.Fields, 2)

		// Check field details
		nameField := info.Fields[0]
		assert.Equal(t, "Name", nameField.Name)
		assert.Equal(t, reflect.TypeOf(""), nameField.Type)
		assert.True(t, nameField.IsExported)
		assert.False(t, nameField.IsAnonymous)

		ageField := info.Fields[1]
		assert.Equal(t, "Age", ageField.Name)
		assert.Equal(t, reflect.TypeOf(0), ageField.Type)
		assert.True(t, ageField.IsExported)
	})

	t.Run("struct with tags", func(t *testing.T) {
		typ := reflect.TypeOf(StructWithTags{})
		info := GetTypeInfo(typ)

		assert.Equal(t, 3, info.NumFields)

		// Check tag parsing
		field1 := info.Fields[0]
		assert.Equal(t, "custom", field1.TagName)
		assert.True(t, field1.IsOptional)

		field2 := info.Fields[1]
		assert.True(t, field2.IsGroup)
		assert.Equal(t, "numbers", field2.GroupName)

		field3 := info.Fields[2]
		assert.False(t, field3.IsOptional)
		assert.False(t, field3.IsGroup)
	})

	t.Run("struct with dig.In", func(t *testing.T) {
		typ := reflect.TypeOf(StructWithDigIn{})
		info := GetTypeInfo(typ)

		assert.True(t, info.HasInField)
		assert.False(t, info.HasOutField)

		// Should have 4 fields (including embedded In)
		assert.Equal(t, 4, info.NumFields)

		// Find the In field
		var inField *fieldInfo
		for _, f := range info.Fields {
			if f.Name == "In" {
				inField = f
				break
			}
		}
		assert.NotNil(t, inField)
		assert.True(t, inField.IsAnonymous)
	})

	t.Run("struct with dig.Out", func(t *testing.T) {
		typ := reflect.TypeOf(StructWithDigOut{})
		info := GetTypeInfo(typ)

		assert.False(t, info.HasInField)
		assert.True(t, info.HasOutField)
	})
}

func TestGetTypeInfo_FunctionAnalysis(t *testing.T) {
	t.Run("simple function", func(t *testing.T) {
		fn := func(a int, b string) (bool, error) {
			return true, nil
		}

		typ := reflect.TypeOf(fn)
		info := GetTypeInfo(typ)

		assert.True(t, info.IsFunc)
		assert.Equal(t, 2, info.NumIn)
		assert.Equal(t, 2, info.NumOut)
		assert.False(t, info.IsVariadic)
		assert.True(t, info.HasErrorReturn)

		// Check input types
		assert.Len(t, info.InTypes, 2)
		assert.Equal(t, reflect.TypeOf(0), info.InTypes[0])
		assert.Equal(t, reflect.TypeOf(""), info.InTypes[1])

		// Check output types
		assert.Len(t, info.OutTypes, 2)
		assert.Equal(t, reflect.TypeOf(true), info.OutTypes[0])
		assert.Equal(t, reflect.TypeOf((*error)(nil)).Elem(), info.OutTypes[1])
	})

	t.Run("variadic function", func(t *testing.T) {
		fn := func(prefix string, values ...int) string {
			return prefix
		}

		typ := reflect.TypeOf(fn)
		info := GetTypeInfo(typ)

		assert.True(t, info.IsFunc)
		assert.True(t, info.IsVariadic)
		assert.Equal(t, 2, info.NumIn)
		assert.Equal(t, 1, info.NumOut)
		assert.False(t, info.HasErrorReturn)
	})

	t.Run("function without error return", func(t *testing.T) {
		fn := func() string { return "test" }

		typ := reflect.TypeOf(fn)
		info := GetTypeInfo(typ)

		assert.False(t, info.HasErrorReturn)
	})

	t.Run("function with multiple returns no error", func(t *testing.T) {
		fn := func() (string, int) { return "test", 42 }

		typ := reflect.TypeOf(fn)
		info := GetTypeInfo(typ)

		assert.Equal(t, 2, info.NumOut)
		assert.False(t, info.HasErrorReturn)
	})
}

func TestGetTypeInfo_ElementTypes(t *testing.T) {
	t.Run("pointer element type", func(t *testing.T) {
		typ := reflect.TypeOf((*SimpleStruct)(nil))
		info := GetTypeInfo(typ)

		assert.True(t, info.IsPointer)
		assert.NotNil(t, info.ElementType)
		assert.Equal(t, reflect.TypeOf(SimpleStruct{}), info.ElementType)
	})

	t.Run("slice element type", func(t *testing.T) {
		typ := reflect.TypeOf([]string{})
		info := GetTypeInfo(typ)

		assert.True(t, info.IsSlice)
		assert.NotNil(t, info.ElementType)
		assert.Equal(t, reflect.TypeOf(""), info.ElementType)
	})

	t.Run("map types", func(t *testing.T) {
		typ := reflect.TypeOf(map[string]int{})
		info := GetTypeInfo(typ)

		assert.True(t, info.IsMap)
		assert.NotNil(t, info.ElementType)
		assert.NotNil(t, info.KeyType)
		assert.Equal(t, reflect.TypeOf(""), info.KeyType)
		assert.Equal(t, reflect.TypeOf(0), info.ElementType)
	})

	t.Run("channel element type", func(t *testing.T) {
		typ := reflect.TypeOf(make(chan bool))
		info := GetTypeInfo(typ)

		assert.True(t, info.IsChan)
		assert.NotNil(t, info.ElementType)
		assert.Equal(t, reflect.TypeOf(true), info.ElementType)
	})
}

func TestGetTypeInfo_Caching(t *testing.T) {
	// Clear cache before test
	clearTypeCache()

	typ := reflect.TypeOf(SimpleStruct{})

	// First call should create new info
	info1 := GetTypeInfo(typ)
	assert.NotNil(t, info1)

	// Second call should return same instance
	info2 := GetTypeInfo(typ)
	assert.Same(t, info1, info2)

	// Different type should create new info
	typ2 := reflect.TypeOf((*SimpleStruct)(nil))
	info3 := GetTypeInfo(typ2)
	assert.NotSame(t, info1, info3)
}

func TestGetTypeInfo_Concurrency(t *testing.T) {
	// Clear cache before test
	clearTypeCache()

	typ := reflect.TypeOf(ComplexStruct{})

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	infos := make([]*typeInfo, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(idx int) {
			defer wg.Done()
			infos[idx] = GetTypeInfo(typ)
		}(i)
	}

	wg.Wait()

	// All goroutines should get the same cached instance
	first := infos[0]
	for i := 1; i < goroutines; i++ {
		assert.Same(t, first, infos[i], "goroutine %d got different instance", i)
	}
}

func TestGetFormattedName(t *testing.T) {
	tests := []struct {
		name     string
		typ      reflect.Type
		expected string
	}{
		{
			name:     "simple struct",
			typ:      reflect.TypeOf(SimpleStruct{}),
			expected: "typecache.SimpleStruct",
		},
		{
			name:     "pointer to struct",
			typ:      reflect.TypeOf((*SimpleStruct)(nil)),
			expected: "*typecache.SimpleStruct",
		},
		{
			name:     "interface",
			typ:      reflect.TypeOf((*InterfaceType)(nil)).Elem(),
			expected: "typecache.InterfaceType",
		},
		{
			name:     "slice",
			typ:      reflect.TypeOf([]string{}),
			expected: "[]string",
		},
		{
			name:     "map",
			typ:      reflect.TypeOf(map[string]int{}),
			expected: "map[string]int",
		},
		{
			name:     "function",
			typ:      reflect.TypeOf(func(int) string { return "" }),
			expected: "func(int) string",
		},
		{
			name:     "nested pointer",
			typ:      reflect.TypeOf((**SimpleStruct)(nil)),
			expected: "**typecache.SimpleStruct",
		},
		{
			name:     "slice of pointers",
			typ:      reflect.TypeOf([]*SimpleStruct{}),
			expected: "[]*typecache.SimpleStruct",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := GetTypeInfo(tt.typ)
			formatted := info.GetFormattedName()
			assert.Equal(t, tt.expected, formatted)
		})
	}
}

func TestDetermineServiceTypeCached(t *testing.T) {
	t.Run("interface type", func(t *testing.T) {
		typ, info, err := DetermineServiceTypeCached[InterfaceType]()

		require.NoError(t, err)
		assert.Equal(t, reflect.TypeOf((*InterfaceType)(nil)).Elem(), typ)
		assert.NotNil(t, info)
		assert.True(t, info.IsInterface)
	})

	t.Run("pointer type", func(t *testing.T) {
		typ, info, err := DetermineServiceTypeCached[*SimpleStruct]()

		require.NoError(t, err)
		assert.Equal(t, reflect.TypeOf((*SimpleStruct)(nil)), typ)
		assert.NotNil(t, info)
		assert.True(t, info.IsPointer)
	})

	t.Run("value type", func(t *testing.T) {
		typ, info, err := DetermineServiceTypeCached[SimpleStruct]()

		require.NoError(t, err)
		// For structs, we expect pointer type
		assert.Equal(t, reflect.TypeOf((*SimpleStruct)(nil)), typ)
		assert.NotNil(t, info)
		assert.True(t, info.IsPointer)
	})

	t.Run("slice type", func(t *testing.T) {
		typ, info, err := DetermineServiceTypeCached[[]string]()

		require.NoError(t, err)
		assert.Equal(t, reflect.TypeOf([]string{}), typ)
		assert.NotNil(t, info)
		assert.True(t, info.IsSlice)
	})

	t.Run("map type", func(t *testing.T) {
		typ, info, err := DetermineServiceTypeCached[map[string]int]()

		require.NoError(t, err)
		assert.Equal(t, reflect.TypeOf(map[string]int{}), typ)
		assert.NotNil(t, info)
		assert.True(t, info.IsMap)
	})

	t.Run("function type", func(t *testing.T) {
		typ, info, err := DetermineServiceTypeCached[func() string]()

		require.NoError(t, err)
		assert.Equal(t, reflect.TypeOf(func() string { return "" }), typ)
		assert.NotNil(t, info)
		assert.True(t, info.IsFunc)
	})

	t.Run("primitive type", func(t *testing.T) {
		typ, info, err := DetermineServiceTypeCached[string]()

		require.NoError(t, err)
		assert.Equal(t, reflect.TypeOf(""), typ)
		assert.NotNil(t, info)
		assert.True(t, info.IsPrimitive)
	})
}

func TestCanCheckNilCached(t *testing.T) {
	tests := []struct {
		name     string
		value    reflect.Value
		expected bool
	}{
		{
			name:     "valid pointer",
			value:    reflect.ValueOf((*SimpleStruct)(nil)),
			expected: true,
		},
		{
			name:     "valid interface",
			value:    reflect.ValueOf((*InterfaceType)(nil)),
			expected: true,
		},
		{
			name:     "valid slice",
			value:    reflect.ValueOf([]string(nil)),
			expected: true,
		},
		{
			name:     "valid map",
			value:    reflect.ValueOf(map[string]int(nil)),
			expected: true,
		},
		{
			name:     "valid channel",
			value:    reflect.ValueOf((chan int)(nil)),
			expected: true,
		},
		{
			name:     "struct value",
			value:    reflect.ValueOf(SimpleStruct{}),
			expected: false,
		},
		{
			name:     "primitive value",
			value:    reflect.ValueOf(42),
			expected: false,
		},
		{
			name:     "invalid value",
			value:    reflect.Value{},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := canCheckNilCached(tt.value)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTypeInfo_EdgeCases(t *testing.T) {
	t.Run("nil type", func(t *testing.T) {
		info := GetTypeInfo(nil)
		assert.Nil(t, info)
	})

	t.Run("anonymous struct", func(t *testing.T) {
		typ := reflect.TypeOf(struct {
			Field1 string
			Field2 int
		}{})

		info := GetTypeInfo(typ)
		assert.NotNil(t, info)
		assert.True(t, info.IsStruct)
		assert.Equal(t, 2, info.NumFields)
		assert.Empty(t, info.Name)
	})

	t.Run("embedded fields", func(t *testing.T) {
		type Embedded struct {
			SimpleStruct
			Extra string
		}

		typ := reflect.TypeOf(Embedded{})
		info := GetTypeInfo(typ)

		assert.Equal(t, 2, info.NumFields)

		// Check embedded field
		embeddedField := info.Fields[0]
		assert.Equal(t, "SimpleStruct", embeddedField.Name)
		assert.True(t, embeddedField.IsAnonymous)
	})

	t.Run("unexported fields", func(t *testing.T) {
		type WithUnexported struct {
			Public  string
			private int //lint:ignore U1000 test unexported field
		}

		typ := reflect.TypeOf(WithUnexported{})
		info := GetTypeInfo(typ)

		assert.Equal(t, 2, info.NumFields)

		publicField := info.Fields[0]
		assert.True(t, publicField.IsExported)

		privateField := info.Fields[1]
		assert.False(t, privateField.IsExported)
	})

	t.Run("recursive type formatting", func(t *testing.T) {
		// Create a deeply nested type
		typ := reflect.TypeOf([]*[]*[]*SimpleStruct{})
		info := GetTypeInfo(typ)

		formatted := info.GetFormattedName()
		assert.Equal(t, "[]*[]*[]*typecache.SimpleStruct", formatted)
	})

	t.Run("array type", func(t *testing.T) {
		typ := reflect.TypeOf([5]int{})
		info := GetTypeInfo(typ)

		assert.Equal(t, reflect.Array, info.Kind)
		assert.NotNil(t, info.ElementType)
		assert.Equal(t, reflect.TypeOf(0), info.ElementType)

		formatted := info.GetFormattedName()
		assert.Equal(t, "[5]int", formatted)
	})
}

func TestClearTypeCache(t *testing.T) {
	// Add some types to cache
	types := []reflect.Type{
		reflect.TypeOf(SimpleStruct{}),
		reflect.TypeOf((*InterfaceType)(nil)).Elem(),
		reflect.TypeOf([]string{}),
	}

	// Get info for each type (caching them)
	for _, typ := range types {
		info := GetTypeInfo(typ)
		assert.NotNil(t, info)
	}

	// Clear cache
	clearTypeCache()

	// Getting info again should create new instances
	// We can't directly test this without exposing internals,
	// but we can verify the function doesn't panic
	for _, typ := range types {
		info := GetTypeInfo(typ)
		assert.NotNil(t, info)
	}
}

func TestLastSegment(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"github.com/junioryono/godi/v3", "v3"},
		{"github.com/junioryono/godi/v3/internal", "internal"},
		{"simple", "simple"},
		{"", ""},
		{"/leading/slash", "slash"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := lastSegment(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// Benchmark tests
func BenchmarkGetTypeInfo(b *testing.B) {
	typ := reflect.TypeOf(ComplexStruct{})

	b.Run("cached", func(b *testing.B) {
		// Warm up cache
		GetTypeInfo(typ)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			info := GetTypeInfo(typ)
			_ = info
		}
	})

	b.Run("uncached", func(b *testing.B) {
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			info := GetTypeInfo(typ)
			tc.cache.Delete(info.Type) // Simulate uncached by deleting after use
		}
	})
}

func BenchmarkGetFormattedName(b *testing.B) {
	typ := reflect.TypeOf([]*map[string][]*ComplexStruct{})
	info := GetTypeInfo(typ)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		name := info.GetFormattedName()
		_ = name
	}
}

func BenchmarkCanCheckNilCached(b *testing.B) {
	values := []reflect.Value{
		reflect.ValueOf((*SimpleStruct)(nil)),
		reflect.ValueOf(SimpleStruct{}),
		reflect.ValueOf([]string{}),
		reflect.ValueOf(42),
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		for _, v := range values {
			canBeNil := canCheckNilCached(v)
			_ = canBeNil
		}
	}
}
