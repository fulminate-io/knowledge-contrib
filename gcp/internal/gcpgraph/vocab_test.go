// SPDX-License-Identifier: Apache-2.0

package gcpgraph_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// vocab_test.go — THE PARITY FLOOR AS A TEST.
//
// The two lists in vocab.go are the checked-in parity list the walk's emitted
// vocabulary is asserted to be a SUPERSET of (the module suite's R3(b) row lives
// in walk_test.go and reads these). This file asserts the lists themselves are
// what the floor says: the counts, no duplicates, the one deliberate spelling
// break, and the five edge types no sub-collector emits.

func TestResourceTypeFloorIsFortyNine(t *testing.T) {
	got := gcpgraph.ResourceTypes()
	if len(got) != 49 {
		t.Fatalf("resource-type floor: got %d types, want the 49 the parity census recorded", len(got))
	}
	seen := map[string]bool{}
	for _, rt := range got {
		if seen[rt] {
			t.Errorf("resource-type floor: %q appears twice", rt)
		}
		seen[rt] = true
	}
}

// TestCIDRSentinelBreaksTheColonConvention pins the ONE deliberate spelling
// break. The ticket's O6 is settled by the owner's parity decision: the internal
// vocabulary is carried forward VERBATIM including gcp-cidr-block, so a later
// author who "fixes" it to gcp:cidr:block breaks parity rather than a style rule.
func TestCIDRSentinelBreaksTheColonConvention(t *testing.T) {
	if !slices.Contains(gcpgraph.ResourceTypes(), gcpgraph.ResourceTypeCIDRBlock) {
		t.Fatalf("the cidr sentinel %q is not in the resource-type floor", gcpgraph.ResourceTypeCIDRBlock)
	}
	if gcpgraph.ResourceTypeCIDRBlock != "gcp-cidr-block" {
		t.Errorf("cidr sentinel spelling: got %q, want the verbatim internal spelling gcp-cidr-block",
			gcpgraph.ResourceTypeCIDRBlock)
	}
	// The control: every OTHER type is colon-namespaced, so the break is one
	// type rather than an unenforced convention.
	for _, rt := range gcpgraph.ResourceTypes() {
		if rt == gcpgraph.ResourceTypeCIDRBlock {
			continue
		}
		if !strings.HasPrefix(rt, "gcp:") {
			t.Errorf("resource type %q is neither the cidr sentinel nor colon-namespaced", rt)
		}
	}
}

func TestEdgeTypeFloorIsThirtyTwo(t *testing.T) {
	got := gcpgraph.EdgeTypes()
	if len(got) != 32 {
		t.Fatalf("edge-type floor: got %d types, want the 32 the parity census recorded", len(got))
	}
	seen := map[string]bool{}
	for _, et := range got {
		if seen[et] {
			t.Errorf("edge-type floor: %q appears twice", et)
		}
		seen[et] = true
	}
}

// TestDerivedEdgeTypesAreInTheFloor pins the five edge types NO sub-collector
// emits. They exist only through the in-memory resolvers this module runs in
// place of the client's post-populate hook, which is why a module that ships the
// vocabulary and skips the resolvers declares them and derives them never.
func TestDerivedEdgeTypesAreInTheFloor(t *testing.T) {
	derived := []string{
		gcpgraph.EdgeAllowsIngressFrom,
		gcpgraph.EdgeAllowsEgressTo,
		gcpgraph.EdgeSharedWith,
		gcpgraph.EdgeUsesImage,
		gcpgraph.EdgeTrusts,
	}
	for _, et := range derived {
		if !slices.Contains(gcpgraph.EdgeTypes(), et) {
			t.Errorf("derived edge type %q is not in the edge-type floor", et)
		}
	}
	// The control: a string that is not an edge type must NOT be in the floor,
	// so a Contains that always answers true is observable.
	if slices.Contains(gcpgraph.EdgeTypes(), "ALLOWS_NOTHING_AT_ALL") {
		t.Error("the edge-type floor contains a value no collector emits")
	}
}

// TestFloorsAreCopies proves a caller cannot mutate the parity floor out from
// under the assertion that reads it.
func TestFloorsAreCopies(t *testing.T) {
	first := gcpgraph.ResourceTypes()
	first[0] = "mutated"
	if gcpgraph.ResourceTypes()[0] == "mutated" {
		t.Error("ResourceTypes returns the backing array; a caller can rewrite the parity floor")
	}
	firstEdges := gcpgraph.EdgeTypes()
	firstEdges[0] = "mutated"
	if gcpgraph.EdgeTypes()[0] == "mutated" {
		t.Error("EdgeTypes returns the backing array; a caller can rewrite the parity floor")
	}
}
