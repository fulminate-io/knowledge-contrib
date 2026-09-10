// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"sort"
)

// derived_membership.go — IN_NAMESPACE and SELECTS: the two derivations that
// are a function of the whole enumeration rather than of any single object.
//
// THEY READ THIS WALK'S OWN NODES AND NOTHING ELSE. That is what lets them run
// in this process with no graph access at all: everything they need is what
// phase 1 just produced.

// buildNamespaceMembershipEdges renders IN_NAMESPACE for every namespaced node
// whose Namespace node this walk actually enumerated.
//
// THE EXISTENCE GATE IS THE POINT. A namespaced node whose Namespace node is
// absent from this walk — because the namespace listing failed, or because the
// namespace was deleted between two listings — gets NO edge, rather than an
// edge to an id nothing resolves. A dangling membership edge is worse than a
// missing one: it reads as a namespace that exists and holds one object.
//
// THREE KINDS OF NODE PRODUCE NOTHING, and only one of them is a skip worth
// counting: a Namespace node itself (a namespace is not in a namespace), a
// cluster-scoped node (there is no namespace to be in), and a namespaced node
// whose Namespace is missing (which IS the skip, and is reported).
func buildNamespaceMembershipEdges(nodes []Node) []Edge {
	edges, _ := namespaceMembership(nodes)
	return edges
}

// namespaceMembership returns the edges and the count of nodes whose Namespace
// node was absent, so a test can assert the skip rather than read a zero edge
// count as success.
func namespaceMembership(nodes []Node) (edges []Edge, skipped int) {
	present := make(map[string]bool)
	for i := range nodes {
		n := &nodes[i]
		if n.Metadata["resource_type"] == "Namespace" {
			present[n.SymbolName] = true
		}
	}

	for i := range nodes {
		n := &nodes[i]
		if n.Metadata["resource_type"] == "Namespace" {
			continue
		}
		ns := n.Metadata["namespace"]
		if ns == "" {
			continue
		}
		if !present[ns] {
			skipped++
			continue
		}
		edges = append(edges, Edge{
			FromID: n.ID,
			ToID:   resourceID("", "Namespace", ns),
			Type:   edgeInNamespace,
		})
	}
	return edges, skipped
}

// buildSelectsEdges renders SELECTS from a Service's label selector to the pods
// it matches.
//
// THREE GATES, AND THE THIRD IS THE ONE THAT SURPRISES PEOPLE:
//
//   - The Service must declare a selector. A headless or externally-managed
//     Service has none and selects nothing.
//   - The pod must be in the SERVICE'S OWN NAMESPACE. A Kubernetes selector
//     never crosses namespaces, so an identically labeled pod one namespace
//     over is not selected, and an edge to it would be a fabricated dependency.
//   - The selector must be NON-EMPTY. An empty equality selector matches every
//     pod by the ordinary rule, which would wire one Service to every pod in
//     its namespace; the walk carries a selector key only for a Service that
//     declared a non-empty one, so an empty map never reaches here as "matches
//     everything".
func buildSelectsEdges(nodes []Node) []Edge {
	pods := podsByNamespace(nodes)

	var out []Edge
	for i := range nodes {
		n := &nodes[i]
		if n.Metadata["resource_type"] != "Service" {
			continue
		}
		selector := parseSelector(n.Metadata["selector"])
		if len(selector) == 0 {
			continue
		}
		for _, pod := range pods[n.Metadata["namespace"]] {
			if labelsMatch(pod.Metadata, selector) {
				out = append(out, Edge{FromID: n.ID, ToID: pod.ID, Type: edgeSelects})
			}
		}
	}
	return out
}

// podsByNamespace indexes this walk's Pod nodes, in a stable order so two
// collects of an unchanged cluster produce edges in the same order.
func podsByNamespace(nodes []Node) map[string][]*Node {
	index := map[string][]*Node{}
	for i := range nodes {
		n := &nodes[i]
		if n.Metadata["resource_type"] != "Pod" {
			continue
		}
		ns := n.Metadata["namespace"]
		index[ns] = append(index[ns], n)
	}
	for ns := range index {
		sort.Slice(index[ns], func(a, b int) bool { return index[ns][a].ID < index[ns][b].ID })
	}
	return index
}

// parseSelector decodes a selector metadata value.
//
// A VALUE THAT DOES NOT DECODE YIELDS NO SELECTOR, so the caller's non-empty
// gate suppresses the family for that object. Returning an empty map that then
// matched everything would turn a corrupt value into a fan-out.
func parseSelector(encoded string) map[string]string {
	if encoded == "" {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(encoded), &m); err != nil {
		return nil
	}
	return m
}

// labelsMatch reports whether a node's label metadata satisfies every pair of
// an equality selector.
//
// IT IS NEVER CALLED WITH AN EMPTY SELECTOR by any caller in this package: an
// empty selector satisfies vacuously, which is the correct Kubernetes semantics
// and the wrong thing to emit edges from. Every caller gates on non-emptiness
// first, and each says so at its own gate.
func labelsMatch(meta map[string]string, selector map[string]string) bool {
	for k, v := range selector {
		if meta["label/"+k] != v {
			return false
		}
	}
	return true
}
