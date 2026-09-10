// SPDX-License-Identifier: Apache-2.0

package azure

import (
	"slices"
	"testing"
)

// fixtures_test.go — the PARITY FIXTURES and the census over them.
//
// WHAT A FIXTURE IS. One hand-built Azure API response value handed to the pure
// conversion function that turns it into nodes and edges. No HTTP, no recorded
// cassette, no subscription: the response struct IS the input, which is what
// makes each fixture readable as "this shape of resource produces these
// relationships".
//
// WHAT THE CENSUS OVER THEM IS FOR. The collector declares a vocabulary — every
// Azure resource type it walks and every relationship it draws — and a
// declaration nothing produces is a claim about coverage that no test would
// otherwise contradict. The two census tests below turn each declaration into
// an obligation: a resource type in the vocabulary with no fixture producing it
// fails, and so does a relationship type.
//
// WHAT THE CENSUS CANNOT SEE, stated so it is not read as more than it is: it
// proves that the conversion function CAN produce the type, not that the walk
// calls that function on real data. The listers above it are thin by design for
// exactly that reason, and the walk-level tests cover the rest.

// fixture is one named Azure response and what this collector makes of it.
type fixture struct {
	// name is what a failing census names, so a missing type is traceable to
	// the service that owes it.
	name string
	// build returns the nodes and edges this response produces.
	build func(t *testing.T) subResult
}

// allFixtures is every parity fixture. Each group lives in its own file beside
// the subcollector it exercises.
func allFixtures() []fixture {
	var out []fixture
	out = append(out, computeFixtures()...)
	out = append(out, networkFixtures()...)
	out = append(out, dnsFixtures()...)
	out = append(out, identityFixtures()...)
	out = append(out, dataFixtures()...)
	out = append(out, messagingFixtures()...)
	out = append(out, webFixtures()...)
	out = append(out, monitoringFixtures()...)
	// The resolvers produce nodes too — the address ranges reachability
	// terminates on — so the resource census must see them or it would report
	// a declared type as unproduced.
	out = append(out, resolverFixtures()...)
	return out
}

// TestParity_EveryResourceTypeIsProducedByAFixture is the coverage floor for
// nodes: the collector claims fifty-one Azure resource types and thirteen
// synthetic ones, and each must have a fixture that produces it.
func TestParity_EveryResourceTypeIsProducedByAFixture(t *testing.T) {
	produced := map[string][]string{}
	for _, f := range allFixtures() {
		for _, r := range f.build(t).resources {
			produced[r.resourceType] = append(produced[r.resourceType], f.name)
		}
	}

	declared := append(append([]string(nil), armResourceTypes...), syntheticResourceTypes...)
	for _, rt := range declared {
		if len(produced[rt]) == 0 {
			t.Errorf("the collector declares resource type %q and no fixture produces it", rt)
		}
	}
	for rt, sources := range produced {
		if !contains(declared, rt) {
			t.Errorf("fixture %v produces resource type %q, which the collector does not declare", sources, rt)
		}
	}
	if t.Failed() {
		return
	}
	if got, want := len(armResourceTypes), 51; got != want {
		t.Errorf("the collector declares %d Azure resource types; the parity floor is %d", got, want)
	}
}

// TestParity_EveryEdgeTypeIsProducedByAFixture is the coverage floor for
// relationships. It is the census that would pass while the graph lost a
// relationship, which is why the per-resolver and per-emitter tests exist
// beside it rather than instead of it.
func TestParity_EveryEdgeTypeIsProducedByAFixture(t *testing.T) {
	produced := map[string][]string{}
	for _, f := range allFixtures() {
		for _, e := range f.build(t).edges {
			produced[e.relation] = append(produced[e.relation], f.name)
		}
	}
	// The four relationships no walk emits directly: they exist only after the
	// resolvers run, so the resolver fixtures supply them.
	for _, f := range resolverFixtures() {
		for _, e := range f.build(t).edges {
			produced[e.relation] = append(produced[e.relation], f.name)
		}
	}

	for _, et := range edgeTypes {
		if len(produced[et]) == 0 {
			t.Errorf("the collector declares relationship %q and no fixture produces it", et)
		}
	}
	for et, sources := range produced {
		if !contains(edgeTypes, et) {
			t.Errorf("fixture %v produces relationship %q, which the collector does not declare", sources, et)
		}
	}
	if got, want := len(edgeTypes), 28; got != want {
		t.Errorf("the collector declares %d relationships; the parity floor is %d", got, want)
	}
}

// TestParity_NodeShape asserts the shape every node carries, over every fixture
// at once: the Azure id verbatim as the node id, the one node type, the Azure
// type in metadata rather than as the node type, and a summary.
func TestParity_NodeShape(t *testing.T) {
	for _, f := range allFixtures() {
		for _, r := range f.build(t).resources {
			n := r.node()
			if n.ID != r.id {
				t.Errorf("%s: node id %q is not the resource id %q", f.name, n.ID, r.id)
			}
			if n.Type != nodeTypeCloudResource {
				t.Errorf("%s: node type is %q, not %q", f.name, n.Type, nodeTypeCloudResource)
			}
			if n.Metadata[metaResourceType] != r.resourceType {
				t.Errorf("%s: resource_type metadata is %q, not %q",
					f.name, n.Metadata[metaResourceType], r.resourceType)
			}
			if n.Source != sourceCloud {
				t.Errorf("%s: source is %q, not %q", f.name, n.Source, sourceCloud)
			}
			if n.Summary == "" {
				t.Errorf("%s: node %s has no summary, so it answers no search but its own id", f.name, n.ID)
			}
			if r.region != "" && n.Metadata[metaRegion] != r.region {
				t.Errorf("%s: region metadata is %q, not %q", f.name, n.Metadata[metaRegion], r.region)
			}
		}
	}
}

// TestParity_ArmIDsAreVerbatim is the negative that makes the node-id row mean
// something: an Azure id is used exactly as Azure spells it, case included.
// Azure returns `resourceGroups` from most APIs and `resourcegroups` from a
// few, and a collector that normalized either would emit two nodes for one
// resource the next time the casing changed.
func TestParity_ArmIDsAreVerbatim(t *testing.T) {
	const mixedCase = "/subscriptions/SUB/resourcegroups/RG/providers/Microsoft.Compute/virtualMachines/VM-One"
	vm := virtualMachine(mixedCase, "VM-One")
	r := fx{t}.res(vmResource(vm))
	if r.node().ID != mixedCase {
		t.Errorf("node id %q was rewritten from %q", r.node().ID, mixedCase)
	}
}

// TestParity_ProxyNodesSayWhyTheyAreNotCollected. A proxy is a node for
// something this collector referenced and could not enumerate, and one without
// its reason is indistinguishable from a resource the walk simply missed.
func TestParity_ProxyNodesSayWhyTheyAreNotCollected(t *testing.T) {
	synthetic := map[string]bool{}
	for _, rt := range syntheticResourceTypes {
		synthetic[rt] = true
	}
	// A SYNTHETIC ID IS NOT THE SAME AS A PROXY, and these two are the
	// difference. A security rule and a directory group are both ENUMERATED —
	// the rule from inside its group's response, the group from the directory
	// — and neither has an Azure resource id to be keyed by, which is the only
	// reason their ids are namespaced. They carry their own fields rather than
	// a reason for not being collected, and asserting otherwise would demand
	// that a collected resource claim it was not.
	enumerated := map[string]bool{rtNSGRule: true, rtAADGroup: true}
	seen := map[string]bool{}
	for _, f := range allFixtures() {
		for _, r := range f.build(t).resources {
			if !synthetic[r.resourceType] {
				continue
			}
			seen[r.resourceType] = true
			if enumerated[r.resourceType] {
				continue
			}
			md := r.node().Metadata
			if md[metaCollected] != "false" {
				t.Errorf("%s: proxy %s does not carry %s=false", f.name, r.id, metaCollected)
			}
			if md[metaCollectedReason] == "" {
				t.Errorf("%s: proxy %s does not say why it was not collected", f.name, r.id)
			}
			if md[metaDiscoveredVia] == "" {
				t.Errorf("%s: proxy %s does not say what referenced it", f.name, r.id)
			}
		}
	}
	for rt := range synthetic {
		if !seen[rt] {
			t.Errorf("no fixture produces the synthetic type %q", rt)
		}
	}
}

// build is a fixture's own handle on the test. It exists so a fixture can write
// fx{t}.res(vmResource(vm)) rather than assigning the pair first: a
// conversion error in a fixture is a failed test, not an empty census row, and
// a multi-value call can only be passed as a call's sole argument.
type fx struct{ t *testing.T }

// res fails the test on a conversion error rather than returning a zero
// resource.
func (b fx) res(r resource, err error) resource {
	b.t.Helper()
	if err != nil {
		b.t.Fatalf("building a fixture resource: %v", err)
	}
	return r
}

// edges is the same for an emitter that reports an encoding failure.
func (b fx) edges(e []edge, err error) []edge {
	b.t.Helper()
	if err != nil {
		b.t.Fatalf("building fixture edges: %v", err)
	}
	return e
}

func contains(haystack []string, needle string) bool {
	return slices.Contains(haystack, needle)
}

// relationsOf collects the relationship types a fixture produced, for a test
// asserting on one fixture's own output.
func relationsOf(edges []edge) map[string]int {
	out := map[string]int{}
	for _, e := range edges {
		out[e.relation]++
	}
	return out
}

// edgeBetween finds the first edge of a relationship between two endpoints.
func edgeBetween(edges []edge, from, to, relation string) (edge, bool) {
	for _, e := range edges {
		if e.from == from && e.to == to && e.relation == relation {
			return e, true
		}
	}
	return edge{}, false
}

// resourceTypesOf collects the resource types a fixture produced.
func resourceTypesOf(rs []resource) map[string]int {
	out := map[string]int{}
	for _, r := range rs {
		out[r.resourceType]++
	}
	return out
}
