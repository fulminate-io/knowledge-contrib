// SPDX-License-Identifier: Apache-2.0

package main

import (
	"fmt"

	"github.com/fulminate-io/knowledge-contrib/common/correlation"
	"github.com/fulminate-io/knowledge-contrib/framework"
)

// proxy.go — the CLOUD-LINKED EMISSION: one proxy node per resolved cloud
// resource, one EMITTED_BY edge per resolution, one CORRELATES_WITH edge per
// confirmed correlation.
//
// A PROXY NODE CROSSES NO GRAPH BOUNDARY. It is an ordinary node in this
// collector's own graph whose id, source and metadata POINT AT a node in
// another one; nothing about emitting it needs the contract to address a
// foreign graph, and both edges below run between nodes this same batch
// carries. What this module cannot do alone is decide WHICH cloud resource a
// log label names — that is a fact about the operator's cloud graph, and it
// arrives as declared context on the collect input. See resolve.go.
//
// THE ID CONVENTION IS NOT THIS MODULE'S TO CHOOSE. `proxy:cloud:<account>:<id>`
// is the form the knowledge client and server both build, so a proxy emitted
// here is the same node as one built there and the two reconcile. A different
// spelling would produce a second node for the same resource.

// proxyForeignGraph is the graph family a proxy from this collector points into.
const proxyForeignGraph = "cloud"

// correlationMethod is the Method every CORRELATES_WITH edge carries: the two
// signals that had to agree for the edge to exist at all. IT IS THE COMMON
// MODULE'S VALUE, not a second spelling of it — the evidence string and the
// method are one contract with every consumer, and this module renders neither
// itself any more.
const correlationMethod = correlation.CorrelationMethod

// materializeProxies builds the proxy nodes and their EMITTED_BY edges.
//
// THE NODE IS DEDUPLICATED AND THE EDGE IS NOT, and that asymmetry is the whole
// shape of this function. Two different log labels can resolve to one cloud
// resource: that is ONE resource, so one node, and TWO facts about it, so two
// edges from two different label nodes. Deduplicating the edge as well — the
// change a reader makes by symmetry — silently drops one of the two facts.
//
// A fully duplicated edge cannot arise, because the resolutions themselves are
// deduplicated by (key, value) upstream in resolve.go.
func materializeProxies(resolutions []resolvedProxyEntry) ([]framework.Node, []framework.Edge, error) {
	if len(resolutions) == 0 {
		return nil, nil, nil
	}
	nodes := make([]framework.Node, 0, len(resolutions))
	edges := make([]framework.Edge, 0, len(resolutions))
	seen := make(map[string]struct{}, len(resolutions))
	for _, r := range resolutions {
		proxy, err := buildCloudProxy(r)
		if err != nil {
			return nil, nil, err
		}
		if _, dup := seen[proxy.ID]; !dup {
			seen[proxy.ID] = struct{}{}
			nodes = append(nodes, proxy)
		}
		edges = append(edges, edgeByID(labelNodeID(r.LabelKey, r.LabelValue), proxy.ID, edgeTypeEmittedBy))
	}
	return nodes, edges, nil
}

// buildCloudProxy stamps one proxy node.
//
// BOTH IDENTIFIERS ARE REQUIRED AND THEIR ABSENCE IS AN ERROR rather than a
// skip: a proxy missing its account or its resource id has an id that names no
// resource, and emitting it would put a node in the graph that resolves to
// nothing while looking exactly like one that does.
func buildCloudProxy(r resolvedProxyEntry) (framework.Node, error) {
	if r.Account == "" {
		return framework.Node{}, fmt.Errorf(
			"stackdriver: the resolution for log label %s=%s names resource %q with no account; "+
				"a cloud proxy id is proxy:cloud:<account>:<resource> and cannot be built without one",
			r.LabelKey, r.LabelValue, r.ResourceID)
	}
	if r.ResourceID == "" {
		return framework.Node{}, fmt.Errorf(
			"stackdriver: the resolution for log label %s=%s names account %q with no resource id; "+
				"a cloud proxy id is proxy:cloud:<account>:<resource> and cannot be built without one",
			r.LabelKey, r.LabelValue, r.Account)
	}
	return framework.Node{
		ID:         "proxy:cloud:" + r.Account + ":" + r.ResourceID,
		Type:       nodeTypeProxy,
		SymbolName: r.LabelKey + "=" + r.LabelValue,
		Source:     "proxy:cloud:" + r.Account,
		Description: fmt.Sprintf("cloud proxy for log label %s=%s (resolved to %s/%s)",
			r.LabelKey, r.LabelValue, r.Account, r.ResourceID),
		Metadata: map[string]string{
			"foreign_graph": proxyForeignGraph,
			"foreign_id":    r.ResourceID,
			"account":       r.Account,
			"foreign_type":  nodeTypeLogLabel,
		},
	}, nil
}
