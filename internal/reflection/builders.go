package reflection

import (
	"fmt"
	"reflect"
)

// ParamObjectBuilder builds parameter objects (In structs) with resolved dependencies.
type ParamObjectBuilder struct {
	analyzer *Analyzer
}

// NewParamObjectBuilder creates a new parameter object builder.
func NewParamObjectBuilder(analyzer *Analyzer) *ParamObjectBuilder {
	return &ParamObjectBuilder{analyzer: analyzer}
}

// BuildParamObject creates and populates an In struct with resolved dependencies.
func (b *ParamObjectBuilder) BuildParamObject(
	paramType reflect.Type,
	resolver DependencyResolver,
) (reflect.Value, error) {
	if resolver == nil {
		return reflect.Value{}, fmt.Errorf("resolver cannot be nil")
	}

	if paramType == nil {
		return reflect.Value{}, fmt.Errorf("paramType cannot be nil")
	}

	// Get struct type (dereference if pointer)
	structType := paramType
	if structType.Kind() == reflect.Ptr {
		structType = structType.Elem()
	}

	if structType.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("param type must be struct, got %v", structType.Kind())
	}

	// Create new instance of the param struct
	// Always create a pointer first, then we'll convert if needed
	structPtr := reflect.New(structType)
	structValue := structPtr.Elem()

	// Populate each field
	for i := 0; i < structType.NumField(); i++ {
		field := structType.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Skip embedded In field
		if field.Anonymous && b.isInField(field.Type) {
			continue
		}

		// Parse tags
		tagInfo := b.analyzer.parseFieldTags(field.Tag)

		// Skip ignored fields
		if tagInfo.Ignore {
			continue
		}

		// Resolve dependency for this field
		fieldValue, err := b.resolveFieldDependency(field, tagInfo, resolver)
		if err != nil {
			if !tagInfo.Optional {
				return reflect.Value{}, fmt.Errorf("failed to resolve field %s: %w", field.Name, err)
			}
			// Optional field - leave as zero value
			continue
		}

		// Set the field value
		fieldToSet := structValue.Field(i)
		if fieldToSet.CanSet() && fieldValue.IsValid() {
			fieldToSet.Set(fieldValue)
		}
	}

	// Return the appropriate type (pointer or value)
	if paramType.Kind() == reflect.Ptr {
		return structPtr, nil
	}
	return structValue, nil
}

// resolveFieldDependency resolves a single field's dependency.
func (b *ParamObjectBuilder) resolveFieldDependency(
	field reflect.StructField,
	tagInfo TagInfo,
	resolver DependencyResolver,
) (reflect.Value, error) {
	fieldType := field.Type

	// Handle group dependencies (slices)
	if tagInfo.Group != "" {
		if fieldType.Kind() != reflect.Slice {
			return reflect.Value{}, fmt.Errorf("group field must be slice, got %v", fieldType.Kind())
		}

		elemType := fieldType.Elem()
		values, err := resolver.GetGroup(elemType, tagInfo.Group)
		if err != nil {
			return reflect.Value{}, err
		}

		// Create slice with resolved values
		slice := reflect.MakeSlice(fieldType, len(values), len(values))
		for i, val := range values {
			slice.Index(i).Set(reflect.ValueOf(val))
		}

		return slice, nil
	}

	// Handle keyed dependencies
	if tagInfo.Name != "" {
		value, err := resolver.GetKeyed(fieldType, tagInfo.Name)
		if err != nil {
			return reflect.Value{}, err
		}
		return reflect.ValueOf(value), nil
	}

	// Regular dependency
	value, err := resolver.Get(fieldType)
	if err != nil {
		return reflect.Value{}, err
	}

	return reflect.ValueOf(value), nil
}

// isInField checks if a type is the In marker type.
func (b *ParamObjectBuilder) isInField(t reflect.Type) bool {
	return b.analyzer.isInOutType(t, b.analyzer.inType)
}

// ResultObjectProcessor processes result objects (Out structs) after construction.
type ResultObjectProcessor struct {
	analyzer *Analyzer
}

// NewResultObjectProcessor creates a new result object processor.
func NewResultObjectProcessor(analyzer *Analyzer) *ResultObjectProcessor {
	return &ResultObjectProcessor{analyzer: analyzer}
}

// ProcessResultObject extracts services from an Out struct.
func (p *ResultObjectProcessor) ProcessResultObject(
	result reflect.Value,
	resultType reflect.Type,
) ([]ServiceRegistration, error) {
	// Handle pointer to struct
	if result.Kind() == reflect.Ptr {
		if result.IsNil() {
			return nil, fmt.Errorf("result object is nil")
		}
		result = result.Elem()
	}

	if resultType.Kind() == reflect.Ptr {
		resultType = resultType.Elem()
	}

	if result.Kind() != reflect.Struct {
		return nil, fmt.Errorf("result must be struct, got %v", result.Kind())
	}

	registrations := make([]ServiceRegistration, 0)

	// Process each field
	for i := 0; i < resultType.NumField(); i++ {
		field := resultType.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Skip embedded Out field
		if field.Anonymous && p.isOutField(field.Type) {
			continue
		}

		// Parse tags
		tagInfo := p.analyzer.parseFieldTags(field.Tag)

		// Skip ignored fields
		if tagInfo.Ignore {
			continue
		}

		// Get field value
		fieldValue := result.Field(i)
		if !fieldValue.IsValid() {
			continue
		}

		// Skip nil values for types that can be nil
		switch fieldValue.Kind() {
		case reflect.Ptr, reflect.Interface, reflect.Slice, reflect.Map, reflect.Chan, reflect.Func:
			if fieldValue.IsNil() {
				continue
			}
		}

		// Create service registration
		reg := ServiceRegistration{
			Type:  field.Type,
			Value: fieldValue.Interface(),
			Name:  field.Name,
			Key:   tagInfo.Name,
			Group: tagInfo.Group,
		}

		registrations = append(registrations, reg)
	}

	return registrations, nil
}

// isOutField checks if a type is the Out marker type.
func (p *ResultObjectProcessor) isOutField(t reflect.Type) bool {
	return p.analyzer.isInOutType(t, p.analyzer.outType)
}

// ServiceRegistration represents a service to be registered from an Out struct.
type ServiceRegistration struct {
	Type  reflect.Type
	Value any
	Name  string // Field name
	Key   string // From name tag
	Group string // From group tag
}

// DependencyResolver is the interface for resolving dependencies.
// This will be implemented by the actual resolver.
type DependencyResolver interface {
	Get(t reflect.Type) (any, error)
	GetKeyed(t reflect.Type, key any) (any, error)
	GetGroup(t reflect.Type, group string) ([]any, error)
}

// ConstructorInvoker invokes constructors with resolved dependencies.
type ConstructorInvoker struct {
	analyzer     *Analyzer
	paramBuilder *ParamObjectBuilder
}

// NewConstructorInvoker creates a new constructor invoker.
func NewConstructorInvoker(analyzer *Analyzer) *ConstructorInvoker {
	return &ConstructorInvoker{
		analyzer:     analyzer,
		paramBuilder: NewParamObjectBuilder(analyzer),
	}
}

// Invoke calls a constructor with resolved dependencies.
func (ci *ConstructorInvoker) Invoke(
	info *ConstructorInfo,
	resolver DependencyResolver,
) ([]reflect.Value, error) {
	if !info.IsFunc {
		// Non-function, return the value itself
		return []reflect.Value{info.Value}, nil
	}

	// Build arguments
	args, err := ci.buildArguments(info, resolver)
	if err != nil {
		return nil, fmt.Errorf("failed to build arguments: %w", err)
	}

	// Call the constructor
	results := info.Value.Call(args)

	// Check for error return
	if info.HasErrorReturn && len(results) > 0 {
		lastResult := results[len(results)-1]
		if !lastResult.IsNil() {
			if err, ok := lastResult.Interface().(error); ok {
				return nil, fmt.Errorf("constructor error: %w", err)
			}
		}
	}

	return results, nil
}

// buildArguments builds the argument list for a constructor.
func (ci *ConstructorInvoker) buildArguments(
	info *ConstructorInfo,
	resolver DependencyResolver,
) ([]reflect.Value, error) {
	if info.IsParamObject {
		// Build the In struct
		paramType := info.Type.In(0)
		paramValue, err := ci.paramBuilder.BuildParamObject(paramType, resolver)
		if err != nil {
			return nil, err
		}
		return []reflect.Value{paramValue}, nil
	}

	// Regular parameters - resolve each one
	args := make([]reflect.Value, len(info.Parameters))
	for i, param := range info.Parameters {
		value, err := ci.resolveParameter(param, resolver)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve parameter %d: %w", i, err)
		}
		args[i] = reflect.ValueOf(value)
	}

	return args, nil
}

// resolveParameter resolves a single parameter.
func (ci *ConstructorInvoker) resolveParameter(
	param ParameterInfo,
	resolver DependencyResolver,
) (any, error) {
	// Handle group parameters
	if param.Group != "" {
		values, err := resolver.GetGroup(param.ElemType, param.Group)
		if err != nil {
			return nil, err
		}

		// Create a slice of the correct type and populate it
		slice := reflect.MakeSlice(param.Type, len(values), len(values))
		for i, val := range values {
			slice.Index(i).Set(reflect.ValueOf(val))
		}
		return slice.Interface(), nil
	}

	// Handle keyed parameters
	if param.Key != nil {
		return resolver.GetKeyed(param.Type, param.Key)
	}

	// Regular parameter
	return resolver.Get(param.Type)
}
