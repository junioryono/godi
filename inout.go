package godi

import (
	"github.com/junioryono/godi/v3/internal/reflection"
)

// In embeds godi.In to leverage godi's parameter object functionality.
// When a constructor function accepts a single struct parameter with embedded In,
// godi will automatically populate all exported fields of that struct
// with the corresponding services.
//
// This is a direct wrapper around godi.In, so all godi features are supported:
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
type In = reflection.In

// Out embeds godi.Out to leverage godi's result object functionality.
// When a constructor returns a struct with embedded Out, each exported field
// of that struct is registered as a separate service in the container.
//
// This is a direct wrapper around godi.Out, so all godi features are supported:
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
type Out = reflection.Out
