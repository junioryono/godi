# Web Applications with Gorilla Mux

Building web applications with godi using the Gorilla Mux router.

## The Controller Pattern with Mux

Gorilla Mux provides powerful routing features while staying close to the standard library. Combined with godi's dependency injection, you get clean, testable handlers:

```go
// Controller with dependencies injected
type PostController struct {
    postService *PostService
    logger      *Logger
}

// Use godi.In for dependency injection
type PostControllerParams struct {
    godi.In

    PostService *PostService
    Logger      *Logger
}

func NewPostController(params PostControllerParams) *PostController {
    return &PostController{
        postService: params.PostService,
        logger:      params.Logger,
    }
}

func (c *PostController) CreatePost(w http.ResponseWriter, r *http.Request) {
    // Access request ID from context
    requestID := r.Context().Value("requestID").(string)
    c.logger.Info("Creating post", "requestID", requestID)

    var req CreatePostRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", 400)
        return
    }

    post, err := c.postService.CreatePost(req.Title, req.Content)
    if err != nil {
        c.logger.Error("Failed to create post", "error", err, "requestID", requestID)
        http.Error(w, err.Error(), 400)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(201)
    json.NewEncoder(w).Encode(post)
}
```

## Setting Up Middleware

Create middleware that sets up a scope for each request. The scope's context automatically contains itself:

```go
// Middleware creates scope and adds request ID
func ScopeMiddleware(provider godi.Provider) mux.MiddlewareFunc {
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

// Logging middleware
func LoggingMiddleware(provider godi.Provider) mux.MiddlewareFunc {
    // Resolve singleton logger once
    logger, _ := godi.Resolve[*Logger](provider)

    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            start := time.Now()

            // Wrap response writer to capture status
            wrapped := &responseWriter{ResponseWriter: w, statusCode: 200}

            next.ServeHTTP(wrapped, r)

            requestID, _ := r.Context().Value("requestID").(string)
            logger.Info("Request completed",
                "method", r.Method,
                "path", r.URL.Path,
                "status", wrapped.statusCode,
                "duration", time.Since(start),
                "requestID", requestID,
            )
        })
    }
}

type responseWriter struct {
    http.ResponseWriter
    statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
    rw.statusCode = code
    rw.ResponseWriter.WriteHeader(code)
}
```

## Complete Example: Blog API

Let's build a blog API with Gorilla Mux and godi.

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

func (s *PostService) GetPost(id string) (*Post, error) {
    return s.db.GetPost(id)
}

func (s *PostService) UpdatePost(id string, title, content string) (*Post, error) {
    return s.db.UpdatePost(id, title, content)
}

func (s *PostService) DeletePost(id string) error {
    return s.db.DeletePost(id)
}
```

### Step 2: Request Context Service

```go
// Request-scoped service for request metadata
func NewRequestContext(ctx context.Context) *RequestContext {
    requestID, _ := ctx.Value("requestID").(string)
    userID, _ := ctx.Value("userID").(string)

    return &RequestContext{
        RequestID: requestID,
        UserID:    userID,
        StartTime: time.Now(),
    }
}

type RequestContext struct {
    RequestID string
    UserID    string
    StartTime time.Time
}
```

### Step 3: Controllers

```go
// PostController handles post-related requests
type PostController struct {
    postService *PostService
    logger      *Logger
}

type PostControllerParams struct {
    godi.In

    PostService *PostService
    Logger      *Logger
}

func NewPostController(params PostControllerParams) *PostController {
    return &PostController{
        postService: params.PostService,
        logger:      params.Logger,
    }
}

func (c *PostController) CreatePost(w http.ResponseWriter, r *http.Request) {
    requestID := r.Context().Value("requestID").(string)
    c.logger.Info("Creating post", "requestID", requestID)

    var req CreatePostRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", 400)
        return
    }

    if req.Title == "" || req.Content == "" {
        http.Error(w, "Title and content are required", 400)
        return
    }

    post, err := c.postService.CreatePost(req.Title, req.Content)
    if err != nil {
        c.logger.Error("Failed to create post", "error", err, "requestID", requestID)
        http.Error(w, err.Error(), 400)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(201)
    json.NewEncoder(w).Encode(post)
}

func (c *PostController) GetPosts(w http.ResponseWriter, r *http.Request) {
    requestID := r.Context().Value("requestID").(string)
    c.logger.Info("Getting posts", "requestID", requestID)

    posts, err := c.postService.GetPosts()
    if err != nil {
        http.Error(w, err.Error(), 500)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]any{
        "posts":      posts,
        "request_id": requestID,
    })
}

func (c *PostController) GetPost(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    postID := vars["id"]
    requestID := r.Context().Value("requestID").(string)

    c.logger.Info("Getting post", "postID", postID, "requestID", requestID)

    post, err := c.postService.GetPost(postID)
    if err != nil {
        http.Error(w, "Post not found", 404)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(post)
}

func (c *PostController) UpdatePost(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    postID := vars["id"]
    requestID := r.Context().Value("requestID").(string)

    c.logger.Info("Updating post", "postID", postID, "requestID", requestID)

    var req CreatePostRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", 400)
        return
    }

    post, err := c.postService.UpdatePost(postID, req.Title, req.Content)
    if err != nil {
        http.Error(w, err.Error(), 400)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(post)
}

func (c *PostController) DeletePost(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    postID := vars["id"]
    requestID := r.Context().Value("requestID").(string)

    c.logger.Info("Deleting post", "postID", postID, "requestID", requestID)

    if err := c.postService.DeletePost(postID); err != nil {
        http.Error(w, err.Error(), 400)
        return
    }

    w.WriteHeader(204)
}

// HealthController for health checks
type HealthController struct {
    db     *Database
    logger *Logger
}

type HealthControllerParams struct {
    godi.In

    DB     *Database
    Logger *Logger
}

func NewHealthController(params HealthControllerParams) *HealthController {
    return &HealthController{
        db:     params.DB,
        logger: params.Logger,
    }
}

func (h *HealthController) CheckHealth(w http.ResponseWriter, r *http.Request) {
    requestID, _ := r.Context().Value("requestID").(string)

    health := map[string]any{
        "status":     "healthy",
        "request_id": requestID,
        "timestamp":  time.Now(),
        "checks": map[string]bool{
            "database": h.db.Ping() == nil,
        },
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
        godi.AddScoped(NewRequestContext),
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

    // Set up router
    router := mux.NewRouter()

    // Apply middleware
    router.Use(
        LoggingMiddleware(provider),
        ScopeMiddleware(provider),
    )

    // Register routes
    RegisterRoutes(router, provider)

    log.Println("Server starting on :8080")
    log.Fatal(http.ListenAndServe(":8080", router))
}

func RegisterRoutes(router *mux.Router, provider godi.Provider) {
    // Helper to resolve controller from scope
    withController := func[T any](handler func(T) http.HandlerFunc) http.HandlerFunc {
        return func(w http.ResponseWriter, r *http.Request) {
            scope, ok := godi.FromContext(r.Context())
            if !ok {
                http.Error(w, "Internal error", 500)
                return
            }

            controller, err := godi.Resolve[T](scope)
            if err != nil {
                http.Error(w, "Service error", 500)
                return
            }

            handler(controller)(w, r)
        }
    }

    // API v1 routes
    api := router.PathPrefix("/api/v1").Subrouter()

    // Post routes
    posts := api.PathPrefix("/posts").Subrouter()
    posts.HandleFunc("", withController(func(c *PostController) http.HandlerFunc {
        return c.CreatePost
    })).Methods("POST")

    posts.HandleFunc("", withController(func(c *PostController) http.HandlerFunc {
        return c.GetPosts
    })).Methods("GET")

    posts.HandleFunc("/{id}", withController(func(c *PostController) http.HandlerFunc {
        return c.GetPost
    })).Methods("GET")

    posts.HandleFunc("/{id}", withController(func(c *PostController) http.HandlerFunc {
        return c.UpdatePost
    })).Methods("PUT")

    posts.HandleFunc("/{id}", withController(func(c *PostController) http.HandlerFunc {
        return c.DeletePost
    })).Methods("DELETE")

    // Health check
    router.HandleFunc("/health", withController(func(c *HealthController) http.HandlerFunc {
        return c.CheckHealth
    })).Methods("GET")
}
```

### Step 5: Test the API

```bash
# Create a post
curl -X POST http://localhost:8080/api/v1/posts \
  -H "Content-Type: application/json" \
  -d '{"title":"Hello Mux","content":"Using Gorilla Mux with godi!"}'

# Get all posts
curl http://localhost:8080/api/v1/posts

# Get specific post
curl http://localhost:8080/api/v1/posts/{post-id}

# Update a post
curl -X PUT http://localhost:8080/api/v1/posts/{post-id} \
  -H "Content-Type: application/json" \
  -d '{"title":"Updated","content":"New content"}'

# Delete a post
curl -X DELETE http://localhost:8080/api/v1/posts/{post-id}

# Check health
curl http://localhost:8080/health
```

## Advanced Patterns

### Authentication Middleware

```go
func AuthMiddleware(provider godi.Provider) mux.MiddlewareFunc {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            token := r.Header.Get("Authorization")
            if token == "" {
                http.Error(w, "Unauthorized", 401)
                return
            }

            // Validate token and get user ID
            userID, err := validateToken(strings.TrimPrefix(token, "Bearer "))
            if err != nil {
                http.Error(w, "Invalid token", 401)
                return
            }

            // Add user ID to context for dependency injection
            ctx := context.WithValue(r.Context(), "userID", userID)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

// Apply to specific routes
authRoutes := router.PathPrefix("/api/v1").Subrouter()
authRoutes.Use(AuthMiddleware(provider))
```

### CORS Middleware

```go
func CORSMiddleware() mux.MiddlewareFunc {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            w.Header().Set("Access-Control-Allow-Origin", "*")
            w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
            w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

            if r.Method == "OPTIONS" {
                w.WriteHeader(204)
                return
            }

            next.ServeHTTP(w, r)
        })
    }
}
```

### Route Groups with Different Middleware

```go
func RegisterRoutes(router *mux.Router, provider godi.Provider) {
    // Public routes (no auth)
    public := router.PathPrefix("/api/public").Subrouter()
    public.HandleFunc("/posts", withController((*PostController).GetPosts)).Methods("GET")

    // Protected routes (require auth)
    protected := router.PathPrefix("/api/v1").Subrouter()
    protected.Use(AuthMiddleware(provider))
    protected.HandleFunc("/posts", withController((*PostController).CreatePost)).Methods("POST")

    // Admin routes (require admin auth)
    admin := router.PathPrefix("/api/admin").Subrouter()
    admin.Use(AdminAuthMiddleware(provider))
    admin.HandleFunc("/posts/{id}", withController((*PostController).DeletePost)).Methods("DELETE")
}
```

## Testing with Mux

```go
func TestPostController(t *testing.T) {
    // Test module
    testModule := godi.NewModule("test",
        godi.AddSingleton(func() *Database {
            return &MockDatabase{posts: []*Post{}}
        }),
        godi.AddSingleton(func() *Logger {
            return &MockLogger{}
        }),
        godi.AddScoped(NewRequestContext),
        godi.AddScoped(NewPostService),
        godi.AddScoped(NewPostController),
    )

    collection := godi.NewCollection()
    collection.AddModules(testModule)
    provider, _ := collection.Build()
    defer provider.Close()

    // Set up test router
    router := mux.NewRouter()
    router.Use(ScopeMiddleware(provider))
    RegisterRoutes(router, provider)

    // Test creating a post
    w := httptest.NewRecorder()
    body := `{"title":"Test","content":"Content"}`
    req := httptest.NewRequest("POST", "/api/v1/posts", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")

    router.ServeHTTP(w, req)

    assert.Equal(t, 201, w.Code)

    var response Post
    json.Unmarshal(w.Body.Bytes(), &response)
    assert.Equal(t, "Test", response.Title)
}
```

## Best Practices

### 1. Use Mux Features

```go
// Path variables
vars := mux.Vars(r)
id := vars["id"]

// Query parameters
query := r.URL.Query()
page := query.Get("page")
limit := query.Get("limit")

// Route matching
router.HandleFunc("/posts/{id:[0-9]+}", handler)  // Regex matching
router.HandleFunc("/users/{username}", handler)    // Named parameters
```

### 2. Structured Route Organization

```go
// Group by version
v1 := router.PathPrefix("/api/v1").Subrouter()
v2 := router.PathPrefix("/api/v2").Subrouter()

// Group by resource
posts := v1.PathPrefix("/posts").Subrouter()
users := v1.PathPrefix("/users").Subrouter()

// Apply middleware to groups
posts.Use(RateLimitMiddleware(provider))
```

### 3. Error Handling

```go
type APIError struct {
    Code    int    `json:"code"`
    Message string `json:"message"`
}

func JSONError(w http.ResponseWriter, err string, code int) {
    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(code)
    json.NewEncoder(w).Encode(APIError{
        Code:    code,
        Message: err,
    })
}

// Use in controllers
if err != nil {
    JSONError(w, "Post not found", 404)
    return
}
```

## Summary

Using Gorilla Mux with godi provides:

1. **Powerful routing** - Path variables, regex matching, subrouters
2. **Clean middleware** - Easy to chain and apply to groups
3. **RESTful APIs** - HTTP method-based routing
4. **Dependency injection** - Clean controllers with godi
5. **Request isolation** - Scopes for each request
6. **Flexible organization** - Route groups and versioning
7. **Standard library compatible** - Works with http.Handler

Gorilla Mux gives you routing power while keeping the simplicity of the standard library, and godi handles all your dependency injection needs!
