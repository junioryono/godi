# Adding Services

Real applications have services that depend on each other. godi automatically wires these dependencies together.

## The Magic: Automatic Wiring

Write your constructors normally. godi figures out what to pass in.

```go
// Logger has no dependencies
type Logger struct{}

func NewLogger() *Logger {
    return &Logger{}
}

// UserService depends on Logger
type UserService struct {
    logger *Logger
}

func NewUserService(logger *Logger) *UserService {
    return &UserService{logger: logger}
}
```

Register both:

```go
services := godi.NewCollection()
services.AddSingleton(NewLogger)
services.AddSingleton(NewUserService)
```

Resolve:

```go
users := godi.MustResolve[*UserService](provider)
// users.logger is already set!
```

godi saw that `NewUserService` needs a `*Logger`, found `NewLogger`, and called it first.

## How It Works

```
┌────────────────────────────────────────────────────────┐
│  You register:                                         │
│    NewLogger()      → *Logger                          │
│    NewUserService() → *UserService (needs *Logger)     │
├────────────────────────────────────────────────────────┤
│  godi builds dependency graph:                         │
│                                                        │
│    *UserService                                        │
│         │                                              │
│         └──depends on──▶ *Logger                       │
├────────────────────────────────────────────────────────┤
│  When you resolve *UserService:                        │
│    1. Create *Logger (no deps)                         │
│    2. Create *UserService (pass *Logger)               │
│    3. Return *UserService                              │
└────────────────────────────────────────────────────────┘
```

## A Realistic Example

```go
package main

import (
    "fmt"
    "log"
    "github.com/junioryono/godi/v4"
)

// Logger - no dependencies
type Logger struct {
    prefix string
}

func NewLogger() *Logger {
    return &Logger{prefix: "[APP]"}
}

func (l *Logger) Log(msg string) {
    fmt.Printf("%s %s\n", l.prefix, msg)
}

// Config - no dependencies
type Config struct {
    DatabaseURL string
    Debug       bool
}

func NewConfig() *Config {
    return &Config{
        DatabaseURL: "postgres://localhost/myapp",
        Debug:       true,
    }
}

// Database - depends on Config and Logger
type Database struct {
    config *Config
    logger *Logger
}

func NewDatabase(config *Config, logger *Logger) *Database {
    logger.Log("Connecting to database...")
    return &Database{config: config, logger: logger}
}

func (d *Database) Query(sql string) {
    d.logger.Log("Executing: " + sql)
}

// UserService - depends on Database and Logger
type UserService struct {
    db     *Database
    logger *Logger
}

func NewUserService(db *Database, logger *Logger) *UserService {
    return &UserService{db: db, logger: logger}
}

func (u *UserService) GetUser(id int) {
    u.logger.Log(fmt.Sprintf("Getting user %d", id))
    u.db.Query(fmt.Sprintf("SELECT * FROM users WHERE id = %d", id))
}

func main() {
    services := godi.NewCollection()

    // Register in any order - godi figures out the dependency order
    services.AddSingleton(NewUserService)
    services.AddSingleton(NewDatabase)
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewConfig)

    provider, err := services.Build()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // Everything is wired up automatically
    users := godi.MustResolve[*UserService](provider)
    users.GetUser(42)
}
```

Output:

```
[APP] Connecting to database...
[APP] Getting user 42
[APP] Executing: SELECT * FROM users WHERE id = 42
```

## Constructor Patterns

godi supports several constructor patterns:

```go
// Simple constructor
func NewLogger() *Logger

// With dependencies
func NewUserService(logger *Logger, db *Database) *UserService

// With error return
func NewDatabase(config *Config) (*Database, error)

// Anonymous function
services.AddSingleton(func(logger *Logger) *Cache {
    return &Cache{logger: logger}
})
```

## Interface Registration

Register a concrete type to satisfy an interface:

```go
type Logger interface {
    Log(string)
}

type consoleLogger struct{}
func (c *consoleLogger) Log(msg string) { fmt.Println(msg) }

// Register concrete type as interface
services.AddSingleton(func() *consoleLogger {
    return &consoleLogger{}
}, godi.As[Logger]())

// Resolve by interface
logger := godi.MustResolve[Logger](provider)
```

## Key Points

- Write normal Go constructors - godi handles the wiring
- Registration order doesn't matter
- Dependencies are resolved recursively
- Errors during construction are returned from `Build()` or `Resolve()`

---

**Next:** [Control instance creation with lifetimes](04-using-lifetimes.md)
