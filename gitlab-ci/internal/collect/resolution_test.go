// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// resolution_test.go — DOES EVERY EDGE POINT AT SOMETHING?
//
// An edge whose endpoint names no node is not an error anywhere: the contract
// passes a dangling edge through deliberately and the write path resolves no
// endpoint, so nothing downstream notices. What it produces is a relationship no
// traversal can follow and no query returns — a promise in the graph that the
// graph cannot keep.
//
// THIS PROVIDER HAS NO STRUCTURALLY DANGLING CLASS. Every edge target's id form
// has a resource type minted under the same form, including the runner-parent
// edge this collector adds. What it does have is THREE CONDITIONAL classes, whose
// resolution depends on what a pipeline document says or on what two separate API
// calls happened to return, and each of them is asserted AS dangling so that an
// accidental later fix is visible rather than silent.
//
// TWO ENDS ARE CHECKED, NOT ONE. The approval edge is the only relationship in
// this graph whose dangling side is the SOURCE — the rule comes from one call and
// the environment from another — so a resolution test that only looked at targets
// would report it clean.

// TestEveryEmittedEdgeResolvesExceptTheThreeConditionalClasses is the whole-graph
// row.
func TestEveryEmittedEdgeResolvesExceptTheThreeConditionalClasses(t *testing.T) {
	got := walkTheFixtureGroup(t)
	nodes := resourceIDs(got)

	// The endpoints expected NOT to resolve, written as literals. Naming them
	// exactly is what keeps this row honest: a fourth dangling class appearing
	// later fails here instead of being absorbed by a pattern.
	danglingTargets := []string{
		// A pipeline referencing a GROUP-scoped variable. The document names the
		// variable by name alone, so the target is built at project scope; the
		// group-scoped node exists in this same result under a different owner
		// segment, which is why this assertion names the exact target STRING
		// rather than the variable's name.
		"gitlab:acme/Variable/acme/api/GROUP_DEPLOY_TOKEN",
		// A job asking for a runner tag no discovered runner carries. The tag node
		// is minted from a runner's own tag list, so this is the truthful record of
		// a job that cannot be scheduled.
		"gitlab:acme/RunnerTag/deploy-only",
	}
	danglingSources := []string{
		// A protected environment the environments enumeration did not return. The
		// approval edge runs FROM the environment, so what dangles is its source.
		"gitlab:acme/Environment/acme/api/canary",
	}

	var unresolved []string
	for _, rel := range got.Relations {
		if _, ok := nodes[rel.ToID]; !ok && !slices.Contains(danglingTargets, rel.ToID) {
			unresolved = append(unresolved, rel.Type+" -> "+rel.ToID)
		}
		if _, ok := nodes[rel.FromID]; !ok && !slices.Contains(danglingSources, rel.FromID) {
			unresolved = append(unresolved, rel.FromID+" -> "+rel.Type)
		}
	}
	if len(unresolved) > 0 {
		t.Errorf("%d edges name an endpoint no node in the same result carries:\n  %s",
			len(unresolved), strings.Join(unresolved, "\n  "))
	}

	// THE KNOWN POSITIVES for the loop above: each class that IS expected to
	// dangle must actually be present and actually dangle. Without them, a walk
	// that emitted none of these edges at all would pass the assertion above while
	// proving nothing about resolution.
	for _, want := range danglingTargets {
		assertPresentAndDangling(t, got, nodes, want, false)
	}
	for _, want := range danglingSources {
		assertPresentAndDangling(t, got, nodes, want, true)
	}

	// AND THE GROUP-SCOPED VARIABLE NODE DOES EXIST, under its own id. This is the
	// distinction the looser assertion would get wrong: what dangles is the
	// pipeline edge's TARGET STRING, not the variable.
	if _, ok := nodes["gitlab:acme/Variable/acme/GROUP_DEPLOY_TOKEN"]; !ok {
		t.Error("the group-scoped variable node is missing, so the row above is asserting the " +
			"absence of a node rather than the shape of an id")
	}
	// And the protection rule the dangling-source edge points AT does exist, which
	// is what makes that class a missing environment rather than a missing rule.
	if _, ok := nodes["gitlab:acme/ProtectionRule/acme/api/canary"]; !ok {
		t.Error("the canary protection rule is missing, so the dangling-source class was never " +
			"exercised")
	}
}

// assertPresentAndDangling is the known positive for one expected-dangling
// endpoint: it must be named by at least one edge, and it must name no node.
func assertPresentAndDangling(
	t *testing.T, got glgraph.Result, nodes map[string]string, endpoint string, isSource bool,
) {
	t.Helper()
	present := slices.ContainsFunc(got.Relations, func(rel glgraph.Relation) bool {
		if isSource {
			return rel.FromID == endpoint
		}
		return rel.ToID == endpoint
	})
	if !present {
		t.Errorf("the fixture emitted no edge naming %q, so the dangling class this row exists to "+
			"pin was never exercised", endpoint)
	}
	if _, resolved := nodes[endpoint]; resolved {
		t.Errorf("%q now resolves. That is a behavior change rather than a bug fix: the graph a "+
			"consumer already reads carries this edge unresolved, so a change here is a decision "+
			"to record and not a silent improvement", endpoint)
	}
}

// TestTheTagEdgesResolveToOneNodePerDistinctTag is the first deviation from the
// source provider, and it carries the four arms the distinctness rule has.
func TestTheTagEdgesResolveToOneNodePerDistinctTag(t *testing.T) {
	got := runOne(t, fixtureAPI(), "gitlab-runners")

	// ARM 1: the distinct SET of tag ids. Counted over the resource list rather
	// than over a set, because a set deduplicates the very thing this row asserts.
	minted := map[string]int{}
	for _, res := range got.Resources {
		if res.ResourceType == glgraph.ResourceTypeRunnerTag {
			minted[res.ID]++
		}
	}
	distinct := make([]string, 0, len(minted))
	for id := range minted {
		distinct = append(distinct, id)
	}
	slices.Sort(distinct)
	want := []string{
		"gitlab:acme/RunnerTag/arm64",
		"gitlab:acme/RunnerTag/docker",
		"gitlab:acme/RunnerTag/linux",
		"gitlab:acme/RunnerTag/shared",
	}
	if !slices.Equal(distinct, want) {
		t.Errorf("the walk minted the tag nodes %v, want exactly %v", distinct, want)
	}

	// ARM 2: a runner carrying THREE tags produces three nodes and three edges.
	const groupRunner = "gitlab:acme/Runner/1"
	edgesFromGroupRunner := 0
	for _, rel := range got.Relations {
		if rel.FromID == groupRunner && rel.Type == glgraph.EdgeHasLabel {
			edgesFromGroupRunner++
		}
	}
	if edgesFromGroupRunner != 3 {
		t.Errorf("the group runner carries 3 tags and produced %d HAS_LABEL edges",
			edgesFromGroupRunner)
	}

	// ARM 3: the shared tag is reached by BOTH runners, at two different scopes.
	// That is what proves the id carries no project — a per-runner or per-project
	// tag id would produce two nodes here and this would pass while the graph
	// carried a tag node per runner.
	const shared = "gitlab:acme/RunnerTag/docker"
	for _, runner := range []string{"gitlab:acme/Runner/1", "gitlab:acme/Runner/2"} {
		if !hasRelation(got, runner, shared, glgraph.EdgeHasLabel) {
			t.Errorf("no HAS_LABEL edge from %q to %q", runner, shared)
		}
	}
	if minted[shared] != 2 {
		t.Errorf("the shared tag was minted %d time(s) before the builder deduplicates; two "+
			"runners carry it, so the raw walk mints it twice and the builder is what keeps one",
			minted[shared])
	}

	// ARM 4: after the builder deduplicates, ONE node stands for the shared tag
	// and BOTH edges into it survive.
	nodes, edges, err := glgraph.Build(got)
	if err != nil {
		t.Fatalf("building the fixture walk: %v", err)
	}
	built := 0
	for _, node := range nodes {
		if node.ID == shared {
			built++
		}
	}
	if built != 1 {
		t.Errorf("the built graph carries %d nodes with the id %q, want exactly 1", built, shared)
	}
	into := 0
	for _, edge := range edges {
		if edge.ToID == shared && edge.Type == glgraph.EdgeHasLabel {
			into++
		}
	}
	if into != 2 {
		t.Errorf("the built graph carries %d HAS_LABEL edges into %q, want 2 — one per runner "+
			"carrying it", into, shared)
	}
}

// TestARunnerWithNoTagsMintsNothing is the empty arm of the same rule.
func TestARunnerWithNoTagsMintsNothing(t *testing.T) {
	api := fixtureAPI()
	for id, details := range api.runnerDetails {
		details.TagList = nil
		api.runnerDetails[id] = details
	}

	got := runOne(t, api, "gitlab-runners")
	for _, res := range got.Resources {
		if res.ResourceType == glgraph.ResourceTypeRunnerTag {
			t.Errorf("a walk whose runners carry no tags minted the tag node %q", res.ID)
		}
	}
	for _, rel := range got.Relations {
		if rel.Type == glgraph.EdgeHasLabel {
			t.Errorf("a walk whose runners carry no tags emitted %s -> %s", rel.FromID, rel.ToID)
		}
	}
	// The known positive: the runners themselves were still enumerated, so this is
	// a walk that found runners and no tags rather than one that found nothing.
	if countByType(got, glgraph.ResourceTypeRunner) == 0 {
		t.Fatal("the walk emitted no runners either; the absence above means nothing")
	}
}

// TestARunnerTagWithNoNameMintsNothing pins the one value the tag reader drops. A
// node keyed on an empty name would be one node standing for every unnamed tag in
// the group.
func TestARunnerTagWithNoNameMintsNothing(t *testing.T) {
	api := fixtureAPI()
	api.runnerDetails[1].TagList = []string{"", "kept"}

	got := runOne(t, api, "gitlab-runners")
	ids := resourceIDs(got)
	if _, minted := ids["gitlab:acme/RunnerTag/"]; minted {
		t.Error("a tag with no name minted a node keyed on the empty name")
	}
	if _, minted := ids["gitlab:acme/RunnerTag/kept"]; !minted {
		t.Error("the named tag beside it was dropped too; the assertion above is not measuring " +
			"the empty name")
	}
}
