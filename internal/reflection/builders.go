package reflection

import (
	"fmt"
	"reflect"
	"runtime/debug"
	"sync"
)

// argsPool reuses []reflect.Value backing arrays across constructor
// invocations. The hot path resolves one slice per call; without the pool
// each Transient resolve allocates a fresh slice. Reusing the backing array
// drops one allocation per resolve.
var argsPool = sync.Pool{
	New: func() any {
		s := make([]reflect.Value, 0, 8)
		return &s
	},
}

// borrowArgs returns a []reflect.Value of length n drawn from argsPool.
// Callers must releaseArgs when done.
func borrowArgs(n int) *[]reflect.Value {
	pooled := argsPool.Get().(*[]reflect.Value)
	if cap(*pooled) < n {
		*pooled = make([]reflect.Value, n)
	} else {
		*pooled = (*pooled)[:n]
	}
	return pooled
}

// releaseArgs returns a borrowed args slice to the pool after zeroing entries
// so the pool doesn't keep references to old values alive.
func releaseArgs(p *[]reflect.Value) {
	if p == nil {
		return
	}
	s := *p
	for i := range s {
		s[i] = reflect.Value{}
	}
	*p = s[:0]
	argsPool.Put(p)
}

// ParamObjectBuilder builds parameter objects (In structs) with resolved dependencies.
type ParamObjectBuilder struct {
	analyzer *Analyzer
}

// NewParamObjectBuilder creates a new parameter object builder.
func NewParamObjectBuilder(analyzer *Analyzer) *ParamObjectBuilder {
	return &ParamObjectBuilder{analyzer: analyzer}
}

// buildParamObject creates and populates an In struct from field metadata
// already produced by Analyze. The hot resolution path uses it so struct
// fields and tags are not re-walked and re-parsed on every construction.
func (b *ParamObjectBuilder) buildParamObject(
	paramType reflect.Type,
	params []ParameterInfo,
	resolver DependencyResolver,
) (reflect.Value, error) {
	// Get struct type (dereference if pointer)
	structType := paramType
	if structType.Kind() == reflect.Pointer {
		structType = structType.Elem()
	}

	if structType.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("param type must be struct, got %v", structType.Kind())
	}

	// Create new instance of the param struct
	// Always create a pointer first, then we'll convert if needed
	structPtr := reflect.New(structType)
	structValue := structPtr.Elem()

	// Populate each analyzed field (unexported, embedded In, and ignored
	// fields were already excluded by analysis).
	for i := range params {
		param := &params[i]

		fieldValue, err := b.resolveFieldDependency(param, resolver)
		if err != nil {
			// Optional only forgives "not registered". A registered
			// dependency whose construction failed must propagate the
			// error instead of silently injecting a zero value.
			if param.Optional && b.analyzer.isNotFound(err) {
				continue
			}
			return reflect.Value{}, fmt.Errorf("failed to resolve field %s: %w", param.Name, err)
		}

		// Set the field value
		fieldToSet := structValue.Field(param.Index)
		if fieldToSet.CanSet() && fieldValue.IsValid() {
			fieldToSet.Set(fieldValue)
		}
	}

	// Return the appropriate type (pointer or value)
	if paramType.Kind() == reflect.Pointer {
		return structPtr, nil
	}
	return structValue, nil
}

// resolveFieldDependency resolves a single field's dependency.
func (b *ParamObjectBuilder) resolveFieldDependency(
	param *ParameterInfo,
	resolver DependencyResolver,
) (reflect.Value, error) {
	fieldType := param.Type

	// Handle group dependencies (slices)
	if param.Group != "" {
		if fieldType.Kind() != reflect.Slice {
			return reflect.Value{}, fmt.Errorf("group field must be slice, got %v", fieldType.Kind())
		}

		elemType := fieldType.Elem()
		values, err := resolver.GetGroup(elemType, param.Group)
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
	if param.Key != nil {
		value, err := resolver.GetKeyed(fieldType, keyName(param.Key))
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

// ResultOutput is one field of a constructed result object (Out struct).
type ResultOutput struct {
	// Index is the field index in the Out struct.
	Index int
	// Value is the field's value; nil when Present is false.
	Value any
	// Present is false when the field holds a nil value, which means the
	// constructor did not provide that output.
	Present bool
}

// ResultObjectOutputs extracts one output per entry of returns (the analyzed
// Out-struct fields, as produced by Analyze) from a constructed result object.
// The returned slice is parallel to returns. Using the analyzed metadata keeps
// the hot resolution path from re-walking struct fields and re-parsing tags.
func ResultObjectOutputs(result reflect.Value, returns []ReturnInfo) ([]ResultOutput, error) {
	if result.Kind() == reflect.Pointer {
		if result.IsNil() {
			return nil, fmt.Errorf("result object is nil")
		}
		result = result.Elem()
	}

	if result.Kind() != reflect.Struct {
		return nil, fmt.Errorf("result must be struct, got %v", result.Kind())
	}

	outputs := make([]ResultOutput, len(returns))
	for i, ret := range returns {
		fieldValue := result.Field(ret.Index)
		outputs[i].Index = ret.Index
		// Nil values are "not provided", unwrapping interfaces so a typed-nil
		// pointer stored in an interface field is not cached as a service.
		if IsNilValue(fieldValue) {
			continue
		}
		outputs[i].Value = fieldValue.Interface()
		outputs[i].Present = true
	}

	return outputs, nil
}

// IsNilValue reports whether v is invalid or a nil value, unwrapping non-nil
// interfaces to their dynamic value. Values of kinds that cannot be nil
// (structs, numbers, ...) are never nil.
func IsNilValue(v reflect.Value) bool {
	if !v.IsValid() {
		return true
	}
	for v.Kind() == reflect.Interface {
		if v.IsNil() {
			return true
		}
		v = v.Elem()
	}
	return canBeNil(v.Kind()) && v.IsNil()
}

// canBeNil reports whether values of kind k can be nil, i.e. whether
// reflect.Value.IsNil may be called on them.
func canBeNil(k reflect.Kind) bool {
	switch k {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice, reflect.UnsafePointer:
		return true
	default:
		return false
	}
}

// CanBeNil reports whether values of type t can be nil.
func CanBeNil(t reflect.Type) bool {
	return canBeNil(t.Kind())
}

// DependencyResolver is the interface for resolving dependencies.
// This will be implemented by the actual resolver.
type DependencyResolver interface {
	Get(t reflect.Type) (any, error)
	GetKeyed(t reflect.Type, name string) (any, error)
	GetGroup(t reflect.Type, group string) ([]any, error)
}

// ReturnedError is the error a constructor (or invoked function) returned.
type ReturnedError struct {
	Err error
}

func (e *ReturnedError) Error() string { return "constructor error: " + e.Err.Error() }

func (e *ReturnedError) Unwrap() error { return e.Err }

// PanicError represents a panic that occurred during constructor invocation.
// It captures the panic value and stack trace for debugging.
type PanicError struct {
	Constructor reflect.Type
	Panic       any
	Stack       []byte
}

func (e *PanicError) Error() string {
	return fmt.Sprintf("constructor %v panicked: %v", e.Constructor, e.Panic)
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

// Invoke calls a constructor with resolved dependencies or returns an instance value.
// Panics in constructors are recovered and returned as PanicError.
func (ci *ConstructorInvoker) Invoke(
	info *ConstructorInfo,
	resolver DependencyResolver,
) (results []reflect.Value, err error) {
	// Handle instance values
	if !info.IsFunc {
		// For instances, return the instance value directly
		return []reflect.Value{reflect.ValueOf(info.InstanceValue)}, nil
	}

	// Build arguments into a pooled scratch slice so the per-resolve
	// allocation cost is zero for the args backing array. The pool is
	// returned after Call (which copies the values it needs).
	argsPtr, err := ci.buildArguments(info, resolver)
	if err != nil {
		return nil, fmt.Errorf("failed to build arguments: %w", err)
	}
	defer releaseArgs(argsPtr)

	var args []reflect.Value
	if argsPtr != nil {
		args = *argsPtr
	}

	// Call the constructor with panic recovery
	results, err = ci.invokeWithRecovery(info, args)
	if err != nil {
		return nil, err
	}

	// Check for error return
	if info.HasErrorReturn && len(results) > 0 {
		lastResult := results[len(results)-1]
		// Value.IsNil panics on kinds that cannot be nil, such as a struct
		// implementing error; such a value is always a non-nil error. A
		// typed nil inside an error interface stays non-nil, as in Go.
		if !canBeNil(lastResult.Kind()) || !lastResult.IsNil() {
			if err, ok := lastResult.Interface().(error); ok {
				return nil, &ReturnedError{Err: err}
			}
		}
	}

	return results, nil
}

// invokeWithRecovery calls the constructor and recovers from any panics.
func (ci *ConstructorInvoker) invokeWithRecovery(info *ConstructorInfo, args []reflect.Value) (results []reflect.Value, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = &PanicError{
				Constructor: info.Type,
				Panic:       r,
				Stack:       debug.Stack(),
			}
		}
	}()

	results = info.Value.Call(args)
	return results, nil
}

// buildArguments builds the argument list for a constructor. The returned
// slice pointer comes from argsPool and must be released by the caller via
// releaseArgs once Call has consumed it.
func (ci *ConstructorInvoker) buildArguments(
	info *ConstructorInfo,
	resolver DependencyResolver,
) (*[]reflect.Value, error) {
	if info.IsParamObject {
		// Build the In struct
		paramType := info.Type.In(0)
		paramValue, err := ci.paramBuilder.buildParamObject(paramType, info.Parameters, resolver)
		if err != nil {
			return nil, err
		}
		argsPtr := borrowArgs(1)
		(*argsPtr)[0] = paramValue
		return argsPtr, nil
	}

	// Regular parameters - resolve each one
	numParams := len(info.Parameters)
	if numParams == 0 {
		return nil, nil
	}

	argsPtr := borrowArgs(numParams)
	args := *argsPtr
	for i, param := range info.Parameters {
		value, err := ci.resolveParameter(&param, resolver)
		if err != nil {
			releaseArgs(argsPtr)
			return nil, fmt.Errorf("failed to resolve parameter %d: %w", i, err)
		}
		args[i] = reflect.ValueOf(value)
	}

	return argsPtr, nil
}

// resolveParameter resolves a single parameter.
func (ci *ConstructorInvoker) resolveParameter(
	param *ParameterInfo,
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
		return resolver.GetKeyed(param.Type, keyName(param.Key))
	}

	// Regular parameter
	return resolver.Get(param.Type)
}

// keyName returns the name of a parameter key (from a name:"..." tag), or ""
// for an unnamed parameter.
func keyName(key any) string {
	name, _ := key.(string)
	return name
}
