// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fulminate-io/knowledge-contrib/common/correlation"
)

// emitted_values_test.go — EVERY EMITTED ID AND TIME VALUE, EXPECTED
// INDEPENDENTLY OF THE PRODUCER.
//
// WHY THIS FILE EXISTS. The parity assertions elsewhere check that the metadata
// KEYS are present. Presence is not a value: with only that, setting first_seen
// from LastSeen and swapping start_time with end_time passes every test, and a
// chunk whose range runs backwards renders fine and is refused nowhere. The
// graph is then silently wrong about time, which is the one thing a consumer
// asking "what happened between 10:00 and 10:05" reads it for.
//
// WHAT "INDEPENDENT" MEANS HERE, because a test that recomputes a value the way
// the emitter does is a second copy of the emitter rather than an expectation.
// Each value below is either a LITERAL written into the test, or computed in
// the test FROM THE RAW FIXTURE ENTRIES — the timestamps and messages the
// fixture declares — never read back out of the product the emitter returned.
//
// THE CENSUS, taken from the emitter code (graph.go, proxy.go, and the common
// module's materialize.go) rather than from this file's coverage, so a value
// nothing asserts is a visible omission. The proxy and CORRELATES_WITH rows are
// asserted in correlate_test.go, which is where this module's projection into
// the common detector is covered:
//
//	log-template  id, alias, pattern, severity, count, first_seen, last_seen
//	log-stream    id, alias, fingerprint, label:<key> per label
//	log-chunk     id, stream_id, template_id, entry_count, start_time, end_time
//	log-label     id, symbol_name, label_key, label_value
//	proxy         id, source, symbol_name, foreign_graph, foreign_id, account
//	edges         BELONGS_TO, CONTAINS, HAS_LABEL, EMITTED_BY endpoints;
//	              CORRELATES_WITH endpoints, confidence, method, evidence

// testResolver and testOracle are the two cloud answers no collector process can
// derive, supplied here as literals. They stand where a declared cloud slice
// stands in production, and they are the SMALLEST thing that answers the common
// detector's two questions — which resource a service names, and whether two
// resources are connected.
type testResolver map[string]correlation.ResolvedResource

func (r testResolver) ResolveService(_ *correlation.Stream, service string) (correlation.ResolvedResource, bool) {
	res, ok := r[service]
	return res, ok
}

type testOracle map[[2]string]struct{}

func (o testOracle) HasDependency(a, b correlation.ResolvedResource) bool {
	_, ok := o[[2]string{a.ID, b.ID}]
	return ok
}

// valuesFixture is the shared fixture, and its raw facts are declared here so
// every expectation below can be computed from them rather than from the graph.
type valuesFixture struct {
	entries  []Entry
	labels   map[string]string
	earliest time.Time
	latest   time.Time
	messages []string
}

func newValuesFixture() valuesFixture {
	base := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	labels := map[string]string{"container": "api", "container_pod": "api-1", "namespace": "dev"}
	// THREE ENTRIES OF ONE SHAPE, so they form ONE template whose bounds are the
	// first and third stamps and whose count is three. The middle one is the
	// LATEST so a first/last swap is visible: with them in order, a swap would
	// still put a plausible-looking pair on the node.
	stamps := []time.Time{base, base.Add(2 * time.Minute), base.Add(time.Minute)}
	messages := []string{
		"connection to database failed for alpha",
		"connection to database failed for beta",
		"connection to database failed for gamma",
	}
	entries := make([]Entry, 0, len(messages))
	for i, msg := range messages {
		entries = append(entries, Entry{
			Timestamp: stamps[i], Severity: SeverityError, Message: msg, Labels: labels,
		})
	}
	return valuesFixture{
		entries:  entries,
		labels:   labels,
		earliest: base,
		latest:   base.Add(2 * time.Minute),
		messages: messages,
	}
}

// stampedLiterally formats a time the way the emitter's metadata layout does,
// spelled out here as a LITERAL format string rather than by referencing the
// package constant, so a change to that constant is visible rather than
// followed.
func stampedLiterally(ts time.Time) string {
	return ts.UTC().Format("2006-01-02T15:04:05.000000000Z07:00")
}

// TestEmittedTemplateValues — the template node's every value.
func TestEmittedTemplateValues(t *testing.T) {
	f := newValuesFixture()
	res, err := Build(f.entries, Options{ChunkWindow: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	node, ok := firstNodeOfType(res.Nodes, NodeLogTemplate)
	if !ok {
		t.Fatal("no template node was emitted")
	}

	// THE ID, computed in the test from the pattern the fixture's own messages
	// broaden to. All three differ only in their last token, so the pattern is
	// the shared prefix with a wildcard at the end.
	wantPattern := "connection to database failed for " + Wildcard
	wantID := fmt.Sprintf("%x", sha256.Sum256([]byte(wantPattern)))[:32]
	if node.ID != wantID {
		t.Errorf("template id is %s, want %s (the truncated sha256 of %q)", node.ID, wantID, wantPattern)
	}
	if got := node.Metadata["pattern"]; got != wantPattern {
		t.Errorf("template pattern is %q, want %q", got, wantPattern)
	}

	// THE TIMES, from the fixture's own stamps.
	if got, want := node.Metadata["first_seen"], stampedLiterally(f.earliest); got != want {
		t.Errorf("first_seen is %q, want the EARLIEST entry's %q. A first_seen taken from LastSeen renders "+
			"as a plausible timestamp and makes the graph silently wrong about when a pattern began", got, want)
	}
	if got, want := node.Metadata["last_seen"], stampedLiterally(f.latest); got != want {
		t.Errorf("last_seen is %q, want the LATEST entry's %q", got, want)
	}
	if node.Metadata["first_seen"] == node.Metadata["last_seen"] {
		t.Error("first_seen equals last_seen over three entries spanning two minutes")
	}

	if got, want := node.Metadata["count"], strconv.Itoa(len(f.entries)); got != want {
		t.Errorf("count is %q, want %q", got, want)
	}
	if got := node.Metadata["severity"]; got != SeverityError {
		t.Errorf("severity is %q, want ERROR", got)
	}
	if got, want := node.SymbolName, "connection-database-failed@err"; got != want {
		t.Errorf("template SymbolName is %q, want %q", got, want)
	}
	if node.Metadata["alias"] != node.SymbolName {
		t.Errorf("the alias metadata %q and the SymbolName %q disagree", node.Metadata["alias"], node.SymbolName)
	}
}

// TestEmittedStreamValues — the stream node's every value.
func TestEmittedStreamValues(t *testing.T) {
	f := newValuesFixture()
	res, err := Build(f.entries, Options{ChunkWindow: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	node, ok := firstNodeOfType(res.Nodes, NodeLogStream)
	if !ok {
		t.Fatal("no stream node was emitted")
	}

	// THE ID, computed in the test from the fixture's own label map: keys
	// sorted, joined as newline-terminated key=value lines, sha256, hex, FULL
	// length.
	var joined strings.Builder
	for _, k := range []string{"container", "container_pod", "namespace"} {
		joined.WriteString(k + "=" + f.labels[k] + "\n")
	}
	wantID := fmt.Sprintf("%x", sha256.Sum256([]byte(joined.String())))
	if node.ID != wantID {
		t.Errorf("stream id is %s, want %s", node.ID, wantID)
	}
	if len(node.ID) != 64 {
		t.Errorf("stream id is %d characters; a stream id is the FULL hash", len(node.ID))
	}

	// THE FINGERPRINT. Every label here is low-cardinality, so it hashes the
	// same input as the id — which is a fact about THIS fixture and is asserted
	// as such, with the differing case covered by
	// TestFingerprintUsesOnlyTheLowCardinalityLabels.
	if node.Metadata["fingerprint"] != wantID {
		t.Errorf("fingerprint is %s, want %s for an all-low-cardinality label set",
			node.Metadata["fingerprint"], wantID)
	}
	for k, v := range f.labels {
		if got := node.Metadata["label:"+k]; got != v {
			t.Errorf("label:%s is %q, want %q", k, got, v)
		}
	}
	if got, want := node.SymbolName, "container=api.container_pod=api-1"; got != want {
		t.Errorf("stream SymbolName is %q, want %q", got, want)
	}
}

// TestEmittedChunkValues — the chunk node's every value, the pair the review
// found swappable included.
func TestEmittedChunkValues(t *testing.T) {
	f := newValuesFixture()
	res, err := Build(f.entries, Options{ChunkWindow: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	node, ok := firstNodeOfType(res.Nodes, NodeLogChunk)
	if !ok {
		t.Fatal("no chunk node was emitted")
	}
	stream, _ := firstNodeOfType(res.Nodes, NodeLogStream)
	template, _ := firstNodeOfType(res.Nodes, NodeLogTemplate)

	if got := node.Metadata["stream_id"]; got != stream.ID {
		t.Errorf("chunk stream_id is %s, want the emitted stream's %s", got, stream.ID)
	}
	if got := node.Metadata["template_id"]; got != template.ID {
		t.Errorf("chunk template_id is %s, want the emitted template's %s", got, template.ID)
	}
	if got, want := node.Metadata["entry_count"], strconv.Itoa(len(f.entries)); got != want {
		t.Errorf("entry_count is %q, want %q", got, want)
	}

	// THE TIMES, from the fixture's own stamps rather than from the chunk.
	if got, want := node.Metadata["start_time"], stampedLiterally(f.earliest); got != want {
		t.Errorf("start_time is %q, want the EARLIEST entry's %q. A swapped pair yields a chunk whose range "+
			"runs backwards, which renders and is refused nowhere", got, want)
	}
	if got, want := node.Metadata["end_time"], stampedLiterally(f.latest); got != want {
		t.Errorf("end_time is %q, want the LATEST entry's %q", got, want)
	}
	if node.Metadata["start_time"] >= node.Metadata["end_time"] {
		t.Errorf("the chunk's range runs backwards or is empty: %q to %q",
			node.Metadata["start_time"], node.Metadata["end_time"])
	}

	// THE ID, computed in the test from the two ids and the window start the
	// fixture's own earliest stamp falls in.
	if got, want := node.ID, ChunkID(stream.ID, template.ID, f.earliest.Truncate(time.Hour)); got != want {
		t.Errorf("chunk id is %s, want %s", got, want)
	}
}

// TestEmittedLabelValues — the label node's id and its two metadata values.
func TestEmittedLabelValues(t *testing.T) {
	f := newValuesFixture()
	res, err := Build(f.entries, Options{ChunkWindow: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][2]string{
		"log-label:container=api":       {"container", "api"},
		"log-label:container_pod=api-1": {"container_pod", "api-1"},
		"log-label:namespace=dev":       {"namespace", "dev"},
	}
	seen := 0
	for _, n := range res.Nodes {
		if n.Type != NodeLogLabel {
			continue
		}
		pair, known := want[n.ID]
		if !known {
			t.Errorf("an unexpected label node %s was emitted", n.ID)
			continue
		}
		seen++
		if n.Metadata["label_key"] != pair[0] || n.Metadata["label_value"] != pair[1] {
			t.Errorf("%s carries key=%q value=%q, want %q and %q",
				n.ID, n.Metadata["label_key"], n.Metadata["label_value"], pair[0], pair[1])
		}
		if n.SymbolName != pair[0]+"="+pair[1] {
			t.Errorf("%s has SymbolName %q, want %q", n.ID, n.SymbolName, pair[0]+"="+pair[1])
		}
	}
	if seen != len(want) {
		t.Fatalf("%d label nodes were emitted, want %d", seen, len(want))
	}
}

// TestEmittedEdgeEndpoints — every edge joins the ids the emitter actually
// produced, checked against the node set rather than against itself.
func TestEmittedEdgeEndpoints(t *testing.T) {
	f := newValuesFixture()
	res, err := Build(f.entries, Options{ChunkWindow: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]string{}
	for _, n := range res.Nodes {
		ids[n.ID] = n.Type
	}
	chunk, _ := firstNodeOfType(res.Nodes, NodeLogChunk)
	stream, _ := firstNodeOfType(res.Nodes, NodeLogStream)
	template, _ := firstNodeOfType(res.Nodes, NodeLogTemplate)

	want := map[string][2]string{
		EdgeBelongsTo: {chunk.ID, stream.ID},
		EdgeContains:  {template.ID, chunk.ID},
	}
	for _, e := range res.Edges {
		if _, known := ids[e.FromID]; !known {
			t.Errorf("%s runs from %s, which no emitted node carries", e.Type, e.FromID)
		}
		if _, known := ids[e.ToID]; !known {
			t.Errorf("%s runs to %s, which no emitted node carries", e.Type, e.ToID)
		}
		if pair, checked := want[e.Type]; checked {
			if e.FromID != pair[0] || e.ToID != pair[1] {
				t.Errorf("%s joins %s -> %s, want %s -> %s", e.Type, e.FromID, e.ToID, pair[0], pair[1])
			}
		}
	}
}
