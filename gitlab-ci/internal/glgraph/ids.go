// SPDX-License-Identifier: Apache-2.0

package glgraph

import "fmt"

// ids.go — the id spellings the graph is joined on.
//
// THEY ARE THE SOURCE PROVIDER'S SPELLINGS, CARRIED VERBATIM. Every one of them
// reproduces an id the built-in CI/CD provider already writes, down to the
// segment order and the capitalised kind word. That is not deference to the old
// code: these ids are what a consumer's stored queries and saved traversals name,
// so a "tidier" spelling would silently orphan them.
//
// EVERY EDGE IN THIS GRAPH IS JOINED ON A STRING BUILT HERE. The source provider
// built each of them with an inline Sprintf at the emission site — twelve of
// them, with three pairs that had to agree across two files — so a drifted
// spelling produced an edge naming nothing while every per-converter test still
// passed. The tests for these functions therefore assert against LITERALS rather
// than against another call of the same helper, and so does the edge-resolution
// test in the parity suite.

// GroupID is the group node's id. It names the group twice, which is the source
// provider's own spelling: the first segment scopes every id in this graph to
// the collect, and the last is this node's own name.
func GroupID(group string) string {
	return fmt.Sprintf("gitlab:%s/Group/%s", group, group)
}

// ProjectID is a project node's id. path is the provider's own
// `namespace/project` path_with_namespace spelling, which may itself carry
// slashes for a subgroup.
func ProjectID(group, path string) string {
	return fmt.Sprintf("gitlab:%s/Project/%s", group, path)
}

// PipelineID is a pipeline DEFINITION node's id, keyed on the project path and
// the ref the definition was read from. It is not a pipeline RUN: see
// [PipelineRunID].
func PipelineID(group, path, ref string) string {
	return fmt.Sprintf("gitlab:%s/Pipeline/%s/%s", group, path, ref)
}

// PipelineRunID is a pipeline-run node's id, keyed on the provider's pipeline id.
func PipelineRunID(group, path string, runID int64) string {
	return fmt.Sprintf("gitlab:%s/PipelineRun/%s/%d", group, path, runID)
}

// JobID is a job node's id, keyed on the provider's job id.
func JobID(group, path string, jobID int64) string {
	return fmt.Sprintf("gitlab:%s/Job/%s/%d", group, path, jobID)
}

// RunnerID is a runner node's id. It carries the GROUP and the runner id and no
// project, on both the group-level and the project-level arm, because the
// provider's runner ids are unique across the instance.
func RunnerID(group string, runnerID int64) string {
	return fmt.Sprintf("gitlab:%s/Runner/%d", group, runnerID)
}

// RunnerTagID is a runner-tag node's id. It is keyed on the group and the tag
// NAME alone, which is what makes one node serve every runner carrying that
// tag — including runners at different scopes.
func RunnerTagID(group, tag string) string {
	return fmt.Sprintf("gitlab:%s/RunnerTag/%s", group, tag)
}

// EnvironmentID is an environment node's id.
func EnvironmentID(group, path, name string) string {
	return fmt.Sprintf("gitlab:%s/Environment/%s/%s", group, path, name)
}

// ProtectionRuleID is a protection-rule node's id, keyed on the environment name
// the rule protects.
func ProtectionRuleID(group, path, envName string) string {
	return fmt.Sprintf("gitlab:%s/ProtectionRule/%s/%s", group, path, envName)
}

// DeploymentID is a deployment node's id, keyed on the provider's deployment id.
func DeploymentID(group, path string, deployID int64) string {
	return fmt.Sprintf("gitlab:%s/Deployment/%s/%d", group, path, deployID)
}

// VariableID is a CI/CD variable node's id. owner is the PROJECT PATH for a
// project-scoped variable and the GROUP for a group-scoped one.
//
// THE TWO SCOPES SHARE ONE SPELLING AND THAT IS WHAT MAKES ONE EDGE CLASS DANGLE
// BY CONSTRUCTION. A pipeline's USES_SECRET edge is built from the variable name
// a job's script referenced, and a `.gitlab-ci.yml` cannot say which scope the
// name came from — so the edge always names the PROJECT-scoped id. A job
// referencing a GROUP-scoped variable therefore names a node that is not there,
// while a node for that variable IS in the same result under a different owner
// segment. That is the source provider's behavior, carried forward, and the
// resolution test asserts it on the exact target string.
func VariableID(group, owner, key string) string {
	return fmt.Sprintf("gitlab:%s/Variable/%s/%s", group, owner, key)
}
