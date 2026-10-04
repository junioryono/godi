package integrationtests

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain fails the package's tests if any goroutine is still running when
// they finish, catching leaks in the library and in the tests themselves.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// fasthttp (under Fiber) starts a process-wide goroutine, once, that
		// refreshes its cached Date header every second; it cannot be stopped.
		goleak.IgnoreAnyFunction("github.com/valyala/fasthttp.updateServerDate.func1"),
	)
}
