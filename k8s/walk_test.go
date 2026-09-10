// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8stesting "k8s.io/client-go/testing"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// walk_test.go — R5-d and the WALK-SIDE half of R5-h: the per-kind converters,
// and the emit-and-suppress pair for every walk-side family that carries a gate
// a naive fixture would never construct.
//
// EVERY GATED FAMILY GETS BOTH ARMS IN THE SAME RUN. The emitting fixture is
// the row and the suppressed fixture is its control: a family whose guard was
// deleted starts emitting on the suppressed fixture, and a family that never
// worked emits nothing on the emitting one. One arm alone cannot tell those
// apart.

func runWalk(t *testing.T, c clientBundle) ([]Node, []Edge, string) {
	t.Helper()
	res, err := walkCluster(context.Background(), c, framework.ForeignContext{})
	require.NoError(t, err)
	complete := ""
	if !res.Complete.IsComplete() {
		complete = res.Complete.Reason()
	}
	return res.Nodes, res.Edges, complete
}

func nodeByID(t *testing.T, nodes []Node, id string) Node {
	t.Helper()
	for _, n := range nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("no node with id %q; got %d nodes", id, len(nodes))
	return Node{}
}

func hasEdge(edges []Edge, from, to, typ string) bool {
	for _, e := range edges {
		if e.FromID == from && e.ToID == to && e.Type == typ {
			return true
		}
	}
	return false
}

func countEdges(edges []Edge, typ string) int {
	n := 0
	for _, e := range edges {
		if e.Type == typ {
			n++
		}
	}
	return n
}

// TestWalk_PodConverter is R5-d: the id under both scheme arms, the name, the
// label prefix, and the metadata the derivations read back.
func TestWalk_PodConverter(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "api-abc",
			Namespace:   "prod",
			Labels:      map[string]string{"app": "api", "tier": "web"},
			Annotations: map[string]string{"kubernetes.io/config.source": "file"},
		},
		Spec: corev1.PodSpec{
			NodeName:           "node-1",
			ServiceAccountName: "api-sa",
			Containers:         []corev1.Container{{Name: "api", Image: "ghcr.io/org/api:v1"}},
		},
		Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.1.2.3"},
	}
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-1"}}

	nodes, edges, _ := runWalk(t, newFakeBundle(t, []runtime.Object{pod, node}, nil, emptyDynamic()))

	got := nodeByID(t, nodes, "prod/Pod/api-abc")
	assert.Equal(t, nodeTypeCloudResource, got.Type)
	assert.Equal(t, "api-abc", got.SymbolName)
	assert.Equal(t, "Pod", got.Metadata["resource_type"])
	assert.Equal(t, "prod", got.Metadata["namespace"])
	assert.Equal(t, "api", got.Metadata["label/app"], "labels carry the label/ prefix")
	assert.Equal(t, "file", got.Metadata["annotation/kubernetes.io/config.source"],
		"annotations carry the annotation/ prefix")
	assert.Equal(t, "ghcr.io/org/api:v1", got.Metadata["images"])
	assert.NotEmpty(t, got.Content, "the object body is the node's content")

	// The cluster-scoped arm of the id scheme, in the same run.
	clusterScoped := nodeByID(t, nodes, "Node/node-1")
	assert.Equal(t, "Node", clusterScoped.Metadata["resource_type"])
	assert.Empty(t, clusterScoped.Metadata["namespace"], "a cluster-scoped node carries no namespace metadata")

	assert.True(t, hasEdge(edges, "prod/Pod/api-abc", "Node/node-1", edgeRunsOn))
	assert.True(t, hasEdge(edges, "prod/Pod/api-abc", "prod/ServiceAccount/api-sa", edgeUsesSA))
}

// TestWalk_RunsOnSuppressedForUnscheduledPod is the RUNS_ON control.
func TestWalk_RunsOnSuppressedForUnscheduledPod(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "pending", Namespace: "prod"},
		Spec:       corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "img"}}},
	}
	_, edges, _ := runWalk(t, newFakeBundle(t, []runtime.Object{pod}, nil, emptyDynamic()))
	assert.Zero(t, countEdges(edges, edgeRunsOn), "an unscheduled pod names no node")
}

// TestWalk_PodTemplateEdges covers the four mount families and their dedupe.
func TestWalk_PodTemplateEdges(t *testing.T) {
	replicas := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					ServiceAccountName: "api-sa",
					Volumes: []corev1.Volume{
						{Name: "cfg", VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "api-config"}}}},
						{Name: "sec", VolumeSource: corev1.VolumeSource{
							Secret: &corev1.SecretVolumeSource{SecretName: "api-secret"}}},
						{Name: "data", VolumeSource: corev1.VolumeSource{
							PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "api-data"}}},
					},
					Containers: []corev1.Container{{
						Name:  "api",
						Image: "ghcr.io/org/api:v1",
						EnvFrom: []corev1.EnvFromSource{{
							// THE SAME SECRET AS THE VOLUME ABOVE: one
							// relationship, and the dedupe is what keeps it
							// from being reported as two.
							SecretRef: &corev1.SecretEnvSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "api-secret"}}}},
					}},
				},
			},
		},
	}

	_, edges, _ := runWalk(t, newFakeBundle(t, []runtime.Object{dep}, nil, emptyDynamic()))
	const from = "prod/Deployment/api"
	assert.True(t, hasEdge(edges, from, "prod/ServiceAccount/api-sa", edgeUsesSA))
	assert.True(t, hasEdge(edges, from, "prod/ConfigMap/api-config", edgeMountsConfigMap))
	assert.True(t, hasEdge(edges, from, "prod/Secret/api-secret", edgeMountsSecret))
	assert.True(t, hasEdge(edges, from, "prod/PersistentVolumeClaim/api-data", edgeUsesPVC))
	assert.Equal(t, 1, countEdges(edges, edgeMountsSecret),
		"a secret named by both a volume and an envFrom is ONE relationship")
}

// TestWalk_StatefulSetVolumeClaimTemplates covers the claims that are not in
// the pod template at all.
func TestWalk_StatefulSetVolumeClaimTemplates(t *testing.T) {
	replicas := int32(2)
	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "prod"},
		Spec: appsv1.StatefulSetSpec{
			Replicas:    &replicas,
			ServiceName: "db",
			Selector:    &metav1.LabelSelector{MatchLabels: map[string]string{"app": "db"}},
			Template:    corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "db", Image: "postgres:17"}}}},
			VolumeClaimTemplates: []corev1.PersistentVolumeClaim{
				{ObjectMeta: metav1.ObjectMeta{Name: "data"}},
			},
		},
	}
	_, edges, _ := runWalk(t, newFakeBundle(t, []runtime.Object{sts}, nil, emptyDynamic()))
	assert.True(t, hasEdge(edges, "prod/StatefulSet/db", "prod/PersistentVolumeClaim/data-db-0", edgeUsesPVC))
	assert.True(t, hasEdge(edges, "prod/StatefulSet/db", "prod/PersistentVolumeClaim/data-db-1", edgeUsesPVC))
}

// TestWalk_EndpointSliceGates is R5-h for the two EndpointSlice families, both
// arms of both gates in one run.
func TestWalk_EndpointSliceGates(t *testing.T) {
	ready := true
	labeled := &discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "api-xyz",
			Namespace: "prod",
			Labels:    map[string]string{discoveryv1.LabelServiceName: "api"},
		},
		AddressType: discoveryv1.AddressTypeIPv4,
		Endpoints: []discoveryv1.Endpoint{
			{TargetRef: &corev1.ObjectReference{Kind: "Pod", Name: "api-1"}, Conditions: discoveryv1.EndpointConditions{Ready: &ready}},
			// A targetRef naming something that is NOT a Pod backs nothing this
			// graph holds.
			{TargetRef: &corev1.ObjectReference{Kind: "Node", Name: "node-1"}},
			// A nil targetRef backs nothing.
			{Addresses: []string{"10.0.0.9"}},
		},
	}
	unlabelled := &discoveryv1.EndpointSlice{
		ObjectMeta:  metav1.ObjectMeta{Name: "orphan", Namespace: "prod"},
		AddressType: discoveryv1.AddressTypeIPv4,
	}

	_, edges, _ := runWalk(t, newFakeBundle(t, []runtime.Object{labeled, unlabelled}, nil, emptyDynamic()))

	assert.True(t, hasEdge(edges, "prod/Service/api", "prod/EndpointSlice/api-xyz", edgeHasEndpointSlice))
	assert.Equal(t, 1, countEdges(edges, edgeHasEndpointSlice),
		"a slice with no kubernetes.io/service-name label belongs to no Service")
	assert.True(t, hasEdge(edges, "prod/EndpointSlice/api-xyz", "prod/Pod/api-1", edgeBacks))
	assert.Equal(t, 1, countEdges(edges, edgeBacks),
		"only a targetRef naming a Pod produces BACKS")
}

// TestWalk_RoleBindingGates is R5-h for BINDS_SUBJECT, whose gate is that the
// subject is a ServiceAccount, plus the cluster-scoped role-reference branch.
func TestWalk_RoleBindingGates(t *testing.T) {
	saBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "api-reader", Namespace: "prod"},
		RoleRef:    rbacv1.RoleRef{Kind: "Role", Name: "reader"},
		Subjects:   []rbacv1.Subject{{Kind: "ServiceAccount", Name: "api-sa", Namespace: "prod"}},
	}
	userBinding := &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "human-reader", Namespace: "prod"},
		// A ClusterRole reference from a NAMESPACED binding: the target id
		// carries NO namespace.
		RoleRef:  rbacv1.RoleRef{Kind: "ClusterRole", Name: "view"},
		Subjects: []rbacv1.Subject{{Kind: "User", Name: "someone@example.com"}},
	}

	_, edges, _ := runWalk(t, newFakeBundle(t, []runtime.Object{saBinding, userBinding}, nil, emptyDynamic()))

	assert.True(t, hasEdge(edges, "prod/RoleBinding/api-reader", "prod/Role/reader", edgeBindsRole))
	assert.True(t, hasEdge(edges, "prod/RoleBinding/api-reader", "prod/ServiceAccount/api-sa", edgeBindsSubject))

	// The cluster-scoped role reference from a namespaced binding.
	assert.True(t, hasEdge(edges, "prod/RoleBinding/human-reader", "ClusterRole/view", edgeBindsRole),
		"a ClusterRole reference is cluster-scoped even from a namespaced binding")
	assert.Equal(t, 1, countEdges(edges, edgeBindsSubject),
		"a User subject names no object in this cluster and produces no BINDS_SUBJECT")
}

// TestWalk_IngressRoutesTo covers ROUTES_TO from both the default backend and
// the rule paths, with its dedupe.
func TestWalk_IngressRoutesTo(t *testing.T) {
	pathType := networkingv1.PathTypePrefix
	ing := &networkingv1.Ingress{
		ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "prod"},
		Spec: networkingv1.IngressSpec{
			DefaultBackend: &networkingv1.IngressBackend{
				Service: &networkingv1.IngressServiceBackend{Name: "fallback"}},
			Rules: []networkingv1.IngressRule{{
				IngressRuleValue: networkingv1.IngressRuleValue{HTTP: &networkingv1.HTTPIngressRuleValue{
					Paths: []networkingv1.HTTPIngressPath{
						{Path: "/a", PathType: &pathType, Backend: networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{Name: "api"}}},
						{Path: "/b", PathType: &pathType, Backend: networkingv1.IngressBackend{
							Service: &networkingv1.IngressServiceBackend{Name: "api"}}},
					}}}}},
		},
	}
	_, edges, _ := runWalk(t, newFakeBundle(t, []runtime.Object{ing}, nil, emptyDynamic()))
	assert.True(t, hasEdge(edges, "prod/Ingress/web", "prod/Service/fallback", edgeRoutesTo))
	assert.True(t, hasEdge(edges, "prod/Ingress/web", "prod/Service/api", edgeRoutesTo))
	assert.Equal(t, 2, countEdges(edges, edgeRoutesTo),
		"two paths to the same Service state one route")
}

// TestWalk_StorageEdges covers BOUND_TO and both USES_STORAGE_CLASS producers,
// with the pending-claim control.
func TestWalk_StorageEdges(t *testing.T) {
	sc := "fast"
	bound := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "prod"},
		Spec:       corev1.PersistentVolumeClaimSpec{VolumeName: "pv-1", StorageClassName: &sc},
	}
	pending := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "waiting", Namespace: "prod"},
		Spec:       corev1.PersistentVolumeClaimSpec{StorageClassName: &sc},
	}
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "pv-1"},
		Spec:       corev1.PersistentVolumeSpec{StorageClassName: sc},
	}

	_, edges, _ := runWalk(t, newFakeBundle(t, []runtime.Object{bound, pending, pv}, nil, emptyDynamic()))
	assert.True(t, hasEdge(edges, "prod/PersistentVolumeClaim/data", "PersistentVolume/pv-1", edgeBoundTo))
	assert.Equal(t, 1, countEdges(edges, edgeBoundTo), "a pending claim names no volume")
	assert.True(t, hasEdge(edges, "prod/PersistentVolumeClaim/data", "StorageClass/fast", edgeUsesStorageClass))
	assert.True(t, hasEdge(edges, "PersistentVolume/pv-1", "StorageClass/fast", edgeUsesStorageClass))
}

// TestWalk_OwnerReferences covers OWNED_BY including its cluster-scoped owner
// branch.
func TestWalk_OwnerReferences(t *testing.T) {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "api-abc", Namespace: "prod",
			OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: "api-rs"}},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "i"}}},
	}
	// THE CASE THE CLUSTER-SCOPED BRANCH EXISTS FOR: a NAMESPACED child with a
	// CLUSTER-SCOPED owner. The PersistentVolume below is a cluster-scoped
	// CHILD, whose own namespace is already empty, so the branch is a no-op
	// there and removing it left the row green. Here the child has a namespace
	// and the owner does not, so without the branch the edge points at
	// "prod/Node/ip-10-0-0-1" and dangles.
	mirrorPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: "static-pod", Namespace: "kube-system",
			OwnerReferences: []metav1.OwnerReference{{Kind: "Node", Name: "ip-10-0-0-1"}},
		},
		Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "i"}}},
	}
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name:            "pv-1",
			OwnerReferences: []metav1.OwnerReference{{Kind: "StorageClass", Name: "fast"}},
		},
	}
	_, edges, _ := runWalk(t, newFakeBundle(t, []runtime.Object{pod, mirrorPod, pv}, nil, emptyDynamic()))
	assert.True(t, hasEdge(edges, "prod/Pod/api-abc", "prod/ReplicaSet/api-rs", edgeOwnedBy))
	assert.True(t, hasEdge(edges, "PersistentVolume/pv-1", "StorageClass/fast", edgeOwnedBy),
		"a cluster-scoped owner's id carries no namespace")
	assert.True(t, hasEdge(edges, "kube-system/Pod/static-pod", "Node/ip-10-0-0-1", edgeOwnedBy),
		"a NAMESPACED child with a cluster-scoped owner points at an id with NO namespace; "+
			"building it in the child's namespace would dangle")
	assert.False(t, hasEdge(edges, "kube-system/Pod/static-pod", "kube-system/Node/ip-10-0-0-1", edgeOwnedBy),
		"and never at the namespaced spelling")
}

// TestWalk_EmptyClusterIsCompleteAndEmpty is the control every other row rests
// on: an empty cluster walks clean, asserts a COMPLETE enumeration, and
// fabricates nothing.
func TestWalk_EmptyClusterIsCompleteAndEmpty(t *testing.T) {
	nodes, edges, reason := runWalk(t, newFakeBundle(t, nil, nil, emptyDynamic()))
	assert.Empty(t, nodes)
	assert.Empty(t, edges)
	assert.Empty(t, reason, "an empty cluster is a COMPLETE walk that found nothing")
}

// TestWalk_ContinuationTokenOnATypedListingIsATruncation. These calls pass no
// page limit, so the API server returns everything it means to; a continuation
// token nonetheless means it paginated and the remainder is unread. Recording
// it is what keeps a partial enumeration from asserting completeness.
func TestWalk_ContinuationTokenOnATypedListingIsATruncation(t *testing.T) {
	bundle := newFakeBundle(t, nil, nil, emptyDynamic())
	fake, ok := bundle.typed.(interface {
		PrependReactor(verb, resource string, reaction k8stesting.ReactionFunc)
	})
	require.True(t, ok)
	fake.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
		list := &corev1.PodList{}
		list.Continue = "more-pods-remain"
		return true, list, nil
	})

	res, err := walkCluster(t.Context(), bundle, framework.ForeignContext{})
	require.NoError(t, err)
	assert.False(t, res.Complete.IsComplete(),
		"a listing the API server truncated leaves the rest unread")
	assert.Contains(t, res.Complete.Reason(), "Pod")
	assert.Contains(t, res.Complete.Reason(), "continuation token")

	// SAME-RUN CONTROL: the identical walk with no token is complete.
	clean := newFakeBundle(t, nil, nil, emptyDynamic())
	cleanRes, err := walkCluster(t.Context(), clean, framework.ForeignContext{})
	require.NoError(t, err)
	assert.True(t, cleanRes.Complete.IsComplete())
}
