# How godi Works

godi is a dependency injection container that automatically resolves and creates your services. Here's the mental model.

## The Big Picture

```
┌─────────────────────────────────────────────────────────────────┐
│                         Your Code                               │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│   type Logger struct{}                                          │
│   type Database struct { logger *Logger }                       │
│   type UserService struct { db *Database, logger *Logger }      │
│                                                                 │
└──────────────────────────────┬──────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Service Collection                         │
│  "Here's what I have"                                           │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│   AddSingleton(NewLogger)                                       │
│   AddSingleton(NewDatabase)                                     │
│   AddScoped(NewUserService)                                     │
│                                                                 │
└──────────────────────────────┬──────────────────────────────────┘
                               │ Build()
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                      Dependency Graph                           │
│  "Here's how things connect"                                    │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│   UserService ──────┬──────▶ Database ──────▶ Logger            │
│                     │                                           │
│                     └──────────────────────▶ Logger             │
│                                                                 │
└──────────────────────────────┬──────────────────────────────────┘
                               │
                               ▼
┌─────────────────────────────────────────────────────────────────┐
│                         Provider                                │
│  "Ask me for anything"                                          │
├─────────────────────────────────────────────────────────────────┤
│                                                                 │
│   godi.MustResolve[*UserService](provider)                      │
│                                                                 │
│   1. Check if Logger exists → No → Create Logger                │
│   2. Check if Database exists → No → Create Database(Logger)    │
│   3. Create UserService(Database, Logger)                       │
│   4. Return UserService                                         │
│                                                                 │
└─────────────────────────────────────────────────────────────────┘
```

## Step by Step

### 1. You Write Constructors

Normal Go functions that create your types:

```go
func NewLogger() *Logger {
    return &Logger{}
}

func NewDatabase(logger *Logger) *Database {
    return &Database{logger: logger}
}

func NewUserService(db *Database, logger *Logger) *UserService {
    return &UserService{db: db, logger: logger}
}
```

godi reads the function signatures to understand dependencies.

### 2. You Register Services

Tell godi about your constructors:

```go
services := godi.NewCollection()
services.AddSingleton(NewLogger)
services.AddSingleton(NewDatabase)
services.AddScoped(NewUserService)
```

Registration order doesn't matter. godi will figure out the correct creation order.

### 3. godi Builds the Graph

When you call `Build()`, godi:

1. **Analyzes constructors** - Looks at function parameters and return types
2. **Builds dependency graph** - Maps what depends on what
3. **Validates** - Checks for circular dependencies, missing services, lifetime conflicts
4. **Returns provider** - Ready to create services on demand

```go
provider, err := services.Build()
if err != nil {
    // Something's wrong with your registrations
    log.Fatal(err)
}
```

### 4. You Request Services

When you resolve a service, godi walks the dependency graph:

```go
userService := godi.MustResolve[*UserService](provider)
```

godi creates dependencies in order:

1. Logger first (no dependencies)
2. Database next (needs Logger)
3. UserService last (needs Database and Logger)

## Type Resolution

godi uses Go generics for type-safe resolution:

```go
// The type in brackets must match what you registered
logger := godi.MustResolve[*Logger](provider)
db := godi.MustResolve[*Database](provider)
users := godi.MustResolve[*UserService](provider)
```

If you request a type that wasn't registered, you get an error:

```go
// Error: no registration found for type *NotRegistered
thing := godi.MustResolve[*NotRegistered](provider)
```

## Instance Caching

godi caches instances based on lifetime:

```go
// Singleton: same instance every time
logger1 := godi.MustResolve[*Logger](provider)
logger2 := godi.MustResolve[*Logger](provider)
// logger1 == logger2 ✓

// Transient: new instance every time
services.AddTransient(NewTempFile)
file1 := godi.MustResolve[*TempFile](provider)
file2 := godi.MustResolve[*TempFile](provider)
// file1 == file2 ✗
```

## Error Handling

godi validates at build time to catch problems early:

**Circular Dependencies**

```go
// A needs B, B needs A
services.AddSingleton(func(b *B) *A { return &A{} })
services.AddSingleton(func(a *A) *B { return &B{} })

provider, err := services.Build()
// Error: circular dependency detected: *A -> *B -> *A
```

**Missing Dependencies**

```go
services.AddSingleton(func(missing *NotRegistered) *MyService {
    return &MyService{}
})

provider, err := services.Build()
// Error: no registration found for type *NotRegistered required by *MyService
```

**Lifetime Conflicts**

```go
services.AddScoped(NewRequestContext)
services.AddSingleton(func(ctx *RequestContext) *Cache {
    return &Cache{}
})

provider, err := services.Build()
// Error: singleton *Cache cannot depend on scoped *RequestContext
```

## Cleanup

Services implementing `Close() error` are automatically cleaned up:

```go
type Database struct {
    conn *sql.DB
}

func (d *Database) Close() error {
    return d.conn.Close()
}

// When you close the provider, Database.Close() is called
provider.Close()
```

---

**Next:** Learn about [service lifetimes](lifetimes.md)
