// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v68/github"
)

// api.go — the narrow list interfaces this package enumerates through, and the
// shared repository walk six of the seven enumerations start from.
//
// THE INTERFACES CARRY EXACTLY THE CALLS THIS COLLECTOR MAKES and not one more.
// That is what makes a recorded fixture a complete stand-in rather than a
// partial one: a test implementing these two interfaces has answered every
// question this collector can ask, so a walk over recorded responses reaches the
// same code an operator's walk does.

// ReposAPI is the repositories half of the provider's API.
type ReposAPI interface {
	ListByOrg(ctx context.Context, org string, opts *gogithub.RepositoryListByOrgOptions) (
		[]*gogithub.Repository, *gogithub.Response, error)
	GetContents(ctx context.Context, owner, repo, path string, opts *gogithub.RepositoryContentGetOptions) (
		*gogithub.RepositoryContent, []*gogithub.RepositoryContent, *gogithub.Response, error)
	ListEnvironments(ctx context.Context, owner, repo string, opts *gogithub.EnvironmentListOptions) (
		*gogithub.EnvResponse, *gogithub.Response, error)
	ListDeployments(ctx context.Context, owner, repo string, opts *gogithub.DeploymentsListOptions) (
		[]*gogithub.Deployment, *gogithub.Response, error)
}

// ActionsAPI is the Actions half of the provider's API.
type ActionsAPI interface {
	ListWorkflows(ctx context.Context, owner, repo string, opts *gogithub.ListOptions) (
		*gogithub.Workflows, *gogithub.Response, error)
	ListRepositoryWorkflowRuns(ctx context.Context, owner, repo string, opts *gogithub.ListWorkflowRunsOptions) (
		*gogithub.WorkflowRuns, *gogithub.Response, error)
	ListOrganizationRunners(ctx context.Context, org string, opts *gogithub.ListRunnersOptions) (
		*gogithub.Runners, *gogithub.Response, error)
	ListRunners(ctx context.Context, owner, repo string, opts *gogithub.ListRunnersOptions) (
		*gogithub.Runners, *gogithub.Response, error)
	ListOrgSecrets(ctx context.Context, org string, opts *gogithub.ListOptions) (
		*gogithub.Secrets, *gogithub.Response, error)
	ListRepoSecrets(ctx context.Context, owner, repo string, opts *gogithub.ListOptions) (
		*gogithub.Secrets, *gogithub.Response, error)
	ListEnvSecrets(ctx context.Context, repoID int, env string, opts *gogithub.ListOptions) (
		*gogithub.Secrets, *gogithub.Response, error)
}

// API bundles the two halves, which is what every enumeration is built from.
type API struct {
	Repos   ReposAPI
	Actions ActionsAPI
}

// perPage is the page size every paginated read asks for. It is the provider's
// own maximum: a smaller page is the same total number of items in more round
// trips, and this collector's cost is round trips rather than bytes.
const perPage = 100

// listRepositories pages the organization's repositories to exhaustion and
// returns the ones that are not archived.
//
// ARCHIVED REPOSITORIES ARE SKIPPED, which is the source provider's own behavior
// and is reproduced deliberately: an archived repository runs nothing, so its
// workflows, runs, runners, environments, deployments and secrets are inventory
// of something that cannot execute.
//
// A FAILURE HERE FAILS THE WHOLE ENUMERATION rather than being recorded as a
// partial read. Six of the seven enumerations start from this list, so a walk
// that could not read it did not fail to see PART of the organization — it never
// found out what the organization contains, and reporting that as a partial read
// would put an empty inventory behind a mark an operator might reasonably ignore.
func listRepositories(ctx context.Context, api ReposAPI, org string) ([]*gogithub.Repository, error) {
	opts := &gogithub.RepositoryListByOrgOptions{
		ListOptions: gogithub.ListOptions{PerPage: perPage},
		Type:        "all",
	}
	var out []*gogithub.Repository
	for {
		page, resp, err := api.ListByOrg(ctx, org, opts)
		if err != nil {
			return nil, fmt.Errorf("listing the repositories of %q: %w", org, err)
		}
		for _, repo := range page {
			if repo.GetArchived() {
				continue
			}
			out = append(out, repo)
		}
		// THE PROVIDER DECIDES WHEN THE PAGING ENDS, not the item count. A full
		// page is not the last page and a short page is not necessarily the last
		// one either; NextPage == 0 is the provider saying so, and reading the
		// count instead is the off-by-one that loses the whole tail of an
		// organization with exactly 100 repositories.
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

// repoNames is the non-archived repository full names, in the provider's own
// `owner/name` spelling.
func repoNames(repos []*gogithub.Repository) []string {
	names := make([]string, 0, len(repos))
	for _, repo := range repos {
		names = append(names, repo.GetFullName())
	}
	return names
}

// splitFullName splits `owner/name`. A value carrying no separator is returned
// whole as the owner with an empty name, which is what the source provider does;
// the resulting request fails and is recorded as a failed read for that scope
// rather than being silently skipped here.
func splitFullName(fullName string) (owner, name string) {
	for i := range fullName {
		if fullName[i] == '/' {
			return fullName[:i], fullName[i+1:]
		}
	}
	return fullName, ""
}
