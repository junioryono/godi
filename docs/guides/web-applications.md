# Building Web Applications

This guide covers patterns for building production web applications with godi.

## Application Structure

A typical godi web application:

```
myapp/
├── main.go                 # Application entry point
├── internal/
│   ├── config/             # Configuration loading
│   ├── database/           # Database connection
│   ├── middleware/         # HTTP middleware
│   ├── handlers/           # HTTP handlers
│   ├── services/           # Business logic
│   └── repositories/       # Data access
└── go.mod
```

## Complete Example

Here's a production-ready setup:

```go
package main

import (
    "context"
    "database/sql"
    "encoding/json"
    "log"
    "log/slog"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/junioryono/godi/v4"
    godihttp "github.com/junioryono/godi/v4/http"
    _ "github.com/lib/pq"
)

// === Configuration ===

type Config struct {
    DatabaseURL string
    ServerAddr  string
    Debug       bool
}

func NewConfig() *Config {
    return &Config{
        DatabaseURL: getEnv("DATABASE_URL", "postgres://localhost/myapp?sslmode=disable"),
        ServerAddr:  getEnv("SERVER_ADDR", ":8080"),
        Debug:       getEnv("DEBUG", "false") == "true",
    }
}

func getEnv(key, fallback string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return fallback
}

// === Logger ===

type Logger struct {
    *slog.Logger
}

func NewLogger(cfg *Config) *Logger {
    level := slog.LevelInfo
    if cfg.Debug {
        level = slog.LevelDebug
    }
    return &Logger{
        Logger: slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})),
    }
}

// === Database ===

type Database struct {
    *sql.DB
}

func NewDatabase(cfg *Config) (*Database, error) {
    db, err := sql.Open("postgres", cfg.DatabaseURL)
    if err != nil {
        return nil, err
    }

    db.SetMaxOpenConns(25)
    db.SetMaxIdleConns(5)
    db.SetConnMaxLifetime(5 * time.Minute)

    if err := db.Ping(); err != nil {
        return nil, err
    }

    return &Database{DB: db}, nil
}

func (d *Database) Close() error {
    return d.DB.Close()
}

// === Request Context ===

type RequestContext struct {
    ID        string
    StartTime time.Time
    UserID    string
}

func NewRequestContext() *RequestContext {
    return &RequestContext{
        ID:        generateID(),
        StartTime: time.Now(),
    }
}

func generateID() string {
    return time.Now().Format("20060102150405.000000")
}

// === User Repository ===

type User struct {
    ID    int    `json:"id"`
    Name  string `json:"name"`
    Email string `json:"email"`
}

type UserRepository struct {
    db  *Database
    log *Logger
}

func NewUserRepository(db *Database, log *Logger) *UserRepository {
    return &UserRepository{db: db, log: log}
}

func (r *UserRepository) GetByID(ctx context.Context, id int) (*User, error) {
    r.log.Debug("fetching user", "id", id)
    var u User
    err := r.db.QueryRowContext(ctx,
        "SELECT id, name, email FROM users WHERE id = $1", id,
    ).Scan(&u.ID, &u.Name, &u.Email)
    if err != nil {
        return nil, err
    }
    return &u, nil
}

func (r *UserRepository) List(ctx context.Context) ([]User, error) {
    rows, err := r.db.QueryContext(ctx, "SELECT id, name, email FROM users LIMIT 100")
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    var users []User
    for rows.Next() {
        var u User
        if err := rows.Scan(&u.ID, &u.Name, &u.Email); err != nil {
            return nil, err
        }
        users = append(users, u)
    }
    return users, rows.Err()
}

// === User Service ===

type UserService struct {
    repo   *UserRepository
    reqCtx *RequestContext
    log    *Logger
}

func NewUserService(repo *UserRepository, reqCtx *RequestContext, log *Logger) *UserService {
    return &UserService{repo: repo, reqCtx: reqCtx, log: log}
}

func (s *UserService) GetUser(ctx context.Context, id int) (*User, error) {
    s.log.Info("getting user",
        "request_id", s.reqCtx.ID,
        "user_id", id,
    )
    return s.repo.GetByID(ctx, id)
}

func (s *UserService) ListUsers(ctx context.Context) ([]User, error) {
    s.log.Info("listing users", "request_id", s.reqCtx.ID)
    return s.repo.List(ctx)
}

// === User Controller ===

type UserController struct {
    service *UserService
    reqCtx  *RequestContext
}

func NewUserController(service *UserService, reqCtx *RequestContext) *UserController {
    return &UserController{service: service, reqCtx: reqCtx}
}

func (c *UserController) List(w http.ResponseWriter, r *http.Request) {
    users, err := c.service.ListUsers(r.Context())
    if err != nil {
        http.Error(w, "Failed to fetch users", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.Header().Set("X-Request-ID", c.reqCtx.ID)
    json.NewEncoder(w).Encode(users)
}

// === Main ===

func main() {
    // Register services
    services := godi.NewCollection()

    // Singletons - shared infrastructure
    services.AddSingleton(NewConfig)
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewDatabase)

    // Scoped - per-request
    services.AddScoped(NewRequestContext)
    services.AddScoped(NewUserRepository)
    services.AddScoped(NewUserService)
    services.AddScoped(NewUserController)

    // Build provider
    provider, err := services.Build()
    if err != nil {
        log.Fatalf("Failed to build provider: %v", err)
    }
    defer provider.Close()

    // Get config and logger
    cfg := godi.MustResolve[*Config](provider)
    logger := godi.MustResolve[*Logger](provider)

    // Setup routes
    mux := http.NewServeMux()
    mux.HandleFunc("GET /users", godihttp.Handle((*UserController).List))
    mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
        w.Write([]byte("OK"))
    })

    // Wrap with middleware
    handler := godihttp.ScopeMiddleware(provider,
        godihttp.WithMiddleware(func(scope godi.Scope, r *http.Request) error {
            // Add request ID to context for logging
            reqCtx := godi.MustResolve[*RequestContext](scope)
            logger.Debug("request started",
                "request_id", reqCtx.ID,
                "method", r.Method,
                "path", r.URL.Path,
            )
            return nil
        }),
    )(mux)

    // Create server
    server := &http.Server{
        Addr:         cfg.ServerAddr,
        Handler:      handler,
        ReadTimeout:  15 * time.Second,
        WriteTimeout: 15 * time.Second,
        IdleTimeout:  60 * time.Second,
    }

    // Graceful shutdown
    go func() {
        sigChan := make(chan os.Signal, 1)
        signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
        <-sigChan

        logger.Info("shutting down server")
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        server.Shutdown(ctx)
    }()

    // Start server
    logger.Info("server starting", "addr", cfg.ServerAddr)
    if err := server.ListenAndServe(); err != http.ErrServerClosed {
        log.Fatalf("Server error: %v", err)
    }
}
```

## Key Patterns

### 1. Layered Architecture

```
┌─────────────────────────────────────────────┐
│  Controllers (HTTP handlers)                 │
│  - Parse requests                            │
│  - Call services                             │
│  - Return responses                          │
├─────────────────────────────────────────────┤
│  Services (Business logic)                   │
│  - Orchestrate operations                    │
│  - Apply business rules                      │
│  - Coordinate repositories                   │
├─────────────────────────────────────────────┤
│  Repositories (Data access)                  │
│  - Database queries                          │
│  - Cache operations                          │
│  - External API calls                        │
├─────────────────────────────────────────────┤
│  Infrastructure (Shared resources)           │
│  - Database connections                      │
│  - Logger                                    │
│  - Configuration                             │
└─────────────────────────────────────────────┘
```

### 2. Lifetime Assignments

```go
// Singleton: Infrastructure
services.AddSingleton(NewConfig)
services.AddSingleton(NewLogger)
services.AddSingleton(NewDatabase)
services.AddSingleton(NewHTTPClient)

// Scoped: Per-request state and services
services.AddScoped(NewRequestContext)
services.AddScoped(NewUserRepository)
services.AddScoped(NewUserService)
services.AddScoped(NewUserController)

// Transient: Utilities (rarely needed)
services.AddTransient(NewQueryBuilder)
```

### 3. Request Context Pattern

Share request-specific data across services:

```go
type RequestContext struct {
    ID        string
    UserID    string
    StartTime time.Time
    Logger    *slog.Logger
}

func NewRequestContext(logger *Logger) *RequestContext {
    id := uuid.New().String()
    return &RequestContext{
        ID:        id,
        StartTime: time.Now(),
        Logger:    logger.With("request_id", id),
    }
}
```

### 4. Middleware Integration

Set request data before handlers run:

```go
handler := godihttp.ScopeMiddleware(provider,
    // Logging middleware
    godihttp.WithMiddleware(func(scope godi.Scope, r *http.Request) error {
        reqCtx := godi.MustResolve[*RequestContext](scope)
        reqCtx.Logger.Info("request started",
            "method", r.Method,
            "path", r.URL.Path,
        )
        return nil
    }),
    // Auth middleware
    godihttp.WithMiddleware(func(scope godi.Scope, r *http.Request) error {
        reqCtx := godi.MustResolve[*RequestContext](scope)
        if userID := r.Header.Get("X-User-ID"); userID != "" {
            reqCtx.UserID = userID
        }
        return nil
    }),
)(mux)
```

### 5. Database Transaction Per Request

```go
type Transaction struct {
    tx *sql.Tx
}

func NewTransaction(db *Database) (*Transaction, error) {
    tx, err := db.Begin()
    if err != nil {
        return nil, err
    }
    return &Transaction{tx: tx}, nil
}

func (t *Transaction) Close() error {
    return t.tx.Commit()
}

// All repositories use the same transaction
type UserRepository struct {
    tx *Transaction
}

type OrderRepository struct {
    tx *Transaction  // Same transaction!
}
```

## Graceful Shutdown

Clean shutdown in the right order:

```go
// 1. Stop accepting new requests
server.Shutdown(ctx)

// 2. Wait for in-flight requests to complete
// (handled by Shutdown)

// 3. Close provider (closes database, etc.)
provider.Close()
```

---

**Next:** Learn about [testing with godi](testing.md)
