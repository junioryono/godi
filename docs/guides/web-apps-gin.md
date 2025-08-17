# Web Applications with Gin

Building web applications with godi using the popular Gin web framework.

## The Controller Pattern with Gin

Gin's middleware system works perfectly with godi's dependency injection. Use middleware to create scopes and controllers for clean handler logic:

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

// Gin handlers as controller methods
func (c *PostController) CreatePost(ctx *gin.Context) {
    // Access request ID from Gin context
    requestID := ctx.GetString("requestID")
    c.logger.Info("Creating post", "requestID", requestID)

    var req CreatePostRequest
    if err := ctx.ShouldBindJSON(&req); err != nil {
        ctx.JSON(400, gin.H{"error": err.Error()})
        return
    }

    post, err := c.postService.CreatePost(req.Title, req.Content)
    if err != nil {
        c.logger.Error("Failed to create post", "error", err, "requestID", requestID)
        ctx.JSON(400, gin.H{"error": err.Error()})
        return
    }

    ctx.JSON(200, post)
}
```

## Setting Up Middleware

Create Gin middleware that sets up a scope for each request. The scope's context automatically contains itself:

```go
// Middleware creates scope and enriches Gin context
func ScopeMiddleware(provider godi.Provider) gin.HandlerFunc {
    return func(c *gin.Context) {
        // Generate request ID for tracing
        requestID := uuid.New().String()
        c.Set("requestID", requestID)
        c.Header("X-Request-ID", requestID)

        // Create context with request ID
        ctx := context.WithValue(c.Request.Context(), "requestID", requestID)

        // Create scope with enriched context
        scope, err := provider.CreateScope(ctx)
        if err != nil {
            c.AbortWithStatusJSON(500, gin.H{"error": "Internal error"})
            return
        }
        defer scope.Close()

        // Update Gin's request context with the scope's context
        // The scope's context contains the scope itself!
        c.Request = c.Request.WithContext(scope.Context())

        c.Next()
    }
}

// Helper to get scope from request context
func GetScope(c *gin.Context) (godi.Scope, error) {
    return godi.FromContext(c.Request.Context())
}
```

## Complete Example: Blog API

Let's build a blog API with Gin and godi.

### Step 1: Models and Services

```go
// models.go
type Post struct {
    ID        string    `json:"id"`
    Title     string    `json:"title" binding:"required"`
    Content   string    `json:"content" binding:"required"`
    CreatedAt time.Time `json:"created_at"`
}

type CreatePostRequest struct {
    Title   string `json:"title" binding:"required"`
    Content string `json:"content" binding:"required"`
}

// services.go
type PostService struct {
    db *Database
}

func NewPostService(db *Database) *PostService {
    return &PostService{db: db}
}

func (s *PostService) CreatePost(title, content string) (*Post, error) {
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

func (c *PostController) CreatePost(ctx *gin.Context) {
    requestID := ctx.GetString("requestID")
    c.logger.Info("Creating post", "requestID", requestID)

    var req CreatePostRequest
    if err := ctx.ShouldBindJSON(&req); err != nil {
        ctx.JSON(400, gin.H{"error": err.Error()})
        return
    }

    post, err := c.postService.CreatePost(req.Title, req.Content)
    if err != nil {
        c.logger.Error("Failed to create post", "error", err, "requestID", requestID)
        ctx.JSON(400, gin.H{"error": err.Error()})
        return
    }

    ctx.JSON(201, post)
}

func (c *PostController) GetPosts(ctx *gin.Context) {
    requestID := ctx.GetString("requestID")
    c.logger.Info("Getting posts", "requestID", requestID)

    posts, err := c.postService.GetPosts()
    if err != nil {
        ctx.JSON(500, gin.H{"error": err.Error()})
        return
    }

    ctx.JSON(200, gin.H{
        "posts":      posts,
        "request_id": requestID,
    })
}

func (c *PostController) GetPost(ctx *gin.Context) {
    requestID := ctx.GetString("requestID")
    postID := ctx.Param("id")

    c.logger.Info("Getting post", "postID", postID, "requestID", requestID)

    post, err := c.postService.GetPost(postID)
    if err != nil {
        ctx.JSON(404, gin.H{"error": "Post not found"})
        return
    }

    ctx.JSON(200, post)
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

func (h *HealthController) CheckHealth(ctx *gin.Context) {
    requestID := ctx.GetString("requestID")

    health := gin.H{
        "status":     "healthy",
        "request_id": requestID,
        "timestamp":  time.Now(),
        "checks": gin.H{
            "database": h.db.Ping() == nil,
        },
    }

    ctx.JSON(200, health)
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

    // Set up Gin
    router := gin.Default()

    // Add middleware
    router.Use(ScopeMiddleware(provider))

    // Register routes
    RegisterRoutes(router)

    log.Println("Server starting on :8080")
    router.Run(":8080")
}

func RegisterRoutes(router *gin.Engine) {
    // Helper function to resolve controller from scope
    withController := func[T any](handler func(T, *gin.Context)) gin.HandlerFunc {
        return func(c *gin.Context) {
            scope, ok := godi.FromContext(c.Request.Context())
            if !ok {
                c.JSON(500, gin.H{"error": "Internal error"})
                return
            }

            controller, err := godi.Resolve[T](scope)
            if err != nil {
                c.JSON(500, gin.H{"error": "Service error"})
                return
            }

            handler(controller, c)
        }
    }

    // Post routes
    posts := router.Group("/posts")
    {
        posts.POST("", withController((*PostController).CreatePost))
        posts.GET("", withController((*PostController).GetPosts))
        posts.GET("/:id", withController((*PostController).GetPost))
    }

    // Health route
    router.GET("/health", withController((*HealthController).CheckHealth))
}
```

### Step 5: Test the API

```bash
# Create a post
curl -X POST http://localhost:8080/posts \
  -H "Content-Type: application/json" \
  -d '{"title":"Hello Gin","content":"Using Gin with godi!"}'

# Get all posts
curl http://localhost:8080/posts

# Get specific post
curl http://localhost:8080/posts/{post-id}

# Check health
curl http://localhost:8080/health
```

## Testing with Gin

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
    router := gin.New()
    router.Use(ScopeMiddleware(provider))
    RegisterRoutes(router)

    // Test creating a post
    w := httptest.NewRecorder()
    body := `{"title":"Test","content":"Content"}`
    req := httptest.NewRequest("POST", "/posts", strings.NewReader(body))
    req.Header.Set("Content-Type", "application/json")

    router.ServeHTTP(w, req)

    assert.Equal(t, 201, w.Code)

    var response Post
    json.Unmarshal(w.Body.Bytes(), &response)
    assert.Equal(t, "Test", response.Title)
}
```

## Advanced Patterns

### Authentication Middleware

```go
func AuthMiddleware() gin.HandlerFunc {
    return func(c *gin.Context) {
        token := c.GetHeader("Authorization")
        if token == "" {
            c.AbortWithStatusJSON(401, gin.H{"error": "Unauthorized"})
            return
        }

        // Validate token and get user ID
        userID, err := validateToken(token)
        if err != nil {
            c.AbortWithStatusJSON(401, gin.H{"error": "Invalid token"})
            return
        }

        // Add user ID to context for dependency injection
        ctx := context.WithValue(c.Request.Context(), "userID", userID)
        c.Request = c.Request.WithContext(ctx)
        c.Set("userID", userID)  // Also in Gin context for convenience

        c.Next()
    }
}
```

### Error Handler Middleware

```go
func ErrorHandler() gin.HandlerFunc {
    return func(c *gin.Context) {
        c.Next()

        if len(c.Errors) > 0 {
            err := c.Errors.Last()

            switch e := err.Err.(type) {
            case *ValidationError:
                c.JSON(400, gin.H{"error": e.Error()})
            case *NotFoundError:
                c.JSON(404, gin.H{"error": e.Error()})
            default:
                c.JSON(500, gin.H{"error": "Internal server error"})
            }
        }
    }
}
```

### Using Request Context in Services

```go
// Any service can inject RequestContext
type AuditService struct {
    db     *Database
    reqCtx *RequestContext
}

type AuditServiceParams struct {
    godi.In

    DB     *Database
    ReqCtx *RequestContext
}

func NewAuditService(params AuditServiceParams) *AuditService {
    return &AuditService{
        db:     params.DB,
        reqCtx: params.ReqCtx,
    }
}

func (a *AuditService) LogAction(action string) {
    log.Printf("[%s] User %s performed: %s",
        a.reqCtx.RequestID,
        a.reqCtx.UserID,
        action)
}
```

## Best Practices

### 1. Use Gin's Built-in Features

```go
// Use Gin's validation
type CreateUserRequest struct {
    Name  string `json:"name" binding:"required,min=2"`
    Email string `json:"email" binding:"required,email"`
}

// Use Gin's binding
if err := ctx.ShouldBindJSON(&req); err != nil {
    ctx.JSON(400, gin.H{"error": err.Error()})
    return
}
```

### 2. Group Related Routes

```go
v1 := router.Group("/api/v1")
{
    posts := v1.Group("/posts")
    posts.Use(AuthMiddleware())
    {
        posts.GET("", withController((*PostController).GetPosts))
        posts.POST("", withController((*PostController).CreatePost))
    }

    admin := v1.Group("/admin")
    admin.Use(AdminAuthMiddleware())
    {
        admin.GET("/stats", withController((*AdminController).GetStats))
    }
}
```

### 3. Leverage Both Contexts

```go
// Use Gin context for Gin-specific features
c.Set("requestID", requestID)        // Store in Gin context
requestID := c.GetString("requestID") // Retrieve from Gin context

// Use request context for dependency injection
ctx := context.WithValue(c.Request.Context(), "userID", userID)
c.Request = c.Request.WithContext(ctx)

// Services can access the request context
func NewUserContext(ctx context.Context) *UserContext {
    userID, _ := ctx.Value("userID").(string)
    return &UserContext{UserID: userID}
}
```

## Summary

Using Gin with godi provides:

1. **Fast routing** - Gin's httprouter-based engine
2. **Rich middleware** - Easy to add cross-cutting concerns
3. **Built-in validation** - Struct tags for request validation
4. **JSON handling** - Automatic JSON binding and responses
5. **Clean dependency injection** - Controllers with godi
6. **Request isolation** - Scopes for each request
7. **Easy testing** - Mock dependencies and test with httptest

The key insight is that the scope's context contains the scope itself, making it easy to pass through Gin's request context!
