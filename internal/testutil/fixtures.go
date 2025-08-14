package testutil

import (
	"testing"

	"github.com/junioryono/godi/v3"
	"github.com/stretchr/testify/assert"
)

// ServiceFixture represents a test fixture for services
type ServiceFixture struct {
	Name         string
	Constructor  interface{}
	Lifetime     godi.ServiceLifetime
	Options      []godi.ProvideOption
	Dependencies []string
}

// CommonFixtures provides common service configurations for testing
var CommonFixtures = struct {
	Logger       ServiceFixture
	Database     ServiceFixture
	Cache        ServiceFixture
	Service      ServiceFixture
	KeyedService func(key string) ServiceFixture
	GroupService func(group string) ServiceFixture
}{
	Logger: ServiceFixture{
		Name:        "Logger",
		Constructor: NewTestLogger,
		Lifetime:    godi.Singleton,
	},
	Database: ServiceFixture{
		Name:        "Database",
		Constructor: NewTestDatabase,
		Lifetime:    godi.Singleton,
	},
	Cache: ServiceFixture{
		Name:        "Cache",
		Constructor: NewTestCache,
		Lifetime:    godi.Singleton,
	},
	Service: ServiceFixture{
		Name:         "Service",
		Constructor:  NewTestServiceWithDeps,
		Lifetime:     godi.Scoped,
		Dependencies: []string{"Logger", "Database", "Cache"},
	},
	KeyedService: func(key string) ServiceFixture {
		return ServiceFixture{
			Name:        "KeyedService",
			Constructor: NewTestService,
			Lifetime:    godi.Singleton,
			Options:     []godi.ProvideOption{godi.Name(key)},
		}
	},
	GroupService: func(group string) ServiceFixture {
		return ServiceFixture{
			Name:        "GroupService",
			Constructor: func() TestHandler { return NewTestHandler("handler") },
			Lifetime:    godi.Singleton,
			Options:     []godi.ProvideOption{godi.Group(group)},
		}
	},
}

// BuildFixture adds a fixture to a service collection
func BuildFixture(t *testing.T, collection godi.ServiceCollection, fixture ServiceFixture) {
	t.Helper()

	var err error
	switch fixture.Lifetime {
	case godi.Singleton:
		err = collection.AddSingleton(fixture.Constructor, fixture.Options...)
	case godi.Scoped:
		err = collection.AddScoped(fixture.Constructor, fixture.Options...)
	default:
		t.Fatalf("unknown lifetime: %v", fixture.Lifetime)
	}

	if err != nil {
		t.Fatalf("failed to add %s: %v", fixture.Name, err)
	}
}

// SetupBasicServices adds common test services to a collection
func SetupBasicServices(t *testing.T, collection godi.ServiceCollection) {
	t.Helper()

	BuildFixture(t, collection, CommonFixtures.Logger)
	BuildFixture(t, collection, CommonFixtures.Database)
	BuildFixture(t, collection, CommonFixtures.Cache)
}

// SetupCompleteServices adds all common services including dependent ones
func SetupCompleteServices(t *testing.T, collection godi.ServiceCollection) {
	t.Helper()

	SetupBasicServices(t, collection)
	BuildFixture(t, collection, CommonFixtures.Service)
}

// CreateProviderWithBasicServices creates a provider with basic test services
func CreateProviderWithBasicServices(t *testing.T) godi.ServiceProvider {
	t.Helper()

	builder := NewServiceCollectionBuilder(t)
	SetupBasicServices(t, builder.Build())
	return builder.BuildProvider()
}

// CreateProviderWithCompleteServices creates a provider with all test services
func CreateProviderWithCompleteServices(t *testing.T) godi.ServiceProvider {
	t.Helper()

	builder := NewServiceCollectionBuilder(t)
	SetupCompleteServices(t, builder.Build())
	return builder.BuildProvider()
}

// TestScenario represents a test scenario configuration
type TestScenario struct {
	Name     string
	Setup    func(t *testing.T) godi.ServiceProvider
	Validate func(t *testing.T, provider godi.ServiceProvider)
	WantErr  bool
}

// RunTestScenarios executes a set of test scenarios
func RunTestScenarios(t *testing.T, scenarios []TestScenario) {
	t.Helper()

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			t.Parallel()

			provider := scenario.Setup(t)
			scenario.Validate(t, provider)
		})
	}
}

// ErrorTestCase represents a test case for error scenarios
type ErrorTestCase struct {
	Name      string
	Setup     func(t *testing.T) godi.ServiceProvider
	Action    func(provider godi.ServiceProvider) error
	WantError error
	CheckErr  func(t *testing.T, err error)
}

// RunErrorTestCases executes error test cases
func RunErrorTestCases(t *testing.T, cases []ErrorTestCase) {
	t.Helper()

	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()

			provider := tc.Setup(t)
			err := tc.Action(provider)

			if tc.WantError != nil {
				RequireError(t, err)
				assert.ErrorIs(t, err, tc.WantError)
			}

			if tc.CheckErr != nil {
				tc.CheckErr(t, err)
			}
		})
	}
}
