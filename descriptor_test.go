package godi_test

import (
	"reflect"
	"testing"

	"github.com/junioryono/godi/v2"
	"github.com/junioryono/godi/v2/internal/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServiceDescriptor_Validation(t *testing.T) {
	// Note: We can't directly create or access serviceDescriptor as it's internal
	// But we can test validation through the public API

	t.Run("valid descriptors through collection", func(t *testing.T) {
		t.Parallel()

		provider := godi.NewServiceProvider()

		// Valid singleton
		err := provider.AddSingleton(testutil.NewTestLogger)
		assert.NoError(t, err)

		// Valid scoped
		err = provider.AddScoped(testutil.NewTestService)
		assert.NoError(t, err)

		// Valid keyed service
		err = provider.AddSingleton(testutil.NewTestDatabase, godi.Name("primary"))
		assert.NoError(t, err)

		// Valid decorator
		err = provider.Decorate(func(logger testutil.TestLogger) testutil.TestLogger {
			return logger
		})
		assert.NoError(t, err)
	})

	t.Run("invalid constructors", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name        string
			constructor interface{}
			wantErr     bool
			errContains string
		}{
			{
				name:        "nil constructor",
				constructor: nil,
				wantErr:     true,
			},
			{
				name:        "non-function constructor",
				constructor: "not a function",
				wantErr:     false,
			},
			{
				name: "no return values",
				constructor: func() {
					// No return
				},
				wantErr: true,
			},
			{
				name: "too many return values",
				constructor: func() (string, int, error, bool) {
					return "", 0, nil, false
				},
				wantErr: true,
			},
			{
				name: "second return not error",
				constructor: func() (string, int) {
					return "", 0
				},
				wantErr: true,
			},
			{
				name: "valid single return",
				constructor: func() testutil.TestLogger {
					return testutil.NewTestLogger()
				},
				wantErr: false,
			},
			{
				name: "valid return with error",
				constructor: func() (testutil.TestLogger, error) {
					return testutil.NewTestLogger(), nil
				},
				wantErr: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				provider := godi.NewServiceProvider()
				err := provider.AddSingleton(tt.constructor)

				if tt.wantErr {
					assert.Error(t, err)
					if tt.errContains != "" {
						assert.Contains(t, err.Error(), tt.errContains)
					}
				} else {
					assert.NoError(t, err)
				}
			})
		}
	})

	t.Run("invalid decorators", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name      string
			decorator interface{}
			wantErr   bool
		}{
			{
				name:      "nil decorator",
				decorator: nil,
				wantErr:   true,
			},
			{
				name:      "non-function decorator",
				decorator: 42,
				wantErr:   true,
			},
			{
				name: "no parameters",
				decorator: func() testutil.TestLogger {
					return nil
				},
				wantErr: true,
			},
			{
				name: "no return value",
				decorator: func(logger testutil.TestLogger) {
					// No return
				},
				wantErr: true,
			},
			{
				name: "valid decorator",
				decorator: func(logger testutil.TestLogger) testutil.TestLogger {
					return logger
				},
				wantErr: false,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				provider := godi.NewServiceProvider()
				err := provider.Decorate(tt.decorator)

				if tt.wantErr {
					assert.Error(t, err)
				} else {
					assert.NoError(t, err)
				}
			})
		}
	})
}

func TestServiceDescriptor_ParameterObjects(t *testing.T) {
	t.Run("detects parameter objects", func(t *testing.T) {
		t.Parallel()

		// Constructor with parameter object
		constructor := func(params testutil.TestServiceParams) *testutil.TestService {
			return &testutil.TestService{ID: "from-params"}
		}

		provider := godi.NewServiceProvider()
		err := provider.AddSingleton(constructor)
		assert.NoError(t, err)
	})

	t.Run("multiple parameter objects not allowed", func(t *testing.T) {
		t.Parallel()

		type Params1 struct {
			godi.In
			Logger testutil.TestLogger
		}

		type Params2 struct {
			godi.In
			Database testutil.TestDatabase
		}

		// Constructor with multiple In parameters
		constructor := func(p1 Params1, p2 Params2) *testutil.TestService {
			return &testutil.TestService{}
		}

		provider := godi.NewServiceProvider()
		err := provider.AddSingleton(constructor)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "multiple In")
	})
}

func TestServiceDescriptor_ResultObjects(t *testing.T) {
	t.Run("result object constructors handled specially", func(t *testing.T) {
		t.Parallel()

		// Constructor returning result object
		constructor := func() testutil.TestServiceResult {
			return testutil.TestServiceResult{
				Service:  testutil.NewTestService(),
				Logger:   testutil.NewTestLogger(),
				Database: testutil.NewTestDatabase(),
			}
		}

		provider := godi.NewServiceProvider()
		err := provider.AddSingleton(constructor)

		// Should succeed - result objects are handled specially
		assert.NoError(t, err)
	})

	t.Run("result object with error", func(t *testing.T) {
		t.Parallel()

		// Constructor returning result object and error
		constructor := func() (testutil.TestServiceResult, error) {
			return testutil.TestServiceResult{
				Service:  testutil.NewTestService(),
				Logger:   testutil.NewTestLogger(),
				Database: testutil.NewTestDatabase(),
			}, nil
		}

		provider := godi.NewServiceProvider()
		err := provider.AddSingleton(constructor)
		assert.NoError(t, err)
	})

	t.Run("result object with too many returns", func(t *testing.T) {
		t.Parallel()

		// Invalid: too many returns
		constructor := func() (testutil.TestServiceResult, error, bool) {
			return testutil.TestServiceResult{}, nil, false
		}

		provider := godi.NewServiceProvider()
		err := provider.AddSingleton(constructor)
		assert.Error(t, err)
	})
}

func TestServiceDescriptor_ServiceTypes(t *testing.T) {
	t.Run("various service types", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			name        string
			constructor interface{}
			lifetime    godi.ServiceLifetime
			expectType  reflect.Type
		}{
			{
				name: "interface type",
				constructor: func() testutil.TestLogger {
					return testutil.NewTestLogger()
				},
				lifetime:   godi.Singleton,
				expectType: reflect.TypeOf((*testutil.TestLogger)(nil)).Elem(),
			},
			{
				name: "pointer type",
				constructor: func() *testutil.TestService {
					return testutil.NewTestService()
				},
				lifetime:   godi.Scoped,
				expectType: reflect.TypeOf((*testutil.TestService)(nil)),
			},
			{
				name: "value type",
				constructor: func() testutil.TestService {
					return testutil.TestService{ID: "test"}
				},
				lifetime:   godi.Singleton,
				expectType: reflect.TypeOf(testutil.TestService{}),
			},
			{
				name: "slice type",
				constructor: func() []string {
					return []string{"a", "b", "c"}
				},
				lifetime:   godi.Singleton,
				expectType: reflect.TypeOf([]string{}),
			},
			{
				name: "map type",
				constructor: func() map[string]int {
					return map[string]int{"a": 1}
				},
				lifetime:   godi.Singleton,
				expectType: reflect.TypeOf(map[string]int{}),
			},
			{
				name: "channel type",
				constructor: func() chan int {
					return make(chan int)
				},
				lifetime:   godi.Singleton,
				expectType: reflect.TypeOf(make(chan int)),
			},
			{
				name: "function type",
				constructor: func() func() string {
					return func() string { return "test" }
				},
				lifetime:   godi.Singleton,
				expectType: reflect.TypeOf(func() string { return "" }),
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				provider := godi.NewServiceProvider()

				var err error
				switch tt.lifetime {
				case godi.Singleton:
					err = provider.AddSingleton(tt.constructor)
				case godi.Scoped:
					err = provider.AddScoped(tt.constructor)
				}

				require.NoError(t, err)

				// Verify the service is registered with correct type
				assert.True(t, provider.IsService(tt.expectType))
			})
		}
	})
}

func TestServiceDescriptor_Metadata(t *testing.T) {
	t.Run("keyed services have metadata", func(t *testing.T) {
		t.Parallel()

		provider := godi.NewServiceProvider()

		// Add keyed service
		err := provider.AddSingleton(testutil.NewTestLogger, godi.Name("primary"))
		require.NoError(t, err)

		// The descriptor should have the key
		loggerType := reflect.TypeOf((*testutil.TestLogger)(nil)).Elem()
		assert.True(t, provider.IsKeyedService(loggerType, "primary"))
	})

	t.Run("group services have metadata", func(t *testing.T) {
		t.Parallel()

		provider := godi.NewServiceProvider()

		// Add to group
		err := provider.AddSingleton(
			func() testutil.TestHandler { return testutil.NewTestHandler("h1") },
			godi.Group("handlers"),
		)
		require.NoError(t, err)

		t.Cleanup(func() {
			require.NoError(t, provider.Close())
		})

		handlers, err := godi.ResolveGroup[testutil.TestHandler](provider, "handlers")
		require.NoError(t, err)
		assert.Len(t, handlers, 1)
	})
}

// Table-driven tests for descriptor edge cases
func TestServiceDescriptor_EdgeCases(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(t *testing.T) godi.ServiceProvider
		action  func(provider godi.ServiceProvider) error
		wantErr bool
		check   func(t *testing.T, provider godi.ServiceProvider)
	}{
		{
			name: "empty struct as service",
			setup: func(t *testing.T) godi.ServiceProvider {
				return godi.NewServiceProvider()
			},
			action: func(provider godi.ServiceProvider) error {
				return provider.AddSingleton(func() struct{} { return struct{}{} })
			},
			wantErr: false,
		},
		{
			name: "nil interface return",
			setup: func(t *testing.T) godi.ServiceProvider {
				return godi.NewServiceProvider()
			},
			action: func(provider godi.ServiceProvider) error {
				return provider.AddSingleton(func() testutil.TestLogger { return nil })
			},
			wantErr: false,
		},
		{
			name: "error-only return",
			setup: func(t *testing.T) godi.ServiceProvider {
				return godi.NewServiceProvider()
			},
			action: func(provider godi.ServiceProvider) error {
				return provider.AddSingleton(func() error { return nil })
			},
			wantErr: true,
		},
		{
			name: "complex nested types",
			setup: func(t *testing.T) godi.ServiceProvider {
				return godi.NewServiceProvider()
			},
			action: func(provider godi.ServiceProvider) error {
				type NestedService struct {
					Data map[string][]chan<- func() error
				}
				return provider.AddSingleton(func() *NestedService {
					return &NestedService{
						Data: make(map[string][]chan<- func() error),
					}
				})
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			collection := tt.setup(t)
			err := tt.action(collection)

			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}

			if tt.check != nil {
				tt.check(t, collection)
			}
		})
	}
}
