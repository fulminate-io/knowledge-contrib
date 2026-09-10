// SPDX-License-Identifier: Apache-2.0

package resolve_test

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/resolve"
)

// dns_test.go — DNS record targeting.
//
// THIS RESOLVER IS NOT A RE-TARGET. The enumeration deliberately emits no
// routing edge at all; every one is produced HERE against a typed address index,
// so an edge naming a bare address never enters the graph in the first place.
// Both halves are asserted: the resolved edges exist, and no edge points at an
// address.

const (
	recordID = "projects/proj-a/managedZones/z/rrsets/a.example.com./A"
	sqlID    = "projects/proj-a/instances/db"
	frID     = "https://www.googleapis.com/compute/v1/projects/proj-a/regions/r/forwardingRules/fr"
)

// recordSet takes its data as an EXPLICIT SLICE rather than a variadic tail.
// The variadic form let a call that passed one argument too many compile
// silently, with the extra value landing in the record TYPE and the case going
// green against a record that resolved nothing.
func recordSet(t *testing.T, recType string, rrdatas []string) gcpgraph.Resource {
	t.Helper()
	return gcpgraph.Resource{
		ID: recordID, Name: "a.example.com.", ResourceType: gcpgraph.ResourceTypeDNSRecordSet,
		Content: mustMarshal(t, gcpcontent.RecordSet{
			Name: "a.example.com.", Type: recType, Rrdatas: rrdatas,
		}),
	}
}

// One address may be shared by several resources — a reserved address behind
// both a regional and a global forwarding rule, an address in transition — so
// every match yields an edge rather than one being picked arbitrarily.
func TestDNSRecordTargetsResolvesEveryResourceHoldingTheAddress(t *testing.T) {
	got, err := resolve.DNSRecordTargets(gcpgraph.Result{Resources: []gcpgraph.Resource{
		instanceResource(t, vmWeb, "web", gcpcontent.Instance{
			Name:              "web",
			NetworkInterfaces: []gcpcontent.NetworkInterface{{ExternalIP: "34.1.2.3"}},
		}),
		{ID: sqlID, Name: "db", ResourceType: gcpgraph.ResourceTypeSQLInstance,
			Content: mustMarshal(t, gcpcontent.SQLInstance{Name: "db", IPAddresses: []string{"34.1.2.3"}})},
		{ID: frID, Name: "fr", ResourceType: gcpgraph.ResourceTypeForwardingRule,
			Content: mustMarshal(t, gcpcontent.ForwardingRule{Name: "fr", IPAddress: "34.1.2.3"})},
		recordSet(t, "A", []string{"34.1.2.3"}),
	}})
	if err != nil {
		t.Fatalf("DNSRecordTargets: %v", err)
	}
	for _, target := range []string{vmWeb, sqlID, frID} {
		if !hasEdge(got.Relations, recordID, target, gcpgraph.EdgeRoutesTo) {
			t.Errorf("no ROUTES_TO from the record to %q: %v", target, edgeKeys(got.Relations))
		}
	}
	if len(got.Relations) != 3 {
		t.Errorf("got %d edges, want 3: %v", len(got.Relations), edgeKeys(got.Relations))
	}
	for _, r := range got.Relations {
		if r.To == "34.1.2.3" {
			t.Errorf("an edge points at a bare address rather than a resource: %+v", r)
		}
		if r.Method != resolve.MethodDNSResolve {
			t.Errorf("edge method: got %q, want %q", r.Method, resolve.MethodDNSResolve)
		}
	}
}

// The cell that closes the RECORD-TYPE axis: a hostname-valued record has no
// address to resolve and yields nothing.
func TestDNSRecordTargetsIgnoresAHostnameValuedRecord(t *testing.T) {
	got, err := resolve.DNSRecordTargets(gcpgraph.Result{Resources: []gcpgraph.Resource{
		instanceResource(t, vmWeb, "web", gcpcontent.Instance{
			NetworkInterfaces: []gcpcontent.NetworkInterface{{ExternalIP: "34.1.2.3"}},
		}),
		recordSet(t, "CNAME", []string{"other.example.com."}),
	}})
	if err != nil {
		t.Fatalf("DNSRecordTargets: %v", err)
	}
	if len(got.Relations) != 0 {
		t.Errorf("a CNAME produced %d edges, want 0: %v", len(got.Relations), edgeKeys(got.Relations))
	}
}

// The record-type filter needs a case where the filter is the ONLY thing
// stopping an edge. A hostname-valued record has nothing in the index to match,
// so removing the filter changes nothing and the CNAME case above proves the
// filter is present rather than that it decides anything. A TXT record whose
// data happens to BE an address a resource holds is the cell that separates
// them: the index would answer, and only the type filter declines.
func TestDNSRecordTargetsIgnoresANonAddressRecordWhoseDataIsAnAddress(t *testing.T) {
	got, err := resolve.DNSRecordTargets(gcpgraph.Result{Resources: []gcpgraph.Resource{
		instanceResource(t, vmWeb, "web", gcpcontent.Instance{
			NetworkInterfaces: []gcpcontent.NetworkInterface{{ExternalIP: "34.1.2.3"}},
		}),
		recordSet(t, "TXT", []string{"34.1.2.3"}),
	}})
	if err != nil {
		t.Fatalf("DNSRecordTargets: %v", err)
	}
	if len(got.Relations) != 0 {
		t.Errorf("a TXT record whose data is an address produced %d edges, want 0: %v",
			len(got.Relations), edgeKeys(got.Relations))
	}
	// The same-run known positive: the identical data on an A record DOES
	// resolve, so the zero above is the type filter deciding and not an index
	// that could never have answered.
	control, err := resolve.DNSRecordTargets(gcpgraph.Result{Resources: []gcpgraph.Resource{
		instanceResource(t, vmWeb, "web", gcpcontent.Instance{
			NetworkInterfaces: []gcpcontent.NetworkInterface{{ExternalIP: "34.1.2.3"}},
		}),
		recordSet(t, "A", []string{"34.1.2.3"}),
	}})
	if err != nil {
		t.Fatalf("DNSRecordTargets(control): %v", err)
	}
	if !hasEdge(control.Relations, recordID, vmWeb, gcpgraph.EdgeRoutesTo) {
		t.Fatal("the control did not resolve; the zero above proves nothing")
	}
}

func TestDNSRecordTargetsHandlesAAAAAndTrailingDots(t *testing.T) {
	got, err := resolve.DNSRecordTargets(gcpgraph.Result{Resources: []gcpgraph.Resource{
		instanceResource(t, vmWeb, "web", gcpcontent.Instance{
			NetworkInterfaces: []gcpcontent.NetworkInterface{{ExternalIP: "2001:db8::1"}},
		}),
		recordSet(t, "AAAA", []string{"2001:db8::1."}),
	}})
	if err != nil {
		t.Fatalf("DNSRecordTargets: %v", err)
	}
	if !hasEdge(got.Relations, recordID, vmWeb, gcpgraph.EdgeRoutesTo) {
		t.Errorf("an AAAA record with a trailing dot did not resolve: %v", edgeKeys(got.Relations))
	}
}

// A value that is not an address is not indexed, so a record whose rdata is
// garbage matches nothing rather than matching a resource by string equality.
func TestDNSRecordTargetsIgnoresANonAddressValue(t *testing.T) {
	got, err := resolve.DNSRecordTargets(gcpgraph.Result{Resources: []gcpgraph.Resource{
		instanceResource(t, vmWeb, "web", gcpcontent.Instance{
			NetworkInterfaces: []gcpcontent.NetworkInterface{{ExternalIP: "not-an-address"}},
		}),
		recordSet(t, "A", []string{"not-an-address"}),
	}})
	if err != nil {
		t.Fatalf("DNSRecordTargets: %v", err)
	}
	if len(got.Relations) != 0 {
		t.Errorf("a non-address value resolved to %d edges, want 0: %v",
			len(got.Relations), edgeKeys(got.Relations))
	}
}

// The INTERNAL address of an instance is not in the index: the built-in address
// index carries external addresses only, and a private address is not what a
// public record resolves to.
func TestDNSRecordTargetsDoesNotIndexInternalAddresses(t *testing.T) {
	got, err := resolve.DNSRecordTargets(gcpgraph.Result{Resources: []gcpgraph.Resource{
		instanceResource(t, vmWeb, "web", gcpcontent.Instance{
			NetworkInterfaces: []gcpcontent.NetworkInterface{{NetworkIP: "10.0.0.5"}},
		}),
		recordSet(t, "A", []string{"10.0.0.5"}),
	}})
	if err != nil {
		t.Fatalf("DNSRecordTargets: %v", err)
	}
	if len(got.Relations) != 0 {
		t.Errorf("an internal address resolved to %d edges, want 0: %v",
			len(got.Relations), edgeKeys(got.Relations))
	}
}

func TestDNSRecordTargetsFailsLoudlyOnCorruptContent(t *testing.T) {
	_, err := resolve.DNSRecordTargets(gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: recordID, Name: "a.example.com.", ResourceType: gcpgraph.ResourceTypeDNSRecordSet,
		Content: []byte("{not json"),
	}}})
	if err == nil {
		t.Fatal("a record set with undecodable content was skipped silently")
	}
}
