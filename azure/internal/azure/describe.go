// SPDX-License-Identifier: Apache-2.0

package azure

import "github.com/fulminate-io/knowledge-contrib/framework"

// describe.go — WHAT THIS COLLECTOR DECLARES ABOUT ITSELF, served on the
// contract's required describe tool.
//
// IT REPLACES A HAND-APPENDED JSON FRAGMENT. This module's worked entry used to
// carry its behavior block as concatenated TEXT inside the example renderer,
// which no compiler and no test could compare against anything; the values below
// are the same ones, now a typed value the client reads off the wire and writes
// into the entry.

// Describe declares this collector: its suggested behavior, its vocabulary and
// the environment it reads.
func (c *Collector) Describe() framework.Declaration {
	return framework.Declaration{
		Behavior: framework.BehaviorDeclaration{
			Summarizable: new(true),
			Embeddable:   new(true),
			Syncable:     new(true),
			// THE KEYWORD DOCUMENT IS THE THREE FIELDS THIS COLLECTOR FILLS. An
			// Azure resource's searchable text is its name, its one-line summary
			// and its ARM configuration body, and nothing else on the node is
			// worth matching a query against.
			Bm25Fields: []string{"summary", "symbol_name", "content"},
		},
		NodeTypes:   []string{nodeTypeCloudResource},
		EdgeTypes:   append([]string(nil), edgeTypes...),
		Environment: DescribedEnvironment(),
	}
}
