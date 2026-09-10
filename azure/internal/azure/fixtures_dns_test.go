// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/dns/armdns"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/privatedns/armprivatedns"
)

// fixtures_dns_test.go — hand-built DNS responses.

const (
	dnsZoneID    = rgID + "/providers/Microsoft.Network/dnsZones/example.com"
	aRecordID    = dnsZoneID + "/A/www"
	aliasRecID   = dnsZoneID + "/A/alias"
	privZoneID   = rgID + "/providers/Microsoft.Network/privateDnsZones/internal.example.com"
	frontendAddr = "10.0.1.4"
)

func dnsFixtures() []fixture {
	return []fixture{
		{name: "dns zone and record set", build: func(t *testing.T) subResult {
			zone := dnsZone()
			record := addressRecordSet()
			alias := aliasRecordSet()
			return subResult{
				resources: []resource{
					fx{t}.res(dnsZoneResource(zone)),
					fx{t}.res(dnsRecordResource(record, zone)),
					fx{t}.res(dnsRecordResource(alias, zone)),
				},
				edges: append(dnsRecordEdges(record, dnsZoneID), dnsRecordEdges(alias, dnsZoneID)...),
			}
		}},
		{name: "private dns zone", build: func(t *testing.T) subResult {
			zone := privateZone()
			return subResult{
				resources: []resource{fx{t}.res(privateDNSZoneResource(zone))},
				edges:     []edge{{from: privZoneID, to: vnetID, relation: edgeUsesNetwork}},
			}
		}},
	}
}

func dnsZone() *armdns.Zone {
	return &armdns.Zone{
		ID:       new(dnsZoneID),
		Name:     new("example.com"),
		Location: new("global"),
		Properties: &armdns.ZoneProperties{
			NumberOfRecordSets: to.Ptr[int64](4),
			ZoneType:           to.Ptr(armdns.ZoneTypePublic),
		},
	}
}

// addressRecordSet is a record naming a raw address, which resolver 2 may
// re-target.
func addressRecordSet() *armdns.RecordSet {
	return &armdns.RecordSet{
		ID:   new(aRecordID),
		Name: new("www"),
		Type: new("Microsoft.Network/dnsZones/A"),
		Properties: &armdns.RecordSetProperties{
			TTL:      to.Ptr[int64](300),
			ARecords: []*armdns.ARecord{{IPv4Address: new(frontendAddr)}},
		},
	}
}

// aliasRecordSet is a record naming a RESOURCE, which is already resolved and
// which resolver 2 must leave alone.
func aliasRecordSet() *armdns.RecordSet {
	return &armdns.RecordSet{
		ID:   new(aliasRecID),
		Name: new("alias"),
		Type: new("Microsoft.Network/dnsZones/A"),
		Properties: &armdns.RecordSetProperties{
			TTL:            to.Ptr[int64](60),
			TargetResource: &armdns.SubResource{ID: new(lbID)},
		},
	}
}

func privateZone() *armprivatedns.PrivateZone {
	return &armprivatedns.PrivateZone{
		ID:       new(privZoneID),
		Name:     new("internal.example.com"),
		Location: new("global"),
	}
}

// TestDNSRecordEdges_RawTargetsAndResourceTargets. Both shapes are real and
// they are not interchangeable: an alias record names a resource and is final,
// while an address record names an address and is a candidate for resolution.
func TestDNSRecordEdges_RawTargetsAndResourceTargets(t *testing.T) {
	raw := dnsRecordEdges(addressRecordSet(), dnsZoneID)
	if _, ok := edgeBetween(raw, dnsZoneID, aRecordID, edgeContains); !ok {
		t.Error("the zone does not contain its record set")
	}
	if _, ok := edgeBetween(raw, aRecordID, frontendAddr, edgeRoutesTo); !ok {
		t.Error("an address record drew no route to the address it names")
	}

	alias := dnsRecordEdges(aliasRecordSet(), dnsZoneID)
	if _, ok := edgeBetween(alias, aliasRecID, lbID, edgeRoutesTo); !ok {
		t.Error("an alias record drew no route to the resource it names")
	}
}

// TestDNSRecordResource_ReducesTheArmTypeToTheRecordType. An operator asking
// for the A records should not have to know the resource type they are nested
// under.
func TestDNSRecordResource_ReducesTheArmTypeToTheRecordType(t *testing.T) {
	r := fx{t}.res(dnsRecordResource(addressRecordSet(), dnsZone()))
	if got := r.metadata["recordType"]; got != "A" {
		t.Errorf("record type is %q, expected the DNS record type A", got)
	}
	if r.region != "global" {
		t.Errorf("the record did not inherit its zone's region: %q", r.region)
	}
}

// TestDNSRecordEdges_TrimsTheTrailingDot. A canonical name is fully qualified
// with a trailing dot, and a hostname a reader would search for is not.
func TestDNSRecordEdges_TrimsTheTrailingDot(t *testing.T) {
	rs := &armdns.RecordSet{
		ID:   new(dnsZoneID + "/CNAME/alias"),
		Name: new("alias"),
		Properties: &armdns.RecordSetProperties{
			CnameRecord: &armdns.CnameRecord{Cname: new("target.example.com.")},
		},
	}
	edges := dnsRecordEdges(rs, dnsZoneID)
	if _, ok := edgeBetween(edges, ptr(rs.ID), "target.example.com", edgeRoutesTo); !ok {
		t.Errorf("the canonical name kept its trailing dot: %v", edges)
	}
}
