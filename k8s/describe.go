// SPDX-License-Identifier: Apache-2.0

package main

import "github.com/fulminate-io/knowledge-contrib/framework"

// describe.go — WHAT THIS COLLECTOR DECLARES ABOUT ITSELF, served on the
// contract's required describe tool.
//
// EVERY HALF IS READ FROM THE SYMBOL THAT ALREADY HELD IT: the behavior from
// declaredBehavior, the edge vocabulary from EmittedEdgeTypes plus the two
// linkage types, the environment from the disposition map, and the foreign
// context from declaredForeignContext. This module was already the most
// self-describing of the eight; what it lacked was a way to SERVE any of it.
//
// THE LINKAGE EDGE TYPES ARE IN THE VOCABULARY EVEN THOUGH THEY ARE COUNTED
// SEPARATELY ELSEWHERE. EmittedEdgeTypes deliberately excludes them because they
// travel to another graph through the contract's target-graph field; but the
// server's refusal reads every edge a chunk carries, and a cross-graph edge is
// carried on that chunk like any other. A vocabulary without them would refuse
// this collector's own output.

// Describe declares this collector: its suggested behavior, its vocabulary, the
// environment it reads and the foreign-graph context it needs.
func (c *k8sCollector) Describe() framework.Declaration {
	declared := declaredBehavior()
	return framework.Declaration{
		Behavior: framework.BehaviorDeclaration{
			Summarizable:    &declared.Summarizable,
			Embeddable:      &declared.Embeddable,
			Syncable:        &declared.Syncable,
			Bm25Fields:      declared.BM25Fields,
			SummarizeFields: declared.SummarizeFields,
			EmbedFields:     declared.EmbedFields,
		},
		NodeTypes:   []string{nodeTypeCloudResource, nodeTypeProxy},
		EdgeTypes:   append(EmittedEdgeTypes(), linkageEdgeTypes()...),
		Environment: describedEnvironment(),
		Context:     describedForeignContext(),
	}
}

// describedForeignContext is this collector's foreign-graph declaration, served
// verbatim.
//
// THERE IS NOTHING TO RENDER ANY MORE, and that is the point of the shared type:
// the declare side and the describe side are one value, so a module that changes
// what it needs changes it once. The reason each entry carries stays on the
// value and never crosses the wire — the type's own `json:"-"` tag is what keeps
// the client's strict entry decode from refusing the whole config file by name.
func describedForeignContext() framework.ForeignContextDeclaration {
	return declaredForeignContext()
}
