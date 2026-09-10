// SPDX-License-Identifier: Apache-2.0

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// binary_test.go — THE SHIPPED BINARY, BUILT AND SPAWNED.
//
// WHY THE OTHER SPAWN TESTS DO NOT COVER THIS. Every other arm re-execs the TEST
// binary, whose TestMain switches on an environment variable and calls
// framework.ServeStdio itself. That exercises the collector and the framework
// entry point and never runs main(). Until the landing rebase the module root's
// main.go was the scaffold's refusal — it printed that the walk was not
// implemented and exited non-zero — and the rebase replaced it with the serving
// path every sibling module has. No run observed that change: a binary that
// still refused would have passed the whole suite.
//
// WHAT THIS ARM UNIQUELY REACHES is main() itself: the signal wiring that gives
// the walk a cancellable context, the call into the framework's stdio entry
// point, and the process staying up to serve rather than printing and exiting.
//
// IT BUILDS THE ARTIFACT THE CONFIGURATION ENTRY NAMES. `go build -o cloudwatch .`
// is the command the README documents and the entry's `command` field is pinned
// against, so this test fails if the module's binary stops being buildable that
// way as surely as if it stopped serving.

// TestTheBuiltBinaryServesTheCollectTool builds the module and speaks MCP to the
// artifact.
func TestTheBuiltBinaryServesTheCollectTool(t *testing.T) {
	binary := buildCollectorBinary(t)

	cmd := exec.Command(binary)
	// The child's environment is set WHOLE and carries nothing, which is both
	// the contract's own shape and what keeps this arm from reaching a real
	// credential: the walk is never called here, only the handshake and the
	// tool listing.
	cmd.Env = []string{}
	cmd.Dir = t.TempDir()
	cmd.Stderr = os.Stderr

	client := mcp.NewClient(&mcp.Implementation{Name: "cloudwatch-suite", Version: "v1"}, nil)
	session, err := client.Connect(t.Context(), &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatalf("the built binary did not complete an MCP handshake: %v; a binary that refuses at "+
			"startup fails here, which is the transition this arm exists to observe", err)
	}
	t.Cleanup(func() { _ = session.Close() })

	list, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("listing the built binary's tools: %v", err)
	}
	_ = toolByName(t, list.Tools, toolName)
	// The advertised input is the contract's, which is what a collect is
	// validated against before it is sent.
	if list.Tools[0].InputSchema == nil {
		t.Error("the built binary advertises no input schema")
	}
}

// buildCollectorBinary builds this module exactly as the README documents and
// returns the artifact's path.
//
// THE BUILD IS PART OF THE ASSERTION. A module whose binary does not build is a
// module that ships nothing, and no other test in this package compiles the root
// package as a COMMAND — `go test` builds it as a test binary, which links a
// different main.
func buildCollectorBinary(t *testing.T) string {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "cloudwatch")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build -o cloudwatch . failed: %v\n%s", err, out)
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("the build reported success and produced no artifact: %v", err)
	}
	return binary
}
