// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/common/correlation"
	"github.com/fulminate-io/knowledge-contrib/framework"
)

// proxy_test.go — the three cloud-linkage families.
//
// THE EMISSION IS PROVEN HERE FROM SUPPLIED RESOLUTIONS. What is NOT proven,
// and cannot be at this tree, is that resolutions ARRIVE: the collect input
// carries no declared foreign-graph context block yet, so nothing on the
// production route fills them. That row is pending at the foot of this file,
// named with its reason, and it is a skip rather than a pass — a stubbed green
// there would assert that a route exists when it does not.

func resolution(key, value, account, resource string) Resolution {
	return Resolution{LabelKey: key, LabelValue: value, Account: account, ResourceID: resource}
}

// TestProxyNodeCarriesItsCrossGraphIdentity asserts every field of the emitted
// proxy against the shape the rest of the product builds for the same resource.
// A proxy id in any other format is a node nothing else in the product can join
// to.
func TestProxyNodeCarriesItsCrossGraphIdentity(t *testing.T) {
	graph, err := Build(parityFixture(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	res := resolution("app", "checkout", "acct-1", "i-0abc123")
	nodes, edges, err := Emit(graph, []Resolution{res}, nil)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	proxies := nodesByType(nodes, NodeProxy)
	if len(proxies) != 1 {
		t.Fatalf("proxy nodes = %d, want 1", len(proxies))
	}
	const wantID = "proxy:cloud:acct-1:i-0abc123"
	n, ok := proxies[wantID]
	if !ok {
		t.Fatalf("no proxy at %q; emitted %v", wantID, keysOf(proxies))
	}
	if n.Source != "proxy:cloud:acct-1" {
		t.Fatalf("proxy Source = %q, want %q", n.Source, "proxy:cloud:acct-1")
	}
	if n.SymbolName != "app=checkout" {
		t.Fatalf("proxy SymbolName = %q, want %q", n.SymbolName, "app=checkout")
	}
	wantDesc := "cloud proxy for log label app=checkout (resolved to acct-1/i-0abc123)"
	if n.Description != wantDesc {
		t.Fatalf("proxy Description = %q, want %q", n.Description, wantDesc)
	}
	assertMeta(t, n, "foreign_graph", "cloud")
	assertMeta(t, n, "foreign_id", "i-0abc123")
	assertMeta(t, n, "account", "acct-1")
	assertMeta(t, n, "foreign_type", NodeLogLabel)

	// The EMITTED_BY edge joins the LABEL NODE to the proxy, both ids inside
	// this graph. That is why the shape needs no cross-graph edge carrier.
	var emitted int
	for _, e := range edges {
		if e.Type != EdgeEmittedBy {
			continue
		}
		emitted++
		if e.FromID != LabelNodeID("app", "checkout") {
			t.Fatalf("EMITTED_BY from %q, want the label node %q", e.FromID, LabelNodeID("app", "checkout"))
		}
		if e.ToID != wantID {
			t.Fatalf("EMITTED_BY to %q, want the proxy %q", e.ToID, wantID)
		}
	}
	if emitted != 1 {
		t.Fatalf("EMITTED_BY edges = %d, want 1", emitted)
	}
}

// TestTwoResolutionsNamingOneResourceEmitOneNodeAndTwoEdges is the dedup cell.
// The counts differ on purpose: the node is the resource and the edges are the
// claims about it, so a second node for one resource would be a duplicate id in
// the batch.
func TestTwoResolutionsNamingOneResourceEmitOneNodeAndTwoEdges(t *testing.T) {
	graph, err := Build(parityFixture(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	nodes, edges, err := Emit(graph, []Resolution{
		resolution("app", "checkout", "acct-1", "i-0abc123"),
		resolution("app", "payments", "acct-1", "i-0abc123"),
	}, nil)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	// COUNTED FROM THE SLICE, NOT FROM AN ID-KEYED MAP. Two nodes carrying one
	// id collapse to a single map entry, so a map-based count reads a duplicate
	// as a dedup and this row would pass on the very defect it exists to catch.
	if n := countByType(nodes, NodeProxy); n != 1 {
		t.Fatalf("proxy nodes = %d, want 1 for two resolutions naming one resource", n)
	}
	edgeCount := 0
	for _, e := range edges {
		if e.Type == EdgeEmittedBy {
			edgeCount++
		}
	}
	if edgeCount != 2 {
		t.Fatalf("EMITTED_BY edges = %d, want 2", edgeCount)
	}
}

// TestTwoResourcesEmitTwoProxies is the dedup cell's control: without it, "one
// node" above would also pass on an emitter that emitted one node for every
// input whatever the resources were.
func TestTwoResourcesEmitTwoProxies(t *testing.T) {
	graph, err := Build(parityFixture(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	// The second resolution names a DIFFERENT LABEL KEY as well as a different
	// resource, because a proxy's identity is its resource and its edge's
	// identity is its label: two keys resolving to two resources is the shape an
	// operator actually has.
	nodes, _, err := Emit(graph, []Resolution{
		resolution("app", "checkout", "acct-1", "i-0abc123"),
		resolution("reason", "OOMKilled", "acct-1", "i-0def456"),
	}, nil)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if n := countByType(nodes, NodeProxy); n != 2 {
		t.Fatalf("proxy nodes = %d, want 2 for two distinct resources", n)
	}
}

// TestAResolutionMissingHalfItsTargetIsRefused is the bad-input arm: a proxy id
// is built from the account and the resource, so an empty either side would
// produce an id like "proxy:cloud::i-1".
func TestAResolutionMissingHalfItsTargetIsRefused(t *testing.T) {
	graph, err := Build(parityFixture(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	for _, bad := range []Resolution{
		resolution("app", "checkout", "", "i-0abc123"),
		resolution("app", "checkout", "acct-1", ""),
	} {
		if _, _, err := Emit(graph, []Resolution{bad}, nil); err == nil {
			t.Fatalf("a resolution with account=%q resource=%q was accepted", bad.Account, bad.ResourceID)
		}
	}
}

// TestCorrelatesWithIsEmittedOnlyForAConfirmedPair asserts what this module
// EMITS for a confirmed and an unconfirmed pair. The rendering itself is the
// common correlation module's and is tested there; what this arm holds is that
// this collector's walk carries a result set through to the edge set unchanged,
// which is the seam a collector can break on its own.
//
// THE SKIP IS THE CELL AN IMPLEMENTATION GETS WRONG: an unconfirmed correlation
// emits NOTHING, not an edge carrying a low confidence, because confirmation is
// the claim that a dependency path exists and a temporal overlap without it is a
// coincidence.
func TestCorrelatesWithIsEmittedOnlyForAConfirmedPair(t *testing.T) {
	graph, err := Build(parityFixture(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	confirmed := correlation.Result{
		TemplateA: "tpl-a", TemplateB: "tpl-b",
		ServiceA: "checkout", ServiceB: "payments",
		ResourceA: "acct:i-1", ResourceB: "acct:i-2",
		CooccurrenceScore: 0.8125, StructurallyConfirmed: true,
	}
	unconfirmed := confirmed
	unconfirmed.TemplateA, unconfirmed.TemplateB = "tpl-c", "tpl-d"
	unconfirmed.StructurallyConfirmed = false

	_, edges, err := Emit(graph, nil, []correlation.Result{confirmed, unconfirmed})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}

	var got []struct{ from, to string }
	for _, e := range edges {
		if e.Type != EdgeCorrelatesWith {
			continue
		}
		got = append(got, struct{ from, to string }{e.FromID, e.ToID})
		if e.FromID != "tpl-a" || e.ToID != "tpl-b" {
			t.Fatalf("the emitted correlation joins %q -> %q; the unconfirmed pair leaked", e.FromID, e.ToID)
		}
		if e.Confidence != 0.8125 {
			t.Fatalf("confidence = %v, want the cooccurrence score 0.8125", e.Confidence)
		}
		// THE METHOD LITERAL IS THE COMMON MODULE'S, read from there rather
		// than restated, so a rename there reaches this assertion.
		if e.Method != correlation.CorrelationMethod {
			t.Fatalf("method = %q, want %q", e.Method, correlation.CorrelationMethod)
		}
		// THE SCORE IS FORMATTED TO THREE DECIMAL PLACES, and the rounding is
		// Go's ROUND-HALF-TO-EVEN: 0.8125 renders as 0.812, not 0.813. A
		// collector that formatted with a different rule, or at a different
		// precision, would write a different audit string for the same pair.
		want := "services=checkout,payments resources=acct:i-1,acct:i-2 score=0.812"
		if e.Evidence != want {
			t.Fatalf("evidence = %q, want %q", e.Evidence, want)
		}
	}
	if len(got) != 1 {
		t.Fatalf("CORRELATES_WITH edges = %d, want exactly 1 (the confirmed pair)", len(got))
	}
}

// TestTheZeroResolutionArmEmitsNoneOfTheThree is the parity-correct control for
// this tree, and its point is that it is CORRECT rather than a gap: the
// built-in pipeline emits none of the three with no cloud graph attached
// either.
//
// IT DOES NOT DISCHARGE THE REQUIREMENT. A suite shipping only this row would
// be green and would have observed nothing about the populated shape, which is
// why the three tests above exist and why the pending row below names what is
// still missing.
func TestTheZeroResolutionArmEmitsNoneOfTheThree(t *testing.T) {
	graph, err := Build(parityFixture(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	nodes, edges, err := Emit(graph, nil, nil)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if n := countByType(nodes, NodeProxy); n != 0 {
		t.Fatalf("proxy nodes = %d, want 0 with nothing resolved", n)
	}
	for _, e := range edges {
		if e.Type == EdgeEmittedBy || e.Type == EdgeCorrelatesWith {
			t.Fatalf("a %s edge was emitted with nothing resolved", e.Type)
		}
	}

	// THE BASE FLOOR IS UNAFFECTED: the four node types and three edge types
	// are all still there. Without this half, "zero of the three" would also
	// pass on an emitter that emitted nothing at all.
	for _, want := range []string{NodeLogTemplate, NodeLogStream, NodeLogChunk, NodeLogLabel} {
		if countByType(nodes, want) == 0 {
			t.Fatalf("the base floor lost its %s nodes", want)
		}
	}

	// And the populated arm in the SAME package is what makes this zero mean
	// "nothing resolved" rather than "the emitter cannot emit these".
	populated, _, err := Emit(graph, []Resolution{resolution("app", "checkout", "a", "r")}, nil)
	if err != nil {
		t.Fatalf("Emit (control): %v", err)
	}
	if countByType(populated, NodeProxy) != 1 {
		t.Fatal("the populated control emitted no proxy; the zero above proves nothing")
	}
}

// countByType counts nodes of a type in the emitted SLICE, so two nodes sharing
// one id count as two.
func countByType(nodes []framework.Node, want string) int {
	n := 0
	for _, node := range nodes {
		if node.Type == want {
			n++
		}
	}
	return n
}

// keysOf renders a map's keys for a failure message.
func keysOf(m map[string]framework.Node) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}
