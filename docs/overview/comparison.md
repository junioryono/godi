# Comparison with Other DI Solutions

This guide compares godi with other dependency injection solutions in the Go ecosystem, helping you choose the right tool for your project.

## Comparison Table

| Feature                | godi                          | wire               | fx             | dig        | Manual DI       |
| ---------------------- | ----------------------------- | ------------------ | -------------- | ---------- | --------------- |
| **Type Safety**        | ✅ Compile-time with generics | ✅ Code generation | ✅ Runtime     | ✅ Runtime | ✅ Compile-time |
| **Runtime/Compile**    | Runtime                       | Compile-time       | Runtime        | Runtime    | Manual          |
| **Scoped Lifetimes**   | ✅ Full support               | ❌ No              | ✅ Via modules | ❌ No      | Manual          |
| **Service Lifetimes**  | Singleton, Scoped, Transient  | Singleton only     | Singleton      | Singleton  | Manual          |
| **Learning Curve**     | Low (familiar API)            | Medium             | High           | Medium     | Low             |
| **Boilerplate**        | Minimal                       | Some               | Moderate       | Minimal    | High            |
| **Testing**            | Excellent                     | Good               | Good           | Good       | Depends         |
| **Performance**        | Good                          | Excellent          | Good           | Good       | Excellent       |
| **Microsoft DI Style** | ✅ Yes                        | ❌ No              | ❌ No          | ❌ No      | ❌ No           |

## Detailed Comparisons

### godi vs wire (Google)

**Wire** uses code generation to create dependency injection code at compile time.

```go
// Wire
//+build wireinject

func InitializeApp() (*App, error) {
    wire.Build(NewConfig, NewDatabase, NewService, NewApp)
    return nil, nil // Wire will generate this
}
```

```go
// godi
services := godi.NewServiceCollection()
services.AddSingleton(NewConfig)
services.AddSingleton(NewDatabase)
services.AddScoped(NewService)
services.AddScoped(NewApp)

provider, _ := services.BuildServiceProvider()
app, _ := godi.Resolve[*App](provider)
```

**Key Differences:**

- **Wire** generates code at compile time (faster runtime)
- **godi** resolves at runtime (more flexible)
- **Wire** requires buil
