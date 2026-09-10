// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// proxy_test.go — the cloud-linked emission: proxy nodes, EMITTED_BY edges and
// CORRELATES_WITH edges, each asserted against the resolutions and correlations
// the module HOLDS rather than against how they arrived.
//
// HOW THEY ARRIVE IS THE ONE THING THESE ARMS DO NOT COVER. The resolutions come
// from the operator's cloud graph, which a collector process cannot read; they
// reach the module as declared foreign-graph context on the collect input, and
// the framework's Walk signature at this tree carries no such block. So the
// EMISSION is proven here and the production ROUTE is not — see
// pending_test.go, which names what is missing rather than standing in for it.

// TestProxyNodeIsDeduplicatedAndCarriesTheSharedIDConvention covers the id
// convention (which is the knowledge client's and the server's, so a proxy
// emitted here is the same node either of them would build) and the dedup.
func TestTheProxyNodeIsDeduplicatedAndCarriesTheSharedIDConvention(t *testing.T) {
	// TWO DIFFERENT LABELS resolving to ONE cloud resource.
	resolutions := []resolvedProxyEntry{
		{LabelKey: "service", LabelValue: "api", Account: "acct-1", ResourceID: "res-1"},
		{LabelKey: "namespace", LabelValue: "prod", Account: "acct-1", ResourceID: "res-1"},
	}
	nodes, edges, err := materializeProxies(resolutions)
	if err != nil {
		t.Fatalf("materializing: %v", err)
	}
	if len(nodes) != 1 {
		t.Fatalf("two resolutions to one resource emitted %d proxy nodes, want 1", len(nodes))
	}
	if len(edges) != 2 {
		t.Fatalf("two resolutions emitted %d EMITTED_BY edges, want 2", len(edges))
	}

	p := nodes[0]
	if p.ID != "proxy:cloud:acct-1:res-1" {
		t.Errorf("proxy id = %q, want proxy:cloud:acct-1:res-1", p.ID)
	}
	if p.Source != "proxy:cloud:acct-1" {
		t.Errorf("proxy source = %q", p.Source)
	}
	if p.Type != nodeTypeProxy {
		t.Errorf("proxy type = %q, want %q", p.Type, nodeTypeProxy)
	}
	for k, want := range map[string]string{
		"foreign_graph": "cloud",
		"foreign_id":    "res-1",
		"account":       "acct-1",
		"foreign_type":  nodeTypeLogLabel,
	} {
		if p.Metadata[k] != want {
			t.Errorf("proxy metadata %s = %q, want %q", k, p.Metadata[k], want)
		}
	}
	if p.SymbolName != "service=api" {
		t.Errorf("proxy SymbolName = %q, want the first resolution's label", p.SymbolName)
	}
	if p.Description != "cloud proxy for log label service=api (resolved to acct-1/res-1)" {
		t.Errorf("proxy Description = %q", p.Description)
	}

	// SAME-RUN CONTROL: distinct targets produce distinct nodes, so the dedup
	// above is a dedup rather than a cap.
	distinct, _, err := materializeProxies([]resolvedProxyEntry{
		{LabelKey: "service", LabelValue: "api", Account: "acct-1", ResourceID: "res-1"},
		{LabelKey: "service", LabelValue: "worker", Account: "acct-1", ResourceID: "res-2"},
	})
	if err != nil {
		t.Fatalf("materializing the control: %v", err)
	}
	if len(distinct) != 2 {
		t.Fatalf("two resolutions to two resources emitted %d proxy nodes, want 2", len(distinct))
	}
}

// TestTheEmittedByEdgeIsNotDeduplicated is the asymmetry a reimplementation
// loses by symmetry. Two labels naming one resource are ONE resource and TWO
// facts about it; deduplicating the edge as well drops one of the two.
func TestTheEmittedByEdgeIsNotDeduplicated(t *testing.T) {
	_, edges, err := materializeProxies([]resolvedProxyEntry{
		{LabelKey: "service", LabelValue: "api", Account: "a", ResourceID: "r"},
		{LabelKey: "namespace", LabelValue: "prod", Account: "a", ResourceID: "r"},
	})
	if err != nil {
		t.Fatalf("materializing: %v", err)
	}
	froms := make(map[string]bool, len(edges))
	for _, e := range edges {
		if e.Type != edgeTypeEmittedBy {
			t.Errorf("edge type = %q, want %q", e.Type, edgeTypeEmittedBy)
		}
		if e.ToID != "proxy:cloud:a:r" {
			t.Errorf("edge target = %q", e.ToID)
		}
		froms[e.FromID] = true
	}
	for _, want := range []string{"log-label:service=api", "log-label:namespace=prod"} {
		if !froms[want] {
			t.Errorf("no EMITTED_BY edge from %s; the edges are %v", want, froms)
		}
	}
}

// TestAResolutionMissingItsIdentifiersIsRefused is the bad-input arm: a proxy
// whose id names no resource must not be emitted looking exactly like one that
// does.
func TestAResolutionMissingItsIdentifiersIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   resolvedProxyEntry
	}{
		{"no account", resolvedProxyEntry{LabelKey: "service", LabelValue: "api", ResourceID: "r"}},
		{"no resource", resolvedProxyEntry{LabelKey: "service", LabelValue: "api", Account: "a"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := materializeProxies([]resolvedProxyEntry{tc.in})
			if err == nil {
				t.Fatalf("an incomplete resolution was accepted")
			}
			if !strings.Contains(err.Error(), "service=api") {
				t.Errorf("the refusal does not name the label it came from: %v", err)
			}
		})
	}
}

// TestTheEmissionIntegratesIntoTheBatchWithBothEndpointsPresent is the widened
// whole-batch assertion: an EMITTED_BY edge must name a proxy the same batch
// carries and a label node the same batch carries.
func TestTheEmissionIntegratesIntoTheBatchWithBothEndpointsPresent(t *testing.T) {
	entries := recordedEntries()
	out, err := runPipeline(entries, defaultPipelineConfig(), nil)
	if err != nil {
		t.Fatalf("running the pipeline: %v", err)
	}
	// The resolutions a cloud context would have produced for this fixture's
	// own service labels, supplied directly.
	out.Resolutions = []resolvedProxyEntry{
		{LabelKey: "service", LabelValue: "api", Account: "acct", ResourceID: "res-api"},
		{LabelKey: "service", LabelValue: "worker", Account: "acct", ResourceID: "res-worker"},
	}
	nodes, edges, err := assembleGraph(out.Templates, out.Streams, out.Chunks, out.Resolutions, out.Correlations)
	if err != nil {
		t.Fatalf("assembling: %v", err)
	}

	present := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		present[n.ID] = struct{}{}
	}
	emitted := 0
	for _, e := range edges {
		if e.Type != edgeTypeEmittedBy {
			continue
		}
		emitted++
		for _, endpoint := range []string{e.FromID, e.ToID} {
			if _, ok := present[endpoint]; !ok {
				t.Errorf("EMITTED_BY endpoint %q names no node in the batch", endpoint)
			}
		}
	}
	if emitted != 2 {
		t.Fatalf("got %d EMITTED_BY edges, want 2", emitted)
	}
}

// TestACollectWithNoCloudContextEmitsNoneOfTheThree is the parity-correct
// control: with no context there is nothing to resolve against, so the collect
// succeeds carrying zero proxies, zero EMITTED_BY and zero CORRELATES_WITH,
// which is exactly what the pipeline this module reimplements does when it runs
// with no cloud graph attached.
//
// THE ZERO NEEDS A CONTROL AND IT IS THE ROW ABOVE: a module that emitted none
// of the three ever would pass this arm and fail that one.
func TestACollectWithNoCloudContextEmitsNoneOfTheThree(t *testing.T) {
	out, err := runPipeline(recordedEntries(), defaultPipelineConfig(), nil)
	if err != nil {
		t.Fatalf("running the pipeline: %v", err)
	}
	if len(out.Resolutions) != 0 {
		t.Errorf("got %d resolutions with no cloud context", len(out.Resolutions))
	}
	nodes, edges, err := assembleGraph(out.Templates, out.Streams, out.Chunks, out.Resolutions, out.Correlations)
	if err != nil {
		t.Fatalf("assembling: %v", err)
	}
	countOf := func(nodes []framework.Node, edges []framework.Edge, nodeType, edgeType string) (int, int) {
		n, e := 0, 0
		for _, node := range nodes {
			if node.Type == nodeType {
				n++
			}
		}
		for _, edge := range edges {
			if edge.Type == edgeType {
				e++
			}
		}
		return n, e
	}
	proxies, emittedBy := countOf(nodes, edges, nodeTypeProxy, edgeTypeEmittedBy)
	if proxies != 0 || emittedBy != 0 {
		t.Errorf("got %d proxies and %d EMITTED_BY edges with no cloud context", proxies, emittedBy)
	}
	_, correlates := countOf(nodes, edges, "", edgeTypeCorrelatesWith)
	if correlates != 0 {
		t.Errorf("got %d CORRELATES_WITH edges with no cloud context", correlates)
	}
	// KNOWN POSITIVE: the collect still produced a real graph.
	if len(nodes) == 0 || len(edges) == 0 {
		t.Fatalf("the collect produced no graph at all, so the zeros above assert nothing")
	}
}
