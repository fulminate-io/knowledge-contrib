// SPDX-License-Identifier: Apache-2.0

package lokiapi

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework/frameworktest"
)

// TestMain installs the framework's goroutine-leak gate under an EMPTY
// allowlist. This package holds an HTTP transport with a connection pool and
// runs a paging loop under a context, so a request whose body is never closed
// or a retry timer that outlives its test leaks a goroutine — and neither fails
// the test that leaked it.
func TestMain(m *testing.M) { frameworktest.VerifyNoGoroutineLeaks(m) }
