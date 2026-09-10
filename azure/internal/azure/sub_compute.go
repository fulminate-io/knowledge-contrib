// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v6"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
)

// sub_compute.go — virtual machines, scale sets and managed disks.

// vmSub walks the subscription's virtual machines.
//
// IT RESOLVES NETWORK INTERFACES FIRST. A virtual machine references its
// interfaces by id and the subnet lives on the interface, so the subnet edge
// needs a second list call; doing it once for the whole subscription rather
// than once per machine is the difference between one call and one per machine.
type vmSub struct{ subBase }

func (s *vmSub) Collect(ctx context.Context) (subResult, error) {
	client, err := armcompute.NewVirtualMachinesClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("virtual machines client: %w", err)
	}
	nicClient, err := armnetwork.NewInterfacesClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("network interfaces client: %w", err)
	}

	nicSubnets, err := interfaceSubnetIndex(ctx, nicClient)
	if err != nil {
		return subResult{}, fmt.Errorf("indexing network interfaces: %w", err)
	}

	var out subResult
	err = drain(ctx, client.NewListAllPager(nil), func(page armcompute.VirtualMachinesClientListAllResponse) error {
		for _, vm := range page.Value {
			if vm == nil || vm.ID == nil {
				continue
			}
			r, err := vmResource(vm)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, vmEdges(vm, nicSubnets)...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing virtual machines: %w", err)
	}
	return out, nil
}

// interfaceSubnetIndex maps each network interface id to the subnets its IP
// configurations sit in. Keys are lowercased because a machine's reference to
// its interface and the interface's own id differ in case often enough that an
// exact-match index silently resolves nothing.
func interfaceSubnetIndex(ctx context.Context, client *armnetwork.InterfacesClient) (map[string][]string, error) {
	index := make(map[string][]string)
	err := drain(ctx, client.NewListAllPager(nil), func(page armnetwork.InterfacesClientListAllResponse) error {
		for _, nic := range page.Value {
			if nic == nil || nic.ID == nil || nic.Properties == nil {
				continue
			}
			key := strings.ToLower(*nic.ID)
			for _, cfg := range nic.Properties.IPConfigurations {
				if cfg == nil || cfg.Properties == nil || cfg.Properties.Subnet == nil {
					continue
				}
				if id := ptr(cfg.Properties.Subnet.ID); id != "" {
					index[key] = append(index[key], id)
				}
			}
		}
		return nil
	})
	return index, err
}

func vmResource(vm *armcompute.VirtualMachine) (resource, error) {
	content, err := marshalContent(vm)
	if err != nil {
		return resource{}, fmt.Errorf("projecting virtual machine %s: %w", ptr(vm.ID), err)
	}
	r := resource{
		id:           ptr(vm.ID),
		name:         ptr(vm.Name),
		resourceType: rtVM,
		region:       ptr(vm.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if p := vm.Properties; p != nil {
		if p.HardwareProfile != nil && p.HardwareProfile.VMSize != nil {
			r.metadata["vmSize"] = string(*p.HardwareProfile.VMSize)
		}
		if p.StorageProfile != nil && p.StorageProfile.OSDisk != nil && p.StorageProfile.OSDisk.OSType != nil {
			r.metadata["osType"] = string(*p.StorageProfile.OSDisk.OSType)
		}
		if v := ptr(p.ProvisioningState); v != "" {
			r.metadata["provisioningState"] = v
		}
	}
	return r, nil
}

func vmEdges(vm *armcompute.VirtualMachine, nicSubnets map[string][]string) []edge {
	id := ptr(vm.ID)
	var out []edge
	if vm.Identity != nil {
		out = append(out, managedIdentityEdges(id, keysOf(vm.Identity.UserAssignedIdentities))...)
	}
	if p := vm.Properties; p != nil {
		if p.NetworkProfile != nil {
			for _, nic := range p.NetworkProfile.NetworkInterfaces {
				if nic == nil || nic.ID == nil {
					continue
				}
				for _, subnetID := range nicSubnets[strings.ToLower(*nic.ID)] {
					out = append(out, edge{from: id, to: subnetID, relation: edgeUsesSubnet})
				}
			}
		}
		out = append(out, storageProfileDiskEdges(id, p.StorageProfile)...)
	}
	return out
}

// vmssSub walks the subscription's virtual machine scale sets.
type vmssSub struct{ subBase }

func (s *vmssSub) Collect(ctx context.Context) (subResult, error) {
	client, err := armcompute.NewVirtualMachineScaleSetsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("scale sets client: %w", err)
	}
	var out subResult
	err = drain(ctx, client.NewListAllPager(nil), func(page armcompute.VirtualMachineScaleSetsClientListAllResponse) error {
		for _, vmss := range page.Value {
			if vmss == nil || vmss.ID == nil {
				continue
			}
			r, err := vmssResource(vmss)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, vmssEdges(vmss)...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing scale sets: %w", err)
	}
	return out, nil
}

func vmssResource(vmss *armcompute.VirtualMachineScaleSet) (resource, error) {
	content, err := marshalContent(vmss)
	if err != nil {
		return resource{}, fmt.Errorf("projecting scale set %s: %w", ptr(vmss.ID), err)
	}
	r := resource{
		id:           ptr(vmss.ID),
		name:         ptr(vmss.Name),
		resourceType: rtVMSS,
		region:       ptr(vmss.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if vmss.SKU != nil {
		if v := ptr(vmss.SKU.Name); v != "" {
			r.metadata["skuName"] = v
		}
		if vmss.SKU.Capacity != nil {
			r.metadata["capacity"] = strconv.FormatInt(*vmss.SKU.Capacity, 10)
		}
	}
	if vmss.Properties != nil {
		if v := ptr(vmss.Properties.ProvisioningState); v != "" {
			r.metadata["provisioningState"] = v
		}
	}
	return r, nil
}

func vmssEdges(vmss *armcompute.VirtualMachineScaleSet) []edge {
	id := ptr(vmss.ID)
	var out []edge
	if vmss.Identity != nil {
		out = append(out, managedIdentityEdges(id, keysOf(vmss.Identity.UserAssignedIdentities))...)
	}
	if vmss.Properties == nil || vmss.Properties.VirtualMachineProfile == nil {
		return out
	}
	profile := vmss.Properties.VirtualMachineProfile
	if np := profile.NetworkProfile; np != nil {
		for _, cfg := range np.NetworkInterfaceConfigurations {
			if cfg == nil || cfg.Properties == nil {
				continue
			}
			for _, ip := range cfg.Properties.IPConfigurations {
				if ip == nil || ip.Properties == nil || ip.Properties.Subnet == nil {
					continue
				}
				if subnetID := ptr(ip.Properties.Subnet.ID); subnetID != "" {
					out = append(out, edge{from: id, to: subnetID, relation: edgeUsesSubnet})
				}
			}
		}
	}
	out = append(out, vmssStorageProfileEdges(id, profile.StorageProfile)...)
	return out
}

// diskSub walks the subscription's managed disks.
type diskSub struct{ subBase }

func (s *diskSub) Collect(ctx context.Context) (subResult, error) {
	client, err := armcompute.NewDisksClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("disks client: %w", err)
	}
	var out subResult
	err = drain(ctx, client.NewListPager(nil), func(page armcompute.DisksClientListResponse) error {
		for _, disk := range page.Value {
			if disk == nil || disk.ID == nil {
				continue
			}
			r, err := diskResource(disk)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			out.edges = append(out.edges, diskEdges(disk)...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing disks: %w", err)
	}
	return out, nil
}

func diskResource(disk *armcompute.Disk) (resource, error) {
	content, err := marshalContent(disk)
	if err != nil {
		return resource{}, fmt.Errorf("projecting disk %s: %w", ptr(disk.ID), err)
	}
	r := resource{
		id:           ptr(disk.ID),
		name:         ptr(disk.Name),
		resourceType: rtDisk,
		region:       ptr(disk.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if disk.SKU != nil && disk.SKU.Name != nil {
		r.metadata["skuName"] = string(*disk.SKU.Name)
	}
	if disk.Properties != nil && disk.Properties.DiskSizeGB != nil {
		r.metadata["diskSizeGB"] = strconv.FormatInt(int64(*disk.Properties.DiskSizeGB), 10)
	}
	return r, nil
}

// diskEdges draws a disk's attachment and its encryption.
//
// THE ATTACHMENT RUNS DISK TO MACHINE, not the other way round: the disk is the
// resource Azure records the relationship on, and a machine can be deleted
// while its disk survives.
func diskEdges(disk *armcompute.Disk) []edge {
	id := ptr(disk.ID)
	var out []edge
	if managedBy := ptr(disk.ManagedBy); managedBy != "" {
		out = append(out, edge{from: id, to: managedBy, relation: edgeBoundTo})
	}
	if disk.Properties != nil && disk.Properties.Encryption != nil {
		if des := ptr(disk.Properties.Encryption.DiskEncryptionSetID); des != "" {
			out = append(out, edge{from: id, to: des, relation: edgeEncryptsWith})
		}
	}
	return out
}

// storageProfileDiskEdges draws the disks a machine's storage profile attaches
// and the encryption sets it uses. The attachment runs disk to machine, as
// above; the encryption runs machine to set, because the machine is what
// carries the setting.
func storageProfileDiskEdges(parentID string, sp *armcompute.StorageProfile) []edge {
	if sp == nil {
		return nil
	}
	var out []edge
	if sp.OSDisk != nil {
		out = append(out, managedDiskEdges(parentID, sp.OSDisk.ManagedDisk)...)
	}
	for _, dd := range sp.DataDisks {
		if dd == nil {
			continue
		}
		out = append(out, managedDiskEdges(parentID, dd.ManagedDisk)...)
	}
	return out
}

func managedDiskEdges(parentID string, md *armcompute.ManagedDiskParameters) []edge {
	if md == nil {
		return nil
	}
	var out []edge
	if id := ptr(md.ID); id != "" {
		out = append(out, edge{from: id, to: parentID, relation: edgeBoundTo})
	}
	if md.DiskEncryptionSet != nil {
		if des := ptr(md.DiskEncryptionSet.ID); des != "" {
			out = append(out, edge{from: parentID, to: des, relation: edgeEncryptsWith})
		}
	}
	return out
}

// vmssStorageProfileEdges draws a scale set's encryption sets.
//
// NO ATTACHMENT EDGE IS DRAWN HERE, and the absence is the point: a scale set's
// storage profile is a TEMPLATE for the disks its instances will get, so it
// carries no disk ids to attach. Only the encryption set, which is set at the
// template level, is a real reference.
func vmssStorageProfileEdges(parentID string, sp *armcompute.VirtualMachineScaleSetStorageProfile) []edge {
	if sp == nil {
		return nil
	}
	var out []edge
	if sp.OSDisk != nil && sp.OSDisk.ManagedDisk != nil && sp.OSDisk.ManagedDisk.DiskEncryptionSet != nil {
		if des := ptr(sp.OSDisk.ManagedDisk.DiskEncryptionSet.ID); des != "" {
			out = append(out, edge{from: parentID, to: des, relation: edgeEncryptsWith})
		}
	}
	for _, dd := range sp.DataDisks {
		if dd == nil || dd.ManagedDisk == nil || dd.ManagedDisk.DiskEncryptionSet == nil {
			continue
		}
		if des := ptr(dd.ManagedDisk.DiskEncryptionSet.ID); des != "" {
			out = append(out, edge{from: parentID, to: des, relation: edgeEncryptsWith})
		}
	}
	return out
}
