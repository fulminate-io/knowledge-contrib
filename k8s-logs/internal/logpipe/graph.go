// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"sort"
	"strconv"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// graph.go — the node and edge vocabulary the collect result carries.
//
// THE SEVEN TYPE STRINGS ARE LITERALS HERE AND THERE IS NO COMPILER BEHIND
// THEM. The client's own vocabulary lives in a package this module cannot
// import, so a typo in any of the seven produces a graph that validates, writes
// and reads as an unrelated shape. That is what the vocabulary test pins.
//
// TWO OF THE THREE EDGE DIRECTIONS RUN BACKWARDS FROM THE NODE ORDER a reader
// would guess, and they are transcribed rather than reasoned:
//
//	chunk    --BELONGS_TO--> stream
//	TEMPLATE --CONTAINS-->   chunk     (the template owns the chunk, not the reverse)
//	stream   --HAS_LABEL-->  label

// The node types.
const (
	NodeLogTemplate = "log-template"
	NodeLogStream   = "log-stream"
	NodeLogChunk    = "log-chunk"
	NodeLogLabel    = "log-label"
	NodeProxy       = "proxy"
)

// The edge types.
const (
	EdgeContains       = "CONTAINS"
	EdgeBelongsTo      = "BELONGS_TO"
	EdgeHasLabel       = "HAS_LABEL"
	EdgeEmittedBy      = "EMITTED_BY"
	EdgeCorrelatesWith = "CORRELATES_WITH"
)

// timestampMetaLayout is how a timestamp is stringified in node metadata.
// Fixed-width fractional seconds keep two nodes' stamps lexically comparable,
// which a plain RFC3339Nano does not: it trims trailing zeros.
const timestampMetaLayout = "2006-01-02T15:04:05.000000000Z07:00"

// AssembleGraph turns the pipeline's products into the contract's nodes and
// edges. Every node carries a deterministic id, so every edge references its
// endpoints by id and nothing here depends on slice position.
func AssembleGraph(templates []*Template, streams []*Stream, chunks []*Chunk) ([]framework.Node, []framework.Edge) {
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

	labelNodes := buildLabelNodes(streams)
	nodes = append(nodes, labelNodes...)

	edges := make([]framework.Edge, 0, len(chunks)*2+len(streams))
	for _, s := range streams {
		edges = append(edges, hasLabelEdges(s)...)
	}
	for _, c := range chunks {
		edges = append(edges,
			framework.Edge{FromID: c.ID, ToID: c.StreamID, Type: EdgeBelongsTo},
			framework.Edge{FromID: c.TemplateID, ToID: c.ID, Type: EdgeContains},
		)
	}
	return nodes, edges
}

// templateNode renders a template. SymbolName is the alias so text search
// matches the readable form; it falls back to the PATTERN rather than staying
// empty, because a node with no symbol name is invisible to a reader.
func templateNode(t *Template) framework.Node {
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
	return framework.Node{
		ID:         t.ID,
		Type:       NodeLogTemplate,
		SymbolName: symbol,
		Metadata:   meta,
	}
}

// streamNode renders a stream. The COMPLETE label set rides in metadata under
// "label:<key>" — including the high-cardinality half, which becomes no node —
// so a reader can reconstruct the stream's identity from the node alone.
func streamNode(s *Stream) framework.Node {
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
	return framework.Node{
		ID:         s.ID,
		Type:       NodeLogStream,
		SymbolName: alias,
		Metadata:   meta,
	}
}

// chunkNode renders a chunk. Content is the COMPRESSED payload carried as a
// string, which is why the registration excludes `content` from the graph-wide
// indexed fields: indexing it would embed and summarize compressed bytes.
func chunkNode(c *Chunk) framework.Node {
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
	return framework.Node{
		ID:       c.ID,
		Type:     NodeLogChunk,
		Content:  string(c.CompressedData),
		Metadata: meta,
	}
}

// buildLabelNodes emits one node per distinct low-cardinality (key, value)
// across every stream. Emitted in sorted id order so two runs agree.
func buildLabelNodes(streams []*Stream) []framework.Node {
	seen := make(map[string]struct{})
	ids := make([]string, 0)
	byID := make(map[string][2]string)
	for _, s := range streams {
		for k, v := range s.LowCardLabels {
			id := LabelNodeID(k, v)
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
			byID[id] = [2]string{k, v}
		}
	}
	sort.Strings(ids)
	nodes := make([]framework.Node, 0, len(ids))
	for _, id := range ids {
		kv := byID[id]
		nodes = append(nodes, framework.Node{
			ID:         id,
			Type:       NodeLogLabel,
			SymbolName: kv[0] + "=" + kv[1],
			Metadata:   map[string]string{"label_key": kv[0], "label_value": kv[1]},
		})
	}
	return nodes
}

// hasLabelEdges joins a stream to its low-cardinality label nodes, in sorted
// key order.
func hasLabelEdges(s *Stream) []framework.Edge {
	keys := make([]string, 0, len(s.LowCardLabels))
	for k := range s.LowCardLabels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	edges := make([]framework.Edge, 0, len(keys))
	for _, k := range keys {
		edges = append(edges, framework.Edge{
			FromID: s.ID,
			ToID:   LabelNodeID(k, s.LowCardLabels[k]),
			Type:   EdgeHasLabel,
		})
	}
	return edges
}
