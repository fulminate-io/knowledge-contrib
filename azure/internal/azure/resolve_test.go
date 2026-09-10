// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"strings"
	"testing"
)

// resolve_test.go — the five in-walk resolvers, one row per branch and one for
// every gate that can silence a branch.
//
// FOUR RELATIONSHIPS HAVE NO OTHER EMITTER — reachability in and out, trust,
// and image lineage — so a resolver that stops firing takes an entire
// relationship type out of the graph. Two more, the re-targeted route and the
// group-terminating grant, lose no TYPE at all when they stop and lose the
// quality of the graph instead, which is the failure a count of types cannot
// see.

// resolverFixtures produces the relationships only the resolvers emit, for the
// parity census.
func resolverFixtures() []fixture {
	return []fixture{
		{name: "reachability from security rules", build: func(t *testing.T) subResult {
			nodes, edges := resolveNSGRules([]resource{fx{t}.res(nsgResource(securityGroup()))})
			return subResult{resources: nodes, edges: edges}
		}},
		{name: "container image lineage", build: func(t *testing.T) subResult {
			edges := resolveImageLineage(
				[]resource{fx{t}.res(siteResource(webSite(), false))},
				nil,
				[]resource{fx{t}.res(registryResource(containerRegistry()))},
			)
			return subResult{edges: edges}
		}},
		{name: "cross-tenant trust", build: func(t *testing.T) subResult {
			edges := resolveCrossTenantTrust(
				[]resource{fx{t}.res(identityResource(managedIdentity()))},
				trustInputEdges(),
			)
			return subResult{edges: edges}
		}},
	}
}

// trustInputEdges is what a walk produces for resolver 4: a federated
// credential from an issuer outside the identity's tenant, and a role
// assignment whose principal is a guest.
func trustInputEdges() []edge {
	return []edge{
		{
			from: "oidc:" + foreignIssuer + "/workload-one", to: identityA,
			relation: edgeWorkloadIdentity,
			metadata: map[string]string{mdIssuer: foreignIssuer, mdSubject: "workload-one"},
		},
		{
			from: identityA, to: scopeID, relation: edgeAssumesRole,
			metadata: map[string]string{mdSource: "rbac", mdPrincipalType: "Guest"},
		},
	}
}

// TestResolveNSGRules_OnlyAllowRulesAndOnlyTheirPeerSide, with each of the
// three drops through the path it actually takes.
func TestResolveNSGRules_OnlyAllowRulesAndOnlyTheirPeerSide(t *testing.T) {
	nsg := fx{t}.res(nsgResource(securityGroup()))
	nodes, edges := resolveNSGRules([]resource{nsg})

	// The Allow inbound rule reaches its SOURCE range.
	in, ok := edgeBetween(edges, nsgID, cidrSentinelID("203.0.113.0/24"), edgeAllowsIngressFrom)
	if !ok {
		t.Fatalf("an inbound Allow rule drew no reachability edge: %v", edges)
	}
	if in.metadata["protocol"] != "tcp" || in.metadata["port_from"] != "443" {
		t.Errorf("the reachability edge does not carry its protocol and port: %v", in.metadata)
	}

	// The Allow OUTBOUND rule reaches its DESTINATION range, and Azure's
	// wildcard is normalized to the range it means.
	if _, ok := edgeBetween(edges, nsgID, cidrSentinelID(anyAddress), edgeAllowsEgressTo); !ok {
		t.Errorf("an outbound Allow rule drew no reachability edge: %v", edges)
	}

	// THE THREE DROPS. A Deny rule describes what cannot reach the group; an
	// edge for it would invert every query over these edges.
	for _, e := range edges {
		if e.metadata["rule"] == "deny-all-in" {
			t.Error("a Deny rule drew a reachability edge")
		}
		if e.metadata["rule"] == "allow-asg-only" {
			t.Error("a rule naming only application security groups drew a reachability edge")
		}
	}

	// EVERY endpoint is an address range, never a resource: pointing one at a
	// machine would assert reachability the source data does not support.
	for _, e := range edges {
		if !strings.HasPrefix(e.to, "azure:cidr:") {
			t.Errorf("a reachability edge terminates on %q rather than an address range", e.to)
		}
		if e.from != nsgID {
			t.Errorf("a reachability edge starts at %q rather than the security group", e.from)
		}
	}

	// The ranges become nodes, once each however many rules name them.
	if len(nodes) != 2 {
		t.Errorf("expected two address-range nodes, got %d: %v", len(nodes), nodes)
	}
	for _, n := range nodes {
		if n.resourceType != rtCIDRBlock {
			t.Errorf("an address-range node has type %q", n.resourceType)
		}
		if n.metadata[metaCollected] != "false" {
			t.Error("an address-range node does not record that it is not an Azure resource")
		}
	}
}

// TestResolveNSGRules_ADirectionProducesExactlyOneRelationship. A rule governs
// one direction, and reading both address fields of every rule would double
// every reachability fact.
func TestResolveNSGRules_ADirectionProducesExactlyOneRelationship(t *testing.T) {
	nsg := fx{t}.res(nsgResource(securityGroup()))
	_, edges := resolveNSGRules([]resource{nsg})
	counts := relationsOf(edges)
	if counts[edgeAllowsIngressFrom] != 1 {
		t.Errorf("expected one inbound reachability edge, got %d", counts[edgeAllowsIngressFrom])
	}
	if counts[edgeAllowsEgressTo] != 1 {
		t.Errorf("expected one outbound reachability edge, got %d", counts[edgeAllowsEgressTo])
	}
}

// TestResolveImageLineage_MatchesOnTheRegistryHost, with two negatives: an
// image from another registry, and a site running no container at all.
func TestResolveImageLineage_MatchesOnTheRegistryHost(t *testing.T) {
	site := fx{t}.res(siteResource(webSite(), false))
	registry := fx{t}.res(registryResource(containerRegistry()))

	edges := resolveImageLineage([]resource{site}, nil, []resource{registry})
	e, ok := edgeBetween(edges, webSiteID, registryID, edgeUsesImage)
	if !ok {
		t.Fatalf("a site running an image from a walked registry drew no lineage edge: %v", edges)
	}
	if e.metadata["image"] != containerRef {
		t.Errorf("the lineage edge does not carry the image reference: %v", e.metadata)
	}

	// A DIFFERENT registry: same shape, no match.
	elsewhere := webSite()
	elsewhere.Properties.SiteConfig.LinuxFxVersion = new("DOCKER|other.azurecr.io/service:1")
	other := fx{t}.res(siteResource(elsewhere, false))
	if got := resolveImageLineage([]resource{other}, nil, []resource{registry}); len(got) != 0 {
		t.Errorf("a site running another registry's image drew %d lineage edges", len(got))
	}

	// A site running code: nothing to match.
	code := webSite()
	code.Properties.SiteConfig.LinuxFxVersion = new("PYTHON|3.12")
	plain := fx{t}.res(siteResource(code, false))
	if got := resolveImageLineage([]resource{plain}, nil, []resource{registry}); len(got) != 0 {
		t.Errorf("a site running a runtime stack drew %d lineage edges", len(got))
	}

	// And with NO registries walked, the index is empty and nothing resolves —
	// which is the arm that would silently produce nothing if the walk fact
	// were dropped.
	if got := resolveImageLineage([]resource{site}, nil, nil); len(got) != 0 {
		t.Errorf("with no registry in the walk, %d lineage edges were drawn", len(got))
	}
}

// TestResolveImageLineage_ReadsFunctionAppsToo. Function apps run containers on
// the same field, and a resolver reading only web apps would lose every one.
func TestResolveImageLineage_ReadsFunctionAppsToo(t *testing.T) {
	fn := fx{t}.res(siteResource(functionApp(), true))
	registry := fx{t}.res(registryResource(containerRegistry()))
	edges := resolveImageLineage(nil, []resource{fn}, []resource{registry})
	if _, ok := edgeBetween(edges, functionAppID, registryID, edgeUsesImage); !ok {
		t.Errorf("a function app running a walked registry's image drew no lineage edge: %v", edges)
	}
}

// TestResolveImageLineage_ResolvesASovereignCloudRegistry. A registry host is
// azurecr.io in the public cloud and azurecr.cn or azurecr.us in the sovereign
// ones, so the match is on the index this walk built rather than on a known
// suffix; a suffix test would refuse every sovereign-cloud subscription while
// looking, in a public-cloud test, exactly like a correct one.
func TestResolveImageLineage_ResolvesASovereignCloudRegistry(t *testing.T) {
	sovereign := containerRegistry()
	sovereign.Properties.LoginServer = new("reg1.azurecr.cn")
	registry := fx{t}.res(registryResource(sovereign))

	site := webSite()
	site.Properties.SiteConfig.LinuxFxVersion = new("DOCKER|reg1.azurecr.cn/service:1.2.3")
	sovereignSite := fx{t}.res(siteResource(site, false))

	edges := resolveImageLineage([]resource{sovereignSite}, nil, []resource{registry})
	if _, ok := edgeBetween(edges, webSiteID, registryID, edgeUsesImage); !ok {
		t.Errorf("a site running a sovereign-cloud registry's image drew no lineage edge: %v", edges)
	}

	// The discriminator is still the index: an image from a host this walk
	// never saw draws nothing, whatever its suffix.
	stranger := webSite()
	stranger.Properties.SiteConfig.LinuxFxVersion = new("DOCKER|docker.io/library/nginx:1")
	unknown := fx{t}.res(siteResource(stranger, false))
	if got := resolveImageLineage([]resource{unknown}, nil, []resource{registry}); len(got) != 0 {
		t.Errorf("an image from a registry this walk never saw drew %d lineage edges", len(got))
	}

	// And a reference with no host at all names no registry.
	if got := imageRegistryHost("nginx:latest"); got != "" {
		t.Errorf("a hostless image reference named the registry %q", got)
	}
}
