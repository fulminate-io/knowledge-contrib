// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// linkage_emission_test.go — THE ROUND TRIP THE RED-PENDING ROWS WAITED ON,
// split from linkage_test.go, which keeps the per-shape PREDICATE rows.
//
// THE CUT IS BETWEEN TWO DIFFERENT SUBJECTS. A predicate row asks what this
// collector decides from its own nodes and the declared context; an emission
// row asks what leaves the walk and whether it names the graph family its far
// endpoint lives in. They were one file while the emission was held and there
// was nothing to assert on that side.

// === THE EMISSION HALF, WHICH WAS PENDING AND IS NOW REAL ===

// TestLinkage_FourShapesReachTheResultThroughTheGraphFamilyFields is the round
// trip: the predicates run, the edges are built, and they leave the walk
// carrying the family their far endpoint lives in — in the field that matches
// WHICH END is foreign.
//
// THE PER-SHAPE FIELD IS PART OF THE EXPECTATION, not just the family. Three
// shapes put the k8s node on the near side and name the family in TargetGraph;
// DEPLOYS runs FROM the chart file node, so its family is in SourceGraph and its
// TargetGraph is empty. A row that only asserted "the family is code" would pass
// on an edge whose two fields were swapped, which is a relationship pointing the
// other way.
func TestLinkage_FourShapesReachTheResultThroughTheGraphFamilyFields(t *testing.T) {
	nodes, fc := fullLinkageFixture()
	edges := buildLinkageEdges(nodes, fc)

	want := []struct {
		from, to, typ, sourceFamily, targetFamily string
	}{
		{"charts/api/Chart.yaml", "prod/Deployment/api-server", edgeDeploys, graphFamilyCode, ""},
		{"prod/ServiceAccount/irsa-sa", "arn:aws:iam::123456789012:role/api-role",
			edgeWorkloadIdentity, "", familyAWS},
		{"prod/ServiceAccount/gcp-sa",
			"projects/fulminate-services/serviceAccounts/api@fulminate-services.iam.gserviceaccount.com",
			edgeWorkloadIdentity, "", familyGCP},
		{"prod/ServiceAccount/azure-sa",
			"/subscriptions/sub-abc/resourceGroups/rg/providers/identity/api-identity",
			edgeWorkloadIdentity, "", familyAzure},
	}
	require.Len(t, edges, len(want), "all four shapes are emitted, and no fifth exists")

	for _, w := range want {
		found := false
		for _, e := range edges {
			if e.FromID == w.from && e.ToID == w.to && e.Type == w.typ &&
				e.SourceGraph == w.sourceFamily && e.TargetGraph == w.targetFamily {
				found = true
			}
		}
		assert.True(t, found, "%s from %s to %s (source_graph %q, target_graph %q)",
			w.typ, w.from, w.to, w.sourceFamily, w.targetFamily)
	}

	// EVERY EMITTED LINKAGE EDGE NAMES EXACTLY ONE FAMILY, IN EXACTLY ONE FIELD.
	// Naming none would make it an ordinary in-graph edge to an id nothing in the
	// k8s graph resolves; naming both is refused by the client's collect pass,
	// because one resolution reaches one foreign family.
	for _, e := range edges {
		named := 0
		for _, family := range []string{e.SourceGraph, e.TargetGraph} {
			if family == "" {
				continue
			}
			named++
			assert.Contains(t, []string{graphFamilyCode, familyAWS, familyGCP, familyAzure}, family,
				"the field names a FAMILY, never a graph instance")
			assert.NotContains(t, []string{"cloud", "logs"}, family,
				"and never a RETIRED family: the three workload-identity shapes point at three "+
					"DIFFERENT providers' objects, which one cloud-wide family could not distinguish")
		}
		assert.Equal(t, 1, named,
			"edge %s -> %s (%s) names %d families; none makes it an in-graph edge to an id nothing "+
				"here resolves, and both is refused by the collect pass", e.FromID, e.ToID, e.Type, named)
	}
}

// TestLinkage_DeploysIsComputedAndEmittedThroughSourceGraph is the shape the
// contract could not carry and now can, and it is the INVERSE of the row that
// stood here: the predicate fires AND the edge leaves the walk.
//
// WHAT THE EMISSION MUST NOT DO, asserted rather than assumed. The two wrong
// forcings were reversing the edge — which asserts a relationship the built-in
// linker does not — and emitting it with no family, which lands an in-graph edge
// to a chart id nothing in the k8s graph resolves. Both are excluded here: the
// direction is pinned FROM the chart file node, and the family is pinned present
// in SourceGraph rather than absent.
func TestLinkage_DeploysIsComputedAndEmittedThroughSourceGraph(t *testing.T) {
	nodes, fc := fullLinkageFixture()

	computed := countLinkage(computeLinkageEdges(nodes, fc), edgeDeploys)
	assert.Positive(t, computed,
		"the predicate fires: the chart is matched and the relationship is computed")

	emitted := 0
	for _, e := range buildLinkageEdges(nodes, fc) {
		if e.Type != edgeDeploys {
			continue
		}
		emitted++
		assert.Equal(t, "charts/api/Chart.yaml", e.FromID,
			"the edge runs FROM the chart file node, the built-in linker's direction")
		assert.Equal(t, "prod/Deployment/api-server", e.ToID, "TO the Kubernetes node")
		assert.Equal(t, graphFamilyCode, e.SourceGraph,
			"the FOREIGN endpoint is the FROM, so the family belongs in source_graph")
		assert.Empty(t, e.TargetGraph,
			"and not in target_graph, which would send the client looking for the k8s node in the code graph")
	}
	assert.Equal(t, computed, emitted,
		"every computed DEPLOYS relationship reaches the result; the filter that discarded them is gone")
}

// TestLinkage_ChartNameComesFromTheFileBodyNotThePath. A chart directory is
// often named for the release rather than the chart, so a predicate reading the
// path matches the wrong chart or none.
func TestLinkage_ChartNameComesFromTheFileBodyNotThePath(t *testing.T) {
	fc := framework.ForeignContext{framework.FamilyCode: []framework.ForeignGraph{{
		GraphName: "infra",
		Nodes: []framework.ForeignNode{
			{
				ID:       "deploy/prod-release/Chart.yaml",
				FilePath: "deploy/prod-release/Chart.yaml",
				Content:  "apiVersion: v2\nname: api\nversion: 9.9.9\n",
			},
			// A SIBLING FILE THAT IS NOT A CHART AND CARRIES A TOP-LEVEL
			// `name:`. It is what makes the basename gate observable: without
			// it this file registers a chart that does not exist, and a
			// workload labeled for it would gain an edge to the wrong node.
			{
				ID:       "deploy/prod-release/values.yaml",
				FilePath: "deploy/prod-release/values.yaml",
				Content:  "name: not-a-chart\nreplicaCount: 2\n",
			},
		},
	}}}
	charts := chartFileNodes(fc)
	assert.Contains(t, charts, "api", "the name is parsed out of the body")
	assert.NotContains(t, charts, "prod-release", "the directory name is not the chart name")
	assert.NotContains(t, charts, "not-a-chart",
		"only a Chart.yaml or Chart.yml declares a chart; a sibling values file does not")
	assert.Len(t, charts, 1)
}

// TestLinkage_ChartNameParseIgnoresNestedKeys. A nested `name:` under an
// indented block belongs to a dependency or a maintainer, not to the chart, and
// what skips it is CutPrefix requiring the line to BEGIN with the key.
func TestLinkage_ChartNameParseIgnoresNestedKeys(t *testing.T) {
	body := "apiVersion: v2\nmaintainers:\n  - name: someone\nname: real-chart\nversion: 1.0.0\n"
	assert.Equal(t, "real-chart", chartNameFromBody(body),
		"the nested maintainer name is not mistaken for the chart's")

	// A body whose ONLY name is nested yields nothing rather than the wrong
	// name: an empty result suppresses the family, a wrong one wires the chart
	// to the wrong workload.
	assert.Empty(t, chartNameFromBody("maintainers:\n  - name: someone\n"))
	assert.Empty(t, chartNameFromBody("dependencies:\n  name: subchart\n"))
	assert.Empty(t, chartNameFromBody("#name: commented-out\n"))
}

// TestLinkage_TheWalkThreadsTheDeclaredContextToTheLinkPhase closes the gap
// every other row in this file leaves open.
//
// THE OTHER ROWS CALL buildLinkageEdges DIRECTLY, so a walk that accepted the
// declared context and then dropped it on the way to phase four would pass all
// of them. This drives the WHOLE walk with a real fixture and a real context
// and asserts the linkage edges arrive in the result.
func TestLinkage_TheWalkThreadsTheDeclaredContextToTheLinkPhase(t *testing.T) {
	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: "api-sa", Namespace: "prod",
			Annotations: map[string]string{annotationIRSARoleARN: "arn:aws:iam::123456789012:role/api-role"},
		},
	}
	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "api-server", Namespace: "prod",
			// THE HELM LABEL IS WHAT MAKES DEPLOYS OBSERVABLE HERE. Without it the
			// chart below matches nothing and the shape whose foreign endpoint is
			// the FROM never reaches this assertion.
			Labels: map[string]string{"app.kubernetes.io/name": "api"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "api", Image: "ghcr.io/org/api:v1"}}}},
		},
	}

	// THE DECLARED SLICE CARRIES A CHART FILE NODE, not just a graph name: the
	// DEPLOYS predicate parses the chart name out of a Chart.yaml BODY, so a
	// name-only declaration cannot exercise it end to end.
	fc := framework.ForeignContext{framework.FamilyCode: []framework.ForeignGraph{
		{GraphName: "infra", Nodes: []framework.ForeignNode{{
			ID:       "charts/api/Chart.yaml",
			Type:     "file",
			FilePath: "charts/api/Chart.yaml",
			Content:  "apiVersion: v2\nname: api\nversion: 1.2.3\n",
		}}},
	}}
	res, err := walkCluster(t.Context(),
		newFakeBundle(t, []runtime.Object{sa, dep}, nil, emptyDynamic()), fc)
	require.NoError(t, err)

	// THE SELECTION READS BOTH FIELDS. Filtering on TargetGraph alone would make
	// the DEPLOYS edge invisible to the very assertion meant to observe it.
	var linkage []Edge
	for _, e := range res.Edges {
		if e.SourceGraph != "" || e.TargetGraph != "" {
			linkage = append(linkage, e)
		}
	}
	require.NotEmpty(t, linkage, "the declared context reached phase four")

	assert.True(t, hasEdge(res.Edges, "prod/ServiceAccount/api-sa",
		"arn:aws:iam::123456789012:role/api-role", edgeWorkloadIdentity),
		"the identity shape needs no declaration and rides the same phase")

	// DEPLOYS END TO END THROUGH THE REAL CHILD: the chart node in the declared
	// slice, the Helm label off the live object, the direction, and the family in
	// the field that matches which end is foreign.
	deploys := 0
	for _, e := range linkage {
		if e.Type != edgeDeploys {
			continue
		}
		deploys++
		assert.Equal(t, "charts/api/Chart.yaml", e.FromID)
		assert.Equal(t, "prod/Deployment/api-server", e.ToID)
		assert.Equal(t, graphFamilyCode, e.SourceGraph)
		assert.Empty(t, e.TargetGraph)
	}
	assert.Equal(t, 1, deploys,
		"the Helm relationship leaves the whole walk exactly once, through the real child rather than through a direct call to buildLinkageEdges")

	// SAME-RUN CONTROL: the identical walk with an EMPTY context emits the
	// identity shape (which needs no declaration) and NOT the DEPLOYS shape
	// (which does). That is what proves the context is read rather than ignored,
	// and DEPLOYS is the shape that can say so: it is the only EMITTED shape
	// left that reads the declared block at all, since IRSA and GCP compose
	// their targets locally and the Azure one is not in this fixture.
	empty, err := walkCluster(t.Context(),
		newFakeBundle(t, []runtime.Object{sa, dep}, nil, emptyDynamic()), framework.ForeignContext{})
	require.NoError(t, err)
	assert.False(t, hasEdge(empty.Edges, "charts/api/Chart.yaml", "prod/Deployment/api-server", edgeDeploys),
		"with nothing declared there is no chart file node, so the Helm label on the live "+
			"object matches nothing — the label alone does not manufacture an edge")
	assert.True(t, hasEdge(empty.Edges, "prod/ServiceAccount/api-sa",
		"arn:aws:iam::123456789012:role/api-role", edgeWorkloadIdentity),
		"and the shape that composes its target locally still emits")
}
