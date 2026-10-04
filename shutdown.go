package godi

import (
	"context"
	"fmt"
	"reflect"
)

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
// Any other Disposable is closed the same way, bounded by ctx.
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
		return fmt.Errorf("shutdown incomplete: %w", context.Cause(ctx))
	}
}

// NoDispose is an AddOption declaring that the registered service's lifetime
// is managed outside the container: godi never disposes it. Use it for
// resources the application shares or closes itself, such as a pre-built
// *sql.DB or os.Stdout passed to AddSingleton.
//
// A NoDispose value is also never adopted by a scope that merely returns it.
func NoDispose() AddOption {
	return addNoDisposeOption{}
}

type addNoDisposeOption struct{}

func (addNoDisposeOption) String() string { return "NoDispose()" }

func (addNoDisposeOption) applyAddOption(opt *addOptions) {
	opt.NoDispose = true
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
// torn down. Errors and panics are discarded: there is no caller to report
// them to, and the goroutine that produced the orphan must not crash.
func closeOrphan(v any) {
	if v == nil {
		return
	}
	_ = safeDispose(context.Background(), v)
}

// shutdownIncomplete reports a shutdown that stopped waiting because ctx was
// done before cleanup finished.
func shutdownIncomplete(owner string, ctx context.Context) error {
	return &DisposalError{
		Context: owner,
		Errors:  []error{fmt.Errorf("shutdown incomplete, cleanup continues in the background: %w", context.Cause(ctx))},
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
