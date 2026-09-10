// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"
	"sort"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// proxy.go — THE CLOUD-LINKED HALF OF THE VOCABULARY: the proxy node, the
// EMITTED_BY edge that reaches it and the CORRELATES_WITH edge between two
// templates.
//
// WHY A COLLECTOR CAN EMIT THESE AT ALL, since "cross-graph" sounds like
// something a single-graph result cannot express. A proxy is an ORDINARY LOCAL
// node of type proxy; what makes it cross-graph is its IDENTITY — the id
// convention below, plus foreign_graph and foreign_id in its metadata — and not
// any field of the result. Both edge families join two ids INSIDE this graph:
// EMITTED_BY runs from a label node to the proxy node beside it, and
// CORRELATES_WITH runs between two templates. So the contract expresses all
// three today, and no target-graph field is needed.
//
// WHAT THIS COLLECTOR CANNOT DO ON ITS OWN is answer the two CLOUD questions:
// which resource a service label names, and whether two resources depend on each
// other. Both are properties of the operator's cloud graph, which a collector
// process cannot read, so both arrive in the declared context block the collect
// carries (see resolve.go). With none declared this collector emits none of the
// three — exactly what the built-in path does with no cloud graph attached.
//
// THE CORRELATION EDGE IS RENDERED BY THE COMMON MODULE, not here. Its evidence
// string is an informal contract with a checked-in parity golden and with
// anyone reading an edge, and it had three independent copies before that module
// existed. Rendering it beside the detector that produces the numbers is what
// keeps it one thing; this file emits the proxy half only.

// proxyIDPrefix and proxySourcePrefix build a cloud proxy's identity. The
// convention is byte-identical to the one the client and the server both use,
// which is what lets a proxy this collector emits and a proxy the client builds
// for the same resource be the SAME node rather than two.
const (
	proxyIDPrefix     = "proxy:cloud:"
	proxySourcePrefix = "proxy:cloud:"
)

// materializeProxies builds one proxy node per DISTINCT resolved resource and
// one EMITTED_BY edge per resolution.
//
// THE NODE IS DEDUPLICATED AND THE EDGE IS NOT, which is the asymmetry to keep:
// two labels resolving to one cloud resource are two facts about that resource
// and must both be recorded, while the resource itself is one node. Collapsing
// the edges would silently drop the second label's link.
func materializeProxies(resolutions []ResolvedProxy) ([]framework.Node, []framework.Edge) {
	if len(resolutions) == 0 {
		return nil, nil
	}
	nodes := make([]framework.Node, 0, len(resolutions))
	edges := make([]framework.Edge, 0, len(resolutions))
	seen := make(map[string]struct{}, len(resolutions))
	for _, r := range resolutions {
		proxy := buildCloudProxy(r)
		if _, dup := seen[proxy.ID]; !dup {
			seen[proxy.ID] = struct{}{}
			nodes = append(nodes, proxy)
		}
		edges = append(edges, framework.Edge{
			FromID: LabelNodeID(r.LabelKey, r.LabelValue),
			ToID:   proxy.ID,
			Type:   edgeEmittedBy,
		})
	}
	return nodes, edges
}

// buildCloudProxy stamps one cloud proxy node.
//
// Its fields are the ones the shared proxy builder derives for a log-label
// source: SymbolName and Description come from the label, the id and Source
// come from the account and resource id, and the metadata carries the foreign
// reference plus the account and the source node's own type. The three optional
// display keys the shared builder copies — resource_type, region and provider —
// are absent here because a log-label source carries none of them.
func buildCloudProxy(r ResolvedProxy) framework.Node {
	return framework.Node{
		ID:         proxyIDPrefix + r.Account + ":" + r.ResourceID,
		Type:       nodeProxy,
		SymbolName: r.LabelKey + "=" + r.LabelValue,
		Source:     proxySourcePrefix + r.Account,
		Description: fmt.Sprintf("cloud proxy for log label %s=%s (resolved to %s/%s)",
			r.LabelKey, r.LabelValue, r.Account, r.ResourceID),
		Metadata: map[string]string{
			"foreign_graph": "cloud",
			"foreign_id":    r.ResourceID,
			"account":       r.Account,
			"foreign_type":  nodeLogLabel,
		},
	}
}

// sortedKeys returns a map's keys in ascending order. Every emission path that
// walks a map goes through it: Go randomizes map iteration, so a walk without
// it would emit the same graph in a different order on every run and no golden
// comparison could hold.
func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
