# Service Groups

Collect multiple services under a group name for batch resolution.

## The Problem

You need to work with multiple services of the same type - validators, middleware, handlers:

```go
// Manually collecting
var validators []Validator
validators = append(validators, emailValidator)
validators = append(validators, phoneValidator)
validators = append(validators, addressValidator)
```

## The Solution: Groups

```go
// Register with group tag
services.AddSingleton(NewEmailValidator, godi.Group("validators"))
services.AddSingleton(NewPhoneValidator, godi.Group("validators"))
services.AddSingleton(NewAddressValidator, godi.Group("validators"))

// Resolve all at once
validators := godi.MustResolveGroup[Validator](provider, "validators")
// validators is []Validator with all three
```

## Registration

Use `godi.Group()` to add services to a group:

```go
// Multiple validators
services.AddSingleton(NewEmailValidator, godi.Group("validators"))
services.AddSingleton(NewPhoneValidator, godi.Group("validators"))
services.AddSingleton(NewAddressValidator, godi.Group("validators"))

// Multiple middleware
services.AddSingleton(NewLoggingMiddleware, godi.Group("middleware"))
services.AddSingleton(NewAuthMiddleware, godi.Group("middleware"))
services.AddSingleton(NewRateLimitMiddleware, godi.Group("middleware"))
```

## Resolution

```go
// Get all services in group
validators := godi.MustResolveGroup[Validator](provider, "validators")

for _, v := range validators {
    if err := v.Validate(input); err != nil {
        return err
    }
}
```

## Use Cases

### Validation Chain

```go
type Validator interface {
    Validate(data any) error
}

// Register validators
services.AddSingleton(NewRequiredFieldValidator, godi.Group("validators"))
services.AddSingleton(NewEmailFormatValidator, godi.Group("validators"))
services.AddSingleton(NewPhoneFormatValidator, godi.Group("validators"))
services.AddSingleton(NewAddressValidator, godi.Group("validators"))

// Use in service: a group is injected through a `group` tag on a
// godi.In field (a plain []Validator parameter is not a group)
type UserService struct {
    validators []Validator
}

type UserServiceParams struct {
    godi.In

    Validators []Validator `group:"validators"`
}

func NewUserService(params UserServiceParams) *UserService {
    return &UserService{validators: params.Validators}
}

func (s *UserService) Create(user *User) error {
    // Run all validators
    for _, v := range s.validators {
        if err := v.Validate(user); err != nil {
            return err
        }
    }
    // Create user...
    return nil
}
```

### Middleware Stack

```go
type Middleware func(http.Handler) http.Handler

services.AddSingleton(NewLoggingMiddleware, godi.Group("middleware"))
services.AddSingleton(NewRecoveryMiddleware, godi.Group("middleware"))
services.AddSingleton(NewCORSMiddleware, godi.Group("middleware"))
services.AddSingleton(NewAuthMiddleware, godi.Group("middleware"))

// Apply all middleware
func BuildHandler(provider godi.Provider, handler http.Handler) http.Handler {
    middlewares := godi.MustResolveGroup[Middleware](provider, "middleware")

    for i := len(middlewares) - 1; i >= 0; i-- {
        handler = middlewares[i](handler)
    }

    return handler
}
```

### Event Handlers

```go
type EventHandler interface {
    Handle(event Event) error
}

services.AddSingleton(NewLoggingHandler, godi.Group("handlers"))
services.AddSingleton(NewMetricsHandler, godi.Group("handlers"))
services.AddSingleton(NewNotificationHandler, godi.Group("handlers"))
services.AddSingleton(NewAuditHandler, godi.Group("handlers"))

// Broadcast events
type EventBus struct {
    handlers []EventHandler
}

func (b *EventBus) Publish(event Event) {
    for _, h := range b.handlers {
        go h.Handle(event)
    }
}
```

### Plugin System

```go
type Plugin interface {
    Name() string
    Init() error
    Shutdown() error
}

services.AddSingleton(NewAuthPlugin, godi.Group("plugins"))
services.AddSingleton(NewCachePlugin, godi.Group("plugins"))
services.AddSingleton(NewMetricsPlugin, godi.Group("plugins"))

func InitializeApp(provider godi.Provider) error {
    plugins := godi.MustResolveGroup[Plugin](provider, "plugins")

    for _, p := range plugins {
        log.Printf("Initializing plugin: %s", p.Name())
        if err := p.Init(); err != nil {
            return err
        }
    }

    return nil
}
```

## With Parameter Objects

Use the `group` tag to inject groups:

```go
type ApplicationParams struct {
    godi.In

    Logger      Logger
    Config      *Config
    Validators  []Validator    `group:"validators"`
    Middlewares []Middleware   `group:"middleware"`
    Handlers    []EventHandler `group:"handlers"`
}

func NewApplication(params ApplicationParams) *Application {
    return &Application{
        logger:      params.Logger,
        config:      params.Config,
        validators:  params.Validators,
        middlewares: params.Middlewares,
        handlers:    params.Handlers,
    }
}
```

### Optional Groups

```go
type ServiceParams struct {
    godi.In

    // Required dependencies
    Logger Logger

    // Optional group - empty slice if no services registered
    Plugins []Plugin `group:"plugins" optional:"true"`
}

func NewService(params ServiceParams) *Service {
    svc := &Service{logger: params.Logger}

    // Check if plugins available
    if len(params.Plugins) > 0 {
        for _, p := range params.Plugins {
            svc.RegisterPlugin(p)
        }
    }

    return svc
}
```

## Combining Keys and Groups

A single registration cannot use both `godi.Name` and `godi.Group` (`Build`
reports a registration error). To make one instance available by key and as a
group member, return it from a [result object](result-objects.md) with two
fields, one tagged `name` and one tagged `group`:

```go
type EmailValidatorResult struct {
    godi.Out

    Named   Validator `name:"email"`
    InGroup Validator `group:"validators"`
}

func NewEmailValidator() EmailValidatorResult {
    v := &EmailValidator{}
    return EmailValidatorResult{Named: v, InGroup: v} // same instance
}

services.AddSingleton(NewEmailValidator)

// Resolve by name
emailValidator := godi.MustResolveKeyed[Validator](provider, "email")

// Or get all validators (includes the same EmailValidator instance)
allValidators := godi.MustResolveGroup[Validator](provider, "validators")
```

## Ordering

Group members are resolved in registration order:

```go
services.AddSingleton(NewFirst, godi.Group("ordered"))   // Index 0
services.AddSingleton(NewSecond, godi.Group("ordered"))  // Index 1
services.AddSingleton(NewThird, godi.Group("ordered"))   // Index 2

items := godi.MustResolveGroup[Item](provider, "ordered")
// items[0] = First, items[1] = Second, items[2] = Third
```

## Empty Groups

If no services are registered to a group, resolution returns an empty slice:

```go
// No services registered to "missing" group
items := godi.MustResolveGroup[Item](provider, "missing")
// items is []Item{} (empty, not nil)
```

---

**See also:** [Keyed Services](keyed-services.md) | [Parameter Objects](parameter-objects.md)
