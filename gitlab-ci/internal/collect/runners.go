// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"fmt"
	"strconv"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// runners.go — the runners registered at the group and at each project, and the
// tag nodes their tag lists resolve to.
//
// A RUNNER IS EMITTED ONCE, UNDER THE FIRST SCOPE THAT REACHED IT. A GitLab
// runner id is unique across the instance and a shared runner is visible to every
// project it is assigned to, so a walk that emitted one per project would mint
// the same node twenty times and claim twenty parents for one machine. The `seen`
// map is the source provider's own, and it is kept.
//
// WHICH PARENT A SHARED RUNNER GETS IS DECIDED HERE AND IT IS DETERMINISTIC. The
// group scope is walked FIRST, so a runner available at group level belongs to
// the group; a runner only a project can see belongs to the LOWEST-SORTING
// project that can see it, because the shared project discovery hands its
// projects over sorted by path. Two collects of an unchanged group therefore
// agree, which a first-caller-wins rule over an unordered list would not.
//
// THE PARENT EDGE ITSELF IS THIS COLLECTOR'S OWN. The source provider writes a
// runner node and its tag edges and no parent edge at all, so a GitLab runner sat
// in the graph unattached while the sibling GitHub and Bitbucket providers both
// attach theirs. That is a cross-provider inconsistency rather than a property of
// GitLab, so the edge is emitted here: to the group node for a group-scoped
// runner and to the project node for a project-scoped one, in the id form each of
// those nodes already carries.

// Runners enumerates the group's runners and every project's.
func Runners(api API, lister *projectLister, group string) Subcollector {
	return Subcollector{
		Name: "gitlab-runners",
		Run: func(ctx context.Context) (glgraph.Result, error) {
			var out glgraph.Result
			var failures reads
			seen := make(map[int64]bool)

			got, err := groupRunners(ctx, api, group, seen, &failures)
			out.Add(got)
			failures.record("the group's runners", err)

			projects, err := lister.list(ctx)
			if err != nil {
				return out, err
			}
			for _, project := range projects {
				if project == nil {
					continue
				}
				got, projectErr := projectRunners(ctx, api, group, project, seen, &failures)
				out.Add(got)
				failures.record(project.PathWithNamespace, projectErr)
			}
			return out, failures.err("gitlab-runners")
		},
	}
}

// groupRunners pages the group-level runners to exhaustion.
func groupRunners(
	ctx context.Context, api API, group string, seen map[int64]bool, failures *reads,
) (glgraph.Result, error) {
	opts := &gl.ListGroupsRunnersOptions{ListOptions: gl.ListOptions{PerPage: perPage}}
	return pageRunners(ctx, api, group, glgraph.GroupID(group), "group", seen, failures,
		func() ([]*gl.Runner, *gl.Response, error) {
			return api.Runners.ListGroupsRunners(ctx, group, opts)
		},
		func(next int64) { opts.Page = next },
	)
}

// projectRunners pages one project's runners to exhaustion.
func projectRunners(
	ctx context.Context, api API, group string, project *gl.Project,
	seen map[int64]bool, failures *reads,
) (glgraph.Result, error) {
	opts := &gl.ListProjectRunnersOptions{ListOptions: gl.ListOptions{PerPage: perPage}}
	parent := glgraph.ProjectID(group, project.PathWithNamespace)
	return pageRunners(ctx, api, group, parent, "project", seen, failures,
		func() ([]*gl.Runner, *gl.Response, error) {
			return api.Runners.ListProjectRunners(ctx, project.ID, opts)
		},
		func(next int64) { opts.Page = next },
	)
}

// pageRunners is the shared walk over both runner scopes. parentID is the id of
// the node the runners found here belong to, which is what makes the two arms
// differ at all.
func pageRunners(
	ctx context.Context, api API, group, parentID, scope string,
	seen map[int64]bool, failures *reads,
	list func() ([]*gl.Runner, *gl.Response, error),
	advance func(int64),
) (glgraph.Result, error) {
	var out glgraph.Result
	for {
		page, resp, err := list()
		if err != nil {
			return out, fmt.Errorf("listing runners: %w", err)
		}
		for _, runner := range page {
			converted, convErr := addRunner(ctx, api, group, parentID, scope, runner, seen, failures)
			if convErr != nil {
				return out, convErr
			}
			out.Add(converted)
		}
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		advance(resp.NextPage)
	}
}

// addRunner converts one runner into its node, its edge to its parent, and one
// tag node and edge per tag it carries.
//
// THE NODE AND ITS PARENT EDGE ARE BUILT BEFORE THE DETAIL READ, and that
// ordering is asserted rather than incidental. A runner's TAG LIST only comes
// back on the provider's per-runner detail call, so a runner whose detail read
// fails keeps its node and its parent edge and loses its tags — the walk is
// incomplete and names the runner, and what was already known is not thrown away
// to punish the part that was not.
//
// THE TAG NODE IS MINTED HERE AND DEDUPLICATED LATER, which is the whole
// mechanism behind one node per distinct tag. Its id carries the group and the
// tag name and nothing else, so every runner carrying `docker` mints the same
// node and the graph builder keeps one — while each runner keeps its own edge
// into it. The source provider mints inside this loop and deduplicates nowhere.
func addRunner(
	ctx context.Context, api API, group, parentID, scope string, runner *gl.Runner,
	seen map[int64]bool, failures *reads,
) (glgraph.Result, error) {
	if runner == nil {
		return glgraph.Result{}, fmt.Errorf("gitlab-runners: the provider returned a nil runner")
	}
	if seen[runner.ID] {
		return glgraph.Result{}, nil
	}
	seen[runner.ID] = true

	meta := map[string]string{
		"runner_type": scope,
		"status":      runner.Status,
		"active":      strconv.FormatBool(!runner.Paused),
		"online":      strconv.FormatBool(runner.Online),
	}
	if runner.Description != "" {
		meta["description"] = runner.Description
	}

	id := glgraph.RunnerID(group, runner.ID)
	out := glgraph.Result{
		Resources: []glgraph.Resource{{
			ID:           id,
			Name:         fmt.Sprintf("runner-%d", runner.ID),
			ResourceType: glgraph.ResourceTypeRunner,
			Metadata:     meta,
		}},
		Relations: []glgraph.Relation{{
			FromID: id,
			ToID:   parentID,
			Type:   glgraph.EdgeBelongsTo,
		}},
	}

	details, _, err := api.Runners.GetRunnerDetails(ctx, runner.ID)
	if err != nil {
		failures.record(fmt.Sprintf("the tags of runner %d", runner.ID), err)
		return out, nil
	}
	if details == nil {
		return out, nil
	}
	for _, tag := range details.TagList {
		// AN EMPTY TAG IS DROPPED RATHER THAN CARRIED. A tag node keyed on an
		// empty name would be one node standing for every unnamed tag in the
		// group, which asserts a grouping the provider does not.
		if tag == "" {
			continue
		}
		tagID := glgraph.RunnerTagID(group, tag)
		out.Resources = append(out.Resources, glgraph.Resource{
			ID:           tagID,
			Name:         tag,
			ResourceType: glgraph.ResourceTypeRunnerTag,
		})
		out.Relations = append(out.Relations, glgraph.Relation{
			FromID: id,
			ToID:   tagID,
			Type:   glgraph.EdgeHasLabel,
		})
	}
	return out, nil
}
