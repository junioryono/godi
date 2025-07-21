# Why Dependency Injection?

Let's be honest: "Dependency Injection" sounds complicated. It's not. It's just a tool to solve real problems you face every day.

## The Problem You're Having Right Now

Your Go app started simple:

```go
func main() {
    db := NewDatabase()
    service := NewUserService(db)
    handler := NewHandler(service)
    // Easy!
}
```

Then reality hit:

```go
func main() {
    config := LoadConfig()
    logger := NewLogger(config.LogLevel)

    db := NewDatabase(config.DBUrl, logger)
    cache := NewCache(config.RedisUrl, logger)

    emailClient := NewEmailClient(config.SMTPHost, logger)
    smsClient := NewSMSClient(config.TwilioKey, logger)

    userRepo := NewUserRepository(db, cache, logger)
    authService := NewAuthService(userRepo, emailClient, logger)
    userService := NewUserService(userRepo, authService, logger)

    notificationService := NewNotificationService(emailClient, smsClient, logger)
    orderRepo := NewOrderRepository(db, cache, logger)
    orderService := NewOrderService(orderRepo, userService, notificationService, logger)

    handler := NewHandler(userService, orderService, authService, logger)

    // 😱 And this is just the beginning...
}
```

## Problem #1: The Constructor Cascade

You need to add a rate limiter to your auth service:

```go
// Before: Update the constructor
func NewAuthService(repo UserRepository, email EmailClient, logger Logger) *AuthService

// After: Now with rate limiter
func NewAuthService(repo UserRepository, email EmailClient, logger Logger, limiter RateLimiter) *AuthService
```

**Without DI**: Update every single place that creates AuthService (main.go, tests, integration tests...)

**With DI**: Update just the constructor. Done.

```go
// Just change the constructor
func NewAuthService(repo UserRepository, email EmailClient, logger Logger, limiter RateLimiter) *AuthService {
    return &AuthService{repo, email, logger, limiter}
}

// godi handles injecting the rate limiter everywhere!
```

## Problem #2: Testing Nightmare

Want to test your user service?

**Without DI**:

```go
func TestUserService(t *testing.T) {
    // Set up real database 😱
    db := NewDatabase("postgres://test...")

    // Set up real cache 😱
    cache := NewCache("redis://test...")

    // Set up real logger
    logger := NewLogger("test.log")

    // Finally create service
    service := NewUserService(db, cache, logger)

    // Test... if the database is running... and Redis... and...
}
```

**With DI**:

```go
func TestUserService(t *testing.T) {
    // Use test module with mocks
    testModule := godi.NewModule("test",
        godi.AddSingleton(func() Database { return &MockDB{} }),
        godi.AddSingleton(func() Cache { return &MockCache{} }),
        godi.AddScoped(NewUserService),
    )

    provider := BuildProvider(testModule)
    service, _ := godi.Resolve[*UserService](provider)

    // Test with fast, reliable mocks!
}
```

## Problem #3: Request Isolation

In web apps, each request needs its own context:

**Without DI**: Pass request context through every function

```go
func (h *Handler) CreateUser(ctx context.Context, w http.ResponseWriter, r *http.Request) {
    tx := h.db.BeginTx(ctx)
    userID := GetUserID(ctx)

    // Pass tx and userID to EVERYTHING
    user, err := h.userService.Create(ctx, tx, userID, userData)
    h.auditService.Log(ctx, tx, userID, "created user")
    h.emailService.SendWelcome(ctx, tx, userID, user.Email)

    // Don't forget to commit!
    tx.Commit()
}
```

**With DI**: Use scopes

```go
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
    scope := h.provider.CreateScope(r.Context())
    defer scope.Close()

    // Services automatically share the same transaction!
    userService, _ := godi.Resolve[*UserService](scope.ServiceProvider())
    user, _ := userService.Create(userData)

    // Transaction commits when scope closes
}
```

## Problem #4: Environment Differences

Different services for different environments:

**Without DI**: If/else everywhere

```go
var emailClient EmailClient
if env == "production" {
    emailClient = NewSendGridClient(apiKey)
} else if env == "staging" {
    emailClient = NewSMTPClient(smtpHost)
} else {
    emailClient = NewMockEmailClient()
}

// Repeat for every service... 😭
```

**With DI**: Clean modules

```go
// Choose module based on environment
var appModule godi.ModuleOption
switch env {
case "production":
    appModule = ProductionModule
case "staging":
    appModule = StagingModule
default:
    appModule = DevelopmentModule
}

// That's it!
provider := BuildProvider(appModule)
```

## The Real Magic: Examples

### Adding Multi-Tenancy

Without DI: Rewrite half your app to pass tenant ID everywhere.

With DI: Add one scoped service:

```go
var TenantModule = godi.NewModule("tenant",
    godi.AddScoped(NewTenantContext),
)

// Now every service in that scope has access to the tenant!
```

### Adding Request Tracing

Without DI: Add traceID parameter to 50 functions.

With DI: Add one scoped service:

```go
var TracingModule = godi.NewModule("tracing",
    godi.AddScoped(NewTraceContext),
)

// Every log automatically includes trace IDs!
```

### Switching Databases

Without DI: Find and update every place that creates connections.

With DI: Change one module:

```go
// From
var DBModule = godi.NewModule("db",
    godi.AddSingleton(NewMySQLDatabase),
)

// To
var DBModule = godi.NewModule("db",
    godi.AddSingleton(NewPostgresDatabase),
)
```

## Common Concerns Addressed

### "But I like Go's simplicity!"

godi IS simple:

```go
// 1. Create a module
var AppModule = godi.NewModule("app",
    godi.AddSingleton(NewLogger),
    godi.AddScoped(NewUserService),
)

// 2. Use it
provider := BuildProvider(AppModule)
service, _ := godi.Resolve[*UserService](provider)

// No magic, no annotations, no reflection abuse
```

### "I don't want a framework!"

godi isn't a framework. Your services don't know about godi:

```go
// This is just a normal Go function
func NewUserService(repo UserRepository, logger Logger) *UserService {
    return &UserService{repo: repo, logger: logger}
}

// No imports from godi, no base classes, nothing
```

### "It's overkill for small apps!"

True! If your app is 200 lines, you don't need DI. But when you have:

- Multiple services (5+)
- Any tests
- HTTP handlers
- Different environments

...DI saves time immediately.

## When You Need DI

✅ **You need DI when:**

- Adding a dependency means updating 10+ files
- Testing requires complex setup
- You have request-scoped data (user context, transactions)
- Different environments need different implementations
- You're tired of writing boilerplate

❌ **You don't need DI when:**

- Your app is a single file
- You have no tests
- Dependencies never change
- It's a simple CLI tool

## The Bottom Line

Dependency injection solves real problems:

1. **Change Management** - Update constructors, not callers
2. **Testing** - Swap implementations instantly
3. **Request Isolation** - Each request gets its own world
4. **Environment Flexibility** - Dev/staging/prod made easy

It's not about being "enterprise" or "sophisticated". It's about writing less boilerplate and focusing on your actual business logic.

**Ready to try it?** Check out the [Quick Start](quick-start.md) - you'll be productive in 5 minutes.
