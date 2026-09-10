// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbclient"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
)

// pipeline_runs.go — each repository's recent pipeline runs, newest first, up to
// the history depth.
//
// THIS IS THE ONLY CAPPED ENUMERATION and it PAGINATES to the cap rather than
// reading a single page, which is why a depth above the provider's page maximum
// is honored rather than refused: the request asks for min(depth, page maximum)
// per page and stops once the accumulated count reaches the depth.

// errDepthReached stops the pagination at the history depth. It is a control
// signal rather than a failure, and it is absorbed by the only function that can
// see it.
var errDepthReached = errors.New("history depth reached")

// PipelineRuns enumerates every repository's recent runs.
func PipelineRuns(client *bbclient.Client, repos []RepoInfo, depth int) Subcollector {
	return Subcollector{
		Name: "bitbucket-pipeline-runs",
		Run: func(ctx context.Context, workspace string) (bbgraph.Result, error) {
			var out bbgraph.Result
			var failures reads

			for _, repo := range repos {
				if err := ctx.Err(); err != nil {
					return out, err
				}
				runs, err := repoRuns(ctx, client, workspace, repo.Slug, depth)
				out.Add(buildRuns(workspace, repo.Slug, runs, &failures))
				failures.record(repo.Slug, err)
			}
			return out, failures.err("bitbucket-pipeline-runs")
		},
	}
}

// repoRuns pages one repository's runs, newest first, stopping at the depth.
//
// IT RETURNS WHAT IT ALREADY READ ALONGSIDE ANY FAILURE. A read whose first page
// answered and whose second failed still saw the newest runs, and discarding
// them would turn a small gap into a whole missing repository.
func repoRuns(
	ctx context.Context, client *bbclient.Client, workspace, slug string, depth int,
) ([]apiPipelineRun, error) {
	path := fmt.Sprintf("repositories/%s/%s/pipelines?sort=-created_on&pagelen=%d",
		workspace, slug, min(depth, bbclient.MaxPagelen))

	var runs []apiPipelineRun
	err := client.GetPaginated(ctx, path, func(raw json.RawMessage) error {
		var page []apiPipelineRun
		if err := json.Unmarshal(raw, &page); err != nil {
			return fmt.Errorf("decoding a pipeline-runs page: %w", err)
		}
		runs = append(runs, page...)
		if len(runs) >= depth {
			runs = runs[:depth]
			return errDepthReached
		}
		return nil
	})
	switch {
	case err == nil, errors.Is(err, errDepthReached):
		return runs, nil
	case IsNotFound(err):
		// A REPOSITORY THAT HAS NEVER RUN A PIPELINE ANSWERS 200 WITH AN EMPTY
		// PAGE, so a 404 is a listing this walk could not read rather than a
		// repository with no history.
		return runs, notFoundOnAListing("pipeline runs", path, err)
	default:
		return runs, fmt.Errorf("reading pipeline runs: %w", err)
	}
}

// buildRuns converts the API runs into nodes and their edges to the repository.
func buildRuns(
	workspace, slug string, runs []apiPipelineRun, failures *reads,
) bbgraph.Result {
	repoID := bbgraph.RepositoryID(workspace, slug)
	var out bbgraph.Result

	for _, run := range runs {
		runID := bbgraph.PipelineRunID(workspace, slug, run.UUID)
		metadata := map[string]string{
			"workspace": workspace,
			"repo":      slug,
			"status":    runStatus(run.State),
		}
		// THE FIVE CONDITIONAL KEYS. A run still in flight has no completion time
		// and no duration, and a run triggered outside a branch or tag has no ref
		// name; each key is absent rather than empty in those states.
		putIfSet(metadata, "created_on", run.CreatedOn)
		putIfSet(metadata, "completed_on", run.CompletedOn)
		putIfPositive(metadata, "duration_seconds", run.DurationInSeconds)
		putIfSet(metadata, "branch", run.Target.RefName)
		putIfSet(metadata, "trigger_type", run.Trigger.Type)

		content, ok := renderContent(failures, fmt.Sprintf("%s run %s", slug, run.UUID), run)
		if !ok {
			continue
		}
		out.Resources = append(out.Resources, bbgraph.Resource{
			ID:           runID,
			Name:         fmt.Sprintf("%s #%d", slug, run.BuildNumber),
			ResourceType: bbgraph.ResourceTypePipelineRun,
			Content:      content,
			Metadata:     metadata,
		})
		out.Relations = append(out.Relations, bbgraph.Relation{
			FromID: runID,
			ToID:   repoID,
			Type:   bbgraph.EdgeBelongsTo,
		})
	}
	return out
}

// runStatus flattens the provider's nested state object into one word: the
// result if the run finished, else the stage it is in, else the state itself.
func runStatus(state apiState) string {
	switch {
	case state.Result.Name != "":
		return state.Result.Name
	case state.Stage.Name != "":
		return state.Stage.Name
	default:
		return state.Name
	}
}

// apiPipelineRun is the pipeline-run shape this collector reads.
type apiPipelineRun struct {
	UUID              string     `json:"uuid"`
	BuildNumber       int        `json:"build_number"`
	CreatedOn         string     `json:"created_on"`
	CompletedOn       string     `json:"completed_on"`
	DurationInSeconds int        `json:"duration_in_seconds"`
	State             apiState   `json:"state"`
	Target            apiTarget  `json:"target"`
	Trigger           apiTrigger `json:"trigger"`
}

type apiState struct {
	Name  string `json:"name"`
	Stage struct {
		Name string `json:"name"`
	} `json:"stage"`
	Result struct {
		Name string `json:"name"`
	} `json:"result"`
}

type apiTarget struct {
	RefName string `json:"ref_name"`
	RefType string `json:"ref_type"`
}

type apiTrigger struct {
	Type string `json:"type"`
}
