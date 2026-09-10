// SPDX-License-Identifier: Apache-2.0

package azure

import "testing"

// resolve_trust_test.go — resolver 4, one row per half, and the metadata keys
// each half gates on.
//
// ONE HALF CANNOT FIRE ON THIS COLLECTOR'S OWN DATA and its row says so: the
// role assignments this walk gathers come from a list filtered by the walked
// identity's own principal, so every one of them reports THAT identity's
// principal type, which for a user-assigned managed identity is
// ServicePrincipal and never Guest. The half is implemented and tested from a
// HAND-BUILT edge, because it is the only emitter of its half of this
// relationship and because an operator whose assignments arrive from another
// source would need it — and a green row here is coverage of the resolver, not
// of the pipeline.

// identityWithTenant is the walked identity every row below reads.
func identityWithTenant(t *testing.T) resource {
	t.Helper()
	return fx{t}.res(identityResource(managedIdentity()))
}

// TestResolveCrossTenantTrust_FederatedHalf. The source is always an oidc node
// and never a Kubernetes or GitHub one, and the two functions have to be read
// together to see why: the source id is minted as a Kubernetes service account
// for an AKS issuer and a github node for a GitHub issuer, and the
// external-issuer gate refuses exactly those two.
func TestResolveCrossTenantTrust_FederatedHalf(t *testing.T) {
	identity := identityWithTenant(t)
	oidcSource := "oidc:" + foreignIssuer + "/workload-one"

	edges := resolveCrossTenantTrust([]resource{identity}, []edge{{
		from: oidcSource, to: identityA, relation: edgeWorkloadIdentity,
		metadata: map[string]string{mdIssuer: foreignIssuer},
	}})
	e, ok := edgeBetween(edges, oidcSource, identityA, edgeTrusts)
	if !ok {
		t.Fatalf("a federation from outside the tenant drew no trust edge: %v", edges)
	}
	if e.metadata[mdIssuer] != foreignIssuer {
		t.Errorf("the trust edge does not carry the issuer it was drawn from: %v", e.metadata)
	}

	// THE TWO NEGATIVE CONTROLS, through the same path. Neither an AKS issuer
	// nor a GitHub one is a cross-tenant relationship, and both are refused by
	// the gate rather than by the absence of a node.
	for _, tc := range []struct {
		name   string
		source string
		issuer string
	}{
		{"kubernetes", "apps/ServiceAccount/api", aksIssuer},
		{"github", "github:acme/service", githubIssuer},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveCrossTenantTrust([]resource{identity}, []edge{{
				from: tc.source, to: identityA, relation: edgeWorkloadIdentity,
				metadata: map[string]string{mdIssuer: tc.issuer},
			}})
			if len(got) != 0 {
				t.Errorf("a %s federation drew %d trust edges: %v", tc.name, len(got), got)
			}
		})
	}

	// An Entra issuer in the identity's OWN tenant is not cross-tenant either.
	sameTenant := "https://login.microsoftonline.com/" + tenantOne + "/v2.0"
	got := resolveCrossTenantTrust([]resource{identity}, []edge{{
		from: "oidc:" + sameTenant + "/x", to: identityA, relation: edgeWorkloadIdentity,
		metadata: map[string]string{mdIssuer: sameTenant},
	}})
	if len(got) != 0 {
		t.Errorf("a federation from the identity's own tenant drew %d trust edges", len(got))
	}
	// And one in ANOTHER Entra tenant is.
	other := "https://login.microsoftonline.com/" + tenantTwo + "/v2.0"
	got = resolveCrossTenantTrust([]resource{identity}, []edge{{
		from: "oidc:" + other + "/x", to: identityA, relation: edgeWorkloadIdentity,
		metadata: map[string]string{mdIssuer: other},
	}})
	if len(got) != 1 {
		t.Errorf("a federation from another Entra tenant drew %d trust edges", len(got))
	}
}

// TestResolveCrossTenantTrust_TheTenantKeyIsCheckedBeforeAnyEdgeIsWalked. This
// is the mutation for the tenant metadata key: an identity whose walk did not
// stamp it produces NO federated trust at all, while every count of edge types
// still passes.
func TestResolveCrossTenantTrust_TheTenantKeyIsCheckedBeforeAnyEdgeIsWalked(t *testing.T) {
	input := []edge{{
		from: "oidc:" + foreignIssuer + "/workload-one", to: identityA, relation: edgeWorkloadIdentity,
		metadata: map[string]string{mdIssuer: foreignIssuer},
	}}

	stamped := identityWithTenant(t)
	with := resolveCrossTenantTrust([]resource{stamped}, input)
	if len(with) != 1 {
		t.Fatalf("the control arm drew %d trust edges", len(with))
	}

	unstamped := stamped
	unstamped.tenantID = ""
	without := resolveCrossTenantTrust([]resource{unstamped}, input)
	if len(without) != 0 {
		t.Errorf("an identity carrying no tenant still produced %d trust edges", len(without))
	}
}

// TestResolveCrossTenantTrust_TheIssuerKeyIsWhatTheGateReads is the mutation
// for the issuer metadata key on the workload edge: stop stamping it and the
// federated half goes silent.
func TestResolveCrossTenantTrust_TheIssuerKeyIsWhatTheGateReads(t *testing.T) {
	identity := identityWithTenant(t)
	withoutIssuer := []edge{{
		from: "oidc:x", to: identityA, relation: edgeWorkloadIdentity,
		metadata: map[string]string{mdSubject: "workload-one"},
	}}
	got := resolveCrossTenantTrust([]resource{identity}, withoutIssuer)
	if len(got) != 0 {
		t.Errorf("a workload edge carrying no issuer produced %d trust edges", len(got))
	}
}

// TestResolveCrossTenantTrust_RBACHalf, from a HAND-BUILT assignment carrying a
// guest principal.
//
// THIS COLLECTOR'S OWN WALK CANNOT PRODUCE THAT INPUT, and the second half of
// this test measures it rather than asserting it: the assignments the identity
// walk gathers carry ServicePrincipal, and through the same resolver they draw
// nothing.
func TestResolveCrossTenantTrust_RBACHalf(t *testing.T) {
	identity := identityWithTenant(t)

	guest := []edge{{
		from: identityA, to: scopeID, relation: edgeAssumesRole,
		metadata: map[string]string{mdSource: "rbac", mdPrincipalType: "Guest"},
	}}
	edges := resolveCrossTenantTrust([]resource{identity}, guest)
	if _, ok := edgeBetween(edges, identityA, scopeID, edgeTrusts); !ok {
		t.Errorf("a guest assignment drew no trust edge: %v", edges)
	}

	// A foreign group is the other admitted principal type.
	foreign := []edge{{
		from: identityA, to: scopeID, relation: edgeAssumesRole,
		metadata: map[string]string{mdPrincipalType: "ForeignGroup"},
	}}
	if got := resolveCrossTenantTrust([]resource{identity}, foreign); len(got) != 1 {
		t.Errorf("a foreign-group assignment drew %d trust edges", len(got))
	}

	// WHAT THIS WALK ACTUALLY PRODUCES, measured through the same resolver:
	// the identity walk's own assignments carry ServicePrincipal and draw
	// nothing, which is why the rows above are built by hand.
	fromThisWalk := roleAssignmentEdges(identityA, roleAssignments())
	got := resolveCrossTenantTrust([]resource{identity}, fromThisWalk)
	if len(got) != 0 {
		t.Errorf("this walk's own assignments drew %d trust edges; the RBAC half was thought unreachable from here", len(got))
	}
}

// TestResolveCrossTenantTrust_ThePrincipalTypeKeyIsWhatTheRBACGateReads is that
// half's metadata mutation.
func TestResolveCrossTenantTrust_ThePrincipalTypeKeyIsWhatTheRBACGateReads(t *testing.T) {
	identity := identityWithTenant(t)
	unstamped := []edge{{
		from: identityA, to: scopeID, relation: edgeAssumesRole,
		metadata: map[string]string{mdSource: "rbac"},
	}}
	got := resolveCrossTenantTrust([]resource{identity}, unstamped)
	if len(got) != 0 {
		t.Errorf("an assignment carrying no principal type produced %d trust edges", len(got))
	}
}

// TestResolveCrossTenantTrust_WithNoIdentitiesThereIsNothingToResolve is the
// degenerate arm, and the one a subscription with no managed identity hits.
func TestResolveCrossTenantTrust_WithNoIdentitiesThereIsNothingToResolve(t *testing.T) {
	got := resolveCrossTenantTrust(nil, trustInputEdges())
	if len(got) != 0 {
		t.Errorf("with no identity in the walk, %d trust edges were drawn", len(got))
	}
}
