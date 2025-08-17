# Modules Guide

Modules are the best way to organize your services in godi. They group related functionality, make dependencies explicit, and enable easy testing.

## Why Use Modules?

Even for small apps, modules keep your code organized:

```go
// Without modules - messy main function
func main() {
    collection := godi.NewCollection()
    collection.AddSingleton(NewDatabase)
    collection.AddSingleton(NewLogger)
    collection.AddSingleton(NewConfig)
    collection.AddSingleton(NewCache)
    collection.AddScoped(NewUserRepository)
    collection.AddScoped(NewUserService)
    collection.AddScoped(NewAuthService)
    // ... 20 more lines
}

// With modules - clean and organized
func main() {
    collection := godi.NewCollection()
    collection.AddModules(infrastructure.Module)
    collection.AddModules(repository.Module)
    collection.AddModules(service.Module)
}
```

## Basic Module

Start simple:

```go
// repository/module.go
package repository

import "github.com/junioryono/godi/v4"

var Module = godi.NewModule("repository",
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewMatchRepository),
    godi.AddScoped(NewTeamRepository),
)
```

Use it:

```go
// main.go
import "myapp/repository"

func main() {
    collection := godi.NewCollection()
    collection.AddModules(repository.Module)

    provider, _ := collection.Build()
    // ...
}
```

## Module Dependencies

Modules can include other modules:

```go
// core/module.go - Basic infrastructure
var CoreModule = godi.NewModule("core",
    godi.AddSingleton(NewConfig),
    godi.AddSingleton(NewLogger),
)

// database/module.go - Database services
var DatabaseModule = godi.NewModule("database",
    CoreModule,  // Depends on core
    godi.AddSingleton(NewDatabaseConnection),
    godi.AddScoped(NewTransaction),
)

// user/module.go - User features
var UserModule = godi.NewModule("user",
    DatabaseModule,  // Depends on database (which includes core)
    godi.AddScoped(NewUserRepository),
    godi.AddScoped(NewUserService),
)

// app/module.go - Complete application
var AppModule = godi.NewModule("app",
    UserModule,
    AuthModule,
    // All dependencies are included automatically
)
```
