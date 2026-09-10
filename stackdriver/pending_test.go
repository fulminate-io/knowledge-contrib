// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fulminate-io/knowledge-contrib/common/correlation"
)

// pending_test.go — THE ARRIVAL of the cloud context, end to end through a real
// stdio child.
//
// WHAT THIS FILE USED TO HOLD, and why the change is worth naming: a skip. The
// framework's Walk carried no context block, so there was no route from the
// operator's cloud graph to this module's resolutions, and standing a fake in
// would have proved that the module can consume a shape nobody had designed.
// The block landed; the skip is gone and this is what replaced it.
//
// THE DIVISION OF PROOF. The EMISSION is proven in proxy_test.go and
// parity_test.go from resolutions the module holds — the id convention, the
// deduplicated node beside the undeduplicated edge, the three-decimal evidence
// string, and the whole batch compared against the client's own materializer.
// What none of those can show is that a declared block ARRIVES and turns into
// those resolutions. That is this file: one call, over a pipe, to a separate
// process, carrying a context block, coming back with proxies.

// TestADeclaredCloudContextArrivesAndProducesTheCloudLinkedEmission is the round
// trip. Both sides are real: the child runs the shipped stdio entry point and
// the block crosses as JSON on the wire.
func TestADeclaredCloudContextArrivesAndProducesTheCloudLinkedEmission(t *testing.T) {
	session := dialSpawnedCollector(t, spawnRecorded)

	// The recorded entries carry service=api and service=worker. The declared
	// slice names both as cloud resources in one account and carries an edge
	// between them, which is what upgrades their overlapping errors from a
	// coincidence to a correlation.
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: toolName,
		Arguments: map[string]any{
			"id":     "collect-1",
			"params": map[string]any{"project": "p"},
			"context": map[string]any{
				"cloud": []any{map[string]any{
					"graph_name": "acct",
					"nodes": []any{
						map[string]any{"id": "res-api", "type": "service", "symbol_name": "api"},
						map[string]any{"id": "res-worker", "type": "service", "symbol_name": "worker"},
					},
					"edges": []any{map[string]any{"from_id": "res-api", "to_id": "res-worker"}},
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("calling with a declared context block: %v", err)
	}
	if res.IsError {
		t.Fatalf("the collect failed: %s", renderContent(t, res))
	}
	got := decodeEnvelope(t, res)

	proxies := got.nodesOfType(nodeTypeProxy)
	if len(proxies) != 2 {
		t.Fatalf("got %d proxy nodes, want one per declared resource the labels resolved to: %+v",
			len(proxies), got.NodeTypes())
	}
	wantIDs := map[string]bool{"proxy:cloud:acct:res-api": true, "proxy:cloud:acct:res-worker": true}
	for _, p := range proxies {
		if !wantIDs[p.ID] {
			t.Errorf("unexpected proxy id %q", p.ID)
		}
		if p.Metadata["account"] != "acct" || p.Metadata["foreign_graph"] != "cloud" {
			t.Errorf("proxy %s carries %v", p.ID, p.Metadata)
		}
	}

	emitted := got.edgesOfType(edgeTypeEmittedBy)
	if len(emitted) != 2 {
		t.Fatalf("got %d EMITTED_BY edges, want one per resolution: %+v", len(emitted), got.EdgeTypes())
	}
	for _, e := range emitted {
		if !got.hasNode(e.FromID) || !got.hasNode(e.ToID) {
			t.Errorf("EMITTED_BY %s->%s names an endpoint the batch does not carry", e.FromID, e.ToID)
		}
	}

	correlations := got.edgesOfType(edgeTypeCorrelatesWith)
	if len(correlations) != 1 {
		t.Fatalf("got %d CORRELATES_WITH edges, want the one the declared dependency confirms: %+v",
			len(correlations), got.EdgeTypes())
	}
	if correlations[0].Method != correlationMethod {
		t.Errorf("correlation method = %q", correlations[0].Method)
	}
}

// TestTheSameCollectWithNoDeclaredContextEmitsNoneOfTheThree is the control the
// round trip needs. Without it the arms above would pass on a module that
// emitted proxies unconditionally, and the whole point of the block is that it
// is what makes the difference.
func TestTheSameCollectWithNoDeclaredContextEmitsNoneOfTheThree(t *testing.T) {
	session := dialSpawnedCollector(t, spawnRecorded)
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      toolName,
		Arguments: map[string]any{"id": "collect-1", "params": map[string]any{"project": "p"}},
	})
	if err != nil {
		t.Fatalf("calling without a context block: %v", err)
	}
	if res.IsError {
		t.Fatalf("the collect failed: %s", renderContent(t, res))
	}
	got := decodeEnvelope(t, res)

	for _, nodeType := range []string{nodeTypeProxy} {
		if n := got.nodesOfType(nodeType); len(n) != 0 {
			t.Errorf("got %d %s nodes with no declared context", len(n), nodeType)
		}
	}
	for _, edgeType := range []string{edgeTypeEmittedBy, edgeTypeCorrelatesWith} {
		if e := got.edgesOfType(edgeType); len(e) != 0 {
			t.Errorf("got %d %s edges with no declared context", len(e), edgeType)
		}
	}
	// KNOWN POSITIVE: the collect still produced the log graph proper, so the
	// zeros above are the absent context rather than an empty walk.
	if len(got.nodesOfType(nodeTypeLogTemplate)) == 0 || len(got.nodesOfType(nodeTypeLogStream)) == 0 {
		t.Fatalf("the collect produced no log graph at all: %+v", got.NodeTypes())
	}
}

// TestTwoDeclaredResourcesWithNoEdgeBetweenThemConfirmNoCorrelation is the
// dependency half on its own.
//
// THE TWO HALVES OF A CORRELATION FAIL SEPARATELY AND THIS SEPARATES THEM. The
// round trip above declares both resources AND an edge, so it would pass on a
// resolver that confirmed every pair it could resolve. Here the same two
// resources are declared with NO edge: the labels still resolve, so the proxies
// and the EMITTED_BY edges are still emitted — that is the control — and the
// correlation is NOT confirmed, because two services erroring at the same moment
// is a coincidence until something says their resources are connected.
func TestTwoDeclaredResourcesWithNoEdgeBetweenThemConfirmNoCorrelation(t *testing.T) {
	session := dialSpawnedCollector(t, spawnRecorded)
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: toolName,
		Arguments: map[string]any{
			"id":     "collect-1",
			"params": map[string]any{"project": "p"},
			"context": map[string]any{
				"cloud": []any{map[string]any{
					"graph_name": "acct",
					"nodes": []any{
						map[string]any{"id": "res-api", "type": "service", "symbol_name": "api"},
						map[string]any{"id": "res-worker", "type": "service", "symbol_name": "worker"},
					},
					// No edges: the resources exist and are unconnected.
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if res.IsError {
		t.Fatalf("the collect failed: %s", renderContent(t, res))
	}
	got := decodeEnvelope(t, res)

	if c := got.edgesOfType(edgeTypeCorrelatesWith); len(c) != 0 {
		t.Errorf("got %d CORRELATES_WITH edges with no declared dependency", len(c))
	}
	// THE CONTROL: resolution still happened, so the zero above is the missing
	// dependency rather than a block that failed to arrive.
	if p := got.nodesOfType(nodeTypeProxy); len(p) != 2 {
		t.Fatalf("got %d proxies, want 2; the block did not resolve at all, so the zero "+
			"above says nothing about the dependency", len(p))
	}
}

// TestADeclaredResourceThatMatchesNoLabelResolvesNothing is the miss arm: a
// block naming resources this collect's labels do not name must produce no
// proxies rather than resolving something approximate. A wrong EMITTED_BY edge
// asserts that a log stream came from a cloud resource it did not.
func TestADeclaredResourceThatMatchesNoLabelResolvesNothing(t *testing.T) {
	session := dialSpawnedCollector(t, spawnRecorded)
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: toolName,
		Arguments: map[string]any{
			"id":     "collect-1",
			"params": map[string]any{"project": "p"},
			"context": map[string]any{
				"cloud": []any{map[string]any{
					"graph_name": "acct",
					"nodes": []any{
						map[string]any{"id": "res-other", "type": "service", "symbol_name": "billing"},
					},
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	if res.IsError {
		t.Fatalf("the collect failed: %s", renderContent(t, res))
	}
	got := decodeEnvelope(t, res)
	if n := got.nodesOfType(nodeTypeProxy); len(n) != 0 {
		t.Errorf("a block naming no matching resource produced %d proxies", len(n))
	}
}

// TestNoEmittedEdgeSetsATargetGraph pins that this collector's edges are all
// IN-GRAPH.
//
// THE FRAMEWORK OFFERS BOTH SHAPES and they are different graphs: setting
// TargetGraph makes the client resolve the endpoint against another family,
// materialize a proxy and link into the LINKAGE graph, while leaving it empty
// keeps the edge in this collect's own graph. A log collector's proxy is an
// OWN-GRAPH node — it is emitted in this same batch and its EMITTED_BY edge
// points at it, so both endpoints are here and there is no family to name. Using
// the field would produce a second, different proxy in the linkage graph beside
// the one this module already emits, and would break parity with the client's
// own materializer, which this module's golden compares against node for node.
func TestNoEmittedEdgeSetsATargetGraph(t *testing.T) {
	session := dialSpawnedCollector(t, spawnRecorded)
	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{
		Name: toolName,
		Arguments: map[string]any{
			"id":     "collect-1",
			"params": map[string]any{"project": "p"},
			"context": map[string]any{
				"cloud": []any{map[string]any{
					"graph_name": "acct",
					"nodes": []any{
						map[string]any{"id": "res-api", "type": "service", "symbol_name": "api"},
						map[string]any{"id": "res-worker", "type": "service", "symbol_name": "worker"},
					},
					"edges": []any{map[string]any{"from_id": "res-api", "to_id": "res-worker"}},
				}},
			},
		},
	})
	if err != nil {
		t.Fatalf("calling: %v", err)
	}
	got := decodeEnvelope(t, res)
	for _, e := range got.Edges {
		if e.TargetGraph != "" {
			t.Errorf("%s edge %s->%s names the target graph %q; every edge this collector emits "+
				"is in-graph, and its proxy is an own-graph node", e.Type, e.FromID, e.ToID, e.TargetGraph)
		}
	}
	// KNOWN POSITIVE: the assertion ran over the cloud-linked edges, which are
	// the only ones a target graph could plausibly have been set on.
	if len(got.edgesOfType(edgeTypeEmittedBy)) == 0 {
		t.Fatalf("no EMITTED_BY edge was emitted, so the assertion above covered nothing")
	}
}

// wireEnvelope is the contract envelope as it comes back over the wire, decoded
// into the shape these arms question rather than into the module's own types.
type wireEnvelope struct {
	Nodes []struct {
		ID       string            `json:"id"`
		Type     string            `json:"type"`
		Metadata map[string]string `json:"metadata"`
	} `json:"nodes"`
	Edges []struct {
		FromID      string `json:"from_id"`
		ToID        string `json:"to_id"`
		Type        string `json:"type"`
		Method      string `json:"method"`
		TargetGraph string `json:"target_graph"`
	} `json:"edges"`
	WalkComplete bool `json:"walk_complete"`
}

func (w wireEnvelope) nodesOfType(t string) []struct {
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Metadata map[string]string `json:"metadata"`
} {
	var out []struct {
		ID       string            `json:"id"`
		Type     string            `json:"type"`
		Metadata map[string]string `json:"metadata"`
	}
	for _, n := range w.Nodes {
		if n.Type == t {
			out = append(out, n)
		}
	}
	return out
}

func (w wireEnvelope) edgesOfType(t string) []struct {
	FromID      string `json:"from_id"`
	ToID        string `json:"to_id"`
	Type        string `json:"type"`
	Method      string `json:"method"`
	TargetGraph string `json:"target_graph"`
} {
	var out []struct {
		FromID      string `json:"from_id"`
		ToID        string `json:"to_id"`
		Type        string `json:"type"`
		Method      string `json:"method"`
		TargetGraph string `json:"target_graph"`
	}
	for _, e := range w.Edges {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

func (w wireEnvelope) hasNode(id string) bool {
	for _, n := range w.Nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}

// NodeTypes and EdgeTypes render what the batch held, for a failure message.
func (w wireEnvelope) NodeTypes() map[string]int {
	out := map[string]int{}
	for _, n := range w.Nodes {
		out[n.Type]++
	}
	return out
}

func (w wireEnvelope) EdgeTypes() map[string]int {
	out := map[string]int{}
	for _, e := range w.Edges {
		out[e.Type]++
	}
	return out
}

func decodeEnvelope(t *testing.T, res *mcp.CallToolResult) wireEnvelope {
	t.Helper()
	var out wireEnvelope
	if err := json.Unmarshal(mustMarshal(t, res.StructuredContent), &out); err != nil {
		t.Fatalf("decoding the envelope: %v", err)
	}
	return out
}

// TestTheWiringSeamIsExactlyOneArgumentWide survives the block's arrival because
// it asserts something the round trip does not: that the cloud context changes
// ONLY the cloud-linked output and leaves the log graph proper identical.
func TestTheWiringSeamIsExactlyOneArgumentWide(t *testing.T) {
	entries := crossServiceErrorEntries()

	without, err := runPipeline(entries, defaultPipelineConfig(), nil)
	if err != nil {
		t.Fatalf("running without a cloud context: %v", err)
	}
	if len(without.Resolutions) != 0 {
		t.Errorf("a nil cloud context produced %d resolutions", len(without.Resolutions))
	}
	if confirmedCount(without.Correlations) != 0 {
		t.Errorf("a nil cloud context confirmed %d correlations", confirmedCount(without.Correlations))
	}

	cloud := &stubCloud{
		services: map[string]resolvedResource{
			"api":    {Account: "acct", ID: "res-api"},
			"worker": {Account: "acct", ID: "res-worker"},
		},
		dependent: map[string]bool{"res-api->res-worker": true},
	}
	with, err := runPipeline(entries, defaultPipelineConfig(), cloud)
	if err != nil {
		t.Fatalf("running with a cloud context: %v", err)
	}
	if len(with.Resolutions) == 0 {
		t.Fatalf("a populated cloud context produced no resolutions")
	}
	if confirmedCount(with.Correlations) == 0 {
		t.Fatalf("a populated cloud context confirmed no correlations")
	}

	if len(without.Templates) != len(with.Templates) ||
		len(without.Streams) != len(with.Streams) ||
		len(without.Chunks) != len(with.Chunks) {
		t.Errorf("the cloud context changed the log graph proper: templates %d/%d streams %d/%d chunks %d/%d",
			len(without.Templates), len(with.Templates),
			len(without.Streams), len(with.Streams),
			len(without.Chunks), len(with.Chunks))
	}
}

func confirmedCount(correlations []correlation.Result) int {
	n := 0
	for _, c := range correlations {
		if c.StructurallyConfirmed {
			n++
		}
	}
	return n
}
