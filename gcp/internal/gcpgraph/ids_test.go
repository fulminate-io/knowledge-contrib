// SPDX-License-Identifier: Apache-2.0

package gcpgraph_test

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// ids_test.go — THE JOIN KEYS.
//
// Node ids are what every edge is joined on, so an id spelling that drifts by
// one character does not fail loudly: it produces a graph whose edges all dangle
// while every node is present and every count looks right. Each spelling below
// is pinned against the form the source-of-truth vocabulary uses.

func TestCIDRSentinelID(t *testing.T) {
	if got, want := gcpgraph.CIDRSentinelID("10.0.0.0/8"), "gcp:cidr:10.0.0.0/8"; got != want {
		t.Errorf("CIDRSentinelID: got %q, want %q", got, want)
	}
}

func TestBGPPeerID(t *testing.T) {
	if got, want := gcpgraph.BGPPeerID("169.254.0.1"), "gcp:bgp-peer:169.254.0.1"; got != want {
		t.Errorf("BGPPeerID: got %q, want %q", got, want)
	}
}

func TestServiceAccountResourceName(t *testing.T) {
	got := gcpgraph.ServiceAccountResourceName("proj-a", "svc@proj-a.iam.gserviceaccount.com")
	want := "projects/proj-a/serviceAccounts/svc@proj-a.iam.gserviceaccount.com"
	if got != want {
		t.Errorf("ServiceAccountResourceName: got %q, want %q", got, want)
	}
}

func TestProjectResourceName(t *testing.T) {
	if got, want := gcpgraph.ProjectResourceName("proj-a"), "projects/proj-a"; got != want {
		t.Errorf("ProjectResourceName: got %q, want %q", got, want)
	}
}

func TestLastSegment(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{in: "projects/p/zones/us-central1-a", want: "us-central1-a"},
		{in: "us-central1-a", want: "us-central1-a"},
		{in: "", want: ""},
		{in: "trailing/", want: ""},
	} {
		if got := gcpgraph.LastSegment(tc.in); got != tc.want {
			t.Errorf("LastSegment(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestProjectFromSelfLink(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{in: "https://www.googleapis.com/compute/v1/projects/host-proj/global/networks/vpc", want: "host-proj"},
		{in: "https://www.googleapis.com/compute/v1/projects/host-proj", want: "host-proj"},
		{in: "gs://bucket", want: ""},
		{in: "", want: ""},
	} {
		if got := gcpgraph.ProjectFromSelfLink(tc.in); got != tc.want {
			t.Errorf("ProjectFromSelfLink(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestProjectFromServiceAccountEmail(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{in: "svc@proj-a.iam.gserviceaccount.com", want: "proj-a"},
		{in: "svc@proj-a.iam.gserviceaccount.com.evil.example", want: "proj-a"},
		{in: "someone@example.com", want: ""},
		{in: "no-at-sign", want: ""},
		{in: "", want: ""},
	} {
		if got := gcpgraph.ProjectFromServiceAccountEmail(tc.in); got != tc.want {
			t.Errorf("ProjectFromServiceAccountEmail(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestProjectFromServiceAccountResourceName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{in: "projects/proj-a/serviceAccounts/svc@proj-a.iam.gserviceaccount.com", want: "proj-a"},
		{in: "projects/proj-a", want: ""}, // no trailing segment: nothing to cut
		{in: "organizations/1/serviceAccounts/x", want: ""},
		{in: "", want: ""},
	} {
		if got := gcpgraph.ProjectFromServiceAccountResourceName(tc.in); got != tc.want {
			t.Errorf("ProjectFromServiceAccountResourceName(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseArtifactRegistryResourceID(t *testing.T) {
	project, repo := gcpgraph.ParseArtifactRegistryResourceID(
		"projects/proj-a/locations/us-central1/repositories/images")
	if project != "proj-a" || repo != "images" {
		t.Errorf("ParseArtifactRegistryResourceID: got (%q,%q), want (proj-a,images)", project, repo)
	}
	if p, r := gcpgraph.ParseArtifactRegistryResourceID("nonsense"); p != "" || r != "" {
		t.Errorf("ParseArtifactRegistryResourceID(nonsense): got (%q,%q), want two empties", p, r)
	}
}
