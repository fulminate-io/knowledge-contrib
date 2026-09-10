// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"fmt"
	"strconv"

	computepb "cloud.google.com/go/compute/apiv1/computepb"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// network.go — the VPC layer: networks, subnetworks and firewall rules.
//
// A FIREWALL RULE'S CONTENT IS THE RESOLVER'S INPUT. It is not stored for a
// reader's benefit: the reachability resolver dispatches on the direction, the
// three source families and the allow clauses, so every one of them is written
// out here and a field dropped here is a cell the resolver can never reach.

// Networks enumerates the project's VPC networks.
func Networks(list Lister[*computepb.Network]) Subcollector {
	return New("gcp-networks", list, convertNetwork)
}

func convertNetwork(_ string, net *computepb.Network) (gcpgraph.Result, error) {
	if net == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil network")
	}
	selfLink := net.GetSelfLink()
	if selfLink == "" {
		return gcpgraph.Result{}, fmt.Errorf("network %q carries no self link", net.GetName())
	}

	fields := map[string]string{}
	setIfNotEmpty(fields, "routingMode", net.GetRoutingConfig().GetRoutingMode())
	content := gcpcontent.Generic{
		Name:        net.GetName(),
		SelfLink:    selfLink,
		Description: net.GetDescription(),
		CreateTime:  net.GetCreationTimestamp(),
		Fields:      fields,
	}
	raw, err := gcpcontent.Marshal(content)
	if err != nil {
		return gcpgraph.Result{}, err
	}

	metadata := map[string]string{
		"auto_create_subnetworks": strconv.FormatBool(net.GetAutoCreateSubnetworks()),
		"subnetwork_count":        strconv.Itoa(len(net.GetSubnetworks())),
	}
	setIfNotEmpty(metadata, "creation_time", net.GetCreationTimestamp())

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID:           selfLink,
		Name:         net.GetName(),
		ResourceType: gcpgraph.ResourceTypeNetwork,
		Content:      raw,
		Metadata:     metadata,
	}}}

	// A network's own subnetwork list is the containment edge. The subnetwork
	// enumeration emits the subnet NODE; this edge exists whether or not that
	// enumeration was permitted, which is what keeps a partially permitted walk
	// from losing the topology.
	for _, subnet := range net.GetSubnetworks() {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: selfLink, To: subnet, Type: gcpgraph.EdgeContains,
		})
	}
	for _, peering := range net.GetPeerings() {
		peer := peering.GetNetwork()
		if peer == "" {
			continue
		}
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: selfLink, To: peer, Type: gcpgraph.EdgePeeredWith,
			Metadata: map[string]string{
				"peering_name": peering.GetName(),
				"state":        peering.GetState(),
			},
		})
	}
	return out, nil
}

// Subnetworks enumerates the project's subnetworks.
func Subnetworks(list Lister[*computepb.Subnetwork]) Subcollector {
	return New("gcp-subnetworks", list, convertSubnetwork)
}

func convertSubnetwork(_ string, subnet *computepb.Subnetwork) (gcpgraph.Result, error) {
	if subnet == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil subnetwork")
	}
	selfLink := subnet.GetSelfLink()
	if selfLink == "" {
		return gcpgraph.Result{}, fmt.Errorf("subnetwork %q carries no self link", subnet.GetName())
	}

	content := gcpcontent.Subnetwork{
		Name:        subnet.GetName(),
		SelfLink:    selfLink,
		Network:     subnet.GetNetwork(),
		Region:      gcpgraph.LastSegment(subnet.GetRegion()),
		IPCIDRRange: subnet.GetIpCidrRange(),
		Purpose:     subnet.GetPurpose(),
	}
	raw, err := gcpcontent.Marshal(content)
	if err != nil {
		return gcpgraph.Result{}, err
	}

	metadata := map[string]string{
		"private_google_access": strconv.FormatBool(subnet.GetPrivateIpGoogleAccess()),
	}
	setIfNotEmpty(metadata, "ip_cidr_range", content.IPCIDRRange)
	setIfNotEmpty(metadata, "purpose", content.Purpose)

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID:           selfLink,
		Name:         subnet.GetName(),
		ResourceType: gcpgraph.ResourceTypeSubnetwork,
		Region:       content.Region,
		Content:      raw,
		Metadata:     metadata,
	}}}
	if content.Network != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: selfLink, To: content.Network, Type: gcpgraph.EdgeUsesNetwork,
		})
	}
	return out, nil
}

// Firewalls enumerates the project's VPC firewall rules.
func Firewalls(list Lister[*computepb.Firewall]) Subcollector {
	return New("gcp-firewalls", list, convertFirewall)
}

func convertFirewall(_ string, fw *computepb.Firewall) (gcpgraph.Result, error) {
	if fw == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil firewall rule")
	}
	selfLink := fw.GetSelfLink()
	if selfLink == "" {
		return gcpgraph.Result{}, fmt.Errorf("firewall rule %q carries no self link", fw.GetName())
	}

	content := gcpcontent.Firewall{
		Name:                  fw.GetName(),
		SelfLink:              selfLink,
		Network:               fw.GetNetwork(),
		Direction:             fw.GetDirection(),
		Disabled:              fw.GetDisabled(),
		Priority:              fw.GetPriority(),
		TargetTags:            fw.GetTargetTags(),
		TargetServiceAccounts: fw.GetTargetServiceAccounts(),
		SourceRanges:          fw.GetSourceRanges(),
		SourceTags:            fw.GetSourceTags(),
		SourceServiceAccounts: fw.GetSourceServiceAccounts(),
		DestinationRanges:     fw.GetDestinationRanges(),
	}
	for _, allowed := range fw.GetAllowed() {
		content.Allowed = append(content.Allowed, gcpcontent.FirewallRule{
			Protocol: allowed.GetIPProtocol(), Ports: allowed.GetPorts(),
		})
	}
	for _, denied := range fw.GetDenied() {
		content.Denied = append(content.Denied, gcpcontent.FirewallRule{
			Protocol: denied.GetIPProtocol(), Ports: denied.GetPorts(),
		})
	}
	raw, err := gcpcontent.Marshal(content)
	if err != nil {
		return gcpgraph.Result{}, err
	}

	metadata := map[string]string{
		"disabled": strconv.FormatBool(content.Disabled),
		"priority": strconv.Itoa(int(content.Priority)),
	}
	setIfNotEmpty(metadata, "direction", content.Direction)

	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID:           selfLink,
		Name:         fw.GetName(),
		ResourceType: gcpgraph.ResourceTypeFirewall,
		Content:      raw,
		Metadata:     metadata,
	}}}
	if content.Network != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: selfLink, To: content.Network, Type: gcpgraph.EdgeProtects,
		})
	}
	return out, nil
}
