# Best Practices

This guide covers best practices for using godi effectively in your Go applications.

## Service Design

### Use Interfaces

Always define your services as interfaces:

```go
// Good: Interface-based design
type UserService interface {
    GetUser(ctx context.Context, id string) (*User, error)
    CreateUser(ctx context.Context, user *User) error
    UpdateUser(ctx context.Context, user *User) error
    DeleteUser(ctx context.Context, id string) error
}

type userService struct {
    repo   UserRepository
    logger Logger
    cache  Cache
}

func NewUserService(repo UserRepository, logger Logger, cache Cache) UserService {
    return &userService{
        repo:   repo,
        logger: logger,
        cache:  cache,
    }
}
```

**Benefits:**

- Easy testing with mocks
- Clear contracts between components
- Flexibility to change implementations
- Better documentation

### Constructor Injection

Always use constructor injection, not property injection:

```go
// Good: Constructor injection
type OrderService struct {
    userRepo  UserRepository
    orderRepo OrderRepository
    payment   PaymentGateway
    logger    Logger
}

func NewOrderService(
    userRepo UserRepository,
    orderRepo OrderRepository,
    payment PaymentGateway,
    logger Logger,
) *OrderService {
    return &OrderService{
        userRepo:  userRepo,
        orderRepo: orderRepo,
        payment:   payment,
        logger:    logger,
    }
}

// Bad: Property injection
type BadOrderService struct {
    UserRepo  UserRepository // Public fields
    OrderRepo OrderRepository
}
```

### Single Responsibility

Each service should have a single, well-defined responsibility:

```go
// Good: Focused services
type AuthService interface {
    Login(username, password string) (*User, error)
    Logout(token string) error
    ValidateToken(token string) (*Claims, error)
}

type UserService interface {
    GetUser(id string) (*User, error)
    UpdateProfile(id string, profile Profile) error
}

// Bad: Mixed responsibilities
type BadService interface {
    // Auth methods
    Login(username, password string) (*User, error)

    // User methods
    GetUser(id string) (*User, error)

    // Email methods
    SendEmail(to, subject, body string) error
}
```

## Lifetime Management

### Choose the Right Lifetime

```go
// Singleton: Stateless, thread-safe, shared resources
services.AddSingleton(NewLogger)           // ✅ Stateless
services.AddSingleton(NewConfiguration)    // ✅ Immutable
services.AddSingleton(NewMetricsCollector) // ✅ Thread-safe

// Scoped: Request-specific, holds state during request
services.AddScoped(NewUnitOfWork)          // ✅ Transaction boundary
services.AddScoped(NewRequestContext)      // ✅ Request metadata
services.AddScoped(NewRepository)          // ✅ May use scoped transaction

// Transient: Lightweight, unique state, short-lived
services.AddTransient(NewCommand)          // ✅ Single operation
services.AddTransient(NewValidator)        // ✅ Stateless operation
services.AddTransient(NewEmailMessage)     // ✅ Unique per use
```

### Avoid Captive Dependencies

Never inject a service with a shorter lifetime into one with a longer lifetime:

```go
// Bad: Scoped service in singleton
type BadSingleton struct {
    scopedService ScopedService // ❌ Will capture first scope's instance
}

// Good: Use a factory or service provider
type GoodSingleton struct {
    provider godi.ServiceProvider
}

func (s *GoodSingleton) DoWork(ctx context.Context) error {
    scope := s.provider.CreateScope(ctx)
    defer scope.Close()

    scopedService, _ := godi.Resolve[ScopedService](scope.ServiceProvider())
    return scopedService.Process()
}
```

## Scope Management

### Always Close Scopes

```go
// Good: Always use defer
func HandleRequest(provider godi.ServiceProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        scope := provider.CreateScope(r.Context())
        defer scope.Close() // ✅ Always cleanup

        // Handle request
    }
}

// Bad: Manual cleanup
func BadHandler(provider godi.ServiceProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        scope := provider.CreateScope(r.Context())

        // Handle request

        scope.Close() // ❌ May not run if panic occurs
    }
}
```

### One Scope Per Operation

```go
// Web request = one scope
// Background job = one scope
// Test case = one scope

// Good: Scope per job
func ProcessJobs(provider godi.ServiceProvider, jobs <-chan Job) {
    for job := range jobs {
        func(j Job) {
            scope := provider.CreateScope(context.Background())
            defer scope.Close()

            processor, _ := godi.Resolve[JobProcessor](scope.ServiceProvider())
            processor.Process(j)
        }(job)
    }
}
```

## Error Handling

### Check Resolution Errors

Always handle errors from service resolution:

```go
// Good: Check errors
service, err := godi.Resolve[UserService](provider)
if err != nil {
    if godi.IsNotFound(err) {
        return nil, fmt.Errorf("user service not registered")
    }
    return nil, fmt.Errorf("failed to resolve user service: %w", err)
}

// Bad: Ignoring errors
service, _ := godi.Resolve[UserService](provider) // ❌
```

### Constructor Validation

Validate dependencies in constructors:

```go
func NewPaymentService(
    gateway PaymentGateway,
    logger Logger,
    config *PaymentConfig,
) (*PaymentService, error) {
    if gateway == nil {
        return nil, errors.New("gateway is required")
    }
    if logger == nil {
        return nil, errors.New("logger is required")
    }
    if config == nil {
        return nil, errors.New("config is required")
    }
    if config.APIKey == "" {
        return nil, errors.New("API key is required")
    }

    return &PaymentService{
        gateway: gateway,
        logger:  logger,
        config:  config,
    }, nil
}
```

## Testing

### Use Test Containers

Create separate service collections for tests:

```go
func TestUserService(t *testing.T) {
    // Test-specific container
    services := godi.NewServiceCollection()

    // Register mocks
    services.AddSingleton(func() UserRepository {
        return &MockUserRepository{
            users: map[string]*User{
                "1": {ID: "1", Name: "Test User"},
            },
        }
    })
    services.AddSingleton(func() Logger {
        return &TestLogger{t: t}
    })
    services.AddScoped(NewUserService)

    provider, err := services.BuildServiceProvider()
    require.NoError(t, err)
    defer provider.Close()

    // Test with mocks
    service, err := godi.Resolve[UserService](provider)
    require.NoError(t, err)

    user, err := service.GetUser(context.Background(), "1")
    assert.NoError(t, err)
    assert.Equal(t, "Test User", user.Name)
}
```

### Test Helpers

Create helpers for common test scenarios:

```go
// testutil/di.go
func NewTestProvider(t *testing.T, opts ...TestOption) godi.ServiceProvider {
    services := godi.NewServiceCollection()

    // Default test services
    services.AddSingleton(NewTestLogger)
    services.AddSingleton(NewTestConfig)

    // Apply options
    for _, opt := range opts {
        opt(services)
    }

    provider, err := services.BuildServiceProvider()
    require.NoError(t, err)

    t.Cleanup(func() {
        provider.Close()
    })

    return provider
}

type TestOption func(godi.ServiceCollection)

func WithMockDatabase(mock Database) TestOption {
    return func(s godi.ServiceCollection) {
        s.AddSingleton(func() Database { return mock })
    }
}
```

## Module Organization

### Group Related Services

```go
// modules/auth.go
var AuthModule = godi.Module("auth",
    godi.AddSingleton(NewPasswordHasher),
    godi.AddSingleton(NewJWTService),
    godi.AddScoped(NewAuthService),
    godi.AddScoped(NewPermissionService),
)

// modules/data.go
var DataModule = godi.Module("data",
    godi.AddSingleton(NewDatabaseConnection),
    godi.AddScoped(NewUnitOfWork),
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewOrderRepository),
)

// modules/api.go
var APIModule = godi.Module("api",
    godi.AddModule(AuthModule),
    godi.AddModule(DataModule),
    godi.AddScoped(NewUserHandler),
    godi.AddScoped(NewOrderHandler),
)
```

### Module Dependencies

Define clear module dependencies:

```go
// Core module has no dependencies
var CoreModule = godi.Module("core",
    godi.AddSingleton(NewConfig),
    godi.AddSingleton(NewLogger),
    godi.AddSingleton(NewMetrics),
)

// Data module depends on core
var DataModule = godi.Module("data",
    godi.AddModule(CoreModule), // Explicit dependency
    godi.AddSingleton(NewDatabase),
    godi.AddScoped(NewRepository),
)

// Business module depends on data
var BusinessModule = godi.Module("business",
    godi.AddModule(DataModule), // Includes core transitively
    godi.AddScoped(NewUserService),
    godi.AddScoped(NewOrderService),
)
```

## Performance

### Cache Service Resolution

For hot paths, cache resolved services:

```go
type CachedHandler struct {
    provider godi.ServiceProvider
    service  UserService
    mu       sync.RWMutex
}

func (h *CachedHandler) getService() (UserService, error) {
    h.mu.RLock()
    if h.service != nil {
        h.mu.RUnlock()
        return h.service, nil
    }
    h.mu.RUnlock()

    h.mu.Lock()
    defer h.mu.Unlock()

    if h.service != nil {
        return h.service, nil
    }

    service, err := godi.Resolve[UserService](h.provider)
    if err != nil {
        return nil, err
    }

    h.service = service
    return service, nil
}
```

### Avoid Over-Injection

Don't inject everything:

```go
// Good: Inject services
func NewOrderService(repo OrderRepository, payment PaymentGateway) *OrderService

// Bad: Injecting simple values
func NewBadService(
    repo Repository,
    timeout time.Duration,      // ❌ Pass in config instead
    maxRetries int,            // ❌ Pass in config instead
    debugMode bool,            // ❌ Pass in config instead
) *BadService

// Good: Group configuration
type ServiceConfig struct {
    Timeout    time.Duration
    MaxRetries int
    DebugMode  bool
}

func NewGoodService(repo Repository, config ServiceConfig) *GoodService
```

## Common Pitfalls

### 1. Circular Dependencies

```go
// Bad: Circular dependency
type UserService struct {
    orderService OrderService
}

type OrderService struct {
    userService UserService // ❌ Circular!
}

// Good: Break the cycle
type UserService struct {
    orderRepo OrderRepository // Use repository instead
}

type OrderService struct {
    userRepo UserRepository // Use repository instead
}
```

### 2. Service Locator Anti-Pattern

```go
// Bad: Service locator
type BadService struct {
    provider godi.ServiceProvider
}

func (s *BadService) DoWork() error {
    // Resolving services in methods
    repo, _ := godi.Resolve[Repository](s.provider) // ❌
    return repo.Save(data)
}

// Good: Constructor injection
type GoodService struct {
    repo Repository
}

func NewGoodService(repo Repository) *GoodService {
    return &GoodService{repo: repo}
}
```

### 3. Leaking Abstractions

```go
// Bad: Exposing DI framework
func NewBadService(provider godi.ServiceProvider) *BadService // ❌

// Good: Hide DI details
func NewGoodService(dep1 Dep1, dep2 Dep2) *GoodService // ✅
```

## Summary Checklist

✅ **DO:**

- Use interfaces for services
- Choose appropriate lifetimes
- Always close scopes with defer
- Handle resolution errors
- Test with mock implementations
- Group services in modules
- Validate in constructors

❌ **DON'T:**

- Mix service lifetimes incorrectly
- Use service locator pattern
- Ignore errors
- Create circular dependencies
- Expose DI framework details
- Over-inject simple values
- Forget to close scopes

Following these best practices will help you build maintainable, testable, and scalable applications with godi.
