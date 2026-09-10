// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"encoding/base64"

	gogithub "github.com/google/go-github/v68/github"
)

// parity_fixture_test.go — THE RECORDED CORPUS: one response per enumeration,
// assembled into ONE organization.
//
// WHY ONE ORGANIZATION AND NOT A FIXTURE PER TEST. A per-test fixture set can
// pass every test in the suite while leaving a resource type nothing emits and an
// edge type nothing produces, because no single test ever runs the whole
// collector. One organization that every enumeration reads means one walk reaches
// the entire declared vocabulary, and the coverage assertion over that walk is
// the row that says whether this collector meets its target at all.
//
// WHY GO LITERALS OF THE SDK'S OWN TYPES AND NO FILES ON DISK. A recorded JSON
// document has to be decoded by something, and the only decoder that matters is
// the SDK's — so a fixture written as JSON tests this collector against the
// author's idea of the wire format, while a fixture written as the SDK's structs
// tests it against the values the SDK actually hands over. It is also the shape
// a compiler checks: a field the SDK renames stops the build here instead of
// silently reading as zero at run time.
//
// EVERY FIXTURE IS SHAPED TO REACH ITS CONVERTER'S EDGES, not merely to produce a
// node. That is why the environment carries BOTH a user and a team reviewer, why
// there are two runners with an overlapping label at two different scopes, why
// the workflow's text names both a secret and an environment, why the deployment
// names an environment the environments enumeration also returns, why there is a
// secret at each of the three scopes, and why the run carries both a path and an
// event: each of those is an edge that would otherwise never be emitted by any
// test.
//
// THE PEOPLE IN IT ARE SHAPED THE SAME WAY, and three of their properties are
// deliberate rather than decorative:
//
//   - THE FIRST RUN'S ACTOR IS THE ENVIRONMENT'S REVIEWER. One login is therefore
//     minted by two different enumerations, from two provider objects carrying
//     different fields, which is the collision the graph builder's tiebreak
//     resolves. Without it the surviving copy would depend on which goroutine
//     finished first and two collects of an unchanged organization would differ.
//   - ITS TRIGGERING ACTOR IS A DIFFERENT PERSON, AND A BOT. A run whose two
//     actors are the same user leaves the second class emitting an edge nobody
//     could tell from the first, and `User` / `Bot` is the distinction the node's
//     detail carries.
//   - THE SECOND RUN NAMES NOBODY AT ALL, so the arm that skips an absent user
//     is exercised by a walk that still emits the run.
//
// AND THE FIRST RUN'S ACTOR CARRIES AN EMAIL ADDRESS, as does its head commit's
// author. Both are fields the provider really sends and this collector really
// drops; the fixture carries them so the disclosure row is asserting an absence
// over a walk that HAD the value to leak.
//
// THE ENDPOINTS MIRRORED. Each block below names the provider endpoint whose
// answer it stands for, so a reader can compare it against the documented shape:
//   - GET /orgs/{org}/repos
//   - GET /repos/{owner}/{repo}/actions/workflows
//   - GET /repos/{owner}/{repo}/contents/{path}
//   - GET /repos/{owner}/{repo}/actions/runs
//   - GET /orgs/{org}/actions/runners and /repos/{owner}/{repo}/actions/runners
//   - GET /repos/{owner}/{repo}/environments
//   - GET /repos/{owner}/{repo}/deployments
//   - GET /orgs/{org}/actions/secrets, /repos/{owner}/{repo}/actions/secrets and
//     /repositories/{repository_id}/environments/{environment_name}/secrets

// The fixture organization and the names inside it. They are written out here
// and compared against LITERAL ids in the assertions, never against another call
// of the id helpers — a test that built its expectation from the code under test
// would agree with that code however wrong both were.
const (
	fixtureOrg      = "acme"
	fixtureAPIRepo  = "acme/api"
	fixtureWebRepo  = "acme/web"
	fixtureArchived = "acme/legacy"
	fixtureWorkflow = ".github/workflows/ci.yml"
	// fixtureReleaseWorkflow is a SECOND workflow, in the second repository, whose
	// text names an environment the environments enumeration does NOT return. It
	// exists so the workflow-parsed DEPLOYS_TO dangling class has a fixture: the
	// first workflow's `production` resolves, so without this one that class is
	// asserted over nothing.
	fixtureReleaseWorkflow = ".github/workflows/release.yml"
	// fixtureUnknownEnvironment is the name that workflow deploys to. No
	// environment of that name is returned for any repository, which is exactly
	// the condition the class depends on.
	fixtureUnknownEnvironment = "canary"
	fixtureEnvironment        = "production"
	// fixtureEnvironmentID is the numeric repository-environment id the
	// provider's environment-secrets call is keyed on.
	fixtureEnvironmentID = 10

	// The three people the walked objects name. fixtureReviewer is BOTH the
	// environment's required reviewer and the first run's actor, which is what
	// makes one user id arrive from two enumerations carrying different fields.
	fixtureReviewer = "ada"
	fixtureBot      = "dependabot[bot]"
	fixtureDeployer = "grace"

	// fixtureActorEmail and fixtureCommitEmail are the two addresses the provider
	// sends and this collector drops. They are recognizable so the disclosure row
	// can look for them by value over the whole encoded result.
	fixtureActorEmail  = "ada-actor@fixture.invalid"
	fixtureCommitEmail = "ada-commit@fixture.invalid"

	// fixtureAddressDomain is the part both addresses share, so the disclosure
	// row can also look for an address this fixture did not spell out.
	fixtureAddressDomain = "@fixture.invalid"
)

// fixtureWorkflowYAML is the recorded body of the workflow definition, and it is
// what the parser reads.
//
// IT NAMES A REPOSITORY-SCOPED SECRET AND AN ORGANIZATION-SCOPED ONE, which is
// what makes the USES_SECRET dangle observable: the workflow's text cannot say
// which scope a name comes from, so the first edge resolves and the second names
// a node that is not there even though a node for that secret IS in the same
// result under a different id.
const fixtureWorkflowYAML = `name: CI
on: push
jobs:
  build:
    runs-on: ubuntu-latest
    environment: production
    steps:
      - uses: actions/checkout@v4
      - run: echo ${{ secrets.API_KEY }}
      - run: echo ${{ secrets.ORG_TOKEN }}
      - run: echo ${{ secrets.API_KEY }}
`

// fixtureReleaseWorkflowYAML deploys to an environment nothing returns.
//
// THE EDGE IT PRODUCES IS THE POINT. A workflow's text is read from the file and
// the environments enumeration is read from the API; nothing makes the two agree,
// so a workflow can name an environment that does not exist — one deleted, one
// created by a later run, or one written as an expression the provider evaluates
// when the workflow runs. The edge is emitted anyway, which is the source
// provider's behavior carried forward, and this fixture is what lets a test
// assert it stays unresolved rather than leaving the class unexercised.
const fixtureReleaseWorkflowYAML = `name: Release
on: workflow_dispatch
jobs:
  ship:
    runs-on: ubuntu-latest
    environment: canary
    steps:
      - uses: actions/checkout@v4
`

// fixtureRepos is GET /orgs/acme/repos: two active repositories and one archived
// one, which every enumeration must skip.
func fixtureRepos() pages[gogithub.Repository] {
	archived := true
	return pages[gogithub.Repository]{{
		{
			FullName:      new(fixtureAPIRepo),
			Name:          new("api"),
			Visibility:    new("private"),
			DefaultBranch: new("main"),
			Language:      new("Go"),
			Topics:        []string{"backend"},
		},
		{
			FullName:      new(fixtureWebRepo),
			Name:          new("web"),
			Visibility:    new("public"),
			DefaultBranch: new("main"),
		},
		{
			FullName:      new(fixtureArchived),
			Name:          new("legacy"),
			Archived:      &archived,
			Visibility:    new("private"),
			DefaultBranch: new("master"),
		},
	}}
}

// fixtureAPI assembles the whole organization: the provider as this collector
// sees it.
func fixtureAPI() (*fakeRepos, *fakeActions) {
	encoded := base64.StdEncoding.EncodeToString([]byte(fixtureWorkflowYAML))
	repos := &fakeRepos{
		repos: fixtureRepos(),
		contents: map[string]*gogithub.RepositoryContent{
			"acme/api/" + fixtureWorkflow: {
				Content:  new(encoded),
				Encoding: new("base64"),
			},
			"acme/web/" + fixtureReleaseWorkflow: {
				Content:  new(base64.StdEncoding.EncodeToString([]byte(fixtureReleaseWorkflowYAML))),
				Encoding: new("base64"),
			},
		},
		environments: map[string]pages[gogithub.Environment]{
			"acme/api": {{
				{
					ID:              new(int64(fixtureEnvironmentID)),
					Name:            new(fixtureEnvironment),
					ProtectionRules: fixtureProtectionRules(),
				},
				{ID: new(int64(11)), Name: new("staging")},
			}},
		},
		deployments: map[string][]*gogithub.Deployment{
			"acme/api": {{
				ID:          new(int64(500)),
				Environment: new(fixtureEnvironment),
				Ref:         new("main"),
				Task:        new("deploy"),
				Description: new("deploy to prod"),
				// The person who created it: a THIRD login, so the deployment's
				// own class cannot pass by reaching a user some other enumeration
				// already minted.
				Creator: &gogithub.User{
					Login:   new(fixtureDeployer),
					ID:      new(int64(9)),
					Type:    new("User"),
					HTMLURL: new("https://github.com/" + fixtureDeployer),
				},
			}},
		},
	}
	return repos, fixtureActions()
}

// fixtureProtectionRules is the environment's protection block: ONE
// required-reviewers rule naming a user and a team, and one wait timer that
// names nobody. The second is what proves the rule filter: a walk that emitted an
// approval edge for it would be asserting an approver the provider never named.
func fixtureProtectionRules() []*gogithub.ProtectionRule {
	return []*gogithub.ProtectionRule{
		{
			Type: new("required_reviewers"),
			Reviewers: []*gogithub.RequiredReviewer{
				{Type: new("User"), Reviewer: &gogithub.User{Login: new("ada")}},
				{Type: new("Team"), Reviewer: &gogithub.Team{Slug: new("platform")}},
			},
		},
		{Type: new("wait_timer"), WaitTimer: new(30)},
	}
}

// fixtureActions is the Actions half: workflows, runs, runners and secrets.
func fixtureActions() *fakeActions {
	return &fakeActions{
		workflows: map[string]pages[gogithub.Workflow]{
			"acme/api": {{
				{
					Name:    new("CI"),
					Path:    new(fixtureWorkflow),
					State:   new("active"),
					HTMLURL: new("https://github.com/acme/api/actions/workflows/ci.yml"),
				},
				// A disabled workflow, which is inventory of something that
				// cannot run and is skipped.
				{
					Name:  new("Old"),
					Path:  new(".github/workflows/old.yml"),
					State: new("disabled_manually"),
				},
			}},
			// THE SECOND REPOSITORY'S WORKFLOW, whose text deploys to an
			// environment nothing returns. It is what gives the workflow-parsed
			// DEPLOYS_TO dangling class something to be asserted over.
			"acme/web": {{
				{
					Name:  new("Release"),
					Path:  new(fixtureReleaseWorkflow),
					State: new("active"),
				},
			}},
		},
		runs: map[string]*gogithub.WorkflowRuns{
			// THE SECOND REPOSITORY HAS A RUN TOO, so a failure on one repository's
			// runs has another repository's to leave standing. Without it, failing
			// the only repository that has runs produces an empty result for a
			// reason that is not the failure, and the "everything else survived"
			// half of the completeness rows cannot be asserted at all.
			// THIS RUN NAMES NOBODY. It is the arm where the provider sends no
			// actor at all, and the walk must still emit the run.
			"acme/web": {WorkflowRuns: []*gogithub.WorkflowRun{{
				ID:        new(int64(101)),
				Name:      new("Release"),
				RunNumber: new(7),
				Status:    new("completed"),
				Event:     new("workflow_dispatch"),
				Path:      new(fixtureReleaseWorkflow),
			}}},
			"acme/api": {WorkflowRuns: []*gogithub.WorkflowRun{{
				ID:         new(int64(100)),
				Name:       new("CI"),
				RunNumber:  new(42),
				Status:     new("completed"),
				Conclusion: new("success"),
				HeadBranch: new("main"),
				Event:      new("push"),
				Path:       new(fixtureWorkflow),
				// The actor the run is filed under, carrying the address this
				// collector drops.
				Actor: &gogithub.User{
					Login:   new(fixtureReviewer),
					ID:      new(int64(7)),
					Type:    new("User"),
					HTMLURL: new("https://github.com/" + fixtureReviewer),
					Email:   new(fixtureActorEmail),
				},
				// A DIFFERENT person started this run, and it is a Bot. The two
				// classes are indistinguishable on a run where the two fields
				// agree.
				TriggeringActor: &gogithub.User{
					Login:   new(fixtureBot),
					ID:      new(int64(49699333)),
					Type:    new("Bot"),
					HTMLURL: new("https://github.com/apps/dependabot"),
				},
				// The head commit's author, which carries a name and an address
				// and is dropped whole.
				HeadCommit: &gogithub.HeadCommit{
					Message: new("a commit"),
					Author: &gogithub.CommitAuthor{
						Name:  new("Ada Lovelace"),
						Email: new(fixtureCommitEmail),
					},
				},
			}}},
		},
		// TWO RUNNERS AT TWO SCOPES SHARING ONE LABEL. This is what the distinct-
		// label materialization has to deduplicate, and it is also what makes both
		// arms of the runner's BELONGS_TO parent fire in one walk.
		orgRunners: pages[gogithub.Runner]{{{
			ID:     new(int64(1)),
			Name:   new("org-runner-1"),
			OS:     new("linux"),
			Status: new("online"),
			Busy:   new(false),
			Labels: []*gogithub.RunnerLabels{
				{Name: new("self-hosted")},
				{Name: new("linux")},
			},
		}}},
		repoRunners: map[string]pages[gogithub.Runner]{
			"acme/api": {{{
				ID:     new(int64(2)),
				Name:   new("repo-runner-1"),
				OS:     new("macos"),
				Status: new("offline"),
				Busy:   new(false),
				Labels: []*gogithub.RunnerLabels{{Name: new("self-hosted")}},
			}}},
		},
		// A SECRET AT EACH OF THE THREE SCOPES, which is what makes all three
		// arms of the secret's BELONGS_TO parent fire and what makes the
		// org-scoped id's different shape observable.
		orgSecrets: pages[gogithub.Secret]{{{Name: "ORG_TOKEN", Visibility: "all"}}},
		repoSecrets: map[string]pages[gogithub.Secret]{
			"acme/api": {{{Name: "API_KEY"}, {Name: "DEPLOY_KEY"}}},
		},
		envSecrets: map[string]pages[gogithub.Secret]{
			"10/production": {{{Name: "PROD_DB_PASS"}}},
		},
	}
}
