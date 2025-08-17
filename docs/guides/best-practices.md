# Best Practices

Follow these guidelines to build clean, maintainable applications with godi.

## Use Modules from the Start

Even for small apps, modules keep your code organized:

```go
// ✅ Good: Organized with modules
var CoreModule = godi.NewModule("core",
    godi.AddSingleton(NewConfig),
    godi.AddSingleton(NewLogger),
)

var DataModule = godi.NewModule("data",
    godi.AddSingleton(NewDatabase),
    godi.AddScoped(NewRepository),
)

var AppModule = godi.NewModule("app",
    godi.AddScoped(NewUserService),
)

// ❌ Avoid: Scattered registrations
services.AddSingleton(NewConfig)
services.AddSingleton(NewLogger)
services.AddSingleton(NewDatabase)
services.AddScoped(NewRepository)
services.AddScoped(NewUserService)
```

## Design for Interfaces

Always use interfaces for your services to enable testing and flexibility:

```go
// ✅ Good: Interface-based design
type UserRepository interface {
    GetUser(id string) (*User, error)
    SaveUser(user *User) error
}

type userRepository struct {
    db Database
}

func NewUserRepository(db Database) UserRepository {
    return &userRepository{db: db}
}

// ❌ Bad: Concrete types everywhere
func NewUserService(repo *userRepository) *UserService {
    // Can't mock in tests!
}
```

## Choose the Right Lifetime

### Singleton - Shared Forever

Use for stateless, thread-safe services:

```go
var InfrastructureModule = godi.NewModule("infra",
    godi.AddSingleton(NewLogger),        // ✅ Stateless
    godi.AddSingleton(NewConfiguration), // ✅ Immutable
    godi.AddSingleton(NewHTTPClient),    // ✅ Thread-safe with pooling
)
```

### Scoped - Per Request/Operation

Use for stateful, request-specific services:

```go
var RequestModule = godi.NewModule("request",
    godi.AddScoped(NewTransaction),    // ✅ Request-specific
    godi.AddScoped(NewUserContext),    // ✅ Contains request data
    godi.AddScoped(NewAuditLogger),    // ✅ Logs for this request
)
```

### Common Mistake: Captive Dependencies

Never inject scoped services into singletons:

```go
// ❌ BAD: Singleton captures first request's transaction!
type BadService struct {
    tx Transaction // Scoped service in singleton
}

func NewBadService(tx Transaction) *BadService {
    return &BadService{tx: tx}
}

// ✅ GOOD: Use a factory pattern
type GoodService struct {
    provider godi.ServiceProvider
}

func NewGoodService(provider godi.ServiceProvider) *GoodService {
    return &GoodService{provider: provider}
}

func (s *GoodService) DoWork(ctx context.Context) error {
    scope := s.provider.CreateScope(ctx)
    defer scope.Close()

    tx, _ := godi.Resolve[Transaction](scope)
    // Use transaction for this request only
}
```

## Always Close Scopes

```go
// ✅ Good: Always use defer
func HandleRequest(provider godi.ServiceProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        scope := provider.CreateScope(r.Context())
        defer scope.Close() // Guaranteed cleanup

        // Handle request...
    }
}

// ❌ Bad: Manual cleanup
func BadHandler(provider godi.ServiceProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        scope := provider.CreateScope(r.Context())
        // Handle request...
        scope.Close() // Might not run if panic!
    }
}
```

## Error Handling

Always check errors from DI operations:

```go
// ✅ Good: Check all errors
provider, err := services.Build()
if err != nil {
    log.Fatal("Failed to build provider:", err)
}

service, err := godi.Resolve[UserService](provider)
if err != nil {
    http.Error(w, "Service unavailable", 500)
    return
}

// ❌ Bad: Ignoring errors
provider, _ := services.Build()
service, _ := godi.Resolve[UserService](provider)
```

## Module Organization

Structure your modules by feature or layer:

```go
// Feature-based (recommended for most apps)
project/
├── features/
│   ├── user/
│   │   ├── module.go
│   │   ├── service.go
│   │   └── repository.go
│   ├── auth/
│   │   ├── module.go
│   │   └── service.go
│   └── billing/
│       ├── module.go
│       └── service.go
└── main.go

// Layer-based (for larger apps)
project/
├── modules/
│   ├── core.go
│   ├── data.go
│   ├── business.go
│   └── web.go
└── main.go
```

## Testing Best Practices

### Create Test Modules

```go
// testutil/modules.go
func NewMockDataModule() godi.ModuleOption {
    return godi.NewModule("test-data",
        godi.AddSingleton(func() Database {
            return &MockDatabase{
                users: []User{{ID: "1", Name: "Test"}},
            }
        }),
        godi.AddSingleton(func() Cache {
            return &MockCache{}
        }),
    )
}

// In tests
func TestUserService(t *testing.T) {
    testModule := godi.NewModule("test",
        NewMockDataModule(),
        godi.AddScoped(NewUserService),
    )

    // Test with mocks...
}
```

### Use Helper Functions

```go
// testutil/di.go
func BuildTestProvider(t *testing.T, modules ...godi.ModuleOption) godi.ServiceProvider {
    services := godi.NewCollection()

    err := services.AddModules(modules...)
    require.NoError(t, err)

    provider, err := services.Build()
    require.NoError(t, err)

    t.Cleanup(func() {
        provider.Close()
    })

    return provider
}
```

## Common Anti-Patterns to Avoid

### 1. Service Locator Pattern

```go
// ❌ Bad: Service locator
type BadService struct {
    provider godi.ServiceProvider
}

func (s *BadService) DoWork() {
    // Resolving inside methods = hidden dependencies
    repo, _ := godi.Resolve[Repository](s.provider)
}

// ✅ Good: Constructor injection
type GoodService struct {
    repo Repository
}

func NewGoodService(repo Repository) *GoodService {
    return &GoodService{repo: repo}
}
```

### 2. Over-Injection

```go
// ❌ Bad: Too many dependencies
func NewBadService(
    logger Logger,
    db Database,
    cache Cache,
    email EmailService,
    sms SMSService,
    push PushService,
    config Config,
    metrics Metrics,
    // ... 10 more
) *BadService

// ✅ Good: Group related dependencies
type NotificationServices struct {
    Email EmailService
    SMS   SMSService
    Push  PushService
}

func NewGoodService(
    logger Logger,
    db Database,
    notifications NotificationServices,
) *GoodService
```

## Performance Tips

1. **Use Singletons for Expensive Resources**

   ```go
   godi.AddSingleton(NewDatabasePool)    // Connection pooling
   godi.AddSingleton(NewHTTPClient)      // Reuse connections
   ```

2. **Dispose Scopes Promptly**

   ```go
   // Process each item in its own scope
   for _, item := range items {
       func() {
           scope := provider.CreateScope(ctx)
           defer scope.Close()
           processItem(scope, item)
       }()
   }
   ```

3. **Cache Resolutions in Hot Paths**

   ```go
   // Resolve once, use many times
   handler, _ := godi.Resolve[Handler](provider)

   for _, request := range requests {
       handler.Process(request)
   }
   ```

## Summary Checklist

✅ **DO:**

- Use modules to organize services
- Design with interfaces
- Choose appropriate lifetimes
- Always close scopes with defer
- Handle all errors
- Create test modules for mocking
- Keep constructors simple

❌ **DON'T:**

- Mix singleton and scoped incorrectly
- Use service locator pattern
- Ignore errors
- Forget to close scopes
- Over-inject dependencies
- Put logic in constructors

Following these practices will help you build maintainable, testable Go applications with godi!
