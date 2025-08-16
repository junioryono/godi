package reflection

import (
	"fmt"
	"reflect"
	"sync"
)

type In struct{}
type Out struct{}

var (
	inType  = reflect.TypeOf((*In)(nil)).Elem()
	outType = reflect.TypeOf((*Out)(nil)).Elem()
	errType = reflect.TypeOf((*error)(nil)).Elem()
)

// Analyzer performs reflection-based analysis of constructors and types.
// It caches analysis results for performance.
type Analyzer struct {
	mu    sync.RWMutex
	cache map[uintptr]*ConstructorInfo
}

// ConstructorInfo contains analyzed information about a constructor function or instance.
type ConstructorInfo struct {
	Type           reflect.Type
	Value          reflect.Value
	Parameters     []ParameterInfo
	Returns        []ReturnInfo
	IsFunc         bool // True if this is a function constructor
	InstanceValue  any  // The actual instance value when IsInstance is true
	IsParamObject  bool // Has In embedded struct
	IsResultObject bool // Has Out embedded struct
	HasErrorReturn bool // Returns error as last value

	// Cached for performance
	dependencies []*Dependency
}

// ParameterInfo describes a constructor parameter or field in an In struct.
type ParameterInfo struct {
	Type     reflect.Type
	Name     string       // Field name for In structs
	Tag      string       // Full tag string
	Index    int          // Parameter index or field index
	Optional bool         // From optional:"true" tag
	Group    string       // From group:"name" tag
	Key      any          // From name:"key" tag
	IsSlice  bool         // True if this is a slice type (for groups)
	ElemType reflect.Type // Element type if slice
}

// ReturnInfo describes a constructor return value or field in an Out struct.
type ReturnInfo struct {
	Type    reflect.Type
	Name    string // Field name for Out structs
	Tag     string // Full tag string
	Index   int    // Return index or field index
	Group   string // From group:"name" tag
	Key     any    // From name:"key" tag
	IsError bool   // True if this is error type
}

// TagInfo contains parsed struct tag information.
type TagInfo struct {
	Optional bool
	Name     string
	Group    string
	Ignore   bool
}

// Dependency represents a single dependency of a provider or decorator
type Dependency struct {
	// Type of the dependency
	Type reflect.Type

	// Key for named dependencies (optional)
	Key any

	// Group for group dependencies (optional)
	Group string

	// Optional indicates if this dependency can be nil
	Optional bool

	// Index is the parameter position (for regular functions)
	Index int

	// FieldName is the field name (for param objects)
	FieldName string
}

// ResultField represents a field in a result object (Out struct)
type ResultField struct {
	Name  string
	Type  reflect.Type
	Key   any    // for named results
	Group string // for group results
	Index int    // field index in struct
}

// ParamField represents a field in a parameter object (In struct)
type ParamField struct {
	Name     string
	Type     reflect.Type
	Key      any    // for named dependencies
	Group    string // for group dependencies
	Optional bool
	Index    int // field index in struct
}

// New creates a new Analyzer.
func New() *Analyzer {
	return &Analyzer{
		cache: make(map[uintptr]*ConstructorInfo),
	}
}

// Analyze analyzes a constructor function and extracts dependency information.
func (a *Analyzer) Analyze(constructor any) (*ConstructorInfo, error) {
	if constructor == nil {
		return nil, fmt.Errorf("constructor cannot be nil")
	}

	val := reflect.ValueOf(constructor)

	// Check for nil function values (typed nil)
	if !val.IsValid() || (val.Kind() == reflect.Func && val.IsNil()) {
		return nil, fmt.Errorf("constructor cannot be nil")
	}

	typ := reflect.TypeOf(constructor)

	// This ensures different functions with the same signature are cached separately
	var cacheKey uintptr
	if typ.Kind() == reflect.Func && val.CanAddr() {
		// For functions, use the function pointer as the cache key
		cacheKey = val.Pointer()
	} else if typ.Kind() == reflect.Func {
		// For non-addressable functions, use the pointer from Value
		cacheKey = val.Pointer()
	} else {
		// For non-functions, we can still use the type's address as a fallback
		// Note: This won't differentiate between different instances of the same type
		cacheKey = reflect.ValueOf(typ).Pointer()
	}

	// Check cache first
	a.mu.RLock()
	if cached, ok := a.cache[cacheKey]; ok {
		a.mu.RUnlock()
		return cached, nil
	}
	a.mu.RUnlock()

	// Perform analysis
	info := &ConstructorInfo{
		Type:  typ,
		Value: val,
	}

	// Check if it's a function
	if typ.Kind() != reflect.Func {
		info.InstanceValue = constructor
		info.Parameters = []ParameterInfo{}
		info.dependencies = []*Dependency{}
		return a.cacheAndReturn(cacheKey, info)
	}

	// It's a function constructor
	info.IsFunc = true

	// Analyze parameters
	if err := a.analyzeParameters(info); err != nil {
		return nil, fmt.Errorf("failed to analyze parameters: %w", err)
	}

	// Analyze return values
	if err := a.analyzeReturns(info); err != nil {
		return nil, fmt.Errorf("failed to analyze returns: %w", err)
	}

	// Build dependencies
	info.dependencies = a.buildDependencies(info)

	return a.cacheAndReturn(cacheKey, info)
}

// analyzeParameters analyzes function parameters or In struct fields.
func (a *Analyzer) analyzeParameters(info *ConstructorInfo) error {
	fnType := info.Type

	// Check for In parameter object
	if fnType.NumIn() == 1 {
		paramType := fnType.In(0)
		if hasEmbeddedType(paramType, inType) {
			info.IsParamObject = true
			return a.analyzeParamObject(info, paramType)
		}
	}

	// Regular parameters
	info.Parameters = make([]ParameterInfo, fnType.NumIn())
	for i := 0; i < fnType.NumIn(); i++ {
		paramType := fnType.In(i)
		info.Parameters[i] = ParameterInfo{
			Type:     paramType,
			Index:    i,
			IsSlice:  paramType.Kind() == reflect.Slice,
			ElemType: a.getSliceElemType(paramType),
		}
	}

	return nil
}

// analyzeParamObject analyzes an In struct's fields.
func (a *Analyzer) analyzeParamObject(info *ConstructorInfo, structType reflect.Type) error {
	if structType.Kind() == reflect.Pointer {
		structType = structType.Elem()
	}

	if structType.Kind() != reflect.Struct {
		return fmt.Errorf("In parameter must be a struct, got %v", structType.Kind())
	}

	params := make([]ParameterInfo, 0)

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Skip embedded In field itself
		if field.Anonymous && isInOutType(field.Type, inType) {
			continue
		}

		// Parse field tags
		tagInfo := a.parseFieldTags(field.Tag)

		// Skip ignored fields
		if tagInfo.Ignore {
			continue
		}

		param := ParameterInfo{
			Type:     field.Type,
			Name:     field.Name,
			Tag:      string(field.Tag),
			Index:    i,
			Optional: tagInfo.Optional,
			Group:    tagInfo.Group,
			IsSlice:  field.Type.Kind() == reflect.Slice,
			ElemType: a.getSliceElemType(field.Type),
		}

		// Set key from name tag
		if tagInfo.Name != "" {
			param.Key = tagInfo.Name
		}

		params = append(params, param)
	}

	info.Parameters = params
	return nil
}

// analyzeReturns analyzes function return values or Out struct fields.
func (a *Analyzer) analyzeReturns(info *ConstructorInfo) error {
	fnType := info.Type

	if fnType.NumOut() == 0 {
		return nil
	}

	// Check for Out result object
	firstReturn := fnType.Out(0)
	if hasEmbeddedType(firstReturn, outType) {
		info.IsResultObject = true
		return a.analyzeResultObject(info, firstReturn)
	}

	// Handle multiple returns (including multiple non-error returns)
	info.Returns = make([]ReturnInfo, 0, fnType.NumOut())

	for i := 0; i < fnType.NumOut(); i++ {
		retType := fnType.Out(i)
		isError := implementsError(retType)

		// Check if this is the last return and it's an error
		if isError && i == fnType.NumOut()-1 {
			info.HasErrorReturn = true
			// Still add it to Returns for completeness, but mark as error
			info.Returns = append(info.Returns, ReturnInfo{
				Type:    retType,
				Index:   i,
				IsError: true,
			})
		} else if isError {
			// Error in non-last position is treated as a regular type
			// This allows for advanced use cases while maintaining backward compatibility
			info.Returns = append(info.Returns, ReturnInfo{
				Type:    retType,
				Index:   i,
				IsError: false, // Treat as regular type if not in last position
			})
		} else {
			// Regular non-error return
			info.Returns = append(info.Returns, ReturnInfo{
				Type:    retType,
				Index:   i,
				IsError: false,
			})
		}
	}

	return nil
}

// analyzeResultObject analyzes an Out struct's fields.
func (a *Analyzer) analyzeResultObject(info *ConstructorInfo, structType reflect.Type) error {
	if structType.Kind() == reflect.Pointer {
		structType = structType.Elem()
	}

	if structType.Kind() != reflect.Struct {
		return fmt.Errorf("Out result must be a struct, got %v", structType.Kind())
	}

	returns := make([]ReturnInfo, 0)

	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Skip embedded Out field itself
		if field.Anonymous && isInOutType(field.Type, outType) {
			continue
		}

		// Parse field tags
		tagInfo := a.parseFieldTags(field.Tag)

		// Skip ignored fields
		if tagInfo.Ignore {
			continue
		}

		ret := ReturnInfo{
			Type:  field.Type,
			Name:  field.Name,
			Tag:   string(field.Tag),
			Index: i,
			Group: tagInfo.Group,
		}

		// Set key from name tag
		if tagInfo.Name != "" {
			ret.Key = tagInfo.Name
		}

		returns = append(returns, ret)
	}

	info.Returns = returns

	// Check if function also returns error
	if info.Type.NumOut() == 2 {
		secondReturn := info.Type.Out(1)
		if implementsError(secondReturn) {
			info.HasErrorReturn = true
		}
	}

	return nil
}

// buildDependencies creates Dependency objects from ParameterInfo.
func (a *Analyzer) buildDependencies(info *ConstructorInfo) []*Dependency {
	deps := make([]*Dependency, 0, len(info.Parameters))

	for _, param := range info.Parameters {
		dep := &Dependency{
			Type:      param.Type,
			Key:       param.Key,
			Group:     param.Group,
			Optional:  param.Optional,
			Index:     param.Index,
			FieldName: param.Name,
		}

		// For slices with group tags, the dependency is on the element type
		if param.IsSlice && param.Group != "" && param.ElemType != nil {
			dep.Type = param.ElemType
		}

		deps = append(deps, dep)
	}

	return deps
}

// GetDependencies returns the analyzed dependencies for a constructor.
func (a *Analyzer) GetDependencies(constructor any) ([]*Dependency, error) {
	info, err := a.Analyze(constructor)
	if err != nil {
		return nil, err
	}

	return info.dependencies, nil
}

// GetServiceType determines the primary service type from a constructor or instance.
func (a *Analyzer) GetServiceType(constructor any) (reflect.Type, error) {
	info, err := a.Analyze(constructor)
	if err != nil {
		return nil, err
	}

	if !info.IsFunc {
		// For instances, the type is the type of the value
		return info.Type, nil
	}

	if len(info.Returns) == 0 {
		return nil, fmt.Errorf("constructor has no return values")
	}

	// For result objects, return the Out struct type
	if info.IsResultObject {
		return info.Type.Out(0), nil
	}

	// Return the first non-error return type
	for _, ret := range info.Returns {
		if !ret.IsError {
			return ret.Type, nil
		}
	}

	return nil, fmt.Errorf("constructor only returns error")
}

// GetResultTypes returns all types produced by a constructor (for Out structs or multiple returns).
func (a *Analyzer) GetResultTypes(constructor any) ([]reflect.Type, error) {
	info, err := a.Analyze(constructor)
	if err != nil {
		return nil, err
	}

	// For all cases (Out structs, multiple returns, single return),
	// return all non-error types
	types := make([]reflect.Type, 0, len(info.Returns))
	for _, ret := range info.Returns {
		if !ret.IsError {
			types = append(types, ret.Type)
		}
	}

	// If no types were found and it's not a function, return the instance type
	if len(types) == 0 && !info.IsFunc {
		return []reflect.Type{info.Type}, nil
	}

	return types, nil
}

// parseFieldTags parses struct field tags for DI-specific annotations.
func (a *Analyzer) parseFieldTags(tag reflect.StructTag) TagInfo {
	info := TagInfo{}

	// Check for optional tag
	if val, ok := tag.Lookup("optional"); ok {
		info.Optional = val == "true"
	}

	// Check for name tag (for keyed services)
	if val, ok := tag.Lookup("name"); ok {
		info.Name = val
	}

	// Check for group tag
	if val, ok := tag.Lookup("group"); ok {
		info.Group = val
	}

	// Check for ignore tag
	if val, ok := tag.Lookup("inject"); ok && val == "-" {
		info.Ignore = true
	}

	return info
}

// getSliceElemType returns the element type of a slice, or nil if not a slice.
func (a *Analyzer) getSliceElemType(t reflect.Type) reflect.Type {
	if t.Kind() == reflect.Slice {
		return t.Elem()
	}
	return nil
}

// cacheAndReturn caches the analysis result and returns it.
func (a *Analyzer) cacheAndReturn(key uintptr, info *ConstructorInfo) (*ConstructorInfo, error) {
	a.mu.Lock()
	a.cache[key] = info
	a.mu.Unlock()

	return info, nil
}

// Clear clears the analysis cache.
func (a *Analyzer) Clear() {
	a.mu.Lock()
	a.cache = make(map[uintptr]*ConstructorInfo)
	a.mu.Unlock()
}

// CacheSize returns the number of cached analyses.
func (a *Analyzer) CacheSize() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.cache)
}

// hasEmbeddedType checks if a type has an embedded field of the given type.
func hasEmbeddedType(t, embedded reflect.Type) bool {
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	if t.Kind() != reflect.Struct {
		return false
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Anonymous && isInOutType(field.Type, embedded) {
			return true
		}
	}

	return false
}

// isInOutType checks if a type matches the In or Out interface.
func isInOutType(t, target reflect.Type) bool {
	// Direct type match
	if t == target {
		return true
	}

	// Check if it implements the interface
	if target.Kind() == reflect.Interface {
		return t.Implements(target)
	}

	// No fallback - type must actually match or implement the interface
	return false
}

// implementsError checks if a type implements the error interface.
func implementsError(t reflect.Type) bool {
	return t.Implements(errType)
}
