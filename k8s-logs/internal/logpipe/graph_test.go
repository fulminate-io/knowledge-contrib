// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"sort"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// graph_test.go — the seven type strings, the three edge DIRECTIONS, and the
// per-type metadata key sets.
//
// A declaration table proves its rows agree with each other and nothing more,
// so each block below is paired with an assertion over a graph the pipeline
// actually built: the vocabulary is checked against what AssembleGraph emitted,
// not against a second copy of the constants.

// TestVocabularyLiterals pins the exact strings. Nothing in this module
// compiles against the client's own vocabulary, so a typo here produces a graph
// that validates and reads as an unrelated shape.
func TestVocabularyLiterals(t *testing.T) {
	for name, got := range map[string]string{
		"node template": NodeLogTemplate,
		"node stream":   NodeLogStream,
		"node chunk":    NodeLogChunk,
		"node label":    NodeLogLabel,
		"node proxy":    NodeProxy,
		"edge contains": EdgeContains,
		"edge belongs":  EdgeBelongsTo,
		"edge label":    EdgeHasLabel,
		"edge emitted":  EdgeEmittedBy,
		"edge correl":   EdgeCorrelatesWith,
	} {
		want := map[string]string{
			"node template": "log-template",
			"node stream":   "log-stream",
			"node chunk":    "log-chunk",
			"node label":    "log-label",
			"node proxy":    "proxy",
			"edge contains": "CONTAINS",
			"edge belongs":  "BELONGS_TO",
			"edge label":    "HAS_LABEL",
			"edge emitted":  "EMITTED_BY",
			"edge correl":   "CORRELATES_WITH",
		}[name]
		if got != want {
			t.Errorf("%s is %q, want %q", name, got, want)
		}
	}
}

// buildFixtureGraph is the shared fixture: two pods of one container in one
// namespace, two templates, spanning one window.
func buildFixtureGraph(t *testing.T) Result {
	t.Helper()
	base := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	labelsA := map[string]string{"container": "api", "container_pod": "api-1", "namespace": "dev"}
	labelsB := map[string]string{"container": "api", "container_pod": "api-2", "namespace": "dev"}
	entries := []Entry{
		{Timestamp: base, Message: "connection to database failed for alpha", Labels: labelsA},
		{Timestamp: base.Add(time.Second), Message: "connection to database failed for beta", Labels: labelsA},
		{Timestamp: base.Add(2 * time.Second), Message: "request served in good time", Labels: labelsB},
	}
	res, err := Build(entries, Options{ChunkWindow: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// TestEmittedGraphUsesOnlyTheDeclaredVocabulary is the pairing that stops the
// table above from being a self-check.
func TestEmittedGraphUsesOnlyTheDeclaredVocabulary(t *testing.T) {
	res := buildFixtureGraph(t)

	nodeTypes := map[string]bool{}
	for _, n := range res.Nodes {
		nodeTypes[n.Type] = true
	}
	for _, want := range []string{NodeLogTemplate, NodeLogStream, NodeLogChunk, NodeLogLabel} {
		if !nodeTypes[want] {
			t.Errorf("the emitted graph carries no %q node; the four are the no-cloud parity floor", want)
		}
		delete(nodeTypes, want)
	}
	if len(nodeTypes) != 0 {
		t.Errorf("the emitted graph carries node types outside the vocabulary: %v", nodeTypes)
	}

	edgeTypes := map[string]bool{}
	for _, e := range res.Edges {
		edgeTypes[e.Type] = true
	}
	for _, want := range []string{EdgeContains, EdgeBelongsTo, EdgeHasLabel} {
		if !edgeTypes[want] {
			t.Errorf("the emitted graph carries no %q edge", want)
		}
		delete(edgeTypes, want)
	}
	if len(edgeTypes) != 0 {
		t.Errorf("the emitted graph carries edge types outside the no-cloud floor: %v", edgeTypes)
	}
}

// TestEdgeDirections pins the two that run backwards from the node order a
// reader would guess.
func TestEdgeDirections(t *testing.T) {
	res := buildFixtureGraph(t)
	byType := map[string][]framework.Edge{}
	for _, e := range res.Edges {
		byType[e.Type] = append(byType[e.Type], e)
	}
	typeOf := map[string]string{}
	for _, n := range res.Nodes {
		typeOf[n.ID] = n.Type
	}

	for _, tc := range []struct{ edge, from, to string }{
		{EdgeBelongsTo, NodeLogChunk, NodeLogStream},
		{EdgeContains, NodeLogTemplate, NodeLogChunk},
		{EdgeHasLabel, NodeLogStream, NodeLogLabel},
	} {
		edges := byType[tc.edge]
		if len(edges) == 0 {
			t.Fatalf("no %s edge was emitted", tc.edge)
		}
		for _, e := range edges {
			if typeOf[e.FromID] != tc.from || typeOf[e.ToID] != tc.to {
				t.Fatalf("%s runs %s -> %s; it runs %s -> %s",
					tc.edge, typeOf[e.FromID], typeOf[e.ToID], tc.from, tc.to)
			}
		}
	}
}

// TestNodeMetadataKeySets pins what each node type carries.
func TestNodeMetadataKeySets(t *testing.T) {
	res := buildFixtureGraph(t)
	for _, tc := range []struct {
		nodeType string
		required []string
	}{
		{NodeLogTemplate, []string{"pattern", "severity", "count", "first_seen", "last_seen", "alias"}},
		{NodeLogStream, []string{"fingerprint", "alias", "label:container", "label:container_pod", "label:namespace"}},
		{NodeLogChunk, []string{"stream_id", "template_id", "entry_count", "start_time", "end_time"}},
		{NodeLogLabel, []string{"label_key", "label_value"}},
	} {
		node, ok := firstNodeOfType(res.Nodes, tc.nodeType)
		if !ok {
			t.Fatalf("no %s node was emitted", tc.nodeType)
		}
		for _, key := range tc.required {
			if _, present := node.Metadata[key]; !present {
				t.Errorf("%s node %s carries no %q metadata; keys present: %v",
					tc.nodeType, node.ID, key, sortedKeys(node.Metadata))
			}
		}
	}
}

// TestChunkContentIsTheCompressedPayload is the fact the registration's field
// lists depend on: a chunk's Content is not text.
func TestChunkContentIsTheCompressedPayload(t *testing.T) {
	res := buildFixtureGraph(t)
	node, ok := firstNodeOfType(res.Nodes, NodeLogChunk)
	if !ok {
		t.Fatal("no chunk node was emitted")
	}
	if node.Content == "" {
		t.Fatal("the chunk node carries no content; the compressed payload is what it stores")
	}
	if node.SymbolName != "" {
		t.Fatalf("the chunk node carries the symbol name %q; a chunk has no readable name", node.SymbolName)
	}
	for _, c := range res.Chunks {
		if c.ID == node.ID && string(c.CompressedData) != node.Content {
			t.Fatal("the chunk node's content is not the chunk's compressed payload")
		}
	}
}

// TestStreamNodeCarriesEveryLabelIncludingTheHighCardinalityHalf — the metadata
// is what lets a reader reconstruct a stream's identity from the node alone.
func TestStreamNodeCarriesEveryLabelIncludingTheHighCardinalityHalf(t *testing.T) {
	tracker := NewCardinalityTracker(2)
	for _, pod := range []string{"api-1", "api-2", "api-3"} {
		tracker.Observe("container_pod", pod)
		tracker.Observe("container", "api")
	}
	s := NewStream(map[string]string{"container": "api", "container_pod": "api-1"}, tracker)
	node := streamNode(s)
	if got := node.Metadata["label:container_pod"]; got != "api-1" {
		t.Fatalf("the stream node dropped its high-cardinality label; label:container_pod = %q", got)
	}
	if got := node.Metadata["label:container"]; got != "api" {
		t.Fatalf("the stream node dropped its low-cardinality label; label:container = %q", got)
	}
}

func firstNodeOfType(nodes []framework.Node, nodeType string) (framework.Node, bool) {
	for _, n := range nodes {
		if n.Type == nodeType {
			return n, true
		}
	}
	return framework.Node{}, false
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
