// SPDX-License-Identifier: Apache-2.0

package bbgraph_test

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
)

// ids_test.go — every id helper, asserted against a LITERAL.
//
// A HELPER THAT SUPPLIED ITS OWN ANSWER KEY WOULD PROVE NOTHING. Every edge in
// this graph is joined on a string one of these built, so a helper that drifted
// from its partner would produce an edge naming nothing while every per-
// converter test still passed. These expectations are transcribed from the
// source provider's own spellings rather than computed.
func TestEveryIDHelperProducesTheSourceProvidersSpelling(t *testing.T) {
	for _, row := range []struct {
		what string
		got  string
		want string
	}{
		{what: "workspace", got: bbgraph.WorkspaceID("acme"),
			want: "bitbucket:acme/Workspace/acme"},
		{what: "repository", got: bbgraph.RepositoryID("acme", "api"),
			want: "bitbucket:acme/Repository/api"},
		{what: "pipeline", got: bbgraph.PipelineID("acme", "api", "branches/trunk"),
			want: "bitbucket:acme/Pipeline/api/branches/trunk"},
		{what: "pipeline run", got: bbgraph.PipelineRunID("acme", "api", "{run-1}"),
			want: "bitbucket:acme/PipelineRun/api/{run-1}"},
		{what: "runner", got: bbgraph.RunnerID("acme", "{runner-1}"),
			want: "bitbucket:acme/Runner/{runner-1}"},
		{what: "label", got: bbgraph.LabelID("acme", "self-hosted"),
			want: "bitbucket:acme/Label/self-hosted"},
		{what: "environment", got: bbgraph.EnvironmentID("acme", "api", "production"),
			want: "bitbucket:acme/Environment/api/production"},
		{what: "approval gate", got: bbgraph.ApprovalGateID("acme", "api", "production"),
			want: "bitbucket:acme/ApprovalGate/api/production"},
		{what: "workspace variable", got: bbgraph.WorkspaceVariableID("acme", "TOKEN"),
			want: "bitbucket:acme/Variable/workspace/TOKEN"},
		{what: "repository variable", got: bbgraph.RepositoryVariableID("acme", "api", "TOKEN"),
			want: "bitbucket:acme/Variable/repository/api/TOKEN"},
		{what: "deployment variable", got: bbgraph.DeploymentVariableID("acme", "api", "production", "TOKEN"),
			want: "bitbucket:acme/Variable/env/api/production/TOKEN"},
	} {
		if row.got != row.want {
			t.Errorf("the %s id is %q, want %q", row.what, row.got, row.want)
		}
	}
}

// TestTheDeploymentVariableIDSaysEnvAndNotDeployment is the one spelling a
// reader is most likely to normalize away.
//
// THE SOURCE PROVIDER MINTS `env` IN THE ID while storing `deployment` as the
// variable's scope metadata, so the two are deliberately different strings. A
// helper "corrected" to use the scope word would orphan every stored query that
// names one of these ids.
func TestTheDeploymentVariableIDSaysEnvAndNotDeployment(t *testing.T) {
	got := bbgraph.DeploymentVariableID("acme", "api", "production", "TOKEN")
	if got != "bitbucket:acme/Variable/env/api/production/TOKEN" {
		t.Errorf("the deployment variable id is %q", got)
	}
}

// TestTheRunnerAndLabelIDsCarryNoRepository. Both are keyed on the workspace
// alone, and that is what makes one runner reached from two scopes one node and
// one label carried by many runners one node.
func TestTheRunnerAndLabelIDsCarryNoRepository(t *testing.T) {
	if got := bbgraph.RunnerID("acme", "{r}"); got != "bitbucket:acme/Runner/{r}" {
		t.Errorf("the runner id is %q; it names no repository on either scope arm", got)
	}
	if got := bbgraph.LabelID("acme", "linux"); got != "bitbucket:acme/Label/linux" {
		t.Errorf("the label id is %q; a per-repository label id would mint one node per "+
			"repository for a label the whole workspace shares", got)
	}
}
