package godi

// In is a marker type for parameter objects in dependency injection.
// When a constructor function accepts a single struct parameter with embedded In,
// the container will automatically populate all exported fields of that struct
// with the corresponding services.
//
// All features are supported:
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
type In struct{}

// isIn is a marker method for the In type
func (In) isIn() {}

// Out is a marker type for result objects in dependency injection.
// When a constructor returns a struct with embedded Out, each exported field
// of that struct is registered as a separate service in the container.
//
// All features are supported:
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
type Out struct{}

// isOut is a marker method for the Out type
func (Out) isOut() {}

// The following provide options are exported for advanced scenarios

// ProvideOption modifies the default behavior of service registration.
type ProvideOption interface {
	apply() // Internal marker method
}

// DecorateOption modifies the default behavior of decorator registration.
type DecorateOption interface {
	apply() // Internal marker method
}

// InvokeOption modifies the default behavior of Invoke.
type InvokeOption interface {
	apply() // Internal marker method
}

// Name is an option for providing named values.
// This can be used with collection.AddSingletonInstance and similar methods.
//
// Example:
//
//	collection.AddSingletonInstance(redisCache, godi.Name("redis"))
//	collection.AddSingletonInstance(memCache, godi.Name("memory"))
func Name(name string) ProvideOption {
	return &nameOption{name: name}
}

type nameOption struct {
	name string
}

func (o *nameOption) apply() {}

// Group is an option for providing values to a group.
// Groups allow multiple values of the same type to be collected into a slice.
//
// Example:
//
//	collection.AddSingleton(NewUserHandler, godi.Group("routes"))
//	collection.AddSingleton(NewAdminHandler, godi.Group("routes"))
//	collection.AddSingleton(NewAPIHandler, godi.Group("routes"))
//
//	// Then consume all handlers:
//	type ServerParams struct {
//	    godi.In
//	    Handlers []http.Handler `group:"routes"`
//	}
func Group(group string) ProvideOption {
	return &groupOption{group: group}
}

type groupOption struct {
	group string
}

func (o *groupOption) apply() {}

// As is an option that specifies that the value produced by the constructor
// implements one or more other interfaces and should be provided as those interfaces.
//
// Example:
//
//	collection.AddSingleton(NewPostgresDB, godi.As(new(Reader), new(Writer)))
//
// This makes the PostgresDB available as both Reader and Writer interfaces.
func As(interfaces ...any) ProvideOption {
	return &asOption{interfaces: interfaces}
}

type asOption struct {
	interfaces []any
}

func (o *asOption) apply() {}
