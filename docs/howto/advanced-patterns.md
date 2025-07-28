# Advanced Patterns

Advanced techniques for complex scenarios. Start with basic godi features - only use these when truly needed.

## Factory Pattern

Create services dynamically based on runtime conditions:

```go
// Notification factory
type NotificationFactory struct {
    email EmailService
    sms   SMSService
    push  PushService
}

func NewNotificationFactory(email EmailService, sms SMSService, push PushService) *NotificationFactory {
    return &NotificationFactory{
        email: email,
        sms:   sms,
        push:  push,
    }
}

func (f *NotificationFactory) CreateNotifier(userPreference string) (Notifier, error) {
    switch userPreference {
    case "email":
        return f.email, nil
    case "sms":
        return f.sms, nil
    case "push":
        return f.push, nil
    default:
        return nil, fmt.Errorf("unknown preference: %s", userPreference)
    }
}

// Usage
var NotificationModule = godi.NewModule("notification",
    godi.AddSingleton(NewEmailService),
    godi.AddSingleton(NewSMSService),
    godi.AddSingleton(NewPushService),
    godi.AddSingleton(NewNotificationFactory),
)

// In your service
func SendNotification(factory *NotificationFactory, user *User) error {
    notifier, err := factory.CreateNotifier(user.NotificationPreference)
    if err != nil {
        return err
    }

    return notifier.Notify(user, "Your order is ready!")
}
```

## Strategy Pattern

Switch algorithms at runtime:

```go
// Pricing strategies
type PricingStrategy interface {
    CalculatePrice(items []Item) float64
}

type RegularPricing struct{}
func (p *RegularPricing) CalculatePrice(items []Item) float64 {
    total := 0.0
    for _, item := range items {
        total += item.Price
    }
    return total
}

type PremiumPricing struct{}
func (p *PremiumPricing) CalculatePrice(items []Item) float64 {
    total := 0.0
    for _, item := range items {
        total += item.Price * 0.9 // 10% discount
    }
    return total
}

// Service using strategy
type CheckoutService struct {
    provider godi.ServiceProvider
}

func NewCheckoutService(provider godi.ServiceProvider) *CheckoutService {
    return &CheckoutService{provider: provider}
}

func (s *CheckoutService) Checkout(user *User, items []Item) (*Order, error) {
    // Choose strategy based on user type
    var strategy PricingStrategy
    if user.IsPremium {
        strategy, _ = godi.ResolveKeyed[PricingStrategy](s.provider, "premium")
    } else {
        strategy, _ = godi.ResolveKeyed[PricingStrategy](s.provider, "regular")
    }

    price := strategy.CalculatePrice(items)
    return &Order{
        UserID:     user.ID,
        TotalPrice: price,
        Items:      items,
    }, nil
}

// Module
var PricingModule = godi.NewModule("pricing",
    godi.AddSingleton(func() PricingStrategy {
        return &RegularPricing{}
    }, godi.Name("regular")),

    godi.AddSingleton(func() PricingStrategy {
        return &PremiumPricing{}
    }, godi.Name("premium")),

    godi.AddScoped(NewCheckoutService),
)
```

## Chain of Responsibility

Process requests through a chain of handlers:

```go
// Request processor interface
type RequestProcessor interface {
    Process(ctx context.Context, req *Request) error
    SetNext(processor RequestProcessor)
}

// Base processor
type BaseProcessor struct {
    next RequestProcessor
}

func (p *BaseProcessor) SetNext(processor RequestProcessor) {
    p.next = processor
}

func (p *BaseProcessor) ProcessNext(ctx context.Context, req *Request) error {
    if p.next != nil {
        return p.next.Process(ctx, req)
    }
    return nil
}

// Concrete processors
type ValidationProcessor struct {
    BaseProcessor
    validator Validator
}

func (p *ValidationProcessor) Process(ctx context.Context, req *Request) error {
    if err := p.validator.Validate(req); err != nil {
        return fmt.Errorf("validation failed: %w", err)
    }
    return p.ProcessNext(ctx, req)
}

type AuthorizationProcessor struct {
    BaseProcessor
    authService AuthService
}

func (p *AuthorizationProcessor) Process(ctx context.Context, req *Request) error {
    if !p.authService.IsAuthorized(ctx, req.UserID, req.Resource) {
        return errors.New("unauthorized")
    }
    return p.ProcessNext(ctx, req)
}

// Build the chain
type ProcessorChain struct {
    first RequestProcessor
}

func NewProcessorChain(params struct {
    godi.In
    Processors []RequestProcessor `group:"processors"`
}) *ProcessorChain {
    if len(params.Processors) == 0 {
        return &ProcessorChain{}
    }

    // Link processors
    for i := 0; i < len(params.Processors)-1; i++ {
        params.Processors[i].SetNext(params.Processors[i+1])
    }

    return &ProcessorChain{
        first: params.Processors[0],
    }
}

func (c *ProcessorChain) Process(ctx context.Context, req *Request) error {
    if c.first == nil {
        return nil
    }
    return c.first.Process(ctx, req)
}
```

## Unit of Work Pattern

Manage transactions across multiple repositories:

```go
type UnitOfWork interface {
    UserRepository() UserRepository
    OrderRepository() OrderRepository
    Commit() error
    Rollback() error
}

type unitOfWork struct {
    tx           *sql.Tx
    userRepo     UserRepository
    orderRepo    OrderRepository
    committed    bool
}

func NewUnitOfWork(db *sql.DB) (UnitOfWork, error) {
    tx, err := db.Begin()
    if err != nil {
        return nil, err
    }

    return &unitOfWork{
        tx:        tx,
        userRepo:  NewUserRepository(tx),
        orderRepo: NewOrderRepository(tx),
    }, nil
}

func (u *unitOfWork) UserRepository() UserRepository {
    return u.userRepo
}

func (u *unitOfWork) OrderRepository() OrderRepository {
    return u.orderRepo
}

func (u *unitOfWork) Commit() error {
    u.committed = true
    return u.tx.Commit()
}

func (u *unitOfWork) Rollback() error {
    if !u.committed {
        return u.tx.Rollback()
    }
    return nil
}

// Scoped registration ensures one UoW per request
var DataModule = godi.NewModule("data",
    godi.AddSingleton(NewDatabase),
    godi.AddScoped(NewUnitOfWork),
)

// Service using UoW
type OrderService struct {
    provider godi.ServiceProvider
}

func (s *OrderService) CreateOrder(ctx context.Context, userID string, items []Item) error {
    scope := s.provider.CreateScope(ctx)
    defer scope.Close()

    uow, _ := godi.Resolve[UnitOfWork](scope)

    // All operations in same transaction
    user, err := uow.UserRepository().GetByID(userID)
    if err != nil {
        return err
    }

    order := &Order{
        UserID: user.ID,
        Items:  items,
        Total:  calculateTotal(items),
    }

    if err := uow.OrderRepository().Create(order); err != nil {
        return err
    }

    // Update user statistics
    user.TotalOrders++
    if err := uow.UserRepository().Update(user); err != nil {
        return err
    }

    // Commit all changes
    return uow.Commit()
}
```

## Lazy Loading Pattern

Delay expensive initialization:

```go
type LazyService struct {
    initOnce sync.Once
    provider godi.ServiceProvider
    service  ExpensiveService
    err      error
}

func NewLazyService(provider godi.ServiceProvider) *LazyService {
    return &LazyService{
        provider: provider,
    }
}

func (l *LazyService) Get() (ExpensiveService, error) {
    l.initOnce.Do(func() {
        // Only initialize when first needed
        l.service, l.err = godi.Resolve[ExpensiveService](l.provider)
    })

    return l.service, l.err
}

// Usage
type MyService struct {
    lazyReport *LazyService
}

func NewMyService(provider godi.ServiceProvider) *MyService {
    return &MyService{
        lazyReport: NewLazyService(provider),
    }
}

func (s *MyService) GenerateReport() error {
    // Only loads ExpensiveService if report is actually generated
    reportService, err := s.lazyReport.Get()
    if err != nil {
        return err
    }

    return reportService.Generate()
}
```

## Circuit Breaker Pattern

Protect against cascading failures:

```go
type CircuitBreaker struct {
    service       ExternalService
    failureCount  int
    lastFailTime  time.Time
    state         string // "closed", "open", "half-open"
    mu            sync.Mutex
}

func NewCircuitBreaker(service ExternalService) *CircuitBreaker {
    return &CircuitBreaker{
        service: service,
        state:   "closed",
    }
}

func (cb *CircuitBreaker) Call(ctx context.Context, request any) (any, error) {
    cb.mu.Lock()
    defer cb.mu.Unlock()

    // Check circuit state
    if cb.state == "open" {
        if time.Since(cb.lastFailTime) < 30*time.Second {
            return nil, errors.New("circuit breaker is open")
        }
        // Try half-open
        cb.state = "half-open"
    }

    // Make the call
    response, err := cb.service.Call(ctx, request)

    if err != nil {
        cb.failureCount++
        cb.lastFailTime = time.Now()

        if cb.failureCount >= 5 {
            cb.state = "open"
            return nil, fmt.Errorf("circuit breaker opened: %w", err)
        }

        return nil, err
    }

    // Success - reset
    cb.failureCount = 0
    cb.state = "closed"

    return response, nil
}

// Module with circuit breaker
var ExternalServiceModule = godi.NewModule("external",
    godi.AddSingleton(NewExternalAPIClient),
    godi.AddSingleton(func(client *ExternalAPIClient) ExternalService {
        return NewCircuitBreaker(client)
    }),
)
```

## When to Use These Patterns

- **Factory**: When you need to create objects based on runtime conditions
- **Strategy**: When you have multiple algorithms for the same task
- **Chain of Responsibility**: For pipeline processing with multiple steps
- **Unit of Work**: When you need transaction consistency across repositories
- **Lazy Loading**: For expensive resources that might not be used
- **Circuit Breaker**: When calling external services that might fail

## Remember

These patterns add complexity. Always ask:

1. Do I really need this pattern?
2. Would a simpler solution work?
3. Will my team understand this?

Start simple, add patterns only when the benefit is clear!
