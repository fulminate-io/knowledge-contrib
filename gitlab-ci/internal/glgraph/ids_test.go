// SPDX-License-Identifier: Apache-2.0

package glgraph_test

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// ids_test.go — the id spellings, asserted against LITERALS.
//
// EVERY EXPECTATION HERE IS WRITTEN OUT. These functions are the only place the
// graph's join keys are built, and three pairs of them have to agree across two
// enumerations for an edge to resolve; an expectation built by calling the helper
// again would agree with a wrong spelling on both sides and prove nothing.
//
// THEY REPRODUCE THE SOURCE PROVIDER'S SPELLINGS EXACTLY, down to the capitalised
// kind word and the group repeated in the group node's own id. A consumer's
// stored queries and saved traversals name these strings.

func TestTheIDSpellingsAreTheSourceProvidersOwn(t *testing.T) {
	for _, row := range []struct{ name, got, want string }{
		{name: "group", got: glgraph.GroupID("acme"), want: "gitlab:acme/Group/acme"},
		{name: "project", got: glgraph.ProjectID("acme", "acme/api"), want: "gitlab:acme/Project/acme/api"},
		{
			name: "a project inside a subgroup",
			got:  glgraph.ProjectID("acme", "acme/platform/infra"),
			want: "gitlab:acme/Project/acme/platform/infra",
		},
		{
			name: "pipeline definition",
			got:  glgraph.PipelineID("acme", "acme/api", "main"),
			want: "gitlab:acme/Pipeline/acme/api/main",
		},
		{
			name: "pipeline run",
			got:  glgraph.PipelineRunID("acme", "acme/api", 100),
			want: "gitlab:acme/PipelineRun/acme/api/100",
		},
		{name: "job", got: glgraph.JobID("acme", "acme/api", 900), want: "gitlab:acme/Job/acme/api/900"},
		{name: "runner", got: glgraph.RunnerID("acme", 3), want: "gitlab:acme/Runner/3"},
		{name: "runner tag", got: glgraph.RunnerTagID("acme", "docker"), want: "gitlab:acme/RunnerTag/docker"},
		{
			name: "environment",
			got:  glgraph.EnvironmentID("acme", "acme/api", "production"),
			want: "gitlab:acme/Environment/acme/api/production",
		},
		{
			name: "protection rule",
			got:  glgraph.ProtectionRuleID("acme", "acme/api", "production"),
			want: "gitlab:acme/ProtectionRule/acme/api/production",
		},
		{
			name: "deployment",
			got:  glgraph.DeploymentID("acme", "acme/api", 500),
			want: "gitlab:acme/Deployment/acme/api/500",
		},
		{
			name: "a project-scoped variable",
			got:  glgraph.VariableID("acme", "acme/api", "API_KEY"),
			want: "gitlab:acme/Variable/acme/api/API_KEY",
		},
		{
			name: "a group-scoped variable",
			got:  glgraph.VariableID("acme", "acme", "GROUP_DEPLOY_TOKEN"),
			want: "gitlab:acme/Variable/acme/GROUP_DEPLOY_TOKEN",
		},
	} {
		if row.got != row.want {
			t.Errorf("%s id is %q, want %q", row.name, row.got, row.want)
		}
	}
}

// TestTheTwoVariableScopesDifferByOneSegment is the pair that makes one edge
// class dangle by construction, pinned so a later "tidy" that made them agree is
// a visible change rather than a silent one.
func TestTheTwoVariableScopesDifferByOneSegment(t *testing.T) {
	project := glgraph.VariableID("acme", "acme/api", "DEPLOY_KEY")
	group := glgraph.VariableID("acme", "acme", "DEPLOY_KEY")
	if project == group {
		t.Fatal("the two variable scopes produce the same id, so a pipeline's reference to a " +
			"group-scoped variable would now resolve. That is a behavior change to record, not a " +
			"silent improvement")
	}
	if project != "gitlab:acme/Variable/acme/api/DEPLOY_KEY" {
		t.Errorf("the project-scoped id is %q", project)
	}
	if group != "gitlab:acme/Variable/acme/DEPLOY_KEY" {
		t.Errorf("the group-scoped id is %q", group)
	}
}

// TestARunnerTagIDCarriesNoProject is what makes one node serve every runner
// carrying that tag, at either scope.
func TestARunnerTagIDCarriesNoProject(t *testing.T) {
	if a, b := glgraph.RunnerTagID("acme", "docker"), glgraph.RunnerTagID("acme", "docker"); a != b {
		t.Fatalf("the same tag produced two ids: %q and %q", a, b)
	}
	if got := glgraph.RunnerTagID("acme", "docker"); got != "gitlab:acme/RunnerTag/docker" {
		t.Errorf("the tag id is %q; a project segment in it would mint one node per project", got)
	}
}
