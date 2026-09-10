// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strconv"

	"github.com/fulminate-io/knowledge-contrib/common/correlation"
	"github.com/fulminate-io/knowledge-contrib/framework"
)

// graph.go — TURNING THE STAGES' OUTPUT INTO THE CONTRACT'S NODES AND EDGES.
//
// Everything above this file is derivation; this file is the vocabulary. The
// type strings, the metadata keys and the edge directions below ARE the parity
// target — a reader of this graph, and every tool over it, keys on them.

// The node types this collector emits.
const (
	nodeLogTemplate = "log-template"
	nodeLogStream   = "log-stream"
	nodeLogChunk    = "log-chunk"
	nodeLogLabel    = "log-label"
	nodeProxy       = "proxy"
)

// The edge types this collector emits.
const (
	edgeHasLabel  = "HAS_LABEL"
	edgeBelongsTo = "BELONGS_TO"
	edgeContains  = "CONTAINS"
	edgeEmittedBy = "EMITTED_BY"
)

// edgeCorrelatesWith is the correlation edge type, taken from the module that
// emits it rather than re-spelled here. A second copy of a wire name is a second
// thing to drift.
const edgeCorrelatesWith = correlation.EdgeCorrelatesWith

// timestampMetaLayout renders every timestamp this collector writes into
// metadata: RFC3339 with nanoseconds, in UTC, FIXED WIDTH so the strings sort
// in the same order as the instants they name.
const timestampMetaLayout = "2006-01-02T15:04:05.000000000Z07:00"

// assembleGraph builds the log half of the graph: the four node types and the
// three edges among them.
//
// The label nodes are derived from the streams rather than passed in, because
// they are shared: one node per distinct low-cardinality pair across the whole
// collect, however many streams carry it.
func assembleGraph(templates []*LogTemplate, streams []*LogStream, chunks []*LogChunk) ([]framework.Node, []framework.Edge) {
	nodes := make([]framework.Node, 0, len(templates)+len(streams)+len(chunks))
	for _, t := range templates {
		nodes = append(nodes, templateNode(t))
	}
	for _, s := range streams {
		nodes = append(nodes, streamNode(s))
	}
	for _, c := range chunks {
		nodes = append(nodes, chunkNode(c))
	}
	nodes = append(nodes, labelNodes(streams)...)

	edges := make([]framework.Edge, 0, len(chunks)*2+len(streams))
	for _, s := range streams {
		edges = append(edges, hasLabelEdges(s)...)
	}
	for _, c := range chunks {
		// The chunk belongs to its stream, and its TEMPLATE contains it. The
		// two edges run in OPPOSITE directions and that is the built-in shape:
		// a reader walks contains from a template down to its chunks, and
		// belongs-to from a chunk up to its stream.
		edges = append(edges,
			framework.Edge{FromID: c.ID, ToID: c.StreamID, Type: edgeBelongsTo},
			framework.Edge{FromID: c.TemplateID, ToID: c.ID, Type: edgeContains},
		)
	}
	return nodes, edges
}

// templateNode renders one template.
//
// THE ALIAS LANDS IN TWO PLACES AND NEITHER IS OPTIONAL: the `alias` metadata
// key and the node's SymbolName, which is what the text index matches. When the
// alias is empty the SymbolName falls back to the PATTERN — so the node stays
// searchable — and the metadata key is OMITTED ENTIRELY rather than written
// empty, which keeps "no alias" distinguishable from "an empty alias".
func templateNode(t *LogTemplate) framework.Node {
	meta := map[string]string{
		"pattern":  t.Pattern,
		"severity": t.Severity,
		"count":    strconv.Itoa(t.Count),
	}
	if !t.FirstSeen.IsZero() {
		meta["first_seen"] = t.FirstSeen.UTC().Format(timestampMetaLayout)
	}
	if !t.LastSeen.IsZero() {
		meta["last_seen"] = t.LastSeen.UTC().Format(timestampMetaLayout)
	}
	alias := t.Alias
	if alias == "" {
		alias = TemplateAliasFor(t)
	}
	if alias != "" {
		meta["alias"] = alias
	}
	symbol := alias
	if symbol == "" {
		symbol = t.Pattern
	}
	return framework.Node{ID: t.ID, Type: nodeLogTemplate, SymbolName: symbol, Metadata: meta}
}

// streamNode renders one stream.
//
// EVERY LABEL RIDES IN METADATA under a "label:" prefix, high-cardinality ones
// included — that is how a reader reconstructs the full label set from the node
// alone, since only the low-cardinality labels get nodes of their own.
//
// Unlike a template, a stream with no alias leaves SymbolName EMPTY rather than
// falling back: there is no second readable form of a label set.
func streamNode(s *LogStream) framework.Node {
	meta := make(map[string]string, len(s.Labels)+2)
	for k, v := range s.Labels {
		meta["label:"+k] = v
	}
	meta["fingerprint"] = s.Fingerprint
	alias := s.Alias
	if alias == "" {
		alias = AliasFor(s)
	}
	if alias != "" {
		meta["alias"] = alias
	}
	return framework.Node{ID: s.ID, Type: nodeLogStream, SymbolName: alias, Metadata: meta}
}

// chunkNode renders one chunk. Its entry text is the node's Content, which is
// what the summarizer and the text index read.
func chunkNode(c *LogChunk) framework.Node {
	meta := map[string]string{
		"stream_id":   c.StreamID,
		"template_id": c.TemplateID,
		"entry_count": strconv.Itoa(c.EntryCount),
	}
	if !c.StartTime.IsZero() {
		meta["start_time"] = c.StartTime.UTC().Format(timestampMetaLayout)
	}
	if !c.EndTime.IsZero() {
		meta["end_time"] = c.EndTime.UTC().Format(timestampMetaLayout)
	}
	return framework.Node{ID: c.ID, Type: nodeLogChunk, Content: c.Content, Metadata: meta}
}

// labelNodes builds one node per distinct low-cardinality pair across every
// stream, in first-observed order. High-cardinality labels get none: they stay
// inline on the stream node.
func labelNodes(streams []*LogStream) []framework.Node {
	seen := make(map[string]struct{})
	var nodes []framework.Node
	for _, s := range streams {
		for _, k := range sortedKeys(s.LowCardLabels) {
			v := s.LowCardLabels[k]
			id := LabelNodeID(k, v)
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			nodes = append(nodes, framework.Node{
				ID:         id,
				Type:       nodeLogLabel,
				SymbolName: k + "=" + v,
				Metadata:   map[string]string{"label_key": k, "label_value": v},
			})
		}
	}
	return nodes
}

// hasLabelEdges links a stream to each of its low-cardinality label nodes.
func hasLabelEdges(s *LogStream) []framework.Edge {
	edges := make([]framework.Edge, 0, len(s.LowCardLabels))
	for _, k := range sortedKeys(s.LowCardLabels) {
		edges = append(edges, framework.Edge{
			FromID: s.ID,
			ToID:   LabelNodeID(k, s.LowCardLabels[k]),
			Type:   edgeHasLabel,
		})
	}
	return edges
}
