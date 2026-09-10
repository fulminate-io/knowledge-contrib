// SPDX-License-Identifier: Apache-2.0

package resolve

import (
	"fmt"
	"net"
	"slices"
	"strings"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// MethodDNSResolve is the method discriminator on a DNS routing edge.
const MethodDNSResolve = "gcp-dns-resolve"

// DNSRecordTargets derives the routing edge from an address record to the
// resource that answers on that address.
//
// THIS IS NOT A RE-TARGET, and the distinction decides the design. The DNS
// enumeration deliberately emits NO routing edge of its own; every one is
// produced here, against a TYPED address index, so an edge naming a bare address
// never enters the graph and is never rewritten later. An edge whose endpoint is
// a string that is not a node id is a dangling edge, and a dangling edge that
// exists for a while is one a reader can see.
//
// ONE ADDRESS MAY BE HELD BY SEVERAL RESOURCES — a reserved address behind both
// a regional and a global forwarding rule, an address mid-transition — so every
// match yields an edge rather than one being chosen arbitrarily.
func DNSRecordTargets(in gcpgraph.Result) (gcpgraph.Result, error) {
	index, err := addressIndex(in.Resources)
	if err != nil {
		return gcpgraph.Result{}, err
	}

	var out gcpgraph.Result
	for _, res := range in.Resources {
		if res.ResourceType != gcpgraph.ResourceTypeDNSRecordSet {
			continue
		}
		var spec gcpcontent.RecordSet
		present, err := gcpcontent.Unmarshal(res.Content, &spec)
		if err != nil {
			return gcpgraph.Result{}, fmt.Errorf(
				"gcp dns resolver: reading record set %q (id=%q): %w", res.Name, res.ID, err)
		}
		if !present {
			continue
		}
		// Only address records carry something this index can answer. A
		// hostname-valued record would need a name index, which nothing in this
		// walk builds; it yields nothing rather than a guess.
		if spec.Type != "A" && spec.Type != "AAAA" {
			continue
		}
		for _, rdata := range spec.Rrdatas {
			address := strings.TrimSuffix(rdata, ".")
			for _, target := range index[address] {
				out.Relations = append(out.Relations, gcpgraph.Relation{
					From: res.ID, To: target, Type: gcpgraph.EdgeRoutesTo,
					Method:   MethodDNSResolve,
					Metadata: map[string]string{"address": address},
				})
			}
		}
	}
	return out, nil
}

// addressIndex maps a reachable address onto every resource holding it. It is a
// MULTI-map on purpose; see the note on [DNSRecordTargets].
func addressIndex(resources []gcpgraph.Resource) (map[string][]string, error) {
	index := map[string][]string{}
	add := func(address, nodeID string) {
		// A value that is not an address is not indexed, so a record whose data
		// is garbage matches nothing rather than matching by string equality.
		if net.ParseIP(address) == nil {
			return
		}
		if slices.Contains(index[address], nodeID) {
			return
		}
		index[address] = append(index[address], nodeID)
	}

	for _, res := range resources {
		addresses, err := addressesOf(res)
		if err != nil {
			return nil, err
		}
		for _, address := range addresses {
			add(address, res.ID)
		}
	}
	return index, nil
}

// addressesOf returns the reachable addresses one resource holds, and nothing
// for a resource kind that holds none.
//
// ONLY THREE KINDS ANSWER, and each answers from its own content shape. A kind
// this collector emits but does not list here contributes no address, which is a
// deliberate silence rather than a gap: a record resolving onto it would be a
// claim this walk cannot support.
func addressesOf(res gcpgraph.Resource) ([]string, error) {
	switch res.ResourceType {
	case gcpgraph.ResourceTypeInstance:
		var spec gcpcontent.Instance
		present, err := gcpcontent.Unmarshal(res.Content, &spec)
		if err != nil {
			return nil, fmt.Errorf(
				"gcp dns resolver: reading instance %q (id=%q): %w", res.Name, res.ID, err)
		}
		if !present {
			return nil, nil
		}
		var out []string
		for _, nic := range spec.NetworkInterfaces {
			// The EXTERNAL address only. A private address is not what a
			// published record resolves to, and indexing it would attach a public
			// record to whatever happens to hold that private address.
			out = append(out, nic.ExternalIP)
		}
		return out, nil

	case gcpgraph.ResourceTypeSQLInstance:
		var spec gcpcontent.SQLInstance
		present, err := gcpcontent.Unmarshal(res.Content, &spec)
		if err != nil {
			return nil, fmt.Errorf(
				"gcp dns resolver: reading sql instance %q (id=%q): %w", res.Name, res.ID, err)
		}
		if !present {
			return nil, nil
		}
		return spec.IPAddresses, nil

	case gcpgraph.ResourceTypeForwardingRule:
		var spec gcpcontent.ForwardingRule
		present, err := gcpcontent.Unmarshal(res.Content, &spec)
		if err != nil {
			return nil, fmt.Errorf(
				"gcp dns resolver: reading forwarding rule %q (id=%q): %w", res.Name, res.ID, err)
		}
		if !present {
			return nil, nil
		}
		return []string{spec.IPAddress}, nil

	default:
		return nil, nil
	}
}
