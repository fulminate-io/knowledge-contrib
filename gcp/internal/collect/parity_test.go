// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/collect"

	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/resolve"
)

// parity_test.go — THE COVERAGE ASSERTION, and the one row that says whether
// this collector meets its target at all.
//
// The declared vocabulary is a promise: a consumer that queries a resource type
// or walks an edge type expects this collector to produce it. A module that
// declared the list and emitted three quarters of it would pass every other test
// in this suite — each converter is correct in isolation — and produce a graph
// missing whole classes of thing. This runs the WHOLE collector over one
// project's recorded responses, derivations included, and asserts the emitted
// set covers the declared one.
//
// THE OTHER DIRECTION IS ENFORCED ELSEWHERE AND BY CONSTRUCTION: the builder
// refuses a type outside the floor, so the emitted set is a subset by
// construction and this closes it into an equality.

// walkTheFixtureProject runs every enumeration over its recorded response, then
// the derivations, exactly as the walk does.
func walkTheFixtureProject(t *testing.T) gcpgraph.Result {
	t.Helper()
	return walkTheFixtureProjectInOrder(t, func(subs []collect.Subcollector) {})
}

// walkTheFixtureProjectInOrder is the same walk with the enumeration order under
// the caller's control.
//
// THE ORDER IS A PARAMETER BECAUSE THE FIXTURES RUN SERIALLY AND THE REAL WALK
// DOES NOT. Production fans out concurrently, so two collects of one unchanged
// project merge their results in whatever order the goroutines finished. A
// stability test that ran the same fixed order twice would produce identical
// input both times and pass with the sorting deleted, which is exactly what the
// first version of this file did.
func walkTheFixtureProjectInOrder(
	t *testing.T, arrange func([]collect.Subcollector),
) gcpgraph.Result {
	t.Helper()
	subs := fixtureEnumerations(t)
	arrange(subs)

	var enumerated gcpgraph.Result
	for _, sub := range subs {
		got, err := sub.Run(t.Context(), fixtureProject)
		if err != nil {
			t.Fatalf("%s: %v", sub.Name, err)
		}
		enumerated.Add(got)
	}
	derived, err := resolve.All(fixtureProject, enumerated)
	if err != nil {
		t.Fatalf("deriving over the fixture project: %v", err)
	}
	return derived
}

func TestEveryDeclaredResourceTypeIsEmitted(t *testing.T) {
	got := walkTheFixtureProject(t)

	emitted := map[string]bool{}
	for _, res := range got.Resources {
		emitted[res.ResourceType] = true
	}

	var missing []string
	for _, want := range gcpgraph.ResourceTypes() {
		if !emitted[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d declared resource types are never emitted: %s\n"+
			"A type in the declared vocabulary that nothing produces is a promise this "+
			"collector does not keep.", len(missing), strings.Join(missing, ", "))
	}

	// The control: the assertion above would pass just as well against a
	// collector that emitted EVERY string, so the emitted set is also checked
	// for anything outside the declared one.
	for emittedType := range emitted {
		if !slices.Contains(gcpgraph.ResourceTypes(), emittedType) {
			t.Errorf("the walk emitted %q, which is not in the declared vocabulary", emittedType)
		}
	}
}

func TestEveryDeclaredEdgeTypeIsEmitted(t *testing.T) {
	got := walkTheFixtureProject(t)

	emitted := map[string]bool{}
	for _, rel := range got.Relations {
		emitted[rel.Type] = true
	}

	var missing []string
	for _, want := range gcpgraph.EdgeTypes() {
		if !emitted[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d declared edge types are never emitted: %s\n"+
			"An edge type nothing produces is a relationship a consumer will look for "+
			"and never find.", len(missing), strings.Join(missing, ", "))
	}
	for emittedType := range emitted {
		if !slices.Contains(gcpgraph.EdgeTypes(), emittedType) {
			t.Errorf("the walk emitted the edge type %q, which is not declared", emittedType)
		}
	}
}

// TestTheFixtureProjectBuildsIntoContractNodes closes the loop: the coverage
// above is over the intermediate shape, and this is the same walk crossing into
// what the contract actually carries. A resource that covered the vocabulary and
// then failed to build would pass the two assertions above.
func TestTheFixtureProjectBuildsIntoContractNodes(t *testing.T) {
	nodes, edges, err := gcpgraph.Build(walkTheFixtureProject(t))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(nodes) == 0 || len(edges) == 0 {
		t.Fatalf("the fixture project built into %d nodes and %d edges", len(nodes), len(edges))
	}
	for _, node := range nodes {
		if node.ID == "" || node.Type == "" {
			t.Errorf("a built node is missing an id or a type: %+v", node)
		}
		if node.Metadata["resource_type"] != node.Type {
			t.Errorf("node %q carries type %q and metadata resource_type %q; they must agree",
				node.ID, node.Type, node.Metadata["resource_type"])
		}
	}
	for _, edge := range edges {
		if edge.FromID == "" || edge.ToID == "" || edge.Type == "" {
			t.Errorf("a built edge is incomplete: %+v", edge)
		}
	}
}

// TestTheWalkIsByteStableAcrossTwoRuns is the CARRY-FORWARD row: a second
// collect of an unchanged project must produce the same output as the first, or
// a diff keyed on node id reports changes the source did not make.
func TestTheWalkIsByteStableAcrossTwoRuns(t *testing.T) {
	// The two runs enumerate in DIFFERENT orders, which is what makes this a
	// test of the sorting rather than of the fixture list. Production fans out
	// concurrently and merges in completion order; a shuffle is that, made
	// deterministic so a failure reproduces.
	shuffle := func(seed uint64) func([]collect.Subcollector) {
		return func(subs []collect.Subcollector) {
			rng := rand.New(rand.NewPCG(seed, seed))
			rng.Shuffle(len(subs), func(i, j int) { subs[i], subs[j] = subs[j], subs[i] })
		}
	}

	firstNodes, firstEdges, err := gcpgraph.Build(walkTheFixtureProjectInOrder(t, shuffle(1)))
	if err != nil {
		t.Fatalf("Build (first run): %v", err)
	}
	secondNodes, secondEdges, err := gcpgraph.Build(walkTheFixtureProjectInOrder(t, shuffle(2)))
	if err != nil {
		t.Fatalf("Build (second run): %v", err)
	}

	// The control: the two shuffles really did produce different orders, so a
	// pass below is the sort doing the work rather than two identical inputs.
	firstOrder := enumerationOrder(t, shuffle(1))
	secondOrder := enumerationOrder(t, shuffle(2))
	if slices.Equal(firstOrder, secondOrder) {
		t.Fatal("the two shuffles produced the same enumeration order; this test would pass with " +
			"the sorting deleted, which is what it exists to catch")
	}

	if len(firstNodes) != len(secondNodes) || len(firstEdges) != len(secondEdges) {
		t.Fatalf("two walks of one unchanged project differ in size: %d/%d nodes, %d/%d edges",
			len(firstNodes), len(secondNodes), len(firstEdges), len(secondEdges))
	}
	for i := range firstNodes {
		if firstNodes[i].ID != secondNodes[i].ID {
			t.Fatalf("node %d differs between two runs: %q vs %q",
				i, firstNodes[i].ID, secondNodes[i].ID)
		}
		if firstNodes[i].Content != secondNodes[i].Content {
			t.Fatalf("node %q content differs between two runs", firstNodes[i].ID)
		}
		if firstNodes[i].Summary != secondNodes[i].Summary {
			t.Fatalf("node %q summary differs between two runs", firstNodes[i].ID)
		}
	}
	for i := range firstEdges {
		if firstEdges[i] != secondEdges[i] {
			t.Fatalf("edge %d differs between two runs: %+v vs %+v",
				i, firstEdges[i], secondEdges[i])
		}
	}
}

// enumerationOrder is the order a given arrangement produces, used as the
// control above.
func enumerationOrder(t *testing.T, arrange func([]collect.Subcollector)) []string {
	t.Helper()
	subs := fixtureEnumerations(t)
	arrange(subs)
	names := make([]string, 0, len(subs))
	for _, sub := range subs {
		names = append(names, sub.Name)
	}
	return names
}

// TestEveryDerivedEdgeNamesANodeTheWalkCarries is the dangling-endpoint check
// for the half of the graph this collector produces itself. An enumerated edge
// legitimately names a resource the walk did not enumerate — a key in another
// project, an image in a public catalog — but a DERIVED edge is built from the
// walk's own contents, so an endpoint it cannot name is a defect in the
// derivation rather than a fact about the project.
func TestEveryDerivedEdgeNamesANodeTheWalkCarries(t *testing.T) {
	got := walkTheFixtureProject(t)
	present := map[string]bool{}
	for _, res := range got.Resources {
		present[res.ID] = true
	}
	derivedMethods := map[string]bool{
		resolve.MethodFirewall:          true,
		resolve.MethodSharedVPC:         false, // names a network in the HOST project
		resolve.MethodImageLineage:      true,
		resolve.MethodCrossProjectTrust: true,
		resolve.MethodDNSResolve:        true,
		resolve.MethodIAMGroupResolve:   false, // the source is a role string, not a node
	}
	for _, rel := range got.Relations {
		mustResolve, isDerived := derivedMethods[rel.Method]
		if !isDerived || !mustResolve {
			continue
		}
		if !present[rel.From] {
			t.Errorf("derived edge %s names a source %q the walk does not carry", rel.Type, rel.From)
		}
		if !present[rel.To] {
			t.Errorf("derived edge %s names a target %q the walk does not carry", rel.Type, rel.To)
		}
	}
}
