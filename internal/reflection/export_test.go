package reflection

import (
	"errors"
	"fmt"
	"reflect"
)

// ErrServiceNotFound stands in for godi.ErrServiceNotFound: analyzers built
// with WithNotFound(NotFoundPolicy) treat it as "not registered".
var ErrServiceNotFound = errors.New("service not found")

// NotFoundPolicy is the WithNotFound policy for ErrServiceNotFound.
func NotFoundPolicy(err error) bool { return err == ErrServiceNotFound }

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

	structType := paramType
	if structType.Kind() == reflect.Pointer {
		structType = structType.Elem()
	}
	if structType.Kind() != reflect.Struct {
		return reflect.Value{}, fmt.Errorf("param type must be struct, got %v", structType.Kind())
	}

	// Analyze the struct's fields, then populate it from that analysis.
	info := &ConstructorInfo{}
	if err := b.analyzer.analyzeParamObject(info, paramType); err != nil {
		return reflect.Value{}, err
	}

	return b.buildParamObject(paramType, info.Parameters, resolver)
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

// Clear clears the analysis cache.
func (a *Analyzer) Clear() {
	a.mu.Lock()
	a.cache = make(map[reflect.Value]*ConstructorInfo)
	a.mu.Unlock()
}

// CacheSize returns the number of cached analyses.
func (a *Analyzer) CacheSize() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return len(a.cache)
}
