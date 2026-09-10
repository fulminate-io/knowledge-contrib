// SPDX-License-Identifier: Apache-2.0

package azure

import "testing"

// resolve_groups_test.go — resolver 5, one row per half, and the metadata keys
// each gates on.
//
// THIS RESOLVER LOSES NO EDGE TYPE WHEN IT STOPS, which is what makes it the
// one a count of relationship types cannot protect: both halves emit a
// relationship the walk already emits elsewhere, and what disappears when they
// stop is the ability to traverse from a resource through a group to its
// members.

// TestResolveAADGroupAssignments_AccessHalfHasTwoInputArms, and it filters on
// NEITHER. A vault has two grant sources, and a subscription on role
// assignments exercises only the second, so a resolver tested against the first
// alone would be tested against half its input.
func TestResolveAADGroupAssignments_AccessHalfHasTwoInputArms(t *testing.T) {
	group := fx{t}.res(groupResource(directoryGroup()))
	vault := fx{t}.res(vaultResource(keyVault()))
	groupNode := aadGroupIDPrefix + groupObjectID

	for _, tc := range []struct {
		name  string
		input []edge
		want  string
	}{
		{"legacy access policy", fx{t}.edges(accessPolicyEdges(keyVault())), vaultGrantSourcePolicy},
		{"role assignment", vaultRoleAssignmentEdges(vaultID, vaultRoleAssignments()), vaultGrantSourceRBAC},
	} {
		t.Run(tc.name, func(t *testing.T) {
			edges := resolveAADGroupAssignments([]resource{group}, []resource{vault}, nil, tc.input)
			added, ok := edgeBetween(edges, vaultID, groupNode, edgeAccessedBy)
			if !ok {
				t.Fatalf("a %s naming a known group drew no edge to the group node: %v", tc.name, edges)
			}
			if added.metadata[mdSource] != tc.want {
				t.Errorf("the added edge lost the grant source it was derived from: %v", added.metadata)
			}
			if added.method != methodGroupResolve {
				t.Errorf("the added edge is not marked as derived: %q", added.method)
			}
		})
	}
}

// TestResolveAADGroupAssignments_TheOriginalEdgeIsNeverReplaced. The original
// grant is what Azure itself says, and a consumer asking "who did Azure name
// here" must still be able to answer; emitting the group edge as a rewrite
// would silently discard that.
func TestResolveAADGroupAssignments_TheOriginalEdgeIsNeverReplaced(t *testing.T) {
	group := fx{t}.res(groupResource(directoryGroup()))
	vault := fx{t}.res(vaultResource(keyVault()))
	original := fx{t}.edges(accessPolicyEdges(keyVault()))

	added := resolveAADGroupAssignments([]resource{group}, []resource{vault}, nil, original)
	// The resolver RETURNS ONLY ITS ADDITIONS: the walk appends them to the
	// edges it already had, so the original survives by construction. This
	// asserts the shape that makes that true.
	for _, e := range added {
		if e.to == groupObjectID {
			t.Error("the resolver returned an edge to the raw principal, which would duplicate the original")
		}
	}
	if _, ok := edgeBetween(original, vaultID, groupObjectID, edgeAccessedBy); !ok {
		t.Error("the original grant to the raw principal is not in the walk's own edges")
	}
}

// TestResolveAADGroupAssignments_AGrantNamingNoKnownGroupDrawsNothing. Most
// grants name a user or a service principal, and a resolver that resolved
// those against the group index would invent memberships.
func TestResolveAADGroupAssignments_AGrantNamingNoKnownGroupDrawsNothing(t *testing.T) {
	group := fx{t}.res(groupResource(directoryGroup()))
	vault := fx{t}.res(vaultResource(keyVault()))

	stranger := []edge{{
		from: vaultID, to: "99999999-9999-9999-9999-999999999999",
		relation: edgeAccessedBy, metadata: map[string]string{mdSource: vaultGrantSourcePolicy},
	}}
	got := resolveAADGroupAssignments([]resource{group}, []resource{vault}, nil, stranger)
	if len(got) != 0 {
		t.Errorf("a grant naming an unknown principal drew %d edges", len(got))
	}

	// With NO groups walked at all the resolver has no index and adds nothing,
	// which is the arm a subscription whose credential cannot read the
	// directory hits.
	none := resolveAADGroupAssignments(nil, []resource{vault}, nil, fx{t}.edges(accessPolicyEdges(keyVault())))
	if len(none) != 0 {
		t.Errorf("with no group in the walk, %d edges were added", len(none))
	}
}

// TestResolveAADGroupAssignments_RoleHalfPassesTwoGates, both unsatisfiable
// from this collector's own walk, so its input is built by hand and this row
// says so.
func TestResolveAADGroupAssignments_RoleHalfPassesTwoGates(t *testing.T) {
	group := fx{t}.res(groupResource(directoryGroup()))
	groupNode := aadGroupIDPrefix + groupObjectID

	// An identity whose principal id IS a group's object id, which a
	// user-assigned managed identity's never is: its principal is a service
	// principal object.
	identity := fx{t}.res(identityResource(managedIdentity()))
	identity.principalID = groupObjectID

	assignment := []edge{{
		from: identityA, to: scopeID, relation: edgeAssumesRole,
		metadata: map[string]string{mdPrincipalType: "Group"},
	}}
	edges := resolveAADGroupAssignments([]resource{group}, nil, []resource{identity}, assignment)
	if _, ok := edgeBetween(edges, groupNode, scopeID, edgeAssumesRole); !ok {
		t.Fatalf("with both gates satisfied the group-terminating assignment was not drawn: %v", edges)
	}

	// GATE ONE, inverted: the identity's principal names no group.
	notAGroup := identity
	notAGroup.principalID = principalOne
	if got := resolveAADGroupAssignments([]resource{group}, nil, []resource{notAGroup}, assignment); len(got) != 0 {
		t.Errorf("an identity whose principal names no group drew %d edges", len(got))
	}

	// GATE ONE, the metadata mutation: stop stamping the principal id at all.
	unstamped := identity
	unstamped.principalID = ""
	if got := resolveAADGroupAssignments([]resource{group}, nil, []resource{unstamped}, assignment); len(got) != 0 {
		t.Errorf("an identity carrying no principal id drew %d edges", len(got))
	}

	// GATE TWO, inverted: the assignment's principal type is not a group.
	servicePrincipal := []edge{{
		from: identityA, to: scopeID, relation: edgeAssumesRole,
		metadata: map[string]string{mdPrincipalType: "ServicePrincipal"},
	}}
	if got := resolveAADGroupAssignments([]resource{group}, nil, []resource{identity}, servicePrincipal); len(got) != 0 {
		t.Errorf("an assignment whose principal is not a group drew %d edges", len(got))
	}

	// AND WHAT THIS WALK ACTUALLY PRODUCES, measured: the identity walk's own
	// assignments report ServicePrincipal, so this half draws nothing from
	// them.
	fromThisWalk := roleAssignmentEdges(identityA, roleAssignments())
	if got := resolveAADGroupAssignments([]resource{group}, nil, []resource{identity}, fromThisWalk); len(got) != 0 {
		t.Errorf("this walk's own assignments drew %d group edges; the role half was thought unreachable from here", len(got))
	}
}

// TestGroupIndex_KeysOnTheObjectIdInsideTheNodeId. A grant names a bare object
// id, and the index is what turns it into a node; a mis-keyed index resolves
// nothing while looking populated.
func TestGroupIndex_KeysOnTheObjectIdInsideTheNodeId(t *testing.T) {
	group := fx{t}.res(groupResource(directoryGroup()))
	index := groupIndex([]resource{group})
	if got := index[groupObjectID]; got != aadGroupIDPrefix+groupObjectID {
		t.Errorf("the index maps %q to %q", groupObjectID, got)
	}
	// A resource that is not a group node contributes nothing, which keeps an
	// unrelated synthetic id out of the index.
	other := resource{id: "azure:ca/Example CA", resourceType: rtCertAuthority}
	if len(groupIndex([]resource{other})) != 0 {
		t.Error("a node that is not a directory group entered the group index")
	}
}
