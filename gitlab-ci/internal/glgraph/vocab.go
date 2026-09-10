// SPDX-License-Identifier: Apache-2.0

// Package glgraph is this collector's GRAPH VOCABULARY and its node/edge
// construction: the resource-type and edge-type parity floors, the id spellings
// the floors are joined on, and the one place a walk's intermediate resources
// and relations become contract nodes and edges.
//
// WHY THE FLOORS ARE CHECKED IN RATHER THAN DERIVED. Coverage of this
// collector's source-of-truth vocabulary is a REQUIREMENT, not an emergent
// property: a converter dropped in a refactor takes its resource type out of the
// emitted set silently, and a set derived from the code can never notice. The
// lists below are the target the emitted set is asserted to be a superset of,
// so a dropped converter is a red rather than a smaller graph.
package glgraph

import "slices"

// NodeType is the ONE contract node type every resource in this graph carries,
// with the kind of thing it is in the `resource_type` metadata key.
//
// ONE TYPE PLUS A METADATA KIND IS THE SHAPE A CONSUMER ALREADY QUERIES, and it
// is carried across verbatim rather than re-designed: a consumer filtering on
// resource_type == "pipeline" keeps working, and a per-kind node type would have
// changed every such query for no gain the graph can see.
const NodeType = "cicd-resource"

// Provider is the value of the `provider` metadata key on every node.
const Provider = "gitlab"

// SourceTag is the node `source` field: the provenance tag the CI/CD graph
// shape carries. It names the DOMAIN this node came from rather than the module
// that produced it, which is why it is not the family name.
const SourceTag = "cicd"

// The resource types this collector emits. ELEVEN, one per kind the source
// provider enumerates, and neither of this module's two deliberate departures
// from that provider adds a twelfth.
const (
	ResourceTypeGroup = "group"
	// ResourceTypeProject is emitted for EVERY project the group contains,
	// archived ones included. An archived GitLab project is recorded with the
	// `archived` metadata key and walked by every other enumeration, which is
	// the source provider's own behavior; the sibling GitHub collector skips its
	// archived repositories and that difference is deliberate on both sides.
	ResourceTypeProject  = "project"
	ResourceTypePipeline = "pipeline"
	// ResourceTypePipelineRun keeps the source provider's HYPHEN spelling. A
	// third provider in this family spells the same kind `pipeline_run`; the two
	// disagree in the graph a consumer already reads, so the spelling is carried
	// verbatim rather than harmonized here.
	ResourceTypePipelineRun = "pipeline-run"
	ResourceTypeJob         = "job"
	ResourceTypeRunner      = "runner"
	// ResourceTypeRunnerTag is a first-class node so every HAS_LABEL edge
	// resolves. ONE NODE PER DISTINCT TAG NAME across the whole walk, not one per
	// runner: the id carries the group and the tag name and nothing else, so two
	// runners carrying `docker` are two edges into one node — at either scope.
	ResourceTypeRunnerTag      = "runner-tag"
	ResourceTypeEnvironment    = "environment"
	ResourceTypeProtectionRule = "protection-rule"
	ResourceTypeDeployment     = "deployment"
	// ResourceTypeVariable is a CI/CD variable's NAME and flags. A variable's
	// VALUE is never read and never carried: see the variables enumeration.
	ResourceTypeVariable = "variable"
)

// The edge types this collector emits. SIX, and the seventh CI/CD relationship
// is deliberately absent: see [EdgeTriggeredBy].
const (
	EdgeBelongsTo        = "BELONGS_TO"
	EdgeDeploysTo        = "DEPLOYS_TO"
	EdgeRunsIn           = "RUNS_IN"
	EdgeUsesSecret       = "USES_SECRET"
	EdgeHasLabel         = "HAS_LABEL"
	EdgeRequiresApproval = "REQUIRES_APPROVAL"

	// EdgeTriggeredBy is the one CI/CD relationship this provider does NOT emit,
	// and it is DECLARED here so its absence is asserted rather than assumed.
	// Nothing this collector reads names what triggered a pipeline run in a form
	// that resolves to another node in this graph, so an edge claiming one would
	// be invented. A test asserts the emitted set excludes it, because a floor
	// that only checks presence lets a seventh type appear unnoticed.
	EdgeTriggeredBy = "TRIGGERED_BY"
)

// resourceTypes is the declared resource-type floor, in a fixed order so a
// failure reads the same twice.
var resourceTypes = []string{
	ResourceTypeGroup,
	ResourceTypeProject,
	ResourceTypePipeline,
	ResourceTypePipelineRun,
	ResourceTypeJob,
	ResourceTypeRunner,
	ResourceTypeRunnerTag,
	ResourceTypeEnvironment,
	ResourceTypeProtectionRule,
	ResourceTypeDeployment,
	ResourceTypeVariable,
}

// edgeTypes is the declared edge-type floor. EdgeTriggeredBy is not in it.
var edgeTypes = []string{
	EdgeBelongsTo,
	EdgeDeploysTo,
	EdgeRunsIn,
	EdgeUsesSecret,
	EdgeHasLabel,
	EdgeRequiresApproval,
}

// ResourceTypes returns the declared resource-type vocabulary.
func ResourceTypes() []string { return slices.Clone(resourceTypes) }

// EdgeTypes returns the declared edge-type vocabulary.
func EdgeTypes() []string { return slices.Clone(edgeTypes) }

// IsDeclaredResourceType reports whether a type is in the declared vocabulary.
// [Build] refuses one that is not, which is what closes the coverage assertion
// into an equality rather than leaving it a one-way superset check.
func IsDeclaredResourceType(rt string) bool { return slices.Contains(resourceTypes, rt) }

// IsDeclaredEdgeType reports whether an edge type is in the declared vocabulary.
func IsDeclaredEdgeType(et string) bool { return slices.Contains(edgeTypes, et) }
