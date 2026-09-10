// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"strings"
)

// derived_netpol.go — RESTRICTS_INGRESS and RESTRICTS_EGRESS: which pods a
// NetworkPolicy governs, and in which direction.
//
// === THE DIRECTION GATE IS THE PART THAT IS EASY TO GET WRONG ===
//
// Kubernetes does NOT read an absent spec.policyTypes as "no directions". It
// reads it as: ingress is always in effect, and egress is in effect exactly
// when the policy carries egress rules. So a policy that declares no
// policyTypes and no egress rules governs INGRESS ONLY, and a derivation that
// treated absent as "both" would report every default-deny ingress policy as
// also restricting egress — a claim about the cluster's security posture that
// is simply false.
//
// The walk carries policy_types ONLY when the object declared it, and
// has_egress_rules when the spec has egress rules, so both halves of that rule
// are readable here without re-parsing the policy body.

// policyDirections reports which directions a policy governs.
func policyDirections(meta map[string]string) (ingress, egress bool) {
	declared := meta["policy_types"]
	if declared == "" {
		// The implicit rule: ingress always, egress only with egress rules.
		return true, meta["has_egress_rules"] == "true"
	}
	for t := range strings.SplitSeq(declared, ",") {
		switch strings.TrimSpace(t) {
		case "Ingress":
			ingress = true
		case "Egress":
			egress = true
		}
	}
	return ingress, egress
}

// buildNetworkPolicyRestrictionEdges renders RESTRICTS_INGRESS and
// RESTRICTS_EGRESS from each policy to the pods its podSelector governs.
//
// GATES: the policy must carry a pod selector this walk could parse; the pod
// must be in the POLICY'S OWN NAMESPACE (a NetworkPolicy governs nothing
// outside it); the pod's labels must satisfy the selector; and the direction
// must be in effect by the rule above.
//
// AN EMPTY podSelector IS A REAL AND COMMON POLICY — it means "every pod in
// this namespace", which is how a default-deny is written. The walk writes no
// pod_selector metadata for it, so it reaches here with an empty selector and
// emits NOTHING. That is a deliberate, stated limitation of this derivation
// rather than an oversight: matching every pod in the namespace from an absent
// key would be indistinguishable from matching every pod because the key failed
// to parse, and the second is a fan-out across the whole namespace.
func buildNetworkPolicyRestrictionEdges(nodes []Node) []Edge {
	pods := podsByNamespace(nodes)

	var out []Edge
	for i := range nodes {
		n := &nodes[i]
		if n.Metadata["resource_type"] != "NetworkPolicy" {
			continue
		}
		selector := parseSelector(n.Metadata["pod_selector"])
		if len(selector) == 0 {
			continue
		}
		ingress, egress := policyDirections(n.Metadata)
		if !ingress && !egress {
			continue
		}
		for _, pod := range pods[n.Metadata["namespace"]] {
			if !labelsMatch(pod.Metadata, selector) {
				continue
			}
			if ingress {
				out = append(out, Edge{FromID: n.ID, ToID: pod.ID, Type: edgeRestrictsIngress})
			}
			if egress {
				out = append(out, Edge{FromID: n.ID, ToID: pod.ID, Type: edgeRestrictsEgress})
			}
		}
	}
	return out
}

// netpolSpec is the slice of a NetworkPolicy body the reachability derivation
// reads. It is decoded from the node's content rather than from metadata
// because the peer selectors are nested structures a flat string map cannot
// carry.
type netpolSpec struct {
	Spec struct {
		PodSelector labelSelector `json:"podSelector"`
		Ingress     []struct {
			From []netpolPeer `json:"from"`
		} `json:"ingress"`
		Egress []struct {
			To []netpolPeer `json:"to"`
		} `json:"egress"`
		PolicyTypes []string `json:"policyTypes"`
	} `json:"spec"`
}

type netpolPeer struct {
	PodSelector       *labelSelector `json:"podSelector"`
	NamespaceSelector *labelSelector `json:"namespaceSelector"`
	IPBlock           *struct {
		CIDR string `json:"cidr"`
	} `json:"ipBlock"`
}

type labelSelector struct {
	MatchLabels map[string]string `json:"matchLabels"`
}

// decodeNetworkPolicy decodes a policy node's body, reporting failure rather
// than yielding a zero value that would silently emit nothing.
func decodeNetworkPolicy(n *Node) (netpolSpec, bool) {
	if n.Content == "" {
		return netpolSpec{}, false
	}
	var spec netpolSpec
	if err := json.Unmarshal([]byte(n.Content), &spec); err != nil {
		return netpolSpec{}, false
	}
	return spec, true
}
