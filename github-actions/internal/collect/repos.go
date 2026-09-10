// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"encoding/json"
	"fmt"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// repos.go — the organization and its repositories, and the enumeration every
// other one depends on for the list of repositories to walk.

// repoDetail is the repository node's Content: the provider's own answer,
// narrowed to the fields that describe the repository rather than count it.
type repoDetail struct {
	DefaultBranch string   `json:"default_branch"`
	Visibility    string   `json:"visibility"`
	Language      string   `json:"language,omitempty"`
	Topics        []string `json:"topics,omitempty"`
}

// Repositories enumerates the organization and its non-archived repositories.
func Repositories(api API) Subcollector {
	return Subcollector{
		Name: "github-repos",
		Run: func(ctx context.Context, org string) (ghgraph.Result, error) {
			repos, err := listRepositories(ctx, api.Repos, org)
			if err != nil {
				return ghgraph.Result{}, err
			}

			out := ghgraph.Result{Resources: []ghgraph.Resource{OrganizationResource(org)}}
			for _, repo := range repos {
				converted, err := repositoryResource(org, repo)
				if err != nil {
					return ghgraph.Result{}, err
				}
				out.Add(converted)
			}
			return out, nil
		},
	}
}

// OrganizationResource is the organization node, which every walk emits
// unconditionally.
//
// IT IS THE ONE NODE WITH NO Content. The provider is never asked about the
// organization itself — the id is the collect's own input — so there is no
// answer to carry, and a fabricated detail block would be this collector
// asserting something it did not read.
func OrganizationResource(org string) ghgraph.Resource {
	return ghgraph.Resource{
		ID:           ghgraph.OrganizationID(org),
		Name:         org,
		ResourceType: ghgraph.ResourceTypeOrganization,
		Metadata:     map[string]string{"org": org},
	}
}

// repositoryResource converts one repository into its node and its edge to the
// organization.
func repositoryResource(org string, repo *gogithub.Repository) (ghgraph.Result, error) {
	if repo == nil {
		return ghgraph.Result{}, fmt.Errorf("github-repos: the provider returned a nil repository")
	}
	fullName := repo.GetFullName()
	if fullName == "" {
		return ghgraph.Result{}, fmt.Errorf(
			"github-repos: a repository of %q carries no full name, so it has no stable id", org)
	}
	content, err := marshalDetail("github-repos", fullName, repoDetail{
		DefaultBranch: repo.GetDefaultBranch(),
		Visibility:    repo.GetVisibility(),
		Language:      repo.GetLanguage(),
		Topics:        repo.Topics,
	})
	if err != nil {
		return ghgraph.Result{}, err
	}

	id := ghgraph.RepositoryID(org, fullName)
	return ghgraph.Result{
		Resources: []ghgraph.Resource{{
			ID:           id,
			Name:         fullName,
			ResourceType: ghgraph.ResourceTypeRepository,
			Content:      content,
			Metadata: map[string]string{
				"org":            org,
				"repo_name":      repo.GetName(),
				"visibility":     repo.GetVisibility(),
				"default_branch": repo.GetDefaultBranch(),
			},
		}},
		Relations: []ghgraph.Relation{{
			FromID: id,
			ToID:   ghgraph.OrganizationID(org),
			Type:   ghgraph.EdgeBelongsTo,
		}},
	}, nil
}

// marshalDetail renders a node's Content.
//
// IT RETURNS THE ERROR RATHER THAN DISCARDING IT. Marshaling a struct of strings
// cannot fail today, which is exactly why the source provider discarded the
// error — and a node whose Content silently became empty would be
// indistinguishable from a resource the provider described with nothing. The
// caller refuses instead.
func marshalDetail(enumeration, subject string, detail any) (string, error) {
	raw, err := json.Marshal(detail)
	if err != nil {
		return "", fmt.Errorf("%s: rendering the detail of %q: %w", enumeration, subject, err)
	}
	return string(raw), nil
}
