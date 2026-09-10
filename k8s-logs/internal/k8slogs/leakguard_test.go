// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework/frameworktest"
)

// leakguard_test.go — the goroutine-leak gate for the package that actually
// holds the collector's long-lived resources.
//
// IT WAS IN THE WRONG PACKAGE. The gate was installed from package main, where
// the stdio round trip lives, while every apiserver connection this collector
// opens is opened here. The models it was copied from both install the gate in
// the package under test, and nothing checked that this one contained its
// subject.
//
// WHAT IT DOES AND DOES NOT OBSERVE, stated because the difference decides
// where the stream Close is tested. This gate sees GOROUTINES outstanding when
// the package's tests finish: a client-go transport that never shut down, an
// httptest server left serving, a reader started and never joined. It does NOT
// see the deferred Close on a log stream, and that is measured rather than
// assumed — with the Close deleted this gate still passes, because the success
// path reads the body to its end and the HTTP transport drains and recycles a
// fully-read body itself. The Close is observed instead through the stream
// seam, in TestTheStreamIsClosedOnBothPaths, which reds on the deleted defer
// directly and needs no goroutine inference.
//
// THE ALLOWLIST IS EMPTY, deliberately. An entry would be one goroutine this
// package declares it cannot account for, and it would need its own reason
// written beside it.
func TestMain(m *testing.M) {
	frameworktest.VerifyNoGoroutineLeaks(m)
}
