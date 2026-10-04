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
│   scope, _ := provider.CreateScope(ctx)                         │
│   godi.MustResolve[*UserService](scope)                         │
│                                                                 │
│   1. Logger and Database already exist (singletons are          │
│      created eagerly during Build, in dependency order)         │
│   2. Create UserService(Database, Logger), cached in the scope  │
│   3. Return UserService                                         │
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
3. **Validates** - Checks for circular dependencies and lifetime conflicts
4. **Creates singletons** - All singletons are constructed eagerly, in dependency order
5. **Returns provider** - Scoped and transient services are created on demand

```go
provider, err := services.Build()
if err != nil {
    // Something's wrong with your registrations
    log.Fatal(err)
}
```

### 4. You Request Services

When you resolve a service, godi walks the dependency graph. `UserService` is
scoped, so resolve it from a scope (in a web app, one scope per request):

```go
scope, err := provider.CreateScope(ctx)
if err != nil {
    log.Fatal(err)
}
defer scope.Close()

userService := godi.MustResolve[*UserService](scope)
```

The singletons (Logger, Database) were already created during `Build()` in
dependency order — Logger first (no dependencies), then Database (needs
Logger). Resolving UserService creates it from those cached instances and
caches it in the scope until the scope is closed.

Resolving a scoped service directly from the provider uses the provider's
root scope, so the instance lives until the provider is closed; resolve
scoped services from a scope you create.

## Type Resolution

godi uses Go generics for type-safe resolution:

```go
// The type in brackets must match what you registered
logger := godi.MustResolve[*Logger](provider)
db := godi.MustResolve[*Database](provider)
users := godi.MustResolve[*UserService](scope)
```

If you request a type that wasn't registered, you get an error
(`MustResolve` panics with it; `Resolve` returns it):

```go
// service not found: *main.NotRegistered
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
// build failed during validation phase: dependency graph validation failed: circular dependency detected: *main.A -> *main.B -> *main.A
// godi.Explain(err) draws the cycle and suggests fixes.
```

**Missing Dependencies**

```go
services.AddSingleton(func(missing *NotRegistered) *MyService {
    return &MyService{}
})

provider, err := services.Build()
// build failed during validation phase: missing dependencies: *main.MyService requires *main.NotRegistered (not registered) [constructor main.main.func1 (main.go:9)]
// Checked at Build for singleton, scoped, and transient services alike.
```

**Lifetime Conflicts**

```go
services.AddScoped(NewRequestContext)
services.AddSingleton(func(ctx *RequestContext) *Cache {
    return &Cache{}
})

provider, err := services.Build()
// build failed during validation phase: lifetime validation failed: lifetime conflict: *main.Cache (Singleton) cannot depend on *main.RequestContext (Scoped)
// godi.Explain(err) explains the conflict and suggests fixes.
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
