// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework/frameworktest"
)

// TestMain installs the framework's goroutine-leak gate under an EMPTY
// allowlist. This package compresses with zstd, whose encoder holds goroutines
// until it is closed, so a chunk path that forgot a Close is exactly what this
// catches — and it catches it in whatever test runs next rather than in the one
// that leaked, which is why it is a package gate and not an assertion.
func TestMain(m *testing.M) { frameworktest.VerifyNoGoroutineLeaks(m) }
