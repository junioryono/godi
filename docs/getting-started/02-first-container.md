# Your First Container

A container holds your application's services. You register services with a **collection**, then **build** it into a **provider** that creates instances on demand.

## The Pattern

```
Collection (registration) → Build → Provider (resolution)
```

## Step 1: Create a Collection

```go
services := godi.NewCollection()
```

The collection is where you tell godi about your services.

## Step 2: Register a Service

```go
services.AddSingleton(func() string {
    return "Hello, godi!"
})
```

This registers a `string` service. The function is called when the service is first needed.

## Step 3: Build the Provider

```go
provider, err := services.Build()
if err != nil {
    log.Fatal(err)
}
defer provider.Close()
```

Building validates your registrations and prepares the dependency graph. Always close the provider when done - it cleans up resources.

## Step 4: Resolve the Service

```go
message := godi.MustResolve[string](provider)
fmt.Println(message) // Hello, godi!
```

`MustResolve` returns the service or panics. Use `Resolve` if you want to handle errors yourself.

## Complete Example

```go
package main

import (
    "fmt"
    "log"
    "github.com/junioryono/godi/v4"
)

func main() {
    // 1. Create collection
    services := godi.NewCollection()

    // 2. Register service
    services.AddSingleton(func() string {
        return "Hello, godi!"
    })

    // 3. Build provider
    provider, err := services.Build()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // 4. Use service
    message := godi.MustResolve[string](provider)
    fmt.Println(message)
}
```

## What Just Happened?

```
┌─────────────────────────────────────────────────────┐
│                    Collection                       │
│  ┌─────────────────────────────────────────────┐    │
│  │  string → func() string { return "Hello" }  │    │
│  └─────────────────────────────────────────────┘    │
└────────────────────────┬────────────────────────────┘
                         │ Build()
                         ▼
┌─────────────────────────────────────────────────────┐
│                     Provider                        │
│  ┌─────────────────────────────────────────────┐    │
│  │  MustResolve[string]() → "Hello, godi!"     │    │
│  └─────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────┘
```

1. You registered a `string` service with a factory function
2. Building created the provider with the dependency graph
3. Resolving called your factory and returned the result

## Key Points

- **Collection** is for registration (before the app runs)
- **Provider** is for resolution (while the app runs)
- **Build** validates everything upfront - no runtime surprises
- **Close** cleans up resources when you're done

---

**Next:** [Add real services with dependencies](03-adding-services.md)
