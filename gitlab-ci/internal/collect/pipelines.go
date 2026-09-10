// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// pipelines.go — each project's pipeline DEFINITION, read from the
// `.gitlab-ci.yml` on its default branch, and the three edge classes only that
// document declares.
//
// THIS IS THE ONLY ENUMERATION THAT READS FILE CONTENT, which makes it the most
// expensive one per project and the only one whose failures are about a document
// rather than a permission.
//
// A PROJECT WITH NO DEFAULT BRANCH IS SKIPPED AND THAT IS NOT AN INCOMPLETENESS.
// An empty repository has no ref to read a definition from, so there is nothing
// this enumeration failed to see. The same goes for a project whose default
// branch carries no `.gitlab-ci.yml`: the provider answered, and the answer is
// that the file is not there.
//
// A DEFINITION THAT DOES NOT PARSE STILL EMITS ITS PIPELINE NODE, and this is a
// decision rather than an inheritance. The source provider logs and returns, so
// the node disappears too and a project with a broken definition looks like a
// project with none. Here the node is emitted with the document as its content —
// it was read, and it exists — while `job_count` and `has_stages` are OMITTED
// rather than written as zero and false, because this collector did not learn
// them. The walk is incomplete and names the project.

// pipelineFile is the path every definition is read from. GitLab allows a project
// to configure another path; this collector does not read that setting, which is
// the source provider's own limitation and is stated rather than hidden.
const pipelineFile = ".gitlab-ci.yml"

// Pipelines enumerates each project's pipeline definition.
func Pipelines(api API, lister *projectLister, group string) Subcollector {
	return Subcollector{
		Name: "gitlab-pipelines",
		Run: func(ctx context.Context) (glgraph.Result, error) {
			projects, err := lister.list(ctx)
			if err != nil {
				return glgraph.Result{}, err
			}

			var out glgraph.Result
			var failures reads
			for _, project := range projects {
				if project == nil || project.DefaultBranch == "" {
					continue
				}
				got, projectErr := projectPipeline(ctx, api, group, project)
				out.Add(got)
				failures.record(project.PathWithNamespace, projectErr)
			}
			return out, failures.err("gitlab-pipelines")
		},
	}
}

// projectPipeline reads and converts one project's definition.
func projectPipeline(
	ctx context.Context, api API, group string, project *gl.Project,
) (glgraph.Result, error) {
	path := project.PathWithNamespace
	raw, err := fetchDefinition(ctx, api, project)
	if err != nil {
		return glgraph.Result{}, err
	}
	if raw == nil {
		return glgraph.Result{}, nil
	}

	id := glgraph.PipelineID(group, path, project.DefaultBranch)
	meta := map[string]string{"project": path, "ref": project.DefaultBranch}

	out := glgraph.Result{
		Resources: []glgraph.Resource{{
			ID:           id,
			Name:         fmt.Sprintf("%s pipeline", path),
			ResourceType: glgraph.ResourceTypePipeline,
			Content:      string(raw),
			Metadata:     meta,
		}},
		Relations: []glgraph.Relation{{
			FromID: id,
			ToID:   glgraph.ProjectID(group, path),
			Type:   glgraph.EdgeBelongsTo,
		}},
	}

	config, err := parseGitLabCI(raw)
	if err != nil {
		return out, fmt.Errorf("the %s of %q did not parse: %w", pipelineFile, path, err)
	}
	meta["job_count"] = strconv.Itoa(len(config.Jobs))
	meta["has_stages"] = strconv.FormatBool(len(config.Stages) > 0)
	if config.Includes > 0 {
		meta["has_includes"] = "true"
	}
	out.Relations = append(out.Relations, jobRelations(group, path, id, config)...)
	return out, nil
}

// fetchDefinition reads the definition off the project's default branch. It
// returns a nil document, and no error, when the file is not there.
func fetchDefinition(ctx context.Context, api API, project *gl.Project) ([]byte, error) {
	ref := project.DefaultBranch
	file, _, err := api.Files.GetFile(ctx, project.ID, pipelineFile, &gl.GetFileOptions{Ref: &ref})
	if err != nil {
		if statusOf(err) == http.StatusNotFound {
			// NOT AN INCOMPLETENESS. The provider answered, and the answer is that
			// this project defines no pipeline.
			return nil, nil
		}
		return nil, fmt.Errorf("reading the %s of %q: %w", pipelineFile, project.PathWithNamespace, err)
	}
	if file == nil {
		return nil, nil
	}
	content, err := base64.StdEncoding.DecodeString(file.Content)
	if err != nil {
		return nil, fmt.Errorf("the %s of %q is not the base64 the provider declared: %w",
			pipelineFile, project.PathWithNamespace, err)
	}
	return content, nil
}

// jobRelations is the three edge classes a definition declares.
//
// EVERY ONE OF THEM RUNS FROM THE PIPELINE NODE rather than from a job node, and
// that is the source provider's shape: a job in a DEFINITION is a template with
// no id of its own, and the `job` nodes in this graph are executions read from a
// different API. The two are not the same thing and an edge joining them would
// claim they were.
func jobRelations(group, path, pipelineID string, config *pipelineConfig) []glgraph.Relation {
	var out []glgraph.Relation
	for _, job := range config.Jobs {
		for _, tag := range job.Tags {
			if tag == "" {
				continue
			}
			// A TAG NO DISCOVERED RUNNER CARRIES NAMES NO NODE. The tag node is
			// minted from a runner's own tag list, so a job asking for `deploy`
			// when no runner offers it produces an edge that resolves to nothing —
			// which is the truthful record of a job that cannot be scheduled.
			out = append(out, glgraph.Relation{
				FromID: pipelineID,
				ToID:   glgraph.RunnerTagID(group, tag),
				Type:   glgraph.EdgeRunsIn,
			})
		}
		if job.Environment != "" {
			out = append(out, glgraph.Relation{
				FromID: pipelineID,
				ToID:   glgraph.EnvironmentID(group, path, job.Environment),
				Type:   glgraph.EdgeDeploysTo,
			})
		}
		for _, name := range job.VarRefs {
			// THE SCOPE IS ALWAYS THE PROJECT'S, and it cannot be anything else: a
			// script says `$DEPLOY_KEY` and nothing in the document says whether
			// that name was declared on the project or on the group. A reference to
			// a GROUP-scoped variable therefore names a node that is not there,
			// while a node for that variable IS in the same result under the
			// group's own owner segment.
			out = append(out, glgraph.Relation{
				FromID: pipelineID,
				ToID:   glgraph.VariableID(group, path, name),
				Type:   glgraph.EdgeUsesSecret,
			})
		}
	}
	return out
}
