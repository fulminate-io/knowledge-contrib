// SPDX-License-Identifier: Apache-2.0

package glclients

import (
	"context"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/collect"
)

// services.go — the adapters from the SDK's own service values to the narrow
// interfaces this collector enumerates through.
//
// THEY EXIST TO MAKE THE CONTEXT POSITIONAL. The SDK takes a context as one of a
// variadic list of request options, so a call that simply forgot it compiles,
// runs and cannot be cancelled. Every method below threads the caller's context
// into that option, which is what makes a cancelled collect stop rather than run
// its remaining round trips to completion.

// services wires one SDK client into the bundle the enumerations take.
func services(client *gl.Client) collect.API {
	return collect.API{
		Groups:       groupsService{client.Groups},
		Runners:      runnersService{client.Runners},
		Environments: environmentsService{client.Environments, client.ProtectedEnvironments},
		Deployments:  deploymentsService{client.Deployments},
		Pipelines:    pipelinesService{client.Pipelines, client.Jobs},
		Files:        filesService{client.RepositoryFiles},
		Variables:    variablesService{client.GroupVariables, client.ProjectVariables},
	}
}

type groupsService struct{ groups gl.GroupsServiceInterface }

func (s groupsService) ListGroupProjects(
	ctx context.Context, gid any, opts *gl.ListGroupProjectsOptions,
) ([]*gl.Project, *gl.Response, error) {
	return s.groups.ListGroupProjects(gid, opts, gl.WithContext(ctx))
}

func (s groupsService) ListSubGroups(
	ctx context.Context, gid any, opts *gl.ListSubGroupsOptions,
) ([]*gl.Group, *gl.Response, error) {
	return s.groups.ListSubGroups(gid, opts, gl.WithContext(ctx))
}

type runnersService struct{ runners gl.RunnersServiceInterface }

func (s runnersService) ListGroupsRunners(
	ctx context.Context, gid any, opts *gl.ListGroupsRunnersOptions,
) ([]*gl.Runner, *gl.Response, error) {
	return s.runners.ListGroupsRunners(gid, opts, gl.WithContext(ctx))
}

func (s runnersService) ListProjectRunners(
	ctx context.Context, pid any, opts *gl.ListProjectRunnersOptions,
) ([]*gl.Runner, *gl.Response, error) {
	return s.runners.ListProjectRunners(pid, opts, gl.WithContext(ctx))
}

func (s runnersService) GetRunnerDetails(
	ctx context.Context, rid any,
) (*gl.RunnerDetails, *gl.Response, error) {
	return s.runners.GetRunnerDetails(rid, gl.WithContext(ctx))
}

type environmentsService struct {
	environments gl.EnvironmentsServiceInterface
	protected    gl.ProtectedEnvironmentsServiceInterface
}

func (s environmentsService) ListEnvironments(
	ctx context.Context, pid any, opts *gl.ListEnvironmentsOptions,
) ([]*gl.Environment, *gl.Response, error) {
	return s.environments.ListEnvironments(pid, opts, gl.WithContext(ctx))
}

func (s environmentsService) ListProtectedEnvironments(
	ctx context.Context, pid any, opts *gl.ListProtectedEnvironmentsOptions,
) ([]*gl.ProtectedEnvironment, *gl.Response, error) {
	return s.protected.ListProtectedEnvironments(pid, opts, gl.WithContext(ctx))
}

type deploymentsService struct {
	deployments gl.DeploymentsServiceInterface
}

func (s deploymentsService) ListProjectDeployments(
	ctx context.Context, pid any, opts *gl.ListProjectDeploymentsOptions,
) ([]*gl.Deployment, *gl.Response, error) {
	return s.deployments.ListProjectDeployments(pid, opts, gl.WithContext(ctx))
}

type pipelinesService struct {
	pipelines gl.PipelinesServiceInterface
	jobs      gl.JobsServiceInterface
}

func (s pipelinesService) ListProjectPipelines(
	ctx context.Context, pid any, opts *gl.ListProjectPipelinesOptions,
) ([]*gl.PipelineInfo, *gl.Response, error) {
	return s.pipelines.ListProjectPipelines(pid, opts, gl.WithContext(ctx))
}

func (s pipelinesService) ListPipelineJobs(
	ctx context.Context, pid any, pipelineID int64, opts *gl.ListJobsOptions,
) ([]*gl.Job, *gl.Response, error) {
	return s.jobs.ListPipelineJobs(pid, pipelineID, opts, gl.WithContext(ctx))
}

type filesService struct {
	files gl.RepositoryFilesServiceInterface
}

func (s filesService) GetFile(
	ctx context.Context, pid any, fileName string, opts *gl.GetFileOptions,
) (*gl.File, *gl.Response, error) {
	return s.files.GetFile(pid, fileName, opts, gl.WithContext(ctx))
}

type variablesService struct {
	group   gl.GroupVariablesServiceInterface
	project gl.ProjectVariablesServiceInterface
}

func (s variablesService) ListGroupVariables(
	ctx context.Context, gid any, opts *gl.ListGroupVariablesOptions,
) ([]*gl.GroupVariable, *gl.Response, error) {
	return s.group.ListVariables(gid, opts, gl.WithContext(ctx))
}

func (s variablesService) ListProjectVariables(
	ctx context.Context, pid any, opts *gl.ListProjectVariablesOptions,
) ([]*gl.ProjectVariable, *gl.Response, error) {
	return s.project.ListVariables(pid, opts, gl.WithContext(ctx))
}

// Compile-time proof that every adapter satisfies the interface the enumerations
// take. Without these, a drifted signature would fail at the one call site that
// builds the bundle, which reads as a broken wiring rather than an incomplete
// adapter.
var (
	_ collect.GroupsAPI       = groupsService{}
	_ collect.RunnersAPI      = runnersService{}
	_ collect.EnvironmentsAPI = environmentsService{}
	_ collect.DeploymentsAPI  = deploymentsService{}
	_ collect.PipelinesAPI    = pipelinesService{}
	_ collect.FilesAPI        = filesService{}
	_ collect.VariablesAPI    = variablesService{}
)
