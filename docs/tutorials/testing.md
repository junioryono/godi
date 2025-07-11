# Testing with godi

One of the greatest benefits of dependency injection is how it transforms testing. This tutorial shows you how to write comprehensive tests for applications using godi.

## Why DI Makes Testing Better

Without DI, testing often involves:

- Complex setup with real databases
- Slow tests due to external dependencies
- Brittle tests that fail due to network issues
- Difficulty isolating components

With godi:

- Easy mock injection
- Fast, isolated unit tests
- Reliable and repeatable
- Clear test boundaries

## Setting Up

We'll test the blog API from the [Web Application Tutorial](web-application.md). First, create a test utilities package:

```bash
mkdir -p internal/testutil
```

## Step 1: Create Test Utilities

Create `internal/testutil/di.go`:

```go
package testutil

import (
    "testing"
    "github.com/junioryono/godi"
    "github.com/stretchr/testify/require"
)

// TestProvider creates a DI provider for tests
type TestProvider struct {
    provider godi.ServiceProvider
    t        *testing.T
}

// NewTestProvider creates a new test provider
func NewTestProvider(t *testing.T, opts ...Option) *TestProvider {
    services := godi.NewServiceCollection()

    // Apply options
    cfg := &config{
        services: services,
    }
    for _, opt := range opts {
        opt(cfg)
    }

    // Build provider
    provider, err := services.BuildServiceProvider()
    require.NoError(t, err)

    // Auto-cleanup
    t.Cleanup(func() {
        provider.Close()
    })

    return &TestProvider{
        provider: provider,
        t:        t,
    }
}

// Provider returns the underlying service provider
func (tp *TestProvider) Provider() godi.ServiceProvider {
    return tp.provider
}

// Resolve resolves a service with automatic error checking
func (tp *TestProvider) Resolve(service interface{}) {
    err := tp.provider.Invoke(func(svc interface{}) {
        // Use reflection to set the service
        reflect.ValueOf(service).Elem().Set(reflect.ValueOf(svc))
    })
    require.NoError(tp.t, err)
}

// WithScope creates a test scope
func (tp *TestProvider) WithScope(fn func(scope godi.Scope)) {
    scope := tp.provider.CreateScope(context.Background())
    defer scope.Close()
    fn(scope)
}

// Option configures the test provider
type Option func(*config)

type config struct {
    services godi.ServiceCollection
}

// WithService adds a service to the test container
func WithService(lifetime godi.ServiceLifetime, constructor interface{}) Option {
    return func(c *config) {
        switch lifetime {
        case godi.Singleton:
            c.services.AddSingleton(constructor)
        case godi.Scoped:
            c.services.AddScoped(constructor)
        case godi.Transient:
            c.services.AddTransient(constructor)
        }
    }
}

// WithMock adds a mock service
func WithMock(serviceType reflect.Type, mock interface{}) Option {
    return func(c *config) {
        c.services.AddSingleton(func() interface{} {
            return mock
        })
    }
}
```

## Step 2: Create Mocks

Create `internal/mocks/repositories.go`:

```go
package mocks

import (
    "context"
    "errors"
    "sync"

    "blog-api/internal/models"
)

// MockUserRepository is a mock implementation of UserRepository
type MockUserRepository struct {
    mu      sync.RWMutex
    users   map[string]*models.User
    calls   []string
    err     error // Can be set to simulate errors
}

func NewMockUserRepository() *MockUserRepository {
    return &MockUserRepository{
        users: make(map[string]*models.User),
    }
}

// WithUsers sets initial users
func (m *MockUserRepository) WithUsers(users ...*models.User) *MockUserRepository {
    for _, user := range users {
        m.users[user.ID] = user
    }
    return m
}

// WithError simulates an error
func (m *MockUserRepository) WithError(err error) *MockUserRepository {
    m.err = err
    return m
}

func (m *MockUserRepository) Create(ctx context.Context, user *models.User) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    m.calls = append(m.calls, "Create")

    if m.err != nil {
        return m.err
    }

    if user.ID == "" {
        user.ID = "test-id"
    }

    m.users[user.ID] = user
    return nil
}

func (m *MockUserRepository) GetByID(ctx context.Context, id string) (*models.User, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()

    m.calls = append(m.calls, "GetByID")

    if m.err != nil {
        return nil, m.err
    }

    user, exists := m.users[id]
    if !exists {
        return nil, errors.New("not found")
    }

    return user, nil
}

func (m *MockUserRepository) GetByUsername(ctx context.Context, username string) (*models.User, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()

    m.calls = append(m.calls, "GetByUsername")

    if m.err != nil {
        return nil, m.err
    }

    for _, user := range m.users {
        if user.Username == username {
            return user, nil
        }
    }

    return nil, errors.New("not found")
}

// GetCalls returns the method calls made
func (m *MockUserRepository) GetCalls() []string {
    m.mu.RLock()
    defer m.mu.RUnlock()

    calls := make([]string, len(m.calls))
    copy(calls, m.calls)
    return calls
}

// MockLogger for testing
type MockLogger struct {
    mu       sync.Mutex
    messages []LogMessage
}

type LogMessage struct {
    Level   string
    Message string
    Args    []interface{}
}

func NewMockLogger() *MockLogger {
    return &MockLogger{}
}

func (l *MockLogger) Info(msg string, args ...interface{}) {
    l.mu.Lock()
    defer l.mu.Unlock()

    l.messages = append(l.messages, LogMessage{
        Level:   "INFO",
        Message: msg,
        Args:    args,
    })
}

func (l *MockLogger) Error(msg string, err error, args ...interface{}) {
    l.mu.Lock()
    defer l.mu.Unlock()

    l.messages = append(l.messages, LogMessage{
        Level:   "ERROR",
        Message: msg,
        Args:    append([]interface{}{err}, args...),
    })
}

func (l *MockLogger) GetMessages() []LogMessage {
    l.mu.Lock()
    defer l.mu.Unlock()

    messages := make([]LogMessage, len(l.messages))
    copy(messages, l.messages)
    return messages
}
```

## Step 3: Unit Testing Services

Create `internal/services/auth_test.go`:

```go
package services_test

import (
    "context"
    "testing"

    "blog-api/internal/config"
    "blog-api/internal/models"
    "blog-api/internal/mocks"
    "blog-api/internal/services"

    "github.com/junioryono/godi"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestAuthService_Register(t *testing.T) {
    tests := []struct {
        name      string
        setup     func(*mocks.MockUserRepository)
        request   *models.RegisterRequest
        wantErr   bool
        errMsg    string
        checkUser func(*testing.T, *models.User)
    }{
        {
            name: "successful registration",
            setup: func(repo *mocks.MockUserRepository) {
                // Empty repo - no existing users
            },
            request: &models.RegisterRequest{
                Username: "newuser",
                Email:    "new@example.com",
                Password: "password123",
            },
            wantErr: false,
            checkUser: func(t *testing.T, user *models.User) {
                assert.Equal(t, "newuser", user.Username)
                assert.Equal(t, "new@example.com", user.Email)
                assert.NotEmpty(t, user.ID)
                assert.NotEqual(t, "password123", user.PasswordHash) // Should be hashed
            },
        },
        {
            name: "duplicate username",
            setup: func(repo *mocks.MockUserRepository) {
                repo.WithUsers(&models.User{
                    ID:       "existing",
                    Username: "newuser",
                    Email:    "other@example.com",
                })
            },
            request: &models.RegisterRequest{
                Username: "newuser",
                Email:    "new@example.com",
                Password: "password123",
            },
            wantErr: true,
            errMsg:  "user already exists",
        },
        {
            name: "duplicate email",
            setup: func(repo *mocks.MockUserRepository) {
                repo.WithUsers(&models.User{
                    ID:       "existing",
                    Username: "other",
                    Email:    "new@example.com",
                })
            },
            request: &models.RegisterRequest{
                Username: "newuser",
                Email:    "new@example.com",
                Password: "password123",
            },
            wantErr: true,
            errMsg:  "user already exists",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Setup DI container
            services := godi.NewServiceCollection()

            // Register mocks
            mockRepo := mocks.NewMockUserRepository()
            if tt.setup != nil {
                tt.setup(mockRepo)
            }

            services.AddSingleton(func() repositories.UserRepository {
                return mockRepo
            })
            services.AddSingleton(func() *config.Config {
                return &config.Config{
                    JWTSecret:     "test-secret",
                    JWTExpiration: time.Hour,
                }
            })
            services.AddScoped(services.NewAuthService)

            // Build provider
            provider, err := services.BuildServiceProvider()
            require.NoError(t, err)
            defer provider.Close()

            // Create scope
            scope := provider.CreateScope(context.Background())
            defer scope.Close()

            // Resolve service
            authService, err := godi.Resolve[services.AuthService](scope.ServiceProvider())
            require.NoError(t, err)

            // Execute test
            resp, err := authService.Register(context.Background(), tt.request)

            // Check results
            if tt.wantErr {
                assert.Error(t, err)
                if tt.errMsg != "" {
                    assert.Contains(t, err.Error(), tt.errMsg)
                }
                assert.Nil(t, resp)
            } else {
                assert.NoError(t, err)
                assert.NotNil(t, resp)
                assert.NotEmpty(t, resp.Token)

                if tt.checkUser != nil {
                    tt.checkUser(t, resp.User)
                }

                // Verify user was saved
                savedUser, _ := mockRepo.GetByID(context.Background(), resp.User.ID)
                assert.NotNil(t, savedUser)
            }

            // Verify repository calls
            calls := mockRepo.GetCalls()
            assert.Contains(t, calls, "GetByUsername")
            assert.Contains(t, calls, "GetByEmail")
            if !tt.wantErr {
                assert.Contains(t, calls, "Create")
            }
        })
    }
}

func TestAuthService_Login(t *testing.T) {
    // Create a test user with hashed password
    hashedPassword, _ := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.DefaultCost)
    testUser := &models.User{
        ID:           "user-123",
        Username:     "testuser",
        Email:        "test@example.com",
        PasswordHash: string(hashedPassword),
    }

    tests := []struct {
        name    string
        setup   func(*mocks.MockUserRepository)
        request *models.LoginRequest
        wantErr bool
        errMsg  string
    }{
        {
            name: "successful login",
            setup: func(repo *mocks.MockUserRepository) {
                repo.WithUsers(testUser)
            },
            request: &models.LoginRequest{
                Username: "testuser",
                Password: "correct-password",
            },
            wantErr: false,
        },
        {
            name: "wrong password",
            setup: func(repo *mocks.MockUserRepository) {
                repo.WithUsers(testUser)
            },
            request: &models.LoginRequest{
                Username: "testuser",
                Password: "wrong-password",
            },
            wantErr: true,
            errMsg:  "invalid credentials",
        },
        {
            name: "user not found",
            setup: func(repo *mocks.MockUserRepository) {
                // Empty repo
            },
            request: &models.LoginRequest{
                Username: "nonexistent",
                Password: "password",
            },
            wantErr: true,
            errMsg:  "invalid credentials",
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Use test helper
            tp := testutil.NewTestProvider(t,
                testutil.WithService(godi.Singleton, func() repositories.UserRepository {
                    repo := mocks.NewMockUserRepository()
                    if tt.setup != nil {
                        tt.setup(repo)
                    }
                    return repo
                }),
                testutil.WithService(godi.Singleton, func() *config.Config {
                    return &config.Config{
                        JWTSecret:     "test-secret",
                        JWTExpiration: time.Hour,
                    }
                }),
                testutil.WithService(godi.Scoped, services.NewAuthService),
            )

            tp.WithScope(func(scope godi.Scope) {
                authService, err := godi.Resolve[services.AuthService](scope.ServiceProvider())
                require.NoError(t, err)

                resp, err := authService.Login(context.Background(), tt.request)

                if tt.wantErr {
                    assert.Error(t, err)
                    if tt.errMsg != "" {
                        assert.Contains(t, err.Error(), tt.errMsg)
                    }
                } else {
                    assert.NoError(t, err)
                    assert.NotNil(t, resp)
                    assert.NotEmpty(t, resp.Token)
                    assert.Equal(t, testUser.ID, resp.User.ID)

                    // Verify token is valid
                    userID, err := authService.ValidateToken(resp.Token)
                    assert.NoError(t, err)
                    assert.Equal(t, testUser.ID, userID)
                }
            })
        })
    }
}
```

## Step 4: Integration Testing

Create `internal/handlers/auth_integration_test.go`:

```go
package handlers_test

import (
    "bytes"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "blog-api/internal/config"
    "blog-api/internal/handlers"
    "blog-api/internal/models"
    "blog-api/internal/repositories"
    "blog-api/internal/services"

    "github.com/gorilla/mux"
    "github.com/junioryono/godi"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

func TestAuthHandler_Integration(t *testing.T) {
    // Setup DI container with real implementations
    services := godi.NewServiceCollection()

    // Use in-memory implementations for integration tests
    services.AddSingleton(repositories.NewInMemoryUserRepository)
    services.AddSingleton(func() *config.Config {
        return &config.Config{
            JWTSecret:     "test-integration-secret",
            JWTExpiration: time.Hour,
        }
    })
    services.AddScoped(services.NewAuthService)
    services.AddScoped(handlers.NewAuthHandler)

    provider, err := services.BuildServiceProvider()
    require.NoError(t, err)
    defer provider.Close()

    // Setup router
    router := mux.NewRouter()
    router.Use(handlers.DIMiddleware(provider))

    router.HandleFunc("/auth/register", func(w http.ResponseWriter, r *http.Request) {
        scope := handlers.GetScope(r.Context())
        handler, _ := godi.Resolve[*handlers.AuthHandler](scope.ServiceProvider())
        handler.Register(w, r)
    }).Methods("POST")

    router.HandleFunc("/auth/login", func(w http.ResponseWriter, r *http.Request) {
        scope := handlers.GetScope(r.Context())
        handler, _ := godi.Resolve[*handlers.AuthHandler](scope.ServiceProvider())
        handler.Login(w, r)
    }).Methods("POST")

    // Test server
    server := httptest.NewServer(router)
    defer server.Close()

    t.Run("full auth flow", func(t *testing.T) {
        // Register
        registerReq := models.RegisterRequest{
            Username: "integrationuser",
            Email:    "integration@test.com",
            Password: "testpass123",
        }

        body, _ := json.Marshal(registerReq)
        resp, err := http.Post(
            server.URL+"/auth/register",
            "application/json",
            bytes.NewReader(body),
        )
        require.NoError(t, err)
        defer resp.Body.Close()

        assert.Equal(t, http.StatusOK, resp.StatusCode)

        var registerResp models.AuthResponse
        err = json.NewDecoder(resp.Body).Decode(&registerResp)
        require.NoError(t, err)

        assert.NotEmpty(t, registerResp.Token)
        assert.Equal(t, "integrationuser", registerResp.User.Username)

        // Login with same credentials
        loginReq := models.LoginRequest{
            Username: "integrationuser",
            Password: "testpass123",
        }

        body, _ = json.Marshal(loginReq)
        resp, err = http.Post(
            server.URL+"/auth/login",
            "application/json",
            bytes.NewReader(body),
        )
        require.NoError(t, err)
        defer resp.Body.Close()

        assert.Equal(t, http.StatusOK, resp.StatusCode)

        var loginResp models.AuthResponse
        err = json.NewDecoder(resp.Body).Decode(&loginResp)
        require.NoError(t, err)

        assert.NotEmpty(t, loginResp.Token)
        assert.Equal(t, registerResp.User.ID, loginResp.User.ID)
    })

    t.Run("duplicate registration", func(t *testing.T) {
        // First registration
        registerReq := models.RegisterRequest{
            Username: "duplicate",
            Email:    "duplicate@test.com",
            Password: "testpass123",
        }

        body, _ := json.Marshal(registerReq)
        resp, err := http.Post(
            server.URL+"/auth/register",
            "application/json",
            bytes.NewReader(body),
        )
        require.NoError(t, err)
        resp.Body.Close()

        assert.Equal(t, http.StatusOK, resp.StatusCode)

        // Duplicate registration
        resp, err = http.Post(
            server.URL+"/auth/register",
            "application/json",
            bytes.NewReader(body),
        )
        require.NoError(t, err)
        defer resp.Body.Close()

        assert.Equal(t, http.StatusConflict, resp.StatusCode)
    })
}
```

## Step 5: Testing Scoped Services

Create `internal/services/scope_test.go`:

```go
package services_test

import (
    "context"
    "sync"
    "testing"

    "blog-api/internal/services"

    "github.com/junioryono/godi"
    "github.com/stretchr/testify/assert"
    "github.com/stretchr/testify/require"
)

// Service that tracks its instances
type InstanceTracker struct {
    ID       string
    Created  time.Time
}

type TrackedService struct {
    instance *InstanceTracker
}

func NewTrackedService() *TrackedService {
    return &TrackedService{
        instance: &InstanceTracker{
            ID:      uuid.New().String(),
            Created: time.Now(),
        },
    }
}

func (s *TrackedService) GetInstance() *InstanceTracker {
    return s.instance
}

func TestScopedLifetime(t *testing.T) {
    services := godi.NewServiceCollection()

    // Register as scoped
    services.AddScoped(NewTrackedService)

    provider, err := services.BuildServiceProvider()
    require.NoError(t, err)
    defer provider.Close()

    t.Run("same instance within scope", func(t *testing.T) {
        scope := provider.CreateScope(context.Background())
        defer scope.Close()

        // Resolve multiple times
        svc1, err := godi.Resolve[*TrackedService](scope.ServiceProvider())
        require.NoError(t, err)

        svc2, err := godi.Resolve[*TrackedService](scope.ServiceProvider())
        require.NoError(t, err)

        // Should be same instance
        assert.Equal(t, svc1.GetInstance().ID, svc2.GetInstance().ID)
        assert.Same(t, svc1, svc2)
    })

    t.Run("different instances across scopes", func(t *testing.T) {
        scope1 := provider.CreateScope(context.Background())
        defer scope1.Close()

        scope2 := provider.CreateScope(context.Background())
        defer scope2.Close()

        svc1, _ := godi.Resolve[*TrackedService](scope1.ServiceProvider())
        svc2, _ := godi.Resolve[*TrackedService](scope2.ServiceProvider())

        // Should be different instances
        assert.NotEqual(t, svc1.GetInstance().ID, svc2.GetInstance().ID)
        assert.NotSame(t, svc1, svc2)
    })

    t.Run("concurrent scope resolution", func(t *testing.T) {
        const numGoroutines = 100
        instances := make(map[string]bool)
        var mu sync.Mutex
        var wg sync.WaitGroup

        wg.Add(numGoroutines)

        for i := 0; i < numGoroutines; i++ {
            go func() {
                defer wg.Done()

                scope := provider.CreateScope(context.Background())
                defer scope.Close()

                svc, err := godi.Resolve[*TrackedService](scope.ServiceProvider())
                require.NoError(t, err)

                mu.Lock()
                instances[svc.GetInstance().ID] = true
                mu.Unlock()
            }()
        }

        wg.Wait()

        // Should have created numGoroutines different instances
        assert.Equal(t, numGoroutines, len(instances))
    })
}

// Test disposal of scoped services
type DisposableService struct {
    ID       string
    disposed bool
    mu       sync.Mutex
}

func NewDisposableService() *DisposableService {
    return &DisposableService{
        ID: uuid.New().String(),
    }
}

func (s *DisposableService) Close() error {
    s.mu.Lock()
    defer s.mu.Unlock()

    if s.disposed {
        return errors.New("already disposed")
    }

    s.disposed = true
    return nil
}

func (s *DisposableService) IsDisposed() bool {
    s.mu.Lock()
    defer s.mu.Unlock()
    return s.disposed
}

func TestScopeDisposal(t *testing.T) {
    services := godi.NewServiceCollection()

    // Track created services
    var createdServices []*DisposableService
    var mu sync.Mutex

    services.AddScoped(func() *DisposableService {
        svc := NewDisposableService()

        mu.Lock()
        createdServices = append(createdServices, svc)
        mu.Unlock()

        return svc
    })

    provider, err := services.BuildServiceProvider()
    require.NoError(t, err)
    defer provider.Close()

    t.Run("services disposed with scope", func(t *testing.T) {
        scope := provider.CreateScope(context.Background())

        // Create service
        svc, err := godi.Resolve[*DisposableService](scope.ServiceProvider())
        require.NoError(t, err)

        assert.False(t, svc.IsDisposed())

        // Close scope
        err = scope.Close()
        require.NoError(t, err)

        // Service should be disposed
        assert.True(t, svc.IsDisposed())
    })

    t.Run("disposal order LIFO", func(t *testing.T) {
        // Clear tracking
        mu.Lock()
        createdServices = nil
        mu.Unlock()

        scope := provider.CreateScope(context.Background())

        // Create multiple services
        const numServices = 5
        for i := 0; i < numServices; i++ {
            _, err := godi.Resolve[*DisposableService](scope.ServiceProvider())
            require.NoError(t, err)
        }

        // Track disposal order
        var disposalOrder []string
        for _, svc := range createdServices {
            id := svc.ID
            originalClose := svc.Close
            svc.Close = func() error {
                disposalOrder = append(disposalOrder, id)
                return originalClose()
            }
        }

        // Close scope
        scope.Close()

        // Verify LIFO order
        assert.Equal(t, numServices, len(disposalOrder))
        for i := 0; i < numServices; i++ {
            expectedIdx := numServices - 1 - i
            assert.Equal(t, createdServices[expectedIdx].ID, disposalOrder[i])
        }
    })
}
```

## Step 6: Testing Best Practices

Create `internal/testutil/assertions.go`:

```go
package testutil

import (
    "testing"
    "github.com/stretchr/testify/assert"
)

// AssertErrorType checks if an error is of a specific type
func AssertErrorType[T error](t *testing.T, err error) {
    t.Helper()

    var target T
    assert.ErrorAs(t, err, &target, "expected error type %T", target)
}

// AssertServiceRegistered verifies a service is registered
func AssertServiceRegistered[T any](t *testing.T, provider godi.ServiceProvider) {
    t.Helper()

    _, err := godi.Resolve[T](provider)
    assert.NoError(t, err, "service %T should be registered", *new(T))
}

// AssertServiceNotRegistered verifies a service is not registered
func AssertServiceNotRegistered[T any](t *testing.T, provider godi.ServiceProvider) {
    t.Helper()

    _, err := godi.Resolve[T](provider)
    assert.Error(t, err, "service %T should not be registered", *new(T))
}
```

Create test table helper `internal/testutil/table.go`:

```go
package testutil

import (
    "context"
    "testing"

    "github.com/junioryono/godi"
)

// ServiceTest defines a table-driven test for services
type ServiceTest struct {
    Name      string
    Setup     func(godi.ServiceCollection)
    Test      func(context.Context, godi.ServiceProvider) error
    WantError bool
    ErrorMsg  string
}

// RunServiceTests executes table-driven tests
func RunServiceTests(t *testing.T, tests []ServiceTest) {
    for _, tt := range tests {
        t.Run(tt.Name, func(t *testing.T) {
            // Create container
            services := godi.NewServiceCollection()

            // Apply setup
            if tt.Setup != nil {
                tt.Setup(services)
            }

            // Build provider
            provider, err := services.BuildServiceProvider()
            if err != nil {
                t.Fatalf("failed to build provider: %v", err)
            }
            defer provider.Close()

            // Run test
            ctx := context.Background()
            err = tt.Test(ctx, provider)

            // Check result
            if tt.WantError {
                assert.Error(t, err)
                if tt.ErrorMsg != "" {
                    assert.Contains(t, err.Error(), tt.ErrorMsg)
                }
            } else {
                assert.NoError(t, err)
            }
        })
    }
}
```

## Step 7: Benchmark Tests

Create `internal/services/benchmark_test.go`:

```go
package services_test

import (
    "context"
    "testing"

    "blog-api/internal/config"
    "blog-api/internal/mocks"
    "blog-api/internal/repositories"
    "blog-api/internal/services"

    "github.com/junioryono/godi"
)

func BenchmarkServiceResolution(b *testing.B) {
    services := godi.NewServiceCollection()

    // Register services
    services.AddSingleton(func() repositories.UserRepository {
        return mocks.NewMockUserRepository()
    })
    services.AddSingleton(func() *config.Config {
        return &config.Config{
            JWTSecret:     "bench-secret",
            JWTExpiration: time.Hour,
        }
    })
    services.AddScoped(services.NewAuthService)

    provider, _ := services.BuildServiceProvider()
    defer provider.Close()

    b.Run("singleton resolution", func(b *testing.B) {
        b.ResetTimer()
        for i := 0; i < b.N; i++ {
            _, err := godi.Resolve[*config.Config](provider)
            if err != nil {
                b.Fatal(err)
            }
        }
    })

    b.Run("scoped resolution", func(b *testing.B) {
        scope := provider.CreateScope(context.Background())
        defer scope.Close()

        b.ResetTimer()
        for i := 0; i < b.N; i++ {
            _, err := godi.Resolve[services.AuthService](scope.ServiceProvider())
            if err != nil {
                b.Fatal(err)
            }
        }
    })

    b.Run("scope creation and disposal", func(b *testing.B) {
        b.ResetTimer()
        for i := 0; i < b.N; i++ {
            scope := provider.CreateScope(context.Background())
            godi.Resolve[services.AuthService](scope.ServiceProvider())
            scope.Close()
        }
    })
}

func BenchmarkConcurrentResolution(b *testing.B) {
    services := godi.NewServiceCollection()

    services.AddSingleton(func() repositories.UserRepository {
        return mocks.NewMockUserRepository()
    })
    services.AddSingleton(func() *config.Config {
        return &config.Config{}
    })
    services.AddScoped(services.NewAuthService)

    provider, _ := services.BuildServiceProvider()
    defer provider.Close()

    b.RunParallel(func(pb *testing.PB) {
        for pb.Next() {
            scope := provider.CreateScope(context.Background())
            godi.Resolve[services.AuthService](scope.ServiceProvider())
            scope.Close()
        }
    })
}
```

## Testing Strategies

### 1. Unit Testing with Mocks

- Mock external dependencies
- Test business logic in isolation
- Fast and reliable

### 2. Integration Testing

- Use in-memory implementations
- Test component interactions
- Verify API contracts

### 3. End-to-End Testing

- Real implementations where possible
- Test complete user flows
- Slower but comprehensive

### 4. Test Organization

```
internal/
├── services/
│   ├── auth.go
│   ├── auth_test.go          # Unit tests
│   └── auth_integration_test.go
├── handlers/
│   ├── auth.go
│   └── auth_test.go          # Handler tests
├── mocks/                    # Shared mocks
│   ├── repositories.go
│   └── services.go
└── testutil/                 # Test utilities
    ├── di.go
    ├── assertions.go
    └── fixtures.go
```

## Key Testing Benefits with godi

### 1. Easy Mock Injection

```go
// Replace real service with mock
services.AddSingleton(func() UserRepository {
    return &MockUserRepository{
        users: testUsers,
    }
})
```

### 2. Isolated Test Environments

```go
// Each test gets fresh container
func TestFeature(t *testing.T) {
    provider := createTestProvider(t)
    // Test in isolation
}
```

### 3. Parallel Testing

```go
// Safe parallel tests with separate containers
func TestParallel(t *testing.T) {
    t.Parallel()

    provider := createTestProvider(t)
    // Each test has its own instances
}
```

### 4. Test-Specific Configuration

```go
// Override configuration for tests
services.AddSingleton(func() *Config {
    return &Config{
        Environment: "test",
        LogLevel:    "debug",
    }
})
```

## Common Testing Patterns

### Factory Pattern for Test Data

```go
func NewTestUser(opts ...func(*models.User)) *models.User {
    user := &models.User{
        ID:       uuid.New().String(),
        Username: "testuser",
        Email:    "test@example.com",
    }

    for _, opt := range opts {
        opt(user)
    }

    return user
}

// Usage
user := NewTestUser(
    WithUsername("custom"),
    WithEmail("custom@test.com"),
)
```

### Test Fixtures

```go
type Fixtures struct {
    Users    []*models.User
    Posts    []*models.Post
    Comments []*models.Comment
}

func LoadFixtures(t *testing.T, provider godi.ServiceProvider) *Fixtures {
    // Load test data
    return &Fixtures{
        Users: loadTestUsers(t, provider),
        Posts: loadTestPosts(t, provider),
    }
}
```

### Cleanup Helpers

```go
func TestWithCleanup(t *testing.T) {
    provider := createTestProvider(t)

    // Automatic cleanup
    t.Cleanup(func() {
        provider.Close()
        cleanupTestData()
    })

    // Test code
}
```

## Summary

Testing with godi transforms the testing experience:

- **Mock injection** is trivial
- **Test isolation** is automatic
- **Parallel tests** are safe
- **Setup/teardown** is simplified
- **Integration tests** use same DI pattern

The combination of dependency injection and Go's testing package creates a powerful testing environment that encourages comprehensive test coverage and maintainable test code.

## Next Steps

- Explore the [Microservices Tutorial](microservices.md)
- Learn about [Advanced Patterns](../howto/advanced-patterns.md)
- Read [Best Practices](../guides/best-practices.md)
