// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"strings"
	"testing"
)

// summary_test.go — the one-line summary every node carries.

// TestSummarizers_CoverEveryArmResourceTypeExactly. The registry is keyed on
// the exact Azure type string with no prefix matching, so a resource type added
// to the vocabulary without a summary is a visible gap rather than a silently
// generic node — and an entry for a type the collector does not emit is dead.
func TestSummarizers_CoverEveryArmResourceTypeExactly(t *testing.T) {
	declared := map[string]bool{}
	for _, rt := range armResourceTypes {
		declared[rt] = true
		if _, ok := summarizers[rt]; !ok {
			t.Errorf("%s has no summary, so its nodes read as the generic form", rt)
		}
	}
	for rt := range summarizers {
		if !declared[rt] {
			t.Errorf("%s has a summary and is not a resource type this collector emits", rt)
		}
	}
	if len(summarizers) != len(armResourceTypes) {
		t.Errorf("%d summaries for %d resource types", len(summarizers), len(armResourceTypes))
	}
}

// TestSummarize_SyntheticTypesFallThroughToTheGenericForm, which is the honest
// shape for a node whose only facts are its type and its name.
func TestSummarize_SyntheticTypesFallThroughToTheGenericForm(t *testing.T) {
	for _, rt := range syntheticResourceTypes {
		if _, ok := summarizers[rt]; ok {
			t.Errorf("%s has a bespoke summary, though a proxy node carries no facts to summarize", rt)
		}
	}
	got := summarize(resource{id: "azure:ca/Example CA", name: "Example CA", resourceType: rtCertAuthority})
	if !strings.Contains(got, rtCertAuthority) || !strings.Contains(got, "Example CA") {
		t.Errorf("the generic summary names neither the type nor the name: %q", got)
	}
}

// TestSummarize_IsDeterministicAndReadsAsProse. Two summaries of one unchanged
// resource must be identical, or every collect would look like a change to
// whatever indexes them.
func TestSummarize_IsDeterministicAndReadsAsProse(t *testing.T) {
	r := fx{t}.res(vmResource(virtualMachine(vmID, "vm1")))
	first := summarize(r)
	for range 20 {
		if got := summarize(r); got != first {
			t.Fatalf("two summaries of one resource differ:\n%q\n%q", first, got)
		}
	}
	for _, want := range []string{"Virtual machine", "vm1", "westeurope", "vmSize=Standard_D2s_v3"} {
		if !strings.Contains(first, want) {
			t.Errorf("the summary does not carry %q: %q", want, first)
		}
	}
	// The noun is what an operator would search for, not the Azure type.
	if strings.Contains(first, rtVM) {
		t.Errorf("the summary reads as the Azure type rather than as prose: %q", first)
	}
}

// TestSummarize_DropsWhatTheResourceDoesNotHave. A summary padded with empty
// fields would match every query that mentioned one of them.
func TestSummarize_DropsWhatTheResourceDoesNotHave(t *testing.T) {
	bare := resource{id: vmID, name: "vm1", resourceType: rtVM}
	got := summarize(bare)
	if strings.Contains(got, "in ") {
		t.Errorf("a resource with no region rendered a region: %q", got)
	}
	if strings.Contains(got, "=") {
		t.Errorf("a resource with no metadata rendered a metadata list: %q", got)
	}
	if got != "Virtual machine vm1" {
		t.Errorf("the bare summary is %q", got)
	}
}

// TestWithMeta_RendersKeysInTheCallersOrder. Map iteration is randomized, so a
// summary rendering its metadata in map order would differ between two collects
// of one resource.
func TestWithMeta_RendersKeysInTheCallersOrder(t *testing.T) {
	r := resource{
		name: "x", resourceType: rtVM,
		metadata: map[string]string{"a": "1", "b": "2", "c": "3"},
	}
	first := withMeta("Thing", r, "c", "b", "a")
	if !strings.Contains(first, "(c=3, b=2, a=1)") {
		t.Errorf("the metadata was not rendered in the caller's order: %q", first)
	}
	for range 20 {
		if got := withMeta("Thing", r, "c", "b", "a"); got != first {
			t.Fatalf("two renders differ:\n%q\n%q", first, got)
		}
	}
}

// TestArmIDs_ReadTheirSegmentsCaseInsensitively. Azure returns `resourceGroups`
// from most APIs and `resourcegroups` from a few, and an exact-match parser
// silently returns nothing for the second group — which would make every
// follow-up call for those resources fail to be made at all.
func TestArmIDs_ReadTheirSegmentsCaseInsensitively(t *testing.T) {
	for _, id := range []string{
		"/subscriptions/s/resourceGroups/rg/providers/Microsoft.Compute/virtualMachines/vm1",
		"/subscriptions/s/resourcegroups/rg/providers/Microsoft.Compute/virtualMachines/vm1",
		"/SUBSCRIPTIONS/S/RESOURCEGROUPS/rg/PROVIDERS/Microsoft.Compute/virtualMachines/vm1",
	} {
		if got := armResourceGroup(id); got != "rg" {
			t.Errorf("resource group of %q read as %q", id, got)
		}
		if got := armName(id); got != "vm1" {
			t.Errorf("name of %q read as %q", id, got)
		}
	}
	if got := armResourceGroup("/subscriptions/s/providers/Microsoft.Insights/diagnosticSettings/x"); got != "" {
		t.Errorf("a subscription-scoped id reported the resource group %q", got)
	}
}

// TestVNetIDFromSubnet_PreservesTheOriginalCasing. The result is used as a node
// id and must match the id the network's own walk emitted; lowercasing it would
// produce a second node for one network.
func TestVNetIDFromSubnet_PreservesTheOriginalCasing(t *testing.T) {
	const mixed = "/subscriptions/S/resourceGroups/RG/providers/Microsoft.Network/virtualNetworks/VNet-One/subnets/Default"
	const want = "/subscriptions/S/resourceGroups/RG/providers/Microsoft.Network/virtualNetworks/VNet-One"
	if got := vnetIDFromSubnet(mixed); got != want {
		t.Errorf("vnetIDFromSubnet returned %q", got)
	}
	// The separator is matched case-insensitively, because Azure spells it
	// both ways.
	upper := strings.Replace(mixed, "/subnets/", "/Subnets/", 1)
	if got := vnetIDFromSubnet(upper); got != want {
		t.Errorf("an id spelling the separator differently returned %q", got)
	}
	// And an id that is not a subnet id names no network.
	if got := vnetIDFromSubnet(vmID); got != "" {
		t.Errorf("a machine id reported the network %q", got)
	}
}

// TestSiblingID_ResolvesABareNameAgainstItsOwnParent, which is how a Service
// Bus forwarding destination becomes an id.
func TestSiblingID_ResolvesABareNameAgainstItsOwnParent(t *testing.T) {
	if got := siblingID(sbQueueID, "orders-dlq"); got != sbDLQTarget {
		t.Errorf("a bare sibling name resolved to %q", got)
	}
	// An absolute path names an entity elsewhere and is returned unchanged.
	elsewhere := "/subscriptions/x/resourceGroups/y/providers/Microsoft.ServiceBus/namespaces/n/queues/q"
	if got := siblingID(sbQueueID, elsewhere); got != elsewhere {
		t.Errorf("an absolute destination was rewritten to %q", got)
	}
}
