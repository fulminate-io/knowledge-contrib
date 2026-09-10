// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/base64"
	"sort"
	"strconv"

	"github.com/fulminate-io/knowledge-contrib/common/correlation"
	"github.com/fulminate-io/knowledge-contrib/framework"
)

// graph.go — the EMITTED VOCABULARY: four node types, three edge types, and the
// metadata keys a reader queries them by.
//
// EVERY ID HERE IS DERIVED, NEVER GENERATED. That is the property the whole
// module is arranged around: a second collect over the same window produces the
// same ids, so it reconciles against the first instead of duplicating it. An id
// that came from a counter, a clock or a map walk would break that silently,
// which is why the derivations live in the files that own each object rather
// than here.

// The node types this collector emits. They are the vocabulary a reader
// searches and traverses by, so they are the strings the built-in log graph
// uses rather than names of this module's choosing.
const (
	nodeTypeLogTemplate = "log-template"
	nodeTypeLogStream   = "log-stream"
	nodeTypeLogChunk    = "log-chunk"
	nodeTypeLogLabel    = "log-label"
	nodeTypeProxy       = "proxy"
)

// The edge types this collector emits.
//
// NOTE THE DIRECTION OF CONTAINS: it runs FROM the template TO the chunk, which
// reads as "this pattern contains these entries". The obvious reading, chunk
// contains entries, would point the other way and produce a graph whose
// traversals answer the opposite question.
const (
	edgeTypeHasLabel       = "HAS_LABEL"
	edgeTypeBelongsTo      = "BELONGS_TO"
	edgeTypeContains       = "CONTAINS"
	edgeTypeEmittedBy      = "EMITTED_BY"
	edgeTypeCorrelatesWith = correlation.EdgeCorrelatesWith
)

// timestampMetaLayout is how a timestamp is written into node metadata. Fixed
// nanosecond width keeps two stamps lexicographically comparable, which is what
// a metadata-range read needs.
const timestampMetaLayout = "2006-01-02T15:04:05.000000000Z07:00"

// chunkContentEncoding names the encoding a chunk's Content carries, and it is
// written into every chunk's metadata so a reader never has to guess.
//
// WHY THE PAYLOAD IS BASE64 AND NOT THE RAW COMPRESSED BYTES. The contract
// envelope is JSON and Content is a JSON string, and Go's encoder replaces every
// byte sequence that is not valid UTF-8 with the Unicode replacement character
// rather than refusing it. A zstd frame is arbitrary bytes, so writing it into
// Content directly does not fail — it SUCCEEDS and silently corrupts the
// payload, and the corruption is only discovered by someone trying to read the
// entries back. Base64 is the only lossless carrier the envelope's field set
// offers. The encoding is recorded here rather than assumed because a reader
// that decompresses without decoding first gets a zstd error, not a hint.
const chunkContentEncoding = "base64+zstd"

// assembleGraph turns the produced objects into the contract's nodes and edges.
//
// The five slices are emitted in a fixed order and each is already sorted by its
// own id, so the result is a function of the collected entries and not of any
// map walk. Correlations and resolutions may both be empty, which is the
// ordinary case for a collect with no foreign-graph context; the shapes below
// then emit nothing rather than erroring.
func assembleGraph(
	templates []*logTemplate,
	streams []*logStream,
	chunks []*logChunk,
	resolutions []resolvedProxyEntry,
	correlations []correlation.Result,
) ([]framework.Node, []framework.Edge, error) {
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

	edges := make([]framework.Edge, 0, len(chunks)*2+len(streams)*2)
	for _, s := range streams {
		edges = append(edges, buildHasLabelEdges(s)...)
	}
	for _, c := range chunks {
		edges = append(edges,
			edgeByID(c.ID, c.StreamID, edgeTypeBelongsTo),
			edgeByID(c.TemplateID, c.ID, edgeTypeContains),
		)
	}

	proxyNodes, proxyEdges, err := materializeProxies(resolutions)
	if err != nil {
		return nil, nil, err
	}
	nodes = append(nodes, proxyNodes...)
	edges = append(edges, proxyEdges...)
	edges = append(edges, correlation.MaterializeCorrelations(correlations)...)

	return nodes, edges, nil
}

// templateNode serializes a template.
//
// THE SymbolName FALLS BACK TO THE RAW PATTERN when the alias derives empty, so
// a template whose pattern is all stopwords is still searchable by its text
// rather than nameless. The alias metadata key is written ONLY when non-empty,
// so an absent key means "derived to nothing" rather than "the deriver did not
// run".
func templateNode(t *logTemplate) framework.Node {
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
		alias = templateAliasFor(t)
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
		Type:       nodeTypeLogTemplate,
		SymbolName: symbol,
		Metadata:   meta,
	}
}

// streamNode serializes a stream. The FULL label set is written out one key at
// a time under a `label:` prefix, so a reader can select streams by any label
// without decoding a packed value.
func streamNode(stream *logStream) framework.Node {
	meta := make(map[string]string, len(stream.Labels)+2)
	for k, v := range stream.Labels {
		meta["label:"+k] = v
	}
	meta["fingerprint"] = stream.Fingerprint
	alias := stream.Alias
	if alias == "" {
		alias = aliasForStream(stream)
	}
	if alias != "" {
		meta["alias"] = alias
	}
	return framework.Node{
		ID:         stream.ID,
		Type:       nodeTypeLogStream,
		SymbolName: alias,
		Metadata:   meta,
	}
}

// chunkNode serializes a chunk, carrying its compressed payload as Content in
// the encoding chunkContentEncoding names.
func chunkNode(c *logChunk) framework.Node {
	meta := map[string]string{
		"stream_id":        c.StreamID,
		"template_id":      c.TemplateID,
		"entry_count":      strconv.Itoa(c.EntryCount),
		"content_encoding": chunkContentEncoding,
	}
	if !c.StartTime.IsZero() {
		meta["start_time"] = c.StartTime.UTC().Format(timestampMetaLayout)
	}
	if !c.EndTime.IsZero() {
		meta["end_time"] = c.EndTime.UTC().Format(timestampMetaLayout)
	}
	return framework.Node{
		ID:       c.ID,
		Type:     nodeTypeLogChunk,
		Content:  base64.StdEncoding.EncodeToString(c.CompressedData),
		Metadata: meta,
	}
}

// buildLabelNodes emits one shared node per unique low-cardinality (key, value)
// pair across every stream, sorted by id.
func buildLabelNodes(streams []*logStream) []framework.Node {
	seen := make(map[string]struct{})
	ids := make([]string, 0)
	byID := make(map[string]framework.Node)
	for _, s := range streams {
		for k, v := range s.LowCardLabels {
			id := labelNodeID(k, v)
			if _, dup := seen[id]; dup {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
			byID[id] = framework.Node{
				ID:         id,
				Type:       nodeTypeLogLabel,
				SymbolName: k + "=" + v,
				Metadata:   map[string]string{"label_key": k, "label_value": v},
			}
		}
	}
	sort.Strings(ids)
	nodes := make([]framework.Node, 0, len(ids))
	for _, id := range ids {
		nodes = append(nodes, byID[id])
	}
	return nodes
}

// buildHasLabelEdges links a stream to each of its shared label nodes, sorted by
// target id so a stream's edges are emitted in a fixed order.
func buildHasLabelEdges(stream *logStream) []framework.Edge {
	targets := make([]string, 0, len(stream.LowCardLabels))
	for k, v := range stream.LowCardLabels {
		targets = append(targets, labelNodeID(k, v))
	}
	sort.Strings(targets)
	edges := make([]framework.Edge, 0, len(targets))
	for _, target := range targets {
		edges = append(edges, edgeByID(stream.ID, target, edgeTypeHasLabel))
	}
	return edges
}

// edgeByID builds an edge between two node ids.
func edgeByID(from, to, edgeType string) framework.Edge {
	return framework.Edge{FromID: from, ToID: to, Type: edgeType}
}
