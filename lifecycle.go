package godi

import (
	"fmt"
	"sync"
)

// lifecycleManager manages the lifecycle of disposable instances
type lifecycleManager struct {
	disposables []Disposable
	mu          sync.Mutex
}

// newLifecycleManager creates a new lifecycle manager
func newLifecycleManager() *lifecycleManager {
	return &lifecycleManager{
		disposables: make([]Disposable, 0),
	}
}

// track adds a disposable instance to be managed
func (m *lifecycleManager) track(instance any) {
	if d, ok := instance.(Disposable); ok {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.disposables = append(m.disposables, d)
	}
}

// dispose disposes all tracked instances in reverse order
func (m *lifecycleManager) dispose() error {
	m.mu.Lock()
	disposables := m.disposables
	m.disposables = nil
	m.mu.Unlock()

	var errs []error

	// Dispose in reverse order (LIFO)
	for i := len(disposables) - 1; i >= 0; i-- {
		if err := disposables[i].Close(); err != nil {
			errs = append(errs, fmt.Errorf("disposal error: %w", err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("lifecycle disposal errors: %v", errs)
	}

	return nil
}

// clear removes all tracked instances without disposing them
func (m *lifecycleManager) clear() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disposables = nil
}
