// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fulminate-io/knowledge-contrib/loki/internal/collect"
)

// context_roundtrip_test.go — the DECLARED FOREIGN-GRAPH CONTEXT BLOCK, end to
// end through a real child process.
//
// This file replaces the pending row this suite carried while the block did not
// exist. The emission was always provable from resolutions handed to the
// emitter directly; what needed the whole provider was the ARRIVAL — a JSON
// document on the wire, the framework's decode of it, the parameter the walk
// takes, this module's matching rules, and the proxy nodes and EMITTED_BY edges
// that come back.

// TestResolutionsArriveThroughTheCollectInput is the ROUND TRIP that replaces
// this suite's former pending row.
//
// WHAT IT OBSERVES, and why it needed a real child. The proxy emission was
// always provable from resolutions a fixture handed the emitter directly; what
// could not be proven was that resolutions ARRIVE — that a collect carrying a
// declared foreign-graph context block reaches the walk with the cloud
// resources that block names. That route crosses the whole provider: a JSON
// document on the wire, the framework's decode of the context field, the
// parameter the walk takes, this module's matching rules, and the proxy nodes
// and EMITTED_BY edges in the result. A fixture cannot stand in for any of it.
//
// So the child is the real binary's entry point, the block is real JSON in the
// call arguments, and the assertion is on the emitted graph that comes back.
func TestResolutionsArriveThroughTheCollectInput(t *testing.T) {
	end := time.Date(2026, 9, 7, 13, 0, 0, 0, time.UTC)
	loki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{"result": []map[string]any{{
				"stream": map[string]string{"app": "checkout", "instance": "host-3"},
				"values": [][]string{{strconv.FormatInt(end.UnixNano(), 10), "disk pressure detected"}},
			}}},
		})
	}))
	defer loki.Close()

	session := child(t, childModeReal, nil)
	call := func(context map[string]any) *mcp.CallToolResult {
		t.Helper()
		args := map[string]any{
			"id": "my-collect-id",
			"params": map[string]any{
				"address": loki.URL,
				"start":   "2026-09-07T12:00:00Z",
				"end":     "2026-09-07T13:00:01Z",
			},
		}
		if context != nil {
			args["context"] = context
		}
		res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: collect.ToolName, Arguments: args})
		if err != nil {
			t.Fatalf("calling the tool: %v", err)
		}
		if res.IsError {
			t.Fatalf("the collect failed: %s", resultText(res))
		}
		return res
	}

	// THE DECLARED BLOCK: one graph of a family THIS MODULE ACTUALLY DECLARES,
	// carrying one resource whose symbol name is the value of the stream's `app`
	// label. The family key used to be `cloud`, which is a RETIRED family name no
	// client can supply — so the round trip was driven with a block an operator
	// could never receive. The node type is the one that family's collector
	// emits, taken from the declaration rather than invented here.
	family := DeclaredForeignContext().Families()[0]
	withBlock := envelopeOf(t, call(map[string]any{
		family: []any{map[string]any{
			"graph_name": "acct-1",
			"nodes": []any{map[string]any{
				"id": "i-0abc123", "type": DeclaredForeignContext()[family].NodeTypes[0], "symbol_name": "checkout",
			}},
			"edges": []any{},
		}},
	}))

	proxies := nodesOfType(withBlock, "proxy")
	if len(proxies) != 1 {
		t.Fatalf("the collect returned %d proxy nodes, want 1; the declared block did not reach the walk", len(proxies))
	}
	if got := proxies[0]["id"]; got != "proxy:cloud:acct-1:i-0abc123" {
		t.Fatalf("proxy id = %v, want the cross-graph id for the declared resource", got)
	}
	meta, _ := proxies[0]["metadata"].(map[string]any)
	for key, want := range map[string]string{
		"foreign_graph": "cloud", "foreign_id": "i-0abc123", "account": "acct-1", "foreign_type": "log-label",
	} {
		if got := fmt.Sprint(meta[key]); got != want {
			t.Fatalf("proxy metadata %s = %q, want %q", key, got, want)
		}
	}

	emitted := edgesOfType(withBlock, "EMITTED_BY")
	if len(emitted) != 1 {
		t.Fatalf("the collect returned %d EMITTED_BY edges, want 1", len(emitted))
	}
	if got := emitted[0]["from_id"]; got != "log-label:app=checkout" {
		t.Fatalf("EMITTED_BY from %v, want the label node", got)
	}
	if got := emitted[0]["to_id"]; got != "proxy:cloud:acct-1:i-0abc123" {
		t.Fatalf("EMITTED_BY to %v, want the proxy", got)
	}
	// IT IS AN OWN-GRAPH EDGE and carries no target graph: both endpoints are
	// ids of this collect's own result. A cross-graph endpoint is the other
	// shape, and this family is not one.
	if got, ok := emitted[0]["target_graph"]; ok && got != "" {
		t.Fatalf("EMITTED_BY carries target_graph %v; the proxy is a node of this collect's own graph", got)
	}

	// THE SAME COLLECT WITH NO BLOCK, in the same run, is the control that makes
	// the assertions above about the BLOCK rather than about this collector
	// emitting a proxy for every stream it sees.
	withoutBlock := envelopeOf(t, call(nil))
	if n := len(nodesOfType(withoutBlock, "proxy")); n != 0 {
		t.Fatalf("a collect carrying no context block returned %d proxy nodes", n)
	}
	if n := len(edgesOfType(withoutBlock, "EMITTED_BY")); n != 0 {
		t.Fatalf("a collect carrying no context block returned %d EMITTED_BY edges", n)
	}
	// And the base floor is identical either way: the block adds families, it
	// does not change the log graph.
	for _, want := range []string{"log-template", "log-stream", "log-chunk", "log-label"} {
		a, b := len(nodesOfType(withBlock, want)), len(nodesOfType(withoutBlock, want))
		if a == 0 || a != b {
			t.Fatalf("%s nodes: %d with the block, %d without; the block must not change the base floor", want, a, b)
		}
	}
}

// TestADeclaredResourceMatchingNoLabelResolvesNothing is the round trip's
// discrimination: a block that arrives and names a resource no label matches
// yields no proxy, so the test above observes the MATCH rather than the
// block's mere presence.
func TestADeclaredResourceMatchingNoLabelResolvesNothing(t *testing.T) {
	end := time.Date(2026, 9, 7, 13, 0, 0, 0, time.UTC)
	loki := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{"result": []map[string]any{{
				"stream": map[string]string{"app": "checkout"},
				"values": [][]string{{strconv.FormatInt(end.UnixNano(), 10), "disk pressure detected"}},
			}}},
		})
	}))
	defer loki.Close()

	session := child(t, childModeReal, nil)
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: collect.ToolName,
		Arguments: map[string]any{
			"id": "my-collect-id",
			"params": map[string]any{
				"address": loki.URL, "start": "2026-09-07T12:00:00Z", "end": "2026-09-07T13:00:01Z",
			},
			"context": map[string]any{
				"cloud": []any{map[string]any{
					"graph_name": "acct-1",
					"nodes":      []any{map[string]any{"id": "i-other", "symbol_name": "payments"}},
					"edges":      []any{},
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("calling the tool: %v", err)
	}
	if res.IsError {
		t.Fatalf("the collect failed: %s", resultText(res))
	}
	if n := len(nodesOfType(envelopeOf(t, res), "proxy")); n != 0 {
		t.Fatalf("a block naming a resource no label matches produced %d proxy nodes", n)
	}
}

// envelopeOf decodes a collect result's structured content.
func envelopeOf(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	return structuredContent(t, res)
}

// nodesOfType returns the envelope's nodes of one type.
func nodesOfType(envelope map[string]any, want string) []map[string]any {
	var out []map[string]any
	nodes, _ := envelope["nodes"].([]any)
	for _, raw := range nodes {
		n, _ := raw.(map[string]any)
		if fmt.Sprint(n["type"]) == want {
			out = append(out, n)
		}
	}
	return out
}

// edgesOfType returns the envelope's edges of one type.
func edgesOfType(envelope map[string]any, want string) []map[string]any {
	var out []map[string]any
	edges, _ := envelope["edges"].([]any)
	for _, raw := range edges {
		e, _ := raw.(map[string]any)
		if fmt.Sprint(e["type"]) == want {
			out = append(out, e)
		}
	}
	return out
}

// TestAConfirmedCorrelationReachesTheGraphThroughTheRealChild is the correlation
// half of the same round trip, and it needs the real provider for the same
// reason the proxy half does: the declared block crosses the wire as JSON, is
// decoded by the framework, reaches the walk as a parameter, is turned into this
// module's cloud context, answers the common detector's two questions, and the
// edge the common materializer renders comes back in the result.
//
// THE FIXTURE IS TWO SERVICES ERRORING IN THE SAME MINUTE, with a declared cloud
// graph naming both resources and ONE EDGE between them. That edge is the only
// thing that separates this run from its control below.
func TestAConfirmedCorrelationReachesTheGraphThroughTheRealChild(t *testing.T) {
	loki := correlationLoki(t)
	session := child(t, childModeReal, nil)

	withDependency := collectWithContext(t, session, loki.URL, map[string]any{
		"cloud": []any{map[string]any{
			"graph_name": "acct-1",
			"nodes": []any{
				map[string]any{"id": "i-checkout", "type": "ec2:instance", "symbol_name": "checkout"},
				map[string]any{"id": "i-payments", "type": "ec2:instance", "symbol_name": "payments"},
			},
			"edges": []any{map[string]any{"from_id": "i-checkout", "to_id": "i-payments"}},
		}},
	})

	edges := edgesOfType(withDependency, "CORRELATES_WITH")
	if len(edges) == 0 {
		t.Fatalf("the collect returned no CORRELATES_WITH edge; the declared dependency did not reach the detector")
	}
	for _, e := range edges {
		if got := fmt.Sprint(e["method"]); got != "temporal+cloud-dependency" {
			t.Fatalf("method = %q, want the common module's literal", got)
		}
		confidence, ok := e["confidence"].(float64)
		if !ok || confidence <= 0 {
			t.Fatalf("confidence = %v, want the cooccurrence score", e["confidence"])
		}
		evidence := fmt.Sprint(e["evidence"])
		// THE COMMON MODULE'S EVIDENCE SHAPE, asserted on the wire. WHICH
		// SERVICE IS A AND WHICH IS B IS THE DETECTOR'S, not alphabetical, so
		// the assertion is on the SHAPE and the PAIRING rather than on an order
		// this module does not choose: both services appear, both resources
		// appear, and service A's resource is the one listed first.
		fields := regexp.MustCompile(
			`^services=([^,]+),([^ ]+) resources=([^,]+),([^ ]+) score=(\d+\.\d{3})$`).FindStringSubmatch(evidence)
		if fields == nil {
			t.Fatalf("evidence = %q, which is not the common module's shape (or the score is not at three decimals)", evidence)
		}
		serviceA, serviceB, resourceA, resourceB := fields[1], fields[2], fields[3], fields[4]
		if serviceA == serviceB {
			t.Fatalf("evidence = %q, which correlates a service with itself", evidence)
		}
		want := map[string]string{"checkout": "acct-1:i-checkout", "payments": "acct-1:i-payments"}
		for service, resource := range map[string]string{serviceA: resourceA, serviceB: resourceB} {
			expected, known := want[service]
			if !known {
				t.Fatalf("evidence = %q names the unexpected service %q", evidence, service)
			}
			if resource != expected {
				t.Fatalf("evidence = %q pairs service %q with resource %q, want %q", evidence, service, resource, expected)
			}
		}
	}

	// THE CONTROL, one thing varied: the SAME two services, the same two
	// resources, the same overlap, and NO declared edge between them. Without
	// it, "an edge appeared" would not be a statement about confirmation.
	withoutDependency := collectWithContext(t, session, loki.URL, map[string]any{
		"cloud": []any{map[string]any{
			"graph_name": "acct-1",
			"nodes": []any{
				map[string]any{"id": "i-checkout", "type": "ec2:instance", "symbol_name": "checkout"},
				map[string]any{"id": "i-payments", "type": "ec2:instance", "symbol_name": "payments"},
			},
			"edges": []any{},
		}},
	})
	if n := len(edgesOfType(withoutDependency, "CORRELATES_WITH")); n != 0 {
		t.Fatalf("%d CORRELATES_WITH edges with no declared dependency; an unconfirmed pair emits nothing", n)
	}
	// And the proxies are identical either way, so the control varies the
	// dependency and nothing else.
	if a, b := len(nodesOfType(withDependency, "proxy")), len(nodesOfType(withoutDependency, "proxy")); a != b || a != 2 {
		t.Fatalf("proxy nodes: %d with the dependency, %d without; want 2 both ways", a, b)
	}

	// AND WITH NO CLOUD FAMILY DECLARED AT ALL: no proxies and no correlations.
	bare := collectWithContext(t, session, loki.URL, nil)
	if n := len(edgesOfType(bare, "CORRELATES_WITH")); n != 0 {
		t.Fatalf("%d CORRELATES_WITH edges with no declared cloud family", n)
	}
	if n := len(nodesOfType(bare, "proxy")); n != 0 {
		t.Fatalf("%d proxy nodes with no declared cloud family", n)
	}
}

// correlationLoki serves two services erroring inside one minute.
func correlationLoki(t *testing.T) *httptest.Server {
	t.Helper()
	base := time.Date(2026, 9, 7, 12, 30, 0, 0, time.UTC)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{"result": []map[string]any{
				{
					"stream": map[string]string{"service": "checkout"},
					"values": [][]string{{strconv.FormatInt(base.UnixNano(), 10), "ERROR checkout upstream refused"}},
				},
				{
					"stream": map[string]string{"service": "payments"},
					"values": [][]string{{strconv.FormatInt(base.Add(2*time.Second).UnixNano(), 10), "ERROR payments gateway timed out"}},
				},
			}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// collectWithContext runs one collect over the whole window, with the supplied
// context block or none.
func collectWithContext(t *testing.T, session *mcp.ClientSession, address string, context map[string]any) map[string]any {
	t.Helper()
	args := map[string]any{
		"id": "my-collect-id",
		"params": map[string]any{
			"address": address,
			"start":   "2026-09-07T12:00:00Z",
			"end":     "2026-09-07T13:00:00Z",
		},
	}
	if context != nil {
		args["context"] = context
	}
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: collect.ToolName, Arguments: args})
	if err != nil {
		t.Fatalf("calling the tool: %v", err)
	}
	if res.IsError {
		t.Fatalf("the collect failed: %s", resultText(res))
	}
	return envelopeOf(t, res)
}
