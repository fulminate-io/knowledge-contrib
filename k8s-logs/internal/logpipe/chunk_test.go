// SPDX-License-Identifier: Apache-2.0

package logpipe

import (
	"strings"
	"testing"
	"time"
)

// chunk_test.go — the chunk id's three failure modes, the window's alignment
// and its boundary cell, and what the compressed payload actually carries.

// TestChunkIDShape pins the prefix and the length, and shows that the
// separator is load-bearing.
func TestChunkIDShape(t *testing.T) {
	id := ChunkID("stream", "template", at(0))
	if !strings.HasPrefix(id, "log-chunk:") {
		t.Fatalf("chunk id %q carries no \"log-chunk:\" prefix; a bare hash names a node the log graph does not have", id)
	}
	hex := strings.TrimPrefix(id, "log-chunk:")
	if len(hex) != 32 {
		t.Fatalf("chunk id hash is %d characters (%q); it is a truncated sha256 of 32", len(hex), hex)
	}

	// Without the separator byte, "ab"+"c" and "a"+"bc" would hash alike.
	if ChunkID("ab", "c", at(0)) == ChunkID("a", "bc", at(0)) {
		t.Fatal("two different (stream, template) pairs hash to one chunk id; the separator byte is missing")
	}
	if ChunkID("s", "t", at(0)) == ChunkID("s", "t", at(1)) {
		t.Fatal("two windows of one (stream, template) hash to one chunk id; the window start is not in the hash")
	}
}

// TestWindowStartIsAlignedToTheUTCEpoch is the property that makes a re-collect
// over a shifted range reproduce the same chunk ids.
func TestWindowStartIsAlignedToTheUTCEpoch(t *testing.T) {
	window := 5 * time.Minute
	// 10:03:30 and 10:04:59 are the same bucket; 10:05:00 opens the next.
	base := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	first := WindowStart(base.Add(3*time.Minute+30*time.Second), window)
	second := WindowStart(base.Add(4*time.Minute+59*time.Second), window)
	third := WindowStart(base.Add(5*time.Minute), window)

	if !first.Equal(base) {
		t.Fatalf("window start is %s, want the epoch-aligned %s; bucketing relative to the first entry moves every "+
			"boundary when the collect's range moves", first, base)
	}
	if !first.Equal(second) {
		t.Fatalf("two entries in one bucket got different window starts: %s and %s", first, second)
	}
	if third.Equal(first) {
		t.Fatal("an entry exactly on a bucket edge stayed in the earlier bucket; it opens the later one")
	}
	if want := base.Add(window); !third.Equal(want) {
		t.Fatalf("the boundary entry landed in bucket %s, want %s", third, want)
	}
	if got := WindowStart(base, 0); !got.Equal(base) {
		t.Fatalf("a non-positive window returned %s; it degrades to the timestamp itself", got)
	}
}

// TestEntriesSpanningTwoWindowsProduceTwoChunks is the structural half.
func TestEntriesSpanningTwoWindowsProduceTwoChunks(t *testing.T) {
	base := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	entries := []Entry{
		{Timestamp: base.Add(time.Minute), Message: "cache miss", Labels: map[string]string{"a": "1"}},
		{Timestamp: base.Add(7 * time.Minute), Message: "cache miss", Labels: map[string]string{"a": "1"}},
	}
	templates, templateIDs := ProcessEntries(entries, DefaultDrainConfig())
	_, streamIDs := BuildStreams(entries, 0)

	chunks, err := AssembleChunks(entries, streamIDs, templateIDs, templates, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 2 {
		t.Fatalf("two entries seven minutes apart under a five-minute window produced %d chunk(s); "+
			"one chunk per (stream, template) ignores the window", len(chunks))
	}
	if chunks[0].ID == chunks[1].ID {
		t.Fatal("the two windows produced one chunk id")
	}
	for _, c := range chunks {
		if c.EntryCount != 1 {
			t.Fatalf("chunk %s holds %d entries, want 1", c.ID, c.EntryCount)
		}
		if c.StartTime.IsZero() || c.EndTime.IsZero() {
			t.Fatalf("chunk %s carries no entry time range", c.ID)
		}
	}
}

// TestChunkPayloadRoundTrips reads the bytes back rather than trusting the
// entry count.
func TestChunkPayloadRoundTrips(t *testing.T) {
	base := time.Date(2026, 9, 7, 10, 0, 0, 0, time.UTC)
	// The differing token must be one PREPROCESSING did not already wildcard:
	// a variable value is recorded only where DRAIN broadened the pattern, so a
	// long number or a uuid — replaced before clustering ever sees it — leaves
	// no value behind. That is the built-in behavior and this fixture is
	// written to observe the case that does store one.
	entries := []Entry{
		{Timestamp: base, Message: "connection to database failed for alpha", Labels: map[string]string{"a": "1"}},
		{Timestamp: base.Add(time.Second), Message: "connection to database failed for beta", Labels: map[string]string{"a": "1"}},
	}
	templates, templateIDs := ProcessEntries(entries, DefaultDrainConfig())
	_, streamIDs := BuildStreams(entries, 0)
	chunks, err := AssembleChunks(entries, streamIDs, templateIDs, templates, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 {
		t.Fatalf("two entries one second apart under an hour window produced %d chunks", len(chunks))
	}

	timestamps, vars, err := DecodeChunk(chunks[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(timestamps) != 2 {
		t.Fatalf("the decoded chunk holds %d entries, want 2", len(timestamps))
	}
	if !timestamps[0].Equal(base) {
		t.Fatalf("decoded timestamp is %s, want %s", timestamps[0], base)
	}
	if len(vars[0]) == 0 {
		t.Fatalf("the chunk stored no variable values; the wildcard positions of %q carry the request id and the duration",
			templates[0].Pattern)
	}
}

// TestAssembleChunksRefusesAMismatchedParallelSlice — bad input errors.
func TestAssembleChunksRefusesAMismatchedParallelSlice(t *testing.T) {
	entries := []Entry{{Timestamp: at(1), Message: "x", Labels: map[string]string{"a": "1"}}}
	if _, err := AssembleChunks(entries, []string{"s"}, nil, nil, time.Hour); err == nil {
		t.Fatal("a template-id slice of the wrong length was accepted; the lengths must be refused, not indexed past")
	}
}

// TestAssembleChunksSkipsAnEntryWithNoTemplate — the empty-message class.
func TestAssembleChunksSkipsAnEntryWithNoTemplate(t *testing.T) {
	entries := []Entry{
		{Timestamp: at(1), Message: "", Labels: map[string]string{"a": "1"}},
		{Timestamp: at(2), Message: "real line", Labels: map[string]string{"a": "1"}},
	}
	templates, templateIDs := ProcessEntries(entries, DefaultDrainConfig())
	_, streamIDs := BuildStreams(entries, 0)
	if templateIDs[0] != "" {
		t.Fatalf("an empty message resolved to template %q", templateIDs[0])
	}
	chunks, err := AssembleChunks(entries, streamIDs, templateIDs, templates, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	total := 0
	for _, c := range chunks {
		total += c.EntryCount
	}
	if total != 1 {
		t.Fatalf("%d entries were chunked; the empty message must be skipped, not bucketed under an arbitrary template", total)
	}
}

// TestDecodeChunkRefusesATruncatedPayload — the corrupt-input arm.
func TestDecodeChunkRefusesATruncatedPayload(t *testing.T) {
	raw := encodeChunkData([]chunkEntry{{Timestamp: at(1), Vars: []string{"abcdef"}}})
	if _, err := decodeChunkData(raw[:len(raw)-3]); err == nil {
		t.Fatal("a truncated chunk payload decoded without error; it must be refused, not read past the end")
	}
}
