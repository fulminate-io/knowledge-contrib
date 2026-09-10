// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"strings"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v6"
)

// fixtures_compute_test.go — hand-built compute responses and what they produce.

const (
	subID     = "/subscriptions/0000"
	rgID      = subID + "/resourceGroups/rg"
	vmID      = rgID + "/providers/Microsoft.Compute/virtualMachines/vm1"
	vmssID    = rgID + "/providers/Microsoft.Compute/virtualMachineScaleSets/scale1"
	diskID    = rgID + "/providers/Microsoft.Compute/disks/disk1"
	nicID     = rgID + "/providers/Microsoft.Network/networkInterfaces/nic1"
	vnetID    = rgID + "/providers/Microsoft.Network/virtualNetworks/vnet1"
	subnetID  = vnetID + "/subnets/default"
	identityA = rgID + "/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id-a"
	identityB = rgID + "/providers/Microsoft.ManagedIdentity/userAssignedIdentities/id-b"
	desID     = rgID + "/providers/Microsoft.Compute/diskEncryptionSets/des1"
)

func computeFixtures() []fixture {
	return []fixture{
		{name: "virtual machine", build: func(t *testing.T) subResult {
			vm := virtualMachine(vmID, "vm1")
			return subResult{
				resources: []resource{fx{t}.res(vmResource(vm))},
				edges:     vmEdges(vm, map[string][]string{strings.ToLower(nicID): {subnetID}}),
			}
		}},
		{name: "scale set", build: func(t *testing.T) subResult {
			vmss := scaleSet()
			return subResult{
				resources: []resource{fx{t}.res(vmssResource(vmss))},
				edges:     vmssEdges(vmss),
			}
		}},
		{name: "managed disk", build: func(t *testing.T) subResult {
			disk := managedDisk()
			return subResult{
				resources: []resource{fx{t}.res(diskResource(disk))},
				edges:     diskEdges(disk),
			}
		}},
	}
}

// virtualMachine builds a machine with an attached identity, a network
// interface, an OS disk and an encryption set: one of each relationship a
// machine can carry.
func virtualMachine(id, name string) *armcompute.VirtualMachine {
	return &armcompute.VirtualMachine{
		ID:       new(id),
		Name:     new(name),
		Location: new("westeurope"),
		Identity: &armcompute.VirtualMachineIdentity{
			UserAssignedIdentities: map[string]*armcompute.UserAssignedIdentitiesValue{
				identityB: {}, identityA: {},
			},
		},
		Properties: &armcompute.VirtualMachineProperties{
			ProvisioningState: new("Succeeded"),
			HardwareProfile:   &armcompute.HardwareProfile{VMSize: to.Ptr(armcompute.VirtualMachineSizeTypesStandardD2SV3)},
			NetworkProfile: &armcompute.NetworkProfile{
				NetworkInterfaces: []*armcompute.NetworkInterfaceReference{{ID: new(nicID)}},
			},
			StorageProfile: &armcompute.StorageProfile{
				OSDisk: &armcompute.OSDisk{
					OSType: to.Ptr(armcompute.OperatingSystemTypesLinux),
					ManagedDisk: &armcompute.ManagedDiskParameters{
						ID:                new(diskID),
						DiskEncryptionSet: &armcompute.DiskEncryptionSetParameters{ID: new(desID)},
					},
				},
			},
		},
	}
}

func scaleSet() *armcompute.VirtualMachineScaleSet {
	return &armcompute.VirtualMachineScaleSet{
		ID:       new(vmssID),
		Name:     new("scale1"),
		Location: new("westeurope"),
		SKU:      &armcompute.SKU{Name: new("Standard_D2s_v3"), Capacity: to.Ptr[int64](3)},
		Identity: &armcompute.VirtualMachineScaleSetIdentity{
			UserAssignedIdentities: map[string]*armcompute.UserAssignedIdentitiesValue{identityA: {}},
		},
		Properties: &armcompute.VirtualMachineScaleSetProperties{
			ProvisioningState: new("Succeeded"),
			VirtualMachineProfile: &armcompute.VirtualMachineScaleSetVMProfile{
				NetworkProfile: &armcompute.VirtualMachineScaleSetNetworkProfile{
					NetworkInterfaceConfigurations: []*armcompute.VirtualMachineScaleSetNetworkConfiguration{{
						Properties: &armcompute.VirtualMachineScaleSetNetworkConfigurationProperties{
							IPConfigurations: []*armcompute.VirtualMachineScaleSetIPConfiguration{{
								Properties: &armcompute.VirtualMachineScaleSetIPConfigurationProperties{
									Subnet: &armcompute.APIEntityReference{ID: new(subnetID)},
								},
							}},
						},
					}},
				},
				StorageProfile: &armcompute.VirtualMachineScaleSetStorageProfile{
					OSDisk: &armcompute.VirtualMachineScaleSetOSDisk{
						ManagedDisk: &armcompute.VirtualMachineScaleSetManagedDiskParameters{
							DiskEncryptionSet: &armcompute.DiskEncryptionSetParameters{ID: new(desID)},
						},
					},
				},
			},
		},
	}
}

func managedDisk() *armcompute.Disk {
	return &armcompute.Disk{
		ID:        new(diskID),
		Name:      new("disk1"),
		Location:  new("westeurope"),
		ManagedBy: new(vmID),
		SKU:       &armcompute.DiskSKU{Name: to.Ptr(armcompute.DiskStorageAccountTypesPremiumLRS)},
		Properties: &armcompute.DiskProperties{
			DiskSizeGB: to.Ptr[int32](128),
			Encryption: &armcompute.Encryption{DiskEncryptionSetID: new(desID)},
		},
	}
}

// TestVMEdges_ResolvesSubnetsThroughTheInterfaceIndex is the machine's own
// relationship row, and its negative is the point: a machine names its
// interface, not its subnet, so with an EMPTY index the subnet edge cannot be
// drawn and must not be invented.
func TestVMEdges_ResolvesSubnetsThroughTheInterfaceIndex(t *testing.T) {
	vm := virtualMachine(vmID, "vm1")

	resolved := vmEdges(vm, map[string][]string{strings.ToLower(nicID): {subnetID}})
	if _, ok := edgeBetween(resolved, vmID, subnetID, edgeUsesSubnet); !ok {
		t.Error("with the interface indexed, the machine draws no subnet edge")
	}

	unresolved := vmEdges(vm, map[string][]string{})
	if _, ok := edgeBetween(unresolved, vmID, subnetID, edgeUsesSubnet); ok {
		t.Error("with the interface NOT indexed, the machine drew a subnet edge anyway")
	}
	// The index is keyed lowercased because Azure's casing of an id is not
	// stable between the two APIs. An index keyed by the exact string would
	// resolve nothing here.
	mixed := vmEdges(vm, map[string][]string{strings.ToLower(nicID): {subnetID}})
	if len(mixed) != len(resolved) {
		t.Error("the interface index is case-sensitive, so a machine's own reference misses it")
	}
}

// TestVMEdges_AttachedIdentitiesAreSortedAndCarryTheirSource. The SDK models
// attached identities as a map, and map iteration is randomized: without a sort
// two identical collects emit the same edges in different orders.
func TestVMEdges_AttachedIdentitiesAreSortedAndCarryTheirSource(t *testing.T) {
	vm := virtualMachine(vmID, "vm1")
	first := vmEdges(vm, nil)
	for range 20 {
		if got := vmEdges(vm, nil); !sameEdgeOrder(first, got) {
			t.Fatal("two walks of one machine produced its identity edges in different orders")
		}
	}
	// The order is SORTED rather than merely stable: a stable-but-arbitrary
	// order would satisfy the repetition above while still differing between
	// two builds of the same walk.
	if len(first) < 2 || first[0].to != identityA || first[1].to != identityB {
		t.Errorf("the identity edges are not in sorted order: %v", first)
	}
	e, ok := edgeBetween(first, vmID, identityA, edgeAssumesRole)
	if !ok {
		t.Fatal("the machine draws no assignment edge to its attached identity")
	}
	if e.metadata[mdRoleSource] != roleSourceManagedIdentity {
		t.Errorf("the assignment does not record that it came from an attached identity: %v", e.metadata)
	}
	if _, hasType := e.metadata[mdPrincipalType]; hasType {
		t.Error("an attached-identity assignment carries a principal type, which only a role assignment can know")
	}
}

// TestDiskEdges_AttachmentRunsDiskToMachine pins the direction. A machine can
// be deleted while its disk survives, so the disk is what records the
// attachment; the reverse direction would make the relationship disappear with
// the machine.
func TestDiskEdges_AttachmentRunsDiskToMachine(t *testing.T) {
	edges := diskEdges(managedDisk())
	if _, ok := edgeBetween(edges, diskID, vmID, edgeBoundTo); !ok {
		t.Error("no attachment edge from the disk to the machine managing it")
	}
	if _, ok := edgeBetween(edges, vmID, diskID, edgeBoundTo); ok {
		t.Error("the attachment edge runs machine to disk, which inverts the relationship")
	}
	if _, ok := edgeBetween(edges, diskID, desID, edgeEncryptsWith); !ok {
		t.Error("no encryption edge from the disk to its encryption set")
	}

	// The negative: an unattached, unencrypted disk draws nothing.
	bare := &armcompute.Disk{ID: new(diskID), Name: new("disk1")}
	if got := diskEdges(bare); len(got) != 0 {
		t.Errorf("an unattached disk drew %d edges: %v", len(got), got)
	}
}

// TestVMSSStorageProfile_DrawsNoAttachment. A scale set's storage profile is a
// TEMPLATE for the disks its instances will get, so it names no disk to attach;
// only its encryption set is a real reference.
func TestVMSSStorageProfile_DrawsNoAttachment(t *testing.T) {
	edges := vmssEdges(scaleSet())
	if relationsOf(edges)[edgeBoundTo] != 0 {
		t.Error("a scale set drew a disk attachment, but its storage profile is a template with no disk ids")
	}
	if _, ok := edgeBetween(edges, vmssID, desID, edgeEncryptsWith); !ok {
		t.Error("a scale set drew no encryption edge for the set on its template")
	}
	// The known positive for that zero, through the same assertion path: a
	// MACHINE with the same encryption set does draw the attachment.
	vmEdgesOut := vmEdges(virtualMachine(vmID, "vm1"), nil)
	if relationsOf(vmEdgesOut)[edgeBoundTo] == 0 {
		t.Error("a machine drew no disk attachment either, so the zero above proves nothing")
	}
}

func sameEdgeOrder(a, b []edge) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].from != b[i].from || a[i].to != b[i].to || a[i].relation != b[i].relation {
			return false
		}
	}
	return true
}
