package godi

import (
	"go.uber.org/dig"
)

// In embeds dig.In to leverage dig's parameter object functionality.
// When a constructor function accepts a single struct parameter with embedded In,
// dig will automatically populate all exported fields of that struct
// with the corresponding services.
//
// This is a direct wrapper around dig.In, so all dig features are supported:
//   - `optional:"true"` - Field is optional and won't cause an error if the service is not found
//   - `name:"serviceName"` - Field should be resolved as a keyed/named service
//   - `group:"groupName"` - Field should be filled from a value group (slice fields only)
//
// Example:
//
//	type ServiceParams struct {
//	    godi.In
//
//	    Database *sql.DB
//	    Logger   Logger `optional:"true"`
//	    Cache    Cache  `name:"redis"`
//	    Handlers []http.Handler `group:"routes"`
//	}
//
//	func NewService(params ServiceParams) *Service {
//	    return &Service{
//	        db:       params.Database,
//	        logger:   params.Logger, // might be nil if not registered
//	        cache:    params.Cache,
//	        handlers: params.Handlers,
//	    }
//	}
//
// The In struct must be embedded anonymously:
//
//	type ServiceParams struct {
//	    godi.In  // ✓ Correct - anonymous embedding
//	    // ...
//	}
//
//	type ServiceParams struct {
//	    In godi.In  // ✗ Wrong - named field
//	    // ...
//	}
type In = dig.In

// Out embeds dig.Out to leverage dig's result object functionality.
// When a constructor returns a struct with embedded Out, each exported field
// of that struct is registered as a separate service in the container.
//
// This is a direct wrapper around dig.Out, so all dig features are supported:
//   - `name:"serviceName"` - Field should be registered as a keyed/named service
//   - `group:"groupName"` - Field should be added to a value group
//
// Example:
//
//	type ServiceResult struct {
//	    godi.Out
//
//	    UserService  *UserService
//	    AdminService *AdminService `name:"admin"`
//	    Handler      http.Handler  `group:"routes"`
//	}
//
//	func NewServices(db *sql.DB) ServiceResult {
//	    userSvc := newUserService(db)
//	    adminSvc := newAdminService(db)
//
//	    return ServiceResult{
//	        UserService:  userSvc,
//	        AdminService: adminSvc,
//	        Handler:      newAPIHandler(userSvc),
//	    }
//	}
//
// Multiple handlers example with groups:
//
//	type Handlers struct {
//	    godi.Out
//
//	    UserHandler  http.Handler `group:"routes"`
//	    AdminHandler http.Handler `group:"routes"`
//	    APIHandler   http.Handler `group:"routes"`
//	}
//
// The Out struct must be embedded anonymously:
//
//	type ServiceResult struct {
//	    godi.Out  // ✓ Correct - anonymous embedding
//	    // ...
//	}
//
//	type ServiceResult struct {
//	    Out godi.Out  // ✗ Wrong - named field
//	    // ...
//	}
//
// Result objects are automatically handled by the regular Add* methods:
//
//	collection.AddSingleton(NewServices) // Each field in ServiceResult is registered
type Out = dig.Out

// The following provide options are exported for advanced scenarios

// Name is an option for providing named values.
// This can be used with collection.AddSingletonInstance and similar methods.
//
// Example:
//
//	collection.AddSingletonInstance(redisCache, godi.Name("redis"))
//	collection.AddSingletonInstance(memCache, godi.Name("memory"))
var Name = dig.Name

// Group is an option for providing values to a group.
// Groups allow multiple values of the same type to be collected into a slice.
//
// Note: Transient services cannot be registered in groups. Only Singleton and
// Scoped services are supported in groups. This is because transient services
// use a factory pattern that is incompatible with dig's group mechanism.
//
// Example:
//
//	collection.AddSingleton(NewUserHandler, godi.Group("routes"))
//	collection.AddSingleton(NewAdminHandler, godi.Group("routes"))
//	collection.AddScoped(NewAPIHandler, godi.Group("routes"))
//
//	// Then consume all handlers:
//	type ServerParams struct {
//	    godi.In
//	    Handlers []http.Handler `group:"routes"`
//	}
var Group = dig.Group

// As is an option that specifies that the value produced by the constructor
// implements one or more other interfaces and should be provided as those interfaces.
//
// Example:
//
//	collection.AddSingleton(NewPostgresDB, godi.As(new(Reader), new(Writer)))
//
// This makes the PostgresDB available as both Reader and Writer interfaces.
var As = dig.As

// FillProvideInfo is an option that writes information about
// what dig was able to get out of the provided constructor.
//
// Example:
//
//	var info godi.ProvideInfo
//	collection.AddSingleton(NewService, godi.FillProvideInfo(&info))
var FillProvideInfo = dig.FillProvideInfo

// FillDecorateInfo is an option that writes information about
// what dig was able to get out of the provided decorator.
var FillDecorateInfo = dig.FillDecorateInfo

// FillInvokeInfo is an option that writes information about
// what dig was able to get out of the invoked function.
//
// Example:
//
//	var info godi.InvokeInfo
//	provider.Invoke(myFunc, godi.FillInvokeInfo(&info))
var FillInvokeInfo = dig.FillInvokeInfo

// WithProviderCallback returns a ProvideOption for adding a callback
// that runs after the constructor finishes.
//
// Example:
//
//	collection.AddSingleton(NewService, godi.WithProviderCallback(func(ci godi.CallbackInfo) {
//	    log.Printf("Service created in %v", ci.Runtime)
//	}))
var WithProviderCallback = dig.WithProviderCallback

// WithProviderBeforeCallback returns a ProvideOption for adding a callback
// that runs before the constructor starts.
var WithProviderBeforeCallback = dig.WithProviderBeforeCallback

// WithDecoratorCallback returns a DecorateOption for adding a callback
// that runs after the decorator finishes.
var WithDecoratorCallback = dig.WithDecoratorCallback

// WithDecoratorBeforeCallback returns a DecorateOption for adding a callback
// that runs before the decorator starts.
var WithDecoratorBeforeCallback = dig.WithDecoratorBeforeCallback

// The following types are exported for use with the above functions

// ProvideInfo contains information about a provided constructor.
type ProvideInfo = dig.ProvideInfo

// DecorateInfo contains information about a decorated type.
type DecorateInfo = dig.DecorateInfo

// InvokeInfo contains information about an invoked function.
type InvokeInfo = dig.InvokeInfo

// Input contains information on an input parameter of a function.
type Input = dig.Input

// Output contains information on an output produced by a function.
type Output = dig.Output

// CallbackInfo contains information about a constructor call.
type CallbackInfo = dig.CallbackInfo

// BeforeCallbackInfo contains information about a constructor call before it runs.
type BeforeCallbackInfo = dig.BeforeCallbackInfo

// Callback is a function called after a constructor or decorator runs.
type Callback = dig.Callback

// BeforeCallback is a function called before a constructor or decorator runs.
type BeforeCallback = dig.BeforeCallback

// ProvideOption modifies the default behavior of Provide.
type ProvideOption = dig.ProvideOption

// DecorateOption modifies the default behavior of Decorate.
type DecorateOption = dig.DecorateOption

// InvokeOption modifies the default behavior of Invoke.
type InvokeOption = dig.InvokeOption

// ScopeOption modifies the default behavior of Scope creation.
type ScopeOption = dig.ScopeOption

// IsIn checks whether the given struct is a dig.In struct.
// This is re-exported from dig for convenience.
var IsIn = dig.IsIn

// IsOut checks whether the given struct is a dig.Out struct.
// This is re-exported from dig for convenience.
var IsOut = dig.IsOut
