# Building Microservices with godi

This tutorial demonstrates how to build a microservices architecture using godi. We'll create a simple e-commerce system with separate services for users, products, and orders, showing how DI helps manage complexity in distributed systems.

## Architecture Overview

We'll build three microservices:

- **User Service** - Manages user accounts and authentication
- **Product Service** - Handles product catalog
- **Order Service** - Processes orders (depends on User and Product services)

Each service will have:

- Its own DI container
- gRPC API
- HTTP gateway
- Shared infrastructure components

## Project Structure

```
microservices-demo/
├── shared/
│   ├── config/
│   ├── logging/
│   ├── tracing/
│   └── proto/
├── services/
│   ├── user/
│   │   ├── cmd/
│   │   ├── internal/
│   │   └── proto/
│   ├── product/
│   │   ├── cmd/
│   │   ├── internal/
│   │   └── proto/
│   └── order/
│       ├── cmd/
│       ├── internal/
│       └── proto/
└── docker-compose.yml
```

## Step 1: Shared Infrastructure

Create shared components that all services will use.

### Shared Configuration

Create `shared/config/config.go`:

```go
package config

import (
    "os"
    "time"
)

// ServiceConfig is common configuration for all services
type ServiceConfig struct {
    ServiceName     string
    Environment     string
    LogLevel        string

    // Server settings
    GRPCPort        string
    HTTPPort        string
    ShutdownTimeout time.Duration

    // Database
    DatabaseURL     string

    // Tracing
    JaegerEndpoint  string

    // Service discovery
    ConsulAddress   string
}
```

### User Service Main

Create `services/user/cmd/main.go`:

```go
package main

import (
    "context"
    "log"

    "github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"

    "github.com/junioryono/godi"
    "shared/service"
    "services/user/internal/repository"
    "services/user/internal/service"
    "services/user/internal/token"
    pb "services/user/proto"
)

func main() {
    baseService, err := service.NewBaseService("user-service", service.ServiceOptions{
        ConfigureServices: configureServices,
        RegisterGRPC:      registerGRPC,
        RegisterHTTP:      registerHTTP,
    })
    if err != nil {
        log.Fatal("Failed to create service:", err)
    }

    if err := baseService.Run(); err != nil {
        log.Fatal("Service failed:", err)
    }
}

func configureServices(collection godi.ServiceCollection) error {
    // Register repositories
    collection.AddSingleton(repository.NewPostgresUserRepository)

    // Register services
    collection.AddSingleton(token.NewJWTTokenService)
    collection.AddScoped(service.NewUserService)

    return nil
}

func registerGRPC(server *grpc.Server) {
    // This will be called with the base service's provider
    // We'll resolve the service in the actual handler registration
}

func registerHTTP(ctx context.Context, mux *runtime.ServeMux, endpoint string) error {
    opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
    return pb.RegisterUserServiceHandlerFromEndpoint(ctx, mux, endpoint, opts)
}

// Custom gRPC registration that uses DI
func RegisterUserServiceServer(server *grpc.Server, provider godi.ServiceProvider) {
    pb.RegisterUserServiceServer(server, &userServiceServer{provider: provider})
}

type userServiceServer struct {
    pb.UnimplementedUserServiceServer
    provider godi.ServiceProvider
}

func (s *userServiceServer) CreateUser(ctx context.Context, req *pb.CreateUserRequest) (*pb.CreateUserResponse, error) {
    scope := ctx.Value("scope").(godi.Scope)
    userService, err := godi.Resolve[*service.UserService](scope.ServiceProvider())
    if err != nil {
        return nil, err
    }
    return userService.CreateUser(ctx, req)
}

func (s *userServiceServer) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.GetUserResponse, error) {
    scope := ctx.Value("scope").(godi.Scope)
    userService, err := godi.Resolve[*service.UserService](scope.ServiceProvider())
    if err != nil {
        return nil, err
    }
    return userService.GetUser(ctx, req)
}

func (s *userServiceServer) AuthenticateUser(ctx context.Context, req *pb.AuthenticateUserRequest) (*pb.AuthenticateUserResponse, error) {
    scope := ctx.Value("scope").(godi.Scope)
    userService, err := godi.Resolve[*service.UserService](scope.ServiceProvider())
    if err != nil {
        return nil, err
    }
    return userService.AuthenticateUser(ctx, req)
}
```

## Step 4: Product Service

Create `services/product/internal/service/product_service.go`:

```go
package service

import (
    "context"
    "time"

    "github.com/google/uuid"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"

    "shared/logging"
    "shared/tracing"
    pb "services/product/proto"
)

type ProductRepository interface {
    Create(ctx context.Context, product *Product) error
    GetByID(ctx context.Context, id string) (*Product, error)
    List(ctx context.Context, limit, offset int) ([]*Product, error)
    Update(ctx context.Context, product *Product) error
}

type Product struct {
    ID          string
    Name        string
    Description string
    Price       float64
    Stock       int32
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

type ProductService struct {
    pb.UnimplementedProductServiceServer

    repo   ProductRepository
    logger logging.Logger
    tracer tracing.Tracer
}

func NewProductService(
    repo ProductRepository,
    logger logging.Logger,
    tracer tracing.Tracer,
) *ProductService {
    return &ProductService{
        repo:   repo,
        logger: logger,
        tracer: tracer,
    }
}

func (s *ProductService) CreateProduct(ctx context.Context, req *pb.CreateProductRequest) (*pb.CreateProductResponse, error) {
    ctx, span := s.tracer.StartSpan(ctx, "ProductService.CreateProduct")
    defer span.End()

    product := &Product{
        ID:          uuid.New().String(),
        Name:        req.Name,
        Description: req.Description,
        Price:       req.Price,
        Stock:       req.Stock,
        CreatedAt:   time.Now(),
        UpdatedAt:   time.Now(),
    }

    if err := s.repo.Create(ctx, product); err != nil {
        s.logger.Error("Failed to create product", logging.Error(err))
        return nil, status.Error(codes.Internal, "failed to create product")
    }

    return &pb.CreateProductResponse{
        Product: s.toProtoProduct(product),
    }, nil
}

func (s *ProductService) GetProduct(ctx context.Context, req *pb.GetProductRequest) (*pb.GetProductResponse, error) {
    ctx, span := s.tracer.StartSpan(ctx, "ProductService.GetProduct")
    defer span.End()

    product, err := s.repo.GetByID(ctx, req.Id)
    if err != nil {
        return nil, status.Error(codes.NotFound, "product not found")
    }

    return &pb.GetProductResponse{
        Product: s.toProtoProduct(product),
    }, nil
}

func (s *ProductService) ListProducts(ctx context.Context, req *pb.ListProductsRequest) (*pb.ListProductsResponse, error) {
    ctx, span := s.tracer.StartSpan(ctx, "ProductService.ListProducts")
    defer span.End()

    limit := int(req.PageSize)
    if limit <= 0 {
        limit = 10
    }

    offset := int(req.Page * req.PageSize)

    products, err := s.repo.List(ctx, limit, offset)
    if err != nil {
        s.logger.Error("Failed to list products", logging.Error(err))
        return nil, status.Error(codes.Internal, "failed to list products")
    }

    pbProducts := make([]*pb.Product, len(products))
    for i, p := range products {
        pbProducts[i] = s.toProtoProduct(p)
    }

    return &pb.ListProductsResponse{
        Products: pbProducts,
    }, nil
}

func (s *ProductService) toProtoProduct(p *Product) *pb.Product {
    return &pb.Product{
        Id:          p.ID,
        Name:        p.Name,
        Description: p.Description,
        Price:       p.Price,
        Stock:       p.Stock,
        CreatedAt:   p.CreatedAt.Unix(),
        UpdatedAt:   p.UpdatedAt.Unix(),
    }
}
```

## Step 5: Order Service (Depends on User and Product)

Create `services/order/internal/service/order_service.go`:

```go
package service

import (
    "context"
    "fmt"
    "time"

    "github.com/google/uuid"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/metadata"
    "google.golang.org/grpc/status"

    "shared/logging"
    "shared/tracing"
    orderpb "services/order/proto"
    productpb "services/product/proto"
    userpb "services/user/proto"
)

type OrderRepository interface {
    Create(ctx context.Context, order *Order) error
    GetByID(ctx context.Context, id string) (*Order, error)
    GetByUserID(ctx context.Context, userID string) ([]*Order, error)
}

type Order struct {
    ID        string
    UserID    string
    Items     []OrderItem
    Total     float64
    Status    string
    CreatedAt time.Time
}

type OrderItem struct {
    ProductID string
    Quantity  int32
    Price     float64
}

// External service clients
type UserServiceClient interface {
    GetUser(ctx context.Context, userID string) (*userpb.User, error)
}

type ProductServiceClient interface {
    GetProduct(ctx context.Context, productID string) (*productpb.Product, error)
    UpdateStock(ctx context.Context, productID string, quantity int32) error
}

type OrderService struct {
    orderpb.UnimplementedOrderServiceServer

    repo        OrderRepository
    userClient  UserServiceClient
    prodClient  ProductServiceClient
    logger      logging.Logger
    tracer      tracing.Tracer
}

func NewOrderService(
    repo OrderRepository,
    userClient UserServiceClient,
    prodClient ProductServiceClient,
    logger logging.Logger,
    tracer tracing.Tracer,
) *OrderService {
    return &OrderService{
        repo:       repo,
        userClient: userClient,
        prodClient: prodClient,
        logger:     logger,
        tracer:     tracer,
    }
}

func (s *OrderService) CreateOrder(ctx context.Context, req *orderpb.CreateOrderRequest) (*orderpb.CreateOrderResponse, error) {
    ctx, span := s.tracer.StartSpan(ctx, "OrderService.CreateOrder")
    defer span.End()

    // Extract user ID from context (set by auth middleware)
    userID := s.getUserIDFromContext(ctx)
    if userID == "" {
        return nil, status.Error(codes.Unauthenticated, "user not authenticated")
    }

    // Verify user exists
    user, err := s.userClient.GetUser(ctx, userID)
    if err != nil {
        s.logger.Error("Failed to get user", logging.Error(err))
        return nil, status.Error(codes.NotFound, "user not found")
    }

    // Process order items
    orderItems := make([]OrderItem, 0, len(req.Items))
    total := 0.0

    for _, item := range req.Items {
        // Get product details
        product, err := s.prodClient.GetProduct(ctx, item.ProductId)
        if err != nil {
            return nil, status.Errorf(codes.NotFound, "product %s not found", item.ProductId)
        }

        // Check stock
        if product.Stock < item.Quantity {
            return nil, status.Errorf(codes.FailedPrecondition,
                "insufficient stock for product %s", product.Name)
        }

        orderItems = append(orderItems, OrderItem{
            ProductID: item.ProductId,
            Quantity:  item.Quantity,
            Price:     product.Price,
        })

        total += product.Price * float64(item.Quantity)
    }

    // Create order
    order := &Order{
        ID:        uuid.New().String(),
        UserID:    userID,
        Items:     orderItems,
        Total:     total,
        Status:    "pending",
        CreatedAt: time.Now(),
    }

    if err := s.repo.Create(ctx, order); err != nil {
        s.logger.Error("Failed to create order", logging.Error(err))
        return nil, status.Error(codes.Internal, "failed to create order")
    }

    // Update product stock (in real system, this would be transactional)
    for _, item := range orderItems {
        if err := s.prodClient.UpdateStock(ctx, item.ProductID, -item.Quantity); err != nil {
            s.logger.Error("Failed to update stock",
                logging.String("product_id", item.ProductID),
                logging.Error(err))
            // In production, we'd need to handle this properly
        }
    }

    s.logger.Info("Order created",
        logging.String("order_id", order.ID),
        logging.String("user_id", userID),
        logging.Any("total", total))

    return &orderpb.CreateOrderResponse{
        Order: s.toProtoOrder(order),
        User:  user,
    }, nil
}

func (s *OrderService) GetOrder(ctx context.Context, req *orderpb.GetOrderRequest) (*orderpb.GetOrderResponse, error) {
    ctx, span := s.tracer.StartSpan(ctx, "OrderService.GetOrder")
    defer span.End()

    order, err := s.repo.GetByID(ctx, req.Id)
    if err != nil {
        return nil, status.Error(codes.NotFound, "order not found")
    }

    // Verify user can access this order
    userID := s.getUserIDFromContext(ctx)
    if order.UserID != userID {
        return nil, status.Error(codes.PermissionDenied, "access denied")
    }

    return &orderpb.GetOrderResponse{
        Order: s.toProtoOrder(order),
    }, nil
}

func (s *OrderService) getUserIDFromContext(ctx context.Context) string {
    if md, ok := metadata.FromIncomingContext(ctx); ok {
        if userIDs := md.Get("user-id"); len(userIDs) > 0 {
            return userIDs[0]
        }
    }
    return ""
}

func (s *OrderService) toProtoOrder(o *Order) *orderpb.Order {
    items := make([]*orderpb.OrderItem, len(o.Items))
    for i, item := range o.Items {
        items[i] = &orderpb.OrderItem{
            ProductId: item.ProductID,
            Quantity:  item.Quantity,
            Price:     item.Price,
        }
    }

    return &orderpb.Order{
        Id:        o.ID,
        UserId:    o.UserID,
        Items:     items,
        Total:     o.Total,
        Status:    o.Status,
        CreatedAt: o.CreatedAt.Unix(),
    }
}
```

### Service Client Implementations

Create `services/order/internal/clients/clients.go`:

```go
package clients

import (
    "context"
    "fmt"

    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"

    "shared/discovery"
    "shared/logging"
    productpb "services/product/proto"
    userpb "services/user/proto"
)

// UserServiceClient implementation
type userServiceClient struct {
    discovery discovery.ServiceDiscovery
    logger    logging.Logger
}

func NewUserServiceClient(discovery discovery.ServiceDiscovery, logger logging.Logger) *userServiceClient {
    return &userServiceClient{
        discovery: discovery,
        logger:    logger,
    }
}

func (c *userServiceClient) GetUser(ctx context.Context, userID string) (*userpb.User, error) {
    // Discover user service
    instances, err := c.discovery.Discover("user-service")
    if err != nil || len(instances) == 0 {
        return nil, fmt.Errorf("user service not available")
    }

    // Use first instance (in production, use load balancing)
    instance := instances[0]
    target := fmt.Sprintf("%s:%d", instance.Address, instance.Port)

    // Create connection
    conn, err := grpc.DialContext(ctx, target,
        grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        return nil, err
    }
    defer conn.Close()

    // Make request
    client := userpb.NewUserServiceClient(conn)
    resp, err := client.GetUser(ctx, &userpb.GetUserRequest{Id: userID})
    if err != nil {
        return nil, err
    }

    return resp.User, nil
}

// ProductServiceClient implementation
type productServiceClient struct {
    discovery discovery.ServiceDiscovery
    logger    logging.Logger
}

func NewProductServiceClient(discovery discovery.ServiceDiscovery, logger logging.Logger) *productServiceClient {
    return &productServiceClient{
        discovery: discovery,
        logger:    logger,
    }
}

func (c *productServiceClient) GetProduct(ctx context.Context, productID string) (*productpb.Product, error) {
    instances, err := c.discovery.Discover("product-service")
    if err != nil || len(instances) == 0 {
        return nil, fmt.Errorf("product service not available")
    }

    instance := instances[0]
    target := fmt.Sprintf("%s:%d", instance.Address, instance.Port)

    conn, err := grpc.DialContext(ctx, target,
        grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        return nil, err
    }
    defer conn.Close()

    client := productpb.NewProductServiceClient(conn)
    resp, err := client.GetProduct(ctx, &productpb.GetProductRequest{Id: productID})
    if err != nil {
        return nil, err
    }

    return resp.Product, nil
}

func (c *productServiceClient) UpdateStock(ctx context.Context, productID string, quantity int32) error {
    // Implementation for updating stock
    return nil
}
```

## Step 6: Docker Compose Setup

Create `docker-compose.yml`:

```yaml
version: "3.8"

services:
  # Infrastructure
  postgres:
    image: postgres:15
    environment:
      POSTGRES_USER: microservices
      POSTGRES_PASSWORD: password
      POSTGRES_DB: microservices
    ports:
      - "5432:5432"
    volumes:
      - postgres_data:/var/lib/postgresql/data

  consul:
    image: consul:1.15
    ports:
      - "8500:8500"
      - "8600:8600/udp"
    command: agent -server -ui -bootstrap-expect=1 -client=0.0.0.0

  jaeger:
    image: jaegertracing/all-in-one:latest
    ports:
      - "16686:16686"
      - "14268:14268"
    environment:
      COLLECTOR_OTLP_ENABLED: "true"

  # User Service
  user-service:
    build:
      context: .
      dockerfile: services/user/Dockerfile
    environment:
      - GRPC_PORT=50051
      - HTTP_PORT=8081
      - DATABASE_URL=postgres://microservices:password@postgres:5432/users?sslmode=disable
      - CONSUL_ADDRESS=consul:8500
      - JAEGER_ENDPOINT=http://jaeger:14268/api/traces
    ports:
      - "8081:8081"
      - "50051:50051"
    depends_on:
      - postgres
      - consul
      - jaeger

  # Product Service
  product-service:
    build:
      context: .
      dockerfile: services/product/Dockerfile
    environment:
      - GRPC_PORT=50052
      - HTTP_PORT=8082
      - DATABASE_URL=postgres://microservices:password@postgres:5432/products?sslmode=disable
      - CONSUL_ADDRESS=consul:8500
      - JAEGER_ENDPOINT=http://jaeger:14268/api/traces
    ports:
      - "8082:8082"
      - "50052:50052"
    depends_on:
      - postgres
      - consul
      - jaeger

  # Order Service
  order-service:
    build:
      context: .
      dockerfile: services/order/Dockerfile
    environment:
      - GRPC_PORT=50053
      - HTTP_PORT=8083
      - DATABASE_URL=postgres://microservices:password@postgres:5432/orders?sslmode=disable
      - CONSUL_ADDRESS=consul:8500
      - JAEGER_ENDPOINT=http://jaeger:14268/api/traces
    ports:
      - "8083:8083"
      - "50053:50053"
    depends_on:
      - postgres
      - consul
      - jaeger
      - user-service
      - product-service

  # API Gateway (optional)
  api-gateway:
    build:
      context: .
      dockerfile: gateway/Dockerfile
    environment:
      - PORT=8080
      - CONSUL_ADDRESS=consul:8500
      - JAEGER_ENDPOINT=http://jaeger:14268/api/traces
    ports:
      - "8080:8080"
    depends_on:
      - consul
      - jaeger
      - user-service
      - product-service
      - order-service

volumes:
  postgres_data:
```

## Key Benefits of DI in Microservices

### 1. Service Isolation

Each microservice has its own DI container, ensuring complete isolation:

```go
// Each service configures its own dependencies
func configureUserService(collection godi.ServiceCollection) error {
    collection.AddSingleton(NewUserRepository)
    collection.AddSingleton(NewTokenService)
    collection.AddScoped(NewUserService)
    return nil
}

func configureOrderService(collection godi.ServiceCollection) error {
    collection.AddSingleton(NewOrderRepository)
    collection.AddSingleton(NewUserServiceClient)
    collection.AddSingleton(NewProductServiceClient)
    collection.AddScoped(NewOrderService)
    return nil
}
```

### 2. Testability

Easy to test services in isolation:

```go
func TestOrderService(t *testing.T) {
    services := godi.NewServiceCollection()

    // Mock external services
    services.AddSingleton(func() UserServiceClient {
        return &MockUserClient{
            users: map[string]*User{"123": testUser},
        }
    })

    services.AddSingleton(func() ProductServiceClient {
        return &MockProductClient{
            products: map[string]*Product{"abc": testProduct},
        }
    })

    // Real repository (in-memory for tests)
    services.AddSingleton(NewInMemoryOrderRepository)

    // Service under test
    services.AddScoped(NewOrderService)

    provider, _ := services.BuildServiceProvider()
    defer provider.Close()

    // Test order creation
    // ...
}
```

### 3. Configuration Management

Centralized configuration with environment-specific overrides:

```go
// Base configuration
services.AddSingleton(func() *Config {
    return &Config{
        ServiceName: "order-service",
        Environment: os.Getenv("ENVIRONMENT"),
    }
})

// Environment-specific
if env == "production" {
    services.Replace(godi.Singleton, NewProductionConfig)
}
```

### 4. Cross-Cutting Concerns

Shared middleware and interceptors across services:

```go
// Logging interceptor injected into all services
func loggingInterceptor(logger logging.Logger) grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
        start := time.Now()
        resp, err := handler(ctx, req)
        logger.Info("Request completed",
            zap.String("method", info.FullMethod),
            zap.Duration("duration", time.Since(start)),
        )
        return resp, err
    }
}
```

### 5. Service Discovery Integration

DI makes it easy to swap discovery mechanisms:

```go
// Development: hardcoded
services.AddSingleton(func() ServiceDiscovery {
    return NewStaticDiscovery(map[string]string{
        "user-service": "localhost:50051",
        "product-service": "localhost:50052",
    })
})

// Production: Consul
services.AddSingleton(func() ServiceDiscovery {
    return NewConsulDiscovery(consulAddr)
})
```

## Best Practices for Microservices with godi

1. **One Container Per Service** - Each microservice gets its own DI container
2. **Scoped Per Request** - Create a scope for each RPC/HTTP request
3. **Mock External Services** - Use interfaces for service clients
4. **Centralize Common Code** - Share infrastructure through packages
5. **Health Checks** - Register health check endpoints with DI
6. **Graceful Shutdown** - Let DI handle cleanup of resources

## Summary

Using godi in a microservices architecture provides:

- **Clean service boundaries** with isolated DI containers
- **Easy testing** through dependency injection
- **Consistent patterns** across all services
- **Simplified configuration** management
- **Better observability** with injected logging/tracing

The combination of godi's lifetime management and gRPC's service definitions creates a powerful foundation for building scalable microservices in Go.

## Next Steps

- Implement circuit breakers for service clients
- Add distributed tracing visualization
- Create integration tests using testcontainers
- Implement saga pattern for distributed transactions
- Add API gateway with authentication

### Shared Logging

Create `shared/logging/logger.go`:

```go
package logging

import (
    "context"
    "go.uber.org/zap"
    "go.uber.org/zap/zapcore"
)

type Logger interface {
    Debug(msg string, fields ...Field)
    Info(msg string, fields ...Field)
    Warn(msg string, fields ...Field)
    Error(msg string, fields ...Field)
    Fatal(msg string, fields ...Field)
    With(fields ...Field) Logger
    WithContext(ctx context.Context) Logger
}

type Field = zap.Field

// Expose commonly used field constructors
var (
    String = zap.String
    Int    = zap.Int
    Error  = zap.Error
    Any    = zap.Any
)

type zapLogger struct {
    logger *zap.Logger
}

func NewLogger(serviceName, environment, level string) (Logger, error) {
    config := zap.NewProductionConfig()
    if environment == "development" {
        config = zap.NewDevelopmentConfig()
    }

    // Parse level
    lvl, err := zapcore.ParseLevel(level)
    if err != nil {
        return nil, err
    }
    config.Level = zap.NewAtomicLevelAt(lvl)

    // Add service name
    config.InitialFields = map[string]interface{}{
        "service": serviceName,
    }

    logger, err := config.Build()
    if err != nil {
        return nil, err
    }

    return &zapLogger{logger: logger}, nil
}

func (l *zapLogger) Debug(msg string, fields ...Field) {
    l.logger.Debug(msg, fields...)
}

func (l *zapLogger) Info(msg string, fields ...Field) {
    l.logger.Info(msg, fields...)
}

func (l *zapLogger) Warn(msg string, fields ...Field) {
    l.logger.Warn(msg, fields...)
}

func (l *zapLogger) Error(msg string, fields ...Field) {
    l.logger.Error(msg, fields...)
}

func (l *zapLogger) Fatal(msg string, fields ...Field) {
    l.logger.Fatal(msg, fields...)
}

func (l *zapLogger) With(fields ...Field) Logger {
    return &zapLogger{logger: l.logger.With(fields...)}
}

func (l *zapLogger) WithContext(ctx context.Context) Logger {
    // Extract trace ID if present
    if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
        return l.With(
            String("trace_id", span.SpanContext().TraceID().String()),
            String("span_id", span.SpanContext().SpanID().String()),
        )
    }
    return l
}
```

### Shared Tracing

Create `shared/tracing/tracing.go`:

```go
package tracing

import (
    "context"
    "fmt"

    "go.opentelemetry.io/otel"
    "go.opentelemetry.io/otel/exporters/jaeger"
    "go.opentelemetry.io/otel/sdk/resource"
    sdktrace "go.opentelemetry.io/otel/sdk/trace"
    semconv "go.opentelemetry.io/otel/semconv/v1.4.0"
    "go.opentelemetry.io/otel/trace"
)

type Tracer interface {
    StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span)
    trace.Tracer
}

type tracerWrapper struct {
    trace.Tracer
}

func (t *tracerWrapper) StartSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
    return t.Tracer.Start(ctx, name, opts...)
}

func NewTracer(serviceName, endpoint string) (Tracer, func(), error) {
    exporter, err := jaeger.New(
        jaeger.WithCollectorEndpoint(jaeger.WithEndpoint(endpoint)),
    )
    if err != nil {
        return nil, nil, fmt.Errorf("failed to create jaeger exporter: %w", err)
    }

    tp := sdktrace.NewTracerProvider(
        sdktrace.WithBatcher(exporter),
        sdktrace.WithResource(resource.NewWithAttributes(
            semconv.SchemaURL,
            semconv.ServiceNameKey.String(serviceName),
        )),
    )

    otel.SetTracerProvider(tp)

    cleanup := func() {
        _ = tp.Shutdown(context.Background())
    }

    tracer := tp.Tracer(serviceName)
    return &tracerWrapper{Tracer: tracer}, cleanup, nil
}
```

### Service Discovery

Create `shared/discovery/discovery.go`:

```go
package discovery

import (
    "fmt"
    "github.com/hashicorp/consul/api"
)

type ServiceDiscovery interface {
    Register(name, id, address string, port int) error
    Deregister(id string) error
    Discover(service string) ([]ServiceInstance, error)
    HealthCheck(id string) error
}

type ServiceInstance struct {
    ID      string
    Address string
    Port    int
}

type consulDiscovery struct {
    client *api.Client
}

func NewConsulDiscovery(address string) (ServiceDiscovery, error) {
    config := api.DefaultConfig()
    config.Address = address

    client, err := api.NewClient(config)
    if err != nil {
        return nil, err
    }

    return &consulDiscovery{client: client}, nil
}

func (d *consulDiscovery) Register(name, id, address string, port int) error {
    registration := &api.AgentServiceRegistration{
        ID:      id,
        Name:    name,
        Address: address,
        Port:    port,
        Check: &api.AgentServiceCheck{
            HTTP:     fmt.Sprintf("http://%s:%d/health", address, port),
            Interval: "10s",
            Timeout:  "5s",
        },
    }

    return d.client.Agent().ServiceRegister(registration)
}

func (d *consulDiscovery) Deregister(id string) error {
    return d.client.Agent().ServiceDeregister(id)
}

func (d *consulDiscovery) Discover(service string) ([]ServiceInstance, error) {
    services, _, err := d.client.Health().Service(service, "", true, nil)
    if err != nil {
        return nil, err
    }

    instances := make([]ServiceInstance, 0, len(services))
    for _, svc := range services {
        instances = append(instances, ServiceInstance{
            ID:      svc.Service.ID,
            Address: svc.Service.Address,
            Port:    svc.Service.Port,
        })
    }

    return instances, nil
}

func (d *consulDiscovery) HealthCheck(id string) error {
    return d.client.Agent().UpdateTTL("service:"+id, "OK", api.HealthPassing)
}
```

## Step 2: Base Service Structure

Create a base service structure that all microservices will use.

Create `shared/service/base.go`:

```go
package service

import (
    "context"
    "fmt"
    "net"
    "net/http"
    "os"
    "os/signal"
    "syscall"
    "time"

    "github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
    "github.com/junioryono/godi"
    "google.golang.org/grpc"
    "google.golang.org/grpc/health"
    "google.golang.org/grpc/health/grpc_health_v1"

    "shared/config"
    "shared/discovery"
    "shared/logging"
    "shared/tracing"
)

// BaseService provides common functionality for all microservices
type BaseService struct {
    config    *config.ServiceConfig
    logger    logging.Logger
    tracer    tracing.Tracer
    discovery discovery.ServiceDiscovery
    provider  godi.ServiceProvider

    grpcServer *grpc.Server
    httpServer *http.Server

    shutdownFuncs []func()
}

// ServiceOptions for creating a base service
type ServiceOptions struct {
    ConfigureServices func(godi.ServiceCollection) error
    RegisterGRPC      func(*grpc.Server)
    RegisterHTTP      func(context.Context, *runtime.ServeMux, string) error
}

// NewBaseService creates a new base service
func NewBaseService(serviceName string, opts ServiceOptions) (*BaseService, error) {
    // Create service collection
    collection := godi.NewServiceCollection()

    // Register shared services
    cfg := config.NewServiceConfig(serviceName)
    collection.AddSingleton(func() *config.ServiceConfig { return cfg })

    // Logger
    collection.AddSingleton(func(cfg *config.ServiceConfig) (logging.Logger, error) {
        return logging.NewLogger(cfg.ServiceName, cfg.Environment, cfg.LogLevel)
    })

    // Tracer
    collection.AddSingleton(func(cfg *config.ServiceConfig) (tracing.Tracer, func(), error) {
        return tracing.NewTracer(cfg.ServiceName, cfg.JaegerEndpoint)
    })

    // Service discovery
    collection.AddSingleton(func(cfg *config.ServiceConfig) (discovery.ServiceDiscovery, error) {
        return discovery.NewConsulDiscovery(cfg.ConsulAddress)
    })

    // Configure service-specific services
    if opts.ConfigureServices != nil {
        if err := opts.ConfigureServices(collection); err != nil {
            return nil, fmt.Errorf("failed to configure services: %w", err)
        }
    }

    // Build provider
    provider, err := collection.BuildServiceProvider()
    if err != nil {
        return nil, fmt.Errorf("failed to build service provider: %w", err)
    }

    // Resolve core services
    logger, err := godi.Resolve[logging.Logger](provider)
    if err != nil {
        return nil, err
    }

    tracer, err := godi.Resolve[tracing.Tracer](provider)
    if err != nil {
        return nil, err
    }

    disc, err := godi.Resolve[discovery.ServiceDiscovery](provider)
    if err != nil {
        return nil, err
    }

    // Get tracer cleanup function
    _, cleanup, _ := godi.Resolve[func()](provider)

    base := &BaseService{
        config:        cfg,
        logger:        logger,
        tracer:        tracer,
        discovery:     disc,
        provider:      provider,
        shutdownFuncs: []func(){cleanup},
    }

    // Setup servers
    if err := base.setupServers(opts); err != nil {
        return nil, err
    }

    return base, nil
}

func (s *BaseService) setupServers(opts ServiceOptions) error {
    // Create gRPC server
    s.grpcServer = grpc.NewServer(
        grpc.ChainUnaryInterceptor(
            s.tracingInterceptor(),
            s.loggingInterceptor(),
            s.scopedInterceptor(),
        ),
    )

    // Register health service
    healthServer := health.NewServer()
    grpc_health_v1.RegisterHealthServer(s.grpcServer, healthServer)

    // Register service-specific gRPC handlers
    if opts.RegisterGRPC != nil {
        opts.RegisterGRPC(s.grpcServer)
    }

    // Create HTTP gateway
    mux := runtime.NewServeMux()

    // Register service-specific HTTP handlers
    if opts.RegisterHTTP != nil {
        ctx := context.Background()
        grpcEndpoint := fmt.Sprintf("localhost:%s", s.config.GRPCPort)

        if err := opts.RegisterHTTP(ctx, mux, grpcEndpoint); err != nil {
            return fmt.Errorf("failed to register HTTP handlers: %w", err)
        }
    }

    // Create HTTP server
    s.httpServer = &http.Server{
        Addr:    ":" + s.config.HTTPPort,
        Handler: s.httpMiddleware(mux),
    }

    return nil
}

// Run starts the service
func (s *BaseService) Run() error {
    // Start gRPC server
    grpcLis, err := net.Listen("tcp", ":"+s.config.GRPCPort)
    if err != nil {
        return fmt.Errorf("failed to listen on gRPC port: %w", err)
    }

    go func() {
        s.logger.Info("Starting gRPC server",
            logging.String("port", s.config.GRPCPort))

        if err := s.grpcServer.Serve(grpcLis); err != nil {
            s.logger.Fatal("gRPC server failed", logging.Error(err))
        }
    }()

    // Start HTTP server
    go func() {
        s.logger.Info("Starting HTTP server",
            logging.String("port", s.config.HTTPPort))

        if err := s.httpServer.ListenAndServe(); err != http.ErrServerClosed {
            s.logger.Fatal("HTTP server failed", logging.Error(err))
        }
    }()

    // Register with service discovery
    serviceID := fmt.Sprintf("%s-%s", s.config.ServiceName, generateID())
    if err := s.discovery.Register(
        s.config.ServiceName,
        serviceID,
        getLocalIP(),
        mustAtoi(s.config.GRPCPort),
    ); err != nil {
        s.logger.Error("Failed to register with service discovery", logging.Error(err))
    } else {
        s.shutdownFuncs = append(s.shutdownFuncs, func() {
            s.discovery.Deregister(serviceID)
        })
    }

    // Wait for shutdown signal
    s.waitForShutdown()

    return nil
}

func (s *BaseService) waitForShutdown() {
    quit := make(chan os.Signal, 1)
    signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
    <-quit

    s.logger.Info("Shutting down service")

    ctx, cancel := context.WithTimeout(context.Background(), s.config.ShutdownTimeout)
    defer cancel()

    // Shutdown HTTP server
    if err := s.httpServer.Shutdown(ctx); err != nil {
        s.logger.Error("HTTP server shutdown error", logging.Error(err))
    }

    // Shutdown gRPC server
    s.grpcServer.GracefulStop()

    // Run cleanup functions
    for _, fn := range s.shutdownFuncs {
        fn()
    }

    // Close DI container
    s.provider.Close()

    s.logger.Info("Service shutdown complete")
}

// Interceptors
func (s *BaseService) tracingInterceptor() grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
        ctx, span := s.tracer.StartSpan(ctx, info.FullMethod)
        defer span.End()

        return handler(ctx, req)
    }
}

func (s *BaseService) loggingInterceptor() grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
        start := time.Now()

        resp, err := handler(ctx, req)

        s.logger.WithContext(ctx).Info("gRPC request",
            logging.String("method", info.FullMethod),
            logging.String("duration", time.Since(start).String()),
            logging.Error(err),
        )

        return resp, err
    }
}

func (s *BaseService) scopedInterceptor() grpc.UnaryServerInterceptor {
    return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
        // Create scope for this request
        scope := s.provider.CreateScope(ctx)
        defer scope.Close()

        // Add scope to context
        ctx = context.WithValue(ctx, "scope", scope)

        return handler(ctx, req)
    }
}

func (s *BaseService) httpMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Create scope for HTTP request
        scope := s.provider.CreateScope(r.Context())
        defer scope.Close()

        // Add scope to context
        ctx := context.WithValue(r.Context(), "scope", scope)

        // Continue with request
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

// Provider returns the DI provider
func (s *BaseService) Provider() godi.ServiceProvider {
    return s.provider
}
```

## Step 3: User Service

Create the user service using the base service structure.

### User Service Proto

Create `services/user/proto/user.proto`:

```proto
syntax = "proto3";

package user.v1;

option go_package = "github.com/example/microservices/services/user/proto;userpb";

service UserService {
  rpc CreateUser(CreateUserRequest) returns (CreateUserResponse);
  rpc GetUser(GetUserRequest) returns (GetUserResponse);
  rpc AuthenticateUser(AuthenticateUserRequest) returns (AuthenticateUserResponse);
}

message User {
  string id = 1;
  string email = 2;
  string name = 3;
  int64 created_at = 4;
}

message CreateUserRequest {
  string email = 1;
  string password = 2;
  string name = 3;
}

message CreateUserResponse {
  User user = 1;
}

message GetUserRequest {
  string id = 1;
}

message GetUserResponse {
  User user = 1;
}

message AuthenticateUserRequest {
  string email = 1;
  string password = 2;
}

message AuthenticateUserResponse {
  string token = 1;
  User user = 2;
}
```

### User Service Implementation

Create `services/user/internal/service/user_service.go`:

```go
package service

import (
    "context"
    "time"

    "github.com/google/uuid"
    "golang.org/x/crypto/bcrypt"
    "google.golang.org/grpc/codes"
    "google.golang.org/grpc/status"

    "shared/logging"
    "shared/tracing"
    pb "services/user/proto"
)

type UserRepository interface {
    Create(ctx context.Context, user *User) error
    GetByID(ctx context.Context, id string) (*User, error)
    GetByEmail(ctx context.Context, email string) (*User, error)
}

type User struct {
    ID           string
    Email        string
    PasswordHash string
    Name         string
    CreatedAt    time.Time
}

type TokenService interface {
    GenerateToken(userID string) (string, error)
    ValidateToken(token string) (string, error)
}

type UserService struct {
    pb.UnimplementedUserServiceServer

    repo    UserRepository
    tokens  TokenService
    logger  logging.Logger
    tracer  tracing.Tracer
}

func NewUserService(
    repo UserRepository,
    tokens TokenService,
    logger logging.Logger,
    tracer tracing.Tracer,
) *UserService {
    return &UserService{
        repo:   repo,
        tokens: tokens,
        logger: logger,
        tracer: tracer,
    }
}

func (s *UserService) CreateUser(ctx context.Context, req *pb.CreateUserRequest) (*pb.CreateUserResponse, error) {
    ctx, span := s.tracer.StartSpan(ctx, "UserService.CreateUser")
    defer span.End()

    // Validate request
    if req.Email == "" || req.Password == "" {
        return nil, status.Error(codes.InvalidArgument, "email and password required")
    }

    // Check if user exists
    existing, _ := s.repo.GetByEmail(ctx, req.Email)
    if existing != nil {
        return nil, status.Error(codes.AlreadyExists, "user already exists")
    }

    // Hash password
    hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
    if err != nil {
        s.logger.Error("Failed to hash password", logging.Error(err))
        return nil, status.Error(codes.Internal, "failed to process request")
    }

    // Create user
    user := &User{
        ID:           uuid.New().String(),
        Email:        req.Email,
        PasswordHash: string(hash),
        Name:         req.Name,
        CreatedAt:    time.Now(),
    }

    if err := s.repo.Create(ctx, user); err != nil {
        s.logger.Error("Failed to create user", logging.Error(err))
        return nil, status.Error(codes.Internal, "failed to create user")
    }

    s.logger.Info("User created",
        logging.String("user_id", user.ID),
        logging.String("email", user.Email))

    return &pb.CreateUserResponse{
        User: &pb.User{
            Id:        user.ID,
            Email:     user.Email,
            Name:      user.Name,
            CreatedAt: user.CreatedAt.Unix(),
        },
    }, nil
}

func (s *UserService) AuthenticateUser(ctx context.Context, req *pb.AuthenticateUserRequest) (*pb.AuthenticateUserResponse, error) {
    ctx, span := s.tracer.StartSpan(ctx, "UserService.AuthenticateUser")
    defer span.End()

    // Find user
    user, err := s.repo.GetByEmail(ctx, req.Email)
    if err != nil {
        return nil, status.Error(codes.Unauthenticated, "invalid credentials")
    }

    // Check password
    if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
        return nil, status.Error(codes.Unauthenticated, "invalid credentials")
    }

    // Generate token
    token, err := s.tokens.GenerateToken(user.ID)
    if err != nil {
        s.logger.Error("Failed to generate token", logging.Error(err))
        return nil, status.Error(codes.Internal, "authentication failed")
    }

    s.logger.Info("User authenticated", logging.String("user_id", user.ID))

    return &pb.AuthenticateUserResponse{
        Token: token,
        User: &pb.User{
            Id:        user.ID,
            Email:     user.Email,
            Name:      user.Name,
            CreatedAt: user.CreatedAt.Unix(),
        },
    }, nil
}

func (s *UserService) GetUser(ctx context.Context, req *pb.GetUserRequest) (*pb.GetUserResponse, error) {
    ctx, span := s.tracer.StartSpan(ctx, "UserService.GetUser")
    defer span.End()

    user, err := s.repo.GetByID(ctx, req.Id)
    if err != nil {
        return nil, status.Error(codes.NotFound, "user not found")
    }

    return &pb.GetUserResponse{
        User: &pb.User{
            Id:        user.ID,
            Email:     user.Email,
            Name:      user.Name,
            CreatedAt: user.CreatedAt.Unix(),
        },
```
