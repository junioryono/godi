# Get Started in 5 Minutes

godi is a dependency injection library that automatically wires up your Go applications. Define your services, specify their lifetimes, and let godi handle the rest.

**What you'll learn:**

1. **Create a container** - Where your services live
2. **Register services** - Tell godi about your types
3. **Resolve dependencies** - Let godi wire everything together
4. **Use lifetimes** - Control when instances are created
5. **Add HTTP integration** - Build web applications

## Before You Start

You need:

- Go 1.21 or later
- A text editor
- 5 minutes

## Tutorial Overview

| Page                                       | Time   | What You'll Build          |
| ------------------------------------------ | ------ | -------------------------- |
| [Installation](01-installation.md)         | 30 sec | Install godi               |
| [First Container](02-first-container.md)   | 60 sec | Create and use a container |
| [Adding Services](03-adding-services.md)   | 90 sec | Wire up real services      |
| [Using Lifetimes](04-using-lifetimes.md)   | 90 sec | Control instance creation  |
| [HTTP Integration](05-http-integration.md) | 90 sec | Build a web server         |
| [Next Steps](06-next-steps.md)             | 30 sec | Where to go from here      |

## Quick Preview

Here's what a complete godi application looks like:

```go
package main

import (
    "fmt"
    "github.com/junioryono/godi/v4"
)

// Your services - normal Go types
type Logger struct{}
func (l *Logger) Log(msg string) { fmt.Println(msg) }

type UserService struct {
    logger *Logger
}
func NewUserService(logger *Logger) *UserService {
    return &UserService{logger: logger}
}

func main() {
    // Register services
    services := godi.NewCollection()
    services.AddSingleton(func() *Logger { return &Logger{} })
    services.AddSingleton(NewUserService)

    // Build and use
    provider, _ := services.Build()
    defer provider.Close()

    // godi automatically wires Logger into UserService
    users := godi.MustResolve[*UserService](provider)
    users.logger.Log("Hello, godi!")
}
```

Ready? Let's start with [installation](01-installation.md).
