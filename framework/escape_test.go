// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// escape_test.go — the NEGATIVE arms, served through the framework-internal raw
// escape, over in-memory transports.
//
// WHY THEY NEED THE ESCAPE. The ordinary path installs the SDK's generic
// AddTool, which validates the call's arguments against the advertised input
// schema before the handler runs and validates the handler's output against the
// advertised output schema before the result leaves. Those are requirements, and
// a requirement nothing can violate is a requirement nothing measures. The two
// tests below serve the same tool definition with the raw handler, which
// validates neither, and show what that costs — which is the whole reason the
// framework never exposes it.

// dialRaw serves a collector over in-memory transports with a raw handler.
func dialRaw(t *testing.T, c *fixtureCollector, h mcp.ToolHandler) *mcp.ClientSession {
	t.Helper()
	srv, err := newRawServer[fixtureParams](c, h)
	if err != nil {
		t.Fatalf("building the raw server: %v", err)
	}
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := srv.Connect(t.Context(), serverTransport, nil)
	if err != nil {
		t.Fatalf("connecting the raw server: %v", err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "framework-suite", Version: "v1"}, nil)
	session, err := client.Connect(t.Context(), clientTransport, nil)
	if err != nil {
		t.Fatalf("connecting the client: %v", err)
	}
	t.Cleanup(func() {
		_ = session.Close()
		_ = serverSession.Wait()
	})
	return session
}

// TestRawEscapeRunsTheWalkOnInvalidParams is row 11, and it is the pre-change
// arm that makes "params are validated before the walk" a requirement rather
// than an accident: on the RAW form the same call the generic form refuses runs
// the walk and returns a successful result.
//
// The control is in the same test: the identical arguments against the ordinary
// generic-form server are refused, and its walk never runs.
func TestRawEscapeRunsTheWalkOnInvalidParams(t *testing.T) {
	invalid := map[string]any{"id": "inst-7", "params": map[string]any{"region": 7}}

	raw := &fixtureCollector{mode: fixtureConforming}
	session := dialRaw(t, raw, rawWalkHandler(raw))
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: DefaultToolName, Arguments: invalid})
	if err != nil {
		t.Fatalf("the raw call failed at the protocol level: %v", err)
	}
	if res.IsError {
		t.Fatalf("the RAW form refused the call; it performs no input validation, so this row's premise is gone: %s",
			resultText(res))
	}
	if raw.callCount() != 1 {
		t.Errorf("the raw form ran the walk %d times, want 1 — it validates nothing, which is the point of this row",
			raw.callCount())
	}

	// SAME PARAMS, SAME INSTRUMENT, the ordinary server: refused, walk unrun.
	generic := dial(t, overHTTP, fixtureConforming, "")
	genericRes, err := generic.call(t, invalid)
	if err != nil {
		t.Fatalf("the generic call failed at the protocol level: %v", err)
	}
	if !genericRes.IsError {
		t.Errorf("the generic form ACCEPTED params its own advertised schema refuses")
	}
	if generic.fixture.callCount() != 0 {
		t.Errorf("the generic form ran the walk %d times on invalid params, want 0", generic.fixture.callCount())
	}
}

// TestRawEscapeCanBreakItsOwnWord is row 13's second half: a provider serving
// the raw form returns a payload its OWN advertised output schema forbids and
// nothing stops it, because neither the transport nor the client validates a
// provider's output against what the provider advertised.
//
// That is what the generic form buys and what a collector must never be able to
// opt out of: this arm is only reachable from inside this module.
func TestRawEscapeCanBreakItsOwnWord(t *testing.T) {
	broken := map[string]any{
		// The advertised output schema requires id AND type on every node.
		"nodes":         []any{map[string]any{"id": "n1"}},
		"edges":         []any{},
		"walk_complete": true,
	}
	session := dialRaw(t, &fixtureCollector{mode: fixtureConforming},
		func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return &mcp.CallToolResult{StructuredContent: broken}, nil
		})

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      DefaultToolName,
		Arguments: map[string]any{"id": "inst-7", "params": map[string]any{"region": "us-east-1"}},
	})
	if err != nil {
		t.Fatalf("the raw call failed at the protocol level: %v", err)
	}
	if res.IsError {
		t.Fatalf("the raw form refused its own broken output; it validates nothing, so this row's premise is gone: %s",
			resultText(res))
	}
	doc := envelopeOf(t, res)
	nodes, _ := doc["nodes"].([]any)
	if len(nodes) != 1 {
		t.Fatalf("the broken payload did not survive: %s", mustJSON(t, doc))
	}
	node, _ := nodes[0].(map[string]any)
	if _, hasType := node["type"]; hasType {
		t.Errorf("the payload was repaired somewhere; this row needs it to arrive as the provider wrote it: %s",
			mustJSON(t, doc))
	}
}

// rawWalkHandler is the raw counterpart of the framework's own handler: it
// decodes the arguments and calls the walk WITHOUT validating them against
// anything, which is precisely what the SDK's raw ToolHandler leaves to its
// caller.
func rawWalkHandler(c *fixtureCollector) mcp.ToolHandler {
	return func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var args struct {
			ID      string         `json:"id"`
			Params  fixtureParams  `json:"params"`
			Context ForeignContext `json:"context"`
		}
		// The decode error is DELIBERATELY ignored: invalid params are the
		// input class this handler exists to let through.
		_ = json.Unmarshal(req.Params.Arguments, &args)
		result, err := c.Walk(ctx, args.ID, args.Params, args.Context)
		if err != nil {
			//nolint:nilerr // a walk failure is an MCP TOOL error, not a transport error: this
			// raw handler mirrors the SDK's own shape, where the nil second return is what makes
			// IsError reach the caller as a result rather than as a broken JSON-RPC frame.
			return &mcp.CallToolResult{IsError: true}, nil
		}
		out, err := encodeResult(DefaultToolName, result)
		if err != nil {
			//nolint:nilerr // same shape: an encoding failure is reported as a tool error.
			return &mcp.CallToolResult{IsError: true}, nil
		}
		return &mcp.CallToolResult{StructuredContent: out}, nil
	}
}
