# Service Groups

Service groups let you collect multiple services of the same type and inject them as a slice. Perfect for plugin systems, validators, or middleware chains.

## Basic Example

```go
// Common interface
type Validator interface {
    Validate(data any) error
}

// Multiple validators
type EmailValidator struct{}
func (v *EmailValidator) Validate(data any) error {
    email, ok := data.(string)
    if !ok || !strings.Contains(email, "@") {
        return errors.New("invalid email")
    }
    return nil
}

type PhoneValidator struct{}
func (v *PhoneValidator) Validate(data any) error {
    phone, ok := data.(string)
    if !ok || len(phone) < 10 {
        return errors.New("invalid phone")
    }
    return nil
}

// Register as group
var ValidationModule = godi.NewModule("validation",
    godi.AddSingleton(func() Validator {
        return &EmailValidator{}
    }, godi.Group("validators")),

    godi.AddSingleton(func() Validator {
        return &PhoneValidator{}
    }, godi.Group("validators")),

    godi.AddSingleton(func() Validator {
        return &AddressValidator{}
    }, godi.Group("validators")),
)

// Use all validators
type ValidationService struct {
    validators []Validator
}

func NewValidationService(params struct {
    godi.In
    Validators []Validator `group:"validators"`
}) *ValidationService {
    return &ValidationService{
        validators: params.Validators,
    }
}

func (s *ValidationService) ValidateAll(data any) error {
    for _, validator := range s.validators {
        if err := validator.Validate(data); err != nil {
            return err
        }
    }
    return nil
}
```

## Real-World Example: HTTP Middleware

```go
// Middleware interface
type Middleware interface {
    Handle(next http.Handler) http.Handler
}

// Various middleware
type LoggingMiddleware struct {
    logger Logger
}

func NewLoggingMiddleware(logger Logger) Middleware {
    return &LoggingMiddleware{logger: logger}
}

func (m *LoggingMiddleware) Handle(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        m.logger.Info("Request started", "path", r.URL.Path)

        next.ServeHTTP(w, r)

        m.logger.Info("Request completed",
            "path", r.URL.Path,
            "duration", time.Since(start))
    })
}

type AuthMiddleware struct {
    authService AuthService
}

func NewAuthMiddleware(authService AuthService) Middleware {
    return &AuthMiddleware{authService: authService}
}

func (m *AuthMiddleware) Handle(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        token := r.Header.Get("Authorization")
        if !m.authService.ValidateToken(token) {
            http.Error(w, "Unauthorized", 401)
            return
        }
        next.ServeHTTP(w, r)
    })
}

// Register middleware
var MiddlewareModule = godi.NewModule("middleware",
    godi.AddScoped(NewLoggingMiddleware, godi.Group("middleware")),
    godi.AddScoped(NewAuthMiddleware, godi.Group("middleware")),
    godi.AddScoped(NewRateLimitMiddleware, godi.Group("middleware")),
    godi.AddScoped(NewCORSMiddleware, godi.Group("middleware")),
)

// HTTP server using middleware chain
type HTTPServer struct {
    middleware []Middleware
    router     *mux.Router
}

func NewHTTPServer(
    router *mux.Router,
    params struct {
        godi.In
        Middleware []Middleware `group:"middleware"`
    },
) *HTTPServer {
    return &HTTPServer{
        middleware: params.Middleware,
        router:     router,
    }
}

func (s *HTTPServer) Start() {
    // Build middleware chain
    handler := http.Handler(s.router)

    // Apply in reverse order (so first registered runs first)
    for i := len(s.middleware) - 1; i >= 0; i-- {
        handler = s.middleware[i].Handle(handler)
    }

    log.Fatal(http.ListenAndServe(":8080", handler))
}
```

## Plugin System Example

```go
// Plugin interface
type Plugin interface {
    Name() string
    Initialize(app *Application) error
    Shutdown() error
}

// Various plugins
type MetricsPlugin struct {
    collector *MetricsCollector
}

func NewMetricsPlugin(collector *MetricsCollector) Plugin {
    return &MetricsPlugin{collector: collector}
}

func (p *MetricsPlugin) Name() string { return "metrics" }

func (p *MetricsPlugin) Initialize(app *Application) error {
    app.Router.Handle("/metrics", p.collector.Handler())
    return nil
}

// Plugin registration
var PluginModule = godi.NewModule("plugins",
    godi.AddSingleton(NewMetricsPlugin, godi.Group("plugins")),
    godi.AddSingleton(NewHealthCheckPlugin, godi.Group("plugins")),
    godi.AddSingleton(NewAdminPlugin, godi.Group("plugins")),
)

// Application with plugins
type Application struct {
    plugins []Plugin
    Router  *mux.Router
}

func NewApplication(
    router *mux.Router,
    params struct {
        godi.In
        Plugins []Plugin `group:"plugins"`
    },
) *Application {
    return &Application{
        plugins: params.Plugins,
        Router:  router,
    }
}

func (app *Application) Start() error {
    // Initialize all plugins
    for _, plugin := range app.plugins {
        log.Printf("Initializing plugin: %s", plugin.Name())
        if err := plugin.Initialize(app); err != nil {
            return fmt.Errorf("failed to initialize %s: %w", plugin.Name(), err)
        }
    }

    return nil
}

func (app *Application) Shutdown() error {
    // Shutdown in reverse order
    for i := len(app.plugins) - 1; i >= 0; i-- {
        if err := app.plugins[i].Shutdown(); err != nil {
            log.Printf("Error shutting down %s: %v",
                app.plugins[i].Name(), err)
        }
    }
    return nil
}
```

## Event System Example

```go
// Event handler interface
type EventHandler interface {
    EventType() string
    Handle(event Event) error
}

// Various event handlers
type UserCreatedHandler struct {
    emailService EmailService
}

func NewUserCreatedHandler(emailService EmailService) EventHandler {
    return &UserCreatedHandler{emailService: emailService}
}

func (h *UserCreatedHandler) EventType() string { return "user.created" }

func (h *UserCreatedHandler) Handle(event Event) error {
    user := event.Data.(*User)
    return h.emailService.SendWelcomeEmail(user.Email)
}

// Register handlers
var EventModule = godi.NewModule("events",
    godi.AddScoped(NewUserCreatedHandler, godi.Group("event-handlers")),
    godi.AddScoped(NewOrderPlacedHandler, godi.Group("event-handlers")),
    godi.AddScoped(NewPaymentProcessedHandler, godi.Group("event-handlers")),
)

// Event dispatcher
type EventDispatcher struct {
    handlers map[string][]EventHandler
}

func NewEventDispatcher(params struct {
    godi.In
    Handlers []EventHandler `group:"event-handlers"`
}) *EventDispatcher {
    dispatcher := &EventDispatcher{
        handlers: make(map[string][]EventHandler),
    }

    // Group handlers by event type
    for _, handler := range params.Handlers {
        eventType := handler.EventType()
        dispatcher.handlers[eventType] = append(
            dispatcher.handlers[eventType],
            handler,
        )
    }

    return dispatcher
}

func (d *EventDispatcher) Dispatch(event Event) error {
    handlers, ok := d.handlers[event.Type]
    if !ok {
        return nil // No handlers for this event
    }

    var errs []error
    for _, handler := range handlers {
        if err := handler.Handle(event); err != nil {
            errs = append(errs, err)
        }
    }

    if len(errs) > 0 {
        return fmt.Errorf("event handling errors: %v", errs)
    }

    return nil
}
```

## Combining Groups with Keys

You can use both groups and keys together:

```go
// Different validator groups
var ValidationModule = godi.NewModule("validation",
    // User validators
    godi.AddSingleton(NewEmailValidator,
        godi.Group("validators"),
        godi.Name("user-validators")),
    godi.AddSingleton(NewPasswordValidator,
        godi.Group("validators"),
        godi.Name("user-validators")),

    // Product validators
    godi.AddSingleton(NewPriceValidator,
        godi.Group("validators"),
        godi.Name("product-validators")),
    godi.AddSingleton(NewSKUValidator,
        godi.Group("validators"),
        godi.Name("product-validators")),
)
```

## Best Practices

### 1. Order Matters

Services are injected in registration order:

```go
// Middleware runs in this order: Auth -> RateLimit -> Logging
var MiddlewareModule = godi.NewModule("middleware",
    godi.AddScoped(NewAuthMiddleware, godi.Group("middleware")),
    godi.AddScoped(NewRateLimitMiddleware, godi.Group("middleware")),
    godi.AddScoped(NewLoggingMiddleware, godi.Group("middleware")),
)
```

### 2. Document Group Members

```go
// Package middleware provides HTTP middleware.
//
// Available middleware (in execution order):
// 1. Authentication - Validates JWT tokens
// 2. Rate Limiting - Limits requests per IP
// 3. Logging - Logs all requests
// 4. CORS - Handles cross-origin requests
var MiddlewareModule = godi.NewModule("middleware",
    // ...
)
```

### 3. Empty Groups are OK

If no services are registered for a group, an empty slice is injected:

```go
func NewPluginManager(params struct {
    godi.In
    Plugins []Plugin `group:"plugins"`
}) *PluginManager {
    // params.Plugins might be empty - that's fine
    return &PluginManager{plugins: params.Plugins}
}
```

### 4. Type Safety

Groups maintain type safety:

```go
// This won't compile if any service in the group
// doesn't implement Validator
type MyService struct {
    validators []Validator
}

func NewMyService(params struct {
    godi.In
    Validators []Validator `group:"validators"`
}) *MyService {
    return &MyService{validators: params.Validators}
}
```

## When to Use Groups

Use service groups for:

- **Plugin systems** - Extensible functionality
- **Middleware chains** - Ordered processing
- **Event handlers** - Multiple handlers per event
- **Validators** - Run all validations
- **Processors** - Pipeline processing
- **Observers** - Notification systems

## Summary

Service groups are powerful for:

- Collecting similar services
- Building extensible systems
- Creating processing pipelines
- Implementing plugin architectures

The key is having a common interface and meaningful grouping!
