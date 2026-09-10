// SPDX-License-Identifier: Apache-2.0

package k8slogs

import "github.com/fulminate-io/knowledge-contrib/framework"

// declaration.go — THE FOREIGN-GRAPH CONTEXT THIS COLLECTOR DECLARES, as code
// rather than as prose in a README.
//
// WHY IT IS CODE. The declaration is what an operator copies into their config
// entry, and it is also what CloudContextFrom reads back out of the block. Those
// two lived in different artifacts — a doc comment and a struct read — and
// nothing made them agree: a declaration that named a family the read did not
// look under would yield a collect that resolved nothing, with no failure
// anywhere. Declaring it here and generating the operator's example from it is
// the same move the k8s collector's registration.go makes, for the same reason.
//
// THE FAMILIES ARE REGISTERED GRAPH TYPES, NOT A BUILT-IN `cloud`. Cloud
// inventory is collected by contrib collectors now, each registering its own
// graph type, so a declaration names the PROVIDERS an operator installed. The
// four below are the contrib collectors that emit the resources a Kubernetes log
// stream can correlate against; an operator running fewer simply deletes the
// entries they have no collector for, and one running a provider this list does
// not name adds it — the read side (CloudContextFrom) takes every declared
// family but `code`, so it needs no change when they do.
//
// THE NODE TYPE IS THE PRODUCING COLLECTOR'S OWN SPELLING, AND IT USED TO BE A
// COMPUTED GUESS. This file declared `family + "-resource"` — aws-resource,
// azure-resource, gcp-resource, k8s-resource — and no collector emits any of
// those four. The aws, azure and k8s inventory collectors all emit ONE node type,
// `cloud-resource`, with the provider's own resource kind in the `resource_type`
// metadata key; gcp emits its resource type AS the node type, one of
// forty-nine `gcp:<service>:<kind>` values plus the `gcp-cidr-block` sentinel.
// The client's fill filters by exact string equality and browses once per
// declared type, so all four declarations selected nothing, this collector's
// CloudContextFrom read an empty block, and every EMITTED_BY proxy and confirmed
// CORRELATES_WITH edge was silently lost. A cross-module census in the root
// module now holds the declared strings against the emitters' own constants, and
// it is the only artifact that can: the modules cannot import each other.

// nodeTypeCloudResource is the ONE node type the aws, azure and k8s inventory
// collectors emit, spelled here because those modules are separate Go modules
// this one cannot import. The census is what keeps this copy honest — it reads
// their constants from source and reds BY NAME when a declared string is one no
// module emits.
const nodeTypeCloudResource = "cloud-resource"

// The families this collector declares. Each is the name a provider collector is
// registered under, which is also the graph family its nodes land in.
const (
	familyAWS   = "aws"
	familyAzure = "azure"
	familyGCP   = "gcp"
	familyK8s   = "k8s"
)

// cloudProviderFamilies are the registered graph types whose resources a
// Kubernetes log stream can correlate against, DERIVED from the declaration
// rather than listed beside it. Two lists that must agree are two lists that can
// disagree; this one cannot.
//
// THEY ARE THE CONTRIB COLLECTORS' REGISTRATION NAMES. Each is the name that
// collector registers itself under, which is also the graph family its nodes
// land in; a declaration naming a type nothing is registered under is refused by
// the client at collect time, naming the types that ARE registered.
func cloudProviderFamilies() []string { return DeclaredForeignContext().Families() }

// gcpReason is the gcp entry's reason, and it is a DISCLOSURE rather than a
// justification: the entry as written selects no nodes at all.
//
// GCP'S NODE TYPE IS ITS RESOURCE TYPE, forty-nine values spelled
// `gcp:<service>:<kind>` plus the `gcp-cidr-block` sentinel, and the client's
// declaration has no prefix, glob or family form — a type is selected by naming
// it exactly, and each named type costs one node browse per gcp graph per
// collect. Enumerating forty-nine of them to select what a family-level form
// will select in one is the wrong artifact to ship, so this entry declares the
// FAMILY and no node types, which by the documented semantics yields the gcp
// graph NAMES and nothing else: no nodes, and therefore no edges, so no gcp
// resource is resolved and no gcp correlation is confirmed until the contract
// gains an explicit family-level selector.
const gcpReason = "NAMES ONLY, AND DELIBERATELY: this entry declares the gcp family with NO node types, " +
	"so the collect receives the gcp graph names and no nodes and no edges. gcp emits its resource " +
	"type as the node type — forty-nine gcp:<service>:<kind> values plus the gcp-cidr-block sentinel " +
	"— and a declaration selects a type only by naming it exactly, at one node browse per type per " +
	"graph per collect. No gcp resource is resolved and no gcp correlation is confirmed from this " +
	"entry; an explicit family-level selector is what restores it."

// cloudResourceReason is the reason the three cloud-resource families share.
const cloudResourceReason = "EMITTED_BY and confirmed CORRELATES_WITH: a log stream's labels name a " +
	"namespace, a cluster and a pod, which identify the resource that emitted it; " +
	"the edges are what CONFIRM a temporal correlation between two templates, so a " +
	"declaration without them yields correlations this collector cannot stand behind."

// DeclaredForeignContext is what this collector declares it needs from other
// graphs, keyed by family in the shape the client's config loader decodes.
//
// THREE ENTRIES ASK FOR THE SAME FIELDS because the matching is on metadata a
// resource carries whatever produced it — a namespace, a cluster name, a region
// — and never on the provider. That uniformity is why the read side flattens the
// families into one set rather than switching on them. The fourth, gcp, asks for
// nothing but its graph names; see gcpReason.
//
// IT IS A LITERAL MAP AND NOT A LOOP, deliberately. The declaration used to be
// built by ranging a family list and computing each node type as
// `family + "-resource"` — a string no collector emits, which is exactly why
// every family arrived empty. A computed type string is also invisible to a
// source-reading census: the four wrong literals appeared 28 times in this
// tree and NOT ONCE in the declaration that shipped them. Spelling each entry
// out is what lets the cross-module census read this file and hold every
// declared string against the emitters' own constants.
func DeclaredForeignContext() framework.ForeignContextDeclaration {
	return framework.ForeignContextDeclaration{
		familyAWS: {
			NodeTypes:  []string{nodeTypeCloudResource},
			NodeFields: []string{"id", "type"},
			MetadataKeys: []string{
				cloudMetaNamespace, cloudMetaCluster, cloudMetaResourceType,
				cloudMetaRegion, cloudMetaProvider,
			},
			EdgeFields: []string{"from_id", "to_id"},
			Reason:     cloudResourceReason,
		},
		familyAzure: {
			NodeTypes:  []string{nodeTypeCloudResource},
			NodeFields: []string{"id", "type"},
			MetadataKeys: []string{
				cloudMetaNamespace, cloudMetaCluster, cloudMetaResourceType,
				cloudMetaRegion, cloudMetaProvider,
			},
			EdgeFields: []string{"from_id", "to_id"},
			Reason:     cloudResourceReason,
		},
		// NO NODE TYPES, AND NOTHING ELSE EITHER. The client refuses node fields,
		// metadata keys or edge fields declared without node types, by name and
		// with the remedy — those fields would ride on nodes that never enter the
		// slice — so the family key alone is the only admissible spelling of "the
		// names and nothing more".
		familyGCP: {Reason: gcpReason},
		familyK8s: {
			NodeTypes:  []string{nodeTypeCloudResource},
			NodeFields: []string{"id", "type"},
			MetadataKeys: []string{
				cloudMetaNamespace, cloudMetaCluster, cloudMetaResourceType,
				cloudMetaRegion, cloudMetaProvider,
			},
			EdgeFields: []string{"from_id", "to_id"},
			Reason:     cloudResourceReason,
		},
	}
}
