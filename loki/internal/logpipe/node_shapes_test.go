// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"testing"
	"time"
)

// node_shapes_test.go — the emitter's PER-NODE rules, split out of the parity
// row: what a node carries when a value is set, what it carries when one is
// not, and what the emitter refuses.

// TestAStreamNodeCarriesItsFULLLabelSetIncludingTheHighCardinalityOnes is the
// cell the parity fixture cannot reach: every label in that fixture is low
// cardinality, so a node built from the low-cardinality subset carries exactly
// the same keys and the parity row passes.
//
// The FULL set is what a reader reconstructs the stream from, and it is what
// the stream's id hashes, so a node carrying only the shared half describes a
// stream whose own id it cannot explain.
func TestAStreamNodeCarriesItsFULLLabelSetIncludingTheHighCardinalityOnes(t *testing.T) {
	tracker := NewCardinalityTracker(2)
	tracker.Observe("app", "checkout")
	for _, v := range []string{"p1", "p2", "p3"} {
		tracker.Observe("pod", v)
	}
	s := NewStream(map[string]string{"app": "checkout", "pod": "p1"}, tracker)
	if _, high := s.HighCardLabels["pod"]; !high {
		t.Fatalf("the fixture did not produce a high-cardinality label: %v", s.HighCardLabels)
	}

	n := streamNode(s)
	assertMeta(t, n, "label:app", "checkout")
	assertMeta(t, n, "label:pod", "p1")
	// And the fingerprint is still the LOW-cardinality hash, so the two sets
	// are distinguishable on the node rather than conflated.
	assertMeta(t, n, "fingerprint", FingerprintLabels(s.LowCardLabels))
	if n.Metadata["fingerprint"] == n.ID {
		t.Fatal("the fingerprint equals the id, so the node carries one set twice")
	}
}

// TestMetadataTimestampLayout covers the layout on both a UTC and a non-UTC
// time, and on a sub-millisecond one, which is where a shorter layout truncates
// silently.
func TestMetadataTimestampLayout(t *testing.T) {
	tokyo := time.FixedZone("JST", 9*3600)
	cases := []struct {
		name string
		ts   time.Time
		want string
	}{
		{"utc", time.Date(2026, 9, 7, 12, 4, 5, 0, time.UTC), "2026-09-07T12:04:05.000000000Z"},
		{"non-utc normalizes", time.Date(2026, 9, 7, 21, 4, 5, 0, tokyo), "2026-09-07T12:04:05.000000000Z"},
		{"sub-millisecond survives", time.Date(2026, 9, 7, 12, 4, 5, 123456, time.UTC), "2026-09-07T12:04:05.000123456Z"},
		{"nanosecond survives", time.Date(2026, 9, 7, 12, 4, 5, 1, time.UTC), "2026-09-07T12:04:05.000000001Z"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := templateNode(&Template{ID: "t", Pattern: "p", Severity: SeverityInfo, FirstSeen: tc.ts, LastSeen: tc.ts})
			assertMeta(t, n, "first_seen", tc.want)
			assertMeta(t, n, "last_seen", tc.want)
		})
	}
}

// TestZeroTimestampsAreOmittedRatherThanRenderedAsTheEpoch covers the
// conditional halves of both node builders.
func TestZeroTimestampsAreOmittedRatherThanRenderedAsTheEpoch(t *testing.T) {
	n := templateNode(&Template{ID: "t", Pattern: "p", Severity: SeverityInfo})
	for _, key := range []string{"first_seen", "last_seen"} {
		if v, ok := n.Metadata[key]; ok {
			t.Fatalf("an unset timestamp rendered %s=%q instead of being omitted", key, v)
		}
	}
	c := chunkNode(&Chunk{ID: "log-chunk:x", StreamID: "s", TemplateID: "t"})
	for _, key := range []string{"start_time", "end_time"} {
		if v, ok := c.Metadata[key]; ok {
			t.Fatalf("an unset timestamp rendered %s=%q instead of being omitted", key, v)
		}
	}
}

// TestTemplateNodeFallsBackToTheRawPatternForSymbolName covers the arm a
// template with no derivable alias takes. A node with no SymbolName would be
// unsearchable.
func TestTemplateNodeFallsBackToTheRawPatternForSymbolName(t *testing.T) {
	n := templateNode(&Template{ID: "t", Pattern: "<*>", Severity: SeverityInfo})
	if n.SymbolName != "<*>" {
		t.Fatalf("SymbolName = %q, want the raw pattern %q", n.SymbolName, "<*>")
	}
	if _, ok := n.Metadata["alias"]; ok {
		t.Fatal("a template with no derivable alias carries an alias key")
	}
}

// TestEmitRefusesANilGraph is the bad-input arm.
func TestEmitRefusesANilGraph(t *testing.T) {
	if _, _, err := Emit(nil, nil, nil); err == nil {
		t.Fatal("Emit(nil) returned no error")
	}
}

// TestEmptyWalkEmitsNothingRatherThanFabricating covers the first real run of a
// new collector: a window that matched nothing.
func TestEmptyWalkEmitsNothingRatherThanFabricating(t *testing.T) {
	graph, err := Build(nil, Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	nodes, edges, err := Emit(graph, nil, nil)
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if len(nodes) != 0 || len(edges) != 0 {
		t.Fatalf("an empty walk emitted %d nodes and %d edges", len(nodes), len(edges))
	}
}

// TestAPartiallyFilledDrainConfigIsRefused is the options bad-input arm: a
// config with a threshold set and no cluster cap would cluster every message
// into one template rather than take a default.
func TestAPartiallyFilledDrainConfigIsRefused(t *testing.T) {
	_, err := Build(nil, Options{Drain: DrainConfig{SimThreshold: 0.9}})
	if err == nil {
		t.Fatal("a DrainConfig with only SimThreshold set was accepted as a default")
	}
}
