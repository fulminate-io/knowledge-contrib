// SPDX-License-Identifier: Apache-2.0

package walk_test

import (
	"context"
	"net/http"
	"sync"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/collect"
)

// fixture_test.go — a provider that answers just enough for a whole walk, and
// records what it was ASKED for.
//
// THE RECORDING IS THE POINT. A cap that reached the graph as a node count is
// asserted over the enumerations themselves; what this observes is the other
// half, the value the collector put in the REQUEST — which is what decides
// whether one round trip is made instead of several, and which no node count can
// show.
//
// IT IS SHARED ACROSS GOROUTINES because the walk fans out, so every field it
// writes is guarded.

// recordingAPI answers every call with one small organization and records the
// page sizes the two capped enumerations asked for.
type recordingAPI struct {
	mu sync.Mutex
	// built reports whether the collector ever asked for a provider, which is
	// what a refusal row asserts is FALSE: parameters are validated before
	// anything is dialed.
	built              bool
	runsPerPage        int
	deploymentsPerPage int

	// failures, when set, is the error every call returns.
	failure error
	// secretsFailure, when set, is returned only by the repository-secrets read,
	// which is the input class the completeness fix rests on.
	secretsFailure error
}

func (r *recordingAPI) build(context.Context) (collect.API, func(), error) {
	r.mu.Lock()
	r.built = true
	r.mu.Unlock()
	return collect.API{Repos: r, Actions: r}, func() {}, nil
}

func (r *recordingAPI) ListByOrg(
	context.Context, string, *gogithub.RepositoryListByOrgOptions,
) ([]*gogithub.Repository, *gogithub.Response, error) {
	if r.failure != nil {
		return nil, nil, r.failure
	}
	return []*gogithub.Repository{{
		FullName: new("acme/api"),
		Name:     new("api"),
	}}, &gogithub.Response{}, nil
}

func (r *recordingAPI) GetContents(
	context.Context, string, string, string, *gogithub.RepositoryContentGetOptions,
) (*gogithub.RepositoryContent, []*gogithub.RepositoryContent, *gogithub.Response, error) {
	return nil, nil, &gogithub.Response{}, nil
}

func (r *recordingAPI) ListEnvironments(
	context.Context, string, string, *gogithub.EnvironmentListOptions,
) (*gogithub.EnvResponse, *gogithub.Response, error) {
	return &gogithub.EnvResponse{}, &gogithub.Response{}, nil
}

func (r *recordingAPI) ListDeployments(
	_ context.Context, _, _ string, opts *gogithub.DeploymentsListOptions,
) ([]*gogithub.Deployment, *gogithub.Response, error) {
	r.mu.Lock()
	r.deploymentsPerPage = opts.PerPage
	r.mu.Unlock()
	return nil, &gogithub.Response{}, nil
}

func (r *recordingAPI) ListWorkflows(
	context.Context, string, string, *gogithub.ListOptions,
) (*gogithub.Workflows, *gogithub.Response, error) {
	return &gogithub.Workflows{}, &gogithub.Response{}, nil
}

func (r *recordingAPI) ListRepositoryWorkflowRuns(
	_ context.Context, _, _ string, opts *gogithub.ListWorkflowRunsOptions,
) (*gogithub.WorkflowRuns, *gogithub.Response, error) {
	r.mu.Lock()
	r.runsPerPage = opts.PerPage
	r.mu.Unlock()
	return &gogithub.WorkflowRuns{}, &gogithub.Response{}, nil
}

func (r *recordingAPI) ListOrganizationRunners(
	context.Context, string, *gogithub.ListRunnersOptions,
) (*gogithub.Runners, *gogithub.Response, error) {
	return &gogithub.Runners{}, &gogithub.Response{}, nil
}

func (r *recordingAPI) ListRunners(
	context.Context, string, string, *gogithub.ListRunnersOptions,
) (*gogithub.Runners, *gogithub.Response, error) {
	return &gogithub.Runners{}, &gogithub.Response{}, nil
}

func (r *recordingAPI) ListOrgSecrets(
	context.Context, string, *gogithub.ListOptions,
) (*gogithub.Secrets, *gogithub.Response, error) {
	return &gogithub.Secrets{}, &gogithub.Response{}, nil
}

func (r *recordingAPI) ListRepoSecrets(
	context.Context, string, string, *gogithub.ListOptions,
) (*gogithub.Secrets, *gogithub.Response, error) {
	if r.secretsFailure != nil {
		return nil, nil, r.secretsFailure
	}
	return &gogithub.Secrets{}, &gogithub.Response{}, nil
}

func (r *recordingAPI) ListEnvSecrets(
	context.Context, int, string, *gogithub.ListOptions,
) (*gogithub.Secrets, *gogithub.Response, error) {
	return &gogithub.Secrets{}, &gogithub.Response{}, nil
}

// refusal is the provider's own refusal shape, built from the SDK's error type
// carrying the real status.
func refusal(status int, message string) error {
	return &gogithub.ErrorResponse{
		Response: &http.Response{StatusCode: status, Request: &http.Request{}},
		Message:  message,
	}
}

var (
	_ collect.ReposAPI   = (*recordingAPI)(nil)
	_ collect.ActionsAPI = (*recordingAPI)(nil)
)
