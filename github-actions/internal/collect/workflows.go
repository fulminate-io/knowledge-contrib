// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// workflows.go — the active workflows of every repository, and the secret and
// environment references parsed out of each one's definition.
//
// THIS IS THE MOST EXPENSIVE ENUMERATION AND THE ONLY ONE THAT READS FILE
// CONTENT. Its cost is one listing per repository plus one file read per active
// workflow, so it is O(workflows) where the others are O(repositories).

// workflowDetail is the workflow node's Content.
type workflowDetail struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	State   string `json:"state"`
	HTMLURL string `json:"html_url,omitempty"`
}

// workflowStateActive is the only state this collector inventories. A disabled
// workflow runs nothing, and the source provider skips it on the same grounds.
const workflowStateActive = "active"

// Workflows enumerates each repository's active workflows.
func Workflows(api API) Subcollector {
	return Subcollector{
		Name: "github-workflows",
		Run: func(ctx context.Context, org string) (ghgraph.Result, error) {
			repos, err := listRepositories(ctx, api.Repos, org)
			if err != nil {
				return ghgraph.Result{}, err
			}

			var out ghgraph.Result
			var failures reads
			for _, fullName := range repoNames(repos) {
				got, err := workflowsForRepo(ctx, api, org, fullName, &failures)
				out.Add(got)
				failures.record(fullName, err)
			}
			return out, failures.err("github-workflows")
		},
	}
}

// workflowsForRepo pages one repository's workflows to exhaustion.
//
// IT RETURNS WHAT IT READ ALONGSIDE ANY ERROR, so a repository whose second page
// failed still contributes its first. The caller records that failure against
// this repository's name, and the walk reports itself incomplete naming it.
func workflowsForRepo(
	ctx context.Context, api API, org, fullName string, failures *reads,
) (ghgraph.Result, error) {
	owner, repo := splitFullName(fullName)
	opts := &gogithub.ListOptions{PerPage: perPage}

	var out ghgraph.Result
	for {
		page, resp, err := api.Actions.ListWorkflows(ctx, owner, repo, opts)
		if err != nil {
			return out, fmt.Errorf("listing workflows: %w", err)
		}
		if page != nil {
			got, err := convertWorkflows(ctx, api, org, fullName, page.Workflows, failures)
			out.Add(got)
			if err != nil {
				return out, err
			}
		}
		// The provider decides when the paging ends, not the item count: a full
		// page is not the last page, and NextPage == 0 is the provider saying so.
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

// convertWorkflows converts one page of workflows and reads each one's
// definition.
//
// A DEFINITION THIS COLLECTOR COULD NOT READ IS A PARTIAL READ, RECORDED AND
// SURVIVED, and that is a decision rather than a fallback. The workflow itself
// was enumerated and its node is emitted; what is missing is the set of edges its
// TEXT declares — the secrets it uses and the environments it deploys to — which
// nothing else in the walk produces. Failing the whole repository over one
// unreadable file would drop workflows this collector did read, and saying
// nothing would let a walk missing a workflow's whole edge contribution assert
// that it saw the organization. So the node lands, the failure is recorded
// against the workflow's own path, and the walk is incomplete naming it.
func convertWorkflows(
	ctx context.Context, api API, org, fullName string,
	workflows []*gogithub.Workflow, failures *reads,
) (ghgraph.Result, error) {
	var out ghgraph.Result
	for _, wf := range workflows {
		if wf == nil || wf.GetState() != workflowStateActive {
			continue
		}
		converted, err := workflowResource(org, fullName, wf)
		if err != nil {
			return out, err
		}
		out.Add(converted)

		refs, err := workflowReferences(ctx, api, org, fullName, wf.GetPath())
		out.Add(refs)
		failures.record(fullName+" "+wf.GetPath(), err)
	}
	return out, nil
}

// workflowResource converts one workflow into its node and its edge to the
// repository.
func workflowResource(org, fullName string, wf *gogithub.Workflow) (ghgraph.Result, error) {
	path := wf.GetPath()
	if path == "" {
		return ghgraph.Result{}, fmt.Errorf(
			"github-workflows: a workflow of %q carries no path, so it has no stable id", fullName)
	}
	content, err := marshalDetail("github-workflows", path, workflowDetail{
		Name:    wf.GetName(),
		Path:    path,
		State:   wf.GetState(),
		HTMLURL: wf.GetHTMLURL(),
	})
	if err != nil {
		return ghgraph.Result{}, err
	}

	id := ghgraph.WorkflowID(org, fullName, path)
	return ghgraph.Result{
		Resources: []ghgraph.Resource{{
			ID:           id,
			Name:         wf.GetName(),
			ResourceType: ghgraph.ResourceTypeWorkflow,
			Content:      content,
			Metadata:     map[string]string{"org": org, "repo": fullName, "path": path},
		}},
		Relations: []ghgraph.Relation{{
			FromID: id,
			ToID:   ghgraph.RepositoryID(org, fullName),
			Type:   ghgraph.EdgeBelongsTo,
		}},
	}, nil
}

// workflowReferences reads a workflow's definition and emits the edges its text
// declares: the secrets it names and the environments it deploys to.
//
// THE SECRET TARGET'S SCOPE IS HARDCODED TO `repo`, AND THAT IS CARRIED FORWARD
// RATHER THAN FIXED. A workflow's text names a secret by NAME alone — the
// definition says nothing about whether the value comes from the repository, the
// organization or the environment — so the target id this builds resolves only
// for a repository-scoped secret. A workflow referencing an organization-scoped
// secret therefore names a node that does not exist, even though a node for that
// secret DOES exist in the same result under a different id. Guessing the scope
// would be this collector inventing a fact the provider did not state; the
// source provider makes the same trade and a consumer already reads this shape,
// so the dangle is reproduced and asserted rather than papered over.
func workflowReferences(
	ctx context.Context, api API, org, fullName, path string,
) (ghgraph.Result, error) {
	owner, repo := splitFullName(fullName)
	file, _, _, err := api.Repos.GetContents(ctx, owner, repo, path, nil)
	if err != nil {
		return ghgraph.Result{}, fmt.Errorf("reading the definition of %q: %w", path, err)
	}
	if file == nil {
		// The provider answered with no file and no error, which is what it does
		// for a path that names a directory. There is nothing to parse and
		// nothing went wrong.
		return ghgraph.Result{}, nil
	}
	definition, err := file.GetContent()
	if err != nil {
		return ghgraph.Result{}, fmt.Errorf("decoding the definition of %q: %w", path, err)
	}

	workflowNodeID := ghgraph.WorkflowID(org, fullName, path)
	var out ghgraph.Result
	for _, secret := range ParseSecretRefs(definition) {
		out.Relations = append(out.Relations, ghgraph.Relation{
			FromID: workflowNodeID,
			ToID:   ghgraph.SecretID(org, fullName, secretScopeRepo, secret),
			Type:   ghgraph.EdgeUsesSecret,
		})
	}
	for _, env := range ParseEnvironmentRefs(definition) {
		out.Relations = append(out.Relations, ghgraph.Relation{
			FromID: workflowNodeID,
			ToID:   ghgraph.EnvironmentID(org, fullName, env),
			Type:   ghgraph.EdgeDeploysTo,
		})
	}
	return out, nil
}
