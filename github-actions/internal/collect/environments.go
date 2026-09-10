// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// environments.go — each repository's deployment environments, their protection
// rules, and the reviewer nodes those rules resolve to.

// envDetail is the environment node's Content.
type envDetail struct {
	Name            string           `json:"name"`
	ProtectionRules []protectionRule `json:"protection_rules,omitempty"`
}

// protectionRule is one protection rule, counted rather than enumerated: the
// reviewers themselves are nodes, so listing them here as well would put the
// same fact in two places and let the two disagree.
type protectionRule struct {
	Type      string `json:"type"`
	WaitTimer int    `json:"wait_timer,omitempty"`
	Reviewers int    `json:"reviewers,omitempty"`
}

// requiredReviewersRule is the protection-rule type that names reviewers. The
// others — a wait timer, a branch policy — gate a deployment without naming
// anybody, so they are counted in the detail and produce no edge.
const requiredReviewersRule = "required_reviewers"

// Environments enumerates each repository's environments.
func Environments(api API) Subcollector {
	return Subcollector{
		Name: "github-environments",
		Run: func(ctx context.Context, org string) (ghgraph.Result, error) {
			repos, err := listRepositories(ctx, api.Repos, org)
			if err != nil {
				return ghgraph.Result{}, err
			}

			var out ghgraph.Result
			var failures reads
			for _, fullName := range repoNames(repos) {
				environments, err := listEnvironments(ctx, api.Repos, fullName)
				failures.record(fullName, err)
				for _, env := range environments {
					converted, err := environmentResource(org, fullName, env)
					if err != nil {
						return out, err
					}
					out.Add(converted)
				}
			}
			return out, failures.err("github-environments")
		},
	}
}

// listEnvironments pages one repository's environments to exhaustion.
//
// IT RETURNS WHAT IT READ ALONGSIDE ANY ERROR, so a repository whose second page
// failed still contributes its first.
func listEnvironments(
	ctx context.Context, api ReposAPI, fullName string,
) ([]*gogithub.Environment, error) {
	owner, repo := splitFullName(fullName)
	opts := &gogithub.EnvironmentListOptions{ListOptions: gogithub.ListOptions{PerPage: perPage}}

	var out []*gogithub.Environment
	for {
		page, resp, err := api.ListEnvironments(ctx, owner, repo, opts)
		if err != nil {
			return out, fmt.Errorf("listing environments: %w", err)
		}
		if page != nil {
			out = append(out, page.Environments...)
		}
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

// environmentResource converts one environment into its node, its edge to the
// repository, and the reviewer nodes and edges its protection rules name.
func environmentResource(
	org, fullName string, env *gogithub.Environment,
) (ghgraph.Result, error) {
	if env == nil {
		return ghgraph.Result{}, fmt.Errorf(
			"github-environments: the provider returned a nil environment for %q", fullName)
	}
	name := env.GetName()
	if name == "" {
		return ghgraph.Result{}, fmt.Errorf(
			"github-environments: an environment of %q carries no name, so it has no stable id", fullName)
	}
	content, err := marshalDetail("github-environments", name, envDetail{
		Name:            name,
		ProtectionRules: summarizeProtection(env.ProtectionRules),
	})
	if err != nil {
		return ghgraph.Result{}, err
	}

	id := ghgraph.EnvironmentID(org, fullName, name)
	out := ghgraph.Result{
		Resources: []ghgraph.Resource{{
			ID:           id,
			Name:         name,
			ResourceType: ghgraph.ResourceTypeEnvironment,
			Content:      content,
			Metadata:     map[string]string{"org": org, "repo": fullName},
		}},
		Relations: []ghgraph.Relation{{
			FromID: id,
			ToID:   ghgraph.RepositoryID(org, fullName),
			Type:   ghgraph.EdgeBelongsTo,
		}},
	}

	reviewers, err := environmentReviewers(org, id, env)
	if err != nil {
		return ghgraph.Result{}, err
	}
	out.Add(reviewers)
	return out, nil
}

// environmentReviewers mints a node per required reviewer and the edge to it.
//
// THE REVIEWER NODES ARE MATERIALIZED, which is this collector's departure from
// the source provider and the reason the whole REQUIRES_APPROVAL class is worth
// having. That provider emits the edge and creates no node, so every one of its
// approval edges names something that does not exist — a relationship nothing can
// traverse and no query returns. The enumeration already carries the reviewer's
// login or slug, so the node costs one more resource per reviewer and makes the
// edge resolve.
func environmentReviewers(
	org, envNodeID string, env *gogithub.Environment,
) (ghgraph.Result, error) {
	var out ghgraph.Result
	for _, rule := range env.ProtectionRules {
		if rule.GetType() != requiredReviewersRule {
			continue
		}
		for _, reviewer := range rule.Reviewers {
			resource, err := reviewerResource(org, reviewer)
			if err != nil {
				return ghgraph.Result{}, err
			}
			if resource.ID == "" {
				// The provider named a reviewer of a kind this collector does
				// not model, or named one with no identity at all. There is
				// nothing to point at, so no edge is emitted — an edge to an
				// empty id would be refused by the builder, and one to a guessed
				// id would be worse.
				continue
			}
			out.Resources = append(out.Resources, resource)
			out.Relations = append(out.Relations, ghgraph.Relation{
				FromID: envNodeID,
				ToID:   resource.ID,
				Type:   ghgraph.EdgeRequiresApproval,
			})
		}
	}
	return out, nil
}

// summarizeProtection renders the environment's protection rules for its detail.
func summarizeProtection(rules []*gogithub.ProtectionRule) []protectionRule {
	out := make([]protectionRule, 0, len(rules))
	for _, rule := range rules {
		info := protectionRule{Type: rule.GetType(), Reviewers: len(rule.Reviewers)}
		if rule.WaitTimer != nil {
			info.WaitTimer = rule.GetWaitTimer()
		}
		out = append(out, info)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
