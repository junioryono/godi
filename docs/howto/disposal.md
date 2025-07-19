# Service Disposal

Service disposal ensures proper cleanup of resources when they're no longer needed. godi automatically manages disposal based on service lifetimes and scope boundaries.

## Disposable Interface

Services that need cleanup should implement the `Disposable` interface:

```go
type Disposable interface {
    Close() error
}
```

Or for context-aware cleanup:

```go
type DisposableWithContext interface {
    Close(ctx context.Context) error
}
```

## Basic Disposal

### Simple Disposable Service

```go
type DatabaseConnection struct {
    db     *sql.DB
    logger Logger
}

func NewDatabaseConnection(config *Config, logger Logger) (*DatabaseConnection, error) {
    db, err := sql.Open("postgres", config.DatabaseURL)
    if err != nil {
        return nil, err
    }

    if err := db.Ping(); err != nil {
        db.Close()
        return nil, err
    }

    return &DatabaseConnection{
        db:     db,
        logger: logger,
    }, nil
}

// Implement Disposable
func (c *DatabaseConnection) Close() error {
    c.logger.Info("Closing database connection")
    return c.db.Close()
}

// Register as singleton - disposed when provider closes
collection.AddSingleton(NewDatabaseConnection)
```

### Context-Aware Disposal

```go
type MessageQueue struct {
    conn     *amqp.Connection
    channel  *amqp.Channel
    logger   Logger
}

// Implement DisposableWithContext for graceful shutdown
func (mq *MessageQueue) Close(ctx context.Context) error {
    mq.logger.Info("Closing message queue connection")

    // Close channel first
    if err := mq.closeChannel(ctx); err != nil {
        return err
    }

    // Then close connection
    return mq.closeConnection(ctx)
}

func (mq *MessageQueue) closeChannel(ctx context.Context) error {
    done := make(chan error, 1)

    go func() {
        done <- mq.channel.Close()
    }()

    select {
    case err := <-done:
        return err
    case <-ctx.Done():
        return ctx.Err()
    }
}
```

## Disposal Order

Services are disposed in reverse order of creation (LIFO):

```go
// Creation order:
// 1. Config (singleton)
// 2. Logger (singleton)
// 3. Database (singleton)
// 4. Cache (singleton)
// 5. Repository (scoped)
// 6. Service (scoped)

// Disposal order when scope closes:
// 1. Service (scoped)
// 2. Repository (scoped)

// Disposal order when provider closes:
// 1. Cache (singleton)
// 2. Database (singleton)
// 3. Logger (singleton)
// 4. Config (singleton)
```

## Lifetime-Based Disposal

### Singleton Disposal

Singletons are disposed when the root provider is closed:

```go
func main() {
    collection := godi.NewServiceCollection()
    collection.AddSingleton(NewDatabaseConnection)
    collection.AddSingleton(NewCacheClient)

    provider, err := collection.BuildServiceProvider()
    if err != nil {
        log.Fatal(err)
    }

    // Ensure cleanup on exit
    defer provider.Close() // Disposes all singletons

    // Use the application...
}
```

### Scoped Disposal

Scoped services are disposed when their scope is closed:

```go
func HandleRequest(provider godi.ServiceProvider) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        // Create scope for this request
        scope := provider.CreateScope(r.Context())
        defer scope.Close() // Disposes all scoped services

        // Handle request...
    }
}
```

## Disposal Patterns

### Resource Pool

```go
type ConnectionPool struct {
    connections []*Connection
    mu          sync.Mutex
}

func NewConnectionPool(size int) *ConnectionPool {
    pool := &ConnectionPool{
        connections: make([]*Connection, 0, size),
    }

    // Pre-create connections
    for i := 0; i < size; i++ {
        conn := createConnection()
        pool.connections = append(pool.connections, conn)
    }

    return pool
}

func (p *ConnectionPool) Close() error {
    p.mu.Lock()
    defer p.mu.Unlock()

    var errs []error

    // Close all connections
    for _, conn := range p.connections {
        if err := conn.Close(); err != nil {
            errs = append(errs, err)
        }
    }

    p.connections = nil

    if len(errs) > 0 {
        return fmt.Errorf("failed to close %d connections", len(errs))
    }

    return nil
}
```

### Composite Disposal

```go
type Application struct {
    server   *http.Server
    db       *sql.DB
    cache    Cache
    logger   Logger
    metrics  *MetricsCollector
    shutdown []func() error
}

func (app *Application) Close() error {
    app.logger.Info("Shutting down application")

    // Shutdown HTTP server
    ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()

    if err := app.server.Shutdown(ctx); err != nil {
        app.logger.Error("Server shutdown error", err)
    }

    // Run custom shutdown hooks
    for _, fn := range app.shutdown {
        if err := fn(); err != nil {
            app.logger.Error("Shutdown hook error", err)
        }
    }

    // Flush metrics
    if err := app.metrics.Flush(); err != nil {
        app.logger.Error("Metrics flush error", err)
    }

    // Note: db and cache are managed by DI container
    // They will be disposed automatically

    app.logger.Info("Application shutdown complete")
    return nil
}
```

### Graceful Shutdown

```go
type Worker struct {
    tasks    chan Task
    done     chan struct{}
    wg       sync.WaitGroup
    logger   Logger
}

func NewWorker(logger Logger) *Worker {
    w := &Worker{
        tasks:  make(chan Task, 100),
        done:   make(chan struct{}),
        logger: logger,
    }

    // Start worker goroutines
    for i := 0; i < 5; i++ {
        w.wg.Add(1)
        go w.process()
    }

    return w
}

func (w *Worker) Close() error {
    w.logger.Info("Shutting down worker")

    // Signal workers to stop
    close(w.done)

    // Wait for workers to finish current tasks
    done := make(chan struct{})
    go func() {
        w.wg.Wait()
        close(done)
    }()

    // Wait with timeout
    select {
    case <-done:
        w.logger.Info("All workers stopped gracefully")
        return nil
    case <-time.After(30 * time.Second):
        return errors.New("worker shutdown timeout")
    }
}

func (w *Worker) process() {
    defer w.wg.Done()

    for {
        select {
        case task := <-w.tasks:
            // Process task
            task.Execute()
        case <-w.done:
            return
        }
    }
}
```

## Error Handling

### Multiple Disposal Errors

```go
type MultiResource struct {
    resources []io.Closer
}

func (m *MultiResource) Close() error {
    var errs []error

    // Close all resources, collecting errors
    for _, resource := range m.resources {
        if err := resource.Close(); err != nil {
            errs = append(errs, fmt.Errorf("resource close: %w", err))
        }
    }

    // Return combined errors
    if len(errs) > 0 {
        return errors.Join(errs...)
    }

    return nil
}
```

### Panic Recovery

```go
type SafeDisposable struct {
    resource io.Closer
    logger   Logger
}

func (s *SafeDisposable) Close() error {
    defer func() {
        if r := recover(); r != nil {
            s.logger.Error("Panic during disposal", "panic", r)
        }
    }()

    return s.resource.Close()
}
```

## Testing Disposal

### Verify Disposal

```go
type MockResource struct {
    closed bool
    mu     sync.Mutex
}

func (m *MockResource) Close() error {
    m.mu.Lock()
    defer m.mu.Unlock()

    if m.closed {
        return errors.New("already closed")
    }

    m.closed = true
    return nil
}

func (m *MockResource) IsClosed() bool {
    m.mu.Lock()
    defer m.mu.Unlock()
    return m.closed
}

func TestDisposal(t *testing.T) {
    resource := &MockResource{}

    collection := godi.NewServiceCollection()
    collection.AddSingleton(func() *MockResource { return resource })

    provider, _ := collection.BuildServiceProvider()

    // Use the resource
    r, _ := godi.Resolve[*MockResource](provider)
    assert.False(t, r.IsClosed())

    // Close provider
    provider.Close()

    // Verify disposal
    assert.True(t, resource.IsClosed())
}
```

### Test Disposal Order

```go
func TestDisposalOrder(t *testing.T) {
    var order []string

    createResource := func(name string) func() Disposable {
        return func() Disposable {
            return &TestDisposable{
                name: name,
                onClose: func() {
                    order = append(order, name)
                },
            }
        }
    }

    collection := godi.NewServiceCollection()
    collection.AddSingleton(createResource("first"))
    collection.AddSingleton(createResource("second"))
    collection.AddSingleton(createResource("third"))

    provider, _ := collection.BuildServiceProvider()

    // Force creation in order
    godi.Resolve[Disposable](provider) // first
    godi.Resolve[Disposable](provider) // second
    godi.Resolve[Disposable](provider) // third

    // Close and verify LIFO order
    provider.Close()

    assert.Equal(t, []string{"third", "second", "first"}, order)
}
```

## Best Practices

### 1. Always Implement Disposal for Resources

```go
// ✅ Good - implements disposal
type FileWriter struct {
    file *os.File
}

func (w *FileWriter) Close() error {
    return w.file.Close()
}

// ❌ Bad - leaks file handle
type BadFileWriter struct {
    file *os.File
    // No Close method!
}
```

### 2. Use Defer for Scopes

```go
// ✅ Good - always cleans up
func ProcessRequest(provider godi.ServiceProvider) {
    scope := provider.CreateScope(context.Background())
    defer scope.Close()

    // Process...
}

// ❌ Bad - might leak on error
func BadProcessRequest(provider godi.ServiceProvider) {
    scope := provider.CreateScope(context.Background())

    // Process...

    scope.Close() // Might not be called
}
```

### 3. Handle Disposal Errors

```go
// ✅ Good - logs disposal errors
func (s *Service) Close() error {
    if err := s.db.Close(); err != nil {
        s.logger.Error("Failed to close database", err)
        return err
    }

    if err := s.cache.Close(); err != nil {
        s.logger.Error("Failed to close cache", err)
        return err
    }

    return nil
}
```

### 4. Make Disposal Idempotent

```go
// ✅ Good - safe to call multiple times
type IdempotentResource struct {
    resource io.Closer
    closed   int32
}

func (r *IdempotentResource) Close() error {
    if !atomic.CompareAndSwapInt32(&r.closed, 0, 1) {
        return nil // Already closed
    }

    return r.resource.Close()
}
```

### 5. Document Disposal Behavior

```go
// NewTempProcessor creates a processor that manages temporary files.
// The processor and all temporary files are automatically cleaned up
// when the containing scope is disposed.
//
// Disposal behavior:
// - Flushes any pending data
// - Deletes all temporary files
// - Closes the underlying writer
func NewTempProcessor(writer io.Writer) *TempProcessor {
    // ...
}
```

## Summary

Proper disposal is crucial for:

- Preventing resource leaks
- Graceful shutdown
- Clean test isolation
- Production reliability

godi's automatic disposal management based on service lifetimes ensures resources are cleaned up properly without manual intervention.
