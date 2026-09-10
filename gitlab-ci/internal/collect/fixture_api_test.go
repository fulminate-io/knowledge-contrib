// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"context"
	"fmt"
	"net/http"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/collect"
)

// fixture_api_test.go — the provider, served from recorded responses.
//
// IT IMPLEMENTS THE SEVEN INTERFACES WHOLE, so a walk over it reaches every line
// a walk over the real provider does. What it adds beyond returning canned values
// is the three things the real provider does that a map lookup would not: it
// PAGES, so the boundary at a full page is reachable offline; it FAILS on demand
// per scope, so the refusal and not-found arms are reachable too; and it RECORDS
// the page sizes the two capped enumerations asked for, which is the half of a
// cap no node count can show.
//
// THE RECORDED RESPONSES THEMSELVES ARE IN parity_fixture_test.go. This file is
// the transport; that one is the corpus.

// fakeAPI serves the whole provider from recorded responses.
//
// EVERY MAP IS KEYED THE WAY THE PROVIDER'S OWN CALL IS: a group path where the
// call takes a group, a numeric project id where it takes a project, and
// "<project id>/<pipeline id>" for the job listing under a run.
type fakeAPI struct {
	groupProjects    map[string]pages[gl.Project]
	groupProjectsErr map[string]error
	subgroups        map[string]pages[gl.Group]
	subgroupsErr     map[string]error

	groupRunners     pages[gl.Runner]
	groupRunnersErr  error
	projectRunners   map[int64]pages[gl.Runner]
	projectRunnerErr map[int64]error
	runnerDetails    map[int64]*gl.RunnerDetails
	runnerDetailsErr map[int64]error

	environments     map[int64]pages[gl.Environment]
	environmentsErr  map[int64]error
	protectedEnvs    map[int64][]*gl.ProtectedEnvironment
	protectedEnvsErr map[int64]error

	deployments    map[int64][]*gl.Deployment
	deploymentsErr map[int64]error

	pipelines    map[int64][]*gl.PipelineInfo
	pipelinesErr map[int64]error
	jobs         map[string]pages[gl.Job]
	jobsErr      map[string]error

	files    map[string]*gl.File
	filesErr map[string]error

	groupVars     pages[gl.GroupVariable]
	groupVarsErr  error
	projectVars   map[int64]pages[gl.ProjectVariable]
	projectVarErr map[int64]error

	// failAfterPage refuses every page AFTER the numbered one for a named scope,
	// which is how a read that answered part of its answer and then failed is
	// reproduced. Keys are "<what>:<scope>".
	failAfterPage map[string]int64

	// The page sizes the two capped enumerations asked for, which is what a cap
	// test observes beyond the node count.
	runsPerPage        int64
	deploymentsPerPage int64

	// reads is what every paginated read was asked for: how many pages, at what
	// page size, and whether one was served twice. See fixture_counters_test.go
	// for why all three are recorded and what each one catches.
	reads map[string]*readLog
}

func (f *fakeAPI) failed(what, scope string, page int64) error {
	if after, set := f.failAfterPage[what+":"+scope]; set && page > after {
		return refusal(http.StatusInternalServerError, "Server Error")
	}
	return nil
}

func (f *fakeAPI) ListGroupProjects(
	_ context.Context, gid any, opts *gl.ListGroupProjectsOptions,
) ([]*gl.Project, *gl.Response, error) {
	group := groupKey(gid)
	if err := f.record(scoped(readGroupProjects, group), opts.Page, opts.PerPage); err != nil {
		return nil, nil, err
	}
	if err, bad := f.groupProjectsErr[group]; bad {
		return nil, nil, err
	}
	if err := f.failed("projects", group, opts.Page); err != nil {
		return nil, nil, err
	}
	items, resp := f.groupProjects[group].page(opts.Page)
	return items, resp, nil
}

func (f *fakeAPI) ListSubGroups(
	_ context.Context, gid any, opts *gl.ListSubGroupsOptions,
) ([]*gl.Group, *gl.Response, error) {
	group := groupKey(gid)
	if err := f.record(scoped(readSubgroups, group), opts.Page, opts.PerPage); err != nil {
		return nil, nil, err
	}
	if err, bad := f.subgroupsErr[group]; bad {
		return nil, nil, err
	}
	items, resp := f.subgroups[group].page(opts.Page)
	return items, resp, nil
}

func (f *fakeAPI) ListGroupsRunners(
	_ context.Context, _ any, opts *gl.ListGroupsRunnersOptions,
) ([]*gl.Runner, *gl.Response, error) {
	if err := f.record(readGroupRunners, opts.Page, opts.PerPage); err != nil {
		return nil, nil, err
	}
	if f.groupRunnersErr != nil {
		return nil, nil, f.groupRunnersErr
	}
	items, resp := f.groupRunners.page(opts.Page)
	return items, resp, nil
}

func (f *fakeAPI) ListProjectRunners(
	_ context.Context, pid any, opts *gl.ListProjectRunnersOptions,
) ([]*gl.Runner, *gl.Response, error) {
	project := projectKey(pid)
	if err := f.record(scoped(readProjectRunners, project), opts.Page, opts.PerPage); err != nil {
		return nil, nil, err
	}
	if err, bad := f.projectRunnerErr[project]; bad {
		return nil, nil, err
	}
	items, resp := f.projectRunners[project].page(opts.Page)
	return items, resp, nil
}

func (f *fakeAPI) ListEnvironments(
	_ context.Context, pid any, opts *gl.ListEnvironmentsOptions,
) ([]*gl.Environment, *gl.Response, error) {
	project := projectKey(pid)
	if err := f.record(scoped(readEnvironments, project), opts.Page, opts.PerPage); err != nil {
		return nil, nil, err
	}
	if err, bad := f.environmentsErr[project]; bad {
		return nil, nil, err
	}
	if err := f.failed("environments", fmt.Sprint(project), opts.Page); err != nil {
		return nil, nil, err
	}
	items, resp := f.environments[project].page(opts.Page)
	return items, resp, nil
}

func (f *fakeAPI) ListProtectedEnvironments(
	_ context.Context, pid any, _ *gl.ListProtectedEnvironmentsOptions,
) ([]*gl.ProtectedEnvironment, *gl.Response, error) {
	project := projectKey(pid)
	if err, bad := f.protectedEnvsErr[project]; bad {
		return nil, nil, err
	}
	return f.protectedEnvs[project], &gl.Response{}, nil
}

func (f *fakeAPI) ListProjectDeployments(
	_ context.Context, pid any, opts *gl.ListProjectDeploymentsOptions,
) ([]*gl.Deployment, *gl.Response, error) {
	project := projectKey(pid)
	f.deploymentsPerPage = opts.PerPage
	if err, bad := f.deploymentsErr[project]; bad {
		return nil, nil, err
	}
	items := f.deployments[project]
	if int64(len(items)) > opts.PerPage {
		// THE PROVIDER TRUNCATES AT THE PAGE SIZE, and the fixture must too:
		// a cap that reached the request and was ignored by the answer would make
		// the node-count assertion pass for the wrong reason.
		items = items[:opts.PerPage]
	}
	return items, &gl.Response{}, nil
}

func (f *fakeAPI) ListProjectPipelines(
	_ context.Context, pid any, opts *gl.ListProjectPipelinesOptions,
) ([]*gl.PipelineInfo, *gl.Response, error) {
	project := projectKey(pid)
	f.runsPerPage = opts.PerPage
	if err, bad := f.pipelinesErr[project]; bad {
		return nil, nil, err
	}
	items := f.pipelines[project]
	if int64(len(items)) > opts.PerPage {
		items = items[:opts.PerPage]
	}
	return items, &gl.Response{}, nil
}

func (f *fakeAPI) ListPipelineJobs(
	_ context.Context, pid any, pipelineID int64, opts *gl.ListJobsOptions,
) ([]*gl.Job, *gl.Response, error) {
	key := fmt.Sprintf("%d/%d", projectKey(pid), pipelineID)
	if err := f.record(scoped(readJobs, key), opts.Page, opts.PerPage); err != nil {
		return nil, nil, err
	}
	if err, bad := f.jobsErr[key]; bad {
		return nil, nil, err
	}
	if err := f.failed("jobs", key, opts.Page); err != nil {
		return nil, nil, err
	}
	items, resp := f.jobs[key].page(opts.Page)
	return items, resp, nil
}

func (f *fakeAPI) ListGroupVariables(
	_ context.Context, _ any, opts *gl.ListGroupVariablesOptions,
) ([]*gl.GroupVariable, *gl.Response, error) {
	if err := f.record(readGroupVariables, opts.Page, opts.PerPage); err != nil {
		return nil, nil, err
	}
	if f.groupVarsErr != nil {
		return nil, nil, f.groupVarsErr
	}
	items, resp := f.groupVars.page(opts.Page)
	return items, resp, nil
}

func (f *fakeAPI) ListProjectVariables(
	_ context.Context, pid any, opts *gl.ListProjectVariablesOptions,
) ([]*gl.ProjectVariable, *gl.Response, error) {
	project := projectKey(pid)
	if err := f.record(scoped(readProjectVars, project), opts.Page, opts.PerPage); err != nil {
		return nil, nil, err
	}
	if err, bad := f.projectVarErr[project]; bad {
		return nil, nil, err
	}
	items, resp := f.projectVars[project].page(opts.Page)
	return items, resp, nil
}

// bundle is the fake presented as the seven interfaces the enumerations take.
func (f *fakeAPI) bundle() collect.API {
	return collect.API{
		Groups:       f,
		Runners:      f,
		Environments: f,
		Deployments:  f,
		Pipelines:    f,
		Files:        f,
		Variables:    f,
	}
}

// compile-time proof that the fixture satisfies the whole of all seven
// interfaces. A fake that had drifted from one would otherwise fail at its first
// use inside a test, which reads as a broken test rather than an incomplete
// fixture.
var (
	_ collect.GroupsAPI       = (*fakeAPI)(nil)
	_ collect.RunnersAPI      = (*fakeAPI)(nil)
	_ collect.EnvironmentsAPI = (*fakeAPI)(nil)
	_ collect.DeploymentsAPI  = (*fakeAPI)(nil)
	_ collect.PipelinesAPI    = (*fakeAPI)(nil)
	_ collect.FilesAPI        = (*fakeAPI)(nil)
	_ collect.VariablesAPI    = (*fakeAPI)(nil)
)
