// SPDX-License-Identifier: Apache-2.0

// Package bbgraph is this collector's GRAPH VOCABULARY and its node/edge
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
package bbgraph

import "slices"

// NodeType is the ONE contract node type every resource in this graph carries,
// with the kind of thing it is in the `resource_type` metadata key.
//
// ONE TYPE PLUS A METADATA KIND IS THE SHAPE A CONSUMER ALREADY QUERIES, and it
// is carried across verbatim rather than re-designed: a consumer filtering on
// resource_type == "pipeline_run" keeps working, and a per-kind node type would
// have changed every such query for no gain the graph can see.
const NodeType = "cicd-resource"

// Provider is the value of the `provider` metadata key on every node, and the
// spelling the source provider's own nodes carried. It is `bitbucket` and NOT
// the collector's family name: a consumer that filters on the provider is
// asking which system the resource lives in, not which module read it.
const Provider = "bitbucket"

// SourceTag is the node `source` field: the provenance tag the CI/CD graph shape
// carries. It names the DOMAIN this node came from rather than the module that
// produced it, which is why it is not the family name.
const SourceTag = "cicd"

// The resource types this collector emits. NINE, one per kind the source
// provider enumerates, and `label` is among them for the reason on
// [ResourceTypeLabel].
const (
	ResourceTypeWorkspace    = "workspace"
	ResourceTypeRepository   = "repository"
	ResourceTypePipeline     = "pipeline"
	ResourceTypePipelineRun  = "pipeline_run"
	ResourceTypeRunner       = "runner"
	ResourceTypeEnvironment  = "environment"
	ResourceTypeApprovalGate = "approval_gate"
	ResourceTypeVariable     = "variable"

	// ResourceTypeLabel is MATERIALIZED so every HAS_LABEL and RUNS_IN edge
	// resolves. ONE NODE PER DISTINCT LABEL NAME across the whole walk, not one
	// per runner: the id carries the workspace and the name and nothing else, so
	// two runners sharing `self-hosted` are two edges into one node. The source
	// provider minted a node per (runner, label) pair with the same id and no
	// dedup across runners; [Build] deduplicates, which is what makes the rule
	// true rather than merely intended.
	ResourceTypeLabel = "label"
)

// ResourceTypePipelineRun IS SPELLED WITH AN UNDERSCORE, and it is worth saying
// so because the sibling CI/CD providers do not agree with each other. This is
// the source provider's own spelling, and it is what a consumer's stored query
// already names.

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
	// and it is DECLARED here so its absence is asserted rather than assumed. The
	// source provider emits it nowhere: a pipeline run carries a trigger TYPE and
	// names no other resource, so an edge asserting one would be invented. A test
	// asserts the emitted set excludes it, because a floor that only checks
	// presence lets a seventh type appear unnoticed.
	EdgeTriggeredBy = "TRIGGERED_BY"
)

// resourceTypes is the declared resource-type floor, in a fixed order so a
// failure reads the same twice.
var resourceTypes = []string{
	ResourceTypeWorkspace,
	ResourceTypeRepository,
	ResourceTypePipeline,
	ResourceTypePipelineRun,
	ResourceTypeRunner,
	ResourceTypeLabel,
	ResourceTypeEnvironment,
	ResourceTypeApprovalGate,
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
