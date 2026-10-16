package huma_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	"go.uber.org/goleak"
)

// leakGuard is how long the leak check waits for the tests' goroutines to
// exit. goleak alone gives up after about 0.4 s, which a goroutine that is
// already returning can outlast on a loaded machine; a leaked goroutine is
// still blocked when the guard runs out, so it is a failure deadline, never a
// condition.
const leakGuard = time.Minute

// TestMain fails the package's tests if any goroutine is still running when
// they finish, catching leaks in the library and in the tests themselves.
func TestMain(m *testing.M) {
	code := m.Run()
	if code == 0 {
		if err := findLeaks(); err != nil {
			fmt.Fprintf(os.Stderr, "goleak: Errors on successful test run: %v\n", err)
			code = 1
		}
	}
	os.Exit(code)
}

// findLeaks is goleak.Find asked again until it finds no goroutine or
// leakGuard has passed.
func findLeaks(options ...goleak.Option) error {
	deadline := time.Now().Add(leakGuard)
	for {
		err := goleak.Find(options...)
		if err == nil || time.Now().After(deadline) {
			return err
		}
	}
}
