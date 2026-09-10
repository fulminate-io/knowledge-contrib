// SPDX-License-Identifier: Apache-2.0

package main

import "github.com/fulminate-io/knowledge-contrib/framework"

// describe.go — WHAT THIS COLLECTOR DECLARES ABOUT ITSELF, served on the
// contract's required describe tool.
//
// ITS BEHAVIOR USED TO LIVE IN THE README ALONE, where nothing could compare it
// to anything. The three booleans below are that block, moved to where the
// collector is and served to the client that writes the entry.

// Describe declares this collector: its suggested behavior, its vocabulary and
// the environment it reads.
func (c *Collector) Describe() framework.Declaration {
	return framework.Declaration{
		Behavior: framework.BehaviorDeclaration{
			Summarizable: new(true),
			Embeddable:   new(true),
			Syncable:     new(true),
		},
		NodeTypes: []string{
			nodeTypeLogTemplate, nodeTypeLogStream, nodeTypeLogChunk, nodeTypeLogLabel, nodeTypeProxy,
		},
		EdgeTypes: []string{
			edgeTypeHasLabel, edgeTypeBelongsTo, edgeTypeContains, edgeTypeEmittedBy, edgeTypeCorrelatesWith,
		},
		Environment: describedEnvironment(),
	}
}
