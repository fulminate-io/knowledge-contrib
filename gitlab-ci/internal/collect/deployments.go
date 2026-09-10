// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"fmt"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// deployments.go — each project's most recent deployments.
//
// THIS READ IS CAPPED AND DOES NOT PAGE, which is the source provider's own
// behavior: one page of the most recently created deployments per project, newest
// first. A deployment history is unbounded and grows forever, so an exhaustive
// read would make the collect's cost a function of how long the group has
// existed. What is different here is that the cap is a COLLECT PARAMETER with the
// source provider's number as its default; there it was an anonymous literal
// beside the page size, reachable by nobody.

// deploymentDetail is the deployment node's Content, narrowed to what describes
// the deployment.
type deploymentDetail struct {
	Ref         string `json:"ref,omitempty"`
	SHA         string `json:"sha,omitempty"`
	Status      string `json:"status,omitempty"`
	Environment string `json:"environment,omitempty"`
}

// Deployments enumerates each project's recent deployments.
func Deployments(api API, lister *projectLister, group string, maxDeployments int) Subcollector {
	return Subcollector{
		Name: "gitlab-deployments",
		Run: func(ctx context.Context) (glgraph.Result, error) {
			projects, err := lister.list(ctx)
			if err != nil {
				return glgraph.Result{}, err
			}

			var out glgraph.Result
			var failures reads
			for _, project := range projects {
				if project == nil {
					continue
				}
				got, projectErr := projectDeployments(ctx, api, group, project, maxDeployments)
				out.Add(got)
				failures.record(project.PathWithNamespace, projectErr)
			}
			return out, failures.err("gitlab-deployments")
		},
	}
}

// projectDeployments reads one page of one project's most recent deployments.
func projectDeployments(
	ctx context.Context, api API, group string, project *gl.Project, maxDeployments int,
) (glgraph.Result, error) {
	orderBy, sort := "created_at", "desc"
	opts := &gl.ListProjectDeploymentsOptions{
		ListOptions: gl.ListOptions{PerPage: int64(maxDeployments)},
		OrderBy:     &orderBy,
		Sort:        &sort,
	}
	path := project.PathWithNamespace

	page, _, err := api.Deployments.ListProjectDeployments(ctx, project.ID, opts)
	if err != nil {
		return glgraph.Result{}, fmt.Errorf("listing the deployments of %q: %w", path, err)
	}

	var out glgraph.Result
	for _, deployment := range page {
		if deployment == nil {
			continue
		}
		converted, convErr := deploymentResource(group, path, deployment)
		if convErr != nil {
			return out, convErr
		}
		out.Add(converted)
	}
	return out, nil
}

// deploymentResource converts one deployment into its node, its edge to the
// project and its edge to the environment it targeted.
//
// A DEPLOYMENT WITH NO ENVIRONMENT EMITS NO DEPLOYS_TO EDGE. The provider returns
// the environment as an optional object, and an edge built from an absent one
// would name the empty environment of that project — a node nothing mints and a
// relationship the provider never stated.
func deploymentResource(group, path string, deployment *gl.Deployment) (glgraph.Result, error) {
	environment := ""
	if deployment.Environment != nil {
		environment = deployment.Environment.Name
	}

	content, err := marshalDetail("gitlab-deployments",
		fmt.Sprintf("%s/%d", path, deployment.ID), deploymentDetail{
			Ref:         deployment.Ref,
			SHA:         deployment.SHA,
			Status:      deployment.Status,
			Environment: environment,
		})
	if err != nil {
		return glgraph.Result{}, err
	}

	meta := map[string]string{
		"project": path,
		"status":  deployment.Status,
		"ref":     deployment.Ref,
		"sha":     deployment.SHA,
	}
	if environment != "" {
		meta["environment"] = environment
	}

	id := glgraph.DeploymentID(group, path, deployment.ID)
	out := glgraph.Result{
		Resources: []glgraph.Resource{{
			ID:           id,
			Name:         fmt.Sprintf("deployment #%d", deployment.ID),
			ResourceType: glgraph.ResourceTypeDeployment,
			Content:      content,
			Metadata:     meta,
		}},
		Relations: []glgraph.Relation{{
			FromID: id,
			ToID:   glgraph.ProjectID(group, path),
			Type:   glgraph.EdgeBelongsTo,
		}},
	}
	if environment != "" {
		out.Relations = append(out.Relations, glgraph.Relation{
			FromID: id,
			ToID:   glgraph.EnvironmentID(group, path, environment),
			Type:   glgraph.EdgeDeploysTo,
		})
	}
	return out, nil
}
