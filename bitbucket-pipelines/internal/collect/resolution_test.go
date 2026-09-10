// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
)

// resolution_test.go — DOES EVERY EDGE POINT AT SOMETHING?
//
// An edge whose target names no node is not an error anywhere: the contract
// passes a dangling edge through deliberately and the write path resolves no
// endpoint, so nothing downstream notices. What it produces is a relationship no
// traversal can follow and no query returns — a promise in the graph that the
// graph cannot keep.
//
// THE SOURCE PROVIDER LEFT ONE WHOLE CLASS DANGLING BY CONSTRUCTION: every one
// of its USES_SECRET edges named a variable id one segment shorter than any
// variable node it wrote, so no input could make the two equal. This collector
// emits the target in the shape of its own variable ids, which is the recorded
// deviation, and the rows below are what prove it rather than asserting it.

// TestEveryEmittedEdgeResolves is the whole-graph row.
//
// THERE IS NO EXPECTED-DANGLING LIST HERE, and its absence is the measurement:
// this collector's every edge names a node the same walk carries.
func TestEveryEmittedEdgeResolves(t *testing.T) {
	got := walkTheFixture(t)
	nodes := resourceIDs(got)

	if len(got.Relations) == 0 {
		t.Fatal("the fixture walk emitted no edges; the assertion below would mean nothing")
	}
	var unresolved []string
	for _, rel := range got.Relations {
		if _, ok := nodes[rel.ToID]; !ok {
			unresolved = append(unresolved, rel.Type+" -> "+rel.ToID)
		}
		if _, ok := nodes[rel.FromID]; !ok {
			unresolved = append(unresolved, rel.Type+" <- "+rel.FromID)
		}
	}
	if len(unresolved) > 0 {
		t.Errorf("%d edges name an endpoint no node in the same result carries:\n  %s",
			len(unresolved), strings.Join(unresolved, "\n  "))
	}
}

// TestTheUsesSecretTargetTakesTheMostSpecificScope carries all four arms of the
// resolution rule, by LITERAL id.
func TestTheUsesSecretTargetTakesTheMostSpecificScope(t *testing.T) {
	got := walkTheFixture(t)

	const (
		deployPipeline = "bitbucket:acme/Pipeline/api/branches/trunk"
		buildPipeline  = "bitbucket:acme/Pipeline/api/default"
	)

	// ARM 1: DEPLOY_KEY exists at all three scopes and the step names a
	// deployment, so the ENVIRONMENT-scoped variable wins.
	if !hasRelation(got, deployPipeline,
		"bitbucket:acme/Variable/env/api/production/DEPLOY_KEY", bbgraph.EdgeUsesSecret) {
		t.Error("the deployment step's $DEPLOY_KEY did not resolve to the environment-scoped variable")
	}
	// And the two less specific candidates are NOT also targeted: one reference
	// is one edge.
	for _, shadowed := range []string{
		"bitbucket:acme/Variable/repository/api/DEPLOY_KEY",
		"bitbucket:acme/Variable/workspace/DEPLOY_KEY",
	} {
		if hasRelation(got, deployPipeline, shadowed, bbgraph.EdgeUsesSecret) {
			t.Errorf("the deployment step also targeted the shadowed variable %q", shadowed)
		}
	}

	// ARM 2: API_KEY exists at the repository and nowhere more specific, so the
	// REPOSITORY-scoped variable wins — from the deployment step too, which shows
	// the precedence is per REFERENCE and not per step.
	for _, from := range []string{buildPipeline, deployPipeline} {
		if !hasRelation(got, from,
			"bitbucket:acme/Variable/repository/api/API_KEY", bbgraph.EdgeUsesSecret) {
			t.Errorf("%s: $API_KEY did not resolve to the repository-scoped variable", from)
		}
	}

	// ARM 3: WORKSPACE_TOKEN exists only at the workspace.
	if !hasRelation(got, buildPipeline,
		"bitbucket:acme/Variable/workspace/WORKSPACE_TOKEN", bbgraph.EdgeUsesSecret) {
		t.Error("$WORKSPACE_TOKEN did not resolve to the workspace-scoped variable")
	}

	// ARM 4: MISSING_VAR exists nowhere, so NO edge is emitted and the reference
	// is recorded on the pipeline node instead of vanishing.
	for _, rel := range got.Relations {
		if rel.Type == bbgraph.EdgeUsesSecret && strings.HasSuffix(rel.ToID, "/MISSING_VAR") {
			t.Errorf("an unresolvable reference produced the edge %s -> %s", rel.FromID, rel.ToID)
		}
	}
	pipeline, ok := resourceByID(got, buildPipeline)
	if !ok {
		t.Fatalf("the fixture emitted no pipeline node %q", buildPipeline)
	}
	if pipeline.Metadata[bbgraph.UnresolvedRefsKey] != "MISSING_VAR" {
		t.Errorf("the pipeline records %q under %q, want %q — an omitted edge that is also "+
			"unrecorded is invisible, which is the state this key exists to end",
			pipeline.Metadata[bbgraph.UnresolvedRefsKey], bbgraph.UnresolvedRefsKey, "MISSING_VAR")
	}
	// The control for arm 4: a pipeline whose references ALL resolved carries no
	// such key, so the key is not written unconditionally.
	deploy, ok := resourceByID(got, deployPipeline)
	if !ok {
		t.Fatalf("the fixture emitted no pipeline node %q", deployPipeline)
	}
	if _, present := deploy.Metadata[bbgraph.UnresolvedRefsKey]; present {
		t.Errorf("a pipeline whose every reference resolved still carries %q: %q",
			bbgraph.UnresolvedRefsKey, deploy.Metadata[bbgraph.UnresolvedRefsKey])
	}
}

// TestEveryUsesSecretTargetIsAVariableThisWalkEmitted is the property row behind
// the four arms above: whatever the precedence chose, it chose from this walk's
// own variables.
func TestEveryUsesSecretTargetIsAVariableThisWalkEmitted(t *testing.T) {
	got := walkTheFixture(t)
	nodes := resourceIDs(got)

	var seen int
	for _, rel := range got.Relations {
		if rel.Type != bbgraph.EdgeUsesSecret {
			continue
		}
		seen++
		if nodes[rel.ToID] != bbgraph.ResourceTypeVariable {
			t.Errorf("the USES_SECRET edge %s -> %s targets a %q node, want a variable — the "+
				"source provider's target shape was one segment short and matched none",
				rel.FromID, rel.ToID, nodes[rel.ToID])
		}
	}
	if seen == 0 {
		t.Fatal("the fixture walk emitted no USES_SECRET edges; the assertion above means nothing")
	}
}

// TestTheProviderBuiltinsAreNotReferences. A step's script mentions $HOME, and a
// collector that treated it as a variable reference would record it as an
// unresolved secret on every pipeline in every workspace.
func TestTheProviderBuiltinsAreNotReferences(t *testing.T) {
	got := walkTheFixture(t)
	pipeline, ok := resourceByID(got, "bitbucket:acme/Pipeline/api/default")
	if !ok {
		t.Fatal("the fixture emitted no default pipeline")
	}
	if strings.Contains(pipeline.Metadata[bbgraph.UnresolvedRefsKey], "HOME") {
		t.Errorf("the shell built-in $HOME was carried as a variable reference: %q",
			pipeline.Metadata[bbgraph.UnresolvedRefsKey])
	}
	// The known positive in the same field: a real unresolved reference IS there,
	// so this is a filter that fired rather than a field that is always empty.
	if !strings.Contains(pipeline.Metadata[bbgraph.UnresolvedRefsKey], "MISSING_VAR") {
		t.Errorf("no unresolved reference is recorded at all, so the absence of HOME says nothing")
	}
}

// TestTheLabelEdgesResolveToOneNodePerDistinctLabel is the label dedup row, and
// it carries the four arms the distinctness rule has.
func TestTheLabelEdgesResolveToOneNodePerDistinctLabel(t *testing.T) {
	got := walkTheFixture(t)

	// ARM 1 and 2: the two named labels the fixture's runners carry are two
	// distinct ids, and the shared one is ONE id however many runners carry it.
	distinct := map[string]bool{}
	for _, res := range got.Resources {
		if res.ResourceType == bbgraph.ResourceTypeLabel {
			distinct[res.ID] = true
		}
	}
	ids := make([]string, 0, len(distinct))
	for id := range distinct {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	want := []string{"bitbucket:acme/Label/linux", "bitbucket:acme/Label/self-hosted"}
	if !slices.Equal(ids, want) {
		t.Errorf("the walk minted the label ids %v, want exactly %v", ids, want)
	}

	// ARM 3: the shared label is reached by runners at BOTH scopes. That is what
	// proves the id carries no repository — a per-runner or per-repository label
	// id would produce a node each and this would pass while the graph carried a
	// label per runner.
	const shared = "bitbucket:acme/Label/self-hosted"
	for _, runner := range []string{
		"bitbucket:acme/Runner/{runner-ws}",
		"bitbucket:acme/Runner/{runner-ws-2}",
		"bitbucket:acme/Runner/{runner-api}",
	} {
		if !hasRelation(got, runner, shared, bbgraph.EdgeHasLabel) {
			t.Errorf("no HAS_LABEL edge from %q to %q", runner, shared)
		}
	}

	// ARM 4: after the builder deduplicates, ONE node stands for the shared label
	// and all three edges into it survive. This is the arm that fails on the
	// source provider's behavior, which minted one resource per (runner, label).
	nodes, edges, err := bbgraph.Build(got)
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
		if edge.ToID == shared && edge.Type == bbgraph.EdgeHasLabel {
			into++
		}
	}
	if into != 3 {
		t.Errorf("the built graph carries %d HAS_LABEL edges into %q, want 3 — one per runner "+
			"carrying it", into, shared)
	}
}

// TestARunnerLabelWithNoNameMintsNothing pins the one value the label reader
// drops. A node keyed on an empty name would be one node standing for every
// unnamed label in the workspace, and it would also be an id ending in a slash
// that a pipeline's `runs-on` could never name.
func TestARunnerLabelWithNoNameMintsNothing(t *testing.T) {
	got := walkTheFixture(t)
	ids := resourceIDs(got)

	if _, minted := ids["bitbucket:acme/Label/"]; minted {
		t.Error("a label the provider returned with no name minted a node keyed on the empty name")
	}
	// The known positive: the named label beside it on the SAME runner was kept,
	// so this is a reader that dropped one value rather than one that dropped the
	// runner.
	if _, minted := ids["bitbucket:acme/Label/self-hosted"]; !minted {
		t.Error("the named label on the same runner was dropped too; the assertion above is not " +
			"measuring the empty name")
	}
}

// TestTheRunsInEdgeResolvesToTheSameLabelNodeAsHasLabel. A pipeline step's
// `runs-on` names a label by the same name a runner declares, and both must
// reach one node or the graph answers "which runners can run this step" with
// nothing.
func TestTheRunsInEdgeResolvesToTheSameLabelNodeAsHasLabel(t *testing.T) {
	got := walkTheFixture(t)
	const (
		pipeline = "bitbucket:acme/Pipeline/api/default"
		label    = "bitbucket:acme/Label/self-hosted"
	)
	if !hasRelation(got, pipeline, label, bbgraph.EdgeRunsIn) {
		t.Errorf("no RUNS_IN edge from %q to %q", pipeline, label)
	}
	if !hasRelation(got, "bitbucket:acme/Runner/{runner-api}", label, bbgraph.EdgeHasLabel) {
		t.Errorf("no HAS_LABEL edge into the same label node %q", label)
	}
}

// TestAStepNamingAnEnvironmentNoRepositoryDeclaresEmitsNoEdge is the DEPLOYS_TO
// half of the class the whole-graph row above exists to close.
//
// THE INPUT IS ORDINARY. A step's `deployment` is a name in the repository's
// bitbucket-pipelines.yml; the environments come from the deployments config
// through a different enumeration. A workspace whose pipeline deploys to an
// environment nobody created there is a configuration that exists every day, and
// no enumeration can supply the node.
//
// THE SOURCE PROVIDER EMITS THE EDGE ANYWAY, which produces a relationship no
// traversal can follow. This collector emits none and records the name, which is
// the same shape it already takes for a `$VAR` nothing satisfies — and the
// alternative, MINTING the environment, would assert that a deployment target
// exists when the provider says it does not.
func TestAStepNamingAnEnvironmentNoRepositoryDeclaresEmitsNoEdge(t *testing.T) {
	got := walkTheFixture(t)
	const pipeline = "bitbucket:acme/Pipeline/api/custom/nightly"

	for _, rel := range got.Relations {
		if rel.Type == bbgraph.EdgeDeploysTo && strings.HasSuffix(rel.ToID, "/qa") {
			t.Errorf("an unresolvable deployment produced the edge %s -> %s", rel.FromID, rel.ToID)
		}
	}
	node, ok := resourceByID(got, pipeline)
	if !ok {
		t.Fatalf("the fixture emitted no pipeline node %q", pipeline)
	}
	if node.Metadata[bbgraph.UnresolvedDeploymentsKey] != "qa" {
		t.Errorf("the pipeline records %q under %q, want %q — an omitted edge that is also "+
			"unrecorded is invisible", node.Metadata[bbgraph.UnresolvedDeploymentsKey],
			bbgraph.UnresolvedDeploymentsKey, "qa")
	}

	// THE SAME-RUN KNOWN POSITIVE: the step whose deployment DOES exist still
	// gets its edge, so this is a resolution that failed rather than an emitter
	// that stopped.
	if !hasRelation(got, "bitbucket:acme/Pipeline/api/branches/trunk",
		"bitbucket:acme/Environment/api/production", bbgraph.EdgeDeploysTo) {
		t.Error("the step deploying to an environment that exists lost its DEPLOYS_TO edge")
	}
}

// TestAStepNamingALabelNoRunnerCarriesEmitsNoEdge is the RUNS_IN half, on the
// same terms: minting the label would assert that a runner class exists when no
// runner in the workspace advertises it.
func TestAStepNamingALabelNoRunnerCarriesEmitsNoEdge(t *testing.T) {
	got := walkTheFixture(t)
	const pipeline = "bitbucket:acme/Pipeline/api/custom/nightly"

	for _, rel := range got.Relations {
		if rel.Type == bbgraph.EdgeRunsIn && strings.HasSuffix(rel.ToID, "/gpu-only") {
			t.Errorf("an unresolvable runs-on label produced the edge %s -> %s",
				rel.FromID, rel.ToID)
		}
	}
	if _, minted := resourceIDs(got)["bitbucket:acme/Label/gpu-only"]; minted {
		t.Error("a label no runner carries was minted as a node; that asserts a runner class " +
			"the provider says does not exist")
	}
	node, ok := resourceByID(got, pipeline)
	if !ok {
		t.Fatalf("the fixture emitted no pipeline node %q", pipeline)
	}
	if node.Metadata[bbgraph.UnresolvedRunsOnKey] != "gpu-only" {
		t.Errorf("the pipeline records %q under %q, want %q",
			node.Metadata[bbgraph.UnresolvedRunsOnKey], bbgraph.UnresolvedRunsOnKey, "gpu-only")
	}

	// THE SAME-RUN KNOWN POSITIVE: the step whose labels a runner DOES carry
	// still gets its edges.
	if !hasRelation(got, "bitbucket:acme/Pipeline/api/default",
		"bitbucket:acme/Label/self-hosted", bbgraph.EdgeRunsIn) {
		t.Error("the step naming a label a runner carries lost its RUNS_IN edge")
	}
}

// TestAPipelineWhoseEveryStepReferenceResolvesCarriesNoUnresolvedKey is the
// control for both rows: the three keys are written when something went
// unmatched and not otherwise.
func TestAPipelineWhoseEveryStepReferenceResolvesCarriesNoUnresolvedKey(t *testing.T) {
	got := walkTheFixture(t)
	node, ok := resourceByID(got, "bitbucket:acme/Pipeline/api/branches/trunk")
	if !ok {
		t.Fatal("the fixture emitted no branches/trunk pipeline")
	}
	for _, key := range []string{
		bbgraph.UnresolvedRefsKey,
		bbgraph.UnresolvedDeploymentsKey,
		bbgraph.UnresolvedRunsOnKey,
	} {
		if value, present := node.Metadata[key]; present {
			t.Errorf("a pipeline whose every reference resolved carries %q: %q", key, value)
		}
	}
}
