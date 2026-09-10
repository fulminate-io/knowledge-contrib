// SPDX-License-Identifier: Apache-2.0

package walk

import (
	"slices"

	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/bbgraph"
	"github.com/fulminate-io/knowledge-contrib/bitbucket-pipelines/internal/enventry"
	"github.com/fulminate-io/knowledge-contrib/framework"
)

// describe.go — WHAT THIS COLLECTOR DECLARES ABOUT ITSELF, served on the
// contract's required describe tool.
//
// EVERY VALUE IS READ FROM THE SYMBOL THAT ALREADY HELD IT: the vocabulary from
// bbgraph's declared resource and edge types, the environment from enventry's
// names and their classes. This module already declared each of those in code
// for its own censuses; what it lacked was a way to SERVE any of it, so an
// operator's entry carried a transcription instead.

// Describe declares this collector: its suggested behavior, its vocabulary and
// the environment it reads.
//
// THE TWO CREDENTIAL NAMES ARE DECLARED AS SECRETS, which is what makes their
// absence from an installed entry a decision rather than an oversight: an
// installer writes neither in any state, and an operator reading the declaration
// can see that the collector needs one and that nothing wrote it.
//
// THE FIELD LISTS ARE THE ONES THE WORKED ENTRY ALREADY CARRIED, with the same
// reasoning: the content is a JSON document or a workflow definition, so the
// embed text is the summary and the name, the summarizer gets the content, and
// the keyword document is all three.
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
		// THE ONE CONTRACT NODE TYPE THIS WALK STAMPS ON EVERY NODE, which is
		// what `node_types` means: the set the server checks an INGESTED node's
		// type field against, not the set of things this collector knows about.
		//
		// IT IS NOT bbgraph.ResourceTypes(). That is the resource-KIND floor —
		// the value of the `resource_type` METADATA key — and declaring it here
		// published a vocabulary no node ever carries, so the server's
		// undeclared-type gate refused the first chunk of every collect by name
		// and this collector produced no graph at all. The kinds have no field
		// in this declaration and are not lost by their absence from it: they
		// are on every node, in metadata, where a consumer already queries them.
		NodeTypes:   []string{bbgraph.NodeType},
		EdgeTypes:   bbgraph.EdgeTypes(),
		Environment: describedEnvironment(),
	}
}

// describedEnvironment renders this collector's environment declaration from the
// two symbols that already hold it: the names it reads, and the class of each.
//
// SORTED, so one unchanged collector renders one byte-identical declaration and
// the installer table generated from it does not reorder itself between runs.
// enventry.Names is in CONSULTATION order, which answers a different question.
func describedEnvironment() []framework.EnvDeclaration {
	classes := enventry.Classes()
	names := slices.Clone(enventry.Names())
	slices.Sort(names)
	out := make([]framework.EnvDeclaration, 0, len(names))
	for _, name := range names {
		out = append(out, framework.EnvDeclaration{
			Name:  name,
			Class: string(classes[name]),
			// The third symbol that already held an answer: which names this
			// collector tells present-and-empty apart from absent. It is read here
			// rather than restated, on the same terms as the class.
			EmptySensitive: enventry.EmptySensitive(name),
		})
	}
	return out
}
