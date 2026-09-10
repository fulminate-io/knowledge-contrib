// SPDX-License-Identifier: Apache-2.0

package ghgraph

import "fmt"

// ids.go — the id spellings the graph is joined on.
//
// THEY ARE THE SOURCE PROVIDER'S SPELLINGS, CARRIED VERBATIM. Every one of them
// reproduces an id the built-in CI/CD provider already writes, down to the
// segment order and the capitalised kind word. That is not deference to the old
// code: these ids are what a consumer's stored queries and saved traversals name,
// so a "tidier" spelling would silently orphan them.
//
// EVERY EDGE IN THIS GRAPH IS JOINED ON A STRING BUILT HERE, so a helper that
// drifted from its partner would produce an edge naming nothing while every
// per-converter test still passed. The tests for these functions therefore assert
// against LITERALS rather than against another call of the same helper, and so
// does the edge-resolution test in the parity suite.

// OrganizationID is the organization node's id.
func OrganizationID(org string) string {
	return fmt.Sprintf("github:%s/Organization/%s", org, org)
}

// RepositoryID is a repository node's id. repoFullName is the provider's own
// `owner/name` spelling.
func RepositoryID(org, repoFullName string) string {
	return fmt.Sprintf("github:%s/Repository/%s", org, repoFullName)
}

// WorkflowID is a workflow node's id, keyed on the workflow's file path.
func WorkflowID(org, repoFullName, path string) string {
	return fmt.Sprintf("github:%s/Workflow/%s/%s", org, repoFullName, path)
}

// WorkflowRunID is a workflow-run node's id, keyed on the provider's run id.
func WorkflowRunID(org, repoFullName string, runID int64) string {
	return fmt.Sprintf("github:%s/WorkflowRun/%s/%d", org, repoFullName, runID)
}

// RunnerID is a runner node's id. It carries the ORGANIZATION and the runner id
// and no repository, on both the org-level and the repo-level arm, because the
// provider's runner ids are unique across the organization.
func RunnerID(org string, runnerID int64) string {
	return fmt.Sprintf("github:%s/Runner/%d", org, runnerID)
}

// EnvironmentID is an environment node's id.
func EnvironmentID(org, repoFullName, envName string) string {
	return fmt.Sprintf("github:%s/Environment/%s/%s", org, repoFullName, envName)
}

// DeploymentID is a deployment node's id, keyed on the provider's deployment id.
func DeploymentID(org, repoFullName string, deployID int64) string {
	return fmt.Sprintf("github:%s/Deployment/%s/%d", org, repoFullName, deployID)
}

// SecretID is a secret node's id at repository or environment scope. scope is
// `repo` or `env/<environment name>`.
//
// AN ORGANIZATION-SCOPED SECRET USES [OrgSecretID] AND ITS ID IS A DIFFERENT
// SHAPE — one segment shorter, because there is no repository to name. Both
// spellings are the source provider's, and the difference between them is
// exactly what makes one edge class in this graph dangle by construction: see
// the note on [EdgeUsesSecret] in the workflow converter.
func SecretID(org, repoFullName, scope, name string) string {
	return fmt.Sprintf("github:%s/Secret/%s/%s/%s", org, repoFullName, scope, name)
}

// OrgSecretID is an organization-scoped secret node's id.
func OrgSecretID(org, scope, name string) string {
	return fmt.Sprintf("github:%s/Secret/%s/%s", org, scope, name)
}

// LabelID is a runner-label node's id. It is keyed on the organization and the
// label NAME alone, which is what makes one node serve every runner carrying
// that label — including runners at different scopes.
func LabelID(org, label string) string {
	return fmt.Sprintf("github:%s/Label/%s", org, label)
}

// UserID is a reviewer user node's id, in the spelling the source provider's own
// REQUIRES_APPROVAL target already used.
func UserID(org, login string) string {
	return fmt.Sprintf("github:%s/User/%s", org, login)
}

// TeamID is a reviewer team node's id, in the spelling the source provider's own
// REQUIRES_APPROVAL target already used.
func TeamID(org, slug string) string {
	return fmt.Sprintf("github:%s/Team/%s", org, slug)
}
