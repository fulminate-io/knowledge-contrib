// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/enventry"
	"github.com/fulminate-io/knowledge-contrib/gcp/internal/gcpgraph"
)

// describe.go — WHAT THIS COLLECTOR DECLARES ABOUT ITSELF, served on the
// contract's required describe tool.
//
// THIS COLLECTOR'S NODE TYPE IS THE RESOURCE TYPE ITSELF, which makes its
// vocabulary the largest in the tree: one entry per resource kind rather than
// one constant. It is read from gcpgraph's checked-in floor, the same value the
// build path enforces against, so a resource kind this collector starts emitting
// is declared by the change that adds it rather than by a second edit here.

// Describe declares this collector: its suggested behavior, its vocabulary and
// the environment it reads.
//
// THE THREE FIELD LISTS ARE THE ONES THE WORKED ENTRY ALREADY CARRIED, with the
// same reasoning: the embed text is the summary and the name, because the
// content is a JSON document and embedding one spends the vector on its
// punctuation; the summarizer gets the content, which is the configuration it
// exists to describe; and the keyword document is all three, because a search
// for an address or a machine type finds it in the content and nowhere else.
func (c Collector) Describe() framework.Declaration {
	return framework.Declaration{
		Behavior: framework.BehaviorDeclaration{
			Summarizable:    new(true),
			Embeddable:      new(true),
			Syncable:        new(true),
			EmbedFields:     []string{"summary", "symbol_name"},
			SummarizeFields: []string{"content", "symbol_name", "summary"},
			Bm25Fields:      []string{"symbol_name", "summary", "content"},
		},
		NodeTypes:   gcpgraph.ResourceTypes(),
		EdgeTypes:   gcpgraph.EdgeTypes(),
		Environment: enventry.DescribedEnvironment(),
	}
}
