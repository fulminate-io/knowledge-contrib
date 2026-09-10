// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// runners.go — the self-hosted runners registered at the organization and at
// each repository, and the label nodes their labels resolve to.

// runnerDetail is the runner node's Content.
type runnerDetail struct {
	Name   string   `json:"name"`
	OS     string   `json:"os"`
	Status string   `json:"status"`
	Busy   bool     `json:"busy"`
	Labels []string `json:"labels"`
}

// Runners enumerates the organization's runners and every repository's.
func Runners(api API) Subcollector {
	return Subcollector{
		Name: "github-runners",
		Run: func(ctx context.Context, org string) (ghgraph.Result, error) {
			var out ghgraph.Result
			var failures reads

			orgRunners, err := orgRunners(ctx, api, org)
			out.Add(orgRunners)
			failures.record("the organization's runners", err)

			repos, err := listRepositories(ctx, api.Repos, org)
			if err != nil {
				return out, err
			}
			for _, fullName := range repoNames(repos) {
				got, err := repoRunners(ctx, api, org, fullName)
				out.Add(got)
				failures.record(fullName, err)
			}
			return out, failures.err("github-runners")
		},
	}
}

// orgRunners pages the organization-level runners to exhaustion.
func orgRunners(ctx context.Context, api API, org string) (ghgraph.Result, error) {
	return pageRunners(org, "", func(opts *gogithub.ListRunnersOptions) (
		*gogithub.Runners, *gogithub.Response, error,
	) {
		return api.Actions.ListOrganizationRunners(ctx, org, opts)
	})
}

// repoRunners pages one repository's runners to exhaustion.
func repoRunners(ctx context.Context, api API, org, fullName string) (ghgraph.Result, error) {
	owner, repo := splitFullName(fullName)
	return pageRunners(org, fullName, func(opts *gogithub.ListRunnersOptions) (
		*gogithub.Runners, *gogithub.Response, error,
	) {
		return api.Actions.ListRunners(ctx, owner, repo, opts)
	})
}

// pageRunners is the shared walk over both runner scopes. repoFullName is empty
// for the organization scope, which is what selects the runner's parent.
func pageRunners(
	org, repoFullName string,
	list func(*gogithub.ListRunnersOptions) (*gogithub.Runners, *gogithub.Response, error),
) (ghgraph.Result, error) {
	opts := &gogithub.ListRunnersOptions{ListOptions: gogithub.ListOptions{PerPage: perPage}}

	var out ghgraph.Result
	for {
		page, resp, err := list(opts)
		if err != nil {
			return out, fmt.Errorf("listing runners: %w", err)
		}
		if page != nil {
			for _, runner := range page.Runners {
				converted, err := runnerResource(org, repoFullName, runner)
				if err != nil {
					return out, err
				}
				out.Add(converted)
			}
		}
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

// runnerResource converts one runner into its node, its edge to its parent and
// one label node and edge per label it carries.
//
// THE LABEL NODE IS MINTED HERE AND DEDUPLICATED LATER, which is the whole
// mechanism behind one node per distinct label. Its id carries the organization
// and the label name and nothing else, so every runner carrying `self-hosted`
// mints the same node and the graph builder keeps one — while each runner keeps
// its own edge into it. Deduplicating here instead would need state threaded
// through both scopes and would still be wrong across the concurrent fan-out.
func runnerResource(org, repoFullName string, runner *gogithub.Runner) (ghgraph.Result, error) {
	if runner == nil {
		return ghgraph.Result{}, fmt.Errorf("github-runners: the provider returned a nil runner")
	}
	labels := runnerLabelNames(runner)
	content, err := marshalDetail("github-runners", runner.GetName(), runnerDetail{
		Name:   runner.GetName(),
		OS:     runner.GetOS(),
		Status: runner.GetStatus(),
		Busy:   runner.GetBusy(),
		Labels: labels,
	})
	if err != nil {
		return ghgraph.Result{}, err
	}

	metadata := map[string]string{
		"org":    org,
		"status": runner.GetStatus(),
		"os":     runner.GetOS(),
	}
	parent := ghgraph.OrganizationID(org)
	if repoFullName != "" {
		metadata["repo"] = repoFullName
		parent = ghgraph.RepositoryID(org, repoFullName)
	}

	id := ghgraph.RunnerID(org, runner.GetID())
	out := ghgraph.Result{
		Resources: []ghgraph.Resource{{
			ID:           id,
			Name:         runner.GetName(),
			ResourceType: ghgraph.ResourceTypeRunner,
			Content:      content,
			Metadata:     metadata,
		}},
		Relations: []ghgraph.Relation{{
			FromID: id,
			ToID:   parent,
			Type:   ghgraph.EdgeBelongsTo,
		}},
	}
	for _, label := range labels {
		labelNodeID := ghgraph.LabelID(org, label)
		out.Resources = append(out.Resources, ghgraph.Resource{
			ID:           labelNodeID,
			Name:         label,
			ResourceType: ghgraph.ResourceTypeLabel,
			Metadata:     map[string]string{"org": org},
		})
		out.Relations = append(out.Relations, ghgraph.Relation{
			FromID: id,
			ToID:   labelNodeID,
			Type:   ghgraph.EdgeHasLabel,
		})
	}
	return out, nil
}

// runnerLabelNames is a runner's label names, dropping an unnamed one.
//
// AN EMPTY NAME IS DROPPED RATHER THAN CARRIED. A label node keyed on an empty
// name would be one node standing for every unnamed label in the organization,
// which asserts a grouping the provider does not.
func runnerLabelNames(runner *gogithub.Runner) []string {
	names := make([]string, 0, len(runner.Labels))
	for _, label := range runner.Labels {
		if name := label.GetName(); name != "" {
			names = append(names, name)
		}
	}
	return names
}
