# Testing with godi

Dependency injection makes testing easier. Replace real implementations with mocks, test services in isolation, and verify behavior.

## Testing Strategies

### 1. Unit Testing with Mocks

Test a service by injecting mock dependencies:

```go
// service.go
type UserService struct {
    repo   UserRepository
    logger Logger
}

func NewUserService(repo UserRepository, logger Logger) *UserService {
    return &UserService{repo: repo, logger: logger}
}

func (s *UserService) GetUser(ctx context.Context, id int) (*User, error) {
    s.logger.Debug("fetching user", "id", id)
    return s.repo.GetByID(ctx, id)
}
```

```go
// service_test.go
type mockRepository struct {
    users map[int]*User
    err   error
}

func (m *mockRepository) GetByID(ctx context.Context, id int) (*User, error) {
    if m.err != nil {
        return nil, m.err
    }
    return m.users[id], nil
}

type mockLogger struct{}

func (m *mockLogger) Debug(msg string, args ...any) {}

func TestUserService_GetUser(t *testing.T) {
    // Arrange
    repo := &mockRepository{
        users: map[int]*User{
            1: {ID: 1, Name: "Alice"},
        },
    }
    logger := &mockLogger{}
    service := NewUserService(repo, logger)

    // Act
    user, err := service.GetUser(context.Background(), 1)

    // Assert
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if user.Name != "Alice" {
        t.Errorf("expected Alice, got %s", user.Name)
    }
}
```

### 2. Integration Testing with Test Container

Create a test provider with some real, some mock services:

```go
func TestUserService_Integration(t *testing.T) {
    services := godi.NewCollection()

    // Real services
    services.AddSingleton(func() *Config {
        return &Config{DatabaseURL: "postgres://test:test@localhost/test"}
    })
    services.AddSingleton(NewDatabase)
    services.AddScoped(NewUserRepository)

    // Mock logger for quiet tests
    services.AddSingleton(func() Logger {
        return &mockLogger{}
    })

    services.AddScoped(NewUserService)

    provider, err := services.Build()
    if err != nil {
        t.Fatal(err)
    }
    defer provider.Close()

    // Create scope and test
    scope, _ := provider.CreateScope(context.Background())
    defer scope.Close()

    service := godi.MustResolve[*UserService](scope)
    // ... test with real database
}
```

### 3. HTTP Handler Testing

Test handlers with request scopes:

```go
func TestUserHandler_List(t *testing.T) {
    services := godi.NewCollection()

    // Mock repository with test data
    services.AddScoped(func() *UserRepository {
        return &mockUserRepository{
            users: []User{{ID: 1, Name: "Alice"}},
        }
    })
    services.AddScoped(NewRequestContext)
    services.AddScoped(NewUserService)
    services.AddScoped(NewUserController)

    provider, _ := services.Build()
    defer provider.Close()

    // Create handler
    mux := http.NewServeMux()
    mux.HandleFunc("GET /users", godihttp.Handle((*UserController).List))
    handler := godihttp.ScopeMiddleware(provider)(mux)

    // Make request
    req := httptest.NewRequest("GET", "/users", nil)
    rec := httptest.NewRecorder()
    handler.ServeHTTP(rec, req)

    // Assert
    if rec.Code != http.StatusOK {
        t.Errorf("expected 200, got %d", rec.Code)
    }

    var users []User
    json.NewDecoder(rec.Body).Decode(&users)
    if len(users) != 1 {
        t.Errorf("expected 1 user, got %d", len(users))
    }
}
```

## Test Modules

Create reusable test modules:

```go
// test/modules.go
package test

import "github.com/junioryono/godi/v4"

// MockInfrastructureModule provides mocks for all infrastructure
func MockInfrastructureModule() godi.Module {
    return func(services *godi.ServiceCollection) {
        services.AddSingleton(func() *Config {
            return &Config{Debug: true}
        })
        services.AddSingleton(func() Logger {
            return &mockLogger{}
        })
    }
}

// TestDatabaseModule provides a test database
func TestDatabaseModule(url string) godi.Module {
    return func(services *godi.ServiceCollection) {
        services.AddSingleton(func() (*Database, error) {
            return NewDatabase(&Config{DatabaseURL: url})
        })
    }
}
```

Use in tests:

```go
func TestWithModules(t *testing.T) {
    services := godi.NewCollection()
    services.AddModule(test.MockInfrastructureModule())
    services.AddModule(test.TestDatabaseModule("postgres://test@localhost/test"))
    services.AddModule(users.Module())

    provider, _ := services.Build()
    defer provider.Close()
    // ...
}
```

## Interface-Based Mocking

Design for testability with interfaces:

```go
// Define interface
type UserRepository interface {
    GetByID(ctx context.Context, id int) (*User, error)
    Create(ctx context.Context, user *User) error
}

// Real implementation
type postgresUserRepository struct {
    db *Database
}

func NewUserRepository(db *Database) UserRepository {
    return &postgresUserRepository{db: db}
}

// Register with interface
services.AddScoped(NewUserRepository)
```

```go
// In tests, replace with mock
type mockUserRepository struct {
    users    map[int]*User
    created  []*User
    getError error
}

func (m *mockUserRepository) GetByID(ctx context.Context, id int) (*User, error) {
    if m.getError != nil {
        return nil, m.getError
    }
    return m.users[id], nil
}

func (m *mockUserRepository) Create(ctx context.Context, user *User) error {
    m.created = append(m.created, user)
    return nil
}

// Register mock
services.AddScoped(func() UserRepository {
    return &mockUserRepository{
        users: map[int]*User{1: {ID: 1, Name: "Test"}},
    }
})
```

## Testing Error Paths

Test how services handle failures:

```go
func TestUserService_GetUser_NotFound(t *testing.T) {
    services := godi.NewCollection()

    services.AddScoped(func() UserRepository {
        return &mockUserRepository{
            getError: sql.ErrNoRows,
        }
    })
    services.AddSingleton(func() Logger { return &mockLogger{} })
    services.AddScoped(NewUserService)

    provider, _ := services.Build()
    scope, _ := provider.CreateScope(context.Background())
    defer scope.Close()

    service := godi.MustResolve[*UserService](scope)
    _, err := service.GetUser(context.Background(), 999)

    if err != sql.ErrNoRows {
        t.Errorf("expected ErrNoRows, got %v", err)
    }
}
```

## Table-Driven Tests

Combine with table-driven tests:

```go
func TestUserService_GetUser_Cases(t *testing.T) {
    cases := []struct {
        name      string
        userID    int
        mockUsers map[int]*User
        mockError error
        wantName  string
        wantErr   bool
    }{
        {
            name:      "found",
            userID:    1,
            mockUsers: map[int]*User{1: {ID: 1, Name: "Alice"}},
            wantName:  "Alice",
        },
        {
            name:      "not found",
            userID:    999,
            mockUsers: map[int]*User{},
            wantErr:   true,
        },
        {
            name:      "database error",
            userID:    1,
            mockError: errors.New("connection lost"),
            wantErr:   true,
        },
    }

    for _, tc := range cases {
        t.Run(tc.name, func(t *testing.T) {
            services := godi.NewCollection()
            services.AddScoped(func() UserRepository {
                return &mockUserRepository{
                    users:    tc.mockUsers,
                    getError: tc.mockError,
                }
            })
            services.AddSingleton(func() Logger { return &mockLogger{} })
            services.AddScoped(NewUserService)

            provider, _ := services.Build()
            scope, _ := provider.CreateScope(context.Background())
            defer scope.Close()

            service := godi.MustResolve[*UserService](scope)
            user, err := service.GetUser(context.Background(), tc.userID)

            if tc.wantErr && err == nil {
                t.Error("expected error")
            }
            if !tc.wantErr && err != nil {
                t.Errorf("unexpected error: %v", err)
            }
            if !tc.wantErr && user.Name != tc.wantName {
                t.Errorf("expected %s, got %s", tc.wantName, user.Name)
            }
        })
    }
}
```

## Best Practices

1. **Use interfaces** for dependencies you need to mock
2. **Create test modules** for reusable mock configurations
3. **Test error paths** not just happy paths
4. **Close providers and scopes** to avoid resource leaks
5. **Isolate tests** - each test should create its own provider

---

**Next:** Learn about [debugging errors](error-handling.md)
