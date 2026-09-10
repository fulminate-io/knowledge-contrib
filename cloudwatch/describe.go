// SPDX-License-Identifier: Apache-2.0

package main

import "github.com/fulminate-io/knowledge-contrib/framework"

// describe.go — WHAT THIS COLLECTOR DECLARES ABOUT ITSELF, served on the
// contract's required describe tool.
//
// THE BEHAVIOR IS THE ONE THE WORKED ENTRY ALREADY CARRIED, read from the same
// place: a log graph registered with the two LLM axes off is collected, stored
// and walkable while a search against it reads zero forever, so this collector
// suggests both on and names the two fields whose text is worth indexing.

// Describe declares this collector: its suggested behavior, its vocabulary and
// the environment it reads.
func (c *Collector) Describe() framework.Declaration {
	return framework.Declaration{
		Behavior: framework.BehaviorDeclaration{
			Summarizable: new(true),
			Embeddable:   new(true),
			Syncable:     new(true),
			// A chunk's content is its entry block and its summary is written by
			// the client's own pipeline; those two are the searchable text.
			Bm25Fields: []string{"summary", "content"},
		},
		NodeTypes:   []string{nodeLogTemplate, nodeLogStream, nodeLogChunk, nodeLogLabel, nodeProxy},
		EdgeTypes:   []string{edgeHasLabel, edgeBelongsTo, edgeContains, edgeEmittedBy, edgeCorrelatesWith},
		Environment: describedEnvironment(),
	}
}
