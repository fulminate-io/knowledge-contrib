// SPDX-License-Identifier: Apache-2.0

package awswalk

import (
	"fmt"

	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

// derive_rules.go — ALLOWS_INGRESS_FROM and ALLOWS_EGRESS_TO, from security-group
// and network-ACL rules, together with the CIDR sentinel nodes that give a rule's
// far end an endpoint.
//
// A RULE'S FAR END IS OFTEN NOT A RESOURCE. "Allow 10.0.0.0/8 on 443" names a
// range, not an instance, and a graph that dropped those edges could not answer
// what can reach a group at all — which is the question a security graph is for.
// So one sentinel node is emitted per DISTINCT CIDR any rule references, and the
// edge points at it. The sentinel is a node of this collector's own making and
// says so: its resource type is cidr-block and its id is composed, not an ARN.
//
// A RULE NAMING ANOTHER SECURITY GROUP points straight at that group's node id,
// which resolves inside this result when the group is in this account and dangles
// when the peer is in another — see derive_peering.go for the cross-account arm.

// addCIDRSentinel emits the node standing for one CIDR, once.
//
// The sink dedupes by id, so calling this per rule is correct rather than
// wasteful: many rules reference one range, and one node is what the graph wants.
func (w *walkContext) addCIDRSentinel(cidr string) string {
	if cidr == "" {
		return ""
	}
	id := cidrSentinelID(cidr)
	w.sink.addNode(newNode(resource{
		id:           id,
		resourceType: ResourceTypeCIDRBlock,
		name:         cidr,
		summary: fmt.Sprintf("CIDR block %s, referenced by a security-group or network-ACL rule in %s",
			cidr, w.region),
		detail: map[string]string{"cidr": cidr},
		region: w.region,
	}, w.account))
	return id
}

// deriveSecurityGroupRules turns every recorded rule into an ALLOWS edge.
func (w *walkContext) deriveSecurityGroupRules() {
	for _, set := range w.derived.sgRules {
		w.emitPermissions(set.groupARN, set.ingress, EdgeAllowsIngressFrom)
		w.emitPermissions(set.groupARN, set.egress, EdgeAllowsEgressTo)
	}
}

// emitPermissions emits one edge per referenced peer, for one rule direction.
//
// THE EDGE DIRECTION IS THE SAME FOR BOTH ARMS — always FROM the group — and the
// relationship type carries the direction instead. That is the built-in
// collector's own shape and it is what lets a traversal ask "what does this group
// allow" without knowing which end it is holding.
func (w *walkContext) emitPermissions(groupARN string, perms []ec2types.IpPermission, edgeType string) {
	for _, p := range perms {
		evidence := map[string]string{}
		put(evidence, "protocol", deref(p.IpProtocol))
		if p.FromPort != nil {
			evidence["from_port"] = fmt.Sprintf("%d", *p.FromPort)
		}
		if p.ToPort != nil {
			evidence["to_port"] = fmt.Sprintf("%d", *p.ToPort)
		}
		for _, r := range p.IpRanges {
			if id := w.addCIDRSentinel(deref(r.CidrIp)); id != "" {
				w.sink.addEdge(groupARN, id, edgeType, evidence)
			}
		}
		for _, r := range p.Ipv6Ranges {
			if id := w.addCIDRSentinel(deref(r.CidrIpv6)); id != "" {
				w.sink.addEdge(groupARN, id, edgeType, evidence)
			}
		}
		for _, peer := range p.UserIdGroupPairs {
			gid := deref(peer.GroupId)
			if gid == "" {
				continue
			}
			// THE PEER'S ACCOUNT DECIDES THE ENDPOINT. A pair carrying another
			// account's UserId names a group this walk never emits, so the id is
			// composed in THAT account and the edge dangles deliberately; without
			// the account substitution it would point at a group id in this
			// account that does not exist, which is worse than dangling because
			// it could collide.
			peerAccount := derefOr(peer.UserId, w.account)
			w.sink.addEdge(groupARN, peerSecurityGroupARN(w.region, peerAccount, gid), edgeType, evidence)
		}
	}
}

// deriveNetworkACLRules turns every recorded network-ACL entry into an ALLOWS
// edge, on the same terms.
//
// A DENY ENTRY IS NOT AN ALLOWS EDGE. The entries carry a rule action, and
// emitting an ALLOWS edge for a deny rule would invert the meaning of the graph's
// most security-relevant relationship.
func (w *walkContext) deriveNetworkACLRules() {
	for _, set := range w.derived.naclEntries {
		for _, e := range set.entries {
			if e.RuleAction != ec2types.RuleActionAllow {
				continue
			}
			edgeType := EdgeAllowsIngressFrom
			if deref(e.Egress) {
				edgeType = EdgeAllowsEgressTo
			}
			evidence := map[string]string{}
			put(evidence, "protocol", deref(e.Protocol))
			if e.RuleNumber != nil {
				evidence["rule_number"] = fmt.Sprintf("%d", *e.RuleNumber)
			}
			if e.PortRange != nil {
				if e.PortRange.From != nil {
					evidence["from_port"] = fmt.Sprintf("%d", *e.PortRange.From)
				}
				if e.PortRange.To != nil {
					evidence["to_port"] = fmt.Sprintf("%d", *e.PortRange.To)
				}
			}
			for _, cidr := range []string{deref(e.CidrBlock), deref(e.Ipv6CidrBlock)} {
				if id := w.addCIDRSentinel(cidr); id != "" {
					w.sink.addEdge(set.aclARN, id, edgeType, evidence)
				}
			}
		}
	}
}
