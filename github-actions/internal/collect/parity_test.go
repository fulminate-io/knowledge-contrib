// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// parity_test.go — THE COVERAGE ASSERTION, and the one row that says whether
// this collector meets its target at all.
//
// The declared vocabulary is a promise: a consumer that queries a resource type
// or walks an edge type expects this collector to produce it. A module that
// declared the list and emitted three quarters of it would pass every other test
// in this suite — each converter is correct in isolation — and produce a graph
// missing whole classes of thing. This runs the WHOLE collector over one
// organization's recorded responses and asserts the emitted set covers the
// declared one.
//
// THE OTHER DIRECTION IS CLOSED BY CONSTRUCTION: the graph builder refuses a
// type outside the declared vocabulary, so the emitted set is a subset already
// and these assertions close it into an equality.

// walkTheFixtureOrganization runs every enumeration over its recorded response.
func walkTheFixtureOrganization(t *testing.T) ghgraph.Result {
	t.Helper()
	return walkTheFixtureOrganizationInOrder(t, func([]collect.Subcollector) {})
}

// walkTheFixtureOrganizationInOrder is the same walk with the enumeration order
// under the caller's control.
//
// THE ORDER IS A PARAMETER BECAUSE THE FIXTURES RUN SERIALLY AND THE REAL WALK
// DOES NOT. Production fans out concurrently, so two collects of one unchanged
// organization merge their results in whatever order the goroutines finished. A
// stability test that ran the same fixed order twice would produce identical
// input both times and pass with the sorting deleted.
func walkTheFixtureOrganizationInOrder(
	t *testing.T, arrange func([]collect.Subcollector),
) ghgraph.Result {
	t.Helper()
	repos, actions := fixtureAPI()
	subs := collect.All(collect.API{Repos: repos, Actions: actions}, collect.Caps{})
	arrange(subs)

	var out ghgraph.Result
	for _, sub := range subs {
		got, err := sub.Run(context.Background(), fixtureOrg)
		if err != nil {
			t.Fatalf("%s over the fixture organization: %v", sub.Name, err)
		}
		out.Add(got)
	}
	return out
}

func TestEveryDeclaredResourceTypeIsEmitted(t *testing.T) {
	got := walkTheFixtureOrganization(t)

	emitted := map[string]bool{}
	for _, res := range got.Resources {
		emitted[res.ResourceType] = true
	}

	var missing []string
	for _, want := range ghgraph.ResourceTypes() {
		if !emitted[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d declared resource types are never emitted: %s\n"+
			"A type in the declared vocabulary that nothing produces is a promise this collector "+
			"does not keep.", len(missing), strings.Join(missing, ", "))
	}

	// The control: the assertion above would pass just as well against a
	// collector that emitted EVERY string, so the emitted set is also checked for
	// anything outside the declared one.
	for emittedType := range emitted {
		if !ghgraph.IsDeclaredResourceType(emittedType) {
			t.Errorf("the walk emitted %q, which is not in the declared vocabulary", emittedType)
		}
	}
}

func TestEveryDeclaredEdgeTypeIsEmitted(t *testing.T) {
	got := walkTheFixtureOrganization(t)

	emitted := map[string]bool{}
	for _, rel := range got.Relations {
		emitted[rel.Type] = true
	}

	var missing []string
	for _, want := range ghgraph.EdgeTypes() {
		if !emitted[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d declared edge types are never emitted: %s", len(missing), strings.Join(missing, ", "))
	}
	for emittedType := range emitted {
		if !ghgraph.IsDeclaredEdgeType(emittedType) {
			t.Errorf("the walk emitted the edge type %q, which is not in the declared vocabulary",
				emittedType)
		}
	}
}

// TestRunsInIsEmittedByNothing is the absence half of the floor, and it needs
// its own row because a coverage assertion that only checks presence lets a
// seventh edge type appear unnoticed.
//
// THE SAME-RUN KNOWN POSITIVE is the assertion above it: the same walk is
// checked for six types it MUST emit, so a walk that produced no edges at all
// cannot satisfy this one silently.
func TestRunsInIsEmittedByNothing(t *testing.T) {
	got := walkTheFixtureOrganization(t)
	if len(got.Relations) == 0 {
		t.Fatal("the fixture walk emitted no edges at all; the absence below would mean nothing")
	}
	for _, rel := range got.Relations {
		if rel.Type == ghgraph.EdgeRunsIn {
			t.Errorf("the walk emitted %s (%s -> %s). This provider names no runner on a run, "+
				"so an edge asserting one was invented.", ghgraph.EdgeRunsIn, rel.FromID, rel.ToID)
		}
	}
}

// TestTheBelongsToFanInReachesEveryParent pins the seven sites the relationship
// is emitted from and the ten parent arms they take, by LITERAL id on both ends.
func TestTheBelongsToFanInReachesEveryParent(t *testing.T) {
	got := walkTheFixtureOrganization(t)

	// Every pair is written out rather than built from the id helpers: a test
	// that asked the code under test what it should have produced would agree
	// with it however wrong both were.
	for _, want := range [][2]string{
		{"github:acme/Repository/acme/api", "github:acme/Organization/acme"},
		{"github:acme/Workflow/acme/api/.github/workflows/ci.yml", "github:acme/Repository/acme/api"},
		{"github:acme/WorkflowRun/acme/api/100", "github:acme/Workflow/acme/api/.github/workflows/ci.yml"},
		{"github:acme/Environment/acme/api/production", "github:acme/Repository/acme/api"},
		{"github:acme/Deployment/acme/api/500", "github:acme/Repository/acme/api"},
		{"github:acme/Runner/1", "github:acme/Organization/acme"},
		{"github:acme/Runner/2", "github:acme/Repository/acme/api"},
		{"github:acme/Secret/org/ORG_TOKEN", "github:acme/Organization/acme"},
		{"github:acme/Secret/acme/api/repo/API_KEY", "github:acme/Repository/acme/api"},
		{"github:acme/Secret/acme/api/env/production/PROD_DB_PASS",
			"github:acme/Environment/acme/api/production"},
	} {
		if !hasRelation(got, want[0], want[1], ghgraph.EdgeBelongsTo) {
			t.Errorf("no BELONGS_TO edge from %q to %q", want[0], want[1])
		}
	}
}

// TestTwoCollectsOfAnUnchangedOrganizationAreIdentical is the carry-forward row.
func TestTwoCollectsOfAnUnchangedOrganizationAreIdentical(t *testing.T) {
	first := walkTheFixtureOrganization(t)
	second := walkTheFixtureOrganizationInOrder(t, func(subs []collect.Subcollector) {
		rand.Shuffle(len(subs), func(i, j int) { subs[i], subs[j] = subs[j], subs[i] })
	})

	firstNodes, firstEdges, err := ghgraph.Build(first)
	if err != nil {
		t.Fatalf("building the first collect: %v", err)
	}
	secondNodes, secondEdges, err := ghgraph.Build(second)
	if err != nil {
		t.Fatalf("building the second collect: %v", err)
	}

	// COMPARED AS THE BYTES THAT GO ON THE WIRE, which is what "changes nothing"
	// means to the receiving server: it compares generations, not Go values. The
	// node struct carries a map and is not comparable in Go anyway, and a
	// field-by-field comparison would silently stop covering a field added later.
	if first, second := marshal(t, firstNodes), marshal(t, secondNodes); first != second {
		t.Errorf("two collects of an unchanged organization produced different nodes:\n%s\nvs\n%s",
			first, second)
	}
	if first, second := marshal(t, firstEdges), marshal(t, secondEdges); first != second {
		t.Errorf("two collects of an unchanged organization produced different edges:\n%s\nvs\n%s",
			first, second)
	}

	// The known positive: an empty walk would compare equal to itself. The
	// fixture organization is not empty, and this says so in the same run.
	if len(firstNodes) == 0 || len(firstEdges) == 0 {
		t.Fatalf("the fixture walk produced %d nodes and %d edges; two empty results are equal for "+
			"the wrong reason", len(firstNodes), len(firstEdges))
	}
}

// marshal renders a value as the JSON the envelope carries.
func marshal(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshaling a collect result: %v", err)
	}
	return string(raw)
}

// hasRelation reports whether the walk emitted exactly this edge.
func hasRelation(got ghgraph.Result, from, to, edgeType string) bool {
	return slices.ContainsFunc(got.Relations, func(rel ghgraph.Relation) bool {
		return rel.FromID == from && rel.ToID == to && rel.Type == edgeType
	})
}

// resourceIDs is every node id the walk emitted.
func resourceIDs(got ghgraph.Result) map[string]string {
	out := make(map[string]string, len(got.Resources))
	for _, res := range got.Resources {
		out[res.ID] = res.ResourceType
	}
	return out
}
