# Parameter Objects

Parameter objects simplify constructors with many dependencies using `godi.In` and `godi.Out`.

## Input Parameters (In)

Use `godi.In` when a constructor has many dependencies:

### Basic Example

```go
// Instead of this long constructor:
func NewUserService(
    db Database,
    cache Cache,
    logger Logger,
    emailService EmailService,
    smsService SMSService,
    config *Config,
) *UserService {
    // ...
}

// Use a parameter object:
type UserServiceParams struct {
    godi.In

    DB           Database
    Cache        Cache
    Logger       Logger
    EmailService EmailService
    SMSService   SMSService
    Config       *Config
}

func NewUserService(params UserServiceParams) *UserService {
    return &UserService{
        db:           params.DB,
        cache:        params.Cache,
        logger:       params.Logger,
        emailService: params.EmailService,
        smsService:   params.SMSService,
        config:       params.Config,
    }
}
```

### Optional Dependencies

Mark dependencies as optional with the `optional` tag:

```go
type ServiceParams struct {
    godi.In

    DB     Database
    Logger Logger
    Cache  Cache   `optional:"true"` // Might be nil
    Tracer Tracer  `optional:"true"` // Might be nil
}

func NewService(params ServiceParams) *Service {
    svc := &Service{
        db:     params.DB,
        logger: params.Logger,
    }

    // Check optional dependencies
    if params.Cache != nil {
        svc.cache = params.Cache
    }

    if params.Tracer != nil {
        svc.tracer = params.Tracer
    }

    return svc
}
```

### Named Dependencies

Use specific implementations with the `name` tag:

```go
type DatabaseParams struct {
    godi.In

    Primary   Database `name:"primary"`
    Secondary Database `name:"secondary"`
    Analytics Database `name:"analytics" optional:"true"`
}

func NewUserRepository(params DatabaseParams) *UserRepository {
    return &UserRepository{
        primary:   params.Primary,
        secondary: params.Secondary,
        analytics: params.Analytics, // Might be nil
    }
}
```

### Groups

Collect all services of a type with the `group` tag:

```go
type ValidatorParams struct {
    godi.In

    Validators []Validator `group:"validators"`
}

func NewValidationService(params ValidatorParams) *ValidationService {
    return &ValidationService{
        validators: params.Validators, // Gets all validators
    }
}

// Register validators
var ValidationModule = godi.NewModule("validation",
    godi.AddSingleton(NewEmailValidator, godi.Group("validators")),
    godi.AddSingleton(NewPhoneValidator, godi.Group("validators")),
    godi.AddSingleton(NewAddressValidator, godi.Group("validators")),
)
```

## Output Parameters (Out)

Use `godi.Out` to register multiple services from one constructor:

### Basic Example

```go
// Return multiple repositories from one constructor
type RepositoryBundle struct {
    godi.Out

    UserRepo    UserRepository
    OrderRepo   OrderRepository
    ProductRepo ProductRepository
}

func NewRepositories(db Database, logger Logger) RepositoryBundle {
    return RepositoryBundle{
        UserRepo:    &userRepository{db: db, logger: logger},
        OrderRepo:   &orderRepository{db: db, logger: logger},
        ProductRepo: &productRepository{db: db, logger: logger},
    }
}

// Register once, get three services!
var DataModule = godi.NewModule("data",
    godi.AddSingleton(NewDatabase),
    godi.AddSingleton(NewLogger),
    godi.AddScoped(NewRepositories), // Registers all three repos
)
```

### Named Outputs

Register services with specific names:

```go
type CacheBundle struct {
    godi.Out

    UserCache    Cache `name:"user-cache"`
    ProductCache Cache `name:"product-cache"`
    OrderCache   Cache `name:"order-cache"`
}

func NewCaches() CacheBundle {
    return CacheBundle{
        UserCache:    NewRedisCache("user:"),
        ProductCache: NewRedisCache("product:"),
        OrderCache:   NewRedisCache("order:"),
    }
}
```

### Groups in Output

Add services to groups:

```go
type HandlerBundle struct {
    godi.Out

    UserHandler    http.Handler `group:"handlers"`
    OrderHandler   http.Handler `group:"handlers"`
    ProductHandler http.Handler `group:"handlers"`
}

func NewHandlers(/* deps */) HandlerBundle {
    return HandlerBundle{
        UserHandler:    &userHandler{/* ... */},
        OrderHandler:   &orderHandler{/* ... */},
        ProductHandler: &productHandler{/* ... */},
    }
}
```

## Real-World Example

Complete example showing both In and Out:

```go
// Input parameters for configuration
type AppConfigParams struct {
    godi.In

    Env       string `name:"environment"`
    Databases struct {
        Primary   Database `name:"primary-db"`
        Secondary Database `name:"secondary-db"`
    }
    Caches []Cache `group:"caches"`
}

// Output bundle for related services
type AppServices struct {
    godi.Out

    HealthCheck  http.Handler `group:"handlers"`
    MetricsRoute http.Handler `group:"handlers"`

    UserService    UserService
    ProductService ProductService
    OrderService   OrderService  `name:"order-svc"`
}

func NewAppServices(params AppConfigParams) AppServices {
    // Use all the inputs to create outputs
    return AppServices{
        HealthCheck:    NewHealthHandler(params.Databases.Primary),
        MetricsRoute:   NewMetricsHandler(),
        UserService:    NewUserService(params.Databases.Primary),
        ProductService: NewProductService(params.Databases.Secondary),
        OrderService:   NewOrderService(params.Databases.Primary),
    }
}
```

## Best Practices

1. **Use In for 4+ dependencies** - Keeps constructors clean
2. **Use Out for related services** - Group initialization logic
3. **Document optional fields** - Make it clear what can be nil
4. **Keep parameter objects focused** - Don't create a "god object"

## When to Use

✅ **Use parameter objects when:**

- Constructor has many parameters (4+)
- You have optional dependencies
- You need named or grouped services
- You want to bundle related outputs

❌ **Don't use when:**

- Constructor has few parameters (1-3)
- Dependencies are simple
- It makes the code harder to understand

Parameter objects are a tool for managing complexity - use them when they make your code cleaner!
