package godi

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"
)

// Disposable is implemented by resources that need cleanup. godi also disposes
// services implementing ContextCloser or Shutdowner; see Shutdown.
//
// Close must not recursively call Close on the Provider or Scope that owns the
// resource. Shutdown is serialized so concurrent callers receive the same
// final result, which makes recursive owner shutdown deadlock by definition.
type Disposable interface {
	Close() error
}

// ContextCloser is implemented by resources whose cleanup honors a context:
// Close(ctx) receives the context passed to Shutdown (or
// context.Background() for Close), so it can stop when the shutdown budget
// runs out.
type ContextCloser interface {
	Close(ctx context.Context) error
}

// Shutdowner is implemented by resources with a graceful shutdown, such as
// *http.Server.
//
// Shutdown with a deadline calls Shutdown(ctx), and if the graceful shutdown
// runs out of time and the resource also implements Disposable, forces it
// with Close() — the *http.Server pattern. A plain Close of the owner prefers
// Close() when available, since a graceful shutdown without a deadline could
// wait forever.
type Shutdowner interface {
	Shutdown(ctx context.Context) error
}

// Shutdown disposes a Provider or Scope like Close, but waits at most until
// ctx is done. Context-aware resources (ContextCloser, Shutdowner) receive
// ctx, so they can finish early.
//
// When ctx is done first, Shutdown returns a *DisposalError matching
// context.DeadlineExceeded or context.Canceled (errors.Is). Cleanup cannot be
// preempted, so it keeps running in the background; a later Close waits for
// it to finish and returns its result.
//
// Any other Disposable is closed the same way, bounded by ctx; if ctx is done
// first, the returned error matches the context error (errors.Is).
func Shutdown(ctx context.Context, d Disposable) error {
	if d == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if owner, ok := d.(interface{ shutdown(context.Context) error }); ok {
		return owner.shutdown(ctx)
	}
	if ctx.Done() == nil {
		return safeDispose(ctx, d)
	}

	done := make(chan error, 1)
	go func() { done <- safeDispose(ctx, d) }()
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		select {
		case err := <-done:
			return err
		default:
		}
		return fmt.Errorf("shutdown incomplete: %w", contextFailure(ctx))
	}
}

// contextFailure returns why ctx is done: ctx.Err(), plus a custom cause
// (context.WithCancelCause, WithTimeoutCause) when there is one, so that both
// errors.Is(err, context.Canceled) and errors.Is(err, cause) match.
func contextFailure(ctx context.Context) error {
	err := ctx.Err()
	cause := context.Cause(ctx)
	if cause == nil || errors.Is(cause, err) {
		return err
	}
	return fmt.Errorf("%w: %w", err, cause)
}

// isDisposable reports whether v has a cleanup method godi calls.
func isDisposable(v any) bool {
	switch v.(type) {
	case Disposable, ContextCloser, Shutdowner:
		return true
	default:
		return false
	}
}

// dispose runs v's cleanup method with ctx.
//
// When ctx can be cancelled (Shutdown), context-aware methods are preferred:
// Close(ctx), else Shutdown(ctx) with a forced Close() fallback when the
// graceful shutdown runs out of time. Without a deadline (Close), Close() is
// preferred when available, since an unbounded graceful shutdown could wait
// forever.
func dispose(ctx context.Context, v any) error {
	if c, ok := v.(ContextCloser); ok {
		return c.Close(ctx)
	}
	s, isShutdowner := v.(Shutdowner)
	d, isCloser := v.(Disposable)
	bounded := ctx.Done() != nil
	if isShutdowner && (bounded || !isCloser) {
		err := s.Shutdown(ctx)
		if err != nil && isCloser && ctx.Err() != nil {
			// The graceful shutdown ran out of time: force it.
			return d.Close()
		}
		return err
	}
	if isCloser {
		return d.Close()
	}
	return nil
}

// safeDispose calls dispose with panic recovery so a single misbehaving
// resource can't abort the rest of a teardown loop. Recovered panics are
// returned as an error so the caller can aggregate them into a DisposalError.
func safeDispose(ctx context.Context, v any) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic during Close: %v", r)
		}
	}()
	return dispose(ctx, v)
}

// closeOrphan disposes a resource produced for an owner that has already been
// torn down. There is no caller to report errors to, and the goroutine that
// produced the orphan must not crash, so errors and panics go only to the
// observer.
func (p *provider) closeOrphan(v any, scopeID string) {
	if v == nil {
		return
	}
	_ = p.disposeObserved(context.Background(), v, scopeID)
}

// disposeObserved disposes v (see safeDispose) and reports it to the
// provider's observer.
func (p *provider) disposeObserved(ctx context.Context, v any, scopeID string) error {
	if p == nil || p.observer.Disposed == nil {
		return safeDispose(ctx, v)
	}
	start := time.Now()
	err := safeDispose(ctx, v)
	p.observer.Disposed(&DisposedEvent{
		Type:     reflect.TypeOf(v),
		ScopeID:  scopeID,
		Duration: time.Since(start),
		Err:      err,
	})
	return err
}

// shutdownIncomplete reports a shutdown that stopped waiting because ctx was
// done before cleanup finished.
func shutdownIncomplete(owner DisposalContext, ctx context.Context) error {
	return &DisposalError{
		Context: owner,
		Errors:  []error{fmt.Errorf("shutdown incomplete, cleanup continues in the background: %w", contextFailure(ctx))},
	}
}

type disposableIdentity struct {
	typ   reflect.Type
	value any
}

// identifyDisposable returns a stable identity for reference-backed disposable
// values. Equal struct values are not deduplicated because they may represent
// independently produced resources that must each be closed.
func identifyDisposable(v any) (disposableIdentity, bool) {
	if v == nil {
		return disposableIdentity{}, false
	}
	value := reflect.ValueOf(v)
	if value.Kind() != reflect.Pointer && value.Kind() != reflect.Chan {
		return disposableIdentity{}, false
	}
	if value.IsNil() {
		return disposableIdentity{}, false
	}
	return disposableIdentity{typ: value.Type(), value: v}, true
}

// ---------------------------------------------------------------------------
// Start and HealthCheck
// ---------------------------------------------------------------------------

// Starter is implemented by singletons that have startup work to run after
// the whole graph is built, such as starting a server or a consumer loop.
type Starter interface {
	Start(ctx context.Context) error
}

// Start calls Start(ctx) on every created singleton that implements Starter,
// in creation order (dependencies first), stopping at the first error. Lazy
// singletons that have not been resolved yet are not created. Start runs at
// most once per provider. Stop started services by closing the provider
// (godi.Shutdown with a deadline): disposal runs in reverse creation order.
func Start(ctx context.Context, p Provider) error {
	root := rootProviderOf(p)
	if root == nil {
		return errors.New("godi.Start requires a Provider built by godi")
	}
	if root.disposed.Load() != 0 {
		return ErrProviderDisposed
	}
	if !root.started.CompareAndSwap(false, true) {
		return errors.New("godi.Start: the provider has already been started")
	}
	for _, s := range root.createdSingletons() {
		starter, ok := s.instance.(Starter)
		if !ok {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := starter.Start(ctx); err != nil {
			return fmt.Errorf("start %s: %w", formatType(s.serviceType), err)
		}
	}
	return nil
}

// HealthChecker is implemented by singletons that can report their health,
// such as a database pool pinging its server.
type HealthChecker interface {
	HealthCheck(ctx context.Context) error
}

// HealthCheck runs HealthCheck(ctx) concurrently on every created singleton
// that implements HealthChecker and returns their failures joined, each
// prefixed with its service type, or nil when all are healthy. It creates
// no services.
func HealthCheck(ctx context.Context, p Provider) error {
	root := rootProviderOf(p)
	if root == nil {
		return errors.New("godi.HealthCheck requires a Provider built by godi")
	}
	if root.disposed.Load() != 0 {
		return ErrProviderDisposed
	}

	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, s := range root.createdSingletons() {
		checker, ok := s.instance.(HealthChecker)
		if !ok {
			continue
		}
		wg.Go(func() {
			if err := checker.HealthCheck(ctx); err != nil {
				mu.Lock()
				errs = append(errs, fmt.Errorf("%s: %w", formatType(s.serviceType), err))
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	return errors.Join(errs...)
}

type createdSingleton struct {
	serviceType reflect.Type
	instance    any
}

// recordConstructed adds a constructed singleton (before decoration) to the
// inventory that Start and HealthCheck act on.
func (p *provider) recordConstructed(serviceType reflect.Type, instance any) {
	p.constructedMu.Lock()
	p.constructed = append(p.constructed, createdSingleton{serviceType: serviceType, instance: instance})
	p.constructedMu.Unlock()
}

// createdSingletons returns the singletons constructed so far, before
// decoration, in creation order, each once (interface aliases and sibling
// outputs share an instance).
func (p *provider) createdSingletons() []createdSingleton {
	p.constructedMu.Lock()
	constructed := append([]createdSingleton(nil), p.constructed...)
	p.constructedMu.Unlock()

	seen := make(map[disposableIdentity]struct{}, len(constructed))
	created := constructed[:0]
	for _, c := range constructed {
		if identity, identifiable := identifyDisposable(c.instance); identifiable {
			if _, dup := seen[identity]; dup {
				continue
			}
			seen[identity] = struct{}{}
		}
		created = append(created, c)
	}
	return created
}
