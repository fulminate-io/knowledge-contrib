// SPDX-License-Identifier: Apache-2.0

package main

// leakguard_test.go — the goroutine-leak gate for this module's root package.
//
// THE PER-TEST SYMPTOM OF A LEAK IS NOTHING AT ALL, which is why this is a
// package-level gate rather than an assertion someone remembers to write: a
// leaked goroutine does not fail the test that leaked it, it fails whatever
// runs after it, or nothing at all until the leak becomes resource exhaustion
// in a long-lived collector process.
//
// WHAT IT OBSERVES HERE. This collector holds one open apiserver stream per
// container per collect, and the stdio round trip spawns a real child process
// and a client session. A dropped reader or an undrained session shows up here.
//
// THE ALLOWLIST IS EMPTY, deliberately. An entry would be one goroutine this
// module declares it cannot account for, and it would need its own reason
// written beside it.
//
// THE GATE IS INSTALLED FROM stdio_test.go's TestMain rather than from a
// TestMain of its own, because a package may have only one and that file's
// re-exec switch has to run first: the child arm must serve MCP instead of
// running tests, and a leak check around a process that never runs tests would
// check nothing.
