# Configuration Options Reference

This guide covers all configuration options available in godi for customizing behavior.

## ServiceProviderOptions

Configure behavior when building a service provider:

```go
type ServiceProviderOptions struct {
    // Validation
    ValidateOnBuild bool

    // Callbacks
    OnServiceResolved func(serviceType reflect.Type, instance interface{}, duration time.Duration)
    OnServiceError    func(serviceType reflect.Type, err error)

    // Timeouts
    ResolutionTimeout time.Duration

    // Testing
    DryRun bool

    // Recovery
    RecoverFromPanics bool

    // Performance
    DeferAcyclicVerification bool
}
```

### ValidateOnBuild

Validates all services can be constructed during `BuildServiceProvider`:

```go
options := &ServiceProviderOptions{
    ValidateOnBuild: true, // Default: false
}

// Catches configuration errors early
provider, err := collection.BuildServiceProviderWithOptions(options)
if err != nil {
    // Error indicates misconfiguration
    log.Fatal("Service configuration error:", err)
}
```

**When to use:**

- Production applications
- When you want to fail fast
- During testing to ensure all dependencies are satisfied

**Performance impact:**

- Increases startup time
- Attempts to construct all singleton services
- Worth it for catching errors early

### OnServiceResolved

Callback after successful service resolution:

```go
options := &ServiceProviderOptions{
    OnServiceResolved: func(serviceType reflect.Type, instance interface{}, duration time.Duration) {
        // Logging
        log.Printf("[DI] Resolved %s in %v", serviceType, duration)

        // Metrics
        resolveCounter.WithLabelValues(serviceType.String()).Inc()
        resolveHistogram.Observe(duration.Seconds())

        // Performance monitoring
        if duration > 100*time.Millisecond {
            log.Warnf("Slow resolution: %s took %v", serviceType, duration)
        }
    },
}
```

**Use cases:**

- Performance monitoring
- Debugging resolution issues
- Metrics collection
- Audit logging

### OnServiceError

Callback when service resolution fails:

```go
options := &ServiceProviderOptions{
    OnServiceError: func(serviceType reflect.Type, err error) {
        // Error tracking
        errorCounter.WithLabelValues(serviceType.String()).Inc()

        // Logging with context
        if godi.IsNotFound(err) {
            log.Errorf("Service not registered: %s", serviceType)
        } else {
            log.Errorf("Failed to resolve %s: %v", serviceType, err)
        }

        // Alert on critical services
        if serviceType == reflect.TypeOf((*Database)(nil)).Elem() {
            alerting.SendCritical("Database resolution failed", err)
        }
    },
}
```

**Use cases:**

- Error monitoring
- Debugging missing services
- Alerting on critical failures
- Error metrics

### ResolutionTimeout

Timeout for individual service resolutions:

```go
options := &ServiceProviderOptions{
    ResolutionTimeout: 5 * time.Second, // Default: 0 (no timeout)
}

// Resolution will timeout if it takes too long
service, err := godi.Resolve[SlowService](provider)
if godi.IsTimeout(err) {
    log.Error("Service resolution timed out")
}
```

**When to use:**

- Prevent hanging on slow constructors
- Detect initialization deadlocks
- Enforce SLAs on service creation

**Considerations:**

- Set reasonable timeouts
- Account for slow operations (database connections)
- May cause false positives under load

### DryRun

Disable actual constructor invocation:

```go
options := &ServiceProviderOptions{
    DryRun: true, // Default: false
}

// Validates graph without creating instances
provider, err := collection.BuildServiceProviderWithOptions(options)
```

**Use cases:**

- Validate dependency graph
- Testing service configuration
- CI/CD validation without side effects

**Limitations:**

- Services are not actually created
- Can't test runtime behavior
- Only validates the dependency graph

### RecoverFromPanics

Recover from panics in constructors:

```go
options := &ServiceProviderOptions{
    RecoverFromPanics: true, // Default: false
}

// Panicking constructor
func NewBadService() *BadService {
    panic("initialization failed")
}

// With recovery enabled, returns error instead of panic
service, err := godi.Resolve[BadService](provider)
if err != nil {
    log.Error("Service panicked:", err)
}
```

**When to use:**

- Integration with third-party code
- Defensive programming
- Development/debugging

**Caution:**

- May hide serious issues
- Panics often indicate bugs
- Consider fixing the root cause

### DeferAcyclicVerification

Defer circular dependency checking:

```go
options := &ServiceProviderOptions{
    DeferAcyclicVerification: true, // Default: false
}

// Cycle detection happens on first Invoke instead of Build
provider, err := collection.BuildServiceProviderWithOptions(options)
```

**Benefits:**

- Faster startup
- Lazy validation

**Trade-offs:**

- Errors discovered later
- May fail at runtime

## Provide Options

Options for service registration:

### Name Option

Register keyed/named services:

```go
// Single implementation
collection.AddSingleton(NewRedisCache, godi.Name("redis"))
collection.AddSingleton(NewMemoryCache, godi.Name("memory"))

// Resolve by name
cache, _ := godi.ResolveKeyed[Cache](provider, "redis")
```

### Group Option

Register services in groups:

```go
// Multiple implementations
collection.AddSingleton(NewUserHandler, godi.Group("handlers"))
collection.AddSingleton(NewOrderHandler, godi.Group("handlers"))
collection.AddSingleton(NewProductHandler, godi.Group("handlers"))

// Consume as slice
type App struct {
    godi.In
    Handlers []Handler `group:"handlers"`
}
```

### As Option

Register service as specific interfaces:

```go
// PostgresDB implements multiple interfaces
collection.AddSingleton(NewPostgresDB,
    godi.As(new(Reader), new(Writer), new(Transactor)))

// Can resolve as any of the interfaces
reader, _ := godi.Resolve[Reader](provider)
writer, _ := godi.Resolve[Writer](provider)
```

### Callback Options

Monitor service creation:

```go
var creationTime time.Time

collection.AddSingleton(NewExpensiveService,
    godi.WithProviderBeforeCallback(func(info godi.BeforeCallbackInfo) {
        creationTime = time.Now()
        log.Printf("Creating %s", info.Name)
    }),
    godi.WithProviderCallback(func(info godi.CallbackInfo) {
        duration := time.Since(creationTime)
        log.Printf("Created %s in %v", info.Name, duration)

        if duration > time.Second {
            log.Warn("Slow service creation")
        }
    }),
)
```

## Decorate Options

Options for service decoration:

```go
// Basic decoration
collection.Decorate(LoggingDecorator)

// With callbacks
collection.Decorate(CachingDecorator,
    godi.WithDecoratorCallback(func(info godi.CallbackInfo) {
        log.Printf("Decorated service in %v", info.Runtime)
    }),
)
```

## Complete Example

```go
func CreateProvider() (godi.ServiceProvider, error) {
    collection := godi.NewServiceCollection()

    // Register services with options
    collection.AddSingleton(NewConfig)
    collection.AddSingleton(NewLogger, godi.As(new(Logger)))

    // Named services
    collection.AddSingleton(NewPostgresDB, godi.Name("primary"))
    collection.AddSingleton(NewPostgresReadReplica, godi.Name("replica"))

    // Groups
    collection.AddSingleton(NewHealthCheck, godi.Group("health"))
    collection.AddSingleton(NewReadinessCheck, godi.Group("health"))

    // Configure provider
    options := &godi.ServiceProviderOptions{
        // Validate everything upfront
        ValidateOnBuild: true,

        // Monitor resolution
        OnServiceResolved: func(serviceType reflect.Type, instance interface{}, duration time.Duration) {
            metrics.RecordResolution(serviceType, duration)
        },

        // Track errors
        OnServiceError: func(serviceType reflect.Type, err error) {
            logger.Error("Resolution failed",
                "type", serviceType,
                "error", err)
        },

        // Prevent hanging
        ResolutionTimeout: 30 * time.Second,

        // Safety
        RecoverFromPanics: true,
    }

    return collection.BuildServiceProviderWithOptions(options)
}
```

## Option Patterns

### Development vs Production

```go
func GetProviderOptions(env string) *ServiceProviderOptions {
    base := &ServiceProviderOptions{
        OnServiceResolved: logResolution,
        OnServiceError:    logError,
    }

    switch env {
    case "development":
        base.ValidateOnBuild = false      // Faster startup
        base.RecoverFromPanics = true     // More forgiving
        base.ResolutionTimeout = 0        // No timeout

    case "production":
        base.ValidateOnBuild = true       // Fail fast
        base.RecoverFromPanics = false    // Don't hide issues
        base.ResolutionTimeout = 10 * time.Second
    }

    return base
}
```

### Testing Configuration

```go
func TestProviderOptions() *ServiceProviderOptions {
    return &ServiceProviderOptions{
        DryRun:           false, // Actually create services
        ValidateOnBuild:  true,  // Catch issues
        ResolutionTimeout: 1 * time.Second, // Fail fast in tests
    }
}
```

### Monitoring Configuration

```go
func MonitoredProviderOptions(metrics *Metrics) *ServiceProviderOptions {
    return &ServiceProviderOptions{
        OnServiceResolved: func(t reflect.Type, _ interface{}, d time.Duration) {
            metrics.ResolutionDuration.
                WithLabelValues(t.String()).
                Observe(d.Seconds())

            metrics.ResolutionCount.
                WithLabelValues(t.String(), "success").
                Inc()
        },

        OnServiceError: func(t reflect.Type, err error) {
            metrics.ResolutionCount.
                WithLabelValues(t.String(), "error").
                Inc()

            errorType := "unknown"
            if godi.IsNotFound(err) {
                errorType = "not_found"
            } else if godi.IsCircularDependency(err) {
                errorType = "circular"
            }

            metrics.ResolutionErrors.
                WithLabelValues(t.String(), errorType).
                Inc()
        },
    }
}
```

## Best Practices

1. **Always validate in production**

   ```go
   options.ValidateOnBuild = true
   ```

2. **Set reasonable timeouts**

   ```go
   options.ResolutionTimeout = 30 * time.Second
   ```

3. **Monitor in production**

   ```go
   options.OnServiceResolved = productionMonitoring
   options.OnServiceError = productionErrorTracking
   ```

4. **Don't hide panics in production**

   ```go
   options.RecoverFromPanics = false // Let them crash
   ```

5. **Use callbacks for cross-cutting concerns**
   - Logging
   - Metrics
   - Tracing
   - Auditing
