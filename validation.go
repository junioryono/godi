package godi

import (
	"reflect"

	"github.com/junioryono/godi/internal/typecache"
)

// validateConstructor validates a constructor using cached type information.
func validateConstructor(constructor interface{}) error {
	if constructor == nil {
		return ErrNilConstructor
	}

	fnType := reflect.TypeOf(constructor)
	fnInfo := typecache.GetTypeInfo(fnType)

	if !fnInfo.IsFunc {
		return ErrConstructorNotFunction
	}

	// Check if any parameter uses In
	usesDigIn := false
	for _, inType := range fnInfo.InTypes {
		inInfo := typecache.GetTypeInfo(inType)
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
		outInfo := typecache.GetTypeInfo(fnInfo.OutTypes[0])
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

// validateDecorator validates that a decorator is compatible with dig.
func validateDecorator(decorator interface{}) error {
	if decorator == nil {
		return ErrDecoratorNil
	}

	fnType := reflect.TypeOf(decorator)
	fnInfo := typecache.GetTypeInfo(fnType)

	if !fnInfo.IsFunc {
		return ErrDecoratorNotFunction
	}

	// Decorators must have at least one input (the value being decorated)
	if fnInfo.NumIn == 0 {
		return ErrDecoratorNoParams
	}

	// Decorators must return at least one value
	if fnInfo.NumOut == 0 {
		return ErrDecoratorNoReturn
	}

	return nil
}
