# Installation

## Requirements

- Go 1.21 or later

## Install

```bash
go get github.com/junioryono/godi/v4
```

## Module Support

godi requires Go modules. If you're starting a new project:

```bash
go mod init myproject
go get github.com/junioryono/godi/v4
```

## IDE Support

godi uses Go generics extensively. Ensure your IDE supports Go 1.21+ for the best experience:

- **VS Code**: Install the official Go extension
- **GoLand**: Version 2021.3 or later
- **Vim/Neovim**: Use gopls v0.11.0 or later

## Next Steps

- Learn about [Core Concepts](core-concepts.md)
- Explore [Service Lifetimes](service-lifetimes.md)
