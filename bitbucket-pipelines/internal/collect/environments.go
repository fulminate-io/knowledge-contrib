// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbclient"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
)

// environments.go — each repository's deployment environments, and the approval
// gate an environment with a deployment restriction implies.
//
// THE GATE IS DERIVED RATHER THAN READ. The provider has no approval-gate
// resource: an environment either carries a lock or names admin-only
// restrictions, and the source provider mints a node for that condition so the
// restriction is a thing in the graph rather than a field on one. Reproduced
// verbatim, including the two conditions it fires on.

// Environments enumerates every repository's deployment environments.
func Environments(client *bbclient.Client, repos []RepoInfo) Subcollector {
	return Subcollector{
		Name: "bitbucket-environments",
		Run: func(ctx context.Context, workspace string) (bbgraph.Result, error) {
			var out bbgraph.Result
			var failures reads

			for _, repo := range repos {
				if err := ctx.Err(); err != nil {
					return out, err
				}
				envs, err := ListEnvironments(ctx, client, workspace, repo.Slug)
				out.Add(buildEnvironments(workspace, repo.Slug, envs, &failures))
				failures.record(repo.Slug, err)
			}
			return out, failures.err("bitbucket-environments")
		},
	}
}

// ListEnvironments pages one repository's environments to exhaustion.
//
// IT IS EXPORTED BECAUSE THE VARIABLES ENUMERATION READS IT TOO. The two run in
// parallel and neither may wait for the other, so the deployment-variable walk
// re-lists the environments it needs the UUIDs of rather than sharing this one's
// result — the same trade the source provider makes, and the same one the
// sibling CI/CD collector makes for its own environment secrets.
func ListEnvironments(
	ctx context.Context, client *bbclient.Client, workspace, slug string,
) ([]apiEnvironment, error) {
	path := fmt.Sprintf("repositories/%s/%s/environments", workspace, slug)

	var envs []apiEnvironment
	err := client.GetPaginated(ctx, path, func(raw json.RawMessage) error {
		var page []apiEnvironment
		if err := json.Unmarshal(raw, &page); err != nil {
			return fmt.Errorf("decoding an environments page: %w", err)
		}
		envs = append(envs, page...)
		return nil
	})
	switch {
	case err == nil:
		return envs, nil
	case IsNotFound(err):
		// A REPOSITORY WITH NO DEPLOYMENT ENVIRONMENTS ANSWERS 200 WITH AN EMPTY
		// PAGE, so a 404 is a listing this walk could not read. It matters twice
		// over here: this read is the one the deployment-variable walk depends on,
		// so a 404 taken as an empty list silently costs every deployment variable
		// in the repository as well as every environment.
		return envs, notFoundOnAListing("environments", path, err)
	default:
		return envs, fmt.Errorf("reading environments: %w", err)
	}
}

// buildEnvironments converts one repository's environments and their gates.
func buildEnvironments(
	workspace, slug string, envs []apiEnvironment, failures *reads,
) bbgraph.Result {
	repoID := bbgraph.RepositoryID(workspace, slug)
	var out bbgraph.Result

	for _, env := range envs {
		envID := bbgraph.EnvironmentID(workspace, slug, env.Name)
		metadata := map[string]string{
			"workspace": workspace,
			"repo":      slug,
		}
		putIfSet(metadata, "environment_type", env.EnvironmentType.Name)
		putIfPositive(metadata, "rank", env.Rank)

		content, ok := renderContent(failures, slug+" environment "+env.Name, env)
		if !ok {
			continue
		}
		out.Resources = append(out.Resources, bbgraph.Resource{
			ID:           envID,
			Name:         env.Name,
			ResourceType: bbgraph.ResourceTypeEnvironment,
			Content:      content,
			Metadata:     metadata,
		})
		out.Relations = append(out.Relations, bbgraph.Relation{
			FromID: envID,
			ToID:   repoID,
			Type:   bbgraph.EdgeBelongsTo,
		})

		if !env.restricted() {
			continue
		}
		gateID := bbgraph.ApprovalGateID(workspace, slug, env.Name)
		// THE GATE NODE CARRIES NO CONTENT, which is the source provider's shape:
		// the restriction it stands for is already the environment's own content.
		out.Resources = append(out.Resources, bbgraph.Resource{
			ID:           gateID,
			Name:         env.Name + " approval",
			ResourceType: bbgraph.ResourceTypeApprovalGate,
			Metadata: map[string]string{
				"workspace":   workspace,
				"repo":        slug,
				"environment": env.Name,
			},
		})
		out.Relations = append(out.Relations, bbgraph.Relation{
			FromID: envID,
			ToID:   gateID,
			Type:   bbgraph.EdgeRequiresApproval,
		})
	}
	return out
}

// apiEnvironment is the deployment-environment shape this collector reads.
type apiEnvironment struct {
	UUID            string `json:"uuid"`
	Name            string `json:"name"`
	Slug            string `json:"slug"`
	Rank            int    `json:"rank"`
	EnvironmentType struct {
		Name string `json:"name"`
	} `json:"environment_type"`
	Lock struct {
		Type string `json:"type"`
	} `json:"lock"`
	// RESTRICTIONS.ADMIN_ONLY IS A BOOLEAN, and it was declared here as a slice.
	// The provider sends `{"type":"deployment_restrictions_configuration",
	// "admin_only":false}`; encoding/json refuses a bool into a slice, the page
	// handler returns the decode error, and the WHOLE environments read fails for
	// that repository — taking every environment, every approval gate, and every
	// deployment-scope variable with it, since the variables enumeration re-lists
	// the environments to find the UUIDs it needs. Read off the live API with a
	// read-only credential; the fixture that agreed with the slice carried an
	// empty array no provider ever sent.
	Restrictions struct {
		AdminOnly bool `json:"admin_only"`
	} `json:"restrictions"`
}

// restricted reports whether this environment implies an approval gate. The two
// conditions are the source provider's.
//
// THE LOCK ARM IS FIXTURE-ONLY AND THE PROVIDER IS NOT KNOWN TO SATISFY IT. A
// live environment with no lock reports `lock.type` as
// `deployment_environment_lock_open`, never the empty string and never `lock`,
// so this comparison matches nothing that has been observed. What a LOCKED
// environment reports has not been observed at all, and the spelling is not
// guessed here: the live workspace holds three environments and all three are
// open. The admin-only arm is the one this module has seen the provider drive.
func (e apiEnvironment) restricted() bool {
	return e.Lock.Type == "lock" || e.Restrictions.AdminOnly
}
