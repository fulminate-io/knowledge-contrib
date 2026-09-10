// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"sort"
	"strings"
)

// shared_edges.go — the relationship shapes a dozen services draw identically.
//
// Nearly every Azure resource can carry a managed identity, sit in a subnet and
// be encrypted with a key vault key, and the SDK models each of those the same
// way on every service. Drawing them in one place is what keeps the twelve
// services that attach an identity from disagreeing about the edge's direction
// or its evidence.

// roleSourceManagedIdentity is the evidence value marking an assignment drawn
// from a resource's ATTACHED identity rather than from a role assignment
// record. The distinction matters to the resolvers: only an edge from a role
// assignment can carry a principal type, so an edge carrying this value can
// never satisfy a principal-type gate, and a reader who confuses the two would
// look for a gate failure that is really an evidence mismatch.
const roleSourceManagedIdentity = "managed_identity"

// managedIdentityEdges draws one assignment edge per user-assigned identity
// attached to a resource.
//
// THE IDS ARE SORTED. The SDK models attached identities as a map keyed by
// resource id, and Go map iteration is randomized, so an unsorted walk would
// emit the same two edges in a different order on every collect.
func managedIdentityEdges(sourceID string, identityIDs []string) []edge {
	out := make([]edge, 0, len(identityIDs))
	for _, id := range identityIDs {
		if id == "" {
			continue
		}
		out = append(out, edge{
			from:     sourceID,
			to:       id,
			relation: edgeAssumesRole,
			metadata: map[string]string{mdRoleSource: roleSourceManagedIdentity},
		})
	}
	return out
}

// keysOf returns a map's keys in sorted order. Every caller is walking an SDK
// map whose iteration order would otherwise reach the output.
func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// subnetEdges draws a resource's use of a subnet and, when the subnet id names
// one, of the virtual network above it.
//
// THE NETWORK EDGE IS DERIVED rather than read: a subnet id contains its
// network's id as a prefix, so the parent is knowable without a second call.
// A subnet id that does not carry one yields only the subnet edge.
func subnetEdges(sourceID, subnetID string) []edge {
	if subnetID == "" {
		return nil
	}
	out := []edge{{from: sourceID, to: subnetID, relation: edgeUsesSubnet}}
	if vnetID := vnetIDFromSubnet(subnetID); vnetID != "" {
		out = append(out, edge{from: sourceID, to: vnetID, relation: edgeUsesNetwork})
	}
	return out
}

// keyVaultKeyEdges draws a resource's use of a customer-managed key, given the
// vault URI and key name the resource carries.
//
// THE TARGET IS THE KEY'S DATA-PLANE URI, not an ARM id, because that is the
// only identity the source data carries: a resource configured with a
// customer-managed key names the vault by hostname and the key by name, and the
// vault's ARM id is not derivable from either.
func keyVaultKeyEdges(sourceID, vaultURI, keyName, keyVersion string) []edge {
	if vaultURI == "" || keyName == "" {
		return nil
	}
	target := strings.TrimRight(vaultURI, "/") + "/keys/" + keyName
	if keyVersion != "" {
		target += "/" + keyVersion
	}
	return []edge{{from: sourceID, to: target, relation: edgeEncryptsWith}}
}

// containsEdges draws a parent's containment of each child id given.
func containsEdges(parentID string, childIDs ...string) []edge {
	out := make([]edge, 0, len(childIDs))
	for _, id := range childIDs {
		if id == "" {
			continue
		}
		out = append(out, edge{from: parentID, to: id, relation: edgeContains})
	}
	return out
}
