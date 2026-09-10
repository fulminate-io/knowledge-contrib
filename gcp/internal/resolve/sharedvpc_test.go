// SPDX-License-Identifier: Apache-2.0

package resolve_test

import (
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpcontent"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/resolve"
)

// sharedvpc_test.go — a subnet whose parent VPC lives in another project.

const (
	hostVPC    = "https://www.googleapis.com/compute/v1/projects/host-proj/global/networks/shared"
	localVPC   = "https://www.googleapis.com/compute/v1/projects/proj-a/global/networks/vpc"
	localSubID = "https://www.googleapis.com/compute/v1/projects/proj-a/regions/r/subnetworks/sn"
)

// subnetResource is the one subnet every case here varies the PARENT of. The
// subnet's own identity is fixed, because what these cases are about is where
// its parent network lives.
func subnetResource(t *testing.T, parentNetwork string) gcpgraph.Resource {
	t.Helper()
	const name = "sn"
	return gcpgraph.Resource{
		ID: localSubID, Name: name, ResourceType: gcpgraph.ResourceTypeSubnetwork,
		Content: mustMarshal(t, gcpcontent.Subnetwork{
			Name: name, SelfLink: localSubID, Network: parentNetwork,
		}),
	}
}

func TestSharedVPCEmitsTheLocalHalfForAForeignParentNetwork(t *testing.T) {
	got, err := resolve.SharedVPC("proj-a", gcpgraph.Result{Resources: []gcpgraph.Resource{
		subnetResource(t, hostVPC),
	}})
	if err != nil {
		t.Fatalf("SharedVPC: %v", err)
	}
	if !hasEdge(got.Relations, hostVPC, localSubID, gcpgraph.EdgeSharedWith) {
		t.Fatalf("no SHARED_WITH from the host network to the local subnet: %v", edgeKeys(got.Relations))
	}
	if len(got.Relations) != 1 {
		t.Errorf("got %d edges, want 1: %v", len(got.Relations), edgeKeys(got.Relations))
	}
	if got.Relations[0].Method != resolve.MethodSharedVPC {
		t.Errorf("edge method: got %q, want %q", got.Relations[0].Method, resolve.MethodSharedVPC)
	}
	// The REMOTE half is deliberately not carried. The built-in resolver also
	// writes this edge into the HOST project's own graph, which is a cross-graph
	// write a single custom graph has no mechanism for. A collect of the host
	// project records that side, exactly as a cross-graph reference behaves.
	if len(got.Resources) != 0 {
		t.Errorf("the shared-VPC resolver materialized %d nodes; it derives edges only",
			len(got.Resources))
	}
}

// The cell that closes the axis: a subnet whose parent network is in THIS
// project is an ordinary subnet, not a shared one.
func TestSharedVPCIgnoresASameProjectParentNetwork(t *testing.T) {
	got, err := resolve.SharedVPC("proj-a", gcpgraph.Result{Resources: []gcpgraph.Resource{
		subnetResource(t, localVPC),
	}})
	if err != nil {
		t.Fatalf("SharedVPC: %v", err)
	}
	if len(got.Relations) != 0 {
		t.Errorf("a same-project subnet produced %d SHARED_WITH edges, want 0: %v",
			len(got.Relations), edgeKeys(got.Relations))
	}
}

// A subnet whose parent network is unparseable resolves to no project, which is
// distinguishable from "the same project" and yields nothing either way.
func TestSharedVPCIgnoresAnUnparseableParentNetwork(t *testing.T) {
	got, err := resolve.SharedVPC("proj-a", gcpgraph.Result{Resources: []gcpgraph.Resource{
		subnetResource(t, "not-a-self-link"),
	}})
	if err != nil {
		t.Fatalf("SharedVPC: %v", err)
	}
	if len(got.Relations) != 0 {
		t.Errorf("an unparseable parent network produced %d edges, want 0", len(got.Relations))
	}
}

func TestSharedVPCFailsLoudlyOnCorruptContent(t *testing.T) {
	_, err := resolve.SharedVPC("proj-a", gcpgraph.Result{Resources: []gcpgraph.Resource{{
		ID: localSubID, Name: "sn", ResourceType: gcpgraph.ResourceTypeSubnetwork,
		Content: []byte("{not json"),
	}}})
	if err == nil {
		t.Fatal("a subnet with undecodable content was skipped silently")
	}
	if !strings.Contains(err.Error(), "sn") {
		t.Errorf("error %q does not name the subnet it could not read", err)
	}
}
