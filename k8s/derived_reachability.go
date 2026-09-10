// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"sort"
)

// derived_reachability.go — the FOUR REACHABILITY FAMILIES:
// ALLOWS_INGRESS_FROM, ALLOWS_EGRESS_TO, ANP_INGRESS_FROM and ANP_EGRESS_TO.
//
// === EVERY ONE OF THE FOUR IS POD-TO-POD, ALWAYS ===
//
// A namespaceSelector on a peer is an INPUT to peer resolution — it chooses
// WHICH NAMESPACES' PODS become peers — and never an endpoint. An edge
// terminating on a Namespace node would be a claim that a namespace is a
// network peer, which nothing in Kubernetes means.
//
// THREE SHAPES PRODUCE NO EDGE AT ALL, and two of them are counter-intuitive:
//
//   - An ipBlock peer is SKIPPED. It names a CIDR outside the cluster, so there
//     is no node to point at; reachability to the outside world is a topology
//     question, not a collection one.
//   - AN EMPTY PEER LIST MEANS "ALLOW EVERYTHING", and emits NOTHING. So the
//     most permissive policy in the cluster produces the fewest edges, which
//     is the opposite of what a reader expects; a test asserting edges from an
//     empty `from:` reds a correct derivation.
//   - A SELF-PAIR IS DROPPED. A policy whose only peer is its own target
//     produces an edge from a pod to itself, which is not reachability.

// buildNetworkPolicyReachabilityEdges renders ALLOWS_INGRESS_FROM and
// ALLOWS_EGRESS_TO.
//
// DIRECTION CONVENTION, which is the reverse of the policy's own wording for
// ingress: an ingress rule says the TARGET accepts traffic from the peer, so
// the edge runs target -> peer. An egress rule says the target may reach the
// peer, so the edge runs target -> peer as well. Both are read as "this pod's
// policy permits this pod pair", with the type naming the direction.
func buildNetworkPolicyReachabilityEdges(nodes []Node) []Edge {
	pods := podsByNamespace(nodes)
	namespaceLabels := namespaceLabelIndex(nodes)

	var out []Edge
	for i := range nodes {
		n := &nodes[i]
		if n.Metadata["resource_type"] != "NetworkPolicy" {
			continue
		}
		spec, ok := decodeNetworkPolicy(n)
		if !ok {
			continue
		}
		policyNS := n.Metadata["namespace"]
		if policyNS == "" {
			continue
		}
		targets := matchPods(pods[policyNS], spec.Spec.PodSelector.MatchLabels)
		if len(targets) == 0 {
			continue
		}
		ingress, egress := policyDirections(n.Metadata)

		if ingress {
			for _, rule := range spec.Spec.Ingress {
				peers := resolvePeers(rule.From, policyNS, pods, namespaceLabels)
				out = append(out, pairEdges(targets, peers, edgeAllowsIngressFrom)...)
			}
		}
		if egress {
			for _, rule := range spec.Spec.Egress {
				peers := resolvePeers(rule.To, policyNS, pods, namespaceLabels)
				out = append(out, pairEdges(targets, peers, edgeAllowsEgressTo)...)
			}
		}
	}
	return out
}

// anpSpec is the slice of an (Baseline)AdminNetworkPolicy body this derivation
// reads. Its subject is a level deeper than a NetworkPolicy's selector, and its
// rules nest their peers under a named list.
type anpSpec struct {
	Spec struct {
		Subject struct {
			Pods *struct {
				NamespaceSelector labelSelector `json:"namespaceSelector"`
				PodSelector       labelSelector `json:"podSelector"`
			} `json:"pods"`
			Namespaces *labelSelector `json:"namespaces"`
		} `json:"subject"`
		Ingress []struct {
			From []anpPeer `json:"from"`
		} `json:"ingress"`
		Egress []struct {
			To []anpPeer `json:"to"`
		} `json:"egress"`
	} `json:"spec"`
}

type anpPeer struct {
	Namespaces *labelSelector `json:"namespaces"`
	Pods       *struct {
		NamespaceSelector labelSelector `json:"namespaceSelector"`
		PodSelector       labelSelector `json:"podSelector"`
	} `json:"pods"`
	Networks []string `json:"networks"`
}

// buildAdminNetworkPolicyEdges renders ANP_INGRESS_FROM and ANP_EGRESS_TO for
// both AdminNetworkPolicy and BaselineAdminNetworkPolicy.
//
// AN ANP IS CLUSTER-SCOPED, so it has no namespace of its own and EVERY PEER
// MUST CARRY ITS OWN NAMESPACE SELECTOR. A derivation that defaulted a peer's
// namespace to the policy's would default it to the empty string and match
// nothing, which looks exactly like a correct suppression.
func buildAdminNetworkPolicyEdges(nodes []Node) []Edge {
	pods := podsByNamespace(nodes)
	namespaceLabels := namespaceLabelIndex(nodes)

	var out []Edge
	for i := range nodes {
		n := &nodes[i]
		kind := n.Metadata["resource_type"]
		if kind != "AdminNetworkPolicy" && kind != "BaselineAdminNetworkPolicy" {
			continue
		}
		if n.Content == "" {
			continue
		}
		var spec anpSpec
		if err := json.Unmarshal([]byte(n.Content), &spec); err != nil {
			continue
		}

		subjects := anpSubjectPods(spec, pods, namespaceLabels)
		if len(subjects) == 0 {
			continue
		}
		for _, rule := range spec.Spec.Ingress {
			peers := resolveANPPeers(rule.From, pods, namespaceLabels)
			out = append(out, pairEdges(subjects, peers, edgeANPIngressFrom)...)
		}
		for _, rule := range spec.Spec.Egress {
			peers := resolveANPPeers(rule.To, pods, namespaceLabels)
			out = append(out, pairEdges(subjects, peers, edgeANPEgressTo)...)
		}
	}
	return out
}

// anpSubjectPods resolves an ANP's subject to pods. A subject is either a
// namespace selector (every pod in the matching namespaces) or a pods selector
// (a namespace selector AND a pod selector).
func anpSubjectPods(spec anpSpec, pods map[string][]*Node, namespaceLabels map[string]map[string]string) []*Node {
	switch {
	case spec.Spec.Subject.Pods != nil:
		var out []*Node
		for _, ns := range matchNamespaces(namespaceLabels, spec.Spec.Subject.Pods.NamespaceSelector.MatchLabels) {
			out = append(out, matchPods(pods[ns], spec.Spec.Subject.Pods.PodSelector.MatchLabels)...)
		}
		return out
	case spec.Spec.Subject.Namespaces != nil:
		var out []*Node
		for _, ns := range matchNamespaces(namespaceLabels, spec.Spec.Subject.Namespaces.MatchLabels) {
			out = append(out, pods[ns]...)
		}
		return out
	default:
		return nil
	}
}

// resolvePeers turns a NetworkPolicy rule's peer list into pods.
//
// AN EMPTY LIST RETURNS NIL, and the caller emits nothing — see this file's
// header for why the most permissive policy is the quietest.
func resolvePeers(
	peers []netpolPeer,
	policyNamespace string,
	pods map[string][]*Node,
	namespaceLabels map[string]map[string]string,
) []*Node {
	if len(peers) == 0 {
		return nil
	}
	var out []*Node
	for i := range peers {
		p := &peers[i]
		if p.IPBlock != nil {
			// A CIDR outside the cluster: no node to point at.
			continue
		}
		namespaces := []string{policyNamespace}
		if p.NamespaceSelector != nil {
			namespaces = matchNamespaces(namespaceLabels, p.NamespaceSelector.MatchLabels)
		}
		for _, ns := range namespaces {
			if p.PodSelector == nil {
				out = append(out, pods[ns]...)
				continue
			}
			out = append(out, matchPods(pods[ns], p.PodSelector.MatchLabels)...)
		}
	}
	return out
}

// resolveANPPeers is resolvePeers for the ANP peer shape, which carries no
// policy namespace to default to.
func resolveANPPeers(
	peers []anpPeer,
	pods map[string][]*Node,
	namespaceLabels map[string]map[string]string,
) []*Node {
	if len(peers) == 0 {
		return nil
	}
	var out []*Node
	for i := range peers {
		p := &peers[i]
		if len(p.Networks) > 0 && p.Namespaces == nil && p.Pods == nil {
			// A networks peer is the ANP spelling of an ipBlock.
			continue
		}
		switch {
		case p.Pods != nil:
			for _, ns := range matchNamespaces(namespaceLabels, p.Pods.NamespaceSelector.MatchLabels) {
				out = append(out, matchPods(pods[ns], p.Pods.PodSelector.MatchLabels)...)
			}
		case p.Namespaces != nil:
			for _, ns := range matchNamespaces(namespaceLabels, p.Namespaces.MatchLabels) {
				out = append(out, pods[ns]...)
			}
		}
	}
	return out
}

// pairEdges renders the cross product of targets and peers, DROPPING SELF-PAIRS
// and deduping, in a stable order.
func pairEdges(targets, peers []*Node, edgeType string) []Edge {
	if len(targets) == 0 || len(peers) == 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []Edge
	for _, t := range targets {
		for _, p := range peers {
			if t.ID == p.ID {
				continue
			}
			key := t.ID + "\x00" + p.ID
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, Edge{FromID: t.ID, ToID: p.ID, Type: edgeType})
		}
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].FromID != out[b].FromID {
			return out[a].FromID < out[b].FromID
		}
		return out[a].ToID < out[b].ToID
	})
	return out
}

// matchPods filters pods by an equality selector. AN EMPTY SELECTOR MATCHES
// EVERY POD, which is the correct Kubernetes semantics: an empty podSelector on
// a peer means "every pod in the selected namespaces".
func matchPods(pods []*Node, selector map[string]string) []*Node {
	out := make([]*Node, 0, len(pods))
	for _, p := range pods {
		if labelsMatch(p.Metadata, selector) {
			out = append(out, p)
		}
	}
	return out
}

// namespaceLabelIndex maps a namespace name to its labels, so a
// namespaceSelector can be evaluated without re-reading the nodes.
//
// THE WELL-KNOWN NAME LABEL IS SYNTHESIZED when the Namespace object does not
// carry it. Kubernetes sets kubernetes.io/metadata.name on every namespace, and
// policies routinely select on it; a namespace fixture that omits it would make
// every such policy match nothing.
func namespaceLabelIndex(nodes []Node) map[string]map[string]string {
	index := map[string]map[string]string{}
	for i := range nodes {
		n := &nodes[i]
		if n.Metadata["resource_type"] != "Namespace" {
			continue
		}
		labels := map[string]string{}
		for k, v := range n.Metadata {
			if len(k) > len("label/") && k[:len("label/")] == "label/" {
				labels[k] = v
			}
		}
		if _, ok := labels["label/kubernetes.io/metadata.name"]; !ok {
			labels["label/kubernetes.io/metadata.name"] = n.SymbolName
		}
		index[n.SymbolName] = labels
	}
	return index
}

// matchNamespaces returns the namespaces whose labels satisfy a selector, in a
// stable order. An empty selector matches every namespace, which is the
// Kubernetes semantics for an empty namespaceSelector.
func matchNamespaces(index map[string]map[string]string, selector map[string]string) []string {
	var out []string
	for ns, labels := range index {
		if labelsMatch(labels, selector) {
			out = append(out, ns)
		}
	}
	sort.Strings(out)
	return out
}
