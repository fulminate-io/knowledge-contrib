// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/common/correlation"
	"github.com/fulminate-io/knowledge-contrib/framework"
)

// correlate_accounts_test.go — THE TWO ACCOUNT RULES the dependency oracle
// applies, each reached by a block declaring TWO cloud graphs.
//
// A single-graph block cannot reach either: both rules are about what happens
// when a resolution or a declared edge crosses from one declared graph into
// another, and the correlation arms next door all declare one. That is why these
// cells are here rather than folded into them.

// TestBothAccountGuardsInTheDependencyOracle covers the two rules the oracle
// applies across declared graphs, each of which a two-graph block reaches.
//
// WHY THEY ARE REACHABLE RATHER THAN DEFENSIVE. The declared block is a SLICE of
// graphs, the context reads all of them, and the resolver ranks across every
// declared resource whatever graph it came from. So two service labels can
// resolve into two DIFFERENT graphs, which the cross-account refusal governs;
// and two graphs can carry the same resource ids, which the account-scoped
// dependency key governs. Neither needs a malformed block — only a second one.
func TestBothAccountGuardsInTheDependencyOracle(t *testing.T) {
	entries := correlationEntries(t)

	// THE CONTROL FIRST, so every zero below is a guard and not a fixture that
	// never correlated: one graph carrying both resources and the edge between
	// them produces the edge.
	t.Run("one graph carrying both resources and the edge produces the edge", func(t *testing.T) {
		if got := correlationEdgeCount(t, entries, oneGraphBlock()); got != 1 {
			t.Fatalf("the single-graph control produced %d correlation edges, want 1; every guard cell "+
				"below would be vacuous", got)
		}
	})

	// GUARD ONE, the cross-account refusal. The two services resolve into two
	// DIFFERENT graphs, and the edge is declared in the first. A resolved pair
	// spanning two accounts is never dependent here, because a declared edge
	// belongs to one graph and its endpoints are ids in that graph.
	t.Run("two resources resolved from DIFFERENT graphs are never dependent", func(t *testing.T) {
		block := framework.ForeignContext{fixtureFamily: []framework.ForeignGraph{
			{
				GraphName: correlationAccount,
				Nodes:     []framework.ForeignNode{cloudNode(correlationResourceA, correlationServiceA, "lambda:function")},
				Edges:     []framework.ForeignEdge{{FromID: correlationResourceA, ToID: correlationResourceB}},
			},
			{
				GraphName: "acct-2",
				Nodes:     []framework.ForeignNode{cloudNode(correlationResourceB, correlationServiceB, "ecs:service")},
			},
		}}
		if got := correlationEdgeCount(t, entries, block); got != 0 {
			t.Errorf("a pair resolved across two accounts produced %d correlation edge(s), want none", got)
		}
	})

	// GUARD TWO, the account inside the dependency key. The edge is declared in
	// one graph between two ids; the SERVICES resolve to those same two ids in
	// ANOTHER graph. Without the account in the key, the first graph's edge
	// would answer the second graph's question.
	t.Run("one graph's declared edge does not answer another graph's question", func(t *testing.T) {
		block := framework.ForeignContext{fixtureFamily: []framework.ForeignGraph{
			{
				GraphName: correlationAccount,
				Nodes: []framework.ForeignNode{
					cloudNode(correlationResourceA, "unrelated-one", "lambda:function"),
					cloudNode(correlationResourceB, "unrelated-two", "ecs:service"),
				},
				Edges: []framework.ForeignEdge{{FromID: correlationResourceA, ToID: correlationResourceB}},
			},
			{
				GraphName: "acct-2",
				Nodes: []framework.ForeignNode{
					cloudNode(correlationResourceA, correlationServiceA, "lambda:function"),
					cloudNode(correlationResourceB, correlationServiceB, "ecs:service"),
				},
			},
		}}
		if got := correlationEdgeCount(t, entries, block); got != 0 {
			t.Errorf("an edge declared in %s answered a dependency question about acct-2 and produced %d "+
				"correlation edge(s), want none", correlationAccount, got)
		}
	})
}

// cloudNode is one declared cloud resource.
func cloudNode(id, symbolName, resourceType string) framework.ForeignNode {
	return framework.ForeignNode{
		ID: id, SymbolName: symbolName,
		Metadata: map[string]string{"resource_type": resourceType},
	}
}

// oneGraphBlock declares both resources and the edge between them in a single
// graph, which is the shape the guard cells vary from.
func oneGraphBlock() framework.ForeignContext {
	return framework.ForeignContext{fixtureFamily: []framework.ForeignGraph{{
		GraphName: correlationAccount,
		Nodes: []framework.ForeignNode{
			cloudNode(correlationResourceA, correlationServiceA, "lambda:function"),
			cloudNode(correlationResourceB, correlationServiceB, "ecs:service"),
		},
		Edges: []framework.ForeignEdge{{FromID: correlationResourceA, ToID: correlationResourceB}},
	}}}
}

// correlationEntries normalizes the correlation fixture's events the way a walk
// does, so these cells drive buildGraph over the same input the round trip does.
func correlationEntries(t *testing.T) []LogEntry {
	t.Helper()
	var out []LogEntry
	for _, g := range correlationEvents() {
		for _, page := range g.Pages {
			for _, e := range page.Events {
				entry, err := normalizeEntry(buildEvents([]recordedEvent{e})[0], g.LogGroup)
				if err != nil {
					t.Fatalf("normalizing: %v", err)
				}
				out = append(out, entry)
			}
		}
	}
	return reclassifySeverity(out)
}

// correlationEdgeCount builds the graph under one declared block and counts the
// correlation edges it emitted.
func correlationEdgeCount(t *testing.T, entries []LogEntry, block framework.ForeignContext) int {
	t.Helper()
	_, edges, err := buildGraph(entries, cloudContextFrom(block))
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}
	n := 0
	for _, e := range edges {
		if e.Type == correlation.EdgeCorrelatesWith {
			n++
		}
	}
	return n
}
