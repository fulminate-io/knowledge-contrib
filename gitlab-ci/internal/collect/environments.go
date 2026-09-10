// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"fmt"
	"strconv"

	gl "gitlab.com/gitlab-org/api/client-go"

	"github.com/fulminate-io/knowledge-contrib/gitlab-ci/internal/glgraph"
)

// environments.go — each project's environments, and the protection rules that
// gate deployments into them.
//
// THE TWO READS ARE SEPARATE CALLS AND EITHER CAN FAIL ALONE. The environments
// list is what mints the environment node; the protected-environments list is
// what mints the protection rule and its approval edge. The source provider
// reports a failure of the second at DEBUG and returns, so every approval edge
// for that project disappears at default verbosity with the walk still asserting
// it saw the whole group. Here both reach the completeness verdict.

// Environments enumerates every project's environments and protection rules.
func Environments(api API, lister *projectLister, group string) Subcollector {
	return Subcollector{
		Name: "gitlab-environments",
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
				path := project.PathWithNamespace

				got, listErr := projectEnvironments(ctx, api, group, project)
				out.Add(got)
				failures.record(path, listErr)

				rules, rulesErr := protectionRules(ctx, api, group, project)
				out.Add(rules)
				failures.record(fmt.Sprintf("the protected environments of %q", path), rulesErr)
			}
			return out, failures.err("gitlab-environments")
		},
	}
}

// projectEnvironments pages one project's environments to exhaustion.
func projectEnvironments(
	ctx context.Context, api API, group string, project *gl.Project,
) (glgraph.Result, error) {
	opts := &gl.ListEnvironmentsOptions{ListOptions: gl.ListOptions{PerPage: perPage}}
	path := project.PathWithNamespace
	projectID := glgraph.ProjectID(group, path)

	var out glgraph.Result
	for {
		page, resp, err := api.Environments.ListEnvironments(ctx, project.ID, opts)
		if err != nil {
			return out, fmt.Errorf("listing the environments of %q: %w", path, err)
		}
		for _, env := range page {
			if env == nil || env.Name == "" {
				continue
			}
			out.Add(environmentResource(group, path, projectID, env))
		}
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

// environmentResource converts one environment into its node and its edge to the
// project.
func environmentResource(
	group, path, projectID string, env *gl.Environment,
) glgraph.Result {
	meta := map[string]string{
		"project": path,
		"state":   env.State,
	}
	if env.Tier != "" {
		meta["tier"] = env.Tier
	}
	if env.ExternalURL != "" {
		meta["external_url"] = env.ExternalURL
	}

	id := glgraph.EnvironmentID(group, path, env.Name)
	return glgraph.Result{
		Resources: []glgraph.Resource{{
			ID:           id,
			Name:         env.Name,
			ResourceType: glgraph.ResourceTypeEnvironment,
			Metadata:     meta,
		}},
		Relations: []glgraph.Relation{{
			FromID: id,
			ToID:   projectID,
			Type:   glgraph.EdgeBelongsTo,
		}},
	}
}

// protectionRules reads one project's protected environments and emits a rule
// node per environment that requires an approval.
//
// AN ENVIRONMENT WITH NO REQUIRED APPROVALS PRODUCES NOTHING. A protected
// environment may exist only to restrict who may deploy, which is a different
// statement from requiring a second person's approval, and a rule node for it
// would assert an approval gate the provider does not describe.
//
// THE APPROVAL EDGE RUNS FROM THE ENVIRONMENT NODE, and its SOURCE is the one end
// in this whole graph that can dangle: the rule is read from the protected-
// environments call and the environment node from the environments call, so a
// protected environment the second call did not return names an environment node
// that is not there. That is data-conditional rather than structural and it is
// carried forward from the source provider rather than papered over.
func protectionRules(
	ctx context.Context, api API, group string, project *gl.Project,
) (glgraph.Result, error) {
	opts := &gl.ListProtectedEnvironmentsOptions{ListOptions: gl.ListOptions{PerPage: perPage}}
	path := project.PathWithNamespace

	protected, _, err := api.Environments.ListProtectedEnvironments(ctx, project.ID, opts)
	if err != nil {
		return glgraph.Result{}, fmt.Errorf("listing the protected environments of %q: %w", path, err)
	}

	var out glgraph.Result
	for _, rule := range protected {
		if rule == nil || rule.Name == "" || rule.RequiredApprovalCount <= 0 {
			continue
		}
		ruleID := glgraph.ProtectionRuleID(group, path, rule.Name)
		out.Resources = append(out.Resources, glgraph.Resource{
			ID:           ruleID,
			Name:         fmt.Sprintf("%s protection", rule.Name),
			ResourceType: glgraph.ResourceTypeProtectionRule,
			Metadata: map[string]string{
				"environment":             rule.Name,
				"project":                 path,
				"required_approval_count": strconv.FormatInt(rule.RequiredApprovalCount, 10),
			},
		})
		out.Relations = append(out.Relations, glgraph.Relation{
			FromID: glgraph.EnvironmentID(group, path, rule.Name),
			ToID:   ruleID,
			Type:   glgraph.EdgeRequiresApproval,
		})
	}
	return out, nil
}
