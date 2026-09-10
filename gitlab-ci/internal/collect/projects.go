// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"fmt"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// projects.go — the group and its projects, and the discovery every other
// enumeration walks.

// projectDetail is the project node's Content: the provider's own answer,
// narrowed to the fields that describe the project.
//
// IT IS NARROWED RATHER THAN CARRIED WHOLE, and this is the type where that
// matters most. The provider's project value carries a `runners_token` — the
// registration credential a maintainer's token is allowed to read — so marshaling
// it whole, which is what the source provider does, puts a credential into a
// stored node, its summary input and its search index. Nothing here can carry one.
type projectDetail struct {
	Description   string   `json:"description,omitempty"`
	DefaultBranch string   `json:"default_branch,omitempty"`
	Visibility    string   `json:"visibility,omitempty"`
	WebURL        string   `json:"web_url,omitempty"`
	Topics        []string `json:"topics,omitempty"`
	Archived      bool     `json:"archived,omitempty"`
}

// Projects enumerates the group and every project under it.
func Projects(api API, lister *projectLister, group string) Subcollector {
	return Subcollector{
		Name: "gitlab-projects",
		Run: func(ctx context.Context) (glgraph.Result, error) {
			projects, err := lister.list(ctx)
			if err != nil {
				return glgraph.Result{}, err
			}

			out := glgraph.Result{Resources: []glgraph.Resource{GroupResource(group)}}
			for _, project := range projects {
				converted, convErr := projectResource(group, project)
				if convErr != nil {
					return glgraph.Result{}, convErr
				}
				out.Add(converted)
			}
			return out, nil
		},
	}
}

// GroupResource is the group node, which every walk emits unconditionally.
//
// IT IS THE ONE NODE WITH NO Content AND NO METADATA. The provider is never asked
// about the group itself — the id is the collect's own input — so there is no
// answer to carry, and a fabricated detail block would be this collector
// asserting something it did not read.
func GroupResource(group string) glgraph.Resource {
	return glgraph.Resource{
		ID:           glgraph.GroupID(group),
		Name:         group,
		ResourceType: glgraph.ResourceTypeGroup,
	}
}

// projectResource converts one project into its node and its edge to the group.
//
// AN ARCHIVED PROJECT IS EMITTED, not skipped, and it is recorded as archived in
// its metadata. That is the source provider's own behavior and it is reproduced
// deliberately: an archived GitLab project keeps its pipelines, environments,
// variables and runner registrations, and the group's inventory is what this
// collector is for. The sibling GitHub collector skips its archived
// repositories, and the two providers differ here on purpose.
func projectResource(group string, project *gl.Project) (glgraph.Result, error) {
	if project == nil {
		return glgraph.Result{}, fmt.Errorf("gitlab-projects: the provider returned a nil project")
	}
	path := project.PathWithNamespace
	if path == "" {
		return glgraph.Result{}, fmt.Errorf(
			"gitlab-projects: a project of the group %q carries no path_with_namespace, so it has "+
				"no stable id", group)
	}

	content, err := marshalDetail("gitlab-projects", path, projectDetail{
		Description:   project.Description,
		DefaultBranch: project.DefaultBranch,
		Visibility:    string(project.Visibility),
		WebURL:        project.WebURL,
		Topics:        project.Topics,
		Archived:      project.Archived,
	})
	if err != nil {
		return glgraph.Result{}, err
	}

	meta := map[string]string{
		"path_with_namespace": path,
		"web_url":             project.WebURL,
		"default_branch":      project.DefaultBranch,
		"visibility":          string(project.Visibility),
	}
	if project.Archived {
		meta["archived"] = "true"
	}

	id := glgraph.ProjectID(group, path)
	return glgraph.Result{
		Resources: []glgraph.Resource{{
			ID:           id,
			Name:         project.Name,
			ResourceType: glgraph.ResourceTypeProject,
			Content:      content,
			Metadata:     meta,
		}},
		Relations: []glgraph.Relation{{
			FromID: id,
			ToID:   glgraph.GroupID(group),
			Type:   glgraph.EdgeBelongsTo,
		}},
	}, nil
}
