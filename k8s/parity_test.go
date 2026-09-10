// SPDX-License-Identifier: Apache-2.0

package main

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// parity_test.go — R5-a, R5-b, R5-c and R5-d: the PARITY FLOOR as an exact set
// assertion rather than a count.
//
// The floor is 32 fixed kinds plus every served CRD kind, and exactly 33 edge
// types. Both lists below are transcribed from a SOURCE census of the built-in
// collector, never from a stored graph: the live GKE graph's edge vocabulary is
// stale (it carries 271 RESTRICTS edges, a spelling with zero occurrences in
// the tree), so a graph-derived expectation would encode the staleness.
//
// THE EDGE ASSERTION IS AN EXACT SET, NOT A SUBSET. An EXTRA type reds as
// readily as a missing one, because a subset assertion would pass a module that
// folded the cross-graph types (DEPLOYS, WORKLOAD_IDENTITY) into its own
// vocabulary — which is a different mechanism reaching a different graph, and is
// asserted separately in linkage_test.go.

// wantFixedKinds is the 32 fixed resource kinds, censused from the parity
// source at 45a0f91ba: 28 quoted ResourceType literals over its 81 non-test
// files, plus 4 supplied as expressions (HTTPRoute and GRPCRoute from
// sub_gatewayapi.go, AdminNetworkPolicy and BaselineAdminNetworkPolicy from
// sub_adminnetworkpolicies.go's table).
var wantFixedKinds = []string{
	"AdminNetworkPolicy",
	"BaselineAdminNetworkPolicy",
	"ClusterRole",
	"ClusterRoleBinding",
	"ConfigMap",
	"CronJob",
	"CustomResourceDefinition",
	"DaemonSet",
	"Deployment",
	"EndpointSlice",
	"GRPCRoute",
	"Gateway",
	"GatewayClass",
	"HTTPRoute",
	"HorizontalPodAutoscaler",
	"Ingress",
	"Job",
	"Namespace",
	"NetworkPolicy",
	"Node",
	"PersistentVolume",
	"PersistentVolumeClaim",
	"Pod",
	"PodDisruptionBudget",
	"ReplicaSet",
	"Role",
	"RoleBinding",
	"Secret",
	"Service",
	"ServiceAccount",
	"StatefulSet",
	"StorageClass",
}

// wantEdgeTypes is the 33 edge types, censused from the parity source at
// 45a0f91ba as the 34 distinct kgtypes.Edge* identifiers less the type name
// kgtypes.EdgeType. Their wire strings are read from
// cmd/knowledge/internal/kgtypes/edge_types_cloud.go.
//
// The census's own composition, recorded so a later reader can re-derive it:
// 18 emitted by the walk, 9 by post-populate passes reading only this
// collector's own graph, and 6 terminating on a proxy node this collector mints
// into its own graph.
var wantEdgeTypes = []string{
	// 18 walk-side.
	"BACKS", "BINDS_ROLE", "BINDS_SUBJECT", "BOUND_TO", "HAS_ENDPOINT_SLICE",
	"ISSUED_BY", "MOUNTS_CONFIGMAP", "MOUNTS_SECRET", "OWNED_BY",
	"REFERENCES_STORE", "ROUTES_TO", "RUNS_ON", "SCALES", "TARGETS",
	"USES_MIDDLEWARE", "USES_PVC", "USES_SA", "USES_STORAGE_CLASS",
	// 9 own-graph derived.
	"ALLOWS_EGRESS_TO", "ALLOWS_INGRESS_FROM", "ANP_EGRESS_TO",
	"ANP_INGRESS_FROM", "IN_NAMESPACE", "RESTRICTS_EGRESS",
	"RESTRICTS_INGRESS", "SELECTS", "USES_IMAGE",
	// 6 proxy-terminating.
	"ASSUMES_IDENTITY", "BACKED_BY_VM", "CONNECTS_TO", "EXPOSED_BY",
	"RUNS_IN_CLUSTER", "USES_DISK",
}

func TestParity_FixedKindsArePresent(t *testing.T) {
	got := FixedKinds()
	require.Len(t, got, 32, "the parity floor is 32 fixed kinds plus every served CRD kind")

	set := make(map[string]bool, len(got))
	for _, k := range got {
		assert.False(t, set[k], "kind %q is registered twice", k)
		set[k] = true
	}
	for _, want := range wantFixedKinds {
		assert.True(t, set[want], "fixed kind %q is missing from the registered set", want)
	}
}

func TestParity_EdgeTypeSetIsExactlyTheThirtyThree(t *testing.T) {
	got := append([]string(nil), EmittedEdgeTypes()...)
	want := append([]string(nil), wantEdgeTypes...)
	sort.Strings(got)
	sort.Strings(want)

	// ElementsMatch reds on a MISSING member and on an EXTRA one alike, which
	// is the whole point: a subset assertion would pass a module that folded
	// the two cross-graph types into its own vocabulary.
	assert.ElementsMatch(t, want, got,
		"the emitted edge-type set must be EXACTLY the 33 the parity source produces")
	assert.Len(t, got, 33)
}

// TestParity_WorkloadIdentityIsNotInTheOwnVocabulary is R5-c. WORKLOAD_IDENTITY
// is declared in kgtypes and has ZERO producers in the parity source; this
// module emits it only as a LINKER edge into another graph, through the
// contract's target-graph field. Folding it into this set would claim it as
// own-graph vocabulary, which is a different mechanism entirely.
func TestParity_WorkloadIdentityIsNotInTheOwnVocabulary(t *testing.T) {
	set := make(map[string]bool)
	for _, e := range EmittedEdgeTypes() {
		set[e] = true
	}
	assert.False(t, set["WORKLOAD_IDENTITY"],
		"WORKLOAD_IDENTITY is a linkage edge into another graph, not this collector's own vocabulary")
	// The control that keeps the row from being a ban on a wire string:
	// ASSUMES_IDENTITY is the own-graph proxy family and IS in the set.
	assert.True(t, set["ASSUMES_IDENTITY"],
		"ASSUMES_IDENTITY is the own-graph proxy family and belongs in the set")
}

// TestParity_UsesImageIsInTheSetAndProducesNoEdge is R5-g. USES_IMAGE is in the
// floor and its producer is reachable, but its registry index is built from
// ECR, ACR and Artifact Registry nodes, which a Kubernetes graph never holds —
// so it is asserted PRESENT in the type set and never asserted to produce an
// edge.
func TestParity_UsesImageIsInTheSetAndProducesNoEdge(t *testing.T) {
	set := make(map[string]bool)
	for _, e := range EmittedEdgeTypes() {
		set[e] = true
	}
	require.True(t, set["USES_IMAGE"], "USES_IMAGE is in the 33")

	nodes := []Node{
		workloadNode("default/Deployment/api", map[string]string{
			"images": "123456789012.dkr.ecr.us-east-1.amazonaws.com/api:v1",
		}),
	}
	assert.Empty(t, buildImageLineageEdges(nodes),
		"a Kubernetes graph holds no registry repo nodes, so the index is empty and no USES_IMAGE edge is produced")
}

func TestResourceID_NamespacedAndClusterScoped(t *testing.T) {
	assert.Equal(t, "default/Pod/api", resourceID("default", "Pod", "api"))
	assert.Equal(t, "Node/ip-10-0-0-1", resourceID("", "Node", "ip-10-0-0-1"),
		"a cluster-scoped id is Kind/name with no leading separator")
}

func TestLabelsToMeta_PrefixesEveryKey(t *testing.T) {
	got := labelsToMeta(map[string]string{"app.kubernetes.io/name": "api", "tier": "web"})
	assert.Equal(t, map[string]string{
		"label/app.kubernetes.io/name": "api",
		"label/tier":                   "web",
	}, got)
}
