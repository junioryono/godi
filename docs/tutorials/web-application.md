# Building a Web Application with godi

Let's build a real REST API - a blog system with posts, comments, and authentication. This tutorial shows how godi makes web apps clean and testable.

## What We're Building

A blog API with:

- User registration and login
- Create, read, update, delete posts
- Add comments to posts
- JWT authentication
- Request-scoped transactions

## Setup

Create a new project:

```bash
mkdir blog-api && cd blog-api
go mod init blog-api
go get github.com/junioryono/godi/v2
go get github.com/gorilla/mux
go get github.com/golang-jwt/jwt/v5
```

## Project Structure

```
blog-api/
├── main.go
├── modules/
│   ├── core.go
│   ├── auth.go
│   ├── blog.go
│   └── web.go
├── services/
│   ├── database.go
│   ├── auth.go
│   ├── post.go
│   └── user.go
├── handlers/
│   ├── auth.go
│   ├── post.go
│   └── middleware.go
└── models/
    └── models.go
```

## Step 1: Define Models

**models/models.go**

```go
package models

import "time"

type User struct {
    ID           string    `json:"id"`
    Username     string    `json:"username"`
    Email        string    `json:"email"`
    PasswordHash string    `json:"-"`
    CreatedAt    time.Time `json:"created_at"`
}

type Post struct {
    ID        string    `json:"id"`
    UserID    string    `json:"user_id"`
    Title     string    `json:"title"`
    Content   string    `json:"content"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

type Comment struct {
    ID        string    `json:"id"`
    PostID    string    `json:"post_id"`
    UserID    string    `json:"user_id"`
    Content   string    `json:"content"`
    CreatedAt time.Time `json:"created_at"`
}

// Request/Response types
type LoginRequest struct {
    Username string `json:"username"`
    Password string `json:"password"`
}

type LoginResponse struct {
    Token string `json:"token"`
    User  User   `json:"user"`
}

type CreatePostRequest struct {
    Title   string `json:"title"`
    Content string `json:"content"`
}
```

## Step 2: Core Services

**services/database.go**

```go
package services

import (
    "blog-api/models"
    "sync"
)

// In real app, this would be a real database
type Database struct {
    mu       sync.RWMutex
    users    map[string]*models.User
    posts    map[string]*models.Post
    comments map[string]*models.Comment
}

func NewDatabase() *Database {
    return &Database{
        users:    make(map[string]*models.User),
        posts:    make(map[string]*models.Post),
        comments: make(map[string]*models.Comment),
    }
}

// User methods
func (db *Database) CreateUser(user *models.User) error {
    db.mu.Lock()
    defer db.mu.Unlock()
    db.users[user.ID] = user
    return nil
}

func (db *Database) GetUser(id string) (*models.User, error) {
    db.mu.RLock()
    defer db.mu.RUnlock()
    user, ok := db.users[id]
    if !ok {
        return nil, errors.New("user not found")
    }
    return user, nil
}

func (db *Database) GetUserByUsername(username string) (*models.User, error) {
    db.mu.RLock()
    defer db.mu.RUnlock()
    for _, user := range db.users {
        if user.Username == username {
            return user, nil
        }
    }
    return nil, errors.New("user not found")
}

// Post methods
func (db *Database) CreatePost(post *models.Post) error {
    db.mu.Lock()
    defer db.mu.Unlock()
    db.posts[post.ID] = post
    return nil
}

func (db *Database) GetPosts() ([]*models.Post, error) {
    db.mu.RLock()
    defer db.mu.RUnlock()
    posts := make([]*models.Post, 0, len(db.posts))
    for _, post := range db.posts {
        posts = append(posts, post)
    }
    return posts, nil
}
```

**services/auth.go**

```go
package services

import (
    "blog-api/models"
    "errors"
    "time"

    "github.com/golang-jwt/jwt/v5"
    "golang.org/x/crypto/bcrypt"
)

type AuthService struct {
    db        *Database
    jwtSecret string
}

func NewAuthService(db *Database) *AuthService {
    return &AuthService{
        db:        db,
        jwtSecret: "your-secret-key", // In production, use env var
    }
}

func (s *AuthService) Register(username, email, password string) (*models.User, error) {
    // Check if user exists
    if _, err := s.db.GetUserByUsername(username); err == nil {
        return nil, errors.New("username already exists")
    }

    // Hash password
    hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
    if err != nil {
        return nil, err
    }

    // Create user
    user := &models.User{
        ID:           generateID(),
        Username:     username,
        Email:        email,
        PasswordHash: string(hash),
        CreatedAt:    time.Now(),
    }

    if err := s.db.CreateUser(user); err != nil {
        return nil, err
    }

    return user, nil
}

func (s *AuthService) Login(username, password string) (string, *models.User, error) {
    // Get user
    user, err := s.db.GetUserByUsername(username)
    if err != nil {
        return "", nil, errors.New("invalid credentials")
    }

    // Check password
    if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
        return "", nil, errors.New("invalid credentials")
    }

    // Generate token
    token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
        "user_id": user.ID,
        "exp":     time.Now().Add(24 * time.Hour).Unix(),
    })

    tokenString, err := token.SignedString([]byte(s.jwtSecret))
    if err != nil {
        return "", nil, err
    }

    return tokenString, user, nil
}

func (s *AuthService) ValidateToken(tokenString string) (string, error) {
    token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
        return []byte(s.jwtSecret), nil
    })

    if err != nil || !token.Valid {
        return "", errors.New("invalid token")
    }

    claims, ok := token.Claims.(jwt.MapClaims)
    if !ok {
        return "", errors.New("invalid token claims")
    }

    userID, ok := claims["user_id"].(string)
    if !ok {
        return "", errors.New("invalid user_id in token")
    }

    return userID, nil
}

func generateID() string {
    return fmt.Sprintf("%d", time.Now().UnixNano())
}
```

**services/post.go**

```go
package services

import (
    "blog-api/models"
    "time"
)

type PostService struct {
    db *Database
}

func NewPostService(db *Database) *PostService {
    return &PostService{db: db}
}

func (s *PostService) CreatePost(userID, title, content string) (*models.Post, error) {
    post := &models.Post{
        ID:        generateID(),
        UserID:    userID,
        Title:     title,
        Content:   content,
        CreatedAt: time.Now(),
        UpdatedAt: time.Now(),
    }

    if err := s.db.CreatePost(post); err != nil {
        return nil, err
    }

    return post, nil
}

func (s *PostService) GetPosts() ([]*models.Post, error) {
    return s.db.GetPosts()
}

func (s *PostService) GetPost(id string) (*models.Post, error) {
    return s.db.GetPost(id)
}
```

## Step 3: HTTP Handlers

**handlers/middleware.go**

```go
package handlers

import (
    "context"
    "net/http"
    "strings"

    "blog-api/services"
    "github.com/junioryono/godi/v2"
)

type contextKey string

const userIDKey contextKey = "userID"

func AuthMiddleware(provider godi.ServiceProvider) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Get token from header
            authHeader := r.Header.Get("Authorization")
            if authHeader == "" {
                http.Error(w, "Missing authorization header", http.StatusUnauthorized)
                return
            }

            // Extract token
            parts := strings.Split(authHeader, " ")
            if len(parts) != 2 || parts[0] != "Bearer" {
                http.Error(w, "Invalid authorization header", http.StatusUnauthorized)
                return
            }

            // Validate token
            authService, err := godi.Resolve[*services.AuthService](provider)
            if err != nil {
                http.Error(w, "Internal error", http.StatusInternalServerError)
                return
            }

            userID, err := authService.ValidateToken(parts[1])
            if err != nil {
                http.Error(w, "Invalid token", http.StatusUnauthorized)
                return
            }

            // Add user ID to context
            ctx := context.WithValue(r.Context(), userIDKey, userID)
            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

func GetUserID(ctx context.Context) string {
    userID, _ := ctx.Value(userIDKey).(string)
    return userID
}
```

**handlers/auth.go**

```go
package handlers

import (
    "encoding/json"
    "net/http"

    "blog-api/models"
    "blog-api/services"
    "github.com/junioryono/godi/v2"
)

type AuthHandler struct {
    provider godi.ServiceProvider
}

func NewAuthHandler(provider godi.ServiceProvider) *AuthHandler {
    return &AuthHandler{provider: provider}
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
    // Create request scope
    scope := h.provider.CreateScope(r.Context())
    defer scope.Close()

    // Parse request
    var req models.RegisterRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }

    // Get service
    authService, err := godi.Resolve[*services.AuthService](scope)
    if err != nil {
        http.Error(w, "Internal error", http.StatusInternalServerError)
        return
    }

    // Register user
    user, err := authService.Register(req.Username, req.Email, req.Password)
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    // Return response
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(user)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
    scope := h.provider.CreateScope(r.Context())
    defer scope.Close()

    var req models.LoginRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }

    authService, err := godi.Resolve[*services.AuthService](scope)
    if err != nil {
        http.Error(w, "Internal error", http.StatusInternalServerError)
        return
    }

    token, user, err := authService.Login(req.Username, req.Password)
    if err != nil {
        http.Error(w, err.Error(), http.StatusUnauthorized)
        return
    }

    resp := models.LoginResponse{
        Token: token,
        User:  *user,
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}
```

**handlers/post.go**

```go
package handlers

import (
    "encoding/json"
    "net/http"

    "blog-api/models"
    "blog-api/services"
    "github.com/junioryono/godi/v2"
)

type PostHandler struct {
    provider godi.ServiceProvider
}

func NewPostHandler(provider godi.ServiceProvider) *PostHandler {
    return &PostHandler{provider: provider}
}

func (h *PostHandler) CreatePost(w http.ResponseWriter, r *http.Request) {
    scope := h.provider.CreateScope(r.Context())
    defer scope.Close()

    // Get user ID from context
    userID := GetUserID(r.Context())

    // Parse request
    var req models.CreatePostRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request", http.StatusBadRequest)
        return
    }

    // Get service
    postService, err := godi.Resolve[*services.PostService](scope)
    if err != nil {
        http.Error(w, "Internal error", http.StatusInternalServerError)
        return
    }

    // Create post
    post, err := postService.CreatePost(userID, req.Title, req.Content)
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(post)
}

func (h *PostHandler) GetPosts(w http.ResponseWriter, r *http.Request) {
    scope := h.provider.CreateScope(r.Context())
    defer scope.Close()

    postService, err := godi.Resolve[*services.PostService](scope)
    if err != nil {
        http.Error(w, "Internal error", http.StatusInternalServerError)
        return
    }

    posts, err := postService.GetPosts()
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(posts)
}
```

## Step 4: Modules

**modules/core.go**

```go
package modules

import (
    "blog-api/services"
    "github.com/junioryono/godi/v2"
)

// CoreModule contains shared infrastructure
var CoreModule = godi.NewModule("core",
    godi.AddSingleton(services.NewDatabase),
)
```

**modules/auth.go**

```go
package modules

import (
    "blog-api/services"
    "github.com/junioryono/godi/v2"
)

// AuthModule contains authentication services
var AuthModule = godi.NewModule("auth",
    CoreModule, // Depends on database
    godi.AddScoped(services.NewAuthService),
)
```

**modules/blog.go**

```go
package modules

import (
    "blog-api/services"
    "github.com/junioryono/godi/v2"
)

// BlogModule contains blog services
var BlogModule = godi.NewModule("blog",
    CoreModule, // Depends on database
    godi.AddScoped(services.NewPostService),
    godi.AddScoped(services.NewCommentService),
)
```

**modules/web.go**

```go
package modules

import (
    "blog-api/handlers"
    "github.com/junioryono/godi/v2"
)

// WebModule contains HTTP handlers
var WebModule = godi.NewModule("web",
    AuthModule,
    BlogModule,
    godi.AddSingleton(handlers.NewAuthHandler),
    godi.AddSingleton(handlers.NewPostHandler),
)
```

## Step 5: Wire Everything Together

**main.go**

```go
package main

import (
    "log"
    "net/http"

    "blog-api/handlers"
    "blog-api/modules"
    "github.com/gorilla/mux"
    "github.com/junioryono/godi/v2"
)

func main() {
    // Create DI container
    services := godi.NewServiceCollection()

    // Add all modules
    if err := services.AddModules(modules.WebModule); err != nil {
        log.Fatal(err)
    }

    // Build provider
    provider, err := services.BuildServiceProvider()
    if err != nil {
        log.Fatal(err)
    }
    defer provider.Close()

    // Set up routes
    router := mux.NewRouter()

    // Auth routes
    authHandler, _ := godi.Resolve[*handlers.AuthHandler](provider)
    router.HandleFunc("/register", authHandler.Register).Methods("POST")
    router.HandleFunc("/login", authHandler.Login).Methods("POST")

    // Post routes (protected)
    postHandler, _ := godi.Resolve[*handlers.PostHandler](provider)
    protected := router.PathPrefix("/api").Subrouter()
    protected.Use(handlers.AuthMiddleware(provider))
    protected.HandleFunc("/posts", postHandler.CreatePost).Methods("POST")
    protected.HandleFunc("/posts", postHandler.GetPosts).Methods("GET")

    // Start server
    log.Println("Server starting on :8080")
    log.Fatal(http.ListenAndServe(":8080", router))
}
```

## Step 6: Test Your API

```bash
# Register a user
curl -X POST http://localhost:8080/register \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","email":"alice@example.com","password":"secret123"}'

# Login
curl -X POST http://localhost:8080/login \
  -H "Content-Type: application/json" \
  -d '{"username":"alice","password":"secret123"}'

# Save the token from login response, then create a post
curl -X POST http://localhost:8080/api/posts \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer YOUR_TOKEN_HERE" \
  -d '{"title":"My First Post","content":"Hello, World!"}'

# Get all posts
curl http://localhost:8080/api/posts \
  -H "Authorization: Bearer YOUR_TOKEN_HERE"
```

## Key Patterns

### 1. Request Scoping

Each HTTP request gets its own scope, ensuring isolation:

```go
scope := provider.CreateScope(r.Context())
defer scope.Close()
```

### 2. Module Organization

- **Core**: Shared infrastructure (database, config)
- **Domain**: Business logic (auth, blog services)
- **Web**: HTTP layer (handlers, middleware)

### 3. Clean Architecture

- Services don't know about HTTP
- Handlers don't contain business logic
- Easy to test each layer independently

## Testing Your Web App

```go
func TestCreatePost(t *testing.T) {
    // Test module with mocks
    testModule := godi.NewModule("test",
        godi.AddSingleton(func() *services.Database {
            return &MockDatabase{}
        }),
        godi.AddScoped(services.NewPostService),
    )

    services := godi.NewServiceCollection()
    services.AddModules(testModule)
    provider, _ := services.BuildServiceProvider()
    defer provider.Close()

    // Create test request
    req := httptest.NewRequest("POST", "/api/posts",
        strings.NewReader(`{"title":"Test","content":"Content"}`))
    req = req.WithContext(context.WithValue(req.Context(), userIDKey, "123"))

    // Test handler
    w := httptest.NewRecorder()
    handler := &PostHandler{provider: provider}
    handler.CreatePost(w, req)

    assert.Equal(t, http.StatusOK, w.Code)
}
```

## Next Steps

You now have a working web API with:

- ✅ Clean architecture
- ✅ Dependency injection
- ✅ Authentication
- ✅ Request isolation
- ✅ Easy testing

To extend this:

1. Add a real database (PostgreSQL/MySQL)
2. Add more features (comments, likes, follows)
3. Add validation and error handling
4. Deploy with Docker

The beauty of using godi: Adding new features is just creating new services and updating modules. The architecture scales with your needs!
