// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// edge_census_test.go — R5-h AS A CENSUS: an emit-and-suppress pair for every
// one of the 33 declared edge types, table-driven, with a completeness check
// that fails when a type has no row.
//
// === WHY A CENSUS AND NOT MORE INDIVIDUAL TESTS ===
//
// The individual family tests were written one at a time, and one family was
// simply never written: ALLOWS_EGRESS_TO shipped with a suppression fixture and
// no emitting one, so disabling its whole branch left the entire suite green
// while the type stayed in the declared vocabulary. Nothing in the suite could
// notice, because TestParity_EdgeTypeSetIsExactlyTheThirtyThree reads the
// DECLARATION and no test read the PRODUCTION.
//
// A census closes that class rather than that instance. Every declared type
// must appear in the table below; a type with no row fails
// TestEdgeCensus_EveryDeclaredTypeHasARow, so the next family added to the
// vocabulary cannot ship unobserved the way this one did.
//
// EACH ROW IS A PAIR, NOT A SINGLE OBSERVATION. The emit fixture must produce
// at least one edge of the type; the suppress fixture, which differs from it
// ONLY in the condition the family gates on, must produce none. One arm alone
// cannot tell a working family from a family that emits unconditionally.
//
// THE CENSUS'S LOCKING DIRECTION — the type this collector must emit none of
// under any input — is its sibling edge_census_absence_test.go.
//
// ONE TYPE IS DECLARED-ONLY AND SAYS SO. USES_IMAGE is in the parity floor and
// its producer is reachable, but its registry index is built from container
// registry repository nodes, which a Kubernetes graph never holds — so it has
// no emitting branch and the census records that with a reason rather than
// letting it look like an oversight.

// edgeCensusRow is one edge type's emit-and-suppress pair.
type edgeCensusRow struct {
	// edgeType is the declared wire string.
	edgeType string
	// declaredOnly marks a type with no reachable emitting branch, and reason
	// says why. A row with declaredOnly set runs its suppress arm only.
	declaredOnly bool
	reason       string
	// emit produces edges from a fixture that CLEARS the family's gate.
	emit func(*testing.T) []Edge
	// suppress produces edges from a fixture that differs only in FAILING the
	// gate.
	suppress func(*testing.T) []Edge
}

// walkEdges runs the whole walk over a typed-object fixture and returns its
// edges, which is how every walk-side row drives its family.
func walkEdges(objs ...runtime.Object) func(*testing.T) []Edge {
	return func(t *testing.T) []Edge {
		t.Helper()
		_, edges, _ := runWalk(t, newFakeBundle(t, objs, nil, emptyDynamic()))
		return edges
	}
}

// dynamicEdges runs the whole walk over a custom-resource fixture.
func dynamicEdges(group, version, plural, kind string, spec map[string]any) func(*testing.T) []Edge {
	return func(t *testing.T) []Edge {
		t.Helper()
		gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: plural}
		listKinds := emptyDynamic()
		listKinds[gvr] = kind + "List"
		bundle := newFakeBundle(t, nil,
			[]runtime.Object{crdFor(group, version, plural, kind, true)}, listKinds,
			customResource(group+"/"+version, kind, "prod", "instance", spec))
		_, edges, _ := runWalk(t, bundle)
		return edges
	}
}

// derivedEdges runs one derivation over a node slice.
func derivedEdges(fn func([]Node) []Edge, nodes ...Node) func(*testing.T) []Edge {
	return func(t *testing.T) []Edge { t.Helper(); return fn(nodes) }
}

// proxyEdges runs the proxy phase over a node slice and a context name.
func proxyEdges(contextName string, nodes ...Node) func(*testing.T) []Edge {
	return func(t *testing.T) []Edge {
		t.Helper()
		_, edges := buildProxyFamilies(nodes, contextName)
		return edges
	}
}

// --- fixture builders shared by several rows ---

func podWithSpec(spec corev1.PodSpec, owners ...metav1.OwnerReference) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "prod", OwnerReferences: owners},
		Spec:       spec,
	}
}

func basicPodSpec() corev1.PodSpec {
	return corev1.PodSpec{Containers: []corev1.Container{{Name: "c", Image: "img:v1"}}}
}

func pvc(volumeName string, storageClass *string) *corev1.PersistentVolumeClaim {
	return &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{Name: "data", Namespace: "prod"},
		Spec:       corev1.PersistentVolumeClaimSpec{VolumeName: volumeName, StorageClassName: storageClass},
	}
}

func endpointSlice(labels map[string]string, ref *corev1.ObjectReference) *discoveryv1.EndpointSlice {
	es := &discoveryv1.EndpointSlice{
		ObjectMeta:  metav1.ObjectMeta{Name: "s", Namespace: "prod", Labels: labels},
		AddressType: discoveryv1.AddressTypeIPv4,
	}
	if ref != nil {
		es.Endpoints = []discoveryv1.Endpoint{{TargetRef: ref}}
	}
	return es
}

func roleBinding(ref rbacv1.RoleRef, subjects ...rbacv1.Subject) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "rb", Namespace: "prod"},
		RoleRef:    ref, Subjects: subjects,
	}
}

func ingress(name string, backend string) *networkingv1.Ingress {
	ing := &networkingv1.Ingress{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "prod"}}
	if backend != "" {
		ing.Spec.DefaultBackend = &networkingv1.IngressBackend{
			Service: &networkingv1.IngressServiceBackend{Name: backend}}
	}
	return ing
}

func hpa(name, targetKind, targetName string) *autoscalingv2.HorizontalPodAutoscaler {
	return &autoscalingv2.HorizontalPodAutoscaler{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "prod"},
		Spec: autoscalingv2.HorizontalPodAutoscalerSpec{
			ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: targetKind, Name: targetName},
			MaxReplicas:    3,
		},
	}
}

func netpolNodeFor(name, body string, meta map[string]string) []Node {
	return []Node{
		node("Namespace/prod", "Namespace", nil),
		netpolNode(name, body, meta),
		namespacedNode("prod", "Pod", "api-1", map[string]string{"label/app": "api"}),
		namespacedNode("prod", "Pod", "db-1", map[string]string{"label/app": "db"}),
	}
}

func anpNodeFor(kind, body string) []Node {
	n := node(resourceID("", kind, "cluster-wide"), kind, nil)
	n.Content = body
	return []Node{
		n,
		node("Namespace/prod", "Namespace", nil),
		node("Namespace/web", "Namespace", nil),
		namespacedNode("prod", "Pod", "api-1", nil),
		namespacedNode("web", "Pod", "web-1", nil),
	}
}

const anpBothDirections = `{"spec":{
	"subject":{"namespaces":{"matchLabels":{"kubernetes.io/metadata.name":"prod"}}},
	"ingress":[{"from":[{"namespaces":{"matchLabels":{"kubernetes.io/metadata.name":"web"}}}]}],
	"egress":[{"to":[{"namespaces":{"matchLabels":{"kubernetes.io/metadata.name":"web"}}}]}]
}}`

// anpNoPeers is the suppression body: a subject with rules whose peer lists are
// EMPTY, which in Kubernetes means allow-everything and emits nothing.
const anpNoPeers = `{"spec":{
	"subject":{"namespaces":{"matchLabels":{"kubernetes.io/metadata.name":"prod"}}},
	"ingress":[{"from":[]}],
	"egress":[{"to":[]}]
}}`

func edgeCensus() []edgeCensusRow {
	fast := "fast"
	return []edgeCensusRow{
		// ---------- the 18 the walk emits ----------
		{edgeType: edgeBacks,
			emit: walkEdges(endpointSlice(map[string]string{discoveryv1.LabelServiceName: "api"},
				&corev1.ObjectReference{Kind: "Pod", Name: "api-1"})),
			suppress: walkEdges(endpointSlice(map[string]string{discoveryv1.LabelServiceName: "api"},
				&corev1.ObjectReference{Kind: "Node", Name: "node-1"}))},

		{edgeType: edgeHasEndpointSlice,
			emit:     walkEdges(endpointSlice(map[string]string{discoveryv1.LabelServiceName: "api"}, nil)),
			suppress: walkEdges(endpointSlice(nil, nil))},

		{edgeType: edgeBindsRole,
			emit:     walkEdges(roleBinding(rbacv1.RoleRef{Kind: "Role", Name: "reader"})),
			suppress: walkEdges(roleBinding(rbacv1.RoleRef{Kind: "Role", Name: ""}))},

		{edgeType: edgeBindsSubject,
			emit: walkEdges(roleBinding(rbacv1.RoleRef{Kind: "Role", Name: "reader"},
				rbacv1.Subject{Kind: "ServiceAccount", Name: "sa", Namespace: "prod"})),
			suppress: walkEdges(roleBinding(rbacv1.RoleRef{Kind: "Role", Name: "reader"},
				rbacv1.Subject{Kind: "User", Name: "someone@example.com"}))},

		{edgeType: edgeBoundTo,
			emit:     walkEdges(pvc("pv-1", nil)),
			suppress: walkEdges(pvc("", nil))},

		{edgeType: edgeUsesStorageClass,
			emit:     walkEdges(pvc("", &fast)),
			suppress: walkEdges(pvc("", nil))},

		{edgeType: edgeRunsOn,
			emit: walkEdges(podWithSpec(corev1.PodSpec{
				NodeName: "node-1", Containers: basicPodSpec().Containers})),
			suppress: walkEdges(podWithSpec(basicPodSpec()))},

		{edgeType: edgeOwnedBy,
			emit: walkEdges(podWithSpec(basicPodSpec(),
				metav1.OwnerReference{Kind: "ReplicaSet", Name: "rs"})),
			suppress: walkEdges(podWithSpec(basicPodSpec()))},

		{edgeType: edgeUsesSA,
			emit: walkEdges(podWithSpec(corev1.PodSpec{
				ServiceAccountName: "sa", Containers: basicPodSpec().Containers})),
			suppress: walkEdges(podWithSpec(basicPodSpec()))},

		{edgeType: edgeMountsConfigMap,
			emit: walkEdges(podWithSpec(corev1.PodSpec{
				Containers: basicPodSpec().Containers,
				Volumes: []corev1.Volume{{Name: "v", VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "cm"}}}}}})),
			suppress: walkEdges(podWithSpec(basicPodSpec()))},

		{edgeType: edgeMountsSecret,
			emit: walkEdges(podWithSpec(corev1.PodSpec{
				Containers: basicPodSpec().Containers,
				Volumes: []corev1.Volume{{Name: "v", VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{SecretName: "sec"}}}}})),
			suppress: walkEdges(podWithSpec(basicPodSpec()))},

		{edgeType: edgeUsesPVC,
			emit: walkEdges(podWithSpec(corev1.PodSpec{
				Containers: basicPodSpec().Containers,
				Volumes: []corev1.Volume{{Name: "v", VolumeSource: corev1.VolumeSource{
					PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "data"}}}}})),
			suppress: walkEdges(podWithSpec(basicPodSpec()))},

		{edgeType: edgeRoutesTo,
			emit:     walkEdges(ingress("web", "api")),
			suppress: walkEdges(ingress("web", ""))},

		{edgeType: edgeScales,
			emit:     walkEdges(hpa("h", "Deployment", "api")),
			suppress: walkEdges(hpa("h", "Deployment", ""))},

		{edgeType: edgeIssuedBy,
			emit: dynamicEdges("cert-manager.io", "v1", "certificates", "Certificate",
				map[string]any{"spec": map[string]any{"issuerRef": map[string]any{"name": "le"}}}),
			suppress: dynamicEdges("cert-manager.io", "v1", "certificates", "Certificate",
				map[string]any{"spec": map[string]any{"issuerRef": map[string]any{}}})},

		{edgeType: edgeReferencesStore,
			emit: dynamicEdges("external-secrets.io", "v1beta1", "externalsecrets", "ExternalSecret",
				map[string]any{"spec": map[string]any{"secretStoreRef": map[string]any{"name": "vault"}}}),
			suppress: dynamicEdges("external-secrets.io", "v1beta1", "externalsecrets", "ExternalSecret",
				map[string]any{"spec": map[string]any{}})},

		{edgeType: edgeUsesMiddleware,
			emit: dynamicEdges("traefik.io", "v1alpha1", "ingressroutes", "IngressRoute",
				map[string]any{"spec": map[string]any{"routes": []any{map[string]any{
					"middlewares": []any{map[string]any{"name": "auth"}}}}}}),
			suppress: dynamicEdges("traefik.io", "v1alpha1", "ingressroutes", "IngressRoute",
				map[string]any{"spec": map[string]any{"routes": []any{map[string]any{}}}})},

		{edgeType: edgeTargets,
			emit: dynamicEdges("argoproj.io", "v1alpha1", "applications", "Application",
				map[string]any{"spec": map[string]any{"destination": map[string]any{"namespace": "staging"}}}),
			suppress: dynamicEdges("argoproj.io", "v1alpha1", "applications", "Application",
				map[string]any{"spec": map[string]any{"destination": map[string]any{}}})},

		// ---------- the 9 derived from the walk's own output ----------
		{edgeType: edgeInNamespace,
			emit: derivedEdges(buildNamespaceMembershipEdges,
				node("Namespace/prod", "Namespace", nil), namespacedNode("prod", "Pod", "api", nil)),
			// The Namespace node is ABSENT, which is the gate.
			suppress: derivedEdges(buildNamespaceMembershipEdges,
				namespacedNode("prod", "Pod", "api", nil))},

		{edgeType: edgeSelects,
			emit: derivedEdges(buildSelectsEdges,
				namespacedNode("prod", "Service", "api", map[string]string{"selector": `{"app":"api"}`}),
				namespacedNode("prod", "Pod", "api-1", map[string]string{"label/app": "api"})),
			// SAME pod, DIFFERENT namespace: a selector never crosses one.
			suppress: derivedEdges(buildSelectsEdges,
				namespacedNode("prod", "Service", "api", map[string]string{"selector": `{"app":"api"}`}),
				namespacedNode("staging", "Pod", "api-1", map[string]string{"label/app": "api"}))},

		{edgeType: edgeRestrictsIngress,
			emit: derivedEdges(buildNetworkPolicyRestrictionEdges,
				namespacedNode("prod", "NetworkPolicy", "p", map[string]string{
					"pod_selector": `{"app":"api"}`, "policy_types": "Ingress"}),
				namespacedNode("prod", "Pod", "api-1", map[string]string{"label/app": "api"})),
			suppress: derivedEdges(buildNetworkPolicyRestrictionEdges,
				namespacedNode("prod", "NetworkPolicy", "p", map[string]string{
					"pod_selector": `{"app":"api"}`, "policy_types": "Egress"}),
				namespacedNode("prod", "Pod", "api-1", map[string]string{"label/app": "api"}))},

		{edgeType: edgeRestrictsEgress,
			emit: derivedEdges(buildNetworkPolicyRestrictionEdges,
				namespacedNode("prod", "NetworkPolicy", "p", map[string]string{
					"pod_selector": `{"app":"api"}`, "policy_types": "Egress"}),
				namespacedNode("prod", "Pod", "api-1", map[string]string{"label/app": "api"})),
			// policy_types ABSENT with no egress rules governs ingress ONLY.
			suppress: derivedEdges(buildNetworkPolicyRestrictionEdges,
				namespacedNode("prod", "NetworkPolicy", "p", map[string]string{
					"pod_selector": `{"app":"api"}`}),
				namespacedNode("prod", "Pod", "api-1", map[string]string{"label/app": "api"}))},

		{edgeType: edgeAllowsIngressFrom,
			emit: derivedEdges(buildNetworkPolicyReachabilityEdges, netpolNodeFor("allow-db", `{"spec":{
				"podSelector":{"matchLabels":{"app":"api"}},
				"policyTypes":["Ingress"],
				"ingress":[{"from":[{"podSelector":{"matchLabels":{"app":"db"}}}]}]}}`,
				map[string]string{"policy_types": "Ingress"})...),
			// An EMPTY peer list means allow-everything and emits nothing.
			suppress: derivedEdges(buildNetworkPolicyReachabilityEdges, netpolNodeFor("allow-all", `{"spec":{
				"podSelector":{"matchLabels":{"app":"api"}},
				"policyTypes":["Ingress"],
				"ingress":[{"from":[]}]}}`,
				map[string]string{"policy_types": "Ingress"})...)},

		// THE FAMILY THAT SHIPPED WITH NO EMITTING FIXTURE. Its whole branch
		// could be disabled and the suite stayed green.
		{edgeType: edgeAllowsEgressTo,
			emit: derivedEdges(buildNetworkPolicyReachabilityEdges, netpolNodeFor("egress-db", `{"spec":{
				"podSelector":{"matchLabels":{"app":"api"}},
				"policyTypes":["Egress"],
				"egress":[{"to":[{"podSelector":{"matchLabels":{"app":"db"}}}]}]}}`,
				map[string]string{"policy_types": "Egress"})...),
			// An ipBlock peer names a CIDR outside the cluster: no node.
			suppress: derivedEdges(buildNetworkPolicyReachabilityEdges, netpolNodeFor("egress-cidr", `{"spec":{
				"podSelector":{"matchLabels":{"app":"api"}},
				"policyTypes":["Egress"],
				"egress":[{"to":[{"ipBlock":{"cidr":"10.0.0.0/8"}}]}]}}`,
				map[string]string{"policy_types": "Egress"})...)},

		{edgeType: edgeANPIngressFrom,
			emit:     derivedEdges(buildAdminNetworkPolicyEdges, anpNodeFor("AdminNetworkPolicy", anpBothDirections)...),
			suppress: derivedEdges(buildAdminNetworkPolicyEdges, anpNodeFor("AdminNetworkPolicy", anpNoPeers)...)},

		{edgeType: edgeANPEgressTo,
			emit:     derivedEdges(buildAdminNetworkPolicyEdges, anpNodeFor("BaselineAdminNetworkPolicy", anpBothDirections)...),
			suppress: derivedEdges(buildAdminNetworkPolicyEdges, anpNodeFor("BaselineAdminNetworkPolicy", anpNoPeers)...)},

		{edgeType: edgeUsesImage, declaredOnly: true,
			reason: "the registry index is built from container-registry repository nodes, " +
				"which a Kubernetes graph never holds, so no branch of this family emits",
			suppress: derivedEdges(buildImageLineageEdges,
				workloadNode("prod/Deployment/api", map[string]string{
					"images": "123456789012.dkr.ecr.us-east-1.amazonaws.com/api:v1"}))},

		// ---------- the 6 that terminate on a minted proxy ----------
		{edgeType: edgeExposedBy,
			emit: proxyEdges("", namespacedNode("prod", "Service", "api",
				map[string]string{"lb_ingress": "ip=203.0.113.5"})),
			suppress: proxyEdges("", namespacedNode("prod", "Service", "api",
				map[string]string{"service_type": "ClusterIP"}))},

		{edgeType: edgeRunsInCluster,
			emit: proxyEdges("gke_p_us-central1_c", namespacedNode("prod", "Pod", "api-1", nil)),
			// A context naming no managed cluster yields no proxy and no edges.
			suppress: proxyEdges("my-laptop", namespacedNode("prod", "Pod", "api-1", nil))},

		{edgeType: edgeBackedByVM,
			emit: proxyEdges("", node("Node/n1", "Node", map[string]string{
				"provider_id": "gce://proj/us-central1-a/n1"})),
			// An AWS providerID carries no account, so the family suppresses.
			suppress: proxyEdges("", node("Node/n1", "Node", map[string]string{
				"provider_id": "aws:///us-east-1a/i-0123456789abcdef0"}))},

		{edgeType: edgeAssumesIdentity,
			emit: proxyEdges("", namespacedNode("prod", "ServiceAccount", "sa", map[string]string{
				metaKeyIRSARoleARN: "arn:aws:iam::123456789012:role/r"})),
			suppress: proxyEdges("", namespacedNode("prod", "ServiceAccount", "sa", nil))},

		{edgeType: edgeUsesDisk,
			emit: proxyEdges("", node("PersistentVolume/pv", "PersistentVolume", map[string]string{
				"volume_handle": "projects/p/zones/z/disks/d", "volume_driver": "pd.csi.storage.gke.io"})),
			suppress: proxyEdges("", node("PersistentVolume/pv", "PersistentVolume", map[string]string{
				"volume_handle": "/mnt/disks/ssd0", "volume_driver": "kubernetes.io/no-provisioner"}))},

		{edgeType: edgeConnectsTo,
			emit: proxyEdges("", workloadNode("prod/Deployment/api", map[string]string{
				"database_url": "postgres://db.abcdef123456.us-east-1.rds.amazonaws.com:5432/app"})),
			suppress: proxyEdges("", workloadNode("prod/Deployment/api", map[string]string{
				"database_url": "postgres://postgres.prod.svc.cluster.local:5432/app"}))},
	}
}

// TestEdgeCensus_EveryDeclaredTypeHasARow is the completeness half. A type in
// the declared vocabulary with no census row fails here, which is what stops
// the next family shipping unobserved.
func TestEdgeCensus_EveryDeclaredTypeHasARow(t *testing.T) {
	rows := map[string]bool{}
	for _, row := range edgeCensus() {
		assert.False(t, rows[row.edgeType], "%s has two census rows", row.edgeType)
		rows[row.edgeType] = true
	}

	var missing []string
	for _, declared := range EmittedEdgeTypes() {
		if !rows[declared] {
			missing = append(missing, declared)
		}
	}
	assert.Empty(t, missing,
		"these declared edge types have no emit-and-suppress row, so a collector that "+
			"never produced them would pass this suite while claiming them: %v", missing)

	var undeclared []string
	declaredSet := set(EmittedEdgeTypes())
	for typ := range rows {
		if !declaredSet[typ] {
			undeclared = append(undeclared, typ)
		}
	}
	assert.Empty(t, undeclared, "these census rows name a type the module does not declare: %v", undeclared)

	assert.Len(t, edgeCensus(), 33, "one row per declared edge type")
}

// TestEdgeCensus_EmitAndSuppress runs both arms of every row.
func TestEdgeCensus_EmitAndSuppress(t *testing.T) {
	for _, row := range edgeCensus() {
		t.Run(row.edgeType, func(t *testing.T) {
			if row.declaredOnly {
				require.NotEmpty(t, row.reason, "a declared-only type states why it never emits")
				require.Nil(t, row.emit, "a declared-only type has no emitting fixture")
				assert.Zero(t, countEdges(row.suppress(t), row.edgeType),
					"declared-only: %s", row.reason)
				return
			}

			require.NotNil(t, row.emit, "every producing type has an emitting fixture")
			require.NotNil(t, row.suppress, "every producing type has a suppressing fixture")

			assert.Positive(t, countEdges(row.emit(t), row.edgeType),
				"the emitting fixture clears this family's gate and must produce at least one edge")
			assert.Zero(t, countEdges(row.suppress(t), row.edgeType),
				"the suppressing fixture fails this family's gate and must produce none")
		})
	}
}

// TestEdgeCensus_AllowsEgressToEmitsToADistinctPeer is the row the census was
// built for, kept as its own named test because the reviewer's finding names it
// and because a census row is a count while this asserts the ENDPOINTS.
func TestEdgeCensus_AllowsEgressToEmitsToADistinctPeer(t *testing.T) {
	nodes := netpolNodeFor("egress-db", `{"spec":{
		"podSelector":{"matchLabels":{"app":"api"}},
		"policyTypes":["Egress"],
		"egress":[{"to":[{"podSelector":{"matchLabels":{"app":"db"}}}]}]}}`,
		map[string]string{"policy_types": "Egress"})

	edges := buildNetworkPolicyReachabilityEdges(nodes)
	require.Len(t, edges, 1)
	assert.True(t, hasEdge(edges, "prod/Pod/api-1", "prod/Pod/db-1", edgeAllowsEgressTo),
		"the edge runs from the governed pod to its distinct peer")
}

var _ = appsv1.Deployment{}
