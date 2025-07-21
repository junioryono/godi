# Installing godi

Get up and running in 30 seconds.

## Requirements

- Go 1.21 or later
- That's it!

## Install

```bash
go get github.com/junioryono/godi
```

## Verify Installation

Create `main.go`:

```go
package main

import (
    "fmt"
    "github.com/junioryono/godi/v2"
)

func main() {
    // Create a simple module
    appModule := godi.NewModule("app",
        godi.AddSingleton(func() string {
            return "Hello from godi!"
        }),
    )

    // Build container
    services := godi.NewServiceCollection()
    services.AddModules(appModule)

    provider, _ := services.BuildServiceProvider()
    defer provider.Close()

    // Use it
    message, _ := godi.Resolve[string](provider)
    fmt.Println(message)
}
```

Run it:

```bash
go run main.go
# Output: Hello from godi!
```

Success! You're ready to use godi.

## VS Code Setup (Recommended)

For the best experience:

1. Install the [Go extension](https://marketplace.visualstudio.com/items?itemName=golang.go)
2. You'll get:
   - Auto-completion for godi functions
   - Type checking
   - Quick documentation

## What's Next?

- **New to DI?** Read [Why Dependency Injection?](why-di.md)
- **Ready to code?** Jump to [Quick Start](../tutorials/quick-start.md)
- **Building a web app?** See [Getting Started](../tutorials/getting-started.md)

## Troubleshooting

### "Module not found"

Make sure you're in a Go module:

```bash
go mod init myapp
go get github.com/junioryono/godi
```

### "Cannot find package"

Update your Go version:

```bash
go version  # Should be 1.21+
```

### IDE not recognizing godi

Restart your IDE after installation, or run:

```bash
go mod download
```

That's all! godi has zero runtime dependencies (except the excellent [dig](https://github.com/uber-go/dig) which is included).
