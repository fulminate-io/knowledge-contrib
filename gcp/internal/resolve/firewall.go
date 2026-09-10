// SPDX-License-Identifier: Apache-2.0

// Package resolve derives the parts of this collector's graph that no
// enumeration produces: reachability between instances, Shared VPC linkage,
// container image lineage, cross-project trust, DNS record targets, and the
// resolution of IAM group placeholders.
//
// WHY THEY LIVE IN THE WALK. In the built-in collector these six run as a
// post-collect hook against the written graph, re-reading it over the wire. A
// custom collector has no such hook — the contract gives a provider one call
// that returns one envelope — so a module that shipped the vocabulary without
// them would DECLARE five edge types and derive them never. Running them here,
// over the walk's own in-memory result, is what makes the produced graph the
// same graph.
//
// AND THEY FAIL LOUD. The built-in hook wraps every resolver in a warning and
// returns nil, so a resolver that could not read its input leaves a quietly
// smaller graph. This package returns the error instead: the caller turns it
// into an incomplete walk, which is what stops the server treating the
// unresolved half as deleted.
package resolve

import (
	"fmt"
	"maps"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// MethodFirewall is the method discriminator on every reachability edge, so a
// consumer can tell a derived edge from an enumerated one.
const MethodFirewall = "gcp-firewall-rule"

// directionEgress is the API's spelling for an egress rule. Anything else,
// including an absent direction, is ingress — which is the API's own default.
const directionEgress = "EGRESS"

// instanceRef is one indexed instance: its node id and the networks it is on.
type instanceRef struct {
	id       string
	networks []string
}

// instanceIndex answers the three questions a firewall rule asks about
// instances. All three are built in one pass and every one of them is consulted
// by BOTH target selection and source selection, which is why a fixture that
// exercises one must not depend on another.
type instanceIndex struct {
	byTag     map[string][]instanceRef
	bySA      map[string][]instanceRef
	byNetwork map[string][]instanceRef
}

// Firewall derives instance reachability from the walk's firewall rules and
// instances, and returns ONLY what it derived: the edges and the CIDR sentinel
// nodes those edges name.
//
// THE RULE'S DIRECTION PICKS BOTH THE ARM AND THE EDGE TYPE, so one rule never
// yields both types. Ingress has three source kinds — ranges, tags and service
// accounts — and egress has one, ranges; the egress arm consults no source index
// at all, so an egress rule naming tags contributes nothing. That is the
// source-of-truth behavior and not an omission here.
func Firewall(in gcpgraph.Result) (gcpgraph.Result, error) {
	idx, err := buildInstanceIndex(in.Resources)
	if err != nil {
		return gcpgraph.Result{}, err
	}

	var out gcpgraph.Result
	cidrs := map[string]string{}
	for _, res := range in.Resources {
		if res.ResourceType != gcpgraph.ResourceTypeFirewall {
			continue
		}
		var spec gcpcontent.Firewall
		present, err := gcpcontent.Unmarshal(res.Content, &spec)
		if err != nil {
			return gcpgraph.Result{}, fmt.Errorf(
				"gcp firewall resolver: reading rule %q (id=%q): %w", res.Name, res.ID, err)
		}
		if !present {
			continue
		}
		if spec.Disabled {
			continue
		}
		// A deny-only rule creates no connectivity, so it contributes no
		// reachability edge. It is not skipped as an error: it is a rule that
		// says what may NOT happen, and this resolver models what may.
		if len(spec.Allowed) == 0 {
			continue
		}
		out.Relations = append(out.Relations, edgesForRule(spec, idx, cidrs)...)
	}

	for id, cidr := range cidrs {
		out.Resources = append(out.Resources, gcpgraph.Resource{
			ID:           id,
			Name:         cidr,
			ResourceType: gcpgraph.ResourceTypeCIDRBlock,
			Summary:      gcpgraph.ResourceTypeCIDRBlock + " " + cidr,
			Metadata:     map[string]string{"cidr": cidr},
		})
	}
	return out, nil
}

// edgesForRule emits one rule's edges. The CIDR sentinels it names are recorded
// in cidrs so they are materialized exactly once, in the same result.
func edgesForRule(spec gcpcontent.Firewall, idx instanceIndex, cidrs map[string]string) []gcpgraph.Relation {
	egress := spec.Direction == directionEgress
	targets := resolveTargets(spec, idx)
	if len(targets) == 0 {
		return nil
	}
	base := ruleEvidence(spec, egress)

	if egress {
		return rangeEdges(targets, spec.DestinationRanges, base, cidrs, gcpgraph.EdgeAllowsEgressTo)
	}

	out := rangeEdges(targets, spec.SourceRanges, base, cidrs, gcpgraph.EdgeAllowsIngressFrom)
	for _, t := range targets {
		for _, s := range resolveSources(spec, idx) {
			// An instance matching both the target set and the source set would
			// otherwise point at itself, which asserts nothing.
			if t.id == s.id {
				continue
			}
			out = append(out, gcpgraph.Relation{
				From: t.id, To: s.id, Type: gcpgraph.EdgeAllowsIngressFrom,
				Method: MethodFirewall, Metadata: copyEvidence(base),
			})
		}
	}
	return out
}

// rangeEdges emits one edge per (target, CIDR) pair and records each CIDR so its
// sentinel node is materialized alongside.
func rangeEdges(
	targets []instanceRef, ranges []string, base map[string]string,
	cidrs map[string]string, edgeType string,
) []gcpgraph.Relation {
	var out []gcpgraph.Relation
	for _, cidr := range ranges {
		sentinel := gcpgraph.CIDRSentinelID(cidr)
		cidrs[sentinel] = cidr
		for _, t := range targets {
			evidence := copyEvidence(base)
			evidence["cidr"] = cidr
			out = append(out, gcpgraph.Relation{
				From: t.id, To: sentinel, Type: edgeType,
				Method: MethodFirewall, Metadata: evidence,
			})
		}
	}
	return out
}

// ruleEvidence is what one rule's edges carry, so two edges between the same
// pair of instances stay distinguishable by what they allow.
func ruleEvidence(spec gcpcontent.Firewall, egress bool) map[string]string {
	evidence := map[string]string{}
	if egress {
		evidence["egress"] = "true"
	}
	if len(spec.Allowed) > 0 {
		if p := spec.Allowed[0].Protocol; p != "" {
			evidence["protocol"] = p
		}
		if ports := spec.Allowed[0].Ports; len(ports) > 0 {
			evidence["ports"] = ports[0]
		}
	}
	return evidence
}

func copyEvidence(in map[string]string) map[string]string {
	out := make(map[string]string, len(in)+1)
	maps.Copy(out, in)
	return out
}

// buildInstanceIndex indexes every instance in the walk by tag, by service
// account and by network, in one pass over the instance content.
func buildInstanceIndex(resources []gcpgraph.Resource) (instanceIndex, error) {
	idx := instanceIndex{
		byTag:     map[string][]instanceRef{},
		bySA:      map[string][]instanceRef{},
		byNetwork: map[string][]instanceRef{},
	}
	for _, res := range resources {
		if res.ResourceType != gcpgraph.ResourceTypeInstance {
			continue
		}
		var spec gcpcontent.Instance
		present, err := gcpcontent.Unmarshal(res.Content, &spec)
		if err != nil {
			return instanceIndex{}, fmt.Errorf(
				"gcp firewall resolver: reading instance %q (id=%q): %w", res.Name, res.ID, err)
		}
		if !present {
			continue
		}
		ref := instanceRef{id: res.ID}
		for _, nic := range spec.NetworkInterfaces {
			if nic.Network != "" {
				ref.networks = append(ref.networks, nic.Network)
			}
		}
		for _, tag := range spec.Tags {
			idx.byTag[tag] = append(idx.byTag[tag], ref)
		}
		for _, sa := range spec.ServiceAccounts {
			if sa != "" {
				idx.bySA[sa] = append(idx.bySA[sa], ref)
			}
		}
		for _, network := range ref.networks {
			idx.byNetwork[network] = append(idx.byNetwork[network], ref)
		}
	}
	return idx, nil
}

// resolveTargets finds the instances a rule applies to. A rule constraining
// NEITHER tags NOR service accounts applies to every instance in its VPC, which
// is the API's own semantics and the reason the network index exists.
func resolveTargets(spec gcpcontent.Firewall, idx instanceIndex) []instanceRef {
	var targets []instanceRef
	seen := map[string]bool{}
	add := func(refs []instanceRef) {
		for _, ref := range refs {
			if !seen[ref.id] {
				seen[ref.id] = true
				targets = append(targets, ref)
			}
		}
	}
	for _, tag := range spec.TargetTags {
		add(idx.byTag[tag])
	}
	for _, sa := range spec.TargetServiceAccounts {
		add(idx.bySA[sa])
	}
	if len(spec.TargetTags) == 0 && len(spec.TargetServiceAccounts) == 0 {
		add(idx.byNetwork[spec.Network])
	}
	return targets
}

// resolveSources finds the instances an INGRESS rule allows traffic from. It is
// reached from the ingress arm only.
func resolveSources(spec gcpcontent.Firewall, idx instanceIndex) []instanceRef {
	var sources []instanceRef
	seen := map[string]bool{}
	add := func(refs []instanceRef) {
		for _, ref := range refs {
			if !seen[ref.id] {
				seen[ref.id] = true
				sources = append(sources, ref)
			}
		}
	}
	for _, tag := range spec.SourceTags {
		add(idx.byTag[tag])
	}
	for _, sa := range spec.SourceServiceAccounts {
		add(idx.bySA[sa])
	}
	return sources
}
