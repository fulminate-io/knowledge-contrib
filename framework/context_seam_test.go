// SPDX-License-Identifier: Apache-2.0

package framework

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// context_seam_test.go — THE DECLARED FOREIGN-GRAPH CONTEXT REACHES THE WALK.
//
// WHY THIS ROW EXISTS AND WHY IT COULD NOT LIVE ON THE CLIENT'S SIDE. The
// client's own harness observes what it SENT; the failure this row is about
// happens on the other side of the wire, inside a module the client cannot
// import. The advertised input schema picks the new property up on its own —
// advertisedInputSchema decodes the whole contract file and replaces only
// params — so a framework collector ADVERTISES the block, the client's gate
// passes, the block is sent, and the SDK's typed handler decodes the arguments
// with an unmarshaller that has no DisallowUnknownFields. Before the field
// existed on the decode target, the block was DROPPED SILENTLY: the collector
// computed from nothing and reported a successful walk.
//
// THE MUTATION THIS ROW IS WRITTEN FOR: drop the Context field from
// collectInput, leaving the seam and the advertised schema in place. The first
// two subtests go red. Nothing else in this module would notice, which is the
// whole reason the row is here.

// contextArgs is a call-argument document carrying a context block in the shape
// the client sends: the contract's own spellings, one entry per graph.
func contextArgs() map[string]any {
	return map[string]any{
		"id":     "board",
		"params": map[string]any{"region": "us-east-1"},
		// THE FAMILY KEY IS A REGISTERED GRAPH TYPE, not a built-in name. That is
		// the whole point of the block being a map: the client supplies `code`
		// plus whatever the operator registered, so a fixture naming a compiled-in
		// family would be testing a key set this contract no longer has.
		"context": map[string]any{
			"aws": []any{map[string]any{
				"graph_name": "prod",
				"nodes": []any{map[string]any{
					"id": "i-1", "type": "aws-resource", "symbol_name": "api-server",
					"metadata": map[string]any{"resource_type": "ec2:instance"},
				}},
				"edges": []any{map[string]any{"from_id": "i-1", "to_id": "i-2"}},
			}},
			"code": []any{map[string]any{
				"graph_name": "api",
				"nodes": []any{map[string]any{
					"id": "api:chart", "file_path": "deploy/Chart.yaml", "content": "name: api-chart\n",
				}},
			}},
		},
	}
}

// callWithArgs drives one tool call over a dialed provider with a raw argument
// document, so a row can send exactly the bytes the client would.
func callWithArgs(t *testing.T, p *provider, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshaling call arguments: %v", err)
	}
	res, err := p.session.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      DefaultToolName,
		Arguments: json.RawMessage(raw),
	})
	if err != nil {
		t.Fatalf("calling the tool: %v", err)
	}
	return res
}

// TestTheContextBlockReachesTheWalk is the seam row, over the in-process
// transport where the fixture value is readable.
func TestTheContextBlockReachesTheWalk(t *testing.T) {
	p := dial(t, overHTTP, fixtureConforming, "")

	res := callWithArgs(t, p, contextArgs())
	if res.IsError {
		t.Fatalf("the call was refused: %v", res.Content)
	}

	calls := p.fixture.recorded()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one walk, got %d", len(calls))
	}
	got := calls[0].foreign

	aws := got.Graphs("aws")
	if len(aws) != 1 || aws[0].GraphName != "prod" {
		t.Fatalf("the aws arm of the block did not reach the walk: %+v", aws)
	}
	if len(aws[0].Nodes) != 1 {
		t.Fatalf("expected one aws node, got %d", len(aws[0].Nodes))
	}
	n := aws[0].Nodes[0]
	if n.ID != "i-1" || n.Type != "aws-resource" || n.SymbolName != "api-server" {
		t.Errorf("the node's declared fields did not survive the decode: %+v", n)
	}
	if n.Metadata["resource_type"] != "ec2:instance" {
		t.Errorf("the node's declared metadata did not survive the decode: %+v", n.Metadata)
	}
	if len(aws[0].Edges) != 1 || aws[0].Edges[0].FromID != "i-1" || aws[0].Edges[0].ToID != "i-2" {
		t.Errorf("the edge endpoints did not survive the decode: %+v", aws[0].Edges)
	}
	code := got.Graphs(FamilyCode)
	if len(code) != 1 || code[0].GraphName != "api" {
		t.Fatalf("the code arm of the block did not reach the walk: %+v", code)
	}
	if code[0].Nodes[0].FilePath != "deploy/Chart.yaml" ||
		!strings.Contains(code[0].Nodes[0].Content, "api-chart") {
		t.Errorf("the code node's declared fields did not survive the decode: %+v", code[0].Nodes[0])
	}

	// THE KEY SET IS EXACTLY WHAT WAS SENT. A decoder that dropped an unknown key
	// into a catch-all, or that seeded families nobody declared, would satisfy
	// every assertion above; the block's whole contract is that a family with no
	// key means the entry never asked.
	if fams := got.Families(); len(fams) != 2 || fams[0] != "aws" || fams[1] != FamilyCode {
		t.Errorf("the decoded key set is not the sent one: %v", fams)
	}
	if _, present := got["cloud"]; present {
		t.Errorf("a family the block never carried must not appear as a key")
	}

	// AND THE EXCEPT HELPER, on the same decoded value: the provider families
	// come back as one set with code left out, which is what a correlating
	// collector reads.
	if others := got.Except(FamilyCode); len(others) != 1 || others[0].GraphName != "prod" {
		t.Errorf("Except(code) must yield the provider graphs alone: %+v", others)
	}

	// THE PARAMS CONTROL, same call and same decode target: params is a field
	// the struct has always named, so its arrival proves the decoder ran. Without
	// it, a green above could not be told from a handler that was never reached.
	if calls[0].params.Region != "us-east-1" {
		t.Errorf("control: params did not arrive, so this row is measuring a dead decoder: %+v", calls[0].params)
	}
}

// TestAContextlessCallLeavesTheWalkUnchanged is the CONTROL for the row above
// and the compatibility promise itself: a collect that carries no block reaches
// a walk whose foreign context is the zero value, and every existing row in this
// module runs that way with no edit.
func TestAContextlessCallLeavesTheWalkUnchanged(t *testing.T) {
	p := dial(t, overHTTP, fixtureConforming, "")

	res := callWithArgs(t, p, map[string]any{
		"id":     "board",
		"params": map[string]any{"region": "us-east-1"},
	})
	if res.IsError {
		t.Fatalf("the call was refused: %v", res.Content)
	}

	calls := p.fixture.recorded()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one walk, got %d", len(calls))
	}
	if !calls[0].foreign.IsEmpty() {
		t.Errorf("a call with no block must reach the walk with an empty one: %+v", calls[0].foreign)
	}
	if calls[0].params.Region != "us-east-1" {
		t.Errorf("control: params must still arrive: %+v", calls[0].params)
	}
}

// TestTheAdvertisedInputSchemaCarriesTheContextProperty pins the half that comes
// free: the advertised schema is the contract file with params spliced in, so
// the new property is advertised by every framework-built collector with no code
// here. That is exactly why the silent drop was reachable, and it is why the
// property has to be pinned rather than assumed.
func TestTheAdvertisedInputSchemaCarriesTheContextProperty(t *testing.T) {
	doc, err := advertisedInputSchema[fixtureParams]()
	if err != nil {
		t.Fatalf("building the advertised input schema: %v", err)
	}
	props, ok := doc.(map[string]any)["properties"].(map[string]any)
	if !ok {
		t.Fatalf("the advertised input schema declares no properties object")
	}
	if _, ok := props["context"]; !ok {
		t.Errorf("the advertised input schema must carry the context property, or a client's gate refuses a declaring entry")
	}
	// THE CONTROLS: the property the splice replaces and the one it leaves alone.
	if _, ok := props["params"]; !ok {
		t.Errorf("control: the spliced params property is missing")
	}
	if _, ok := props["id"]; !ok {
		t.Errorf("control: the collect id property is missing")
	}
}

// TestALargeContextBlockIsAcceptedOverHTTP is the INBOUND half of "collector
// traffic carries no size cap", on this module's own server arm.
//
// THE DEFAULT IT DISPROVES IS THE SDK'S, NOT OURS BY OMISSION. The streamable
// handler bounds an incoming request body at 4 MiB when the option is left at
// zero and answers anything larger with 413 Request Entity Too Large. A collect
// whose entry declares cloud or code context arrives carrying that declared
// slice, which crosses 4 MiB on any real account — so under the default a
// declaring collector's first serious call fails with an HTTP status and no
// mention of collectors anywhere in it.
//
// THE MUTATION: pass nil options to NewStreamableHTTPHandler, restoring the
// default. This row goes red.
func TestALargeContextBlockIsAcceptedOverHTTP(t *testing.T) {
	const (
		bodyLen = 1 << 20 // 1 MiB per node body.
		rows    = 8       // 8 MiB, twice the SDK's 4 MiB default.
	)
	body := strings.Repeat("x", bodyLen)
	nodes := make([]any, 0, rows)
	for i := range rows {
		nodes = append(nodes, map[string]any{"id": fmt.Sprintf("i-%d", i), "content": body})
	}
	args := map[string]any{
		"id":     "board",
		"params": map[string]any{"region": "us-east-1"},
		"context": map[string]any{
			"aws": []any{map[string]any{"graph_name": "prod", "nodes": nodes}},
		},
	}

	p := dial(t, overHTTP, fixtureConforming, "")
	res := callWithArgs(t, p, args)
	if res.IsError {
		t.Fatalf("an 8 MiB declared block must be accepted: %v", res.Content)
	}

	calls := p.fixture.recorded()
	if len(calls) != 1 {
		t.Fatalf("expected exactly one walk, got %d", len(calls))
	}
	got := calls[0].foreign
	bulk := got.Graphs("aws")
	if len(bulk) != 1 || len(bulk[0].Nodes) != rows {
		t.Fatalf("the block must arrive WHOLE, not truncated: %d graphs, %d nodes",
			len(bulk), len(bulk[0].Nodes))
	}
	if len(bulk[0].Nodes[rows-1].Content) != bodyLen {
		t.Errorf("the last node's body was truncated: %d bytes", len(bulk[0].Nodes[rows-1].Content))
	}
}
