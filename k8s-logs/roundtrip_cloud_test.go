// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fulminate-io/knowledge-contrib/common/correlation"
	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/k8s-logs/internal/k8slogs"
	"github.com/fulminate-io/knowledge-contrib/k8s-logs/internal/logpipe"
)

// roundtrip_cloud_test.go — THE CLOUD HALF, END TO END, over the same real child
// process and the same real JSON-RPC framing as every other round trip.
//
// WHY THESE TWO ARE HERE RATHER THAN AS UNIT ARMS. The declared context block is
// a WIRE property: it arrives beside the params on a tool call, decoded by the
// framework, and the detector that consumes it lives in a third module. A unit
// test can show that the computation is right given a slice; only a walk shows
// that the slice arrives at all. Both tests below carry their own control — the
// identical collect with the block, or the dependency, removed — so each zero is
// a measurement rather than an absence.

// TestARoundTripCarriesTheDeclaredCloudContext — the WIRING, over a real child
// process and a real tool call.
//
// THIS ROW WAS PENDING AND HAS COME DUE. While the collect input declared only
// the collect id and the params, nothing could deliver a cloud slice to a
// collector, so the module's cloud computation was covered in both arms and its
// delivery was covered by nothing; the row that stood here asserted the
// contract's own shape and said what to replace it with. The contract now
// carries the block, so this is that replacement: a client-filled context block
// travels the same JSON-RPC framing everything else does, and the proxy nodes
// and EMITTED_BY edges built from it come back in the result.
//
// ITS CONTROL IS THE SAME CALL WITHOUT THE BLOCK, in the same test, against the
// same provider and the same stub cluster. That is what makes the zero a
// measurement rather than an absence: the identical collect emits none of the
// three, which is also what the built-in log pipeline emits with no cloud graph
// attached.
func TestARoundTripCarriesTheDeclaredCloudContext(t *testing.T) {
	api := newStubAPI(t)
	session := dialProvider(t, api.URL)

	collect := func(args map[string]any) struct {
		Nodes        []framework.Node `json:"nodes"`
		Edges        []framework.Edge `json:"edges"`
		WalkComplete bool             `json:"walk_complete"`
	} {
		t.Helper()
		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
			Name: k8slogs.ToolName, Arguments: args,
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Fatalf("the collect returned a tool error: %v", res.Content)
		}
		return decodePayload(t, res)
	}

	params := map[string]any{"namespaces": []string{"dev"}, "chunk_window_seconds": 3600}

	control := collect(map[string]any{"id": "dev-window", "params": params})
	if n := countNodeType(control.Nodes, logpipe.NodeProxy); n != 0 {
		t.Fatalf("%d proxy nodes came back from a collect carrying NO context block", n)
	}
	if n := countEdgeType(control.Edges, logpipe.EdgeEmittedBy); n != 0 {
		t.Fatalf("%d EMITTED_BY edges came back from a collect carrying no context block", n)
	}

	withContext := collect(map[string]any{
		"id":     "dev-window",
		"params": params,
		"context": map[string]any{
			"cloud": []map[string]any{{
				"graph_name": "fulminate-services",
				"nodes": []map[string]any{{
					"id":   "ns/dev",
					"type": "k8s_namespace",
					"metadata": map[string]string{
						"namespace":     "dev",
						"resource_type": "k8s_namespace",
					},
				}},
				"edges": []map[string]any{},
			}},
		},
	})

	if n := countNodeType(withContext.Nodes, logpipe.NodeProxy); n != 1 {
		t.Fatalf("%d proxy nodes came back from a collect carrying a cloud slice, want 1. The block travels "+
			"on the collect input beside the params; if it did not arrive, the walk saw no cloud graph and "+
			"emitted the same nothing the control did", n)
	}
	if len(withContext.Nodes) != len(control.Nodes)+1 {
		t.Fatalf("the context block added %d nodes, want exactly the one proxy",
			len(withContext.Nodes)-len(control.Nodes))
	}
	for _, n := range withContext.Nodes {
		if n.Type != logpipe.NodeProxy {
			continue
		}
		if n.ID != "proxy:cloud:fulminate-services:ns/dev" {
			t.Errorf("the proxy's id is %q", n.ID)
		}
		if n.Metadata["foreign_id"] != "ns/dev" || n.Metadata["account"] != "fulminate-services" {
			t.Errorf("the proxy carries foreign_id=%q account=%q",
				n.Metadata["foreign_id"], n.Metadata["account"])
		}
	}
	found := false
	for _, e := range withContext.Edges {
		if e.Type != logpipe.EdgeEmittedBy {
			continue
		}
		found = true
		if e.FromID != "log-label:namespace=dev" {
			t.Errorf("EMITTED_BY runs from %q, want the namespace label node", e.FromID)
		}
	}
	if !found {
		t.Fatal("no EMITTED_BY edge came back from a collect carrying a cloud slice")
	}
}

func countNodeType(nodes []framework.Node, nodeType string) int {
	n := 0
	for _, node := range nodes {
		if node.Type == nodeType {
			n++
		}
	}
	return n
}

func countEdgeType(edges []framework.Edge, edgeType string) int {
	n := 0
	for _, e := range edges {
		if e.Type == edgeType {
			n++
		}
	}
	return n
}

// TestARoundTripEmitsAConfirmedCorrelation — the whole correlation path, over a
// real child process, through the common detector.
//
// WHAT MAKES THIS DIFFERENT FROM THE UNIT ARMS. The detector, its ERROR filter,
// its overlap scoring and its rendered edge all live in another module now, and
// what this module supplies is the projection and the two cloud answers. Only a
// walk end to end shows that the pieces still meet: two services' errors from a
// real cluster read, projected into the detector's vocabulary, resolved against
// a client-filled context block, and the edge coming back over JSON-RPC.
//
// THE CONTROL IS THE SAME COLLECT WITH THE DEPENDENCY REMOVED, in the same test.
// Both services still error at the same instant and both still resolve, so the
// pair is still a CANDIDATE — what changes is only whether the operator's graph
// says the resources are connected. That is what separates a correlation from a
// coincidence, and inverting it must produce no edge.
func TestARoundTripEmitsAConfirmedCorrelation(t *testing.T) {
	api := newStubAPI(t)
	session := dialProvider(t, api.URL)

	cloudGraph := func(edges []map[string]any) map[string]any {
		return map[string]any{
			"cloud": []map[string]any{{
				"graph_name": "acct",
				"nodes": []map[string]any{
					{"id": "r-api", "type": "k8s_namespace", "metadata": map[string]string{"namespace": "dev"}},
					{"id": "r-db", "type": "k8s_namespace", "metadata": map[string]string{"namespace": "data"}},
				},
				"edges": edges,
			}},
		}
	}
	collect := func(ctxBlock map[string]any) struct {
		Nodes        []framework.Node `json:"nodes"`
		Edges        []framework.Edge `json:"edges"`
		WalkComplete bool             `json:"walk_complete"`
	} {
		t.Helper()
		res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
			Name: k8slogs.ToolName,
			Arguments: map[string]any{
				"id": "two-services",
				"params": map[string]any{
					"namespaces":           []string{"dev", "data"},
					"chunk_window_seconds": 3600,
				},
				"context": ctxBlock,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Fatalf("the collect returned a tool error: %v", res.Content)
		}
		return decodePayload(t, res)
	}

	unconfirmed := collect(cloudGraph([]map[string]any{}))
	if n := countEdgeType(unconfirmed.Edges, correlation.EdgeCorrelatesWith); n != 0 {
		t.Fatalf("%d CORRELATES_WITH edges came back with the two resources declared but NO edge between "+
			"them. Two services erroring at the same moment is a coincidence until the operator's graph "+
			"says their resources depend on each other", n)
	}

	confirmed := collect(cloudGraph([]map[string]any{{"from_id": "r-api", "to_id": "r-db"}}))
	edges := 0
	for _, e := range confirmed.Edges {
		if e.Type != correlation.EdgeCorrelatesWith {
			continue
		}
		edges++
		if e.Method != correlation.CorrelationMethod {
			t.Errorf("the edge's method is %q, want the common detector's %q", e.Method, correlation.CorrelationMethod)
		}
		if e.Confidence <= 0 || e.Confidence > 1 {
			t.Errorf("the edge's confidence is %v, want a score in (0,1]", e.Confidence)
		}
		// THE PAIR'S ORDER IS THE TEMPLATE LIST'S, NOT ALPHABETICAL. The detector
		// sorts RESULTS by (ServiceA, ServiceB, TemplateA); within a pair, A is
		// whichever error template it reached first. With one result the sort
		// changes nothing, so the order here is the order the walk produced.
		for _, want := range []string{"services=dev,data", "resources=acct:r-api,acct:r-db", "score="} {
			if !strings.Contains(e.Evidence, want) {
				t.Errorf("the edge's evidence %q does not carry %q", e.Evidence, want)
			}
		}
		if e.FromID == "" || e.ToID == "" || e.FromID == e.ToID {
			t.Errorf("the edge joins %q -> %q", e.FromID, e.ToID)
		}
	}
	if edges != 1 {
		t.Fatalf("%d CORRELATES_WITH edges came back with the dependency declared, want 1", edges)
	}
}
