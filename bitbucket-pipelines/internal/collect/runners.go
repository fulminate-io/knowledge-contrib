// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbclient"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
)

// runners.go — the workspace's runners and each repository's, and the label
// nodes their labels resolve to.
//
// A RUNNER IS ONE NODE HOWEVER MANY SCOPES REACH IT. The provider's runner UUIDs
// are unique across the workspace and a repository-scoped listing can return a
// workspace runner, so the enumeration deduplicates by UUID as the source
// provider does; the first scope that saw it decides the scope metadata and the
// parent it belongs to.
//
// A LABEL IS ONE NODE PER DISTINCT NAME ACROSS THE WHOLE WALK, and that is a fix
// rather than a reproduction. The source provider mints a label resource inside
// its per-runner loop with no dedup across runners, so two runners carrying
// `self-hosted` produced two resources with an identical id. The graph builder
// deduplicates by id, which makes the rule true rather than merely intended.

// Runners enumerates the workspace's runners and every repository's.
func Runners(client *bbclient.Client, repos []RepoInfo) Subcollector {
	return Subcollector{
		Name: "bitbucket-runners",
		Run: func(ctx context.Context, workspace string) (bbgraph.Result, error) {
			var out bbgraph.Result
			var failures reads
			seen := map[string]bool{}

			workspaceRunners, err := listRunners(ctx, client,
				fmt.Sprintf("workspaces/%s/pipelines-config/runners", workspace))
			out.Add(buildRunners(workspace, workspaceRunners, "workspace", "", seen, &failures))
			failures.record("the workspace's runners", err)

			for _, repo := range repos {
				if err := ctx.Err(); err != nil {
					return out, err
				}
				repoRunners, err := listRunners(ctx, client,
					fmt.Sprintf("repositories/%s/%s/pipelines-config/runners", workspace, repo.Slug))
				out.Add(buildRunners(workspace, repoRunners, "repository", repo.Slug, seen, &failures))
				failures.record(repo.Slug, err)
			}
			return out, failures.err("bitbucket-runners")
		},
	}
}

// listRunners pages one runners endpoint to exhaustion, returning what it read
// alongside any failure.
func listRunners(
	ctx context.Context, client *bbclient.Client, path string,
) ([]apiRunner, error) {
	var runners []apiRunner
	err := client.GetPaginated(ctx, path, func(raw json.RawMessage) error {
		var page []apiRunner
		if err := json.Unmarshal(raw, &page); err != nil {
			return fmt.Errorf("decoding a runners page: %w", err)
		}
		runners = append(runners, page...)
		return nil
	})
	switch {
	case err == nil:
		return runners, nil
	case IsNotFound(err):
		// A SCOPE WITH NO RUNNERS ANSWERS 200 WITH AN EMPTY PAGE — measured on the
		// live provider, on a repository that has none — so a 404 here is the URL
		// not being there rather than the scope being empty.
		return runners, notFoundOnAListing("runners", path, err)
	default:
		return runners, fmt.Errorf("reading runners: %w", err)
	}
}

// buildRunners converts one scope's runners, skipping any already seen.
func buildRunners(
	workspace string, runners []apiRunner, scope, repoSlug string, seen map[string]bool,
	failures *reads,
) bbgraph.Result {
	var out bbgraph.Result
	for _, runner := range runners {
		if seen[runner.UUID] {
			continue
		}
		seen[runner.UUID] = true
		out.Add(buildRunner(workspace, runner, scope, repoSlug, failures))
	}
	return out
}

// buildRunner converts one runner: its node, its edge to whatever owns it, and
// one label node plus one HAS_LABEL edge per label it carries.
func buildRunner(
	workspace string, runner apiRunner, scope, repoSlug string, failures *reads,
) bbgraph.Result {
	runnerID := bbgraph.RunnerID(workspace, runner.UUID)
	labels := labelNames(runner.Labels)

	content, ok := renderContent(failures, "runner "+runner.UUID, runner)
	if !ok {
		return bbgraph.Result{}
	}

	metadata := map[string]string{
		"workspace": workspace,
		"state":     runner.State.Status,
		"scope":     scope,
	}
	putIfSet(metadata, "repo", repoSlug)
	putIfSet(metadata, "labels", strings.Join(labels, ","))

	// A REPOSITORY-SCOPED RUNNER BELONGS TO ITS REPOSITORY AND A WORKSPACE-SCOPED
	// ONE TO THE WORKSPACE. Both arms are the source provider's and both are
	// exercised by the fixture corpus.
	parentID := bbgraph.WorkspaceID(workspace)
	if repoSlug != "" {
		parentID = bbgraph.RepositoryID(workspace, repoSlug)
	}

	out := bbgraph.Result{
		Resources: []bbgraph.Resource{{
			ID:           runnerID,
			Name:         runner.Name,
			ResourceType: bbgraph.ResourceTypeRunner,
			Content:      content,
			Metadata:     metadata,
		}},
		Relations: []bbgraph.Relation{{
			FromID: runnerID,
			ToID:   parentID,
			Type:   bbgraph.EdgeBelongsTo,
		}},
	}

	for _, label := range labels {
		labelID := bbgraph.LabelID(workspace, label)
		// THE LABEL NODE CARRIES NO CONTENT, which is the source provider's shape:
		// a label is a name and the workspace it is in, and there is nothing else
		// the provider returns about one.
		out.Resources = append(out.Resources, bbgraph.Resource{
			ID:           labelID,
			Name:         label,
			ResourceType: bbgraph.ResourceTypeLabel,
			Metadata:     map[string]string{"workspace": workspace},
		})
		out.Relations = append(out.Relations, bbgraph.Relation{
			FromID: runnerID,
			ToID:   labelID,
			Type:   bbgraph.EdgeHasLabel,
		})
	}
	return out
}

// labelNames is the runner's label names, dropping any the provider returned
// without one.
//
// A NODE KEYED ON AN EMPTY NAME would be one node standing for every unnamed
// label in the workspace, which is why the empty ones are dropped rather than
// carried.
func labelNames(labels []apiLabel) []string {
	out := make([]string, 0, len(labels))
	for _, label := range labels {
		if label.Name != "" {
			out = append(out, label.Name)
		}
	}
	return out
}

// apiRunner is the runner shape this collector reads.
type apiRunner struct {
	UUID   string     `json:"uuid"`
	Name   string     `json:"name"`
	Labels []apiLabel `json:"labels"`
	State  struct {
		Status string `json:"status"`
	} `json:"state"`
}

// apiLabel is one of a runner's labels.
type apiLabel struct {
	Name string `json:"name"`
}
