// SPDX-License-Identifier: Apache-2.0

package resolve_test

import (
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/resolve"
)

// firewall_test.go — THE FOUR POPULATED CELLS of a DIRECTION x SOURCE-KIND axis,
// and the fifth that is structurally absent.
//
// The rule's own direction picks BOTH the arm and the edge type, so no single
// rule ever yields both. Each cell has its own case and its own fixture shape so
// a mutation that empties one index reds one cell and leaves the others green;
// a single combined fixture would red all four together and locate nothing.
//
//	(i)   INGRESS + sourceRanges          -> ALLOWS_INGRESS_FROM, instance to CIDR
//	(ii)  INGRESS + sourceTags            -> ALLOWS_INGRESS_FROM, instance to instance
//	(iii) INGRESS + sourceServiceAccounts -> ALLOWS_INGRESS_FROM, instance to instance
//	(iv)  EGRESS  + destinationRanges     -> ALLOWS_EGRESS_TO,    instance to CIDR
//	(v)   EGRESS  + tags or accounts      -> NOTHING, and that is parity

const (
	vpc    = "https://www.googleapis.com/compute/v1/projects/proj-a/global/networks/vpc"
	vmWeb  = "https://www.googleapis.com/compute/v1/projects/proj-a/zones/z/instances/web"
	vmBast = "https://www.googleapis.com/compute/v1/projects/proj-a/zones/z/instances/bastion"
)

func instanceResource(t *testing.T, id, name string, c gcpcontent.Instance) gcpgraph.Resource {
	t.Helper()
	raw, err := gcpcontent.Marshal(c)
	if err != nil {
		t.Fatalf("marshaling the instance fixture: %v", err)
	}
	return gcpgraph.Resource{
		ID: id, Name: name, ResourceType: gcpgraph.ResourceTypeInstance, Content: raw,
	}
}

func firewallResource(t *testing.T, name string, c gcpcontent.Firewall) gcpgraph.Resource {
	t.Helper()
	raw, err := gcpcontent.Marshal(c)
	if err != nil {
		t.Fatalf("marshaling the firewall fixture: %v", err)
	}
	return gcpgraph.Resource{
		ID:           "https://www.googleapis.com/compute/v1/projects/proj-a/global/firewalls/" + name,
		Name:         name,
		ResourceType: gcpgraph.ResourceTypeFirewall,
		Content:      raw,
	}
}

// webInstance carries a tag, a service account and a network, so ONE fixture
// serves every cell and each case varies only the rule.
func webInstance(t *testing.T) gcpgraph.Resource {
	t.Helper()
	return instanceResource(t, vmWeb, "web", gcpcontent.Instance{
		Name:              "web",
		Tags:              []string{"web"},
		ServiceAccounts:   []string{"web@proj-a.iam.gserviceaccount.com"},
		NetworkInterfaces: []gcpcontent.NetworkInterface{{Network: vpc}},
	})
}

func bastionInstance(t *testing.T) gcpgraph.Resource {
	t.Helper()
	return instanceResource(t, vmBast, "bastion", gcpcontent.Instance{
		Name:              "bastion",
		Tags:              []string{"bastion"},
		ServiceAccounts:   []string{"bastion@proj-a.iam.gserviceaccount.com"},
		NetworkInterfaces: []gcpcontent.NetworkInterface{{Network: vpc}},
	})
}

func allowTCP443() []gcpcontent.FirewallRule {
	return []gcpcontent.FirewallRule{{Protocol: "tcp", Ports: []string{"443"}}}
}

// Cell (i). The rule names NEITHER targetTags NOR targetServiceAccounts, so its
// target resolves through the network index — which is deliberate, and keeps
// this cell green under the mutations that empty the tag and account indexes.
func TestFirewallIngressSourceRangesYieldsCIDREdgeOnly(t *testing.T) {
	got, err := resolve.Firewall(gcpgraph.Result{Resources: []gcpgraph.Resource{
		webInstance(t),
		firewallResource(t, "allow-https", gcpcontent.Firewall{
			Name: "allow-https", Network: vpc, Direction: "INGRESS",
			SourceRanges: []string{"0.0.0.0/0"}, Allowed: allowTCP443(),
		}),
	}})
	if err != nil {
		t.Fatalf("Firewall: %v", err)
	}
	sentinel := gcpgraph.CIDRSentinelID("0.0.0.0/0")
	if !hasEdge(got.Relations, vmWeb, sentinel, gcpgraph.EdgeAllowsIngressFrom) {
		t.Fatalf("no ALLOWS_INGRESS_FROM from the instance to the CIDR sentinel: %v",
			edgeKeys(got.Relations))
	}
	if len(got.Relations) != 1 {
		t.Errorf("an ingress-with-ranges rule yielded %d edges, want exactly 1: %v",
			len(got.Relations), edgeKeys(got.Relations))
	}
	// The sentinel node is materialized in the same result as the edge that
	// names it, so the endpoint exists.
	if len(got.Resources) != 1 || got.Resources[0].ID != sentinel {
		t.Fatalf("the CIDR sentinel node was not emitted: %+v", got.Resources)
	}
	if got.Resources[0].ResourceType != gcpgraph.ResourceTypeCIDRBlock {
		t.Errorf("sentinel type: got %q, want %q",
			got.Resources[0].ResourceType, gcpgraph.ResourceTypeCIDRBlock)
	}
	if got.Resources[0].Metadata["cidr"] != "0.0.0.0/0" {
		t.Errorf("sentinel metadata cidr: got %q", got.Resources[0].Metadata["cidr"])
	}
	// The evidence carries what the rule allowed, which is what makes one edge
	// between a pair distinguishable from another.
	ev := got.Relations[0].Metadata
	if ev["protocol"] != "tcp" || ev["ports"] != "443" || ev["cidr"] != "0.0.0.0/0" {
		t.Errorf("edge evidence: got %v", ev)
	}
	if _, ok := ev["egress"]; ok {
		t.Errorf("an ingress edge carries an egress marker: %v", ev)
	}
	if got.Relations[0].Method != "gcp-firewall-rule" {
		t.Errorf("edge method: got %q, want gcp-firewall-rule", got.Relations[0].Method)
	}
}

// Cell (ii): the tag index.
func TestFirewallIngressSourceTagsYieldsInstanceToInstance(t *testing.T) {
	got, err := resolve.Firewall(gcpgraph.Result{Resources: []gcpgraph.Resource{
		webInstance(t), bastionInstance(t),
		firewallResource(t, "allow-bastion", gcpcontent.Firewall{
			Name: "allow-bastion", Network: vpc, Direction: "INGRESS",
			TargetTags: []string{"web"}, SourceTags: []string{"bastion"},
			Allowed: allowTCP443(),
		}),
	}})
	if err != nil {
		t.Fatalf("Firewall: %v", err)
	}
	if !hasEdge(got.Relations, vmWeb, vmBast, gcpgraph.EdgeAllowsIngressFrom) {
		t.Fatalf("no instance-to-instance ALLOWS_INGRESS_FROM through the tag index: %v",
			edgeKeys(got.Relations))
	}
	if len(got.Relations) != 1 {
		t.Errorf("got %d edges, want 1: %v", len(got.Relations), edgeKeys(got.Relations))
	}
	for _, r := range got.Relations {
		if r.Type == gcpgraph.EdgeAllowsEgressTo {
			t.Error("an INGRESS rule produced an ALLOWS_EGRESS_TO edge")
		}
	}
}

// Cell (iii): the service-account index. A separate case from (ii) because it is
// a separate index, and a mutation that empties one must red only one.
func TestFirewallIngressSourceServiceAccountsYieldsInstanceToInstance(t *testing.T) {
	got, err := resolve.Firewall(gcpgraph.Result{Resources: []gcpgraph.Resource{
		webInstance(t), bastionInstance(t),
		firewallResource(t, "allow-bastion-sa", gcpcontent.Firewall{
			Name: "allow-bastion-sa", Network: vpc, Direction: "INGRESS",
			TargetServiceAccounts: []string{"web@proj-a.iam.gserviceaccount.com"},
			SourceServiceAccounts: []string{"bastion@proj-a.iam.gserviceaccount.com"},
			Allowed:               allowTCP443(),
		}),
	}})
	if err != nil {
		t.Fatalf("Firewall: %v", err)
	}
	if !hasEdge(got.Relations, vmWeb, vmBast, gcpgraph.EdgeAllowsIngressFrom) {
		t.Fatalf("no instance-to-instance ALLOWS_INGRESS_FROM through the account index: %v",
			edgeKeys(got.Relations))
	}
	if len(got.Relations) != 1 {
		t.Errorf("got %d edges, want 1: %v", len(got.Relations), edgeKeys(got.Relations))
	}
}

// Cell (iv): the ONLY cell that observes ALLOWS_EGRESS_TO at all.
func TestFirewallEgressDestinationRangesYieldsEgressEdgeOnly(t *testing.T) {
	got, err := resolve.Firewall(gcpgraph.Result{Resources: []gcpgraph.Resource{
		webInstance(t),
		firewallResource(t, "allow-out", gcpcontent.Firewall{
			Name: "allow-out", Network: vpc, Direction: "EGRESS",
			DestinationRanges: []string{"10.0.0.0/8"}, Allowed: allowTCP443(),
		}),
	}})
	if err != nil {
		t.Fatalf("Firewall: %v", err)
	}
	sentinel := gcpgraph.CIDRSentinelID("10.0.0.0/8")
	if !hasEdge(got.Relations, vmWeb, sentinel, gcpgraph.EdgeAllowsEgressTo) {
		t.Fatalf("no ALLOWS_EGRESS_TO from the instance to the CIDR sentinel: %v",
			edgeKeys(got.Relations))
	}
	if len(got.Relations) != 1 {
		t.Errorf("got %d edges, want 1: %v", len(got.Relations), edgeKeys(got.Relations))
	}
	if got.Relations[0].Metadata["egress"] != "true" {
		t.Errorf("an egress edge does not carry the egress marker: %v", got.Relations[0].Metadata)
	}
}

// Cell (v): the closing negative. An EGRESS rule naming source tags or accounts
// yields NO instance-to-instance edge, because the egress arm never consults the
// source index at all. Stated as its own case so its absence reads as parity
// rather than as something this module forgot.
func TestFirewallEgressWithSourceTagsYieldsNoInstanceToInstanceEdge(t *testing.T) {
	got, err := resolve.Firewall(gcpgraph.Result{Resources: []gcpgraph.Resource{
		webInstance(t), bastionInstance(t),
		firewallResource(t, "egress-with-sources", gcpcontent.Firewall{
			Name: "egress-with-sources", Network: vpc, Direction: "EGRESS",
			TargetTags: []string{"web"},
			SourceTags: []string{"bastion"},
			// No destinationRanges: the egress arm has nothing else to emit.
			Allowed: allowTCP443(),
		}),
	}})
	if err != nil {
		t.Fatalf("Firewall: %v", err)
	}
	if len(got.Relations) != 0 {
		t.Errorf("an EGRESS rule carrying source tags produced %d edges, want 0: %v",
			len(got.Relations), edgeKeys(got.Relations))
	}
}

func TestFirewallSkipsDisabledAndDenyOnlyRules(t *testing.T) {
	for _, tc := range []struct {
		name string
		fw   gcpcontent.Firewall
	}{
		{"disabled", gcpcontent.Firewall{
			Name: "off", Network: vpc, Direction: "INGRESS", Disabled: true,
			SourceRanges: []string{"0.0.0.0/0"}, Allowed: allowTCP443(),
		}},
		{"deny only", gcpcontent.Firewall{
			Name: "deny", Network: vpc, Direction: "INGRESS",
			SourceRanges: []string{"0.0.0.0/0"},
			Denied:       []gcpcontent.FirewallRule{{Protocol: "tcp"}},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolve.Firewall(gcpgraph.Result{Resources: []gcpgraph.Resource{
				webInstance(t), firewallResource(t, tc.fw.Name, tc.fw),
			}})
			if err != nil {
				t.Fatalf("Firewall: %v", err)
			}
			if len(got.Relations) != 0 {
				t.Errorf("a %s rule produced %d edges, want 0", tc.name, len(got.Relations))
			}
		})
	}
}

// TestFirewallNeverEmitsASelfEdge pins the guard that keeps an instance matching
// both the target and the source set from pointing at itself.
func TestFirewallNeverEmitsASelfEdge(t *testing.T) {
	got, err := resolve.Firewall(gcpgraph.Result{Resources: []gcpgraph.Resource{
		webInstance(t),
		firewallResource(t, "self", gcpcontent.Firewall{
			Name: "self", Network: vpc, Direction: "INGRESS",
			TargetTags: []string{"web"}, SourceTags: []string{"web"},
			Allowed: allowTCP443(),
		}),
	}})
	if err != nil {
		t.Fatalf("Firewall: %v", err)
	}
	for _, r := range got.Relations {
		if r.From == r.To {
			t.Errorf("a rule whose target and source both match one instance emitted a self edge: %+v", r)
		}
	}
	if len(got.Relations) != 0 {
		t.Errorf("got %d edges, want 0: %v", len(got.Relations), edgeKeys(got.Relations))
	}
}

// TestFirewallFailsLoudlyOnCorruptContent is the fail-loud arm. The internal
// provider hook logs a decode failure and returns nil; a collector module does
// not inherit that posture, because a resolver that cannot read its input has
// produced an incomplete graph and the caller must be able to say so.
func TestFirewallFailsLoudlyOnCorruptContent(t *testing.T) {
	_, err := resolve.Firewall(gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID:           "https://www.googleapis.com/compute/v1/projects/proj-a/global/firewalls/broken",
		Name:         "broken",
		ResourceType: gcpgraph.ResourceTypeFirewall,
		Content:      []byte("{not json"),
	}}})
	if err == nil {
		t.Fatal("a firewall rule with undecodable content was skipped silently")
	}
	if !strings.Contains(err.Error(), "broken") {
		t.Errorf("error %q does not name the rule it could not read", err)
	}
}

// TestFirewallOnAnEmptyWalkIsEmpty is the same-run control for every zero above:
// with no firewalls at all the resolver returns nothing and no error, so a zero
// in the cases above is a measured zero rather than a resolver that never ran.
func TestFirewallOnAnEmptyWalkIsEmpty(t *testing.T) {
	got, err := resolve.Firewall(gcpgraph.Result{})
	if err != nil {
		t.Fatalf("Firewall(empty): %v", err)
	}
	if len(got.Relations) != 0 || len(got.Resources) != 0 {
		t.Errorf("an empty walk derived %d relations and %d resources",
			len(got.Relations), len(got.Resources))
	}
}
