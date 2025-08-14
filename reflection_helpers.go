package godi

import (
	"fmt"
	"reflect"
	"strings"
)

// TypeFormatter provides utilities for formatting types in error messages.
type TypeFormatter struct{}

// FormatType formats a reflect.Type for display.
func (tf *TypeFormatter) FormatType(t reflect.Type) string {
	if t == nil {
		return "<nil>"
	}

	// Handle common types with simpler names
	switch t.Kind() {
	case reflect.Ptr:
		return "*" + tf.FormatType(t.Elem())
	case reflect.Slice:
		return "[]" + tf.FormatType(t.Elem())
	case reflect.Map:
		return fmt.Sprintf("map[%s]%s", tf.FormatType(t.Key()), tf.FormatType(t.Elem()))
	case reflect.Chan:
		dir := ""
		switch t.ChanDir() {
		case reflect.RecvDir:
			dir = "<-"
		case reflect.SendDir:
			dir = "->"
		}
		return fmt.Sprintf("%schan %s", dir, tf.FormatType(t.Elem()))
	case reflect.Func:
		return tf.formatFunc(t)
	case reflect.Interface:
		if t.Name() != "" {
			return t.Name()
		}
		return "interface{}"
	case reflect.Struct:
		if t.Name() != "" {
			return t.Name()
		}
		return "struct{...}"
	default:
		return t.String()
	}
}

// formatFunc formats a function type.
func (tf *TypeFormatter) formatFunc(t reflect.Type) string {
	var params []string
	for i := 0; i < t.NumIn(); i++ {
		params = append(params, tf.FormatType(t.In(i)))
	}

	var returns []string
	for i := 0; i < t.NumOut(); i++ {
		returns = append(returns, tf.FormatType(t.Out(i)))
	}

	paramStr := strings.Join(params, ", ")
	returnStr := strings.Join(returns, ", ")

	if len(returns) == 0 {
		return fmt.Sprintf("func(%s)", paramStr)
	} else if len(returns) == 1 {
		return fmt.Sprintf("func(%s) %s", paramStr, returnStr)
	} else {
		return fmt.Sprintf("func(%s) (%s)", paramStr, returnStr)
	}
}

// FormatConstructor formats constructor info for display.
func (tf *TypeFormatter) FormatConstructor(info *ConstructorInfo) string {
	if info == nil {
		return "<nil>"
	}

	if !info.IsFunc {
		return fmt.Sprintf("value[%s]", tf.FormatType(info.Type))
	}

	if info.IsParamObject {
		var fields []string
		for _, param := range info.Parameters {
			field := param.Name + " " + tf.FormatType(param.Type)
			if param.Optional {
				field += " `optional`"
			}
			if param.Key != nil {
				field += fmt.Sprintf(" `name:%v`", param.Key)
			}
			if param.Group != "" {
				field += fmt.Sprintf(" `group:%s`", param.Group)
			}
			fields = append(fields, field)
		}
		return fmt.Sprintf("func(struct{In; %s})", strings.Join(fields, "; "))
	}

	return tf.FormatType(info.Type)
}

// ValidateConstructor performs validation on analyzed constructor info.
type ValidateConstructor struct {
	analyzer *Analyzer
}

// NewValidator creates a new constructor validator.
func NewValidator(analyzer *Analyzer) *ValidateConstructor {
	return &ValidateConstructor{analyzer: analyzer}
}

// Validate validates a constructor for use in DI.
func (v *ValidateConstructor) Validate(info *ConstructorInfo) error {
	if info == nil {
		return fmt.Errorf("constructor info is nil")
	}

	if !info.IsFunc {
		// Non-functions are valid (treated as instances)
		return nil
	}

	// Check return values
	if err := v.validateReturns(info); err != nil {
		return err
	}

	// Check parameters
	if err := v.validateParameters(info); err != nil {
		return err
	}

	return nil
}

// validateReturns validates constructor return values.
func (v *ValidateConstructor) validateReturns(info *ConstructorInfo) error {
	if len(info.Returns) == 0 {
		return fmt.Errorf("constructor must return at least one value")
	}

	if info.IsResultObject {
		// Result objects can return (Out) or (Out, error)
		if info.Type.NumOut() > 2 {
			return fmt.Errorf("constructor with Out result can return at most 2 values")
		}

		if info.Type.NumOut() == 2 && !info.HasErrorReturn {
			return fmt.Errorf("second return value must be error for Out result")
		}

		// Check that Out struct has at least one field
		if len(info.Returns) == 0 {
			return fmt.Errorf("Out struct must have at least one exported field")
		}
	} else {
		// Regular constructors can return (T) or (T, error)
		if info.Type.NumOut() > 2 {
			return fmt.Errorf("constructor can return at most 2 values")
		}

		if info.Type.NumOut() == 2 && !info.HasErrorReturn {
			return fmt.Errorf("second return value must be error")
		}

		// Check that we're not returning only error
		hasNonError := false
		for _, ret := range info.Returns {
			if !ret.IsError {
				hasNonError = true
				break
			}
		}

		if !hasNonError {
			return fmt.Errorf("constructor must return at least one non-error value")
		}
	}

	return nil
}

// validateParameters validates constructor parameters.
func (v *ValidateConstructor) validateParameters(info *ConstructorInfo) error {
	if info.IsParamObject {
		// Check for duplicate field names
		seen := make(map[string]bool)
		for _, param := range info.Parameters {
			if seen[param.Name] {
				return fmt.Errorf("duplicate field name in In struct: %s", param.Name)
			}
			seen[param.Name] = true
		}

		// Check that group fields are slices
		for _, param := range info.Parameters {
			if param.Group != "" && !param.IsSlice {
				return fmt.Errorf("field %s with group tag must be a slice", param.Name)
			}
		}
	} else {
		// For regular functions, check that we don't have multiple In parameters
		inCount := 0
		for i := 0; i < info.Type.NumIn(); i++ {
			if v.analyzer.hasEmbeddedType(info.Type.In(i), v.analyzer.inType) {
				inCount++
			}
		}

		if inCount > 1 {
			return fmt.Errorf("constructor cannot have multiple In parameters")
		}
	}

	return nil
}

// DependencyMatcher helps match dependencies to providers.
type DependencyMatcher struct {
	formatter *TypeFormatter
}

// NewMatcher creates a new dependency matcher.
func NewMatcher() *DependencyMatcher {
	return &DependencyMatcher{
		formatter: &TypeFormatter{},
	}
}

// MatchType checks if a provider type satisfies a dependency type.
func (m *DependencyMatcher) MatchType(providerType, dependencyType reflect.Type) bool {
	// Exact match
	if providerType == dependencyType {
		return true
	}

	// Check if provider implements dependency interface
	if dependencyType.Kind() == reflect.Interface {
		return providerType.Implements(dependencyType)
	}

	// Check if provider is assignable to dependency
	return providerType.AssignableTo(dependencyType)
}

// MatchKey checks if provider and dependency keys match.
func (m *DependencyMatcher) MatchKey(providerKey, dependencyKey any) bool {
	// Both nil means no key constraint
	if providerKey == nil && dependencyKey == nil {
		return true
	}

	// If dependency has no key requirement, any provider matches
	if dependencyKey == nil {
		return true
	}

	// Keys must match exactly
	return providerKey == dependencyKey
}

// MatchGroup checks if a provider belongs to a dependency's group.
func (m *DependencyMatcher) MatchGroup(providerGroups []string, dependencyGroup string) bool {
	if dependencyGroup == "" {
		return false
	}

	for _, group := range providerGroups {
		if group == dependencyGroup {
			return true
		}
	}

	return false
}

// FormatDependencyError creates a formatted error message for missing dependencies.
func (m *DependencyMatcher) FormatDependencyError(depType reflect.Type, key any, group string) string {
	if group != "" {
		return fmt.Sprintf("no providers found for group %q of type %s",
			group, m.formatter.FormatType(depType))
	}

	if key != nil {
		return fmt.Sprintf("no provider found for %s with key %q",
			m.formatter.FormatType(depType), key)
	}

	return fmt.Sprintf("no provider found for %s", m.formatter.FormatType(depType))
}
