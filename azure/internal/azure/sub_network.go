// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/network/armnetwork/v6"
)

// sub_network.go — virtual networks and their subnets, security groups and
// their rules, and network peerings.

// vnetSub walks the subscription's virtual networks and emits their subnets as
// nodes of their own.
//
// SUBNETS ARE NOT LISTED SEPARATELY. They arrive inside their network's
// response, so enumerating them costs nothing extra; a separate list call would
// be one call per network for data already in hand.
type vnetSub struct{ subBase }

func (s *vnetSub) Collect(ctx context.Context) (subResult, error) {
	client, err := armnetwork.NewVirtualNetworksClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("virtual networks client: %w", err)
	}
	var out subResult
	err = drain(ctx, client.NewListAllPager(nil), func(page armnetwork.VirtualNetworksClientListAllResponse) error {
		for _, vnet := range page.Value {
			if vnet == nil || vnet.ID == nil {
				continue
			}
			r, err := vnetResource(vnet)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			subnets, edges, err := subnetResources(vnet)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, subnets...)
			out.edges = append(out.edges, edges...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing virtual networks: %w", err)
	}
	return out, nil
}

func vnetResource(vnet *armnetwork.VirtualNetwork) (resource, error) {
	content, err := marshalContent(vnet)
	if err != nil {
		return resource{}, fmt.Errorf("projecting virtual network %s: %w", ptr(vnet.ID), err)
	}
	r := resource{
		id:           ptr(vnet.ID),
		name:         ptr(vnet.Name),
		resourceType: rtVNet,
		region:       ptr(vnet.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if vnet.Properties != nil && vnet.Properties.AddressSpace != nil {
		for i, prefix := range derefStrings(vnet.Properties.AddressSpace.AddressPrefixes) {
			r.metadata["addressPrefix_"+strconv.Itoa(i)] = prefix
		}
	}
	return r, nil
}

// subnetResources emits a node per subnet plus the two relationships a subnet
// carries: the network it belongs to and the security group applied to it.
//
// THE MEMBERSHIP EDGE RUNS SUBNET TO NETWORK, matching the direction every
// other "sits inside a network" edge in this collector runs.
func subnetResources(vnet *armnetwork.VirtualNetwork) ([]resource, []edge, error) {
	if vnet.Properties == nil {
		return nil, nil, nil
	}
	var resources []resource
	var edges []edge
	for _, subnet := range vnet.Properties.Subnets {
		if subnet == nil || subnet.ID == nil {
			continue
		}
		content, err := marshalContent(subnet)
		if err != nil {
			return nil, nil, fmt.Errorf("projecting subnet %s: %w", ptr(subnet.ID), err)
		}
		r := resource{
			id:           ptr(subnet.ID),
			name:         ptr(subnet.Name),
			resourceType: rtSubnet,
			region:       ptr(vnet.Location),
			content:      content,
			metadata:     map[string]string{},
		}
		if subnet.Properties != nil {
			if v := ptr(subnet.Properties.AddressPrefix); v != "" {
				r.metadata["addressPrefix"] = v
			}
		}
		resources = append(resources, r)
		edges = append(edges, edge{from: r.id, to: ptr(vnet.ID), relation: edgeUsesNetwork})
		if subnet.Properties != nil && subnet.Properties.NetworkSecurityGroup != nil {
			if nsgID := ptr(subnet.Properties.NetworkSecurityGroup.ID); nsgID != "" {
				edges = append(edges, edge{from: r.id, to: nsgID, relation: edgeUsesSecurityGroup})
			}
		}
	}
	return resources, edges, nil
}

// nsgSub walks the subscription's network security groups and emits each of
// their rules as a node.
//
// THE RULES ARE ALSO CARRIED AS A WALK FACT on the group's resource, which is
// what resolver 1 reads to draw reachability. The rule NODES are for a reader
// who wants the rule itself; the walk fact is for the resolver, and neither is
// derived from the other.
type nsgSub struct{ subBase }

func (s *nsgSub) Collect(ctx context.Context) (subResult, error) {
	client, err := armnetwork.NewSecurityGroupsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("security groups client: %w", err)
	}
	var out subResult
	err = drain(ctx, client.NewListAllPager(nil), func(page armnetwork.SecurityGroupsClientListAllResponse) error {
		for _, nsg := range page.Value {
			if nsg == nil || nsg.ID == nil {
				continue
			}
			r, err := nsgResource(nsg)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			rules, edges, err := nsgRuleResources(nsg)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, rules...)
			out.edges = append(out.edges, edges...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing security groups: %w", err)
	}
	return out, nil
}

func nsgResource(nsg *armnetwork.SecurityGroup) (resource, error) {
	content, err := marshalContent(nsg)
	if err != nil {
		return resource{}, fmt.Errorf("projecting security group %s: %w", ptr(nsg.ID), err)
	}
	r := resource{
		id:           ptr(nsg.ID),
		name:         ptr(nsg.Name),
		resourceType: rtNSG,
		region:       ptr(nsg.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if nsg.Properties != nil {
		r.metadata["ruleCount"] = strconv.Itoa(len(nsg.Properties.SecurityRules))
		// The walk fact resolver 1 reads. DEFAULT RULES ARE DELIBERATELY
		// EXCLUDED from it while being INCLUDED as nodes below: every group
		// carries the same default rules, so admitting them would draw an
		// identical reachability fan out of every security group in the
		// subscription and drown the rules an operator actually wrote.
		r.nsgRules = readNSGRules(nsg.Properties.SecurityRules, false)
	}
	return r, nil
}

// nsgRuleResources emits a node per rule, both the operator's own and Azure's
// defaults, with the containment edge from the group.
func nsgRuleResources(nsg *armnetwork.SecurityGroup) ([]resource, []edge, error) {
	if nsg.Properties == nil {
		return nil, nil, nil
	}
	var resources []resource
	var edges []edge
	emit := func(rules []*armnetwork.SecurityRule, isDefault bool) error {
		for _, rule := range rules {
			if rule == nil || rule.Name == nil {
				continue
			}
			content, err := marshalContent(rule)
			if err != nil {
				return fmt.Errorf("projecting security rule %s: %w", ptr(rule.Name), err)
			}
			id := ptr(nsg.ID) + "/securityRules/" + *rule.Name
			r := resource{
				id:           id,
				name:         *rule.Name,
				resourceType: rtNSGRule,
				region:       ptr(nsg.Location),
				content:      content,
				metadata:     nsgRuleMetadata(rule, isDefault),
			}
			resources = append(resources, r)
			edges = append(edges, containsEdges(ptr(nsg.ID), id)...)
		}
		return nil
	}
	if err := emit(nsg.Properties.SecurityRules, false); err != nil {
		return nil, nil, err
	}
	if err := emit(nsg.Properties.DefaultSecurityRules, true); err != nil {
		return nil, nil, err
	}
	return resources, edges, nil
}

func nsgRuleMetadata(rule *armnetwork.SecurityRule, isDefault bool) map[string]string {
	md := map[string]string{
		"name":       ptr(rule.Name),
		"is_default": strconv.FormatBool(isDefault),
	}
	p := rule.Properties
	if p == nil {
		return md
	}
	if p.Access != nil {
		md["access"] = string(*p.Access)
	}
	if p.Direction != nil {
		md["direction"] = string(*p.Direction)
	}
	if p.Protocol != nil {
		md["protocol"] = string(*p.Protocol)
	}
	if p.Priority != nil {
		md["priority"] = strconv.Itoa(int(*p.Priority))
	}
	setIfNotEmpty(md, "source_address_prefix", ptr(p.SourceAddressPrefix))
	setIfNotEmpty(md, "destination_address_prefix", ptr(p.DestinationAddressPrefix))
	setIfNotEmpty(md, "source_port_range", ptr(p.SourcePortRange))
	setIfNotEmpty(md, "destination_port_range", ptr(p.DestinationPortRange))
	return md
}

// readNSGRules projects the SDK's rules into the walk fact resolver 1 reads.
func readNSGRules(rules []*armnetwork.SecurityRule, isDefault bool) []nsgRule {
	out := make([]nsgRule, 0, len(rules))
	for _, rule := range rules {
		if rule == nil || rule.Properties == nil {
			continue
		}
		p := rule.Properties
		r := nsgRule{
			name:      ptr(rule.Name),
			protocol:  stringOfPtr(p.Protocol),
			isDefault: isDefault,
		}
		if p.Access != nil {
			r.access = string(*p.Access)
		}
		if p.Direction != nil {
			r.direction = string(*p.Direction)
		}
		if p.Priority != nil {
			r.priority = *p.Priority
		}
		r.sourceCIDRs = appendNonEmpty(derefStrings(p.SourceAddressPrefixes), ptr(p.SourceAddressPrefix))
		r.destCIDRs = appendNonEmpty(derefStrings(p.DestinationAddressPrefixes), ptr(p.DestinationAddressPrefix))
		r.destPorts = appendNonEmpty(derefStrings(p.DestinationPortRanges), ptr(p.DestinationPortRange))
		out = append(out, r)
	}
	return out
}

// vnetPeeringSub walks each network's peerings.
//
// IT LISTS NETWORKS AGAIN rather than sharing the network walk's output,
// because the two subcollectors run concurrently and share nothing. The cost is
// one extra subscription-wide list; the alternative is a dependency between
// subcollectors that would make one wait on the other.
type vnetPeeringSub struct{ subBase }

func (s *vnetPeeringSub) Collect(ctx context.Context) (subResult, error) {
	vnetClient, err := armnetwork.NewVirtualNetworksClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("virtual networks client: %w", err)
	}
	peerClient, err := armnetwork.NewVirtualNetworkPeeringsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("peerings client: %w", err)
	}

	var out subResult
	err = drain(ctx, vnetClient.NewListAllPager(nil), func(page armnetwork.VirtualNetworksClientListAllResponse) error {
		for _, vnet := range page.Value {
			if vnet == nil || vnet.ID == nil || vnet.Name == nil {
				continue
			}
			rg := armResourceGroup(*vnet.ID)
			if rg == "" {
				continue
			}
			err := drain(ctx, peerClient.NewListPager(rg, *vnet.Name, nil),
				func(pp armnetwork.VirtualNetworkPeeringsClientListResponse) error {
					for _, peering := range pp.Value {
						if peering == nil || peering.ID == nil {
							continue
						}
						r, err := peeringResource(peering, vnet)
						if err != nil {
							return err
						}
						out.resources = append(out.resources, r)
						out.edges = append(out.edges, peeringEdges(peering, ptr(vnet.ID))...)
					}
					return nil
				})
			if err != nil {
				return fmt.Errorf("listing peerings of %s: %w", *vnet.Name, err)
			}
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing virtual networks for peerings: %w", err)
	}
	return out, nil
}

func peeringResource(peering *armnetwork.VirtualNetworkPeering, vnet *armnetwork.VirtualNetwork) (resource, error) {
	content, err := marshalContent(peering)
	if err != nil {
		return resource{}, fmt.Errorf("projecting peering %s: %w", ptr(peering.ID), err)
	}
	return resource{
		id:           ptr(peering.ID),
		name:         ptr(peering.Name),
		resourceType: rtVNetPeering,
		region:       ptr(vnet.Location),
		content:      content,
		metadata:     map[string]string{},
	}, nil
}

// peeringEdges draws the peering in BOTH directions.
//
// A peering is one-way in Azure's model — each side configures its own — but a
// consumer asking "what is this network peered with" must get an answer from
// either end, and the remote side's own peering resource may live in a
// subscription this walk cannot see.
func peeringEdges(peering *armnetwork.VirtualNetworkPeering, localVNetID string) []edge {
	if peering.Properties == nil || peering.Properties.RemoteVirtualNetwork == nil {
		return nil
	}
	remote := ptr(peering.Properties.RemoteVirtualNetwork.ID)
	if remote == "" {
		return nil
	}
	return []edge{
		{from: localVNetID, to: remote, relation: edgePeeredWith},
		{from: remote, to: localVNetID, relation: edgePeeredWith},
	}
}

// setIfNotEmpty writes a metadata key only when it has a value, so a node's
// metadata carries facts rather than a fixed set of keys half of them empty.
func setIfNotEmpty(md map[string]string, key, value string) {
	if value != "" {
		md[key] = value
	}
}

// appendNonEmpty appends a scalar to a slice when it is set.
func appendNonEmpty(list []string, extra string) []string {
	if extra == "" {
		return list
	}
	return append(list, extra)
}

// stringOfPtr renders a pointer to a stringy SDK enum as its string value.
func stringOfPtr[T ~string](p *T) string {
	if p == nil {
		return ""
	}
	return string(*p)
}
