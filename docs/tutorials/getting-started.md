# Getting Started with godi

This tutorial will walk you through creating your first application using godi. We'll build a simple task management system to demonstrate core dependency injection concepts.

## Prerequisites

- Go 1.21 or later installed
- Basic familiarity with Go
- A text editor or IDE

## Setting Up

Create a new directory for our project:

```bash
mkdir godi-tutorial
cd godi-tutorial
```

Initialize a new Go module:

```bash
go mod init tutorial/taskapp
```

Install godi:

```bash
go get github.com/junioryono/godi
```

## Step 1: Define Our Services

First, let's define the interfaces and types for our task management system.

Create a file named `types.go`:

```go
package main

import (
    "context"
    "time"
)

// Task represents a task in our system
type Task struct {
    ID          string
    Title       string
    Description string
    Completed   bool
    CreatedAt   time.Time
    UpdatedAt   time.Time
}

// Logger interface for logging
type Logger interface {
    Info(msg string, args ...interface{})
    Error(msg string, err error, args ...interface{})
}

// TaskRepository interface for data access
type TaskRepository interface {
    Create(ctx context.Context, task *Task) error
    GetByID(ctx context.Context, id string) (*Task, error)
    List(ctx context.Context) ([]*Task, error)
    Update(ctx context.Context, task *Task) error
    Delete(ctx context.Context, id string) error
}

// TaskService interface for business logic
type TaskService interface {
    CreateTask(ctx context.Context, title, description string) (*Task, error)
    GetTask(ctx context.Context, id string) (*Task, error)
    ListTasks(ctx context.Context) ([]*Task, error)
    CompleteTask(ctx context.Context, id string) error
    DeleteTask(ctx context.Context, id string) error
}

// NotificationService interface for notifications
type NotificationService interface {
    NotifyTaskCreated(task *Task) error
    NotifyTaskCompleted(task *Task) error
}
```

## Step 2: Implement Our Services

Now let's implement these interfaces. Create a file named `services.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "sync"
    "time"

    "github.com/google/uuid"
)

// ConsoleLogger implements Logger
type ConsoleLogger struct {
    prefix string
}

func NewConsoleLogger() Logger {
    return &ConsoleLogger{prefix: "[TASK-APP]"}
}

func (l *ConsoleLogger) Info(msg string, args ...interface{}) {
    log.Printf("%s INFO: %s %v", l.prefix, msg, args)
}

func (l *ConsoleLogger) Error(msg string, err error, args ...interface{}) {
    log.Printf("%s ERROR: %s - %v %v", l.prefix, msg, err, args)
}

// InMemoryTaskRepository implements TaskRepository
type InMemoryTaskRepository struct {
    mu    sync.RWMutex
    tasks map[string]*Task
}

func NewInMemoryTaskRepository() TaskRepository {
    return &InMemoryTaskRepository{
        tasks: make(map[string]*Task),
    }
}

func (r *InMemoryTaskRepository) Create(ctx context.Context, task *Task) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    task.ID = uuid.New().String()
    task.CreatedAt = time.Now()
    task.UpdatedAt = time.Now()

    r.tasks[task.ID] = task
    return nil
}

func (r *InMemoryTaskRepository) GetByID(ctx context.Context, id string) (*Task, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    task, exists := r.tasks[id]
    if !exists {
        return nil, fmt.Errorf("task not found: %s", id)
    }

    return task, nil
}

func (r *InMemoryTaskRepository) List(ctx context.Context) ([]*Task, error) {
    r.mu.RLock()
    defer r.mu.RUnlock()

    tasks := make([]*Task, 0, len(r.tasks))
    for _, task := range r.tasks {
        tasks = append(tasks, task)
    }

    return tasks, nil
}

func (r *InMemoryTaskRepository) Update(ctx context.Context, task *Task) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    if _, exists := r.tasks[task.ID]; !exists {
        return fmt.Errorf("task not found: %s", task.ID)
    }

    task.UpdatedAt = time.Now()
    r.tasks[task.ID] = task
    return nil
}

func (r *InMemoryTaskRepository) Delete(ctx context.Context, id string) error {
    r.mu.Lock()
    defer r.mu.Unlock()

    delete(r.tasks, id)
    return nil
}

// DefaultTaskService implements TaskService
type DefaultTaskService struct {
    repo   TaskRepository
    notif  NotificationService
    logger Logger
}

func NewTaskService(
    repo TaskRepository,
    notif NotificationService,
    logger Logger,
) TaskService {
    return &DefaultTaskService{
        repo:   repo,
        notif:  notif,
        logger: logger,
    }
}

func (s *DefaultTaskService) CreateTask(ctx context.Context, title, description string) (*Task, error) {
    task := &Task{
        Title:       title,
        Description: description,
        Completed:   false,
    }

    if err := s.repo.Create(ctx, task); err != nil {
        s.logger.Error("Failed to create task", err)
        return nil, err
    }

    s.logger.Info("Task created", "id", task.ID, "title", title)

    if err := s.notif.NotifyTaskCreated(task); err != nil {
        s.logger.Error("Failed to send notification", err)
    }

    return task, nil
}

func (s *DefaultTaskService) GetTask(ctx context.Context, id string) (*Task, error) {
    return s.repo.GetByID(ctx, id)
}

func (s *DefaultTaskService) ListTasks(ctx context.Context) ([]*Task, error) {
    return s.repo.List(ctx)
}

func (s *DefaultTaskService) CompleteTask(ctx context.Context, id string) error {
    task, err := s.repo.GetByID(ctx, id)
    if err != nil {
        return err
    }

    task.Completed = true
    if err := s.repo.Update(ctx, task); err != nil {
        return err
    }

    s.logger.Info("Task completed", "id", id)

    if err := s.notif.NotifyTaskCompleted(task); err != nil {
        s.logger.Error("Failed to send notification", err)
    }

    return nil
}

func (s *DefaultTaskService) DeleteTask(ctx context.Context, id string) error {
    if err := s.repo.Delete(ctx, id); err != nil {
        return err
    }

    s.logger.Info("Task deleted", "id", id)
    return nil
}

// EmailNotificationService implements NotificationService
type EmailNotificationService struct {
    logger Logger
}

func NewEmailNotificationService(logger Logger) NotificationService {
    return &EmailNotificationService{logger: logger}
}

func (s *EmailNotificationService) NotifyTaskCreated(task *Task) error {
    s.logger.Info("Email notification: Task created", "title", task.Title)
    return nil
}

func (s *EmailNotificationService) NotifyTaskCompleted(task *Task) error {
    s.logger.Info("Email notification: Task completed", "title", task.Title)
    return nil
}
```

## Step 3: Wire Everything with godi

Now comes the magic! Let's use godi to wire all these services together. Create `main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/junioryono/godi"
)

func main() {
    // Create a service collection
    services := godi.NewServiceCollection()

    // Register our services
    // Logger is a singleton - one instance for the entire app
    services.AddSingleton(NewConsoleLogger)

    // Repository is a singleton - shared data store
    services.AddSingleton(NewInMemoryTaskRepository)

    // Notification service is scoped - could have per-request config
    services.AddScoped(NewEmailNotificationService)

    // Task service is scoped - new instance per scope
    services.AddScoped(NewTaskService)

    // Build the service provider
    provider, err := services.BuildServiceProvider()
    if err != nil {
        log.Fatal("Failed to build service provider:", err)
    }
    defer provider.Close()

    // Create a scope (in a real app, this would be per-request)
    scope := provider.CreateScope(context.Background())
    defer scope.Close()

    // Resolve our task service - godi automatically injects all dependencies!
    taskService, err := godi.Resolve[TaskService](scope.ServiceProvider())
    if err != nil {
        log.Fatal("Failed to resolve task service:", err)
    }

    // Use our service
    fmt.Println("=== Task Management System ===")

    // Create some tasks
    task1, err := taskService.CreateTask(context.Background(),
        "Learn godi",
        "Understand dependency injection in Go")
    if err != nil {
        log.Fatal("Failed to create task:", err)
    }

    task2, err := taskService.CreateTask(context.Background(),
        "Build an app",
        "Create a real application using godi")
    if err != nil {
        log.Fatal("Failed to create task:", err)
    }

    // List all tasks
    fmt.Println("\nAll tasks:")
    tasks, err := taskService.ListTasks(context.Background())
    if err != nil {
        log.Fatal("Failed to list tasks:", err)
    }

    for _, task := range tasks {
        status := "Pending"
        if task.Completed {
            status = "Completed"
        }
        fmt.Printf("- [%s] %s: %s\n", status, task.Title, task.Description)
    }

    // Complete a task
    fmt.Printf("\nCompleting task: %s\n", task1.Title)
    err = taskService.CompleteTask(context.Background(), task1.ID)
    if err != nil {
        log.Fatal("Failed to complete task:", err)
    }

    // List tasks again
    fmt.Println("\nUpdated task list:")
    tasks, err = taskService.ListTasks(context.Background())
    if err != nil {
        log.Fatal("Failed to list tasks:", err)
    }

    for _, task := range tasks {
        status := "Pending"
        if task.Completed {
            status = "Completed"
        }
        fmt.Printf("- [%s] %s\n", status, task.Title)
    }
}
```

## Step 4: Run the Application

Run your application:

```bash
go run .
```

You should see output like:

```
[TASK-APP] INFO: Task created [id <uuid> title Learn godi]
[TASK-APP] INFO: Email notification: Task created [title Learn godi]
[TASK-APP] INFO: Task created [id <uuid> title Build an app]
[TASK-APP] INFO: Email notification: Task created [title Build an app]

All tasks:
- [Pending] Learn godi: Understand dependency injection in Go
- [Pending] Build an app: Create a real application using godi

Completing task: Learn godi
[TASK-APP] INFO: Task completed [id <uuid>]
[TASK-APP] INFO: Email notification: Task completed [title Learn godi]

Updated task list:
- [Completed] Learn godi
- [Pending] Build an app
```

## What Just Happened?

Let's understand the magic that godi performed:

1. **Automatic Wiring**: We never manually created instances or passed dependencies. godi analyzed the constructor functions and automatically provided the required dependencies.

2. **Lifetime Management**:

   - The logger and repository are singletons (one instance)
   - The services are scoped (new instances per scope)

3. **Clean Separation**: Our services don't know about godi - they're just regular Go types with constructor functions.

4. **Type Safety**: Using generics, we get compile-time type checking when resolving services.

## Step 5: Add a New Feature

Let's see how easy it is to add new functionality. We'll add a statistics service:

Add to `types.go`:

```go
// StatsService interface for statistics
type StatsService interface {
    GetTaskStats(ctx context.Context) (total, completed int, err error)
}
```

Add to `services.go`:

```go
// TaskStatsService implements StatsService
type TaskStatsService struct {
    repo   TaskRepository
    logger Logger
}

func NewTaskStatsService(repo TaskRepository, logger Logger) StatsService {
    return &TaskStatsService{
        repo:   repo,
        logger: logger,
    }
}

func (s *TaskStatsService) GetTaskStats(ctx context.Context) (total, completed int, err error) {
    tasks, err := s.repo.List(ctx)
    if err != nil {
        return 0, 0, err
    }

    total = len(tasks)
    for _, task := range tasks {
        if task.Completed {
            completed++
        }
    }

    s.logger.Info("Stats calculated", "total", total, "completed", completed)
    return total, completed, nil
}
```

Update `main.go` - just add one line to register the service:

```go
// Add this line with the other service registrations
services.AddScoped(NewTaskStatsService)

// And use it after creating the scope:
statsService, err := godi.Resolve[StatsService](scope.ServiceProvider())
if err != nil {
    log.Fatal("Failed to resolve stats service:", err)
}

// Display stats
total, completed, err := statsService.GetTaskStats(context.Background())
if err != nil {
    log.Fatal("Failed to get stats:", err)
}
fmt.Printf("\nStatistics: %d/%d tasks completed\n", completed, total)
```

Notice how we:

1. Added the new service and constructor
2. Registered it with one line
3. Started using it immediately

No changes to existing code were needed!

## Next Steps

Congratulations! You've learned the basics of dependency injection with godi. Here's what to explore next:

1. **[Web Application Tutorial](web-application.md)** - Build a REST API with godi
2. **[Testing with godi](testing.md)** - Learn how DI makes testing easy
3. **[Service Scopes](../howto/use-scopes.md)** - Deep dive into scope management
4. **[Advanced Patterns](../howto/advanced-patterns.md)** - Modules, decorators, and more

## Key Takeaways

- **Services** are just interfaces and types - no special requirements
- **Constructors** are regular functions that godi calls with dependencies
- **Registration** tells godi about your services and their lifetimes
- **Resolution** gets instances with all dependencies automatically injected
- **Adding features** is as simple as creating and registering new services

Happy coding with godi!
