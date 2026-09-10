// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"fmt"
	"strconv"

	computepb "cloud.google.com/go/compute/apiv1/computepb"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// router.go — Cloud Routers and the two things that live INSIDE one and have no
// resource of their own in the API: a NAT gateway and a BGP peer.
//
// BOTH ARE SYNTHESIZED. A NAT config and a BGP peer are fields on the router, not
// addressable resources, so this collector gives each a node with an id derived
// from its parent. That is what lets an egress path or a peering be walked at
// all; without it the whole topology is one opaque router node.
//
// THE BGP PEER IS ALSO AN UNCOLLECTED NODE, and it says so. The far side of a
// peering is somebody else's router, on somebody else's network, which no
// enumeration of this project can reach. It is emitted with a sentinel id and a
// metadata flag saying it was referenced rather than collected, so a reader can
// tell an unknown peer from a peer this walk failed to describe.

// Routers enumerates the project's Cloud Routers, their NAT gateways and their
// BGP peers.
func Routers(list Lister[*computepb.Router]) Subcollector {
	return New("gcp-routers", list, convertRouter)
}

func convertRouter(_ string, router *computepb.Router) (gcpgraph.Result, error) {
	if router == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil router")
	}
	selfLink := router.GetSelfLink()
	if selfLink == "" {
		return gcpgraph.Result{}, fmt.Errorf("router %q carries no self link", router.GetName())
	}
	region := gcpgraph.LastSegment(router.GetRegion())

	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: router.GetName(), SelfLink: selfLink, Description: router.GetDescription(),
		Location: region, CreateTime: router.GetCreationTimestamp(),
		Fields: nonEmptyFields(map[string]string{"network": router.GetNetwork()}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: selfLink, Name: router.GetName(), ResourceType: gcpgraph.ResourceTypeRouter,
		Region: region, Content: raw,
		Metadata: map[string]string{
			"nat_count":      strconv.Itoa(len(router.GetNats())),
			"bgp_peer_count": strconv.Itoa(len(router.GetBgpPeers())),
		},
	}}}
	if network := router.GetNetwork(); network != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: selfLink, To: network, Type: gcpgraph.EdgeUsesNetwork,
		})
	}

	for _, nat := range router.GetNats() {
		got, err := natResource(selfLink, region, nat)
		if err != nil {
			return gcpgraph.Result{}, err
		}
		out.Add(got)
	}
	for _, peer := range router.GetBgpPeers() {
		out.Add(bgpPeerResource(selfLink, peer))
	}
	return out, nil
}

// natResource synthesizes the node for one NAT gateway configured on a router.
// Its id is the router's self-link with the gateway's name appended, because the
// API gives it no identifier of its own and a name alone would collide across
// routers.
func natResource(routerSelfLink, region string, nat *computepb.RouterNat) (gcpgraph.Result, error) {
	name := nat.GetName()
	if name == "" {
		return gcpgraph.Result{}, fmt.Errorf(
			"router %q carries a NAT gateway with no name, which is the only thing that identifies it",
			routerSelfLink)
	}
	id := routerSelfLink + "/nats/" + name
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: name, SelfLink: id, Location: region,
		Fields: nonEmptyFields(map[string]string{
			"natIpAllocateOption":           nat.GetNatIpAllocateOption(),
			"sourceSubnetworkIpRangesToNat": nat.GetSourceSubnetworkIpRangesToNat(),
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: id, Name: name, ResourceType: gcpgraph.ResourceTypeNAT, Region: region, Content: raw,
		Metadata: map[string]string{
			"nat_ip_count":      strconv.Itoa(len(nat.GetNatIps())),
			"allocate_option":   nat.GetNatIpAllocateOption(),
			"subnetwork_ranges": nat.GetSourceSubnetworkIpRangesToNat(),
		},
	}}}
	out.Relations = append(out.Relations, gcpgraph.Relation{
		From: routerSelfLink, To: id, Type: gcpgraph.EdgeContains,
	})
	// The subnetworks whose egress this gateway carries. A gateway configured
	// for the whole network names none, and the router's own network edge is
	// what carries that case.
	for _, subnet := range nat.GetSubnetworks() {
		if ref := subnet.GetName(); ref != "" {
			out.Relations = append(out.Relations, gcpgraph.Relation{
				From: id, To: ref, Type: gcpgraph.EdgeRoutesVia,
			})
		}
	}
	return out, nil
}

// bgpPeerResource synthesizes the node for the FAR SIDE of a BGP session. It is
// an uncollected reference by construction and is marked as one.
func bgpPeerResource(routerSelfLink string, peer *computepb.RouterBgpPeer) gcpgraph.Result {
	address := peer.GetPeerIpAddress()
	if address == "" {
		// Without an address there is nothing to identify the peer BY, so no
		// node is synthesized. This is not a malformed response: a peer entry
		// mid-configuration legitimately has no address yet.
		return gcpgraph.Result{}
	}
	id := gcpgraph.BGPPeerID(address)
	metadata := map[string]string{
		"collected":        "false",
		"collected_reason": "external BGP peer with no counterpart in this project",
		"peer_ip":          address,
	}
	setIfNotEmpty(metadata, "peer_name", peer.GetName())
	if asn := peer.GetPeerAsn(); asn != 0 {
		metadata["peer_asn"] = strconv.FormatUint(uint64(asn), 10)
	}
	return gcpgraph.Result{
		Resources: []gcpgraph.Resource{{
			ID: id, Name: address, ResourceType: gcpgraph.ResourceTypeBGPPeer,
			Summary:  gcpgraph.ResourceTypeBGPPeer + " " + address,
			Metadata: metadata,
		}},
		Relations: []gcpgraph.Relation{{
			From: routerSelfLink, To: id, Type: gcpgraph.EdgePeeredWith,
			Metadata: map[string]string{"peer_name": peer.GetName()},
		}},
	}
}

// InstanceGroups enumerates the project's instance groups.
func InstanceGroups(list Lister[*computepb.InstanceGroup]) Subcollector {
	return New("gcp-instance-groups", list, convertInstanceGroup)
}

func convertInstanceGroup(_ string, group *computepb.InstanceGroup) (gcpgraph.Result, error) {
	if group == nil {
		return gcpgraph.Result{}, fmt.Errorf("the API returned a nil instance group")
	}
	selfLink := group.GetSelfLink()
	if selfLink == "" {
		return gcpgraph.Result{}, fmt.Errorf("instance group %q carries no self link", group.GetName())
	}
	location := gcpgraph.LastSegment(group.GetZone())
	if location == "" {
		location = gcpgraph.LastSegment(group.GetRegion())
	}
	raw, err := gcpcontent.Marshal(gcpcontent.Generic{
		Name: group.GetName(), SelfLink: selfLink, Description: group.GetDescription(),
		Location: location, CreateTime: group.GetCreationTimestamp(),
		Fields: nonEmptyFields(map[string]string{
			"network":    group.GetNetwork(),
			"subnetwork": group.GetSubnetwork(),
		}),
	})
	if err != nil {
		return gcpgraph.Result{}, err
	}
	out := gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: selfLink, Name: group.GetName(), ResourceType: gcpgraph.ResourceTypeInstanceGroup,
		Region: location, Content: raw,
		Metadata: map[string]string{"size": strconv.Itoa(int(group.GetSize()))},
	}}}
	if network := group.GetNetwork(); network != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: selfLink, To: network, Type: gcpgraph.EdgeUsesNetwork,
		})
	}
	if subnet := group.GetSubnetwork(); subnet != "" {
		out.Relations = append(out.Relations, gcpgraph.Relation{
			From: selfLink, To: subnet, Type: gcpgraph.EdgeUsesSubnet,
		})
	}
	return out, nil
}
