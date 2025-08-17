# Web Applications with net/http

Building web applications with godi using Go's standard library HTTP package.

## The Controller Pattern

Instead of creating scopes in every handler, use middleware to create the scope once per request, then use controllers with dependency injection:

```go
// Controller with all dependencies injected
type PostController struct {
    postService *PostService
    logger      *Logger
    requestID   string
}

// Use godi.In for clean dependency injection
type PostControllerParams struct {
    godi.In

    PostService *PostService
    Logger      *Logger
    RequestID   string  // Injected from request context!
}

func NewPostController(params PostControllerParams) *PostController {
    return &PostController{
        postService: params.PostService,
        logger:      params.Logger,
        requestID:   params.RequestID,
    }
}

func (c *PostController) CreatePost(w http.ResponseWriter, r *http.Request) {
    // Use injected services directly - no resolution needed!
    c.logger.Info("Creating post", "requestID", c.requestID)

    var req CreatePostRequest
    json.NewDecoder(r.Body).Decode(&req)

    post, err := c.postService.CreatePost(req.Title, req.Content)
    if err != nil {
        http.Error(w, err.Error(), 400)
        return
    }

    json.NewEncoder(w).Encode(post)
}
```

## Setting Up Middleware

Create middleware that sets up a scope for each request. The scope's context automatically contains itself:

```go
// Middleware creates scope and adds request ID
func ScopeMiddleware(provider godi.Provider) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Generate request ID for tracing
            requestID := uuid.New().String()
            w.Header().Set("X-Request-ID", requestID)

            // Create context with request ID
            ctx := context.WithValue(r.Context(), "requestID", requestID)

            // Create scope with enriched context
            scope, err := provider.CreateScope(ctx)
            if err != nil {
                http.Error(w, "Internal error", 500)
                return
            }
            defer scope.Close()

            // Use the scope's context which contains the scope itself
            next.ServeHTTP(w, r.WithContext(scope.Context()))
        })
    }
}
```

## Complete Example: Blog API

Let's build a simple blog API with posts and health checks.

### Step 1: Models and Services

```go
// models.go
type Post struct {
    ID        string    `json:"id"`
    Title     string    `json:"title"`
    Content   string    `json:"content"`
    CreatedAt time.Time `json:"created_at"`
}

type CreatePostRequest struct {
    Title   string `json:"title"`
    Content string `json:"content"`
}

// services.go
type PostService struct {
    db *Database
}

func NewPostService(db *Database) *PostService {
    return &PostService{db: db}
}

func (s *PostService) CreatePost(title, content string) (*Post, error) {
    if title == "" {
        return nil, errors.New("title required")
    }

    post := &Post{
        ID:        uuid.New().String(),
        Title:     title,
        Content:   content,
        CreatedAt: time.Now(),
    }

    return s.db.SavePost(post)
}

func (s *PostService) GetPosts() ([]*Post, error) {
    return s.db.GetAllPosts()
}
```

### Step 2: Request Context Service

Create a service that captures request-specific data:

```go
// Request-scoped service for request metadata
func NewRequestID(ctx context.Context) string {
    if id, ok := ctx.Value("requestID").(string); ok {
        return id
    }
    return "unknown"
}
```

### Step 3: Controllers

```go
// PostController handles post-related requests
type PostController struct {
    postService *PostService
    logger      *Logger
    requestID   string
}

type PostControllerParams struct {
    godi.In

    PostService *PostService
    Logger      *Logger
    RequestID   string
}

func NewPostController(params PostControllerParams) *PostController {
    return &PostController{
        postService: params.PostService,
        logger:      params.Logger,
        requestID:   params.RequestID,
    }
}

func (c *PostController) CreatePost(w http.ResponseWriter, r *http.Request) {
    c.logger.Info("Creating post", "requestID", c.requestID)

    var req CreatePostRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", 400)
        return
    }

    post, err := c.postService.CreatePost(req.Title, req.Content)
    if err != nil {
        c.logger.Error("Failed to create post", "error", err, "requestID", c.requestID)
        http.Error(w, err.Error(), 400)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(post)
}

func (c *PostController) GetPosts(w http.ResponseWriter, r *http.Request) {
    c.logger.Info("Getting posts", "requestID", c.requestID)

    posts, err := c.postService.GetPosts()
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(posts)
}

// HealthController for health checks
type HealthController struct {
    db        *Database
    requestID string
}

type HealthControllerParams struct {
    godi.In

    DB        *Database
    RequestID string
}

func NewHealthController(params HealthControllerParams) *HealthController {
    return &HealthController{
        db:        params.DB,
        requestID: params.RequestID,
    }
}

func (h *HealthController) CheckHealth(w http.ResponseWriter, r *http.Request) {
    health := map[string]any{
        "status":     "healthy",
        "request_id": h.requestID,
        "database":   h.db.Ping() == nil,
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(health)
}
```

### Step 4: Wire Everything Together

```go
func main() {
    // Set up modules
    appModule := godi.NewModule("app",
        // Infrastructure (Singleton - shared)
        godi.AddSingleton(NewLogger),
        godi.AddSingleton(NewDatabase),

        // Request-scoped services
        godi.AddScoped(NewRequestID),      // Gets ID from context
        godi.AddScoped(NewPostService),

        // Controllers (Scoped - per request)
        godi.AddScoped(NewPostController),
        godi.AddScoped(NewHealthController),
    )

    // Build provider
    collection := godi.NewCollection()
    collection.AddModules(appModule)

    provider, err := collection.Build()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // Set up router with middleware
    mux := http.NewServeMux()

    // Wrap with middleware
    handler := ScopeMiddleware(provider)(mux)

    // Register routes
    RegisterRoutes(mux, provider)

    log.Println("Server starting on :8080")
    log.Fatal(http.ListenAndServe(":8080", handler))
}

func RegisterRoutes(mux *http.ServeMux, provider godi.Provider) {
    // POST /posts
    mux.HandleFunc("/posts", func(w http.ResponseWriter, r *http.Request) {
        scope, ok := godi.FromContext(r.Context())
        if !ok {
            http.Error(w, "Internal error", 500)
            return
        }

        controller, err := godi.Resolve[*PostController](scope)
        if err != nil {
            http.Error(w, "Service error", 500)
            return
        }

        switch r.Method {
        case "POST":
            controller.CreatePost(w, r)
        case "GET":
            controller.GetPosts(w, r)
        default:
            http.Error(w, "Method not allowed", 405)
        }
    })

    // GET /health
    mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
        scope, ok := godi.FromContext(r.Context())
        if !ok {
            http.Error(w, "Internal error", 500)
            return
        }

        controller, err := godi.Resolve[*HealthController](scope)
        if err != nil {
            http.Error(w, "Service error", 500)
            return
        }

        controller.CheckHealth(w, r)
    })
}
```

### Step 5: Test the API

```bash
# Create a post
curl -X POST http://localhost:8080/posts \
  -H "Content-Type: application/json" \
  -d '{"title":"Hello World","content":"My first post!"}'

# Response includes request ID from header
# Check X-Request-ID header in response

# Get all posts
curl http://localhost:8080/posts

# Check health
curl http://localhost:8080/health
# Response: {"status":"healthy","request_id":"...","database":true}
```

## Testing Controllers

Controllers are easy to test since they use dependency injection:

```go
func TestPostController(t *testing.T) {
    // Test module with mocks
    testModule := godi.NewModule("test",
        godi.AddSingleton(func() *Database {
            return &MockDatabase{
                posts: []*Post{},
            }
        }),
        godi.AddSingleton(func() *Logger {
            return &MockLogger{}
        }),
        godi.AddScoped(func() string {
            return "test-request-123"  // Mock request ID
        }),
        godi.AddScoped(NewPostService),
        godi.AddScoped(NewPostController),
    )

    collection := godi.NewCollection()
    collection.AddModules(testModule)
    provider, _ := collection.Build()
    defer provider.Close()

    // Create test scope
    ctx := context.WithValue(context.Background(), "requestID", "test-request-123")
    scope, _ := provider.CreateScope(ctx)
    defer scope.Close()

    // Get controller
    controller, _ := godi.Resolve[*PostController](scope)

    // Test controller has the right request ID
    assert.Equal(t, "test-request-123", controller.requestID)

    // Test CreatePost
    req := httptest.NewRequest("POST", "/posts",
        strings.NewReader(`{"title":"Test","content":"Content"}`))
    w := httptest.NewRecorder()

    controller.CreatePost(w, req)

    assert.Equal(t, 200, w.Code)
}
```

## Best Practices

### 1. Use Middleware for Cross-Cutting Concerns

```go
// Chain middleware
handler := LoggingMiddleware(provider)(
    ScopeMiddleware(provider)(
        AuthMiddleware(provider)(mux),
    ),
)
```

### 2. Controllers for Complex Handlers

Use controllers when you have:

- Multiple dependencies
- Complex business logic
- Need for better testability

### 3. Keep Controllers Focused

```go
// ✅ Good - focused controller
type UserController struct {
    userService *UserService
    logger      *Logger
}

// ❌ Bad - too many responsibilities
type EverythingController struct {
    userService    *UserService
    postService    *PostService
    commentService *CommentService
    // ... 10 more services
}
```

## Summary

Using net/http with godi provides:

1. **Clean separation** - Middleware handles infrastructure, controllers handle business logic
2. **Dependency injection** - Controllers get all dependencies injected
3. **Request tracing** - Request ID flows through all services
4. **Easy testing** - Mock dependencies for unit tests
5. **No external dependencies** - Just Go's standard library and godi

This pattern works well for APIs that don't need the extra features of a web framework!
