// SPDX-License-Identifier: Apache-2.0

package azure

import "strings"

// resolve_groups.go — RESOLVER 5: a grant naming a raw principal id that turns
// out to be a known group gets a SECOND edge terminating on the group node.
//
// IT ADDS AND NEVER REPLACES. The original edge to the raw principal id stays:
// it is what the source data says, and a consumer asking "who did Azure name
// here" must still be able to answer. Emitting the group edge as a REWRITE
// would silently discard that.
//
// TWO HALVES, ONE OF WHICH CANNOT FIRE ON THIS WALK'S OWN DATA.
//
//   - The access half runs from a vault to the group, and fires on ordinary
//     data. It takes EVERY outgoing grant on the vault and filters on nothing
//     but the group index, which matters because a vault has two grant sources:
//     legacy access policies and RBAC role assignments. A subscription that
//     uses RBAC exercises only the second, so both need a fixture or the walk
//     ships tested against half its input.
//   - The role half runs from the group to the assignment's scope and passes
//     TWO gates, both unsatisfiable from this walk: the identity's principal id
//     must name a group node, and a managed identity's principal is a service
//     principal object rather than a group; and the assignment's principal type
//     must read Group, which the per-identity assignment list cannot report for
//     the reason resolve_trust.go states. It is implemented for the same reason
//     resolver 4's RBAC half is, and its test builds its input by hand and says
//     so.

// methodGroupResolve marks every edge this resolver emits.
const methodGroupResolve = "azure-aad-group-resolve"

// aadGroupIDPrefix is the namespace of a group node id. The object id follows
// it, which is how a raw principal id in a grant is matched back to a node.
const aadGroupIDPrefix = "azure:aad:group/"

// resolveAADGroupAssignments returns the group-terminating grants implied by
// the walk's vault grants and role assignments.
func resolveAADGroupAssignments(groups, vaults, identities []resource, edges []edge) []edge {
	index := groupIndex(groups)
	if len(index) == 0 {
		return nil
	}
	out := accessedByGroups(vaults, edges, index)
	out = append(out, assumesRoleGroups(identities, edges, index)...)
	return out
}

// groupIndex maps each group's Entra object id to its node id.
func groupIndex(groups []resource) map[string]string {
	index := make(map[string]string, len(groups))
	for _, g := range groups {
		objectID := strings.TrimPrefix(g.id, aadGroupIDPrefix)
		if objectID == g.id || objectID == "" {
			continue
		}
		index[objectID] = g.id
	}
	return index
}

// accessedByGroups returns an additional grant edge from each vault to the
// group node its raw principal id names.
func accessedByGroups(vaults []resource, edges []edge, index map[string]string) []edge {
	var out []edge
	for _, e := range edgesOfType(edges, edgeAccessedBy, idSet(vaults)) {
		groupNode, ok := index[e.to]
		if !ok {
			continue
		}
		out = append(out, edge{
			from:     e.from,
			to:       groupNode,
			relation: edgeAccessedBy,
			metadata: e.metadata,
			method:   methodGroupResolve,
		})
	}
	return out
}

// assumesRoleGroups returns an additional assignment edge from the group node
// to the assignment's scope, for an identity whose principal id names a group.
func assumesRoleGroups(identities []resource, edges []edge, index map[string]string) []edge {
	var out []edge
	for _, identity := range identities {
		// GATE ONE, checked before any edge is walked.
		if identity.principalID == "" {
			continue
		}
		groupNode, ok := index[identity.principalID]
		if !ok {
			continue
		}
		for _, e := range edges {
			if e.relation != edgeAssumesRole || e.from != identity.id {
				continue
			}
			// GATE TWO.
			if e.metadata[mdPrincipalType] != "Group" {
				continue
			}
			out = append(out, edge{
				from:     groupNode,
				to:       e.to,
				relation: edgeAssumesRole,
				metadata: e.metadata,
				method:   methodGroupResolve,
			})
		}
	}
	return out
}
