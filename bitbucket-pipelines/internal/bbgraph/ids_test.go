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
		{"workspace", bbgraph.WorkspaceID("acme"),
			"bitbucket:acme/Workspace/acme"},
		{"repository", bbgraph.RepositoryID("acme", "api"),
			"bitbucket:acme/Repository/api"},
		{"pipeline", bbgraph.PipelineID("acme", "api", "branches/trunk"),
			"bitbucket:acme/Pipeline/api/branches/trunk"},
		{"pipeline run", bbgraph.PipelineRunID("acme", "api", "{run-1}"),
			"bitbucket:acme/PipelineRun/api/{run-1}"},
		{"runner", bbgraph.RunnerID("acme", "{runner-1}"),
			"bitbucket:acme/Runner/{runner-1}"},
		{"label", bbgraph.LabelID("acme", "self-hosted"),
			"bitbucket:acme/Label/self-hosted"},
		{"environment", bbgraph.EnvironmentID("acme", "api", "production"),
			"bitbucket:acme/Environment/api/production"},
		{"approval gate", bbgraph.ApprovalGateID("acme", "api", "production"),
			"bitbucket:acme/ApprovalGate/api/production"},
		{"workspace variable", bbgraph.WorkspaceVariableID("acme", "TOKEN"),
			"bitbucket:acme/Variable/workspace/TOKEN"},
		{"repository variable", bbgraph.RepositoryVariableID("acme", "api", "TOKEN"),
			"bitbucket:acme/Variable/repository/api/TOKEN"},
		{"deployment variable", bbgraph.DeploymentVariableID("acme", "api", "production", "TOKEN"),
			"bitbucket:acme/Variable/env/api/production/TOKEN"},
	} {
		if row.got != row.want {
			t.Errorf("the %s id is %q, want %q", row.what, row.got, row.want)
		}
	}
}

// TestTheDeploymentVariableIdSaysEnvAndNotDeployment is the one spelling a
// reader is most likely to normalize away.
//
// THE SOURCE PROVIDER MINTS `env` IN THE ID while storing `deployment` as the
// variable's scope metadata, so the two are deliberately different strings. A
// helper "corrected" to use the scope word would orphan every stored query that
// names one of these ids.
func TestTheDeploymentVariableIdSaysEnvAndNotDeployment(t *testing.T) {
	got := bbgraph.DeploymentVariableID("acme", "api", "production", "TOKEN")
	if got != "bitbucket:acme/Variable/env/api/production/TOKEN" {
		t.Errorf("the deployment variable id is %q", got)
	}
}

// TestTheRunnerAndLabelIdsCarryNoRepository. Both are keyed on the workspace
// alone, and that is what makes one runner reached from two scopes one node and
// one label carried by many runners one node.
func TestTheRunnerAndLabelIdsCarryNoRepository(t *testing.T) {
	if got := bbgraph.RunnerID("acme", "{r}"); got != "bitbucket:acme/Runner/{r}" {
		t.Errorf("the runner id is %q; it names no repository on either scope arm", got)
	}
	if got := bbgraph.LabelID("acme", "linux"); got != "bitbucket:acme/Label/linux" {
		t.Errorf("the label id is %q; a per-repository label id would mint one node per "+
			"repository for a label the whole workspace shares", got)
	}
}
