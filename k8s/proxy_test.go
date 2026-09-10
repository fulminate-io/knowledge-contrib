// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// proxy_test.go — the SIX PROXY FAMILIES, one row each.
//
// EVERY ROW ASSERTS THE PROXY ID, NOT ONLY THE EDGE. An edge to a
// differently-minted id is a dangling edge that no later resolution repairs,
// and a row that counted edges would pass on it. Three of these families can
// emit the right number of edges to the wrong ids.
//
// EVERY ROW ALSO ASSERTS THAT THE PROXY NODE IS IN THE RESULT. The whole reason
// the proxy mechanism works without a cross-graph edge field is that both
// endpoints land in the same batch; a family that emitted its edge and forgot
// its node would produce exactly the dangling edge it exists to avoid.

func proxyNodeByID(t *testing.T, nodes []Node, id string) Node {
	t.Helper()
	for _, n := range nodes {
		if n.ID == id {
			require.Equal(t, nodeTypeProxy, n.Type, "node %q is expected to be a proxy", id)
			return n
		}
	}
	t.Fatalf("no proxy node with id %q among %d nodes", id, len(nodes))
	return Node{}
}

// === PXY-a: EXPOSED_BY ===

func TestProxy_ExposedBy_DedupesTwoIngressEntriesForOneLoadBalancer(t *testing.T) {
	// A Service whose status carries BOTH an ip and a hostname for the SAME
	// load balancer, which is the ordinary case on every cloud.
	svc := namespacedNode("prod", "Service", "api", map[string]string{
		"service_type": "LoadBalancer",
		"lb_ingress":   "ip=203.0.113.5,hostname=203.0.113.5",
	})
	nodes, edges := buildProxyFamilies([]Node{svc}, "")

	assert.Equal(t, 1, countEdges(edges, edgeExposedBy),
		"two ingress entries resolving to the SAME load balancer are one relationship")
	proxyID := proxyIDFor("", "203.0.113.5")
	assert.True(t, hasEdge(edges, "prod/Service/api", proxyID, edgeExposedBy))
	proxyNodeByID(t, nodes, proxyID)

	e := findEdge(edges, edgeExposedBy)
	assert.Contains(t, e.Evidence, "203.0.113.5",
		"the evidence names which lookup key matched, so a mis-linkage is auditable")
}

func TestProxy_ExposedBy_ServiceWithNoLoadBalancerEmitsNothing(t *testing.T) {
	svc := namespacedNode("prod", "Service", "internal", map[string]string{"service_type": "ClusterIP"})
	nodes, edges := buildProxyFamilies([]Node{svc}, "")
	assert.Zero(t, countEdges(edges, edgeExposedBy))
	assert.Empty(t, nodes, "no edge means no proxy was minted either")
}

// === PXY-b: RUNS_IN_CLUSTER ===

func TestProxy_RunsInCluster_OneEdgePerResourceAndNoSelfOrProxyEdges(t *testing.T) {
	// A Service with a load balancer, so the EXPOSED_BY family mints a proxy
	// into the same result. That proxy must NOT then gain a RUNS_IN_CLUSTER
	// edge of its own: it stands for a resource in another graph, which has its
	// own cluster relationship over there.
	nodes := []Node{
		node("Namespace/prod", "Namespace", nil),
		namespacedNode("prod", "Pod", "api-1", nil),
		namespacedNode("prod", "Service", "api", map[string]string{"lb_ingress": "ip=203.0.113.5"}),
	}
	const context = "gke_fulminate-services_us-central1_main-us-central1"
	proxies, edges := buildProxyFamilies(nodes, context)

	clusterProxyID := proxyIDFor("fulminate-services",
		"projects/fulminate-services/locations/us-central1/clusters/main-us-central1")
	proxyNodeByID(t, proxies, clusterProxyID)

	assert.Equal(t, 3, countEdges(edges, edgeRunsInCluster),
		"exactly one edge per non-proxy resource: the three input nodes and no more")
	for _, n := range nodes {
		assert.True(t, hasEdge(edges, n.ID, clusterProxyID, edgeRunsInCluster), "node %s", n.ID)
	}
	for _, e := range edges {
		if e.Type != edgeRunsInCluster {
			continue
		}
		assert.NotEqual(t, clusterProxyID, e.FromID, "the cluster proxy must not run in itself")
		assert.NotContains(t, e.FromID, "proxy:", "a proxy must not gain a RUNS_IN_CLUSTER edge")
	}
}

func TestProxy_RunsInCluster_UnresolvableContextEmitsNothing(t *testing.T) {
	nodes := []Node{namespacedNode("prod", "Pod", "api-1", nil)}
	// A self-managed cluster, or an operator-renamed context, names no cloud
	// resource. This arm is also what proves a fixture reached the producer at
	// all rather than being filtered out earlier.
	proxies, edges := buildProxyFamilies(nodes, "my-laptop-cluster")
	assert.Zero(t, countEdges(edges, edgeRunsInCluster))
	assert.Empty(t, proxies)
}

func TestProxy_RunsInCluster_EKSContext(t *testing.T) {
	nodes := []Node{namespacedNode("prod", "Pod", "api-1", nil)}
	const context = "arn:aws:eks:us-east-1:123456789012:cluster/prod"
	proxies, edges := buildProxyFamilies(nodes, context)
	proxyID := proxyIDFor("123456789012", context)
	assert.True(t, hasEdge(edges, "prod/Pod/api-1", proxyID, edgeRunsInCluster))
	p := proxyNodeByID(t, proxies, proxyID)
	assert.Equal(t, "aws", p.Metadata["provider"])
	assert.Equal(t, "123456789012", p.Metadata["account"])
}

// === PXY-c: BACKED_BY_VM ===

func TestProxy_BackedByVM_GCE(t *testing.T) {
	n := node("Node/gke-node-1", "Node", map[string]string{
		"provider_id": "gce://fulminate-services/us-central1-a/gke-node-1",
	})
	proxies, edges := buildProxyFamilies([]Node{n}, "")
	proxyID := proxyIDFor("fulminate-services",
		"projects/fulminate-services/zones/us-central1-a/instances/gke-node-1")
	assert.True(t, hasEdge(edges, "Node/gke-node-1", proxyID, edgeBackedByVM))
	p := proxyNodeByID(t, proxies, proxyID)
	assert.Equal(t, "gcp:compute:instance", p.Metadata["resource_type"])
}

func TestProxy_BackedByVM_SuppressedWhenTheAccountCannotBeDetermined(t *testing.T) {
	// An AWS providerID carries the zone and the instance id and NO ACCOUNT.
	// A VM always belongs to a determinable account, so emitting a DANGLING
	// proxy here would mint a second proxy for a VM whose account a later pass
	// can learn — and the two would never merge.
	aws := node("Node/ip-10-0-0-1", "Node", map[string]string{
		"provider_id": "aws:///us-east-1a/i-0123456789abcdef0",
	})
	// And a providerID that does not parse at all.
	garbage := node("Node/weird", "Node", map[string]string{"provider_id": "not-a-provider-id"})

	proxies, edges := buildProxyFamilies([]Node{aws, garbage}, "")
	assert.Zero(t, countEdges(edges, edgeBackedByVM))
	assert.Empty(t, proxies)
}

// === PXY-d: ASSUMES_IDENTITY, BOTH ID-MINTING ARMS ===

// TestProxy_AssumesIdentity_BothIDMintingArms is the row a collapsed
// implementation cannot pass. The account-bearing arm and the dangling arm mint
// DIFFERENT ids, and a family that always took one produces no id at all for an
// unknown account, or the wrong id when the account is known.
func TestProxy_AssumesIdentity_BothIDMintingArms(t *testing.T) {
	// ARM 1 — the account IS knowable, from the ARN itself.
	irsa := namespacedNode("prod", "ServiceAccount", "api-sa", map[string]string{
		metaKeyIRSARoleARN: "arn:aws:iam::123456789012:role/api-role",
	})
	// ARM 2 — the account is NOT knowable. An Azure client id is a bare UUID
	// that names no subscription, so the dangling form is the ordinary case
	// here rather than a failure.
	azure := namespacedNode("prod", "ServiceAccount", "azure-sa", map[string]string{
		metaKeyAzureClientID: "11111111-2222-3333-4444-555555555555",
	})

	proxies, edges := buildProxyFamilies([]Node{irsa, azure}, "")

	accountArm := proxyIDFor("123456789012", "arn:aws:iam::123456789012:role/api-role")
	danglingArm := proxyIDFor("", "11111111-2222-3333-4444-555555555555")
	require.NotEqual(t, accountArm, danglingArm, "the two arms mint different ids")
	assert.True(t, strings.HasPrefix(danglingArm, "proxy:cloud::"),
		"the dangling form carries an EMPTY account segment and is still deterministic")

	assert.True(t, hasEdge(edges, "prod/ServiceAccount/api-sa", accountArm, edgeAssumesIdentity))
	assert.True(t, hasEdge(edges, "prod/ServiceAccount/azure-sa", danglingArm, edgeAssumesIdentity))
	account := proxyNodeByID(t, proxies, accountArm)
	dangling := proxyNodeByID(t, proxies, danglingArm)
	assert.Equal(t, "123456789012", account.Metadata["account"])
	assert.Empty(t, dangling.Metadata["account"])

	// THE SOURCE LABEL IS PART OF THE ARM, not decoration. Its whole purpose is
	// that an operator reading the graph can tell a proxy whose account is
	// UNKNOWN from one whose account is the empty string by accident — and the
	// two are otherwise indistinguishable, since both carry an empty account.
	// Nothing observed it until this assertion, so the label could be changed
	// to anything and the suite stayed green.
	assert.Equal(t, "proxy:cloud:dangling", dangling.Source,
		"the dangling arm names itself, so an unknown account is distinguishable from an empty one")
	assert.Equal(t, "proxy:cloud:123456789012", account.Source,
		"the account-bearing arm names its account")
}

func TestProxy_AssumesIdentity_GCPArm(t *testing.T) {
	sa := namespacedNode("prod", "ServiceAccount", "gcp-sa", map[string]string{
		metaKeyGCPServiceAccount: "api@fulminate-services.iam.gserviceaccount.com",
	})
	proxies, edges := buildProxyFamilies([]Node{sa}, "")
	proxyID := proxyIDFor("fulminate-services",
		"projects/fulminate-services/serviceAccounts/api@fulminate-services.iam.gserviceaccount.com")
	assert.True(t, hasEdge(edges, "prod/ServiceAccount/gcp-sa", proxyID, edgeAssumesIdentity))
	proxyNodeByID(t, proxies, proxyID)
}

func TestProxy_AssumesIdentity_NoAnnotationEmitsNothing(t *testing.T) {
	sa := namespacedNode("prod", "ServiceAccount", "plain", nil)
	proxies, edges := buildProxyFamilies([]Node{sa}, "")
	assert.Zero(t, countEdges(edges, edgeAssumesIdentity))
	assert.Empty(t, proxies)
}

// === PXY-e: USES_DISK, BOTH ID-MINTING ARMS ===

// The two arms again, reached through a DIFFERENT PARSE. This is a separate row
// from PXY-d rather than a parameter of it: the source shapes have nothing in
// common, so one implementation tested over one of them leaves the other's
// parse uncovered.
func TestProxy_UsesDisk_BothIDMintingArms(t *testing.T) {
	gce := node("PersistentVolume/pv-gce", "PersistentVolume", map[string]string{
		"volume_handle": "projects/fulminate-services/zones/us-central1-a/disks/pvc-abc",
		"volume_driver": "pd.csi.storage.gke.io",
	})
	// A bare EBS volume id names no account.
	ebs := node("PersistentVolume/pv-ebs", "PersistentVolume", map[string]string{
		"volume_handle": "vol-0123456789abcdef0",
		"volume_driver": "ebs.csi.aws.com",
	})

	proxies, edges := buildProxyFamilies([]Node{gce, ebs}, "")

	accountArm := proxyIDFor("fulminate-services",
		"projects/fulminate-services/zones/us-central1-a/disks/pvc-abc")
	danglingArm := proxyIDFor("", "vol-0123456789abcdef0")
	assert.True(t, hasEdge(edges, "PersistentVolume/pv-gce", accountArm, edgeUsesDisk))
	assert.True(t, hasEdge(edges, "PersistentVolume/pv-ebs", danglingArm, edgeUsesDisk))
	assert.Equal(t, "fulminate-services", proxyNodeByID(t, proxies, accountArm).Metadata["account"])
	assert.Empty(t, proxyNodeByID(t, proxies, danglingArm).Metadata["account"])
}

func TestProxy_UsesDisk_VolumeNamingNoCloudDiskEmitsNothing(t *testing.T) {
	local := node("PersistentVolume/pv-local", "PersistentVolume", map[string]string{
		"volume_handle": "/mnt/disks/ssd0",
		"volume_driver": "kubernetes.io/no-provisioner",
	})
	proxies, edges := buildProxyFamilies([]Node{local}, "")
	assert.Zero(t, countEdges(edges, edgeUsesDisk))
	assert.Empty(t, proxies)
}

// === PXY-f: CONNECTS_TO AND ITS SECURITY INVARIANT ===

// TestProxy_ConnectsTo_EvidenceCarriesTheMatchNotTheValue is this row's real
// subject.
//
// THE FIXTURE IS BUILT SO THE VALUE AND THE MATCH DIFFER. The metadata value
// carries a password beside the hostname; the pattern matches only the
// hostname. An implementation that built its evidence from the raw value would
// write that password into a graph that is summarized, embedded and synced —
// and every other assertion on this family would still pass, because the edge
// would exist with the right endpoints and the right count.
func TestProxy_ConnectsTo_EvidenceCarriesTheMatchNotTheValue(t *testing.T) {
	const password = "s3cr3t-p4ssw0rd-must-not-appear"
	const host = "db-prod.abcdef123456.us-east-1.rds.amazonaws.com"
	workload := workloadNode("prod/Deployment/api", map[string]string{
		"database_url": "postgres://admin:" + password + "@" + host + ":5432/app",
	})

	proxies, edges := buildProxyFamilies([]Node{workload}, "")
	require.Equal(t, 1, countEdges(edges, edgeConnectsTo))
	e := findEdge(edges, edgeConnectsTo)

	assert.Contains(t, e.Evidence, host, "the evidence names the matched endpoint")
	assert.NotContains(t, e.Evidence, password, "the evidence must NOT carry the value the match was found in")
	assert.NotContains(t, e.Method, password)
	assert.NotContains(t, e.Evidence, "admin", "nor the username beside it")

	proxyID := proxyIDFor("", host)
	assert.Equal(t, proxyID, e.ToID)
	p := proxyNodeByID(t, proxies, proxyID)
	for k, v := range p.Metadata {
		assert.NotContains(t, v, password, "the proxy's metadata key %q must not carry the value either", k)
	}
}

func TestProxy_ConnectsTo_DedupesTwoMatchesToOneTarget(t *testing.T) {
	const host = "db-prod.abcdef123456.us-east-1.rds.amazonaws.com"
	workload := workloadNode("prod/Deployment/api", map[string]string{
		"database_url":  "postgres://" + host + ":5432/app",
		"database_host": host,
	})
	_, edges := buildProxyFamilies([]Node{workload}, "")
	assert.Equal(t, 1, countEdges(edges, edgeConnectsTo),
		"a workload naming the same database twice has one dependency")
}

func TestProxy_ConnectsTo_NoRecognizedEndpointEmitsNothing(t *testing.T) {
	workload := workloadNode("prod/Deployment/api", map[string]string{
		"database_url": "postgres://postgres.prod.svc.cluster.local:5432/app",
	})
	proxies, edges := buildProxyFamilies([]Node{workload}, "")
	assert.Zero(t, countEdges(edges, edgeConnectsTo),
		"an in-cluster address is not an external cloud service")
	assert.Empty(t, proxies)
}

// === THE PROXY ID SCHEME ITSELF ===

// TestProxy_IDSchemeIsDeterministicAcrossRuns is the carry-forward assertion
// the whole proxy mechanism rests on: a second collect of an unchanged cluster
// must mint the SAME ids, or the graph grows a duplicate proxy per run.
func TestProxy_IDSchemeIsDeterministicAcrossRuns(t *testing.T) {
	nodes := []Node{
		node("Node/gke-node-1", "Node", map[string]string{
			"provider_id": "gce://fulminate-services/us-central1-a/gke-node-1"}),
		namespacedNode("prod", "ServiceAccount", "api-sa", map[string]string{
			metaKeyIRSARoleARN: "arn:aws:iam::123456789012:role/api-role"}),
	}
	first, firstEdges := buildProxyFamilies(nodes, "gke_p_us-central1_c")
	second, secondEdges := buildProxyFamilies(nodes, "gke_p_us-central1_c")
	assert.Equal(t, first, second, "the proxy nodes are byte-identical across runs")
	assert.Equal(t, firstEdges, secondEdges, "and so are the edges")
}

// TestProxy_TargetWithNoForeignIDMintsNothing is the backstop on the id
// builder. "proxy:cloud:<account>:" is a well-formed string naming nothing, and
// every later resolution would treat it as a real reference.
func TestProxy_TargetWithNoForeignIDMintsNothing(t *testing.T) {
	acc := newProxyAccumulator()
	id, ok := acc.proxy(proxyTarget{Account: "123456789012"})
	assert.False(t, ok)
	assert.Empty(t, id)
	assert.Empty(t, acc.nodes())
}

// TestProxy_RunsInCluster_SkipsAProxyAlreadyInTheNodeSlice drives
// buildRunsInClusterEdges DIRECTLY with a proxy node already present.
//
// WHY IT IS A SEPARATE ROW FROM THE ONE ABOVE, and this is the mutation pass
// speaking rather than a preference: through buildProxyFamilies the proxies are
// accumulated separately and appended by the CALLER, so the input slice never
// contains one and neither skip is reachable — deleting both guards left the
// row above green. The guards are not decoration: they make the function
// correct for a caller that hands it a result which ALREADY carries proxies,
// which is exactly the shape the built-in collector's own emitter produces, and
// which a future reordering of the phases here would produce too. Reached
// directly, both are observed.
func TestProxy_RunsInCluster_SkipsAProxyAlreadyInTheNodeSlice(t *testing.T) {
	const context = "gke_fulminate-services_us-central1_main-us-central1"
	clusterProxyID := proxyIDFor("fulminate-services",
		"projects/fulminate-services/locations/us-central1/clusters/main-us-central1")

	acc := newProxyAccumulator()
	nodes := []Node{
		namespacedNode("prod", "Pod", "api-1", nil),
		// A proxy some other family minted, standing for a resource in another
		// graph. It has its own cluster relationship over there.
		{ID: proxyIDFor("", "203.0.113.5"), Type: nodeTypeProxy, SymbolName: "203.0.113.5"},
		// And the cluster proxy itself, which must not run in itself.
		{ID: clusterProxyID, Type: nodeTypeProxy, SymbolName: "main-us-central1"},
	}

	edges := buildRunsInClusterEdges(nodes, context, acc)

	require.Len(t, edges, 1, "only the one non-proxy resource gets an edge")
	_ = clusterProxyID
	assert.Equal(t, "prod/Pod/api-1", edges[0].FromID)
	assert.Equal(t, clusterProxyID, edges[0].ToID)
	for _, e := range edges {
		assert.NotEqual(t, clusterProxyID, e.FromID, "the cluster proxy must not run in itself")
		assert.NotContains(t, e.FromID, "proxy:", "a proxy must not gain a RUNS_IN_CLUSTER edge")
	}
}

// TestProxy_RunsInCluster_SkipsANodeCarryingTheClusterProxyID is T3-1's row:
// the SELF-EDGE guard, made reachable.
//
// WHY IT NEEDED ITS OWN ROW. Removing the guard left the whole suite green,
// because in this module's phase order the six families run over the PRE-PROXY
// node slice and the caller appends the minted proxies afterwards — so no node
// reaching the function could carry a proxy id, and resourceID never produces
// one. The built-in collector's equivalent pass runs over a result that ALREADY
// holds proxies, which is where the guard comes from.
//
// THE GUARD IS KEPT RATHER THAN DELETED, and this row is why that is a choice
// and not inertia: it makes the function correct for a caller that hands it a
// result already carrying proxies, which is the shape the built-in produces and
// the shape a reordering of the phases here would produce. Driven directly, the
// guard is observed.
//
// IT IS DISTINCT FROM THE PROXY-TYPE GUARD BESIDE IT. That one skips a node
// whose TYPE is proxy; this one skips a node whose ID equals the cluster
// proxy's, which catches a node that carries the id without carrying the type.
func TestProxy_RunsInCluster_SkipsANodeCarryingTheClusterProxyID(t *testing.T) {
	const context = "gke_fulminate-services_us-central1_main-us-central1"
	clusterProxyID := proxyIDFor("fulminate-services",
		"projects/fulminate-services/locations/us-central1/clusters/main-us-central1")

	acc := newProxyAccumulator()
	nodes := []Node{
		namespacedNode("prod", "Pod", "api-1", nil),
		// A node carrying the cluster proxy's exact ID and an ORDINARY type, so
		// the sibling proxy-type guard cannot be what skips it.
		{ID: clusterProxyID, Type: nodeTypeCloudResource, SymbolName: "main-us-central1"},
	}

	edges := buildRunsInClusterEdges(nodes, context, acc)

	require.Len(t, edges, 1, "the cluster must not run in itself")
	assert.Equal(t, "prod/Pod/api-1", edges[0].FromID)
	for _, e := range edges {
		assert.NotEqual(t, clusterProxyID, e.FromID,
			"a node carrying the cluster proxy's id gets no self-edge, whatever its type")
	}
}
