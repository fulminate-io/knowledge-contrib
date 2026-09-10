// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbclient"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
)

// repos.go — the workspace's repositories, and the workspace node itself.
//
// THIS ENUMERATION RUNS ALONE AND FIRST, and it is the only one that does. Every
// other enumeration needs the repository list and each repository's main branch,
// so the walk reads this one synchronously and hands its result to the five that
// fan out after it. The cost is stated rather than hidden: a failure HERE takes
// the whole walk down, because a walk with no repository list has nothing to
// enumerate. See the walk package for why that coupling is kept.

// RepoInfo is what the five later enumerations need per repository.
type RepoInfo struct {
	// Slug is the provider's own repository slug, which every request path and
	// every repository-scoped node id is built from.
	Slug string
	// Mainbranch is the branch a pipeline definition is read from. Empty means
	// the provider returned none, and the pipelines-config enumeration falls back
	// to "main".
	Mainbranch string
}

// Repositories enumerates the workspace's repositories.
func Repositories(client *bbclient.Client) Subcollector {
	return Subcollector{
		Name: "bitbucket-repos",
		Run: func(ctx context.Context, workspace string) (bbgraph.Result, error) {
			repos, err := listRepos(ctx, client, workspace)
			if err != nil {
				return bbgraph.Result{}, fmt.Errorf("listing the workspace's repositories: %w", err)
			}
			var failures reads
			return buildRepos(workspace, repos, &failures), failures.err("bitbucket-repos")
		},
	}
}

// RepoInfos extracts what the later enumerations need from a repos result.
func RepoInfos(result bbgraph.Result) []RepoInfo {
	var out []RepoInfo
	for _, res := range result.Resources {
		if res.ResourceType != bbgraph.ResourceTypeRepository {
			continue
		}
		out = append(out, RepoInfo{
			Slug:       res.Metadata["slug"],
			Mainbranch: res.Metadata["mainbranch"],
		})
	}
	return out
}

// listRepos pages the workspace's repositories to exhaustion.
func listRepos(
	ctx context.Context, client *bbclient.Client, workspace string,
) ([]apiRepo, error) {
	var repos []apiRepo
	err := client.GetPaginated(ctx, fmt.Sprintf("repositories/%s", workspace),
		func(raw json.RawMessage) error {
			var page []apiRepo
			if err := json.Unmarshal(raw, &page); err != nil {
				return fmt.Errorf("decoding a repositories page: %w", err)
			}
			repos = append(repos, page...)
			return nil
		})
	if err != nil {
		return nil, err
	}
	return repos, nil
}

// buildRepos converts the API repositories into the workspace node, one node per
// repository, and a BELONGS_TO edge from each into the workspace.
//
// A WORKSPACE WITH NO REPOSITORIES EMITS NOTHING AT ALL, not even the workspace
// node, and that is the source provider's behavior carried forward deliberately:
// a real but empty workspace collects to zero nodes. It is asserted by a test
// rather than left to be discovered.
func buildRepos(workspace string, repos []apiRepo, failures *reads) bbgraph.Result {
	if len(repos) == 0 {
		return bbgraph.Result{}
	}

	workspaceID := bbgraph.WorkspaceID(workspace)
	out := bbgraph.Result{Resources: []bbgraph.Resource{{
		ID:           workspaceID,
		Name:         workspace,
		ResourceType: bbgraph.ResourceTypeWorkspace,
		Metadata:     map[string]string{"workspace": workspace},
	}}}

	for _, repo := range repos {
		repoID := bbgraph.RepositoryID(workspace, repo.Slug)
		metadata := map[string]string{
			"workspace":  workspace,
			"slug":       repo.Slug,
			"is_private": boolText(repo.IsPrivate),
		}
		// THE THREE CONDITIONAL KEYS. Each is written only when the provider
		// returned a value, so a repository with no declared language carries no
		// `language` key rather than an empty one — the distinction a consumer
		// filtering on the key depends on.
		putIfSet(metadata, "mainbranch", repo.Mainbranch.Name)
		putIfSet(metadata, "language", repo.Language)
		putIfSet(metadata, "scm", repo.SCM)

		content, ok := renderContent(failures, repo.Slug, repo)
		if !ok {
			continue
		}
		out.Resources = append(out.Resources, bbgraph.Resource{
			ID:           repoID,
			Name:         repo.FullName,
			ResourceType: bbgraph.ResourceTypeRepository,
			Content:      content,
			Metadata:     metadata,
		})
		out.Relations = append(out.Relations, bbgraph.Relation{
			FromID: repoID,
			ToID:   workspaceID,
			Type:   bbgraph.EdgeBelongsTo,
		})
	}
	return out
}

// apiRepo is the repository shape this collector reads out of the API response.
type apiRepo struct {
	UUID       string `json:"uuid"`
	Slug       string `json:"slug"`
	FullName   string `json:"full_name"`
	IsPrivate  bool   `json:"is_private"`
	SCM        string `json:"scm"`
	Language   string `json:"language"`
	Mainbranch struct {
		Name string `json:"name"`
	} `json:"mainbranch"`
}
