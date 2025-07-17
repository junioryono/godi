package godi

import (
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// Test types for cache testing
type testInterface interface {
	Method() string
}

type testStruct struct {
	Field1 string
	Field2 int
}

type testStructWithTags struct {
	Required   string
	Optional   string `optional:"true"`
	GroupField string `group:"testgroup"`
	NamedField string `name:"custom"`
	//lint:ignore U1000 This is a private field for testing
	privateField string
}

type testStructWithIn struct {
	In
	Service1 testInterface
	Service2 *testStruct
}

type testStructWithOut struct {
	Out
	Service1 testInterface
	Service2 *testStruct
}

type testStructComplex struct {
	In
	Required   testInterface
	Optional   *testStruct `optional:"true"`
	GroupSlice []string    `group:"names"`
}

func TestTypeCache_GetTypeInfo(t *testing.T) {
	cache := &typeCache{}

	t.Run("nil type", func(t *testing.T) {
		info := cache.getTypeInfo(nil)
		if info != nil {
			t.Error("expected nil for nil type")
		}
	})

	t.Run("caches type info", func(t *testing.T) {
		typ := reflect.TypeOf((*testInterface)(nil)).Elem()

		// First call should create and cache
		info1 := cache.getTypeInfo(typ)
		if info1 == nil {
			t.Fatal("expected non-nil type info")
		}

		// Second call should return same instance
		info2 := cache.getTypeInfo(typ)
		if info1 != info2 {
			t.Error("expected same type info instance from cache")
		}
	})

	t.Run("concurrent access", func(t *testing.T) {
		cache := &typeCache{}
		typ := reflect.TypeOf(testStruct{})

		var wg sync.WaitGroup
		infos := make([]*typeInfo, 10)

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				infos[idx] = cache.getTypeInfo(typ)
			}(i)
		}

		wg.Wait()

		// All should be the same instance
		first := infos[0]
		for i := 1; i < 10; i++ {
			if infos[i] != first {
				t.Errorf("concurrent access returned different instances at index %d", i)
			}
		}
	})
}

func TestTypeInfo_BasicTypes(t *testing.T) {
	cache := &typeCache{}

	tests := []struct {
		name          string
		typ           reflect.Type
		wantKind      reflect.Kind
		wantPrimitive bool
		wantCanBeNil  bool
	}{
		{
			name:          "int",
			typ:           reflect.TypeOf(42),
			wantKind:      reflect.Int,
			wantPrimitive: true,
			wantCanBeNil:  false,
		},
		{
			name:          "string",
			typ:           reflect.TypeOf(""),
			wantKind:      reflect.String,
			wantPrimitive: true,
			wantCanBeNil:  false,
		},
		{
			name:          "bool",
			typ:           reflect.TypeOf(true),
			wantKind:      reflect.Bool,
			wantPrimitive: true,
			wantCanBeNil:  false,
		},
		{
			name:          "float64",
			typ:           reflect.TypeOf(3.14),
			wantKind:      reflect.Float64,
			wantPrimitive: true,
			wantCanBeNil:  false,
		},
		{
			name:          "interface",
			typ:           reflect.TypeOf((*testInterface)(nil)).Elem(),
			wantKind:      reflect.Interface,
			wantPrimitive: false,
			wantCanBeNil:  true,
		},
		{
			name:          "pointer",
			typ:           reflect.TypeOf((*testStruct)(nil)),
			wantKind:      reflect.Ptr,
			wantPrimitive: false,
			wantCanBeNil:  true,
		},
		{
			name:          "slice",
			typ:           reflect.TypeOf([]string{}),
			wantKind:      reflect.Slice,
			wantPrimitive: false,
			wantCanBeNil:  true,
		},
		{
			name:          "array",
			typ:           reflect.TypeOf([5]int{}),
			wantKind:      reflect.Array,
			wantPrimitive: false,
			wantCanBeNil:  false,
		},
		{
			name:          "map",
			typ:           reflect.TypeOf(map[string]int{}),
			wantKind:      reflect.Map,
			wantPrimitive: false,
			wantCanBeNil:  true,
		},
		{
			name:          "chan",
			typ:           reflect.TypeOf(make(chan int)),
			wantKind:      reflect.Chan,
			wantPrimitive: false,
			wantCanBeNil:  true,
		},
		{
			name:          "func",
			typ:           reflect.TypeOf(func() {}),
			wantKind:      reflect.Func,
			wantPrimitive: false,
			wantCanBeNil:  true,
		},
		{
			name:          "struct",
			typ:           reflect.TypeOf(testStruct{}),
			wantKind:      reflect.Struct,
			wantPrimitive: false,
			wantCanBeNil:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := cache.getTypeInfo(tt.typ)

			if info.Kind != tt.wantKind {
				t.Errorf("Kind = %v, want %v", info.Kind, tt.wantKind)
			}

			if info.IsPrimitive != tt.wantPrimitive {
				t.Errorf("IsPrimitive = %v, want %v", info.IsPrimitive, tt.wantPrimitive)
			}

			if info.CanBeNil != tt.wantCanBeNil {
				t.Errorf("CanBeNil = %v, want %v", info.CanBeNil, tt.wantCanBeNil)
			}
		})
	}
}

func TestTypeInfo_ElementTypes(t *testing.T) {
	cache := &typeCache{}

	t.Run("pointer element type", func(t *testing.T) {
		typ := reflect.TypeOf((*testStruct)(nil))
		info := cache.getTypeInfo(typ)

		if info.ElementType == nil {
			t.Fatal("expected non-nil element type for pointer")
		}

		if info.ElementType != reflect.TypeOf(testStruct{}) {
			t.Error("incorrect element type for pointer")
		}
	})

	t.Run("slice element type", func(t *testing.T) {
		typ := reflect.TypeOf([]int{})
		info := cache.getTypeInfo(typ)

		if info.ElementType == nil {
			t.Fatal("expected non-nil element type for slice")
		}

		if info.ElementType != reflect.TypeOf(0) {
			t.Error("incorrect element type for slice")
		}
	})

	t.Run("array element type", func(t *testing.T) {
		typ := reflect.TypeOf([5]string{})
		info := cache.getTypeInfo(typ)

		if info.ElementType == nil {
			t.Fatal("expected non-nil element type for array")
		}

		if info.ElementType != reflect.TypeOf("") {
			t.Error("incorrect element type for array")
		}

		if info.Kind != reflect.Array {
			t.Errorf("expected Array kind, got %v", info.Kind)
		}
	})

	t.Run("array of structs", func(t *testing.T) {
		typ := reflect.TypeOf([3]testStruct{})
		info := cache.getTypeInfo(typ)

		if info.ElementType == nil {
			t.Fatal("expected non-nil element type for array")
		}

		if info.ElementType != reflect.TypeOf(testStruct{}) {
			t.Error("incorrect element type for array of structs")
		}
	})

	t.Run("map key and element types", func(t *testing.T) {
		typ := reflect.TypeOf(map[string]int{})
		info := cache.getTypeInfo(typ)

		if info.KeyType == nil {
			t.Fatal("expected non-nil key type for map")
		}

		if info.ElementType == nil {
			t.Fatal("expected non-nil element type for map")
		}

		if info.KeyType != reflect.TypeOf("") {
			t.Error("incorrect key type for map")
		}

		if info.ElementType != reflect.TypeOf(0) {
			t.Error("incorrect element type for map")
		}
	})

	t.Run("channel element type", func(t *testing.T) {
		typ := reflect.TypeOf(make(chan string))
		info := cache.getTypeInfo(typ)

		if info.ElementType == nil {
			t.Fatal("expected non-nil element type for channel")
		}

		if info.ElementType != reflect.TypeOf("") {
			t.Error("incorrect element type for channel")
		}
	})
}

func TestTypeInfo_Functions(t *testing.T) {
	cache := &typeCache{}

	t.Run("simple function", func(t *testing.T) {
		fn := func(s string, i int) error { return nil }
		info := cache.getTypeInfo(reflect.TypeOf(fn))

		if !info.IsFunc {
			t.Error("expected IsFunc to be true")
		}

		if info.NumIn != 2 {
			t.Errorf("NumIn = %d, want 2", info.NumIn)
		}

		if info.NumOut != 1 {
			t.Errorf("NumOut = %d, want 1", info.NumOut)
		}

		if !info.HasErrorReturn {
			t.Error("expected HasErrorReturn to be true")
		}

		if info.IsVariadic {
			t.Error("expected IsVariadic to be false")
		}
	})

	t.Run("variadic function", func(t *testing.T) {
		fn := func(prefix string, args ...int) {}
		info := cache.getTypeInfo(reflect.TypeOf(fn))

		if !info.IsVariadic {
			t.Error("expected IsVariadic to be true")
		}

		if info.NumIn != 2 {
			t.Errorf("NumIn = %d, want 2", info.NumIn)
		}
	})

	t.Run("multiple returns without error", func(t *testing.T) {
		fn := func() (string, int) { return "", 0 }
		info := cache.getTypeInfo(reflect.TypeOf(fn))

		if info.NumOut != 2 {
			t.Errorf("NumOut = %d, want 2", info.NumOut)
		}

		if info.HasErrorReturn {
			t.Error("expected HasErrorReturn to be false")
		}
	})

	t.Run("cached input and output types", func(t *testing.T) {
		fn := func(s string, i int) (bool, error) { return false, nil }
		info := cache.getTypeInfo(reflect.TypeOf(fn))

		if len(info.InTypes) != 2 {
			t.Fatalf("expected 2 input types, got %d", len(info.InTypes))
		}

		if info.InTypes[0] != reflect.TypeOf("") {
			t.Error("incorrect first input type")
		}

		if info.InTypes[1] != reflect.TypeOf(0) {
			t.Error("incorrect second input type")
		}

		if len(info.OutTypes) != 2 {
			t.Fatalf("expected 2 output types, got %d", len(info.OutTypes))
		}

		if info.OutTypes[0] != reflect.TypeOf(true) {
			t.Error("incorrect first output type")
		}
	})
}

func TestTypeInfo_Structs(t *testing.T) {
	cache := &typeCache{}

	t.Run("simple struct", func(t *testing.T) {
		info := cache.getTypeInfo(reflect.TypeOf(testStruct{}))

		if !info.IsStruct {
			t.Error("expected IsStruct to be true")
		}

		if info.NumFields != 2 {
			t.Errorf("NumFields = %d, want 2", info.NumFields)
		}

		if len(info.Fields) != 2 {
			t.Fatalf("expected 2 fields, got %d", len(info.Fields))
		}

		// Check first field
		field1 := info.Fields[0]
		if field1.Name != "Field1" {
			t.Errorf("field 0 name = %s, want Field1", field1.Name)
		}
		if field1.Type != reflect.TypeOf("") {
			t.Error("incorrect type for field 0")
		}
		if !field1.IsExported {
			t.Error("expected field 0 to be exported")
		}
	})

	t.Run("struct with tags", func(t *testing.T) {
		info := cache.getTypeInfo(reflect.TypeOf(testStructWithTags{}))

		if info.NumFields != 5 { // 4 public + 1 private
			t.Errorf("NumFields = %d, want 5", info.NumFields)
		}

		// Check optional field
		optionalField := info.Fields[1]
		if !optionalField.IsOptional {
			t.Error("expected Optional field to have IsOptional = true")
		}

		// Check group field
		groupField := info.Fields[2]
		if !groupField.IsGroup {
			t.Error("expected GroupField to have IsGroup = true")
		}
		if groupField.GroupName != "testgroup" {
			t.Errorf("GroupName = %s, want testgroup", groupField.GroupName)
		}

		// Check named field
		namedField := info.Fields[3]
		if namedField.TagName != "custom" {
			t.Errorf("ServiceName = %s, want custom", namedField.TagName)
		}

		// Check private field
		privateField := info.Fields[4]
		if privateField.IsExported {
			t.Error("expected privateField to not be exported")
		}
	})

	t.Run("struct with In", func(t *testing.T) {
		info := cache.getTypeInfo(reflect.TypeOf(testStructWithIn{}))

		if !info.HasInField {
			t.Error("expected HasInField to be true")
		}

		if info.HasOutField {
			t.Error("expected HasOutField to be false")
		}
	})

	t.Run("struct with Out", func(t *testing.T) {
		info := cache.getTypeInfo(reflect.TypeOf(testStructWithOut{}))

		if info.HasInField {
			t.Error("expected HasInField to be false")
		}

		if !info.HasOutField {
			t.Error("expected HasOutField to be true")
		}
	})
}

func TestFormatTypeCached(t *testing.T) {
	tests := []struct {
		name     string
		typ      reflect.Type
		contains string
	}{
		{
			name:     "interface",
			typ:      reflect.TypeOf((*testInterface)(nil)).Elem(),
			contains: "testInterface",
		},
		{
			name:     "pointer to struct",
			typ:      reflect.TypeOf((*testStruct)(nil)),
			contains: "*",
		},
		{
			name:     "slice",
			typ:      reflect.TypeOf([]int{}),
			contains: "[]",
		},
		{
			name:     "array",
			typ:      reflect.TypeOf([5]int{}),
			contains: "[5]",
		},
		{
			name:     "array of strings",
			typ:      reflect.TypeOf([3]string{}),
			contains: "[3]",
		},
		{
			name:     "map",
			typ:      reflect.TypeOf(map[string]int{}),
			contains: "map[",
		},
		{
			name:     "struct",
			typ:      reflect.TypeOf(testStruct{}),
			contains: "testStruct",
		},
		{
			name:     "builtin type",
			typ:      reflect.TypeOf(""),
			contains: "string",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			info := globalTypeCache.getTypeInfo(tt.typ)
			formatted := formatTypeCachedWithDepth(info, 0)

			if !strings.Contains(formatted, tt.contains) {
				t.Errorf("formatTypeCached() = %v, want to contain %v", formatted, tt.contains)
			}
		})
	}

	t.Run("nil info", func(t *testing.T) {
		result := formatTypeCachedWithDepth(nil, 0)
		if result != "<nil>" {
			t.Errorf("formatTypeCached(nil) = %v, want <nil>", result)
		}
	})
}

func TestDetermineServiceTypeCached(t *testing.T) {
	tests := []struct {
		name          string
		typeFunc      func() (reflect.Type, *typeInfo, error)
		wantPointer   bool
		wantInterface bool
	}{
		{
			name: "interface type",
			typeFunc: func() (reflect.Type, *typeInfo, error) {
				return determineServiceTypeCached[testInterface]()
			},
			wantInterface: true,
		},
		{
			name: "pointer type",
			typeFunc: func() (reflect.Type, *typeInfo, error) {
				return determineServiceTypeCached[*testStruct]()
			},
			wantPointer: true,
		},
		{
			name: "struct type",
			typeFunc: func() (reflect.Type, *typeInfo, error) {
				return determineServiceTypeCached[testStruct]()
			},
			wantPointer: true, // Should return pointer to struct
		},
		{
			name: "slice type",
			typeFunc: func() (reflect.Type, *typeInfo, error) {
				return determineServiceTypeCached[[]string]()
			},
			wantPointer: false, // Slices are used directly
		},
		{
			name: "map type",
			typeFunc: func() (reflect.Type, *typeInfo, error) {
				return determineServiceTypeCached[map[string]int]()
			},
			wantPointer: false, // Maps are used directly
		},
		{
			name: "primitive type",
			typeFunc: func() (reflect.Type, *typeInfo, error) {
				return determineServiceTypeCached[int]()
			},
			wantPointer: false, // Primitives are used directly
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			typ, info, err := tt.typeFunc()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantPointer && info.Kind != reflect.Ptr {
				t.Errorf("expected pointer type, got %v", info.Kind)
			}

			if tt.wantInterface && info.Kind != reflect.Interface {
				t.Errorf("expected interface type, got %v", info.Kind)
			}

			if typ != info.Type {
				t.Error("returned type doesn't match info.Type")
			}
		})
	}
}

func TestValidateConstructorCached(t *testing.T) {
	tests := []struct {
		name        string
		constructor interface{}
		wantErr     error
	}{
		{
			name:        "nil constructor",
			constructor: nil,
			wantErr:     ErrNilConstructor,
		},
		{
			name:        "not a function",
			constructor: "not a function",
			wantErr:     ErrConstructorNotFunction,
		},
		{
			name:        "valid simple constructor",
			constructor: func() *testStruct { return &testStruct{} },
			wantErr:     nil,
		},
		{
			name:        "constructor with error",
			constructor: func() (*testStruct, error) { return nil, nil },
			wantErr:     nil,
		},
		{
			name:        "no return values",
			constructor: func() {},
			wantErr:     ErrConstructorNoReturn,
		},
		{
			name:        "too many returns",
			constructor: func() (int, int, int) { return 0, 0, 0 },
			wantErr:     ErrConstructorTooManyReturns,
		},
		{
			name:        "invalid second return",
			constructor: func() (int, string) { return 0, "" },
			wantErr:     ErrConstructorInvalidSecondReturn,
		},
		{
			name:        "with dig.In parameter",
			constructor: func(in testStructWithIn) *testStruct { return nil },
			wantErr:     nil,
		},
		{
			name:        "with dig.Out return",
			constructor: func() testStructWithOut { return testStructWithOut{} },
			wantErr:     nil,
		},
		{
			name:        "dig.Out with error",
			constructor: func() (testStructWithOut, error) { return testStructWithOut{}, nil },
			wantErr:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateConstructorCached(tt.constructor)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("validateConstructorCached() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCanCheckNilCached(t *testing.T) {
	tests := []struct {
		name  string
		value reflect.Value
		want  bool
	}{
		{
			name:  "invalid value",
			value: reflect.Value{},
			want:  false,
		},
		{
			name:  "int value",
			value: reflect.ValueOf(42),
			want:  false,
		},
		{
			name:  "string value",
			value: reflect.ValueOf("test"),
			want:  false,
		},
		{
			name:  "pointer value",
			value: reflect.ValueOf((*testStruct)(nil)),
			want:  true,
		},
		{
			name:  "slice value",
			value: reflect.ValueOf([]int(nil)),
			want:  true,
		},
		{
			name:  "map value",
			value: reflect.ValueOf(map[string]int(nil)),
			want:  true,
		},
		{
			name:  "interface value",
			value: reflect.ValueOf((*testInterface)(nil)),
			want:  true,
		},
		{
			name:  "channel value",
			value: reflect.ValueOf((chan int)(nil)),
			want:  true,
		},
		{
			name:  "func value",
			value: reflect.ValueOf((func())(nil)),
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canCheckNilCached(tt.value); got != tt.want {
				t.Errorf("canCheckNilCached() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestClearTypeCache(t *testing.T) {
	// Add some types to the cache
	types := []reflect.Type{
		reflect.TypeOf(42),
		reflect.TypeOf(""),
		reflect.TypeOf(testStruct{}),
		reflect.TypeOf((*testInterface)(nil)).Elem(),
	}

	// Ensure they're cached
	for _, typ := range types {
		globalTypeCache.getTypeInfo(typ)
	}

	// Clear the cache
	clearTypeCache()

	// Verify cache is empty by checking that new calls create different instances
	// (This is a bit indirect, but we can't directly inspect the sync.Map size)
	count := 0
	globalTypeCache.cache.Range(func(key, value interface{}) bool {
		count++
		return true
	})

	if count != 0 {
		t.Errorf("expected empty cache after clear, but found %d entries", count)
	}
}

func TestLastSegment(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"github.com/user/package", "package"},
		{"simple", "simple"},
		{"path/to/package", "package"},
		{"", ""},
		{"/leading/slash", "slash"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			if got := lastSegment(tt.path); got != tt.want {
				t.Errorf("lastSegment(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

// Benchmark tests
func BenchmarkTypeCache_GetTypeInfo(b *testing.B) {
	cache := &typeCache{}
	typ := reflect.TypeOf(testStruct{})

	// Warm up the cache
	cache.getTypeInfo(typ)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.getTypeInfo(typ)
	}
}

func BenchmarkTypeCache_GetTypeInfo_Parallel(b *testing.B) {
	cache := &typeCache{}
	typ := reflect.TypeOf(testStruct{})

	// Warm up the cache
	cache.getTypeInfo(typ)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cache.getTypeInfo(typ)
		}
	})
}

func BenchmarkTypeCache_CreateTypeInfo_Struct(b *testing.B) {
	cache := &typeCache{}
	typ := reflect.TypeOf(testStructComplex{})

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.createTypeInfo(typ)
	}
}

func BenchmarkTypeCache_CreateTypeInfo_Function(b *testing.B) {
	cache := &typeCache{}
	fn := func(a string, b int, c testInterface) (*testStruct, error) { return nil, nil }
	typ := reflect.TypeOf(fn)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		cache.createTypeInfo(typ)
	}
}
