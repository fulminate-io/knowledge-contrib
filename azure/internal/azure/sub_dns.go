// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/privatedns/armprivatedns"
)

// sub_dns.go — public DNS zones with their record sets, and private DNS zones
// with the networks they serve.

// dnsSub walks the subscription's public DNS zones and their record sets.
type dnsSub struct{ subBase }

func (s *dnsSub) Collect(ctx context.Context) (subResult, error) {
	zones, err := armdns.NewZonesClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("dns zones client: %w", err)
	}
	records, err := armdns.NewRecordSetsClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("dns record sets client: %w", err)
	}

	var out subResult
	err = drain(ctx, zones.NewListPager(nil), func(page armdns.ZonesClientListResponse) error {
		for _, zone := range page.Value {
			if zone == nil || zone.ID == nil || zone.Name == nil {
				continue
			}
			r, err := dnsZoneResource(zone)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			if err := s.collectRecordSets(ctx, records, zone, &out); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing dns zones: %w", err)
	}
	return out, nil
}

func (s *dnsSub) collectRecordSets(
	ctx context.Context, client *armdns.RecordSetsClient, zone *armdns.Zone, out *subResult,
) error {
	rg := armResourceGroup(*zone.ID)
	if rg == "" {
		return nil
	}
	err := drain(ctx, client.NewListAllByDNSZonePager(rg, *zone.Name, nil),
		func(page armdns.RecordSetsClientListAllByDNSZoneResponse) error {
			for _, rs := range page.Value {
				if rs == nil || rs.ID == nil {
					continue
				}
				r, err := dnsRecordResource(rs, zone)
				if err != nil {
					return err
				}
				out.resources = append(out.resources, r)
				out.edges = append(out.edges, dnsRecordEdges(rs, ptr(zone.ID))...)
			}
			return nil
		})
	if err != nil {
		return fmt.Errorf("listing record sets of zone %s: %w", *zone.Name, err)
	}
	return nil
}

func dnsZoneResource(zone *armdns.Zone) (resource, error) {
	content, err := marshalContent(zone)
	if err != nil {
		return resource{}, fmt.Errorf("projecting dns zone %s: %w", ptr(zone.ID), err)
	}
	r := resource{
		id:           ptr(zone.ID),
		name:         ptr(zone.Name),
		resourceType: rtDNSZone,
		region:       ptr(zone.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	if p := zone.Properties; p != nil {
		if p.NumberOfRecordSets != nil {
			r.metadata["numberOfRecordSets"] = strconv.FormatInt(*p.NumberOfRecordSets, 10)
		}
		if p.ZoneType != nil {
			r.metadata["zoneType"] = string(*p.ZoneType)
		}
	}
	return r, nil
}

func dnsRecordResource(rs *armdns.RecordSet, zone *armdns.Zone) (resource, error) {
	content, err := marshalContent(rs)
	if err != nil {
		return resource{}, fmt.Errorf("projecting record set %s: %w", ptr(rs.ID), err)
	}
	r := resource{
		id:           ptr(rs.ID),
		name:         ptr(rs.Name),
		resourceType: rtDNSRecordSet,
		region:       ptr(zone.Location),
		content:      content,
		metadata:     map[string]string{},
	}
	// The record TYPE is the last segment of the ARM type ("Microsoft.Network/
	// dnsZones/A"), which is how an operator asks for "the A records".
	if t := ptr(rs.Type); t != "" {
		r.metadata["recordType"] = recordTypeOf(t)
	}
	if rs.Properties != nil && rs.Properties.TTL != nil {
		r.metadata["ttl"] = strconv.FormatInt(*rs.Properties.TTL, 10)
	}
	return r, nil
}

// recordTypeOf reduces an ARM record-set type to the DNS record type.
func recordTypeOf(armType string) string {
	if i := strings.LastIndex(armType, "/"); i >= 0 && i < len(armType)-1 {
		return armType[i+1:]
	}
	return armType
}

// dnsRecordEdges draws the zone's containment of the record set and what the
// record points at.
//
// AN ALIAS RECORD NAMES A RESOURCE and its edge is final. AN ADDRESS RECORD
// NAMES AN ADDRESS, and its edge is emitted with the raw address as the target;
// resolver 2 re-targets it at the resource answering there when the walk found
// one, and leaves it alone when it did not, because a record pointing outside
// this subscription is a fact worth keeping.
func dnsRecordEdges(rs *armdns.RecordSet, zoneID string) []edge {
	id := ptr(rs.ID)
	out := containsEdges(zoneID, id)
	if rs.Properties == nil {
		return out
	}
	p := rs.Properties
	if p.TargetResource != nil {
		if target := ptr(p.TargetResource.ID); target != "" {
			out = append(out, edge{from: id, to: target, relation: edgeRoutesTo})
		}
	}
	for _, a := range p.ARecords {
		if a == nil {
			continue
		}
		if v := ptr(a.IPv4Address); v != "" {
			out = append(out, edge{from: id, to: v, relation: edgeRoutesTo})
		}
	}
	for _, a := range p.AaaaRecords {
		if a == nil {
			continue
		}
		if v := ptr(a.IPv6Address); v != "" {
			out = append(out, edge{from: id, to: v, relation: edgeRoutesTo})
		}
	}
	if c := p.CnameRecord; c != nil {
		if v := ptr(c.Cname); v != "" {
			// The trailing dot of a fully qualified name is dropped so the
			// target matches the hostname a consumer would search for.
			out = append(out, edge{from: id, to: strings.TrimSuffix(v, "."), relation: edgeRoutesTo})
		}
	}
	return out
}

// privateDNSSub walks the subscription's private DNS zones and the networks
// linked to them.
type privateDNSSub struct{ subBase }

func (s *privateDNSSub) Collect(ctx context.Context) (subResult, error) {
	zones, err := armprivatedns.NewPrivateZonesClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("private dns zones client: %w", err)
	}
	links, err := armprivatedns.NewVirtualNetworkLinksClient(s.subscriptionID, s.cred, s.clientOptions())
	if err != nil {
		return subResult{}, fmt.Errorf("private dns links client: %w", err)
	}

	var out subResult
	err = drain(ctx, zones.NewListPager(nil), func(page armprivatedns.PrivateZonesClientListResponse) error {
		for _, zone := range page.Value {
			if zone == nil || zone.ID == nil || zone.Name == nil {
				continue
			}
			r, err := privateDNSZoneResource(zone)
			if err != nil {
				return err
			}
			out.resources = append(out.resources, r)
			edges, err := s.zoneLinkEdges(ctx, links, zone)
			if err != nil {
				return err
			}
			out.edges = append(out.edges, edges...)
		}
		return nil
	})
	if err != nil {
		return out, fmt.Errorf("listing private dns zones: %w", err)
	}
	return out, nil
}

func (s *privateDNSSub) zoneLinkEdges(
	ctx context.Context, client *armprivatedns.VirtualNetworkLinksClient, zone *armprivatedns.PrivateZone,
) ([]edge, error) {
	rg := armResourceGroup(*zone.ID)
	if rg == "" {
		return nil, nil
	}
	var out []edge
	err := drain(ctx, client.NewListPager(rg, *zone.Name, nil),
		func(page armprivatedns.VirtualNetworkLinksClientListResponse) error {
			for _, link := range page.Value {
				if link == nil || link.Properties == nil || link.Properties.VirtualNetwork == nil {
					continue
				}
				if vnetID := ptr(link.Properties.VirtualNetwork.ID); vnetID != "" {
					out = append(out, edge{from: ptr(zone.ID), to: vnetID, relation: edgeUsesNetwork})
				}
			}
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("listing network links of private zone %s: %w", *zone.Name, err)
	}
	return out, nil
}

// privateDNSZoneResource converts one private zone. A private zone is global
// and Azure reports its location as "global", which is carried through rather
// than rewritten: it is what Azure says.
func privateDNSZoneResource(zone *armprivatedns.PrivateZone) (resource, error) {
	return entityResource(ptr(zone.ID), ptr(zone.Name), rtPrivateDNSZone, ptr(zone.Location), zone)
}
