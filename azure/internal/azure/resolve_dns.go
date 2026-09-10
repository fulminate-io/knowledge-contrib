// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"maps"
	"net"
)

// resolve_dns.go — RESOLVER 2: a DNS record whose target is a raw address
// becomes a route to the resource answering at that address.
//
// IT IS THE ONLY RESOLVER THAT REMOVES ANYTHING, and the removal is why it must
// run in the walk rather than over a written graph. The builtin version rewrites
// an already-written graph: it unlinks the raw-address edge, then relinks the
// resolved one, so a version of it that unlinks without a working index deletes
// every unresolvable edge. In the walk the shape inverts and the hazard goes
// with it — the raw-address edge is simply not carried forward for a target
// that resolves, and an UNRESOLVABLE target keeps its raw-address edge
// untouched, because that edge is the only record that the record set points
// somewhere at all.
//
// THE INDEX IS LOAD BALANCERS ONLY. It is built from each load balancer's
// frontend private addresses, which the walk carried forward as a fact. Virtual
// machine addresses are deliberately absent: a VM's address lives on its
// network interface, which this walk enumerates only far enough to resolve
// subnets, so an index claiming to cover VMs would silently cover none of them.

// methodDNSResolve marks an edge this resolver re-targeted, and its evidence
// carries the raw address the record actually named, so the rewrite is
// reversible by a reader.
const methodDNSResolve = "azure-dns-resolve"

// resolveDNSRecordTargets rewrites every ROUTES_TO edge from a DNS record set
// whose target is an address the index answers. It returns the whole edge list
// with those edges replaced IN PLACE, so the surrounding order is unchanged.
func resolveDNSRecordTargets(records, loadBalancers []resource, edges []edge) []edge {
	index := frontendAddressIndex(loadBalancers)
	if len(index) == 0 {
		return edges
	}
	recordIDs := idSet(records)

	out := make([]edge, 0, len(edges))
	for _, e := range edges {
		out = append(out, retargetDNSEdge(e, recordIDs, index))
	}
	return out
}

// retargetDNSEdge returns the edge re-targeted at the resource answering its
// raw address, or the edge unchanged when nothing answers.
func retargetDNSEdge(e edge, recordIDs map[string]struct{}, index map[string]string) edge {
	if e.relation != edgeRoutesTo {
		return e
	}
	if _, isRecord := recordIDs[e.from]; !isRecord {
		return e
	}
	if isARMResourceID(e.to) {
		// Already a resource: an alias record names its target directly.
		return e
	}
	resolved, ok := index[e.to]
	if !ok {
		// UNRESOLVABLE, AND THE EDGE SURVIVES. A record pointing at an address
		// outside this subscription is a real fact about the record.
		return e
	}
	md := map[string]string{"resolved_from": e.to}
	maps.Copy(md, e.metadata)
	return edge{from: e.from, to: resolved, relation: edgeRoutesTo, metadata: md, method: methodDNSResolve}
}

// frontendAddressIndex maps each load balancer's frontend addresses to its id.
// A malformed address is skipped rather than indexed: an index entry that is
// not an address can never be hit by a record's target and would only make the
// index bigger.
func frontendAddressIndex(loadBalancers []resource) map[string]string {
	index := make(map[string]string)
	for _, lb := range loadBalancers {
		for _, addr := range lb.lbFrontendIPs {
			if net.ParseIP(addr) == nil {
				continue
			}
			// FIRST WRITER WINS, matching the walk's own merge order, so two
			// load balancers sharing an address resolve the same way on every
			// collect rather than by map iteration luck.
			if _, dup := index[addr]; dup {
				continue
			}
			index[addr] = lb.id
		}
	}
	return index
}
