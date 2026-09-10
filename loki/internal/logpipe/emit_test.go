// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"strconv"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/framework"
)

// emit_test.go — THE PARITY ROW, plus the metadata layout.
//
// The parity assertion is node by node, over the derived IDS, the metadata keys
// AND VALUES, and the SymbolName on the two node types that carry an alias. It
// is not a type census: a test that counted log-chunk nodes would pass on a
// chunk id built from the wrong preimage, and one that ignored SymbolName would
// pass on an alias from the wrong dispatch arm or the wrong deriver.
//
// THE FIXTURE'S SHAPE IS PART OF THE ROW. It carries one stream per
// inferProvider arm AND one COLLAPSED stream per arm, because the four
// foreign-arm mutations claim a red this suite cannot produce otherwise: a K8s
// stream carrying both `reason` and `pod_name` takes the same path under the
// real dispatch and under an unconditional two-component join.

// parityFixture is the entry set every parity assertion runs over.
func parityFixture() []Entry {
	base := time.Date(2026, 9, 7, 12, 4, 0, 0, time.UTC)
	// TWO OF THE SETS SHARE `namespace=prod`, which is what exercises the label
	// node's dedup: a label is a SHARED node, so two streams carrying one pair
	// must point at one node. Without a shared pair the dedup is unreachable
	// and every assertion about it passes vacuously. The key is not a dispatch
	// discriminator, so no arm's alias moves.
	labelSets := []map[string]string{
		// One per dispatch arm, populated.
		{"reason": "OOMKilled", "pod_name": "api-7b6", "namespace": "prod"},
		{"log_stream": "ecs-task", "service": "api-server"},
		{"resource_type": "k8s_container", "host": "gce-3"},
		{"app": "checkout", "instance": "host-3", "namespace": "prod"},
		{"job": "j1", "zone": "z1"},
		// One COLLAPSED per arm: the second component is absent.
		{"reason": "Evicted"},
		{"log_group": "/aws/lambda/fn"},
		{"log_stream": "solo-stream"},
		{"resource_type": "gce_instance"},
		{"app": "payments"},
	}
	entries := make([]Entry, 0, len(labelSets)+1)
	for i, labels := range labelSets {
		entries = append(entries, Entry{
			Timestamp: base.Add(time.Duration(i) * time.Second),
			Severity:  SeverityWarn,
			Message:   "disk pressure detected",
			Labels:    labels,
		})
	}
	// A template whose pattern exercises the reason-prefix strip, so the
	// template alias rules are on the parity surface too.
	entries = append(entries, Entry{
		Timestamp: base.Add(time.Minute),
		Severity:  SeverityError,
		Message:   "NodeNotReady: kubelet stopped posting status",
		Labels:    map[string]string{"app": "checkout", "instance": "host-3", "namespace": "prod"},
	})
	return entries
}

func emitParity(t *testing.T) ([]framework.Node, []framework.Edge, *Graph) {
	t.Helper()
	entries := parityFixture()
	graph, err := Build(entries, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	nodes, edges, err := Emit(graph, nil, nil)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	return nodes, edges, graph
}

func nodesByType(nodes []framework.Node, want string) map[string]framework.Node {
	out := make(map[string]framework.Node)
	for _, n := range nodes {
		if n.Type == want {
			out[n.ID] = n
		}
	}
	return out
}

// TestEmittedGraphCarriesTheFourNodeTypesAndThreeEdgeTypes is the vocabulary
// floor. It is the weakest assertion in this file and is here so a missing
// FAMILY is a one-line failure rather than a puzzling per-node one.
func TestEmittedGraphCarriesTheFourNodeTypesAndThreeEdgeTypes(t *testing.T) {
	nodes, edges, _ := emitParity(t)
	for _, want := range []string{NodeLogTemplate, NodeLogStream, NodeLogChunk, NodeLogLabel} {
		if len(nodesByType(nodes, want)) == 0 {
			t.Fatalf("no %s node was emitted", want)
		}
	}
	seen := map[string]int{}
	for _, e := range edges {
		seen[e.Type]++
	}
	for _, want := range []string{EdgeHasLabel, EdgeBelongsTo, EdgeContains} {
		if seen[want] == 0 {
			t.Fatalf("no %s edge was emitted; edges seen: %v", want, seen)
		}
	}
	// The cloud families are NOT emitted with nothing resolved.
	if n := countByType(nodes, NodeProxy); n != 0 {
		t.Fatalf("%d proxy nodes were emitted from an unresolved collect", n)
	}
	for _, unwanted := range []string{EdgeEmittedBy, EdgeCorrelatesWith} {
		if seen[unwanted] != 0 {
			t.Fatalf("%d %s edges were emitted from an unresolved collect", seen[unwanted], unwanted)
		}
	}
}

// TestEveryEmittedNodeCarriesItsDerivedIDAndMetadata is the parity row proper.
func TestEveryEmittedNodeCarriesItsDerivedIDAndMetadata(t *testing.T) {
	nodes, _, graph := emitParity(t)

	t.Run("templates", func(t *testing.T) {
		got := nodesByType(nodes, NodeLogTemplate)
		if len(got) != len(graph.Templates) {
			t.Fatalf("emitted %d template nodes for %d templates", len(got), len(graph.Templates))
		}
		for _, tmpl := range graph.Templates {
			n, ok := got[tmpl.ID]
			if !ok {
				t.Fatalf("no node for template %q (%q)", tmpl.ID, tmpl.Pattern)
			}
			// The id is hashed HERE from the node's own pattern metadata, by
			// the documented rule, rather than read back off the Template.
			if want := expectedTemplateID(t, n.Metadata["pattern"]); n.ID != want {
				t.Fatalf("template node id %q is not sha256 of its own pattern metadata %q (%q)",
					n.ID, n.Metadata["pattern"], want)
			}
			if n.ID != TemplateID(tmpl.Pattern) {
				t.Fatalf("template node id %q is not the id of its pattern %q", n.ID, tmpl.Pattern)
			}
			assertMeta(t, n, "pattern", tmpl.Pattern)
			assertMeta(t, n, "severity", tmpl.Severity)
			assertMeta(t, n, "count", strconv.Itoa(tmpl.Count))
			assertMeta(t, n, "first_seen", tmpl.FirstSeen.UTC().Format(timestampMetaLayout))
			assertMeta(t, n, "last_seen", tmpl.LastSeen.UTC().Format(timestampMetaLayout))

			// SymbolName comes from the TEMPLATE deriver, and this is the
			// assertion that fails when the stream rules are used here.
			wantAlias := TemplateAliasFor(tmpl)
			wantSymbol := wantAlias
			if wantSymbol == "" {
				wantSymbol = tmpl.Pattern
			}
			if n.SymbolName != wantSymbol {
				t.Fatalf("template %q SymbolName = %q, want %q", tmpl.ID, n.SymbolName, wantSymbol)
			}
			if wantAlias != "" {
				assertMeta(t, n, "alias", wantAlias)
			} else if _, ok := n.Metadata["alias"]; ok {
				t.Fatalf("template %q carries an alias key with no alias", tmpl.ID)
			}
		}
	})

	t.Run("streams", func(t *testing.T) {
		got := nodesByType(nodes, NodeLogStream)
		if len(got) != len(graph.Streams) {
			t.Fatalf("emitted %d stream nodes for %d streams", len(got), len(graph.Streams))
		}
		for _, s := range graph.Streams {
			n, ok := got[s.ID]
			if !ok {
				t.Fatalf("no node for stream %q", s.ID)
			}
			// THE EXPECTATION COMES FROM THE FIXTURE'S OWN LABEL SETS, not
			// from the Stream struct: a producer that hashed the wrong set
			// would otherwise be compared against the set it hashed.
			fromFixture := ""
			for _, e := range parityFixture() {
				if id := expectedStreamID(t, e.Labels); id == n.ID {
					fromFixture = id
					break
				}
			}
			if fromFixture == "" {
				t.Fatalf("stream node id %q matches no fixture label set hashed by the documented rule", n.ID)
			}
			if n.ID != FingerprintLabels(s.Labels) {
				t.Fatalf("stream node id %q is not the hash of its full label set", n.ID)
			}
			assertMeta(t, n, "fingerprint", FingerprintLabels(s.LowCardLabels))
			for k, v := range s.Labels {
				assertMeta(t, n, "label:"+k, v)
			}
			// SymbolName comes from the STREAM deriver.
			if want := AliasFor(s); n.SymbolName != want {
				t.Fatalf("stream %q SymbolName = %q, want %q", s.ID, n.SymbolName, want)
			}
		}
	})

	t.Run("chunks", func(t *testing.T) {
		got := nodesByType(nodes, NodeLogChunk)
		if len(got) != len(graph.Chunks) {
			t.Fatalf("emitted %d chunk nodes for %d chunks", len(got), len(graph.Chunks))
		}

		// EVERY TIMESTAMP THE FIXTURE CONTAINS, so a chunk's bounds can be
		// checked against the entries rather than against the Chunk struct that
		// produced them.
		fixtureTimes := map[string]bool{}
		for _, e := range parityFixture() {
			fixtureTimes[e.Timestamp.UTC().Format(timestampMetaLayout)] = true
		}

		for _, c := range graph.Chunks {
			n, ok := got[c.ID]
			if !ok {
				t.Fatalf("no node for chunk %q", c.ID)
			}
			assertMeta(t, n, "stream_id", c.StreamID)
			assertMeta(t, n, "template_id", c.TemplateID)
			assertMeta(t, n, "entry_count", strconv.Itoa(c.EntryCount))
			if n.Content != string(c.Data) {
				t.Fatalf("chunk %q Content is not the chunk body", c.ID)
			}
			if n.SymbolName != "" {
				t.Fatalf("chunk %q carries SymbolName %q; a chunk has no alias", c.ID, n.SymbolName)
			}

			// THE ID IS RECOMPUTED FROM THE NODE'S OWN METADATA, not read off
			// the Chunk struct: the endpoints come from the emitted node and
			// the WINDOW is floored here, in the test. An id built from the
			// bucket's earliest entry rather than the floored window disagrees
			// with this, which looking the node up by c.ID never could.
			start, err := time.Parse(timestampMetaLayout, n.Metadata["start_time"])
			if err != nil {
				t.Fatalf("chunk %q start_time %q does not parse: %v", c.ID, n.Metadata["start_time"], err)
			}
			want := ChunkID(n.Metadata["stream_id"], n.Metadata["template_id"], windowStart(start, DefaultChunkWindow))
			if n.ID != want {
				t.Fatalf("chunk id %q is not the id of (stream, template, the epoch-floored window of its own start_time) %q",
					n.ID, want)
			}

			// AND ITS BOUNDS ARE TIMESTAMPS THE FIXTURE ACTUALLY CARRIES, in
			// order, inside the bucket the id names.
			end, err := time.Parse(timestampMetaLayout, n.Metadata["end_time"])
			if err != nil {
				t.Fatalf("chunk %q end_time %q does not parse: %v", c.ID, n.Metadata["end_time"], err)
			}
			if !fixtureTimes[n.Metadata["start_time"]] {
				t.Fatalf("chunk %q start_time %q is not a timestamp any fixture entry carries", c.ID, n.Metadata["start_time"])
			}
			if !fixtureTimes[n.Metadata["end_time"]] {
				t.Fatalf("chunk %q end_time %q is not a timestamp any fixture entry carries", c.ID, n.Metadata["end_time"])
			}
			if end.Before(start) {
				t.Fatalf("chunk %q ends at %s before it starts at %s", c.ID, end, start)
			}
			if w := windowStart(start, DefaultChunkWindow); end.Sub(w) >= DefaultChunkWindow {
				t.Fatalf("chunk %q ends at %s, outside the %s bucket its id names", c.ID, end, w)
			}
		}
	})

	t.Run("labels", func(t *testing.T) {
		got := nodesByType(nodes, NodeLogLabel)
		want := map[string]string{}
		for _, s := range graph.Streams {
			for k, v := range s.LowCardLabels {
				want[LabelNodeID(k, v)] = k + "=" + v
			}
		}
		// COUNTED FROM THE SLICE. A label is a SHARED node — two streams
		// carrying namespace=prod point at one — and two nodes under one id
		// collapse to a single entry in an id-keyed map, so a map-based count
		// reads a duplicate as a dedup and this row would pass on the very
		// defect it exists to catch.
		if n := countByType(nodes, NodeLogLabel); n != len(want) {
			t.Fatalf("emitted %d label nodes for %d distinct low-cardinality labels", n, len(want))
		}
		if len(got) != len(want) {
			t.Fatalf("emitted %d distinct label node ids for %d distinct labels", len(got), len(want))
		}
		// THE DEDUP'S OWN KNOWN POSITIVE: the fixture really does carry a label
		// two streams share, so "one node per distinct label" is an observation
		// rather than a restatement of "every label appeared once".
		shared := 0
		for _, s := range graph.Streams {
			if s.LowCardLabels["namespace"] == "prod" {
				shared++
			}
		}
		if shared < 2 {
			t.Fatalf("%d streams carry namespace=prod; the fixture no longer exercises the label dedup", shared)
		}
		for id, symbol := range want {
			n, ok := got[id]
			if !ok {
				t.Fatalf("no node for label %q", id)
			}
			if n.SymbolName != symbol {
				t.Fatalf("label %q SymbolName = %q, want %q", id, n.SymbolName, symbol)
			}
			if n.Metadata["label_key"]+"="+n.Metadata["label_value"] != symbol {
				t.Fatalf("label %q metadata %v does not reconstruct %q", id, n.Metadata, symbol)
			}
		}
	})
}

// TestParityFixtureReachesEveryDispatchArmAndEveryCollapse is the fixture's own
// known positive. Without it, a fixture that quietly stopped covering the
// foreign arms would leave every SymbolName assertion above passing over a
// surface that cannot tell a correct dispatch from a hardcoded one.
func TestParityFixtureReachesEveryDispatchArmAndEveryCollapse(t *testing.T) {
	nodes, _, _ := emitParity(t)
	symbols := map[string]bool{}
	for _, n := range nodesByType(nodes, NodeLogStream) {
		symbols[n.SymbolName] = true
	}
	for _, want := range []string{
		"api-7b6.OOMKilled",   // k8s, populated
		"ecs-task.api-server", // cloudwatch, populated
		"gce-3.k8s_container", // stackdriver, populated
		"checkout@host-3",     // loki, populated
		"job=j1.zone=z1",      // generic
		"Evicted",             // k8s, collapsed
		"aws-lambda-fn",       // cloudwatch, collapsed on log_group
		"solo-stream",         // cloudwatch, collapsed on log_stream
		"gce_instance",        // stackdriver, collapsed
		"payments",            // loki, collapsed
	} {
		if !symbols[want] {
			t.Fatalf("the fixture emitted no stream named %q; the parity surface does not reach that arm.\nseen: %v", want, symbols)
		}
	}
}

// TestEdgesJoinTheEmittedNodes covers the three edge families' endpoints.
func TestEdgesJoinTheEmittedNodes(t *testing.T) {
	nodes, edges, graph := emitParity(t)
	ids := map[string]string{}
	for _, n := range nodes {
		ids[n.ID] = n.Type
	}

	var hasLabel, belongsTo, contains int
	for _, e := range edges {
		from, to := ids[e.FromID], ids[e.ToID]
		switch e.Type {
		case EdgeHasLabel:
			hasLabel++
			if from != NodeLogStream || to != NodeLogLabel {
				t.Fatalf("HAS_LABEL joins %s -> %s, want log-stream -> log-label", from, to)
			}
		case EdgeBelongsTo:
			belongsTo++
			if from != NodeLogChunk || to != NodeLogStream {
				t.Fatalf("BELONGS_TO joins %s -> %s, want log-chunk -> log-stream", from, to)
			}
		case EdgeContains:
			contains++
			if from != NodeLogTemplate || to != NodeLogChunk {
				t.Fatalf("CONTAINS joins %s -> %s, want log-template -> log-chunk", from, to)
			}
		default:
			t.Fatalf("an unexpected edge type %q was emitted", e.Type)
		}
	}
	if belongsTo != len(graph.Chunks) || contains != len(graph.Chunks) {
		t.Fatalf("chunk edges: %d belongs-to and %d contains for %d chunks", belongsTo, contains, len(graph.Chunks))
	}
	wantHasLabel := 0
	for _, s := range graph.Streams {
		wantHasLabel += len(s.LowCardLabels)
	}
	if hasLabel != wantHasLabel {
		t.Fatalf("has-label edges = %d, want one per stream label pair (%d)", hasLabel, wantHasLabel)
	}
}

func assertMeta(t *testing.T, n framework.Node, key, want string) {
	t.Helper()
	got, ok := n.Metadata[key]
	if !ok {
		t.Fatalf("node %q (%s) carries no %s; metadata: %v", n.ID, n.Type, key, n.Metadata)
	}
	if got != want {
		t.Fatalf("node %q (%s) %s = %q, want %q", n.ID, n.Type, key, got, want)
	}
}
