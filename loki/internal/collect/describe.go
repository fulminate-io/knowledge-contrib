// SPDX-License-Identifier: Apache-2.0

package collect

import (
	"github.com/fulminate-io/knowledge-contrib/framework"
	"github.com/fulminate-io/knowledge-contrib/loki/internal/logpipe"
	"github.com/fulminate-io/knowledge-contrib/loki/internal/lokiapi"
)

// describe.go — WHAT THIS COLLECTOR DECLARES ABOUT ITSELF, served on the
// contract's required describe tool.
//
// THE VOCABULARY IS THE PIPELINE'S OWN, imported rather than restated: the
// module that emits a node type is the one that names it, and a second copy here
// would be a name that drifts the first time the pipeline gains a type.

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
			logpipe.NodeLogTemplate, logpipe.NodeLogStream, logpipe.NodeLogChunk,
			logpipe.NodeLogLabel, logpipe.NodeProxy,
		},
		EdgeTypes: []string{
			logpipe.EdgeHasLabel, logpipe.EdgeBelongsTo, logpipe.EdgeContains,
			logpipe.EdgeEmittedBy, logpipe.EdgeCorrelatesWith,
		},
		Environment: lokiapi.DescribedEnvironment(),
	}
}
