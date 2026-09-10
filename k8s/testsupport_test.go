// SPDX-License-Identifier: Apache-2.0

package main

import "maps"

// testsupport_test.go — the fixture builders every table in this suite shares.
//
// THEY BUILD CONTRACT NODES, NOT API OBJECTS. A derived pass reads a walk's own
// output, so its fixture is the node the converter produced rather than the
// Kubernetes object the converter read. Fixtures for the converters themselves
// build real typed API objects and hand them to a fake clientset; those live
// beside the tests that use them.

// node builds one contract node with the resource_type metadata every derived
// pass keys on.
func node(id, kind string, meta map[string]string) Node {
	m := map[string]string{"resource_type": kind}
	maps.Copy(m, meta)
	_, _, name, _ := splitID(id)
	return Node{
		ID:         id,
		Type:       nodeTypeCloudResource,
		SymbolName: name,
		Metadata:   m,
	}
}

// workloadNode builds a node for a Deployment, the kind carrying a pod template
// that every table here exercises.
func workloadNode(id string, meta map[string]string) Node {
	return node(id, "Deployment", meta)
}

// namespacedNode builds a node carrying the namespace metadata key that the
// namespace-membership derivation reads.
func namespacedNode(namespace, kind, name string, meta map[string]string) Node {
	m := map[string]string{"namespace": namespace}
	maps.Copy(m, meta)
	return node(resourceID(namespace, kind, name), kind, m)
}

// edgeTypes reduces an edge slice to the multiset of its type strings, which is
// what most assertions here are about.
func edgeTypes(edges []Edge) []string {
	out := make([]string, 0, len(edges))
	for _, e := range edges {
		out = append(out, e.Type)
	}
	return out
}

// findEdge returns the first edge of the given type, or the zero Edge when the
// slice carries none.
func findEdge(edges []Edge, typ string) Edge {
	for _, e := range edges {
		if e.Type == typ {
			return e
		}
	}
	return Edge{}
}
