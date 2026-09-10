// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/common/correlation"
	"github.com/fulminate-io/knowledge-contrib/framework"
)

// proxy_test.go — the cloud-linked half of the vocabulary: the proxy node and
// its EMITTED_BY edge, the correlation edge and its confirmation gate, and the
// resolution rule that produces the input for both.
//
// THE PARITY OF THESE AGAINST THE BUILT-IN PATH is asserted by
// TestParityAgainstBuiltinGolden, which compares them field by field against
// the built-in materializer's own output. What is asserted HERE is the
// presence, the deduplication, the gate and the resolution — the properties a
// golden that happened to be right about ids could still leave unobserved.

// TestProxyNodeAndEmittedByEdgePerResolution is the presence row.
func TestProxyNodeAndEmittedByEdgePerResolution(t *testing.T) {
	nodes, edges := materializeProxies([]ResolvedProxy{
		{LabelKey: "service", LabelValue: "api-server", Account: "acct-1", ResourceID: "res-1"},
	})
	if len(nodes) != 1 || len(edges) != 1 {
		t.Fatalf("one resolution produced %d nodes and %d edges, want 1 and 1", len(nodes), len(edges))
	}
	if nodes[0].Type != nodeProxy {
		t.Errorf("node type %q, want %q", nodes[0].Type, nodeProxy)
	}
	if nodes[0].ID != "proxy:cloud:acct-1:res-1" {
		t.Errorf("proxy id %q, want the proxy:cloud:<account>:<resource> convention", nodes[0].ID)
	}
	if nodes[0].Metadata["foreign_graph"] != "cloud" || nodes[0].Metadata["foreign_id"] != "res-1" {
		t.Errorf("the proxy's foreign reference is %v; the id alone does not carry it", nodes[0].Metadata)
	}
	// THE EDGE STARTS AT THE LABEL NODE, not at the stream. A stream-sourced
	// edge is still an edge, so a presence assertion alone would not catch it —
	// which is why the endpoint is asserted here and again in the parity row.
	if edges[0].FromID != LabelNodeID("service", "api-server") {
		t.Errorf("the EMITTED_BY edge starts at %q, want the label node", edges[0].FromID)
	}
	if edges[0].ToID != nodes[0].ID || edges[0].Type != edgeEmittedBy {
		t.Errorf("edge = %+v, want an %s edge to the proxy", edges[0], edgeEmittedBy)
	}
}

// TestTheProxyNodeIsDedupedAndTheEdgeIsNot pins the asymmetry. Two labels
// resolving to one resource are two facts about that resource and both must be
// recorded; the resource itself is one node.
func TestTheProxyNodeIsDedupedAndTheEdgeIsNot(t *testing.T) {
	nodes, edges := materializeProxies([]ResolvedProxy{
		{LabelKey: "service", LabelValue: "api-server", Account: "acct-1", ResourceID: "res-1"},
		{LabelKey: "app", LabelValue: "api", Account: "acct-1", ResourceID: "res-1"},
	})
	if len(nodes) != 1 {
		t.Errorf("two resolutions to one resource produced %d proxy nodes, want 1", len(nodes))
	}
	if len(edges) != 2 {
		t.Errorf("two resolutions produced %d EMITTED_BY edges, want 2; collapsing them drops a label's link",
			len(edges))
	}
}

// TestNoResolutionsEmitsNeither is the zero-resolution control, and it is what
// this collector does today on every production collect: with no cloud context
// supplied it emits no proxy, no EMITTED_BY and no CORRELATES_WITH — exactly
// what the built-in path emits with no cloud graph attached.
func TestNoResolutionsEmitsNeither(t *testing.T) {
	nodes, edges := materializeProxies(nil)
	if len(nodes) != 0 || len(edges) != 0 {
		t.Errorf("no resolutions produced %d nodes and %d edges", len(nodes), len(edges))
	}
	if got := correlation.MaterializeCorrelations(nil); len(got) != 0 {
		t.Errorf("no correlations produced %d edges", len(got))
	}
}

// TestTheWalkEmitsNothingCloudSidedWithoutAContext carries the same control
// through the whole walk rather than through the emission helper, so an
// implementation that supplied itself a context somewhere would red.
func TestTheWalkEmitsNothingCloudSidedWithoutAContext(t *testing.T) {
	f := loadFixture(t)
	result, err := collectorWithFake(newFakeClient(t, f)).Walk(
		t.Context(), "id", f.params(), framework.ForeignContext{})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	for _, n := range result.Nodes {
		if n.Type == nodeProxy {
			t.Errorf("the walk emitted proxy %q with no cloud context supplied", n.ID)
		}
	}
	for _, e := range result.Edges {
		if e.Type == edgeEmittedBy || e.Type == edgeCorrelatesWith {
			t.Errorf("the walk emitted a %s edge with no cloud context supplied", e.Type)
		}
	}
	// THE KNOWN POSITIVE: the same walk with a DECLARED BLOCK does emit them,
	// through the argument the framework hands the walk, so the zeros above are
	// the block's absence rather than a walk that never emits.
	withContext, err := collectorWithFake(newFakeClient(t, f)).Walk(
		t.Context(), "id", f.params(), foreignBlock("acct-1", "res-1", "api-server", "ecs:service"))
	if err != nil {
		t.Fatalf("walk with a context: %v", err)
	}
	if countType(withContext.Nodes) == 0 {
		t.Error("the walk with a resolvable cloud resource emitted no proxy; the control is inert")
	}
}

// TestResolutionMatchesByNameAndTypeRank covers the resolution rule: a
// case-insensitive name match against a resource whose type is service-like,
// with the priority order deciding among several.
func TestResolutionMatchesByNameAndTypeRank(t *testing.T) {
	stream := &LogStream{LowCardLabels: map[string]string{"service": "api-server", "log_group": "/ecs/prod/api-server"}}

	t.Run("a matching service-like resource resolves", func(t *testing.T) {
		got := resolveStreams([]*LogStream{stream}, CloudContext{Resources: []CloudResource{
			{Account: "acct-1", ID: "res-1", SymbolName: "API-Server", ResourceType: "ecs:service"},
		}})
		if len(got) != 1 || got[0].ResourceID != "res-1" {
			t.Fatalf("resolutions = %+v, want one naming res-1 (the name match is case-insensitive)", got)
		}
		if got[0].LabelKey != "service" || got[0].LabelValue != "api-server" {
			t.Errorf("the resolution names label %s=%s, want the stream's own label",
				got[0].LabelKey, got[0].LabelValue)
		}
	})

	t.Run("priority order decides among several matches", func(t *testing.T) {
		got := resolveStreams([]*LogStream{stream}, CloudContext{Resources: []CloudResource{
			{Account: "a", ID: "deployment", SymbolName: "api-server", ResourceType: "Deployment"},
			{Account: "a", ID: "ecs", SymbolName: "api-server", ResourceType: "ecs:service"},
		}})
		if len(got) != 1 || got[0].ResourceID != "ecs" {
			t.Fatalf("resolutions = %+v, want the higher-priority ecs:service", got)
		}
	})

	t.Run("a non-service-like type does not resolve", func(t *testing.T) {
		got := resolveStreams([]*LogStream{stream}, CloudContext{Resources: []CloudResource{
			{Account: "a", ID: "bucket", SymbolName: "api-server", ResourceType: "s3:bucket"},
		}})
		if len(got) != 0 {
			t.Errorf("resolutions = %+v, want none: an s3 bucket is not a service", got)
		}
	})

	t.Run("a prefix match requires a word boundary", func(t *testing.T) {
		// "Service" must NOT match "ServiceAccount": those are distinct kinds
		// and the priority order depends on telling them apart.
		got := resolveStreams([]*LogStream{stream}, CloudContext{Resources: []CloudResource{
			{Account: "a", ID: "sa", SymbolName: "api-server", ResourceType: "ServiceAccount"},
		}})
		if len(got) != 0 {
			t.Errorf("resolutions = %+v; ServiceAccount is not a Service", got)
		}
	})

	t.Run("a non-service-identifying label key is never resolved", func(t *testing.T) {
		other := &LogStream{LowCardLabels: map[string]string{"log_stream": "api-server"}}
		got := resolveStreams([]*LogStream{other}, CloudContext{Resources: []CloudResource{
			{Account: "a", ID: "res", SymbolName: "api-server", ResourceType: "ecs:service"},
		}})
		if len(got) != 0 {
			t.Errorf("resolutions = %+v; log_stream is not a service-identifying key", got)
		}
	})

	t.Run("one label on two streams resolves once", func(t *testing.T) {
		second := &LogStream{LowCardLabels: map[string]string{"service": "api-server", "log_stream": "other"}}
		got := resolveStreams([]*LogStream{stream, second}, CloudContext{Resources: []CloudResource{
			{Account: "a", ID: "res", SymbolName: "api-server", ResourceType: "ecs:service"},
		}})
		if len(got) != 1 {
			t.Errorf("resolutions = %+v, want one: every stream in a log group shares its service label", got)
		}
	})
}

// countType counts proxy nodes, the one type this suite's rows assert on.
func countType(nodes []framework.Node) int {
	n := 0
	for _, node := range nodes {
		if node.Type == nodeProxy {
			n++
		}
	}
	return n
}

// TestProxyDescriptionNamesBothSides pins the human-readable half, which is
// what a reader of the graph sees when they open a proxy node.
func TestProxyDescriptionNamesBothSides(t *testing.T) {
	nodes, _ := materializeProxies([]ResolvedProxy{
		{LabelKey: "service", LabelValue: "api-server", Account: "acct-1", ResourceID: "res-1"},
	})
	for _, want := range []string{"service=api-server", "acct-1", "res-1"} {
		if !strings.Contains(nodes[0].Description, want) {
			t.Errorf("the proxy description %q does not name %q", nodes[0].Description, want)
		}
	}
	if nodes[0].SymbolName != "service=api-server" {
		t.Errorf("the proxy symbol name is %q, want the label pair", nodes[0].SymbolName)
	}
	if nodes[0].Source != "proxy:cloud:acct-1" {
		t.Errorf("the proxy source is %q, want the account-scoped source", nodes[0].Source)
	}
}

// foreignBlock builds a declared cloud block carrying one resource, in the
// framework's own shape.
func foreignBlock(account, id, symbolName, resourceType string) framework.ForeignContext {
	return framework.ForeignContext{fixtureFamily: []framework.ForeignGraph{{
		GraphName: account,
		Nodes: []framework.ForeignNode{{
			ID:         id,
			SymbolName: symbolName,
			Metadata:   map[string]string{"resource_type": resourceType},
		}},
	}}}
}

// TestTheResourceTypeIsReadFromMetadataNotTheNodeType pins which field the
// resolver ranks on. A declaration that carries the node's graph-level type but
// not the cloud collector's own resource_type metadata key resolves nothing,
// which is correct: ranking on the wrong field would match labels to resources
// the built-in resolver would not have.
func TestTheResourceTypeIsReadFromMetadataNotTheNodeType(t *testing.T) {
	withMetadata := cloudContextFrom(foreignBlock("acct-1", "res-1", "api-server", "ecs:service"))
	if len(withMetadata.Resources) != 1 || withMetadata.Resources[0].ResourceType != "ecs:service" {
		t.Fatalf("resources = %+v, want the resource_type metadata key", withMetadata.Resources)
	}

	typeOnly := framework.ForeignContext{fixtureFamily: []framework.ForeignGraph{{
		GraphName: "acct-1",
		Nodes:     []framework.ForeignNode{{ID: "res-1", SymbolName: "api-server", Type: "ecs:service"}},
	}}}
	got := cloudContextFrom(typeOnly)
	if len(got.Resources) != 1 || got.Resources[0].ResourceType != "" {
		t.Fatalf("resources = %+v, want an empty resource type from a node carrying only its graph type",
			got.Resources)
	}
	stream := &LogStream{LowCardLabels: map[string]string{"service": "api-server"}}
	if res := resolveStreams([]*LogStream{stream}, got); len(res) != 0 {
		t.Errorf("resolutions = %+v, want none: a node with no resource_type ranks against nothing", res)
	}
}

// TestADeclaredCodeGraphResolvesNothing pins that the code half of the block is
// ignored here. A log label resolves to a CLOUD resource; this collector emits
// no code-graph proxy, and silently resolving against a code graph would emit a
// proxy whose id names a cloud account that does not exist.
func TestADeclaredCodeGraphResolvesNothing(t *testing.T) {
	block := framework.ForeignContext{framework.FamilyCode: []framework.ForeignGraph{{
		GraphName: "knowledge",
		Nodes: []framework.ForeignNode{{
			ID: "f.go:Handler", SymbolName: "api-server",
			Metadata: map[string]string{"resource_type": "ecs:service"},
		}},
	}}}
	if got := cloudContextFrom(block); len(got.Resources) != 0 {
		t.Errorf("resources = %+v, want none from a declared CODE graph", got.Resources)
	}
}
