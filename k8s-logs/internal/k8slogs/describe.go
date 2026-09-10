// SPDX-License-Identifier: Apache-2.0

package k8slogs

import (
	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/k8s-logs/internal/logpipe"
)

// describe.go — WHAT THIS COLLECTOR DECLARES ABOUT ITSELF, served on the
// contract's required describe tool.
//
// IT IS THE ONE MODULE WITH A PER-NODE-TYPE OVERRIDE, and the override is the
// reason the cascade has that level at all: a log chunk carries compressed bytes
// rather than text, so embedding it produces a vector of compression noise while
// the rest of the graph is worth embedding.

// Describe declares this collector: its suggested behavior, its per-node-type
// override, its vocabulary, the environment it reads and the foreign-graph
// context it correlates against.
func (c *Collector) Describe() framework.Declaration {
	return framework.Declaration{
		Behavior: framework.BehaviorDeclaration{
			Summarizable:    new(true),
			Embeddable:      new(true),
			Syncable:        new(true),
			Bm25Fields:      GraphBM25Fields,
			EmbedFields:     GraphEmbedFields,
			SummarizeFields: GraphSummarizeFields,
		},
		NodeTypeOverrides: map[string]framework.NodeTypeOverrideDeclaration{
			logpipe.NodeLogChunk: {
				Embeddable: new(false),
				Bm25Fields: []string{"symbol_name"},
			},
		},
		NodeTypes: []string{
			logpipe.NodeLogTemplate, logpipe.NodeLogStream, logpipe.NodeLogChunk,
			logpipe.NodeLogLabel, logpipe.NodeProxy,
		},
		EdgeTypes: []string{
			logpipe.EdgeContains, logpipe.EdgeBelongsTo, logpipe.EdgeHasLabel,
			logpipe.EdgeEmittedBy, logpipe.EdgeCorrelatesWith,
		},
		Environment: DescribedEnvironment(),
		Context:     describedForeignContext(),
	}
}

// describedForeignContext is what a COLLECT asks for, which is a different
// question from what this module publishes for review.
//
// THE SLICE IS STATED HERE, from this module's own correlation constants, and it
// is deliberately NOT derived from DeclaredForeignContext(). That value is the
// declaration this module ships in its README and its worked entry, and a
// sibling change is free to narrow it: it names node TYPES, and the client
// refuses node fields declared beside an empty type list, so a family whose
// types could not be named honestly was reduced to the family key alone. Derived
// from that, this render silently became the selector with zero node fields,
// zero metadata keys and zero edges for such a family — a correlation against
// nothing, reported as a successful collect.
//
// EVERY FAMILY USES THE ALL-NODE-TYPES SELECTOR, which is the correction the
// published declaration could not express. This collector correlates on METADATA
// a resource carries whatever produced it — a namespace, a cluster, a region —
// and never on the resource's type; the names it could write, "<family>-resource"
// and then "cloud-resource", were each right for some providers and wrong for
// others, and gcp emits one type per resource kind so no list is maintainable at
// all.
//
// THE EDGE FIELDS ARE WHAT CONFIRM A CORRELATION. A temporal co-occurrence
// between two templates becomes an edge only when the resources behind them have
// a declared dependency, so a declaration without the edge half yields
// correlations this collector cannot stand behind — which is why the node fields
// carry `id`, the field the edge read is pivoted on.
func describedForeignContext() framework.ForeignContextDeclaration {
	return foreignContextFrom(DeclaredForeignContext())
}

// foreignContextFrom renders one family declaration per cloud-provider family,
// taking the published declaration for its REASON and for nothing else.
func foreignContextFrom(published framework.ForeignContextDeclaration) framework.ForeignContextDeclaration {
	out := framework.ForeignContextDeclaration{}
	for _, family := range cloudProviderFamilies() {
		out[family] = framework.ForeignFamilyDeclaration{
			// EVERY NODE OF THE FAMILY, whatever its type.
			AllNodeTypes: true,
			// `id` is what the edge read is pivoted on; `type` is the fallback the
			// correlation reads when a resource carries no resource_type key.
			NodeFields: []string{"id", "type"},
			MetadataKeys: []string{
				cloudMetaNamespace, cloudMetaCluster, cloudMetaResourceType,
				cloudMetaRegion, cloudMetaProvider,
			},
			// THE EDGES ARE WHAT CONFIRM A CORRELATION: a temporal co-occurrence
			// between two templates becomes an edge only when the resources behind
			// them have a declared dependency.
			EdgeFields: []string{"from_id", "to_id"},
			Reason:     published[family].Reason,
		}
	}
	return out
}
