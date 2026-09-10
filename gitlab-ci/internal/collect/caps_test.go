// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"testing"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/collect"
	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// caps_test.go — the two enumerations that read a project's HISTORY rather than
// its current state.
//
// THEY READ A SINGLE PAGE AND STOP, which is the source provider's own behavior
// and the reason both are collect parameters here: a deployment or run history is
// unbounded, so an exhaustive read would make the collect's cost a function of how
// long the group has existed. What a cap asks for has to reach the REQUEST, not
// only the emitted node count, or the cap is one round trip per item with the
// graph looking identical.
//
// THE READS THAT DO PAGE ARE IN pagination_boundary_test.go, one row each.

// TestTheTwoCapsHoldAtTheirDefaults is the row that pins the source provider's own
// numbers, on both the REQUEST and the emitted node count.
func TestTheTwoCapsHoldAtTheirDefaults(t *testing.T) {
	api := fixtureAPI()
	api.pipelines[apiID] = manyRuns(25)
	api.deployments[apiID] = manyDeployments(40)

	runs, err := runOneAllowingError(t, api, "gitlab-pipeline-runs")
	if err != nil {
		t.Fatalf("gitlab-pipeline-runs: %v", err)
	}
	if got := countByType(runs, glgraph.ResourceTypePipelineRun); got != 20 {
		t.Errorf("%d pipeline runs were emitted from a project with 25, want the default cap of 20",
			got)
	}
	if api.runsPerPage != 20 {
		t.Errorf("the runs listing asked for %d per page, want the default cap of 20; the cap has "+
			"to reach the REQUEST or it is one round trip per item", api.runsPerPage)
	}

	deployments, err := runOneAllowingError(t, api, "gitlab-deployments")
	if err != nil {
		t.Fatalf("gitlab-deployments: %v", err)
	}
	if got := countByType(deployments, glgraph.ResourceTypeDeployment); got != 20 {
		t.Errorf("%d deployments were emitted from a project with 40, want the default cap of 20", got)
	}
	if api.deploymentsPerPage != 20 {
		t.Errorf("the deployments listing asked for %d per page, want the default cap of 20",
			api.deploymentsPerPage)
	}
}

// TestACapSetByTheCallerIsHonoredExactly is the honored arm of the cap matrix at
// the enumeration level; the refusal arms live with the parameter validator,
// which is where a bad value is refused before anything is dialed.
func TestACapSetByTheCallerIsHonoredExactly(t *testing.T) {
	api := fixtureAPI()
	api.pipelines[apiID] = manyRuns(25)
	api.deployments[apiID] = manyDeployments(40)

	runs, err := runOneWithCaps(t, api, "gitlab-pipeline-runs",
		collect.Caps{MaxPipelineRuns: 5, MaxDeployments: 7})
	if err != nil {
		t.Fatalf("gitlab-pipeline-runs: %v", err)
	}
	if got := countByType(runs, glgraph.ResourceTypePipelineRun); got != 5 {
		t.Errorf("a caller asking for 5 runs got %d", got)
	}

	deployments, err := runOneWithCaps(t, api, "gitlab-deployments",
		collect.Caps{MaxPipelineRuns: 5, MaxDeployments: 7})
	if err != nil {
		t.Fatalf("gitlab-deployments: %v", err)
	}
	if got := countByType(deployments, glgraph.ResourceTypeDeployment); got != 7 {
		t.Errorf("a caller asking for 7 deployments got %d", got)
	}
}

// TestAVariableCarriesItsKeyAndItsFlagsAndNoValue is the shape row for the one
// resource whose value is a live credential.
func TestAVariableCarriesItsKeyAndItsFlagsAndNoValue(t *testing.T) {
	got := runOne(t, fixtureAPI(), "gitlab-variables")

	project, ok := resourceByID(got, "gitlab:acme/Variable/acme/api/API_KEY")
	if !ok {
		t.Fatal("the project-scoped variable node is missing")
	}
	if project.Name != "API_KEY" {
		t.Errorf("the variable node is named %q, want its key", project.Name)
	}
	if project.Content != "" {
		t.Errorf("the variable node carries content %q; there is nothing to put in it but the "+
			"value", project.Content)
	}
	for key, want := range map[string]string{
		"scope": "project", "project": apiPath, "protected": "true", "masked": "true",
	} {
		if project.Metadata[key] != want {
			t.Errorf("the project variable's %q is %q, want %q", key, project.Metadata[key], want)
		}
	}

	group, ok := resourceByID(got, "gitlab:acme/Variable/acme/GROUP_DEPLOY_TOKEN")
	if !ok {
		t.Fatal("the group-scoped variable node is missing")
	}
	for key, want := range map[string]string{"scope": "group", "group": fixtureGroup} {
		if group.Metadata[key] != want {
			t.Errorf("the group variable's %q is %q, want %q", key, group.Metadata[key], want)
		}
	}
}

// TestAJobNamesItsRunnerOnlyWhenTheProviderDid. A queued job has not been picked
// up by anything, and an edge to runner 0 would name a node nothing mints.
func TestAJobNamesItsRunnerOnlyWhenTheProviderDid(t *testing.T) {
	got := runOne(t, fixtureAPI(), "gitlab-pipeline-runs")

	if !hasRelation(got, "gitlab:acme/Job/acme/api/900", "gitlab:acme/Runner/2", glgraph.EdgeRunsIn) {
		t.Error("the job the provider says a runner executed has no RUNS_IN edge")
	}
	for _, rel := range got.Relations {
		if rel.FromID == "gitlab:acme/Job/acme/api/901" && rel.Type == glgraph.EdgeRunsIn {
			t.Errorf("a job with no runner emitted %s -> %s", rel.FromID, rel.ToID)
		}
	}
	// And the metadata follows the same rule.
	executed, _ := resourceByID(got, "gitlab:acme/Job/acme/api/900")
	if executed.Metadata["runner_id"] != "2" {
		t.Errorf("the executed job's runner_id is %q, want \"2\"", executed.Metadata["runner_id"])
	}
	queued, _ := resourceByID(got, "gitlab:acme/Job/acme/api/901")
	if _, present := queued.Metadata["runner_id"]; present {
		t.Errorf("a job with no runner carries runner_id=%q", queued.Metadata["runner_id"])
	}
}

// TestAProtectionRuleRequiringNoApprovalsProducesNothing. A protected environment
// may exist only to restrict who may deploy, which is a different statement from
// requiring a second person's approval.
func TestAProtectionRuleRequiringNoApprovalsProducesNothing(t *testing.T) {
	got := runOne(t, fixtureAPI(), "gitlab-environments")
	ids := resourceIDs(got)

	if _, ok := ids["gitlab:acme/ProtectionRule/acme/api/staging"]; ok {
		t.Error("a protected environment requiring no approvals produced a rule node")
	}
	// The known positive, in the same run: the one that DOES require approvals
	// produced its node and its edge.
	rule, ok := resourceByID(got, "gitlab:acme/ProtectionRule/acme/api/production")
	if !ok {
		t.Fatal("the rule requiring approvals is missing, so the absence above says nothing")
	}
	if rule.Metadata["required_approval_count"] != "2" {
		t.Errorf("the rule's approval count is %q, want \"2\"",
			rule.Metadata["required_approval_count"])
	}
	if !hasRelation(got, "gitlab:acme/Environment/acme/api/production",
		"gitlab:acme/ProtectionRule/acme/api/production", glgraph.EdgeRequiresApproval) {
		t.Error("the environment has no REQUIRES_APPROVAL edge to its rule")
	}
}

// TestAnEnvironmentCarriesItsOptionalMetadataOnlyWhenTheProviderSentIt.
func TestAnEnvironmentCarriesItsOptionalMetadataOnlyWhenTheProviderSentIt(t *testing.T) {
	got := runOne(t, fixtureAPI(), "gitlab-environments")

	full, ok := resourceByID(got, "gitlab:acme/Environment/acme/api/production")
	if !ok {
		t.Fatal("the production environment is missing")
	}
	for key, want := range map[string]string{
		"tier": "production", "external_url": "https://api.example.com", "state": "available",
	} {
		if full.Metadata[key] != want {
			t.Errorf("the environment's %q is %q, want %q", key, full.Metadata[key], want)
		}
	}

	bare, ok := resourceByID(got, "gitlab:acme/Environment/acme/api/staging")
	if !ok {
		t.Fatal("the staging environment is missing")
	}
	for _, key := range []string{"tier", "external_url"} {
		if _, present := bare.Metadata[key]; present {
			t.Errorf("an environment the provider sent no %s for carries %s=%q",
				key, key, bare.Metadata[key])
		}
	}
}

// TestADeploymentWithNoEnvironmentEmitsNoDeploysToEdge.
func TestADeploymentWithNoEnvironmentEmitsNoDeploysToEdge(t *testing.T) {
	api := fixtureAPI()
	api.deployments[apiID] = []*gl.Deployment{
		{ID: 500, Ref: "main", SHA: "deadbeef", Status: "success"},
	}

	got := runOne(t, api, "gitlab-deployments")
	if countByType(got, glgraph.ResourceTypeDeployment) != 1 {
		t.Fatal("the deployment node is missing, so the absence below says nothing")
	}
	for _, rel := range got.Relations {
		if rel.Type == glgraph.EdgeDeploysTo {
			t.Errorf("a deployment with no environment emitted %s -> %s", rel.FromID, rel.ToID)
		}
	}
	deployment, _ := resourceByID(got, "gitlab:acme/Deployment/acme/api/500")
	if _, present := deployment.Metadata["environment"]; present {
		t.Errorf("a deployment with no environment carries environment=%q",
			deployment.Metadata["environment"])
	}
}

// manyRuns builds n recorded pipeline runs.
func manyRuns(n int) []*gl.PipelineInfo {
	out := make([]*gl.PipelineInfo, 0, n)
	for i := range n {
		out = append(out, &gl.PipelineInfo{
			ID: int64(1000 + i), Status: "success", Source: "push", Ref: "main",
		})
	}
	return out
}

// manyDeployments builds n recorded deployments.
func manyDeployments(n int) []*gl.Deployment {
	out := make([]*gl.Deployment, 0, n)
	for i := range n {
		out = append(out, &gl.Deployment{ID: int64(2000 + i), Ref: "main", Status: "success"})
	}
	return out
}
