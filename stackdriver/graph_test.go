// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// graph_test.go — the emitted vocabulary, the whole-batch endpoint invariant,
// and the chunk payload's wire encoding.

// TestEmittedNodeTypesAndMetadataKeys pins the vocabulary a reader queries by.
// A module that emitted the right shapes under different type strings or
// metadata keys would produce a graph that reconciles against nothing.
func TestEmittedNodeTypesAndMetadataKeys(t *testing.T) {
	nodes, _ := graphFor(t, recordedEntries())

	byType := make(map[string][]framework.Node)
	for _, n := range nodes {
		byType[n.Type] = append(byType[n.Type], n)
	}
	for _, want := range []string{nodeTypeLogTemplate, nodeTypeLogStream, nodeTypeLogChunk, nodeTypeLogLabel} {
		if len(byType[want]) == 0 {
			t.Fatalf("no node of type %s was emitted; the types present are %v", want, keysOf(byType))
		}
	}

	requireMetadata(t, byType[nodeTypeLogTemplate][0], "pattern", "severity", "count", "first_seen", "last_seen")
	requireMetadata(t, byType[nodeTypeLogChunk][0], "stream_id", "template_id", "entry_count",
		"start_time", "end_time", "content_encoding")
	requireMetadata(t, byType[nodeTypeLogLabel][0], "label_key", "label_value")
	requireMetadata(t, byType[nodeTypeLogStream][0], "fingerprint")

	stream := byType[nodeTypeLogStream][0]
	if stream.Metadata["label:service"] == "" {
		t.Errorf("the stream node carries no label: entry: %v", stream.Metadata)
	}
}

// TestEmittedEdgeTypesAndDirections pins the three edge types AND the direction
// of CONTAINS, which runs from the template to the chunk. The obvious reading
// points it the other way and produces a graph whose traversals answer the
// opposite question.
func TestEmittedEdgeTypesAndDirections(t *testing.T) {
	nodes, edges := graphFor(t, recordedEntries())
	typeByID := make(map[string]string, len(nodes))
	for _, n := range nodes {
		typeByID[n.ID] = n.Type
	}

	seen := make(map[string]int)
	for _, e := range edges {
		seen[e.Type]++
		switch e.Type {
		case edgeTypeContains:
			assertEndpointTypes(t, typeByID, e, nodeTypeLogTemplate, nodeTypeLogChunk)
		case edgeTypeBelongsTo:
			assertEndpointTypes(t, typeByID, e, nodeTypeLogChunk, nodeTypeLogStream)
		case edgeTypeHasLabel:
			assertEndpointTypes(t, typeByID, e, nodeTypeLogStream, nodeTypeLogLabel)
		}
	}
	for _, want := range []string{edgeTypeContains, edgeTypeBelongsTo, edgeTypeHasLabel} {
		if seen[want] == 0 {
			t.Errorf("no %s edge was emitted; the types present are %v", want, seen)
		}
	}
}

// TestEveryEdgeEndpointNamesANodeInTheSameBatch is the whole-batch invariant,
// asserted over EVERY edge rather than a sample. It is the defect the knowledge
// client's own logs graphs carry: its consolidator leaves entries pointing at a
// dropped template, the CONTAINS edges built from those ids dangle, and the
// client admits a dangling edge silently so nothing downstream reports it.
func TestEveryEdgeEndpointNamesANodeInTheSameBatch(t *testing.T) {
	// The fixture deliberately includes a consolidated Go-panic burst, which is
	// the shape that produces the dangling edges in the pipeline this module
	// reimplements.
	entries := append(recordedEntries(), goPanicEntries()...)
	nodes, edges := graphFor(t, entries)

	present := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		present[n.ID] = struct{}{}
	}
	for _, e := range edges {
		if _, ok := present[e.FromID]; !ok {
			t.Errorf("%s edge's source %q names no node in the batch", e.Type, e.FromID)
		}
		if _, ok := present[e.ToID]; !ok {
			t.Errorf("%s edge's target %q names no node in the batch", e.Type, e.ToID)
		}
	}
	// KNOWN POSITIVE: the assertion ran over a real edge set.
	if len(edges) == 0 {
		t.Fatalf("no edges were emitted, so the invariant above asserted nothing")
	}
}

// TestNoTwoNodesShareAnID is the other half of the batch's integrity: an id that
// names two nodes is a graph nobody can read back.
func TestNoTwoNodesShareAnID(t *testing.T) {
	entries := append(recordedEntries(), goPanicEntries()...)
	nodes, _ := graphFor(t, entries)
	seen := make(map[string]string, len(nodes))
	for _, n := range nodes {
		if prior, dup := seen[n.ID]; dup {
			t.Errorf("id %q names both a %s and a %s", n.ID, prior, n.Type)
		}
		seen[n.ID] = n.Type
	}
	if len(nodes) == 0 {
		t.Fatalf("no nodes were emitted")
	}
}

// TestNoNodeCarriesAnEmptyID is the divergence from the knowledge client's
// merged template, which carries no id at all and so lands under a
// store-generated one that differs between collects.
func TestNoNodeCarriesAnEmptyID(t *testing.T) {
	nodes, _ := graphFor(t, goPanicEntries())
	for _, n := range nodes {
		if n.ID == "" {
			t.Errorf("a %s node was emitted with an empty id: %+v", n.Type, n)
		}
	}
}

// TestChunkContentSurvivesTheJSONEnvelope is the arm that justifies the base64
// encoding, and it carries its own disproof: the RAW compressed bytes are shown
// to be corrupted by the same encoder in the same run. Go's JSON encoder
// replaces every byte sequence that is not valid UTF-8 with the replacement
// character rather than refusing it, so a raw zstd frame in a JSON string does
// not fail — it succeeds and silently destroys the payload.
func TestChunkContentSurvivesTheJSONEnvelope(t *testing.T) {
	nodes, _ := graphFor(t, recordedEntries())
	var chunk framework.Node
	for _, n := range nodes {
		if n.Type == nodeTypeLogChunk {
			chunk = n
			break
		}
	}
	if chunk.ID == "" {
		t.Fatalf("no chunk node was emitted")
	}

	// The encoded content round-trips through JSON and decodes to real entries.
	wire, err := json.Marshal(chunk)
	if err != nil {
		t.Fatalf("marshaling the chunk node: %v", err)
	}
	var back framework.Node
	if err := json.Unmarshal(wire, &back); err != nil {
		t.Fatalf("unmarshalling the chunk node: %v", err)
	}
	compressed, err := base64.StdEncoding.DecodeString(back.Content)
	if err != nil {
		t.Fatalf("the content did not survive as base64: %v", err)
	}
	raw, err := decompressBytes(compressed)
	if err != nil {
		t.Fatalf("decompressing the round-tripped payload: %v", err)
	}
	entries, err := decodeChunkData(raw)
	if err != nil {
		t.Fatalf("decoding the round-tripped payload: %v", err)
	}
	if len(entries) == 0 {
		t.Fatalf("the round-tripped chunk decoded to no entries")
	}

	// THE DISPROOF: the same bytes carried raw do NOT survive.
	rawBytes := compressed
	rawWire, err := json.Marshal(framework.Node{ID: "x", Type: "y", Content: string(rawBytes)})
	if err != nil {
		t.Fatalf("marshaling the raw-content node: %v", err)
	}
	var rawBack framework.Node
	if err := json.Unmarshal(rawWire, &rawBack); err != nil {
		t.Fatalf("unmarshalling the raw-content node: %v", err)
	}
	if bytes.Equal([]byte(rawBack.Content), rawBytes) {
		t.Skipf("this compressed payload happens to be valid UTF-8, so the corruption is not " +
			"demonstrable on this fixture; the encoding is still required for payloads that are not")
	}
	if _, err := decompressBytes([]byte(rawBack.Content)); err == nil {
		t.Errorf("the raw payload survived the JSON round trip, so this test's premise is gone")
	}
	if chunk.Metadata["content_encoding"] != chunkContentEncoding {
		t.Errorf("content_encoding = %q, want %q", chunk.Metadata["content_encoding"], chunkContentEncoding)
	}
}

// TestTemplateSymbolNameFallsBackToTheRawPattern covers the one node whose
// readable name has a fallback, and its control.
func TestTemplateSymbolNameFallsBackToTheRawPattern(t *testing.T) {
	stopwordsOnly := &logTemplate{ID: "id", Pattern: "the of on for to"}
	node := templateNode(stopwordsOnly)
	if node.SymbolName != stopwordsOnly.Pattern {
		t.Errorf("SymbolName = %q, want the raw pattern", node.SymbolName)
	}
	if _, present := node.Metadata["alias"]; present {
		t.Errorf("an alias key was written for a template that derives none: %v", node.Metadata)
	}

	named := &logTemplate{ID: "id", Pattern: "disk pressure detected", Severity: severityWarn}
	named.Alias = templateAliasFor(named)
	if got := templateNode(named); got.SymbolName != "disk-pressure-detected@warn" ||
		got.Metadata["alias"] != "disk-pressure-detected@warn" {
		t.Errorf("a template with an alias emitted SymbolName %q and alias %q",
			got.SymbolName, got.Metadata["alias"])
	}
}

// TestLabelNodesAreSharedAcrossStreams covers the deduplication that makes a
// label node worth having, with the per-stream edge as its control.
func TestLabelNodesAreSharedAcrossStreams(t *testing.T) {
	entries := []logEntry{
		testEntry(0, severityInfo, "m", labels("namespace_name", "prod", "pod_name", "a")),
		testEntry(time.Second, severityInfo, "m", labels("namespace_name", "prod", "pod_name", "b")),
	}
	nodes, edges := graphFor(t, entries)

	shared := 0
	for _, n := range nodes {
		if n.Type == nodeTypeLogLabel && n.ID == labelNodeID("namespace_name", "prod") {
			shared++
		}
	}
	if shared != 1 {
		t.Errorf("the shared label node was emitted %d times, want once", shared)
	}
	edgesToShared := 0
	for _, e := range edges {
		if e.Type == edgeTypeHasLabel && e.ToID == labelNodeID("namespace_name", "prod") {
			edgesToShared++
		}
	}
	if edgesToShared != 2 {
		t.Errorf("%d streams link to the shared label node, want 2", edgesToShared)
	}
}

// TestHighCardinalityLabelsProduceNoLabelNode is the zero, with the
// low-cardinality node in the SAME run as its control.
func TestHighCardinalityLabelsProduceNoLabelNode(t *testing.T) {
	var entries []logEntry
	for i := range 5 {
		entries = append(entries, testEntry(time.Duration(i)*time.Second, severityInfo, "m",
			labels("service", "api", "request_id", string(rune('a'+i)))))
	}
	cfg := defaultPipelineConfig()
	cfg.CardinalityCutoff = 3
	out, err := runPipeline(entries, cfg, nil)
	if err != nil {
		t.Fatalf("running the pipeline: %v", err)
	}
	nodes, _, err := assembleGraph(out.Templates, out.Streams, out.Chunks, out.Resolutions, out.Correlations)
	if err != nil {
		t.Fatalf("assembling: %v", err)
	}

	lowSeen, highSeen := false, false
	for _, n := range nodes {
		if n.Type != nodeTypeLogLabel {
			continue
		}
		switch n.Metadata["label_key"] {
		case "service":
			lowSeen = true
		case "request_id":
			highSeen = true
		}
	}
	if highSeen {
		t.Errorf("a high-cardinality key produced a label node")
	}
	if !lowSeen {
		t.Fatalf("no low-cardinality label node was produced, so the zero above asserts nothing")
	}
}

// TestTheEmittedGraphIsIdenticalAcrossRuns is the reproducibility property every
// derived id exists to serve, asserted over the whole batch.
func TestTheEmittedGraphIsIdenticalAcrossRuns(t *testing.T) {
	entries := append(recordedEntries(), goPanicEntries()...)
	first := renderGraph(t, entries)
	for range 30 {
		if got := renderGraph(t, entries); got != first {
			t.Fatalf("two runs of one fixture produced different graphs")
		}
	}
}

// graphFor runs the real pipeline and assembles the graph.
func graphFor(t *testing.T, entries []logEntry) ([]framework.Node, []framework.Edge) {
	t.Helper()
	out, err := runPipeline(entries, defaultPipelineConfig(), nil)
	if err != nil {
		t.Fatalf("running the pipeline: %v", err)
	}
	nodes, edges, err := assembleGraph(out.Templates, out.Streams, out.Chunks, out.Resolutions, out.Correlations)
	if err != nil {
		t.Fatalf("assembling the graph: %v", err)
	}
	return nodes, edges
}

// renderGraph is the whole emitted batch as one comparable string.
func renderGraph(t *testing.T, entries []logEntry) string {
	t.Helper()
	nodes, edges := graphFor(t, entries)
	b, err := json.Marshal(struct {
		Nodes []framework.Node
		Edges []framework.Edge
	}{nodes, edges})
	if err != nil {
		t.Fatalf("rendering: %v", err)
	}
	return string(b)
}

// requireMetadata asserts a node carries every named key with a non-empty value.
func requireMetadata(t *testing.T, n framework.Node, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if n.Metadata[k] == "" {
			t.Errorf("%s node %q carries no %s: %v", n.Type, n.ID, k, n.Metadata)
		}
	}
}

// assertEndpointTypes asserts an edge runs between the two node types named, in
// that direction.
func assertEndpointTypes(t *testing.T, typeByID map[string]string, e framework.Edge, from, to string) {
	t.Helper()
	if typeByID[e.FromID] != from {
		t.Errorf("%s edge runs FROM a %s, want a %s", e.Type, typeByID[e.FromID], from)
	}
	if typeByID[e.ToID] != to {
		t.Errorf("%s edge runs TO a %s, want a %s", e.Type, typeByID[e.ToID], to)
	}
}

// keysOf renders a map's keys for a failure message.
func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
