package godi

import "context"

// Disposable allows disposal with context for graceful shutdown.
// Services implementing this interface can perform context-aware cleanup.
//
// Example:
//
//	type DatabaseConnection struct {
//	    conn *sql.DB
//	}
//
//	func (dc *DatabaseConnection) Close() error {
//	    return dc.conn.Close()
//	}
type Disposable interface {
	// Close disposes the resource with the provided context.
	// Implementations should respect context cancellation for graceful shutdown.
	Close() error
}

// DisposableWithContext allows disposal with context for graceful shutdown.
// Services implementing this interface can perform context-aware cleanup.
//
// Example:
//
//	type DatabaseConnection struct {
//	    conn *sql.DB
//	}
//
//	func (dc *DatabaseConnection) Close(ctx context.Context) error {
//	    done := make(chan error, 1)
//	    go func() {
//	        done <- dc.conn.Close()
//	    }()
//
//	    select {
//	    case err := <-done:
//	        return err
//	    case <-ctx.Done():
//	        return ctx.Err()
//	    }
//	}
type DisposableWithContext interface {
	// Close disposes the resource with the provided context.
	// Implementations should respect context cancellation for graceful shutdown.
	Close(ctx context.Context) error
}
