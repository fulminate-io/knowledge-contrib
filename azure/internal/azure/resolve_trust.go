// SPDX-License-Identifier: Apache-2.0

package azure

import "strings"

// resolve_trust.go — RESOLVER 4: cross-tenant trust, from two directions that
// point opposite ways.
//
// THE TWO HALVES ARE NOT SYMMETRIC AND ONE OF THEM CANNOT FIRE ON THIS WALK'S
// OWN DATA.
//
//   - The RBAC half walks a managed identity's OUTGOING role assignments and
//     keeps the ones whose principal is a guest user or a foreign group, so the
//     trust runs FROM the identity TO the assignment's scope.
//   - The federated half walks the same identity's INCOMING workload-identity
//     edges and keeps the ones whose issuer is outside the identity's tenant,
//     so the trust runs FROM the external issuer's synthetic node TO the
//     identity.
//
// WHY THE RBAC HALF IS UNREACHABLE FROM THIS WALK, stated here so nobody reads
// its empty output as a defect: the only role assignments this collector
// gathers come from a per-identity list filtered by that identity's own
// principal id, so every assignment it returns reports the WALKED identity's
// principal type, which for a user-assigned managed identity is
// ServicePrincipal and never Guest or ForeignGroup. The half is implemented
// anyway, because it is the parity target's only emitter for its half of this
// edge type and because an operator whose role assignments arrive from another
// source (a future scope-wide walk) would need it. Its test says the same and
// builds its input by hand.
//
// THE FEDERATED HALF'S SOURCE IS ALWAYS AN oidc: NODE. The two functions have
// to be read together to see it: the source id is minted as a Kubernetes
// service account for an AKS issuer, a github: node for a GitHub issuer, and an
// oidc: node otherwise — and the external-issuer gate returns false for exactly
// the AKS and GitHub issuers. So the two source kinds the gate admits are the
// one kind it does not refuse.

// methodCrossTenantTrust marks every edge this resolver emits.
const methodCrossTenantTrust = "azure-cross-tenant-trust"

// entraAuthorityHost is the Entra login host whose issuer URLs carry a tenant
// id as their first path segment.
const entraAuthorityHost = "login.microsoftonline.com"

// resolveCrossTenantTrust returns the trust edges implied by role assignments
// and federated credentials on the walk's managed identities.
func resolveCrossTenantTrust(identities []resource, edges []edge) []edge {
	if len(identities) == 0 {
		return nil
	}
	ids := idSet(identities)
	out := rbacGuestTrust(edges, ids)
	out = append(out, federatedTenantTrust(identities, edges)...)
	return out
}

// rbacGuestTrust returns a trust edge for every role assignment whose principal
// is a guest user or a foreign group.
func rbacGuestTrust(edges []edge, identityIDs map[string]struct{}) []edge {
	var out []edge
	for _, e := range edgesOfType(edges, edgeAssumesRole, identityIDs) {
		if !isCrossTenantPrincipal(e.metadata[mdPrincipalType]) {
			continue
		}
		out = append(out, edge{
			from:     e.from,
			to:       e.to,
			relation: edgeTrusts,
			metadata: map[string]string{mdPrincipalType: e.metadata[mdPrincipalType]},
			method:   methodCrossTenantTrust,
		})
	}
	return out
}

// federatedTenantTrust returns a trust edge for every federated credential
// whose issuer is outside the identity's own tenant.
func federatedTenantTrust(identities []resource, edges []edge) []edge {
	var out []edge
	for _, identity := range identities {
		// THE TENANT GATE COMES FIRST AND IS CHECKED BEFORE ANY EDGE IS
		// WALKED: an identity whose walk did not stamp its tenant cannot be
		// compared against an issuer's tenant at all, so a collector that
		// stopped stamping it would produce zero federated trusts while every
		// edge-type count still passed.
		if identity.tenantID == "" {
			continue
		}
		for _, e := range edges {
			if e.relation != edgeWorkloadIdentity || e.to != identity.id {
				continue
			}
			issuer := e.metadata[mdIssuer]
			if issuer == "" {
				continue
			}
			if !isExternalIssuer(issuer, identity.tenantID) {
				continue
			}
			out = append(out, edge{
				from:     e.from,
				to:       e.to,
				relation: edgeTrusts,
				metadata: map[string]string{mdIssuer: issuer, mdTenantID: identity.tenantID},
				method:   methodCrossTenantTrust,
			})
		}
	}
	return out
}

// isCrossTenantPrincipal reports whether a principal type names a principal
// from outside this tenant. The two values are Entra's own.
func isCrossTenantPrincipal(principalType string) bool {
	switch principalType {
	case "Guest", "ForeignGroup":
		return true
	default:
		return false
	}
}

// isExternalIssuer reports whether a federated credential's issuer sits outside
// the identity's tenant.
//
// AKS AND GITHUB ISSUERS ARE NOT CROSS-TENANT BY DEFINITION: an AKS cluster's
// issuer belongs to this subscription, and a GitHub Actions issuer is a
// workload federation rather than a tenant relationship. Any OTHER non-Entra
// issuer is external, because there is no tenant to compare it against; an
// Entra issuer is external exactly when its tenant differs.
func isExternalIssuer(issuer, identityTenant string) bool {
	if isAKSIssuer(issuer) || isGitHubIssuer(issuer) {
		return false
	}
	if !strings.Contains(issuer, entraAuthorityHost) {
		return true
	}
	issuerTenant := tenantFromIssuer(issuer)
	return issuerTenant != "" && issuerTenant != identityTenant
}

// tenantFromIssuer reads the tenant id out of an Entra OIDC issuer URL, which
// is spelled https://login.microsoftonline.com/{tenant}/v2.0.
func tenantFromIssuer(issuer string) string {
	parts := strings.Split(issuer, "/")
	for i, p := range parts {
		if p == entraAuthorityHost && i+1 < len(parts) {
			return parts[i+1]
		}
	}
	return ""
}

// isAKSIssuer reports whether an issuer URL is an AKS cluster's OIDC endpoint.
func isAKSIssuer(issuer string) bool {
	return strings.Contains(issuer, ".oic.prod-aks.azure.com")
}

// isGitHubIssuer reports whether an issuer URL is GitHub Actions' OIDC issuer.
func isGitHubIssuer(issuer string) bool {
	return strings.Contains(issuer, "token.actions.githubusercontent.com")
}
