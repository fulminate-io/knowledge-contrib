// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// deployments.go — the most recent deployments of every repository.
//
// LIKE THE RUNS, THIS ENUMERATION IS CAPPED AND DOES NOT PAGE, for the same
// reason: deployments accumulate without bound and the graph is for the current
// state of a pipeline, not its history. The cap is a collect parameter; see
// [Params].

// DefaultMaxDeployments is how many deployments per repository a collect reads
// when the caller names no cap. It is the source provider's own default, carried
// forward so a collect with no parameters produces the graph a consumer already
// has.
const DefaultMaxDeployments = 20

// deployDetail is the deployment node's Content.
type deployDetail struct {
	Environment string `json:"environment"`
	Ref         string `json:"ref,omitempty"`
	Task        string `json:"task,omitempty"`
	Description string `json:"description,omitempty"`
}

// Deployments enumerates each repository's most recent deployments.
func Deployments(api API, maxDeployments int) Subcollector {
	return Subcollector{
		Name: "github-deployments",
		Run: func(ctx context.Context, org string) (ghgraph.Result, error) {
			repos, err := listRepositories(ctx, api.Repos, org)
			if err != nil {
				return ghgraph.Result{}, err
			}

			var out ghgraph.Result
			var failures reads
			for _, fullName := range repoNames(repos) {
				got, err := deploymentsForRepo(ctx, api, org, fullName, maxDeployments)
				out.Add(got)
				failures.record(fullName, err)
			}
			return out, failures.err("github-deployments")
		},
	}
}

// deploymentsForRepo reads one page of one repository's deployments and keeps
// the first maxDeployments of them.
func deploymentsForRepo(
	ctx context.Context, api API, org, fullName string, maxDeployments int,
) (ghgraph.Result, error) {
	owner, repo := splitFullName(fullName)
	opts := &gogithub.DeploymentsListOptions{
		ListOptions: gogithub.ListOptions{PerPage: min(maxDeployments, perPage)},
	}
	page, _, err := api.Repos.ListDeployments(ctx, owner, repo, opts)
	if err != nil {
		return ghgraph.Result{}, fmt.Errorf("listing deployments: %w", err)
	}

	var out ghgraph.Result
	for i, deployment := range page {
		if i >= maxDeployments {
			break
		}
		converted, err := deploymentResource(org, fullName, deployment)
		if err != nil {
			return out, err
		}
		out.Add(converted)
	}
	return out, nil
}

// deploymentResource converts one deployment into its node and its edges.
func deploymentResource(
	org, fullName string, deployment *gogithub.Deployment,
) (ghgraph.Result, error) {
	if deployment == nil {
		return ghgraph.Result{}, fmt.Errorf(
			"github-deployments: the provider returned a nil deployment for %q", fullName)
	}
	environment := deployment.GetEnvironment()
	content, err := marshalDetail("github-deployments", fullName, deployDetail{
		Environment: environment,
		Ref:         deployment.GetRef(),
		Task:        deployment.GetTask(),
		Description: deployment.GetDescription(),
	})
	if err != nil {
		return ghgraph.Result{}, err
	}

	id := ghgraph.DeploymentID(org, fullName, deployment.GetID())
	out := ghgraph.Result{
		Resources: []ghgraph.Resource{{
			ID:           id,
			Name:         fmt.Sprintf("deploy/%s/%s", fullName, environment),
			ResourceType: ghgraph.ResourceTypeDeployment,
			Content:      content,
			Metadata: map[string]string{
				"org":         org,
				"repo":        fullName,
				"environment": environment,
				"ref":         deployment.GetRef(),
			},
		}},
		Relations: []ghgraph.Relation{{
			FromID: id,
			ToID:   ghgraph.RepositoryID(org, fullName),
			Type:   ghgraph.EdgeBelongsTo,
		}},
	}

	// DEPLOYS_TO the environment it deployed to, WHEN it names one. This edge's
	// target is an environment the environments enumeration produced from the
	// same repository, so it resolves — unlike the one a workflow's text
	// produces, which names whatever string the file contained.
	if environment != "" {
		out.Relations = append(out.Relations, ghgraph.Relation{
			FromID: id,
			ToID:   ghgraph.EnvironmentID(org, fullName, environment),
			Type:   ghgraph.EdgeDeploysTo,
		})
	}

	// CREATED_BY the person who created it, WHEN the provider names one. A
	// deployment has one originating person and no re-run, so it takes one class
	// where a run takes a pair; the field rides this response, so the edge costs
	// nothing beyond the read already made.
	users, err := userRelations(org, id, []namedUser{{deployment.Creator, ghgraph.EdgeCreatedBy}})
	if err != nil {
		return ghgraph.Result{}, err
	}
	out.Add(users)
	return out, nil
}
