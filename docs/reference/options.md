# Service Provider Options

Configure how godi builds and manages your services.

## Basic Usage

```go
options := &godi.ServiceProviderOptions{
    ValidateOnBuild: true,
}

provider, err := services.BuildServiceProviderWithOptions(options)
```

## Available Options

### ValidateOnBuild

Validates all services can be created when building the provider.

```go
options := &ServiceProviderOptions{
    ValidateOnBuild: true, // Default: false
}

// Catches errors early
provider, err := services.BuildServiceProviderWithOptions(options)
if err != nil {
    // Error shows exactly what's misconfigured
    log.Fatal("Configuration error:", err)
}
```

**When to use:**

- Always in production
- During development for early error detection
- In tests to verify configuration

### OnServiceResolved

Callback after successful service resolution.

```go
options := &ServiceProviderOptions{
    OnServiceResolved: func(serviceType reflect.Type, instance any, duration time.Duration) {
        // Logging
        log.Printf("[DI] Resolved %s in %v", serviceType, duration)

        // Metrics
        metrics.RecordResolution(serviceType.String(), duration)

        // Performance monitoring
        if duration > 100*time.Millisecond {
            log.Warnf("Slow resolution: %s took %v", serviceType, duration)
        }
    },
}
```

**Use cases:**

- Performance monitoring
- Debug logging
- Metrics collection

### OnServiceError

Callback when service resolution fails.

```go
options := &ServiceProviderOptions{
    OnServiceError: func(serviceType reflect.Type, err error) {
        // Logging
        log.Errorf("Failed to resolve %s: %v", serviceType, err)

        // Metrics
        errorCounter.WithLabelValues(serviceType.String()).Inc()

        // Alerting for critical services
        if serviceType == reflect.TypeOf((*Database)(nil)).Elem() {
            alerts.SendCritical("Database resolution failed", err)
        }
    },
}
```

**Use cases:**

- Error tracking
- Debugging
- Alerting

### ResolutionTimeout

Timeout for individual service resolutions.

```go
options := &ServiceProviderOptions{
    ResolutionTimeout: 30 * time.Second, // Default: 0 (no timeout)
}

// Resolution will timeout if constructor takes too long
service, err := godi.Resolve[SlowService](provider)
if godi.IsTimeout(err) {
    log.Error("Service resolution timed out")
}
```

**When to use:**

- Prevent hanging on slow constructors
- Detect initialization deadlocks
- Enforce SLAs

## Complete Example

```go
// production/options.go
package production

import (
    "github.com/junioryono/godi/v2"
    "myapp/monitoring"
    "time"
    "log"
)

func GetProviderOptions() *godi.ServiceProviderOptions {
    return &godi.ServiceProviderOptions{
        // Always validate in production
        ValidateOnBuild: true,

        // Monitor performance
        OnServiceResolved: func(serviceType reflect.Type, instance any, duration time.Duration) {
            monitoring.RecordServiceResolution(serviceType, duration)

            if duration > 500*time.Millisecond {
                log.Warnf("Slow service resolution: %s took %v", serviceType, duration)
            }
        },

        // Track errors
        OnServiceError: func(serviceType reflect.Type, err error) {
            monitoring.RecordServiceError(serviceType, err)
            log.Errorf("Service resolution failed for %s: %v", serviceType, err)
        },

        // Prevent hanging
        ResolutionTimeout: 30 * time.Second,
    }
}

// main.go
func main() {
    services := godi.NewServiceCollection()
    services.AddModules(app.Module)

    // Use production options
    provider, err := services.BuildServiceProviderWithOptions(
        production.GetProviderOptions(),
    )
    if err != nil {
        log.Fatal("Failed to build services:", err)
    }
    defer provider.Close()
}
```

## Development vs Production

### Development Options

```go
// development/options.go
package development

func GetProviderOptions() *godi.ServiceProviderOptions {
    return &godi.ServiceProviderOptions{
        // Validate to catch errors early
        ValidateOnBuild: true,

        // Verbose logging
        OnServiceResolved: func(serviceType reflect.Type, instance any, duration time.Duration) {
            log.Printf("[DEV] Resolved %s in %v", serviceType, duration)
        },

        // Detailed error logging
        OnServiceError: func(serviceType reflect.Type, err error) {
            log.Printf("[DEV] ERROR: Failed to resolve %s: %+v", serviceType, err)
        },
    }
}
```

### Production Options

```go
// production/options.go
package production

func GetProviderOptions() *godi.ServiceProviderOptions {
    return &godi.ServiceProviderOptions{
        ValidateOnBuild: true,

        // Send to monitoring service
        OnServiceResolved: func(serviceType reflect.Type, instance any, duration time.Duration) {
            metrics.ServiceResolutionDuration.
                WithLabelValues(serviceType.String()).
                Observe(duration.Seconds())
        },

        // Alert on critical failures
        OnServiceError: func(serviceType reflect.Type, err error) {
            metrics.ServiceResolutionErrors.
                WithLabelValues(serviceType.String()).
                Inc()

            if isCriticalService(serviceType) {
                alerting.NotifyOps(serviceType, err)
            }
        },

        ResolutionTimeout: 30 * time.Second,
    }
}
```

## Best Practices

1. **Always validate in production** - Set `ValidateOnBuild: true`
2. **Monitor resolution times** - Use `OnServiceResolved` for metrics
3. **Track errors** - Use `OnServiceError` for alerting
4. **Set reasonable timeouts** - Prevent hanging services
5. **Different options per environment** - Dev vs Prod settings

## Quick Reference

```go
type ServiceProviderOptions struct {
    // Validate all services on build
    ValidateOnBuild bool

    // Called after successful resolution
    OnServiceResolved func(serviceType reflect.Type, instance any, duration time.Duration)

    // Called on resolution error
    OnServiceError func(serviceType reflect.Type, err error)

    // Timeout for resolution
    ResolutionTimeout time.Duration
}
```

Use options to add observability and safety to your dependency injection!
