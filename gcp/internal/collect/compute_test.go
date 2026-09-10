// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"context"
	"strings"
	"testing"

	computepb "cloud.google.com/go/compute/apiv1/computepb"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

const vmSelfLink = "https://www.googleapis.com/compute/v1/projects/proj-a/zones/us-central1-a/instances/web"

func staticLister[T any](items ...T) collect.Lister[T] {
	return func(context.Context, string) ([]T, error) { return items, nil }
}

// runOne drives a sub-collector over its canned page and returns the result.
func runOne(t *testing.T, sub collect.Subcollector) gcpgraph.Result {
	t.Helper()
	got, err := sub.Run(t.Context(), "proj-a")
	if err != nil {
		t.Fatalf("%s: %v", sub.Name, err)
	}
	return got
}

func findResource(t *testing.T, got gcpgraph.Result, id string) gcpgraph.Resource {
	t.Helper()
	for _, res := range got.Resources {
		if res.ID == id {
			return res
		}
	}
	t.Fatalf("no resource with id %q; got %d resources", id, len(got.Resources))
	return gcpgraph.Resource{}
}

func hasRelation(got gcpgraph.Result, from, to, edgeType string) bool {
	for _, r := range got.Relations {
		if r.From == from && r.To == to && r.Type == edgeType {
			return true
		}
	}
	return false
}

func relationKeys(got gcpgraph.Result) []string {
	out := make([]string, 0, len(got.Relations))
	for _, r := range got.Relations {
		out = append(out, r.Type+" "+r.From+" -> "+r.To)
	}
	return out
}

func TestComputeInstancesConvertsOneInstance(t *testing.T) {
	got := runOne(t, collect.ComputeInstances(staticLister(&computepb.Instance{
		Name:              new("web"),
		SelfLink:          new(vmSelfLink),
		Zone:              new("https://www.googleapis.com/compute/v1/projects/proj-a/zones/us-central1-a"),
		MachineType:       new("https://www.googleapis.com/compute/v1/projects/proj-a/zones/us-central1-a/machineTypes/e2-medium"),
		Status:            new("RUNNING"),
		CreationTimestamp: new("2026-01-01T00:00:00Z"),
		Labels:            map[string]string{"env": "prod"},
		Tags:              &computepb.Tags{Items: []string{"web"}},
		ServiceAccounts: []*computepb.ServiceAccount{
			{Email: new("svc@proj-a.iam.gserviceaccount.com")},
		},
		NetworkInterfaces: []*computepb.NetworkInterface{{
			Network:    new("net-self-link"),
			Subnetwork: new("subnet-self-link"),
			NetworkIP:  new("10.0.0.5"),
			AccessConfigs: []*computepb.AccessConfig{
				{NatIP: new("")},
				{NatIP: new("34.1.2.3")},
			},
		}},
		Disks: []*computepb.AttachedDisk{{Source: new("disk-self-link")}},
	})))

	res := findResource(t, got, vmSelfLink)
	if res.ResourceType != gcpgraph.ResourceTypeInstance {
		t.Errorf("resource type: got %q", res.ResourceType)
	}
	if res.Name != "web" {
		t.Errorf("name: got %q", res.Name)
	}
	// The zone and machine type are the LAST SEGMENT of their URLs, not the URLs.
	if res.Region != "us-central1-a" {
		t.Errorf("region: got %q, want the zone name", res.Region)
	}
	for key, want := range map[string]string{
		"status":        "RUNNING",
		"machineType":   "e2-medium",
		"zone":          "us-central1-a",
		"creation_time": "2026-01-01T00:00:00Z",
		"label/env":     "prod",
		"tag/web":       "",
		"primary_ip":    "10.0.0.5",
		"external_ip":   "34.1.2.3",
	} {
		if _, ok := res.Metadata[key]; !ok {
			t.Errorf("metadata key %q is missing: %v", key, res.Metadata)
			continue
		}
		if res.Metadata[key] != want {
			t.Errorf("metadata %q: got %q, want %q", key, res.Metadata[key], want)
		}
	}
	// The service account is an EDGE, not metadata: two answers to one question
	// is what a later reader disagrees with itself over.
	for key := range res.Metadata {
		if strings.Contains(key, "serviceAccount") {
			t.Errorf("the service account was duplicated into metadata as %q", key)
		}
	}

	for _, want := range []struct{ from, to, edgeType string }{
		{vmSelfLink, "subnet-self-link", gcpgraph.EdgeUsesSubnet},
		{vmSelfLink, "net-self-link", gcpgraph.EdgeUsesNetwork},
		{vmSelfLink, gcpgraph.ServiceAccountResourceName("proj-a", "svc@proj-a.iam.gserviceaccount.com"),
			gcpgraph.EdgeUsesSA},
		// The disk is bound TO the instance, not the other way round.
		{"disk-self-link", vmSelfLink, gcpgraph.EdgeBoundTo},
	} {
		if !hasRelation(got, want.from, want.to, want.edgeType) {
			t.Errorf("missing edge %s %s -> %s; got %v",
				want.edgeType, want.from, want.to, relationKeys(got))
		}
	}
	if len(got.Relations) != 4 {
		t.Errorf("got %d edges, want 4: %v", len(got.Relations), relationKeys(got))
	}
}

// The content the DOWNSTREAM RESOLVERS read is written by this converter, so the
// three fields they index are asserted on the converter's own output rather than
// on a fixture written by hand.
func TestComputeInstanceContentFeedsTheResolvers(t *testing.T) {
	got := runOne(t, collect.ComputeInstances(staticLister(&computepb.Instance{
		Name:     new("web"),
		SelfLink: new(vmSelfLink),
		Tags:     &computepb.Tags{Items: []string{"web"}},
		ServiceAccounts: []*computepb.ServiceAccount{
			{Email: new("svc@proj-a.iam.gserviceaccount.com")},
		},
		NetworkInterfaces: []*computepb.NetworkInterface{{
			Network:       new("net-self-link"),
			AccessConfigs: []*computepb.AccessConfig{{NatIP: new("34.1.2.3")}},
		}},
	})))
	var content gcpcontent.Instance
	present, err := gcpcontent.Unmarshal(findResource(t, got, vmSelfLink).Content, &content)
	if err != nil || !present {
		t.Fatalf("the stored instance content did not decode: present=%v err=%v", present, err)
	}
	if len(content.Tags) != 1 {
		t.Errorf("tags: the firewall tag index would read nothing: %v", content.Tags)
	}
	if len(content.ServiceAccounts) != 1 {
		t.Errorf("serviceAccounts: the firewall account index would read nothing: %v",
			content.ServiceAccounts)
	}
	if len(content.NetworkInterfaces) != 1 || content.NetworkInterfaces[0].Network != "net-self-link" {
		t.Errorf("networkInterfaces: the firewall network index would read nothing: %v",
			content.NetworkInterfaces)
	}
	if content.NetworkInterfaces[0].ExternalIP != "34.1.2.3" {
		t.Errorf("externalIP: the DNS address index would read nothing: %q",
			content.NetworkInterfaces[0].ExternalIP)
	}
}

func TestComputeInstancesRefusesAnInstanceWithNoSelfLink(t *testing.T) {
	_, err := collect.ComputeInstances(staticLister(&computepb.Instance{
		Name: new("nameless"),
	})).Run(t.Context(), "proj-a")
	if err == nil {
		t.Fatal("an instance with no self link was emitted anyway")
	}
	if !strings.Contains(err.Error(), "nameless") {
		t.Errorf("error %q does not name the instance it refused", err)
	}
}

func TestComputeInstancesOnAnEmptyProjectEmitsNothing(t *testing.T) {
	got := runOne(t, collect.ComputeInstances(staticLister[*computepb.Instance]()))
	if len(got.Resources) != 0 || len(got.Relations) != 0 {
		t.Errorf("an empty project produced %d resources and %d edges",
			len(got.Resources), len(got.Relations))
	}
}
