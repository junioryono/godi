package godi

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ============================================================================
// Shared Test Types
// ============================================================================

// TService is a basic service for testing.
type TService struct {
	ID    string
	Value int
}

// TDependency is a basic dependency for testing.
type TDependency struct {
	Name string
}

// TServiceWithDeps demonstrates dependency injection.
type TServiceWithDeps struct {
	Svc *TService
	Dep *TDependency
}

// TInterface is a basic interface for testing.
type TInterface interface {
	GetID() string
}

func (s *TService) GetID() string { return s.ID }

// TDisposable implements io.Closer for lifecycle testing.
type TDisposable struct {
	Name      string
	closed    atomic.Bool
	closeErr  error
	closeChan chan struct{}
	mu        sync.Mutex
}

func (d *TDisposable) Close() error {
	if d.closed.Swap(true) {
		return errors.New("already closed")
	}
	if d.closeChan != nil {
		close(d.closeChan)
	}
	return d.closeErr
}

func (d *TDisposable) IsClosed() bool {
	return d.closed.Load()
}

// SetCloseError sets an error to return on Close.
func (d *TDisposable) SetCloseError(err error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.closeErr = err
}

// TScoped represents a scoped service with creation tracking.
type TScoped struct {
	Created time.Time
	ScopeID string
}

// TTransient represents a transient service.
type TTransient struct {
	Instance int
}

// ============================================================================
// Circular Dependency Test Types
// ============================================================================

type TCircularA struct{ B *TCircularB }
type TCircularB struct{ A *TCircularA }

func NewTCircularA(b *TCircularB) *TCircularA { return &TCircularA{B: b} }
func NewTCircularB(a *TCircularA) *TCircularB { return &TCircularB{A: a} }

// ============================================================================
// Param/Result Object Test Types
// ============================================================================

// TParams demonstrates parameter object injection.
type TParams struct {
	In
	Svc      *TService
	Dep      *TDependency `optional:"true"`
	Named    *TService    `name:"named"`
	Services []*TService  `group:"services"`
	Iface    TInterface   `optional:"true"`
}

// TResult demonstrates result object registration.
type TResult struct {
	Out
	Primary   *TService
	Secondary *TService `name:"secondary"`
	Grouped   *TService `group:"services"`
}

// ============================================================================
// Shared Constructors
// ============================================================================

var instanceCounter atomic.Int64

func NewTService() *TService {
	return &TService{ID: "test", Value: 42}
}

func NewTServiceWithID(id string) func() *TService {
	return func() *TService {
		return &TService{ID: id, Value: 42}
	}
}

func NewTServiceWithValue(id string, value int) func() *TService {
	return func() *TService {
		return &TService{ID: id, Value: value}
	}
}

func NewTDependency() *TDependency {
	return &TDependency{Name: "dep"}
}

func NewTDependencyWithName(name string) func() *TDependency {
	return func() *TDependency {
		return &TDependency{Name: name}
	}
}

func NewTServiceWithDeps(svc *TService, dep *TDependency) *TServiceWithDeps {
	return &TServiceWithDeps{Svc: svc, Dep: dep}
}

func NewTDisposable() *TDisposable {
	return &TDisposable{Name: "disposable", closeChan: make(chan struct{})}
}

func NewTDisposableWithName(name string) func() *TDisposable {
	return func() *TDisposable {
		return &TDisposable{Name: name, closeChan: make(chan struct{})}
	}
}

func NewTScoped() *TScoped {
	return &TScoped{Created: time.Now(), ScopeID: "default"}
}

func NewTTransient() *TTransient {
	return &TTransient{Instance: int(instanceCounter.Add(1))}
}

// Error-returning constructors

func NewTServiceError() (*TService, error) {
	return nil, errors.New("constructor error")
}

func NewTServiceWithError() (*TService, error) {
	return &TService{ID: "with-error", Value: 1}, nil
}

// Multi-return constructors

func NewTMultiReturn() (*TService, *TDependency) {
	return &TService{ID: "multi", Value: 1}, &TDependency{Name: "multi-dep"}
}

func NewTMultiReturnWithError() (*TService, *TDependency, error) {
	return &TService{ID: "multi-err", Value: 2}, &TDependency{Name: "multi-err-dep"}, nil
}

func NewTTripleReturn() (*TService, *TDependency, *TDisposable) {
	return &TService{ID: "triple"}, &TDependency{Name: "triple"}, &TDisposable{Name: "triple"}
}

// Result object constructor

func NewTResult() TResult {
	return TResult{
		Primary:   &TService{ID: "primary", Value: 1},
		Secondary: &TService{ID: "secondary", Value: 2},
		Grouped:   &TService{ID: "grouped", Value: 3},
	}
}

// Param object constructor

func NewTFromParams(p TParams) *TServiceWithDeps {
	var depName string
	if p.Dep != nil {
		depName = p.Dep.Name
	}
	return &TServiceWithDeps{
		Svc: p.Svc,
		Dep: &TDependency{Name: depName},
	}
}

// Void constructor (for testing edge cases)

func NewTVoid() {}

// ============================================================================
// Test Helpers
// ============================================================================

// BuildProvider creates a provider with the given module options.
// Automatically registers cleanup.
func BuildProvider(t *testing.T, opts ...ModuleOption) Provider {
	t.Helper()
	c := NewCollection()
	if len(opts) > 0 {
		require.NoError(t, c.AddModules(opts...))
	}
	p, err := c.Build()
	require.NoError(t, err)
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// BuildScope creates a scope with the given module options.
// Automatically registers cleanup.
func BuildScope(t *testing.T, opts ...ModuleOption) Scope {
	t.Helper()
	p := BuildProvider(t, opts...)
	s, err := p.CreateScope(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// BuildCollection creates a collection with the given module options.
func BuildCollection(t *testing.T, opts ...ModuleOption) Collection {
	t.Helper()
	c := NewCollection()
	if len(opts) > 0 {
		require.NoError(t, c.AddModules(opts...))
	}
	return c
}

// RequireResolve resolves a service or fails the test.
func RequireResolve[T any](t *testing.T, p Provider) T {
	t.Helper()
	v, err := Resolve[T](p)
	require.NoError(t, err)
	return v
}

// RequireResolveFrom resolves from a scope or fails the test.
func RequireResolveFrom[T any](t *testing.T, s Scope) T {
	t.Helper()
	v, err := Resolve[T](s)
	require.NoError(t, err)
	return v
}

// RequireResolveKeyed resolves a keyed service or fails the test.
func RequireResolveKeyed[T any](t *testing.T, p Provider, key any) T {
	t.Helper()
	v, err := ResolveKeyed[T](p, key)
	require.NoError(t, err)
	return v
}

// TypeOf returns the reflect.Type for a type parameter.
func TypeOf[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}

// PtrTypeOf returns the reflect.Type for a pointer to the type parameter.
func PtrTypeOf[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil))
}
