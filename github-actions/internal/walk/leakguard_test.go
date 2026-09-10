// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"os"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework/frameworktest"
)

// leakguard_test.go — the goroutine-leak gate for the one package in this module
// that starts goroutines, and the re-exec switch the stdio test needs.
//
// THE PER-TEST SYMPTOM OF A LEAK IS NOTHING AT ALL, which is why this is a
// package-level gate rather than an assertion someone remembers to write: a
// leaked worker does not fail the test that leaked it. It fails whatever runs
// after it, or nothing at all until a long-lived collector process exhausts
// something in production.
//
// THE ALLOWLIST IS EMPTY and stays that way unless a specific goroutine is named
// with the reason its lifetime legitimately exceeds the test that started it.
// This module's own fan-out has no such goroutine: it closes its work channel and
// waits, on every path including cancellation.
func TestMain(m *testing.M) {
	// THE CHILD ARM COMES FIRST AND NEVER RETURNS. A child re-exec'd as the
	// collector serves the protocol on its stdout and exits; running the suite or
	// the leak gate in it would write test output onto the protocol stream, which
	// is the exact corruption the transport rule exists to prevent.
	if os.Getenv(stubModeEnv) != "" {
		serveAsCollector()
		return
	}
	frameworktest.VerifyNoGoroutineLeaks(m)
}
