// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"fmt"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbclient"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
)

// pipelines_config.go — each repository's bitbucket-pipelines.yml, and the three
// classes of relationship a step declares.
//
// THE VARIABLE REFERENCES LEAVE HERE UNRESOLVED. A step's `$VAR` cannot be
// turned into an edge at this point: the variables that could satisfy it are
// read by an enumeration running in parallel with this one. They are carried on
// the result and joined once the whole walk has returned.

// defaultBranch is the branch a pipeline definition is read from when the
// provider returned no main branch for the repository.
const defaultBranch = "main"

// PipelinesConfig enumerates every repository's pipeline definition.
func PipelinesConfig(client *bbclient.Client, repos []RepoInfo) Subcollector {
	return Subcollector{
		Name: "bitbucket-pipelines-config",
		Run: func(ctx context.Context, workspace string) (bbgraph.Result, error) {
			var out bbgraph.Result
			var failures reads

			for _, repo := range repos {
				if err := ctx.Err(); err != nil {
					return out, err
				}
				got, err := repoPipelines(ctx, client, workspace, repo, &failures)
				out.Add(got)
				failures.record(repo.Slug, err)
			}
			return out, failures.err("bitbucket-pipelines-config")
		},
	}
}

// repoPipelines reads and converts one repository's definition.
func repoPipelines(
	ctx context.Context, client *bbclient.Client, workspace string, repo RepoInfo,
	failures *reads,
) (bbgraph.Result, error) {
	branch := repo.Mainbranch
	if branch == "" {
		branch = defaultBranch
	}
	path := fmt.Sprintf("repositories/%s/%s/src/%s/bitbucket-pipelines.yml",
		workspace, repo.Slug, branch)

	data, err := client.GetRaw(ctx, path)
	if err != nil {
		if IsNotFound(err) {
			// NO PIPELINE IS CONFIGURED FOR THIS REPOSITORY. The provider answers
			// 404 for a file that is not there, which is a true reading of the
			// repository rather than a read this walk failed to make.
			return bbgraph.Result{}, nil
		}
		return bbgraph.Result{}, fmt.Errorf("reading bitbucket-pipelines.yml on %s: %w", branch, err)
	}

	parsed, err := parsePipelinesYAML(data)
	if err != nil {
		return bbgraph.Result{}, fmt.Errorf("parsing bitbucket-pipelines.yml on %s: %w", branch, err)
	}
	return buildPipelines(workspace, repo.Slug, extractPipelines(parsed.Pipelines), failures), nil
}

// buildPipelines converts one repository's parsed definitions.
func buildPipelines(
	workspace, slug string, pipelines []parsedPipeline, failures *reads,
) bbgraph.Result {
	repoID := bbgraph.RepositoryID(workspace, slug)
	var out bbgraph.Result

	for _, pipeline := range pipelines {
		pipelineID := bbgraph.PipelineID(workspace, slug, pipeline.Name)
		content, ok := renderContent(failures, slug+" pipeline "+pipeline.Name, pipeline)
		if !ok {
			continue
		}
		out.Resources = append(out.Resources, bbgraph.Resource{
			ID:           pipelineID,
			Name:         slug + "/" + pipeline.Name,
			ResourceType: bbgraph.ResourceTypePipeline,
			Content:      content,
			Metadata: map[string]string{
				"workspace":   workspace,
				"repo":        slug,
				"trigger_key": pipeline.TriggerKey,
			},
		})
		out.Relations = append(out.Relations, bbgraph.Relation{
			FromID: pipelineID,
			ToID:   repoID,
			Type:   bbgraph.EdgeBelongsTo,
		})
		out.Add(stepRelations(slug, pipelineID, pipeline.Steps))
	}
	return out
}

// stepRelations carries the three classes of relationship a step DECLARES: the
// environment it deploys to, the runner labels it asks to run on, and the
// variables its script references. None of the three becomes an edge here: every
// one of them names a node a different enumeration reads.
//
// IT TAKES NO WORKSPACE, and the absence is the shape rather than an oversight.
// A workspace is what an id is BUILT from, and nothing here builds one; the
// references leave as names, and the join that turns them into ids is the only
// place that needs to know which workspace they belong to.
func stepRelations(
	slug, pipelineID string, steps []parsedStep,
) bbgraph.Result {
	var out bbgraph.Result
	for _, step := range steps {
		// THE ENVIRONMENT AND THE LABELS ARE CARRIED, NOT EMITTED. Both are names
		// out of this repository's YAML, and the nodes that would satisfy them are
		// read from the API by two other enumerations running in parallel with
		// this one — so at this point there is nothing to resolve them against,
		// exactly as there is nothing to resolve a `$VAR` against.
		if step.Deployment != "" {
			out.StepTargets = append(out.StepTargets, bbgraph.StepTarget{
				PipelineID: pipelineID,
				RepoSlug:   slug,
				Name:       step.Deployment,
				Edge:       bbgraph.EdgeDeploysTo,
			})
		}
		for _, label := range step.RunsOn {
			out.StepTargets = append(out.StepTargets, bbgraph.StepTarget{
				PipelineID: pipelineID,
				RepoSlug:   slug,
				Name:       label,
				Edge:       bbgraph.EdgeRunsIn,
			})
		}
		for _, ref := range step.VarRefs {
			out.SecretRefs = append(out.SecretRefs, bbgraph.SecretRef{
				PipelineID: pipelineID,
				RepoSlug:   slug,
				Deployment: step.Deployment,
				Name:       ref,
			})
		}
	}
	return out
}
