// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"
	"sort"
	"strconv"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/authorization/armauthorization/v3"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/keyvault/armkeyvault"
)

// sub_keyvault.go — key vaults and who can reach them.
//
// A VAULT HAS TWO GRANT MODELS AND THIS WALK READS BOTH. The older one is a
// list of access policies on the vault itself; the newer one is role
// assignments scoped to the vault, which live in a different service entirely.
// A vault uses one or the other, decided by a flag, so a walk reading only the
// first is blind on every RBAC vault and vice versa. Both produce the same edge
// type, distinguished by the source recorded in its evidence — which is exactly
// why resolver 5 filters on neither and needs a fixture for each.

// vaultGrantSourcePolicy and vaultGrantSourceRBAC name the two grant models in
// an edge's evidence.
const (
	vaultGrantSourcePolicy = "access_policy"
	vaultGrantSourceRBAC   = "rbac"
)

// keyVaultSub walks the subscription's key vaults.
type keyVaultSub struct{ subBase }

func (s *keyVaultSub) Collect(ctx context.Context) (subResult, error) {
	client, err := armkeyvault.NewVaultsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("key vaults client: %w", err)
	}
	roles, err := armauthorization.NewRoleAssignmentsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("role assignments client: %w", err)
	}

	var out subResult
	err = drain(ctx, client.NewListBySubscriptionPager(nil), func(page armkeyvault.VaultsClientListBySubscriptionResponse) error {
		for _, vault := range page.Value {
			if vault == nil || vault.ID == nil {
				continue
			}
			r, err := vaultResource(vault)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			grants, err := vaultEdges(vault)
			if err != nil {
				return err
			}
			out.edges = append(out.edges, grants...)
			if !vaultUsesRBAC(vault) {
				continue
			}
			edges, err := s.collectVaultRoleAssignments(ctx, roles, ptr(vault.ID))
			if err != nil {
				return err
			}
			out.edges = append(out.edges, edges...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing key vaults: %w", err)
	}
	return out, nil
}

// vaultUsesRBAC reports whether a vault's grants live in role assignments
// rather than in its own access policies. A vault that does not say is on the
// older model, which is Azure's own default.
func vaultUsesRBAC(vault *armkeyvault.Vault) bool {
	return vault.Properties != nil &&
		vault.Properties.EnableRbacAuthorization != nil &&
		*vault.Properties.EnableRbacAuthorization
}

func vaultResource(vault *armkeyvault.Vault) (resource, error) {
	content, err := marshalContent(vault)
	if err != nil {
		return resource{}, fmt.Errorf("projecting key vault %s: %w", ptr(vault.ID), err)
	}
	r := resource{
		id:           ptr(vault.ID),
		name:         ptr(vault.Name),
		resourceType: rtVault,
		region:       ptr(vault.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	vaultMetadata(vault.Properties, r.metadata)
	return r, nil
}

// vaultMetadata promotes the vault's protection settings, which are what an
// operator audits a vault for: whether a deleted secret is recoverable, whether
// the vault itself can be purged, and which grant model it is on.
func vaultMetadata(p *armkeyvault.VaultProperties, md map[string]string) {
	if p == nil {
		return
	}
	if p.SKU != nil && p.SKU.Name != nil {
		md["skuName"] = string(*p.SKU.Name)
	}
	setIfNotEmpty(md, mdNodeTenantID, ptr(p.TenantID))
	if p.EnableSoftDelete != nil {
		md["enableSoftDelete"] = strconv.FormatBool(*p.EnableSoftDelete)
	}
	if p.EnablePurgeProtection != nil {
		md["enablePurgeProtection"] = strconv.FormatBool(*p.EnablePurgeProtection)
	}
	if p.EnableRbacAuthorization != nil {
		md["enableRbacAuthorization"] = strconv.FormatBool(*p.EnableRbacAuthorization)
	}
}

// vaultEdges draws the vault's network rules and its access-policy grants.
func vaultEdges(vault *armkeyvault.Vault) ([]edge, error) {
	if vault.Properties == nil {
		return nil, nil
	}
	id := ptr(vault.ID)
	var out []edge
	if acls := vault.Properties.NetworkACLs; acls != nil {
		for _, rule := range acls.VirtualNetworkRules {
			if rule == nil {
				continue
			}
			if subnetID := ptr(rule.ID); subnetID != "" {
				out = append(out, edge{from: id, to: subnetID, relation: edgeUsesSubnet})
			}
		}
	}
	policies, err := accessPolicyEdges(vault)
	if err != nil {
		return nil, err
	}
	return append(out, policies...), nil
}

// accessPolicyEdges draws one grant edge per access policy.
//
// THE TARGET IS THE RAW OBJECT ID the policy names, because that is all a policy
// carries: it says which principal, not what kind of principal. Resolver 5 adds
// a second edge to the group node when the id turns out to name one; this edge
// stays either way, as the record of what Azure itself says.
func accessPolicyEdges(vault *armkeyvault.Vault) ([]edge, error) {
	var out []edge
	for _, ap := range vault.Properties.AccessPolicies {
		if ap == nil {
			continue
		}
		objectID := ptr(ap.ObjectID)
		if objectID == "" {
			continue
		}
		md := map[string]string{mdSource: vaultGrantSourcePolicy}
		setIfNotEmpty(md, mdTenantID, ptr(ap.TenantID))
		perms, err := permissionSummary(ap.Permissions)
		if err != nil {
			return nil, fmt.Errorf("access policy of vault %s: %w", ptr(vault.ID), err)
		}
		setIfNotEmpty(md, "permissions", perms)
		out = append(out, edge{from: ptr(vault.ID), to: objectID, relation: edgeAccessedBy, metadata: md})
	}
	return out, nil
}

// permissionSummary renders an access policy's permissions compactly, with the
// four categories in a fixed order so two identical policies render identically.
//
// IT RETURNS THE ENCODING ERROR RATHER THAN THE EMPTY STRING, on the same
// reasoning [marshalContent] gives: a policy that grants nothing and a policy
// whose permissions could not be encoded both render as empty, and a caller
// handed only the string cannot tell them apart. The branch is unreachable for
// the map[string][]string built below — which is why the enforcement here is
// the SIGNATURE rather than a test: with a second result the caller must
// decide, so the absorb-into-the-zero-value shape cannot be written at all.
func permissionSummary(p *armkeyvault.Permissions) (string, error) {
	if p == nil {
		return "", nil
	}
	out := map[string][]string{}
	add := func(key string, values []string) {
		if len(values) > 0 {
			sort.Strings(values)
			out[key] = values
		}
	}
	add("keys", enumStrings(p.Keys))
	add("secrets", enumStrings(p.Secrets))
	add("certificates", enumStrings(p.Certificates))
	add("storage", enumStrings(p.Storage))
	if len(out) == 0 {
		return "", nil
	}
	raw, err := marshalContent(out)
	if err != nil {
		return "", fmt.Errorf("encoding an access policy's permissions: %w", err)
	}
	return string(raw), nil
}

// enumStrings flattens a slice of SDK enum pointers to their string values.
func enumStrings[T ~string](in []*T) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == nil {
			continue
		}
		out = append(out, string(*v))
	}
	return out
}

// collectVaultRoleAssignments lists the role assignments scoped to one vault.
func (s *keyVaultSub) collectVaultRoleAssignments(
	ctx context.Context, client *armauthorization.RoleAssignmentsClient, vaultID string,
) ([]edge, error) {
	var out []edge
	err := drain(ctx, client.NewListForScopePager(vaultID, nil),
		func(page armauthorization.RoleAssignmentsClientListForScopeResponse) error {
			out = append(out, vaultRoleAssignmentEdges(vaultID, page.Value)...)
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("listing role assignments scoped to vault %s: %w", armName(vaultID), err)
	}
	return out, nil
}

// vaultRoleAssignmentEdges draws one grant edge per role assignment.
//
// THESE CARRY A PRINCIPAL TYPE and the access-policy edges do not, because a
// role assignment records what kind of principal it granted to and an access
// policy does not. That difference is why resolver 5's role half can gate on
// the type and its access half cannot.
func vaultRoleAssignmentEdges(vaultID string, assignments []*armauthorization.RoleAssignment) []edge {
	var out []edge
	for _, ra := range assignments {
		if ra == nil || ra.Properties == nil {
			continue
		}
		principal := ptr(ra.Properties.PrincipalID)
		if principal == "" {
			continue
		}
		md := map[string]string{mdSource: vaultGrantSourceRBAC}
		setIfNotEmpty(md, mdRoleDefID, ptr(ra.Properties.RoleDefinitionID))
		setIfNotEmpty(md, "scope", ptr(ra.Properties.Scope))
		if ra.Properties.PrincipalType != nil {
			md[mdPrincipalType] = string(*ra.Properties.PrincipalType)
		}
		out = append(out, edge{from: vaultID, to: principal, relation: edgeAccessedBy, metadata: md})
	}
	return out
}
