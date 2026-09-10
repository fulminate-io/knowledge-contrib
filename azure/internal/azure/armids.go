// SPDX-License-Identifier: Apache-2.0

package azure

import "strings"

// armids.go — reading the parts of an ARM resource id back out of it.
//
// An ARM id is a path of alternating type and name segments:
//
//	/subscriptions/{sub}/resourceGroups/{rg}/providers/Microsoft.X/{type}/{name}
//
// Several list APIs are subscription-wide while the follow-up call that
// enumerates a resource's children is scoped to a resource group and a name, so
// the only way to make the second call is to read those back out of the id the
// first returned. Every helper here is that, and every one is
// CASE-INSENSITIVE on the segment name: Azure returns `resourceGroups` from
// most APIs and `resourcegroups` from a few, and an exact-match parser silently
// returns empty for the second group.

// armSegment returns the value following the named path segment in an ARM id,
// matched case-insensitively, or "" when the segment is absent. When the
// segment repeats, the LAST occurrence wins, which is what a nested type
// (.../virtualNetworks/{vnet}/subnets/{subnet}) needs.
func armSegment(id, segment string) string {
	parts := strings.Split(strings.TrimPrefix(id, "/"), "/")
	out := ""
	for i := 0; i < len(parts)-1; i++ {
		if strings.EqualFold(parts[i], segment) {
			out = parts[i+1]
		}
	}
	return out
}

// armResourceGroup returns the resource group an ARM id names.
func armResourceGroup(id string) string { return armSegment(id, "resourceGroups") }

// armName returns the last segment of an ARM id, which for a top-level
// resource is its name.
func armName(id string) string {
	idx := strings.LastIndex(id, "/")
	if idx < 0 || idx == len(id)-1 {
		return ""
	}
	return id[idx+1:]
}

// vnetIDFromSubnet returns the virtual network id a subnet id sits under, or ""
// when the id is not a subnet id. The cut is case-insensitive on the `/subnets/`
// separator but preserves the ORIGINAL casing of the prefix, because the result
// is used as a node id and must match the id the virtual-network walk emitted.
func vnetIDFromSubnet(subnetID string) string {
	idx := strings.Index(strings.ToLower(subnetID), "/subnets/")
	if idx < 0 {
		return ""
	}
	return subnetID[:idx]
}

// isARMResourceID reports whether an id is an already-resolved ARM path rather
// than a raw address, hostname or synthetic proxy id.
func isARMResourceID(id string) bool {
	return strings.HasPrefix(strings.ToLower(id), "/subscriptions/")
}

// siblingID replaces the last segment of an ARM id with name, which is how a
// bare entity name in a Service Bus forwarding property becomes the id of the
// sibling entity it names. A name that is already a path is returned unchanged.
func siblingID(sourceID, name string) string {
	if strings.HasPrefix(name, "/") {
		return name
	}
	idx := strings.LastIndex(sourceID, "/")
	if idx < 0 {
		return name
	}
	return sourceID[:idx+1] + name
}

// ptr returns the value a pointer points at, or the zero value when it is nil.
// The ARM SDK models every optional field as a pointer, so a walk that reads
// them without this reads as a wall of nil checks.
func ptr[T any](p *T) T {
	if p == nil {
		var zero T
		return zero
	}
	return *p
}

// derefStrings flattens a slice of string pointers, dropping nil entries.
func derefStrings(in []*string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s != nil && *s != "" {
			out = append(out, *s)
		}
	}
	return out
}
