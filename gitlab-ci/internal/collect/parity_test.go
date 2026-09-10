// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// parity_test.go — THE COVERAGE ASSERTION, and the one row that says whether
// this collector meets its target at all.
//
// The declared vocabulary is a promise: a consumer that queries a resource type
// or walks an edge type expects this collector to produce it. A module that
// declared the list and emitted three quarters of it would pass every other test
// in this suite — each converter is correct in isolation — and produce a graph
// missing whole classes of thing. This runs the WHOLE collector over one group's
// recorded responses and asserts the emitted set covers the declared one.
//
// THE OTHER DIRECTION IS CLOSED BY CONSTRUCTION: the graph builder refuses a type
// outside the declared vocabulary, so the emitted set is a subset already and
// these assertions close it into an equality.
//
// THE TEST LIST ITSELF IS DERIVED FROM THE SOURCE PROVIDER'S OWN SUITE, and that
// derivation came up nearly empty, which is stated here rather than left to be
// inferred from a short file. That provider carries ten test files: one for its
// pipeline parser, one for its summarizer registry and eight for the individual
// summarizers. It has NO per-enumeration behavioral test at all — no pagination
// arm, no refusal arm, no id assertion, no edge assertion, no coverage row. So
// there was nothing to carry across for seven of its eight behaviors, and the
// input-class rows in enumerations_test.go and completeness_test.go are the whole
// of this collector's coverage of them rather than a supplement to an inherited
// suite.

// walkTheFixtureGroup runs every enumeration over its recorded response.
func walkTheFixtureGroup(t *testing.T) glgraph.Result {
	t.Helper()
	return walkTheFixtureGroupInOrder(t, fixtureAPI(), func([]collect.Subcollector) {})
}

// walkTheFixtureGroupInOrder is the same walk with the enumeration order under
// the caller's control.
//
// THE ORDER IS A PARAMETER BECAUSE THE FIXTURES RUN SERIALLY AND THE REAL WALK
// DOES NOT. Production fans out concurrently, so two collects of one unchanged
// group merge their results in whatever order the goroutines finished. A
// stability test that ran the same fixed order twice would produce identical
// input both times and pass with the sorting deleted.
func walkTheFixtureGroupInOrder(
	t *testing.T, api *fakeAPI, arrange func([]collect.Subcollector),
) glgraph.Result {
	t.Helper()
	api.newWalk()
	plan := collect.All(api.bundle(), collect.Caps{}, fixtureGroup)
	arrange(plan.Subcollectors)

	var out glgraph.Result
	for _, sub := range plan.Subcollectors {
		got, err := sub.Run(context.Background())
		if err != nil {
			t.Fatalf("%s over the fixture group: %v", sub.Name, err)
		}
		out.Add(got)
	}
	return out
}

func TestEveryDeclaredResourceTypeIsEmitted(t *testing.T) {
	got := walkTheFixtureGroup(t)

	emitted := map[string]bool{}
	for _, res := range got.Resources {
		emitted[res.ResourceType] = true
	}

	var missing []string
	for _, want := range glgraph.ResourceTypes() {
		if !emitted[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d declared resource types are never emitted: %s\n"+
			"A type in the declared vocabulary that nothing produces is a promise this collector "+
			"does not keep.", len(missing), strings.Join(missing, ", "))
	}
	if len(glgraph.ResourceTypes()) != 11 {
		t.Errorf("the declared vocabulary carries %d resource types, and the parity floor is 11",
			len(glgraph.ResourceTypes()))
	}

	// The control: the assertion above would pass just as well against a
	// collector that emitted EVERY string, so the emitted set is also checked for
	// anything outside the declared one.
	for emittedType := range emitted {
		if !glgraph.IsDeclaredResourceType(emittedType) {
			t.Errorf("the walk emitted %q, which is not in the declared vocabulary", emittedType)
		}
	}
}

func TestEveryDeclaredEdgeTypeIsEmitted(t *testing.T) {
	got := walkTheFixtureGroup(t)

	emitted := map[string]bool{}
	for _, rel := range got.Relations {
		emitted[rel.Type] = true
	}

	var missing []string
	for _, want := range glgraph.EdgeTypes() {
		if !emitted[want] {
			missing = append(missing, want)
		}
	}
	if len(missing) > 0 {
		t.Errorf("%d declared edge types are never emitted: %s", len(missing), strings.Join(missing, ", "))
	}
	if len(glgraph.EdgeTypes()) != 6 {
		t.Errorf("the declared vocabulary carries %d edge types, and the parity floor is 6",
			len(glgraph.EdgeTypes()))
	}
	for emittedType := range emitted {
		if !glgraph.IsDeclaredEdgeType(emittedType) {
			t.Errorf("the walk emitted the edge type %q, which is not in the declared vocabulary",
				emittedType)
		}
	}
}

// TestTriggeredByIsEmittedByNothing is the absence half of the floor, and it
// needs its own row because a coverage assertion that only checks presence lets a
// seventh edge type appear unnoticed.
//
// THE SAME-RUN KNOWN POSITIVE is the assertion above it: the same walk is checked
// for six types it MUST emit, so a walk that produced no edges at all cannot
// satisfy this one silently.
func TestTriggeredByIsEmittedByNothing(t *testing.T) {
	got := walkTheFixtureGroup(t)
	if len(got.Relations) == 0 {
		t.Fatal("the fixture walk emitted no edges at all; the absence below would mean nothing")
	}
	for _, rel := range got.Relations {
		if rel.Type == glgraph.EdgeTriggeredBy {
			t.Errorf("the walk emitted %s (%s -> %s). Nothing this provider returns names what "+
				"triggered a run in a form that resolves here, so an edge asserting one was invented.",
				glgraph.EdgeTriggeredBy, rel.FromID, rel.ToID)
		}
	}
}

// TestTheBelongsToFanInReachesEveryParent pins the NINE arms the relationship is
// emitted from, by LITERAL id on both ends.
//
// EIGHT OF THEM ARE THE SOURCE PROVIDER'S. The ninth — a runner to its parent —
// is this collector's own: that provider emits none, while both sibling CI/CD
// providers emit theirs, so a GitLab runner sat in the graph attached to nothing.
func TestTheBelongsToFanInReachesEveryParent(t *testing.T) {
	got := walkTheFixtureGroup(t)

	// Every pair is written out rather than built from the id helpers: a test that
	// asked the code under test what it should have produced would agree with it
	// however wrong both were.
	for _, want := range [][2]string{
		{"gitlab:acme/Project/acme/api", "gitlab:acme/Group/acme"},
		{"gitlab:acme/Pipeline/acme/api/main", "gitlab:acme/Project/acme/api"},
		{"gitlab:acme/PipelineRun/acme/api/100", "gitlab:acme/Project/acme/api"},
		{"gitlab:acme/Job/acme/api/900", "gitlab:acme/PipelineRun/acme/api/100"},
		{"gitlab:acme/Environment/acme/api/production", "gitlab:acme/Project/acme/api"},
		{"gitlab:acme/Deployment/acme/api/500", "gitlab:acme/Project/acme/api"},
		{"gitlab:acme/Variable/acme/api/API_KEY", "gitlab:acme/Project/acme/api"},
		{"gitlab:acme/Variable/acme/GROUP_DEPLOY_TOKEN", "gitlab:acme/Group/acme"},
		// The ninth and its two arms: a group-scoped runner belongs to the group,
		// a project-scoped one to the project that reached it.
		{"gitlab:acme/Runner/1", "gitlab:acme/Group/acme"},
		{"gitlab:acme/Runner/2", "gitlab:acme/Project/acme/api"},
	} {
		if !hasRelation(got, want[0], want[1], glgraph.EdgeBelongsTo) {
			t.Errorf("no BELONGS_TO edge from %q to %q", want[0], want[1])
		}
	}
}

// TestASharedRunnerIsEmittedOnceWithOneDeterministicParent is the third arm of
// the parent edge, and the one the source provider's own `seen` map forces.
//
// A runner visible to several projects is emitted ONCE, so it gets exactly ONE
// parent — and WHICH one must not depend on the order the provider happened to
// return the projects in, or two collects of an unchanged group would disagree.
func TestASharedRunnerIsEmittedOnceWithOneDeterministicParent(t *testing.T) {
	got := runOne(t, fixtureAPI(), "gitlab-runners")

	const shared = "gitlab:acme/Runner/3"
	nodes := 0
	for _, res := range got.Resources {
		if res.ID == shared {
			nodes++
		}
	}
	if nodes != 1 {
		t.Errorf("the shared runner produced %d nodes, want exactly 1; it is returned by two "+
			"projects' listings", nodes)
	}

	var parents []string
	for _, rel := range got.Relations {
		if rel.FromID == shared && rel.Type == glgraph.EdgeBelongsTo {
			parents = append(parents, rel.ToID)
		}
	}
	if len(parents) != 1 {
		t.Fatalf("the shared runner has %d parent edges (%v), want exactly 1", len(parents), parents)
	}
	// THE LOWEST-SORTING PROJECT THAT CAN SEE IT. The provider returns acme/api
	// and acme/web, and the shared discovery hands them over sorted by path.
	if want := "gitlab:acme/Project/acme/api"; parents[0] != want {
		t.Errorf("the shared runner belongs to %q, want %q — the parent must not depend on the "+
			"order the provider returned the projects in", parents[0], want)
	}
}
