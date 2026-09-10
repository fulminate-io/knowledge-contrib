// SPDX-License-Identifier: Apache-2.0

package azure

import "testing"

// resolve_dns_test.go — resolver 2, the only one that removes anything.
//
// THE DATA-LOSS ARM IS THE ROW THAT MATTERS. Rewriting a record's target is
// safe only when something answers at the address; a resolver that dropped the
// original edge for a target it could not resolve would delete the only record
// that the record set points anywhere at all. Both arms are asserted here, in
// one run, so neither can pass alone.

// TestResolveDNSRecordTargets_ResolvableTargetsAreReTargetedAndTheRawEdgeIsGone.
func TestResolveDNSRecordTargets_ResolvableTargetsAreReTargetedAndTheRawEdgeIsGone(t *testing.T) {
	records := []resource{fx{t}.res(dnsRecordResource(addressRecordSet(), dnsZone()))}
	balancers := []resource{fx{t}.res(loadBalancerResource(loadBalancer()))}
	input := dnsRecordEdges(addressRecordSet(), dnsZoneID)

	out := resolveDNSRecordTargets(records, balancers, input)

	resolved, ok := edgeBetween(out, aRecordID, lbID, edgeRoutesTo)
	if !ok {
		t.Fatalf("a record naming an address a walked balancer answers was not re-targeted: %v", out)
	}
	if resolved.metadata["resolved_from"] != frontendAddr {
		t.Errorf("the re-targeted edge does not record the address it came from: %v", resolved.metadata)
	}
	if _, stillRaw := edgeBetween(out, aRecordID, frontendAddr, edgeRoutesTo); stillRaw {
		t.Error("the raw-address edge survived beside the resolved one, so the graph carries the route twice")
	}
	// The containment edge is untouched: this resolver rewrites routes only.
	if _, ok := edgeBetween(out, dnsZoneID, aRecordID, edgeContains); !ok {
		t.Error("the zone's containment edge was lost by the route resolver")
	}
}

// TestResolveDNSRecordTargets_AnUnresolvableTargetKeepsItsRawEdge. This is the
// arm a resolver that unlinked before relinking would fail: a record pointing
// at an address outside this subscription is a real fact about the record.
func TestResolveDNSRecordTargets_AnUnresolvableTargetKeepsItsRawEdge(t *testing.T) {
	records := []resource{fx{t}.res(dnsRecordResource(addressRecordSet(), dnsZone()))}
	// A balancer answering at a DIFFERENT address, so the index is populated
	// and the miss is a miss rather than an empty index.
	elsewhere := loadBalancer()
	elsewhere.Properties.FrontendIPConfigurations[0].Properties.PrivateIPAddress = new("10.9.9.9")
	balancers := []resource{fx{t}.res(loadBalancerResource(elsewhere))}

	out := resolveDNSRecordTargets(records, balancers, dnsRecordEdges(addressRecordSet(), dnsZoneID))
	if _, ok := edgeBetween(out, aRecordID, frontendAddr, edgeRoutesTo); !ok {
		t.Errorf("an unresolvable target lost its raw-address edge, which is the only record it points anywhere: %v", out)
	}
}

// TestResolveDNSRecordTargets_AnAliasRecordIsLeftAlone. It already names a
// resource, and re-targeting it would mean matching a resource id against an
// address index.
func TestResolveDNSRecordTargets_AnAliasRecordIsLeftAlone(t *testing.T) {
	records := []resource{fx{t}.res(dnsRecordResource(aliasRecordSet(), dnsZone()))}
	balancers := []resource{fx{t}.res(loadBalancerResource(loadBalancer()))}

	out := resolveDNSRecordTargets(records, balancers, dnsRecordEdges(aliasRecordSet(), dnsZoneID))
	e, ok := edgeBetween(out, aliasRecID, lbID, edgeRoutesTo)
	if !ok {
		t.Fatalf("the alias record's route was lost: %v", out)
	}
	if e.method == methodDNSResolve {
		t.Error("an alias record's route was marked as re-targeted, though it already named a resource")
	}
}

// TestResolveDNSRecordTargets_OnlyARecordSetsRoutesAreRewritten. Another
// resource's route to the same address — an API gateway's backend, say — is not
// a DNS record and must not be rewritten by the DNS resolver.
func TestResolveDNSRecordTargets_OnlyARecordSetsRoutesAreRewritten(t *testing.T) {
	records := []resource{fx{t}.res(dnsRecordResource(addressRecordSet(), dnsZone()))}
	balancers := []resource{fx{t}.res(loadBalancerResource(loadBalancer()))}

	foreign := edge{from: apimID, to: frontendAddr, relation: edgeRoutesTo}
	out := resolveDNSRecordTargets(records, balancers, []edge{foreign})
	if _, ok := edgeBetween(out, apimID, frontendAddr, edgeRoutesTo); !ok {
		t.Errorf("a route from something that is not a DNS record was rewritten: %v", out)
	}
}

// TestResolveDNSRecordTargets_WithNoBalancersNothingIsRewritten, which is the
// arm that would silently produce nothing if the walk fact the index is built
// from were dropped.
func TestResolveDNSRecordTargets_WithNoBalancersNothingIsRewritten(t *testing.T) {
	records := []resource{fx{t}.res(dnsRecordResource(addressRecordSet(), dnsZone()))}
	input := dnsRecordEdges(addressRecordSet(), dnsZoneID)

	out := resolveDNSRecordTargets(records, nil, input)
	if len(out) != len(input) {
		t.Errorf("with no balancer in the walk the edge list changed length: %d to %d", len(input), len(out))
	}
	if _, ok := edgeBetween(out, aRecordID, frontendAddr, edgeRoutesTo); !ok {
		t.Error("with no balancer in the walk the raw-address edge was dropped")
	}
}

// TestFrontendAddressIndex_DropsWhatIsNotAnAddress. An index entry that is not
// an address can never be hit by a record's target, and one that is malformed
// would only make the index bigger.
func TestFrontendAddressIndex_DropsWhatIsNotAnAddress(t *testing.T) {
	good := fx{t}.res(loadBalancerResource(loadBalancer()))
	malformed := good
	malformed.id = lbID + "-2"
	malformed.lbFrontendIPs = []string{"not-an-address", "10.0.2.5"}

	index := frontendAddressIndex([]resource{good, malformed})
	if index[frontendAddr] != lbID {
		t.Errorf("a valid address indexed to %q", index[frontendAddr])
	}
	if _, ok := index["not-an-address"]; ok {
		t.Error("a malformed address entered the index")
	}
	if index["10.0.2.5"] != malformed.id {
		t.Error("a valid address beside a malformed one was dropped with it")
	}
}
