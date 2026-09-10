// SPDX-License-Identifier: Apache-2.0

package azure

// resolve.go — THE FIVE RELATIONSHIP RESOLVERS, RUN INSIDE THE WALK.
//
// WHY THEY ARE IN THE WALK AT ALL. Four relationship kinds cannot be drawn
// while enumerating one Azure service, because each needs a fact from another:
// an NSG rule's address ranges become reachability only once you know they are
// ranges rather than resources; a DNS record's raw address becomes a route only
// once you know which load balancer answers it; a site's container image
// becomes lineage only once you know which registry serves it; a federated
// credential becomes a trust only once you know the identity's own tenant. The
// builtin collector derives these AFTER its graph is written, by querying it
// back. A collector installed as a custom family gets no such pass — nothing
// compiled-in enriches its graph after the collect — so whatever this walk does
// not emit does not exist, and these five run here instead.
//
// THEY COST NO ROUND TRIPS. Every input is a resource or edge this walk already
// produced; they are a second pass over the walk's own output, after the
// fan-out has joined.
//
// NOTHING HERE IS BEST-EFFORT. The builtin provider hook this replaces logs
// each resolver's failure and returns nil, so a subscription whose enrichment
// silently produced nothing is indistinguishable from one with nothing to
// enrich. These five cannot fail at all — see [runResolvers] — so there is no
// failure to swallow, and a future resolver that CAN fail belongs in the walk's
// failure list rather than in a log line.

// runResolvers runs the five resolvers over one walk's merged output and
// returns the output with their additions.
//
// NONE OF THEM RETURNS AN ERROR, and that is a property rather than an
// omission: every one is a pure function over values this walk already holds,
// with no I/O to fail at, and malformed input is handled where it is read — a
// rule with no addresses draws nothing, an unresolvable target keeps its raw
// edge, a grant naming no known group is skipped. An error return here would be
// a branch nothing could reach, which is worse than none: it would look like a
// failure path that had been thought about.
//
// EACH RESOLVER READS THE PRE-RESOLUTION SNAPSHOT, never a sibling's additions.
// The alternative — letting resolver 5's new ASSUMES_ROLE edges feed resolver
// 4's guest-trust scan — would make the output depend on the order the
// resolvers happen to be listed in, which is exactly the kind of dependence
// that is invisible until someone reorders them.
func runResolvers(in walkOutcome) walkOutcome {
	resources := dedupeResources(in.resources)
	edges := dedupeEdges(in.edges)
	byType := resourceIndex(resources)

	// 1. NSG rules become reachability edges to address-range sentinels.
	nsgNodes, nsgEdges := resolveNSGRules(byType[rtNSG])

	// 2. DNS record targets that name a raw address are RE-TARGETED at the
	// resource answering it. This is the only resolver that removes anything.
	edges = resolveDNSRecordTargets(byType[rtDNSRecordSet], byType[rtLoadBalancer], edges)

	// 3. Container images on sites become lineage to the registry serving them.
	imageEdges := resolveImageLineage(byType[rtWebSite], byType[rtFunctionApp], byType[rtRegistry])

	// 4. Cross-tenant trust, from role assignments and federated credentials.
	trustEdges := resolveCrossTenantTrust(byType[rtManagedIdentity], edges)

	// 5. Group-terminating grants, where a principal id names a known group.
	groupEdges := resolveAADGroupAssignments(byType[rtAADGroup], byType[rtVault], byType[rtManagedIdentity], edges)

	return walkOutcome{
		resources: append(resources, nsgNodes...),
		edges:     append(edges, concatEdges(nsgEdges, imageEdges, trustEdges, groupEdges)...),
		failures:  in.failures,
	}
}

func concatEdges(groups ...[]edge) []edge {
	var out []edge
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// edgesOfType returns the edges of one relationship whose source is in the
// given id set, preserving order. It is the in-memory counterpart of the
// builtin resolvers' per-node edge browse.
func edgesOfType(edges []edge, relation string, from map[string]struct{}) []edge {
	var out []edge
	for _, e := range edges {
		if e.relation != relation {
			continue
		}
		if from != nil {
			if _, ok := from[e.from]; !ok {
				continue
			}
		}
		out = append(out, e)
	}
	return out
}

// idSet indexes resources by id, for the edge scans above.
func idSet(rs []resource) map[string]struct{} {
	out := make(map[string]struct{}, len(rs))
	for _, r := range rs {
		out[r.id] = struct{}{}
	}
	return out
}
