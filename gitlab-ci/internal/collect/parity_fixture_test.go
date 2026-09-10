// SPDX-License-Identifier: Apache-2.0

package collect_test

import gl "gitlab.com/gitlab-org/api/client-go"

// parity_fixture_test.go — THE RECORDED CORPUS: one response per enumeration,
// assembled into ONE group.
//
// WHY ONE GROUP AND NOT A FIXTURE PER TEST. A per-test fixture set can pass every
// test in the suite while leaving a resource type nothing emits and an edge type
// nothing produces, because no single test ever runs the whole collector. One
// group that every enumeration reads means one walk reaches the entire declared
// vocabulary, and the coverage assertion over that walk is the row that says
// whether this collector meets its target at all.
//
// WHY GO LITERALS OF THE SDK'S OWN TYPES AND NO FILES ON DISK. A recorded JSON
// document has to be decoded by something, and the only decoder that matters is
// the SDK's — so a fixture written as JSON tests this collector against the
// author's idea of the wire format, while a fixture written as the SDK's structs
// tests it against the values the SDK actually hands over. It is also the shape a
// compiler checks: a field the SDK renames stops the build here instead of
// silently reading as zero at run time.
//
// EVERY FIXTURE IS SHAPED TO REACH ITS CONVERTER'S EDGES, not merely to produce a
// node. That is why there is a SUBGROUP with a project in it, why there are
// THREE runners at two scopes with an overlapping tag and one of them visible to
// two projects, why the pipeline definition names a project variable, a group
// variable, an environment and two tags of which one no runner carries, why a
// protected environment exists for an environment the environments call did not
// return, why one project is archived and one has no default branch, and why one
// job names a runner and another does not: each of those is an edge, an absence
// or an input class that would otherwise never be reached by any test.
//
// THE ENDPOINTS MIRRORED. Each block below stands for one provider endpoint:
//   - GET /groups/{group}/projects and /groups/{group}/subgroups
//   - GET /projects/{id}/repository/files/.gitlab-ci.yml
//   - GET /projects/{id}/pipelines and /projects/{id}/pipelines/{id}/jobs
//   - GET /groups/{group}/runners, /projects/{id}/runners and /runners/{id}
//   - GET /projects/{id}/environments and /protected_environments
//   - GET /projects/{id}/deployments
//   - GET /groups/{group}/variables and /projects/{id}/variables

// The fixture group and the names inside it. They are written out here and
// compared against LITERAL ids in the assertions, never against another call of
// the id helpers — a test that built its expectation from the code under test
// would agree with that code however wrong both were.
const (
	fixtureGroup    = "acme"
	fixtureSubgroup = "acme/platform"

	apiPath      = "acme/api"
	webPath      = "acme/web"
	legacyPath   = "acme/legacy"
	emptyPath    = "acme/empty"
	infraPath    = "acme/platform/infra"
	fixtureRunID = int64(100)
)

// The numeric project ids the provider keys its per-project calls on.
const (
	apiID    = int64(1)
	webID    = int64(2)
	legacyID = int64(3)
	emptyID  = int64(4)
	infraID  = int64(5)
)

// fixturePipelineYAML is the recorded `.gitlab-ci.yml` of the api project, and it
// is what the parser reads.
//
// IT NAMES A PROJECT-SCOPED VARIABLE AND A GROUP-SCOPED ONE, which is what makes
// the USES_SECRET dangle observable: the document cannot say which scope a name
// comes from, so the first edge resolves and the second names a node that is not
// there even though a node for that variable IS in the same result under a
// different owner segment. It also names a tag no runner carries, a tag two
// runners do, and a predefined variable that must be filtered out.
const fixturePipelineYAML = `stages:
  - build
  - deploy
include:
  - local: /templates/base.yml
variables:
  BUILD_IMAGE: alpine
.hidden-template:
  script:
    - echo this is a template and runs nothing
build:
  stage: build
  tags:
    - docker
  script:
    - echo $API_KEY
    - echo ${API_KEY}
    - echo $CI_JOB_TOKEN
deploy:
  stage: deploy
  tags:
    - deploy-only
  environment:
    name: production
  script:
    - echo ${GROUP_DEPLOY_TOKEN}
    - echo $CI_COMMIT_SHA
`

// legacyPipelineYAML is the archived project's definition. Its only purpose is to
// prove that an archived project is WALKED rather than skipped.
const legacyPipelineYAML = `test:
  script:
    - echo ok
`

// fixtureAPI assembles the whole group: the provider as this collector sees it.
func fixtureAPI() *fakeAPI {
	return &fakeAPI{
		groupProjects: map[string]pages[gl.Project]{
			fixtureGroup:    {fixtureTopLevelProjects()},
			fixtureSubgroup: {{infraProject()}},
		},
		subgroups: map[string]pages[gl.Group]{
			fixtureGroup: {{{ID: 90, Name: "platform", Path: "platform", FullPath: fixtureSubgroup}}},
		},

		groupRunners:   pages[gl.Runner]{{fixtureGroupRunner()}},
		projectRunners: fixtureProjectRunners(),
		runnerDetails:  fixtureRunnerDetails(),

		environments:  map[int64]pages[gl.Environment]{apiID: {fixtureEnvironments()}},
		protectedEnvs: map[int64][]*gl.ProtectedEnvironment{apiID: fixtureProtectedEnvironments()},

		deployments: map[int64][]*gl.Deployment{apiID: {{
			ID:          500,
			Ref:         "main",
			SHA:         "deadbeef",
			Status:      "success",
			Environment: &gl.Environment{Name: "production"},
		}}},

		pipelines: map[int64][]*gl.PipelineInfo{apiID: {{
			ID:     fixtureRunID,
			Status: "success",
			Source: "push",
			Ref:    "main",
			SHA:    "deadbeef",
			WebURL: "https://gitlab.example.com/acme/api/-/pipelines/100",
		}}},
		jobs: map[string]pages[gl.Job]{"1/100": {fixtureJobs()}},

		files: map[string]*gl.File{
			"1/.gitlab-ci.yml": encodedFile(fixturePipelineYAML),
			"3/.gitlab-ci.yml": encodedFile(legacyPipelineYAML),
		},

		groupVars:   pages[gl.GroupVariable]{{{Key: "GROUP_DEPLOY_TOKEN", Value: groupVarValue}}},
		projectVars: fixtureProjectVariables(),
	}
}

// groupVarValue is a planted sentinel: a value the provider DOES return on the
// variables listing and that no byte of this collector's output may carry.
const groupVarValue = "sentinel-group-variable-value"

// projectVarValue is the same plant at project scope.
const projectVarValue = "sentinel-project-variable-value"

// runnersTokenValue is the third plant: the registration credential the
// provider's own project value carries, which a collector marshaling that value
// whole would put into a stored node.
const runnersTokenValue = "sentinel-runners-registration-token"

// fixtureTopLevelProjects is the group's own projects: two ordinary ones, one
// ARCHIVED and one with no default branch.
func fixtureTopLevelProjects() []*gl.Project {
	return []*gl.Project{
		{
			ID:                apiID,
			Name:              "api",
			PathWithNamespace: apiPath,
			DefaultBranch:     "main",
			Visibility:        gl.PrivateVisibility,
			WebURL:            "https://gitlab.example.com/acme/api",
			Description:       "The API service",
			Topics:            []string{"backend"},
			RunnersToken:      runnersTokenValue,
		},
		{
			ID:                webID,
			Name:              "web",
			PathWithNamespace: webPath,
			DefaultBranch:     "main",
			Visibility:        gl.PublicVisibility,
			WebURL:            "https://gitlab.example.com/acme/web",
		},
		{
			ID:                legacyID,
			Name:              "legacy",
			PathWithNamespace: legacyPath,
			DefaultBranch:     "master",
			Visibility:        gl.PrivateVisibility,
			Archived:          true,
		},
		{
			ID:                emptyID,
			Name:              "empty",
			PathWithNamespace: emptyPath,
			Visibility:        gl.PrivateVisibility,
		},
	}
}

// infraProject is the subgroup's project, which only the recursion reaches.
func infraProject() *gl.Project {
	return &gl.Project{
		ID:                infraID,
		Name:              "infra",
		PathWithNamespace: infraPath,
		DefaultBranch:     "main",
		Visibility:        gl.PrivateVisibility,
	}
}
