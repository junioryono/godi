# Testing Guide

Testing with godi is simple and powerful. Replace real services with mocks instantly, run tests in milliseconds, and achieve better coverage with less effort.

## The Basic Pattern

```go
func TestUserService(t *testing.T) {
    // 1. Create test module with mocks
    testModule := godi.NewModule("test",
        godi.AddSingleton(func() *Database {
            return &MockDatabase{
                users: []User{{ID: "1", Name: "Alice"}},
            }
        }),
        godi.AddSingleton(func() *Logger {
            return &MockLogger{}
        }),
        godi.AddScoped(NewUserService),  // Real service, mock dependencies
    )

    // 2. Build provider
    collection := godi.NewCollection()
    collection.AddModules(testModule)
    provider, _ := collection.Build()
    defer provider.Close()

    // 3. Test!
    service, _ := godi.Resolve[*UserService](provider)
    user, err := service.GetUser("1")

    assert.NoError(t, err)
    assert.Equal(t, "Alice", user.Name)
}
```

## Creating Mocks

### Simple Mock

```go
type MockDatabase struct {
    users map[string]*User
}

func (m *MockDatabase) GetUser(id string) (*User, error) {
    user, ok := m.users[id]
    if !ok {
        return nil, errors.New("not found")
    }
    return user, nil
}

func (m *MockDatabase) SaveUser(user *User) error {
    m.users[user.ID] = user
    return nil
}
```

### Mock with Behavior Control

```go
type MockEmailService struct {
    shouldFail   bool
    sentEmails   []Email
}

func (m *MockEmailService) Send(to, subject, body string) error {
    if m.shouldFail {
        return errors.New("email service down")
    }

    m.sentEmails = append(m.sentEmails, Email{
        To:      to,
        Subject: subject,
        Body:    body,
    })
    return nil
}
```

### Spy Pattern for Verification

```go
type SpyLogger struct {
    messages []string
    callCount int
}

func (s *SpyLogger) Log(msg string) {
    s.callCount++
    s.messages = append(s.messages, msg)
}

// In your test
func TestLogging(t *testing.T) {
    spy := &SpyLogger{}

    testModule := godi.NewModule("test",
        godi.AddSingleton(func() Logger {
            return spy
        }),
        godi.AddScoped(NewUserService),
    )

    // ... run test ...

    // Verify behavior
    assert.Equal(t, 2, spy.callCount)
    assert.Contains(t, spy.messages, "User created")
}
```

## Test Helpers

Create reusable test utilities:

```go
// testutil/helpers.go
package testutil

import (
    "testing"
    "github.com/junioryono/godi/v4"
    "github.com/stretchr/testify/require"
)

// BuildTestProvider creates a provider and ensures cleanup
func BuildTestProvider(t *testing.T, modules ...godi.ModuleOption) godi.Provider {
    collection := godi.NewCollection()

    err := collection.AddModules(modules...)
    require.NoError(t, err)

    provider, err := collection.Build()
    require.NoError(t, err)

    t.Cleanup(func() {
        provider.Close()
    })

    return provider
}

// MockDatabaseModule returns a module with a mock database
func MockDatabaseModule(users []User) godi.ModuleOption {
    return godi.NewModule("mock-db",
        godi.AddSingleton(func() *Database {
            db := &MockDatabase{
                users: make(map[string]*User),
            }
            for _, u := range users {
                db.users[u.ID] = &u
            }
            return db
        }),
    )
}
```

Use the helpers:

```go
func TestWithHelpers(t *testing.T) {
    users := []User{
        {ID: "1", Name: "Alice"},
        {ID: "2", Name: "Bob"},
    }

    provider := testutil.BuildTestProvider(t,
        testutil.MockDatabaseModule(users),
        godi.AddScoped(NewUserService),
    )

    service, _ := godi.Resolve[*UserService](provider)
    // Test...
}
```

## Table-Driven Tests

```go
func TestUserValidation(t *testing.T) {
    tests := []struct {
        name      string
        username  string
        email     string
        wantError bool
        errorMsg  string
    }{
        {
            name:      "valid user",
            username:  "alice",
            email:     "alice@example.com",
            wantError: false,
        },
        {
            name:      "empty username",
            username:  "",
            email:     "alice@example.com",
            wantError: true,
            errorMsg:  "username required",
        },
        {
            name:      "invalid email",
            username:  "alice",
            email:     "not-an-email",
            wantError: true,
            errorMsg:  "invalid email",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Fresh provider for each test
            provider := testutil.BuildTestProvider(t,
                testutil.MockDatabaseModule(nil),
                godi.AddScoped(NewUserService),
            )

            service, _ := godi.Resolve[*UserService](provider)
            err := service.CreateUser(tt.username, tt.email)

            if tt.wantError {
                assert.Error(t, err)
                assert.Contains(t, err.Error(), tt.errorMsg)
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

## Testing Error Scenarios

```go
// Module for error scenarios
func ErrorModule() godi.ModuleOption {
    return godi.NewModule("errors",
        godi.AddSingleton(func() *Database {
            return &MockDatabase{
                shouldError: true,
                errorMsg:    "database connection failed",
            }
        }),
        godi.AddSingleton(func() *EmailService {
            return &MockEmailService{
                shouldFail: true,
            }
        }),
    )
}

func TestErrorHandling(t *testing.T) {
    provider := testutil.BuildTestProvider(t,
        ErrorModule(),
        godi.AddScoped(NewUserService),
    )

    service, _ := godi.Resolve[*UserService](provider)
    err := service.CreateUser("alice", "alice@example.com")

    assert.Error(t, err)
    assert.Contains(t, err.Error(), "database connection failed")
}
```

## Testing with Scopes

Test request isolation:

```go
func TestRequestIsolation(t *testing.T) {
    provider := testutil.BuildTestProvider(t,
        godi.AddSingleton(NewDatabase),
        godi.AddScoped(NewTransaction),
        godi.AddScoped(NewUserService),
    )

    // Simulate two concurrent requests
    var wg sync.WaitGroup
    wg.Add(2)

    go func() {
        defer wg.Done()

        // Request 1
        scope, _ := provider.CreateScope(context.Background())
        defer scope.Close()

        service, _ := godi.Resolve[*UserService](scope)
        tx, _ := godi.Resolve[*Transaction](scope)

        // Each request has its own transaction
        assert.NotNil(t, tx)
    }()

    go func() {
        defer wg.Done()

        // Request 2
        scope, _ := provider.CreateScope(context.Background())
        defer scope.Close()

        service, _ := godi.Resolve[*UserService](scope)
        tx, _ := godi.Resolve[*Transaction](scope)

        // Different transaction than request 1
        assert.NotNil(t, tx)
    }()

    wg.Wait()
}
```

## Integration Testing

Gradually replace mocks with real services:

```go
func IntegrationModule(useRealDB bool) godi.ModuleOption {
    return godi.NewModule("integration",
        godi.AddSingleton(func() *Database {
            if useRealDB {
                // Real database for integration tests
                return NewPostgresDatabase("postgres://test...")
            }
            // Mock for unit tests
            return &MockDatabase{}
        }),
        godi.AddSingleton(NewLogger),  // Always use real logger
        godi.AddScoped(NewUserService),
    )
}

func TestIntegration(t *testing.T) {
    if testing.Short() {
        t.Skip("Skipping integration test")
    }

    provider := testutil.BuildTestProvider(t,
        IntegrationModule(true),  // Use real database
    )

    service, _ := godi.Resolve[*UserService](provider)
    // Test with real database...
}
```

## Testing HTTP Handlers

```go
func TestHTTPHandler(t *testing.T) {
    provider := testutil.BuildTestProvider(t,
        testutil.MockDatabaseModule([]User{
            {ID: "1", Name: "Alice"},
        }),
        godi.AddScoped(NewUserService),
    )

    handler := NewUserHandler(provider)

    // Create test request
    req := httptest.NewRequest("GET", "/users/1", nil)
    w := httptest.NewRecorder()

    // Call handler
    handler.GetUser(w, req)

    // Check response
    assert.Equal(t, 200, w.Code)

    var user User
    json.Unmarshal(w.Body.Bytes(), &user)
    assert.Equal(t, "Alice", user.Name)
}
```

## Benchmark Testing

```go
func BenchmarkUserService(b *testing.B) {
    provider := testutil.BuildTestProvider(b,
        testutil.MockDatabaseModule(nil),
        godi.AddScoped(NewUserService),
    )

    service, _ := godi.Resolve[*UserService](provider)

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        service.GetUser("1")
    }
}
```

## Best Practices

### 1. Use Interfaces for Mocking

```go
// Define interfaces
type Database interface {
    GetUser(id string) (*User, error)
    SaveUser(user *User) error
}

// Easy to mock
type MockDatabase struct{}

func (m *MockDatabase) GetUser(id string) (*User, error) {
    return &User{ID: id, Name: "Mock User"}, nil
}
```

### 2. Create Test Modules for Common Scenarios

```go
// testutil/modules.go
var HappyPathModule = godi.NewModule("happy",
    MockDatabaseModule(testUsers),
    MockEmailModule(false),  // success
)

var ErrorModule = godi.NewModule("error",
    MockDatabaseModule(nil),
    MockEmailModule(true),  // fail
)

// Use in tests
provider := testutil.BuildTestProvider(t,
    testutil.HappyPathModule,
    godi.AddScoped(NewUserService),
)
```

### 3. Test One Thing at a Time

```go
// Good - focused test
func TestUserService_GetUser_NotFound(t *testing.T) {
    provider := testutil.BuildTestProvider(t,
        testutil.MockDatabaseModule(nil),  // No users
        godi.AddScoped(NewUserService),
    )

    service, _ := godi.Resolve[*UserService](provider)
    _, err := service.GetUser("999")

    assert.Error(t, err)
    assert.Equal(t, "user not found", err.Error())
}
```

### 4. Use t.Cleanup for Resources

```go
func TestWithTempFile(t *testing.T) {
    file, _ := os.CreateTemp("", "test")
    t.Cleanup(func() {
        os.Remove(file.Name())
    })

    provider := testutil.BuildTestProvider(t,
        godi.AddSingleton(func() *FileService {
            return NewFileService(file.Name())
        }),
    )

    // Test...
}
```

## Common Testing Patterns

### Assert Mock Calls

```go
type MockRepository struct {
    calls []string
}

func (m *MockRepository) Save(entity any) error {
    m.calls = append(m.calls, "Save")
    return nil
}

// In test
repo := &MockRepository{}
// ... run test ...
assert.Equal(t, []string{"Save", "Save"}, repo.calls)
```

### Test with Context

```go
func TestWithContext(t *testing.T) {
    ctx := context.WithValue(context.Background(), "userID", "123")

    provider := testutil.BuildTestProvider(t,
        testutil.MockDatabaseModule(nil),
        godi.AddScoped(NewRequestContext),  // Uses context
    )

    scope, _ := provider.CreateScope(ctx)
    defer scope.Close()

    reqCtx, _ := godi.Resolve[*RequestContext](scope)
    assert.Equal(t, "123", reqCtx.UserID)
}
```

## Summary

Testing with godi gives you:

- ✅ **Fast tests** - No real dependencies
- ✅ **Isolated tests** - Each test independent
- ✅ **Easy mocking** - Swap implementations instantly
- ✅ **Better coverage** - Easy to test edge cases
- ✅ **Reusable setup** - Test modules and helpers

The key is to use modules to organize your mocks and let godi handle the wiring!
