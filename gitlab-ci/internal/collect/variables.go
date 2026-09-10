// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"fmt"
	"strconv"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// variables.go — the CI/CD variables declared at the group and at each project.
//
// A VARIABLE'S VALUE IS NEVER READ AND NEVER CARRIED, and this enumeration is
// the one place in the collector where that is a security property rather than a
// preference: a GitLab CI/CD variable IS the secret store, so its value is a
// live credential of the operator's own. What reaches the graph is the KEY, the
// scope, and the protected and masked flags — a variable node carries no
// Content at all, and there is no field on it a value could travel in.
//
// THE PROVIDER'S LIST CALL IS WHAT MAKES THAT TRUE AT THE SEAM rather than only
// in this file: the interface this package enumerates through carries the list
// and not the per-variable get, so no call available here returns a value.

// Variables enumerates the group's variables and every project's.
func Variables(api API, lister *projectLister, group string) Subcollector {
	return Subcollector{
		Name: "gitlab-variables",
		Run: func(ctx context.Context) (glgraph.Result, error) {
			var out glgraph.Result
			var failures reads

			got, err := groupVariables(ctx, api, group)
			out.Add(got)
			failures.record("the group's variables", err)

			projects, err := lister.list(ctx)
			if err != nil {
				return out, err
			}
			for _, project := range projects {
				if project == nil {
					continue
				}
				got, projectErr := projectVariables(ctx, api, group, project)
				out.Add(got)
				failures.record(project.PathWithNamespace, projectErr)
			}
			return out, failures.err("gitlab-variables")
		},
	}
}

// groupVariables pages the group-scoped variables to exhaustion.
func groupVariables(ctx context.Context, api API, group string) (glgraph.Result, error) {
	opts := &gl.ListGroupVariablesOptions{ListOptions: gl.ListOptions{PerPage: perPage}}

	var out glgraph.Result
	for {
		page, resp, err := api.Variables.ListGroupVariables(ctx, group, opts)
		if err != nil {
			return out, fmt.Errorf("listing the group's variables: %w", err)
		}
		for _, variable := range page {
			if variable == nil || variable.Key == "" {
				continue
			}
			out.Add(variableResource(glgraph.VariableID(group, group, variable.Key),
				variable.Key, glgraph.GroupID(group), map[string]string{
					"scope": "group",
					"group": group,
				}))
		}
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

// projectVariables pages one project's variables to exhaustion.
func projectVariables(
	ctx context.Context, api API, group string, project *gl.Project,
) (glgraph.Result, error) {
	opts := &gl.ListProjectVariablesOptions{ListOptions: gl.ListOptions{PerPage: perPage}}
	path := project.PathWithNamespace

	var out glgraph.Result
	for {
		page, resp, err := api.Variables.ListProjectVariables(ctx, project.ID, opts)
		if err != nil {
			return out, fmt.Errorf("listing the variables of %q: %w", path, err)
		}
		for _, variable := range page {
			if variable == nil || variable.Key == "" {
				continue
			}
			out.Add(variableResource(glgraph.VariableID(group, path, variable.Key),
				variable.Key, glgraph.ProjectID(group, path), map[string]string{
					"scope":     "project",
					"project":   path,
					"protected": strconv.FormatBool(variable.Protected),
					"masked":    strconv.FormatBool(variable.Masked),
				}))
		}
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

// variableResource is the node and the ownership edge both scopes share.
func variableResource(id, key, parentID string, meta map[string]string) glgraph.Result {
	return glgraph.Result{
		Resources: []glgraph.Resource{{
			ID:           id,
			Name:         key,
			ResourceType: glgraph.ResourceTypeVariable,
			Metadata:     meta,
			// NO Content. The only thing left to put in it would be the value.
		}},
		Relations: []glgraph.Relation{{
			FromID: id,
			ToID:   parentID,
			Type:   glgraph.EdgeBelongsTo,
		}},
	}
}
