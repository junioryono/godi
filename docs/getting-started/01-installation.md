# Installation

## Install godi

```bash
go get github.com/junioryono/godi/v5
```

## Verify It Works

Create a file called `main.go`:

```go
package main

import (
    "fmt"
    "github.com/junioryono/godi/v5"
)

func main() {
    services := godi.NewCollection()
    fmt.Println("godi is ready!", services.Count())
}
```

Run it:

```bash
go run main.go
```

You should see:

```
godi is ready! 0
```

## Requirements

- **Go 1.26+** - godi uses generics for type safety
- **No code generation** - godi works at runtime, no build steps needed
- **No dependencies** - the core library has zero external dependencies

## Framework Integrations (Optional)

If you're using a web framework, install the corresponding integration:

```bash
# For Gin
go get github.com/junioryono/godi/gin/v5

# For Chi
go get github.com/junioryono/godi/chi/v5

# For Echo v4 / Echo v5
go get github.com/junioryono/godi/echo/v5
go get github.com/junioryono/godi/echov5/v5

# For Fiber v2 / Fiber v3
go get github.com/junioryono/godi/fiber/v5
go get github.com/junioryono/godi/fiberv3/v5

# For net/http
go get github.com/junioryono/godi/http/v5
```

---

**Next:** [Create your first container](02-first-container.md)
