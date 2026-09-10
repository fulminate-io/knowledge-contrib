// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/framework/frameworktest"
)

// main_test.go — the goroutine-leak gate, and the switch that turns this test
// binary into a real stdio collector.
//
// THE STDIO PROVIDER IS THIS TEST BINARY, RE-EXECED. The thing under test on
// that arm is a real child process speaking JSON-RPC over its own pipes, and a
// child that is this binary needs no compile step and no module scaffolding in a
// temp directory. TestMain checks one switch variable and, when it is set,
// serves a collector through the framework's shipped ServeStdio entry point
// instead of running tests — so the arm exercises the entry point the binary
// uses rather than an approximation of it.
//
// THE SPAWNED CHILD READS NO CLOUD. Its entry reader is the recorded one below,
// so the arm proves the served surface (the advertised schemas, the params
// names, the envelope) without a credential and without a network.

// spawnModeEnv switches this binary from "run tests" to "be a collector".
const spawnModeEnv = "KN_STACKDRIVER_SPAWN_MODE"

// The two shapes the spawned child can serve.
const (
	// spawnRecorded serves a walk over the recorded entries.
	spawnRecorded = "recorded"
	// spawnEmpty serves a walk that reads nothing, which is the shape that
	// exercises the empty-result normalization end to end.
	spawnEmpty = "empty"
)

// TestMain runs this package's tests under an EMPTY goleak allowlist, inherited
// from the framework so this module takes the same gate by one line.
//
// THIS PACKAGE IS ON THE GUARDED SIDE BY WHAT IT DOES rather than by where it
// sits: it runs an MCP server over stdio through the SDK and, in production,
// holds a gRPC Cloud Logging client, and both own goroutines that outlive a
// test's return. An entry added to the allowlist later must name the goroutine
// and say why its lifetime legitimately exceeds the test that started it.
func TestMain(m *testing.M) {
	if mode := os.Getenv(spawnModeEnv); mode != "" {
		serveSpawnedCollector(mode)
		return
	}
	frameworktest.VerifyNoGoroutineLeaks(m)
}

// serveSpawnedCollector runs a collector on the real stdio entry point and never
// returns. Its one diagnostic goes to stderr, because stdout is the protocol.
func serveSpawnedCollector(mode string) {
	c := &Collector{config: defaultPipelineConfig()}
	switch mode {
	case spawnEmpty:
		c.read = readerReturning(drainResult{})
	default:
		c.read = readerReturning(drainResult{Entries: recordedEntries()})
	}
	if err := framework.ServeStdio(context.Background(), c); err != nil {
		fmt.Fprintf(os.Stderr, "spawned stackdriver collector: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}
