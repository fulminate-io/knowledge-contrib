// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"

	gl "gitlab.com/gitlab-org/api/client-go"
)

// api.go — the narrow list interfaces this package enumerates through.
//
// THE INTERFACES CARRY EXACTLY THE CALLS THIS COLLECTOR MAKES and not one more.
// That is what makes a recorded fixture a complete stand-in rather than a
// partial one: a test implementing them has answered every question this
// collector can ask, so a walk over recorded responses reaches the same code an
// operator's walk does.
//
// THE CONTEXT IS THE FIRST PARAMETER RATHER THAN AN OPTION. The provider SDK
// passes a context as one of a variadic list of request options, which makes a
// call that forgot it compile and run uncancellable. Here it is positional, so
// every paginated read propagates cancellation because it cannot do otherwise.

// GroupsAPI is the group half of the provider: the project discovery every
// other enumeration starts from.
type GroupsAPI interface {
	ListGroupProjects(ctx context.Context, gid any, opts *gl.ListGroupProjectsOptions) (
		[]*gl.Project, *gl.Response, error)
	ListSubGroups(ctx context.Context, gid any, opts *gl.ListSubGroupsOptions) (
		[]*gl.Group, *gl.Response, error)
}

// RunnersAPI is the runners half: both scopes, plus the per-runner detail read
// the tag list only comes back on.
type RunnersAPI interface {
	ListGroupsRunners(ctx context.Context, gid any, opts *gl.ListGroupsRunnersOptions) (
		[]*gl.Runner, *gl.Response, error)
	ListProjectRunners(ctx context.Context, pid any, opts *gl.ListProjectRunnersOptions) (
		[]*gl.Runner, *gl.Response, error)
	GetRunnerDetails(ctx context.Context, rid any) (*gl.RunnerDetails, *gl.Response, error)
}

// EnvironmentsAPI is the environments half, including the separate protected-
// environment read the approval rules come from.
type EnvironmentsAPI interface {
	ListEnvironments(ctx context.Context, pid any, opts *gl.ListEnvironmentsOptions) (
		[]*gl.Environment, *gl.Response, error)
	ListProtectedEnvironments(ctx context.Context, pid any, opts *gl.ListProtectedEnvironmentsOptions) (
		[]*gl.ProtectedEnvironment, *gl.Response, error)
}

// DeploymentsAPI is the deployments half.
type DeploymentsAPI interface {
	ListProjectDeployments(ctx context.Context, pid any, opts *gl.ListProjectDeploymentsOptions) (
		[]*gl.Deployment, *gl.Response, error)
}

// PipelinesAPI is the pipeline-run half, with the job listing under a run.
type PipelinesAPI interface {
	ListProjectPipelines(ctx context.Context, pid any, opts *gl.ListProjectPipelinesOptions) (
		[]*gl.PipelineInfo, *gl.Response, error)
	ListPipelineJobs(ctx context.Context, pid any, pipelineID int64, opts *gl.ListJobsOptions) (
		[]*gl.Job, *gl.Response, error)
}

// FilesAPI is the repository-file read the pipeline DEFINITION is parsed from.
type FilesAPI interface {
	GetFile(ctx context.Context, pid any, fileName string, opts *gl.GetFileOptions) (
		*gl.File, *gl.Response, error)
}

// VariablesAPI is the CI/CD variable half at both scopes.
//
// IT LISTS AND NEVER GETS. The provider's per-variable GET is what returns a
// variable's VALUE; the list this collector calls returns the key and the flags,
// and there is no call on this interface that could hand a value back.
type VariablesAPI interface {
	ListGroupVariables(ctx context.Context, gid any, opts *gl.ListGroupVariablesOptions) (
		[]*gl.GroupVariable, *gl.Response, error)
	ListProjectVariables(ctx context.Context, pid any, opts *gl.ListProjectVariablesOptions) (
		[]*gl.ProjectVariable, *gl.Response, error)
}

// API bundles the halves, which is what every enumeration is built from.
type API struct {
	Groups       GroupsAPI
	Runners      RunnersAPI
	Environments EnvironmentsAPI
	Deployments  DeploymentsAPI
	Pipelines    PipelinesAPI
	Files        FilesAPI
	Variables    VariablesAPI
}

// perPage is the page size every paginated read asks for. It is the provider's
// own maximum: a smaller page is the same total number of items in more round
// trips, and this collector's cost is round trips rather than bytes.
const perPage = 100

// The two enumerations that read a project's HISTORY rather than its current
// state read a single page and stop, which is the source provider's own
// behavior. Both defaults are that provider's own numbers; both are collect
// parameters here, where in the source they were a named constant and an
// anonymous literal that no operator could reach.
const (
	// DefaultMaxPipelineRuns is how many recent pipeline runs are read per
	// project.
	DefaultMaxPipelineRuns = 20
	// DefaultMaxDeployments is how many recent deployments are read per project.
	DefaultMaxDeployments = 20
)
