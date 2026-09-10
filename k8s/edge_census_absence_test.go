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

// edge_census_absence_test.go — THE CENSUS'S LOCKING DIRECTION: the edge type
// this collector must produce none of, under any input. It is a sibling of
// edge_census_test.go rather than a section in it because that file is at the
// length gate's ceiling.
//
// WHY AN ABSENCE NEEDS A ROW OF ITS OWN. Every row in the census proper is an
// emit-and-suppress PAIR: the family emits under its gate and stays silent
// otherwise. This one says a family emits under NO condition, which no pair can
// express — and the completeness check next door cannot carry it either,
// because that check reads the DECLARED vocabulary and the whole point here is
// that the type is not declared.

// retiredEdgeBuilds is the wire string of the edge type this collector does NOT
// emit. It is a LITERAL rather than a reference to a vocabulary constant on
// purpose: a constant would have been deleted along with the emission, and an
// assertion written against it would then be about nothing.
const retiredEdgeBuilds = "BUILDS"

// TestEdgeCensus_BuildsIsNeitherDeclaredNorEmitted.
//
// THE FIXTURE IS THE ONE THAT USED TO PRODUCE THE EDGE: a workload whose image
// basename matches the NAME of a code graph carried in the declared context. It
// is driven through the WHOLE walk, so a predicate re-added anywhere in the
// linkage phase reaches this assertion rather than only a re-added call site.
//
// WHY THE EDGE IS GONE, so the row is not read as an arbitrary ban: its far
// endpoint was a code graph's NAME, the client resolves a cross-graph endpoint
// by looking for a NODE carrying that id, and a code graph holds no node
// denoting a repository — so the edge resolves against nothing and the resolver
// fails the entire collect rather than dropping one edge.
func TestEdgeCensus_BuildsIsNeitherDeclaredNorEmitted(t *testing.T) {
	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api-server", Namespace: "prod"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{
				Containers: []corev1.Container{{Name: "api", Image: "ghcr.io/org/api:v1"}}}},
		},
	}
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
		Name: "api-sa", Namespace: "prod",
		Annotations: map[string]string{annotationIRSARoleARN: "arn:aws:iam::123456789012:role/api-role"},
	}}

	// The context that used to make the image basename match: a code graph
	// named for the image's own repository.
	fc := framework.ForeignContext{framework.FamilyCode: []framework.ForeignGraph{{GraphName: "api"}}}
	res, err := walkCluster(t.Context(),
		newFakeBundle(t, []runtime.Object{dep, sa}, nil, emptyDynamic()), fc)
	require.NoError(t, err)

	// FIXTURE CONTROL, SAME RUN: the walk ran and its linkage phase IS emitting,
	// so the zero below is a decision rather than a walk that produced nothing.
	require.True(t, hasEdge(res.Edges, "prod/ServiceAccount/api-sa",
		"arn:aws:iam::123456789012:role/api-role", edgeWorkloadIdentity),
		"fixture control: the linkage phase ran and emitted the identity shape")

	assert.Zero(t, countEdges(res.Edges, retiredEdgeBuilds),
		"no BUILDS edge leaves the walk, on the very input that used to produce one")

	// KNOWN POSITIVE ON THE INSTRUMENT, over the SAME slice: the counter finds a
	// BUILDS edge when one is present, so the zero above is an observation
	// rather than a counter that can only ever return zero.
	assert.Equal(t, 1, countEdges(append(res.Edges, Edge{
		FromID: "prod/Deployment/api-server", ToID: "api", Type: retiredEdgeBuilds,
	}), retiredEdgeBuilds), "known positive: this counter sees a BUILDS edge in this exact slice")

	// THE DECLARATION HALF. Neither vocabulary claims the type, so nothing
	// downstream expects an edge no branch produces.
	assert.NotContains(t, EmittedEdgeTypes(), retiredEdgeBuilds,
		"BUILDS is not this collector's own-graph vocabulary")
	assert.NotContains(t, linkageEdgeTypes(), retiredEdgeBuilds,
		"and it is not claimed as a linkage type either")
	assert.Len(t, linkageEdgeTypes(), 2,
		"two linkage types remain: DEPLOYS, which is computed and held, and WORKLOAD_IDENTITY")
}
