// SPDX-License-Identifier: Apache-2.0

package gcpcontent_test

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
)

// gcpcontent_test.go — WHAT KEYS THE STORED CONTENT ACTUALLY CARRIES.
//
// THE INSTRUMENT MATTERS HERE AND THE OBVIOUS ONE IS WORTHLESS. Marshaling a
// value of type T and unmarshaling it back into a T proves nothing about key
// spellings: writer and reader are the same struct tag, so renaming the tag
// renames both sides and the assertion still passes. That round trip was written
// first, mutated, and stayed green — it is an identity check whose subject
// supplies its own answer key.
//
// So the assertion below is against an EXTERNAL expectation: the literal key
// names, written out in the test. A tag that drifts changes the encoded document
// and reds a named case, which is the property that matters, because the stored
// content is read by consumers this module does not compile against.

// keysOf decodes the encoded form into a generic map and returns its top-level
// keys, which is what a consumer that does not import this package sees.
func keysOf(t *testing.T, v any) []string {
	t.Helper()
	raw, err := gcpcontent.Marshal(v)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var generic map[string]json.RawMessage
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("decoding the encoded content generically: %v", err)
	}
	keys := make([]string, 0, len(generic))
	for k := range generic {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

func assertKeys(t *testing.T, got, want []string) {
	t.Helper()
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("stored content is missing the key %q; it carries %v", w, got)
		}
	}
	for _, g := range got {
		if !slices.Contains(want, g) {
			t.Errorf("stored content carries the unexpected key %q; the declared set is %v", g, want)
		}
	}
}

// TestInstanceContentKeys pins every key of the stored instance content. Three
// of them are read by a resolver on the far side of the walk — tags,
// serviceAccounts and networkInterfaces — and a drift in any of them empties an
// index rather than raising an error, so each is in the declared set below.
func TestInstanceContentKeys(t *testing.T) {
	got := keysOf(t, gcpcontent.Instance{
		Name: "vm-1", SelfLink: "self", Zone: "z", MachineType: "e2", Status: "RUNNING",
		CreationTimestamp: "t", Labels: map[string]string{"env": "prod"},
		Tags: []string{"web"}, ServiceAccounts: []string{"svc@p.iam.gserviceaccount.com"},
		NetworkInterfaces: []gcpcontent.NetworkInterface{{Network: "n"}},
		Disks:             []string{"d"},
	})
	assertKeys(t, got, []string{
		"creationTimestamp", "disks", "labels", "machineType", "name",
		"networkInterfaces", "selfLink", "serviceAccounts", "status", "tags", "zone",
	})
}

func TestNetworkInterfaceContentKeys(t *testing.T) {
	got := keysOf(t, gcpcontent.NetworkInterface{
		Network: "n", Subnetwork: "s", NetworkIP: "10.0.0.1", ExternalIP: "34.1.2.3",
	})
	assertKeys(t, got, []string{"externalIP", "network", "networkIP", "subnetwork"})
}

// TestFirewallContentKeys pins the rule shape the firewall resolver dispatches
// on. Every one of these decides a branch: the direction picks the arm and the
// edge type, the three source families pick the cell, and disabled and allowed
// decide whether the rule contributes at all.
func TestFirewallContentKeys(t *testing.T) {
	got := keysOf(t, gcpcontent.Firewall{
		Name: "fw", SelfLink: "self", Network: "n", Direction: "INGRESS", Disabled: true,
		Priority: 1000, TargetTags: []string{"t"}, TargetServiceAccounts: []string{"tsa"},
		SourceRanges: []string{"0.0.0.0/0"}, SourceTags: []string{"s"},
		SourceServiceAccounts: []string{"ssa"}, DestinationRanges: []string{"10.0.0.0/8"},
		Allowed: []gcpcontent.FirewallRule{{Protocol: "tcp"}},
		Denied:  []gcpcontent.FirewallRule{{Protocol: "udp"}},
	})
	assertKeys(t, got, []string{
		"allowed", "denied", "destinationRanges", "direction", "disabled", "name",
		"network", "priority", "selfLink", "sourceRanges", "sourceServiceAccounts",
		"sourceTags", "targetServiceAccounts", "targetTags",
	})
}

func TestFirewallRuleContentKeys(t *testing.T) {
	got := keysOf(t, gcpcontent.FirewallRule{Protocol: "tcp", Ports: []string{"443"}})
	assertKeys(t, got, []string{"ports", "protocol"})
}

func TestSubnetworkContentKeys(t *testing.T) {
	got := keysOf(t, gcpcontent.Subnetwork{
		Name: "sn", SelfLink: "self", Network: "n", Region: "r",
		IPCIDRRange: "10.0.0.0/24", Purpose: "PRIVATE",
	})
	assertKeys(t, got, []string{"ipCidrRange", "name", "network", "purpose", "region", "selfLink"})
}

func TestRunServiceContentKeys(t *testing.T) {
	got := keysOf(t, gcpcontent.RunService{
		Name: "svc", URI: "https://svc", Images: []string{"gcr.io/p/app"},
		Ingress: "all", ServiceAcc: "svc@p.iam.gserviceaccount.com",
	})
	assertKeys(t, got, []string{"images", "ingress", "name", "serviceAccount", "uri"})
}

func TestRecordSetContentKeys(t *testing.T) {
	got := keysOf(t, gcpcontent.RecordSet{
		Name: "a.example.com.", Type: "A", TTL: 300, Rrdatas: []string{"34.1.2.3"}, Zone: "z",
	})
	assertKeys(t, got, []string{"name", "rrdatas", "ttl", "type", "zone"})
}

func TestSQLInstanceContentKeys(t *testing.T) {
	got := keysOf(t, gcpcontent.SQLInstance{
		Name: "db", DatabaseVersion: "POSTGRES_15", Region: "r", State: "RUNNABLE",
		Tier: "db-f1-micro", IPAddresses: []string{"35.1.2.3"},
	})
	assertKeys(t, got, []string{"databaseVersion", "ipAddresses", "name", "region", "state", "tier"})
}

func TestForwardingRuleContentKeys(t *testing.T) {
	got := keysOf(t, gcpcontent.ForwardingRule{
		Name: "fr", SelfLink: "self", IPAddress: "34.9.9.9", IPProtocol: "TCP",
		PortRange: "443", Target: "t", Network: "n", Subnetwork: "s", Region: "r",
	})
	assertKeys(t, got, []string{
		"ipAddress", "ipProtocol", "name", "network", "portRange", "region",
		"selfLink", "subnetwork", "target",
	})
}

func TestGenericContentKeys(t *testing.T) {
	got := keysOf(t, gcpcontent.Generic{
		Name: "n", SelfLink: "self", Description: "d", State: "s", Location: "l",
		CreateTime: "t", Labels: map[string]string{"a": "b"}, Fields: map[string]string{"c": "d"},
	})
	assertKeys(t, got, []string{
		"createTime", "description", "fields", "labels", "location", "name", "selfLink", "state",
	})
}

// TestOmitemptyKeepsAnEmptyResourceSmall is the other half of the key
// assertions: every field is omitempty, so a resource with nothing to say stores
// an empty document rather than a page of nulls a search index would then embed.
func TestOmitemptyKeepsAnEmptyResourceSmall(t *testing.T) {
	raw, err := gcpcontent.Marshal(gcpcontent.Instance{})
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if string(raw) != "{}" {
		t.Errorf("an empty instance encodes as %s, want {}", raw)
	}
}

// TestUnmarshalTreatsEmptyContentAsAbsentNotAsAFailure keeps the two apart: a
// resource whose converter stored nothing is not a decode error, and a resolver
// that could not tell them apart would swallow real corruption.
func TestUnmarshalTreatsEmptyContentAsAbsentNotAsAFailure(t *testing.T) {
	var got gcpcontent.Instance
	present, err := gcpcontent.Unmarshal(nil, &got)
	if err != nil || present {
		t.Errorf("nil content: got (present=%v, err=%v), want (false, nil)", present, err)
	}
	present, err = gcpcontent.Unmarshal([]byte(""), &got)
	if err != nil || present {
		t.Errorf("empty content: got (present=%v, err=%v), want (false, nil)", present, err)
	}
}

// TestUnmarshalFailsLoudlyOnCorruptContent is the other half: content that is
// present and not decodable is an error naming the target type, never a
// zero-valued struct a resolver would read as "this resource has nothing".
func TestUnmarshalFailsLoudlyOnCorruptContent(t *testing.T) {
	var got gcpcontent.Instance
	present, err := gcpcontent.Unmarshal([]byte("{not json"), &got)
	if err == nil {
		t.Fatal("corrupt content decoded without an error")
	}
	if present {
		t.Error("corrupt content reported as present")
	}
	if !strings.Contains(err.Error(), "gcpcontent.Instance") {
		t.Errorf("error %q does not name the target type", err)
	}
}

// TestUnmarshalRoundTripsThroughTheDeclaredKeys is the positive control for the
// key assertions above: the keys they pin are the ones a decode actually binds,
// so a set that were merely well-formed and wrong would still be caught here.
func TestUnmarshalRoundTripsThroughTheDeclaredKeys(t *testing.T) {
	// The document is written out BY HAND rather than produced by Marshal, so
	// this reads the reader against an external expectation too.
	const doc = `{
		"name":"vm-1",
		"tags":["web"],
		"serviceAccounts":["svc@p.iam.gserviceaccount.com"],
		"networkInterfaces":[{"network":"vpc","externalIP":"34.1.2.3"}]
	}`
	var got gcpcontent.Instance
	if _, err := gcpcontent.Unmarshal([]byte(doc), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got.Name != "vm-1" {
		t.Errorf("name did not bind: %q", got.Name)
	}
	if len(got.Tags) != 1 {
		t.Errorf("tags did not bind: %v — the firewall byTag index reads this", got.Tags)
	}
	if len(got.ServiceAccounts) != 1 {
		t.Errorf("serviceAccounts did not bind: %v — the firewall bySA index reads this",
			got.ServiceAccounts)
	}
	if len(got.NetworkInterfaces) != 1 {
		t.Fatalf("networkInterfaces did not bind: %v — the firewall byNetwork index and the "+
			"DNS address index read this", got.NetworkInterfaces)
	}
	if got.NetworkInterfaces[0].ExternalIP != "34.1.2.3" {
		t.Errorf("externalIP did not bind: %q — the DNS address index reads this",
			got.NetworkInterfaces[0].ExternalIP)
	}
}
