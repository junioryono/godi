# Installation

## Install godi

```bash
go get github.com/junioryono/godi/v4
```

## Verify It Works

Create a file called `main.go`:

```go
package main

import (
    "fmt"
    "github.com/junioryono/godi/v4"
)

func main() {
    services := godi.NewCollection()
    fmt.Println("godi is ready!")
}
```

Run it:

```bash
go run main.go
```

You should see:

```
godi is ready!
```

## Requirements

- **Go 1.21+** - godi uses generics for type safety
- **No code generation** - godi works at runtime, no build steps needed
- **No dependencies** - the core library has zero external dependencies

## Framework Integrations (Optional)

If you're using a web framework, install the corresponding integration:

```bash
# For Gin
go get github.com/junioryono/godi/v4/gin

# For Chi
go get github.com/junioryono/godi/v4/chi

# For Echo
go get github.com/junioryono/godi/v4/echo

# For Fiber
go get github.com/junioryono/godi/v4/fiber

# For net/http
go get github.com/junioryono/godi/v4/http
```

---

**Next:** [Create your first container](02-first-container.md)
