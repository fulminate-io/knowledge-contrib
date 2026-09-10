// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"slices"
	"strings"
	"testing"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// resolution_test.go — DOES EVERY EDGE POINT AT SOMETHING?
//
// An edge whose target names no node is not an error anywhere: the contract
// passes a dangling edge through deliberately and the write path resolves no
// endpoint, so nothing downstream notices. What it produces is a relationship no
// traversal can follow and no query returns — a promise in the graph that the
// graph cannot keep.
//
// THE SOURCE PROVIDER LEFT TWO WHOLE CLASSES DANGLING BY CONSTRUCTION: every one
// of its approval edges named a reviewer node it never created, and every one of
// its label edges named a label node it never created. This collector
// materializes both, and the two rows below are what prove it rather than
// asserting it.
//
// TWO CLASSES STILL DANGLE, AND THEY ARE ASSERTED AS DANGLING so that an
// accidental later fix is visible rather than silent. Both are CONDITIONAL rather
// than structural: whether they resolve depends on what a workflow's text says,
// not on a node class nothing ever creates. See the notes on each.

// TestEveryEmittedEdgeResolvesExceptTheTwoConditionalClasses is the whole-graph
// row.
func TestEveryEmittedEdgeResolvesExceptTheTwoConditionalClasses(t *testing.T) {
	got := walkTheFixtureOrganization(t)
	nodes := resourceIDs(got)

	// The two target ids that are expected NOT to resolve, written as literals.
	// Naming them exactly is what keeps this row honest: a third dangling class
	// appearing later fails here instead of being absorbed by a pattern.
	expectedDangling := []string{
		// A workflow referencing an ORGANIZATION-scoped secret. The workflow's
		// text names the secret by name alone, so the target is built at
		// repository scope; the org-scoped node exists in this same result under
		// a shorter id, which is why this assertion names the exact target string
		// rather than the secret's name.
		"github:acme/Secret/acme/api/repo/ORG_TOKEN",
		// A workflow deploying to an environment the environments enumeration did
		// not return. The text is read from the file and the environments come
		// from the API; nothing makes the two agree, so a workflow can name an
		// environment that was deleted, is created later, or is written as an
		// expression the provider evaluates when the workflow runs.
		"github:acme/Environment/acme/web/canary",
	}

	var unresolved []string
	for _, rel := range got.Relations {
		if _, ok := nodes[rel.ToID]; ok {
			continue
		}
		if slices.Contains(expectedDangling, rel.ToID) {
			continue
		}
		unresolved = append(unresolved, rel.Type+" -> "+rel.ToID)
	}
	if len(unresolved) > 0 {
		t.Errorf("%d edges name a target no node in the same result carries:\n  %s",
			len(unresolved), strings.Join(unresolved, "\n  "))
	}

	// THE KNOWN POSITIVE for the loop above: the class that IS expected to
	// dangle must actually be present and actually dangle. Without this, a walk
	// that emitted no USES_SECRET edges at all would pass the assertion above
	// while proving nothing about resolution.
	for _, want := range expectedDangling {
		if !slices.ContainsFunc(got.Relations, func(rel ghgraph.Relation) bool {
			return rel.ToID == want
		}) {
			t.Errorf("the fixture emitted no edge to %q, so the dangling class this row exists to "+
				"pin was never exercised", want)
		}
		if _, resolved := nodes[want]; resolved {
			t.Errorf("%q now resolves. That is a behavior change rather than a bug fix: the graph a "+
				"consumer already reads carries this edge unresolved, so a change here is a "+
				"decision to record and not a silent improvement", want)
		}
	}

	// AND THE ORGANIZATION-SCOPED SECRET NODE DOES EXIST, under its own id. This
	// is the distinction the looser assertion would get wrong: what dangles is
	// the workflow edge's TARGET STRING, not the secret.
	if _, ok := nodes["github:acme/Secret/org/ORG_TOKEN"]; !ok {
		t.Error("the organization-scoped secret node is missing, so the row above is asserting the " +
			"absence of a node rather than the shape of an id")
	}
}

// TestTheApprovalEdgesResolveToMaterializedReviewers is the first of the two
// materialization rows, by literal id on both ends.
func TestTheApprovalEdgesResolveToMaterializedReviewers(t *testing.T) {
	got := walkTheFixtureOrganization(t)
	nodes := resourceIDs(got)

	const environment = "github:acme/Environment/acme/api/production"
	for _, reviewer := range []struct{ id, resourceType string }{
		{"github:acme/User/ada", ghgraph.ResourceTypeUser},
		{"github:acme/Team/platform", ghgraph.ResourceTypeTeam},
	} {
		if !hasRelation(got, environment, reviewer.id, ghgraph.EdgeRequiresApproval) {
			t.Errorf("no REQUIRES_APPROVAL edge from %q to %q", environment, reviewer.id)
		}
		if nodes[reviewer.id] != reviewer.resourceType {
			t.Errorf("%q is a %q node, want %q — the edge names a node that is not there, which is "+
				"the defect this materialization exists to fix",
				reviewer.id, nodes[reviewer.id], reviewer.resourceType)
		}
	}

	// A protection rule that names nobody produces no approval edge. The fixture
	// carries a wait timer beside the reviewers rule for exactly this.
	for _, rel := range got.Relations {
		if rel.Type != ghgraph.EdgeRequiresApproval {
			continue
		}
		if rel.ToID != "github:acme/User/ada" && rel.ToID != "github:acme/Team/platform" {
			t.Errorf("an approval edge names %q, which no protection rule in the fixture names",
				rel.ToID)
		}
	}
}

// TestTheLabelEdgesResolveToOneNodePerDistinctLabel is the second, and it
// carries the four arms the distinctness rule has.
func TestTheLabelEdgesResolveToOneNodePerDistinctLabel(t *testing.T) {
	got := walkTheFixtureOrganization(t)

	// ARM 1 and 2: the two labels the fixture's runners carry are two nodes, and
	// the shared one is ONE node however many runners carry it. Counted over the
	// resource list rather than over a set, because a set deduplicates the very
	// thing this row is asserting.
	labelNodes := map[string]int{}
	for _, res := range got.Resources {
		if res.ResourceType == ghgraph.ResourceTypeLabel {
			labelNodes[res.ID]++
		}
	}
	// The RAW count carries duplicates by design — the graph builder is what
	// deduplicates — so what is asserted here is the SET of distinct label ids.
	distinct := make([]string, 0, len(labelNodes))
	for id := range labelNodes {
		distinct = append(distinct, id)
	}
	slices.Sort(distinct)
	if want := []string{"github:acme/Label/linux", "github:acme/Label/self-hosted"}; !slices.Equal(distinct, want) {
		t.Errorf("the walk minted the label nodes %v, want exactly %v", distinct, want)
	}

	// ARM 3: the shared label is reached by BOTH runners, at two different
	// scopes. That is what proves the id carries no repository — a per-runner or
	// per-repository label id would produce two nodes here and this would pass
	// while the graph carried a label per runner.
	const shared = "github:acme/Label/self-hosted"
	for _, runner := range []string{"github:acme/Runner/1", "github:acme/Runner/2"} {
		if !hasRelation(got, runner, shared, ghgraph.EdgeHasLabel) {
			t.Errorf("no HAS_LABEL edge from %q to %q", runner, shared)
		}
	}

	// ARM 4: after the builder deduplicates, ONE node stands for the shared label
	// and BOTH edges into it survive.
	nodes, edges, err := ghgraph.Build(got)
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
		if edge.ToID == shared && edge.Type == ghgraph.EdgeHasLabel {
			into++
		}
	}
	if into != 2 {
		t.Errorf("the built graph carries %d HAS_LABEL edges into %q, want 2 — one per runner "+
			"carrying it", into, shared)
	}
}

// TestARunnerWithNoLabelsMintsNothing is the empty arm of the same rule.
func TestARunnerWithNoLabelsMintsNothing(t *testing.T) {
	repos, actions := fixtureAPI()
	for _, runner := range actions.orgRunners[0] {
		runner.Labels = nil
	}
	for _, page := range actions.repoRunners["acme/api"] {
		for _, runner := range page {
			runner.Labels = nil
		}
	}

	got := runOne(t, repos, actions, "github-runners")
	for _, res := range got.Resources {
		if res.ResourceType == ghgraph.ResourceTypeLabel {
			t.Errorf("a walk whose runners carry no labels minted the label node %q", res.ID)
		}
	}
	for _, rel := range got.Relations {
		if rel.Type == ghgraph.EdgeHasLabel {
			t.Errorf("a walk whose runners carry no labels emitted %s -> %s", rel.FromID, rel.ToID)
		}
	}
	// The known positive: the runners themselves were still enumerated, so this
	// is a walk that found runners and no labels rather than one that found
	// nothing.
	if len(got.Resources) == 0 {
		t.Fatal("the walk emitted no runners either; the absence above means nothing")
	}
}

// TestARunnerLabelWithNoNameMintsNothing pins the one value the label reader
// drops. A node keyed on an empty name would be one node standing for every
// unnamed label in the organization.
func TestARunnerLabelWithNoNameMintsNothing(t *testing.T) {
	repos, actions := fixtureAPI()
	actions.orgRunners[0][0].Labels = []*gogithub.RunnerLabels{
		{Name: new("")},
		{Name: new("kept")},
	}

	got := runOne(t, repos, actions, "github-runners")
	ids := resourceIDs(got)
	if _, minted := ids["github:acme/Label/"]; minted {
		t.Error("a label with no name minted a node keyed on the empty name")
	}
	if _, minted := ids["github:acme/Label/kept"]; !minted {
		t.Error("the named label beside it was dropped too; the assertion above is not measuring " +
			"the empty name")
	}
}

// TestBothDeploysToEmissionSitesAreEmitted is the per-SITE row for the one edge
// type with two producers.
//
// WHY A TYPE-LEVEL ROW IS NOT ENOUGH HERE, and this is the shape that fell
// through every other assertion in this suite. TestEveryDeclaredEdgeTypeIsEmitted
// asks whether the type appears anywhere in the walk, so with two producers
// EITHER ONE ALONE satisfies it. The resolution row asserts that every edge that
// IS emitted resolves, which says nothing about one that stopped being emitted.
// And the single-repository row asserts node presence only. Measured on this
// tree before this test existed: deleting the workflow's whole environment
// emission block left every package green.
//
// SO EACH SITE IS NAMED BY ITS ENDPOINTS, both as literals, the way the
// BELONGS_TO fan-in row already writes its ten arms. A helper on either side
// would let the two agree on the same wrong string.
func TestBothDeploysToEmissionSitesAreEmitted(t *testing.T) {
	got := walkTheFixtureOrganization(t)

	for _, want := range []struct{ what, from, to string }{
		{
			// The deployment's own edge. Its target is an environment the
			// environments enumeration returned for the same repository, so it
			// resolves.
			"a deployment deploys to the environment it named",
			"github:acme/Deployment/acme/api/500",
			"github:acme/Environment/acme/api/production",
		},
		{
			// The workflow's parsed edge, from the text of its definition. A
			// different producer of the same type, and the one no test observed.
			"a workflow deploys to the environment its text names",
			"github:acme/Workflow/acme/api/.github/workflows/ci.yml",
			"github:acme/Environment/acme/api/production",
		},
		{
			// The second workflow, whose environment nothing returns. It is
			// emitted all the same — that is the carried-forward behavior — and
			// its target is asserted as dangling in the row above.
			"a workflow deploys to an environment the API did not return",
			"github:acme/Workflow/acme/web/.github/workflows/release.yml",
			"github:acme/Environment/acme/web/canary",
		},
	} {
		if !hasRelation(got, want.from, want.to, ghgraph.EdgeDeploysTo) {
			t.Errorf("%s: no DEPLOYS_TO edge from %q to %q", want.what, want.from, want.to)
		}
	}

	// AND THE COUNT, so a site that started emitting the edge twice, or a fourth
	// producer nobody meant to add, is a red rather than a silent addition.
	emitted := 0
	for _, rel := range got.Relations {
		if rel.Type == ghgraph.EdgeDeploysTo {
			emitted++
		}
	}
	if emitted != 3 {
		t.Errorf("the walk emitted %d DEPLOYS_TO edges over this fixture, want 3 — one from the "+
			"deployment and one from each of the two workflows whose text names an environment",
			emitted)
	}
}
