package godi_test

import (
	"fmt"
	"reflect"

	"github.com/junioryono/godi/v3"
)

// Example demonstrates using constructors with multiple return values
func ExampleCollection_multipleReturns() {
	// Define services
	type Database struct {
		ConnectionString string
	}
	
	type Cache struct {
		Provider string
	}
	
	type Logger struct {
		Level string
	}
	
	// Constructor that returns multiple services at once
	NewInfrastructure := func() (*Database, *Cache, *Logger) {
		return &Database{ConnectionString: "postgres://localhost"},
			&Cache{Provider: "redis"},
			&Logger{Level: "info"}
	}
	
	// Create collection and register the multi-return constructor
	collection := godi.NewCollection()
	_ = collection.AddSingleton(NewInfrastructure)
	
	// Build the provider
	provider, _ := collection.Build()
	defer provider.Close()
	
	// Each return type can be resolved independently
	db, _ := provider.Get(reflect.TypeOf((*Database)(nil)))
	cache, _ := provider.Get(reflect.TypeOf((*Cache)(nil)))
	logger, _ := provider.Get(reflect.TypeOf((*Logger)(nil)))
	
	fmt.Printf("Database: %s\n", db.(*Database).ConnectionString)
	fmt.Printf("Cache: %s\n", cache.(*Cache).Provider)
	fmt.Printf("Logger: %s\n", logger.(*Logger).Level)
	
	// Output:
	// Database: postgres://localhost
	// Cache: redis
	// Logger: info
}

// Example demonstrates multiple returns with error handling
func ExampleCollection_multipleReturnsWithError() {
	type UserService struct {
		Name string
	}
	
	type AdminService struct {
		Name string
	}
	
	// Constructor that returns multiple services and an error
	NewServices := func() (*UserService, *AdminService, error) {
		// In real code, this might connect to a database or external service
		return &UserService{Name: "user-service"},
			&AdminService{Name: "admin-service"},
			nil // No error
	}
	
	collection := godi.NewCollection()
	_ = collection.AddSingleton(NewServices)
	
	provider, _ := collection.Build()
	defer provider.Close()
	
	userSvc, _ := provider.Get(reflect.TypeOf((*UserService)(nil)))
	adminSvc, _ := provider.Get(reflect.TypeOf((*AdminService)(nil)))
	
	fmt.Printf("User Service: %s\n", userSvc.(*UserService).Name)
	fmt.Printf("Admin Service: %s\n", adminSvc.(*AdminService).Name)
	
	// Output:
	// User Service: user-service
	// Admin Service: admin-service
}

// Example demonstrates that multi-return constructors maintain relationships
func ExampleCollection_multipleReturnsRelationships() {
	type Repository struct {
		ID string
	}
	
	type Service struct {
		Repo *Repository
	}
	
	// Constructor returns related instances
	NewComponents := func() (*Repository, *Service) {
		repo := &Repository{ID: "shared-repo"}
		svc := &Service{Repo: repo} // Service uses the same repo instance
		return repo, svc
	}
	
	collection := godi.NewCollection()
	_ = collection.AddSingleton(NewComponents)
	
	provider, _ := collection.Build()
	defer provider.Close()
	
	repo, _ := provider.Get(reflect.TypeOf((*Repository)(nil)))
	svc, _ := provider.Get(reflect.TypeOf((*Service)(nil)))
	
	repoInstance := repo.(*Repository)
	svcInstance := svc.(*Service)
	
	// The service's repository is the same instance
	fmt.Printf("Repository ID: %s\n", repoInstance.ID)
	fmt.Printf("Service's Repository ID: %s\n", svcInstance.Repo.ID)
	fmt.Printf("Same instance: %v\n", repoInstance == svcInstance.Repo)
	
	// Output:
	// Repository ID: shared-repo
	// Service's Repository ID: shared-repo
	// Same instance: true
}