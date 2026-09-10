// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// derive_peering.go — PEERED_WITH and ROUTES_TO_PEER.
//
// FOUR GUARDS DECIDE WHETHER A PEERING PRODUCES ANYTHING, and each is a way to
// emit an edge that should not exist rather than a tidiness rule:
//
//   - the status must be exactly "active". A pending-acceptance, rejected,
//     failed or deleted peering carries both VPC ids and describes no path
//     traffic can take, so an edge for it asserts reachability that does not
//     exist.
//   - both VpcInfo blocks must be present, and both VPC ids non-empty. A
//     half-populated response yields an edge with one real end and one empty
//     one, which the sink would drop anyway — but silently, where this is
//     explicit.
//   - the CIDRs decide ROUTES_TO_PEER alone. PEERED_WITH is about the two VPCs;
//     ROUTES_TO_PEER is about the ranges that reach each other, so a peering
//     whose response carries no CIDR blocks yields the first and not the second.
//
// A KNOWN PARITY BOUND, recorded rather than discovered. The built-in collector
// suppresses a CROSS-ACCOUNT peering whose peer VPC it cannot find in another
// account's already-collected cloud graph. That filter is a READ of another
// graph, which the collector contract does not express: one collect returns one
// envelope for one graph instance, and there is no cross-graph read. So this
// collector emits the edge for EVERY active peering it sees, and its
// cross-account PEERED_WITH and ROUTES_TO_PEER sets are SUPERSETS of the
// built-in collector's rather than subsets. A superset of true relationships is
// the correct behavior under a contract with no cross-graph read; silently
// dropping cross-account peerings would be the alternative and it loses real
// edges.

// peeringActive is the one status code that describes a usable path.
const peeringActive = "active"

func (w *walkContext) derivePeering() {
	for _, c := range w.derived.peerings {
		w.emitPeering(c)
	}
}

func (w *walkContext) emitPeering(c ec2types.VpcPeeringConnection) {
	if c.Status == nil || string(c.Status.Code) != peeringActive {
		return
	}
	if c.RequesterVpcInfo == nil || c.AccepterVpcInfo == nil {
		return
	}
	reqVPC := deref(c.RequesterVpcInfo.VpcId)
	accVPC := deref(c.AccepterVpcInfo.VpcId)
	if reqVPC == "" || accVPC == "" {
		return
	}

	// EACH SIDE'S ARN IS BUILT IN ITS OWN ACCOUNT AND REGION, not in this walk's.
	// A peering's far side routinely lives in another account, another region or
	// both, and composing its id from this walk's would name a VPC that does not
	// exist rather than the one that does.
	reqARN := ec2ARN(
		derefOr(c.RequesterVpcInfo.Region, w.region),
		derefOr(c.RequesterVpcInfo.OwnerId, w.account),
		"vpc", reqVPC)
	accARN := ec2ARN(
		derefOr(c.AccepterVpcInfo.Region, w.region),
		derefOr(c.AccepterVpcInfo.OwnerId, w.account),
		"vpc", accVPC)

	evidence := map[string]string{
		"peering_connection_id": deref(c.VpcPeeringConnectionId),
		"status":                string(c.Status.Code),
	}
	// PEERED_WITH IS SYMMETRIC and both directions are emitted, because a
	// traversal starting at either VPC must find the other and the graph carries
	// directed edges.
	w.sink.addEdge(reqARN, accARN, EdgePeeredWith, evidence)
	w.sink.addEdge(accARN, reqARN, EdgePeeredWith, evidence)

	reqCIDR := deref(c.RequesterVpcInfo.CidrBlock)
	accCIDR := deref(c.AccepterVpcInfo.CidrBlock)
	if reqCIDR == "" || accCIDR == "" {
		return
	}
	// THE TO-ENDPOINT IS THE CIDR SENTINEL, not a bare CIDR string. The built-in
	// collector writes the bare string, which names no node in its own result and
	// therefore resolves against nothing; emitting the sentinel id makes the edge
	// land on the node the rules pass already creates for that range, so a
	// traversal from a VPC to the range it reaches actually arrives somewhere.
	// The choice is stated here because it is a deliberate divergence from the
	// built-in shape and the endpoint assertion in this package's tests names it.
	w.sink.addEdge(reqARN, w.addCIDRSentinel(accCIDR), EdgeRoutesToPeer, evidence)
	w.sink.addEdge(accARN, w.addCIDRSentinel(reqCIDR), EdgeRoutesToPeer, evidence)
}
