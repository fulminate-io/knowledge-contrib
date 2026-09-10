// SPDX-License-Identifier: Apache-2.0

package bbgraph_test

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
)

// resolve_test.go — the USES_SECRET join in isolation, over hand-built inputs.
//
// The whole-walk arms live with the fixture corpus; these are the unit arms,
// including the two the corpus cannot reach without a second fixture: a
// deployment step whose environment-scoped candidate is absent, and a result
// carrying no references at all.

// variables builds a result carrying one variable node per id.
func variables(ids ...string) bbgraph.Result {
	out := bbgraph.Result{}
	for _, id := range ids {
		out.Resources = append(out.Resources, bbgraph.Resource{
			ID: id, Name: "V", ResourceType: bbgraph.ResourceTypeVariable,
		})
	}
	return out
}

// pipelineWithRef adds a pipeline node and one carried reference.
func pipelineWithRef(result bbgraph.Result, deployment, name string) bbgraph.Result {
	const pipelineID = "bitbucket:acme/Pipeline/api/default"
	result.Resources = append(result.Resources, bbgraph.Resource{
		ID: pipelineID, Name: "api/default", ResourceType: bbgraph.ResourceTypePipeline,
	})
	result.SecretRefs = append(result.SecretRefs, bbgraph.SecretRef{
		PipelineID: pipelineID, RepoSlug: "api", Deployment: deployment, Name: name,
	})
	return result
}

// usesSecretTargets is every USES_SECRET target in a result.
func usesSecretTargets(result bbgraph.Result) []string {
	var out []string
	for _, rel := range result.Relations {
		if rel.Type == bbgraph.EdgeUsesSecret {
			out = append(out, rel.ToID)
		}
	}
	return out
}

// TestThePrecedenceIsEnvironmentThenRepositoryThenWorkspace, over every subset.
func TestThePrecedenceIsEnvironmentThenRepositoryThenWorkspace(t *testing.T) {
	const (
		env       = "bitbucket:acme/Variable/env/api/production/KEY"
		repo      = "bitbucket:acme/Variable/repository/api/KEY"
		workspace = "bitbucket:acme/Variable/workspace/KEY"
	)
	for _, row := range []struct {
		present    []string
		deployment string
		want       string
	}{
		{[]string{env, repo, workspace}, "production", env},
		{[]string{repo, workspace}, "production", repo},
		{[]string{workspace}, "production", workspace},
		{[]string{env, repo, workspace}, "", repo},
		{[]string{env, workspace}, "", workspace},
	} {
		result := pipelineWithRef(variables(row.present...), row.deployment, "KEY")
		bbgraph.ResolveSecretRefs(&result, "acme")

		targets := usesSecretTargets(result)
		if len(targets) != 1 || targets[0] != row.want {
			t.Errorf("with %v present and deployment %q the targets are %v, want exactly [%s]",
				row.present, row.deployment, targets, row.want)
		}
	}
}

// TestAStepWithNoDeploymentNeverTakesAnEnvironmentScopedVariable is the fourth
// row above, stated on its own because it is the arm most easily lost.
//
// AN ENVIRONMENT-SCOPED VARIABLE IS NOT IN SCOPE FOR A STEP THAT DEPLOYS
// NOWHERE. Taking it would assert that a build step reads a production
// credential it cannot see.
func TestAStepWithNoDeploymentNeverTakesAnEnvironmentScopedVariable(t *testing.T) {
	result := pipelineWithRef(
		variables("bitbucket:acme/Variable/env/api/production/KEY"), "", "KEY")
	bbgraph.ResolveSecretRefs(&result, "acme")

	if targets := usesSecretTargets(result); len(targets) != 0 {
		t.Errorf("a step naming no deployment resolved to %v", targets)
	}
	pipeline := result.Resources[len(result.Resources)-1]
	if pipeline.Metadata[bbgraph.UnresolvedRefsKey] != "KEY" {
		t.Errorf("the unresolvable reference was not recorded: %v", pipeline.Metadata)
	}
}

// TestUnresolvedRefsAreSortedAndJoined, so two collects of an unchanged
// workspace produce the same bytes.
func TestUnresolvedRefsAreSortedAndJoined(t *testing.T) {
	result := variables()
	for _, name := range []string{"ZULU", "ALPHA", "MIKE", "ALPHA"} {
		result = pipelineWithRef(result, "", name)
	}
	bbgraph.ResolveSecretRefs(&result, "acme")

	var stamped string
	for _, res := range result.Resources {
		if res.ResourceType == bbgraph.ResourceTypePipeline {
			stamped = res.Metadata[bbgraph.UnresolvedRefsKey]
			break
		}
	}
	if stamped != "ALPHA,MIKE,ZULU" {
		t.Errorf("the unresolved references are recorded as %q, want %q — sorted and "+
			"deduplicated, or two collects of one workspace differ", stamped, "ALPHA,MIKE,ZULU")
	}
}

// TestAResultWithNoReferencesIsUntouched. The join is a no-op rather than a pass
// that stamps an empty key on every pipeline.
func TestAResultWithNoReferencesIsUntouched(t *testing.T) {
	result := variables("bitbucket:acme/Variable/workspace/KEY")
	result.Resources = append(result.Resources, bbgraph.Resource{
		ID: "bitbucket:acme/Pipeline/api/default", ResourceType: bbgraph.ResourceTypePipeline,
	})
	bbgraph.ResolveSecretRefs(&result, "acme")

	if len(result.Relations) != 0 {
		t.Errorf("a result with no references produced %d relations", len(result.Relations))
	}
	for _, res := range result.Resources {
		if _, stamped := res.Metadata[bbgraph.UnresolvedRefsKey]; stamped {
			t.Errorf("%s carries the unresolved-references key with nothing to record", res.ID)
		}
	}
}

// TestTheTargetIsChosenFromTheWALKSOwnVariables, which is what makes every
// emitted edge resolvable by construction rather than by convention.
//
// THE CONTROL IS A VARIABLE THAT LOOKS RIGHT AND IS NOT THERE: the candidate id
// is well-formed, and with no node carrying it no edge is emitted.
func TestTheTargetIsChosenFromTheWALKSOwnVariables(t *testing.T) {
	result := pipelineWithRef(bbgraph.Result{}, "production", "KEY")
	bbgraph.ResolveSecretRefs(&result, "acme")

	if targets := usesSecretTargets(result); len(targets) != 0 {
		t.Errorf("an edge was emitted to %v with no variable node in the result — that is the "+
			"source provider's defect, an edge naming a node nothing ever creates", targets)
	}
}
