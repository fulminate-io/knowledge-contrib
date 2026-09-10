// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"
)

// roundtrip_test.go — THE CLOUD CONTEXT END TO END, over the wire, through a
// spawned child running the shipped collector.
//
// WHY THIS TEST EXISTS IN THIS SHAPE. It replaces a pending pin that asserted
// two properties of THIS MODULE — that its own params sub-schema declared no
// context property, and that its constructor carried no context — and was
// therefore blind to where the contract actually grew: the block is an ENVELOPE
// property beside `id` and `params`, delivered as a Walk ARGUMENT. A pin over a
// module's own surface cannot see a change to the framework's, wherever the
// framework chooses to put it. The remedy is not a better pin: the dependency
// has landed, so the thing to assert is the behavior itself.
//
// WHAT IS REAL HERE. The framework's own serving path decodes the envelope, the
// shipped Walk receives the block as its argument, the production resolver
// matches it against the walk's own stream labels, and the production emitter
// builds the proxy and its edge. The only substitution is the CloudWatch client,
// which serves recorded pages.

// TestADeclaredCloudBlockProducesAProxyOverTheWire is the round trip, with the
// control that makes its green a statement about the block.
func TestADeclaredCloudBlockProducesAProxyOverTheWire(t *testing.T) {
	const (
		account  = "acct-1"
		resource = "arn:aws:ecs:us-east-1:111122223333:service/prod/api-server"
		wantEdge = "log-label:service=api-server"
	)
	wantProxy := proxyIDPrefix + account + ":" + resource

	params := map[string]any{"log_groups": []any{"/ecs/prod/api-server"}}

	t.Run("with a declared block, the proxy and its edge ride the envelope", func(t *testing.T) {
		child := dialStdioProvider(t, modeRoundTrip)
		envelope := callForEnvelope(t, child, map[string]any{
			"id":     "instance-1",
			"params": params,
			"context": map[string]any{
				"cloud": []any{map[string]any{
					"graph_name": account,
					"nodes": []any{map[string]any{
						"id":          resource,
						"symbol_name": "api-server",
						"metadata":    map[string]any{"resource_type": "ecs:service"},
					}},
					"edges": []any{},
				}},
			},
		})

		if !hasNode(envelope, wantProxy, nodeProxy) {
			t.Errorf("the result carries no proxy %q; nodes: %s", wantProxy, nodeSummary(envelope))
		}
		if !hasEdge(envelope, wantEdge, wantProxy, edgeEmittedBy) {
			t.Errorf("the result carries no %s edge from %q to %q; edges: %s",
				edgeEmittedBy, wantEdge, wantProxy, edgeSummary(envelope))
		}
		// The rest of the graph is unchanged by the block, which is what makes
		// the proxy an addition rather than a different walk.
		if !hasType(envelope, nodeLogTemplate) || !hasType(envelope, nodeLogStream) {
			t.Errorf("the result lost its log nodes: %s", nodeSummary(envelope))
		}
	})

	t.Run("with NO declared block, neither is emitted and the walk is otherwise identical", func(t *testing.T) {
		child := dialStdioProvider(t, modeRoundTrip)
		envelope := callForEnvelope(t, child, map[string]any{"id": "instance-1", "params": params})

		if hasType(envelope, nodeProxy) {
			t.Errorf("a collect with no declared block emitted a proxy: %s", nodeSummary(envelope))
		}
		for _, edgeType := range []string{edgeEmittedBy, edgeCorrelatesWith} {
			if hasEdgeType(envelope, edgeType) {
				t.Errorf("a collect with no declared block emitted a %s edge: %s", edgeType, edgeSummary(envelope))
			}
		}
		if !hasType(envelope, nodeLogTemplate) || !hasType(envelope, nodeLogStream) {
			t.Errorf("the unblocked walk produced no log nodes: %s", nodeSummary(envelope))
		}
	})

	t.Run("a declared resource that matches no label resolves nothing", func(t *testing.T) {
		child := dialStdioProvider(t, modeRoundTrip)
		envelope := callForEnvelope(t, child, map[string]any{
			"id":     "instance-1",
			"params": params,
			"context": map[string]any{
				"cloud": []any{map[string]any{
					"graph_name": account,
					"nodes": []any{map[string]any{
						"id":          "arn:aws:ecs:us-east-1:111122223333:service/prod/other",
						"symbol_name": "some-other-service",
						"metadata":    map[string]any{"resource_type": "ecs:service"},
					}},
					"edges": []any{},
				}},
			},
		})
		if hasType(envelope, nodeProxy) {
			t.Errorf("a block whose resource matches no stream label emitted a proxy: %s", nodeSummary(envelope))
		}
	})
}

// callForEnvelope makes one collect call and returns its decoded envelope,
// failing the test on a refusal.
func callForEnvelope(t *testing.T, p *stdioProvider, args map[string]any) map[string]any {
	t.Helper()
	res, err := p.call(t, args)
	if err != nil {
		t.Fatalf("the collect failed at the transport: %v", err)
	}
	if res.IsError {
		t.Fatalf("the collect was refused: %s", refusalText(res, nil))
	}
	envelope, ok := res.StructuredContent.(map[string]any)
	if !ok {
		t.Fatalf("the result carries no envelope: %v", res.StructuredContent)
	}
	if envelope["walk_complete"] != true {
		t.Fatalf("the walk asserted %v over an exhausted recording", envelope["walk_complete"])
	}
	return envelope
}

// rows reads one array of objects out of the envelope.
func rows(envelope map[string]any, key string) []map[string]any {
	raw, _ := envelope[key].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		if row, ok := r.(map[string]any); ok {
			out = append(out, row)
		}
	}
	return out
}

func hasNode(envelope map[string]any, id, nodeType string) bool {
	for _, n := range rows(envelope, "nodes") {
		if n["id"] == id && n["type"] == nodeType {
			return true
		}
	}
	return false
}

func hasType(envelope map[string]any, nodeType string) bool {
	for _, n := range rows(envelope, "nodes") {
		if n["type"] == nodeType {
			return true
		}
	}
	return false
}

func hasEdge(envelope map[string]any, from, to, edgeType string) bool {
	for _, e := range rows(envelope, "edges") {
		if e["from_id"] == from && e["to_id"] == to && e["type"] == edgeType {
			return true
		}
	}
	return false
}

func hasEdgeType(envelope map[string]any, edgeType string) bool {
	for _, e := range rows(envelope, "edges") {
		if e["type"] == edgeType {
			return true
		}
	}
	return false
}

// nodeSummary and edgeSummary render what a failure actually saw.
func nodeSummary(envelope map[string]any) string {
	return jsonRender(rows(envelope, "nodes"))
}

func edgeSummary(envelope map[string]any) string {
	return jsonRender(rows(envelope, "edges"))
}
