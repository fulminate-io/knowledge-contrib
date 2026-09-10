// SPDX-License-Identifier: Apache-2.0

package main

import (
	"sort"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// vocabulary.go — the NODE AND EDGE VOCABULARY this collector writes into its
// own graph family, as two exact sets rather than two counts.
//
// THE FLOOR IS 32 FIXED KINDS PLUS EVERY SERVED CRD KIND, AND EXACTLY 33 EDGE
// TYPES. Both lists are transcribed from a source census of the built-in
// Kubernetes collector, never from a stored graph, because a stored graph's
// vocabulary drifts behind the source that writes it and a graph-derived
// expectation would encode that drift as a requirement.
//
// WHAT IS DELIBERATELY ABSENT. WORKLOAD_IDENTITY is declared in the built-in
// vocabulary and has no producer in it. This collector emits it too, but as a
// LINKAGE edge naming another graph rather than as its own-graph vocabulary, so
// it is not in the set below; see linkage.go. Folding it in here would claim a
// cross-graph mechanism as an in-graph one.

// Node and Edge are the contract shapes the framework encodes. They are aliased
// rather than re-declared so a walk builds the framework's own values and no
// conversion layer can drift from the contract.
type (
	Node = framework.Node
	Edge = framework.Edge
)

// nodeTypeCloudResource is the node type every Kubernetes object this collector
// produces carries. It matches the built-in collector's own node type, so a
// consumer reading either graph reads one type string.
const nodeTypeCloudResource = "cloud-resource"

// nodeTypeProxy is the node type of a proxy: a lightweight, deterministically
// identified reference to a node that lives in another graph. See proxy.go.
const nodeTypeProxy = "proxy"

// The 33 edge types, one constant each, with the wire string the built-in
// vocabulary declares. They are grouped by WHICH MECHANISM produces them,
// because the three groups are tested differently: the walk emits the first
// group directly from an API object, the second group is derived from the
// walk's own output before it is returned, and the third terminates on a proxy
// node this collector mints into its own result.
const (
	// Emitted by the walk itself, 18.
	edgeBacks            = "BACKS"
	edgeBindsRole        = "BINDS_ROLE"
	edgeBindsSubject     = "BINDS_SUBJECT"
	edgeBoundTo          = "BOUND_TO"
	edgeHasEndpointSlice = "HAS_ENDPOINT_SLICE"
	edgeIssuedBy         = "ISSUED_BY"
	edgeMountsConfigMap  = "MOUNTS_CONFIGMAP"
	edgeMountsSecret     = "MOUNTS_SECRET"
	edgeOwnedBy          = "OWNED_BY"
	edgeReferencesStore  = "REFERENCES_STORE"
	edgeRoutesTo         = "ROUTES_TO"
	edgeRunsOn           = "RUNS_ON"
	edgeScales           = "SCALES"
	edgeTargets          = "TARGETS"
	edgeUsesMiddleware   = "USES_MIDDLEWARE"
	edgeUsesPVC          = "USES_PVC"
	edgeUsesSA           = "USES_SA"
	edgeUsesStorageClass = "USES_STORAGE_CLASS"

	// Derived from the walk's own output, reading only this collector's own
	// graph, 9.
	edgeAllowsEgressTo    = "ALLOWS_EGRESS_TO"
	edgeAllowsIngressFrom = "ALLOWS_INGRESS_FROM"
	edgeANPEgressTo       = "ANP_EGRESS_TO"
	edgeANPIngressFrom    = "ANP_INGRESS_FROM"
	edgeInNamespace       = "IN_NAMESPACE"
	edgeRestrictsEgress   = "RESTRICTS_EGRESS"
	edgeRestrictsIngress  = "RESTRICTS_INGRESS"
	edgeSelects           = "SELECTS"
	edgeUsesImage         = "USES_IMAGE"

	// Terminating on a proxy node minted into this collector's own result, 6.
	edgeAssumesIdentity = "ASSUMES_IDENTITY"
	edgeBackedByVM      = "BACKED_BY_VM"
	edgeConnectsTo      = "CONNECTS_TO"
	edgeExposedBy       = "EXPOSED_BY"
	edgeRunsInCluster   = "RUNS_IN_CLUSTER"
	edgeUsesDisk        = "USES_DISK"
)

// The two linkage edge types. THEY ARE NOT IN THE 33: each names an endpoint in
// ANOTHER graph and is emitted through the contract's target-graph field, so it
// reaches the linkage resolution rather than this collector's own graph.
const (
	edgeDeploys          = "DEPLOYS"
	edgeWorkloadIdentity = "WORKLOAD_IDENTITY"
)

// emittedEdgeTypes is the exact 33-member own-graph vocabulary.
var emittedEdgeTypes = []string{
	edgeBacks, edgeBindsRole, edgeBindsSubject, edgeBoundTo, edgeHasEndpointSlice,
	edgeIssuedBy, edgeMountsConfigMap, edgeMountsSecret, edgeOwnedBy,
	edgeReferencesStore, edgeRoutesTo, edgeRunsOn, edgeScales, edgeTargets,
	edgeUsesMiddleware, edgeUsesPVC, edgeUsesSA, edgeUsesStorageClass,

	edgeAllowsEgressTo, edgeAllowsIngressFrom, edgeANPEgressTo, edgeANPIngressFrom,
	edgeInNamespace, edgeRestrictsEgress, edgeRestrictsIngress, edgeSelects,
	edgeUsesImage,

	edgeAssumesIdentity, edgeBackedByVM, edgeConnectsTo, edgeExposedBy,
	edgeRunsInCluster, edgeUsesDisk,
}

// fixedKinds is the 32 resource kinds this collector always enumerates. Every
// other kind it produces is a CustomResource, whose kind is discovered at walk
// time and cannot be listed here.
var fixedKinds = []string{
	"AdminNetworkPolicy", "BaselineAdminNetworkPolicy", "ClusterRole",
	"ClusterRoleBinding", "ConfigMap", "CronJob", "CustomResourceDefinition",
	"DaemonSet", "Deployment", "EndpointSlice", "GRPCRoute", "Gateway",
	"GatewayClass", "HTTPRoute", "HorizontalPodAutoscaler", "Ingress", "Job",
	"Namespace", "NetworkPolicy", "Node", "PersistentVolume",
	"PersistentVolumeClaim", "Pod", "PodDisruptionBudget", "ReplicaSet", "Role",
	"RoleBinding", "Secret", "Service", "ServiceAccount", "StatefulSet",
	"StorageClass",
}

// FixedKinds returns the 32 resource kinds this collector always enumerates, in
// sorted order. It returns a copy: the registered set is this module's parity
// declaration and a caller must not be able to edit it.
func FixedKinds() []string {
	out := append([]string(nil), fixedKinds...)
	sort.Strings(out)
	return out
}

// EmittedEdgeTypes returns the exact set of edge types this collector emits into
// its OWN graph, in sorted order, as a copy.
//
// The linkage types are deliberately absent; see [linkageEdgeTypes].
func EmittedEdgeTypes() []string {
	out := append([]string(nil), emittedEdgeTypes...)
	sort.Strings(out)
	return out
}

// linkageEdgeTypes returns the edge types this collector computes for ANOTHER
// graph, as a copy. They are counted separately from the 33 on purpose: they
// travel through the contract's target-graph field and are resolved against a
// foreign graph, which is a different mechanism with a different failure mode.
func linkageEdgeTypes() []string {
	return []string{edgeDeploys, edgeWorkloadIdentity}
}
