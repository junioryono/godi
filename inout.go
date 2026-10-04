package godi

import (
	"github.com/junioryono/godi/v6/internal/reflection"
)

// In marks a struct as a parameter object.
// When a constructor function accepts a single struct parameter with embedded In,
// godi will automatically populate all exported fields of that struct
// with the corresponding services.
//
// Fields support these struct tags:
//   - `optional:"true"` - Field is optional and won't cause an error if the service is not found
//   - `name:"serviceName"` - Field should be resolved as a keyed/named service
//   - `group:"groupName"` - Field should be filled from a value group (slice fields only)
//   - `inject:"-"` - Field is skipped: godi neither resolves it nor treats it as a dependency
//
// A field cannot have both a name and a group tag.
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
type In = reflection.In

// Out marks a struct as a result object.
// When a constructor returns a struct with embedded Out, each exported field
// of that struct is registered as a separate service in the container.
//
// Fields support these struct tags:
//   - `name:"serviceName"` - Field should be registered as a keyed/named service
//   - `group:"groupName"` - Field should be added to a value group
//   - `inject:"-"` - Field is skipped and not registered
//
// A field cannot have both a name and a group tag. To provide the same value
// under a name and in a group, return it from two fields.
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
type Out = reflection.Out
