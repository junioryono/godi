# Core Concepts

Let's understand godi's core concepts through real examples. No fluff, just what you need to know.

## Services: Your Application Building Blocks

A **service** is any type that does something useful in your app:

```go
// This is a service - it sends emails
type EmailService struct {
    smtp SMTPClient
}

// This is also a service - it logs things
type Logger interface {
    Log(message string)
}

// Services can be interfaces or structs - your choice!
```

## Constructors: How Services Are Created

A **constructor** is just a function that creates your service:

```go
// Constructor for EmailService
func NewEmailService(smtp SMTPClient) *EmailService {
    return &EmailService{smtp: smtp}
}

// Constructor for Logger
func NewLogger(config *Config) Logger {
    return &FileLogger{
        path: config.LogPath,
    }
}

// godi calls these functions and provides the parameters automatically!
```

## The Container: Where Everything Comes Together

Think of godi as having two main parts:

### 1. ServiceCollection - The Recipe Book

This is where you tell godi about your services:

```go
services := godi.NewServiceCollection()

// Tell godi about your services
services.AddSingleton(NewLogger)      // "Here's how to make a Logger"
services.AddScoped(NewEmailService)   // "Here's how to make an EmailService"
```

### 2. ServiceProvider - The Kitchen

This is what actually creates and manages your services:

```go
// Build the provider from your collection
provider, _ := services.BuildServiceProvider()

// Now you can get your services
logger, _ := godi.Resolve[Logger](provider)
emailService, _ := godi.Resolve[*EmailService](provider)
```

## Service Lifetimes: When Things Are Created

This is the most important concept in godi. There are two lifetimes:

### Singleton - One for the Whole App

```go
services.AddSingleton(NewLogger)

// Created once, reused everywhere
logger1, _ := godi.Resolve[Logger](provider)  // Creates new logger
logger2, _ := godi.Resolve[Logger](provider)  // Returns SAME logger
// logger1 == logger2
```

**Use for:** Database connections, loggers, configuration, caches

### Scoped - One per "Operation"

```go
services.AddScoped(NewShoppingCart)

// In web apps, typically one per HTTP request
scope1 := provider.CreateScope(ctx)
cart1, _ := godi.Resolve[*ShoppingCart](scope1.ServiceProvider())

scope2 := provider.CreateScope(ctx)
cart2, _ := godi.Resolve[*ShoppingCart](scope2.ServiceProvider())
// cart1 != cart2 (different scopes = different instances)
```

**Use for:** Database transactions, request context, user sessions

## Scopes: Isolation for Operations

A **scope** creates a boundary for scoped services. Think of it as a "bubble":

```go
// Web request example
func HandleRequest(provider godi.ServiceProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Create scope for this request
        scope := provider.CreateScope(r.Context())
        defer scope.Close()

        // Everything in this scope shares the same instances
        repo, _ := godi.Resolve[*UserRepository](scope.ServiceProvider())
        service, _ := godi.Resolve[*UserService](scope.ServiceProvider())

        // Both repo and service share the same transaction!
    }
}
```

## Real Example: Putting It All Together

Let's see how these concepts work in a real app:

```go
package main

import (
    "context"
    "database/sql"
    "fmt"
    "github.com/junioryono/godi"
)

// 1. Define your services
type Logger interface {
    Log(msg string)
}

type Database struct {
    conn *sql.DB
}

type Transaction struct {
    tx *sql.Tx
}

type UserRepository struct {
    tx *Transaction
}

type EmailService struct {
    logger Logger
}

type UserService struct {
    repo  *UserRepository
    email *EmailService
    tx    *Transaction
}

// 2. Create constructors
func NewLogger() Logger {
    return &ConsoleLogger{}
}

func NewDatabase() *Database {
    conn, _ := sql.Open("sqlite3", ":memory:")
    return &Database{conn: conn}
}

func NewTransaction(db *Database) *Transaction {
    tx, _ := db.conn.Begin()
    return &Transaction{tx: tx}
}

func NewUserRepository(tx *Transaction) *UserRepository {
    return &UserRepository{tx: tx}
}

func NewEmailService(logger Logger) *EmailService {
    return &EmailService{logger: logger}
}

func NewUserService(repo *UserRepository, email *EmailService, tx *Transaction) *UserService {
    return &UserService{repo: repo, email: email, tx: tx}
}

// 3. Wire everything up
func main() {
    // Configure services
    services := godi.NewServiceCollection()

    // Singletons - shared across app
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewDatabase)
    services.AddSingleton(NewEmailService)

    // Scoped - per operation
    services.AddScoped(NewTransaction)
    services.AddScoped(NewUserRepository)
    services.AddScoped(NewUserService)

    // Build container
    provider, _ := services.BuildServiceProvider()
    defer provider.Close()

    // Simulate handling a request
    handleUserCreation(provider)
}

func handleUserCreation(provider godi.ServiceProvider) {
    // Create scope for this operation
    scope := provider.CreateScope(context.Background())
    defer scope.Close() // Transaction rollback if not committed

    // Get user service - everything is wired automatically!
    userService, _ := godi.Resolve[*UserService](scope.ServiceProvider())

    // Use it
    userService.CreateUser("john@example.com")
    userService.tx.Commit() // Explicit commit
}
```

## The Magic of Dependency Injection

Notice what we DIDN'T have to do:

- ❌ Manually create each service in the right order
- ❌ Pass dependencies through multiple layers
- ❌ Worry about cleanup/disposal
- ❌ Handle transaction passing

godi handled all of that for us!

## Quick Reference

| Concept               | What It Is                      | When to Use            |
| --------------------- | ------------------------------- | ---------------------- |
| **Service**           | Any type that does work         | Everything in your app |
| **Constructor**       | Function that creates a service | One per service type   |
| **ServiceCollection** | Where you register services     | Once at startup        |
| **ServiceProvider**   | What creates service instances  | Throughout your app    |
| **Singleton**         | One instance forever            | Shared resources       |
| **Scoped**            | One instance per scope          | Request-specific data  |
| **Scope**             | Boundary for scoped services    | Per request/operation  |

## Next Steps

Now that you understand the basics:

1. Try the [Getting Started Tutorial](../tutorials/getting-started.md)
2. Learn about [Scoped Services in Detail](../tutorials/scoped-services-explained.md)
3. Understand [When to Use Modules](../tutorials/simple-vs-modules.md)

Remember: Start simple, add complexity only when needed!
