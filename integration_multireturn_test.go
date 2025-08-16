package godi_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/junioryono/godi/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test services for integration tests
type Database struct {
	ConnectionString string
}

type Cache struct {
	Provider string
}

type Logger struct {
	Level string
}

type UserRepository struct {
	DB    *Database
	Cache *Cache
}

type UserService struct {
	Repo   *UserRepository
	Logger *Logger
}

type AdminService struct {
	UserService *UserService
	Logger      *Logger
}

type NotificationService struct {
	Logger *Logger
}

// Multi-return constructors
func NewInfrastructure() (*Database, *Cache, *Logger) {
	return &Database{ConnectionString: "postgres://localhost"},
		&Cache{Provider: "redis"},
		&Logger{Level: "info"}
}

func NewInfrastructureWithError() (*Database, *Cache, *Logger, error) {
	return &Database{ConnectionString: "postgres://localhost"},
		&Cache{Provider: "redis"},
		&Logger{Level: "info"},
		nil
}

func NewServices(db *Database, cache *Cache, logger *Logger) (*UserRepository, *UserService, *AdminService) {
	repo := &UserRepository{DB: db, Cache: cache}
	userSvc := &UserService{Repo: repo, Logger: logger}
	adminSvc := &AdminService{UserService: userSvc, Logger: logger}
	return repo, userSvc, adminSvc
}

func NewServicesWithError(db *Database, cache *Cache, logger *Logger) (*UserRepository, *UserService, *AdminService, error) {
	if db == nil {
		return nil, nil, nil, errors.New("database required")
	}
	repo := &UserRepository{DB: db, Cache: cache}
	userSvc := &UserService{Repo: repo, Logger: logger}
	adminSvc := &AdminService{UserService: userSvc, Logger: logger}
	return repo, userSvc, adminSvc, nil
}

func NewMixedServices(logger *Logger) (*UserService, *NotificationService, string, int) {
	userSvc := &UserService{Logger: logger}
	notifSvc := &NotificationService{Logger: logger}
	return userSvc, notifSvc, "config-value", 42
}

// Test multi-return integration
func TestMultiReturnIntegration(t *testing.T) {
	t.Run("basic multi-return singleton", func(t *testing.T) {
		collection := godi.NewCollection()
		
		// Register multi-return constructor
		err := collection.AddSingleton(NewInfrastructure)
		require.NoError(t, err)
		
		// Build provider
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()
		
		// Resolve each type
		db, err := provider.Get(reflect.TypeOf((*Database)(nil)))
		require.NoError(t, err)
		assert.NotNil(t, db)
		assert.Equal(t, "postgres://localhost", db.(*Database).ConnectionString)
		
		cache, err := provider.Get(reflect.TypeOf((*Cache)(nil)))
		require.NoError(t, err)
		assert.NotNil(t, cache)
		assert.Equal(t, "redis", cache.(*Cache).Provider)
		
		logger, err := provider.Get(reflect.TypeOf((*Logger)(nil)))
		require.NoError(t, err)
		assert.NotNil(t, logger)
		assert.Equal(t, "info", logger.(*Logger).Level)
	})
	
	t.Run("multi-return with error", func(t *testing.T) {
		collection := godi.NewCollection()
		
		err := collection.AddSingleton(NewInfrastructureWithError)
		require.NoError(t, err)
		
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()
		
		// All non-error types should be resolvable
		db, err := provider.Get(reflect.TypeOf((*Database)(nil)))
		require.NoError(t, err)
		assert.NotNil(t, db)
		
		cache, err := provider.Get(reflect.TypeOf((*Cache)(nil)))
		require.NoError(t, err)
		assert.NotNil(t, cache)
		
		logger, err := provider.Get(reflect.TypeOf((*Logger)(nil)))
		require.NoError(t, err)
		assert.NotNil(t, logger)
	})
	
	t.Run("multi-return with dependencies", func(t *testing.T) {
		collection := godi.NewCollection()
		
		// Register infrastructure (multi-return)
		err := collection.AddSingleton(NewInfrastructure)
		require.NoError(t, err)
		
		// Register services that depend on infrastructure (also multi-return)
		err = collection.AddSingleton(NewServices)
		require.NoError(t, err)
		
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()
		
		// Resolve services
		userRepo, err := provider.Get(reflect.TypeOf((*UserRepository)(nil)))
		require.NoError(t, err)
		assert.NotNil(t, userRepo)
		repo := userRepo.(*UserRepository)
		assert.NotNil(t, repo.DB)
		assert.NotNil(t, repo.Cache)
		
		userSvc, err := provider.Get(reflect.TypeOf((*UserService)(nil)))
		require.NoError(t, err)
		assert.NotNil(t, userSvc)
		svc := userSvc.(*UserService)
		assert.Same(t, repo, svc.Repo)
		
		adminSvc, err := provider.Get(reflect.TypeOf((*AdminService)(nil)))
		require.NoError(t, err)
		assert.NotNil(t, adminSvc)
		admin := adminSvc.(*AdminService)
		assert.Same(t, svc, admin.UserService)
	})
	
	t.Run("multi-return scoped lifetime", func(t *testing.T) {
		collection := godi.NewCollection()
		
		err := collection.AddScoped(NewInfrastructure)
		require.NoError(t, err)
		
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()
		
		// Create two scopes
		scope1, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope1.Close()
		
		scope2, err := provider.CreateScope(context.Background())
		require.NoError(t, err)
		defer scope2.Close()
		
		// Get services from scope1
		db1, err := scope1.Get(reflect.TypeOf((*Database)(nil)))
		require.NoError(t, err)
		cache1, err := scope1.Get(reflect.TypeOf((*Cache)(nil)))
		require.NoError(t, err)
		logger1, err := scope1.Get(reflect.TypeOf((*Logger)(nil)))
		require.NoError(t, err)
		
		// Get services from scope2
		db2, err := scope2.Get(reflect.TypeOf((*Database)(nil)))
		require.NoError(t, err)
		cache2, err := scope2.Get(reflect.TypeOf((*Cache)(nil)))
		require.NoError(t, err)
		logger2, err := scope2.Get(reflect.TypeOf((*Logger)(nil)))
		require.NoError(t, err)
		
		// Services from different scopes should be different instances
		assert.NotSame(t, db1, db2)
		assert.NotSame(t, cache1, cache2)
		assert.NotSame(t, logger1, logger2)
		
		// But within same scope, multiple gets should return same instance
		db1Again, err := scope1.Get(reflect.TypeOf((*Database)(nil)))
		require.NoError(t, err)
		assert.Same(t, db1, db1Again)
	})
	
	t.Run("multi-return transient lifetime", func(t *testing.T) {
		invocations := 0
		trackingConstructor := func() (*Database, *Cache) {
			invocations++
			return &Database{ConnectionString: "tracked"},
				&Cache{Provider: "tracked"}
		}
		
		collection := godi.NewCollection()
		err := collection.AddTransient(trackingConstructor)
		require.NoError(t, err)
		
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()
		
		// Each Get should create new instances
		db1, err := provider.Get(reflect.TypeOf((*Database)(nil)))
		require.NoError(t, err)
		assert.NotNil(t, db1)
		
		db2, err := provider.Get(reflect.TypeOf((*Database)(nil)))
		require.NoError(t, err)
		assert.NotNil(t, db2)
		
		cache1, err := provider.Get(reflect.TypeOf((*Cache)(nil)))
		require.NoError(t, err)
		assert.NotNil(t, cache1)
		
		// For transient, each type resolution creates new instances
		// So we should have 3 invocations
		assert.Equal(t, 3, invocations)
		
		// Instances should be different
		assert.NotSame(t, db1, db2)
	})
	
	t.Run("multi-return with mixed types", func(t *testing.T) {
		collection := godi.NewCollection()
		
		// Register logger first
		err := collection.AddSingleton(func() *Logger {
			return &Logger{Level: "debug"}
		})
		require.NoError(t, err)
		
		// Register multi-return with various types
		err = collection.AddSingleton(NewMixedServices)
		require.NoError(t, err)
		
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()
		
		// Resolve services
		userSvc, err := provider.Get(reflect.TypeOf((*UserService)(nil)))
		require.NoError(t, err)
		assert.NotNil(t, userSvc)
		
		notifSvc, err := provider.Get(reflect.TypeOf((*NotificationService)(nil)))
		require.NoError(t, err)
		assert.NotNil(t, notifSvc)
		
		// String type
		strVal, err := provider.Get(reflect.TypeOf(""))
		require.NoError(t, err)
		assert.Equal(t, "config-value", strVal)
		
		// Int type
		intVal, err := provider.Get(reflect.TypeOf(0))
		require.NoError(t, err)
		assert.Equal(t, 42, intVal)
	})
	
	t.Run("multi-return with keyed services", func(t *testing.T) {
		collection := godi.NewCollection()
		
		// Register with Name option (applies to first return)
		err := collection.AddSingleton(NewInfrastructure, godi.Name("primary"))
		require.NoError(t, err)
		
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()
		
		// First type should be keyed
		db, err := provider.GetKeyed(reflect.TypeOf((*Database)(nil)), "primary")
		require.NoError(t, err)
		assert.NotNil(t, db)
		
		// Other types should not be keyed but still resolvable
		cache, err := provider.Get(reflect.TypeOf((*Cache)(nil)))
		require.NoError(t, err)
		assert.NotNil(t, cache)
		
		logger, err := provider.Get(reflect.TypeOf((*Logger)(nil)))
		require.NoError(t, err)
		assert.NotNil(t, logger)
	})
	
	t.Run("multi-return preserves relationships", func(t *testing.T) {
		collection := godi.NewCollection()
		
		// Constructor that returns related instances
		relatedConstructor := func() (*UserRepository, *UserService) {
			repo := &UserRepository{
				DB:    &Database{ConnectionString: "test"},
				Cache: &Cache{Provider: "test"},
			}
			svc := &UserService{
				Repo:   repo, // Same repo instance
				Logger: &Logger{Level: "test"},
			}
			return repo, svc
		}
		
		err := collection.AddSingleton(relatedConstructor)
		require.NoError(t, err)
		
		provider, err := collection.Build()
		require.NoError(t, err)
		defer provider.Close()
		
		// Get both services
		repo, err := provider.Get(reflect.TypeOf((*UserRepository)(nil)))
		require.NoError(t, err)
		
		svc, err := provider.Get(reflect.TypeOf((*UserService)(nil)))
		require.NoError(t, err)
		
		// They should share the same repository instance
		userSvc := svc.(*UserService)
		assert.Same(t, repo, userSvc.Repo)
	})
	
	t.Run("error in multi-return constructor", func(t *testing.T) {
		collection := godi.NewCollection()
		
		// Constructor that returns error
		failingConstructor := func() (*Database, *Cache, error) {
			return nil, nil, errors.New("connection failed")
		}
		
		err := collection.AddSingleton(failingConstructor)
		require.NoError(t, err)
		
		_, err = collection.Build()
		// Build should fail because singleton initialization fails
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to initialize singletons")
	})
}

// Benchmark multi-return performance
func BenchmarkMultiReturnResolution(b *testing.B) {
	collection := godi.NewCollection()
	_ = collection.AddSingleton(NewInfrastructure)
	
	provider, _ := collection.Build()
	defer provider.Close()
	
	dbType := reflect.TypeOf((*Database)(nil))
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = provider.Get(dbType)
	}
}

func BenchmarkMultiReturnVsSingle(b *testing.B) {
	b.Run("multi-return", func(b *testing.B) {
		collection := godi.NewCollection()
		_ = collection.AddSingleton(NewInfrastructure)
		provider, _ := collection.Build()
		defer provider.Close()
		
		types := []reflect.Type{
			reflect.TypeOf((*Database)(nil)),
			reflect.TypeOf((*Cache)(nil)),
			reflect.TypeOf((*Logger)(nil)),
		}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, t := range types {
				_, _ = provider.Get(t)
			}
		}
	})
	
	b.Run("single-return", func(b *testing.B) {
		collection := godi.NewCollection()
		_ = collection.AddSingleton(func() *Database { return &Database{} })
		_ = collection.AddSingleton(func() *Cache { return &Cache{} })
		_ = collection.AddSingleton(func() *Logger { return &Logger{} })
		provider, _ := collection.Build()
		defer provider.Close()
		
		types := []reflect.Type{
			reflect.TypeOf((*Database)(nil)),
			reflect.TypeOf((*Cache)(nil)),
			reflect.TypeOf((*Logger)(nil)),
		}
		
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			for _, t := range types {
				_, _ = provider.Get(t)
			}
		}
	})
}