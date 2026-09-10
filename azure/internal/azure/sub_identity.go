// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v3"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/msi/armmsi"
)

// sub_identity.go — user-assigned managed identities, the roles they hold and
// the workloads federated to them.

// identitySub walks the subscription's user-assigned managed identities.
//
// IT MAKES THREE CALLS PER IDENTITY'S WORTH OF DATA and the two follow-ups are
// where the identity graph actually lives: the role assignments say what the
// identity can do, and the federated credentials say who can become it.
type identitySub struct{ subBase }

func (s *identitySub) Collect(ctx context.Context) (subResult, error) {
	client, err := armmsi.NewUserAssignedIdentitiesClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("managed identities client: %w", err)
	}
	roles, err := armauthorization.NewRoleAssignmentsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("role assignments client: %w", err)
	}
	federated, err := armmsi.NewFederatedIdentityCredentialsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("federated credentials client: %w", err)
	}

	var out subResult
	err = drain(ctx, client.NewListBySubscriptionPager(nil),
		func(page armmsi.UserAssignedIdentitiesClientListBySubscriptionResponse) error {
			for _, identity := range page.Value {
				if identity == nil || identity.ID == nil {
					continue
				}
				r, err := identityResource(identity)
				if err != nil {
					return err
				}
				out.resources = append(out.resources, r)
				if err := s.collectRoleAssignments(ctx, roles, identity, &out); err != nil {
					return err
				}
				if err := s.collectFederated(ctx, federated, identity, &out); err != nil {
					return err
				}
			}
			return nil
		})
	if err != nil {
		return out, fmt.Errorf("listing managed identities: %w", err)
	}
	return out, nil
}

// identityResource emits the identity, stamping the two keys the resolvers gate
// on.
//
// tenantId AND principalId ARE LOAD-BEARING METADATA, not decoration: resolver
// 4's federated half refuses to compare an issuer against an identity with no
// tenant, and resolver 5's role half refuses to look up an identity with no
// principal. A walk that stopped stamping either would make the resolver that
// reads it produce nothing at all while every count of edge types still passed.
func identityResource(identity *armmsi.Identity) (resource, error) {
	content, err := marshalContent(identity)
	if err != nil {
		return resource{}, fmt.Errorf("projecting managed identity %s: %w", ptr(identity.ID), err)
	}
	r := resource{
		id:           ptr(identity.ID),
		name:         ptr(identity.Name),
		resourceType: rtManagedIdentity,
		region:       ptr(identity.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if p := identity.Properties; p != nil {
		r.principalID = ptr(p.PrincipalID)
		r.tenantID = ptr(p.TenantID)
		setIfNotEmpty(r.metadata, mdNodeClientID, ptr(p.ClientID))
		setIfNotEmpty(r.metadata, mdNodePrincipalID, r.principalID)
		setIfNotEmpty(r.metadata, mdNodeTenantID, r.tenantID)
	}
	return r, nil
}

// collectRoleAssignments lists the assignments held by one identity.
//
// THE FILTER IS THE IDENTITY'S OWN PRINCIPAL, which is what makes this call
// per-identity rather than subscription-wide, and it is also why every
// assignment it returns reports THAT identity's principal type. Two resolvers
// gate on a principal type this filter can therefore never produce; both say so
// at their own site.
func (s *identitySub) collectRoleAssignments(
	ctx context.Context, client *armauthorization.RoleAssignmentsClient, identity *armmsi.Identity, out *subResult,
) error {
	if identity.Properties == nil || ptr(identity.Properties.PrincipalID) == "" {
		return nil
	}
	filter := fmt.Sprintf("principalId eq '%s'", *identity.Properties.PrincipalID)
	pager := client.NewListForSubscriptionPager(&armauthorization.RoleAssignmentsClientListForSubscriptionOptions{
		Filter: &filter,
	})
	err := drain(ctx, pager, func(page armauthorization.RoleAssignmentsClientListForSubscriptionResponse) error {
		out.edges = append(out.edges, roleAssignmentEdges(ptr(identity.ID), page.Value)...)
		return nil
	})
	if err != nil {
		return fmt.Errorf("listing role assignments of identity %s: %w", ptr(identity.Name), err)
	}
	return nil
}

// roleAssignmentEdges draws one assignment edge per role the identity holds.
func roleAssignmentEdges(identityID string, assignments []*armauthorization.RoleAssignment) []edge {
	var out []edge
	for _, ra := range assignments {
		if ra == nil || ra.Properties == nil {
			continue
		}
		scope := ptr(ra.Properties.Scope)
		if scope == "" {
			continue
		}
		md := map[string]string{mdSource: "rbac"}
		setIfNotEmpty(md, mdRoleDefID, ptr(ra.Properties.RoleDefinitionID))
		if ra.Properties.PrincipalType != nil {
			md[mdPrincipalType] = string(*ra.Properties.PrincipalType)
		}
		out = append(out, edge{from: identityID, to: scope, relation: edgeAssumesRole, metadata: md})
	}
	return out
}

// collectFederated lists the federated credentials on one identity.
func (s *identitySub) collectFederated(
	ctx context.Context, client *armmsi.FederatedIdentityCredentialsClient, identity *armmsi.Identity, out *subResult,
) error {
	rg := armResourceGroup(ptr(identity.ID))
	name := armName(ptr(identity.ID))
	if rg == "" || name == "" {
		return nil
	}
	err := drain(ctx, client.NewListPager(rg, name, nil),
		func(page armmsi.FederatedIdentityCredentialsClientListResponse) error {
			for _, cred := range page.Value {
				resources, edges := federatedCredential(ptr(identity.ID), cred)
				out.resources = append(out.resources, resources...)
				out.edges = append(out.edges, edges...)
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("listing federated credentials of identity %s: %w", name, err)
	}
	return nil
}

// federatedCredential turns one federated credential into the workload that can
// assume the identity, plus the edge saying so.
//
// THE SOURCE IS A NODE THIS COLLECTOR MINTS, because the workload is not an
// Azure resource: it is a Kubernetes service account, a GitHub repository, or
// an OIDC subject at some other issuer. A Kubernetes source gets NO node here —
// a Kubernetes collector owns those, and minting one would put a foreign
// family's node in this graph — while the other two do, because nothing else
// will ever emit them.
func federatedCredential(identityID string, cred *armmsi.FederatedIdentityCredential) ([]resource, []edge) {
	if cred == nil || cred.Properties == nil {
		return nil, nil
	}
	issuer := ptr(cred.Properties.Issuer)
	subject := ptr(cred.Properties.Subject)
	if issuer == "" || subject == "" {
		return nil, nil
	}
	sourceID, sourceType := federatedSource(issuer, subject)
	if sourceID == "" {
		return nil, nil
	}

	md := map[string]string{mdIssuer: issuer, mdSubject: subject}
	if aud := derefStrings(cred.Properties.Audiences); len(aud) > 0 {
		md[mdAudiences] = strings.Join(aud, ",")
	}

	var resources []resource
	if sourceType != "" {
		p := proxy(sourceID, sourceID, sourceType,
			"the federated workload is not an Azure resource and has no ARM id",
			"federated identity credential", "")
		p.metadata[mdIssuer] = issuer
		p.metadata[mdSubject] = subject
		delete(p.metadata, metaSubscriptionID)
		resources = append(resources, p)
	}
	return resources, []edge{{
		from:     sourceID,
		to:       identityID,
		relation: edgeWorkloadIdentity,
		metadata: md,
	}}
}

// federatedSource mints the id of the workload a credential federates, and the
// resource type of the node to emit for it. An empty type means "emit no node":
// the Kubernetes case, whose nodes belong to another collector.
func federatedSource(issuer, subject string) (id, resourceType string) {
	if ns, sa, ok := kubernetesServiceAccount(subject); ok && isAKSIssuer(issuer) {
		return ns + "/ServiceAccount/" + sa, ""
	}
	if org, repo, ok := gitHubRepository(subject); ok && isGitHubIssuer(issuer) {
		return "github:" + org + "/" + repo, rtGitHubIdentity
	}
	return "oidc:" + issuer + "/" + subject, rtOIDCIdentity
}

// kubernetesServiceAccount reads a Kubernetes service-account subject,
// "system:serviceaccount:{namespace}:{name}".
func kubernetesServiceAccount(subject string) (namespace, name string, ok bool) {
	rest, found := strings.CutPrefix(subject, "system:serviceaccount:")
	if !found {
		return "", "", false
	}
	namespace, name, found = strings.Cut(rest, ":")
	if !found || namespace == "" || name == "" {
		return "", "", false
	}
	return namespace, name, true
}

// gitHubRepository reads a GitHub Actions subject, "repo:{org}/{repo}:{ref}".
func gitHubRepository(subject string) (org, repo string, ok bool) {
	rest, found := strings.CutPrefix(subject, "repo:")
	if !found {
		return "", "", false
	}
	orgRepo, _, _ := strings.Cut(rest, ":")
	org, repo, found = strings.Cut(orgRepo, "/")
	if !found || org == "" || repo == "" {
		return "", "", false
	}
	return org, repo, true
}
