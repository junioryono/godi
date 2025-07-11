# Service Groups

Service groups allow you to collect multiple services of the same type into a slice. This is perfect for plugin architectures, middleware chains, or any scenario where you need to work with collections of services.

## Basic Concept

Groups collect services registered with the same group name:

```go
// Register multiple handlers in a group
services.AddSingleton(NewUserHandler, godi.Group("handlers"))
services.AddSingleton(NewProductHandler, godi.Group("handlers"))
services.AddSingleton(NewOrderHandler, godi.Group("handlers"))

// Consume all handlers as a slice
type Application struct {
    godi.In
    Handlers []Handler `group:"handlers"`
}
```

## HTTP Handler Example

Building a modular HTTP application:

```go
// Handler interface
type Handler interface {
    Pattern() string
    ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// User handler
type UserHandler struct {
    userService UserService
}

func NewUserHandler(userService UserService) Handler {
    return &UserHandler{userService: userService}
}

func (h *UserHandler) Pattern() string {
    return "/users"
}

func (h *UserHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // Handle user requests
}

// Product handler
type ProductHandler struct {
    productService ProductService
}

func NewProductHandler(productService ProductService) Handler {
    return &ProductHandler{productService: productService}
}

func (h *ProductHandler) Pattern() string {
    return "/products"
}

func (h *ProductHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    // Handle product requests
}

// Register handlers
services.AddScoped(NewUserService)
services.AddScoped(NewProductService)
services.AddSingleton(NewUserHandler, godi.Group("routes"))
services.AddSingleton(NewProductHandler, godi.Group("routes"))

// Router that consumes all handlers
type Router struct {
    godi.In
    Routes []Handler `group:"routes"`
}

func NewHTTPServer(params Router) *http.ServeMux {
    mux := http.NewServeMux()

    for _, route := range params.Routes {
        mux.Handle(route.Pattern(), route)
    }

    return mux
}
```

## Middleware Chain

Creating a middleware pipeline:

```go
// Middleware interface
type Middleware interface {
    Wrap(next http.Handler) http.Handler
    Priority() int // Lower numbers run first
}

// Logging middleware
type LoggingMiddleware struct {
    logger Logger
}

func NewLoggingMiddleware(logger Logger) Middleware {
    return &LoggingMiddleware{logger: logger}
}

func (m *LoggingMiddleware) Wrap(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        start := time.Now()
        next.ServeHTTP(w, r)
        m.logger.Info("Request processed",
            "method", r.Method,
            "path", r.URL.Path,
            "duration", time.Since(start),
        )
    })
}

func (m *LoggingMiddleware) Priority() int { return 10 }

// Auth middleware
type AuthMiddleware struct {
    authService AuthService
}

func NewAuthMiddleware(authService AuthService) Middleware {
    return &AuthMiddleware{authService: authService}
}

func (m *AuthMiddleware) Wrap(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        token := r.Header.Get("Authorization")
        if !m.authService.ValidateToken(token) {
            http.Error(w, "Unauthorized", http.StatusUnauthorized)
            return
        }
        next.ServeHTTP(w, r)
    })
}

func (m *AuthMiddleware) Priority() int { return 20 }

// Register middlewares
services.AddSingleton(NewLoggingMiddleware, godi.Group("middleware"))
services.AddSingleton(NewAuthMiddleware, godi.Group("middleware"))
services.AddSingleton(NewRateLimitMiddleware, godi.Group("middleware"))
services.AddSingleton(NewCORSMiddleware, godi.Group("middleware"))

// Middleware chain builder
type MiddlewareChain struct {
    godi.In
    Middlewares []Middleware `group:"middleware"`
}

func BuildMiddlewareChain(params MiddlewareChain, handler http.Handler) http.Handler {
    // Sort by priority
    sort.Slice(params.Middlewares, func(i, j int) bool {
        return params.Middlewares[i].Priority() < params.Middlewares[j].Priority()
    })

    // Apply in reverse order (innermost first)
    for i := len(params.Middlewares) - 1; i >= 0; i-- {
        handler = params.Middlewares[i].Wrap(handler)
    }

    return handler
}
```

## Event Handlers

Event-driven architecture with groups:

```go
// Event handler interface
type EventHandler interface {
    EventType() string
    Handle(ctx context.Context, event Event) error
}

// User created handler
type UserCreatedHandler struct {
    emailService EmailService
    logger       Logger
}

func NewUserCreatedHandler(emailService EmailService, logger Logger) EventHandler {
    return &UserCreatedHandler{
        emailService: emailService,
        logger:       logger,
    }
}

func (h *UserCreatedHandler) EventType() string {
    return "user.created"
}

func (h *UserCreatedHandler) Handle(ctx context.Context, event Event) error {
    userEvent := event.(*UserCreatedEvent)

    // Send welcome email
    err := h.emailService.SendWelcomeEmail(userEvent.Email)
    if err != nil {
        h.logger.Error("Failed to send welcome email", err)
    }

    return nil
}

// Order placed handler
type OrderPlacedHandler struct {
    inventoryService InventoryService
    notifyService    NotificationService
}

func NewOrderPlacedHandler(
    inventoryService InventoryService,
    notifyService NotificationService,
) EventHandler {
    return &OrderPlacedHandler{
        inventoryService: inventoryService,
        notifyService:    notifyService,
    }
}

func (h *OrderPlacedHandler) EventType() string {
    return "order.placed"
}

func (h *OrderPlacedHandler) Handle(ctx context.Context, event Event) error {
    orderEvent := event.(*OrderPlacedEvent)

    // Update inventory
    if err := h.inventoryService.Reserve(orderEvent.Items); err != nil {
        return err
    }

    // Send notification
    return h.notifyService.NotifyOrderPlaced(orderEvent.OrderID)
}

// Register handlers
services.AddSingleton(NewUserCreatedHandler, godi.Group("event-handlers"))
services.AddSingleton(NewOrderPlacedHandler, godi.Group("event-handlers"))
services.AddSingleton(NewPaymentProcessedHandler, godi.Group("event-handlers"))

// Event dispatcher
type EventDispatcher struct {
    godi.In
    Handlers []EventHandler `group:"event-handlers"`
}

func NewEventBus(params EventDispatcher) *EventBus {
    bus := &EventBus{
        handlers: make(map[string][]EventHandler),
    }

    // Group handlers by event type
    for _, handler := range params.Handlers {
        eventType := handler.EventType()
        bus.handlers[eventType] = append(bus.handlers[eventType], handler)
    }

    return bus
}

type EventBus struct {
    handlers map[string][]EventHandler
}

func (bus *EventBus) Publish(ctx context.Context, event Event) error {
    handlers, ok := bus.handlers[event.Type()]
    if !ok {
        return nil // No handlers for this event
    }

    // Execute all handlers
    var errs []error
    for _, handler := range handlers {
        if err := handler.Handle(ctx, event); err != nil {
            errs = append(errs, err)
        }
    }

    if len(errs) > 0 {
        return fmt.Errorf("event handling errors: %v", errs)
    }

    return nil
}
```

## Validation Rules

Composable validation system:

```go
// Validation rule interface
type ValidationRule interface {
    Name() string
    Validate(value interface{}) error
}

// Required rule
type RequiredRule struct{}

func NewRequiredRule() ValidationRule {
    return &RequiredRule{}
}

func (r *RequiredRule) Name() string { return "required" }

func (r *RequiredRule) Validate(value interface{}) error {
    if value == nil || value == "" {
        return errors.New("field is required")
    }
    return nil
}

// Email rule
type EmailRule struct{}

func NewEmailRule() ValidationRule {
    return &EmailRule{}
}

func (r *EmailRule) Name() string { return "email" }

func (r *EmailRule) Validate(value interface{}) error {
    email, ok := value.(string)
    if !ok {
        return errors.New("value must be string")
    }

    if !strings.Contains(email, "@") {
        return errors.New("invalid email format")
    }

    return nil
}

// Register rules
services.AddSingleton(NewRequiredRule, godi.Group("validators"))
services.AddSingleton(NewEmailRule, godi.Group("validators"))
services.AddSingleton(NewMinLengthRule, godi.Group("validators"))
services.AddSingleton(NewMaxLengthRule, godi.Group("validators"))

// Validator service
type ValidatorParams struct {
    godi.In
    Rules []ValidationRule `group:"validators"`
}

type Validator struct {
    rules map[string]ValidationRule
}

func NewValidator(params ValidatorParams) *Validator {
    rules := make(map[string]ValidationRule)

    for _, rule := range params.Rules {
        rules[rule.Name()] = rule
    }

    return &Validator{rules: rules}
}

func (v *Validator) ValidateStruct(s interface{}) error {
    // Use reflection to validate struct fields
    // Apply rules based on struct tags
    // Example: `validate:"required,email"`
    return nil
}
```

## Plugin System

Building a plugin architecture:

```go
// Plugin interface
type Plugin interface {
    Name() string
    Version() string
    Initialize(app *Application) error
    Start(ctx context.Context) error
    Stop(ctx context.Context) error
}

// Analytics plugin
type AnalyticsPlugin struct {
    config AnalyticsConfig
    client AnalyticsClient
}

func NewAnalyticsPlugin(config AnalyticsConfig) Plugin {
    return &AnalyticsPlugin{
        config: config,
        client: NewAnalyticsClient(config),
    }
}

func (p *AnalyticsPlugin) Name() string    { return "analytics" }
func (p *AnalyticsPlugin) Version() string { return "1.0.0" }

func (p *AnalyticsPlugin) Initialize(app *Application) error {
    // Register routes, middleware, etc.
    app.RegisterMiddleware(p.trackingMiddleware())
    return nil
}

// Search plugin
type SearchPlugin struct {
    searchEngine SearchEngine
    indexer      Indexer
}

func NewSearchPlugin(config SearchConfig) Plugin {
    return &SearchPlugin{
        searchEngine: NewElasticSearch(config),
        indexer:      NewIndexer(config),
    }
}

func (p *SearchPlugin) Name() string    { return "search" }
func (p *SearchPlugin) Version() string { return "2.1.0" }

// Register plugins
services.AddSingleton(NewAnalyticsPlugin, godi.Group("plugins"))
services.AddSingleton(NewSearchPlugin, godi.Group("plugins"))
services.AddSingleton(NewCachePlugin, godi.Group("plugins"))

// Plugin manager
type PluginManager struct {
    godi.In
    Plugins []Plugin `group:"plugins"`
}

func NewApplication(params PluginManager) (*Application, error) {
    app := &Application{
        plugins: make(map[string]Plugin),
    }

    // Initialize all plugins
    for _, plugin := range params.Plugins {
        log.Printf("Loading plugin: %s v%s", plugin.Name(), plugin.Version())

        if err := plugin.Initialize(app); err != nil {
            return nil, fmt.Errorf("failed to initialize plugin %s: %w",
                plugin.Name(), err)
        }

        app.plugins[plugin.Name()] = plugin
    }

    return app, nil
}

type Application struct {
    plugins map[string]Plugin
}

func (app *Application) Start(ctx context.Context) error {
    // Start all plugins
    for _, plugin := range app.plugins {
        if err := plugin.Start(ctx); err != nil {
            return fmt.Errorf("failed to start plugin %s: %w",
                plugin.Name(), err)
        }
    }

    return nil
}
```

## Observers and Listeners

Observer pattern with groups:

```go
// Observer interface
type Observer interface {
    OnEvent(event interface{})
}

// Metrics observer
type MetricsObserver struct {
    metrics MetricsCollector
}

func NewMetricsObserver(metrics MetricsCollector) Observer {
    return &MetricsObserver{metrics: metrics}
}

func (o *MetricsObserver) OnEvent(event interface{}) {
    switch e := event.(type) {
    case RequestEvent:
        o.metrics.IncrementCounter("requests", e.Method, e.Path)
    case ErrorEvent:
        o.metrics.IncrementCounter("errors", e.Type)
    }
}

// Logging observer
type LoggingObserver struct {
    logger Logger
}

func NewLoggingObserver(logger Logger) Observer {
    return &LoggingObserver{logger: logger}
}

func (o *LoggingObserver) OnEvent(event interface{}) {
    o.logger.Info("Event occurred", "event", event)
}

// Register observers
services.AddSingleton(NewMetricsObserver, godi.Group("observers"))
services.AddSingleton(NewLoggingObserver, godi.Group("observers"))
services.AddSingleton(NewAuditObserver, godi.Group("observers"))

// Observable service
type ObservableService struct {
    godi.In
    Observers []Observer `group:"observers"`
}

type EventEmitter struct {
    observers []Observer
}

func NewEventEmitter(params ObservableService) *EventEmitter {
    return &EventEmitter{
        observers: params.Observers,
    }
}

func (e *EventEmitter) Emit(event interface{}) {
    for _, observer := range e.observers {
        // Run observers asynchronously
        go observer.OnEvent(event)
    }
}
```

## Conditional Registration

Register in groups conditionally:

```go
func ConfigureFeatures(services godi.ServiceCollection, features FeatureFlags) {
    // Always register core features
    services.AddSingleton(NewCoreFeature, godi.Group("features"))

    // Conditionally register features
    if features.IsEnabled("advanced-search") {
        services.AddSingleton(NewAdvancedSearchFeature, godi.Group("features"))
    }

    if features.IsEnabled("real-time-sync") {
        services.AddSingleton(NewRealTimeSyncFeature, godi.Group("features"))
    }

    if features.IsEnabled("ai-recommendations") {
        services.AddSingleton(NewAIRecommendationsFeature, godi.Group("features"))
    }
}

// Feature manager consumes whatever is registered
type FeatureManager struct {
    godi.In
    Features []Feature `group:"features"`
}

func (fm *FeatureManager) ListEnabledFeatures() []string {
    names := make([]string, len(fm.Features))
    for i, f := range fm.Features {
        names[i] = f.Name()
    }
    return names
}
```

## Testing with Groups

Groups make testing modular components easy:

```go
func TestMiddlewareOrder(t *testing.T) {
    services := godi.NewServiceCollection()

    // Register test middlewares with specific priorities
    services.AddSingleton(func() Middleware {
        return &TestMiddleware{name: "first", priority: 1}
    }, godi.Group("middleware"))

    services.AddSingleton(func() Middleware {
        return &TestMiddleware{name: "second", priority: 2}
    }, godi.Group("middleware"))

    services.AddSingleton(func() Middleware {
        return &TestMiddleware{name: "third", priority: 3}
    }, godi.Group("middleware"))

    provider, _ := services.BuildServiceProvider()
    defer provider.Close()

    // Verify middleware order
    var params MiddlewareChain
    provider.Invoke(func(p MiddlewareChain) {
        params = p
    })

    assert.Len(t, params.Middlewares, 3)

    // Test that they execute in correct order
    handler := BuildMiddlewareChain(params, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("handled"))
    }))

    // Make request and verify middleware execution order
    // ...
}
```

## Best Practices

### 1. Use Meaningful Group Names

```go
// Good
godi.Group("http-routes")
godi.Group("event-handlers")
godi.Group("validation-rules")

// Avoid
godi.Group("stuff")
godi.Group("things")
```

### 2. Document Group Members

```go
// HealthChecker performs health checks.
// Register implementations with group:"health-checks"
type HealthChecker interface {
    Name() string
    Check(ctx context.Context) error
}
```

### 3. Handle Empty Groups

```go
type ServiceParams struct {
    godi.In
    Handlers []Handler `group:"handlers"`
}

func NewService(params ServiceParams) *Service {
    if len(params.Handlers) == 0 {
        log.Warn("No handlers registered")
    }

    return &Service{handlers: params.Handlers}
}
```

### 4. Order Matters Sometimes

```go
// When order is important, use a priority or order field
type OrderedHandler interface {
    Handler
    Order() int
}

// Sort before use
sort.Slice(handlers, func(i, j int) bool {
    return handlers[i].Order() < handlers[j].Order()
})
```

## Summary

Service groups enable:

- Plugin architectures
- Middleware chains
- Event handling systems
- Modular applications
- Feature toggles
- Extensible systems

They provide a clean way to work with collections of services while maintaining type safety and dependency injection benefits.
