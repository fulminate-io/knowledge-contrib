// SPDX-License-Identifier: Apache-2.0

package collect_test

import (
	"context"
	"fmt"
	"net/http"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/collect"
)

// fixture_api_test.go — the provider, served from recorded responses.
//
// IT IMPLEMENTS THE TWO INTERFACES WHOLE, so a walk over it reaches every line a
// walk over the real provider does. What it adds beyond returning canned values
// is the two things the real provider does that a map lookup would not: it PAGES,
// so the boundary at a full page is reachable offline, and it FAILS on demand per
// scope, so the refusal and not-found arms are reachable too.
//
// THE RECORDED RESPONSES THEMSELVES ARE IN parity_fixture_test.go. This file is
// the transport; that one is the corpus.

// pages is a paginated answer: one slice per page, served in order. A single
// page is the ordinary case and is written as one element.
type pages[T any] [][]*T

// page returns the requested page and the response carrying the next page
// number, in the provider's own convention: page 0 and page 1 are both the
// first, and NextPage is 0 on the last.
func (p pages[T]) page(requested int) ([]*T, *gogithub.Response) {
	if len(p) == 0 {
		return nil, &gogithub.Response{}
	}
	index := max(requested, 1) - 1
	if index >= len(p) {
		return nil, &gogithub.Response{}
	}
	resp := &gogithub.Response{}
	if index+1 < len(p) {
		resp.NextPage = index + 2
	}
	return p[index], resp
}

// refusal is the provider's answer when a credential may not read something. It
// is built from the SDK's own error type carrying the real status, because that
// is what this collector's classifier reads — an error built any other way would
// exercise a classification path the provider never produces.
func refusal(status int, message string) error {
	return &gogithub.ErrorResponse{
		Response: &http.Response{StatusCode: status, Request: &http.Request{}},
		Message:  message,
	}
}

// fakeRepos serves the repositories half of the provider.
type fakeRepos struct {
	repos    pages[gogithub.Repository]
	reposErr error

	// contents is keyed by "owner/repo/path".
	contents    map[string]*gogithub.RepositoryContent
	contentsErr map[string]error

	// environments and deployments are keyed by "owner/repo".
	environments    map[string]pages[gogithub.Environment]
	environmentsErr map[string]error
	deployments     map[string][]*gogithub.Deployment
	deploymentsErr  map[string]error
}

func (f *fakeRepos) ListByOrg(
	_ context.Context, _ string, opts *gogithub.RepositoryListByOrgOptions,
) ([]*gogithub.Repository, *gogithub.Response, error) {
	if f.reposErr != nil {
		return nil, nil, f.reposErr
	}
	items, resp := f.repos.page(opts.Page)
	return items, resp, nil
}

func (f *fakeRepos) GetContents(
	_ context.Context, owner, repo, path string, _ *gogithub.RepositoryContentGetOptions,
) (*gogithub.RepositoryContent, []*gogithub.RepositoryContent, *gogithub.Response, error) {
	key := fmt.Sprintf("%s/%s/%s", owner, repo, path)
	if err, bad := f.contentsErr[key]; bad {
		return nil, nil, nil, err
	}
	return f.contents[key], nil, &gogithub.Response{}, nil
}

func (f *fakeRepos) ListEnvironments(
	_ context.Context, owner, repo string, opts *gogithub.EnvironmentListOptions,
) (*gogithub.EnvResponse, *gogithub.Response, error) {
	key := owner + "/" + repo
	if err, bad := f.environmentsErr[key]; bad {
		return nil, nil, err
	}
	items, resp := f.environments[key].page(opts.Page)
	return &gogithub.EnvResponse{Environments: items}, resp, nil
}

func (f *fakeRepos) ListDeployments(
	_ context.Context, owner, repo string, _ *gogithub.DeploymentsListOptions,
) ([]*gogithub.Deployment, *gogithub.Response, error) {
	key := owner + "/" + repo
	if err, bad := f.deploymentsErr[key]; bad {
		return nil, nil, err
	}
	return f.deployments[key], &gogithub.Response{}, nil
}

// fakeActions serves the Actions half of the provider.
type fakeActions struct {
	// workflows, runs, repoRunners and repoSecrets are keyed by "owner/repo";
	// envSecrets by "<repository id>/<environment name>", which is how the
	// provider's own call is keyed.
	workflows    map[string]pages[gogithub.Workflow]
	workflowsErr map[string]error
	// workflowsFailAfter refuses every page AFTER the numbered one, which is how
	// a read that answered part of its answer and then failed is reproduced.
	workflowsFailAfter map[string]int
	runs               map[string]*gogithub.WorkflowRuns
	runsErr            map[string]error

	orgRunners     pages[gogithub.Runner]
	orgRunnersErr  error
	repoRunners    map[string]pages[gogithub.Runner]
	repoRunnersErr map[string]error

	orgSecrets     pages[gogithub.Secret]
	orgSecretsErr  error
	repoSecrets    map[string]pages[gogithub.Secret]
	repoSecretsErr map[string]error
	envSecrets     map[string]pages[gogithub.Secret]
	envSecretsErr  map[string]error
}

func (f *fakeActions) ListWorkflows(
	_ context.Context, owner, repo string, opts *gogithub.ListOptions,
) (*gogithub.Workflows, *gogithub.Response, error) {
	key := owner + "/" + repo
	if err, bad := f.workflowsErr[key]; bad {
		return nil, nil, err
	}
	if after, set := f.workflowsFailAfter[key]; set && opts.Page > after {
		return nil, nil, refusal(500, "Server Error")
	}
	items, resp := f.workflows[key].page(opts.Page)
	return &gogithub.Workflows{Workflows: items}, resp, nil
}

func (f *fakeActions) ListRepositoryWorkflowRuns(
	_ context.Context, owner, repo string, _ *gogithub.ListWorkflowRunsOptions,
) (*gogithub.WorkflowRuns, *gogithub.Response, error) {
	key := owner + "/" + repo
	if err, bad := f.runsErr[key]; bad {
		return nil, nil, err
	}
	return f.runs[key], &gogithub.Response{}, nil
}

func (f *fakeActions) ListOrganizationRunners(
	_ context.Context, _ string, opts *gogithub.ListRunnersOptions,
) (*gogithub.Runners, *gogithub.Response, error) {
	if f.orgRunnersErr != nil {
		return nil, nil, f.orgRunnersErr
	}
	items, resp := f.orgRunners.page(opts.Page)
	return &gogithub.Runners{Runners: items}, resp, nil
}

func (f *fakeActions) ListRunners(
	_ context.Context, owner, repo string, opts *gogithub.ListRunnersOptions,
) (*gogithub.Runners, *gogithub.Response, error) {
	key := owner + "/" + repo
	if err, bad := f.repoRunnersErr[key]; bad {
		return nil, nil, err
	}
	items, resp := f.repoRunners[key].page(opts.Page)
	return &gogithub.Runners{Runners: items}, resp, nil
}

func (f *fakeActions) ListOrgSecrets(
	_ context.Context, _ string, opts *gogithub.ListOptions,
) (*gogithub.Secrets, *gogithub.Response, error) {
	if f.orgSecretsErr != nil {
		return nil, nil, f.orgSecretsErr
	}
	items, resp := f.orgSecrets.page(opts.Page)
	return &gogithub.Secrets{Secrets: items}, resp, nil
}

func (f *fakeActions) ListRepoSecrets(
	_ context.Context, owner, repo string, opts *gogithub.ListOptions,
) (*gogithub.Secrets, *gogithub.Response, error) {
	key := owner + "/" + repo
	if err, bad := f.repoSecretsErr[key]; bad {
		return nil, nil, err
	}
	items, resp := f.repoSecrets[key].page(opts.Page)
	return &gogithub.Secrets{Secrets: items}, resp, nil
}

func (f *fakeActions) ListEnvSecrets(
	_ context.Context, repoID int, env string, opts *gogithub.ListOptions,
) (*gogithub.Secrets, *gogithub.Response, error) {
	key := fmt.Sprintf("%d/%s", repoID, env)
	if err, bad := f.envSecretsErr[key]; bad {
		return nil, nil, err
	}
	items, resp := f.envSecrets[key].page(opts.Page)
	return &gogithub.Secrets{Secrets: items}, resp, nil
}

// compile-time proof that the fixtures satisfy the whole of both interfaces. A
// fake that had drifted from one would otherwise fail at its first use inside a
// test, which reads as a broken test rather than an incomplete fixture.
var (
	_ collect.ReposAPI   = (*fakeRepos)(nil)
	_ collect.ActionsAPI = (*fakeActions)(nil)
)
