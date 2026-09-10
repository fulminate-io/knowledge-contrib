// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbclient"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
)

// variables.go — the pipeline variables declared at the workspace, at each
// repository and at each deployment environment.
//
// NAMES AND METADATA ONLY, NEVER A VALUE, AND THE OMISSION IS STRUCTURAL RATHER
// THAN A FILTER. [apiVariable] declares no field for a value, so a value in the
// provider's response is dropped at DECODE and never exists as a Go string this
// module could accidentally carry — a redaction pass applied later would be one
// forgotten call away from a credential in a searchable, replicated, embedded
// graph. [variableDetail] is the whole of what a variable node stores. A test
// asserts over the RAW BYTES of the whole emitted result that a planted value
// appears nowhere in it.
//
// THE SCOPE WORD AND THE SCOPE KEY ARE DIFFERENT STRINGS. The stored `scope`
// metadata is `workspace`, `repository` or `deployment`; the id's scope segments
// are `workspace`, `repository/<slug>` and `env/<slug>/<environment>`. An
// implementer who reads the id shape and passes `env` as the scope word gets an
// identical id and a silently changed stored value, which is why the three ids
// are three functions and the three words are constants here.
const (
	scopeWorkspace  = "workspace"
	scopeRepository = "repository"
	scopeDeployment = "deployment"
)

// Variables enumerates the variables at all three scopes.
func Variables(client *bbclient.Client, repos []RepoInfo) Subcollector {
	return Subcollector{
		Name: "bitbucket-variables",
		Run: func(ctx context.Context, workspace string) (bbgraph.Result, error) {
			var out bbgraph.Result
			var failures reads

			workspaceVars, err := listVariables(ctx, client,
				fmt.Sprintf("workspaces/%s/pipelines-config/variables", workspace))
			out.Add(buildWorkspaceVariables(workspace, workspaceVars, &failures))
			failures.record("the workspace's variables", err)

			for _, repo := range repos {
				if err := ctx.Err(); err != nil {
					return out, err
				}
				got := repoVariables(ctx, client, workspace, repo.Slug, &failures)
				out.Add(got)
			}
			return out, failures.err("bitbucket-variables")
		},
	}
}

// repoVariables reads one repository's own variables and every one of its
// deployment environments'.
func repoVariables(
	ctx context.Context, client *bbclient.Client, workspace, slug string, failures *reads,
) bbgraph.Result {
	var out bbgraph.Result

	// THE REPOSITORY SCOPE IS `pipelines_config` WITH AN UNDERSCORE, AND ITS
	// WORKSPACE-SCOPE SIBLING TWENTY LINES ABOVE IS `pipelines-config` WITH A
	// HYPHEN. That is Bitbucket's own inconsistency and not a typo class to
	// normalize: the runners enumeration reads the SAME repository scope at
	// `pipelines-config/runners` and is right to. Measured live with one
	// credential in one run, on a repository that has both:
	//
	//	GET repositories/<ws>/<repo>/pipelines_config/variables -> 200
	//	GET repositories/<ws>/<repo>/pipelines-config/variables -> 404
	//	GET repositories/<ws>/<repo>/pipelines-config/runners   -> 200
	//	GET repositories/<ws>/<repo>/pipelines_config/runners   -> 404
	//
	// THE HYPHEN SHIPPED HERE AND COST EVERY REPOSITORY-SCOPE VARIABLE IN EVERY
	// WORKSPACE, silently, because a 404 on a listing used to be read as a scope
	// with nothing in it. Both halves of that are fixed: the spelling here, and
	// the reading in [listVariables].
	repoVars, err := listVariables(ctx, client,
		fmt.Sprintf("repositories/%s/%s/pipelines_config/variables", workspace, slug))
	out.Add(buildRepositoryVariables(workspace, slug, repoVars, failures))
	failures.record(slug, err)

	// THE ENVIRONMENT LIST IS RE-READ HERE, and its failure is REPORTED. The
	// source provider discards this one with no log at any level — the worst of
	// its nine dropped reads — so a repository whose environments it could not
	// list produced no deployment variables at all while the walk still asserted
	// it had seen everything.
	envs, err := ListEnvironments(ctx, client, workspace, slug)
	failures.record(slug+" environments, for its deployment variables", err)

	for _, env := range envs {
		if ctx.Err() != nil {
			return out
		}
		envVars, err := listVariables(ctx, client, fmt.Sprintf(
			"repositories/%s/%s/deployments_config/environments/%s/variables",
			workspace, slug, env.UUID))
		out.Add(buildDeploymentVariables(workspace, slug, env.Name, envVars, failures))
		failures.record(slug+" environment "+env.Name, err)
	}
	return out
}

// listVariables pages one variables endpoint to exhaustion.
func listVariables(
	ctx context.Context, client *bbclient.Client, path string,
) ([]apiVariable, error) {
	var vars []apiVariable
	err := client.GetPaginated(ctx, path, func(raw json.RawMessage) error {
		var page []apiVariable
		if err := json.Unmarshal(raw, &page); err != nil {
			return fmt.Errorf("decoding a variables page: %w", err)
		}
		vars = append(vars, page...)
		return nil
	})
	switch {
	case err == nil:
		return vars, nil
	case IsNotFound(err):
		// NOT "NO VARIABLES ARE CONFIGURED AT THIS SCOPE", WHICH IS WHAT THIS ARM
		// USED TO SAY. A scope with no variables answers 200 with an empty page;
		// this URL was not there.
		return vars, notFoundOnAListing("variables", path, err)
	default:
		return vars, fmt.Errorf("reading variables: %w", err)
	}
}

// buildWorkspaceVariables converts the workspace-scoped variables.
func buildWorkspaceVariables(
	workspace string, vars []apiVariable, failures *reads,
) bbgraph.Result {
	return buildVariables(workspace, vars, scopeWorkspace, bbgraph.WorkspaceID(workspace), nil,
		func(key string) string { return bbgraph.WorkspaceVariableID(workspace, key) }, failures)
}

// buildRepositoryVariables converts one repository's own variables.
func buildRepositoryVariables(
	workspace, slug string, vars []apiVariable, failures *reads,
) bbgraph.Result {
	return buildVariables(workspace, vars, scopeRepository, bbgraph.RepositoryID(workspace, slug),
		map[string]string{"repo": slug},
		func(key string) string { return bbgraph.RepositoryVariableID(workspace, slug, key) },
		failures)
}

// buildDeploymentVariables converts one deployment environment's variables.
func buildDeploymentVariables(
	workspace, slug, envName string, vars []apiVariable, failures *reads,
) bbgraph.Result {
	return buildVariables(workspace, vars, scopeDeployment,
		bbgraph.EnvironmentID(workspace, slug, envName),
		map[string]string{"repo": slug, "environment": envName},
		func(key string) string {
			return bbgraph.DeploymentVariableID(workspace, slug, envName, key)
		}, failures)
}

// buildVariables is the shared converter for all three scopes: one node per
// variable and one BELONGS_TO edge into whatever declares it.
func buildVariables(
	workspace string,
	vars []apiVariable,
	scope, parentID string,
	extra map[string]string,
	id func(key string) string,
	failures *reads,
) bbgraph.Result {
	var out bbgraph.Result
	for _, variable := range vars {
		variableID := id(variable.Key)
		metadata := map[string]string{
			"workspace": workspace,
			"key":       variable.Key,
			"scope":     scope,
			"secured":   boolText(variable.Secured),
		}
		maps.Copy(metadata, extra)

		content, ok := renderContent(failures, scope+" variable "+variable.Key, variableDetail{
			Key:     variable.Key,
			Scope:   scope,
			Secured: variable.Secured,
		})
		if !ok {
			continue
		}
		out.Resources = append(out.Resources, bbgraph.Resource{
			ID:           variableID,
			Name:         variable.Key,
			ResourceType: bbgraph.ResourceTypeVariable,
			Content:      content,
			Metadata:     metadata,
		})
		out.Relations = append(out.Relations, bbgraph.Relation{
			FromID: variableID,
			ToID:   parentID,
			Type:   bbgraph.EdgeBelongsTo,
		})
	}
	return out
}

// variableDetail is the sanitized content a variable node stores. It has NO
// value field and it never will: see this file's package note.
type variableDetail struct {
	Key     string `json:"key"`
	Scope   string `json:"scope"`
	Secured bool   `json:"secured"`
}

// apiVariable is the variable shape this collector reads.
//
// THE VALUE FIELD IS ABSENT DELIBERATELY. The provider returns one for an
// unsecured variable; declaring no field for it means the decoder drops it, so
// there is no point in this module at which a variable's value exists.
type apiVariable struct {
	UUID    string `json:"uuid"`
	Key     string `json:"key"`
	Secured bool   `json:"secured"`
	System  bool   `json:"system"`
}
