// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// declaration_seam_test.go — THE DECLARE SIDE AND THE READ SIDE, DRIVEN AS ONE.
//
// WHY A SEAM TEST AND NOT TWO UNIT TESTS. This module's read side was fully
// tested against blocks the tests handed it, and its entry declared NOTHING, so
// in production the block was always empty and the whole cloud-linked half was
// dead. Every read-side row above passed the entire time. What has to hold is
// that the block an operator's ENTRY causes to be filled is a block THIS
// collector can consume — and that is a property of the declaration and the read
// together, which neither side can assert alone.
//
// THE FIXTURE IS BUILT FROM THE DECLARATION, FIELD BY FIELD, and never from what
// the reader happens to want. A fixture that supplies whatever the reader asks
// for is a subject holding its own answer key: it stays green under a
// declaration that names a node type nothing emits, metadata keys nothing
// writes, or no fields at all. Here, a field the declaration does not name is
// ABSENT from the fixture node, exactly as the client's projection would leave
// it.

// seamServiceLabel is the service label the fixture entries carry AND the symbol
// name the declared resources are projected with, because the resolution under
// test is exactly "this label value names that resource".
const seamServiceLabel = "api-server"

// filledFromDeclaration builds the block a client WOULD fill for this module's
// entry: one graph per declared family, carrying one node projected to exactly
// the declared node fields and metadata keys, and one edge if edges were
// declared.
//
// resourceType is the value the declared `resource_type` metadata key carries —
// when the declaration names that key at all.
func filledFromDeclaration(decl framework.ForeignContextDeclaration, resourceType string) framework.ForeignContext {
	out := framework.ForeignContext{}
	for _, family := range decl.Families() {
		d := decl[family]
		graph := framework.ForeignGraph{GraphName: family + "-acct"}
		for _, nodeType := range d.NodeTypes {
			node := framework.ForeignNode{}
			for _, field := range d.NodeFields {
				switch field {
				case "id":
					node.ID = family + ":res-1"
				case "type":
					node.Type = nodeType
				case "symbol_name":
					node.SymbolName = seamServiceLabel
				}
			}
			for _, key := range d.MetadataKeys {
				if node.Metadata == nil {
					node.Metadata = map[string]string{}
				}
				// ONE KEY HAS A VALUE THE RESOLVER RANKS ON and every other declared
				// key carries a placeholder, because what is under test is whether the
				// declaration named the key the read side reads — not whether a fixture
				// can be made to satisfy it.
				if key == metaResourceType {
					node.Metadata[key] = resourceType
					continue
				}
				node.Metadata[key] = "declared-but-unused"
			}
			graph.Nodes = append(graph.Nodes, node)
		}
		if len(d.EdgeFields) > 0 && len(graph.Nodes) > 0 {
			graph.Edges = []framework.ForeignEdge{{FromID: family + ":res-1", ToID: family + ":res-2"}}
		}
		out[family] = []framework.ForeignGraph{graph}
	}
	return out
}

// seamEntries is one log entry set whose stream carries the service label the
// declaration's resources are named by. Three entries of two shapes, so the walk
// produces templates, a stream and a chunk rather than an empty graph.
func seamEntries() []LogEntry {
	base := time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC)
	labels := map[string]string{
		"log_group":  "/aws/lambda/api",
		"service":    seamServiceLabel,
		"log_stream": "2026/03/01/[$LATEST]abc",
	}
	out := make([]LogEntry, 0, 3)
	for i, msg := range []string{
		"request completed in 12ms",
		"request completed in 47ms",
		"cache miss for key user:9",
	} {
		out = append(out, LogEntry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Severity:  "info",
			Message:   msg,
			Labels:    labels,
		})
	}
	return out
}

// seamGraph builds the graph for one declared block, failing the test on a walk
// error rather than returning one.
func seamGraph(t *testing.T, cloud CloudContext) ([]framework.Node, []framework.Edge) {
	t.Helper()
	nodes, edges, err := buildGraph(seamEntries(), cloud)
	if err != nil {
		t.Fatalf("buildGraph: %v", err)
	}
	return nodes, edges
}

// TestSeam_TheDeclarationYieldsResolutionsTheEmptyBlockDoesNot: with the
// declaration, a walk emits proxy nodes and EMITTED_BY edges; with the empty
// block this module shipped with, it emits neither. Both arms are the SAME walk
// over the SAME entries, so the difference is the declaration and nothing else.
func TestSeam_TheDeclarationYieldsResolutionsTheEmptyBlockDoesNot(t *testing.T) {
	withDecl := cloudContextFrom(filledFromDeclaration(declaredForeignContext(), "ecs:service"))
	if len(withDecl.Resources) == 0 {
		t.Fatal("the block filled from this module's OWN declaration carries no resource to resolve " +
			"against — the declaration and the read side disagree, which is the failure this seam " +
			"exists to catch")
	}

	nodes, edges := seamGraph(t, withDecl)
	if got := countType(nodes); got == 0 {
		t.Error("the declared block produced no proxy node")
	}
	if got := countEdge(edges); got == 0 {
		t.Error("the declared block produced no EMITTED_BY edge")
	}

	// THE PRE-CHANGE ARM, in the same run: the entry as it shipped declared
	// nothing, so the collector received the zero value.
	emptyNodes, emptyEdges := seamGraph(t, cloudContextFrom(framework.ForeignContext{}))
	if got := countType(emptyNodes); got != 0 {
		t.Errorf("an undeclared entry emitted %d proxy node(s); it receives an empty block and must "+
			"emit none — which is what this collector did on every collect until the declaration existed", got)
	}
	if got := countEdge(emptyEdges); got != 0 {
		t.Errorf("an undeclared entry emitted %d EMITTED_BY edge(s)", got)
	}
}

// TestSeam_TheFieldSetIsThisModulesOwnAndNotAnotherCollectorsIs the copy-paste
// trap.
//
// The k8s-logs collector's declaration looks like a thorough one and asks for
// namespace, cluster_name, region and provider. This module ranks candidates on
// the `resource_type` metadata key and on nothing else, so a declaration copied
// from that module would carry four keys this module never reads and OMIT the one
// it does — and the client's projection drops an undeclared key rather than
// carrying it empty. The result is zero resolutions from a declaration that reads
// as careful, which is this ticket's defect class by another route.
func TestSeam_TheFieldSetIsThisModulesOwnAndNotAnotherCollectorsIs(t *testing.T) {
	mutated := framework.ForeignContextDeclaration{}
	for family, d := range declaredForeignContext() {
		d.MetadataKeys = []string{"namespace", "cluster_name", "region", "provider"}
		mutated[family] = d
	}

	borrowed := cloudContextFrom(filledFromDeclaration(mutated, "ecs:service"))
	if len(borrowed.Resources) == 0 {
		t.Fatal("control: the nodes must still arrive; only the key the ranking reads is missing")
	}
	for _, r := range borrowed.Resources {
		if r.ResourceType != "" {
			t.Errorf("a resource arrived with resource type %q under a declaration that never asked "+
				"for the key", r.ResourceType)
		}
	}

	nodes, edges := seamGraph(t, borrowed)
	if got := countType(nodes); got != 0 {
		t.Errorf("the borrowed field set produced %d proxy node(s); it must produce none, because "+
			"every candidate ranks against an empty resource type", got)
	}
	if got := countEdge(edges); got != 0 {
		t.Errorf("the borrowed field set produced %d EMITTED_BY edge(s)", got)
	}

	// THE SAME-RUN KNOWN POSITIVE: the module's OWN field set over the same
	// entries does resolve, so the zero above is a statement about the borrowed
	// keys rather than about a walk that resolves nothing whatever it is given.
	ownNodes, ownEdges := seamGraph(t, cloudContextFrom(
		filledFromDeclaration(declaredForeignContext(), "ecs:service")))
	if countType(ownNodes) == 0 || countEdge(ownEdges) == 0 {
		t.Error("known positive failed: the module's own field set must resolve over the same entries")
	}
}

// TestSeam_EveryDeclaredFamilyIsOneTheRankingCanMatch. A declared family whose
// resource types the prefix lists cannot rank is a declaration that costs the
// operator's privacy and the collect's size and resolves nothing.
func TestSeam_EveryDeclaredFamilyIsOneTheRankingCanMatch(t *testing.T) {
	decl := declaredForeignContext()
	if len(decl) == 0 {
		t.Fatal("this module reads a foreign context on every collect, so it declares one")
	}

	// ONE RESOURCE TYPE PER DECLARED FAMILY, taken from the prefix lists this
	// module actually ranks on rather than invented here.
	rankable := map[string]string{
		familyAWS: "ecs:service",
		familyK8s: "Deployment",
	}
	for _, family := range decl.Families() {
		resourceType, ok := rankable[family]
		if !ok {
			t.Fatalf("the %q family is declared and no resource type in this module's own prefix lists "+
				"belongs to it; either widen the lists in the same change or drop the declaration", family)
		}
		one := framework.ForeignContextDeclaration{family: decl[family]}
		nodes, _ := seamGraph(t, cloudContextFrom(filledFromDeclaration(one, resourceType)))
		if countType(nodes) == 0 {
			t.Errorf("the %q family declares fields that resolve nothing against a %q resource",
				family, resourceType)
		}
	}

	// AND THE TWO ABSENCES ARE ASSERTED RATHER THAN LEFT IMPLICIT, because an
	// absent family reads as an oversight otherwise. azure has no type in either
	// prefix list; gcp emits its resource type as the node type and cannot be
	// selected until the declaration contract gains a family-level form.
	if _, declared := decl["azure"]; declared {
		t.Error("no Azure type appears in this module's prefix lists, so an azure declaration would " +
			"resolve nothing; widen the lists in the same change or leave it undeclared")
	}
	if _, declared := decl["gcp"]; declared {
		t.Error("gcp emits its resource type as the node type; the only admissible spelling today " +
			"selects no nodes, so the family waits for an explicit family-level selector")
	}
}

// countEdge counts EMITTED_BY edges, the one type this seam's rows assert on.
func countEdge(edges []framework.Edge) int {
	n := 0
	for _, e := range edges {
		if e.Type == edgeEmittedBy {
			n++
		}
	}
	return n
}
