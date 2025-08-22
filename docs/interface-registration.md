# Resource Management

Learn how godi manages resources and ensures proper cleanup.

## The Disposable Interface

### Implementing Disposable

Any service that needs cleanup should implement the `Disposable` interface:

```go
type DatabaseConnection interface {
    godi.Disposable

    Query(query string) Result
}

// Example: Database connection
type databaseConnection struct {
    conn *sql.DB
}

func NewDatabaseConnection(config *Config) DatabaseConnection {
    conn, _ := sql.Open("postgres", config.DatabaseURL)
    return &databaseConnection{conn: conn}
}

func (d *databaseConnection) Query(query string) Result {
    // Execute query...
    return Result{}
}

func (d *databaseConnection) Close() error {
    return d.conn.Close()
}

// Automatically closed when provider/scope closes
services.AddSingleton(NewDatabaseConnection)
```

### Common Disposable Resources

```go
// File handles
type FileLogger struct {
    file *os.File
}

func (f *FileLogger) Close() error {
    return f.file.Close()
}

// Network connections
type APIClient struct {
    client *http.Client
    conn   net.Conn
}

func (a *APIClient) Close() error {
    return a.conn.Close()
}

// Background workers
type Worker struct {
    done chan struct{}
}

func (w *Worker) Close() error {
    close(w.done)
    return nil
}

// Temporary resources
type TempDirectory struct {
    path string
}

func (t *TempDirectory) Close() error {
    return os.RemoveAll(t.path)
}
```

## Cleanup Lifecycle

### Disposal Order

Resources are disposed in reverse order of creation:

```go
// Services created in order: Logger -> Database -> Cache -> Service
// Disposal happens in reverse: Service -> Cache -> Database -> Logger

type Service struct {
    db    *Database
    cache *Cache
}

func (s *Service) Close() error {
    fmt.Println("Closing Service")
    return nil
}

type Cache struct {
    conn *redis.Client
}

func (c *Cache) Close() error {
    fmt.Println("Closing Cache")
    return c.conn.Close()
}

type Database struct {
    conn *sql.DB
}

func (d *Database) Close() error {
    fmt.Println("Closing Database")
    return d.conn.Close()
}

// When provider closes:
// Output:
// Closing Service
// Closing Cache
// Closing Database
```

### Lifetime-Based Cleanup

Different lifetimes are cleaned up at different times:

```go
// Singleton - cleaned up when provider closes
services.AddSingleton(NewDatabasePool)
provider.Close() // Singletons disposed here

// Scoped - cleaned up when scope closes
services.AddScoped(NewTransaction)
scope.Close() // Scoped services disposed here

// Transient - cleaned up when scope closes (if tracked)
services.AddTransient(NewTempFile)
scope.Close() // Transients created in scope disposed here
```

## Resource Pooling

### Connection Pool Pattern

```go
type ConnectionPool struct {
    connections chan *Connection
    maxSize     int
}

func NewConnectionPool(config *Config) *ConnectionPool {
    pool := &ConnectionPool{
        connections: make(chan *Connection, config.MaxConnections),
        maxSize:     config.MaxConnections,
    }

    // Pre-create connections
    for i := 0; i < config.MinConnections; i++ {
        conn := createConnection(config)
        pool.connections <- conn
    }

    return pool
}

func (p *ConnectionPool) Get() *Connection {
    select {
    case conn := <-p.connections:
        return conn
    default:
        return createConnection(p.config)
    }
}

func (p *ConnectionPool) Put(conn *Connection) {
    select {
    case p.connections <- conn:
        // Returned to pool
    default:
        // Pool full, close connection
        conn.Close()
    }
}

func (p *ConnectionPool) Close() error {
    close(p.connections)
    for conn := range p.connections {
        conn.Close()
    }
    return nil
}

// Register as singleton for app-wide pooling
services.AddSingleton(NewConnectionPool)
```

## Graceful Shutdown

### Provider Shutdown

```go
func main() {
    services := godi.NewCollection()
    services.AddSingleton(NewLogger)
    services.AddSingleton(NewDatabase)
    services.AddSingleton(NewWorker)

    provider, _ := services.Build()

    // Setup graceful shutdown
    sigChan := make(chan os.Signal, 1)
    signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

    // Run application
    go runApp(provider)

    // Wait for shutdown signal
    <-sigChan

    fmt.Println("Shutting down gracefully...")

    // Close provider - all resources cleaned up
    if err := provider.Close(); err != nil {
        log.Printf("Error during shutdown: %v", err)
    }

    fmt.Println("Shutdown complete")
}
```

### Background Worker Management

```go
type BackgroundWorker struct {
    done   chan struct{}
    wg     sync.WaitGroup
    ticker *time.Ticker
}

func NewBackgroundWorker(logger Logger) *BackgroundWorker {
    w := &BackgroundWorker{
        done:   make(chan struct{}),
        ticker: time.NewTicker(5 * time.Second),
    }

    w.wg.Add(1)
    go w.run(logger)

    return w
}

func (w *BackgroundWorker) run(logger Logger) {
    defer w.wg.Done()

    for {
        select {
        case <-w.done:
            logger.Info("Worker shutting down")
            return
        case <-w.ticker.C:
            // Do periodic work
            w.doWork()
        }
    }
}

func (w *BackgroundWorker) Close() error {
    // Signal shutdown
    close(w.done)

    // Stop ticker
    w.ticker.Stop()

    // Wait for worker to finish
    w.wg.Wait()

    return nil
}

// Worker automatically stopped when provider closes
services.AddSingleton(NewBackgroundWorker)
```

## Transaction Management

### Auto-Rollback Pattern

```go
type Transaction struct {
    tx        *sql.Tx
    committed bool
}

func NewTransaction(db *Database) (*Transaction, error) {
    tx, err := db.Begin()
    if err != nil {
        return nil, err
    }

    return &Transaction{
        tx:        tx,
        committed: false,
    }, nil
}

func (t *Transaction) Commit() error {
    err := t.tx.Commit()
    if err == nil {
        t.committed = true
    }
    return err
}

func (t *Transaction) Rollback() error {
    return t.tx.Rollback()
}

func (t *Transaction) Close() error {
    if !t.committed {
        // Auto-rollback if not committed
        return t.tx.Rollback()
    }
    return nil
}

// Transaction automatically rolled back if not committed
services.AddScoped(NewTransaction)

func ProcessOrder(scope godi.Scope) error {
    tx := godi.MustResolve[*Transaction](scope)

    // Do work...

    if err := validateOrder(); err != nil {
        // No need to call Rollback - auto cleanup
        return err
    }

    return tx.Commit()
    // If we don't reach Commit, transaction rolls back automatically
}
```

## Memory Management

### Preventing Memory Leaks

```go
// ❌ Potential memory leak
type LeakyCache struct {
    data map[string]*BigObject
}

func NewLeakyCache() *LeakyCache {
    return &LeakyCache{
        data: make(map[string]*BigObject),
    }
}

// ✅ Proper cleanup
type SafeCache struct {
    data map[string]*BigObject
    mu   sync.RWMutex
}

func NewSafeCache() *SafeCache {
    return &SafeCache{
        data: make(map[string]*BigObject),
    }
}

func (c *SafeCache) Close() error {
    c.mu.Lock()
    defer c.mu.Unlock()

    // Clear references
    for k := range c.data {
        delete(c.data, k)
    }

    return nil
}
```

### Resource Limits

```go
type RateLimiter struct {
    semaphore chan struct{}
    wg        sync.WaitGroup
}

func NewRateLimiter(maxConcurrent int) *RateLimiter {
    return &RateLimiter{
        semaphore: make(chan struct{}, maxConcurrent),
    }
}

func (r *RateLimiter) Execute(fn func()) {
    r.wg.Add(1)
    r.semaphore <- struct{}{} // Acquire

    go func() {
        defer r.wg.Done()
        defer func() { <-r.semaphore }() // Release

        fn()
    }()
}

func (r *RateLimiter) Close() error {
    // Wait for all operations to complete
    r.wg.Wait()
    close(r.semaphore)
    return nil
}
```

## Error Handling in Cleanup

### Aggregating Disposal Errors

```go
type ServiceManager struct {
    services []Disposable
}

func (m *ServiceManager) AddService(service Disposable) {
    m.services = append(m.services, service)
}

func (m *ServiceManager) Close() error {
    var errors []error

    // Close in reverse order
    for i := len(m.services) - 1; i >= 0; i-- {
        if err := m.services[i].Close(); err != nil {
            errors = append(errors, fmt.Errorf("service %d: %w", i, err))
        }
    }

    if len(errors) > 0 {
        return fmt.Errorf("disposal errors: %v", errors)
    }

    return nil
}
```

### Critical vs Non-Critical Cleanup

```go
type ComplexService struct {
    db       *sql.DB        // Critical
    cache    *Cache         // Non-critical
    metrics  *MetricsClient // Non-critical
}

func (s *ComplexService) Close() error {
    var criticalError error

    // Non-critical cleanup (log but don't fail)
    if err := s.metrics.Close(); err != nil {
        log.Printf("Warning: failed to close metrics: %v", err)
    }

    if err := s.cache.Close(); err != nil {
        log.Printf("Warning: failed to close cache: %v", err)
    }

    // Critical cleanup (return error)
    if err := s.db.Close(); err != nil {
        criticalError = fmt.Errorf("failed to close database: %w", err)
    }

    return criticalError
}
```

## Testing Resource Management

### Verify Cleanup

```go
func TestResourceCleanup(t *testing.T) {
    cleaned := false

    services := godi.NewCollection()
    services.AddScoped(func() *TestResource {
        return &TestResource{
            onClose: func() { cleaned = true },
        }
    })

    provider, _ := services.Build()
    scope, _ := provider.CreateScope(context.Background())

    resource := godi.MustResolve[*TestResource](scope)
    resource.DoWork()

    // Verify cleanup happens
    scope.Close()
    assert.True(t, cleaned, "Resource should be cleaned up")
}

type TestResource struct {
    onClose func()
}

func (r *TestResource) Close() error {
    if r.onClose != nil {
        r.onClose()
    }
    return nil
}
```

### Mock Disposables

```go
type MockConnection struct {
    closed bool
    mu     sync.Mutex
}

func (m *MockConnection) IsClosed() bool {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.closed
}

func (m *MockConnection) Close() error {
    m.mu.Lock()
    defer m.mu.Unlock()

    if m.closed {
        return errors.New("already closed")
    }

    m.closed = true
    return nil
}

func TestConnectionLifecycle(t *testing.T) {
    conn := &MockConnection{}

    services := godi.NewCollection()
    services.AddSingleton(func() *MockConnection { return conn })

    provider, _ := services.Build()

    // Use connection
    c := godi.MustResolve[*MockConnection](provider)
    assert.False(t, c.IsClosed())

    // Close provider
    provider.Close()
    assert.True(t, conn.IsClosed())
}
```

## Best Practices

1. **Always implement Close()** for resources needing cleanup
2. **Use defer for cleanup** in goroutines
3. **Handle errors in Close()** but don't panic
4. **Clean up in reverse order** of creation
5. **Test cleanup paths** explicitly
6. **Use context for cancellation** in long-running operations
7. **Log cleanup failures** for debugging

## Common Patterns

### Resource Wrapper

```go
type ResourceWrapper[T any] struct {
    resource T
    cleanup  func() error
}

func NewResourceWrapper[T any](
    resource T,
    cleanup func() error,
) *ResourceWrapper[T] {
    return &ResourceWrapper[T]{
        resource: resource,
        cleanup:  cleanup,
    }
}

func (w *ResourceWrapper[T]) Get() T {
    return w.resource
}

func (w *ResourceWrapper[T]) Close() error {
    if w.cleanup != nil {
        return w.cleanup()
    }
    return nil
}

// Usage
services.AddSingleton(func() *ResourceWrapper[*os.File] {
    file, _ := os.Create("app.log")
    return NewResourceWrapper(file, file.Close)
})
```

### Cleanup Chain

```go
type CleanupChain struct {
    cleanups []func() error
}

func (c *CleanupChain) Add(cleanup func() error) {
    c.cleanups = append(c.cleanups, cleanup)
}

func (c *CleanupChain) Close() error {
    var errs []error

    // Execute in reverse order
    for i := len(c.cleanups) - 1; i >= 0; i-- {
        if err := c.cleanups[i](); err != nil {
            errs = append(errs, err)
        }
    }

    if len(errs) > 0 {
        return fmt.Errorf("cleanup errors: %v", errs)
    }

    return nil
}
```

## Next Steps

- Learn about [Modules](modules.md)
- Explore [Keyed Services](keyed-services.md)
- Understand [Service Groups](service-groups.md)
