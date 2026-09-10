// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework/frameworktest"
)

// TestMain installs the framework's goroutine-leak gate under an EMPTY
// allowlist. This package builds a real MCP server value and drives real HTTP
// requests, both of which hold goroutines a test can leave running.
func TestMain(m *testing.M) { frameworktest.VerifyNoGoroutineLeaks(m) }
