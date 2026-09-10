// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"fmt"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// proxy.go — the cloud-linkage families: the proxy node, its EMITTED_BY edge,
// and the CORRELATES_WITH edge.
//
// WHY THIS COLLECTOR EMITS THEM RATHER THAN THE CLIENT. The client's
// post-collect tail is gated on registration, and the log materializer that
// built these three for the built-in loki family is reachable only from the
// built-in logs collect. For a registered custom family the client writes NO
// proxy node, NO EMITTED_BY and NO CORRELATES_WITH, so this module's walk is
// the only writer of them and they are part of its floor.
//
// WHY NO CONTRACT CHANGE IS NEEDED FOR THE SHAPE. A proxy is an ORDINARY LOCAL
// node whose cross-graph reference rides its deterministic id, its Source and
// its foreign_graph / foreign_id metadata; both edges join ids INSIDE the
// emitted graph. An edge whose ENDPOINT lives in another graph is a different
// thing that the contract cannot carry, and this collector emits none.
//
// THE CORRELATION HALF IS NOT IN THIS FILE ANY MORE. Its detector, its
// confirmation rule and its evidence string live in the common correlation
// module, which every logs collector requires; correlate.go is the adapter and
// nothing here renders an edge for it.
//
// WHAT THIS COLLECTOR DOES NOT DO IS QUERY A CLOUD. It holds no cloud session
// and no cloud graph, and it never will: the (account, resource) pairs are
// derived from the DECLARED foreign-graph context block the collect input
// carries, by resolve.go, and arrive here as Resolutions. A collect whose entry
// declares no cloud family resolves nothing and emits nothing — see the zero
// arm below, which is parity-correct because the built-in path emits none of
// these families with no cloud graph attached either.

// emitProxies builds one proxy node per DISTINCT resolved cloud resource and
// one EMITTED_BY edge per resolution.
//
// THE COUNTS DIFFER ON PURPOSE. Two labels resolving to one resource yield ONE
// node and TWO edges: the node is the resource and the edges are the claims
// about it. Emitting a second node for the same resource would put a duplicate
// id in the batch.
func emitProxies(resolutions []Resolution) ([]framework.Node, []framework.Edge, error) {
	if len(resolutions) == 0 {
		return nil, nil, nil
	}
	nodes := make([]framework.Node, 0, len(resolutions))
	edges := make([]framework.Edge, 0, len(resolutions))
	seen := make(map[string]struct{}, len(resolutions))
	for i, r := range resolutions {
		if r.Account == "" || r.ResourceID == "" {
			return nil, nil, fmt.Errorf(
				"logpipe: resolution %d for label %q=%q names account=%q resource=%q; "+
					"a proxy id is built from both, so neither may be empty",
				i, r.LabelKey, r.LabelValue, r.Account, r.ResourceID)
		}
		id := ProxyID(r.Account, r.ResourceID)
		if _, dup := seen[id]; !dup {
			seen[id] = struct{}{}
			nodes = append(nodes, framework.Node{
				ID:         id,
				Type:       NodeProxy,
				SymbolName: r.LabelKey + "=" + r.LabelValue,
				Source:     ProxySource(r.Account),
				Description: fmt.Sprintf("cloud proxy for log label %s=%s (resolved to %s/%s)",
					r.LabelKey, r.LabelValue, r.Account, r.ResourceID),
				Metadata: map[string]string{
					"foreign_graph": proxyForeignGraph,
					"foreign_id":    r.ResourceID,
					"account":       r.Account,
					"foreign_type":  NodeLogLabel,
				},
			})
		}
		edges = append(edges, framework.Edge{
			FromID: LabelNodeID(r.LabelKey, r.LabelValue),
			ToID:   id,
			Type:   EdgeEmittedBy,
		})
	}
	return nodes, edges, nil
}

// ProxyID is the deterministic cross-graph proxy id for a cloud resource. The
// format is byte-identical to the one the knowledge client and server both
// build, which is what makes a proxy this collector emits the SAME node the
// rest of the product would have built for that resource.
func ProxyID(account, resourceID string) string {
	return "proxy:cloud:" + account + ":" + resourceID
}

// ProxySource is the Source stamped on such a proxy: the account, without the
// resource.
func ProxySource(account string) string { return "proxy:cloud:" + account }
