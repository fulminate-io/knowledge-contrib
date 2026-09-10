// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// derived_test.go — the DERIVED half of R5-h: the nine families that are a
// function of the whole enumeration, each with its emitting fixture and its
// suppressed control in the same run.
//
// THESE TESTS DRIVE THE DERIVATIONS DIRECTLY over hand-built nodes rather than
// through a fake clientset. The subject is what the derivation does with a set
// of nodes; routing that through an API fixture would add a converter's
// behavior to every assertion without adding coverage of it.

func TestDerived_NamespaceMembership(t *testing.T) {
	nodes := []Node{
		node("Namespace/prod", "Namespace", nil),
		namespacedNode("prod", "Pod", "api", nil),
		// A namespaced node whose Namespace this walk did NOT enumerate.
		namespacedNode("gone", "Pod", "orphan", nil),
		// A cluster-scoped node: no namespace to be in, and not a skip.
		node("Node/node-1", "Node", nil),
	}

	edges, skipped := namespaceMembership(nodes)

	assert.True(t, hasEdge(edges, "prod/Pod/api", "Namespace/prod", edgeInNamespace))
	assert.Len(t, edges, 1, "only the node whose Namespace exists gets an edge")
	assert.Equal(t, 1, skipped,
		"the node whose Namespace is missing is COUNTED as skipped, not silently dropped")
}

func TestDerived_NamespaceMembership_NamespaceNodeIsNotItsOwnMember(t *testing.T) {
	edges, skipped := namespaceMembership([]Node{node("Namespace/prod", "Namespace", nil)})
	assert.Empty(t, edges, "a Namespace is not in a namespace")
	assert.Zero(t, skipped, "and it is not counted as a skip either")
}

func TestDerived_Selects(t *testing.T) {
	nodes := []Node{
		namespacedNode("prod", "Service", "api", map[string]string{"selector": `{"app":"api"}`}),
		namespacedNode("prod", "Pod", "api-1", map[string]string{"label/app": "api"}),
		namespacedNode("prod", "Pod", "other", map[string]string{"label/app": "worker"}),
		// SAME LABELS, DIFFERENT NAMESPACE. A Kubernetes selector never
		// crosses namespaces, so this pod is not selected and an edge to it
		// would be a fabricated dependency.
		namespacedNode("staging", "Pod", "api-1", map[string]string{"label/app": "api"}),
	}

	edges := buildSelectsEdges(nodes)
	assert.True(t, hasEdge(edges, "prod/Service/api", "prod/Pod/api-1", edgeSelects))
	assert.Len(t, edges, 1,
		"neither the mismatched pod nor the identically labeled pod in another namespace is selected")
}

func TestDerived_Selects_EmptySelectorSelectsNothing(t *testing.T) {
	nodes := []Node{
		namespacedNode("prod", "Service", "headless", nil),
		namespacedNode("prod", "Pod", "api-1", map[string]string{"label/app": "api"}),
	}
	assert.Empty(t, buildSelectsEdges(nodes),
		"a Service with no selector selects nothing; an empty selector matching every pod "+
			"would wire one Service to the whole namespace")
}

// TestDerived_PolicyDirections is the DIRECTION GATE, which is the part of the
// NetworkPolicy derivation that is easy to get exactly backwards.
func TestDerived_PolicyDirections(t *testing.T) {
	cases := []struct {
		name            string
		meta            map[string]string
		ingress, egress bool
	}{
		{
			name:    "declared both",
			meta:    map[string]string{"policy_types": "Ingress,Egress"},
			ingress: true, egress: true,
		},
		{
			name:    "declared egress only",
			meta:    map[string]string{"policy_types": "Egress"},
			ingress: false, egress: true,
		},
		{
			// THE ARM THAT MATTERS. An ABSENT policyTypes with no egress rules
			// governs INGRESS ONLY. A derivation reading absent as "both"
			// reports every default-deny ingress policy as also restricting
			// egress, which is a false claim about the cluster's posture.
			name:    "absent policyTypes, no egress rules",
			meta:    map[string]string{},
			ingress: true, egress: false,
		},
		{
			// And absent WITH egress rules governs both, by the same rule.
			name:    "absent policyTypes, with egress rules",
			meta:    map[string]string{"has_egress_rules": "true"},
			ingress: true, egress: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ingress, egress := policyDirections(tc.meta)
			assert.Equal(t, tc.ingress, ingress, "ingress")
			assert.Equal(t, tc.egress, egress, "egress")
		})
	}
}

func TestDerived_NetworkPolicyRestrictions(t *testing.T) {
	nodes := []Node{
		namespacedNode("prod", "NetworkPolicy", "deny-api", map[string]string{
			"pod_selector": `{"app":"api"}`,
			"policy_types": "Ingress,Egress",
		}),
		namespacedNode("prod", "Pod", "api-1", map[string]string{"label/app": "api"}),
		namespacedNode("prod", "Pod", "worker-1", map[string]string{"label/app": "worker"}),
		namespacedNode("staging", "Pod", "api-1", map[string]string{"label/app": "api"}),
	}

	edges := buildNetworkPolicyRestrictionEdges(nodes)
	assert.True(t, hasEdge(edges, "prod/NetworkPolicy/deny-api", "prod/Pod/api-1", edgeRestrictsIngress))
	assert.True(t, hasEdge(edges, "prod/NetworkPolicy/deny-api", "prod/Pod/api-1", edgeRestrictsEgress))
	assert.Len(t, edges, 2,
		"neither the mismatched pod nor the pod in another namespace is governed")
}

func TestDerived_NetworkPolicyRestrictions_IngressOnlyWhenPolicyTypesAbsent(t *testing.T) {
	nodes := []Node{
		namespacedNode("prod", "NetworkPolicy", "default-deny", map[string]string{
			"pod_selector": `{"app":"api"}`,
		}),
		namespacedNode("prod", "Pod", "api-1", map[string]string{"label/app": "api"}),
	}
	edges := buildNetworkPolicyRestrictionEdges(nodes)
	assert.Equal(t, []string{edgeRestrictsIngress}, edgeTypes(edges),
		"an absent policyTypes with no egress rules governs INGRESS ONLY")
}

// netpolNode builds a NetworkPolicy node whose CONTENT is a real policy body,
// which is what the reachability derivation reads.
func netpolNode(name, body string, meta map[string]string) Node {
	n := namespacedNode("prod", "NetworkPolicy", name, meta)
	n.Content = body
	return n
}

func TestDerived_Reachability_PodToPod(t *testing.T) {
	body := `{"spec":{
		"podSelector":{"matchLabels":{"app":"api"}},
		"policyTypes":["Ingress"],
		"ingress":[{"from":[{"podSelector":{"matchLabels":{"app":"web"}}}]}]
	}}`
	nodes := []Node{
		node("Namespace/prod", "Namespace", nil),
		netpolNode("allow-web", body, map[string]string{"policy_types": "Ingress"}),
		namespacedNode("prod", "Pod", "api-1", map[string]string{"label/app": "api"}),
		namespacedNode("prod", "Pod", "web-1", map[string]string{"label/app": "web"}),
	}

	edges := buildNetworkPolicyReachabilityEdges(nodes)
	require.Len(t, edges, 1)
	assert.True(t, hasEdge(edges, "prod/Pod/api-1", "prod/Pod/web-1", edgeAllowsIngressFrom))
	// EVERY REACHABILITY EDGE IS POD-TO-POD. An edge terminating on a Namespace
	// node would be a claim nothing in Kubernetes means.
	for _, e := range edges {
		assert.NotContains(t, e.ToID, "Namespace/", "a reachability peer is never a Namespace node")
	}
}

func TestDerived_Reachability_EmptyPeerListEmitsNothing(t *testing.T) {
	// AN EMPTY from: MEANS ALLOW EVERYTHING, and the most permissive policy in
	// the cluster therefore produces the FEWEST edges. A test asserting edges
	// here would red a correct derivation.
	body := `{"spec":{
		"podSelector":{"matchLabels":{"app":"api"}},
		"policyTypes":["Ingress"],
		"ingress":[{"from":[]}]
	}}`
	nodes := []Node{
		netpolNode("allow-all", body, map[string]string{"policy_types": "Ingress"}),
		namespacedNode("prod", "Pod", "api-1", map[string]string{"label/app": "api"}),
		namespacedNode("prod", "Pod", "web-1", map[string]string{"label/app": "web"}),
	}
	assert.Empty(t, buildNetworkPolicyReachabilityEdges(nodes))
}

func TestDerived_Reachability_IPBlockPeerIsSkipped(t *testing.T) {
	body := `{"spec":{
		"podSelector":{"matchLabels":{"app":"api"}},
		"policyTypes":["Egress"],
		"egress":[{"to":[{"ipBlock":{"cidr":"10.0.0.0/8"}}]}]
	}}`
	nodes := []Node{
		netpolNode("egress-cidr", body, map[string]string{"policy_types": "Egress"}),
		namespacedNode("prod", "Pod", "api-1", map[string]string{"label/app": "api"}),
		// A SECOND POD THAT IS NOT THE TARGET, and it is what makes this row
		// observe anything. An ipBlock peer carries no podSelector, so a
		// derivation that failed to skip it would fall through to "every pod in
		// the policy's namespace" — and with only the target pod present, the
		// self-pair rule would drop that edge and the row would pass on a
		// broken derivation.
		namespacedNode("prod", "Pod", "web-1", map[string]string{"label/app": "web"}),
	}
	assert.Empty(t, buildNetworkPolicyReachabilityEdges(nodes),
		"a CIDR outside the cluster is not a node this graph holds, and must not "+
			"degrade into every pod in the namespace")
}

func TestDerived_Reachability_SelfPairIsDropped(t *testing.T) {
	body := `{"spec":{
		"podSelector":{"matchLabels":{"app":"api"}},
		"policyTypes":["Ingress"],
		"ingress":[{"from":[{"podSelector":{"matchLabels":{"app":"api"}}}]}]
	}}`
	nodes := []Node{
		netpolNode("self", body, map[string]string{"policy_types": "Ingress"}),
		namespacedNode("prod", "Pod", "api-1", map[string]string{"label/app": "api"}),
	}
	assert.Empty(t, buildNetworkPolicyReachabilityEdges(nodes),
		"a pod reaching itself is not reachability")
}

func TestDerived_AdminNetworkPolicy_BothKinds(t *testing.T) {
	body := `{"spec":{
		"subject":{"namespaces":{"matchLabels":{"kubernetes.io/metadata.name":"prod"}}},
		"ingress":[{"from":[{"namespaces":{"matchLabels":{"kubernetes.io/metadata.name":"web"}}}]}],
		"egress":[{"to":[{"namespaces":{"matchLabels":{"kubernetes.io/metadata.name":"web"}}}]}]
	}}`
	for _, kind := range []string{"AdminNetworkPolicy", "BaselineAdminNetworkPolicy"} {
		t.Run(kind, func(t *testing.T) {
			anp := node(resourceID("", kind, "cluster-wide"), kind, nil)
			anp.Content = body
			nodes := []Node{
				anp,
				node("Namespace/prod", "Namespace", nil),
				node("Namespace/web", "Namespace", nil),
				namespacedNode("prod", "Pod", "api-1", nil),
				namespacedNode("web", "Pod", "web-1", nil),
			}
			edges := buildAdminNetworkPolicyEdges(nodes)
			assert.True(t, hasEdge(edges, "prod/Pod/api-1", "web/Pod/web-1", edgeANPIngressFrom))
			assert.True(t, hasEdge(edges, "prod/Pod/api-1", "web/Pod/web-1", edgeANPEgressTo))
		})
	}
}

// TestDerived_AdminNetworkPolicy_NamespaceNameLabelIsSynthesized covers the
// well-known label every namespace carries in a real cluster and a hand-built
// fixture usually omits. Policies routinely select on it.
func TestDerived_AdminNetworkPolicy_NamespaceNameLabelIsSynthesized(t *testing.T) {
	index := namespaceLabelIndex([]Node{node("Namespace/prod", "Namespace", nil)})
	assert.Equal(t, "prod", index["prod"]["label/kubernetes.io/metadata.name"])
}

func TestDerived_ImageRepositoryName(t *testing.T) {
	cases := map[string]string{
		"123456789012.dkr.ecr.us-east-1.amazonaws.com/api:v1": "api",
		"ghcr.io/org/api@sha256:deadbeef":                     "api",
		"registry.example.com:5000/team/api:v2":               "api",
		"api":                                                 "api",
		"api:latest":                                          "api",
		"":                                                    "",
	}
	for image, want := range cases {
		assert.Equal(t, want, imageRepositoryName(image), "image %q", image)
	}
}
