package huma_test

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package's tests if any goroutine is still running when
// they finish, catching leaks in the library and in the tests themselves.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
