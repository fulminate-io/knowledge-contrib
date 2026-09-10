// SPDX-License-Identifier: Apache-2.0

package bbgraph

import (
	"slices"
	"strings"
)

// resolve.go — THE USES_SECRET JOIN, and the one place this collector's graph
// deliberately differs from the built-in provider's.
//
// THE DEFECT IT FIXES IS STRUCTURAL RATHER THAN DATA-DEPENDENT. The source
// provider builds a pipeline's USES_SECRET target as
// `bitbucket:<workspace>/Variable/<name>` — three path segments after the
// workspace — while every variable node it writes has four or more, because a
// variable id carries its scope key. The two forms differ in SEGMENT COUNT, so
// no input makes them equal: every one of that provider's USES_SECRET edges
// names a node it never created. A dangling edge is contractually legal and the
// write path stores it verbatim, so nothing downstream ever noticed; what it
// produces is a relationship no traversal can follow.
//
// THE FIX IS THE TARGET SHAPE, NOT THE EDGE COUNT. Recorded as a deliberate
// deviation from the built-in. The edges are emitted in the shape of this
// module's own variable ids so they RESOLVE, and where nothing satisfies a
// reference no edge is emitted at all — see below for why that is not a silent
// drop.
//
// WHY THE JOIN IS HERE AND NOT IN THE ENUMERATION THAT READS THE PIPELINE. The
// pipelines-config and variables enumerations run in parallel and neither can
// see the other's result, so at the moment a step's `$VAR` is read there is no
// variable set to resolve it against. The join runs once, after every
// enumeration has returned.

// EnsureWorkspaceRoot mints the workspace node when the walk emitted anything
// that belongs to it and no enumeration produced it.
//
// WHY IT IS NEEDED AT ALL, and it is a defect found while building this module.
// The repositories enumeration emits the workspace node only when the workspace
// has at least one repository — that is the source provider's behavior and it is
// reproduced. But the runners and variables enumerations read the workspace
// SCOPE directly, so a workspace with no repositories and one workspace-level
// runner produced a runner node whose BELONGS_TO edge named a workspace node
// nothing ever created. That is the same defect class as the USES_SECRET target
// this module already fixes: an edge no traversal can follow, legal at the
// contract and invisible downstream.
//
// IT IS NOT THE SAME AS ALWAYS EMITTING THE ROOT. A workspace with nothing in it
// at all still collects to zero nodes, which is the source provider's behavior
// and what the enumeration's own row asserts; this mints the root only when
// there is something for it to be the root OF.
func EnsureWorkspaceRoot(r *Result, workspace string) {
	if len(r.Resources) == 0 {
		return
	}
	id := WorkspaceID(workspace)
	for _, res := range r.Resources {
		if res.ID == id {
			return
		}
	}
	r.Resources = append(r.Resources, Resource{
		ID:           id,
		Name:         workspace,
		ResourceType: ResourceTypeWorkspace,
		Metadata:     map[string]string{"workspace": workspace},
	})
}

// SecretRef is one variable reference a pipeline step names, carried out of the
// enumeration that read it so the whole walk can resolve it later.
type SecretRef struct {
	// PipelineID is the pipeline node the reference was found in.
	PipelineID string
	// RepoSlug is the repository the pipeline belongs to, which scopes the
	// repository-level and deployment-level candidates.
	RepoSlug string
	// Deployment is the step's `deployment` value, empty when the step names
	// none. It is what makes an environment-scoped variable a candidate.
	Deployment string
	// Name is the referenced variable's name, as the step spelled it.
	Name string
}

// UnresolvedRefsKey is the metadata key a pipeline node carries the references
// nothing satisfied under.
//
// THE OMISSION IS RECORDED RATHER THAN SILENT, which is the whole reason this
// key exists. A `$VAR` matching no variable is a shell variable, a provider
// built-in the filter did not list, or a variable the credential could not read;
// emitting an edge to an id nothing ever creates is the defect this join exists
// to end, and emitting nothing while saying nothing would replace one invisible
// state with another. A reader of the pipeline node can see exactly which names
// went unmatched.
const UnresolvedRefsKey = "unresolved_variable_refs"

// ResolveSecretRefs turns the walk's carried references into USES_SECRET
// relations, and records on each pipeline node the references nothing satisfied.
//
// THE PRECEDENCE IS THE PROVIDER'S OWN, MOST SPECIFIC FIRST: a deployment
// environment's variable shadows the repository's, which shadows the
// workspace's. At most ONE edge is emitted per (pipeline, name) pair, to the
// first candidate the walk itself emitted — resolving against the walk's own
// variable ids rather than against a guess is what makes every emitted edge
// resolvable by construction.
//
// A REFUSED VARIABLE READ CANNOT BE MISREAD AS "no such variable", because a
// refused or failed read makes the whole walk INCOMPLETE and says so. So an
// unresolved reference on a COMPLETE walk means the name really is not a
// variable of this workspace.
func ResolveSecretRefs(r *Result, workspace string) {
	if len(r.SecretRefs) == 0 {
		return
	}
	variables := variableIDs(r.Resources)
	unresolved := map[string]map[string]bool{}

	for _, ref := range r.SecretRefs {
		if target, ok := resolveOne(workspace, ref, variables); ok {
			r.Relations = append(r.Relations, Relation{
				FromID: ref.PipelineID,
				ToID:   target,
				Type:   EdgeUsesSecret,
			})
			continue
		}
		if unresolved[ref.PipelineID] == nil {
			unresolved[ref.PipelineID] = map[string]bool{}
		}
		unresolved[ref.PipelineID][ref.Name] = true
	}

	stampUnresolved(r.Resources, unresolved, UnresolvedRefsKey)
}

// resolveOne returns the id of the most specific variable this walk emitted that
// satisfies one reference.
func resolveOne(workspace string, ref SecretRef, variables map[string]bool) (string, bool) {
	var candidates []string
	if ref.Deployment != "" {
		candidates = append(candidates,
			DeploymentVariableID(workspace, ref.RepoSlug, ref.Deployment, ref.Name))
	}
	candidates = append(candidates,
		RepositoryVariableID(workspace, ref.RepoSlug, ref.Name),
		WorkspaceVariableID(workspace, ref.Name))

	for _, candidate := range candidates {
		if variables[candidate] {
			return candidate, true
		}
	}
	return "", false
}

// variableIDs is the set of variable node ids this walk emitted. It is built
// from the RESULT rather than from the reference's own guess, which is what
// makes an emitted edge resolvable by construction.
func variableIDs(resources []Resource) map[string]bool {
	out := make(map[string]bool, len(resources))
	for _, res := range resources {
		if res.ResourceType == ResourceTypeVariable {
			out[res.ID] = true
		}
	}
	return out
}

// stampUnresolved writes the unmatched names onto their pipeline nodes, sorted
// and comma-joined so two collects of an unchanged workspace produce the same
// bytes.
func stampUnresolved(
	resources []Resource, unresolved map[string]map[string]bool, key string,
) {
	for i := range resources {
		names, ok := unresolved[resources[i].ID]
		if !ok {
			continue
		}
		sorted := make([]string, 0, len(names))
		for name := range names {
			sorted = append(sorted, name)
		}
		slices.Sort(sorted)
		if resources[i].Metadata == nil {
			resources[i].Metadata = map[string]string{}
		}
		resources[i].Metadata[key] = strings.Join(sorted, ",")
	}
}

// StepTarget is one node a pipeline step NAMES rather than declares: the
// environment it deploys to, or a runner label it asks to run on.
type StepTarget struct {
	// PipelineID is the pipeline node the step belongs to.
	PipelineID string
	// RepoSlug scopes an environment, which is per repository. A label is not
	// scoped by it; the field is carried for both so one type serves both.
	RepoSlug string
	// Name is the environment's name or the label's, as the YAML spelled it.
	Name string
	// Edge is the relationship this reference would become: [EdgeDeploysTo] or
	// [EdgeRunsIn].
	Edge string
}

// The metadata keys a pipeline node carries the step references nothing
// satisfied under.
//
// THEY EXIST FOR THE REASON [UnresolvedRefsKey] DOES. A step's `deployment` and
// its `runs-on` are read out of the repository's bitbucket-pipelines.yml; the
// environments and the runners that would satisfy them are read from the API by
// two other enumerations. A workspace holds both mismatches every day — a step
// deploying to an environment nobody created in the deployments config, a
// `runs-on` label no online runner advertises — and neither enumeration can
// supply the node.
//
// SO THE EDGE IS NOT EMITTED AND THE NAME IS RECORDED. The alternative the
// source provider takes is to emit the edge anyway, which produces a
// relationship no traversal can follow and no query returns; the alternative of
// MINTING the node would assert that an environment or a runner label exists
// when the provider says it does not. Recording the name keeps the omission
// visible without inventing a resource.
const (
	UnresolvedDeploymentsKey = "unresolved_deployments"
	UnresolvedRunsOnKey      = "unresolved_runs_on"
)

// ResolveStepTargets turns the step references the walk carried into DEPLOYS_TO
// and RUNS_IN relations, and records on each pipeline node the names nothing
// satisfied.
//
// AN UNRESOLVED NAME ON A COMPLETE WALK IS A REAL MISMATCH. A refused or failed
// environments or runners read makes the whole walk INCOMPLETE and says so, so a
// name recorded here on a walk that asserts completeness means the environment
// or the label really is not there.
func ResolveStepTargets(r *Result, workspace string) {
	if len(r.StepTargets) == 0 {
		return
	}
	emitted := resourceIDs(r.Resources)
	unresolved := map[string]map[string]map[string]bool{}

	for _, target := range r.StepTargets {
		id := LabelID(workspace, target.Name)
		key := UnresolvedRunsOnKey
		if target.Edge == EdgeDeploysTo {
			id = EnvironmentID(workspace, target.RepoSlug, target.Name)
			key = UnresolvedDeploymentsKey
		}
		if emitted[id] {
			r.Relations = append(r.Relations, Relation{
				FromID: target.PipelineID,
				ToID:   id,
				Type:   target.Edge,
			})
			continue
		}
		if unresolved[target.PipelineID] == nil {
			unresolved[target.PipelineID] = map[string]map[string]bool{}
		}
		if unresolved[target.PipelineID][key] == nil {
			unresolved[target.PipelineID][key] = map[string]bool{}
		}
		unresolved[target.PipelineID][key][target.Name] = true
	}

	for pipelineID, byKey := range unresolved {
		for key, names := range byKey {
			stampUnresolved(r.Resources, map[string]map[string]bool{pipelineID: names}, key)
		}
	}
}

// resourceIDs is the set of every node id this walk emitted.
func resourceIDs(resources []Resource) map[string]bool {
	out := make(map[string]bool, len(resources))
	for _, res := range resources {
		out[res.ID] = true
	}
	return out
}
