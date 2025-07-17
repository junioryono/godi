package godi

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// typeCache provides a thread-safe cache for reflection type information
// to avoid repeated reflection operations on the same types.
type typeCache struct {
	cache sync.Map // map[reflect.Type]*typeInfo
}

// typeInfo holds pre-computed reflection information about a type.
type typeInfo struct {
	// Basic type information
	Type    reflect.Type
	Kind    reflect.Kind
	PkgPath string
	Name    string
	String  string // Cached string representation

	// Type characteristics
	IsInterface bool
	IsPointer   bool
	IsSlice     bool
	IsMap       bool
	IsChan      bool
	IsFunc      bool
	IsStruct    bool
	IsPrimitive bool
	CanBeNil    bool

	// Related types
	ElementType reflect.Type // For pointer, slice, array, chan, map
	KeyType     reflect.Type // For map

	// For functions
	NumIn          int
	NumOut         int
	IsVariadic     bool
	InTypes        []reflect.Type
	OutTypes       []reflect.Type
	HasErrorReturn bool

	// For structs
	NumFields   int
	Fields      []*fieldInfo
	HasInField  bool // Has embedded dig.In
	HasOutField bool // Has embedded dig.Out

	// Formatted name for error messages
	FormattedName     string
	formattedNameOnce sync.Once
}

// fieldInfo holds information about a struct field.
type fieldInfo struct {
	Index       int
	Name        string
	Type        reflect.Type
	Tag         reflect.StructTag
	IsExported  bool
	IsAnonymous bool
	IsOptional  bool   // Has optional:"true" tag
	IsGroup     bool   // Has group:"name" tag
	GroupName   string // Group name if IsGroup
	TagName     string // Name tag value if present
}

// globalTypeCache is the singleton type cache used throughout the library.
var globalTypeCache = &typeCache{}

// getTypeInfo returns cached type information or creates it if not present.
func (tc *typeCache) getTypeInfo(t reflect.Type) *typeInfo {
	if t == nil {
		return nil
	}

	// Check if already cached
	if cached, ok := tc.cache.Load(t); ok {
		return cached.(*typeInfo)
	}

	// Create new type info
	info := tc.createTypeInfo(t)

	// Store in cache (handle race condition where another goroutine might have stored it)
	actual, _ := tc.cache.LoadOrStore(t, info)
	return actual.(*typeInfo)
}

// createTypeInfo creates a new typeInfo for the given type.
func (tc *typeCache) createTypeInfo(t reflect.Type) *typeInfo {
	info := &typeInfo{
		Type:    t,
		Kind:    t.Kind(),
		PkgPath: t.PkgPath(),
		Name:    t.Name(),
		String:  t.String(),
	}

	// Set basic characteristics
	info.IsInterface = info.Kind == reflect.Interface
	info.IsPointer = info.Kind == reflect.Ptr
	info.IsSlice = info.Kind == reflect.Slice
	info.IsMap = info.Kind == reflect.Map
	info.IsChan = info.Kind == reflect.Chan
	info.IsFunc = info.Kind == reflect.Func
	info.IsStruct = info.Kind == reflect.Struct

	// Check if primitive
	switch info.Kind {
	case reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128, reflect.String:
		info.IsPrimitive = true
	}

	// Check if can be nil
	switch info.Kind {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		info.CanBeNil = true
	}

	// Set element types
	switch info.Kind {
	case reflect.Ptr, reflect.Slice, reflect.Array, reflect.Chan:
		info.ElementType = t.Elem()
	case reflect.Map:
		info.ElementType = t.Elem()
		info.KeyType = t.Key()
	}

	// Handle function types
	if info.IsFunc {
		info.NumIn = t.NumIn()
		info.NumOut = t.NumOut()
		info.IsVariadic = t.IsVariadic()

		// Cache input types
		info.InTypes = make([]reflect.Type, info.NumIn)
		for i := 0; i < info.NumIn; i++ {
			info.InTypes[i] = t.In(i)
		}

		// Cache output types
		info.OutTypes = make([]reflect.Type, info.NumOut)
		for i := 0; i < info.NumOut; i++ {
			info.OutTypes[i] = t.Out(i)
		}

		// Check for error return
		if info.NumOut > 0 {
			errorType := reflect.TypeOf((*error)(nil)).Elem()
			lastOut := info.OutTypes[info.NumOut-1]
			info.HasErrorReturn = lastOut.Implements(errorType)
		}
	}

	// Handle struct types
	if info.IsStruct {
		info.NumFields = t.NumField()
		info.Fields = make([]*fieldInfo, 0, info.NumFields)

		for i := 0; i < info.NumFields; i++ {
			field := t.Field(i)
			fieldInfo := &fieldInfo{
				Index:       i,
				Name:        field.Name,
				Type:        field.Type,
				Tag:         field.Tag,
				IsExported:  field.IsExported(),
				IsAnonymous: field.Anonymous,
			}

			// Parse tags
			if optional := field.Tag.Get("optional"); optional == "true" {
				fieldInfo.IsOptional = true
			}

			if group := field.Tag.Get("group"); group != "" {
				fieldInfo.IsGroup = true
				fieldInfo.GroupName = group
			}

			if name := field.Tag.Get("name"); name != "" {
				fieldInfo.TagName = name
			}

			// Check for dig.In/Out
			if field.Anonymous && field.Type != nil {
				if tc.isInType(field.Type) {
					info.HasInField = true
				}
				if tc.isOutType(field.Type) {
					info.HasOutField = true
				}
			}

			info.Fields = append(info.Fields, fieldInfo)
		}
	}

	return info
}

func (tc *typeCache) isInType(t reflect.Type) bool {
	if t == nil {
		return false
	}

	info := tc.getTypeInfo(t)
	return info.Name == "In" && strings.HasSuffix(info.PkgPath, "dig")
}

func (tc *typeCache) isOutType(t reflect.Type) bool {
	if t == nil {
		return false
	}

	info := tc.getTypeInfo(t)
	return info.Name == "Out" && strings.HasSuffix(info.PkgPath, "dig")
}

func (info *typeInfo) GetFormattedName() string {
	info.formattedNameOnce.Do(func() {
		info.FormattedName = formatTypeCachedWithDepth(info, 0)
	})

	return info.FormattedName
}

// formatTypeCachedWithDepth formats a type using cached information.
func formatTypeCachedWithDepth(info *typeInfo, depth int) string {
	const maxDepth = 50

	if depth > maxDepth {
		return info.String
	}

	if info == nil || info.Type == nil {
		return "<nil>"
	}

	switch info.Kind {
	case reflect.Invalid:
		return "<invalid>"

	case reflect.Interface:
		if info.PkgPath == "" {
			return info.Name
		}
		return lastSegment(info.PkgPath) + "." + info.Name

	case reflect.Ptr:
		if info.ElementType != nil {
			elemInfo := globalTypeCache.getTypeInfo(info.ElementType)
			return "*" + formatTypeCachedWithDepth(elemInfo, depth+1)
		}
		return "*" + info.ElementType.String()

	case reflect.Slice:
		if info.ElementType != nil {
			elemInfo := globalTypeCache.getTypeInfo(info.ElementType)
			return "[]" + formatTypeCachedWithDepth(elemInfo, depth+1)
		}
		return "[]" + info.ElementType.String()

	case reflect.Map:
		if info.KeyType != nil && info.ElementType != nil {
			keyInfo := globalTypeCache.getTypeInfo(info.KeyType)
			elemInfo := globalTypeCache.getTypeInfo(info.ElementType)
			return "map[" + keyInfo.GetFormattedName() + "]" + formatTypeCachedWithDepth(elemInfo, depth+1)
		}
		return info.String

	case reflect.Array:
		if info.ElementType != nil {
			elemInfo := globalTypeCache.getTypeInfo(info.ElementType)
			return fmt.Sprintf("[%d]%s", info.Type.Len(), formatTypeCachedWithDepth(elemInfo, depth+1))
		}
		return info.String

	case reflect.Func:
		// For functions, we might want to use the pre-computed string
		// or build it from cached InTypes/OutTypes
		return info.String

	case reflect.Struct:
		if info.PkgPath == "" {
			return info.String
		}
		return lastSegment(info.PkgPath) + "." + info.Name

	default:
		if info.PkgPath == "" {
			return info.String
		}
		return lastSegment(info.PkgPath) + "." + info.Name
	}
}

func lastSegment(path string) string {
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		return path[idx+1:]
	}

	return path
}

// Update the determineServiceType function to use the cache
func determineServiceTypeCached[T any]() (reflect.Type, *typeInfo, error) {
	tType := reflect.TypeOf((*T)(nil)).Elem()
	tInfo := globalTypeCache.getTypeInfo(tType)

	// Determine the actual service type to resolve
	switch tInfo.Kind {
	case reflect.Interface:
		// For interfaces, use the interface type directly
		return tType, tInfo, nil

	case reflect.Ptr:
		// T is already a pointer type (e.g., *UserService)
		// So we use T directly as the service type
		return tType, tInfo, nil

	case reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
		// For slices, maps, channels, and functions, we use the type directly
		return tType, tInfo, nil

	default:
		if tInfo.IsPrimitive {
			// For primitive types, we use the type directly
			return tType, tInfo, nil
		}
		// For structs and other complex types, services are typically registered as pointers
		ptrType := reflect.PointerTo(tType)
		ptrInfo := globalTypeCache.getTypeInfo(ptrType)
		return ptrType, ptrInfo, nil
	}
}

// validateConstructorCached validates a constructor using cached type information.
func validateConstructorCached(constructor interface{}) error {
	if constructor == nil {
		return ErrNilConstructor
	}

	fnType := reflect.TypeOf(constructor)
	fnInfo := globalTypeCache.getTypeInfo(fnType)

	if !fnInfo.IsFunc {
		return ErrConstructorNotFunction
	}

	// Check if any parameter uses In
	usesDigIn := false
	for _, inType := range fnInfo.InTypes {
		inInfo := globalTypeCache.getTypeInfo(inType)
		if inInfo.IsStruct && inInfo.HasInField {
			if usesDigIn {
				return ErrConstructorMultipleIn
			}
			usesDigIn = true
		}
	}

	// Check outputs
	usesDigOut := false
	if fnInfo.NumOut > 0 {
		outInfo := globalTypeCache.getTypeInfo(fnInfo.OutTypes[0])
		if outInfo.IsStruct && outInfo.HasOutField {
			usesDigOut = true
		}
	}

	if usesDigOut {
		// Can only return the result object or (result object, error)
		if fnInfo.NumOut > 2 {
			return ErrConstructorOutMaxReturns
		}

		if fnInfo.NumOut == 2 && !fnInfo.HasErrorReturn {
			return ErrConstructorInvalidSecondReturn
		}
	} else {
		// Regular constructor validation
		if fnInfo.NumOut == 0 {
			return ErrConstructorNoReturn
		}

		if fnInfo.NumOut > 2 {
			return ErrConstructorTooManyReturns
		}

		if fnInfo.NumOut == 2 && !fnInfo.HasErrorReturn {
			return ErrConstructorInvalidSecondReturn
		}
	}

	return nil
}

// Helper function to check if a value can be nil
func canCheckNilCached(v reflect.Value) bool {
	if !v.IsValid() {
		return false
	}

	info := globalTypeCache.getTypeInfo(v.Type())
	return info.CanBeNil
}

// clearTypeCache clears the global type cache. Useful for testing.
func clearTypeCache() {
	globalTypeCache.cache.Range(func(key, value interface{}) bool {
		globalTypeCache.cache.Delete(key)
		return true
	})
}
