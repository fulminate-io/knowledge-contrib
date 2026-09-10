// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// workflow_runs.go — the most recent workflow runs of every repository.
//
// THIS ENUMERATION IS CAPPED AND DOES NOT PAGE, and that is the source
// provider's shape carried forward. A busy repository has hundreds of thousands
// of runs and they are the one resource here that is unbounded in time; walking
// them to exhaustion would make a collect take hours and land a graph whose bulk
// is execution history nobody queries. What the cap buys is the RECENT state of
// each repository's pipelines, which is what the graph is for. The cap is a
// collect parameter so an operator who wants more can ask; see [Params].

// DefaultMaxRuns is how many runs per repository a collect reads when the caller
// names no cap. It is the source provider's own default, carried forward so a
// collect with no parameters produces the graph a consumer already has.
const DefaultMaxRuns = 10

// runDetail is the workflow-run node's Content.
type runDetail struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion,omitempty"`
	Branch     string `json:"branch,omitempty"`
	Event      string `json:"event"`
	HTMLURL    string `json:"html_url,omitempty"`
	RunNumber  int    `json:"run_number"`
}

// WorkflowRuns enumerates each repository's most recent runs, up to maxRuns.
func WorkflowRuns(api API, maxRuns int) Subcollector {
	return Subcollector{
		Name: "github-workflow-runs",
		Run: func(ctx context.Context, org string) (ghgraph.Result, error) {
			repos, err := listRepositories(ctx, api.Repos, org)
			if err != nil {
				return ghgraph.Result{}, err
			}

			var out ghgraph.Result
			var failures reads
			for _, fullName := range repoNames(repos) {
				got, err := runsForRepo(ctx, api, org, fullName, maxRuns)
				out.Add(got)
				failures.record(fullName, err)
			}
			return out, failures.err("github-workflow-runs")
		},
	}
}

// runsForRepo reads one page of one repository's runs and keeps the first
// maxRuns of them.
//
// THE PAGE SIZE IS THE CAP, BOUNDED BY THE PROVIDER'S OWN MAXIMUM, and the count
// is enforced again over the answer. Asking for the cap directly is one round
// trip instead of several; enforcing it again is what keeps the count exact when
// the provider returns more than it was asked for.
func runsForRepo(
	ctx context.Context, api API, org, fullName string, maxRuns int,
) (ghgraph.Result, error) {
	owner, repo := splitFullName(fullName)
	opts := &gogithub.ListWorkflowRunsOptions{
		ListOptions: gogithub.ListOptions{PerPage: min(maxRuns, perPage)},
	}
	page, _, err := api.Actions.ListRepositoryWorkflowRuns(ctx, owner, repo, opts)
	if err != nil {
		return ghgraph.Result{}, fmt.Errorf("listing workflow runs: %w", err)
	}
	if page == nil {
		return ghgraph.Result{}, nil
	}

	var out ghgraph.Result
	for i, run := range page.WorkflowRuns {
		if i >= maxRuns {
			break
		}
		converted, err := workflowRunResource(org, fullName, run)
		if err != nil {
			return out, err
		}
		out.Add(converted)
	}
	return out, nil
}

// workflowRunResource converts one run into its node and its edges.
func workflowRunResource(org, fullName string, run *gogithub.WorkflowRun) (ghgraph.Result, error) {
	if run == nil {
		return ghgraph.Result{}, fmt.Errorf(
			"github-workflow-runs: the provider returned a nil run for %q", fullName)
	}
	content, err := marshalDetail("github-workflow-runs", fullName, runDetail{
		Status:     run.GetStatus(),
		Conclusion: run.GetConclusion(),
		Branch:     run.GetHeadBranch(),
		Event:      run.GetEvent(),
		HTMLURL:    run.GetHTMLURL(),
		RunNumber:  run.GetRunNumber(),
	})
	if err != nil {
		return ghgraph.Result{}, err
	}

	id := ghgraph.WorkflowRunID(org, fullName, run.GetID())
	out := ghgraph.Result{Resources: []ghgraph.Resource{{
		ID:           id,
		Name:         fmt.Sprintf("%s #%d", run.GetName(), run.GetRunNumber()),
		ResourceType: ghgraph.ResourceTypeWorkflowRun,
		Content:      content,
		Metadata: map[string]string{
			"org":        org,
			"repo":       fullName,
			"status":     run.GetStatus(),
			"conclusion": run.GetConclusion(),
			"event":      run.GetEvent(),
		},
	}}}

	// BELONGS_TO the workflow it ran, WHEN the run names one. A run carries the
	// workflow's path only where the provider knows it; without it there is no
	// workflow id to name, and an edge to a guessed path would resolve to
	// nothing while looking exactly like one that resolves.
	if path := run.GetPath(); path != "" {
		out.Relations = append(out.Relations, ghgraph.Relation{
			FromID: id,
			ToID:   ghgraph.WorkflowID(org, fullName, path),
			Type:   ghgraph.EdgeBelongsTo,
		})
	}

	// TRIGGERED_BY names the REPOSITORY and carries the event in its evidence.
	// The event is not a node — `push` and `schedule` are kinds of occurrence
	// rather than things with an identity — so the edge points at the repository
	// the trigger fired in and says which kind it was.
	if event := run.GetEvent(); event != "" {
		evidence, err := marshalDetail("github-workflow-runs", id, map[string]string{"event": event})
		if err != nil {
			return ghgraph.Result{}, err
		}
		out.Relations = append(out.Relations, ghgraph.Relation{
			FromID:   id,
			ToID:     ghgraph.RepositoryID(org, fullName),
			Type:     ghgraph.EdgeTriggeredBy,
			Evidence: evidence,
		})
	}

	// THE TWO PEOPLE A RUN NAMES, and they are two edges because they are two
	// facts. The provider's `actor` is who the run is filed under and its
	// `triggering_actor` is who started THIS one; on a first run they are the same
	// person and on a re-run they are not, which is the only thing in the response
	// that says a re-run happened at all. Both ride this response, so the pair
	// costs nothing beyond the read already made.
	users, err := userRelations(org, id, []namedUser{
		{run.Actor, ghgraph.EdgeAttributedTo},
		{run.TriggeringActor, ghgraph.EdgeInitiatedBy},
	})
	if err != nil {
		return ghgraph.Result{}, err
	}
	out.Add(users)

	// THE RUN'S HEAD COMMIT NAMES AN AUTHOR AND IS NOT READ. That author is a
	// name and an EMAIL ADDRESS, and a person's address is not inventory of an
	// organization's CI/CD; it is also a different person from either actor —
	// the author of the code, not the runner of the pipeline — so reading it
	// would put a fourth kind of relationship on the same class of edge.
	return out, nil
}
