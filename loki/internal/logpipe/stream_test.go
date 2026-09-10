// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"crypto/sha256"
	"fmt"
	"testing"
	"time"
)

// stream_test.go — stream identity, the cardinality split, and the asymmetry
// between the two that carry-forward rests on.

// TestFingerprintLabelsIsTheDocumentedHash builds the rendering here — sorted
// "key=value\n" lines — and hashes it, rather than comparing two calls.
func TestFingerprintLabelsIsTheDocumentedHash(t *testing.T) {
	labels := map[string]string{"zone": "z1", "app": "checkout", "instance": "host-3"}
	want := fmt.Sprintf("%x", sha256.Sum256([]byte("app=checkout\ninstance=host-3\nzone=z1\n")))
	if got := FingerprintLabels(labels); got != want {
		t.Fatalf("FingerprintLabels = %q, want %q", got, want)
	}
	if len(want) != 64 {
		t.Fatalf("the id is %d hex characters, want 64", len(want))
	}
}

// TestFingerprintOverAnEmptySetHashesEmptyInput covers the arm a stream with no
// labels takes. It is the sha256 of NOTHING, not of the empty rendering, and
// the two differ.
func TestFingerprintOverAnEmptySetHashesEmptyInput(t *testing.T) {
	want := fmt.Sprintf("%x", sha256.Sum256(nil))
	if got := FingerprintLabels(nil); got != want {
		t.Fatalf("FingerprintLabels(nil) = %q, want %q", got, want)
	}
	if got := FingerprintLabels(map[string]string{}); got != want {
		t.Fatalf("FingerprintLabels over an empty map = %q, want %q", got, want)
	}
}

// TestStreamIDIsStableAcrossMapOrderAndEntryOrder is the property R5's
// carry-forward diff rests on: the same label set produces the same id whatever
// order the walk sees its entries in, and whatever order Go walks its maps in.
func TestStreamIDIsStableAcrossMapOrderAndEntryOrder(t *testing.T) {
	labels := map[string]string{"app": "checkout", "instance": "host-3", "zone": "z1", "region": "r1", "tier": "t1"}
	first := FingerprintLabels(labels)
	for i := range 50 {
		if got := FingerprintLabels(labels); got != first {
			t.Fatalf("iteration %d produced %q, want %q; the rendering is not sorted", i, got, first)
		}
	}

	forward := []Entry{
		{Timestamp: at(0), Message: "a", Labels: map[string]string{"app": "checkout"}},
		{Timestamp: at(time.Second), Message: "b", Labels: map[string]string{"app": "payments"}},
	}
	backward := []Entry{forward[1], forward[0]}
	fs, _ := buildStreams(forward, DefaultCardinalityThreshold)
	bs, _ := buildStreams(backward, DefaultCardinalityThreshold)
	if len(fs) != 2 || len(bs) != 2 {
		t.Fatalf("streams: forward=%d backward=%d, want 2 each", len(fs), len(bs))
	}
	forwardIDs := map[string]bool{fs[0].ID: true, fs[1].ID: true}
	for _, s := range bs {
		if !forwardIDs[s.ID] {
			t.Fatalf("the reversed entry order produced stream id %q, which the forward order did not", s.ID)
		}
	}
}

// TestAPartialOrUnsortedRenderingProducesADifferentID is the negative half:
// dropping a label or rendering the set unsorted gives a different id, which is
// what makes the sort load-bearing rather than cosmetic.
func TestAPartialOrUnsortedRenderingProducesADifferentID(t *testing.T) {
	full := map[string]string{"app": "checkout", "instance": "host-3"}
	id := FingerprintLabels(full)

	partial := FingerprintLabels(map[string]string{"app": "checkout"})
	if partial == id {
		t.Fatal("dropping a label did not change the id; the id is not over the full set")
	}
	unsorted := fmt.Sprintf("%x", sha256.Sum256([]byte("instance=host-3\napp=checkout\n")))
	if unsorted == id {
		t.Fatal("an unsorted rendering hashes to the same value; the sort is not in the preimage")
	}
}

// TestCardinalityClassifiesOnObservedValues covers the threshold in both
// directions and the never-observed key.
func TestCardinalityClassifiesOnObservedValues(t *testing.T) {
	tracker := NewCardinalityTracker(3)
	for _, v := range []string{"a", "b"} {
		tracker.Observe("low", v)
	}
	for _, v := range []string{"a", "b", "c", "d"} {
		tracker.Observe("high", v)
	}

	if !tracker.IsLowCardinality("low") {
		t.Fatal("a key with two of three distinct values is low cardinality")
	}
	if tracker.IsLowCardinality("high") {
		t.Fatal("a key with four of three distinct values is high cardinality")
	}
	// A KEY NEVER OBSERVED READS AS LOW, because zero is fewer than the
	// threshold. A classifier written as "observed and under" gets this wrong.
	if !tracker.IsLowCardinality("never-seen") {
		t.Fatal("a key never observed must read as low cardinality")
	}

	low, high := tracker.Classify(map[string]string{"low": "a", "high": "a", "never-seen": "a"})
	if len(low) != 2 || len(high) != 1 {
		t.Fatalf("classify split %d low and %d high, want 2 and 1", len(low), len(high))
	}
	if _, ok := high["high"]; !ok {
		t.Fatalf("the high-cardinality key landed in the low set: low=%v high=%v", low, high)
	}
}

// TestZeroThresholdTakesTheDefault covers the knob's own default arm.
func TestZeroThresholdTakesTheDefault(t *testing.T) {
	tracker := NewCardinalityTracker(0)
	for i := range DefaultCardinalityThreshold - 1 {
		tracker.Observe("k", fmt.Sprintf("v%d", i))
	}
	if !tracker.IsLowCardinality("k") {
		t.Fatalf("a key one below the default threshold must be low cardinality")
	}
	tracker.Observe("k", "one-more")
	if tracker.IsLowCardinality("k") {
		t.Fatalf("a key at the default threshold must be high cardinality")
	}
}

// TestTheCardinalitySplitMovesWhileTheStreamIDDoesNot is the asymmetry that
// separates what a carry-forward diff sees as an update from what it sees as a
// delete and a create. It is one cell with two assertions, because the two
// halves are only meaningful together.
func TestTheCardinalitySplitMovesWhileTheStreamIDDoesNot(t *testing.T) {
	// One collect where `pod` carries two values: LOW cardinality.
	narrow := []Entry{
		{Timestamp: at(0), Message: "a", Labels: map[string]string{"app": "checkout", "pod": "p1"}},
		{Timestamp: at(time.Second), Message: "b", Labels: map[string]string{"app": "checkout", "pod": "p1"}},
	}
	// A second collect over the same stream where `pod` crossed the threshold.
	wide := append([]Entry{}, narrow...)
	for i := range 3 {
		wide = append(wide, Entry{
			Timestamp: at(time.Duration(i+2) * time.Second),
			Message:   "c",
			Labels:    map[string]string{"app": "checkout", "pod": fmt.Sprintf("p%d", i+2)},
		})
	}

	narrowStreams, _ := buildStreams(narrow, 3)
	wideStreams, _ := buildStreams(wide, 3)

	var narrowP1, wideP1 *Stream
	for _, s := range narrowStreams {
		if s.Labels["pod"] == "p1" {
			narrowP1 = s
		}
	}
	for _, s := range wideStreams {
		if s.Labels["pod"] == "p1" {
			wideP1 = s
		}
	}
	if narrowP1 == nil || wideP1 == nil {
		t.Fatal("the p1 stream is missing from one of the two collects")
	}

	// THE ID DOES NOT MOVE: it hashes the full label set, which did not change.
	if narrowP1.ID != wideP1.ID {
		t.Fatalf("the stream id moved from %q to %q when only the cardinality class changed", narrowP1.ID, wideP1.ID)
	}
	// THE SPLIT DOES MOVE: pod was shared as a label node in the first collect
	// and is inline in the second, so its HAS_LABEL emission and its
	// fingerprint membership change.
	if _, low := narrowP1.LowCardLabels["pod"]; !low {
		t.Fatalf("pod was not low cardinality in the narrow collect: %v", narrowP1.LowCardLabels)
	}
	if _, low := wideP1.LowCardLabels["pod"]; low {
		t.Fatalf("pod is still low cardinality in the wide collect: %v", wideP1.LowCardLabels)
	}
	if narrowP1.Fingerprint == wideP1.Fingerprint {
		t.Fatal("the fingerprint did not move when a label left the low-cardinality set")
	}
}

// TestStreamCarriesBothHashesOverTheirOwnSets covers the two-hash rule
// directly: the id is over the full set and the fingerprint over the low set,
// and they differ whenever a label is high cardinality.
func TestStreamCarriesBothHashesOverTheirOwnSets(t *testing.T) {
	tracker := NewCardinalityTracker(2)
	tracker.Observe("app", "checkout")
	for _, v := range []string{"p1", "p2", "p3"} {
		tracker.Observe("pod", v)
	}
	s := NewStream(map[string]string{"app": "checkout", "pod": "p1"}, tracker)

	if s.ID != FingerprintLabels(map[string]string{"app": "checkout", "pod": "p1"}) {
		t.Fatalf("the id is not the hash of the full label set")
	}
	if s.Fingerprint != FingerprintLabels(map[string]string{"app": "checkout"}) {
		t.Fatalf("the fingerprint is not the hash of the low-cardinality labels alone")
	}
	if s.ID == s.Fingerprint {
		t.Fatal("the id and the fingerprint agree over a set with a high-cardinality label; they hash different sets")
	}
	if len(s.HighCardLabels) != 1 {
		t.Fatalf("high-cardinality labels = %v, want just pod", s.HighCardLabels)
	}
	// THE ALIAS IS DERIVED FROM THE FULL LABEL SET, not the low-cardinality
	// one, so a high-cardinality label still names the stream. That is what
	// keeps the alias stable while the cardinality split moves: the low set is
	// only the fallback for a stream reconstructed without its full labels.
	if s.Alias != "checkout@p1" {
		t.Fatalf("alias = %q, want %q; the deriver reads the full label set", s.Alias, "checkout@p1")
	}
}

// TestStreamsComeOutInFirstAppearanceOrder pins the emission order. The set is
// built in a Go map, whose iteration order is randomized, so without this the
// same input would emit its streams in a different order on every run.
func TestStreamsComeOutInFirstAppearanceOrder(t *testing.T) {
	entries := []Entry{
		{Timestamp: at(0), Message: "a", Labels: map[string]string{"app": "c"}},
		{Timestamp: at(0), Message: "b", Labels: map[string]string{"app": "b"}},
		{Timestamp: at(0), Message: "c", Labels: map[string]string{"app": "a"}},
		{Timestamp: at(0), Message: "d", Labels: map[string]string{"app": "c"}},
	}
	want := []string{"c", "b", "a"}
	for run := range 20 {
		streams, ids := buildStreams(entries, DefaultCardinalityThreshold)
		if len(streams) != 3 {
			t.Fatalf("streams = %d, want 3", len(streams))
		}
		for i, w := range want {
			if streams[i].Labels["app"] != w {
				t.Fatalf("run %d: stream %d is app=%q, want app=%q", run, i, streams[i].Labels["app"], w)
			}
		}
		if ids[0] != ids[3] {
			t.Fatalf("two entries carrying one label set got different stream ids")
		}
	}
}

// TestLabelNodeIDCarriesItsPrefix pins the label node id, which is the endpoint
// of both the HAS_LABEL and the EMITTED_BY edge.
func TestLabelNodeIDCarriesItsPrefix(t *testing.T) {
	if got := LabelNodeID("namespace", "prod"); got != "log-label:namespace=prod" {
		t.Fatalf("LabelNodeID = %q, want %q", got, "log-label:namespace=prod")
	}
}
