// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"fmt"
	"strconv"
	"strings"
)

// resolve_nsg.go — RESOLVER 1: an NSG's Allow rules become reachability edges.
//
// WHAT IT PRODUCES AND WHAT IT DOES NOT. Both endpoints are the same on both
// arms: the edge runs from the NSG NODE to an ADDRESS-RANGE SENTINEL this
// resolver mints, never to a virtual machine or a subnet. That is deliberate
// and it is the shape the parity target holds: a security rule admits an
// address range, and the resources inside that range are a question the range
// does not answer. Pointing the edge at a VM would produce a reachability claim
// the source data does not support, and no count of edge types would notice.
//
// THREE DROPS, AND THE THIRD IS THE ONE THAT SURPRISES. A Deny rule produces
// nothing; a rule produces exactly one direction, never both; and a rule whose
// address fields are all empty produces nothing THROUGH THE SAME PATH as a rule
// that names only application security groups. Those last two look like
// different cases and are one: the prefix list comes back empty and the emit
// loop runs zero times.

// nsgRule is one security rule as the walk read it, carried on the NSG resource
// as a walk fact. It is the resolver's whole input: the builtin collector
// re-parses the NSG's stored JSON to recover these fields, which this collector
// does not have to do because the walk still held the SDK struct.
type nsgRule struct {
	name        string
	access      string
	direction   string
	protocol    string
	priority    int32
	sourceCIDRs []string
	destCIDRs   []string
	destPorts   []string
	isDefault   bool
}

// methodNSGRule marks every edge this resolver emits, so a consumer can tell a
// derived reachability edge from a walked relationship and can decode the
// evidence below.
const methodNSGRule = "azure-nsg-rule"

// anyAddress is what Azure's wildcard address means, spelled as a range so a
// consumer reading the sentinel does not have to special-case an asterisk.
const anyAddress = "0.0.0.0/0"

// cidrSentinelID is the node id of an address range. The namespace keeps it
// from ever colliding with an ARM id.
func cidrSentinelID(cidr string) string { return "azure:cidr:" + cidr }

// resolveNSGRules returns the address-range sentinel nodes and the reachability
// edges implied by every NSG's rules.
func resolveNSGRules(nsgs []resource) ([]resource, []edge) {
	var edges []edge
	var order []string
	seen := map[string]struct{}{}

	for _, nsg := range nsgs {
		for _, rule := range nsg.nsgRules {
			if !isAllowRule(rule.access) {
				continue
			}
			egress := isOutbound(rule.direction)
			for _, cidr := range rulePeerCIDRs(rule, egress) {
				id := cidrSentinelID(cidr)
				if _, dup := seen[id]; !dup {
					seen[id] = struct{}{}
					order = append(order, cidr)
				}
				edges = append(edges, nsgEdge(nsg.id, id, egress, rule, cidr))
			}
		}
	}

	nodes := make([]resource, 0, len(order))
	for _, cidr := range order {
		nodes = append(nodes, resource{
			id:           cidrSentinelID(cidr),
			name:         cidr,
			resourceType: rtCIDRBlock,
			metadata: map[string]string{
				"cidr":              cidr,
				metaCollected:       "false",
				metaCollectedReason: "an address range is not an Azure resource and has no ARM id",
				metaDiscoveredVia:   "network security group rule",
			},
		})
	}
	return nodes, edges
}

// isAllowRule reports whether a rule permits rather than denies. A Deny rule
// describes what CANNOT reach the group, and an edge asserting reachability for
// it would invert the meaning of every query over these edges.
func isAllowRule(access string) bool { return strings.EqualFold(access, "Allow") }

// isOutbound reports which direction a rule governs, which decides which of the
// two edge types it produces and which of its address fields is the peer.
func isOutbound(direction string) bool { return strings.EqualFold(direction, "Outbound") }

// rulePeerCIDRs returns the address ranges on the PEER side of a rule: the
// destination for an outbound rule, the source for an inbound one. A rule whose
// peer side is empty — because it names application security groups instead, or
// because it names nothing — returns none, and its caller's loop runs zero
// times.
func rulePeerCIDRs(rule nsgRule, egress bool) []string {
	raw := rule.sourceCIDRs
	if egress {
		raw = rule.destCIDRs
	}
	out := make([]string, 0, len(raw))
	for _, a := range raw {
		if a == "" {
			continue
		}
		out = append(out, normalizeAddress(a))
	}
	return out
}

// normalizeAddress maps Azure's wildcard to the range it means and passes
// everything else through. A service tag ("Internet", "AzureLoadBalancer")
// passes through as itself: it names a real peer, and rewriting it to an
// address range would be a claim about which addresses Azure puts in it.
func normalizeAddress(addr string) string {
	if addr == "*" {
		return anyAddress
	}
	return addr
}

// nsgEdge builds one reachability edge. Inbound runs the NSG back to the source
// peer, outbound runs it out to the destination peer, and both carry the port
// and protocol as evidence so a consumer can tell "reachable on 443" from
// "reachable on anything".
func nsgEdge(nsgID, peerID string, egress bool, rule nsgRule, cidr string) edge {
	relation := edgeAllowsIngressFrom
	if egress {
		relation = edgeAllowsEgressTo
	}
	md := map[string]string{"cidr": cidr}
	if egress {
		md["egress"] = "true"
	}
	if p := normalizeProtocol(rule.protocol); p != "" {
		md["protocol"] = p
	}
	if from, to, ok := destPortRange(rule.destPorts); ok {
		md["port_from"] = strconv.Itoa(from)
		md["port_to"] = strconv.Itoa(to)
	}
	if rule.name != "" {
		md["rule"] = rule.name
	}
	return edge{from: nsgID, to: peerID, relation: relation, metadata: md, method: methodNSGRule}
}

// normalizeProtocol lowercases a protocol and drops Azure's wildcard, which
// means "every protocol" and is better expressed by the key's absence than by
// an asterisk a consumer has to know to ignore.
func normalizeProtocol(p string) string {
	if p == "" || p == "*" {
		return ""
	}
	return strings.ToLower(p)
}

// destPortRange returns the first parseable destination port range. Azure
// spells a range "80-443", a single port "443" and every port "*"; the last is
// reported as no range rather than as 0-0, so a consumer does not read a
// wildcard as port zero.
func destPortRange(ports []string) (from, to int, ok bool) {
	for _, p := range ports {
		if p == "" || p == "*" {
			continue
		}
		lo, hi, err := parsePortRange(p)
		if err != nil {
			continue
		}
		return lo, hi, true
	}
	return 0, 0, false
}

func parsePortRange(s string) (int, int, error) {
	lo, hi, found := strings.Cut(s, "-")
	from, err := strconv.Atoi(strings.TrimSpace(lo))
	if err != nil {
		return 0, 0, fmt.Errorf("port range %q: %w", s, err)
	}
	if !found {
		return from, from, nil
	}
	toPort, err := strconv.Atoi(strings.TrimSpace(hi))
	if err != nil {
		return 0, 0, fmt.Errorf("port range %q: %w", s, err)
	}
	return from, toPort, nil
}
