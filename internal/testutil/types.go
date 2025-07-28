package testutil

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/junioryono/godi/v3"
)

// Common test errors
var (
	ErrTest            = errors.New("test error")
	ErrIntentional     = errors.New("intentional error")
	ErrConstructor     = errors.New("constructor error")
	ErrDisposal        = errors.New("disposal error")
	ErrAlreadyDisposed = errors.New("already disposed")
	ErrAlreadyClosed   = errors.New("already closed")
	ErrStop            = errors.New("stop")
)

// TestService is a basic test service
type TestService struct {
	ID        string
	CreatedAt time.Time
	Data      string
}

// NewTestService creates a new test service
func NewTestService() *TestService {
	return &TestService{
		ID:        uuid.NewString(),
		CreatedAt: time.Now(),
		Data:      "test",
	}
}

// TestLogger is a test logger interface
type TestLogger interface {
	Log(msg string)
	GetLogs() []string
}

// TestLoggerImpl implements TestLogger
type TestLoggerImpl struct {
	logs []string
	mu   sync.Mutex
}

func NewTestLogger() TestLogger {
	return &TestLoggerImpl{}
}

func (l *TestLoggerImpl) Log(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logs = append(l.logs, msg)
}

func (l *TestLoggerImpl) GetLogs() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	result := make([]string, len(l.logs))
	copy(result, l.logs)
	return result
}

// TestDatabase is a test database interface
type TestDatabase interface {
	Query(sql string) string
	Close() error
}

// TestDatabaseImpl implements TestDatabase
type TestDatabaseImpl struct {
	name     string
	closed   bool
	closeMu  sync.Mutex
	closeErr error
}

func NewTestDatabase() TestDatabase {
	return &TestDatabaseImpl{name: "testdb"}
}

func NewTestDatabaseNamed(name string) TestDatabase {
	return &TestDatabaseImpl{name: name}
}

func (d *TestDatabaseImpl) Query(sql string) string {
	return fmt.Sprintf("%s: %s", d.name, sql)
}

func (d *TestDatabaseImpl) Close() error {
	d.closeMu.Lock()
	defer d.closeMu.Unlock()

	if d.closed {
		return ErrAlreadyClosed
	}
	d.closed = true
	return d.closeErr
}

// TestCache is a test cache interface
type TestCache interface {
	Get(key string) (string, bool)
	Set(key string, value string)
}

// TestCacheImpl implements TestCache
type TestCacheImpl struct {
	data map[string]string
	mu   sync.RWMutex
}

func NewTestCache() TestCache {
	return &TestCacheImpl{data: make(map[string]string)}
}

func (c *TestCacheImpl) Get(key string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.data[key]
	return val, ok
}

func (c *TestCacheImpl) Set(key, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data == nil {
		c.data = make(map[string]string)
	}
	c.data[key] = value
}

// TestDisposable is a test type that implements Disposable
type TestDisposable struct {
	ID           string
	disposed     bool
	disposeError error
	disposeTime  time.Time
	mu           sync.Mutex
}

func NewTestDisposable() *TestDisposable {
	return &TestDisposable{
		ID: uuid.NewString(),
	}
}

func NewTestDisposableWithError(err error) *TestDisposable {
	return &TestDisposable{
		ID:           uuid.NewString(),
		disposeError: err,
	}
}

func (s *TestDisposable) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.disposed {
		return ErrAlreadyDisposed
	}

	s.disposed = true
	s.disposeTime = time.Now()
	return s.disposeError
}

func (s *TestDisposable) IsDisposed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.disposed
}

// TestContextDisposable implements DisposableWithContext
type TestContextDisposable struct {
	ID          string
	disposed    bool
	ctx         context.Context
	disposeErr  error
	disposeTime time.Duration
	mu          sync.Mutex
}

func NewTestContextDisposable() *TestContextDisposable {
	return &TestContextDisposable{
		ID: uuid.NewString(),
	}
}

func (s *TestContextDisposable) Close(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.disposed {
		return ErrAlreadyDisposed
	}

	s.ctx = ctx

	if s.disposeTime > 0 {
		select {
		case <-time.After(s.disposeTime):
			// Normal disposal
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	s.disposed = true
	return s.disposeErr
}

func (s *TestContextDisposable) SetDisposeTime(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disposeTime = d
}

func (s *TestContextDisposable) SetDisposeError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disposeErr = err
}

func (s *TestContextDisposable) WasDisposedWithContext() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ctx != nil
}

func (s *TestContextDisposable) IsDisposed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.disposed
}

// TestHandler is a test handler interface
type TestHandler interface {
	Handle() string
}

// TestHandlerImpl implements TestHandler
type TestHandlerImpl struct {
	name string
}

func NewTestHandler(name string) TestHandler {
	return &TestHandlerImpl{name: name}
}

func (h *TestHandlerImpl) Handle() string {
	return h.name
}

// TestServiceWithDeps is a service with dependencies for testing
type TestServiceWithDeps struct {
	Logger   TestLogger
	Database TestDatabase
	Cache    TestCache
	ID       string
}

func NewTestServiceWithDeps(logger TestLogger, db TestDatabase, cache TestCache) *TestServiceWithDeps {
	return &TestServiceWithDeps{
		Logger:   logger,
		Database: db,
		Cache:    cache,
		ID:       uuid.NewString(),
	}
}

// TestServiceParams demonstrates parameter objects
type TestServiceParams struct {
	godi.In

	Logger   TestLogger
	Database TestDatabase
	Cache    TestCache `optional:"true"`
}

// TestServiceResult demonstrates result objects
type TestServiceResult struct {
	godi.Out

	Service  *TestService
	Logger   TestLogger   `name:"service"`
	Database TestDatabase `group:"databases"`
}

// CircularServiceA and CircularServiceB for testing circular dependencies
type CircularServiceA struct {
	B *CircularServiceB
}

type CircularServiceB struct {
	A *CircularServiceA
}

func NewCircularServiceA(b *CircularServiceB) *CircularServiceA {
	return &CircularServiceA{B: b}
}

func NewCircularServiceB(a *CircularServiceA) *CircularServiceB {
	return &CircularServiceB{A: a}
}

// CloserFunc is a helper type to wrap a function as a Disposable
type CloserFunc func() error

func (f CloserFunc) Close() error {
	return f()
}

// DecoratedLogger wraps another logger with a prefix
type DecoratedLogger struct {
	Inner  TestLogger
	Prefix string
}

func (d *DecoratedLogger) Log(message string) {
	d.Inner.Log(d.Prefix + message)
}

// GetLogs returns the logs with the prefix applied
func (d *DecoratedLogger) GetLogs() []string {
	logs := d.Inner.GetLogs()
	prefixedLogs := make([]string, len(logs))
	for i, log := range logs {
		prefixedLogs[i] = d.Prefix + log
	}
	return prefixedLogs
}

// DecoratedDatabase wraps another database with a prefix
type DecoratedDatabase struct {
	Inner  TestDatabase
	Prefix string
}

func (d *DecoratedDatabase) Query(sql string) string {
	return d.Inner.Query(d.Prefix + sql)
}

func (d *DecoratedDatabase) Close() error {
	return d.Inner.Close()
}

// DecoratedHandler wraps another handler with a prefix
type DecoratedHandler struct {
	Inner  TestHandler
	Prefix string
}

func (d *DecoratedHandler) Handle() string {
	return d.Prefix + d.Inner.Handle()
}

// TestServiceWithLogger is a service that has a logger dependency
type TestServiceWithLogger struct {
	ID     string
	Logger TestLogger
}
