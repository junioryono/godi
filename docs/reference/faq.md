# Frequently Asked Questions

Common questions and answers about godi.

## General Questions

### What is godi?

godi is a dependency injection library for Go that helps you manage service dependencies. It automatically wires your services together, making your code more modular, testable, and maintainable.

### When should I use dependency injection?

Use DI when:

- Your app has many services with complex dependencies
- You need different implementations for testing/production
- You're building web applications that need request isolation
- You want to make your code more testable

Don't use DI when:

- Building simple CLI tools
- Your app has fewer than 5 services
- Dependencies rarely change

### How is godi different from other DI libraries?

| Feature            | godi | wire | fx  | dig |
| ------------------ | ---- | ---- | --- | --- |
| Type-safe          | ✅   | ✅   | ✅  | ✅  |
| No code generation | ✅   | ❌   | ✅  | ✅  |
| Scoped services    | ✅   | ❌   | ⚠️  | ✅  |
| Modules            | ✅   | ❌   | ✅  | ❌  |
| Simple API         | ✅   | ⚠️   | ❌  | ✅  |

godi focuses on simplicity and is particularly good for web applications.

## Getting Started

### How do I install godi?

```bash
go get github.com/junioryono/godi/v4
```

### What's the simplest example?

```go
// 1. Define service
type Greeter struct{}
func NewGreeter() *Greeter { return &Greeter{} }

// 2. Register
collection := godi.NewCollection()
collection.AddSingleton(NewGreeter)

// 3. Build
provider, _ := collection.Build()
defer provider.Close()

// 4. Use
greeter, _ := godi.Resolve[*Greeter](provider)
```

### Should I always use modules?

Yes! Even for small apps. Modules keep your code organized:

```go
var AppModule = godi.NewModule("app",
    godi.AddSingleton(NewLogger),
    godi.AddScoped(NewService),
)
```

## Concepts

### What's the difference between Singleton, Scoped, and Transient?

- **Singleton**: Created once, reused everywhere (database connections)
- **Scoped**: Created once per scope/request (user context)
- **Transient**: Created new every time (temporary objects)

### When should I use scopes?

Use scopes for web applications where each request needs isolation:

```go
func handler(provider godi.Provider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        scope, _ := provider.CreateScope(r.Context())
        defer scope.Close()
        // Each request gets its own instances
    }
}
```

### What are keyed services?

Multiple implementations of the same interface:

```go
godi.AddSingleton(NewPrimaryDB, godi.Name("primary"))
godi.AddSingleton(NewReplicaDB, godi.Name("replica"))

primary, _ := godi.ResolveKeyed[Database](provider, "primary")
```

## Common Issues

### "Service not found" error

**Problem**: Service isn't registered

**Solution**: Make sure to register the service:

```go
collection.AddScoped(NewUserService)  // Add this!
```

### "Circular dependency" error

**Problem**: A → B → A

**Solution**: Break the cycle with interfaces or lazy loading:

```go
// Use interface instead of concrete type
type ServiceA struct {
    b ServiceBInterface  // Not *ServiceB
}
```

### "Scope disposed" error

**Problem**: Using scope after closing

**Solution**: Always use defer:

```go
scope, _ := provider.CreateScope(ctx)
defer scope.Close()  // After all usage
```

### Can Singleton depend on Scoped?

No! This would break isolation. Singleton lives forever but Scoped is per-request.

**Rule**:

- Singleton → Singleton ✅
- Scoped → Scoped ✅
- Scoped → Singleton ✅
- Singleton → Scoped ❌

## Testing

### How do I mock services for testing?

Create test modules with mocks:

```go
testModule := godi.NewModule("test",
    godi.AddSingleton(func() Database {
        return &MockDatabase{}
    }),
    godi.AddScoped(NewUserService),  // Real service, mock DB
)
```

### How do I test with different scenarios?

Create different modules for different scenarios:

```go
var HappyPathModule = godi.NewModule("happy", /* mocks that succeed */)
var ErrorModule = godi.NewModule("error", /* mocks that fail */)

// Use different module based on test
```

## Performance

### Is DI slow?

No. godi has minimal overhead:

- Singleton: resolved once at startup
- Scoped: cached within scope
- Transient: only allocation overhead

### How can I optimize performance?

1. Use Singleton for expensive resources
2. Use Scoped for request-specific data
3. Only use Transient when you need unique instances

### Does godi use reflection?

Yes, but efficiently:

- Type analysis is cached
- Resolution is optimized
- No runtime code generation

## Best Practices

### How should I structure modules?

By feature (recommended):

```
features/
├── user/module.go
├── product/module.go
└── order/module.go
```

### Should I use parameter objects?

Yes, when you have 4+ dependencies:

```go
type ServiceParams struct {
    godi.In
    DB     Database
    Cache  Cache
    Logger Logger
}

func NewService(params ServiceParams) *Service
```

### How do I handle optional dependencies?

Use the `optional` tag:

```go
type ServiceParams struct {
    godi.In
    Cache Cache `optional:"true"`
}

func NewService(params ServiceParams) *Service {
    if params.Cache != nil {
        // Use cache
    }
}
```

## Advanced

### Can I register multiple interfaces for one type?

Yes, use the `As` option:

```go
godi.AddSingleton(NewRedisCache,
    godi.As(new(Cache), new(Storage)))
```

### How do I create services dynamically?

Use a factory pattern:

```go
type ServiceFactory struct {
    provider godi.Provider
}

func (f *ServiceFactory) CreateService(type string) Service {
    switch type {
    case "user":
        service, _ := godi.Resolve[*UserService](f.provider)
        return service
    }
}
```

### Can services be disposed automatically?

Yes! Implement the `Disposable` interface:

```go
func (s *Service) Close() error {
    // Cleanup
    return nil
}
// Automatically called when scope/provider closes
```

## Troubleshooting

### How do I debug dependency resolution?

1. Check if service is registered:

```go
if !collection.Contains(reflect.TypeOf((*Service)(nil)).Elem()) {
    log.Println("Not registered!")
}
```

2. Build with validation:

```go
provider, err := collection.Build()
if err != nil {
    log.Printf("Build error: %v", err)
}
```

### How do I visualize dependencies?

Currently, godi doesn't have built-in visualization. You can:

1. Log during resolution
2. Inspect `collection.ToSlice()` for all services
3. Write custom visualization using the descriptors

## Need More Help?

- Check the [Getting Started](../getting-started.md) guide
- Review [API Reference](api.md)
- Look at [example code](../guides/web-apps.md)

Still stuck? File an issue on GitHub with:

1. Your code (simplified)
2. The error message
3. What you expected to happen
