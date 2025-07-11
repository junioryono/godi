# Advanced Patterns

This guide covers advanced dependency injection patterns and techniques for complex scenarios.

## Factory Pattern

### Service Factory

Create services dynamically based on runtime parameters:

```go
// Factory interface
type ServiceFactory interface {
    CreateService(serviceType string) (Service, error)
}

// Factory implementation
type serviceFactory struct {
    provider godi.ServiceProvider
    logger   Logger
}

func NewServiceFactory(provider godi.ServiceProvider, logger Logger) ServiceFactory {
    return &serviceFactory{
        provider: provider,
        logger:   logger,
    }
}

func (f *serviceFactory) CreateService(serviceType string) (Service, error) {
    switch serviceType {
    case "email":
        return godi.Resolve[EmailService](f.provider)
    case "sms":
        return godi.Resolve[SMSService](f.provider)
    case "push":
        return godi.Resolve[PushService](f.provider)
    default:
        return nil, fmt.Errorf("unknown service type: %s", serviceType)
    }
}

// Usage
factory, _ := godi.Resolve[ServiceFactory](provider)
service, _ := factory.CreateService("email")
```

### Abstract Factory

Create families of related services:

```go
// Abstract factory for different environments
type EnvironmentFactory interface {
    CreateDatabase() Database
    CreateCache() Cache
    CreateQueue() Queue
}

// Development factory
type developmentFactory struct {
    provider godi.ServiceProvider
}

func NewDevelopmentFactory(provider godi.ServiceProvider) EnvironmentFactory {
    return &developmentFactory{provider: provider}
}

func (f *developmentFactory) CreateDatabase() Database {
    db, _ := godi.ResolveKeyed[Database](f.provider, "sqlite")
    return db
}

func (f *developmentFactory) CreateCache() Cache {
    cache, _ := godi.ResolveKeyed[Cache](f.provider, "memory")
    return cache
}

func (f *developmentFactory) CreateQueue() Queue {
    queue, _ := godi.ResolveKeyed[Queue](f.provider, "memory")
    return queue
}

// Production factory
type productionFactory struct {
    provider godi.ServiceProvider
}

func (f *productionFactory) CreateDatabase() Database {
    db, _ := godi.ResolveKeyed[Database](f.provider, "postgres")
    return db
}

func (f *productionFactory) CreateCache() Cache {
    cache, _ := godi.ResolveKeyed[Cache](f.provider, "redis")
    return cache
}

func (f *productionFactory) CreateQueue() Queue {
    queue, _ := godi.ResolveKeyed[Queue](f.provider, "rabbitmq")
    return queue
}
```

## Strategy Pattern

### Dynamic Strategy Selection

```go
// Strategy interface
type PaymentStrategy interface {
    ProcessPayment(amount float64) error
}

// Context that uses strategies
type PaymentProcessor struct {
    strategies map[string]PaymentStrategy
    logger     Logger
}

func NewPaymentProcessor(provider godi.ServiceProvider, logger Logger) *PaymentProcessor {
    return &PaymentProcessor{
        strategies: map[string]PaymentStrategy{
            "credit_card": resolveStrategy[CreditCardStrategy](provider),
            "paypal":      resolveStrategy[PayPalStrategy](provider),
            "crypto":      resolveStrategy[CryptoStrategy](provider),
        },
        logger: logger,
    }
}

func resolveStrategy[T PaymentStrategy](provider godi.ServiceProvider) PaymentStrategy {
    strategy, _ := godi.Resolve[T](provider)
    return strategy
}

func (p *PaymentProcessor) Process(method string, amount float64) error {
    strategy, ok := p.strategies[method]
    if !ok {
        return fmt.Errorf("unknown payment method: %s", method)
    }

    p.logger.Info("Processing payment", "method", method, "amount", amount)
    return strategy.ProcessPayment(amount)
}
```

## Chain of Responsibility

### Middleware Chain

```go
// Handler interface
type Handler interface {
    Handle(ctx context.Context, request Request) (Response, error)
}

// Middleware interface
type Middleware interface {
    Process(next Handler) Handler
}

// Chain builder
type ChainBuilder struct {
    godi.In

    Middlewares []Middleware `group:"middleware"`
}

func BuildChain(params ChainBuilder, final Handler) Handler {
    // Sort middlewares by priority
    sort.Slice(params.Middlewares, func(i, j int) bool {
        return getPriority(params.Middlewares[i]) < getPriority(params.Middlewares[j])
    })

    // Build chain in reverse order
    handler := final
    for i := len(params.Middlewares) - 1; i >= 0; i-- {
        handler = params.Middlewares[i].Process(handler)
    }

    return handler
}

// Example middleware
type loggingMiddleware struct {
    logger Logger
}

func (m *loggingMiddleware) Process(next Handler) Handler {
    return HandlerFunc(func(ctx context.Context, req Request) (Response, error) {
        m.logger.Info("Processing request", "id", req.ID)

        resp, err := next.Handle(ctx, req)

        if err != nil {
            m.logger.Error("Request failed", "id", req.ID, "error", err)
        } else {
            m.logger.Info("Request succeeded", "id", req.ID)
        }

        return resp, err
    })
}
```

## Lazy Initialization

### Lazy Service Resolution

```go
// Lazy wrapper for expensive services
type Lazy[T any] struct {
    provider godi.ServiceProvider
    once     sync.Once
    value    T
    err      error
}

func NewLazy[T any](provider godi.ServiceProvider) *Lazy[T] {
    return &Lazy[T]{
        provider: provider,
    }
}

func (l *Lazy[T]) Get() (T, error) {
    l.once.Do(func() {
        l.value, l.err = godi.Resolve[T](l.provider)
    })

    return l.value, l.err
}

// Usage in service
type MyService struct {
    expensiveService *Lazy[ExpensiveService]
}

func NewMyService(provider godi.ServiceProvider) *MyService {
    return &MyService{
        expensiveService: NewLazy[ExpensiveService](provider),
    }
}

func (s *MyService) DoWork() error {
    // Service is only created when needed
    expensive, err := s.expensiveService.Get()
    if err != nil {
        return err
    }

    return expensive.Process()
}
```

## Service Locator (Anti-)Pattern

While generally discouraged, sometimes useful:

```go
// Service locator for legacy code integration
type ServiceLocator struct {
    provider godi.ServiceProvider
}

func NewServiceLocator(provider godi.ServiceProvider) *ServiceLocator {
    return &ServiceLocator{provider: provider}
}

func (sl *ServiceLocator) Get(serviceType reflect.Type) (interface{}, error) {
    return sl.provider.Resolve(serviceType)
}

func (sl *ServiceLocator) GetKeyed(serviceType reflect.Type, key string) (interface{}, error) {
    return sl.provider.ResolveKeyed(serviceType, key)
}

// Generic version
func Get[T any](sl *ServiceLocator) (T, error) {
    return godi.Resolve[T](sl.provider)
}

// Use sparingly, prefer constructor injection
var Locator *ServiceLocator

func InitializeLocator(provider godi.ServiceProvider) {
    Locator = NewServiceLocator(provider)
}
```

## Composite Pattern

### Composite Services

```go
// Notification service that combines multiple channels
type CompositeNotificationService struct {
    channels []NotificationChannel
    logger   Logger
}

type NotificationChannelParams struct {
    godi.In

    Channels []NotificationChannel `group:"notifications"`
    Logger   Logger
}

func NewCompositeNotificationService(params NotificationChannelParams) NotificationService {
    return &CompositeNotificationService{
        channels: params.Channels,
        logger:   params.Logger,
    }
}

func (s *CompositeNotificationService) Send(notification Notification) error {
    var errors []error

    for _, channel := range s.channels {
        if err := channel.Send(notification); err != nil {
            s.logger.Error("Channel failed", "channel", channel.Name(), "error", err)
            errors = append(errors, err)
        }
    }

    if len(errors) > 0 {
        return fmt.Errorf("notification failed on %d channels", len(errors))
    }

    return nil
}

// Register channels
collection.AddSingleton(NewEmailChannel, godi.Group("notifications"))
collection.AddSingleton(NewSMSChannel, godi.Group("notifications"))
collection.AddSingleton(NewPushChannel, godi.Group("notifications"))
collection.AddSingleton(NewCompositeNotificationService)
```

## Proxy Pattern

### Service Proxy

```go
// Proxy for adding behavior without modifying the service
type CachingProxy struct {
    service UserService
    cache   Cache
    ttl     time.Duration
}

func NewCachingProxy(service UserService, cache Cache) UserService {
    return &CachingProxy{
        service: service,
        cache:   cache,
        ttl:     5 * time.Minute,
    }
}

func (p *CachingProxy) GetUser(ctx context.Context, id string) (*User, error) {
    // Check cache
    cacheKey := fmt.Sprintf("user:%s", id)
    if cached, found := p.cache.Get(cacheKey); found {
        return cached.(*User), nil
    }

    // Call real service
    user, err := p.service.GetUser(ctx, id)
    if err != nil {
        return nil, err
    }

    // Cache result
    p.cache.Set(cacheKey, user, p.ttl)

    return user, nil
}

// Register with decoration
collection.AddScoped(NewUserService)
collection.Decorate(func(service UserService, cache Cache) UserService {
    return NewCachingProxy(service, cache)
})
```

## Observer Pattern

### Event System

```go
// Event system with DI
type EventBus struct {
    handlers map[string][]EventHandler
    mu       sync.RWMutex
}

type EventHandlerParams struct {
    godi.In

    Handlers []EventHandler `group:"events"`
}

func NewEventBus(params EventHandlerParams) *EventBus {
    bus := &EventBus{
        handlers: make(map[string][]EventHandler),
    }

    // Register all handlers
    for _, handler := range params.Handlers {
        for _, eventType := range handler.EventTypes() {
            bus.Subscribe(eventType, handler)
        }
    }

    return bus
}

func (eb *EventBus) Subscribe(eventType string, handler EventHandler) {
    eb.mu.Lock()
    defer eb.mu.Unlock()

    eb.handlers[eventType] = append(eb.handlers[eventType], handler)
}

func (eb *EventBus) Publish(event Event) error {
    eb.mu.RLock()
    handlers := eb.handlers[event.Type()]
    eb.mu.RUnlock()

    for _, handler := range handlers {
        if err := handler.Handle(event); err != nil {
            return err
        }
    }

    return nil
}
```

## Command Pattern

### Command Execution

```go
// Command interface
type Command interface {
    Execute(ctx context.Context) error
    Name() string
}

// Command dispatcher
type CommandDispatcher struct {
    commands map[string]Command
    logger   Logger
}

type CommandParams struct {
    godi.In

    Commands []Command `group:"commands"`
    Logger   Logger
}

func NewCommandDispatcher(params CommandParams) *CommandDispatcher {
    dispatcher := &CommandDispatcher{
        commands: make(map[string]Command),
        logger:   params.Logger,
    }

    for _, cmd := range params.Commands {
        dispatcher.commands[cmd.Name()] = cmd
    }

    return dispatcher
}

func (d *CommandDispatcher) Dispatch(ctx context.Context, commandName string) error {
    cmd, ok := d.commands[commandName]
    if !ok {
        return fmt.Errorf("unknown command: %s", commandName)
    }

    d.logger.Info("Executing command", "name", commandName)

    start := time.Now()
    err := cmd.Execute(ctx)
    duration := time.Since(start)

    if err != nil {
        d.logger.Error("Command failed", "name", commandName, "error", err, "duration", duration)
        return err
    }

    d.logger.Info("Command succeeded", "name", commandName, "duration", duration)
    return nil
}
```

## Unit of Work Pattern

### Transaction Management

```go
// Unit of Work for managing database transactions
type UnitOfWork interface {
    UserRepository() UserRepository
    OrderRepository() OrderRepository
    Commit() error
    Rollback() error
}

type unitOfWork struct {
    tx       *sql.Tx
    userRepo UserRepository
    orderRepo OrderRepository
    committed bool
}

func NewUnitOfWork(db *sql.DB) (UnitOfWork, error) {
    tx, err := db.Begin()
    if err != nil {
        return nil, err
    }

    return &unitOfWork{
        tx:        tx,
        userRepo:  NewTxUserRepository(tx),
        orderRepo: NewTxOrderRepository(tx),
    }, nil
}

func (uow *unitOfWork) UserRepository() UserRepository {
    return uow.userRepo
}

func (uow *unitOfWork) OrderRepository() OrderRepository {
    return uow.orderRepo
}

func (uow *unitOfWork) Commit() error {
    if uow.committed {
        return errors.New("already committed")
    }

    uow.committed = true
    return uow.tx.Commit()
}

func (uow *unitOfWork) Rollback() error {
    if uow.committed {
        return errors.New("already committed")
    }

    return uow.tx.Rollback()
}

// Service using Unit of Work
type OrderService struct {
    db     *sql.DB
    logger Logger
}

func (s *OrderService) CreateOrder(ctx context.Context, order *Order) error {
    uow, err := NewUnitOfWork(s.db)
    if err != nil {
        return err
    }
    defer uow.Rollback() // Rollback if not committed

    // Update user
    user, err := uow.UserRepository().GetByID(ctx, order.UserID)
    if err != nil {
        return err
    }

    user.OrderCount++
    if err := uow.UserRepository().Update(ctx, user); err != nil {
        return err
    }

    // Create order
    if err := uow.OrderRepository().Create(ctx, order); err != nil {
        return err
    }

    // Commit transaction
    return uow.Commit()
}
```

## Specification Pattern

### Dynamic Queries

```go
// Specification interface
type Specification[T any] interface {
    IsSatisfiedBy(item T) bool
    And(other Specification[T]) Specification[T]
    Or(other Specification[T]) Specification[T]
    Not() Specification[T]
}

// Base specification
type baseSpec[T any] struct {
    predicate func(T) bool
}

func (s *baseSpec[T]) IsSatisfiedBy(item T) bool {
    return s.predicate(item)
}

func (s *baseSpec[T]) And(other Specification[T]) Specification[T] {
    return &baseSpec[T]{
        predicate: func(item T) bool {
            return s.IsSatisfiedBy(item) && other.IsSatisfiedBy(item)
        },
    }
}

// User specifications
func UserIsActive() Specification[User] {
    return &baseSpec[User]{
        predicate: func(u User) bool {
            return u.Status == "active"
        },
    }
}

func UserHasRole(role string) Specification[User] {
    return &baseSpec[User]{
        predicate: func(u User) bool {
            return u.Role == role
        },
    }
}

// Repository using specifications
type SpecificationRepository[T any] interface {
    Find(spec Specification[T]) ([]T, error)
}

// Usage
activeAdmins := UserIsActive().And(UserHasRole("admin"))
users, err := repo.Find(activeAdmins)
```

## Summary

These advanced patterns enable:

- **Flexible architectures** with factories and strategies
- **Clean abstractions** with proxies and decorators
- **Complex workflows** with command and unit of work patterns
- **Dynamic behavior** with specifications and observers

Use these patterns judiciously - they add complexity but can greatly improve maintainability and flexibility in large applications.
