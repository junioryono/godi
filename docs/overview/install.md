# Installing godi

godi is distributed as a Go module with zero runtime dependencies (besides the excellent [dig](https://github.com/uber-go/dig) which is bundled).

## Requirements

- Go 1.21 or later
- A Go module (recommended)

## Installation

### Using Go Modules (Recommended)

Add godi to your project:

```bash
go get github.com/junioryono/godi
```

This will add godi to your `go.mod` file:

```go
go get github.com/junioryono/godi@{sub}`version`
```

### Import in Your Code

```go
import "github.com/junioryono/godi"
```

## Verify Installation

Create a simple test file to verify the installation:

```go
package main

import (
    "fmt"
    "github.com/junioryono/godi"
)

func main() {
    collection := godi.NewServiceCollection()
    fmt.Println("godi installed successfully!")
    fmt.Printf("Collection type: %T\n", collection)
}
```

Run it:

```bash
go run main.go
```

## Version Management

### Check Current Version

```bash
go list -m github.com/junioryono/godi
```

### Update to Latest

```bash
go get -u github.com/junioryono/godi
```

### Use Specific Version

```bash
go get github.com/junioryono/godi@{sub}`version`
```

## Development Setup

If you want to contribute to godi:

```bash
# Clone the repository
git clone https://github.com/junioryono/godi.git
cd godi

# Install dependencies
go mod download

# Run tests
go test ./...

# Run tests with coverage
go test -cover ./...
```

## Editor Support

godi works great with any Go development environment:

- **VS Code**: Install the official Go extension
- **GoLand**: Full support out of the box
- **Vim/Neovim**: Use gopls for LSP support
- **Emacs**: Use go-mode or lsp-mode

The type-safe generic helpers in godi provide excellent IDE support with auto-completion and type checking.

## Next Steps

- Read [Why Dependency Injection?](why-di.md) to understand the benefits
- Follow the [Getting Started Tutorial](../tutorials/getting-started.md)
- Explore [Core Concepts](concepts.md)
