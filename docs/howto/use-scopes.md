# Using Scopes

Scopes are one of the most powerful features in godi, enabling proper lifecycle management for services in scenarios like web requests, background jobs, or any bounded operation. This guide shows you how to effectively use scopes.

## What is a Scope?

A scope creates a boundary for service instances. Services with a **Scoped** lifetime are created once per scope and shared within that scope. When the scope is disposed, all scoped services are cleaned up.

```go
// Create a scope
scope := provider.CreateScope(context.Background())
defer scope.Close() // Always close scopes!

// Services resolved from this scope share scoped instances
service1, _ := godi.Resolve[MyService](scope.ServiceProvider())
service2, _ := godi.Resolve[MyService](scope.ServiceProvider())
// service1 == service2 (same instance within scope)
```

## Web Request Scoping

The most common use case for scopes is handling web requests:

### HTTP Middleware Pattern

```go
func DIMiddleware(provider godi.ServiceProvider) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Create a scope for this request
            scope := provider.CreateScope(r.Context())
            defer scope.Close()

            // Add scope to request context
            ctx := context.WithValue(r.Context(), "scope", scope)

            // Continue with scoped services
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

// Using in a handler
func UserHandler(w http.ResponseWriter, r *http.Request) {
    scope := r.Context().Value("scope").(godi.Scope)

    userService, err := godi.Resolve[UserService](scope.ServiceProvider())
    if err != nil {
        http.Error(w, "Service error", http.StatusInternalServerError)
        return
    }
```
