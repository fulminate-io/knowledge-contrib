// SPDX-License-Identifier: Apache-2.0

package logpipe

import "github.com/fulminate-io/knowledge-contrib/common/correlation"

// vocabulary.go — the node and edge type strings this collector emits.
//
// THESE ARE WIRE VALUES, NOT NAMES. The knowledge client validates a collect
// result's SHAPE and never its vocabulary, so a collector that spelled any
// string below differently would produce a graph that collects, stores and
// reads back, and disagrees with every other log graph in the product. They are
// declared here, once, so a reader can check the whole vocabulary in one place
// and so a test can assert them literally.

// The node types. Four are the unconditional floor of a log graph; the fifth is
// emitted only when the collect input supplies cloud resolutions.
const (
	NodeLogTemplate = "log-template"
	NodeLogStream   = "log-stream"
	NodeLogChunk    = "log-chunk"
	NodeLogLabel    = "log-label"
	NodeProxy       = "proxy"
)

// The edge types. Three are the unconditional floor; two are emitted only from
// supplied resolutions and confirmed correlations.
const (
	EdgeHasLabel  = "HAS_LABEL"  // stream -> label
	EdgeBelongsTo = "BELONGS_TO" // chunk -> stream
	EdgeContains  = "CONTAINS"   // template -> chunk
	EdgeEmittedBy = "EMITTED_BY" // label -> cloud proxy
)

// EdgeCorrelatesWith is the common correlation module's own, not a second
// spelling of it. The module that produces a correlation renders its edge, so
// the type name is read from there rather than restated here where it could
// drift.
const EdgeCorrelatesWith = correlation.EdgeCorrelatesWith

// timestampMetaLayout is how every timestamp is rendered into node metadata:
// RFC 3339 with NINE fractional digits, applied after normalising to UTC. A
// shorter layout truncates silently, which is why a sub-millisecond timestamp
// is one of the cells this layout is tested on.
const timestampMetaLayout = "2006-01-02T15:04:05.000000000Z07:00"

// proxyForeignGraph names the graph a proxy points into.
const proxyForeignGraph = "cloud"
