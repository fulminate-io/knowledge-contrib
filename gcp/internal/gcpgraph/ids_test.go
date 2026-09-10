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
		{"projects/p/zones/us-central1-a", "us-central1-a"},
		{"us-central1-a", "us-central1-a"},
		{"", ""},
		{"trailing/", ""},
	} {
		if got := gcpgraph.LastSegment(tc.in); got != tc.want {
			t.Errorf("LastSegment(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestProjectFromSelfLink(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"https://www.googleapis.com/compute/v1/projects/host-proj/global/networks/vpc", "host-proj"},
		{"https://www.googleapis.com/compute/v1/projects/host-proj", "host-proj"},
		{"gs://bucket", ""},
		{"", ""},
	} {
		if got := gcpgraph.ProjectFromSelfLink(tc.in); got != tc.want {
			t.Errorf("ProjectFromSelfLink(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestProjectFromServiceAccountEmail(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"svc@proj-a.iam.gserviceaccount.com", "proj-a"},
		{"svc@proj-a.iam.gserviceaccount.com.evil.example", "proj-a"},
		{"someone@example.com", ""},
		{"no-at-sign", ""},
		{"", ""},
	} {
		if got := gcpgraph.ProjectFromServiceAccountEmail(tc.in); got != tc.want {
			t.Errorf("ProjectFromServiceAccountEmail(%q): got %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestProjectFromServiceAccountResourceName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"projects/proj-a/serviceAccounts/svc@proj-a.iam.gserviceaccount.com", "proj-a"},
		{"projects/proj-a", ""}, // no trailing segment: nothing to cut
		{"organizations/1/serviceAccounts/x", ""},
		{"", ""},
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
