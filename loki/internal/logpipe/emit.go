// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"fmt"
	"sort"
	"strconv"

	"github.com/fulminate-io/knowledge-contrib/common/correlation"
	"github.com/fulminate-io/knowledge-contrib/framework"
)

// emit.go — the graph the walk returns, in the framework's node and edge shape.
//
// EVERY DERIVED VALUE BELOW IS AN UNCOMPILED CONTRACT. The client validates the
// result's shape — id and type present on a node, endpoints and type present on
// an edge — and never its vocabulary, so a chunk id built from a different
// preimage, a timestamp in a different layout, an alias from the wrong deriver
// or a metadata key spelled differently all produce a graph that collects,
// stores and reads back, and is wrong. That is why this package's tests assert
// the emitted values literally.

// Emit turns one walk's graph, plus whatever cloud resolutions and confirmed
// correlations the caller supplied, into the nodes and edges the collect
// returns.
//
// resolutions and correlations may be empty, and a collect whose entry declares
// no cloud family produces both empty. That zero is PARITY-CORRECT rather than a
// gap: the built-in pipeline emits none of the three families with no cloud
// graph attached either.
func Emit(g *Graph, resolutions []Resolution, correlations []correlation.Result) ([]framework.Node, []framework.Edge, error) {
	if g == nil {
		return nil, nil, fmt.Errorf("logpipe: Emit: nil graph")
	}
	nodes := make([]framework.Node, 0, len(g.Templates)+len(g.Streams)+len(g.Chunks))
	for _, t := range g.Templates {
		nodes = append(nodes, templateNode(t))
	}
	for _, s := range g.Streams {
		nodes = append(nodes, streamNode(s))
	}
	for _, c := range g.Chunks {
		nodes = append(nodes, chunkNode(c))
	}

	labelNodes := labelNodesFor(g.Streams)
	nodes = append(nodes, labelNodes...)

	edges := make([]framework.Edge, 0, len(g.Chunks)*2+len(g.Streams))
	for _, s := range g.Streams {
		edges = append(edges, hasLabelEdges(s)...)
	}
	for _, c := range g.Chunks {
		edges = append(edges,
			framework.Edge{FromID: c.ID, ToID: c.StreamID, Type: EdgeBelongsTo},
			framework.Edge{FromID: c.TemplateID, ToID: c.ID, Type: EdgeContains},
		)
	}

	proxyNodes, proxyEdges, err := emitProxies(resolutions)
	if err != nil {
		return nil, nil, err
	}
	nodes = append(nodes, proxyNodes...)
	edges = append(edges, proxyEdges...)
	// THE CORRELATION EDGE IS RENDERED BY THE MODULE THAT PRODUCED IT. Its
	// confidence, its method literal and its evidence string are one contract
	// with every consumer of a log graph, and this collector held a copy of it
	// until the common module existed.
	edges = append(edges, correlation.MaterializeCorrelations(correlations)...)

	return nodes, edges, nil
}

// templateNode carries the pattern, severity and count unconditionally and each
// timestamp only when it is set. SymbolName is the alias, falling back to the
// RAW PATTERN when the alias derives to nothing — a template whose pattern is
// all wildcards or all stopwords has no alias, and a node with no SymbolName
// would be unsearchable.
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
	return framework.Node{ID: t.ID, Type: NodeLogTemplate, SymbolName: symbol, Metadata: meta}
}

// streamNode carries the FULL label set as one "label:<key>" entry each, so a
// reader reconstructs the stream from its node, plus the fingerprint over the
// low-cardinality subset.
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
	return framework.Node{ID: s.ID, Type: NodeLogStream, SymbolName: alias, Metadata: meta}
}

// chunkNode carries the zstd frame as Content verbatim and nothing else on the
// typed fields. It has no alias and no SymbolName: a chunk is addressed by its
// derived id and read through its template.
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
	return framework.Node{ID: c.ID, Type: NodeLogChunk, Content: string(c.Data), Metadata: meta}
}

// labelNodesFor builds one shared node per distinct low-cardinality
// (key, value) across every stream. The dedup is what makes the label a SHARED
// node: two streams carrying namespace=prod point at one node, which is the
// whole reason low-cardinality labels are hoisted out.
//
// The result is sorted by id so two runs over the same streams emit the same
// order; the set is built from Go maps, whose iteration order is randomized.
func labelNodesFor(streams []*Stream) []framework.Node {
	seen := make(map[string]struct{})
	nodes := make([]framework.Node, 0)
	for _, s := range streams {
		for k, v := range s.LowCardLabels {
			id := LabelNodeID(k, v)
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			nodes = append(nodes, framework.Node{
				ID:         id,
				Type:       NodeLogLabel,
				SymbolName: k + "=" + v,
				Metadata:   map[string]string{"label_key": k, "label_value": v},
			})
		}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	return nodes
}

// hasLabelEdges joins a stream to each of its low-cardinality label nodes,
// sorted by target id for the same determinism reason.
func hasLabelEdges(s *Stream) []framework.Edge {
	edges := make([]framework.Edge, 0, len(s.LowCardLabels))
	for k, v := range s.LowCardLabels {
		edges = append(edges, framework.Edge{FromID: s.ID, ToID: LabelNodeID(k, v), Type: EdgeHasLabel})
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].ToID < edges[j].ToID })
	return edges
}
