// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// derive_peering_test.go — the PEERING guard battery, split from derive_test.go
// for length. Every case here carries two VPC ids and describes no path traffic
// can take, which is what makes each one a way to emit an edge that should not
// exist rather than a tidiness rule.

// TestDerive_PeeringNegativeCells is the guard battery: every one of these
// shapes carries two VPC ids and describes no path traffic can take.
func TestDerive_PeeringNegativeCells(t *testing.T) {
	cases := []struct {
		name    string
		peering ec2types.VpcPeeringConnection
		why     string
	}{
		{
			name: "a pending-acceptance peering",
			peering: ec2types.VpcPeeringConnection{
				VpcPeeringConnectionId: new("pcx-pending"),
				Status:                 &ec2types.VpcPeeringConnectionStateReason{Code: ec2types.VpcPeeringConnectionStateReasonCodePendingAcceptance},
				RequesterVpcInfo:       &ec2types.VpcPeeringConnectionVpcInfo{VpcId: new(fixtureVPC), CidrBlock: new("10.0.0.0/16")},
				AccepterVpcInfo:        &ec2types.VpcPeeringConnectionVpcInfo{VpcId: new(fixturePeerVPC), CidrBlock: new("10.9.0.0/16")},
			},
			why: "no traffic flows over a peering nobody has accepted",
		},
		{
			name: "a peering missing one VpcInfo block",
			peering: ec2types.VpcPeeringConnection{
				VpcPeeringConnectionId: new("pcx-halfblock"),
				Status:                 &ec2types.VpcPeeringConnectionStateReason{Code: ec2types.VpcPeeringConnectionStateReasonCodeActive},
				RequesterVpcInfo:       &ec2types.VpcPeeringConnectionVpcInfo{VpcId: new(fixtureVPC)},
			},
			why: "an edge with one real end and one empty one",
		},
		{
			name: "a peering with an empty VPC id",
			peering: ec2types.VpcPeeringConnection{
				VpcPeeringConnectionId: new("pcx-emptyid"),
				Status:                 &ec2types.VpcPeeringConnectionStateReason{Code: ec2types.VpcPeeringConnectionStateReasonCodeActive},
				RequesterVpcInfo:       &ec2types.VpcPeeringConnectionVpcInfo{VpcId: new(fixtureVPC)},
				AccepterVpcInfo:        &ec2types.VpcPeeringConnectionVpcInfo{VpcId: new("")},
			},
			why: "an id composed around an empty segment",
		},
		{
			name: "a peering with no status block at all",
			peering: ec2types.VpcPeeringConnection{
				VpcPeeringConnectionId: new("pcx-nostatus"),
				RequesterVpcInfo:       &ec2types.VpcPeeringConnectionVpcInfo{VpcId: new(fixtureVPC)},
				AccepterVpcInfo:        &ec2types.VpcPeeringConnectionVpcInfo{VpcId: new(fixturePeerVPC)},
			},
			why: "a peering whose state cannot be read is not known to be active",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clients := emptyClients()
			clients.EC2.(*fakeEC2).describePeering = func(*ec2.DescribeVpcPeeringConnectionsInput) (*ec2.DescribeVpcPeeringConnectionsOutput, error) {
				return &ec2.DescribeVpcPeeringConnectionsOutput{
					VpcPeeringConnections: []ec2types.VpcPeeringConnection{tc.peering},
				}, nil
			}
			res := runWalk(t, clients, Params{})
			if got := len(edgesOfType(res, EdgePeeredWith)); got != 0 {
				t.Errorf("%d PEERED_WITH edge(s) emitted for %s: %s", got, tc.name, tc.why)
			}
			if got := len(edgesOfType(res, EdgeRoutesToPeer)); got != 0 {
				t.Errorf("%d ROUTES_TO_PEER edge(s) emitted for %s", got, tc.name)
			}
			// THE SAME-RUN KNOWN POSITIVE: the peering node itself IS emitted, so
			// the zeros above are the guard firing rather than the walk finding
			// nothing.
			if len(nodesOfType(res, ResourceTypeVPCPeeringConnection)) != 1 {
				t.Error("the peering RESOURCE must still be emitted; only its edges are guarded")
			}
		})
	}
}

// TestDerive_PeeringWithoutCIDRsYieldsPeeredWithButNoRoutes is the cell that
// separates the two relationships: PEERED_WITH is about the two VPCs and
// ROUTES_TO_PEER is about the ranges that reach each other.
func TestDerive_PeeringWithoutCIDRsYieldsPeeredWithButNoRoutes(t *testing.T) {
	clients := emptyClients()
	clients.EC2.(*fakeEC2).describePeering = func(*ec2.DescribeVpcPeeringConnectionsInput) (*ec2.DescribeVpcPeeringConnectionsOutput, error) {
		return &ec2.DescribeVpcPeeringConnectionsOutput{
			VpcPeeringConnections: []ec2types.VpcPeeringConnection{{
				VpcPeeringConnectionId: new("pcx-nocidr"),
				Status:                 &ec2types.VpcPeeringConnectionStateReason{Code: ec2types.VpcPeeringConnectionStateReasonCodeActive},
				RequesterVpcInfo:       &ec2types.VpcPeeringConnectionVpcInfo{VpcId: new(fixtureVPC)},
				AccepterVpcInfo:        &ec2types.VpcPeeringConnectionVpcInfo{VpcId: new(fixturePeerVPC)},
			}},
		}, nil
	}
	res := runWalk(t, clients, Params{})
	if len(edgesOfType(res, EdgePeeredWith)) != 2 {
		t.Errorf("an active peering with no CIDR blocks must still yield both PEERED_WITH directions; got %d",
			len(edgesOfType(res, EdgePeeredWith)))
	}
	if got := len(edgesOfType(res, EdgeRoutesToPeer)); got != 0 {
		t.Errorf("%d ROUTES_TO_PEER edge(s) emitted with no CIDR blocks to route between", got)
	}
}
