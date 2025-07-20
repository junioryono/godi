package godi_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/junioryono/godi"
	"github.com/junioryono/godi/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Integration tests that test the entire system working together

func TestIntegration_WebApplicationSimulation(t *testing.T) {
	t.Run("simulates web request handling", func(t *testing.T) {
		t.Parallel()

		// Application setup
		provider := createWebAppProvider(t)

		// Simulate multiple concurrent requests
		const numRequests = 50
		var wg sync.WaitGroup
		wg.Add(numRequests)

		requestErrors := make([]error, numRequests)

		type ctxKeyRequestID struct{}
		for i := 0; i < numRequests; i++ {
			go func(requestID int) {
				defer wg.Done()

				// Each request gets its own scope
				ctx := context.WithValue(context.Background(), ctxKeyRequestID{}, fmt.Sprintf("req-%d", requestID))
				scope := provider.CreateScope(ctx)
				defer scope.Close()

				// Simulate request handling
				err := handleWebRequest(t, scope, requestID)
				requestErrors[requestID] = err
			}(i)
		}

		wg.Wait()

		// Check all requests succeeded
		for i, err := range requestErrors {
			assert.NoError(t, err, "request %d failed", i)
		}
	})
}

func TestIntegration_BackgroundJobProcessing(t *testing.T) {
	t.Run("simulates background job processing", func(t *testing.T) {
		t.Parallel()

		provider := createJobProcessorProvider(t)

		// Job queue
		jobQueue := make(chan int, 100)

		// Add jobs
		const numJobs = 20
		for i := 0; i < numJobs; i++ {
			jobQueue <- i
		}
		close(jobQueue)

		// Process jobs concurrently
		const numWorkers = 5
		var wg sync.WaitGroup
		wg.Add(numWorkers)

		jobResults := make([]bool, numJobs)
		var resultMu sync.Mutex

		for w := 0; w < numWorkers; w++ {
			go func(workerID int) {
				defer wg.Done()

				for jobID := range jobQueue {
					// Each job gets its own scope
					type ctxKeyJobID struct{}
					ctx := context.WithValue(context.Background(), ctxKeyJobID{}, jobID)
					scope := provider.CreateScope(ctx)

					success := processJob(t, scope, workerID, jobID)

					resultMu.Lock()
					jobResults[jobID] = success
					resultMu.Unlock()

					scope.Close()
				}
			}(w)
		}

		wg.Wait()

		// Verify all jobs processed
		for i, success := range jobResults {
			assert.True(t, success, "job %d failed", i)
		}
	})
}

func TestIntegration_MicroserviceArchitecture(t *testing.T) {
	t.Run("simulates microservice with multiple components", func(t *testing.T) {
		t.Parallel()

		// Create a complex service setup
		provider := createMicroserviceProvider(t)

		// Simulate service startup
		err := provider.Invoke(func(
			health *HealthService,
			api *APIService,
			worker *WorkerService,
			metrics *MetricsService,
		) error {
			// Start all services
			assert.True(t, health.IsHealthy())
			assert.Equal(t, "running", api.Status())
			assert.Greater(t, worker.ProcessedCount(), 0)
			assert.NotEmpty(t, metrics.Collect())
			return nil
		})

		require.NoError(t, err)

		// Simulate graceful shutdown
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Create shutdown scope
		shutdownScope := provider.CreateScope(ctx)
		defer shutdownScope.Close()

		// Services should handle shutdown gracefully
		var shutdownErr error
		err = shutdownScope.Invoke(func(health *HealthService) {
			health.Shutdown()
		})
		assert.NoError(t, err)
		assert.NoError(t, shutdownErr)
	})
}

func TestIntegration_PluginSystem(t *testing.T) {
	t.Run("dynamic plugin loading and execution", func(t *testing.T) {
		t.Parallel()

		// Create base system
		collection := godi.NewServiceCollection()

		// Core services
		require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))
		require.NoError(t, collection.AddSingleton(NewPluginRegistry))

		// Load plugins dynamically
		plugins := []Plugin{
			NewAuthPlugin(),
			NewLoggingPlugin(),
			NewMetricsPlugin(),
		}

		for _, plugin := range plugins {
			p := plugin // capture
			require.NoError(t, collection.AddSingleton(
				func() Plugin { return p },
				godi.Group("plugins"),
			))
		}

		// Plugin manager that uses all plugins
		require.NoError(t, collection.AddSingleton(func(params struct {
			godi.In
			Logger   testutil.TestLogger
			Registry *PluginRegistry
			Plugins  []Plugin `group:"plugins"`
		}) *PluginManager {
			manager := &PluginManager{
				logger:   params.Logger,
				registry: params.Registry,
			}

			// Register all plugins
			for _, plugin := range params.Plugins {
				manager.Register(plugin)
			}

			return manager
		}))

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Use the plugin system
		manager := testutil.AssertServiceResolvable[*PluginManager](t, provider)

		// Execute all plugins
		results := manager.ExecuteAll("test-data")
		assert.Len(t, results, 3)

		// Verify each plugin executed
		assert.Contains(t, results, "auth: processed test-data")
		assert.Contains(t, results, "logging: processed test-data")
		assert.Contains(t, results, "metrics: processed test-data")
	})
}

func TestIntegration_ComplexDependencyGraph(t *testing.T) {
	t.Run("resolves complex dependency graph correctly", func(t *testing.T) {
		t.Parallel()

		// Create a complex graph:
		// A -> B -> D
		// A -> C -> D
		// E -> B
		// E -> C
		// F -> A
		// F -> E

		type ServiceD struct{ ID string }
		type ServiceC struct{ D *ServiceD }
		type ServiceB struct{ D *ServiceD }
		type ServiceA struct {
			B *ServiceB
			C *ServiceC
		}
		type ServiceE struct {
			B *ServiceB
			C *ServiceC
		}
		type ServiceF struct {
			A *ServiceA
			E *ServiceE
		}

		collection := godi.NewServiceCollection()

		// Register in dependency order
		require.NoError(t, collection.AddSingleton(func() *ServiceD {
			return &ServiceD{ID: "D"}
		}))
		require.NoError(t, collection.AddSingleton(func(d *ServiceD) *ServiceC {
			return &ServiceC{D: d}
		}))
		require.NoError(t, collection.AddSingleton(func(d *ServiceD) *ServiceB {
			return &ServiceB{D: d}
		}))
		require.NoError(t, collection.AddSingleton(func(b *ServiceB, c *ServiceC) *ServiceA {
			return &ServiceA{B: b, C: c}
		}))
		require.NoError(t, collection.AddSingleton(func(b *ServiceB, c *ServiceC) *ServiceE {
			return &ServiceE{B: b, C: c}
		}))
		require.NoError(t, collection.AddSingleton(func(a *ServiceA, e *ServiceE) *ServiceF {
			return &ServiceF{A: a, E: e}
		}))

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Resolve the most complex service
		f := testutil.AssertServiceResolvable[*ServiceF](t, provider)

		// Verify the entire graph
		assert.Equal(t, "D", f.A.B.D.ID)
		assert.Equal(t, "D", f.A.C.D.ID)
		assert.Equal(t, "D", f.E.B.D.ID)
		assert.Equal(t, "D", f.E.C.D.ID)

		// All D instances should be the same (singleton)
		assert.Same(t, f.A.B.D, f.A.C.D)
		assert.Same(t, f.A.B.D, f.E.B.D)
		assert.Same(t, f.A.B.D, f.E.C.D)
	})
}

func TestIntegration_ErrorPropagation(t *testing.T) {
	t.Run("constructor errors propagate correctly", func(t *testing.T) {
		t.Parallel()

		expectedErr := errors.New("service initialization failed")

		type FailingService struct{}
		type DependentService struct{ Failing *FailingService }

		collection := godi.NewServiceCollection()

		// Service that fails during construction
		require.NoError(t, collection.AddSingleton(func() (*FailingService, error) {
			return nil, expectedErr
		}))

		// Service that depends on failing service
		require.NoError(t, collection.AddSingleton(func(f *FailingService) *DependentService {
			return &DependentService{Failing: f}
		}))

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)
		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		// Direct resolution should fail
		_, err = godi.Resolve[*FailingService](provider)
		assert.ErrorIs(t, err, expectedErr)

		// Dependent resolution should also fail
		_, err = godi.Resolve[*DependentService](provider)
		assert.ErrorIs(t, err, expectedErr)
	})
}

func TestIntegration_LifecycleManagement(t *testing.T) {
	t.Run("proper lifecycle for all service types", func(t *testing.T) {
		t.Parallel()

		var (
			singletonCreated  int32
			scopedCreated     int32
			singletonDisposed int32
			scopedDisposed    int32
		)

		collection := godi.NewServiceCollection()

		// Singleton with disposal tracking
		require.NoError(t, collection.AddSingleton(func() *TrackedService {
			atomic.AddInt32(&singletonCreated, 1)
			return &TrackedService{
				name: "singleton",
				onDispose: func() {
					atomic.AddInt32(&singletonDisposed, 1)
				},
			}
		}))

		// Scoped with disposal tracking
		require.NoError(t, collection.AddScoped(func() *TrackedService {
			atomic.AddInt32(&scopedCreated, 1)
			return &TrackedService{
				name: "scoped",
				onDispose: func() {
					atomic.AddInt32(&scopedDisposed, 1)
				},
			}
		}))

		provider, err := collection.BuildServiceProvider()
		require.NoError(t, err)

		// Create multiple scopes
		for i := 0; i < 3; i++ {
			scope := provider.CreateScope(context.Background())

			// Resolve both services
			singleton := testutil.AssertServiceResolvableInScope[*TrackedService](t, scope)
			assert.Equal(t, "singleton", singleton.name)

			scoped := testutil.AssertServiceResolvableInScope[*TrackedService](t, scope)
			assert.Equal(t, "scoped", scoped.name)

			// Close scope - should dispose scoped service
			require.NoError(t, scope.Close())
		}

		// Check creation counts
		assert.Equal(t, int32(1), atomic.LoadInt32(&singletonCreated), "singleton should be created once")
		assert.Equal(t, int32(3), atomic.LoadInt32(&scopedCreated), "scoped should be created per scope")

		// Check disposal counts before provider close
		assert.Equal(t, int32(0), atomic.LoadInt32(&singletonDisposed), "singleton should not be disposed yet")
		assert.Equal(t, int32(3), atomic.LoadInt32(&scopedDisposed), "scoped should be disposed with scope")

		// Close provider - should dispose singleton
		require.NoError(t, provider.Close())

		assert.Equal(t, int32(1), atomic.LoadInt32(&singletonDisposed), "singleton should be disposed once")
	})
}

func TestIntegration_RealWorldScenarios(t *testing.T) {
	t.Run("REST API with middleware", func(t *testing.T) {
		t.Parallel()

		// Setup DI for REST API
		provider := createRESTAPIProvider(t)

		// Simulate request through middleware chain
		type ctxKeyPath struct{}
		ctx := context.WithValue(context.Background(), ctxKeyPath{}, "/api/users/123")
		scope := provider.CreateScope(ctx)
		defer scope.Close()

		// Execute request through middleware
		var response string
		err := scope.Invoke(func(
			auth *AuthMiddleware,
			logging *LoggingMiddleware,
			handler *UserHandler,
		) error {
			// Simulate middleware chain
			return auth.Execute(func() error {
				return logging.Execute(func() error {
					response = handler.GetUser("123")
					return nil
				})
			})
		})

		require.NoError(t, err)
		assert.Equal(t, "user-123", response)
	})

	t.Run("event-driven architecture", func(t *testing.T) {
		t.Parallel()

		// Setup event bus with handlers
		provider := createEventDrivenProvider(t)

		// Publish events
		err := provider.Invoke(func(
			bus *EventBus,
			logger testutil.TestLogger,
		) error {
			// Publish various events
			events := []Event{
				{Type: "user.created", Data: "user1"},
				{Type: "order.placed", Data: "order1"},
				{Type: "payment.processed", Data: "payment1"},
			}

			for _, event := range events {
				bus.Publish(event)
			}

			// Give handlers time to process
			time.Sleep(10 * time.Millisecond)

			// Verify processing
			logs := logger.GetLogs()
			assert.Contains(t, logs, "UserHandler: user.created - user1")
			assert.Contains(t, logs, "OrderHandler: order.placed - order1")
			assert.Contains(t, logs, "PaymentHandler: payment.processed - payment1")

			return nil
		})

		require.NoError(t, err)
	})
}

// Helper types and functions for integration tests

type HealthService struct {
	healthy bool
	mu      sync.RWMutex
}

func NewHealthService() *HealthService {
	return &HealthService{healthy: true}
}

func (h *HealthService) IsHealthy() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.healthy
}

func (h *HealthService) Shutdown() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.healthy = false
}

type APIService struct {
	status string
}

func NewAPIService() *APIService {
	return &APIService{status: "running"}
}

func (a *APIService) Status() string { return a.status }

type WorkerService struct {
	processed int32
}

func NewWorkerService() *WorkerService {
	w := &WorkerService{}
	// Simulate some work
	go func() {
		for i := 0; i < 10; i++ {
			atomic.AddInt32(&w.processed, 1)
			time.Sleep(time.Millisecond)
		}
	}()
	return w
}

func (w *WorkerService) ProcessedCount() int {
	return int(atomic.LoadInt32(&w.processed))
}

type MetricsService struct {
	metrics map[string]int
	mu      sync.RWMutex
}

func NewMetricsService() *MetricsService {
	return &MetricsService{
		metrics: map[string]int{
			"requests": 100,
			"errors":   5,
		},
	}
}

func (m *MetricsService) Collect() map[string]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make(map[string]int)
	for k, v := range m.metrics {
		result[k] = v
	}
	return result
}

type Plugin interface {
	Name() string
	Execute(data string) string
}

type PluginRegistry struct {
	plugins []Plugin
	mu      sync.RWMutex
}

func NewPluginRegistry() *PluginRegistry {
	return &PluginRegistry{}
}

func (r *PluginRegistry) Register(p Plugin) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.plugins = append(r.plugins, p)
}

type PluginManager struct {
	logger   testutil.TestLogger
	registry *PluginRegistry
}

func (m *PluginManager) Register(p Plugin) {
	m.logger.Log(fmt.Sprintf("Registering plugin: %s", p.Name()))
	m.registry.Register(p)
}

func (m *PluginManager) ExecuteAll(data string) []string {
	m.registry.mu.RLock()
	defer m.registry.mu.RUnlock()

	results := make([]string, 0, len(m.registry.plugins))
	for _, p := range m.registry.plugins {
		results = append(results, p.Execute(data))
	}
	return results
}

// Plugin implementations
type AuthPlugin struct{}

func NewAuthPlugin() Plugin                      { return &AuthPlugin{} }
func (p *AuthPlugin) Name() string               { return "auth" }
func (p *AuthPlugin) Execute(data string) string { return fmt.Sprintf("auth: processed %s", data) }

type LoggingPlugin struct{}

func NewLoggingPlugin() Plugin        { return &LoggingPlugin{} }
func (p *LoggingPlugin) Name() string { return "logging" }
func (p *LoggingPlugin) Execute(data string) string {
	return fmt.Sprintf("logging: processed %s", data)
}

type MetricsPlugin struct{}

func NewMetricsPlugin() Plugin        { return &MetricsPlugin{} }
func (p *MetricsPlugin) Name() string { return "metrics" }
func (p *MetricsPlugin) Execute(data string) string {
	return fmt.Sprintf("metrics: processed %s", data)
}

type TrackedService struct {
	name      string
	onDispose func()
}

func (t *TrackedService) Close() error {
	if t.onDispose != nil {
		t.onDispose()
	}
	return nil
}

// Helper functions for creating test providers

func createWebAppProvider(t *testing.T) godi.ServiceProvider {
	collection := godi.NewServiceCollection()

	// Infrastructure
	require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))
	require.NoError(t, collection.AddSingleton(testutil.NewTestDatabase))
	require.NoError(t, collection.AddSingleton(testutil.NewTestCache))

	// Request-scoped services
	require.NoError(t, collection.AddScoped(func(ctx context.Context) *RequestContext {
		requestID, _ := ctx.Value("requestID").(string)
		return &RequestContext{RequestID: requestID}
	}))

	require.NoError(t, collection.AddScoped(func(
		ctx *RequestContext,
		logger testutil.TestLogger,
		db testutil.TestDatabase,
	) *RequestHandler {
		return &RequestHandler{
			ctx:    ctx,
			logger: logger,
			db:     db,
		}
	}))

	provider, err := collection.BuildServiceProvider()
	require.NoError(t, err)
	return provider
}

func createJobProcessorProvider(t *testing.T) godi.ServiceProvider {
	collection := godi.NewServiceCollection()

	require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))
	require.NoError(t, collection.AddScoped(func(ctx context.Context) *JobProcessor {
		jobID, _ := ctx.Value("jobID").(int)
		return &JobProcessor{JobID: jobID}
	}))

	provider, err := collection.BuildServiceProvider()
	require.NoError(t, err)
	return provider
}

func createMicroserviceProvider(t *testing.T) godi.ServiceProvider {
	collection := godi.NewServiceCollection()

	require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))
	require.NoError(t, collection.AddSingleton(NewHealthService))
	require.NoError(t, collection.AddSingleton(NewAPIService))
	require.NoError(t, collection.AddSingleton(NewWorkerService))
	require.NoError(t, collection.AddSingleton(NewMetricsService))

	provider, err := collection.BuildServiceProvider()
	require.NoError(t, err)
	return provider
}

func createRESTAPIProvider(t *testing.T) godi.ServiceProvider {
	collection := godi.NewServiceCollection()

	require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))
	require.NoError(t, collection.AddScoped(NewAuthMiddleware))
	require.NoError(t, collection.AddScoped(NewLoggingMiddleware))
	require.NoError(t, collection.AddScoped(NewUserHandler))

	provider, err := collection.BuildServiceProvider()
	require.NoError(t, err)
	return provider
}

func createEventDrivenProvider(t *testing.T) godi.ServiceProvider {
	collection := godi.NewServiceCollection()

	require.NoError(t, collection.AddSingleton(testutil.NewTestLogger))
	require.NoError(t, collection.AddSingleton(NewEventBus))

	// Register event handlers
	require.NoError(t, collection.AddSingleton(NewUserEventHandler))
	require.NoError(t, collection.AddSingleton(NewOrderEventHandler))
	require.NoError(t, collection.AddSingleton(NewPaymentEventHandler))

	// Wire up handlers to bus
	require.NoError(t, collection.AddSingleton(func(
		bus *EventBus,
		userHandler *UserEventHandler,
		orderHandler *OrderEventHandler,
		paymentHandler *PaymentEventHandler,
	) struct{} {
		bus.Subscribe("user.created", userHandler)
		bus.Subscribe("order.placed", orderHandler)
		bus.Subscribe("payment.processed", paymentHandler)
		return struct{}{}
	}))

	provider, err := collection.BuildServiceProvider()
	require.NoError(t, err)

	// Ensure wiring happens
	_, _ = godi.Resolve[struct{}](provider)

	return provider
}

// Additional helper types

type RequestContext struct {
	RequestID string
}

type RequestHandler struct {
	ctx    *RequestContext
	logger testutil.TestLogger
	db     testutil.TestDatabase
}

type JobProcessor struct {
	JobID int
}

type AuthMiddleware struct {
	logger testutil.TestLogger
}

func NewAuthMiddleware(logger testutil.TestLogger) *AuthMiddleware {
	return &AuthMiddleware{logger: logger}
}

func (m *AuthMiddleware) Execute(next func() error) error {
	m.logger.Log("Auth check")
	return next()
}

type LoggingMiddleware struct {
	logger testutil.TestLogger
}

func NewLoggingMiddleware(logger testutil.TestLogger) *LoggingMiddleware {
	return &LoggingMiddleware{logger: logger}
}

func (m *LoggingMiddleware) Execute(next func() error) error {
	m.logger.Log("Request logged")
	return next()
}

type UserHandler struct{}

func NewUserHandler() *UserHandler {
	return &UserHandler{}
}

func (h *UserHandler) GetUser(id string) string {
	return fmt.Sprintf("user-%s", id)
}

type Event struct {
	Type string
	Data string
}

type EventHandler interface {
	Handle(event Event)
}

type EventBus struct {
	handlers map[string][]EventHandler
	mu       sync.RWMutex
}

func NewEventBus() *EventBus {
	return &EventBus{
		handlers: make(map[string][]EventHandler),
	}
}

func (b *EventBus) Subscribe(eventType string, handler EventHandler) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[eventType] = append(b.handlers[eventType], handler)
}

func (b *EventBus) Publish(event Event) {
	b.mu.RLock()
	handlers := b.handlers[event.Type]
	b.mu.RUnlock()

	for _, h := range handlers {
		go h.Handle(event)
	}
}

type UserEventHandler struct {
	logger testutil.TestLogger
}

func NewUserEventHandler(logger testutil.TestLogger) *UserEventHandler {
	return &UserEventHandler{logger: logger}
}

func (h *UserEventHandler) Handle(event Event) {
	h.logger.Log(fmt.Sprintf("UserHandler: %s - %s", event.Type, event.Data))
}

type OrderEventHandler struct {
	logger testutil.TestLogger
}

func NewOrderEventHandler(logger testutil.TestLogger) *OrderEventHandler {
	return &OrderEventHandler{logger: logger}
}

func (h *OrderEventHandler) Handle(event Event) {
	h.logger.Log(fmt.Sprintf("OrderHandler: %s - %s", event.Type, event.Data))
}

type PaymentEventHandler struct {
	logger testutil.TestLogger
}

func NewPaymentEventHandler(logger testutil.TestLogger) *PaymentEventHandler {
	return &PaymentEventHandler{logger: logger}
}

func (h *PaymentEventHandler) Handle(event Event) {
	h.logger.Log(fmt.Sprintf("PaymentHandler: %s - %s", event.Type, event.Data))
}

// Helper functions for test scenarios

func handleWebRequest(t *testing.T, scope godi.Scope, requestID int) error {
	return scope.Invoke(func(handler *RequestHandler) error {
		assert.Equal(t, fmt.Sprintf("req-%d", requestID), handler.ctx.RequestID)
		handler.logger.Log(fmt.Sprintf("Handling request %d", requestID))
		handler.db.Query("SELECT * FROM users")
		return nil
	})
}

func processJob(t *testing.T, scope godi.Scope, workerID, jobID int) bool {
	err := scope.Invoke(func(processor *JobProcessor, logger testutil.TestLogger) error {
		assert.Equal(t, jobID, processor.JobID)
		logger.Log(fmt.Sprintf("Worker %d processing job %d", workerID, jobID))
		// Simulate work
		time.Sleep(time.Millisecond * time.Duration(jobID%5))
		return nil
	})
	return err == nil
}
