package testutil

import (
	"context"
	"reflect"
	"testing"

	"github.com/junioryono/godi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// AssertServiceResolvable checks if a service can be resolved
func AssertServiceResolvable[T any](t *testing.T, provider godi.ServiceProvider) T {
	t.Helper()
	service, err := godi.Resolve[T](provider)
	require.NoError(t, err, "failed to resolve service of type %T", *new(T))
	require.NotNil(t, service, "resolved service is nil")
	return service
}

// AssertServiceResolvableInScope checks if a service can be resolved in a scope
func AssertServiceResolvableInScope[T any](t *testing.T, scope godi.Scope) T {
	t.Helper()
	service, err := godi.Resolve[T](scope)
	require.NoError(t, err, "failed to resolve service of type %T in scope", *new(T))
	require.NotNil(t, service, "resolved service is nil")
	return service
}

// AssertKeyedServiceResolvable checks if a keyed service can be resolved
func AssertKeyedServiceResolvable[T any](t *testing.T, provider godi.ServiceProvider, key interface{}) T {
	t.Helper()
	service, err := godi.ResolveKeyed[T](provider, key)
	require.NoError(t, err, "failed to resolve keyed service of type %T with key %v", *new(T), key)
	require.NotNil(t, service, "resolved keyed service is nil")
	return service
}

// AssertGroupServiceResolvable checks if a group service can be resolved
func AssertGroupServiceResolvable[T any](t *testing.T, provider godi.ServiceProvider, group string) []T {
	t.Helper()
	service, err := godi.ResolveGroup[T](provider, group)
	require.NoError(t, err, "failed to resolve group service of type %T with group %s", *new(T), group)
	require.NotNil(t, service, "resolved group service is nil")
	require.NotEmpty(t, service, "resolved group service is empty")
	return service
}

// AssertServiceNotFound checks if a service resolution fails with not found error
func AssertServiceNotFound[T any](t *testing.T, provider godi.ServiceProvider) {
	t.Helper()
	_, err := godi.Resolve[T](provider)
	assert.Error(t, err)
	assert.True(t, godi.IsNotFound(err), "expected service not found error, got: %v", err)
}

// AssertKeyedServiceNotFound checks if a keyed service resolution fails with not found error
func AssertKeyedServiceNotFound[T any](t *testing.T, provider godi.ServiceProvider, key interface{}) {
	t.Helper()
	_, err := godi.ResolveKeyed[T](provider, key)
	assert.Error(t, err)
	assert.True(t, godi.IsNotFound(err), "expected keyed service not found error, got: %v", err)
}

// AssertPanicsWithError checks if a function panics with specific error
func AssertPanicsWithError(t *testing.T, expectedError error, f func(), msgAndArgs ...interface{}) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			assert.Fail(t, "function did not panic", msgAndArgs...)
			return
		}

		err, ok := r.(error)
		if !ok {
			assert.Fail(t, "panic value is not an error: %v", r)
			return
		}

		assert.ErrorIs(t, err, expectedError, msgAndArgs...)
	}()
	f()
}

// AssertPanics checks if a function panics
func AssertPanics(t *testing.T, f func(), msgAndArgs ...interface{}) {
	t.Helper()
	assert.Panics(t, f, msgAndArgs...)
}

// AssertSameInstance verifies two services are the same instance
func AssertSameInstance(t *testing.T, expected, actual interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	assert.Same(t, expected, actual, msgAndArgs...)
}

// AssertDifferentInstances verifies two services are different instances
func AssertDifferentInstances(t *testing.T, first, second interface{}, msgAndArgs ...interface{}) {
	t.Helper()
	assert.NotSame(t, first, second, msgAndArgs...)
}

// AssertImplements checks if a type implements an interface
func AssertImplements(t *testing.T, interfaceType, implementation interface{}) {
	t.Helper()
	interfaceTypeReflect := reflect.TypeOf(interfaceType).Elem()
	assert.Implements(t, interfaceType, implementation,
		"%T does not implement %v", implementation, interfaceTypeReflect)
}

// AssertProviderDisposed checks if operations on a disposed provider fail correctly
func AssertProviderDisposed(t *testing.T, provider godi.ServiceProvider) {
	t.Helper()
	assert.True(t, provider.IsDisposed(), "provider should be disposed")

	// Test that operations fail
	_, err := provider.Resolve(reflect.TypeOf((*interface{})(nil)).Elem())
	assert.ErrorIs(t, err, godi.ErrProviderDisposed)

	_, err = provider.ResolveKeyed(reflect.TypeOf((*interface{})(nil)).Elem(), "key")
	assert.ErrorIs(t, err, godi.ErrProviderDisposed)

	assert.Panics(t, func() {
		provider.CreateScope(context.Background())
	}, "CreateScope should panic when provider is disposed")
}

// AssertScopeDisposed checks if operations on a disposed scope fail correctly
func AssertScopeDisposed(t *testing.T, scope godi.Scope) {
	t.Helper()

	// Most operations should panic when scope is disposed
	assert.Panics(t, func() {
		scope.ID()
	}, "ID() should panic when scope is disposed")

	assert.Panics(t, func() {
		scope.Context()
	}, "Context() should panic when scope is disposed")

	assert.Panics(t, func() {
		scope.IsRootScope()
	}, "IsRootScope() should panic when scope is disposed")

	assert.Panics(t, func() {
		scope.CreateScope(context.Background())
	}, "CreateScope should panic when scope is disposed")
}

// AssertErrorType checks if an error is of a specific type
func AssertErrorType[T error](t *testing.T, err error, msgAndArgs ...interface{}) T {
	t.Helper()
	var target T
	assert.ErrorAs(t, err, &target, msgAndArgs...)
	return target
}

// AssertCircularDependency checks if an error is a circular dependency error
func AssertCircularDependency(t *testing.T, err error) {
	t.Helper()
	assert.Error(t, err)
	assert.True(t, godi.IsCircularDependency(err), "expected circular dependency error, got: %v", err)
}

// AssertTimeout checks if an error is a timeout error
func AssertTimeout(t *testing.T, err error) {
	t.Helper()
	assert.Error(t, err)
	assert.True(t, godi.IsTimeout(err), "expected timeout error, got: %v", err)
}

// AssertDisposed checks if an error indicates a disposed resource
func AssertDisposed(t *testing.T, err error) {
	t.Helper()
	assert.Error(t, err)
	assert.True(t, godi.IsDisposed(err), "expected disposed error, got: %v", err)
}

// RequireNoError is a helper that uses require.NoError
func RequireNoError(t *testing.T, err error, msgAndArgs ...interface{}) {
	t.Helper()
	require.NoError(t, err, msgAndArgs...)
}

// RequireError is a helper that uses require.Error
func RequireError(t *testing.T, err error, msgAndArgs ...interface{}) {
	t.Helper()
	require.Error(t, err, msgAndArgs...)
}
