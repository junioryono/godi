package godi

import (
	"reflect"
	"time"
)

// Observer receives construction and disposal events from a provider and its
// scopes (see WithObserver). Set the callbacks you need; nil ones are
// skipped, and new events may be added as fields. Callbacks are called
// synchronously on the goroutine doing the work, so they must be safe for
// concurrent use and should return quickly.
type Observer struct {
	// Constructed, if set, reports a constructor call: its service, scope,
	// duration and error (nil on success).
	Constructed func(*ConstructedEvent)
	// Disposed, if set, reports a resource's cleanup, including cleanup of
	// values produced after their owner closed, which has no caller to
	// report to.
	Disposed func(*DisposedEvent)
}

// ConstructedEvent describes one constructor call.
type ConstructedEvent struct {
	ServiceType reflect.Type
	Key         any
	Lifetime    Lifetime
	// ScopeID is the ID of the scope that ran the constructor (the root
	// scope for singletons).
	ScopeID string
	// Constructor names the constructor and its source location.
	Constructor string
	Duration    time.Duration
	Err         error
}

// DisposedEvent describes one resource's cleanup.
type DisposedEvent struct {
	// Type is the disposed value's dynamic type.
	Type reflect.Type
	// ScopeID is the ID of the owning scope, or "" for the provider.
	ScopeID  string
	Duration time.Duration
	Err      error
}
