// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"fmt"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// pipeline_runs.go — each project's most recent pipeline RUNS and the jobs
// inside them.
//
// A RUN IS NOT A PIPELINE DEFINITION. The definition is the project's
// `.gitlab-ci.yml`, read by the pipelines enumeration and emitted once per
// project; a run is one execution of it. The two carry different resource types
// and different ids, and the hyphen in `pipeline-run` is the source provider's
// spelling.
//
// THE RUN LIST IS CAPPED AND DOES NOT PAGE; the JOB list under each run pages to
// exhaustion. That asymmetry is the source provider's and it is deliberate: a
// project's run history is unbounded, while the jobs of ONE run are a bounded
// property of the pipeline that produced them, so truncating those would drop
// part of a thing rather than the older part of a list.

// pipelineRunDetail is the pipeline-run node's Content.
type pipelineRunDetail struct {
	Status string `json:"status,omitempty"`
	Source string `json:"source,omitempty"`
	Ref    string `json:"ref,omitempty"`
	SHA    string `json:"sha,omitempty"`
	WebURL string `json:"web_url,omitempty"`
}

// jobDetail is the job node's Content.
type jobDetail struct {
	Name   string   `json:"name,omitempty"`
	Stage  string   `json:"stage,omitempty"`
	Status string   `json:"status,omitempty"`
	Ref    string   `json:"ref,omitempty"`
	Tags   []string `json:"tags,omitempty"`
}

// PipelineRuns enumerates each project's recent pipeline runs and their jobs.
func PipelineRuns(api API, lister *projectLister, group string, maxRuns int) Subcollector {
	return Subcollector{
		Name: "gitlab-pipeline-runs",
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
				got, projectErr := projectRuns(ctx, api, group, project, maxRuns, &failures)
				out.Add(got)
				failures.record(project.PathWithNamespace, projectErr)
			}
			return out, failures.err("gitlab-pipeline-runs")
		},
	}
}

// projectRuns reads one page of one project's most recent runs and every job
// under each of them.
func projectRuns(
	ctx context.Context, api API, group string, project *gl.Project, maxRuns int, failures *reads,
) (glgraph.Result, error) {
	sort, orderBy := "desc", "id"
	opts := &gl.ListProjectPipelinesOptions{
		ListOptions: gl.ListOptions{PerPage: int64(maxRuns)},
		Sort:        &sort,
		OrderBy:     &orderBy,
	}
	path := project.PathWithNamespace

	runs, _, err := api.Pipelines.ListProjectPipelines(ctx, project.ID, opts)
	if err != nil {
		return glgraph.Result{}, fmt.Errorf("listing the pipeline runs of %q: %w", path, err)
	}

	var out glgraph.Result
	for _, run := range runs {
		if run == nil {
			continue
		}
		converted, convErr := pipelineRunResource(group, path, run)
		if convErr != nil {
			return out, convErr
		}
		out.Add(converted)

		// A RUN WHOSE JOBS CANNOT BE READ KEEPS ITS OWN NODE. The run was
		// enumerated and only its contents were unreadable, so dropping it would
		// lose a fact the provider did answer in order to punish one it did not.
		jobs, jobsErr := runJobs(ctx, api, group, path, project.ID, run.ID)
		out.Add(jobs)
		failures.record(fmt.Sprintf("the jobs of pipeline run %d of %q", run.ID, path), jobsErr)
	}
	return out, nil
}

// pipelineRunResource converts one run into its node and its edge to the project.
func pipelineRunResource(group, path string, run *gl.PipelineInfo) (glgraph.Result, error) {
	content, err := marshalDetail("gitlab-pipeline-runs",
		fmt.Sprintf("%s/%d", path, run.ID), pipelineRunDetail{
			Status: run.Status,
			Source: run.Source,
			Ref:    run.Ref,
			SHA:    run.SHA,
			WebURL: run.WebURL,
		})
	if err != nil {
		return glgraph.Result{}, err
	}

	id := glgraph.PipelineRunID(group, path, run.ID)
	return glgraph.Result{
		Resources: []glgraph.Resource{{
			ID:           id,
			Name:         fmt.Sprintf("pipeline #%d", run.ID),
			ResourceType: glgraph.ResourceTypePipelineRun,
			Content:      content,
			Metadata: map[string]string{
				"project": path,
				"ref":     run.Ref,
				"status":  run.Status,
				"source":  run.Source,
				"sha":     run.SHA,
			},
		}},
		Relations: []glgraph.Relation{{
			FromID: id,
			ToID:   glgraph.ProjectID(group, path),
			Type:   glgraph.EdgeBelongsTo,
		}},
	}, nil
}

// runJobs pages every job under one pipeline run to exhaustion.
func runJobs(
	ctx context.Context, api API, group, path string, projectID, runID int64,
) (glgraph.Result, error) {
	opts := &gl.ListJobsOptions{ListOptions: gl.ListOptions{PerPage: perPage}}

	var out glgraph.Result
	for {
		page, resp, err := api.Pipelines.ListPipelineJobs(ctx, projectID, runID, opts)
		if err != nil {
			return out, fmt.Errorf("listing the jobs of pipeline run %d: %w", runID, err)
		}
		for _, job := range page {
			if job == nil {
				continue
			}
			converted, convErr := jobResource(group, path, runID, job)
			if convErr != nil {
				return out, convErr
			}
			out.Add(converted)
		}
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

// jobResource converts one job into its node, its edge to the run it belongs to
// and, where the provider named one, its edge to the runner that executed it.
//
// A JOB WITH NO RUNNER EMITS NO RUNS_IN EDGE. A queued or skipped job has not
// been picked up by anything, and the provider says so by leaving the runner id
// zero; an edge to runner 0 would name a node nothing mints.
func jobResource(group, path string, runID int64, job *gl.Job) (glgraph.Result, error) {
	content, err := marshalDetail("gitlab-pipeline-runs",
		fmt.Sprintf("%s/%d", path, job.ID), jobDetail{
			Name:   job.Name,
			Stage:  job.Stage,
			Status: job.Status,
			Ref:    job.Ref,
			Tags:   job.TagList,
		})
	if err != nil {
		return glgraph.Result{}, err
	}

	meta := map[string]string{
		"project": path,
		"stage":   job.Stage,
		"status":  job.Status,
		"name":    job.Name,
	}
	if job.Runner.ID != 0 {
		meta["runner_id"] = fmt.Sprintf("%d", job.Runner.ID)
	}

	id := glgraph.JobID(group, path, job.ID)
	out := glgraph.Result{
		Resources: []glgraph.Resource{{
			ID:           id,
			Name:         job.Name,
			ResourceType: glgraph.ResourceTypeJob,
			Content:      content,
			Metadata:     meta,
		}},
		Relations: []glgraph.Relation{{
			FromID: id,
			ToID:   glgraph.PipelineRunID(group, path, runID),
			Type:   glgraph.EdgeBelongsTo,
		}},
	}
	if job.Runner.ID != 0 {
		out.Relations = append(out.Relations, glgraph.Relation{
			FromID: id,
			ToID:   glgraph.RunnerID(group, job.Runner.ID),
			Type:   glgraph.EdgeRunsIn,
		})
	}
	return out, nil
}
