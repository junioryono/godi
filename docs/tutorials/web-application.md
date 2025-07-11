# Building a Web Application with godi

This tutorial will guide you through building a complete REST API using godi. We'll create a blog API with posts, comments, and user authentication to demonstrate real-world dependency injection patterns.

## Prerequisites

- Go 1.21 or later
- Basic knowledge of HTTP and REST APIs
- Completed the [Getting Started](getting-started.md) tutorial

## Project Setup

Create a new project:

```bash
mkdir blog-api
cd blog-api
go mod init blog-api
```

Install dependencies:

```bash
go get github.com/junioryono/godi
go get github.com/gorilla/mux
go get github.com/golang-jwt/jwt/v5
go get golang.org/x/crypto/bcrypt
```

## Project Structure

```
blog-api/
├── main.go
├── internal/
│   ├── models/
│   │   └── models.go
│   ├── services/
│   │   ├── auth.go
│   │   ├── post.go
│   │   └── user.go
│   ├── repositories/
│   │   ├── user.go
│   │   └── post.go
│   ├── handlers/
│   │   ├── auth.go
│   │   ├── post.go
│   │   └── middleware.go
│   └── config/
│       └── config.go
└── go.mod
```

## Step 1: Define Models and Interfaces

Create `internal/models/models.go`:

```go
package models

import (
    "time"
)

// User represents a blog user
type User struct {
    ID           string    `json:"id"`
    Username     string    `json:"username"`
    Email        string    `json:"email"`
    PasswordHash string    `json:"-"`
    CreatedAt    time.Time `json:"created_at"`
    UpdatedAt    time.Time `json:"updated_at"`
}

// Post represents a blog post
type Post struct {
    ID        string    `json:"id"`
    UserID    string    `json:"user_id"`
    Title     string    `json:"title"`
    Content   string    `json:"content"`
    Published bool      `json:"published"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
}

// Comment represents a comment on a post
type Comment struct {
    ID        string    `json:"id"`
    PostID    string    `json:"post_id"`
    UserID    string    `json:"user_id"`
    Content   string    `json:"content"`
    CreatedAt time.Time `json:"created_at"`
}

// Auth models
type LoginRequest struct {
    Username string `json:"username"`
    Password string `json:"password"`
}

type RegisterRequest struct {
    Username string `json:"username"`
    Email    string `json:"email"`
    Password string `json:"password"`
}

type AuthResponse struct {
    Token string `json:"token"`
    User  *User  `json:"user"`
}
```

## Step 2: Create Configuration

Create `internal/config/config.go`:

```go
package config

import (
    "os"
    "time"
)

type Config struct {
    Port            string
    JWTSecret       string
    JWTExpiration   time.Duration
    DatabaseURL     string
    AllowedOrigins  []string
}

func NewConfig() *Config {
    return &Config{
        Port:          getEnv("PORT", "8080"),
        JWTSecret:     getEnv("JWT_SECRET", "your-secret-key"),
        JWTExpiration: 24 * time.Hour,
        DatabaseURL:   getEnv("DATABASE_URL", "memory"),
        AllowedOrigins: []string{"*"},
    }
}

func getEnv(key, defaultValue string) string {
    if value := os.Getenv(key); value != "" {
        return value
    }
    return defaultValue
}
```

## Step 3: Implement Repositories

Create `internal/repositories/user.go`:

```go
package repositories

import (
    "context"
    "errors"
    "sync"
    "time"

    "blog-api/internal/models"
    "github.com/google/uuid"
)

var ErrNotFound = errors.New("not found")
var ErrDuplicate = errors.New("duplicate entry")

type UserRepository interface {
    Create(ctx context.Context, user *models.User) error
    GetByID(ctx context.Context, id string) (*models.User, error)
    GetByUsername(ctx context.Context, username string) (*models.User, error)
    GetByEmail(ctx context.Context, email string) (*models.User, error)
    Update(ctx context.Context, user *models.User) error
    Delete(ctx context.Context, id string) error
}

type InMemoryUserRepository struct {
    mu    sync.RWMutex
    users map[string]*models.User
    byUsername map[string]string // username -> userID
    byEmail    map[string]string // email -> userID
}

func NewInMemoryUserRepository() UserRepository {
    return &InMemoryUserRepository{
        users:      make(map[string]*models.User),
        byUsername: make(map[string]string),
        byEmail:    make(map[string]string),
    }
}

func (r *InMemoryUserRepository) Create(ctx context.Context, user *models.User) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    // Check duplicates
    if _, exists := r.byUsername[user.Username]; exists {
        return ErrDuplicate
    }
    if _, exists := r.byEmail[user.Email]; exists {
        return ErrDuplicate
    }

    user.ID = uuid.New().String()
    user.CreatedAt = time.Now()
    user.UpdatedAt = time.Now()

    r.users[user.ID] = user
    r.byUsername[user.Username] = user.ID
    r.byEmail[user.Email] = user.ID

    return nil
}

func (r *InMemoryUserRepository) GetByID(ctx context.Context, id string) (*models.User, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    user, exists := r.users[id]
    if !exists {
        return nil, ErrNotFound
    }

    // Return a copy
    userCopy := *user
    return &userCopy, nil
}

func (r *InMemoryUserRepository) GetByUsername(ctx context.Context, username string) (*models.User, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    userID, exists := r.byUsername[username]
    if !exists {
        return nil, ErrNotFound
    }

    user := r.users[userID]
    userCopy := *user
    return &userCopy, nil
}

func (r *InMemoryUserRepository) GetByEmail(ctx context.Context, email string) (*models.User, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    userID, exists := r.byEmail[email]
    if !exists {
        return nil, ErrNotFound
    }

    user := r.users[userID]
    userCopy := *user
    return &userCopy, nil
}

func (r *InMemoryUserRepository) Update(ctx context.Context, user *models.User) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    if _, exists := r.users[user.ID]; !exists {
        return ErrNotFound
    }

    user.UpdatedAt = time.Now()
    r.users[user.ID] = user

    return nil
}

func (r *InMemoryUserRepository) Delete(ctx context.Context, id string) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    user, exists := r.users[id]
    if !exists {
        return ErrNotFound
    }

    delete(r.users, id)
    delete(r.byUsername, user.Username)
    delete(r.byEmail, user.Email)

    return nil
}
```

Create `internal/repositories/post.go`:

```go
package repositories

import (
    "context"
    "sync"
    "time"

    "blog-api/internal/models"
    "github.com/google/uuid"
)

type PostRepository interface {
    Create(ctx context.Context, post *models.Post) error
    GetByID(ctx context.Context, id string) (*models.Post, error)
    GetByUserID(ctx context.Context, userID string) ([]*models.Post, error)
    GetPublished(ctx context.Context, limit, offset int) ([]*models.Post, error)
    Update(ctx context.Context, post *models.Post) error
    Delete(ctx context.Context, id string) error
}

type InMemoryPostRepository struct {
    mu    sync.RWMutex
    posts map[string]*models.Post
}

func NewInMemoryPostRepository() PostRepository {
    return &InMemoryPostRepository{
        posts: make(map[string]*models.Post),
    }
}

func (r *InMemoryPostRepository) Create(ctx context.Context, post *models.Post) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    post.ID = uuid.New().String()
    post.CreatedAt = time.Now()
    post.UpdatedAt = time.Now()

    r.posts[post.ID] = post
    return nil
}

func (r *InMemoryPostRepository) GetByID(ctx context.Context, id string) (*models.Post, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    post, exists := r.posts[id]
    if !exists {
        return nil, ErrNotFound
    }

    postCopy := *post
    return &postCopy, nil
}

func (r *InMemoryPostRepository) GetByUserID(ctx context.Context, userID string) ([]*models.Post, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    var posts []*models.Post
    for _, post := range r.posts {
        if post.UserID == userID {
            postCopy := *post
            posts = append(posts, &postCopy)
        }
    }

    return posts, nil
}

func (r *InMemoryPostRepository) GetPublished(ctx context.Context, limit, offset int) ([]*models.Post, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    var published []*models.Post
    for _, post := range r.posts {
        if post.Published {
            postCopy := *post
            published = append(published, &postCopy)
        }
    }

    // Simple pagination
    start := offset
    if start > len(published) {
        return []*models.Post{}, nil
    }

    end := start + limit
    if end > len(published) {
        end = len(published)
    }

    return published[start:end], nil
}

func (r *InMemoryPostRepository) Update(ctx context.Context, post *models.Post) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    if _, exists := r.posts[post.ID]; !exists {
        return ErrNotFound
    }

    post.UpdatedAt = time.Now()
    r.posts[post.ID] = post

    return nil
}

func (r *InMemoryPostRepository) Delete(ctx context.Context, id string) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    if _, exists := r.posts[id]; !exists {
        return ErrNotFound
    }

    delete(r.posts, id)
    return nil
}
```

## Step 4: Implement Services

Create `internal/services/auth.go`:

```go
package services

import (
    "context"
    "errors"
    "time"

    "blog-api/internal/config"
    "blog-api/internal/models"
    "blog-api/internal/repositories"

    "github.com/golang-jwt/jwt/v5"
    "golang.org/x/crypto/bcrypt"
)

var (
    ErrInvalidCredentials = errors.New("invalid credentials")
    ErrUserExists        = errors.New("user already exists")
)

type AuthService interface {
    Register(ctx context.Context, req *models.RegisterRequest) (*models.AuthResponse, error)
    Login(ctx context.Context, req *models.LoginRequest) (*models.AuthResponse, error)
    ValidateToken(token string) (string, error) // returns userID
}

type JWTAuthService struct {
    userRepo repositories.UserRepository
    config   *config.Config
}

func NewAuthService(userRepo repositories.UserRepository, config *config.Config) AuthService {
    return &JWTAuthService{
        userRepo: userRepo,
        config:   config,
    }
}

func (s *JWTAuthService) Register(ctx context.Context, req *models.RegisterRequest) (*models.AuthResponse, error) {
    // Check if user exists
    if _, err := s.userRepo.GetByUsername(ctx, req.Username); err == nil {
        return nil, ErrUserExists
    }
    if _, err := s.userRepo.GetByEmail(ctx, req.Email); err == nil {
        return nil, ErrUserExists
    }

    // Hash password
    hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
    if err != nil {
        return nil, err
    }

    // Create user
    user := &models.User{
        Username:     req.Username,
        Email:        req.Email,
        PasswordHash: string(hashedPassword),
    }

    if err := s.userRepo.Create(ctx, user); err != nil {
        return nil, err
    }

    // Generate token
    token, err := s.generateToken(user.ID)
    if err != nil {
        return nil, err
    }

    return &models.AuthResponse{
        Token: token,
        User:  user,
    }, nil
}

func (s *JWTAuthService) Login(ctx context.Context, req *models.LoginRequest) (*models.AuthResponse, error) {
    // Find user
    user, err := s.userRepo.GetByUsername(ctx, req.Username)
    if err != nil {
        return nil, ErrInvalidCredentials
    }

    // Check password
    if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
        return nil, ErrInvalidCredentials
    }

    // Generate token
    token, err := s.generateToken(user.ID)
    if err != nil {
        return nil, err
    }

    return &models.AuthResponse{
        Token: token,
        User:  user,
    }, nil
}

func (s *JWTAuthService) ValidateToken(tokenString string) (string, error) {
    token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
        if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
            return nil, errors.New("unexpected signing method")
        }
        return []byte(s.config.JWTSecret), nil
    })

    if err != nil {
        return "", err
    }

    if claims, ok := token.Claims.(jwt.MapClaims); ok && token.Valid {
        userID, ok := claims["user_id"].(string)
        if !ok {
            return "", errors.New("invalid token claims")
        }
        return userID, nil
    }

    return "", errors.New("invalid token")
}

func (s *JWTAuthService) generateToken(userID string) (string, error) {
    claims := jwt.MapClaims{
        "user_id": userID,
        "exp":     time.Now().Add(s.config.JWTExpiration).Unix(),
    }

    token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
    return token.SignedString([]byte(s.config.JWTSecret))
}
```

Create `internal/services/post.go`:

```go
package services

import (
    "context"
    "errors"

    "blog-api/internal/models"
    "blog-api/internal/repositories"
)

var (
    ErrUnauthorized = errors.New("unauthorized")
    ErrNotFound     = errors.New("not found")
)

type PostService interface {
    CreatePost(ctx context.Context, userID string, title, content string) (*models.Post, error)
    GetPost(ctx context.Context, id string) (*models.Post, error)
    GetUserPosts(ctx context.Context, userID string) ([]*models.Post, error)
    GetPublishedPosts(ctx context.Context, page, pageSize int) ([]*models.Post, error)
    UpdatePost(ctx context.Context, userID, postID string, title, content string) (*models.Post, error)
    PublishPost(ctx context.Context, userID, postID string) error
    DeletePost(ctx context.Context, userID, postID string) error
}

type DefaultPostService struct {
    postRepo repositories.PostRepository
    userRepo repositories.UserRepository
}

func NewPostService(postRepo repositories.PostRepository, userRepo repositories.UserRepository) PostService {
    return &DefaultPostService{
        postRepo: postRepo,
        userRepo: userRepo,
    }
}

func (s *DefaultPostService) CreatePost(ctx context.Context, userID string, title, content string) (*models.Post, error) {
    // Verify user exists
    if _, err := s.userRepo.GetByID(ctx, userID); err != nil {
        return nil, ErrUnauthorized
    }

    post := &models.Post{
        UserID:    userID,
        Title:     title,
        Content:   content,
        Published: false,
    }

    if err := s.postRepo.Create(ctx, post); err != nil {
        return nil, err
    }

    return post, nil
}

func (s *DefaultPostService) GetPost(ctx context.Context, id string) (*models.Post, error) {
    post, err := s.postRepo.GetByID(ctx, id)
    if err != nil {
        if errors.Is(err, repositories.ErrNotFound) {
            return nil, ErrNotFound
        }
        return nil, err
    }

    return post, nil
}

func (s *DefaultPostService) GetUserPosts(ctx context.Context, userID string) ([]*models.Post, error) {
    return s.postRepo.GetByUserID(ctx, userID)
}

func (s *DefaultPostService) GetPublishedPosts(ctx context.Context, page, pageSize int) ([]*models.Post, error) {
    offset := (page - 1) * pageSize
    return s.postRepo.GetPublished(ctx, pageSize, offset)
}

func (s *DefaultPostService) UpdatePost(ctx context.Context, userID, postID string, title, content string) (*models.Post, error) {
    post, err := s.postRepo.GetByID(ctx, postID)
    if err != nil {
        return nil, ErrNotFound
    }

    if post.UserID != userID {
        return nil, ErrUnauthorized
    }

    post.Title = title
    post.Content = content

    if err := s.postRepo.Update(ctx, post); err != nil {
        return nil, err
    }

    return post, nil
}

func (s *DefaultPostService) PublishPost(ctx context.Context, userID, postID string) error {
    post, err := s.postRepo.GetByID(ctx, postID)
    if err != nil {
        return ErrNotFound
    }

    if post.UserID != userID {
        return ErrUnauthorized
    }

    post.Published = true
    return s.postRepo.Update(ctx, post)
}

func (s *DefaultPostService) DeletePost(ctx context.Context, userID, postID string) error {
    post, err := s.postRepo.GetByID(ctx, postID)
    if err != nil {
        return ErrNotFound
    }

    if post.UserID != userID {
        return ErrUnauthorized
    }

    return s.postRepo.Delete(ctx, postID)
}
```

## Step 5: Create HTTP Handlers

Create `internal/handlers/middleware.go`:

```go
package handlers

import (
    "context"
    "net/http"
    "strings"

    "blog-api/internal/services"
    "github.com/junioryono/godi"
)

type contextKey string

const (
    userIDKey contextKey = "userID"
    scopeKey  contextKey = "scope"
)

// DIMiddleware creates a scope for each request
func DIMiddleware(provider godi.ServiceProvider) func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            // Create scope for this request
            scope := provider.CreateScope(r.Context())
            defer scope.Close()

            // Add scope to context
            ctx := context.WithValue(r.Context(), scopeKey, scope)

            next.ServeHTTP(w, r.WithContext(ctx))
        })
    }
}

// AuthMiddleware validates JWT tokens
func AuthMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Get scope from context
        scope := r.Context().Value(scopeKey).(godi.Scope)

        // Resolve auth service
        authService, err := godi.Resolve[services.AuthService](scope.ServiceProvider())
        if err != nil {
            http.Error(w, "Internal server error", http.StatusInternalServerError)
            return
        }

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

// GetUserID retrieves the authenticated user ID from context
func GetUserID(ctx context.Context) string {
    userID, _ := ctx.Value(userIDKey).(string)
    return userID
}

// GetScope retrieves the DI scope from context
func GetScope(ctx context.Context) godi.Scope {
    scope, _ := ctx.Value(scopeKey).(godi.Scope)
    return scope
}
```

Create `internal/handlers/auth.go`:

```go
package handlers

import (
    "encoding/json"
    "net/http"

    "blog-api/internal/models"
    "blog-api/internal/services"
    "github.com/junioryono/godi"
)

type AuthHandler struct{}

func NewAuthHandler() *AuthHandler {
    return &AuthHandler{}
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
    scope := GetScope(r.Context())
    authService, err := godi.Resolve[services.AuthService](scope.ServiceProvider())
    if err != nil {
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }

    var req models.RegisterRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request body", http.StatusBadRequest)
        return
    }

    resp, err := authService.Register(r.Context(), &req)
    if err != nil {
        if err == services.ErrUserExists {
            http.Error(w, "User already exists", http.StatusConflict)
            return
        }
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
    scope := GetScope(r.Context())
    authService, err := godi.Resolve[services.AuthService](scope.ServiceProvider())
    if err != nil {
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }

    var req models.LoginRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request body", http.StatusBadRequest)
        return
    }

    resp, err := authService.Login(r.Context(), &req)
    if err != nil {
        if err == services.ErrInvalidCredentials {
            http.Error(w, "Invalid credentials", http.StatusUnauthorized)
            return
        }
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(resp)
}
```

Create `internal/handlers/post.go`:

```go
package handlers

import (
    "encoding/json"
    "net/http"
    "strconv"

    "blog-api/internal/services"
    "github.com/gorilla/mux"
    "github.com/junioryono/godi"
)

type PostHandler struct{}

func NewPostHandler() *PostHandler {
    return &PostHandler{}
}

type CreatePostRequest struct {
    Title   string `json:"title"`
    Content string `json:"content"`
}

type UpdatePostRequest struct {
    Title   string `json:"title"`
    Content string `json:"content"`
}

func (h *PostHandler) CreatePost(w http.ResponseWriter, r *http.Request) {
    userID := GetUserID(r.Context())
    scope := GetScope(r.Context())

    postService, err := godi.Resolve[services.PostService](scope.ServiceProvider())
    if err != nil {
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }

    var req CreatePostRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request body", http.StatusBadRequest)
        return
    }

    post, err := postService.CreatePost(r.Context(), userID, req.Title, req.Content)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)
    json.NewEncoder(w).Encode(post)
}

func (h *PostHandler) GetPost(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    postID := vars["id"]

    scope := GetScope(r.Context())
    postService, err := godi.Resolve[services.PostService](scope.ServiceProvider())
    if err != nil {
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }

    post, err := postService.GetPost(r.Context(), postID)
    if err != nil {
        if err == services.ErrNotFound {
            http.Error(w, "Post not found", http.StatusNotFound)
            return
        }
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(post)
}

func (h *PostHandler) GetPublishedPosts(w http.ResponseWriter, r *http.Request) {
    page, _ := strconv.Atoi(r.URL.Query().Get("page"))
    if page < 1 {
        page = 1
    }

    pageSize, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
    if pageSize < 1 || pageSize > 100 {
        pageSize = 10
    }

    scope := GetScope(r.Context())
    postService, err := godi.Resolve[services.PostService](scope.ServiceProvider())
    if err != nil {
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }

    posts, err := postService.GetPublishedPosts(r.Context(), page, pageSize)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(posts)
}

func (h *PostHandler) UpdatePost(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    postID := vars["id"]
    userID := GetUserID(r.Context())

    scope := GetScope(r.Context())
    postService, err := godi.Resolve[services.PostService](scope.ServiceProvider())
    if err != nil {
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }

    var req UpdatePostRequest
    if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
        http.Error(w, "Invalid request body", http.StatusBadRequest)
        return
    }

    post, err := postService.UpdatePost(r.Context(), userID, postID, req.Title, req.Content)
    if err != nil {
        switch err {
        case services.ErrNotFound:
            http.Error(w, "Post not found", http.StatusNotFound)
        case services.ErrUnauthorized:
            http.Error(w, "Unauthorized", http.StatusForbidden)
        default:
            http.Error(w, err.Error(), http.StatusInternalServerError)
        }
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(post)
}

func (h *PostHandler) PublishPost(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    postID := vars["id"]
    userID := GetUserID(r.Context())

    scope := GetScope(r.Context())
    postService, err := godi.Resolve[services.PostService](scope.ServiceProvider())
    if err != nil {
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }

    err = postService.PublishPost(r.Context(), userID, postID)
    if err != nil {
        switch err {
        case services.ErrNotFound:
            http.Error(w, "Post not found", http.StatusNotFound)
        case services.ErrUnauthorized:
            http.Error(w, "Unauthorized", http.StatusForbidden)
        default:
            http.Error(w, err.Error(), http.StatusInternalServerError)
        }
        return
    }

    w.WriteHeader(http.StatusNoContent)
}

func (h *PostHandler) DeletePost(w http.ResponseWriter, r *http.Request) {
    vars := mux.Vars(r)
    postID := vars["id"]
    userID := GetUserID(r.Context())

    scope := GetScope(r.Context())
    postService, err := godi.Resolve[services.PostService](scope.ServiceProvider())
    if err != nil {
        http.Error(w, "Internal server error", http.StatusInternalServerError)
        return
    }

    err = postService.DeletePost(r.Context(), userID, postID)
    if err != nil {
        switch err {
        case services.ErrNotFound:
            http.Error(w, "Post not found", http.StatusNotFound)
        case services.ErrUnauthorized:
            http.Error(w, "Unauthorized", http.StatusForbidden)
        default:
            http.Error(w, err.Error(), http.StatusInternalServerError)
        }
        return
    }

    w.WriteHeader(http.StatusNoContent)
}
```

## Step 6: Wire Everything with godi

Create `main.go`:

```go
package main

import (
    "log"
    "net/http"

    "blog-api/internal/config"
    "blog-api/internal/handlers"
    "blog-api/internal/repositories"
    "blog-api/internal/services"

    "github.com/gorilla/mux"
    "github.com/junioryono/godi"
)

func main() {
    // Create service collection
    collection := godi.NewServiceCollection()

    // Register configuration
    collection.AddSingleton(config.NewConfig)

    // Register repositories
    collection.AddSingleton(repositories.NewInMemoryUserRepository)
    collection.AddSingleton(repositories.NewInMemoryPostRepository)

    // Register services
    collection.AddScoped(services.NewAuthService)
    collection.AddScoped(services.NewPostService)

    // Register handlers
    collection.AddScoped(handlers.NewAuthHandler)
    collection.AddScoped(handlers.NewPostHandler)

    // Build service provider
    provider, err := collection.BuildServiceProvider()
    if err != nil {
        log.Fatal("Failed to build service provider:", err)
    }
    defer provider.Close()

    // Get configuration
    cfg, err := godi.Resolve[*config.Config](provider)
    if err != nil {
        log.Fatal("Failed to resolve config:", err)
    }

    // Setup routes
    router := setupRoutes(provider)

    // Start server
    log.Printf("Server starting on port %s", cfg.Port)
    if err := http.ListenAndServe(":"+cfg.Port, router); err != nil {
        log.Fatal("Server failed:", err)
    }
}

func setupRoutes(provider godi.ServiceProvider) *mux.Router {
    router := mux.NewRouter()

    // Apply DI middleware to all routes
    router.Use(handlers.DIMiddleware(provider))

    // Public routes
    router.HandleFunc("/api/auth/register", func(w http.ResponseWriter, r *http.Request) {
        scope := handlers.GetScope(r.Context())
        handler, _ := godi.Resolve[*handlers.AuthHandler](scope.ServiceProvider())
        handler.Register(w, r)
    }).Methods("POST")

    router.HandleFunc("/api/auth/login", func(w http.ResponseWriter, r *http.Request) {
        scope := handlers.GetScope(r.Context())
        handler, _ := godi.Resolve[*handlers.AuthHandler](scope.ServiceProvider())
        handler.Login(w, r)
    }).Methods("POST")

    router.HandleFunc("/api/posts", func(w http.ResponseWriter, r *http.Request) {
        scope := handlers.GetScope(r.Context())
        handler, _ := godi.Resolve[*handlers.PostHandler](scope.ServiceProvider())
        handler.GetPublishedPosts(w, r)
    }).Methods("GET")

    router.HandleFunc("/api/posts/{id}", func(w http.ResponseWriter, r *http.Request) {
        scope := handlers.GetScope(r.Context())
        handler, _ := godi.Resolve[*handlers.PostHandler](scope.ServiceProvider())
        handler.GetPost(w, r)
    }).Methods("GET")

    // Protected routes
    protected := router.PathPrefix("/api").Subrouter()
    protected.Use(handlers.AuthMiddleware)

    protected.HandleFunc("/posts", func(w http.ResponseWriter, r *http.Request) {
        scope := handlers.GetScope(r.Context())
        handler, _ := godi.Resolve[*handlers.PostHandler](scope.ServiceProvider())
        handler.CreatePost(w, r)
    }).Methods("POST")

    protected.HandleFunc("/posts/{id}", func(w http.ResponseWriter, r *http.Request) {
        scope := handlers.GetScope(r.Context())
        handler, _ := godi.Resolve[*handlers.PostHandler](scope.ServiceProvider())
        handler.UpdatePost(w, r)
    }).Methods("PUT")

    protected.HandleFunc("/posts/{id}/publish", func(w http.ResponseWriter, r *http.Request) {
        scope := handlers.GetScope(r.Context())
        handler, _ := godi.Resolve[*handlers.PostHandler](scope.ServiceProvider())
        handler.PublishPost(w, r)
    }).Methods("POST")

    protected.HandleFunc("/posts/{id}", func(w http.ResponseWriter, r *http.Request) {
        scope := handlers.GetScope(r.Context())
        handler, _ := godi.Resolve[*handlers.PostHandler](scope.ServiceProvider())
        handler.DeletePost(w, r)
    }).Methods("DELETE")

    return router
}
```

## Step 7: Test the API

Run the application:

```bash
go run main.go
```

Test the endpoints:

```bash
# Register a user
curl -X POST http://localhost:8080/api/auth/register \
  -H "Content-Type: application/json" \
  -d '{"username":"john","email":"john@example.com","password":"secret123"}'

# Login
curl -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"john","password":"secret123"}'

# Save the token from the login response
TOKEN="your-jwt-token"

# Create a post
curl -X POST http://localhost:8080/api/posts \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"title":"My First Post","content":"Hello, World!"}'

# Publish the post
curl -X POST http://localhost:8080/api/posts/{post-id}/publish \
  -H "Authorization: Bearer $TOKEN"

# Get published posts (public)
curl http://localhost:8080/api/posts
```

## Key Takeaways

1. **Request Scoping**: Each HTTP request gets its own scope, ensuring proper isolation of services.

2. **Middleware Pattern**: The DI middleware creates scopes automatically for each request.

3. **Clean Architecture**: Services don't know about HTTP concerns, making them testable and reusable.

4. **Automatic Wiring**: godi handles all dependency injection - we just declare what we need.

5. **Lifetime Management**:
   - Config and repositories are singletons (shared)
   - Services are scoped (per-request)
   - Proper cleanup with `defer scope.Close()`

## Next Steps

- Add database persistence with transactions
- Implement comment functionality
- Add rate limiting and caching
- Deploy with Docker
- Add comprehensive tests

Check out the [Testing Tutorial](testing.md) to learn how to test this application!
