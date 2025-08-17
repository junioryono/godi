# Core Concepts

Understanding these 5 concepts is all you need to master godi.

## 1. Services

A **service** is any type that does something useful in your app.

```go
// This is a service
type EmailSender struct {
    apiKey string
}

func NewEmailSender() *EmailSender {
    return &EmailSender{apiKey: "secret"}
}

func (e *EmailSender) Send(to, subject, body string) error {
    // Send email...
    return nil
}
```

**Key Points:**

- Services are just regular Go types
- No special interfaces or base types needed
- If it does work, it's probably a service

## 2. Constructors

A **constructor** is a function that creates a service. godi calls these for you.

```go
// Simple constructor
func NewLogger() *Logger {
    return &Logger{}
}

// Constructor with dependencies
func NewEmailService(sender *EmailSender, logger *Logger) *EmailService {
    return &EmailService{
        sender: sender,
        logger: logger,
    }
}
```

**godi's Magic**: When you ask for EmailService, godi:

1. Sees it needs EmailSender and Logger
2. Creates those first (if needed)
3. Passes them to NewEmailService
4. Returns your ready-to-use service

## 3. Modules

A **module** groups related services together. Think of it as a package of functionality.

```go
// Group email-related services
var EmailModule = godi.NewModule("email",
    godi.AddSingleton(NewEmailSender),
    godi.AddScoped(NewEmailService),
    godi.AddScoped(NewEmailValidator),
)

// Group user-related services
var UserModule = godi.NewModule("user",
    EmailModule, // Modules can include other modules!
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewUserService),
)
```

**Why Modules?**

- **Organization**: Keep related things together
- **Reusability**: Use the same module in different apps
- **Clarity**: See dependencies at a glance

## 4. Lifetimes

A **lifetime** controls when and how instances are created.

### Singleton (One Forever)

```go
// Created once, reused everywhere
godi.AddSingleton(NewDatabase)

// Everyone gets the same database connection
db1, _ := godi.Resolve[*Database](provider)
db2, _ := godi.Resolve[*Database](provider)
// db1 == db2 (same instance)
```

**Use for:** Databases, loggers, caches, configuration

### Scoped (One Per Request)

```go
// New instance for each scope
godi.AddScoped(NewShoppingCart)

// Different requests get different carts
scope1, _ := provider.CreateScope(ctx)
cart1, _ := godi.Resolve[*ShoppingCart](scope1)

scope2, _ := provider.CreateScope(ctx)
cart2, _ := godi.Resolve[*ShoppingCart](scope2)
// cart1 != cart2 (different instances)
```

**Use for:** Request handlers, repositories, business logic

### Transient (New Every Time)

```go
// New instance every time
godi.AddTransient(NewRequestID)

// Every resolution creates a new instance
id1, _ := godi.Resolve[*RequestID](provider)
id2, _ := godi.Resolve[*RequestID](provider)
// id1 != id2 (always different instances)
```

**Use for:** Unique IDs, temporary objects, stateless operations

## 5. Scopes

A **scope** creates a boundary for scoped services. Perfect for web requests!

```go
func handleRequest(provider godi.Provider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Create scope for this request
        scope, err := provider.CreateScope(r.Context())
        if err != nil {
            http.Error(w, "Scope error", http.StatusInternalServerError)
            return
        }
        defer scope.Close() // Clean up when done

        // All scoped services in this request share the same instances
        userService, _ := godi.Resolve[*UserService](scope)
        cartService, _ := godi.Resolve[*CartService](scope)

        // Both services might share the same transaction!
    }
}
```

**Real Example**: Request with Database Transaction

```go
// Transaction service (scoped)
type Transaction struct {
    tx *sql.Tx
}

func NewTransaction(db *Database) *Transaction {
    tx, _ := db.Begin()
    return &Transaction{tx: tx}
}

// Repository uses the transaction
type UserRepository struct {
    tx *Transaction
}

func NewUserRepository(tx *Transaction) *UserRepository {
    return &UserRepository{tx: tx}
}

// In a request scope:
// 1. Transaction is created (once per scope)
// 2. All repositories in this scope use the SAME transaction
// 3. When scope closes, transaction commits/rollbacks
```

## Quick Reference

| Concept         | What It Is                      | When to Use            |
| --------------- | ------------------------------- | ---------------------- |
| **Service**     | Any type that does work         | Everything in your app |
| **Constructor** | Function that creates a service | One per service        |
| **Module**      | Group of related services       | Organize your app      |
| **Singleton**   | One instance forever            | Shared resources       |
| **Scoped**      | One instance per scope          | Request-specific       |
| **Transient**   | New instance every time         | Temporary objects      |
| **Scope**       | Boundary for scoped services    | Web requests           |

## Visual: How It All Works

```
1. Define Module
   EmailModule = [EmailSender(singleton), EmailService(scoped)]

2. Build Provider
   provider = Build(EmailModule)

3. Handle Request
   scope = provider.CreateScope()

4. Resolve Service
   service = Resolve[EmailService](scope)
   // godi creates: EmailSender (if needed) → EmailService

5. Clean Up
   scope.Close()
```

## Common Patterns

### Pattern 1: Shared Database, Scoped Transaction

```go
var DatabaseModule = godi.NewModule("db",
    godi.AddSingleton(NewDatabase),      // Shared connection
    godi.AddScoped(NewTransaction),      // Per-request transaction
    godi.AddScoped(NewUserRepository),   // Uses transaction
)
```

### Pattern 2: Request Context

```go
// Scoped service that holds request data
type RequestContext struct {
    UserID    string
    RequestID string
}

func NewRequestContext(ctx context.Context) *RequestContext {
    return &RequestContext{
        UserID:    ctx.Value("userID").(string),
        RequestID: ctx.Value("requestID").(string),
    }
}

// Other services can use it
func NewAuditLogger(ctx *RequestContext, logger *Logger) *AuditLogger {
    return &AuditLogger{ctx: ctx, logger: logger}
}
```

### Pattern 3: Module Composition

```go
// Small, focused modules
var LoggingModule = godi.NewModule("logging", ...)
var MetricsModule = godi.NewModule("metrics", ...)
var TracingModule = godi.NewModule("tracing", ...)

// Combine into larger module
var ObservabilityModule = godi.NewModule("observability",
    LoggingModule,
    MetricsModule,
    TracingModule,
)

// Use in your app
var AppModule = godi.NewModule("app",
    ObservabilityModule,
    DatabaseModule,
    BusinessModule,
)
```

## Next Steps

Now that you understand the concepts:

1. **[Quick Start](quick-start.md)** - See it all in action
2. **[Getting Started](getting-started.md)** - Build something real
3. **[Module Patterns](../howto/modules.md)** - Advanced techniques

Remember: Start simple, add complexity only when needed!
