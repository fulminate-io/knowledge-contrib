// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strconv"
	"testing"
	"time"
)

// stream_test.go — the cardinality crossing, which decides the graph's SHAPE,
// and the two hashes a reimplementation conflates.

// TestCardinalityCrossingBothSidesAndTheBoundary covers the threshold in three
// places: one below, exactly at it, and a key never observed at all.
func TestCardinalityCrossingBothSidesAndTheBoundary(t *testing.T) {
	const threshold = 4

	tracker := NewCardinalityTracker(threshold)
	// "low" gets three distinct values, one BELOW the threshold.
	for i := range 3 {
		tracker.Observe("low", "v"+strconv.Itoa(i))
	}
	// "high" gets exactly the threshold, so it is high-cardinality: the
	// comparison is strictly less-than.
	for i := range threshold {
		tracker.Observe("high", "v"+strconv.Itoa(i))
	}

	if !tracker.IsLowCardinality("low") {
		t.Error("a key one below the threshold is high-cardinality, want low")
	}
	if tracker.IsLowCardinality("high") {
		t.Error("a key AT the threshold is low-cardinality, want high")
	}
	if !tracker.IsLowCardinality("never-observed") {
		t.Error("a key never observed is high-cardinality, want low")
	}

	low, high := tracker.Classify(map[string]string{"low": "v0", "high": "v0", "never-observed": "x"})
	if _, ok := low["low"]; !ok {
		t.Error("the below-threshold key was not classified low")
	}
	if _, ok := high["high"]; !ok {
		t.Error("the at-threshold key was not classified high")
	}
	if _, ok := low["never-observed"]; !ok {
		t.Error("the never-observed key was not classified low")
	}
}

// TestClassificationIsWholeCollect is the cell a one-pass implementation gets
// wrong: a key crosses the threshold because of entries in a LATER stream, and
// the EARLIER stream must still see it as high-cardinality.
func TestClassificationIsWholeCollect(t *testing.T) {
	const threshold = 2
	entries := []LogEntry{
		{Labels: map[string]string{"shared": "a", "churn": "x1"}},
		{Labels: map[string]string{"shared": "a", "churn": "x2"}},
		{Labels: map[string]string{"shared": "a", "churn": "x3"}},
	}
	streams, _ := buildStreams(entries, threshold)
	if len(streams) != 3 {
		t.Fatalf("built %d streams, want 3", len(streams))
	}
	// The FIRST stream is built before the third entry's churn value is seen
	// in a single-pass implementation, so a single-pass tracker would classify
	// churn as low-cardinality there.
	if _, low := streams[0].LowCardLabels["churn"]; low {
		t.Error("the first stream classified `churn` as low-cardinality; the observe pass must cover " +
			"every entry of the collect before any stream is built")
	}
	if _, high := streams[0].HighCardLabels["churn"]; !high {
		t.Error("the first stream did not classify `churn` as high-cardinality")
	}
	if _, low := streams[0].LowCardLabels["shared"]; !low {
		t.Error("the first stream did not classify `shared` as low-cardinality")
	}
}

// TestHighCardinalityLabelsGetNoNodeAndNoEdge is the SHAPE half: the
// classification decides how many nodes and edges exist, not just a field.
func TestHighCardinalityLabelsGetNoNodeAndNoEdge(t *testing.T) {
	stream := &LogStream{
		ID:             "s1",
		Labels:         map[string]string{"shared": "a", "churn": "x1"},
		LowCardLabels:  map[string]string{"shared": "a"},
		HighCardLabels: map[string]string{"churn": "x1"},
	}
	nodes := labelNodes([]*LogStream{stream})
	if len(nodes) != 1 || nodes[0].ID != "log-label:shared=a" {
		t.Fatalf("label nodes = %v, want exactly the low-cardinality pair", nodes)
	}
	edges := hasLabelEdges(stream)
	if len(edges) != 1 || edges[0].ToID != "log-label:shared=a" || edges[0].Type != edgeHasLabel {
		t.Fatalf("has-label edges = %v, want exactly one to the low-cardinality label", edges)
	}
	// The high-cardinality label still rides the stream node inline.
	node := streamNode(stream)
	if node.Metadata["label:churn"] != "x1" {
		t.Error("the high-cardinality label is not carried inline on the stream node")
	}
}

// TestStreamIDAndFingerprintAreTwoHashesOverTwoInputs is the cell that
// separates the two: two streams sharing their low-card labels and differing in
// a high-card one share a FINGERPRINT and have DIFFERENT ids.
func TestStreamIDAndFingerprintAreTwoHashesOverTwoInputs(t *testing.T) {
	const threshold = 2
	entries := []LogEntry{
		{Labels: map[string]string{"shared": "a", "churn": "x1"}},
		{Labels: map[string]string{"shared": "a", "churn": "x2"}},
		{Labels: map[string]string{"shared": "a", "churn": "x3"}},
	}
	streams, _ := buildStreams(entries, threshold)
	if streams[0].ID == streams[1].ID {
		t.Error("two streams differing in a high-cardinality label share an id")
	}
	if streams[0].Fingerprint != streams[1].Fingerprint {
		t.Error("two streams sharing their low-cardinality labels have different fingerprints")
	}
	if streams[0].ID == streams[0].Fingerprint {
		t.Error("the id and the fingerprint are the same hash; they cover different label sets")
	}
	if len(streams[0].ID) != 64 {
		t.Errorf("stream id %q is %d hex characters, want the full 64", streams[0].ID, len(streams[0].ID))
	}
}

// TestFingerprintIsOrderIndependentAndNonEmptyForNoLabels pins the two
// remaining properties of the hash.
func TestFingerprintIsOrderIndependentAndNonEmptyForNoLabels(t *testing.T) {
	a := FingerprintLabels(map[string]string{"b": "2", "a": "1"})
	b := FingerprintLabels(map[string]string{"a": "1", "b": "2"})
	if a != b {
		t.Error("the fingerprint depends on map iteration order; keys must be sorted")
	}
	if FingerprintLabels(nil) == "" {
		t.Error("an empty label set produced an empty fingerprint; a stream with no labels still needs an identity")
	}
}

// TestStreamOrderIsDeterministic pins the emission order across runs. Node
// IDENTITY does not depend on it — every id is a hash — but a stable order is
// what lets a golden compare whole outputs instead of sorted projections.
func TestStreamOrderIsDeterministic(t *testing.T) {
	entries := make([]LogEntry, 0, 20)
	for i := range 20 {
		entries = append(entries, LogEntry{
			Timestamp: time.Unix(int64(i), 0).UTC(),
			Labels:    map[string]string{"k": "v" + strconv.Itoa(i)},
		})
	}
	first, _ := buildStreams(entries, 0)
	for range 5 {
		again, _ := buildStreams(entries, 0)
		for i := range first {
			if first[i].ID != again[i].ID {
				t.Fatalf("stream order differs between runs at position %d: %s then %s", i, first[i].ID, again[i].ID)
			}
		}
	}
}
