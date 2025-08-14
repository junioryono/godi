# Testing with godi

Testing with dependency injection is a game-changer. No more complex setups, no more slow tests, just fast and reliable unit tests.

## Why DI Makes Testing Amazing

**Without DI**: Tests are painful

- Real database connections
- Complex test fixtures
- Slow test suites
- Flaky tests due to external dependencies

**With DI**: Tests are a joy

- Use mocks instead of real services
- Tests run in milliseconds
- Completely isolated tests
- Easy to test edge cases

## Quick Example

Here's how simple testing becomes:

```go
// production code
type UserService struct {
    db     Database
    logger Logger
}

func NewUserService(db Database, logger Logger) *UserService {
    return &UserService{db: db, logger: logger}
}

func (s *UserService) GetUser(id string) (*User, error) {
    s.logger.Log("Getting user " + id)
    return s.db.FindUser(id)
}

// test code
func TestUserService_GetUser(t *testing.T) {
    // Create test module with mocks
    testModule := godi.NewModule("test",
        godi.AddSingleton(func() Database {
            return &MockDatabase{
                users: map[string]*User{
                    "123": {ID: "123", Name: "Alice"},
                },
            }
        }),
        godi.AddSingleton(func() Logger {
            return &MockLogger{}
        }),
        godi.AddScoped(NewUserService),
    )

    // Build provider
    services := godi.NewServiceCollection()
    services.AddModules(testModule)
    provider, _ := services.Build()
    defer provider.Close()

    // Test!
    service, _ := godi.Resolve[*UserService](provider)
    user, err := service.GetUser("123")

    assert.NoError(t, err)
    assert.Equal(t, "Alice", user.Name)
}
```

## Step-by-Step Guide

### Step 1: Define Interfaces

First, use interfaces for your dependencies:

```go
// interfaces.go
type Database interface {
    FindUser(id string) (*User, error)
    SaveUser(user *User) error
}

type Logger interface {
    Log(message string)
    Error(message string)
}

type EmailClient interface {
    Send(to, subject, body string) error
}
```

### Step 2: Create Mock Implementations

```go
// mocks/database.go
type MockDatabase struct {
    users     map[string]*User
    saveError error // Control errors
}

func NewMockDatabase() *MockDatabase {
    return &MockDatabase{
        users: make(map[string]*User),
    }
}

func (m *MockDatabase) FindUser(id string) (*User, error) {
    user, ok := m.users[id]
    if !ok {
        return nil, errors.New("user not found")
    }
    return user, nil
}

func (m *MockDatabase) SaveUser(user *User) error {
    if m.saveError != nil {
        return m.saveError
    }
    m.users[user.ID] = user
    return nil
}

// mocks/logger.go
type MockLogger struct {
    messages []string
}

func (m *MockLogger) Log(message string) {
    m.messages = append(m.messages, message)
}

func (m *MockLogger) Error(message string) {
    m.messages = append(m.messages, "ERROR: " + message)
}

// mocks/email.go
type MockEmailClient struct {
    sentEmails []SentEmail
    shouldFail bool
}

type SentEmail struct {
    To      string
    Subject string
    Body    string
}

func (m *MockEmailClient) Send(to, subject, body string) error {
    if m.shouldFail {
        return errors.New("email failed")
    }
    m.sentEmails = append(m.sentEmails, SentEmail{to, subject, body})
    return nil
}
```

### Step 3: Create Test Modules

Organize your test dependencies:

```go
// testutil/modules.go
package testutil

import "github.com/junioryono/godi/v3"

// Basic test module with mocks
func NewTestModule() godi.ModuleOption {
    return godi.NewModule("test-base",
        godi.AddSingleton(func() Database {
            return NewMockDatabase()
        }),
        godi.AddSingleton(func() Logger {
            return &MockLogger{}
        }),
        godi.AddSingleton(func() EmailClient {
            return &MockEmailClient{}
        }),
    )
}

// Test module with preset data
func NewTestModuleWithData(users []*User) godi.ModuleOption {
    return godi.NewModule("test-with-data",
        godi.AddSingleton(func() Database {
            db := NewMockDatabase()
            for _, user := range users {
                db.users[user.ID] = user
            }
            return db
        }),
        godi.AddSingleton(func() Logger {
            return &MockLogger{}
        }),
    )
}

// Test module for error scenarios
func NewErrorTestModule() godi.ModuleOption {
    return godi.NewModule("test-errors",
        godi.AddSingleton(func() Database {
            return &MockDatabase{
                saveError: errors.New("database error"),
            }
        }),
        godi.AddSingleton(func() EmailClient {
            return &MockEmailClient{
                shouldFail: true,
            }
        }),
    )
}
```

### Step 4: Write Your Tests

Now testing is easy and clean:

```go
// user_service_test.go
func TestUserService_CreateUser(t *testing.T) {
    // Arrange
    testModule := godi.NewModule("test",
        testutil.NewTestModule(),
        godi.AddScoped(NewUserService),
    )

    services := godi.NewServiceCollection()
    services.AddModules(testModule)
    provider, _ := services.Build()
    defer provider.Close()

    // Act
    service, _ := godi.Resolve[*UserService](provider)
    err := service.CreateUser("Alice", "alice@example.com")

    // Assert
    assert.NoError(t, err)

    // Verify mock was called
    db, _ := godi.Resolve[Database](provider)
    mockDB := db.(*MockDatabase)
    assert.Contains(t, mockDB.users, "alice")
}

func TestUserService_CreateUser_DatabaseError(t *testing.T) {
    // Use error module
    testModule := godi.NewModule("test",
        testutil.NewErrorTestModule(),
        godi.AddScoped(NewUserService),
    )

    services := godi.NewServiceCollection()
    services.AddModules(testModule)
    provider, _ := services.Build()
    defer provider.Close()

    service, _ := godi.Resolve[*UserService](provider)
    err := service.CreateUser("Alice", "alice@example.com")

    assert.Error(t, err)
    assert.Contains(t, err.Error(), "database error")
}
```

## Advanced Testing Patterns

### Table-Driven Tests with DI

```go
func TestUserService_Validation(t *testing.T) {
    tests := []struct {
        name      string
        username  string
        email     string
        wantError string
    }{
        {
            name:      "valid user",
            username:  "alice",
            email:     "alice@example.com",
            wantError: "",
        },
        {
            name:      "empty username",
            username:  "",
            email:     "alice@example.com",
            wantError: "username required",
        },
        {
            name:      "invalid email",
            username:  "alice",
            email:     "not-an-email",
            wantError: "invalid email",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Fresh provider for each test
            testModule := godi.NewModule("test",
                testutil.NewTestModule(),
                godi.AddScoped(NewUserService),
            )

            services := godi.NewServiceCollection()
            services.AddModules(testModule)
            provider, _ := services.Build()
            defer provider.Close()

            service, _ := godi.Resolve[*UserService](provider)
            err := service.CreateUser(tt.username, tt.email)

            if tt.wantError == "" {
                assert.NoError(t, err)
            } else {
                assert.Error(t, err)
                assert.Contains(t, err.Error(), tt.wantError)
            }
        })
    }
}
```

### Testing with Scopes

```go
func TestConcurrentRequests(t *testing.T) {
    // Shared infrastructure
    appModule := godi.NewModule("app",
        godi.AddSingleton(func() Database {
            return NewMockDatabase()
        }),
        godi.AddScoped(NewUserService),
        godi.AddScoped(NewRequestContext),
    )

    services := godi.NewServiceCollection()
    services.AddModules(appModule)
    provider, _ := services.Build()
    defer provider.Close()

    // Simulate concurrent requests
    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(requestID int) {
            defer wg.Done()

            // Each request gets its own scope
            ctx := context.WithValue(context.Background(), "requestID", requestID)
            scope := provider.CreateScope(ctx)
            defer scope.Close()

            service, _ := godi.Resolve[*UserService](scope)
            // Each request has isolated instances
            service.DoWork()
        }(i)
    }

    wg.Wait()
}
```

### Spy Pattern for Behavior Verification

```go
type SpyEmailClient struct {
    MockEmailClient
    CallCount   int
    LastTo      string
    LastSubject string
}

func (s *SpyEmailClient) Send(to, subject, body string) error {
    s.CallCount++
    s.LastTo = to
    s.LastSubject = subject
    return s.MockEmailClient.Send(to, subject, body)
}

func TestUserService_SendsWelcomeEmail(t *testing.T) {
    spy := &SpyEmailClient{}

    testModule := godi.NewModule("test",
        godi.AddSingleton(func() EmailClient { return spy }),
        godi.AddScoped(NewUserService),
    )

    // ... setup provider ...

    service, _ := godi.Resolve[*UserService](provider)
    service.CreateUser("alice", "alice@example.com")

    assert.Equal(t, 1, spy.CallCount)
    assert.Equal(t, "alice@example.com", spy.LastTo)
    assert.Equal(t, "Welcome!", spy.LastSubject)
}
```

## Testing Best Practices

### 1. Use Test Helpers

```go
// testutil/di.go
func BuildTestProvider(t *testing.T, modules ...godi.ModuleOption) godi.ServiceProvider {
    services := godi.NewServiceCollection()

    // Always include base test module
    allModules := append([]godi.ModuleOption{NewTestModule()}, modules...)

    err := services.AddModules(allModules...)
    require.NoError(t, err)

    provider, err := services.Build()
    require.NoError(t, err)

    t.Cleanup(func() {
        provider.Close()
    })

    return provider
}

// Usage
func TestSomething(t *testing.T) {
    provider := BuildTestProvider(t,
        godi.AddScoped(NewUserService),
    )

    service, _ := godi.Resolve[*UserService](provider)
    // Test...
}
```

### 2. Test Module Variants

```go
// Different scenarios
var HappyPathModule = godi.NewModule("happy", ...)
var ErrorModule = godi.NewModule("errors", ...)
var SlowNetworkModule = godi.NewModule("slow", ...)

func TestUserService_Scenarios(t *testing.T) {
    scenarios := []struct {
        name   string
        module godi.ModuleOption
        check  func(t *testing.T, service *UserService)
    }{
        {
            name:   "happy path",
            module: HappyPathModule,
            check: func(t *testing.T, s *UserService) {
                err := s.CreateUser("alice", "alice@example.com")
                assert.NoError(t, err)
            },
        },
        {
            name:   "database error",
            module: ErrorModule,
            check: func(t *testing.T, s *UserService) {
                err := s.CreateUser("alice", "alice@example.com")
                assert.Error(t, err)
            },
        },
    }

    for _, sc := range scenarios {
        t.Run(sc.name, func(t *testing.T) {
            provider := BuildTestProvider(t,
                sc.module,
                godi.AddScoped(NewUserService),
            )

            service, _ := godi.Resolve[*UserService](provider)
            sc.check(t, service)
        })
    }
}
```

### 3. Integration Test Support

```go
// Can gradually replace mocks with real services
func IntegrationTestModule(useRealDB bool) godi.ModuleOption {
    return godi.NewModule("integration",
        godi.AddSingleton(func() Database {
            if useRealDB {
                return NewPostgresDatabase("postgres://test...")
            }
            return NewMockDatabase()
        }),
        godi.AddSingleton(func() Logger {
            return NewLogger() // Always use real logger
        }),
    )
}
```

## Summary

Testing with godi gives you:

✅ **Fast tests** - No real dependencies
✅ **Isolated tests** - Each test is independent  
✅ **Easy setup** - Modules make it simple
✅ **Flexible mocking** - Control every scenario
✅ **Better coverage** - Easy to test edge cases

The key is to start simple: create basic mocks, use modules to organize them, and let godi handle the wiring. Your tests will be cleaner, faster, and more maintainable.
