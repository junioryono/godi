package godi

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test types for validation tests
type ValidationServiceA struct {
	B *ValidationServiceB
}

type ValidationServiceB struct {
	C *ValidationServiceC
}

type ValidationServiceC struct {
	A *ValidationServiceA // Creates a cycle
}

type ValidationServiceD struct {
	// No dependencies
}

type ValidationServiceE struct {
	D *ValidationServiceD
}

// Constructor functions
func NewValidationServiceA(b *ValidationServiceB) *ValidationServiceA {
	return &ValidationServiceA{B: b}
}

func NewValidationServiceB(c *ValidationServiceC) *ValidationServiceB {
	return &ValidationServiceB{C: c}
}

func NewValidationServiceC(a *ValidationServiceA) *ValidationServiceC {
	return &ValidationServiceC{A: a}
}

func NewValidationServiceD() *ValidationServiceD {
	return &ValidationServiceD{}
}

func NewValidationServiceE(d *ValidationServiceD) *ValidationServiceE {
	return &ValidationServiceE{D: d}
}

// Test validateDependencyGraph
func TestValidateDependencyGraph(t *testing.T) {
	t.Run("valid dependency graph", func(t *testing.T) {
		c := &collection{
			services:   make(map[TypeKey]*Descriptor),
			groups:     make(map[GroupKey][]*Descriptor),
			decorators: make(map[reflect.Type][]*Descriptor),
		}

		// Add services without cycles
		d1, err := newDescriptor(NewValidationServiceD, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		d2, err := newDescriptor(NewValidationServiceE, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		err = validateDependencyGraph(c)
		assert.NoError(t, err)
	})

	t.Run("circular dependency", func(t *testing.T) {
		c := &collection{
			services:   make(map[TypeKey]*Descriptor),
			groups:     make(map[GroupKey][]*Descriptor),
			decorators: make(map[reflect.Type][]*Descriptor),
		}

		// Create circular dependency A -> B -> C -> A
		dA, err := newDescriptor(NewValidationServiceA, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: dA.Type}] = dA

		dB, err := newDescriptor(NewValidationServiceB, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: dB.Type}] = dB

		dC, err := newDescriptor(NewValidationServiceC, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: dC.Type}] = dC

		err = validateDependencyGraph(c)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circular dependency")
	})

	t.Run("self dependency", func(t *testing.T) {
		type SelfDependent struct {
			Self *SelfDependent
		}

		newSelfDependent := func(self *SelfDependent) *SelfDependent {
			return &SelfDependent{Self: self}
		}

		c := &collection{
			services:   make(map[TypeKey]*Descriptor),
			groups:     make(map[GroupKey][]*Descriptor),
			decorators: make(map[reflect.Type][]*Descriptor),
		}

		d, err := newDescriptor(newSelfDependent, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d.Type}] = d

		err = validateDependencyGraph(c)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "circular dependency")
	})

	t.Run("graph with groups", func(t *testing.T) {
		c := &collection{
			services:   make(map[TypeKey]*Descriptor),
			groups:     make(map[GroupKey][]*Descriptor),
			decorators: make(map[reflect.Type][]*Descriptor),
		}

		// Add services to groups
		d1, err := newDescriptor(NewValidationServiceD, Singleton)
		require.NoError(t, err)
		d1.Group = "test-group"

		d2, err := newDescriptor(func() *ValidationServiceD {
			return &ValidationServiceD{}
		}, Singleton)
		require.NoError(t, err)
		d2.Group = "test-group"

		groupKey := GroupKey{
			Type:  reflect.TypeOf((*ValidationServiceD)(nil)),
			Group: "test-group",
		}
		c.groups[groupKey] = []*Descriptor{d1, d2}

		err = validateDependencyGraph(c)
		assert.NoError(t, err)
	})

	t.Run("empty collection", func(t *testing.T) {
		c := &collection{
			services:   make(map[TypeKey]*Descriptor),
			groups:     make(map[GroupKey][]*Descriptor),
			decorators: make(map[reflect.Type][]*Descriptor),
		}

		err := validateDependencyGraph(c)
		assert.NoError(t, err)
	})

	t.Run("complex valid graph", func(t *testing.T) {
		// Create a more complex but valid dependency graph
		type Service1 struct{}
		type Service2 struct{ S1 *Service1 }
		type Service3 struct {
			S1 *Service1
			S2 *Service2
		}
		type Service4 struct {
			S2 *Service2
			S3 *Service3
		}

		c := &collection{
			services:   make(map[TypeKey]*Descriptor),
			groups:     make(map[GroupKey][]*Descriptor),
			decorators: make(map[reflect.Type][]*Descriptor),
		}

		d1, err := newDescriptor(func() *Service1 { return &Service1{} }, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		d2, err := newDescriptor(func(s1 *Service1) *Service2 {
			return &Service2{S1: s1}
		}, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		d3, err := newDescriptor(func(s1 *Service1, s2 *Service2) *Service3 {
			return &Service3{S1: s1, S2: s2}
		}, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d3.Type}] = d3

		d4, err := newDescriptor(func(s2 *Service2, s3 *Service3) *Service4 {
			return &Service4{S2: s2, S3: s3}
		}, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d4.Type}] = d4

		err = validateDependencyGraph(c)
		assert.NoError(t, err)
	})
}

// Test validateLifetimes
func TestValidateLifetimes(t *testing.T) {
	t.Run("valid lifetimes", func(t *testing.T) {
		c := &collection{
			services:   make(map[TypeKey]*Descriptor),
			groups:     make(map[GroupKey][]*Descriptor),
			decorators: make(map[reflect.Type][]*Descriptor),
		}

		// Singleton depending on singleton - OK
		d1, err := newDescriptor(NewValidationServiceD, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		d2, err := newDescriptor(NewValidationServiceE, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		err = validateLifetimes(c)
		assert.NoError(t, err)
	})

	t.Run("singleton depending on scoped", func(t *testing.T) {
		c := &collection{
			services:   make(map[TypeKey]*Descriptor),
			groups:     make(map[GroupKey][]*Descriptor),
			decorators: make(map[reflect.Type][]*Descriptor),
		}

		// Scoped service
		d1, err := newDescriptor(NewValidationServiceD, Scoped)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		// Singleton depending on scoped - NOT OK
		d2, err := newDescriptor(NewValidationServiceE, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		err = validateLifetimes(c)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "singleton service")
		assert.Contains(t, err.Error(), "cannot depend on scoped service")
	})

	t.Run("singleton depending on transient", func(t *testing.T) {
		c := &collection{
			services:   make(map[TypeKey]*Descriptor),
			groups:     make(map[GroupKey][]*Descriptor),
			decorators: make(map[reflect.Type][]*Descriptor),
		}

		// Transient service
		d1, err := newDescriptor(NewValidationServiceD, Transient)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		// Singleton depending on transient - OK (transient is created fresh each time)
		d2, err := newDescriptor(NewValidationServiceE, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		err = validateLifetimes(c)
		assert.NoError(t, err)
	})

	t.Run("scoped depending on singleton", func(t *testing.T) {
		c := &collection{
			services:   make(map[TypeKey]*Descriptor),
			groups:     make(map[GroupKey][]*Descriptor),
			decorators: make(map[reflect.Type][]*Descriptor),
		}

		// Singleton service
		d1, err := newDescriptor(NewValidationServiceD, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		// Scoped depending on singleton - OK
		d2, err := newDescriptor(NewValidationServiceE, Scoped)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		err = validateLifetimes(c)
		assert.NoError(t, err)
	})

	t.Run("scoped depending on scoped", func(t *testing.T) {
		c := &collection{
			services:   make(map[TypeKey]*Descriptor),
			groups:     make(map[GroupKey][]*Descriptor),
			decorators: make(map[reflect.Type][]*Descriptor),
		}

		// Scoped service
		d1, err := newDescriptor(NewValidationServiceD, Scoped)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		// Scoped depending on scoped - OK
		d2, err := newDescriptor(NewValidationServiceE, Scoped)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		err = validateLifetimes(c)
		assert.NoError(t, err)
	})

	t.Run("transient depending on any", func(t *testing.T) {
		c := &collection{
			services:   make(map[TypeKey]*Descriptor),
			groups:     make(map[GroupKey][]*Descriptor),
			decorators: make(map[reflect.Type][]*Descriptor),
		}

		// Scoped service
		d1, err := newDescriptor(NewValidationServiceD, Scoped)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		// Transient depending on scoped - OK
		d2, err := newDescriptor(NewValidationServiceE, Transient)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		err = validateLifetimes(c)
		assert.NoError(t, err)
	})

	t.Run("empty collection", func(t *testing.T) {
		c := &collection{
			services:   make(map[TypeKey]*Descriptor),
			groups:     make(map[GroupKey][]*Descriptor),
			decorators: make(map[reflect.Type][]*Descriptor),
		}

		err := validateLifetimes(c)
		assert.NoError(t, err)
	})

}

// Test validateDescriptor
func TestValidateDescriptor(t *testing.T) {
	t.Run("valid descriptor", func(t *testing.T) {
		descriptor, err := newDescriptor(NewValidationServiceD, Singleton)
		require.NoError(t, err)

		err = descriptor.Validate()
		assert.NoError(t, err)
	})

	t.Run("descriptor with nil type", func(t *testing.T) {
		descriptor := &Descriptor{
			Type:            nil,
			Constructor:     reflect.ValueOf(NewValidationServiceD),
			ConstructorType: reflect.TypeOf(NewValidationServiceD),
			Lifetime:        Singleton,
		}

		err := descriptor.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "type cannot be nil")
	})

	t.Run("descriptor with invalid constructor", func(t *testing.T) {
		descriptor := &Descriptor{
			Type:            reflect.TypeOf((*ValidationServiceD)(nil)),
			Constructor:     reflect.Value{},
			ConstructorType: reflect.TypeOf(NewValidationServiceD),
			Lifetime:        Singleton,
		}

		err := descriptor.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "constructor is invalid")
	})

	t.Run("descriptor with invalid service lifetime", func(t *testing.T) {
		descriptor, err := newDescriptor(NewValidationServiceD, Singleton)
		require.NoError(t, err)

		descriptor.Lifetime = Lifetime(999)

		err = descriptor.Validate()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid service lifetime")
	})

	t.Run("all valid lifetimes", func(t *testing.T) {
		lifetimes := []Lifetime{Singleton, Scoped, Transient}

		for _, lt := range lifetimes {
			descriptor, err := newDescriptor(NewValidationServiceD, lt)
			require.NoError(t, err)

			err = descriptor.Validate()
			assert.NoError(t, err, "Lifetime %v should be valid", lt)
		}
	})
}

// Test complex validation scenarios
func TestComplexValidationScenarios(t *testing.T) {
	t.Run("multiple dependency chains", func(t *testing.T) {
		// Create a complex but valid dependency structure
		type Logger struct{}
		type Database struct{ Log *Logger }
		type Cache struct{ Log *Logger }
		type Repository struct {
			DB    *Database
			Cache *Cache
		}
		type Service struct {
			Repo *Repository
			Log  *Logger
		}

		c := &collection{
			services:   make(map[TypeKey]*Descriptor),
			groups:     make(map[GroupKey][]*Descriptor),
			decorators: make(map[reflect.Type][]*Descriptor),
		}

		// All singletons - should be valid
		descriptors := []struct {
			constructor any
			lifetime    Lifetime
		}{
			{func() *Logger { return &Logger{} }, Singleton},
			{func(log *Logger) *Database { return &Database{Log: log} }, Singleton},
			{func(log *Logger) *Cache { return &Cache{Log: log} }, Singleton},
			{func(db *Database, cache *Cache) *Repository {
				return &Repository{DB: db, Cache: cache}
			}, Singleton},
			{func(repo *Repository, log *Logger) *Service {
				return &Service{Repo: repo, Log: log}
			}, Singleton},
		}

		for _, desc := range descriptors {
			d, err := newDescriptor(desc.constructor, desc.lifetime)
			require.NoError(t, err)
			c.services[TypeKey{Type: d.Type}] = d
		}

		// Should pass both validations
		err := validateDependencyGraph(c)
		assert.NoError(t, err)

		err = validateLifetimes(c)
		assert.NoError(t, err)
	})

	t.Run("mixed lifetime validation", func(t *testing.T) {
		type Service1 struct{}
		type Service2 struct{ S1 *Service1 }
		type Service3 struct{ S2 *Service2 }

		c := &collection{
			services:   make(map[TypeKey]*Descriptor),
			groups:     make(map[GroupKey][]*Descriptor),
			decorators: make(map[reflect.Type][]*Descriptor),
		}

		// Service1: Singleton
		d1, err := newDescriptor(func() *Service1 { return &Service1{} }, Singleton)
		require.NoError(t, err)
		c.services[TypeKey{Type: d1.Type}] = d1

		// Service2: Scoped, depends on Singleton (OK)
		d2, err := newDescriptor(func(s1 *Service1) *Service2 {
			return &Service2{S1: s1}
		}, Scoped)
		require.NoError(t, err)
		c.services[TypeKey{Type: d2.Type}] = d2

		// Service3: Transient, depends on Scoped (OK)
		d3, err := newDescriptor(func(s2 *Service2) *Service3 {
			return &Service3{S2: s2}
		}, Transient)
		require.NoError(t, err)
		c.services[TypeKey{Type: d3.Type}] = d3

		err = validateLifetimes(c)
		assert.NoError(t, err)

		// Now change Service3 to Singleton - should fail
		d3.Lifetime = Singleton
		err = validateLifetimes(c)
		assert.Error(t, err)
	})
}

// Benchmark validation
func BenchmarkValidateDependencyGraph(b *testing.B) {
	c := &collection{
		services:   make(map[TypeKey]*Descriptor),
		groups:     make(map[GroupKey][]*Descriptor),
		decorators: make(map[reflect.Type][]*Descriptor),
	}

	// Add some services
	d1, _ := newDescriptor(NewValidationServiceD, Singleton)
	c.services[TypeKey{Type: d1.Type}] = d1

	d2, _ := newDescriptor(NewValidationServiceE, Singleton)
	c.services[TypeKey{Type: d2.Type}] = d2

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateDependencyGraph(c)
	}
}

func BenchmarkValidateLifetimes(b *testing.B) {
	c := &collection{
		services:   make(map[TypeKey]*Descriptor),
		groups:     make(map[GroupKey][]*Descriptor),
		decorators: make(map[reflect.Type][]*Descriptor),
	}

	// Add some services
	d1, _ := newDescriptor(NewValidationServiceD, Singleton)
	c.services[TypeKey{Type: d1.Type}] = d1

	d2, _ := newDescriptor(NewValidationServiceE, Scoped)
	c.services[TypeKey{Type: d2.Type}] = d2

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = validateLifetimes(c)
	}
}
