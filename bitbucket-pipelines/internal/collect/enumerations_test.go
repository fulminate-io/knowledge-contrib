// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbclient"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/collect"
)

// enumerations_test.go — the per-converter input classes: the ids, the stored
// values, the conditional metadata both ways, and the boundaries.
//
// EVERY ID AND EVERY STORED VALUE IS ASSERTED AS A LITERAL, computed by the
// reader from the fixture rather than by calling the module's own helper. A
// helper that supplied its own answer key would agree with itself however wrong
// both were, and every edge in this graph is joined on a string one of those
// helpers built.

// TestTheWorkspaceAndRepositoryIdsAndMetadata is the repos converter.
func TestTheWorkspaceAndRepositoryIdsAndMetadata(t *testing.T) {
	got := walkTheFixture(t)

	workspace, ok := resourceByID(got, "bitbucket:acme/Workspace/acme")
	if !ok {
		t.Fatal("no workspace node was emitted")
	}
	if workspace.ResourceType != bbgraph.ResourceTypeWorkspace {
		t.Errorf("the workspace node is a %q", workspace.ResourceType)
	}

	api, ok := resourceByID(got, "bitbucket:acme/Repository/api")
	if !ok {
		t.Fatal("no node for the api repository")
	}
	if api.Name != "acme/api" {
		t.Errorf("the repository's name is %q, want the provider's own full name", api.Name)
	}
	for key, want := range map[string]string{
		"workspace":  "acme",
		"slug":       "api",
		"is_private": "true",
		"mainbranch": "trunk",
		"language":   "go",
		"scm":        "git",
	} {
		if api.Metadata[key] != want {
			t.Errorf("the repository's %q is %q, want %q", key, api.Metadata[key], want)
		}
	}

	// THE CONDITIONAL KEYS, THE OTHER WAY. The second repository declares no
	// language and no main branch, so those keys are ABSENT rather than empty —
	// which is the distinction a consumer filtering on the key depends on.
	web, ok := resourceByID(got, "bitbucket:acme/Repository/web")
	if !ok {
		t.Fatal("no node for the web repository")
	}
	// The mainbranch half of this pair lives with the fallback row below, which
	// owns the only repository in this suite the provider returns none for.
	for _, absent := range []string{"language"} {
		if value, present := web.Metadata[absent]; present {
			t.Errorf("the repository the provider returned no %s for carries the key anyway: %q",
				absent, value)
		}
	}
	if web.Metadata["is_private"] != "false" {
		t.Errorf("a public repository's is_private is %q, want %q — this key is written in BOTH "+
			"states rather than omitted when false", web.Metadata["is_private"], "false")
	}
}

// TestAWorkspaceWithNoRepositoriesEmitsNothingAtAll is the zero-repository
// boundary, and it is deliberately carried forward from the source provider: a
// real but empty workspace collects to zero nodes, not to a lone workspace node.
func TestAWorkspaceWithNoRepositoriesEmitsNothingAtAll(t *testing.T) {
	fixture := newFixture(t)
	fixture.pages["repositories/acme"] = []string{`[]`}

	got, err := runOne(t, fixture, "bitbucket-repos")
	if err != nil {
		t.Fatalf("an empty workspace was reported as a failure: %v", err)
	}
	if len(got.Resources) != 0 || len(got.Relations) != 0 {
		t.Errorf("an empty workspace emitted %d resources and %d relations, want none — including "+
			"no workspace node", len(got.Resources), len(got.Relations))
	}
}

// TestAMissingMainBranchFallsBackToMain is the other repos-derived boundary: the
// pipeline definition is read from the repository's main branch, and a
// repository the provider returned none for is read from "main".
//
// THE OBSERVABLE IS THE REQUEST PATH, because the fallback is invisible in the
// result: a repository with no definition emits nothing either way.
//
// IT BUILDS ITS OWN REPOSITORY LIST rather than riding the shared corpus, and
// that separation is the point. The corpus's repositories both declare a branch,
// so the one 404 it produces is a file missing on a branch the provider NAMED —
// which is the case the complete-and-empty control is entitled to pin. The
// repository below is the other case, and it is deliberately kept out of any
// walk that asserts completeness.
//
// WHAT IS STILL NOT OBSERVED ANYWHERE, and is not this suite's to close: a
// repository whose branch is neither reported nor named "main" gets a 404 on a
// URL this module GUESSED, and that reads as a repository with no pipeline —
// silently, with the walk complete. Confirming it needs a real repository with a
// differently-named default branch, so it belongs to the live confirmation
// rather than to a recorded corpus.
func TestAMissingMainBranchFallsBackToMain(t *testing.T) {
	fixture := newFixture(t)
	// The provider returns no main branch for this repository, which is the input
	// the fallback exists for and which the shared corpus no longer carries.
	fixture.pages["repositories/acme"] = []string{`[
		{"uuid":"{repo-api}","slug":"api","full_name":"acme/api","is_private":true,
		 "scm":"git","language":"go","mainbranch":{"name":"trunk"}},
		{"uuid":"{repo-web}","slug":"web","full_name":"acme/web","is_private":false,
		 "scm":"git","language":"","mainbranch":{"name":""}}
	]`}

	got, err := walkTheFixtureAllowingError(t, fixture, func([]collect.Subcollector) {})
	if err != nil {
		t.Fatalf("the fixture walk: %v", err)
	}
	if fixture.requests("repositories/acme/web/src/main/bitbucket-pipelines.yml") == 0 {
		t.Error("the repository with no main branch was not read from `main`")
	}
	// The known positive: the repository that DOES declare one was read from it,
	// so this is a fallback rather than a constant.
	if fixture.requests("repositories/acme/api/src/trunk/bitbucket-pipelines.yml") == 0 {
		t.Error("the repository declaring `trunk` was not read from it")
	}
	// AND THE METADATA KEY IS ABSENT RATHER THAN EMPTY, which is the other half of
	// the conditional-key pair the repos converter owes. It lives here because
	// this is now the only repository in the suite the provider names no branch
	// for.
	web, ok := resourceByID(got, "bitbucket:acme/Repository/web")
	if !ok {
		t.Fatal("no node for the repository with no main branch")
	}
	if value, present := web.Metadata["mainbranch"]; present {
		t.Errorf("the repository the provider returned no main branch for carries the key "+
			"anyway: %q", value)
	}
}

// TestThePipelineRunIdsMetadataAndConditionalKeys is the runs converter, both
// ways on all five conditional keys.
func TestThePipelineRunIdsMetadataAndConditionalKeys(t *testing.T) {
	got := walkTheFixture(t)

	finished, ok := resourceByID(got, "bitbucket:acme/PipelineRun/api/{run-1}")
	if !ok {
		t.Fatal("no node for the finished run")
	}
	if finished.Name != "api #41" {
		t.Errorf("the run's name is %q, want %q", finished.Name, "api #41")
	}
	for key, want := range map[string]string{
		"workspace":        "acme",
		"repo":             "api",
		"status":           "SUCCESSFUL",
		"created_on":       "2026-09-01T10:00:00Z",
		"completed_on":     "2026-09-01T10:04:00Z",
		"duration_seconds": "240",
		"branch":           "trunk",
		"trigger_type":     "push",
	} {
		if finished.Metadata[key] != want {
			t.Errorf("the finished run's %q is %q, want %q", key, finished.Metadata[key], want)
		}
	}

	// THE IN-FLIGHT RUN IS THE OTHER SIDE OF EVERY CONDITIONAL. It has no
	// completion time, no duration, no ref and no trigger type, and its status
	// comes from the STAGE rather than from the result.
	running, ok := resourceByID(got, "bitbucket:acme/PipelineRun/api/{run-2}")
	if !ok {
		t.Fatal("no node for the in-flight run")
	}
	if running.Metadata["status"] != "RUNNING" {
		t.Errorf("an in-flight run's status is %q, want the stage name %q",
			running.Metadata["status"], "RUNNING")
	}
	for _, absent := range []string{"completed_on", "duration_seconds", "branch", "trigger_type"} {
		if value, present := running.Metadata[absent]; present {
			t.Errorf("an in-flight run carries %q anyway: %q", absent, value)
		}
	}
}

// TestTheHistoryDepthCapsWhatIsRead is the one capped enumeration's boundary.
func TestTheHistoryDepthCapsWhatIsRead(t *testing.T) {
	fixture := newFixture(t)
	client := fixture.client()
	repositories, err := collect.Repositories(client).Run(context.Background(), fixtureWorkspace)
	if err != nil {
		t.Fatalf("the repository listing: %v", err)
	}

	for _, depth := range []struct {
		value int
		want  int
	}{
		{1, 1},  // BELOW what the fixture carries: the cap bites.
		{2, 2},  // EXACTLY what it carries.
		{50, 2}, // ABOVE it: everything, and the cap does not invent runs.
	} {
		var runs collect.Subcollector
		for _, sub := range collect.AfterRepos(client, collect.RepoInfos(repositories), depth.value) {
			if sub.Name == "bitbucket-pipeline-runs" {
				runs = sub
			}
		}
		got, err := runs.Run(context.Background(), fixtureWorkspace)
		if err != nil {
			t.Fatalf("depth %d: %v", depth.value, err)
		}
		var emitted int
		for _, res := range got.Resources {
			if res.ResourceType == bbgraph.ResourceTypePipelineRun {
				emitted++
			}
		}
		if emitted != depth.want {
			t.Errorf("a history depth of %d emitted %d runs, want %d",
				depth.value, emitted, depth.want)
		}
	}
}

// TestTheRunnerIdsScopesAndBothParentArms is the runners converter.
func TestTheRunnerIdsScopesAndBothParentArms(t *testing.T) {
	got := walkTheFixture(t)

	workspaceRunner, ok := resourceByID(got, "bitbucket:acme/Runner/{runner-ws}")
	if !ok {
		t.Fatal("no node for the workspace-level runner")
	}
	if workspaceRunner.Metadata["scope"] != "workspace" {
		t.Errorf("the workspace runner's scope is %q", workspaceRunner.Metadata["scope"])
	}
	if _, present := workspaceRunner.Metadata["repo"]; present {
		t.Error("a workspace-level runner carries a repo key")
	}
	if workspaceRunner.Metadata["labels"] != "self-hosted,linux" {
		t.Errorf("the workspace runner's labels are %q, want %q",
			workspaceRunner.Metadata["labels"], "self-hosted,linux")
	}
	if workspaceRunner.Metadata["state"] != "ONLINE" {
		t.Errorf("the workspace runner's state is %q", workspaceRunner.Metadata["state"])
	}

	repoRunner, ok := resourceByID(got, "bitbucket:acme/Runner/{runner-api}")
	if !ok {
		t.Fatal("no node for the repository-level runner")
	}
	if repoRunner.Metadata["scope"] != "repository" || repoRunner.Metadata["repo"] != "api" {
		t.Errorf("the repository runner's scope is %q and its repo %q",
			repoRunner.Metadata["scope"], repoRunner.Metadata["repo"])
	}
}

// TestTheThreeVariableScopesCarryTheirOwnWordAndTheirOwnIdShape is the row that
// an id assertion alone cannot make.
//
// THE ID AND THE STORED SCOPE ARE DIFFERENT SPELLINGS, and only one of them is
// visible in an id assertion. A converter passing `env` as the deployment scope
// word produces an IDENTICAL id — the id's own scope segment is minted as `env`
// regardless — while the stored `scope` metadata and the stored content both
// silently change from `deployment` to `env`. So the word is asserted directly,
// for all three scopes, in both places it is stored.
func TestTheThreeVariableScopesCarryTheirOwnWordAndTheirOwnIdShape(t *testing.T) {
	got := walkTheFixture(t)

	for _, want := range []struct{ id, scope, repo, environment string }{
		{"bitbucket:acme/Variable/workspace/WORKSPACE_TOKEN", "workspace", "", ""},
		{"bitbucket:acme/Variable/repository/api/API_KEY", "repository", "api", ""},
		{"bitbucket:acme/Variable/env/api/production/DEPLOY_KEY", "deployment", "api", "production"},
	} {
		variable, ok := resourceByID(got, want.id)
		if !ok {
			t.Errorf("no variable node with the id %q", want.id)
			continue
		}
		if variable.Metadata["scope"] != want.scope {
			t.Errorf("%s: the stored scope is %q, want %q",
				want.id, variable.Metadata["scope"], want.scope)
		}
		if variable.Metadata["repo"] != want.repo {
			t.Errorf("%s: the stored repo is %q, want %q",
				want.id, variable.Metadata["repo"], want.repo)
		}
		if variable.Metadata["environment"] != want.environment {
			t.Errorf("%s: the stored environment is %q, want %q",
				want.id, variable.Metadata["environment"], want.environment)
		}

		// THE SAME WORD IS STORED A SECOND TIME, in the node's content, and the
		// two must agree.
		var content struct {
			Key     string `json:"key"`
			Scope   string `json:"scope"`
			Secured bool   `json:"secured"`
		}
		if err := json.Unmarshal([]byte(variable.Content), &content); err != nil {
			t.Errorf("%s: the content does not decode: %v", want.id, err)
			continue
		}
		if content.Scope != want.scope {
			t.Errorf("%s: the content's scope is %q, want %q", want.id, content.Scope, want.scope)
		}
		if content.Key != variable.Metadata["key"] {
			t.Errorf("%s: the content's key %q and the metadata's %q disagree",
				want.id, content.Key, variable.Metadata["key"])
		}
	}
}

// TestTheSecuredFlagIsCarriedBothWays. It is the one property of a variable that
// tells an operator whether the value is readable at all, and a converter that
// hard-coded it would be indistinguishable from one that read it on a fixture
// where every variable is secured.
func TestTheSecuredFlagIsCarriedBothWays(t *testing.T) {
	got := walkTheFixture(t)
	for id, want := range map[string]string{
		"bitbucket:acme/Variable/repository/api/API_KEY":    "true",
		"bitbucket:acme/Variable/repository/api/DEPLOY_KEY": "false",
	} {
		variable, ok := resourceByID(got, id)
		if !ok {
			t.Errorf("no variable node %q", id)
			continue
		}
		if variable.Metadata["secured"] != want {
			t.Errorf("%s: secured is %q, want %q", id, variable.Metadata["secured"], want)
		}
	}
}

// TestOneRunnerReachedFromTwoScopesIsOneNode. The provider's repository-scoped
// listing can return a workspace runner, and the enumeration deduplicates by
// UUID as the source provider does.
func TestOneRunnerReachedFromTwoScopesIsOneNode(t *testing.T) {
	fixture := newFixture(t)
	// The repository's listing returns the workspace runner as well as its own.
	fixture.pages[repoRunnersPath(fixtureAPIRepo)] = []string{
		`[{"uuid":"{runner-ws}","name":"workspace runner","state":{"status":"ONLINE"},
		   "labels":[{"name":"self-hosted"}]},
		  {"uuid":"{runner-api}","name":"api runner","state":{"status":"ONLINE"},
		   "labels":[{"name":"self-hosted"}]}]`,
	}

	got, err := runOne(t, fixture, "bitbucket-runners")
	if err != nil {
		t.Fatalf("the runners enumeration: %v", err)
	}
	var minted int
	for _, res := range got.Resources {
		if res.ID == "bitbucket:acme/Runner/{runner-ws}" {
			minted++
		}
	}
	if minted != 1 {
		t.Errorf("a runner returned by two scopes was minted %d times, want 1", minted)
	}
	// AND THE FIRST SCOPE WINS. It was seen at the workspace first, so it belongs
	// to the workspace rather than to the repository that listed it second.
	if !hasRelation(got, "bitbucket:acme/Runner/{runner-ws}",
		"bitbucket:acme/Workspace/acme", bbgraph.EdgeBelongsTo) {
		t.Error("the deduplicated runner lost its workspace parent")
	}
	if strings.Contains(marshal(t, got.Relations), `"bitbucket:acme/Runner/{runner-ws}","ToID":"bitbucket:acme/Repository/api"`) {
		t.Error("the deduplicated runner also belongs to the repository that listed it second")
	}
}

// TestTheHistoryDepthStopsThePaginationAndNotOnlyTheResult is the boundary the
// row above cannot see.
//
// COUNTING NODES CANNOT OBSERVE THE SENTINEL. The cap truncates the accumulated
// slice AND halts the client's page loop; with the halt removed the truncation
// still bounds the result, so the node count is identical and every existing
// assertion passes — while the collector reads every page of a repository's
// whole run history and discards the tail. Against a repository with thousands
// of runs that is a large multiple of the requests, against the same workspace
// rate limit this module retries on.
//
// SO THE DISCRIMINATING HALF IS THE REQUEST COUNT, at the page boundary: one
// below a full page, exactly a full page, and one above it.
func TestTheHistoryDepthStopsThePaginationAndNotOnlyTheResult(t *testing.T) {
	const runsPath = "repositories/acme/api/pipelines"

	for _, arm := range []struct {
		depth    int
		runs     int
		requests int
		why      string
	}{
		{99, 99, 1, "below a full page: the first page satisfies the depth"},
		{100, 100, 1, "exactly a full page: the depth is reached ON that page and the walk stops"},
		{101, 101, 2, "one above: the second page is needed, and only the second"},
	} {
		fixture := newFixture(t)
		fixture.pages[runsPath] = []string{fullRunsPage(0), fullRunsPage(1)}

		client := fixture.client()
		repositories, err := collect.Repositories(client).Run(context.Background(), fixtureWorkspace)
		if err != nil {
			t.Fatalf("the repository listing: %v", err)
		}
		var runs collect.Subcollector
		for _, sub := range collect.AfterRepos(client, collect.RepoInfos(repositories), arm.depth) {
			if sub.Name == "bitbucket-pipeline-runs" {
				runs = sub
			}
		}
		got, err := runs.Run(context.Background(), fixtureWorkspace)
		if err != nil {
			t.Fatalf("depth %d: %v", arm.depth, err)
		}

		var emitted int
		for _, res := range got.Resources {
			if res.ResourceType == bbgraph.ResourceTypePipelineRun {
				emitted++
			}
		}
		if emitted != arm.runs {
			t.Errorf("depth %d emitted %d runs, want %d (%s)",
				arm.depth, emitted, arm.runs, arm.why)
		}
		if requests := fixture.requests(runsPath); requests != arm.requests {
			t.Errorf("depth %d made %d requests, want %d (%s). The result is bounded either "+
				"way; what the sentinel decides is whether the whole history was read first",
				arm.depth, requests, arm.requests, arm.why)
		}
	}
}

// fullRunsPage is one page of exactly the provider's page maximum, with ids
// distinct across pages so the emitted count is the number of runs rather than
// the number of distinct ids.
func fullRunsPage(page int) string {
	var runs []string
	for i := range bbclient.MaxPagelen {
		runs = append(runs, fmt.Sprintf(
			`{"uuid":"{run-%d-%d}","build_number":%d,"created_on":"2026-09-01T10:00:00Z",
			  "completed_on":"2026-09-01T10:04:00Z","duration_in_seconds":240,
			  "state":{"name":"COMPLETED","stage":{"name":""},"result":{"name":"SUCCESSFUL"}},
			  "target":{"ref_name":"trunk","ref_type":"branch"},"trigger":{"type":"push"}}`,
			page, i, page*bbclient.MaxPagelen+i))
	}
	return "[" + strings.Join(runs, ",") + "]"
}
