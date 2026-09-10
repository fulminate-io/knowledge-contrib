// SPDX-License-Identifier: Apache-2.0

package bbgraph

import "fmt"

// ids.go — the id spellings the graph is joined on.
//
// THEY ARE THE SOURCE PROVIDER'S SPELLINGS, CARRIED VERBATIM. Every one of them
// reproduces an id the built-in CI/CD provider already writes, down to the
// segment order and the capitalised kind word. That is not deference to the old
// code: these ids are what a consumer's stored queries and saved traversals
// name, so a "tidier" spelling would silently orphan them.
//
// EVERY EDGE IN THIS GRAPH IS JOINED ON A STRING BUILT HERE, so a helper that
// drifted from its partner would produce an edge naming nothing while every
// per-converter test still passed. The tests for these functions therefore
// assert against LITERALS rather than against another call of the same helper,
// and so does the edge-resolution suite.

// WorkspaceID is the workspace root node's id. The workspace name appears twice
// and that is the source provider's spelling: the first segment scopes every id
// in the graph and the last is this node's own key.
func WorkspaceID(workspace string) string {
	return fmt.Sprintf("bitbucket:%s/Workspace/%s", workspace, workspace)
}

// RepositoryID is a repository node's id, keyed on the provider's own slug.
func RepositoryID(workspace, slug string) string {
	return fmt.Sprintf("bitbucket:%s/Repository/%s", workspace, slug)
}

// PipelineID is a pipeline definition's node id. name is the flattened
// definition name — `default`, or `<section>/<trigger key>`.
func PipelineID(workspace, slug, name string) string {
	return fmt.Sprintf("bitbucket:%s/Pipeline/%s/%s", workspace, slug, name)
}

// PipelineRunID is a pipeline-run node's id, keyed on the provider's own UUID
// including the braces the API returns it in.
func PipelineRunID(workspace, slug, uuid string) string {
	return fmt.Sprintf("bitbucket:%s/PipelineRun/%s/%s", workspace, slug, uuid)
}

// RunnerID is a runner node's id. It carries the WORKSPACE and the runner UUID
// and no repository, on both the workspace-level and the repository-level arm,
// because the provider's runner UUIDs are unique across the workspace — which is
// also what lets one runner reached from two scopes deduplicate to one node.
func RunnerID(workspace, uuid string) string {
	return fmt.Sprintf("bitbucket:%s/Runner/%s", workspace, uuid)
}

// LabelID is a runner-label node's id. It is keyed on the workspace and the
// label NAME alone, which is what makes one node serve every runner carrying
// that label — including runners at different scopes — and what makes a
// pipeline step's `runs-on` reference resolve to the same node.
func LabelID(workspace, label string) string {
	return fmt.Sprintf("bitbucket:%s/Label/%s", workspace, label)
}

// EnvironmentID is a deployment environment's node id.
func EnvironmentID(workspace, slug, envName string) string {
	return fmt.Sprintf("bitbucket:%s/Environment/%s/%s", workspace, slug, envName)
}

// ApprovalGateID is an approval gate's node id. It is keyed on the same
// repository and environment as the environment it guards, so one environment
// has at most one gate.
func ApprovalGateID(workspace, slug, envName string) string {
	return fmt.Sprintf("bitbucket:%s/ApprovalGate/%s/%s", workspace, slug, envName)
}

// The three scope keys a variable id can carry. THE SCOPE KEY IS NOT THE SCOPE
// WORD: a repository-scoped variable's key is the word joined to the repository
// slug, and a deployment-scoped one's first segment is the literal `env` that
// the source provider mints rather than the word `deployment` its stored `scope`
// metadata carries. Both spellings are load-bearing and they are different
// strings, so they are built by three functions rather than by one with a
// branch.

// WorkspaceVariableID is a workspace-scoped variable's node id.
func WorkspaceVariableID(workspace, key string) string {
	return fmt.Sprintf("bitbucket:%s/Variable/workspace/%s", workspace, key)
}

// RepositoryVariableID is a repository-scoped variable's node id.
func RepositoryVariableID(workspace, slug, key string) string {
	return fmt.Sprintf("bitbucket:%s/Variable/repository/%s/%s", workspace, slug, key)
}

// DeploymentVariableID is a deployment-environment-scoped variable's node id.
//
// ITS FIRST SCOPE SEGMENT IS `env` AND NOT `deployment`. The source provider
// mints that literal in the id while storing `deployment` as the variable's
// scope metadata, so the two spellings are deliberately different and both are
// reproduced.
func DeploymentVariableID(workspace, slug, envName, key string) string {
	return fmt.Sprintf("bitbucket:%s/Variable/env/%s/%s/%s", workspace, slug, envName, key)
}
