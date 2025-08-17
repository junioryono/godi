# Resource Disposal

godi automatically manages cleanup for services that implement the `Disposable` interface.

## The Disposable Interface

Services can implement cleanup logic:

```go
type Disposable interface {
    Close() error
}
```

## Basic Example

```go
// Service with cleanup
type DatabaseConnection struct {
    conn *sql.DB
}

func NewDatabase(config *Config) (*DatabaseConnection, error) {
    conn, err := sql.Open("postgres", config.DSN)
    if err != nil {
        return nil, err
    }
    
    return &DatabaseConnection{conn: conn}, nil
}

// Implement Disposable
func (db *DatabaseConnection) Close() error {
    if db.conn != nil {
        return db.conn.Close()
    }
    return nil
}

// Register the service
services.AddSingleton(NewDatabase)
```

## Automatic Cleanup

godi automatically calls `Close()` when:

1. **Provider is closed** - All singleton disposables are cleaned up
2. **Scope is closed** - All scoped disposables are cleaned up
3. **Cleanup order** - Services are disposed in reverse creation order

```go
func main() {
    services := godi.NewCollection()
    services.AddSingleton(NewDatabase)
    services.AddSingleton(NewCache)
    
    provider, _ := services.Build()
    defer provider.Close() // Automatically closes Database and Cache
    
    // Use services...
}
```

## Scoped Disposal

Scoped services are disposed when their scope closes:

```go
func handleRequest(provider godi.Provider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        scope, _ := provider.CreateScope(r.Context())
        defer scope.Close() // Disposes all scoped services
        
        // Scoped services created here
        service, _ := godi.Resolve[*Transaction](scope)
        // Transaction.Close() called when scope closes
    }
}
```

## Real-World Examples

### Database Transaction

```go
type Transaction struct {
    tx *sql.Tx
}

func NewTransaction(db *DatabaseConnection) (*Transaction, error) {
    tx, err := db.conn.Begin()
    if err != nil {
        return nil, err
    }
    return &Transaction{tx: tx}, nil
}

func (t *Transaction) Close() error {
    if t.tx != nil {
        // Rollback if not committed
        return t.tx.Rollback()
    }
    return nil
}

// Register as scoped - one per request
services.AddScoped(NewTransaction)
```

### File Handler

```go
type FileProcessor struct {
    file   *os.File
    writer *bufio.Writer
}

func NewFileProcessor(path string) (*FileProcessor, error) {
    file, err := os.Create(path)
    if err != nil {
        return nil, err
    }
    
    return &FileProcessor{
        file:   file,
        writer: bufio.NewWriter(file),
    }, nil
}

func (f *FileProcessor) Close() error {
    var errs []error
    
    // Flush writer first
    if err := f.writer.Flush(); err != nil {
        errs = append(errs, err)
    }
    
    // Then close file
    if err := f.file.Close(); err != nil {
        errs = append(errs, err)
    }
    
    if len(errs) > 0 {
        return fmt.Errorf("disposal errors: %v", errs)
    }
    return nil
}
```

### Connection Pool

```go
type ConnectionPool struct {
    connections []*Connection
    mu          sync.Mutex
}

func NewConnectionPool(size int) (*ConnectionPool, error) {
    pool := &ConnectionPool{
        connections: make([]*Connection, 0, size),
    }
    
    // Initialize connections
    for i := 0; i < size; i++ {
        conn, err := NewConnection()
        if err != nil {
            // Clean up already created connections
            pool.Close()
            return nil, err
        }
        pool.connections = append(pool.connections, conn)
    }
    
    return pool, nil
}

func (p *ConnectionPool) Close() error {
    p.mu.Lock()
    defer p.mu.Unlock()
    
    var errs []error
    for _, conn := range p.connections {
        if err := conn.Close(); err != nil {
            errs = append(errs, err)
        }
    }
    
    p.connections = nil
    
    if len(errs) > 0 {
        return &DisposalError{
            Context: "connection pool",
            Errors:  errs,
        }
    }
    return nil
}
```

## Disposal Order

Services are disposed in reverse order of creation:

```go
// Creation order: Logger -> Database -> Cache -> Service
services.AddSingleton(NewLogger)    // Created 1st
services.AddSingleton(NewDatabase)  // Created 2nd
services.AddSingleton(NewCache)     // Created 3rd
services.AddScoped(NewService)      // Created 4th (per scope)

// Disposal order: Service -> Cache -> Database -> Logger
// This ensures dependencies are still available during cleanup
```

## Best Practices

### 1. Always Implement Close() Safely

```go
func (s *Service) Close() error {
    // Check for nil
    if s.resource == nil {
        return nil
    }
    
    // Prevent double-close
    if s.closed {
        return nil
    }
    s.closed = true
    
    // Do cleanup
    return s.resource.Close()
}
```

### 2. Use defer for Scopes

```go
// Always use defer immediately after creating scope
scope, err := provider.CreateScope(ctx)
if err != nil {
    return err
}
defer scope.Close() // Guaranteed cleanup
```

### 3. Handle Disposal Errors

```go
provider, _ := collection.Build()

// Option 1: Log and continue
defer func() {
    if err := provider.Close(); err != nil {
        log.Printf("Disposal error: %v", err)
    }
}()

// Option 2: Return error
if err := provider.Close(); err != nil {
    return fmt.Errorf("cleanup failed: %w", err)
}
```

### 4. Context Cancellation

Scopes automatically close when their context is cancelled:

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

scope, _ := provider.CreateScope(ctx)
// No need for defer scope.Close() - auto-closes on context cancel
```

## Testing with Disposal

```go
func TestServiceWithCleanup(t *testing.T) {
    services := godi.NewCollection()
    services.AddSingleton(NewTestDatabase)
    
    provider, err := services.Build()
    require.NoError(t, err)
    
    // Ensure cleanup even if test fails
    t.Cleanup(func() {
        err := provider.Close()
        assert.NoError(t, err)
    })
    
    // Run tests...
}
```

## Common Patterns

### Graceful Shutdown

```go
func main() {
    provider, _ := setupDI()
    
    // Handle shutdown signals
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
    
    go func() {
        <-sigChan
        log.Println("Shutting down...")
        
        // Graceful cleanup
        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()
        
        if err := provider.Close(); err != nil {
            log.Printf("Cleanup error: %v", err)
        }
        
        os.Exit(0)
    }()
    
    // Run application...
}
```

### Cleanup with Metrics

```go
type MetricsCollector struct {
    metrics *prometheus.Registry
    server  *http.Server
}

func (m *MetricsCollector) Close() error {
    // Gracefully shutdown metrics server
    ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
    defer cancel()
    
    if err := m.server.Shutdown(ctx); err != nil {
        return fmt.Errorf("metrics server shutdown failed: %w", err)
    }
    
    // Unregister collectors
    m.metrics.Unregister(m.collectors...)
    
    return nil
}
```

## Summary

- Implement `Disposable` for automatic cleanup
- Services disposed in reverse creation order
- Scoped services cleaned up with scope
- Singleton services cleaned up with provider
- Always use `defer` for guaranteed cleanup
- Handle disposal errors appropriately