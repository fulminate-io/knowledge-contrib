// SPDX-License-Identifier: Apache-2.0

package ghgraph_test

import (
	"testing"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// ids_test.go — every id spelling, against a LITERAL.
//
// A TEST THAT BUILT ITS EXPECTATION FROM THE HELPER WOULD PROVE NOTHING. Every
// edge in this graph is joined on a string one of these functions produced, and
// an assertion of the form `got == helper(args)` agrees with the helper however
// wrong it is — while the graph it produces has every edge pointing at nothing.
// So each row below writes out the string a consumer's stored query already
// names.
func TestEveryIDSpelling(t *testing.T) {
	for _, row := range []struct {
		name string
		got  string
		want string
	}{
		{name: "organization", got: ghgraph.OrganizationID("acme"), want: "github:acme/Organization/acme"},
		{name: "repository", got: ghgraph.RepositoryID("acme", "acme/api"), want: "github:acme/Repository/acme/api"},
		{
			name: "workflow",
			got:  ghgraph.WorkflowID("acme", "acme/api", ".github/workflows/ci.yml"),
			want: "github:acme/Workflow/acme/api/.github/workflows/ci.yml",
		},
		{
			name: "workflow run",
			got:  ghgraph.WorkflowRunID("acme", "acme/api", 100),
			want: "github:acme/WorkflowRun/acme/api/100",
		},
		{name: "runner", got: ghgraph.RunnerID("acme", 7), want: "github:acme/Runner/7"},
		{
			name: "environment",
			got:  ghgraph.EnvironmentID("acme", "acme/api", "production"),
			want: "github:acme/Environment/acme/api/production",
		},
		{
			name: "deployment",
			got:  ghgraph.DeploymentID("acme", "acme/api", 500),
			want: "github:acme/Deployment/acme/api/500",
		},
		{
			name: "repository-scoped secret",
			got:  ghgraph.SecretID("acme", "acme/api", "repo", "API_KEY"),
			want: "github:acme/Secret/acme/api/repo/API_KEY",
		},
		{
			name: "environment-scoped secret",
			got:  ghgraph.SecretID("acme", "acme/api", "env/production", "PROD_DB_PASS"),
			want: "github:acme/Secret/acme/api/env/production/PROD_DB_PASS",
		},
		{
			name: "organization-scoped secret, one segment shorter",
			got:  ghgraph.OrgSecretID("acme", "org", "ORG_TOKEN"),
			want: "github:acme/Secret/org/ORG_TOKEN",
		},
		{name: "label", got: ghgraph.LabelID("acme", "self-hosted"), want: "github:acme/Label/self-hosted"},
		{name: "reviewer user", got: ghgraph.UserID("acme", "ada"), want: "github:acme/User/ada"},
		{name: "reviewer team", got: ghgraph.TeamID("acme", "platform"), want: "github:acme/Team/platform"},
	} {
		t.Run(row.name, func(t *testing.T) {
			if row.got != row.want {
				t.Errorf("%s ID = %q, want %q", row.name, row.got, row.want)
			}
		})
	}
}

// TestALabelIDCarriesNoRepository is the property the distinct-label
// materialization rests on, asserted directly rather than only through the walk.
// A label id that carried a repository would mint one node per repository and
// every assertion about distinctness would still pass at the organization scope.
func TestALabelIDCarriesNoRepository(t *testing.T) {
	if a, b := ghgraph.LabelID("acme", "self-hosted"), ghgraph.LabelID("acme", "self-hosted"); a != b {
		t.Fatalf("the same label produced two ids: %q and %q", a, b)
	}
	if got := ghgraph.LabelID("acme", "self-hosted"); got != "github:acme/Label/self-hosted" {
		t.Errorf("the label id is %q; it must carry the organization and the name and nothing "+
			"else, or two runners in different repositories mint two nodes", got)
	}
}

// TestARunnerIDCarriesNoRepositoryEither. The provider's runner ids are unique
// across the organization, and both scopes must produce the same id for the same
// runner or a repository-level runner would be a second node.
func TestARunnerIDCarriesNoRepositoryEither(t *testing.T) {
	if got := ghgraph.RunnerID("acme", 2); got != "github:acme/Runner/2" {
		t.Errorf("the runner id is %q, want %q", got, "github:acme/Runner/2")
	}
}
