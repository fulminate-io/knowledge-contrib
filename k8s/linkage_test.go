// SPDX-License-Identifier: Apache-2.0

package main

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// linkage_test.go — LNK-b through LNK-f: the two cross-graph edge types, on
// the PREDICATE side. What leaves the walk is linkage_emission_test.go's
// subject; what this collector decides from its own nodes and the declared
// context is this file's.
//
// THE ROWS HERE WERE ONCE HALF-PENDING and are not any more, which is worth
// saying because the shape of the file still shows it. Two capabilities the
// contract lacked kept the emission half red-pending: a field on a contract edge
// that could name another graph family, and a declared foreign-graph context on
// the collect input. Both landed, in that order — the target-graph field first,
// which unblocked the shapes whose foreign endpoint is the TO, and then the
// source-graph field, which unblocked the Helm shape whose foreign endpoint is
// the FROM. Every row is a live assertion now; none is a placeholder.
//
// WHAT REPLACED THE PENDING PIN is the reflection row directly below. A pin that
// reds when a field APPEARS has nothing left to do once it has; a row that reds
// when a field DISAPPEARS is what keeps the emitted shapes from silently
// becoming in-graph edges to ids nothing here resolves.

// TestLinkage_ContractCarriesBothGraphFamilyFields pins the contract edge's two
// family fields BY REFLECTION over the framework's own type.
//
// BOTH ARE LOAD-BEARING FOR THIS COLLECTOR, in opposite directions. Drop
// TargetGraph and the three shapes whose far endpoint is the TO become in-graph
// edges to ids nothing here resolves; drop SourceGraph and the Helm shape does,
// or goes back to being computed and discarded. A hand-written field list here
// would be a comment: the contract could lose either one without moving it.
func TestLinkage_ContractCarriesBothGraphFamilyFields(t *testing.T) {
	fields := edgeFieldNames()
	assert.Contains(t, fields, "TargetGraph",
		"the three foreign-TO linkage shapes name another graph through this field; without it "+
			"they would land as in-graph edges to ids nothing resolves")
	assert.Contains(t, fields, "SourceGraph",
		"and the Helm shape names it through the mirror field, because its foreign endpoint is "+
			"the FROM; without it that shape has no expressible direction at all")
	require.Equal(t, "framework.Edge", contractEdgeType().String(),
		"the assertion reflects over the FRAMEWORK's contract edge, not a stand-in declared here")
}

// contractEdgeType is the ONE reflection call site, so the field assertions and
// the type-identity assertion cannot disagree about what they read.
func contractEdgeType() reflect.Type { return reflect.TypeFor[Edge]() }

// edgeFieldNames reports the contract edge's field names BY REFLECTION over the
// aliased type. A hand-written list here would be a comment: the contract could
// gain or drop a field without moving anything that reads this.
func edgeFieldNames() []string {
	t := contractEdgeType()
	names := make([]string, 0, t.NumField())
	for f := range t.Fields() {
		names = append(names, f.Name)
	}
	return names
}

// fullLinkageFixture builds nodes exercising all four shapes, and the declared
// foreign context they are matched against.
//
// THE CONTEXT IS BUILT IN THE CONTRACT'S OWN SHAPE, which is what the client
// fills and the walk receives. The code slice carries the Chart.yaml file nodes
// with their bodies; the azure slice carries an identity keyed by its client id.
func fullLinkageFixture() ([]Node, framework.ForeignContext) {
	nodes := []Node{
		// THE WORKLOAD'S NAME DIFFERS FROM THE CHART IT IS LABELED FOR ON
		// PURPOSE. With both spelled "api" a predicate reading the node's own
		// name instead of the label gives the same answer, and the DEPLOYS row
		// cannot tell the two apart.
		workloadNode("prod/Deployment/api-server", map[string]string{
			"label/app.kubernetes.io/name": "api",
		}),
		namespacedNode("prod", "ServiceAccount", "irsa-sa", map[string]string{
			metaKeyIRSARoleARN: "arn:aws:iam::123456789012:role/api-role",
		}),
		namespacedNode("prod", "ServiceAccount", "gcp-sa", map[string]string{
			metaKeyGCPServiceAccount: "api@fulminate-services.iam.gserviceaccount.com",
		}),
		namespacedNode("prod", "ServiceAccount", "azure-sa", map[string]string{
			metaKeyAzureClientID: "11111111-2222-3333-4444-555555555555",
		}),
	}
	fc := framework.ForeignContext{
		framework.FamilyCode: []framework.ForeignGraph{
			{
				GraphName: "infra",
				Nodes: []framework.ForeignNode{{
					ID:       "charts/api/Chart.yaml",
					Type:     "file",
					FilePath: "charts/api/Chart.yaml",
					Content:  "apiVersion: v2\nname: api\nversion: 1.2.3\n",
				}},
			},
		},
		familyAzure: []framework.ForeignGraph{{
			GraphName: "sub-abc",
			Nodes: []framework.ForeignNode{{
				ID:       "/subscriptions/sub-abc/resourceGroups/rg/providers/identity/api-identity",
				Type:     "cloud-resource",
				Metadata: map[string]string{"client_id": "11111111-2222-3333-4444-555555555555"},
			}},
		}},
	}
	return nodes, fc
}

// foreignFamilyOf reports the family a computed edge's FOREIGN endpoint lives
// in, whichever of the two fields carries it. Three shapes put the k8s node on
// the near side and name the family in TargetGraph; DEPLOYS runs from the chart
// file node and names it in SourceGraph.
//
// IT DELIBERATELY CANNOT TELL THE TWO FIELDS APART, which is why
// TestLinkage_TheFamilyFieldMatchesWhichEndIsForeign exists beside it: a row
// reading only this helper would pass on an edge whose fields were swapped, and
// a swapped edge sends the client looking for the k8s node in the code graph.
func foreignFamilyOf(e linkageEdge) string {
	if e.SourceGraph != "" {
		return e.SourceGraph
	}
	return e.TargetGraph
}

func hasLinkage(edges []linkageEdge, from, to, typ, family string) bool {
	for _, e := range edges {
		if e.FromID == from && e.ToID == to && e.Type == typ && foreignFamilyOf(e) == family {
			return true
		}
	}
	return false
}

// TestLinkage_TheFamilyFieldMatchesWhichEndIsForeign is the discriminating row
// the family-only helper above cannot be: for every computed shape, the family
// sits in the field naming the end that is NOT this collector's own node.
func TestLinkage_TheFamilyFieldMatchesWhichEndIsForeign(t *testing.T) {
	nodes, fc := fullLinkageFixture()
	edges := computeLinkageEdges(nodes, fc)
	require.NotEmpty(t, edges)

	seen := map[string]bool{}
	for _, e := range edges {
		seen[e.Type] = true
		if e.Type == edgeDeploys {
			assert.Equal(t, graphFamilyCode, e.SourceGraph,
				"DEPLOYS runs FROM the chart file node, so its foreign endpoint is the source")
			assert.Empty(t, e.TargetGraph, "and its TO is this collector's own node")
			continue
		}
		assert.NotEmpty(t, e.TargetGraph,
			"%s runs FROM this collector's own node, so its foreign endpoint is the target", e.Type)
		assert.Empty(t, e.SourceGraph, "and its FROM is not foreign")
	}
	// The row is only as good as the shapes it reached; a fixture that stopped
	// producing DEPLOYS would leave the branch above unexercised and silent.
	assert.True(t, seen[edgeDeploys], "the fixture produced a DEPLOYS edge to judge")
	assert.True(t, seen[edgeWorkloadIdentity],
		"and the identity shapes, which are the foreign-TO edges to contrast it with")
}

func countLinkage(edges []linkageEdge, typ string) int {
	n := 0
	for _, e := range edges {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// LNK-b — DEPLOYS, and THE DIRECTION IS THE ROW'S SUBJECT.
func TestLinkage_Deploys_DirectionIsChartToKubernetesNode(t *testing.T) {
	nodes, fc := fullLinkageFixture()
	edges := computeLinkageEdges(nodes, fc)

	assert.True(t, hasLinkage(edges, "charts/api/Chart.yaml", "prod/Deployment/api-server",
		edgeDeploys, graphFamilyCode),
		"the computed edge runs FROM the chart file node TO the Kubernetes node")
	assert.False(t, hasLinkage(edges, "prod/Deployment/api-server", "charts/api/Chart.yaml",
		edgeDeploys, graphFamilyCode),
		"the reverse direction is a different claim; a row asserting only that a DEPLOYS "+
			"edge exists would pass on it")
}

// The second Helm label needs its version suffix stripped before it matches. A
// fixture using only the unversioned label never exercises the stripping.
func TestLinkage_Deploys_VersionedChartLabelIsStripped(t *testing.T) {
	_, fc := fullLinkageFixture()
	versioned := []Node{workloadNode("prod/Deployment/api-server", map[string]string{
		"label/helm.sh/chart": "api-1.2.3",
	})}
	edges := computeLinkageEdges(versioned, fc)
	assert.True(t, hasLinkage(edges, "charts/api/Chart.yaml", "prod/Deployment/api-server",
		edgeDeploys, graphFamilyCode),
		"helm.sh/chart carries <name>-<version> and matches only after the version is stripped")

	// CONTROL: a node carrying neither label matches nothing.
	assert.Zero(t, countLinkage(computeLinkageEdges(
		[]Node{workloadNode("prod/Deployment/plain", nil)}, fc), edgeDeploys))
}

func TestLinkage_StripChartVersion(t *testing.T) {
	cases := map[string]string{
		"api-1.2.3":            "api",
		"my-app-0.1.0":         "my-app",
		"api":                  "api",
		"cert-manager-v1.13.0": "cert-manager-v1.13.0", // a "v" prefix is not a digit
	}
	for in, want := range cases {
		assert.Equal(t, want, stripChartVersion(in), "input %q", in)
	}
}

// LNK-c — WORKLOAD_IDENTITY, IRSA. THIS SHAPE NEEDS NO FOREIGN INPUT: the role
// ARN read off the ServiceAccount IS the target id.
func TestLinkage_WorkloadIdentity_IRSANeedsNoContext(t *testing.T) {
	nodes, _ := fullLinkageFixture()

	// The EMPTY-BLOCK arm is a POSITIVE here, and a row asserting silence on it
	// would red a correct collector.
	edges := computeLinkageEdges(nodes, framework.ForeignContext{})
	assert.True(t, hasLinkage(edges, "prod/ServiceAccount/irsa-sa",
		"arn:aws:iam::123456789012:role/api-role", edgeWorkloadIdentity, familyAWS),
		"IRSA composes its target from the ServiceAccount's own metadata, and points it at "+
			"the aws family — the collector that emits IAM roles")

	// CONTROL: the identical fixture with the key ABSENT emits none. Removing
	// the PREDICATE is what suppresses this shape, not removing the block.
	assert.Zero(t, countLinkage(computeLinkageEdges(
		[]Node{namespacedNode("prod", "ServiceAccount", "plain", nil)}, framework.ForeignContext{}),
		edgeWorkloadIdentity))
}

// LNK-d — WORKLOAD_IDENTITY, GCP. Also needs no foreign input: the target is
// composed from the service-account email.
func TestLinkage_WorkloadIdentity_GCPNeedsNoContext(t *testing.T) {
	nodes, _ := fullLinkageFixture()
	edges := computeLinkageEdges(nodes, framework.ForeignContext{})
	assert.True(t, hasLinkage(edges, "prod/ServiceAccount/gcp-sa",
		"projects/fulminate-services/serviceAccounts/api@fulminate-services.iam.gserviceaccount.com",
		edgeWorkloadIdentity, familyGCP),
		"and points it at the gcp family — the collector that emits service accounts")
}

// LNK-e — WORKLOAD_IDENTITY, Azure. THE ONE IDENTITY SHAPE THAT READS A FOREIGN
// GRAPH, so here the empty-block control is a real assertion rather than a
// false one.
func TestLinkage_WorkloadIdentity_AzureIsTheOneThatReadsAForeignGraph(t *testing.T) {
	nodes, fc := fullLinkageFixture()

	withContext := computeLinkageEdges(nodes, fc)
	assert.True(t, hasLinkage(withContext, "prod/ServiceAccount/azure-sa",
		"/subscriptions/sub-abc/resourceGroups/rg/providers/identity/api-identity",
		edgeWorkloadIdentity, familyAzure),
		"the Azure shape resolves the bare client id to the cloud node's own id")

	// A bare client id names nothing on its own, so with no identities in the
	// block there is nothing to point at.
	azureOnly := []Node{namespacedNode("prod", "ServiceAccount", "azure-sa", map[string]string{
		metaKeyAzureClientID: "11111111-2222-3333-4444-555555555555"})}
	assert.Zero(t, countLinkage(computeLinkageEdges(azureOnly, framework.ForeignContext{}), edgeWorkloadIdentity),
		"unlike IRSA and GCP, the Azure shape cannot resolve without the block")
}

// LNK-f — THE NEGATIVE THAT KEEPS THE SET HONEST. It removes the PREDICATE
// rather than the block, so it works for all four shapes at once. Without it, a
// collector emitting WORKLOAD_IDENTITY unconditionally passes LNK-c, LNK-d and
// LNK-e together.
func TestLinkage_NoPredicateMeansNoEdges(t *testing.T) {
	nodes := []Node{
		// A ServiceAccount carrying NONE of the three identity keys.
		namespacedNode("prod", "ServiceAccount", "plain", nil),
		// A workload carrying neither Helm label.
		workloadNode("prod/Deployment/other", nil),
		// A node of ANOTHER KIND carrying an identity key. Nothing in this
		// collector writes these keys onto a non-ServiceAccount — the three
		// writes are in walkServiceAccounts and nowhere else, and every other
		// walker leaves them alone — so this is a shape only a future change
		// could produce, which is exactly why the kind gate is ASSERTED rather
		// than trusted. Without it the identity shapes would read an arbitrary
		// node's metadata and mint an edge from an object that assumes no
		// identity at all.
		namespacedNode("prod", "ConfigMap", "not-a-serviceaccount", map[string]string{
			metaKeyIRSARoleARN: "arn:aws:iam::123456789012:role/api-role"}),
	}
	_, fc := fullLinkageFixture()
	assert.Empty(t, computeLinkageEdges(nodes, fc),
		"no predicate fires, so no linkage edge of any type is produced")
}

// TestLinkage_EmptyBlockDoesNotCrash is run for every shape, for the separate
// reason that the collector must not fall over when no block arrives — which is
// the state on this tree and will remain a legal state afterwards.
func TestLinkage_EmptyBlockDoesNotCrash(t *testing.T) {
	nodes, _ := fullLinkageFixture()
	require.NotPanics(t, func() {
		_ = computeLinkageEdges(nodes, framework.ForeignContext{})
		_ = buildLinkageEdges(nodes, framework.ForeignContext{})
	})
	assert.True(t, framework.ForeignContext{}.IsEmpty())
}

// TestLinkage_TypesAreNotInTheOwnGraphVocabulary re-states R5-c from this side:
// the linkage types are counted separately from the 33 precisely because
// they are a different mechanism reaching a different graph.
func TestLinkage_TypesAreNotInTheOwnGraphVocabulary(t *testing.T) {
	own := set(EmittedEdgeTypes())
	for _, typ := range linkageEdgeTypes() {
		assert.False(t, own[typ],
			"%s is a cross-graph edge and must not be claimed as own-graph vocabulary", typ)
	}
	assert.Len(t, linkageEdgeTypes(), 2)
}
