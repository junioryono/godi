# Core Concepts

Understanding these 5 concepts is all you need to master godi.

## 1. Services

A **service** is any type that does work in your app. No special interfaces needed.

```go
// This is a service
type EmailSender struct {
    apiKey string
}

func NewEmailSender() *EmailSender {
    return &EmailSender{apiKey: "secret"}
}

// This is also a service
type UserRepository struct {
    db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
    return &UserRepository{db: db}
}
```

**Key Point**: If it does work, it's a service. That's it.

## 2. Constructors

A **constructor** is a function that creates a service. godi calls these for you.

```go
// Simple constructor - no dependencies
func NewLogger() *Logger {
    return &Logger{}
}

// Constructor with dependencies - godi provides them!
func NewUserService(db *Database, logger *Logger) *UserService {
    return &UserService{
        db:     db,
        logger: logger,
    }
}
```

**The Magic**: When you ask for `UserService`, godi:

1. Sees it needs `Database` and `Logger`
2. Creates those first (if needed)
3. Passes them to `NewUserService`
4. Returns your ready-to-use service

You never manually wire dependencies!

## 3. Lifetimes

A **lifetime** controls when instances are created and how long they live.

### Singleton - One Forever

Created once, reused everywhere. Perfect for shared resources.

```go
collection.AddSingleton(NewDatabase)

// Everyone gets the same instance
db1, _ := godi.Resolve[*Database](provider)
db2, _ := godi.Resolve[*Database](provider)
// db1 == db2 (same instance)
```

**Use for**: Database connections, loggers, configuration, caches

### Scoped - One Per Request

New instance for each scope. Perfect for web requests.

```go
collection.AddScoped(NewShoppingCart)

// Different scopes get different instances
scope1, _ := provider.CreateScope(ctx)
cart1, _ := godi.Resolve[*ShoppingCart](scope1)

scope2, _ := provider.CreateScope(ctx)
cart2, _ := godi.Resolve[*ShoppingCart](scope2)
// cart1 != cart2 (different instances)
```

**Use for**: Request handlers, transactions, user context

### Transient - Always New

New instance every time. Never cached.

```go
collection.AddTransient(NewGuid)

id1, _ := godi.Resolve[*Guid](provider)
id2, _ := godi.Resolve[*Guid](provider)
// id1 != id2 (always different)
```

**Use for**: Unique IDs, temporary objects

### Quick Reference

| Lifetime  | When Created    | Use For           | Example                   |
| --------- | --------------- | ----------------- | ------------------------- |
| Singleton | Once at startup | Shared resources  | Database, Logger          |
| Scoped    | Once per scope  | Request data      | Transaction, User context |
| Transient | Every time      | Temporary objects | IDs, Builders             |

## 4. Modules

**Modules** group related services together. Think of them as packages of functionality.

```go
// Group database-related services
var DataModule = godi.NewModule("data",
    godi.AddSingleton(NewDatabase),
    godi.AddScoped(NewTransaction),
    godi.AddScoped(NewUserRepository),
)

// Group authentication services
var AuthModule = godi.NewModule("auth",
    DataModule,  // Can depend on other modules!
    godi.AddSingleton(NewTokenService),
    godi.AddScoped(NewAuthService),
)

// Your app module combines everything
var AppModule = godi.NewModule("app",
    DataModule,
    AuthModule,
)
```

**Why use modules?**

- **Organization**: Keep related things together
- **Reusability**: Share modules between projects
- **Testing**: Easy to swap modules for testing
- **Clarity**: Dependencies are explicit

## 5. Scopes

A **scope** creates a boundary for scoped services. Essential for web applications!

```go
func handleRequest(provider godi.Provider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Create scope for this request
        scope, _ := provider.CreateScope(r.Context())
        defer scope.Close()  // Clean up when done

        // All scoped services in this request share instances
        userService, _ := godi.Resolve[*UserService](scope)
        cartService, _ := godi.Resolve[*CartService](scope)

        // They might share the same transaction!
    }
}
```

**Real Example**: Database Transaction per Request

```go
// Transaction is scoped - one per request
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

// In a request:
// 1. Scope is created
// 2. Transaction is created (once for this scope)
// 3. All repositories use the SAME transaction
// 4. Scope closes, transaction commits/rollbacks
```

## Putting It All Together

Here's how these concepts work together:

```go
// 1. Define services (with constructors)
type Logger struct{}
func NewLogger() *Logger { return &Logger{} }

type Database struct{ logger *Logger }
func NewDatabase(logger *Logger) *Database {
    return &Database{logger: logger}
}

type UserService struct{ db *Database }
func NewUserService(db *Database) *UserService {
    return &UserService{db: db}
}

// 2. Create module (organizes services with lifetimes)
var AppModule = godi.NewModule("app",
    godi.AddSingleton(NewLogger),      // One logger
    godi.AddSingleton(NewDatabase),    // One database
    godi.AddScoped(NewUserService),    // New per request
)

// 3. Build provider
collection := godi.NewCollection()
collection.AddModules(AppModule)
provider, _ := collection.Build()

// 4. Use with scopes (for requests)
scope, _ := provider.CreateScope(context.Background())
defer scope.Close()

service, _ := godi.Resolve[*UserService](scope)
// godi automatically creates: Logger → Database → UserService
```

## Common Patterns

### Pattern 1: Shared Infrastructure, Request-Specific Logic

```go
var AppModule = godi.NewModule("app",
    // Shared across all requests
    godi.AddSingleton(NewLogger),
    godi.AddSingleton(NewDatabase),
    godi.AddSingleton(NewCache),

    // New for each request
    godi.AddScoped(NewTransaction),
    godi.AddScoped(NewUserContext),
    godi.AddScoped(NewRequestHandler),
)
```

### Pattern 2: Test Modules

```go
var TestModule = godi.NewModule("test",
    godi.AddSingleton(func() *Database {
        return &MockDatabase{}  // Mock for testing
    }),
    godi.AddScoped(NewUserService),  // Real service, mock database
)
```

### Pattern 3: Environment-Specific Modules

```go
var DevModule = godi.NewModule("dev",
    godi.AddSingleton(func() *Database {
        return NewSQLite(":memory:")
    }),
)

var ProdModule = godi.NewModule("prod",
    godi.AddSingleton(func() *Database {
        return NewPostgres(os.Getenv("DATABASE_URL"))
    }),
)
```

## Quick Decision Guide

**Which lifetime should I use?**

- Stateless + thread-safe → Singleton
- Request-specific data → Scoped
- Must be unique → Transient

**Should I use modules?**

- Yes, always! Even for small apps. They keep things organized.

**Do I need scopes?**

- Building a web app? → Yes
- Building a CLI tool? → Probably not
- Running tests? → Sometimes (for isolation)

## Summary

That's all you need to know:

1. **Services** - Your types that do work
2. **Constructors** - Functions that create services
3. **Lifetimes** - When instances are created (Singleton/Scoped/Transient)
4. **Modules** - Groups of related services
5. **Scopes** - Boundaries for scoped services (for web requests)

godi handles the rest. No magic, just smart dependency management!
