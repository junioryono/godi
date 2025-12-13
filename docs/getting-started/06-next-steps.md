# Next Steps

You now understand the fundamentals of godi. Here's where to go next based on what you're building.

## Building a Web Application?

1. **[Web Applications Guide](../guides/web-applications.md)** - Complete patterns for production web apps
2. **[Framework Integration](../integrations/)** - Dedicated guides for Gin, Chi, Echo, Fiber, and net/http
3. **[Scopes & Isolation](../concepts/scopes.md)** - Deep dive into request isolation

## Organizing a Large Application?

1. **[Modules](../concepts/modules.md)** - Group related services for better organization
2. **[Keyed Services](../features/keyed-services.md)** - Multiple implementations of the same interface
3. **[Service Groups](../features/service-groups.md)** - Collect services for batch operations

## Simplifying Complex Constructors?

1. **[Parameter Objects](../features/parameter-objects.md)** - Automatic field injection with `In` types
2. **[Result Objects](../features/result-objects.md)** - Register multiple services from one constructor

## Testing Your Application?

1. **[Testing Guide](../guides/testing.md)** - Strategies for testing with DI
2. **[Interface Binding](../features/interface-binding.md)** - Mock implementations for testing

## Something Went Wrong?

1. **[Error Handling Guide](../guides/error-handling.md)** - Debug common issues
2. **[Lifetimes Deep Dive](../concepts/lifetimes.md)** - Understand lifetime rules

## Quick Reference

### Core Concepts

| Topic                                       | Description                            |
| ------------------------------------------- | -------------------------------------- |
| [How It Works](../concepts/how-it-works.md) | Visual guide to dependency resolution  |
| [Lifetimes](../concepts/lifetimes.md)       | Singleton, Scoped, Transient explained |
| [Scopes](../concepts/scopes.md)             | Request isolation and context          |
| [Modules](../concepts/modules.md)           | Organizing large applications          |

### Features

| Feature                                               | Use Case                              |
| ----------------------------------------------------- | ------------------------------------- |
| [Keyed Services](../features/keyed-services.md)       | Multiple implementations of same type |
| [Service Groups](../features/service-groups.md)       | Collect related services              |
| [Parameter Objects](../features/parameter-objects.md) | Simplify complex constructors         |
| [Result Objects](../features/result-objects.md)       | Multi-service registration            |
| [Interface Binding](../features/interface-binding.md) | Register concrete as interface        |
| [Resource Cleanup](../features/resource-cleanup.md)   | Automatic disposal                    |

### Integrations

| Framework | Guide                                               |
| --------- | --------------------------------------------------- |
| Gin       | [Gin Integration](../integrations/gin.md)           |
| Chi       | [Chi Integration](../integrations/chi.md)           |
| Echo      | [Echo Integration](../integrations/echo.md)         |
| Fiber     | [Fiber Integration](../integrations/fiber.md)       |
| net/http  | [net/http Integration](../integrations/net-http.md) |

## Get Help

- **[GitHub Issues](https://github.com/junioryono/godi/issues)** - Report bugs or request features
- **[API Reference](https://pkg.go.dev/github.com/junioryono/godi/v4)** - Complete API documentation

---

Happy building with godi!
