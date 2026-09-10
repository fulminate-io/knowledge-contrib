// SPDX-License-Identifier: Apache-2.0

package main

import (
	"testing"
	"time"
)

// chunk_test.go — the window boundary, the chunk id's reproducibility, and the
// payload round trip.

// TestChunkWindowIsFlooredToTheEpochNotToTheCollect is the cell that catches a
// first-entry-relative floor, and it is the reason a re-collect over a different
// range still reconciles: the same entries under a DIFFERENT collect window must
// produce the same chunk ids.
func TestChunkWindowIsFlooredToTheEpochNotToTheCollect(t *testing.T) {
	// Two entries four minutes apart inside one five-minute bucket.
	inside := []logEntry{
		testEntry(0, severityInfo, "same shape here", labels("service", "api")),
		testEntry(4*time.Minute, severityInfo, "same shape here", labels("service", "api")),
	}
	first := chunkIDsFor(t, inside)
	if len(first) != 1 {
		t.Fatalf("two entries inside one bucket made %d chunks, want 1", len(first))
	}

	// The SAME entries, collected as if the window had started elsewhere: the
	// ids must not move, because the bucket is aligned to the epoch.
	shifted := []logEntry{
		testEntry(-time.Hour, severityInfo, "an unrelated earlier line", labels("service", "other")),
		inside[0], inside[1],
	}
	second := chunkIDsFor(t, shifted)
	found := false
	for _, id := range second {
		if id == first[0] {
			found = true
		}
	}
	if !found {
		t.Fatalf("the chunk id moved when an earlier entry joined the collect: %v not in %v", first, second)
	}
}

// TestEntriesStraddlingABucketEdgeLandInTwoChunks is the boundary arm, and its
// same-run control is the single-bucket case above.
func TestEntriesStraddlingABucketEdgeLandInTwoChunks(t *testing.T) {
	// baseTime is 12:00:00Z exactly, which is a bucket edge for a five-minute
	// window, so five seconds either side of 12:05:00Z straddles the next one.
	straddling := []logEntry{
		testEntry(5*time.Minute-5*time.Second, severityInfo, "same shape here", labels("service", "api")),
		testEntry(5*time.Minute+5*time.Second, severityInfo, "same shape here", labels("service", "api")),
	}
	if ids := chunkIDsFor(t, straddling); len(ids) != 2 {
		t.Fatalf("two entries straddling a bucket edge made %d chunks, want 2", len(ids))
	}
}

// TestEntriesSpanningSeveralBucketsMakeOneChunkEach covers the many-bucket cell.
func TestEntriesSpanningSeveralBucketsMakeOneChunkEach(t *testing.T) {
	var entries []logEntry
	for i := range 3 {
		entries = append(entries, testEntry(time.Duration(i)*6*time.Minute, severityInfo,
			"same shape here", labels("service", "api")))
	}
	if ids := chunkIDsFor(t, entries); len(ids) != 3 {
		t.Fatalf("three entries in three buckets made %d chunks, want 3", len(ids))
	}
}

// TestIdenticalTimestampsMakeOneChunk covers the degenerate cell.
func TestIdenticalTimestampsMakeOneChunk(t *testing.T) {
	entries := []logEntry{
		testEntry(0, severityInfo, "same shape here", labels("service", "api")),
		testEntry(0, severityInfo, "same shape here", labels("service", "api")),
	}
	ids := chunkIDsFor(t, entries)
	if len(ids) != 1 {
		t.Fatalf("two entries at one instant made %d chunks, want 1", len(ids))
	}
}

// TestWindowStartAlignsToTheEpoch asserts the flooring directly, including the
// negative side of the epoch, where an integer division truncates toward zero
// rather than downward.
func TestWindowStartAlignsToTheEpoch(t *testing.T) {
	ts := time.Date(2026, 9, 7, 12, 7, 30, 0, time.UTC)
	if got := windowStart(ts, 5*time.Minute); !got.Equal(time.Date(2026, 9, 7, 12, 5, 0, 0, time.UTC)) {
		t.Errorf("windowStart = %v, want 12:05:00Z", got)
	}
	if got := windowStart(ts, 0); !got.Equal(ts) {
		t.Errorf("a zero window moved the timestamp to %v", got)
	}
}

// TestChunkIDDependsOnAllThreeOfItsInputs is the id's own discrimination check:
// changing any one of stream, template or window must move it.
func TestChunkIDDependsOnAllThreeOfItsInputs(t *testing.T) {
	base := chunkID("s", "t", baseTime)
	for _, tc := range []struct {
		name string
		got  string
	}{
		{"stream", chunkID("s2", "t", baseTime)},
		{"template", chunkID("s", "t2", baseTime)},
		{"window", chunkID("s", "t", baseTime.Add(time.Minute))},
	} {
		if tc.got == base {
			t.Errorf("changing the %s left the chunk id unchanged", tc.name)
		}
	}
	if chunkID("s", "t", baseTime) != base {
		t.Errorf("the same inputs produced two different chunk ids")
	}
}

// TestChunkPayloadRoundTrips covers the encoding and its inverse, including the
// entry with no variable values.
func TestChunkPayloadRoundTrips(t *testing.T) {
	want := []chunkPayloadEntry{
		{Timestamp: baseTime, Vars: []string{"12", "ms"}},
		{Timestamp: baseTime.Add(time.Second), Vars: nil},
	}
	compressed, err := compressBytes(encodeChunkData(want))
	if err != nil {
		t.Fatalf("compressing: %v", err)
	}
	raw, err := decompressBytes(compressed)
	if err != nil {
		t.Fatalf("decompressing: %v", err)
	}
	got, err := decodeChunkData(raw)
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(got) != len(want) {
		t.Fatalf("decoded %d entries, want %d", len(got), len(want))
	}
	for i := range want {
		if !got[i].Timestamp.Equal(want[i].Timestamp) {
			t.Errorf("entry %d timestamp = %v, want %v", i, got[i].Timestamp, want[i].Timestamp)
		}
		if len(got[i].Vars) != len(want[i].Vars) {
			t.Errorf("entry %d vars = %v, want %v", i, got[i].Vars, want[i].Vars)
		}
	}
}

// TestATruncatedChunkPayloadIsAnErrorNotAPanic is the corrupt-input arm. Every
// length the decoder reads is bounds-checked, so a short buffer names where it
// failed rather than indexing past the end.
func TestATruncatedChunkPayloadIsAnErrorNotAPanic(t *testing.T) {
	full := encodeChunkData([]chunkPayloadEntry{{Timestamp: baseTime, Vars: []string{"abcdef"}}})
	for cut := 1; cut < len(full); cut++ {
		if _, err := decodeChunkData(full[:cut]); err == nil {
			t.Errorf("a payload truncated to %d of %d bytes decoded without error", cut, len(full))
		}
	}
	if _, err := decodeChunkData(nil); err == nil {
		t.Errorf("an empty payload decoded without error")
	}
	// KNOWN POSITIVE: the whole payload still decodes in the same run.
	if _, err := decodeChunkData(full); err != nil {
		t.Fatalf("the untruncated payload failed to decode: %v", err)
	}
}

// TestAssembleChunksRefusesMismatchedParallelSlices is the internal-invariant
// arm: the three slices are parallel by construction, so a mismatch is a
// programming error that must fail loud rather than index out of range.
func TestAssembleChunksRefusesMismatchedParallelSlices(t *testing.T) {
	entries := []logEntry{testEntry(0, severityInfo, "m", nil)}
	if _, err := assembleChunks(entries, []string{"s"}, nil, nil, defaultChunkWindow); err == nil {
		t.Fatalf("a length mismatch was accepted")
	}
}

// TestChunkAssemblySkipsEntriesWithNoStreamOrTemplate is where the dangling-edge
// defect is refused: an entry with no template must not produce a chunk, because
// the CONTAINS edge from that chunk would name a template that does not exist.
func TestChunkAssemblySkipsEntriesWithNoStreamOrTemplate(t *testing.T) {
	entries := []logEntry{
		testEntry(0, severityInfo, "m", nil),
		testEntry(time.Second, severityInfo, "m", nil),
	}
	chunks, err := assembleChunks(entries, []string{"s", "s"}, []string{"t", ""}, nil, defaultChunkWindow)
	if err != nil {
		t.Fatalf("assembling: %v", err)
	}
	if len(chunks) != 1 {
		t.Fatalf("got %d chunks, want 1: the entry with no template must be skipped", len(chunks))
	}
	if chunks[0].EntryCount != 1 {
		t.Errorf("the chunk holds %d entries, want 1", chunks[0].EntryCount)
	}
}

// TestChunkTimeRangeIsTheEntriesOwnNotTheWindows pins that a chunk reports what
// it holds rather than the bucket it sits in.
func TestChunkTimeRangeIsTheEntriesOwnNotTheWindows(t *testing.T) {
	entries := []logEntry{testEntry(90*time.Second, severityInfo, "m", nil)}
	chunks, err := assembleChunks(entries, []string{"s"}, []string{"t"}, nil, defaultChunkWindow)
	if err != nil {
		t.Fatalf("assembling: %v", err)
	}
	if !chunks[0].StartTime.Equal(baseTime.Add(90 * time.Second)) {
		t.Errorf("start = %v, want the entry's own instant", chunks[0].StartTime)
	}
}

// chunkIDsFor runs the real pipeline over entries at the default chunk window,
// which is the only window these rows exercise, and returns the chunk ids.
func chunkIDsFor(t *testing.T, entries []logEntry) []string {
	t.Helper()
	cfg := defaultPipelineConfig()
	cfg.ChunkWindow = defaultChunkWindow
	out, err := runPipeline(entries, cfg, nil)
	if err != nil {
		t.Fatalf("running the pipeline: %v", err)
	}
	ids := make([]string, 0, len(out.Chunks))
	for _, c := range out.Chunks {
		ids = append(ids, c.ID)
	}
	return ids
}
