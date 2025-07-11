# Decorators

Decorators allow you to modify or enhance services after they're created but before they're used. This is useful for adding cross-cutting concerns like logging, caching, metrics, or validation.

## What are Decorators?

A decorator is a function that:

- Takes an existing service as input
- Returns a modified or wrapped version of that service
- Maintains the same interface as the original service

## Basic Decorator

### Simple Logging Decorator

```go
// Original service interface
type UserService interface {
    GetUser(ctx context.Context, id string) (*User, error)
    CreateUser(ctx context.Context, user *User) error
}

// Decorator function
func LoggingDecorator(service UserService, logger Logger) UserService {
    return &loggingUserService{
        inner:  service,
        logger: logger,
    }
}

// Decorator implementation
type loggingUserService struct {
    inner  UserService
    logger Logger
}

func (s *loggingUserService) GetUser(ctx context.Context, id string) (*User, error) {
    s.logger.Info("GetUser called", "id", id)

    start := time.Now()
    user, err := s.inner.GetUser(ctx, id)
    duration := time.Since(start)

    if err != nil {
        s.logger.Error("GetUser failed", "id", id, "error", err, "duration", duration)
        return nil, err
    }

    s.logger.Info("GetUser succeeded", "id", id, "duration", duration)
    return user, nil
}

func (s *loggingUserService) CreateUser(ctx context.Context, user *User) error {
    s.logger.Info("CreateUser called", "username", user.Username)

    err := s.inner.CreateUser(ctx, user)

    if err != nil {
        s.logger.Error("CreateUser failed", "username", user.Username, "error", err)
    } else {
        s.logger.Info("CreateUser succeeded", "username", user.Username)
    }

    return err
}
```

### Registering Decorators

```go
// Register the service
collection.AddScoped(NewUserService)

// Register the decorator
collection.Decorate(LoggingDecorator)

// When UserService is resolved, it will be wrapped with logging
```

## Common Decorator Patterns

### Caching Decorator

```go
func CachingDecorator(service ProductService, cache Cache) ProductService {
    return &cachingProductService{
        inner: service,
        cache: cache,
        ttl:   5 * time.Minute,
    }
}

type cachingProductService struct {
    inner ProductService
    cache Cache
    ttl   time.Duration
}

func (s *cachingProductService) GetProduct(ctx context.Context, id string) (*Product, error) {
    // Try cache first
    cacheKey := fmt.Sprintf("product:%s", id)
    if cached, found := s.cache.Get(cacheKey); found {
        return cached.(*Product), nil
    }

    // Cache miss - call inner service
    product, err := s.inner.GetProduct(ctx, id)
    if err != nil {
        return nil, err
    }

    // Cache the result
    s.cache.Set(cacheKey, product, s.ttl)

    return product, nil
}

func (s *cachingProductService) ListProducts(ctx context.Context) ([]*Product, error) {
    // Some methods might not be cached
    return s.inner.ListProducts(ctx)
}
```

### Metrics Decorator

```go
func MetricsDecorator(service OrderService, metrics Metrics) OrderService {
    return &metricsOrderService{
        inner:   service,
        metrics: metrics,
    }
}

type metricsOrderService struct {
    inner   OrderService
    metrics Metrics
}

func (s *metricsOrderService) CreateOrder(ctx context.Context, order *Order) error {
    timer := s.metrics.NewTimer("order.create.duration")
    defer timer.ObserveDuration()

    err := s.inner.CreateOrder(ctx, order)

    if err != nil {
        s.metrics.IncrementCounter("order.create.error")
    } else {
        s.metrics.IncrementCounter("order.create.success")
        s.metrics.RecordValue("order.create.amount", order.Total)
    }

    return err
}
```

### Retry Decorator

```go
func RetryDecorator(service PaymentService, maxRetries int) PaymentService {
    return &retryPaymentService{
        inner:      service,
        maxRetries: maxRetries,
        backoff:    time.Second,
    }
}

type retryPaymentService struct {
    inner      PaymentService
    maxRetries int
    backoff    time.Duration
}

func (s *retryPaymentService) ProcessPayment(ctx context.Context, payment *Payment) error {
    var err error

    for attempt := 0; attempt <= s.maxRetries; attempt++ {
        if attempt > 0 {
            // Exponential backoff
            time.Sleep(s.backoff * time.Duration(1<<(attempt-1)))
        }

        err = s.inner.ProcessPayment(ctx, payment)

        // Success or non-retryable error
        if err == nil || !isRetryable(err) {
            return err
        }
    }

    return fmt.Errorf("max retries exceeded: %w", err)
}
```

### Validation Decorator

```go
func ValidationDecorator(service UserService, validator Validator) UserService {
    return &validatingUserService{
        inner:     service,
        validator: validator,
    }
}

type validatingUserService struct {
    inner     UserService
    validator Validator
}

func (s *validatingUserService) CreateUser(ctx context.Context, user *User) error {
    // Validate input
    if err := s.validator.ValidateStruct(user); err != nil {
        return fmt.Errorf("validation failed: %w", err)
    }

    // Additional business rules
    if len(user.Password) < 8 {
        return errors.New("password must be at least 8 characters")
    }

    if !strings.Contains(user.Email, "@") {
        return errors.New("invalid email format")
    }

    return s.inner.CreateUser(ctx, user)
}
```

## Chaining Decorators

Multiple decorators can be applied to the same service:

```go
// Register service
collection.AddScoped(NewOrderService)

// Register multiple decorators - applied in order
collection.Decorate(ValidationDecorator)  // Applied first
collection.Decorate(LoggingDecorator)     // Applied second
collection.Decorate(MetricsDecorator)     // Applied third

// Result: Metrics(Logging(Validation(OrderService)))
```

## Advanced Decorator Patterns

### Conditional Decorator

```go
func ConditionalCachingDecorator(service ProductService, cache Cache, config *Config) ProductService {
    // Only apply caching in production
    if !config.CachingEnabled {
        return service
    }

    return &cachingProductService{
        inner: service,
        cache: cache,
        ttl:   config.CacheTTL,
    }
}
```

### Context-Aware Decorator

```go
func AuthorizationDecorator(service AdminService, authz AuthorizationService) AdminService {
    return &authorizingAdminService{
        inner: service,
        authz: authz,
    }
}

type authorizingAdminService struct {
    inner AdminService
    authz AuthorizationService
}

func (s *authorizingAdminService) DeleteUser(ctx context.Context, userID string) error {
    // Extract current user from context
    currentUser, ok := ctx.Value("user").(*User)
    if !ok {
        return errors.New("unauthorized: no user in context")
    }

    // Check permissions
    if !s.authz.Can(currentUser, "users:delete") {
        return errors.New("forbidden: insufficient permissions")
    }

    return s.inner.DeleteUser(ctx, userID)
}
```

### Generic Decorator

```go
// Generic logging decorator for any service
func LoggingDecoratorFor[T any](service T, logger Logger, serviceName string) T {
    // Use reflection to create a proxy
    serviceType := reflect.TypeOf(service)
    if serviceType.Kind() != reflect.Interface {
        panic("service must be an interface")
    }

    handler := &genericLoggingHandler{
        inner:       service,
        logger:      logger,
        serviceName: serviceName,
    }

    proxy := reflect.MakeFunc(serviceType, handler.invoke)
    return proxy.Interface().(T)
}
```

### Circuit Breaker Decorator

```go
func CircuitBreakerDecorator(service ExternalService, breaker CircuitBreaker) ExternalService {
    return &circuitBreakerService{
        inner:   service,
        breaker: breaker,
    }
}

type circuitBreakerService struct {
    inner   ExternalService
    breaker CircuitBreaker
}

func (s *circuitBreakerService) CallExternal(ctx context.Context, req *Request) (*Response, error) {
    // Check circuit breaker state
    if !s.breaker.AllowRequest() {
        return nil, errors.New("circuit breaker open")
    }

    // Make the call
    resp, err := s.inner.CallExternal(ctx, req)

    // Record result
    if err != nil {
        s.breaker.RecordFailure()
    } else {
        s.breaker.RecordSuccess()
    }

    return resp, err
}
```

## Testing Decorators

### Unit Testing Decorators

```go
func TestLoggingDecorator(t *testing.T) {
    // Create a mock service
    mockService := &MockUserService{
        GetUserFunc: func(ctx context.Context, id string) (*User, error) {
            if id == "123" {
                return &User{ID: "123", Name: "Test"}, nil
            }
            return nil, errors.New("not found")
        },
    }

    // Create a test logger
    var logs []string
    testLogger := &TestLogger{
        InfoFunc: func(msg string, args ...interface{}) {
            logs = append(logs, msg)
        },
    }

    // Apply decorator
    decorated := LoggingDecorator(mockService, testLogger)

    // Test successful call
    user, err := decorated.GetUser(context.Background(), "123")
    assert.NoError(t, err)
    assert.Equal(t, "Test", user.Name)
    assert.Contains(t, logs, "GetUser called")
    assert.Contains(t, logs, "GetUser succeeded")

    // Test error call
    _, err = decorated.GetUser(context.Background(), "999")
    assert.Error(t, err)
    assert.Contains(t, logs, "GetUser failed")
}
```

### Integration Testing

```go
func TestDecoratorChain(t *testing.T) {
    collection := godi.NewServiceCollection()

    // Register base service
    collection.AddScoped(NewUserService)

    // Register decorators
    collection.Decorate(ValidationDecorator)
    collection.Decorate(LoggingDecorator)
    collection.Decorate(MetricsDecorator)

    provider, _ := collection.BuildServiceProvider()
    defer provider.Close()

    // Resolve decorated service
    service, _ := godi.Resolve[UserService](provider)

    // Test that all decorators are applied
    err := service.CreateUser(context.Background(), &User{
        Username: "test",
        Email:    "invalid-email", // Should fail validation
    })

    assert.Error(t, err)
    assert.Contains(t, err.Error(), "validation failed")
}
```

## Best Practices

### 1. Keep Decorators Focused

Each decorator should handle one concern:

```go
// ✅ Good - single responsibility
func LoggingDecorator(service Service, logger Logger) Service
func CachingDecorator(service Service, cache Cache) Service
func MetricsDecorator(service Service, metrics Metrics) Service

// ❌ Bad - multiple concerns
func LoggingAndCachingDecorator(service Service, logger Logger, cache Cache) Service
```

### 2. Maintain Interface Compatibility

Decorators must implement the same interface:

```go
// ✅ Good - returns same interface
func CachingDecorator(service UserService, cache Cache) UserService {
    return &cachingUserService{inner: service, cache: cache}
}

// ❌ Bad - returns different type
func CachingDecorator(service UserService, cache Cache) *CachingUserService {
    return &CachingUserService{inner: service, cache: cache}
}
```

### 3. Make Decorators Optional

Allow services to work without decorators:

```go
// Service works without any decorators
collection.AddScoped(NewUserService)

// Decorators are optional additions
if config.EnableLogging {
    collection.Decorate(LoggingDecorator)
}

if config.EnableCaching {
    collection.Decorate(CachingDecorator)
}
```

### 4. Document Decorator Order

```go
// Decorators are applied in registration order:
// 1. Validation - validates input
// 2. Logging - logs the validated request
// 3. Metrics - measures the entire operation
collection.Decorate(ValidationDecorator)
collection.Decorate(LoggingDecorator)
collection.Decorate(MetricsDecorator)
```

### 5. Consider Performance

```go
// Lightweight decorators for hot paths
func SimpleMetricsDecorator(service Service, counter Counter) Service {
    return &metricsService{
        inner:   service,
        counter: counter,
    }
}

// Heavier decorators for less frequent operations
func FullAuditDecorator(service Service, audit AuditLog) Service {
    return &auditService{
        inner: service,
        audit: audit,
    }
}
```

## Common Use Cases

### 1. Cross-Cutting Concerns

- Logging
- Metrics
- Tracing
- Error handling

### 2. Security

- Authentication
- Authorization
- Input validation
- Rate limiting

### 3. Resilience

- Retry logic
- Circuit breakers
- Timeouts
- Fallbacks

### 4. Performance

- Caching
- Batching
- Lazy loading
- Connection pooling

### 5. Business Logic

- Audit trails
- Notifications
- Event publishing
- Data transformation

## Summary

Decorators provide a powerful way to:

- Add behavior without modifying original services
- Apply cross-cutting concerns uniformly
- Keep services focused on their core responsibility
- Build flexible, composable systems

Use decorators to enhance your services while maintaining clean separation of concerns.
