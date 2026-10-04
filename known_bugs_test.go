package godi_test

// Regression tests for bugs found by the model-based test (model_test.go).
// Each is skipped until the bug is fixed; the model works around the same
// bug through a known* constant, to be set to false with the fix.

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/junioryono/godi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// closeLog records Close calls in order.
type closeLog struct {
	mu     sync.Mutex
	closed []string
}

func (l *closeLog) add(name string) {
	l.mu.Lock()
	l.closed = append(l.closed, name)
	l.mu.Unlock()
}

func (l *closeLog) names() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.closed)
}

type bugStore struct {
	name string
	log  *closeLog
}

func (s *bugStore) Close() error { s.log.add(s.name); return nil }

type bugConn struct {
	name string
	log  *closeLog
}

func (c *bugConn) Close() error { c.log.add(c.name); return nil }

type (
	bugDep   struct{}
	bugCache struct{}
	bugReq   struct{}
	bugApp   struct{}
)

func TestDecoratorChainClosesIntermediateResults(t *testing.T) {
	t.Skip("bug: with several decorators, the results of all but the last decorator are never closed")
	t.Parallel()

	log := &closeLog{}
	c := godi.NewCollection()
	c.AddSingleton(func() *bugStore { return &bugStore{name: "original", log: log} })
	c.AddModules(
		godi.Decorate(func(*bugStore) *bugStore { return &bugStore{name: "wrapper1", log: log} }),
		godi.Decorate(func(*bugStore) *bugStore { return &bugStore{name: "wrapper2", log: log} }),
	)
	p, err := c.Build()
	require.NoError(t, err)
	require.NoError(t, p.Close())

	// Today: [wrapper2 original].
	assert.Equal(t, []string{"wrapper2", "wrapper1", "original"}, log.names())
}

func TestDecoratorFailureClosesSiblingOutputs(t *testing.T) {
	t.Skip("bug: when a decorator of one output of a multi-output constructor fails, the outputs before it are never closed")
	t.Parallel()

	log := &closeLog{}
	c := godi.NewCollection()
	c.AddScoped(func() (*bugConn, *bugStore) {
		return &bugConn{name: "conn", log: log}, &bugStore{name: "store", log: log}
	})
	c.AddScoped(func() (*bugDep, error) { return nil, errors.New("dependency failed") })
	c.AddModules(godi.Decorate(func(s *bugStore, _ *bugDep) *bugStore { return s }))
	p, err := c.Build()
	require.NoError(t, err)

	s, err := p.CreateScope(context.Background())
	require.NoError(t, err)
	_, err = godi.Resolve[*bugConn](s)
	require.Error(t, err)
	require.NoError(t, s.Close())
	require.NoError(t, p.Close())

	// Today: [store]; conn is never closed.
	assert.ElementsMatch(t, []string{"conn", "store"}, log.names())
}

func TestDecoratorDependingOnSiblingOutput(t *testing.T) {
	t.Skip("bug: a decorator depending on another output of its service's constructor deadlocks Build (singleton) or resolution (scoped) although Validate passes")
	t.Parallel()

	// within fails the test instead of hanging.
	within := func(t *testing.T, what string, f func() error) error {
		t.Helper()
		done := make(chan error, 1)
		go func() { done <- f() }()
		select {
		case err := <-done:
			return err
		case <-time.After(5 * time.Second):
			t.Fatalf("%s deadlocked", what)
			return nil
		}
	}

	for _, lifetime := range []godi.Lifetime{godi.Singleton, godi.Scoped} {
		t.Run(lifetime.String(), func(t *testing.T) {
			c := godi.NewCollection()
			ctor := func() (*bugStore, *bugCache) { return &bugStore{log: &closeLog{}}, &bugCache{} }
			if lifetime == godi.Singleton {
				c.AddSingleton(ctor)
			} else {
				c.AddScoped(ctor)
			}
			c.AddModules(godi.Decorate(func(s *bugStore, _ *bugCache) *bugStore { return s }))

			validateErr := godi.Validate(c)
			var p godi.Provider
			buildErr := within(t, "Build", func() error {
				var err error
				p, err = c.Build()
				return err
			})
			// Either the wiring is rejected up front, or it works.
			assert.Equal(t, validateErr == nil, buildErr == nil, "Validate: %v, Build: %v", validateErr, buildErr)
			if buildErr != nil {
				return
			}
			defer p.Close()
			s, err := p.CreateScope(context.Background())
			require.NoError(t, err)
			defer s.Close()
			assert.NoError(t, within(t, "Resolve", func() error {
				_, err := godi.Resolve[*bugStore](s)
				return err
			}))
		})
	}
}

func TestInstanceDecoratorClosesTransientDependencies(t *testing.T) {
	t.Skip("bug: transient dependencies of a decorator of an instance (value) registration are never closed")
	t.Parallel()

	log := &closeLog{}
	c := godi.NewCollection()
	c.AddSingleton(&bugStore{name: "instance", log: log})
	c.AddTransient(func() *bugConn { return &bugConn{name: "conn", log: log} })
	c.AddModules(godi.Decorate(func(s *bugStore, _ *bugConn) *bugStore { return s }))
	p, err := c.Build()
	require.NoError(t, err)
	require.NoError(t, p.Close())

	// Today: [instance]. The decorator's transient dependency belongs to the
	// singleton, like a constructor's.
	assert.Equal(t, []string{"instance", "conn"}, log.names())
}

func TestLifetimeValidationFollowsSiblingDecorators(t *testing.T) {
	t.Skip("bug: lifetime validation ignores the decorators of a constructor's other outputs, so a singleton captures a scoped service through them")
	t.Parallel()

	c := godi.NewCollection()
	c.AddScoped(func() *bugReq { return &bugReq{} })
	c.AddTransient(func() (*bugStore, *bugCache) { return &bugStore{log: &closeLog{}}, &bugCache{} })
	// Every construction of the transient pair runs this decorator, which
	// needs the scoped *bugReq...
	c.AddModules(godi.Decorate(func(c *bugCache, _ *bugReq) *bugCache { return c }))
	// ...so this singleton captures one request's *bugReq for good. Without
	// ValidateScopes, Build succeeds and creates *bugReq in the root scope;
	// with it, Validate passes but Build fails.
	c.AddSingleton(func(*bugStore) *bugApp { return &bugApp{} })

	var conflict *godi.LifetimeConflictError
	assert.ErrorAs(t, godi.Validate(c), &conflict)
}
