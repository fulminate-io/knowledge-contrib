// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework/frameworktest"
)

// leakguard_test.go — LEAK-a: the package-level goroutine-leak gate.
//
// THE PER-TEST SYMPTOM IS NOTHING AT ALL, which is why this has to be a
// package-level gate rather than an assertion someone remembers to write: a
// leaked goroutine does not fail the test that leaked it, it fails whatever
// runs after it, or nothing at all until the leak becomes a resource
// exhaustion in a long-lived collector process.
//
// THIS MODULE IS THE RUNNER CLASS, NOT THE LEAF CLASS. It holds client-go REST
// transports, an exec credential plugin that spawns a child process on the
// operator's behalf, and an MCP server loop over stdio. Every one of those
// outlives the call that started it if it is not closed.
//
// THE ALLOWLIST IS EMPTY, and it is the framework's shared gate that provides
// it. A collector needing an entry does not add one there — it writes its own
// TestMain naming the goroutine and saying why its lifetime legitimately
// exceeds the test that started it, so one collector's excuse is never spent on
// the others. This module needs no entry: nothing in its closure starts a
// goroutine from an init.

func TestMain(m *testing.M) {
	frameworktest.VerifyNoGoroutineLeaks(m)
}
