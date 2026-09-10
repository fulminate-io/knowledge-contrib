// SPDX-License-Identifier: Apache-2.0

// Package ghgraph is this collector's GRAPH VOCABULARY and its node/edge
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
package ghgraph

import "slices"

// NodeType is the ONE contract node type every resource in this graph carries,
// with the kind of thing it is in the `resource_type` metadata key.
//
// ONE TYPE PLUS A METADATA KIND IS THE SHAPE A CONSUMER ALREADY QUERIES, and it
// is carried across verbatim rather than re-designed: a consumer filtering on
// resource_type == "workflow" keeps working, and a per-kind node type would have
// changed every such query for no gain the graph can see.
const NodeType = "cicd-resource"

// Provider is the value of the `provider` metadata key on every node, and the
// scope the summarizer registry is keyed under.
const Provider = "github"

// SourceTag is the node `source` field: the provenance tag the CI/CD graph
// shape carries. It names the DOMAIN this node came from rather than the module
// that produced it, which is why it is not the family name.
const SourceTag = "cicd"

// The resource types this collector emits. EIGHT are the parity floor, one per
// kind the source provider enumerates. THREE more are materialized from fields
// the enumerations already carry: see [ResourceTypeUser], [ResourceTypeTeam]
// and [ResourceTypeLabel].
const (
	ResourceTypeOrganization = "organization"
	ResourceTypeRepository   = "repository"
	ResourceTypeWorkflow     = "workflow"
	ResourceTypeWorkflowRun  = "workflow_run"
	ResourceTypeRunner       = "runner"
	ResourceTypeEnvironment  = "environment"
	ResourceTypeDeployment   = "deployment"
	ResourceTypeSecret       = "secret"

	// ResourceTypeUser is THE PERSON BEHIND A PIECE OF CI/CD ACTIVITY, and it is
	// materialized from every walked field that names one: an environment's
	// required reviewers, a workflow run's actor and triggering actor, and a
	// deployment's creator. Each of those fields rides a response this collector
	// already fetches, so the whole class costs no additional call to the source
	// provider.
	//
	// IT BEGAN AS A NODE THAT EXISTED ONLY SO REQUIRES_APPROVAL COULD RESOLVE and
	// it is no longer only that. A run's actor and a deployment's creator are
	// facts about who runs this organization's pipelines, which is what the
	// [EdgeAttributedTo], [EdgeInitiatedBy] and [EdgeCreatedBy] classes carry.
	//
	// WHAT IT NEVER CARRIES IS AN EMAIL ADDRESS. The provider's user object has
	// one field for a person's address and a run's head commit has another; both
	// are dropped, and a test asserts over the raw bytes of a whole collect that
	// neither reaches the graph.
	ResourceTypeUser = "user"

	// ResourceTypeTeam is MATERIALIZED so every REQUIRES_APPROVAL edge naming a
	// team resolves. An environment's protection rules name a reviewer by login
	// or by slug, and the enumeration this collector already reads carries
	// everything needed to mint the node — so the alternative was an edge whose
	// target named nothing at all.
	ResourceTypeTeam = "team"

	// ResourceTypeLabel is MATERIALIZED on the same terms, so every HAS_LABEL
	// edge resolves. ONE NODE PER DISTINCT LABEL NAME across the whole walk, not
	// one per runner: the id carries the organization and the name and nothing
	// else, so two runners sharing `self-hosted` are two edges into one node.
	ResourceTypeLabel = "label"
)

// The edge types this collector emits, and one CI/CD relationship that is
// deliberately absent: see [EdgeRunsIn].
const (
	EdgeBelongsTo        = "BELONGS_TO"
	EdgeDeploysTo        = "DEPLOYS_TO"
	EdgeUsesSecret       = "USES_SECRET"
	EdgeHasLabel         = "HAS_LABEL"
	EdgeRequiresApproval = "REQUIRES_APPROVAL"

	// EdgeTriggeredBy names the REPOSITORY a run's trigger fired in and carries
	// the trigger EVENT as its evidence. It does NOT name a person, and the three
	// classes below are why it does not have to: the event is a kind of
	// occurrence rather than a thing with an identity, so this edge says which
	// kind fired and where, and the person who caused it is a node of its own at
	// the other end of [EdgeAttributedTo] or [EdgeInitiatedBy].
	EdgeTriggeredBy = "TRIGGERED_BY"

	// The three classes that name a PERSON, one per user-bearing field the walk
	// reads. THEY ARE THREE CLASSES AND NOT ONE because the source provider makes
	// three distinct statements and a single class would collapse them into an
	// edge set a consumer could not read apart: a run whose actor and triggering
	// actor differ is precisely a re-run, and which of the two a deployment's
	// creator resembles is neither.
	//
	// EdgeAttributedTo is a run to the user the provider ATTRIBUTES it to — the
	// `actor` field, the person the run is filed under.
	EdgeAttributedTo = "ATTRIBUTED_TO"
	// EdgeInitiatedBy is a run to the user whose action started THIS run — the
	// `triggering_actor` field. On a first run it is the same person as the
	// actor; on a re-run it is whoever pressed the button, which is the fact this
	// class exists to keep.
	EdgeInitiatedBy = "INITIATED_BY"
	// EdgeCreatedBy is a deployment to the user who created it — the `creator`
	// field. A deployment has one originating person and no re-run, so it takes
	// one class rather than the run's pair.
	EdgeCreatedBy = "CREATED_BY"

	// EdgeRunsIn is the one CI/CD relationship this provider does NOT emit, and
	// it is DECLARED here so its absence is asserted rather than assumed. The
	// source provider emits it nowhere: a run names no runner in any response
	// this collector reads, so an edge claiming one would be invented. A test
	// asserts the emitted set excludes it, because a floor that only checks
	// presence lets a seventh type appear unnoticed.
	EdgeRunsIn = "RUNS_IN"
)

// resourceTypes is the declared resource-type floor, in a fixed order so a
// failure reads the same twice.
var resourceTypes = []string{
	ResourceTypeOrganization,
	ResourceTypeRepository,
	ResourceTypeWorkflow,
	ResourceTypeWorkflowRun,
	ResourceTypeRunner,
	ResourceTypeEnvironment,
	ResourceTypeDeployment,
	ResourceTypeSecret,
	ResourceTypeUser,
	ResourceTypeTeam,
	ResourceTypeLabel,
}

// edgeTypes is the declared edge-type floor. EdgeRunsIn is not in it.
var edgeTypes = []string{
	EdgeBelongsTo,
	EdgeDeploysTo,
	EdgeUsesSecret,
	EdgeHasLabel,
	EdgeRequiresApproval,
	EdgeTriggeredBy,
	EdgeAttributedTo,
	EdgeInitiatedBy,
	EdgeCreatedBy,
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
