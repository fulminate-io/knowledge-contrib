// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"net/http"
	"net/url"
	"sync"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/collect"
)

// fixture_test.go — a provider that answers just enough for a whole walk, and
// records what it was ASKED for.
//
// THE RECORDING IS THE POINT. A cap that reached the graph as a node count is
// asserted over the enumerations themselves; what this observes is the other
// half, the value the collector put in the REQUEST — which is what decides
// whether one round trip is made instead of several, and which no node count can
// show. It also records whether the provider was BUILT at all, which is what a
// refusal row asserts is false: parameters are validated before anything is
// dialed.
//
// IT IS SHARED ACROSS GOROUTINES because the walk fans out, so every field it
// writes is guarded.

// recordingAPI answers every call with one small group.
type recordingAPI struct {
	mu    sync.Mutex
	built bool

	runsPerPage        int64
	deploymentsPerPage int64

	// failure, when set, is the error every call returns.
	failure error
	// variablesFailure, when set, is returned only by the project-variables read,
	// which is the input class the completeness fix rests on.
	variablesFailure error
	// subgroupProjectsFailure, when set, is returned when the subgroup's own
	// projects are listed — the shared discovery's partial arm.
	subgroupProjectsFailure error
	// buildFailure, when set, is what building the provider returns. It stands for
	// every way the one credential-holding package can refuse before a walk starts:
	// no token, a token that is present and empty, an instance selector carrying
	// userinfo, a selector that is not a URL.
	buildFailure error
}

func (r *recordingAPI) build(context.Context) (collect.API, func(), error) {
	r.mu.Lock()
	r.built = true
	r.mu.Unlock()
	if r.buildFailure != nil {
		return collect.API{}, nil, r.buildFailure
	}
	return collect.API{
		Groups: r, Runners: r, Environments: r, Deployments: r,
		Pipelines: r, Files: r, Variables: r,
	}, func() {}, nil
}

func (r *recordingAPI) wasBuilt() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.built
}

const (
	fixtureGroup    = "acme"
	fixtureSubgroup = "acme/platform"
	fixtureProject  = "acme/api"
)

func (r *recordingAPI) ListGroupProjects(
	_ context.Context, gid any, _ *gl.ListGroupProjectsOptions,
) ([]*gl.Project, *gl.Response, error) {
	if r.failure != nil {
		return nil, nil, r.failure
	}
	if gid == fixtureSubgroup {
		if r.subgroupProjectsFailure != nil {
			return nil, nil, r.subgroupProjectsFailure
		}
		return nil, &gl.Response{}, nil
	}
	return []*gl.Project{{
		ID: 1, Name: "api", PathWithNamespace: fixtureProject, DefaultBranch: "main",
	}}, &gl.Response{}, nil
}

func (r *recordingAPI) ListSubGroups(
	context.Context, any, *gl.ListSubGroupsOptions,
) ([]*gl.Group, *gl.Response, error) {
	if r.failure != nil {
		return nil, nil, r.failure
	}
	if r.subgroupProjectsFailure == nil {
		return nil, &gl.Response{}, nil
	}
	return []*gl.Group{{ID: 90, FullPath: fixtureSubgroup}}, &gl.Response{}, nil
}

func (r *recordingAPI) ListGroupsRunners(
	context.Context, any, *gl.ListGroupsRunnersOptions,
) ([]*gl.Runner, *gl.Response, error) {
	return nil, &gl.Response{}, nil
}

func (r *recordingAPI) ListProjectRunners(
	context.Context, any, *gl.ListProjectRunnersOptions,
) ([]*gl.Runner, *gl.Response, error) {
	return nil, &gl.Response{}, nil
}

func (r *recordingAPI) GetRunnerDetails(
	context.Context, any,
) (*gl.RunnerDetails, *gl.Response, error) {
	return &gl.RunnerDetails{}, &gl.Response{}, nil
}

func (r *recordingAPI) ListEnvironments(
	context.Context, any, *gl.ListEnvironmentsOptions,
) ([]*gl.Environment, *gl.Response, error) {
	return nil, &gl.Response{}, nil
}

func (r *recordingAPI) ListProtectedEnvironments(
	context.Context, any, *gl.ListProtectedEnvironmentsOptions,
) ([]*gl.ProtectedEnvironment, *gl.Response, error) {
	return nil, &gl.Response{}, nil
}

func (r *recordingAPI) ListProjectDeployments(
	_ context.Context, _ any, opts *gl.ListProjectDeploymentsOptions,
) ([]*gl.Deployment, *gl.Response, error) {
	r.mu.Lock()
	r.deploymentsPerPage = opts.PerPage
	r.mu.Unlock()
	return nil, &gl.Response{}, nil
}

func (r *recordingAPI) ListProjectPipelines(
	_ context.Context, _ any, opts *gl.ListProjectPipelinesOptions,
) ([]*gl.PipelineInfo, *gl.Response, error) {
	r.mu.Lock()
	r.runsPerPage = opts.PerPage
	r.mu.Unlock()
	return nil, &gl.Response{}, nil
}

func (r *recordingAPI) ListPipelineJobs(
	context.Context, any, int64, *gl.ListJobsOptions,
) ([]*gl.Job, *gl.Response, error) {
	return nil, &gl.Response{}, nil
}

func (r *recordingAPI) GetFile(
	context.Context, any, string, *gl.GetFileOptions,
) (*gl.File, *gl.Response, error) {
	return nil, nil, refusal(http.StatusNotFound, "404 File Not Found")
}

func (r *recordingAPI) ListGroupVariables(
	context.Context, any, *gl.ListGroupVariablesOptions,
) ([]*gl.GroupVariable, *gl.Response, error) {
	return nil, &gl.Response{}, nil
}

func (r *recordingAPI) ListProjectVariables(
	context.Context, any, *gl.ListProjectVariablesOptions,
) ([]*gl.ProjectVariable, *gl.Response, error) {
	if r.variablesFailure != nil {
		return nil, nil, r.variablesFailure
	}
	return nil, &gl.Response{}, nil
}

// refusal is the provider's own refusal shape, built from the SDK's error type
// carrying the real status.
//
// THE REQUEST CARRIES A METHOD AND A URL, and that is not decoration. The SDK's
// Error method dereferences Response.Request.URL unconditionally, so a request
// built as &http.Request{} makes every RENDERING of this error panic; fmt
// recovers and substitutes a panic marker. Classification still works — it reads
// the status — so a suite built that way measures the right verdicts while every
// reason text it observes carries a marker where the provider's own message
// belongs.
func refusal(status int, message string) error {
	return &gl.ErrorResponse{
		Response: &http.Response{
			StatusCode: status,
			Request: &http.Request{
				Method: http.MethodGet,
				URL: &url.URL{
					Scheme: "https",
					Host:   "gitlab.example.com",
					Path:   "/api/v4/projects/1/variables",
				},
			},
		},
		Message: message,
	}
}

var (
	_ collect.GroupsAPI       = (*recordingAPI)(nil)
	_ collect.RunnersAPI      = (*recordingAPI)(nil)
	_ collect.EnvironmentsAPI = (*recordingAPI)(nil)
	_ collect.DeploymentsAPI  = (*recordingAPI)(nil)
	_ collect.PipelinesAPI    = (*recordingAPI)(nil)
	_ collect.FilesAPI        = (*recordingAPI)(nil)
	_ collect.VariablesAPI    = (*recordingAPI)(nil)
)
