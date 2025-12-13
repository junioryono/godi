# Resource Cleanup

Automatic disposal of resources when scopes and providers close.

## How It Works

Services implementing `Close() error` are automatically cleaned up:

```go
type Database struct {
    conn *sql.DB
}

func (d *Database) Close() error {
    return d.conn.Close()
}

services.AddSingleton(NewDatabase)

provider, _ := services.Build()
// ... use database ...

provider.Close()  // Database.Close() called automatically
```

## The Disposable Pattern

Any type with a `Close() error` method is disposable:

```go
// Automatically disposed
type FileHandler struct {
    file *os.File
}

func (f *FileHandler) Close() error {
    return f.file.Close()
}

// Also automatically disposed
type Connection struct {
    conn net.Conn
}

func (c *Connection) Close() error {
    return c.conn.Close()
}
```

## Disposal by Lifetime

### Singleton Disposal

Disposed when the provider closes:

```go
services.AddSingleton(NewDatabase)

provider, _ := services.Build()
db := godi.MustResolve[*Database](provider)
// ... use throughout app ...

provider.Close()  // Database.Close() called here
```

### Scoped Disposal

Disposed when the scope closes:

```go
services.AddScoped(NewTransaction)

scope, _ := provider.CreateScope(ctx)
tx := godi.MustResolve[*Transaction](scope)
// ... use transaction ...

scope.Close()  // Transaction.Close() called here
```

### Transient Disposal

Disposed when the scope they were created in closes:

```go
services.AddTransient(NewTempFile)

scope, _ := provider.CreateScope(ctx)
file1 := godi.MustResolve[*TempFile](scope)  // Created
file2 := godi.MustResolve[*TempFile](scope)  // Created
// Each resolution creates new instance

scope.Close()  // Both file1.Close() and file2.Close() called
```

## Disposal Order

Resources are disposed in reverse creation order:

```
Created:  Database → Cache → UserService
Disposed: UserService → Cache → Database
```

This ensures dependencies are still available during disposal.

## Error Handling

Disposal errors are collected but don't stop other disposals:

```go
// Custom close error handler
godihttp.ScopeMiddleware(provider,
    godihttp.WithCloseErrorHandler(func(err error) {
        log.Printf("Cleanup error: %v", err)
        // Still continues closing other resources
    }),
)
```

## Practical Examples

### Database Connection

```go
type Database struct {
    pool *sql.DB
}

func NewDatabase(config *Config) (*Database, error) {
    pool, err := sql.Open("postgres", config.DatabaseURL)
    if err != nil {
        return nil, err
    }

    pool.SetMaxOpenConns(25)
    pool.SetMaxIdleConns(5)

    return &Database{pool: pool}, nil
}

func (d *Database) Close() error {
    return d.pool.Close()
}
```

### Database Transaction

```go
type Transaction struct {
    tx *sql.Tx
}

func NewTransaction(db *Database) (*Transaction, error) {
    tx, err := db.pool.Begin()
    if err != nil {
        return nil, err
    }
    return &Transaction{tx: tx}, nil
}

func (t *Transaction) Close() error {
    // Commit on successful close, or rollback
    return t.tx.Commit()
}

// Register as scoped - one per request
services.AddScoped(NewTransaction)
```

### File Handler

```go
type FileHandler struct {
    file *os.File
}

func NewFileHandler() (*FileHandler, error) {
    f, err := os.CreateTemp("", "app-*")
    if err != nil {
        return nil, err
    }
    return &FileHandler{file: f}, nil
}

func (f *FileHandler) Close() error {
    f.file.Close()
    return os.Remove(f.file.Name())  // Clean up temp file
}
```

### HTTP Client with Keep-Alive

```go
type HTTPClient struct {
    client *http.Client
}

func NewHTTPClient() *HTTPClient {
    return &HTTPClient{
        client: &http.Client{
            Transport: &http.Transport{
                MaxIdleConns:        100,
                MaxIdleConnsPerHost: 10,
                IdleConnTimeout:     90 * time.Second,
            },
        },
    }
}

func (c *HTTPClient) Close() error {
    c.client.CloseIdleConnections()
    return nil
}
```

## Web Application Pattern

```go
func main() {
    services := godi.NewCollection()

    // Singletons - closed on app shutdown
    services.AddSingleton(NewDatabase)
    services.AddSingleton(NewRedisClient)

    // Scoped - closed per request
    services.AddScoped(NewTransaction)
    services.AddScoped(NewRequestContext)

    provider, _ := services.Build()
    defer provider.Close()  // Closes singletons on shutdown

    mux := http.NewServeMux()
    handler := godihttp.ScopeMiddleware(provider)(mux)
    // Middleware creates/closes scopes automatically

    server := &http.Server{Handler: handler}

    // Graceful shutdown
    go func() {
        <-signalChan
        server.Shutdown(ctx)
    }()

    server.ListenAndServe()
}
```

## Manual Disposal

You can check if a service is disposable:

```go
service := godi.MustResolve[SomeService](scope)

// If you need manual disposal
if closer, ok := service.(godi.Disposable); ok {
    defer closer.Close()
}
```

## Best Practices

1. **Always defer Close()** for providers and scopes
2. **Handle close errors** with custom handlers in production
3. **Keep disposal fast** - don't do heavy work in Close()
4. **Log disposal errors** for debugging
5. **Use scoped lifetime** for per-request resources like transactions

## Common Resources to Dispose

| Resource        | Lifetime  | Close Action           |
| --------------- | --------- | ---------------------- |
| Database pool   | Singleton | Close connections      |
| Redis client    | Singleton | Close connections      |
| HTTP client     | Singleton | Close idle connections |
| File handle     | Transient | Close and delete       |
| DB transaction  | Scoped    | Commit/rollback        |
| gRPC connection | Singleton | Close connection       |
| WebSocket       | Scoped    | Close connection       |

---

**See also:** [Service Lifetimes](../concepts/lifetimes.md) | [Scopes](../concepts/scopes.md)
