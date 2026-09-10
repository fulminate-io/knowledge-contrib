// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"testing"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// enumerations_test.go — the INPUT CLASSES each enumeration is specified over:
// the empty group, one project of everything, the pagination boundary, the two
// caps, the archived project, the project with no default branch, and the
// subgroup the recursion reaches.
//
// EVERY ID IS ASSERTED AGAINST A LITERAL. There is no id helper called on the
// expectation side anywhere in this file: every node id in this graph is built by
// a helper the code under test also calls, so an expectation built the same way
// would agree with a wrong spelling on both sides.

// TestAnEmptyGroupEmitsItsGroupNodeAndIsComplete. An empty read is not an
// incomplete one.
func TestAnEmptyGroupEmitsItsGroupNodeAndIsComplete(t *testing.T) {
	empty := &fakeAPI{
		groupProjects: map[string]pages[gl.Project]{fixtureGroup: {{}}},
		subgroups:     map[string]pages[gl.Group]{},
		runnerDetails: map[int64]*gl.RunnerDetails{},
	}

	got, errs, listerPartial := runAll(t, empty)
	for name, err := range errs {
		if err != nil {
			t.Errorf("%s over an empty group reported %v; a group with nothing in it is a "+
				"complete read of nothing", name, err)
		}
	}
	if len(listerPartial) != 0 {
		t.Errorf("the discovery over an empty group reported %v", listerPartial)
	}

	ids := resourceIDs(got)
	if _, ok := ids["gitlab:acme/Group/acme"]; !ok {
		t.Error("the group node was not emitted; it is unconditional and does not depend on the " +
			"group containing anything")
	}
	if len(ids) != 1 {
		t.Errorf("an empty group emitted %d nodes, want exactly the group node: %v", len(ids), ids)
	}
	if len(got.Relations) != 0 {
		t.Errorf("an empty group emitted %d edges", len(got.Relations))
	}
}

// TestAGroupLevelRunnerInAnEmptyGroupStillGetsItsParentEdge is the one
// interaction the empty arm has with the runner parent edge: the group node is
// unconditional, so the edge resolves even with no projects at all.
func TestAGroupLevelRunnerInAnEmptyGroupStillGetsItsParentEdge(t *testing.T) {
	api := &fakeAPI{
		groupProjects: map[string]pages[gl.Project]{fixtureGroup: {{}}},
		subgroups:     map[string]pages[gl.Group]{},
		groupRunners:  pages[gl.Runner]{{{ID: 7, Status: "online"}}},
		runnerDetails: map[int64]*gl.RunnerDetails{7: {ID: 7}},
	}

	got := runOne(t, api, "gitlab-runners")
	if !hasRelation(got, "gitlab:acme/Runner/7", "gitlab:acme/Group/acme", glgraph.EdgeBelongsTo) {
		t.Error("a group-level runner in a group with no projects has no parent edge")
	}
}

// TestOneProjectResolvesTheWholeOwnershipChain walks the two chains end to end by
// literal id: job to run to project to group, and project-level runner to project
// to group.
func TestOneProjectResolvesTheWholeOwnershipChain(t *testing.T) {
	got := walkTheFixtureGroup(t)
	nodes := resourceIDs(got)

	for _, hop := range [][2]string{
		{"gitlab:acme/Job/acme/api/900", "gitlab:acme/PipelineRun/acme/api/100"},
		{"gitlab:acme/PipelineRun/acme/api/100", "gitlab:acme/Project/acme/api"},
		{"gitlab:acme/Project/acme/api", "gitlab:acme/Group/acme"},
		{"gitlab:acme/Runner/2", "gitlab:acme/Project/acme/api"},
	} {
		if !hasRelation(got, hop[0], hop[1], glgraph.EdgeBelongsTo) {
			t.Errorf("the ownership chain breaks at %q -> %q", hop[0], hop[1])
		}
		for _, end := range hop {
			if _, ok := nodes[end]; !ok {
				t.Errorf("the chain names %q, which no node in the same result carries", end)
			}
		}
	}
}

// TestTheSubgroupRecursionReachesItsProjects. A group's projects are not only its
// own, and a collector that read one level would silently inventory a fraction of
// the group.
func TestTheSubgroupRecursionReachesItsProjects(t *testing.T) {
	got := runOne(t, fixtureAPI(), "gitlab-projects")
	if _, ok := resourceIDs(got)["gitlab:acme/Project/acme/platform/infra"]; !ok {
		t.Error("the project inside the subgroup was never enumerated")
	}
	// The known positive: the top-level projects are there too, so this is a
	// recursion that reached deeper rather than one that replaced the first level.
	if _, ok := resourceIDs(got)["gitlab:acme/Project/acme/api"]; !ok {
		t.Error("the group's own projects are missing")
	}
}

// TestAnArchivedProjectIsEmittedAndWalked. The source provider records the flag
// and walks the project anyway; the sibling GitHub collector skips its archived
// repositories, and inheriting that here would silently drop a project's whole
// inventory.
func TestAnArchivedProjectIsEmittedAndWalked(t *testing.T) {
	got := walkTheFixtureGroup(t)

	const archived = "gitlab:acme/Project/acme/legacy"
	res, ok := resourceByID(got, archived)
	if !ok {
		t.Fatalf("the archived project %q produced no node. A test asserting it is SKIPPED would "+
			"be red on a correct build: this provider records the flag and walks the project",
			archived)
	}
	if res.Metadata["archived"] != "true" {
		t.Errorf("the archived project's metadata carries archived=%q, want \"true\"",
			res.Metadata["archived"])
	}
	// AND IT IS WALKED BY THE OTHER ENUMERATIONS: its pipeline definition was read.
	if _, ok := resourceIDs(got)["gitlab:acme/Pipeline/acme/legacy/master"]; !ok {
		t.Error("the archived project's pipeline definition was never read, so the project node " +
			"is emitted but the project is not actually walked")
	}
	// The control: an ACTIVE project carries no archived key at all, so the
	// assertion above is reading a value rather than a default.
	active, ok := resourceByID(got, "gitlab:acme/Project/acme/api")
	if !ok {
		t.Fatal("the active project is missing")
	}
	if _, present := active.Metadata["archived"]; present {
		t.Errorf("an active project carries archived=%q", active.Metadata["archived"])
	}
}

// TestAProjectWithNoDefaultBranchIsSkippedByThePipelinesEnumerationOnly. There is
// no ref to read a definition from, and that is not a failure — but the project
// itself is still inventoried.
func TestAProjectWithNoDefaultBranchIsSkippedByThePipelinesEnumerationOnly(t *testing.T) {
	got := walkTheFixtureGroup(t)
	ids := resourceIDs(got)

	if _, ok := ids["gitlab:acme/Project/acme/empty"]; !ok {
		t.Error("a project with no default branch produced no project node")
	}
	for id := range ids {
		if id == "gitlab:acme/Pipeline/acme/empty/" {
			t.Error("a project with no default branch produced a pipeline node keyed on an empty ref")
		}
	}
}
