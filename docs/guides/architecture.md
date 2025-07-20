# Architecture Guide

This guide explains how godi works internally and how to architect applications using dependency injection effectively.

## How godi Works

### Core Components

```
┌────────────────────────────────────┐
│          ServiceCollection         │
│  ┌─────────────┐  ┌─────────────┐  │
│  │ Descriptor 1│  │ Descriptor 2│  │
│  │  Singleton  │  │   Scoped    │  │
│  └─────────────┘  └─────────────┘  │
└─────────────────┬──────────────────┘
                  │ Build
                  ▼
┌─────────────────────────────────────────────────────┐
│                    ServiceProvider                  │
│  ┌──────────────────────────────────────────────┐   │
│  │                  dig.Container               │   │
│  │  (Singleton and Scoped Service Definitions)  │   │
│  └──────────────────────────────────────────────┘   │
│                                                     │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  │
│  │   Scope 1   │  │   Scope 2   │  │   Scope 3   │  │
│  │ (Request 1) │  │ (Request 2) │  │ (Request 3) │  │
│  └─────────────┘  └─────────────┘  └─────────────┘  │
└─────────────────────────────────────────────────────┘
```

### Lifetime Management

godi implements Microsoft-style DI lifetimes on top of Uber's dig:

1. **Singleton**: Services registered at the root dig container
2. **Scoped**: Services registered in dig scopes

### Resolution Process

```
1. Service Resolution Request
   └─> Check service lifetime
       ├─> Singleton: Resolve from root container
       └─> Scoped: Resolve from current scope

2. Dependency Analysis (by dig)
   └─> Build dependency graph
       └─> Resolve dependencies recursively
           └─> Return constructed instance
```

## Application Architecture Patterns

### Clean Architecture

```
┌─────────────────────────────────────────────┐
│              Presentation Layer             │
│         (HTTP Handlers, CLI, gRPC)          │
├─────────────────────────────────────────────┤
│             Application Layer               │
│        (Use Cases, Business Logic)          │
├─────────────────────────────────────────────┤
│               Domain Layer                  │
│        (Entities, Domain Services)          │
├─────────────────────────────────────────────┤
│           Infrastructure Layer              │
│     (Repositories, External Services)       │
└─────────────────────────────────────────────┘
```

Implementation with godi:

```go
// Domain layer - no dependencies
var DomainModule = godi.Module("domain",
    godi.AddSingleton(NewDomainEventBus),
    godi.AddSingleton(NewDomainValidator),
)

// Infrastructure layer - implements domain interfaces
var InfrastructureModule = godi.Module("infrastructure",
    godi.AddModule(DomainModule),
    godi.AddSingleton(NewPostgresDatabase),
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewOrderRepository),
    godi.AddSingleton(NewEmailService),
)

// Application layer - uses domain and infrastructure
var ApplicationModule = godi.Module("application",
    godi.AddModule(InfrastructureModule),
    godi.AddScoped(NewCreateUserUseCase),
    godi.AddScoped(NewProcessOrderUseCase),
)

// Presentation layer - uses application layer
var PresentationModule = godi.Module("presentation",
    godi.AddModule(ApplicationModule),
    godi.AddScoped(NewUserHTTPHandler),
    godi.AddScoped(NewOrderGRPCHandler),
)
```

### Hexagonal Architecture (Ports and Adapters)

```
           ┌─────────────────┐
           │  HTTP Adapter   │
           └────────┬────────┘
                    │
┌──────────────┐    ▼    ┌──────────────┐
│ gRPC Adapter │◄───────►│     Core     │
└──────────────┘         │   Business   │
                         │    Logic     │
┌──────────────┐         │              │
│  CLI Adapter │◄───────►│   (Ports)    │
└──────────────┘         └──────┬───────┘
                                │
           ┌────────────────────┴───────┐
           │                            │
    ┌──────▼──────┐            ┌────────▼────────┐
    │  Database   │            │ Message Queue   │
    │   Adapter   │            │    Adapter      │
    └─────────────┘            └─────────────────┘
```

Implementation:

```go
// Core domain (hexagon center)
type UserPort interface {
    GetUser(id string) (*User, error)
    CreateUser(user *User) error
}

type NotificationPort interface {
    SendEmail(to, subject, body string) error
}

// Adapters implement ports
type PostgresAdapter struct {
    db *sql.DB
}

func (a *PostgresAdapter) GetUser(id string) (*User, error) {
    // Database-specific implementation
}

// Wire with DI
var CoreModule = godi.Module("core",
    godi.AddScoped(NewUserService), // Uses UserPort
)

var AdaptersModule = godi.Module("adapters",
    // Database adapter
    godi.AddSingleton(NewPostgresAdapter),
    godi.AddScoped(func(adapter *PostgresAdapter) UserPort {
        return adapter
    }),

    // Email adapter
    godi.AddSingleton(NewSMTPAdapter),
    godi.AddSingleton(func(adapter *SMTPAdapter) NotificationPort {
        return adapter
    }),
)
```

### Vertical Slice Architecture

Organize by features rather than layers:

```
features/
├── user/
│   ├── create/
│   │   ├── handler.go
│   │   ├── service.go
│   │   ├── repository.go
│   │   └── module.go
│   ├── list/
│   │   ├── handler.go
│   │   ├── service.go
│   │   ├── repository.go
│   │   └── module.go
│   └── module.go
├── order/
│   ├── create/
│   ├── process/
│   └── module.go
└── shared/
    └── module.go
```

Module organization:

```go
// features/user/create/module.go
var CreateUserModule = godi.Module("user.create",
    godi.AddScoped(NewCreateUserHandler),
    godi.AddScoped(NewCreateUserService),
    godi.AddScoped(NewCreateUserRepository),
)

// features/user/module.go
var UserModule = godi.Module("user",
    godi.AddModule(CreateUserModule),
    godi.AddModule(ListUserModule),
    godi.AddModule(UpdateUserModule),
    godi.AddModule(DeleteUserModule),
)

// features/module.go
var FeaturesModule = godi.Module("features",
    godi.AddModule(UserModule),
    godi.AddModule(OrderModule),
    godi.AddModule(ProductModule),
)
```

## Service Organization Patterns

### Service Layers

```go
// 1. Infrastructure Services (Singletons)
// - Database connections
// - Cache clients
// - Message queues
// - External API clients

// 2. Domain Services (Scoped)
// - Business logic
// - Domain rules
// - Aggregates

// 3. Application Services (Scoped)
// - Use cases
// - Orchestration
// - Transaction boundaries

// 4. Presentation Services (Scoped)
// - Request handlers
// - Response mapping
// - Validation
```

### Service Boundaries

```go
// Clear service boundaries with interfaces
type UserService interface {
    // Public API
    GetUser(ctx context.Context, id string) (*User, error)
    CreateUser(ctx context.Context, req CreateUserRequest) (*User, error)
}

// Implementation details hidden
type userService struct {
    repo       UserRepository      // Internal dependency
    events     EventBus           // Internal dependency
    validator  Validator          // Internal dependency
}

// Only expose what's needed
func NewUserService(
    repo UserRepository,
    events EventBus,
    validator Validator,
) UserService { // Return interface, not implementation
    return &userService{
        repo:      repo,
        events:    events,
        validator: validator,
    }
}
```

## Dependency Management

### Dependency Direction

```
┌─────────────┐
│   Handler   │ ──depends on──> UserService interface
└─────────────┘                        ▲
                                       │
┌─────────────┐                        │
│ UserService │ ────implements─────────┘
└─────────────┘
      │
      └──depends on──> Repository interface
                              ▲
                              │
┌─────────────┐               │
│ Repository  │ ──implements──┘
└─────────────┘
```

### Avoiding Circular Dependencies

```go
// ❌ Bad - Circular dependency
type OrderService struct {
    userService UserService
}

type UserService struct {
    orderService OrderService // Circular!
}

// ✅ Good - Use events or shared interface
type OrderService struct {
    userRepo UserRepository // Use repository instead
    events   EventBus      // Communicate via events
}

type UserService struct {
    userRepo UserRepository
    events   EventBus
}
```

## Testing Architecture

### Test Pyramid with DI

```
         ┌───┐
        /     \      E2E Tests
       /───────\     (Full DI container)
      /         \
     /───────────\   Integration Tests
    /             \  (Partial DI with some mocks)
   /───────────────\
  /                 \ Unit Tests
 /───────────────────\(Mocked dependencies)
└─────────────────────┘
```

### Test Organization

```go
// Unit test - mock all dependencies
func TestUserService_CreateUser(t *testing.T) {
    mockRepo := &MockUserRepository{}
    mockEvents := &MockEventBus{}

    service := NewUserService(mockRepo, mockEvents)
    // Test service in isolation
}

// Integration test - use real implementations where possible
func TestUserAPI_Integration(t *testing.T) {
    collection := godi.NewServiceCollection()
    collection.AddSingleton(NewInMemoryDatabase)
    collection.AddScoped(NewUserRepository)
    collection.AddScoped(NewUserService)

    provider, _ := collection.BuildServiceProvider()
    // Test with real components
}

// E2E test - full application
func TestApplication_E2E(t *testing.T) {
    app := NewApplication() // Full DI setup
    // Test complete workflows
}
```

## Performance Considerations

### Service Resolution Performance

```go
// Cache frequently resolved services
type PerformantHandler struct {
    userService  UserService  // Resolved once
    orderService OrderService // Resolved once
}

// Instead of resolving in each method
func (h *PerformantHandler) Handle(ctx context.Context) {
    // Use h.userService directly
}
```

### Scope Management

```go
// ✅ Good - One scope per operation
func ProcessBatch(provider godi.ServiceProvider, items []Item) {
    for _, item := range items {
        scope := provider.CreateScope(context.Background())
        processItem(scope, item)
        scope.Close()
    }
}

// ❌ Bad - Scope keeps growing
func ProcessBatchBad(provider godi.ServiceProvider, items []Item) {
    scope := provider.CreateScope(context.Background())
    defer scope.Close()

    for _, item := range items {
        processItem(scope, item) // Accumulates instances
    }
}
```

## Scalability Patterns

### Modular Monolith

```go
// Each module is independent
var UserModule = godi.Module("user", /*...*/)
var OrderModule = godi.Module("order", /*...*/)
var InventoryModule = godi.Module("inventory", /*...*/)

// Can be split into microservices later
var MonolithModule = godi.Module("monolith",
    godi.AddModule(SharedModule),
    godi.AddModule(UserModule),
    godi.AddModule(OrderModule),
    godi.AddModule(InventoryModule),
)
```

### Microservices Ready

```go
// Shared interfaces package
package contracts

type UserService interface {
    GetUser(ctx context.Context, id string) (*User, error)
}

// Local implementation
type localUserService struct {
    repo UserRepository
}

// Remote implementation (after split)
type remoteUserService struct {
    client UserServiceClient
}

// Easy to swap implementations
if config.UseRemoteService {
    collection.AddSingleton(NewRemoteUserService)
} else {
    collection.AddSingleton(NewLocalUserService)
}
```

## Best Practices Summary

1. **Layer Dependencies**: Higher layers depend on lower layers
2. **Depend on Interfaces**: Not concrete implementations
3. **Module Boundaries**: Clear, well-defined modules
4. **Lifetime Appropriateness**: Right lifetime for each service
5. **Testability First**: Design for testing from the start
6. **Performance Aware**: Consider resolution and scope costs
7. **Scalability Ready**: Design for future growth

## Conclusion

Good architecture with dependency injection:

- Makes dependencies explicit
- Enables testing at all levels
- Supports multiple architectural patterns
- Scales from monoliths to microservices
- Maintains clean separation of concerns

godi provides the foundation for building well-architected Go applications that are maintainable, testable, and scalable.
