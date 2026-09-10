// SPDX-License-Identifier: Apache-2.0

package awswalk

import "github.com/fulminate-io/knowledge-contrib/framework"

// describe.go — WHAT THIS COLLECTOR DECLARES ABOUT ITSELF, served on the
// contract's required describe tool.
//
// EVERY VALUE IS READ FROM THIS MODULE'S OWN SYMBOLS rather than retyped. The
// node type is the one constant this collector emits, the edge types are the
// parity floor the coverage suite already asserts against, and the environment
// rows are the disposition map beside the names. A declaration typed out beside
// those values would be a second copy that drifts, which is the state this tool
// exists to end.

// Describe declares this collector: its suggested behavior, its vocabulary and
// the environment it reads.
//
// THE TWO LLM AXES ARE A SUGGESTION. A cloud-resource graph is worth
// summarizing and embedding — a resource's own configuration is what an
// operator searches for — but what is paid for is the operator's decision, and
// this document is printed by the add rather than applied.
//
// NO FIELD LISTS ARE DECLARED, and that is this collector's own position rather
// than an omission: its nodes carry the resource's configuration in content and
// its name in symbol_name, and the client's default composition already reads
// both. A list here would narrow that for every operator who installs it.
func (c *Collector) Describe() framework.Declaration {
	return framework.Declaration{
		Behavior: framework.BehaviorDeclaration{
			Summarizable: new(true),
			Embeddable:   new(true),
			Syncable:     new(true),
		},
		NodeTypes:   []string{NodeTypeCloudResource},
		EdgeTypes:   append([]string(nil), AllEdgeTypes...),
		Environment: DescribedEnvironment(),
	}
}
