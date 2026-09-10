// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"fmt"

	gogithub "github.com/google/go-github/v68/github"

	"github.com/fulminate-io/knowledge-contrib/github-actions/internal/ghgraph"
)

// secrets.go — the secrets declared at the organization, at each repository and
// at each environment.
//
// NAMES AND METADATA ONLY, NEVER A VALUE. The provider's own listing carries no
// value — a secret's value is write-only and cannot be read back through any
// call this collector makes — and the three fields carried below are the whole
// of what a secret node holds. That is not merely what the API happens to
// return: the graph a collect lands in is indexed, replicated and searchable, so
// a value reaching it would be a credential in a searchable store. A test asserts
// over the RAW BYTES of the whole emitted result that a planted value appears
// nowhere in it.

// The three scopes a secret is declared at. The scope is part of the node's id,
// which is why they are constants rather than literals at the call sites.
const (
	secretScopeOrg  = "org"
	secretScopeRepo = "repo"
	// secretScopeEnvPrefix is joined to the environment's own name, so two
	// environments of one repository declaring the same secret name are two nodes.
	secretScopeEnvPrefix = "env/"
)

// secretDetail is the secret node's Content, and it is the WHOLE of it: a name,
// the scope it is declared at and who it is visible to.
type secretDetail struct {
	Name       string `json:"name"`
	Scope      string `json:"scope"`
	Visibility string `json:"visibility,omitempty"`
}

// Secrets enumerates the organization's secrets, every repository's, and every
// environment's.
func Secrets(api API) Subcollector {
	return Subcollector{
		Name: "github-secrets",
		Run: func(ctx context.Context, org string) (ghgraph.Result, error) {
			var out ghgraph.Result
			var failures reads

			orgSecrets, err := orgSecrets(ctx, api, org)
			out.Add(orgSecrets)
			failures.record("the organization's secrets", err)

			repos, err := listRepositories(ctx, api.Repos, org)
			if err != nil {
				return out, err
			}
			for _, fullName := range repoNames(repos) {
				repoSecrets, err := repoSecrets(ctx, api, org, fullName)
				out.Add(repoSecrets)
				failures.record(fullName, err)

				envSecrets, err := envSecrets(ctx, api, org, fullName)
				out.Add(envSecrets)
				failures.record(fullName+" environments", err)
			}
			return out, failures.err("github-secrets")
		},
	}
}

// orgSecrets pages the organization-level secrets to exhaustion.
func orgSecrets(ctx context.Context, api API, org string) (ghgraph.Result, error) {
	return pageSecrets(
		func(opts *gogithub.ListOptions) (*gogithub.Secrets, *gogithub.Response, error) {
			return api.Actions.ListOrgSecrets(ctx, org, opts)
		},
		func(secret *gogithub.Secret) (ghgraph.Result, error) {
			return secretResource(org, "", secretScopeOrg, secret, ghgraph.OrganizationID(org))
		})
}

// repoSecrets pages one repository's secrets to exhaustion.
func repoSecrets(ctx context.Context, api API, org, fullName string) (ghgraph.Result, error) {
	owner, repo := splitFullName(fullName)
	return pageSecrets(
		func(opts *gogithub.ListOptions) (*gogithub.Secrets, *gogithub.Response, error) {
			return api.Actions.ListRepoSecrets(ctx, owner, repo, opts)
		},
		func(secret *gogithub.Secret) (ghgraph.Result, error) {
			return secretResource(org, fullName, secretScopeRepo, secret,
				ghgraph.RepositoryID(org, fullName))
		})
}

// envSecrets pages every environment's secrets in one repository to exhaustion.
//
// IT RE-READS THE ENVIRONMENT LIST rather than sharing the environments
// enumeration's. The two run concurrently and neither may wait for the other, so
// sharing would turn the fan-out into a dependency graph; the extra listing is
// one call per repository against a walk that already makes several.
func envSecrets(ctx context.Context, api API, org, fullName string) (ghgraph.Result, error) {
	environments, err := listEnvironments(ctx, api.Repos, fullName)
	if err != nil {
		return ghgraph.Result{}, err
	}

	var out ghgraph.Result
	for _, env := range environments {
		envName := env.GetName()
		if envName == "" || env.ID == nil {
			// An environment with no name has no id to hang a secret off, and
			// one with no numeric id cannot be asked about: the provider's
			// environment-secrets call is keyed on the repository id, which
			// arrives on the environment. Neither is a failure of this read.
			continue
		}
		got, err := pageSecrets(
			func(opts *gogithub.ListOptions) (*gogithub.Secrets, *gogithub.Response, error) {
				return api.Actions.ListEnvSecrets(ctx, int(env.GetID()), envName, opts)
			},
			func(secret *gogithub.Secret) (ghgraph.Result, error) {
				return secretResource(org, fullName, secretScopeEnvPrefix+envName, secret,
					ghgraph.EnvironmentID(org, fullName, envName))
			})
		out.Add(got)
		if err != nil {
			return out, fmt.Errorf("environment %q: %w", envName, err)
		}
	}
	return out, nil
}

// pageSecrets is the shared walk over a secrets listing at any scope.
func pageSecrets(
	list func(*gogithub.ListOptions) (*gogithub.Secrets, *gogithub.Response, error),
	convert func(*gogithub.Secret) (ghgraph.Result, error),
) (ghgraph.Result, error) {
	opts := &gogithub.ListOptions{PerPage: perPage}

	var out ghgraph.Result
	for {
		page, resp, err := list(opts)
		if err != nil {
			return out, fmt.Errorf("listing secrets: %w", err)
		}
		if page != nil {
			for _, secret := range page.Secrets {
				converted, err := convert(secret)
				if err != nil {
					return out, err
				}
				out.Add(converted)
			}
		}
		if resp == nil || resp.NextPage == 0 {
			return out, nil
		}
		opts.Page = resp.NextPage
	}
}

// secretResource converts one secret into its node and its edge to whatever
// declares it.
func secretResource(
	org, repoFullName, scope string, secret *gogithub.Secret, parentID string,
) (ghgraph.Result, error) {
	if secret == nil {
		return ghgraph.Result{}, fmt.Errorf("github-secrets: the provider returned a nil secret")
	}
	if secret.Name == "" {
		return ghgraph.Result{}, fmt.Errorf(
			"github-secrets: a %q-scoped secret carries no name, so it has no stable id", scope)
	}
	content, err := marshalDetail("github-secrets", secret.Name, secretDetail{
		Name:       secret.Name,
		Scope:      scope,
		Visibility: secret.Visibility,
	})
	if err != nil {
		return ghgraph.Result{}, err
	}

	// THE ORGANIZATION-SCOPED ID IS ONE SEGMENT SHORTER, because there is no
	// repository to name. It is the source provider's own spelling and it is the
	// reason a workflow's USES_SECRET edge cannot resolve to an org-scoped
	// secret: see the note in workflows.go.
	id := ghgraph.SecretID(org, repoFullName, scope, secret.Name)
	metadata := map[string]string{"org": org, "scope": scope}
	if repoFullName == "" {
		id = ghgraph.OrgSecretID(org, scope, secret.Name)
	} else {
		metadata["repo"] = repoFullName
	}

	return ghgraph.Result{
		Resources: []ghgraph.Resource{{
			ID:           id,
			Name:         secret.Name,
			ResourceType: ghgraph.ResourceTypeSecret,
			Content:      content,
			Metadata:     metadata,
		}},
		Relations: []ghgraph.Relation{{
			FromID: id,
			ToID:   parentID,
			Type:   ghgraph.EdgeBelongsTo,
		}},
	}, nil
}
