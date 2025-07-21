# Decorators

Decorators wrap services to add behavior like logging, caching, or metrics without changing the original service.

## Basic Example

```go
// Original service
type UserService interface {
    GetUser(id string) (*User, error)
}

type userService struct {
    db Database
}

func NewUserService(db Database) UserService {
    return &userService{db: db}
}

// Logging decorator
func LoggingDecorator(service UserService, logger Logger) UserService {
    return &loggingUserService{
        inner:  service,
        logger: logger,
    }
}

type loggingUserService struct {
    inner  UserService
    logger Logger
}

func (s *loggingUserService) GetUser(id string) (*User, error) {
    s.logger.Info("Getting user", "id", id)

    user, err := s.inner.GetUser(id)

    if err != nil {
        s.logger.Error("Failed to get user", "id", id, "error", err)
        return nil, err
    }

    s.logger.Info("Got user", "id", id, "name", user.Name)
    return user, nil
}

// Register with decorator
var UserModule = godi.NewModule("user",
    godi.AddScoped(NewUserService),
    godi.AddDecorator(LoggingDecorator),
)
```

## Common Decorators

### Caching Decorator

```go
func CachingDecorator(service ProductService, cache Cache) ProductService {
    return &cachingService{
        inner: service,
        cache: cache,
        ttl:   5 * time.Minute,
    }
}

type cachingService struct {
    inner ProductService
    cache Cache
    ttl   time.Duration
}

func (s *cachingService) GetProduct(id string) (*Product, error) {
    // Check cache first
    if cached, ok := s.cache.Get(id); ok {
        return cached.(*Product), nil
    }

    // Get from inner service
    product, err := s.inner.GetProduct(id)
    if err != nil {
        return nil, err
    }

    // Cache for next time
    s.cache.Set(id, product, s.ttl)
    return product, nil
}
```

### Metrics Decorator

```go
func MetricsDecorator(service OrderService, metrics Metrics) OrderService {
    return &metricsService{
        inner:   service,
        metrics: metrics,
    }
}

type metricsService struct {
    inner   OrderService
    metrics Metrics
}

func (s *metricsService) CreateOrder(order *Order) error {
    start := time.Now()

    err := s.inner.CreateOrder(order)

    duration := time.Since(start)
    s.metrics.RecordDuration("order.create", duration)

    if err != nil {
        s.metrics.IncrementCounter("order.create.error")
    } else {
        s.metrics.IncrementCounter("order.create.success")
    }

    return err
}
```

### Retry Decorator

```go
func RetryDecorator(service PaymentService, maxAttempts int) PaymentService {
    return &retryService{
        inner:       service,
        maxAttempts: maxAttempts,
    }
}

type retryService struct {
    inner       PaymentService
    maxAttempts int
}

func (s *retryService) ProcessPayment(payment *Payment) error {
    var err error

    for attempt := 1; attempt <= s.maxAttempts; attempt++ {
        err = s.inner.ProcessPayment(payment)

        if err == nil {
            return nil // Success!
        }

        if attempt < s.maxAttempts {
            time.Sleep(time.Duration(attempt) * time.Second)
        }
    }

    return fmt.Errorf("failed after %d attempts: %w", s.maxAttempts, err)
}
```

## Chaining Decorators

Decorators are applied in order:

```go
var PaymentModule = godi.NewModule("payment",
    godi.AddScoped(NewPaymentService),

    // Applied in this order:
    godi.AddDecorator(ValidationDecorator),  // 1. Validates input
    godi.AddDecorator(LoggingDecorator),     // 2. Logs the request
    godi.AddDecorator(MetricsDecorator),     // 3. Records metrics
    godi.AddDecorator(RetryDecorator),       // 4. Retries on failure
)

// Result: Retry(Metrics(Logging(Validation(PaymentService))))
```

## When to Use Decorators

Use decorators for **cross-cutting concerns**:

- ✅ Logging
- ✅ Metrics/Monitoring
- ✅ Caching
- ✅ Retry logic
- ✅ Rate limiting
- ✅ Authorization
- ✅ Validation

Don't use decorators for:

- ❌ Business logic (put in service)
- ❌ Data transformation (use separate layer)
- ❌ Complex workflows (use patterns)

## Best Practices

1. **Keep decorators focused** - One concern per decorator
2. **Maintain the interface** - Decorator must return same type
3. **Make decorators optional** - Service should work without them
4. **Order matters** - Think about decoration sequence

## Testing with Decorators

```go
func TestUserServiceWithLogging(t *testing.T) {
    // Create mock logger to verify behavior
    mockLogger := &MockLogger{}

    testModule := godi.NewModule("test",
        godi.AddSingleton(func() Database { return &MockDB{} }),
        godi.AddSingleton(func() Logger { return mockLogger }),
        godi.AddScoped(NewUserService),
        godi.AddDecorator(LoggingDecorator),
    )

    // ... test and verify mockLogger was called
}
```

Decorators are powerful for adding behavior without modifying your services. Use them wisely!
