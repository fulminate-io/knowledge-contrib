// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"fmt"

	computepb "cloud.google.com/go/compute/apiv1/computepb"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// compute.go — Compute Engine instances.
//
// THE NODE ID IS THE SELF-LINK, UNMODIFIED. Every other compute resource
// references an instance by that URL, so the id is the join key and shortening
// it to a name would dangle every edge into it.

// ComputeInstances enumerates the project's virtual machines.
func ComputeInstances(list Lister[*computepb.Instance]) Subcollector {
	return New("gcp-compute-instances", list, convertInstance)
}

func convertInstance(projectID string, inst *computepb.Instance) (gcpgraph.Result, error) {
	if inst == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil instance")
	}
	selfLink := inst.GetSelfLink()
	if selfLink == "" {
		// The self-link is the id every edge joins on, so an instance without
		// one cannot be placed in the graph at all. It is a malformed response
		// rather than an instance with nothing to say.
		return gcpgraph.Result{}, fmt.Errorf(
			"instance %q carries no self link, which is the node id every edge joins on",
			inst.GetName())
	}

	content := gcpcontent.Instance{
		Name:              inst.GetName(),
		SelfLink:          selfLink,
		Zone:              gcpgraph.LastSegment(inst.GetZone()),
		MachineType:       gcpgraph.LastSegment(inst.GetMachineType()),
		Status:            inst.GetStatus(),
		CreationTimestamp: inst.GetCreationTimestamp(),
		Labels:            inst.GetLabels(),
		Tags:              inst.GetTags().GetItems(),
	}
	for _, sa := range inst.GetServiceAccounts() {
		if email := sa.GetEmail(); email != "" {
			content.ServiceAccounts = append(content.ServiceAccounts, email)
		}
	}
	for _, nic := range inst.GetNetworkInterfaces() {
		content.NetworkInterfaces = append(content.NetworkInterfaces, gcpcontent.NetworkInterface{
			Network:    nic.GetNetwork(),
			Subnetwork: nic.GetSubnetwork(),
			NetworkIP:  nic.GetNetworkIP(),
			ExternalIP: firstExternalIP(nic),
		})
	}
	for _, disk := range inst.GetDisks() {
		if source := disk.GetSource(); source != "" {
			content.Disks = append(content.Disks, source)
		}
	}

	raw, err := gcpcontent.Marshal(content)
	if err != nil {
		return gcpgraph.Result{}, err
	}

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID:           selfLink,
		Name:         inst.GetName(),
		ResourceType: gcpgraph.ResourceTypeInstance,
		Region:       content.Zone,
		Content:      raw,
		Metadata:     instanceMetadata(content),
	}}}

	for _, nic := range content.NetworkInterfaces {
		if nic.Subnetwork != "" {
			out.Relations = append(out.Relations, gcpgraph.Relation{
				From: selfLink, To: nic.Subnetwork, Type: gcpgraph.EdgeUsesSubnet,
			})
		}
		if nic.Network != "" {
			out.Relations = append(out.Relations, gcpgraph.Relation{
				From: selfLink, To: nic.Network, Type: gcpgraph.EdgeUsesNetwork,
			})
		}
	}
	for _, email := range content.ServiceAccounts {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: selfLink, To: gcpgraph.ServiceAccountResourceName(projectID, email),
			Type: gcpgraph.EdgeUsesSA,
		})
	}
	for _, source := range content.Disks {
		// The edge runs FROM the disk: a disk is bound to the instance it is
		// attached to, and reading it the other way would make an instance
		// "bound to" its own storage.
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: source, To: selfLink, Type: gcpgraph.EdgeBoundTo,
		})
	}
	return out, nil
}

// firstExternalIP returns the first external address on a NIC, which is the one
// a published DNS record can resolve to.
func firstExternalIP(nic *computepb.NetworkInterface) string {
	for _, ac := range nic.GetAccessConfigs() {
		if nat := ac.GetNatIP(); nat != "" {
			return nat
		}
	}
	return ""
}

// instanceMetadata is what a consumer filters on without opening the content.
//
// Labels and tags are FLATTENED under a prefix rather than nested, so a metadata
// equality filter can ask for one of them; a tag has no value, so its presence
// under the key is the whole signal. Service accounts are deliberately absent:
// they are edges, and duplicating them here would give two answers to one
// question.
func instanceMetadata(c gcpcontent.Instance) map[string]string {
	m := make(map[string]string, 8+len(c.Labels)+len(c.Tags))
	setIfNotEmpty(m, "status", c.Status)
	setIfNotEmpty(m, "machineType", c.MachineType)
	setIfNotEmpty(m, "zone", c.Zone)
	setIfNotEmpty(m, "creation_time", c.CreationTimestamp)
	for k, v := range c.Labels {
		m["label/"+k] = v
	}
	for _, tag := range c.Tags {
		if tag != "" {
			m["tag/"+tag] = ""
		}
	}
	if len(c.NetworkInterfaces) > 0 {
		setIfNotEmpty(m, "primary_ip", c.NetworkInterfaces[0].NetworkIP)
		setIfNotEmpty(m, "external_ip", c.NetworkInterfaces[0].ExternalIP)
	}
	return m
}

// setIfNotEmpty keeps empty-string placeholders out of the metadata map, so a
// consumer's "is this key set" question has one answer rather than two.
func setIfNotEmpty(m map[string]string, key, value string) {
	if value != "" {
		m[key] = value
	}
}
