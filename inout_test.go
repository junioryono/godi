package godi_test

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/junioryono/godi"
	"go.uber.org/dig"
)

// Test types for In/Out functionality
type (
	// Services for testing
	inoutTestLogger interface {
		Log(string)
	}

	inoutTestDatabase interface {
		Query(string) string
	}

	// Parameter objects using In
	simpleInParams struct {
		godi.In

		Logger inoutTestLogger
		DB     inoutTestDatabase
	}

	// Result objects using Out
	simpleOutResult struct {
		godi.Out

		Logger inoutTestLogger
		DB     inoutTestDatabase
	}

	// Handler for group tests
	Handler interface {
		Handle()
	}
)

func TestInOut_TypeAliases(t *testing.T) {
	t.Run("In type alias", func(t *testing.T) {
		// Verify that our In is the same as dig.In
		var godiIn godi.In
		var digIn dig.In

		godiType := reflect.TypeOf(godiIn)
		digType := reflect.TypeOf(digIn)

		if godiType != digType {
			t.Error("godi.In should be the same type as dig.In")
		}
	})

	t.Run("Out type alias", func(t *testing.T) {
		// Verify that our Out is the same as dig.Out
		var godiOut godi.Out
		var digOut dig.Out

		godiType := reflect.TypeOf(godiOut)
		digType := reflect.TypeOf(digOut)

		if godiType != digType {
			t.Error("godi.Out should be the same type as dig.Out")
		}
	})
}

func TestInOut_IsInIsOut(t *testing.T) {
	t.Run("IsIn function", func(t *testing.T) {
		// Test with In struct
		if !dig.IsIn(simpleInParams{}) {
			t.Error("expected IsIn to return true for struct with embedded In")
		}

		// Test with non-In struct
		if dig.IsIn(struct{}{}) {
			t.Error("expected IsIn to return false for regular struct")
		}

		// Test with type instead of value
		if !dig.IsIn(reflect.TypeOf(simpleInParams{})) {
			t.Error("expected IsIn to work with reflect.Type")
		}
	})

	t.Run("IsOut function", func(t *testing.T) {
		// Test with Out struct
		if !dig.IsOut(simpleOutResult{}) {
			t.Error("expected IsOut to return true for struct with embedded Out")
		}

		// Test with non-Out struct
		if dig.IsOut(struct{}{}) {
			t.Error("expected IsOut to return false for regular struct")
		}

		// Test with type instead of value
		if !dig.IsOut(reflect.TypeOf(simpleOutResult{})) {
			t.Error("expected IsOut to work with reflect.Type")
		}
	})
}

func TestInOut_ProvideOptions(t *testing.T) {
	t.Run("Name function", func(t *testing.T) {
		// Test that Name returns a valid ProvideOption
		opt := godi.Name("test-name")
		if opt == nil {
			t.Error("Name should return non-nil ProvideOption")
		}

		// Verify it's the same function as dig.Name
		digOpt := dig.Name("test-name")

		// We can't directly compare functions, but we can check their string representations
		if formatOption(opt) != formatOption(digOpt) {
			t.Error("godi.Name should produce same option as dig.Name")
		}
	})

	t.Run("Group function", func(t *testing.T) {
		opt := godi.Group("test-group")
		if opt == nil {
			t.Error("Group should return non-nil ProvideOption")
		}

		digOpt := dig.Group("test-group")
		if formatOption(opt) != formatOption(digOpt) {
			t.Error("godi.Group should produce same option as dig.Group")
		}
	})

	t.Run("As function", func(t *testing.T) {
		opt := godi.As(new(inoutTestLogger), new(inoutTestDatabase))
		if opt == nil {
			t.Error("As should return non-nil ProvideOption")
		}

		// Test with single interface
		opt = godi.As(new(inoutTestLogger))
		if opt == nil {
			t.Error("As should work with single interface")
		}
	})

	t.Run("FillProvideInfo function", func(t *testing.T) {
		var info godi.ProvideInfo
		opt := godi.FillProvideInfo(&info)
		if opt == nil {
			t.Error("FillProvideInfo should return non-nil ProvideOption")
		}
	})
}

func TestInOut_DecorateOptions(t *testing.T) {
	t.Run("FillDecorateInfo function", func(t *testing.T) {
		var info godi.DecorateInfo
		opt := godi.FillDecorateInfo(&info)
		if opt == nil {
			t.Error("FillDecorateInfo should return non-nil DecorateOption")
		}
	})
}

func TestInOut_InvokeOptions(t *testing.T) {
	t.Run("FillInvokeInfo function", func(t *testing.T) {
		var info godi.InvokeInfo
		opt := godi.FillInvokeInfo(&info)
		if opt == nil {
			t.Error("FillInvokeInfo should return non-nil InvokeOption")
		}
	})
}

func TestInOut_Callbacks(t *testing.T) {
	t.Run("WithProviderCallback function", func(t *testing.T) {
		callback := func(ci godi.CallbackInfo) {
			// declared and not used: called
		}

		opt := godi.WithProviderCallback(callback)
		if opt == nil {
			t.Error("WithProviderCallback should return non-nil ProvideOption")
		}
	})

	t.Run("WithProviderBeforeCallback function", func(t *testing.T) {
		callback := func(bci godi.BeforeCallbackInfo) {
			// declared and not used: called
		}

		opt := godi.WithProviderBeforeCallback(callback)
		if opt == nil {
			t.Error("WithProviderBeforeCallback should return non-nil ProvideOption")
		}
	})

	t.Run("WithDecoratorCallback function", func(t *testing.T) {
		callback := func(ci godi.CallbackInfo) {
			// declared and not used: called
		}

		opt := godi.WithDecoratorCallback(callback)
		if opt == nil {
			t.Error("WithDecoratorCallback should return non-nil DecorateOption")
		}
	})

	t.Run("WithDecoratorBeforeCallback function", func(t *testing.T) {
		callback := func(bci godi.BeforeCallbackInfo) {
			// declared and not used: called
		}

		opt := godi.WithDecoratorBeforeCallback(callback)
		if opt == nil {
			t.Error("WithDecoratorBeforeCallback should return non-nil DecorateOption")
		}
	})
}

func TestInOut_TypeAliasesInfo(t *testing.T) {
	t.Run("info type aliases are same as dig types", func(t *testing.T) {
		// These should all be true since they're type aliases
		var provideInfo godi.ProvideInfo
		var digProvideInfo dig.ProvideInfo
		if reflect.TypeOf(provideInfo) != reflect.TypeOf(digProvideInfo) {
			t.Error("ProvideInfo should be same type as dig.ProvideInfo")
		}

		var decorateInfo godi.DecorateInfo
		var digDecorateInfo dig.DecorateInfo
		if reflect.TypeOf(decorateInfo) != reflect.TypeOf(digDecorateInfo) {
			t.Error("DecorateInfo should be same type as dig.DecorateInfo")
		}

		var invokeInfo godi.InvokeInfo
		var digInvokeInfo dig.InvokeInfo
		if reflect.TypeOf(invokeInfo) != reflect.TypeOf(digInvokeInfo) {
			t.Error("InvokeInfo should be same type as dig.InvokeInfo")
		}

		var input godi.Input
		var digInput dig.Input
		if reflect.TypeOf(input) != reflect.TypeOf(digInput) {
			t.Error("Input should be same type as dig.Input")
		}

		var output godi.Output
		var digOutput dig.Output
		if reflect.TypeOf(output) != reflect.TypeOf(digOutput) {
			t.Error("Output should be same type as dig.Output")
		}

		var callbackInfo godi.CallbackInfo
		var digCallbackInfo dig.CallbackInfo
		if reflect.TypeOf(callbackInfo) != reflect.TypeOf(digCallbackInfo) {
			t.Error("CallbackInfo should be same type as dig.CallbackInfo")
		}

		var beforeCallbackInfo godi.BeforeCallbackInfo
		var digBeforeCallbackInfo dig.BeforeCallbackInfo
		if reflect.TypeOf(beforeCallbackInfo) != reflect.TypeOf(digBeforeCallbackInfo) {
			t.Error("BeforeCallbackInfo should be same type as dig.BeforeCallbackInfo")
		}
	})
}

func TestInOut_OptionTypes(t *testing.T) {
	t.Run("option type aliases", func(t *testing.T) {
		// Verify option types are properly aliased
		var provideOpt godi.ProvideOption
		var digProvideOpt dig.ProvideOption
		if reflect.TypeOf(provideOpt) != reflect.TypeOf(digProvideOpt) {
			t.Error("ProvideOption should be same type as dig.ProvideOption")
		}

		var decorateOpt godi.DecorateOption
		var digDecorateOpt dig.DecorateOption
		if reflect.TypeOf(decorateOpt) != reflect.TypeOf(digDecorateOpt) {
			t.Error("DecorateOption should be same type as dig.DecorateOption")
		}

		var invokeOpt godi.InvokeOption
		var digInvokeOpt dig.InvokeOption
		if reflect.TypeOf(invokeOpt) != reflect.TypeOf(digInvokeOpt) {
			t.Error("InvokeOption should be same type as dig.InvokeOption")
		}

		var scopeOpt godi.ScopeOption
		var digScopeOpt dig.ScopeOption
		if reflect.TypeOf(scopeOpt) != reflect.TypeOf(digScopeOpt) {
			t.Error("ScopeOption should be same type as dig.ScopeOption")
		}
	})
}

// Helper function to format options for comparison
func formatOption(opt interface{}) string {
	return fmt.Sprintf("%v", opt)
}
