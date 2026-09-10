// SPDX-License-Identifier: Apache-2.0

package main

import (
	"strconv"
	"testing"
	"time"
)

// stream_test.go — the fingerprint, the two-pass classification, and the
// cardinality boundary.

// TestStreamIDHashesEveryLabelAndTheFingerprintOnlyTheLowCardinalityOnes is the
// pair that distinguishes the two hashes. A module that hashed only the
// low-cardinality labels into the ID would pass a naive stability check and
// collapse two genuinely different streams into one.
func TestStreamIDHashesEveryLabelAndTheFingerprintOnlyTheLowCardinalityOnes(t *testing.T) {
	// One key crosses the threshold, so it classifies high-cardinality; the
	// other stays low.
	tracker := newCardinalityTracker(3)
	for i := range 5 {
		tracker.observe("request_id", strconv.Itoa(i))
	}
	tracker.observe("service", "api")

	a := newLogStream(labels("service", "api", "request_id", "r1"), tracker)
	b := newLogStream(labels("service", "api", "request_id", "r2"), tracker)

	if a.ID == b.ID {
		t.Errorf("two streams differing in a high-cardinality label share the id %s", a.ID)
	}
	if a.Fingerprint != b.Fingerprint {
		t.Errorf("the fingerprints differ (%s vs %s) although the low-cardinality labels are identical",
			a.Fingerprint, b.Fingerprint)
	}
	if _, low := a.LowCardLabels["request_id"]; low {
		t.Errorf("request_id was classified low-cardinality: %v", a.LowCardLabels)
	}
	if _, high := a.HighCardLabels["service"]; high {
		t.Errorf("service was classified high-cardinality: %v", a.HighCardLabels)
	}
}

// TestFingerprintIsOrderIndependentAndUnambiguous covers the two properties the
// preimage's shape buys: sorting the keys, and terminating each pair so two
// different label sets cannot share a preimage.
func TestFingerprintIsOrderIndependentAndUnambiguous(t *testing.T) {
	first := fingerprintLabels(map[string]string{"a": "1", "b": "2"})
	second := fingerprintLabels(map[string]string{"b": "2", "a": "1"})
	if first != second {
		t.Errorf("the fingerprint depends on map order: %s vs %s", first, second)
	}
	if fingerprintLabels(labels("ab", "c")) == fingerprintLabels(labels("a", "bc")) {
		t.Errorf("two different label sets share a preimage")
	}
	if fingerprintLabels(nil) == "" {
		t.Errorf("an empty label set produced no fingerprint")
	}
}

// TestCardinalityBoundary walks the threshold one value at a time. The
// comparison is strict, so a key AT the threshold is high-cardinality.
func TestCardinalityBoundary(t *testing.T) {
	for _, tc := range []struct {
		unique  int
		wantLow bool
	}{
		{1, true},
		{defaultCardinalityThreshold - 1, true},
		{defaultCardinalityThreshold, false},
		{defaultCardinalityThreshold + 1, false},
	} {
		t.Run(strconv.Itoa(tc.unique), func(t *testing.T) {
			tracker := newCardinalityTracker(0)
			for i := range tc.unique {
				tracker.observe("k", strconv.Itoa(i))
			}
			if got := tracker.isLowCardinality("k"); got != tc.wantLow {
				t.Errorf("%d unique values classified low=%v, want %v", tc.unique, got, tc.wantLow)
			}
		})
	}
	// KNOWN POSITIVE: a never-observed key is low-cardinality, which is the
	// right answer for a label set the tracker was not shown.
	if !newCardinalityTracker(0).isLowCardinality("never-seen") {
		t.Errorf("an unobserved key was classified high-cardinality")
	}
}

// TestCardinalityIsClassifiedOverTheWholeCollectNotPerEntry is what makes
// buildStreams two passes. A single pass would classify the first entry's stream
// against the values seen so far and the last entry's against all of them.
func TestCardinalityIsClassifiedOverTheWholeCollectNotPerEntry(t *testing.T) {
	var entries []logEntry
	for i := range 4 {
		entries = append(entries, testEntry(time.Duration(i)*time.Second, severityInfo, "m",
			labels("service", "api", "request_id", strconv.Itoa(i))))
	}
	streams, _ := buildStreams(entries, 3)
	if len(streams) != 4 {
		t.Fatalf("got %d streams, want 4", len(streams))
	}
	for _, s := range streams {
		if _, low := s.LowCardLabels["request_id"]; low {
			t.Errorf("stream %s classified request_id low-cardinality; "+
				"the first entry's stream was classified before the later values were observed", s.ID)
		}
	}
}

// TestBuildStreamsDeduplicatesAndSortsByID covers the grouping and the ordering
// that keeps the emitted node list a function of the entry set.
func TestBuildStreamsDeduplicatesAndSortsByID(t *testing.T) {
	entries := []logEntry{
		testEntry(0, severityInfo, "m", labels("service", "api")),
		testEntry(time.Second, severityInfo, "m", labels("service", "api")),
		testEntry(2*time.Second, severityInfo, "m", labels("service", "worker")),
	}
	streams, ids := buildStreams(entries, 0)
	if len(streams) != 2 {
		t.Fatalf("got %d streams from two distinct label sets, want 2", len(streams))
	}
	if streams[0].ID >= streams[1].ID {
		t.Errorf("the stream list is not sorted by id")
	}
	if ids[0] != ids[1] {
		t.Errorf("two entries with identical labels landed in different streams")
	}
	if ids[0] == ids[2] {
		t.Errorf("two entries with different labels landed in the same stream")
	}
}

// TestAnEntryWithNoLabelsStillGetsAStream is the empty input class: the entry
// belongs somewhere rather than being dropped.
func TestAnEntryWithNoLabelsStillGetsAStream(t *testing.T) {
	streams, ids := buildStreams([]logEntry{testEntry(0, severityInfo, "m", nil)}, 0)
	if len(streams) != 1 || ids[0] == "" {
		t.Fatalf("an unlabelled entry produced %d streams and the id %q", len(streams), ids[0])
	}
	if streams[0].Alias != "" {
		t.Errorf("an unlabelled stream derived the alias %q, want the empty string", streams[0].Alias)
	}
}

// TestLabelNodeIDIsReadable pins the one id built by concatenation, because it
// is also the source endpoint of every EMITTED_BY edge and a reader resolving
// one should be able to read the label straight out of it.
func TestLabelNodeIDIsReadable(t *testing.T) {
	if got := labelNodeID("service", "api"); got != "log-label:service=api" {
		t.Fatalf("labelNodeID = %q", got)
	}
}
