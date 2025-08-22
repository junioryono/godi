# Service Groups

Collect multiple services under a group name for batch operations and plugin systems.

## Understanding Service Groups

Service groups allow you to register multiple services under the same group name and resolve them all at once:

```go
// Register middleware in a group
services.AddScoped(NewAuthMiddleware, godi.Group("middleware"))
services.AddScoped(NewLoggingMiddleware, godi.Group("middleware"))
services.AddScoped(NewCORSMiddleware, godi.Group("middleware"))

// Resolve all middleware at once
middlewares := godi.MustResolveGroup[Middleware](provider, "middleware")
for _, mw := range middlewares {
    app.Use(mw)
}
```

## Registration with Groups

### Basic Group Registration

```go
// Register multiple handlers
services.AddSingleton(NewUserHandler, godi.Group("handlers"))
services.AddSingleton(NewOrderHandler, godi.Group("handlers"))
services.AddSingleton(NewProductHandler, godi.Group("handlers"))

// Register multiple validators
services.AddTransient(NewEmailValidator, godi.Group("validators"))
services.AddTransient(NewPhoneValidator, godi.Group("validators"))
services.AddTransient(NewAddressValidator, godi.Group("validators"))
```

### With Interface Registration

```go
// Register concrete types as interfaces in a group
services.AddSingleton(NewCSVExporter, godi.Group("exporters"), godi.As[Exporter]())
services.AddSingleton(NewJSONExporter, godi.Group("exporters"), godi.As[Exporter]())
services.AddSingleton(NewXMLExporter, godi.Group("exporters"), godi.As[Exporter]())

// Resolve all exporters
exporters := godi.MustResolveGroup[Exporter](provider, "exporters")
```

## Common Use Cases

### Middleware Pipeline

```go
type Middleware interface {
    Process(next http.Handler) http.Handler
}

// Register middleware
services.AddScoped(NewAuthMiddleware, godi.Group("middleware"))
services.AddScoped(NewRateLimitMiddleware, godi.Group("middleware"))
services.AddScoped(NewLoggingMiddleware, godi.Group("middleware"))
services.AddScoped(NewMetricsMiddleware, godi.Group("middleware"))

// Build middleware pipeline
func BuildPipeline(provider godi.Provider) http.Handler {
    middlewares := godi.MustResolveGroup[Middleware](provider, "middleware")

    handler := http.HandlerFunc(finalHandler)

    // Apply middleware in reverse order
    for i := len(middlewares) - 1; i >= 0; i-- {
        handler = middlewares[i].Process(handler)
    }

    return handler
}
```

### Plugin System

```go
type Plugin interface {
    Name() string
    Initialize() error
    Execute() error
}

// Register plugins
services.AddSingleton(NewDatabasePlugin, godi.Group("plugins"), godi.As[Plugin]())
services.AddSingleton(NewCachePlugin, godi.Group("plugins"), godi.As[Plugin]())
services.AddSingleton(NewMonitoringPlugin, godi.Group("plugins"), godi.As[Plugin]())

// Plugin manager
type PluginManager struct {
    plugins []Plugin
}

func NewPluginManager(provider godi.Provider) *PluginManager {
    return &PluginManager{
        plugins: godi.MustResolveGroup[Plugin](provider, "plugins"),
    }
}

func (m *PluginManager) InitializeAll() error {
    for _, plugin := range m.plugins {
        if err := plugin.Initialize(); err != nil {
            return fmt.Errorf("failed to initialize %s: %w",
                plugin.Name(), err)
        }
    }
    return nil
}
```

### Event Handlers

```go
type EventHandler interface {
    CanHandle(event Event) bool
    Handle(event Event) error
}

// Register event handlers
services.AddSingleton(NewUserCreatedHandler, godi.Group("event-handlers"), godi.As[EventHandler]())
services.AddSingleton(NewOrderPlacedHandler, godi.Group("event-handlers"), godi.As[EventHandler]())
services.AddSingleton(NewPaymentProcessedHandler, godi.Group("event-handlers"), godi.As[EventHandler]())

// Event dispatcher
type EventDispatcher struct {
    handlers []EventHandler
}

func NewEventDispatcher(provider godi.Provider) *EventDispatcher {
    return &EventDispatcher{
        handlers: godi.MustResolveGroup[EventHandler](provider, "event-handlers"),
    }
}

func (d *EventDispatcher) Dispatch(event Event) error {
    for _, handler := range d.handlers {
        if handler.CanHandle(event) {
            if err := handler.Handle(event); err != nil {
                return err
            }
        }
    }
    return nil
}
```

### Validation Pipeline

```go
type Validator interface {
    Validate(data any) []ValidationError
}

// Register validators
services.AddTransient(NewRequiredFieldsValidator, godi.Group("validators"), godi.As[Validator]())
services.AddTransient(NewEmailFormatValidator, godi.Group("validators"), godi.As[Validator]())
services.AddTransient(NewBusinessRulesValidator, godi.Group("validators"), godi.As[Validator]())

// Validation service
func ValidateData(provider godi.Provider, data any) []ValidationError {
    validators := godi.MustResolveGroup[Validator](provider, "validators")

    var errors []ValidationError
    for _, validator := range validators {
        errors = append(errors, validator.Validate(data)...)
    }

    return errors
}
```

## Advanced Patterns

### Ordered Groups

```go
type OrderedService interface {
    Order() int
    Execute() error
}

// Services with order
type FirstService struct{}
func (s *FirstService) Order() int { return 1 }
func (s *FirstService) Execute() error { return nil }

type SecondService struct{}
func (s *SecondService) Order() int { return 2 }
func (s *SecondService) Execute() error { return nil }

// Register in group
services.AddSingleton(NewFirstService, godi.Group("ordered"), godi.As[OrderedService]())
services.AddSingleton(NewSecondService, godi.Group("ordered"), godi.As[OrderedService]())

// Execute in order
func ExecuteOrdered(provider godi.Provider) error {
    services := godi.MustResolveGroup[OrderedService](provider, "ordered")

    // Sort by order
    sort.Slice(services, func(i, j int) bool {
        return services[i].Order() < services[j].Order()
    })

    // Execute in order
    for _, svc := range services {
        if err := svc.Execute(); err != nil {
            return err
        }
    }

    return nil
}
```

### Conditional Group Members

```go
func RegisterExporters(services godi.Collection, config *Config) {
    // Always register CSV
    services.AddSingleton(NewCSVExporter, godi.Group("exporters"), godi.As[Exporter]())

    // Conditionally register others
    if config.EnableJSON {
        services.AddSingleton(NewJSONExporter, godi.Group("exporters"), godi.As[Exporter]())
    }

    if config.EnableXML {
        services.AddSingleton(NewXMLExporter, godi.Group("exporters"), godi.As[Exporter]())
    }

    if config.EnableExcel {
        services.AddSingleton(NewExcelExporter, godi.Group("exporters"), godi.As[Exporter]())
    }
}
```

### Dynamic Group Resolution

```go
type ServiceRegistry struct {
    provider godi.Provider
    groups   map[string]string
}

func (r *ServiceRegistry) GetServices(category string) []Service {
    groupName, ok := r.groups[category]
    if !ok {
        return nil
    }

    services, err := godi.ResolveGroup[Service](r.provider, groupName)
    if err != nil {
        return nil
    }

    return services
}
```

## With Parameter Objects

### Group Dependencies in Parameter Objects

```go
type ApplicationParams struct {
    godi.In

    Middlewares []Middleware   `group:"middleware"`
    Validators  []Validator    `group:"validators"`
    Handlers    []EventHandler `group:"event-handlers"`
}

func NewApplication(params ApplicationParams) *Application {
    return &Application{
        middlewares: params.Middlewares,
        validators:  params.Validators,
        handlers:    params.Handlers,
    }
}

// godi automatically injects all group members
services.AddSingleton(NewApplication)
```

## Testing with Groups

### Test Specific Group Members

```go
func TestEventHandlers(t *testing.T) {
    services := godi.NewCollection()

    // Register test handlers
    services.AddSingleton(NewMockUserHandler, godi.Group("handlers"), godi.As[EventHandler]())
    services.AddSingleton(NewMockOrderHandler, godi.Group("handlers"), godi.As[EventHandler]())

    provider, err := services.Build()
    assert.NoError(t, err)

    handlers := godi.MustResolveGroup[EventHandler](provider, "handlers")
    assert.Len(t, handlers, 2)

    // Test each handler
    for _, handler := range handlers {
        err := handler.Handle(testEvent)
        assert.NoError(t, err)
    }
}
```

### Mock Entire Groups

```go
func SetupTestProvider() godi.Provider {
    services := godi.NewCollection()

    // Register mock group
    for i := 0; i < 3; i++ {
        services.AddSingleton(
            func(id int) func() Service {
                return func() Service {
                    return &MockService{ID: id}
                }
            }(i),
            godi.Group("services"),
        )
    }

    provider, _ := services.Build()
    return provider
}
```

## Best Practices

1. **Use consistent naming** - Group names should be plural and descriptive
2. **Document group members** - List what belongs in each group
3. **Handle empty groups** - Check if group is empty before using
4. **Consider ordering** - Some groups may need ordered execution
5. **Keep groups focused** - Don't mix unrelated services

### Group Constants

```go
const (
    MiddlewareGroup    = "middleware"
    ValidatorsGroup    = "validators"
    HandlersGroup      = "handlers"
    ExportersGroup     = "exporters"
)

// Use constants
services.AddScoped(NewAuthMiddleware, godi.Group(MiddlewareGroup))
middlewares := godi.MustResolveGroup[Middleware](provider, MiddlewareGroup)
```

### Empty Group Handling

```go
func ProcessWithHandlers(provider godi.Provider, data any) error {
    handlers, err := godi.ResolveGroup[Handler](provider, "handlers")
    if err != nil {
        return err
    }

    if len(handlers) == 0 {
        return errors.New("no handlers registered")
    }

    for _, handler := range handlers {
        if err := handler.Process(data); err != nil {
            return err
        }
    }

    return nil
}
```

## Common Pitfalls

### Mixing Keys and Groups

```go
// ❌ Can't use both Name and Group
services.AddSingleton(NewService, godi.Name("service1"), godi.Group("services")) // Error!

// ✅ Use one or the other
services.AddSingleton(NewService1, godi.Group("services"))
services.AddSingleton(NewService2, godi.Name("special"))
```

### Type Consistency

```go
// ❌ Mixed types in group
services.AddSingleton(NewLogger, godi.Group("services"))
services.AddSingleton(NewDatabase, godi.Group("services"))
// Can't resolve as single type!

// ✅ Same interface for all group members
services.AddSingleton(NewLogger, godi.Group("services"), godi.As[Service]())
services.AddSingleton(NewDatabase, godi.Group("services"), godi.As[Service]())
```

## Next Steps

- Learn about [Parameter Objects](parameter-objects.md)
- Explore [Result Objects](result-objects.md)
- Understand [Interface Registration](interface-registration.md)
