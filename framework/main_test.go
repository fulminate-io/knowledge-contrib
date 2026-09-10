// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fulminate-io/knowledge-contrib/framework/frameworktest"
)

// main_test.go — the goroutine-leak gate, and the switch that turns this test
// binary into a real stdio collector.
//
// THE STDIO PROVIDER IS THIS TEST BINARY, RE-EXECED, for the same reason the
// client's own host tests re-exec theirs: the thing under test on the stdio arm
// is a real child process speaking JSON-RPC over its own pipes, and a child that
// is this binary needs no compile step and no module scaffolding in a temp
// directory. TestMain checks one switch variable and, when it is set, serves the
// fixture collector through the framework's own ServeStdio entry point instead
// of running tests — so the stdio arm exercises the shipped entry point rather
// than an approximation of it.
//
// THE PROVIDER WRITES NOTHING TO STDOUT BUT JSON-RPC: stdout is the protocol
// stream, and a stray print corrupts the framing into an opaque handshake
// failure. The one diagnostic below goes to stderr.

const (
	// fixtureModeEnv switches this binary from "run tests" to "be a collector".
	fixtureModeEnv = "KN_FRAMEWORK_FIXTURE_MODE"
	// fixtureToolEnv names the tool the child serves; empty means the default.
	fixtureToolEnv = "KN_FRAMEWORK_FIXTURE_TOOL"
	// fixtureExtraToolEnv makes the child serve a THIRD tool beside the two the
	// framework serves. It is the mutation that proves this suite selects tools
	// by name rather than by counting them: with it set, every row must still
	// pass. A suite that had merely swapped one count for another would go red
	// here, which is the shape a later ticket's own tests must not inherit.
	fixtureExtraToolEnv = "KN_FRAMEWORK_FIXTURE_EXTRA_TOOL"
	// fixtureExtraToolName is that third tool's name.
	fixtureExtraToolName = "unrelated_third_tool"
)

// TestMain runs this package's tests under an EMPTY goleak allowlist, inherited
// from frameworktest so a collector module takes the same gate by one line.
//
// An entry added to that allowlist later must name the goroutine and say why its
// lifetime legitimately exceeds the test that started it; this package needs
// none, because every HTTP test drains the server's sessions.
func TestMain(m *testing.M) {
	if mode := os.Getenv(fixtureModeEnv); mode != "" {
		serveFixtureOverStdio(fixtureMode(mode), os.Getenv(fixtureToolEnv))
		return
	}
	frameworktest.VerifyNoGoroutineLeaks(m)
}

// serveFixtureOverStdio runs the fixture collector on the real stdio entry point
// and never returns.
func serveFixtureOverStdio(mode fixtureMode, tool string) {
	collector := &fixtureCollector{mode: mode, name: tool}
	var err error
	if os.Getenv(fixtureExtraToolEnv) != "" {
		err = serveWithAThirdTool(collector)
	} else {
		err = ServeStdio(context.Background(), collector)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "fixture collector: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// serveWithAThirdTool serves the fixture with an EXTRA tool installed beside the
// two the framework serves, over the same stdio transport ServeStdio uses.
//
// It goes through NewServer and the SDK's generic mcp.AddTool, which is the same
// installer the framework itself uses — the raw method form is what the escape
// census refuses, and using it here would red that census correctly.
func serveWithAThirdTool(c *fixtureCollector) error {
	srv, err := NewServer[fixtureParams](c)
	if err != nil {
		return err
	}
	type thirdIn struct {
		Anything string `json:"anything,omitempty"`
	}
	type thirdOut struct {
		Echo string `json:"echo"`
	}
	mcp.AddTool(srv, &mcp.Tool{Name: fixtureExtraToolName, Description: "an unrelated tool a host also serves"},
		func(_ context.Context, _ *mcp.CallToolRequest, in thirdIn) (*mcp.CallToolResult, thirdOut, error) {
			return nil, thirdOut{Echo: in.Anything}, nil
		})
	return srv.Run(context.Background(), &mcp.StdioTransport{})
}
